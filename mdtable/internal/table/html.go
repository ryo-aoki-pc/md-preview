package table

import (
	"fmt"
	"github.com/ryo-aoki-pc/md-table-excel/internal/blocks"
	"github.com/ryo-aoki-pc/md-table-excel/internal/cellfromhtml"
	"github.com/ryo-aoki-pc/md-table-excel/internal/cellsyntax"
	"github.com/ryo-aoki-pc/md-table-excel/internal/htmltok"
	"github.com/ryo-aoki-pc/md-table-excel/internal/inline"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"regexp"
	"sort"
	"strings"
)

var textAlign = regexp.MustCompile(`(?i)(^|;)[` + pycompat.ReSpace + `]*text-align[` + pycompat.ReSpace + `]*:[` + pycompat.ReSpace + `]*([a-zA-Z-]+)[` + pycompat.ReSpace + `]*(;|$)`)
var alignValues = map[string]bool{"left": true, "center": true, "right": true, "justify": true}

type blockStart struct{ position, line, offset int }

// html_table._block_text に対応する HTML ブロックの中身。
func blockText(m *MarkdownFile, n *blocks.Node) (string, []blockStart) {
	var parts []string
	var starts []blockStart
	position := 0
	for _, l := range n.Lines {
		starts = append(starts, blockStart{position, l.Number, l.Offset})
		body := m.Lines[l.Number][l.Offset:]
		parts = append(parts, body)
		position += len(body) + 1
	}
	return strings.Join(parts, "\n"), starts
}

// html_table._locate に対応する元の行位置。
func locate(starts []blockStart, pos int) (int, int) {
	for i := len(starts) - 1; i >= 0; i-- {
		s := starts[i]
		if pos >= s.position {
			return s.line, s.offset + pos - s.position
		}
	}
	return starts[0].line, starts[0].offset
}

// html_table._chunk_has_structural に対応する Markdown 部分の構造タグ。
func chunkHasStructural(m *MarkdownFile, n *blocks.Node) bool {
	for _, sub := range n.Walk() {
		if sub.Kind == blocks.CodeBlock && sub.Info.Fenced {
			continue
		}
		if sub.Kind == blocks.Paragraph || sub.Kind == blocks.Heading || sub.Kind == blocks.HTMLBlock || sub.Kind == blocks.CodeBlock {
			var parts []string
			for _, l := range sub.Lines {
				parts = append(parts, m.Lines[l.Number][l.Offset:])
			}
			for _, t := range inline.Tokenize(strings.Join(parts, "\n"), true) {
				if t.Kind == "html" && cellsyntax.Structural[t.Tag] {
					return true
				}
			}
		}
		if sub.Kind == blocks.Table {
			for i := sub.StartLine; i <= sub.EndLine; i++ {
				for _, t := range inline.Tokenize(m.Lines[i], true) {
					if t.Kind == "html" && cellsyntax.Structural[t.Tag] {
						return true
					}
				}
			}
		}
	}
	return false
}

// html_table._constructed_prefix に対応する続きの行の接頭辞。
func constructedPrefix(c *blocks.Node) string {
	var chain []*blocks.Node
	for n := c; n != nil && n.Kind != blocks.DocumentKind; n = n.Parent {
		chain = append(chain, n)
	}
	var b strings.Builder
	for i := len(chain) - 1; i >= 0; i-- {
		n := chain[i]
		switch n.Kind {
		case blocks.BlockQuote, blocks.Alert:
			b.WriteString("> ")
		case blocks.Item, blocks.DescriptionItem:
			b.WriteString(strings.Repeat(" ", n.Info.MarkerOffset+n.Info.Padding))
		case blocks.FootnoteDefinition:
			b.WriteString("    ")
		}
	}
	return b.String()
}

type segment struct {
	kind string
	node *blocks.Node
}
type pending struct {
	segments         []segment
	startCol, endCol int
}

// html_table._fill_lines に対応するコンテナ接頭辞の切り分け。
func fillHTMLLines(m *MarkdownFile, r *Region, c *blocks.Node) {
	r.Container = c
	for line := r.Start; line <= r.End; line++ {
		s := m.Lines[line]
		offset, ok := m.Doc().LineInfos[line].OffsetAfter(c)
		if !ok {
			if r.Excluded == "" {
				r.Excluded = fmt.Sprintf("%d 行目が引用やリストの続きになっていない", line+1)
			}
			offset = 0
		}
		r.Prefixes = append(r.Prefixes, s[:offset])
		r.Bodies = append(r.Bodies, s[offset:])
	}
	var continuation []string
	unique := map[string]bool{}
	tabbed := false
	for i := 1; i < len(r.Bodies); i++ {
		p := r.Prefixes[i]
		if pycompat.Strip(r.Bodies[i]) != "" {
			continuation = append(continuation, p)
			unique[p] = true
		}
		if strings.Contains(p, "\t") {
			tabbed = true
		}
	}
	r.ContPrefix = constructedPrefix(c)
	if len(continuation) > 0 {
		r.ContPrefix = continuation[0]
	}
	if tabbed && len(unique) > 1 && r.Excluded == "" {
		r.Excluded = "タブを含む字下げが行ごとに違う"
	}
}

// html_table._scan に対応する開始タグから終了タグまでの走査。
func scanHTML(m *MarkdownFile, c *blocks.Node, children []*blocks.Node, index int) (*Region, int) {
	first := children[index]
	text, starts := blockText(m, first)
	tokens := htmltok.Tokenize(text, 0, len(text))
	var startToken *htmltok.Token
	for _, t := range tokens {
		if t.Kind == "start" && t.Name == "table" {
			startToken = t
			break
		}
	}
	if startToken == nil {
		return nil, index
	}
	startLine, startCol := locate(starts, startToken.Start)
	depth := 0
	endLine, endCol := -1, -1
	var segments []segment
	unsupported := ""
	current := index
	for current < len(children) {
		n := children[current]
		if n.Kind == blocks.HTMLBlock {
			nt, ns := blockText(m, n)
			for _, t := range htmltok.Tokenize(nt, 0, len(nt)) {
				if current == index && t.Start < startToken.Start {
					continue
				}
				if t.Kind == "start" && t.Name == "table" {
					depth++
				} else if t.Kind == "end" && t.Name == "table" {
					depth--
					if depth == 0 {
						endLine, endCol = locate(ns, t.End)
						break
					}
				}
			}
			segments = append(segments, segment{"html", n})
			if endLine >= 0 {
				break
			}
		} else {
			if chunkHasStructural(m, n) {
				unsupported = "セルの中の Markdown の部分に表の構造のタグ（<td> など）がある（GitLab でも崩れる）"
			}
			segments = append(segments, segment{"md", n})
		}
		current++
	}
	if endLine < 0 {
		last := children[len(children)-1]
		r := &Region{Kind: "html", Start: startLine, End: max(startLine, last.EndLine), Excluded: "対応する </table> が同じ場所（コンテナ）の HTML の中に無い（字下げや空行で GitLab でも崩れる）"}
		fillHTMLLines(m, r, c)
		return r, len(children)
	}
	r := &Region{Kind: "html", Start: startLine, End: endLine}
	fillHTMLLines(m, r, c)
	startCol -= len(r.Prefixes[0])
	endCol -= len(r.Prefixes[len(r.Prefixes)-1])
	if r.Excluded == "" {
		r.Head = r.Bodies[0][:startCol]
		r.Tail = r.Bodies[len(r.Bodies)-1][endCol:]
	}
	r.Pending = &pending{segments, startCol, endCol}
	if unsupported != "" && r.Excluded == "" {
		r.Excluded = unsupported
	}
	following := endLine + 1
	if following < len(m.Lines) {
		if offset, ok := m.Doc().LineInfos[following].OffsetAfter(c); ok {
			r.NextNonblank = pycompat.Strip(m.Lines[following][offset:]) != ""
		}
	}
	return r, current + 1
}

// html_table.find_regions に対応する HTML の表の範囲。
func findHTMLRegions(m *MarkdownFile) []*Region {
	var regions []*Region
	for _, c := range m.Doc().Root.Walk() {
		if !blocks.BlockContainers[c.Kind] {
			continue
		}
		children := append([]*blocks.Node{}, c.Children...)
		for index := 0; index < len(children); {
			if children[index].Kind == blocks.HTMLBlock {
				if r, next := scanHTML(m, c, children, index); r != nil {
					regions = append(regions, r)
					index = next
					continue
				}
			}
			index++
		}
	}
	sort.SliceStable(regions, func(i, j int) bool {
		if regions[i].Start != regions[j].Start {
			return regions[i].Start < regions[j].Start
		}
		return regions[i].End > regions[j].End
	})
	var result []*Region
	for _, r := range regions {
		inside := false
		for _, other := range result {
			if other != r && other.Start <= r.Start && r.End <= other.End {
				inside = true
				break
			}
		}
		if !inside {
			result = append(result, r)
		}
	}
	return result
}

type HTMLCell struct {
	Tag                        string
	StartToken, EndToken       *htmltok.Token
	ContentStart, ContentEnd   int
	Tokens                     []*htmltok.Token
	Row, Col, Rowspan, Colspan int
	Text                       string
	LinkDefinitions            bool
}
type HTMLRow struct {
	Token, EndToken *htmltok.Token
	Section         *HTMLSection
	Cells           []*HTMLCell
	Index           int
}
type HTMLSection struct {
	Token *htmltok.Token
	Tag   string
	Rows  []*HTMLRow
}
type HTMLTable struct {
	Text                     string
	Start, End               int
	TableToken               *htmltok.Token
	Caption                  *string
	Colgroups                []string
	Sections                 []*HTMLSection
	ExplicitSections         bool
	Rows                     []*HTMLRow
	Cells                    []*HTMLCell
	Interstitial, AlignStyle bool
	Model                    *model.TableModel
	CellAt                   map[model.Position]*HTMLCell
}

// html_table.HtmlCell.attribute に対応する属性検索。
func (c *HTMLCell) Attribute(name string) *string { return c.StartToken.Attribute(name) }

var spanNumber = regexp.MustCompile(`^[` + pycompat.ReSpace + `]*([` + pycompat.ReDigit + `]+)`)

// html_table._parse_span に対応する結合範囲。
func parseSpan(value *string, def, maximum int) (int, bool) {
	if value == nil {
		return def, false
	}
	m := spanNumber.FindStringSubmatch(*value)
	if m == nil {
		return def, false
	}
	n, ok := pycompat.Atoi(m[1])
	if !ok {
		return maximum, true
	}
	return min(n, maximum), true
}

// html_table._cell_align に対応する配置。
func cellAlign(c *HTMLCell) (string, bool) {
	if value := c.Attribute("align"); value != nil {
		v := pycompat.Lower(pycompat.Strip(*value))
		if alignValues[v] {
			return v, false
		}
	}
	if style := c.Attribute("style"); style != nil && *style != "" {
		if m := textAlign.FindStringSubmatch(*style); m != nil {
			v := pycompat.Lower(m[2])
			if alignValues[v] {
				return v, true
			}
		}
	}
	return "", false
}

// html_table._tokens_for_region に対応する HTML と Markdown 部分の字句。
func tokensForRegion(r *Region, p *pending) (string, int, int, []*htmltok.Token) {
	bodies := r.Bodies
	text := strings.Join(bodies, "\n")
	var lineStarts []int
	position := 0
	for _, body := range bodies {
		lineStarts = append(lineStarts, position)
		position += len(body) + 1
	}
	tableStart := lineStarts[0] + p.startCol
	tableEnd := lineStarts[len(lineStarts)-1] + p.endCol
	var ranges [][2]int
	var chunk *[2]int
	for _, seg := range p.segments {
		first, last := seg.node.StartLine-r.Start, seg.node.EndLine-r.Start
		if seg.kind == "md" {
			if chunk == nil {
				chunk = &[2]int{first, last}
			} else {
				chunk[1] = last
			}
		} else if chunk != nil {
			ranges = append(ranges, *chunk)
			chunk = nil
		}
	}
	if chunk != nil {
		ranges = append(ranges, *chunk)
	}
	var tokens []*htmltok.Token
	position = tableStart
	for _, span := range ranges {
		first, last := span[0], span[1]
		start, end := lineStarts[first], lineStarts[last]+len(bodies[last])
		tokens = append(tokens, htmltok.Tokenize(text, position, start)...)
		tokens = append(tokens, &htmltok.Token{Kind: "md", Start: start, End: end, Text: strings.Join(bodies[first:last+1], "\n")})
		position = end
	}
	tokens = append(tokens, htmltok.Tokenize(text, position, tableEnd)...)
	return text, tableStart, tableEnd, tokens
}

// html_table.analyze に対応する HTML 表の解析。
func analyzeHTML(m *MarkdownFile, r *Region) {
	t := &HTMLTable{CellAt: map[model.Position]*HTMLCell{}}
	text, start, end, tokens := tokensForRegion(r, r.Pending)
	t.Text, t.Start, t.End = text, start, end
	e := parseStructure(t, tokens)
	if e == nil {
		e = buildGrid(t)
	}
	if e == nil {
		markLinkDefinitions(t)
	}
	if e != nil {
		r.Excluded = e.Error()
		r.HTML = nil
		return
	}
	r.HTML = t
	r.Pending = nil
}

// html_table._parse_structure に対応する省略タグを含む表の構造。
func parseStructure(t *HTMLTable, tokens []*htmltok.Token) error {
	if len(tokens) == 0 || tokens[0].Kind != "start" || tokens[0].Name != "table" {
		return cellsyntax.Cannot("<table> を読めない")
	}
	t.TableToken = tokens[0]
	var section *HTMLSection
	var row *HTMLRow
	var cell *HTMLCell
	nested := 0
	caption, colgroup := -1, -1
	finished := false
	closeCell := func(pos int) {
		if cell != nil {
			if cell.EndToken == nil {
				cell.ContentEnd = pos
			}
			cell = nil
		}
	}
	ensureSection := func() *HTMLSection {
		if section == nil {
			section = &HTMLSection{Tag: "tbody"}
			t.Sections = append(t.Sections, section)
		}
		return section
	}
	for _, token := range tokens[1:] {
		if finished {
			if token.Kind == "text" && pycompat.Strip(token.Text) == "" {
				continue
			}
			return cellsyntax.Cannot("</table> の後に続きがある")
		}
		if cell != nil && nested > 0 {
			cell.Tokens = append(cell.Tokens, token)
			if token.Kind == "start" && token.Name == "table" {
				nested++
			} else if token.Kind == "end" && token.Name == "table" {
				nested--
			}
			continue
		}
		if caption >= 0 {
			if token.Kind == "end" && token.Name == "caption" {
				s := t.Text[caption:token.End]
				t.Caption = &s
				caption = -1
			}
			continue
		}
		if colgroup >= 0 {
			if token.Kind == "end" && token.Name == "colgroup" {
				t.Colgroups = append(t.Colgroups, t.Text[colgroup:token.End])
				colgroup = -1
				continue
			}
			if token.Kind == "start" && token.Name == "col" {
				continue
			}
			if token.Kind == "text" && pycompat.Strip(token.Text) == "" {
				continue
			}
			t.Colgroups = append(t.Colgroups, t.Text[colgroup:token.Start])
			colgroup = -1
		}
		kind, name := token.Kind, token.Name
		if kind == "start" && (name == "td" || name == "th") {
			closeCell(token.Start)
			if row == nil {
				row = &HTMLRow{Section: ensureSection()}
				row.Section.Rows = append(row.Section.Rows, row)
			}
			cell = &HTMLCell{Tag: name, StartToken: token, ContentStart: token.End, ContentEnd: token.End, Rowspan: 1, Colspan: 1}
			row.Cells = append(row.Cells, cell)
			t.Cells = append(t.Cells, cell)
			continue
		}
		if kind == "end" && (name == "td" || name == "th") {
			if cell != nil && cell.Tag == name {
				cell.EndToken = token
				cell.ContentEnd = token.Start
				cell = nil
			} else if cell != nil {
				cell.Tokens = append(cell.Tokens, token)
			}
			continue
		}
		if kind == "start" && name == "tr" {
			closeCell(token.Start)
			row = &HTMLRow{Token: token, Section: ensureSection()}
			row.Section.Rows = append(row.Section.Rows, row)
			continue
		}
		if kind == "end" && name == "tr" {
			closeCell(token.Start)
			if row != nil {
				row.EndToken = token
			}
			row = nil
			continue
		}
		if kind == "start" && (name == "thead" || name == "tbody" || name == "tfoot") {
			closeCell(token.Start)
			row = nil
			section = &HTMLSection{Token: token, Tag: name}
			t.Sections = append(t.Sections, section)
			t.ExplicitSections = true
			continue
		}
		if kind == "end" && (name == "thead" || name == "tbody" || name == "tfoot") {
			closeCell(token.Start)
			row = nil
			section = nil
			continue
		}
		if kind == "end" && name == "table" {
			closeCell(token.Start)
			finished = true
			continue
		}
		if kind == "start" && name == "table" {
			if cell != nil {
				cell.Tokens = append(cell.Tokens, token)
				nested = 1
				continue
			}
			return cellsyntax.Cannot("セルの外に入れ子の <table> がある")
		}
		if cell != nil {
			if kind == "start" && (htmltok.RawTextElements[name] || name == "caption" || name == "colgroup" || name == "col") {
				return cellsyntax.Cannot(fmt.Sprintf("セルの中に <%s> がある", name))
			}
			cell.Tokens = append(cell.Tokens, token)
			continue
		}
		if kind == "start" && name == "caption" && len(t.Sections) == 0 && t.Caption == nil {
			caption = token.Start
			continue
		}
		if kind == "start" && name == "colgroup" && len(t.Sections) == 0 {
			colgroup = token.Start
			continue
		}
		if kind == "start" && name == "col" && len(t.Sections) == 0 {
			t.Colgroups = append(t.Colgroups, token.Text)
			continue
		}
		if kind == "text" && pycompat.Strip(token.Text) == "" {
			continue
		}
		if kind == "comment" || kind == "other" {
			t.Interstitial = true
			continue
		}
		return cellsyntax.Cannot("表の構造（<tr> など）の間に文字や要素がある（GitLab では表の外に出る）")
	}
	if !finished {
		return cellsyntax.Cannot("</table> が無い")
	}
	foot := -1
	for i, s := range t.Sections {
		if s.Tag == "tfoot" && foot < 0 {
			foot = i
		}
		if s.Tag == "tbody" && foot >= 0 && i > foot {
			return cellsyntax.Cannot("<tfoot> が <tbody> の前にある（表示の順が元の順と違う）")
		}
	}
	return nil
}

// html_table._check_markdown_parents に対応する Markdown の親の検査。
func checkMarkdownParents(tree *htmltok.Node) error {
	var walk func(*htmltok.Node, string) error
	walk = func(e *htmltok.Node, inside string) error {
		for _, ch := range e.Children {
			if ch.Kind == "md" && inside != "" {
				return cellsyntax.Cannot(fmt.Sprintf("<%s> の中に空行がある（GitLab でも崩れる）", inside))
			}
			if ch.Kind == "element" {
				next := inside
				if next == "" && (ch.Tag == "pre" || ch.Tag == "code" || ch.Tag == "textarea") {
					next = ch.Tag
				}
				if err := walk(ch, next); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(tree, "")
}

// html_table._build_grid に対応する結合を含む格子。
func buildGrid(t *HTMLTable) error {
	occupied := map[model.Position]*HTMLCell{}
	rowIndex := 0
	for _, s := range t.Sections {
		sectionEnd := rowIndex + len(s.Rows)
		for _, row := range s.Rows {
			row.Index = rowIndex
			t.Rows = append(t.Rows, row)
			col := 0
			for _, cell := range row.Cells {
				for occupied[model.Position{Row: rowIndex, Col: col}] != nil {
					col++
				}
				colspan, _ := parseSpan(cell.Attribute("colspan"), 1, 1000)
				if colspan == 0 {
					colspan = 1
				}
				rowspan, ok := parseSpan(cell.Attribute("rowspan"), 1, 65534)
				if ok && rowspan == 0 {
					rowspan = sectionEnd - rowIndex
				}
				rowspan = max(1, min(rowspan, sectionEnd-rowIndex))
				for r := rowIndex; r < rowIndex+rowspan; r++ {
					for c := col; c < col+colspan; c++ {
						p := model.Position{Row: r, Col: c}
						if occupied[p] != nil {
							return cellsyntax.Cannot("セルが重なっている（rowspan / colspan）")
						}
						occupied[p] = cell
					}
				}
				cell.Row, cell.Col, cell.Rowspan, cell.Colspan = rowIndex, col, rowspan, colspan
				col += colspan
			}
			rowIndex++
		}
	}
	rows, cols := rowIndex, 0
	for p := range occupied {
		cols = max(cols, p.Col+1)
	}
	if rows == 0 || cols == 0 {
		return cellsyntax.Cannot("セルが無い")
	}
	holes := map[model.Position]bool{}
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			p := model.Position{Row: r, Col: c}
			if occupied[p] == nil {
				holes[p] = true
			}
		}
	}
	styles := 0
	var cells []*model.Cell
	for _, cell := range t.Cells {
		tree := htmltok.BuildTree(cell.Tokens)
		if e := checkMarkdownParents(tree); e != nil {
			return e
		}
		cell.Text = cellfromhtml.Convert(tree.Children, t.Text)
		align, byStyle := cellAlign(cell)
		if byStyle {
			styles++
		}
		cells = append(cells, &model.Cell{Row: cell.Row, Col: cell.Col, Text: cell.Text, Rowspan: cell.Rowspan, Colspan: cell.Colspan, Header: cell.Tag == "th", Align: align})
		t.CellAt[model.Position{Row: cell.Row, Col: cell.Col}] = cell
	}
	t.AlignStyle = styles > 0
	t.Model = model.New("html", rows, cols, cells, holes)
	return nil
}

// html_table._mark_link_definitions に対応する元の位置からのリンク参照の定義の検出。
func markLinkDefinitions(t *HTMLTable) {
	if !strings.Contains(t.Text, "]:") {
		return
	}
	doc := blocks.ParseText(t.Text, false, true)
	var starts []int
	position := 0
	for _, line := range doc.Lines {
		starts = append(starts, position)
		position += len(line) + 1
	}
	for _, node := range doc.Root.Walk() {
		if node.Kind != blocks.Paragraph {
			continue
		}
		var parts []string
		for _, line := range node.Lines {
			parts = append(parts, doc.Lines[line.Number][line.Offset:])
		}
		content := strings.Join(parts, "\n")
		if blocks.StripLinkReferenceDefinitions(content) == content {
			continue
		}
		first := node.Lines[0]
		position = starts[first.Number] + first.Offset
		for _, cell := range t.Cells {
			if cell.ContentStart <= position && position < cell.ContentEnd {
				cell.LinkDefinitions = true
				break
			}
		}
	}
}

var textAlignStart = regexp.MustCompile(`(?i)^text-align[` + pycompat.ReSpace + `]*:`)

// html_table._without_text_align に対応する配置以外のスタイル。
func withoutTextAlign(s string) string {
	var kept []string
	for _, p := range strings.Split(s, ";") {
		p = pycompat.Strip(p)
		if p != "" && !textAlignStart.MatchString(p) {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "; ")
}

// html_table._start_tag に対応する属性を保った開始タグ。
func startTag(attrs []htmltok.Attribute, tag, align string, alignStyle bool, rowspan, colspan int, keepSpans bool) string {
	parts := []string{"<" + tag}
	style := ""
	for _, a := range attrs {
		if a.Name == "rowspan" || a.Name == "colspan" {
			if keepSpans {
				parts = append(parts, " "+a.Raw)
			}
			continue
		}
		if a.Name == "align" {
			continue
		}
		if a.Name == "style" {
			style = withoutTextAlign(attrValue(a))
			continue
		}
		parts = append(parts, " "+a.Raw)
	}
	if !keepSpans {
		if rowspan > 1 {
			parts = append(parts, fmt.Sprintf(` rowspan="%d"`, rowspan))
		}
		if colspan > 1 {
			parts = append(parts, fmt.Sprintf(` colspan="%d"`, colspan))
		}
	}
	if align != "" {
		if alignStyle {
			if style != "" {
				style += "; "
			}
			style += "text-align: " + align
		} else {
			parts = append(parts, ` align="`+align+`"`)
		}
	}
	if style != "" {
		parts = append(parts, ` style="`+strings.ReplaceAll(style, `"`, "&quot;")+`"`)
	}
	parts = append(parts, ">")
	return strings.Join(parts, "")
}

// html_table.render_from_pipe に対応するパイプ表からの生成。
func RenderFromPipe(r *Region, m *model.TableModel, warnings *[]string) ([]string, error) {
	result, e := regenerateHTML(r, m, warnings, true)
	if e != nil {
		return nil, e
	}
	if r.NextNonblank {
		result = append(result, r.BlankPrefix())
	}
	return result, nil
}
