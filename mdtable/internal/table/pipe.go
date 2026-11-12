package table

import (
	"fmt"
	"github.com/ryo-aoki-pc/md-table-excel/internal/blocks"
	"github.com/ryo-aoki-pc/md-table-excel/internal/cellsyntax"
	"github.com/ryo-aoki-pc/md-table-excel/internal/htmltok"
	"github.com/ryo-aoki-pc/md-table-excel/internal/inline"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"regexp"
	"strings"
)

var br = regexp.MustCompile(`(?i)^<br[ \t]*/?>$`)
var canonOpen = regexp.MustCompile(`(?i)^<(ul|ol|li|dl|dt|dd)>$`)
var canonStart = regexp.MustCompile(`(?i)^<ol start=(?:"([` + pycompat.ReDigit + `]{1,9})"|'([` + pycompat.ReDigit + `]{1,9})'|([` + pycompat.ReDigit + `]{1,9}))>$`)
var canonClose = regexp.MustCompile(`(?i)^</(ul|ol|li|dl|dt|dd)>$`)

type pipeTag struct {
	name     string
	closing  bool
	start    int
	hasStart bool
}

// pipe_table._tag に対応する定型タグ。
func tag(t inline.Token) pipeTag {
	if t.Kind != "html" {
		return pipeTag{}
	}
	if m := canonOpen.FindStringSubmatch(t.Text); m != nil {
		return pipeTag{name: pycompat.Lower(m[1])}
	}
	if m := canonStart.FindStringSubmatch(t.Text); m != nil {
		v := ""
		for _, s := range m[1:] {
			if s != "" {
				v = s
				break
			}
		}
		n, _ := pycompat.Atoi(v)
		return pipeTag{name: "ol", start: n, hasStart: true}
	}
	if m := canonClose.FindStringSubmatch(t.Text); m != nil {
		return pipeTag{name: pycompat.Lower(m[1]), closing: true}
	}
	return pipeTag{}
}

// pipe_table._is_br に対応する改行タグ。
func isBR(t inline.Token) bool { return t.Kind == "html" && br.MatchString(t.Text) }

// pipe_table._skip_blank に対応する空白の読み飛ばし。
func skipBlank(ts []inline.Token, i int) int {
	for i < len(ts) && ts[i].Kind == "text" && strings.Trim(ts[i].Text, " \t") == "" {
		i++
	}
	return i
}

type pipeNode struct {
	kind, name string
	token      inline.Token
	ordered    bool
	start      int
	items      [][]pipeNode
	entries    []pipeEntry
}
type pipeEntry struct {
	name    string
	content []pipeNode
}

// pipe_table._parse_sequence に対応するセルの節。
func parseSequence(ts []inline.Token, i int, stop string) ([]pipeNode, int) {
	var nodes []pipeNode
	for i < len(ts) {
		t := ts[i]
		g := tag(t)
		if g.name != "" && g.closing && g.name == stop {
			return nodes, i
		}
		if isBR(t) {
			nodes = append(nodes, pipeNode{kind: "br"})
			i++
			continue
		}
		if g.name != "" && !g.closing && (g.name == "ul" || g.name == "ol" || g.name == "dl") {
			previousBR := len(nodes) > 0 && nodes[len(nodes)-1].kind == "br"
			if !previousBR {
				var node pipeNode
				var j int
				var ok bool
				if g.name == "dl" {
					node, j, ok = parseDL(ts, i)
				} else {
					node, j, ok = parseList(ts, i)
				}
				if ok && !(j < len(ts) && isBR(ts[j])) {
					nodes = append(nodes, node)
					i = j
					continue
				}
			}
		}
		nodes = append(nodes, pipeNode{kind: "inline", token: t})
		i++
	}
	return nodes, i
}

// pipe_table._parse_list に対応するリスト解析。
func parseList(ts []inline.Token, i int) (pipeNode, int, bool) {
	g := tag(ts[i])
	name, start := g.name, g.start
	if !g.hasStart {
		start = 1
	}
	i++
	var items [][]pipeNode
	for {
		i = skipBlank(ts, i)
		if i >= len(ts) {
			return pipeNode{}, i, false
		}
		g = tag(ts[i])
		if g == (pipeTag{name: name, closing: true}) {
			i++
			break
		}
		if g != (pipeTag{name: "li"}) {
			return pipeNode{}, i, false
		}
		content, j := parseSequence(ts, i+1, "li")
		if j >= len(ts) || tag(ts[j]) != (pipeTag{name: "li", closing: true}) {
			return pipeNode{}, i, false
		}
		items = append(items, content)
		i = j + 1
	}
	if len(items) == 0 {
		return pipeNode{}, i, false
	}
	return pipeNode{kind: "list", ordered: name == "ol", start: start, items: items}, i, true
}

// pipe_table._parse_dl に対応する説明リスト解析。
func parseDL(ts []inline.Token, i int) (pipeNode, int, bool) {
	i++
	var entries []pipeEntry
	for {
		i = skipBlank(ts, i)
		if i >= len(ts) {
			return pipeNode{}, i, false
		}
		g := tag(ts[i])
		if g == (pipeTag{name: "dl", closing: true}) {
			i++
			break
		}
		if g != (pipeTag{name: "dt"}) && g != (pipeTag{name: "dd"}) {
			return pipeNode{}, i, false
		}
		name := g.name
		content, j := parseSequence(ts, i+1, name)
		if j >= len(ts) || tag(ts[j]) != (pipeTag{name: name, closing: true}) {
			return pipeNode{}, i, false
		}
		if name == "dt" {
			if len(entries) > 0 && entries[len(entries)-1].name == "dt" {
				return pipeNode{}, i, false
			}
			for _, n := range content {
				if n.kind == "list" || n.kind == "dl" {
					return pipeNode{}, i, false
				}
			}
		} else if len(entries) == 0 {
			return pipeNode{}, i, false
		}
		entries = append(entries, pipeEntry{name, content})
		i = j + 1
	}
	if len(entries) == 0 || entries[len(entries)-1].name == "dt" {
		return pipeNode{}, i, false
	}
	return pipeNode{kind: "dl", entries: entries}, i, true
}

// pipe_table._line_text に対応する行の文字。
func lineText(ts []inline.Token) string {
	var b strings.Builder
	for _, t := range ts {
		b.WriteString(t.Text)
	}
	text := b.String()
	stripped := strings.TrimLeft(text, " \t")
	if stripped == "" {
		return ""
	}
	leading := len(text) - len(stripped)
	firstKind := ""
	position := 0
	for _, t := range ts {
		if position+len(t.Text) > leading {
			firstKind = t.Kind
			break
		}
		position += len(t.Text)
	}
	if firstKind == "text" {
		stripped = inline.EscapeLineStart(stripped)
	}
	return strings.TrimRight(stripped, " \t")
}

// pipe_table._render_list に対応するリストの描画。
func renderPipeList(n pipeNode, marker string) []string {
	var lines []string
	for i, item := range n.items {
		label := marker + " "
		if n.ordered {
			label = fmt.Sprintf("%d%s ", n.start+i, marker)
		}
		body := renderNodes(item)
		if len(body) == 0 {
			body = []string{""}
		}
		if cellsyntax.TaskMarker(body[0]) {
			body[0] = "\\" + body[0]
		}
		lines = append(lines, strings.TrimRight(label+body[0], " "))
		for _, line := range body[1:] {
			if line != "" {
				line = strings.Repeat(" ", pycompat.Len(label)) + line
			}
			lines = append(lines, line)
		}
	}
	return lines
}

// pipe_table._render_dl に対応する説明リストの描画。
func renderPipeDL(n pipeNode) []string {
	var lines []string
	for _, entry := range n.entries {
		body := renderNodes(entry.content)
		if len(body) == 0 {
			body = []string{""}
		}
		if entry.name == "dt" {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, body...)
		} else {
			first := ":"
			if body[0] != "" {
				first = ": " + body[0]
			}
			lines = append(lines, first)
			for _, line := range body[1:] {
				if line != "" {
					line = "  " + line
				}
				lines = append(lines, line)
			}
		}
	}
	return lines
}

type pipeGroup struct {
	kind  string
	value pipeNode
	nodes []pipeNode
}

// pipe_table._render_nodes と _inline_lines、_list_can_interrupt に対応する描画。
func renderNodes(nodes []pipeNode) []string {
	var groups []pipeGroup
	var current []pipeNode
	for _, n := range nodes {
		if n.kind == "list" || n.kind == "dl" {
			if len(current) > 0 {
				groups = append(groups, pipeGroup{kind: "para", nodes: current})
				current = nil
			}
			groups = append(groups, pipeGroup{kind: n.kind, value: n})
		} else {
			current = append(current, n)
		}
	}
	if len(current) > 0 {
		groups = append(groups, pipeGroup{kind: "para", nodes: current})
	}
	var lines []string
	previous, previousMarker := "", ""
	previousOrdered := false
	for _, g := range groups {
		if g.kind == "para" {
			if previous != "" {
				lines = append(lines, "")
			}
			var ts []inline.Token
			for _, n := range g.nodes {
				if n.kind == "br" {
					lines = append(lines, lineText(ts))
					ts = nil
				} else {
					ts = append(ts, n.token)
				}
			}
			lines = append(lines, lineText(ts))
			previousMarker = ""
		} else if g.kind == "list" {
			v := g.value
			marker, other := "-", "*"
			if v.ordered {
				marker, other = ".", ")"
			}
			if previousMarker == marker && previousOrdered == v.ordered {
				marker = other
			}
			interrupt := !v.ordered || v.start == 1
			if interrupt {
				first := renderNodes(v.items[0])
				interrupt = len(first) > 0 && first[0] != ""
			}
			if previous == "para" && !interrupt || previous == "dl" {
				lines = append(lines, "")
			}
			lines = append(lines, renderPipeList(v, marker)...)
			previousOrdered = v.ordered
			previousMarker = marker
		} else {
			if previous != "" {
				lines = append(lines, "")
			}
			lines = append(lines, renderPipeDL(g.value)...)
			previousMarker = ""
		}
		previous = g.kind
	}
	return lines
}

// pipe_table.cell_to_excel に対応するセルの正規化。
func PipeCellToExcel(s string) string {
	nodes, _ := parseSequence(inline.Tokenize(s, true), 0, "")
	return model.NormalizeText(strings.Join(renderNodes(nodes), "\n"))
}

type PipeRow struct {
	Line                      int
	Prefix, Body              string
	RawCells, Contents, Texts []string
}
type PipeTable struct {
	Header                                                  *PipeRow
	DelimiterPrefix, DelimiterBody                          string
	Rows                                                    []*PipeRow
	Alignments                                              []string
	LeadingPipe, TrailingPipe, DelimiterSpaced, SpacedCells bool
	Model                                                   *model.TableModel
}

// pipe_table._parse_row に対応する元の行。
func parsePipeRow(line int, prefix, body string) *PipeRow {
	parsed := blocks.ParseTableRow(body + "\n")
	r := &PipeRow{Line: line, Prefix: prefix, Body: body}
	for _, c := range parsed.Cells {
		r.RawCells = append(r.RawCells, blocks.StripASCIISpace(c.Raw))
		r.Contents = append(r.Contents, c.Content)
		r.Texts = append(r.Texts, PipeCellToExcel(c.Content))
	}
	return r
}

// pipe_table.analyze に対応する表のモデル。
func analyzePipe(m *MarkdownFile, r *Region) {
	t := &PipeTable{LeadingPipe: true, TrailingPipe: true, SpacedCells: true}
	t.Header = parsePipeRow(r.Start, r.Prefixes[0], r.Bodies[0])
	t.DelimiterPrefix = r.Prefixes[1]
	t.DelimiterBody = r.Bodies[1]
	for i := 2; i < len(r.Bodies); i++ {
		t.Rows = append(t.Rows, parsePipeRow(r.Start+i, r.Prefixes[i], r.Bodies[i]))
	}
	t.Alignments = append([]string{}, r.Node.Info.Alignments...)
	columns := len(t.Alignments)
	hb := strings.TrimRight(r.Bodies[0], " \t")
	t.LeadingPipe = strings.HasPrefix(strings.TrimLeft(r.Bodies[0], " \t"), "|")
	t.TrailingPipe = strings.HasSuffix(hb, "|") && !strings.HasSuffix(hb, "\\|")
	for _, c := range blocks.ParseTableRow(t.DelimiterBody + "\n").Cells {
		if strings.HasSuffix(c.Raw, " ") || strings.HasSuffix(c.Raw, "\t") {
			t.DelimiterSpaced = true
		}
	}
	rawCount, spaced := 0, false
	for _, c := range blocks.ParseTableRow(r.Bodies[0] + "\n").Cells {
		if pycompat.Strip(c.Raw) != "" {
			rawCount++
			if strings.HasSuffix(c.Raw, " ") || strings.HasSuffix(c.Raw, "\t") {
				spaced = true
			}
		}
	}
	t.SpacedCells = rawCount == 0 || spaced
	var cells []*model.Cell
	for rowIndex, row := range append([]*PipeRow{t.Header}, t.Rows...) {
		if len(row.Contents) > columns {
			r.Warnings = append(r.Warnings, fmt.Sprintf("%d 行目: 見出しより多いセルがある（GitLab では表示されない）。この行を編集すると消える", row.Line+1))
		}
		for col := 0; col < columns; col++ {
			text := ""
			if col < len(row.Texts) {
				text = row.Texts[col]
			}
			c := model.NewCell(rowIndex, col, text)
			c.Header = rowIndex == 0
			c.Align = t.Alignments[col]
			cells = append(cells, c)
		}
	}
	t.Model = model.New("pipe", len(t.Rows)+1, columns, cells, nil)
	r.Pipe = t
}

var delimiters = map[string]string{"": "---", "left": ":---", "right": "---:", "center": ":---:"}

// pipe_table._join_cells に対応する行の連結。
func joinCells(parts []string, leading, trailing bool) string {
	body := strings.Join(parts, "|")
	if !leading {
		body = strings.TrimLeft(body, " ")
	}
	if !trailing {
		body = strings.TrimRight(body, " ")
	}
	if leading {
		body = "|" + body
	}
	if trailing {
		body += "|"
	}
	return body
}

// pipe_table._format_row に対応する行の整形。
func formatRow(cells []string, t *PipeTable) string {
	parts := append([]string{}, cells...)
	if t.SpacedCells {
		for i, c := range cells {
			if c == "" {
				parts[i] = " "
			} else {
				parts[i] = " " + c + " "
			}
		}
	}
	leading := t.LeadingPipe || cells[0] == "" || inline.EscapeLineStart(cells[0]) != cells[0]
	trailing := t.TrailingPipe || cells[len(cells)-1] == ""
	return joinCells(parts, leading, trailing)
}

// pipe_table._format_delimiter に対応する区切り行。
func formatDelimiter(aligns []string, t *PipeTable) string {
	var parts []string
	for _, a := range aligns {
		spec := delimiters[a]
		if t.DelimiterSpaced {
			spec = " " + spec + " "
		}
		parts = append(parts, spec)
	}
	return joinCells(parts, t.LeadingPipe, t.TrailingPipe)
}

var alignStyle = regexp.MustCompile(`(?i)^[` + pycompat.ReSpace + `]*text-align[` + pycompat.ReSpace + `]*:[` + pycompat.ReSpace + `]*(left|center|right|justify)[` + pycompat.ReSpace + `]*;?[` + pycompat.ReSpace + `]*$`)

// pipe_table._check_conversion_attributes の attribute.value or "" に対応する参照。
func attrValue(a htmltok.Attribute) string {
	if a.Value == nil {
		return ""
	}
	return *a.Value
}

// pipe_table._check_conversion_attributes に対応する失われる属性の検査。
func checkConversionAttributes(t *htmltok.Token, row, col int) error {
	if t == nil {
		return nil
	}
	isCell := t.Name == "td" || t.Name == "th"
	for _, a := range t.Attributes {
		if isCell && a.Name == "align" {
			if pycompat.Lower(pycompat.Strip(attrValue(a))) == "justify" {
				return &cellsyntax.NotRepresentable{Message: "justify の配置がある", Row: row, Col: col}
			}
			continue
		}
		if isCell && a.Name == "style" {
			if m := alignStyle.FindStringSubmatch(attrValue(a)); m != nil {
				if pycompat.Lower(m[1]) == "justify" {
					return &cellsyntax.NotRepresentable{Message: "justify の配置がある", Row: row, Col: col}
				}
				continue
			}
		}
		if isCell && (a.Name == "rowspan" || a.Name == "colspan") && pycompat.Strip(attrValue(a)) == "1" {
			continue
		}
		return &cellsyntax.NotRepresentable{Message: fmt.Sprintf("<%s> にパイプ表では残せない属性（%s）がある", t.Name, a.Name), Row: row, Col: col}
	}
	return nil
}

// pipe_table.render_new に対応する HTML からパイプ表への変換。
func RenderNewPipe(r *Region, m *model.TableModel, warnings *[]string) ([]string, error) {
	if m.Rows < 1 || m.Cols < 1 {
		return nil, cellsyntax.Cannot("見出し行と 1 列以上の列が要る")
	}
	for _, c := range m.Cells {
		if c.Rowspan > 1 || c.Colspan > 1 {
			return nil, &cellsyntax.NotRepresentable{Message: "セル結合がある", Row: c.Row + 1, Col: c.Col + 1}
		}
	}
	if len(m.Holes) > 0 {
		row, col := m.Rows, m.Cols
		for p := range m.Holes {
			if p.Row < row || p.Row == row && p.Col < col {
				row, col = p.Row, p.Col
			}
		}
		return nil, &cellsyntax.NotRepresentable{Message: "セルの無い位置がある", Row: row + 1, Col: col + 1}
	}
	for _, c := range m.Cells {
		if c.Header != (c.Row == 0) {
			return nil, &cellsyntax.NotRepresentable{Message: "1 行目はすべて見出しセル、2 行目以降は見出しでないセルにする必要がある", Row: c.Row + 1, Col: c.Col + 1}
		}
	}
	var aligns []string
	for col := 0; col < m.Cols; col++ {
		var column []*model.Cell
		for _, c := range m.Cells {
			if c.Col == col {
				column = append(column, c)
			}
		}
		a := column[0].Align
		for _, c := range column {
			reason := ""
			if c.Align == "justify" {
				reason = "justify の配置がある"
			} else if c.Align != a {
				reason = "列の中でセルの配置が違う"
			}
			if reason != "" {
				return nil, &cellsyntax.NotRepresentable{Message: reason, Row: c.Row + 1, Col: c.Col + 1}
			}
		}
		aligns = append(aligns, a)
	}
	t := r.HTML
	if t.Caption != nil {
		return nil, cellsyntax.Cannot("<caption> がある")
	}
	if len(t.Colgroups) > 0 {
		return nil, cellsyntax.Cannot("<colgroup> または <col> がある")
	}
	for _, s := range t.Sections {
		if s.Tag == "tfoot" {
			return nil, cellsyntax.Cannot("<tfoot> がある")
		}
	}
	for _, s := range t.Sections {
		if s.Tag == "thead" && len(s.Rows) > 1 {
			return nil, cellsyntax.Cannot("<thead> に 2 行以上ある")
		}
	}
	if t.Interstitial {
		return nil, cellsyntax.Cannot("表の構造の間にコメントなどがある")
	}
	if r.Head != "" || r.Tail != "" {
		return nil, cellsyntax.Cannot("<table> の前か </table> の後に同じ行の文字がある")
	}
	if e := checkConversionAttributes(t.TableToken, 0, 0); e != nil {
		return nil, e
	}
	for _, s := range t.Sections {
		if e := checkConversionAttributes(s.Token, 0, 0); e != nil {
			return nil, e
		}
	}
	for _, row := range t.Rows {
		if e := checkConversionAttributes(row.Token, row.Index+1, 1); e != nil {
			return nil, e
		}
	}
	for _, c := range t.Cells {
		if e := checkConversionAttributes(c.StartToken, c.Row+1, c.Col+1); e != nil {
			return nil, e
		}
	}
	grid := make([][]string, m.Rows)
	for i := range grid {
		grid[i] = make([]string, m.Cols)
	}
	for _, c := range m.Cells {
		for _, token := range inline.Tokenize(c.Text, true) {
			if token.Kind == "html" && cellsyntax.Structural[token.Tag] {
				return nil, &cellsyntax.NotRepresentable{Message: "セルの中に入れ子の表（<table> など）がある", Row: c.Row + 1, Col: c.Col + 1}
			}
		}
		s, e := cellsyntax.ToPipeCell(c.Text)
		if e != nil {
			return nil, &cellsyntax.NotRepresentable{Message: e.Error(), Row: c.Row + 1, Col: c.Col + 1}
		}
		grid[c.Row][c.Col] = s
	}
	formatting := &PipeTable{LeadingPipe: true, TrailingPipe: true, DelimiterSpaced: true, SpacedCells: true}
	var out []string
	for row, cells := range grid {
		prefix := r.ContPrefix
		if row == 0 {
			prefix = r.FirstPrefix()
		}
		out = append(out, prefix+formatRow(cells, formatting))
		if row == 0 {
			out = append(out, r.ContPrefix+formatDelimiter(aligns, formatting))
		}
	}
	if r.NextNonblank {
		out = append(out, r.BlankPrefix())
	}
	return out, nil
}
