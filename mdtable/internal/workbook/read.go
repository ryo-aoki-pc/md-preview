package workbook

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
	"github.com/xuri/excelize/v2"
)

type bounds struct{ top, left, bottom, right int }
type rawCell struct {
	Ref       string    `xml:"r,attr"`
	Type      string    `xml:"t,attr"`
	Style     int       `xml:"s,attr"`
	Value     *string   `xml:"v"`
	Formula   *struct{} `xml:"f"`
	Inline    *struct{} `xml:"is"`
	hyperlink bool
	target    string
	fallback  *string
}
type rawSheet struct {
	Rows []struct {
		Cells []rawCell `xml:"c"`
	} `xml:"sheetData>row"`
	Merges []struct {
		Ref string `xml:"ref,attr"`
	} `xml:"mergeCells>mergeCell"`
	Hyperlinks []struct {
		Ref string `xml:"ref,attr"`
	} `xml:"hyperlinks>hyperlink"`
	cells          map[model.Position]rawCell
	merges         []bounds
	covered        map[model.Position]bool
	maxRow, maxCol int
}
type reader struct {
	ctx      context.Context
	file     *excelize.File
	parts    map[string]*zip.File
	sheets   map[string]string
	styles   map[int]*excelize.Style
	format   rawStyles
	date1904 bool
}

func parseAddress(ref string) (row, col int, ok bool) {
	ref = strings.ReplaceAll(ref, "$", "")
	c, r, e := excelize.CellNameToCoordinates(ref)
	return r, c, e == nil
}

func parseRange(ref string) (bounds, bool) {
	parts := strings.Split(ref, ":")
	if len(parts) > 2 {
		return bounds{}, false
	}
	r, c, ok := parseAddress(parts[0])
	if !ok {
		return bounds{}, false
	}
	r2, c2 := r, c
	if len(parts) == 2 {
		r2, c2, ok = parseAddress(parts[1])
	}
	return bounds{r, c, r2, c2}, ok && r2 >= r && c2 >= c
}

func (r *reader) decode(name string, value any) error {
	if e := r.ctx.Err(); e != nil {
		return e
	}
	part := r.parts[name]
	if part == nil {
		return fmt.Errorf("ブック内のファイルがありません: %s", name)
	}
	f, e := part.Open()
	if e != nil {
		return e
	}
	defer f.Close()
	e = xml.NewDecoder(f).Decode(value)
	if canceled := r.ctx.Err(); canceled != nil {
		return canceled
	}
	return e
}

func openReader(ctx context.Context, filename string) (*reader, error) {
	data, e := os.ReadFile(filename)
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	if e != nil {
		if table.IsPermissionError(e) {
			return nil, failure("ブックを読めません（Excel で開いたままではありませんか）: %s", filename)
		}
		return nil, e
	}
	// ZIP の直接参照と excelize に同じ保存時点の内容を渡す。
	z, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if e != nil {
		if errors.Is(e, zip.ErrFormat) {
			return nil, failure("ブックを読めません: %s (File is not a zip file)", filename)
		}
		return nil, failure("ブックを読めません: %s (%s)", filename, e)
	}
	r := &reader{ctx: ctx, parts: map[string]*zip.File{}, sheets: map[string]string{}, styles: map[int]*excelize.Style{}}
	for _, f := range z.File {
		r.parts[f.Name] = f
	}
	if r.parts["xl/styles.xml"] != nil {
		if e = r.decode("xl/styles.xml", &r.format); e != nil {
			return nil, failure("ブックを読めません: %s (%s)", filename, e)
		}
	}
	var wb struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	var rels struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if e = r.decode("xl/workbook.xml", &wb); e == nil {
		e = r.decode("xl/_rels/workbook.xml.rels", &rels)
	}
	if e != nil {
		return nil, failure("ブックを読めません: %s (%s)", filename, e)
	}
	for _, s := range wb.Sheets {
		for _, rel := range rels.Items {
			if rel.ID == s.ID {
				target := path.Clean(path.Join("xl", rel.Target))
				if strings.HasPrefix(rel.Target, "/") {
					target = strings.TrimPrefix(rel.Target, "/")
				}
				r.sheets[s.Name] = target
			}
		}
	}
	r.file, e = excelize.OpenReader(bytes.NewReader(data), excelize.Options{RawCellValue: true})
	if e != nil {
		return nil, failure("ブックを読めません: %s (%s)", filename, e)
	}
	if canceled := ctx.Err(); canceled != nil {
		r.close()
		return nil, canceled
	}
	props, e := r.file.GetWorkbookProps()
	if e != nil {
		r.close()
		return nil, e
	}
	r.date1904 = props.Date1904 != nil && *props.Date1904
	return r, nil
}

func (r *reader) close() { r.file.Close() }

func (r *reader) sheet(name string) (*rawSheet, error) {
	s := &rawSheet{cells: map[model.Position]rawCell{}, covered: map[model.Position]bool{}}
	if e := r.decode(r.sheets[name], s); e != nil {
		return nil, e
	}
	for _, row := range s.Rows {
		for _, c := range row.Cells {
			rr, cc, ok := parseAddress(c.Ref)
			if !ok {
				return nil, failure("ブックのセルの位置が壊れています: %s", c.Ref)
			}
			s.cells[model.Position{Row: rr, Col: cc}] = c
			s.maxRow = max(s.maxRow, rr)
			s.maxCol = max(s.maxCol, cc)
		}
	}
	for _, m := range s.Merges {
		b, ok := parseRange(m.Ref)
		if !ok {
			continue
		}
		s.merges = append(s.merges, b)
		for row := b.top; row <= b.bottom; row++ {
			for col := b.left; col <= b.right; col++ {
				if row != b.top || col != b.left {
					s.covered[model.Position{Row: row, Col: col}] = true
				}
			}
		}
	}
	// openpyxl は結合の裏に付いたリンクを左上へ移し、空のセルにはリンク先を入れる。
	for _, link := range s.Hyperlinks {
		b, ok := parseRange(link.Ref)
		if !ok {
			continue
		}
		for row := b.top; row <= b.bottom; row++ {
			for col := b.left; col <= b.right; col++ {
				p := model.Position{Row: row, Col: col}
				if s.covered[p] {
					if b.top != b.bottom || b.left != b.right {
						continue
					}
					for _, merged := range s.merges {
						if row >= merged.top && row <= merged.bottom && col >= merged.left && col <= merged.right {
							p = model.Position{Row: merged.top, Col: merged.left}
							break
						}
					}
				}
				linked, target, e := r.file.GetCellHyperLink(name, address(row, col))
				if e != nil {
					return nil, e
				}
				c := s.cells[p]
				if c.Ref == "" {
					c.Ref = address(p.Row, p.Col)
				}
				c.hyperlink, c.target = linked, target
				if c.Value == nil && c.Inline == nil && c.Formula == nil && target != "" {
					value := target
					c.fallback = &value
					c.Type = "str"
				}
				s.cells[p] = c
				s.maxRow = max(s.maxRow, p.Row)
				s.maxCol = max(s.maxCol, p.Col)
			}
		}
	}
	return s, nil
}

func (r *reader) style(id int) (*excelize.Style, error) {
	if s, ok := r.styles[id]; ok {
		return s, nil
	}
	s, e := r.format.style(id)
	if e != nil {
		return nil, e
	}
	r.styles[id] = s
	return s, nil
}

func (r *reader) value(sheet string, c rawCell) (string, error) {
	if c.fallback != nil {
		return *c.fallback, nil
	}
	if c.Ref == "" {
		return "", nil
	}
	if c.Formula != nil {
		formula, e := r.file.GetCellFormula(sheet, c.Ref)
		return "=" + formula, e
	}
	if c.Type == "inlineStr" && c.Inline == nil {
		return "", nil
	}
	return r.file.GetCellValue(sheet, c.Ref, excelize.Options{RawCellValue: true})
}

func (r *reader) metaValue(sheet *rawSheet, row, col int) (any, error) {
	c := sheet.cells[model.Position{Row: row, Col: col}]
	if c.Ref == "" || c.Value == nil && c.Inline == nil && c.Formula == nil && c.fallback == nil {
		return nil, nil
	}
	v, e := r.value(MetaSheet, c)
	if e != nil {
		return nil, e
	}
	switch c.Type {
	case "inlineStr", "s", "str", "e", "d":
		return v, nil
	case "b":
		return v == "1" || v == "TRUE", nil
	}
	if c.Formula != nil {
		return v, nil
	}
	if strings.ContainsAny(v, ".Ee") {
		f, e := strconv.ParseFloat(v, 64)
		return f, e
	}
	i, e := strconv.Atoi(v)
	return i, e
}

func pythonString(value any) string {
	if value == nil {
		return "None"
	}
	if b, ok := value.(bool); ok {
		if b {
			return "True"
		}
		return "False"
	}
	return fmt.Sprint(value)
}

func metaInt(value any) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case float64:
		if math.IsInf(v, 0) {
			return 0, unexpected("OverflowError", "cannot convert float infinity to integer")
		}
		if math.IsNaN(v) {
			return 0, unexpected("ValueError", "cannot convert float NaN to integer")
		}
		return int(v), nil
	case bool:
		if v {
			return 1, nil
		}
		return 0, nil
	case string:
		if n, ok := pycompat.Atoi(v); ok {
			return n, nil
		}
		return 0, unexpected("ValueError", "invalid literal for int() with base 10: "+pycompat.StringRepr(v))
	}
	return 0, unexpected("TypeError", "int() argument must be a string, a bytes-like object or a real number, not 'NoneType'")
}

func unexpected(kind, message string) error {
	return fmt.Errorf("想定外の失敗です（MDTABLE_DEBUG=1 で詳細を出せます）: %s(%s)", kind, pycompat.StringRepr(message))
}

func (r *reader) readMeta(data *Data) error {
	s, e := r.sheet(MetaSheet)
	if e != nil {
		return e
	}
	rows := make([][]any, s.maxRow)
	for row := 1; row <= s.maxRow; row++ {
		rows[row-1] = make([]any, max(9, s.maxCol))
		for col := 1; col <= s.maxCol; col++ {
			rows[row-1][col-1], e = r.metaValue(s, row, col)
			if e != nil {
				return e
			}
		}
	}
	if len(rows) == 0 || rows[0][0] != "md-table-excel" {
		return failure("md-table-excel で書き出したブックではありません")
	}
	version := rows[0][1]
	if version != 1 && version != float64(1) && version != true {
		return failure("このブックの形式（%s）は読めません", pythonString(version))
	}
	header := -1
	for i, values := range rows {
		if values[0] == "markdown" {
			if v, ok := values[1].(string); ok {
				data.MarkdownPath = v
			}
			if v, ok := values[2].(string); ok {
				data.MarkdownRelpath = v
			}
		}
		if values[0] == "number" && header < 0 {
			header = i
		}
	}
	if header < 0 {
		return failure("ブックの管理用シートが壊れています")
	}
	for _, v := range rows[header+1:] {
		if v[0] == nil {
			continue
		}
		t := &SheetTable{}
		targets := []*int{&t.Number, &t.Start, &t.End, &t.Occurrence}
		indices := []int{0, 2, 3, 7}
		for i, index := range indices {
			if index == 7 && v[index] == nil {
				continue
			}
			*targets[i], e = metaInt(v[index])
			if e != nil {
				return e
			}
		}
		t.Start--
		t.End--
		t.Kind, _ = v[1].(string)
		t.SourceHash, _ = v[4].(string)
		t.Fingerprint, _ = v[5].(string)
		t.SheetTitle, _ = v[6].(string)
		if v[8] != nil {
			same, e := metaInt(v[8])
			if e != nil {
				return e
			}
			t.Same = &same
		}
		data.Tables = append(data.Tables, t)
	}
	return nil
}

func strictCellError(t *SheetTable, where, kind, text string) {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r", `\r`), "\n", `\n`)
	t.Errors = append(t.Errors, fmt.Sprintf("%s: Excel が%sに変えたセルがある（%s）。セルを文字列の書式にして入力し直す", where, kind, text))
}

func (r *reader) cellText(sheet string, c rawCell, t *SheetTable, force, strict bool, where string) (string, error) {
	value, e := r.value(sheet, c)
	if e != nil || value == "" {
		return value, e
	}
	if c.Formula != nil || c.Type == "e" {
		kind := "エラー値"
		warning := "エラー値は文字のまま書き戻す"
		if c.Formula != nil {
			kind = "数式"
			warning = "数式は式の文字のまま書き戻す"
		}
		if strict {
			strictCellError(t, where, kind, value)
		} else {
			t.Warnings = append(t.Warnings, fmt.Sprintf("%s: %s: %s", where, warning, value))
		}
		return model.NormalizeText(value), nil
	}
	if c.Type == "inlineStr" || c.Type == "s" || c.Type == "str" {
		runs, e := r.file.GetCellRichText(sheet, c.Ref)
		if e != nil {
			return "", e
		}
		rich := len(runs) > 1
		for _, run := range runs {
			rich = rich || run.Font != nil
		}
		if rich {
			t.Warnings = append(t.Warnings, where+": セルの一部だけの書式は書き戻さない（文字だけを使う）")
		}
		return model.NormalizeText(value), nil
	}
	if c.Type == "b" {
		text := "FALSE"
		if value == "1" || value == "TRUE" {
			text = "TRUE"
		}
		if strict {
			strictCellError(t, where, "真偽値", text)
		} else {
			t.Warnings = append(t.Warnings, fmt.Sprintf("%s: 真偽値を文字にした: %s", where, text))
		}
		return text, nil
	}
	style, e := r.style(c.Style)
	if e != nil {
		return "", e
	}
	format := builtinFormats[style.NumFmt]
	if format == "" {
		format = "General"
	}
	if style.CustomNumFmt != nil {
		format = *style.CustomNumFmt
	}
	if c.Type == "d" || isDateFormat(format) {
		text := isoDate(value)
		duration := c.Type != "d" && durationFormat.MatchString(strings.Split(format, ";")[0])
		if c.Type != "d" {
			f, e := strconv.ParseFloat(value, 64)
			if e != nil && !errors.Is(e, strconv.ErrRange) {
				return "", e
			}
			text, e = excelDate(f, r.date1904, duration)
			if e != nil {
				// openpyxl は変換範囲外の日付をエラー値として読む。
				text = "#VALUE!"
				if strict {
					strictCellError(t, where, "エラー値", text)
				} else {
					t.Warnings = append(t.Warnings, fmt.Sprintf("%s: エラー値は文字のまま書き戻す: %s", where, text))
				}
				return text, nil
			}
		}
		if duration {
			return model.NormalizeText(text), nil
		}
		if force {
			t.Warnings = append(t.Warnings, fmt.Sprintf("%s: 日付・時刻を文字にした: %s", where, text))
		} else {
			t.Errors = append(t.Errors, fmt.Sprintf("%s: Excel が日付・時刻に変えたセルがある（%s）。セルを文字列の書式にして入力し直すか、--force で %s として書き戻す", where, text, text))
		}
		return text, nil
	}
	text := formatNumber(value)
	if strict {
		strictCellError(t, where, "数値", text)
	} else if format == "General" || format == "@" {
		t.Warnings = append(t.Warnings, fmt.Sprintf("%s: 数値を文字にした: %s（文字列の書式で入力し直すと確実）", where, text))
	} else if force {
		t.Warnings = append(t.Warnings, fmt.Sprintf("%s: 数値を文字にした: %s", where, text))
	} else {
		t.Errors = append(t.Errors, fmt.Sprintf("%s: Excel が数値に変えたセルがある（%s、書式 %s）。セルを文字列の書式にして入力し直すか、--force で数値のまま文字にする", where, text, format))
	}
	return text, nil
}

func horizontal(s *excelize.Style) string {
	if s.Alignment == nil {
		return ""
	}
	switch h := s.Alignment.Horizontal; h {
	case "general", "fill":
		return ""
	case "center", "centerContinuous":
		return "center"
	case "justify", "distributed":
		return "justify"
	default:
		return h
	}
}

func isHeader(s *excelize.Style) bool {
	return s.Font != nil && s.Font.Bold && s.Fill.Type == "pattern" && s.Fill.Pattern == 1
}

func linkInText(target, text string) bool {
	if strings.HasPrefix(pycompat.Lower(target), "mailto:") {
		target = target[7:]
	}
	target = strings.TrimRight(target, "/")
	if target == "" {
		return true
	}
	unquoted := uriEscape.ReplaceAllStringFunc(target, func(s string) string { n, _ := strconv.ParseUint(s[1:], 16, 8); return string([]byte{byte(n)}) })
	unquoted = pycompat.DecodeUTF8([]byte(unquoted))
	text = pycompat.Lower(text)
	return strings.Contains(text, pycompat.Lower(target)) || strings.Contains(text, pycompat.Lower(unquoted))
}

var uriEscape = regexp.MustCompile(`%[0-9a-fA-F]{2}`)

func (r *reader) readTable(name string, start bounds, t *SheetTable, force, strict bool) (*model.TableModel, error) {
	s, e := r.sheet(name)
	if e != nil {
		return nil, e
	}
	values := map[model.Position]bool{}
	for p, c := range s.cells {
		if s.covered[p] {
			continue
		}
		v, e := r.value(name, c)
		if e != nil {
			return nil, e
		}
		if v != "" {
			values[p] = true
		}
	}
	b := start
	for changed := true; changed; {
		changed = false
		for _, m := range s.merges {
			if m.top <= b.bottom+1 && m.bottom >= b.top && m.left <= b.right+1 && m.right >= b.left {
				next := bounds{min(b.top, m.top), min(b.left, m.left), max(b.bottom, m.bottom), max(b.right, m.right)}
				if next != b {
					changed = true
					b = next
				}
			}
		}
		for col := b.left; col <= b.right; col++ {
			if values[model.Position{Row: b.bottom + 1, Col: col}] {
				b.bottom++
				changed = true
				break
			}
		}
		for row := b.top; row <= b.bottom; row++ {
			if values[model.Position{Row: row, Col: b.right + 1}] {
				b.right++
				changed = true
				break
			}
		}
	}
	var ignored []model.Position
	for p := range values {
		if p.Row < b.top || p.Row > b.bottom || p.Col < b.left || p.Col > b.right {
			ignored = append(ignored, p)
		}
	}
	sort.Slice(ignored, func(i, j int) bool {
		if ignored[i].Row != ignored[j].Row {
			return ignored[i].Row < ignored[j].Row
		}
		return ignored[i].Col < ignored[j].Col
	})
	if len(ignored) > 0 {
		var names []string
		for _, p := range ignored[:min(5, len(ignored))] {
			names = append(names, address(p.Row, p.Col))
		}
		t.Warnings = append(t.Warnings, fmt.Sprintf("シート「%s」: 表から離れた位置の値を無視した（%s）", name, strings.Join(names, ", ")))
	}
	merges := map[model.Position]bounds{}
	for _, m := range s.merges {
		if m.top >= b.top && m.left >= b.left && m.bottom <= b.bottom && m.right <= b.right {
			merges[model.Position{Row: m.top, Col: m.left}] = m
		}
	}
	comments, e := r.file.GetComments(name)
	if e != nil {
		return nil, e
	}
	commentCells := map[string]bool{}
	for _, c := range comments {
		commentCells[c.Cell] = true
	}
	var cells []*model.Cell
	var styled, links, notes []string
	for row := b.top; row <= b.bottom; row++ {
		if e := r.ctx.Err(); e != nil {
			return nil, e
		}
		for col := b.left; col <= b.right; col++ {
			p := model.Position{Row: row, Col: col}
			if s.covered[p] {
				continue
			}
			c := s.cells[p]
			ref := address(row, col)
			where := fmt.Sprintf("シート「%s」の %s", name, ref)
			text, e := r.cellText(name, c, t, force, strict, where)
			if e != nil {
				return nil, e
			}
			style, e := r.style(c.Style)
			if e != nil {
				return nil, e
			}
			linked, target := c.hyperlink, c.target
			font := style.Font
			if text != "" && font != nil && (font.Italic || font.Strike || font.Underline != "" && font.Underline != "none" && !linked || font.Bold && !isHeader(style) && !(t.Kind == "pipe" && row == b.top)) {
				styled = append(styled, ref)
			}
			if linked && !linkInText(target, text) {
				links = append(links, ref)
			}
			if commentCells[ref] {
				notes = append(notes, ref)
			}
			cell := model.NewCell(row-b.top, col-b.left, text)
			cell.Header = isHeader(style)
			cell.Align = horizontal(style)
			if m, ok := merges[p]; ok {
				cell.Rowspan = m.bottom - m.top + 1
				cell.Colspan = m.right - m.left + 1
			}
			cells = append(cells, cell)
		}
	}
	features := []struct {
		names         []string
		feature, hint string
	}{{styled, "セル全体の書式（太字・斜体・取り消し線・下線）", "（強調は ** などで書く）"}, {links, "ハイパーリンク", "（[文字](URL) の形で書く）"}, {notes, "コメント（メモ）", ""}}
	for _, f := range features {
		if len(f.names) > 0 {
			positions := strings.Join(f.names[:min(5, len(f.names))], ", ")
			if len(f.names) > 5 {
				positions += fmt.Sprintf(" ほか %d 個", len(f.names)-5)
			}
			t.Warnings = append(t.Warnings, fmt.Sprintf("シート「%s」: %sは書き戻さない: %s%s", name, f.feature, positions, f.hint))
		}
	}
	return model.New("", b.bottom-b.top+1, b.right-b.left+1, cells, nil), nil
}

var destination = regexp.MustCompile(`(?:^|,)\s*(?:'((?:[^']|'')*)'|([^'!,]+))!(\$?[A-Za-z]+\$?\d+(?::\$?[A-Za-z]+\$?\d+)?|#REF!)`)

// Read は excelize の文字列復号を一度だけ使い、セルの値と管理情報を読む。
func Read(filename string, force, strict bool, tables []int) (*Data, error) {
	return ReadContext(context.Background(), filename, force, strict, tables)
}

// ReadContext は表ごとにも取り消しを確かめ、開いたブックを閉じて戻る。
func ReadContext(ctx context.Context, filename string, force, strict bool, tables []int) (data *Data, err error) {
	defer func() {
		if canceled := ctx.Err(); canceled != nil {
			data = nil
			err = canceled
		}
	}()
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".xlsx" && ext != ".xlsm" {
		return nil, failure("Excel のブック（.xlsx）を指定してください: %s", filename)
	}
	if _, e := os.Stat(filename); errors.Is(e, os.ErrNotExist) {
		return nil, failure("ブックがありません: %s", filename)
	}
	r, e := openReader(ctx, filename)
	if e != nil {
		return nil, e
	}
	defer r.close()
	if _, ok := r.sheets[MetaSheet]; !ok {
		return nil, failure("md-table-excel で書き出したブックではありません（管理用シートが無い）")
	}
	data = &Data{SourceHashes: map[string]bool{}}
	if e = r.readMeta(data); e != nil {
		return nil, e
	}
	selected := map[int]bool{}
	numbers := []string{}
	for _, t := range data.Tables {
		numbers = append(numbers, strconv.Itoa(t.Number))
		data.SourceHashes[t.SourceHash] = true
	}
	for _, n := range tables {
		found := false
		for _, t := range data.Tables {
			found = found || t.Number == n
		}
		if !found {
			return nil, failure("ブックに表 %d はありません（ブックの表: %s）", n, strings.Join(numbers, ", "))
		}
		selected[n] = true
	}
	names := map[string]string{}
	for _, n := range r.file.GetDefinedName() {
		if n.Scope == "Workbook" || n.Scope == "" {
			names[n.Name] = n.RefersTo
		}
	}
	known := map[string]bool{}
	var chosen []*SheetTable
	for _, t := range data.Tables {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		name := NamePrefix + strconv.Itoa(t.Number)
		sheet := ""
		start := bounds{1, 1, 1, 1}
		for _, match := range destination.FindAllStringSubmatch(names[name], -1) {
			title := match[1]
			if title == "" {
				title = match[2]
			}
			title = strings.ReplaceAll(title, "''", "'")
			if _, ok := r.sheets[title]; ok {
				sheet = title
				if b, ok := parseRange(match[3]); ok {
					start = b
				}
				break
			}
		}
		if sheet == "" {
			if _, ok := r.sheets[t.SheetTitle]; ok {
				sheet = t.SheetTitle
			}
		}
		if tables == nil || selected[t.Number] {
			chosen = append(chosen, t)
		}
		if sheet == "" {
			t.Missing = fmt.Sprintf("シートが見つからない（消したか、名前の定義 %s が壊れた）", name)
			continue
		}
		known[sheet] = true
		t.SheetTitle = sheet
		if tables == nil || selected[t.Number] {
			t.Model, e = r.readTable(sheet, start, t, force, strict)
			if e != nil {
				return nil, e
			}
		}
	}
	data.Tables = chosen
	for _, title := range r.file.GetSheetList() {
		if !known[title] && title != IndexSheet && title != MetaSheet {
			data.Warnings = append(data.Warnings, fmt.Sprintf("シート「%s」はどの表とも対応しないので読まない", title))
		}
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	return data, nil
}
