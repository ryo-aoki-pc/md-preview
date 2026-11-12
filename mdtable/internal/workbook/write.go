package workbook

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
	"github.com/ryo-aoki-pc/md-table-excel/internal/textwidth"
)

const mainNS = `http://schemas.openxmlformats.org/spreadsheetml/2006/main`
const relNS = `http://schemas.openxmlformats.org/officeDocument/2006/relationships`

func xmlText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func column(n int) string {
	s := ""
	for n > 0 {
		n--
		s = string(rune('A'+n%26)) + s
		n /= 26
	}
	return s
}

func address(row, col int) string { return column(col) + strconv.Itoa(row) }

type style struct {
	font, fill, border, num int
	horizontal, vertical    string
	wrap                    bool
}

type styles struct {
	family string
	items  []style
}

func (s *styles) id(v style) int {
	for i, existing := range s.items {
		if existing == v {
			return i
		}
	}
	s.items = append(s.items, v)
	return len(s.items) - 1
}

func alignment(v style) string {
	if v.horizontal == "" && v.vertical == "" && !v.wrap {
		return ""
	}
	x := "<alignment"
	if v.horizontal != "" {
		x += ` horizontal="` + xmlText(v.horizontal) + `"`
	}
	if v.vertical != "" {
		x += ` vertical="` + v.vertical + `"`
	}
	if v.wrap {
		x += ` wrapText="1"`
	}
	return x + "/>"
}

func (s *styles) XML() string {
	var b strings.Builder
	fmt.Fprintf(&b, `<styleSheet xmlns="%s"><fonts count="4"><font><name val="Calibri"/><family val="2"/><color theme="1"/><sz val="11"/><scheme val="minor"/></font>`, mainNS)
	for _, extra := range []string{"", "<b/>", `<color rgb="000563C1"/><u val="single"/>`} {
		fmt.Fprintf(&b, `<font><name val="%s"/><sz val="10.5"/>%s</font>`, xmlText(s.family), extra)
	}
	b.WriteString(`</fonts>`)
	b.WriteString(`<fills count="3"><fill><patternFill/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="00D9D9D9"/></patternFill></fill></fills><borders count="2"><border><left/><right/><top/><bottom/><diagonal/></border><border>`)
	for _, side := range []string{"left", "right", "top", "bottom"} {
		fmt.Fprintf(&b, `<%s style="thin"><color rgb="00A6A6A6"/></%s>`, side, side)
	}
	b.WriteString(`</border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>`)
	fmt.Fprintf(&b, `<cellXfs count="%d">`, len(s.items))
	for _, v := range s.items {
		fmt.Fprintf(&b, `<xf numFmtId="%d" fontId="%d" fillId="%d" borderId="%d" xfId="0" applyAlignment="1">%s</xf>`, v.num, v.font, v.fill, v.border, alignment(v))
	}
	b.WriteString(`</cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`)
	return b.String()
}

type outputCell struct {
	value any
	style int
}
type outputColumn struct {
	width int
	style int
}
type outputSheet struct {
	name, state, freeze string
	selected            bool
	cells               map[model.Position]outputCell
	cols                []outputColumn
	heights             map[int]int
	merges              []string
	links               map[string]string
}

func newSheet(name string) *outputSheet {
	return &outputSheet{name: name, cells: map[model.Position]outputCell{}, heights: map[int]int{}, links: map[string]string{}}
}

func (s *outputSheet) put(row, col int, value any, styleID int) {
	s.cells[model.Position{Row: row, Col: col}] = outputCell{value, styleID}
}

func (s *outputSheet) XML() string {
	var b strings.Builder
	fmt.Fprintf(&b, `<worksheet xmlns="%s" xmlns:r="%s"><sheetViews><sheetView workbookViewId="0" tabSelected="%t">`, mainNS, relNS, s.selected)
	if s.freeze != "" {
		row, _, _ := parseAddress(s.freeze)
		fmt.Fprintf(&b, `<pane ySplit="%d" topLeftCell="%s" activePane="bottomLeft" state="frozen"/><selection pane="bottomLeft" activeCell="A1" sqref="A1"/>`, row-1, s.freeze)
	} else {
		b.WriteString(`<selection activeCell="A1" sqref="A1"/>`)
	}
	b.WriteString(`</sheetView></sheetViews><sheetFormatPr baseColWidth="8" defaultRowHeight="15"/>`)
	if len(s.cols) > 0 {
		b.WriteString("<cols>")
		for i, c := range s.cols {
			fmt.Fprintf(&b, `<col width="%d" customWidth="1" min="%d" max="%d" style="%d"/>`, c.width, i+1, i+1, c.style)
		}
		b.WriteString("</cols>")
	}
	positions := make([]model.Position, 0, len(s.cells))
	for p := range s.cells {
		positions = append(positions, p)
	}
	sort.Slice(positions, func(i, j int) bool {
		if positions[i].Row != positions[j].Row {
			return positions[i].Row < positions[j].Row
		}
		return positions[i].Col < positions[j].Col
	})
	b.WriteString("<sheetData>")
	row := 0
	for _, p := range positions {
		if p.Row != row {
			if row != 0 {
				b.WriteString("</row>")
			}
			row = p.Row
			fmt.Fprintf(&b, `<row r="%d"`, row)
			if height, ok := s.heights[row]; ok {
				fmt.Fprintf(&b, ` ht="%d" customHeight="1"`, height)
			}
			b.WriteString(">")
		}
		c := s.cells[p]
		fmt.Fprintf(&b, `<c r="%s" s="%d"`, address(p.Row, p.Col), c.style)
		switch v := c.value.(type) {
		case string:
			b.WriteString(` t="inlineStr">`)
			if v != "" {
				b.WriteString(`<is><t xml:space="preserve">` + xmlText(v) + `</t></is>`)
			}
			b.WriteString("</c>")
		case int:
			fmt.Fprintf(&b, ` t="n"><v>%d</v></c>`, v)
		default:
			b.WriteString(` t="n"></c>`)
		}
	}
	if row != 0 {
		b.WriteString("</row>")
	}
	b.WriteString("</sheetData>")
	if len(s.merges) > 0 {
		fmt.Fprintf(&b, `<mergeCells count="%d">`, len(s.merges))
		for _, ref := range s.merges {
			b.WriteString(`<mergeCell ref="` + ref + `"/>`)
		}
		b.WriteString("</mergeCells>")
	}
	if len(s.links) > 0 {
		b.WriteString("<hyperlinks>")
		keys := make([]string, 0, len(s.links))
		for k := range s.links {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, ref := range keys {
			fmt.Fprintf(&b, `<hyperlink ref="%s" location="%s"/>`, ref, xmlText(s.links[ref]))
		}
		b.WriteString("</hyperlinks>")
	}
	b.WriteString(`<pageMargins left="0.75" right="0.75" top="1" bottom="1" header="0.5" footer="0.5"/></worksheet>`)
	return b.String()
}

func writeTableSheet(s *outputSheet, m *model.TableModel, styles *styles) {
	widths := make([]int, m.Cols)
	aligns := make([]string, m.Cols)
	for col := 0; col < m.Cols; col++ {
		width := 0
		set := map[string]bool{}
		for _, c := range m.Cells {
			if c.Col == col && c.Colspan == 1 {
				set[c.Align] = true
				for _, line := range strings.Split(c.Text, "\n") {
					width = max(width, textwidth.TextWidth(pycompat.ExpandTabs(line, 4)))
				}
			}
		}
		widths[col] = 8
		if width > 0 {
			widths[col] = min(60, max(6, width+2))
		}
		if len(set) == 1 {
			for a := range set {
				aligns[col] = a
			}
		}
	}
	for col := 0; col < m.Cols+5; col++ {
		width, align := 8, ""
		if col < m.Cols {
			width, align = widths[col], aligns[col]
		}
		s.cols = append(s.cols, outputColumn{width, styles.id(style{font: 1, num: 49, horizontal: align, vertical: "top", wrap: true})})
	}
	body := styles.id(style{font: 1, border: 1, num: 49, vertical: "top", wrap: true})
	for row := 1; row <= m.Rows; row++ {
		for col := 1; col <= m.Cols; col++ {
			s.put(row, col, nil, body)
		}
	}
	heights := map[int]int{}
	for _, c := range m.Cells {
		v := style{font: 1, border: 1, num: 49, horizontal: c.Align, vertical: "top", wrap: true}
		if c.Header {
			v.font = 2
			v.fill = 2
		}
		id := styles.id(v)
		for r := c.Row; r < c.Row+c.Rowspan; r++ {
			for col := c.Col; col < c.Col+c.Colspan; col++ {
				s.put(r+1, col+1, nil, id)
			}
		}
		if c.Text != "" {
			s.put(c.Row+1, c.Col+1, EscapeForExcel(c.Text), id)
		}
		if c.Rowspan == 1 && c.Colspan == 1 {
			continue
		}
		s.merges = append(s.merges, address(c.Row+1, c.Col+1)+":"+address(c.Row+c.Rowspan, c.Col+c.Colspan))
		width := -1
		for col := c.Col; col < c.Col+c.Colspan; col++ {
			width += widths[col]
		}
		lines := textwidth.WrappedLineCount(c.Text, width)
		perRow := (lines + c.Rowspan - 1) / c.Rowspan
		for r := c.Row; r < c.Row+c.Rowspan; r++ {
			heights[r] = max(heights[r], 1, perRow)
		}
	}
	for _, c := range m.Cells {
		if _, ok := heights[c.Row]; ok && c.Rowspan == 1 && c.Colspan == 1 {
			heights[c.Row] = max(heights[c.Row], textwidth.WrappedLineCount(c.Text, widths[c.Col]-1))
		}
	}
	for row, lines := range heights {
		s.heights[row+1] = min(409, max(15, 15*lines))
	}
	headers := 0
	for row := 0; row < m.Rows; row++ {
		found, all := false, true
		for _, c := range m.Cells {
			if c.Row == row {
				found = true
				all = all && c.Header
			}
		}
		if !found || !all {
			break
		}
		headers++
	}
	if headers > 0 && headers < m.Rows {
		s.freeze = address(headers+1, 1)
	}
}

func writeIndex(s *outputSheet, mdPath string, tables []*ExportTable, excluded []*table.Region, styles *styles) {
	normal := styles.id(style{font: 1})
	bold := styles.id(style{font: 2})
	link := styles.id(style{font: 3})
	header := styles.id(style{font: 2, fill: 2})
	s.put(1, 1, "Markdown", bold)
	s.put(1, 2, mdPath, normal)
	for i, title := range []string{"表", "シート", "種類", "行", "場所", "大きさ", "直前の見出し"} {
		s.put(3, i+1, title, header)
	}
	row := 4
	for _, t := range tables {
		r := t.Region
		kind := "HTML の表"
		if r.Kind == "pipe" {
			kind = "Markdown の表"
		}
		values := []any{r.Number, t.SheetName, kind, fmt.Sprintf("%d〜%d", r.Start+1, r.End+1), r.Location, fmt.Sprintf("%d 行 × %d 列", t.Model.Rows, t.Model.Cols), r.Heading}
		for col, value := range values {
			s.put(row, col+1, value, normal)
		}
		c := s.cells[model.Position{Row: row, Col: 2}]
		c.style = link
		s.cells[model.Position{Row: row, Col: 2}] = c
		s.links[address(row, 2)] = NamePrefix + strconv.Itoa(r.Number)
		row++
	}
	if len(excluded) > 0 {
		row++
		s.put(row, 1, "対象外の表", bold)
		row++
		for _, r := range excluded {
			s.put(row, 1, r.Number, normal)
			s.put(row, 4, fmt.Sprintf("%d〜%d", r.Start+1, r.End+1), normal)
			s.put(row, 5, r.Excluded, normal)
			row++
		}
	}
	row++
	for i, line := range quickReference {
		id := normal
		if i == 0 {
			id = bold
		}
		s.put(row, 1, line, id)
		row++
	}
	for _, width := range []int{10, 28, 14, 12, 30, 14, 40} {
		s.cols = append(s.cols, outputColumn{width, 0})
	}
	for r := 4; r <= row; r++ {
		for col := 1; col <= 7; col++ {
			p := model.Position{Row: r, Col: col}
			c := s.cells[p]
			v := styles.items[c.style]
			v.vertical = "top"
			c.style = styles.id(v)
			s.cells[p] = c
		}
	}
}

func writeMeta(s *outputSheet, mdPath, xlsxPath string, tables []*ExportTable, version string) {
	rel, _ := filepath.Rel(filepath.Dir(xlsxPath), mdPath)
	rows := [][]any{{"md-table-excel", 1, version}, {"markdown", mdPath, filepath.ToSlash(rel)}, {"exported", time.Now().Format("2006-01-02T15:04:05")}, {}, {"number", "kind", "start", "end", "source_sha256", "fingerprint", "sheet", "occurrence", "same"}}
	for _, t := range tables {
		r := t.Region
		rows = append(rows, []any{r.Number, r.Kind, r.Start + 1, r.End + 1, r.SourceHash(), model.SheetForm(t.Model).Fingerprint(), t.SheetName, t.Occurrence, t.Same})
	}
	for row, values := range rows {
		for col, value := range values {
			s.put(row+1, col+1, value, 0)
		}
	}
}

// Write は XML を ZIP に書き、一時ファイルから置き換える。
func Write(path, mdPath string, tables []*ExportTable, excluded []*table.Region, version string) error {
	return WriteContext(context.Background(), path, mdPath, tables, excluded, version)
}

type contextWriter struct {
	ctx    context.Context
	writer io.Writer
}

func (w contextWriter) Write(p []byte) (int, error) {
	if e := w.ctx.Err(); e != nil {
		return 0, e
	}
	return w.writer.Write(p)
}

// WriteContext は ZIP の書き込みと置換まで同じ取り消しを受け渡す。
func WriteContext(ctx context.Context, path, mdPath string, tables []*ExportTable, excluded []*table.Region, version string) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	target, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	styles := &styles{family: FontName(), items: []style{{}}}
	index := newSheet(IndexSheet)
	sheets := []*outputSheet{index}
	used := map[string]bool{pycompat.Lower(IndexSheet): true, MetaSheet: true}
	for _, t := range tables {
		if e := ctx.Err(); e != nil {
			return e
		}
		t.SheetName = SheetNameFor(t.Region.Number, t.Region.Heading, used)
		s := newSheet(t.SheetName)
		writeTableSheet(s, t.Model, styles)
		sheets = append(sheets, s)
	}
	writeIndex(index, mdPath, tables, excluded, styles)
	meta := newSheet(MetaSheet)
	meta.state = "veryHidden"
	writeMeta(meta, mdPath, target, tables, version)
	sheets = append(sheets, meta)
	active := 0
	if len(tables) == 1 {
		active = 1
	}
	sheets[active].selected = true
	var workbook, rels, types strings.Builder
	fmt.Fprintf(&workbook, `<workbook xmlns="%s" xmlns:r="%s"><bookViews><workbookView activeTab="%d"/></bookViews><sheets>`, mainNS, relNS, active)
	rels.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	types.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`)
	for i, s := range sheets {
		fmt.Fprintf(&workbook, `<sheet name="%s" sheetId="%d" r:id="rId%d"`, xmlText(s.name), i+1, i+1)
		if s.state != "" {
			fmt.Fprintf(&workbook, ` state="%s"`, s.state)
		}
		workbook.WriteString("/>")
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="%s/worksheet" Target="worksheets/sheet%d.xml"/>`, i+1, relNS, i+1)
		fmt.Fprintf(&types, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i+1)
	}
	workbook.WriteString("</sheets><definedNames>")
	for _, t := range tables {
		ref := fmt.Sprintf("%s!$A$1:$%s$%d", quoteSheet(t.SheetName), column(max(1, t.Model.Cols)), max(1, t.Model.Rows))
		fmt.Fprintf(&workbook, `<definedName name="%s%d">%s</definedName>`, NamePrefix, t.Region.Number, xmlText(ref))
	}
	workbook.WriteString("</definedNames></workbook>")
	fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="%s/styles" Target="styles.xml"/>`, len(sheets)+1, relNS)
	fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="%s/theme" Target="theme/theme1.xml"/></Relationships>`, len(sheets)+2, relNS)
	types.WriteString(`<Override PartName="/xl/theme/theme1.xml" ContentType="application/vnd.openxmlformats-officedocument.theme+xml"/></Types>`)
	if e := ctx.Err(); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(target), ".mdtable-*.xlsx")
	if e != nil {
		return e
	}
	temp := f.Name()
	defer os.Remove(temp)
	defer f.Close()
	z := zip.NewWriter(contextWriter{ctx: ctx, writer: f})
	add := func(name, body string) error {
		w, e := z.Create(name)
		if e != nil {
			return e
		}
		if !strings.HasPrefix(body, "<?xml") {
			body = xml.Header + body
		}
		_, e = w.Write([]byte(body))
		return e
	}
	parts := [][2]string{
		{"[Content_Types].xml", types.String()},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="` + relNS + `/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", workbook.String()},
		{"xl/_rels/workbook.xml.rels", rels.String()},
		{"xl/styles.xml", styles.XML()},
		{"xl/theme/theme1.xml", defaultTheme},
	}
	for i, s := range sheets {
		parts = append(parts, [2]string{fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), s.XML()})
	}
	for _, part := range parts {
		if canceled := ctx.Err(); canceled != nil {
			e = canceled
			break
		}
		if e == nil {
			e = add(part[0], part[1])
		}
	}
	ze := z.Close()
	ce := f.Close()
	if e != nil {
		return e
	}
	if ze != nil {
		return ze
	}
	if ce != nil {
		return ce
	}
	e = table.ReplaceWithRetryContext(ctx, temp, target, 5)
	if table.IsPermissionError(e) {
		return failure("ブックを書けません（Excel で開いていませんか）: %s", target)
	}
	return e
}
