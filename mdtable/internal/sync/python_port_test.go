// Python の単体テストと同じ入力を保存したデータで検査する。
package sync

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
	"github.com/ryo-aoki-pc/md-table-excel/internal/version"
	"github.com/xuri/excelize/v2"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type pythonCase struct {
	Test, Op    string
	Input, Want json.RawMessage
	Error       []any
}

func pythonDecode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func pythonCanonical(v any) any {
	switch x := v.(type) {
	case nil:
		return []any{}
	case []any:
		for i := range x {
			x[i] = pythonCanonical(x[i])
		}
		return x
	case map[string]any:
		for k := range x {
			x[k] = pythonCanonical(x[k])
		}
		return x
	}
	return v
}

func pythonCheck(t *testing.T, got any, want json.RawMessage) {
	t.Helper()
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	a := pythonCanonical(pythonDecode[any](t, data))
	b := pythonCanonical(pythonVersionExpected(pythonDecode[any](t, want)))
	if !reflect.DeepEqual(a, b) {
		t.Fatal(pythonDifference(a, b, "結果"))
	}
}

func pythonDifference(a, b any, path string) string {
	if reflect.DeepEqual(a, b) {
		return ""
	}
	if x, ok := a.(map[string]any); ok {
		if y, ok := b.(map[string]any); ok {
			keys := []string{}
			for k := range x {
				keys = append(keys, k)
			}
			for k := range y {
				if _, ok := x[k]; !ok {
					keys = append(keys, k)
				}
			}
			for _, k := range keys {
				if d := pythonDifference(x[k], y[k], path+"."+k); d != "" {
					return d
				}
			}
		}
	}
	if x, ok := a.([]any); ok {
		if y, ok := b.([]any); ok && len(x) == len(y) {
			for i := range x {
				if d := pythonDifference(x[i], y[i], fmt.Sprintf("%s[%d]", path, i)); d != "" {
					return d
				}
			}
		}
	}
	return fmt.Sprintf("%s が Python の期待値と違う: 結果 %.1000v、期待 %.1000v", path, a, b)
}

func pythonRun(t *testing.T, name string) {
	t.Helper()
	t.Setenv("MDTABLE_FONT", "BIZ UDゴシック")
	f, err := os.Open("../../testdata/unit/sync.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	var cases []pythonCase
	if err := json.NewDecoder(gz).Decode(&cases); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, c := range cases {
		if c.Test != name {
			continue
		}
		count++
		t.Run(fmt.Sprintf("%s_%d", c.Op, count), func(t *testing.T) {
			got, err := pythonEvaluate(t, c)
			if len(c.Error) > 0 {
				if err == nil {
					t.Fatalf("エラーを返さなかった: %s", c.Error[0])
				}
				row, col := 0, 0
				v := reflect.ValueOf(err)
				if v.Kind() == reflect.Pointer {
					v = v.Elem()
					if v.Kind() == reflect.Struct {
						if r := v.FieldByName("Row"); r.IsValid() {
							row = int(r.Int())
						}
						if r := v.FieldByName("Col"); r.IsValid() {
							col = int(r.Int())
						}
					}
				}
				data, _ := json.Marshal(c.Error)
				pythonCheck(t, []any{err.Error(), row, col}, data)
			} else {
				if err != nil {
					t.Fatal(err)
				}
				pythonCheck(t, got, c.Want)
			}
		})
	}
	if count == 0 {
		t.Fatal("元のテストの入力がない")
	}
}

type pythonModel struct {
	Kind       string
	Rows, Cols int
	Cells      [][]any
	Holes      [][]int
}

func (m pythonModel) model() *model.TableModel {
	cells := []*model.Cell{}
	for _, v := range m.Cells {
		align, _ := v[5].(string)
		cells = append(cells, &model.Cell{Row: int(v[0].(float64)), Col: int(v[1].(float64)), Rowspan: int(v[2].(float64)), Colspan: int(v[3].(float64)), Header: v[4].(bool), Align: align, Text: v[6].(string)})
	}
	holes := map[model.Position]bool{}
	for _, p := range m.Holes {
		holes[model.Position{Row: p[0], Col: p[1]}] = true
	}
	return model.New(m.Kind, m.Rows, m.Cols, cells, holes)
}
func pythonModelValue(m *model.TableModel) any {
	if m == nil {
		return nil
	}
	cells := []any{}
	for _, c := range m.Cells {
		cells = append(cells, c.Key())
	}
	holes := [][2]int{}
	for p := range m.Holes {
		holes = append(holes, [2]int{p.Row, p.Col})
	}
	sort.Slice(holes, func(i, j int) bool {
		return holes[i][0] < holes[j][0] || holes[i][0] == holes[j][0] && holes[i][1] < holes[j][1]
	})
	return map[string]any{"kind": m.Kind, "rows": m.Rows, "cols": m.Cols, "cells": cells, "holes": holes}
}

// 作業フォルダーが変わる前に、保存データの場所を確定する。
var pythonTestdataDir = func() string {
	path, e := filepath.Abs("../../testdata")
	if e != nil {
		panic(e)
	}
	return path
}()

// 保存したブックは一度だけ読み、並行する検査には読み取り専用で渡す。
var pythonSavedBooks = sync.OnceValues(func() (map[string][]byte, error) {
	archive, e := zip.OpenReader(filepath.Join(pythonTestdataDir, "workbooks.zip"))
	if e != nil {
		return nil, e
	}
	defer archive.Close()
	books := map[string][]byte{}
	for _, f := range archive.File {
		r, e := f.Open()
		if e != nil {
			return nil, e
		}
		data, e := io.ReadAll(r)
		r.Close()
		if e != nil {
			return nil, e
		}
		books[f.Name] = data
	}
	return books, nil
})

// 版の印は期待値にだけ使う。実際の値が実行中の版と違えば検査は失敗する。
func pythonVersionExpected(value any) any {
	switch x := value.(type) {
	case string:
		if x == "<VERSION>" {
			return version.Version
		}
		if x == "mdtable <VERSION>\n" {
			return "mdtable " + version.Version + "\n"
		}
	case []any:
		for i := range x {
			x[i] = pythonVersionExpected(x[i])
		}
	case map[string]any:
		for k := range x {
			x[k] = pythonVersionExpected(x[k])
		}
	}
	return value
}

// 保存した openpyxl 製ブックのパスだけを実行先に合わせる。
func pythonRestoreBook(t *testing.T, key, path, directory string) {
	t.Helper()
	books, e := pythonSavedBooks()
	if e != nil {
		t.Fatal(e)
	}
	data := books[key+".xlsx"]
	if data == nil {
		t.Fatalf("保存したブックがない: %s", key)
	}
	source, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	target := zip.NewWriter(&out)
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(directory))
	for _, f := range source.File {
		r, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(r)
		r.Close()
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.Contains(b, []byte("&lt;DIR&gt;")) {
			if e = target.Copy(f); e != nil {
				t.Fatal(e)
			}
			continue
		}
		b = bytes.ReplaceAll(b, []byte("&lt;DIR&gt;"), escaped.Bytes())
		w, e := target.Create(f.Name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(b); e != nil {
			t.Fatal(e)
		}
	}
	if e = target.Close(); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, out.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
}

func pythonNormalize(v any, directory string) any {
	data, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	var result any
	if e = json.Unmarshal(data, &result); e != nil {
		panic(e)
	}
	escaped, _ := json.Marshal(directory)
	var normalize func(any) any
	normalize = func(item any) any {
		switch x := item.(type) {
		case string:
			for _, path := range []string{string(escaped[1 : len(escaped)-1]), directory, filepath.ToSlash(directory)} {
				x = strings.ReplaceAll(x, path, "<DIR>")
			}
			x = strings.ReplaceAll(x, `<DIR>\\`, "<DIR>/")
			return strings.ReplaceAll(x, `<DIR>\`, "<DIR>/")
		case []any:
			for i := range x {
				x[i] = normalize(x[i])
			}
		case map[string]any:
			for k := range x {
				x[k] = normalize(x[k])
			}
		}
		return item
	}
	return normalize(result)
}

func pythonWriteHex(t *testing.T, path, value string) {
	t.Helper()
	data, e := hex.DecodeString(value)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
}
func pythonReadHex(t *testing.T, path string) string {
	t.Helper()
	data, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		return ""
	}
	if e != nil {
		t.Fatal(e)
	}
	return hex.EncodeToString(data)
}

// XML の並びや ZIP のバイト列には依存せず、excelize でブックの性質を読む。
func pythonInspectBook(t *testing.T, path, directory string) any {
	t.Helper()
	f, e := excelize.OpenFile(path)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	archive, e := zip.OpenReader(path)
	if e != nil {
		t.Fatal(e)
	}
	defer archive.Close()
	parts := map[string][]byte{}
	for _, p := range archive.File {
		r, e := p.Open()
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(r)
		r.Close()
		if e != nil {
			t.Fatal(e)
		}
		parts[p.Name] = b
	}
	// シートの状態は XML の属性から、セルの値と書式は excelize から読む。
	var wb struct {
		Sheets []struct {
			Name  string `xml:"name,attr"`
			State string `xml:"state,attr"`
		} `xml:"sheets>sheet"`
	}
	if e = xml.Unmarshal(parts["xl/workbook.xml"], &wb); e != nil {
		t.Fatal(e)
	}
	names := [][2]string{}
	for _, n := range f.GetDefinedName() {
		names = append(names, [2]string{n.Name, n.RefersTo})
	}
	sort.Slice(names, func(i, j int) bool { return names[i][0] < names[j][0] })
	sheets := []any{}
	font := func(s *excelize.Style) any {
		if s.Font == nil {
			return []any{nil, nil, false, false, false, nil, nil}
		}
		p := s.Font
		var color any
		if p.Color != "" && p.ColorTheme == nil && p.ColorIndexed == 0 {
			value := strings.TrimPrefix(p.Color, "#")
			if len(value) > 6 {
				value = value[len(value)-6:]
			}
			color = value
		}
		var name, size, underline any
		if p.Family != "" {
			name = p.Family
		}
		if p.Size != 0 {
			size = p.Size
		}
		if p.Underline != "" {
			underline = p.Underline
		}
		return []any{name, size, p.Bold, p.Italic, p.Strike, underline, color}
	}
	align := func(s *excelize.Style) any {
		var h, v any
		wrap := false
		if s.Alignment != nil {
			if s.Alignment.Horizontal != "" {
				h = s.Alignment.Horizontal
			}
			if s.Alignment.Vertical != "" {
				v = s.Alignment.Vertical
			}
			wrap = s.Alignment.WrapText
		}
		return []any{h, v, wrap}
	}
	style := func(id int) *excelize.Style {
		v, e := f.GetStyle(id)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	num := func(s *excelize.Style) string {
		if s.CustomNumFmt != nil {
			return *s.CustomNumFmt
		}
		if s.NumFmt == 49 {
			return "@"
		}
		return "General"
	}
	for index, title := range f.GetSheetList() {
		var ws struct {
			Views []struct {
				Selected bool `xml:"tabSelected,attr"`
			} `xml:"sheetViews>sheetView"`
			Cols []struct {
				Min    int     `xml:"min,attr"`
				Max    int     `xml:"max,attr"`
				Width  float64 `xml:"width,attr"`
				Custom bool    `xml:"customWidth,attr"`
				Style  int     `xml:"style,attr"`
			} `xml:"cols>col"`
			Rows []struct {
				Number int      `xml:"r,attr"`
				Height *float64 `xml:"ht,attr"`
				Cells  []struct {
					Ref    string    `xml:"r,attr"`
					Type   string    `xml:"t,attr"`
					Style  int       `xml:"s,attr"`
					Value  *string   `xml:"v"`
					Inline *struct{} `xml:"is"`
				} `xml:"c"`
			} `xml:"sheetData>row"`
		}
		if e = xml.Unmarshal(parts[fmt.Sprintf("xl/worksheets/sheet%d.xml", index+1)], &ws); e != nil {
			t.Fatal(e)
		}
		state := "visible"
		if wb.Sheets[index].State != "" {
			state = wb.Sheets[index].State
		}
		selected := false
		for _, v := range ws.Views {
			selected = v.Selected
		}
		var freeze any
		panes, e := f.GetPanes(title)
		if e != nil {
			t.Fatal(e)
		}
		if panes.Freeze {
			freeze = panes.TopLeftCell
		}
		merges := []string{}
		ms, e := f.GetMergeCells(title, true)
		if e != nil {
			t.Fatal(e)
		}
		for _, m := range ms {
			merges = append(merges, m.GetStartAxis()+":"+m.GetEndAxis())
		}
		sort.Strings(merges)
		cols := map[string]any{}
		rows := map[string]any{}
		cells := map[string]any{}
		for _, c := range ws.Cols {
			s := style(c.Style)
			for column := c.Min; column <= c.Max; column++ {
				letter, _ := excelize.ColumnNumberToName(column)
				width, e := f.GetColWidth(title, letter)
				if e != nil {
					t.Fatal(e)
				}
				cols[strconv.Itoa(column)] = []any{width, c.Custom, num(s), font(s), align(s)}
			}
		}
		for _, r := range ws.Rows {
			if r.Height != nil {
				height, e := f.GetRowHeight(title, r.Number)
				if e != nil {
					t.Fatal(e)
				}
				rows[strconv.Itoa(r.Number)] = height
			}
			for _, c := range r.Cells {
				if c.Style == 0 && c.Value == nil && c.Inline == nil {
					continue
				}
				id, e := f.GetCellStyle(title, c.Ref)
				if e != nil {
					t.Fatal(e)
				}
				s := style(id)
				// openpyxl と同じく、結合に隠れる位置は外周の罫線だけを読む。
				column, row, e := excelize.CellNameToCoordinates(c.Ref)
				if e != nil {
					t.Fatal(e)
				}
				hidden := false
				for _, merge := range ms {
					left, top, e := excelize.CellNameToCoordinates(merge.GetStartAxis())
					if e != nil {
						t.Fatal(e)
					}
					right, bottom, e := excelize.CellNameToCoordinates(merge.GetEndAxis())
					if e != nil {
						t.Fatal(e)
					}
					if column < left || column > right || row < top || row > bottom || column == left && row == top {
						continue
					}
					anchorID, e := f.GetCellStyle(title, merge.GetStartAxis())
					if e != nil {
						t.Fatal(e)
					}
					anchor := style(anchorID)
					s = style(0)
					s.Border = nil
					for _, b := range anchor.Border {
						if b.Style > 0 && (b.Type == "left" && column == left || b.Type == "right" && column == right || b.Type == "top" && row == top || b.Type == "bottom" && row == bottom) {
							s.Border = append(s.Border, b)
						}
					}
					hidden = true
					break
				}
				if hidden && len(s.Border) == 0 {
					continue
				}
				text, e := f.GetCellValue(title, c.Ref, excelize.Options{RawCellValue: true})
				if e != nil {
					t.Fatal(e)
				}
				var value any
				typ := "n"
				if c.Type == "s" || c.Type == "inlineStr" || c.Type == "str" {
					value = text
					typ = "s"
					if text == "" {
						value = nil
						typ = "inlineStr"
					}
				} else if c.Value != nil {
					n, e := strconv.ParseFloat(text, 64)
					if e != nil {
						t.Fatal(e)
					}
					value = n
				}
				if title == "_mdtable" && c.Ref == "B3" {
					value = "<日時>"
				}
				var fillType, fillColor any
				if s.Fill.Pattern > 0 {
					patterns := []string{"none", "solid", "mediumGray", "darkGray", "lightGray", "darkHorizontal", "darkVertical", "darkDown", "darkUp", "darkGrid", "darkTrellis", "lightHorizontal", "lightVertical", "lightDown", "lightUp", "lightGrid", "lightTrellis", "gray125", "gray0625"}
					fillType = patterns[s.Fill.Pattern]
					if len(s.Fill.Color) > 0 {
						color := s.Fill.Color[0]
						if len(color) > 6 {
							color = color[len(color)-6:]
						}
						fillColor = color
					}
				}
				borders := []any{nil, nil, nil, nil}
				for _, b := range s.Border {
					for i, side := range []string{"left", "right", "top", "bottom"} {
						if b.Type == side && b.Style > 0 {
							styles := []string{"", "thin", "medium", "dashed", "dotted", "thick", "double", "hair", "mediumDashed", "dashDot", "mediumDashDot", "dashDotDot", "mediumDashDotDot", "slantDashDot"}
							color := b.Color
							if len(color) > 6 {
								color = color[len(color)-6:]
							}
							borders[i] = []any{styles[b.Style], color}
						}
					}
				}
				var link any
				exists, target, e := f.GetCellHyperLink(title, c.Ref)
				if e != nil {
					t.Fatal(e)
				}
				if exists {
					if strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mdtable_") {
						link = []any{nil, strings.TrimPrefix(target, "#")}
					} else {
						link = []any{target, nil}
					}
				}
				cells[c.Ref] = map[string]any{"value": value, "type": typ, "num": num(s), "font": font(s), "fill": []any{fillType, fillColor}, "border": borders, "align": align(s), "link": link, "comment": nil}
			}
		}
		sheets = append(sheets, map[string]any{"title": title, "state": state, "selected": selected, "freeze": freeze, "merges": merges, "cols": cols, "rows": rows, "cells": cells})
	}
	return pythonNormalize(map[string]any{"active": f.GetSheetName(f.GetActiveSheetIndex()), "names": names, "sheets": sheets}, directory)
}

func pythonReport(r *Report) any {
	var output any
	if r.Output != "" {
		output = r.Output
	}
	return map[string]any{"messages": r.Messages, "warnings": r.Warnings, "errors": r.Errors, "diff": r.Diff, "changed": r.Changed, "output": output}
}
func pythonEvaluate(t *testing.T, c pythonCase) (any, error) {
	if c.Op == "verify" {
		a := pythonDecode[[]json.RawMessage](t, c.Input)
		md := table.NewMarkdown("<test>", pythonDecode[string](t, a[0]), false)
		regions := table.FindTables(md)
		inputs := pythonDecode[[]struct {
			Number int
			Lines  []string
			Model  *pythonModel
			Target string
		}](t, a[2])
		replacements := []*table.Replacement{}
		for _, v := range inputs {
			var m *model.TableModel
			if v.Model != nil {
				m = v.Model.model()
			}
			replacements = append(replacements, &table.Replacement{Region: regions[v.Number-1], Lines: v.Lines, Model: m, TargetKind: v.Target})
		}
		return Verify(md, pythonDecode[string](t, a[1]), replacements), nil
	}
	if c.Op == "render" {
		a := pythonDecode[[]json.RawMessage](t, c.Input)
		md := table.NewMarkdown("<test>", pythonDecode[string](t, a[0]), false)
		r := table.FindTables(md)[pythonDecode[int](t, a[1])-1]
		m := pythonDecode[pythonModel](t, a[2]).model()
		warnings := []string{}
		lines, reason, e := render(r, m, &warnings, pythonDecode[bool](t, a[3]))
		var converted any
		if reason != "" {
			converted = reason
		}
		return []any{lines, converted, warnings}, e
	}
	if c.Op == "rewrite" {
		a := pythonDecode[[]json.RawMessage](t, c.Input)
		source := pythonDecode[string](t, a[0])
		number := pythonDecode[int](t, a[1])
		m := pythonDecode[pythonModel](t, a[2]).model()
		md := table.NewMarkdown("<test>", source, false)
		r := table.FindTables(md)[number-1]
		m = normalizedSheetModel(m, r)
		warnings := []string{}
		lines, converted, e := render(r, m, &warnings, false)
		if e != nil {
			return nil, e
		}
		replacement := &table.Replacement{Region: r, Lines: lines}
		after := table.ApplyReplacements(md, []*table.Replacement{replacement})
		var reason any
		if converted != "" {
			reason = converted
		}
		return []any{after, reason, Verify(md, after, []*table.Replacement{replacement})}, nil
	}
	directory := t.TempDir()
	var report *Report
	var e error
	var path string
	if c.Op == "import" {
		input := pythonDecode[struct {
			Book, Md, Before string
			Exists           bool
			Options          struct {
				DryRun     bool `json:"dry_run"`
				Force      bool
				KeepKind   bool `json:"keep_kind"`
				StrictText bool `json:"strict_text"`
				Tables     []int
			}
		}](t, c.Input)
		path = filepath.Join(directory, input.Md)
		if input.Exists {
			pythonWriteHex(t, path, input.Before)
		}
		book := filepath.Join(directory, "book.xlsx")
		pythonRestoreBook(t, input.Book, book, directory)
		report, e = ImportWorkbook(book, path, input.Options.DryRun, input.Options.Force, input.Options.KeepKind, input.Options.StrictText, input.Options.Tables)
	} else {
		input := pythonDecode[struct {
			Path, Before, Font string
			Exists             bool
			Args               []json.RawMessage
			Options            map[string]json.RawMessage
		}](t, c.Input)
		if input.Font != "" {
			t.Setenv("MDTABLE_FONT", input.Font)
		}
		path = filepath.Join(directory, input.Path)
		if input.Exists {
			pythonWriteHex(t, path, input.Before)
		}
		opt := func(name string) bool {
			if v, ok := input.Options[name]; ok {
				return pythonDecode[bool](t, v)
			}
			return false
		}
		if c.Op == "convert" {
			selectors := pythonDecode[[]string](t, input.Args[0])
			target := pythonDecode[string](t, input.Args[1])
			dry := opt("dry_run")
			if len(input.Args) > 2 {
				dry = pythonDecode[bool](t, input.Args[2])
			}
			report, e = Convert(path, selectors, target, dry)
		} else if c.Op == "export" {
			output := ""
			if len(input.Args) > 0 {
				output = pythonDecode[string](t, input.Args[0])
				output = strings.ReplaceAll(output, "<DIR>", directory)
			}
			var selectors []string
			if v, ok := input.Options["selectors"]; ok {
				selectors = pythonDecode[[]string](t, v)
			}
			report, e = Export(path, output, selectors, opt("force"))
		} else {
			t.Fatalf("未知の操作: %s", c.Op)
		}
	}
	if e != nil {
		return nil, fmt.Errorf("%s", strings.ReplaceAll(e.Error(), directory, "<DIR>"))
	}
	result := map[string]any{"report": pythonReport(report), "after": pythonReadHex(t, path)}
	if c.Op == "export" && report.Output != "" {
		result["properties"] = pythonInspectBook(t, report.Output, directory)
	}
	return pythonNormalize(result, directory), nil
}

// Python: test_cli.CliTest.test_edit_retries_unreadable_workbook_once
func TestPythonTestCliClitestTestEditRetriesUnreadableWorkbookOnce(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_edit_retries_unreadable_workbook_once")
}

// Python: test_convert.ConversionVerifyTest.test_collapsed_spaces_are_reported_with_the_cell
func TestPythonTestConvertConversionverifytestTestCollapsedSpacesAreReportedWithTheCell(t *testing.T) {
	pythonRun(t, "test_convert.ConversionVerifyTest.test_collapsed_spaces_are_reported_with_the_cell")
}

// Python: test_convert.ConversionVerifyTest.test_conversion_requires_target_kind
func TestPythonTestConvertConversionverifytestTestConversionRequiresTargetKind(t *testing.T) {
	pythonRun(t, "test_convert.ConversionVerifyTest.test_conversion_requires_target_kind")
}

// Python: test_convert.ConversionVerifyTest.test_extra_cells_are_warned
func TestPythonTestConvertConversionverifytestTestExtraCellsAreWarned(t *testing.T) {
	pythonRun(t, "test_convert.ConversionVerifyTest.test_extra_cells_are_warned")
}

// Python: test_convert.ConversionVerifyTest.test_model_differences_block_conversion
func TestPythonTestConvertConversionverifytestTestModelDifferencesBlockConversion(t *testing.T) {
	pythonRun(t, "test_convert.ConversionVerifyTest.test_model_differences_block_conversion")
}

// Python: test_convert.ConvertTest.test_adds_blank_after_new_html_when_next_block_stays_the_same
func TestPythonTestConvertConverttestTestAddsBlankAfterNewHtmlWhenNextBlockStaysTheSame(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_adds_blank_after_new_html_when_next_block_stays_the_same")
}

// Python: test_convert.ConvertTest.test_allows_unit_spans_and_alignment_attributes
func TestPythonTestConvertConverttestTestAllowsUnitSpansAndAlignmentAttributes(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_allows_unit_spans_and_alignment_attributes")
}

// Python: test_convert.ConvertTest.test_already_same_kind_does_not_rewrite
func TestPythonTestConvertConverttestTestAlreadySameKindDoesNotRewrite(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_already_same_kind_does_not_rewrite")
}

// Python: test_convert.ConvertTest.test_api_requires_selector_and_known_target
func TestPythonTestConvertConverttestTestApiRequiresSelectorAndKnownTarget(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_api_requires_selector_and_known_target")
}

// Python: test_convert.ConvertTest.test_code_with_pipe_and_table_tag_text
func TestPythonTestConvertConverttestTestCodeWithPipeAndTableTagText(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_code_with_pipe_and_table_tag_text")
}

// Python: test_convert.ConvertTest.test_complex_list_item_table_round_trip
func TestPythonTestConvertConverttestTestComplexListItemTableRoundTrip(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_complex_list_item_table_round_trip")
}

// Python: test_convert.ConvertTest.test_container_prefixes_and_tight_warning
func TestPythonTestConvertConverttestTestContainerPrefixesAndTightWarning(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_container_prefixes_and_tight_warning")
}

// Python: test_convert.ConvertTest.test_dry_run_outputs_diff_and_preserves_bom_crlf
func TestPythonTestConvertConverttestTestDryRunOutputsDiffAndPreservesBomCrlf(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_dry_run_outputs_diff_and_preserves_bom_crlf")
}

// Python: test_convert.ConvertTest.test_empty_cells_and_header_only
func TestPythonTestConvertConverttestTestEmptyCellsAndHeaderOnly(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_empty_cells_and_header_only")
}

// Python: test_convert.ConvertTest.test_excluded_table_is_a_problem_even_with_same_kind
func TestPythonTestConvertConverttestTestExcludedTableIsAProblemEvenWithSameKind(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_excluded_table_is_a_problem_even_with_same_kind")
}

// Python: test_convert.ConvertTest.test_html_block_error_names_the_tag
func TestPythonTestConvertConverttestTestHtmlBlockErrorNamesTheTag(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_html_block_error_names_the_tag")
}

// Python: test_convert.ConvertTest.test_html_blocks_refused_in_both_directions
func TestPythonTestConvertConverttestTestHtmlBlocksRefusedInBothDirections(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_html_blocks_refused_in_both_directions")
}

// Python: test_convert.ConvertTest.test_html_cells_breaks_lists_pipes_and_alignment
func TestPythonTestConvertConverttestTestHtmlCellsBreaksListsPipesAndAlignment(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_html_cells_breaks_lists_pipes_and_alignment")
}

// Python: test_convert.ConvertTest.test_html_on_marker_line_uses_continuation_prefix
func TestPythonTestConvertConverttestTestHtmlOnMarkerLineUsesContinuationPrefix(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_html_on_marker_line_uses_continuation_prefix")
}

// Python: test_convert.ConvertTest.test_html_paragraph_lists_convert_both_ways
func TestPythonTestConvertConverttestTestHtmlParagraphListsConvertBothWays(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_html_paragraph_lists_convert_both_ways")
}

// Python: test_convert.ConvertTest.test_leading_and_repeated_breaks_refused
func TestPythonTestConvertConverttestTestLeadingAndRepeatedBreaksRefused(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_leading_and_repeated_breaks_refused")
}

// Python: test_convert.ConvertTest.test_link_definitions_refused
func TestPythonTestConvertConverttestTestLinkDefinitionsRefused(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_link_definitions_refused")
}

// Python: test_convert.ConvertTest.test_literal_task_lists_pipe_html_pipe
func TestPythonTestConvertConverttestTestLiteralTaskListsPipeHtmlPipe(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_literal_task_lists_pipe_html_pipe")
}

// Python: test_convert.ConvertTest.test_loose_lists_still_refused
func TestPythonTestConvertConverttestTestLooseListsStillRefused(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_loose_lists_still_refused")
}

// Python: test_convert.ConvertTest.test_multiple_selectors_and_unselected_problem
func TestPythonTestConvertConverttestTestMultipleSelectorsAndUnselectedProblem(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_multiple_selectors_and_unselected_problem")
}

// Python: test_convert.ConvertTest.test_one_problem_prevents_all_replacements
func TestPythonTestConvertConverttestTestOneProblemPreventsAllReplacements(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_one_problem_prevents_all_replacements")
}

// Python: test_convert.ConvertTest.test_paragraph_lists_pipe_html_pipe
func TestPythonTestConvertConverttestTestParagraphListsPipeHtmlPipe(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_paragraph_lists_pipe_html_pipe")
}

// Python: test_convert.ConvertTest.test_pipe_html_pipe_preserves_models_and_other_lines
func TestPythonTestConvertConverttestTestPipeHtmlPipePreservesModelsAndOtherLines(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_pipe_html_pipe_preserves_models_and_other_lines")
}

// Python: test_convert.ConvertTest.test_refuses_alignment_on_table_section_and_row
func TestPythonTestConvertConverttestTestRefusesAlignmentOnTableSectionAndRow(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_refuses_alignment_on_table_section_and_row")
}

// Python: test_convert.ConvertTest.test_refuses_attributes_on_each_structural_element
func TestPythonTestConvertConverttestTestRefusesAttributesOnEachStructuralElement(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_refuses_attributes_on_each_structural_element")
}

// Python: test_convert.ConvertTest.test_refuses_caption
func TestPythonTestConvertConverttestTestRefusesCaption(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_refuses_caption")
}

// Python: test_convert.ConvertTest.test_refuses_code_block_and_task_cells
func TestPythonTestConvertConverttestTestRefusesCodeBlockAndTaskCells(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_refuses_code_block_and_task_cells")
}

// Python: test_convert.ConvertTest.test_refuses_colgroups
func TestPythonTestConvertConverttestTestRefusesColgroups(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_refuses_colgroups")
}

// Python: test_convert.ConvertTest.test_refuses_header_shape
func TestPythonTestConvertConverttestTestRefusesHeaderShape(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_refuses_header_shape")
}

// Python: test_convert.ConvertTest.test_refuses_holes
func TestPythonTestConvertConverttestTestRefusesHoles(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_refuses_holes")
}

// Python: test_convert.ConvertTest.test_refuses_interstitial_comments
func TestPythonTestConvertConverttestTestRefusesInterstitialComments(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_refuses_interstitial_comments")
}

// Python: test_convert.ConvertTest.test_refuses_justify
func TestPythonTestConvertConverttestTestRefusesJustify(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_refuses_justify")
}

// Python: test_convert.ConvertTest.test_refuses_merges
func TestPythonTestConvertConverttestTestRefusesMerges(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_refuses_merges")
}

// Python: test_convert.ConvertTest.test_refuses_mixed_alignment
func TestPythonTestConvertConverttestTestRefusesMixedAlignment(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_refuses_mixed_alignment")
}

// Python: test_convert.ConvertTest.test_refuses_nested_table_in_cell
func TestPythonTestConvertConverttestTestRefusesNestedTableInCell(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_refuses_nested_table_in_cell")
}

// Python: test_convert.ConvertTest.test_refuses_text_before_or_after_table_on_same_line
func TestPythonTestConvertConverttestTestRefusesTextBeforeOrAfterTableOnSameLine(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_refuses_text_before_or_after_table_on_same_line")
}

// Python: test_convert.ConvertTest.test_refuses_tfoot
func TestPythonTestConvertConverttestTestRefusesTfoot(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_refuses_tfoot")
}

// Python: test_convert.ConvertTest.test_refuses_two_rows_in_thead
func TestPythonTestConvertConverttestTestRefusesTwoRowsInThead(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_refuses_two_rows_in_thead")
}

// Python: test_convert.ConvertTest.test_refuses_when_blank_line_changes_following_block
func TestPythonTestConvertConverttestTestRefusesWhenBlankLineChangesFollowingBlock(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_refuses_when_blank_line_changes_following_block")
}

// Python: test_convert.ConvertTest.test_relaxed_markdown_tasks_refused
func TestPythonTestConvertConverttestTestRelaxedMarkdownTasksRefused(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_relaxed_markdown_tasks_refused")
}

// Python: test_convert.ConvertTest.test_safe_single_html_block_converts
func TestPythonTestConvertConverttestTestSafeSingleHtmlBlockConverts(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_safe_single_html_block_converts")
}

// Python: test_convert.ConvertTest.test_table_in_description_list
func TestPythonTestConvertConverttestTestTableInDescriptionList(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_table_in_description_list")
}

// Python: test_convert.ConvertTest.test_trailing_and_only_breaks_convert
func TestPythonTestConvertConverttestTestTrailingAndOnlyBreaksConvert(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_trailing_and_only_breaks_convert")
}

// Python: test_tables.ComplexCellTest.test_cell_write_errors_include_position_in_both_render_paths
func TestPythonTestTablesComplexcelltestTestCellWriteErrorsIncludePositionInBothRenderPaths(t *testing.T) {
	pythonRun(t, "test_tables.ComplexCellTest.test_cell_write_errors_include_position_in_both_render_paths")
}

// Python: test_tables.ComplexCellTest.test_unchanged_nested_table_cell_is_preserved
func TestPythonTestTablesComplexcelltestTestUnchangedNestedTableCellIsPreserved(t *testing.T) {
	pythonRun(t, "test_tables.ComplexCellTest.test_unchanged_nested_table_cell_is_preserved")
}

// Python: test_tables.PatchTest.test_alignment
func TestPythonTestTablesPatchtestTestAlignment(t *testing.T) {
	pythonRun(t, "test_tables.PatchTest.test_alignment")
}

// Python: test_tables.PatchTest.test_blockquote_prefixes
func TestPythonTestTablesPatchtestTestBlockquotePrefixes(t *testing.T) {
	pythonRun(t, "test_tables.PatchTest.test_blockquote_prefixes")
}

// Python: test_tables.PatchTest.test_cell_becomes_chunk
func TestPythonTestTablesPatchtestTestCellBecomesChunk(t *testing.T) {
	pythonRun(t, "test_tables.PatchTest.test_cell_becomes_chunk")
}

// Python: test_tables.PatchTest.test_header_to_data
func TestPythonTestTablesPatchtestTestHeaderToData(t *testing.T) {
	pythonRun(t, "test_tables.PatchTest.test_header_to_data")
}

// Python: test_tables.PatchTest.test_implicit_end
func TestPythonTestTablesPatchtestTestImplicitEnd(t *testing.T) {
	pythonRun(t, "test_tables.PatchTest.test_implicit_end")
}

// Python: test_tables.PatchTest.test_ordered_list_container
func TestPythonTestTablesPatchtestTestOrderedListContainer(t *testing.T) {
	pythonRun(t, "test_tables.PatchTest.test_ordered_list_container")
}

// Python: test_tables.PipeRenderTest.test_alignment_majority
func TestPythonTestTablesPiperendertestTestAlignmentMajority(t *testing.T) {
	pythonRun(t, "test_tables.PipeRenderTest.test_alignment_majority")
}

// Python: test_tables.PipeRenderTest.test_edit_keeps_other_rows
func TestPythonTestTablesPiperendertestTestEditKeepsOtherRows(t *testing.T) {
	pythonRun(t, "test_tables.PipeRenderTest.test_edit_keeps_other_rows")
}

// Python: test_tables.PipeRenderTest.test_identical_rows
func TestPythonTestTablesPiperendertestTestIdenticalRows(t *testing.T) {
	pythonRun(t, "test_tables.PipeRenderTest.test_identical_rows")
}

// Python: test_tables.PipeRenderTest.test_new_column
func TestPythonTestTablesPiperendertestTestNewColumn(t *testing.T) {
	pythonRun(t, "test_tables.PipeRenderTest.test_new_column")
}

// Python: test_tables.PipeRenderTest.test_without_leading_pipes
func TestPythonTestTablesPiperendertestTestWithoutLeadingPipes(t *testing.T) {
	pythonRun(t, "test_tables.PipeRenderTest.test_without_leading_pipes")
}

// Python: test_tables.PipeToHtmlTest.test_in_list_item
func TestPythonTestTablesPipetohtmltestTestInListItem(t *testing.T) {
	pythonRun(t, "test_tables.PipeToHtmlTest.test_in_list_item")
}

// Python: test_tables.PipeToHtmlTest.test_merge_converts_and_keeps_following_list
func TestPythonTestTablesPipetohtmltestTestMergeConvertsAndKeepsFollowingList(t *testing.T) {
	pythonRun(t, "test_tables.PipeToHtmlTest.test_merge_converts_and_keeps_following_list")
}

// Python: test_tables.PipeToHtmlTest.test_pipes_are_unescaped_in_chunks
func TestPythonTestTablesPipetohtmltestTestPipesAreUnescapedInChunks(t *testing.T) {
	pythonRun(t, "test_tables.PipeToHtmlTest.test_pipes_are_unescaped_in_chunks")
}

// Python: test_tables.RegenerateTest.test_add_row_keeps_other_rows
func TestPythonTestTablesRegeneratetestTestAddRowKeepsOtherRows(t *testing.T) {
	pythonRun(t, "test_tables.RegenerateTest.test_add_row_keeps_other_rows")
}

// Python: test_tables.RegenerateTest.test_tfoot_stays
func TestPythonTestTablesRegeneratetestTestTfootStays(t *testing.T) {
	pythonRun(t, "test_tables.RegenerateTest.test_tfoot_stays")
}

// Python: test_workbook.ExportImportTest.test_adjacent_row_extends
func TestPythonTestWorkbookExportimporttestTestAdjacentRowExtends(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_adjacent_row_extends")
}

// Python: test_workbook.ExportImportTest.test_append_row_without_final_newline
func TestPythonTestWorkbookExportimporttestTestAppendRowWithoutFinalNewline(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_append_row_without_final_newline")
}

// Python: test_workbook.ExportImportTest.test_applied_twice
func TestPythonTestWorkbookExportimporttestTestAppliedTwice(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_applied_twice")
}

// Python: test_workbook.ExportImportTest.test_applied_twice_after_normalizing
func TestPythonTestWorkbookExportimporttestTestAppliedTwiceAfterNormalizing(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_applied_twice_after_normalizing")
}

// Python: test_workbook.ExportImportTest.test_cleared_trailing_column_warns
func TestPythonTestWorkbookExportimporttestTestClearedTrailingColumnWarns(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_cleared_trailing_column_warns")
}

// Python: test_workbook.ExportImportTest.test_cleared_trailing_row_does_not_warn_on_conflict
func TestPythonTestWorkbookExportimporttestTestClearedTrailingRowDoesNotWarnOnConflict(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_cleared_trailing_row_does_not_warn_on_conflict")
}

// Python: test_workbook.ExportImportTest.test_cleared_trailing_row_warns
func TestPythonTestWorkbookExportimporttestTestClearedTrailingRowWarns(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_cleared_trailing_row_warns")
}

// Python: test_workbook.ExportImportTest.test_column_style_alignment
func TestPythonTestWorkbookExportimporttestTestColumnStyleAlignment(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_column_style_alignment")
}

// Python: test_workbook.ExportImportTest.test_conflict_and_force
func TestPythonTestWorkbookExportimporttestTestConflictAndForce(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_conflict_and_force")
}

// Python: test_workbook.ExportImportTest.test_control_character_is_excluded
func TestPythonTestWorkbookExportimporttestTestControlCharacterIsExcluded(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_control_character_is_excluded")
}

// Python: test_workbook.ExportImportTest.test_conversion_then_reimport
func TestPythonTestWorkbookExportimporttestTestConversionThenReimport(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_conversion_then_reimport")
}

// Python: test_workbook.ExportImportTest.test_delete_all_pipe_body_rows
func TestPythonTestWorkbookExportimporttestTestDeleteAllPipeBodyRows(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_delete_all_pipe_body_rows")
}

// Python: test_workbook.ExportImportTest.test_delete_last_row_with_mixed_line_endings
func TestPythonTestWorkbookExportimporttestTestDeleteLastRowWithMixedLineEndings(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_delete_last_row_with_mixed_line_endings")
}

// Python: test_workbook.ExportImportTest.test_delete_last_row_without_final_newline
func TestPythonTestWorkbookExportimporttestTestDeleteLastRowWithoutFinalNewline(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_delete_last_row_without_final_newline")
}

// Python: test_workbook.ExportImportTest.test_deleted_rows_do_not_warn_about_columns
func TestPythonTestWorkbookExportimporttestTestDeletedRowsDoNotWarnAboutColumns(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_deleted_rows_do_not_warn_about_columns")
}

// Python: test_workbook.ExportImportTest.test_discarded_cell_features
func TestPythonTestWorkbookExportimporttestTestDiscardedCellFeatures(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_discarded_cell_features")
}

// Python: test_workbook.ExportImportTest.test_discarded_features_limit_positions
func TestPythonTestWorkbookExportimporttestTestDiscardedFeaturesLimitPositions(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_discarded_features_limit_positions")
}

// Python: test_workbook.ExportImportTest.test_dry_run_cell_limit_per_table
func TestPythonTestWorkbookExportimporttestTestDryRunCellLimitPerTable(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_dry_run_cell_limit_per_table")
}

// Python: test_workbook.ExportImportTest.test_dry_run_shows_difference_after_long_prefix
func TestPythonTestWorkbookExportimporttestTestDryRunShowsDifferenceAfterLongPrefix(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_dry_run_shows_difference_after_long_prefix")
}

// Python: test_workbook.ExportImportTest.test_dry_run_summary_and_cells_follow_each_table
func TestPythonTestWorkbookExportimporttestTestDryRunSummaryAndCellsFollowEachTable(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_dry_run_summary_and_cells_follow_each_table")
}

// Python: test_workbook.ExportImportTest.test_edit_and_import
func TestPythonTestWorkbookExportimporttestTestEditAndImport(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_edit_and_import")
}

// Python: test_workbook.ExportImportTest.test_edit_header_only_pipe
func TestPythonTestWorkbookExportimporttestTestEditHeaderOnlyPipe(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_edit_header_only_pipe")
}

// Python: test_workbook.ExportImportTest.test_edit_other_cell_preserves_raw_html_link_row
func TestPythonTestWorkbookExportimporttestTestEditOtherCellPreservesRawHtmlLinkRow(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_edit_other_cell_preserves_raw_html_link_row")
}

// Python: test_workbook.ExportImportTest.test_empty_merged_trailing_cells_are_not_empty_rows_or_columns
func TestPythonTestWorkbookExportimporttestTestEmptyMergedTrailingCellsAreNotEmptyRowsOrColumns(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_empty_merged_trailing_cells_are_not_empty_rows_or_columns")
}

// Python: test_workbook.ExportImportTest.test_excel_shaped_round_trip
func TestPythonTestWorkbookExportimporttestTestExcelShapedRoundTrip(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_excel_shaped_round_trip")
}

// Python: test_workbook.ExportImportTest.test_expected_cell_formats_do_not_warn
func TestPythonTestWorkbookExportimporttestTestExpectedCellFormatsDoNotWarn(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_expected_cell_formats_do_not_warn")
}

// Python: test_workbook.ExportImportTest.test_export_does_not_warn_about_link_definitions_in_code
func TestPythonTestWorkbookExportimporttestTestExportDoesNotWarnAboutLinkDefinitionsInCode(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_export_does_not_warn_about_link_definitions_in_code")
}

// Python: test_workbook.ExportImportTest.test_export_warns_about_link_definitions
func TestPythonTestWorkbookExportimporttestTestExportWarnsAboutLinkDefinitions(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_export_warns_about_link_definitions")
}

// Python: test_workbook.ExportImportTest.test_far_values_are_ignored
func TestPythonTestWorkbookExportimporttestTestFarValuesAreIgnored(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_far_values_are_ignored")
}

// Python: test_workbook.ExportImportTest.test_font
func TestPythonTestWorkbookExportimporttestTestFont(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_font")
}

// Python: test_workbook.ExportImportTest.test_formula_like_text
func TestPythonTestWorkbookExportimporttestTestFormulaLikeText(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_formula_like_text")
}

// Python: test_workbook.ExportImportTest.test_html_alignment_with_added_row
func TestPythonTestWorkbookExportimporttestTestHtmlAlignmentWithAddedRow(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_html_alignment_with_added_row")
}

// Python: test_workbook.ExportImportTest.test_html_header_format_and_bold_only
func TestPythonTestWorkbookExportimporttestTestHtmlHeaderFormatAndBoldOnly(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_html_header_format_and_bold_only")
}

// Python: test_workbook.ExportImportTest.test_html_header_with_added_column
func TestPythonTestWorkbookExportimporttestTestHtmlHeaderWithAddedColumn(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_html_header_with_added_column")
}

// Python: test_workbook.ExportImportTest.test_html_verification_rejects_lost_structure
func TestPythonTestWorkbookExportimporttestTestHtmlVerificationRejectsLostStructure(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_html_verification_rejects_lost_structure")
}

// Python: test_workbook.ExportImportTest.test_hyperlinks_with_visible_targets_do_not_warn
func TestPythonTestWorkbookExportimporttestTestHyperlinksWithVisibleTargetsDoNotWarn(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_hyperlinks_with_visible_targets_do_not_warn")
}

// Python: test_workbook.ExportImportTest.test_identical_table_count_conflict_and_force
func TestPythonTestWorkbookExportimporttestTestIdenticalTableCountConflictAndForce(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_identical_table_count_conflict_and_force")
}

// Python: test_workbook.ExportImportTest.test_identical_tables
func TestPythonTestWorkbookExportimporttestTestIdenticalTables(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_identical_tables")
}

// Python: test_workbook.ExportImportTest.test_identical_tables_applied_twice
func TestPythonTestWorkbookExportimporttestTestIdenticalTablesAppliedTwice(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_identical_tables_applied_twice")
}

// Python: test_workbook.ExportImportTest.test_import_missing_number_lists_exported_numbers
func TestPythonTestWorkbookExportimporttestTestImportMissingNumberListsExportedNumbers(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_import_missing_number_lists_exported_numbers")
}

// Python: test_workbook.ExportImportTest.test_import_selected_tables_only
func TestPythonTestWorkbookExportimporttestTestImportSelectedTablesOnly(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_import_selected_tables_only")
}

// Python: test_workbook.ExportImportTest.test_internal_and_empty_hyperlinks
func TestPythonTestWorkbookExportimporttestTestInternalAndEmptyHyperlinks(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_internal_and_empty_hyperlinks")
}

// Python: test_workbook.ExportImportTest.test_line_endings_and_bom
func TestPythonTestWorkbookExportimporttestTestLineEndingsAndBom(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_line_endings_and_bom")
}

// Python: test_workbook.ExportImportTest.test_list_spacing_is_unchanged_and_original_cell_is_reused
func TestPythonTestWorkbookExportimporttestTestListSpacingIsUnchangedAndOriginalCellIsReused(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_list_spacing_is_unchanged_and_original_cell_is_reused")
}

// Python: test_workbook.ExportImportTest.test_many_tables_with_body
func TestPythonTestWorkbookExportimporttestTestManyTablesWithBody(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_many_tables_with_body")
}

// Python: test_workbook.ExportImportTest.test_merged_cells_keep_text_format
func TestPythonTestWorkbookExportimporttestTestMergedCellsKeepTextFormat(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_merged_cells_keep_text_format")
}

// Python: test_workbook.ExportImportTest.test_mixed_line_endings_inside_table
func TestPythonTestWorkbookExportimporttestTestMixedLineEndingsInsideTable(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_mixed_line_endings_inside_table")
}

// Python: test_workbook.ExportImportTest.test_multiple_tables_open_index_sheet
func TestPythonTestWorkbookExportimporttestTestMultipleTablesOpenIndexSheet(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_multiple_tables_open_index_sheet")
}

// Python: test_workbook.ExportImportTest.test_no_edit_round_trip
func TestPythonTestWorkbookExportimporttestTestNoEditRoundTrip(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_no_edit_round_trip")
}

// Python: test_workbook.ExportImportTest.test_numbers_and_dates
func TestPythonTestWorkbookExportimporttestTestNumbersAndDates(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_numbers_and_dates")
}

// Python: test_workbook.ExportImportTest.test_old_metadata_applied_twice
func TestPythonTestWorkbookExportimporttestTestOldMetadataAppliedTwice(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_old_metadata_applied_twice")
}

// Python: test_workbook.ExportImportTest.test_old_metadata_without_same
func TestPythonTestWorkbookExportimporttestTestOldMetadataWithoutSame(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_old_metadata_without_same")
}

// Python: test_workbook.ExportImportTest.test_only_increased_trailing_empty_rows_warn
func TestPythonTestWorkbookExportimporttestTestOnlyIncreasedTrailingEmptyRowsWarn(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_only_increased_trailing_empty_rows_warn")
}

// Python: test_workbook.ExportImportTest.test_original_empty_trailing_rows_and_columns_do_not_warn
func TestPythonTestWorkbookExportimporttestTestOriginalEmptyTrailingRowsAndColumnsDoNotWarn(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_original_empty_trailing_rows_and_columns_do_not_warn")
}

// Python: test_workbook.ExportImportTest.test_pipe_table_typed_in_cell_has_positioned_error
func TestPythonTestWorkbookExportimporttestTestPipeTableTypedInCellHasPositionedError(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_pipe_table_typed_in_cell_has_positioned_error")
}

// Python: test_workbook.ExportImportTest.test_renamed_and_deleted_sheets
func TestPythonTestWorkbookExportimporttestTestRenamedAndDeletedSheets(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_renamed_and_deleted_sheets")
}

// Python: test_workbook.ExportImportTest.test_rich_text_warning_is_preserved
func TestPythonTestWorkbookExportimporttestTestRichTextWarningIsPreserved(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_rich_text_warning_is_preserved")
}

// Python: test_workbook.ExportImportTest.test_selected_import_of_swapped_tables
func TestPythonTestWorkbookExportimporttestTestSelectedImportOfSwappedTables(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_selected_import_of_swapped_tables")
}

// Python: test_workbook.ExportImportTest.test_selected_import_still_reports_unrelated_sheets
func TestPythonTestWorkbookExportimporttestTestSelectedImportStillReportsUnrelatedSheets(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_selected_import_still_reports_unrelated_sheets")
}

// Python: test_workbook.ExportImportTest.test_single_table_document_opens_its_sheet
func TestPythonTestWorkbookExportimporttestTestSingleTableDocumentOpensItsSheet(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_single_table_document_opens_its_sheet")
}

// Python: test_workbook.ExportImportTest.test_single_table_opens_its_sheet
func TestPythonTestWorkbookExportimporttestTestSingleTableOpensItsSheet(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_single_table_opens_its_sheet")
}

// Python: test_workbook.ExportImportTest.test_summary_counts_inserted_row_only
func TestPythonTestWorkbookExportimporttestTestSummaryCountsInsertedRowOnly(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_summary_counts_inserted_row_only")
}

// Python: test_workbook.ExportImportTest.test_swapped_tables_in_document
func TestPythonTestWorkbookExportimporttestTestSwappedTablesInDocument(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_swapped_tables_in_document")
}

// Python: test_workbook.ExportImportTest.test_text_cells
func TestPythonTestWorkbookExportimporttestTestTextCells(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_text_cells")
}

// Python: test_workbook.ExportImportTest.test_typed_tasks_keep_automatic_conversion_and_keep_kind
func TestPythonTestWorkbookExportimporttestTestTypedTasksKeepAutomaticConversionAndKeepKind(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_typed_tasks_keep_automatic_conversion_and_keep_kind")
}

// Python: test_workbook.ExportImportTest.test_unedited_html_with_holes
func TestPythonTestWorkbookExportimporttestTestUneditedHtmlWithHoles(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_unedited_html_with_holes")
}

// Python: test_workbook.ExportImportTest.test_unedited_sheet_preserves_document_edit
func TestPythonTestWorkbookExportimporttestTestUneditedSheetPreservesDocumentEdit(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_unedited_sheet_preserves_document_edit")
}

// Python: test_workbook.ExportImportTest.test_unselected_missing_sheet_is_ignored
func TestPythonTestWorkbookExportimporttestTestUnselectedMissingSheetIsIgnored(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_unselected_missing_sheet_is_ignored")
}

// Python: test_workbook.ExportImportTest.test_unselected_sheet_errors_and_warnings_are_ignored
func TestPythonTestWorkbookExportimporttestTestUnselectedSheetErrorsAndWarningsAreIgnored(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_unselected_sheet_errors_and_warnings_are_ignored")
}
