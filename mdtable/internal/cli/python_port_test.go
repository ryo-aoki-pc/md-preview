// Python の単体テストと同じ入力を保存したデータで検査する。
package cli

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
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
	f, err := os.Open("../../testdata/unit/cli.json.gz")
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

// cli.main の出力先だけを一時ファイルに向け、実際の解析とコマンド処理を実行する。
func pythonCommand(t *testing.T, argv []string) (int, string, string) {
	return pythonInvoke(t, context.Background(), argv, func(string) error { return fmt.Errorf("このテストでは Excel を起動しない") })
}
func pythonInvoke(t *testing.T, ctx context.Context, argv []string, open func(string) error) (int, string, string) {
	t.Helper()
	oldOut, oldErr := stdout, stderr
	out, e := os.CreateTemp(t.TempDir(), "stdout")
	if e != nil {
		t.Fatal(e)
	}
	defer out.Close()
	errout, e := os.CreateTemp(t.TempDir(), "stderr")
	if e != nil {
		t.Fatal(e)
	}
	defer errout.Close()
	stdout, stderr = textStream{out}, textStream{errout}
	defer func() { stdout, stderr = oldOut, oldErr }()
	code := mainContext(ctx, argv, open)
	a, e := os.ReadFile(out.Name())
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(errout.Name())
	if e != nil {
		t.Fatal(e)
	}
	if runtimeGOOSWindows() {
		a = bytes.ReplaceAll(a, []byte("\r\n"), []byte("\n"))
		b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	}
	return code, string(a), string(b)
}
func runtimeGOOSWindows() bool { return os.PathSeparator == '\\' }
func pythonEvaluate(t *testing.T, c pythonCase) (any, error) {
	input := pythonDecode[struct {
		Args  []string
		Files map[string]struct{ Book, Text string }
	}](t, c.Input)
	directory := t.TempDir()
	for name, f := range input.Files {
		path := filepath.Join(directory, name)
		if f.Book != "" {
			pythonRestoreBook(t, f.Book, path, directory)
		} else {
			pythonWriteHex(t, path, f.Text)
		}
	}
	for i, s := range input.Args {
		input.Args[i] = strings.ReplaceAll(s, "<DIR>", directory)
	}
	code, out, errout := pythonCommand(t, input.Args)
	expected := pythonDecode[struct{ Code int }](t, c.Want)
	after := map[string]string{}
	entries, e := os.ReadDir(directory)
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range entries {
		if strings.HasSuffix(p.Name(), ".md") {
			after[p.Name()] = pythonReadHex(t, filepath.Join(directory, p.Name()))
		}
	}
	if expected.Code == 2 {
		if code != 2 || out != "" || errout == "" || strings.Contains(errout, "Traceback") {
			t.Fatalf("引数の誤りの扱い: %d %q %q", code, out, errout)
		}
		// argparse の使い方の文だけを除き、終了コード・標準出力・文書は同じ条件で比べる。
		wanted := pythonDecode[map[string]any](t, c.Want)
		errout = wanted["err"].(string)
	}
	return pythonNormalize(map[string]any{"code": code, "out": out, "err": errout, "after": after}, directory), nil
}

// Python: test_cli.CliTest.test_convert_already_same_kind
func TestPythonTestCliClitestTestConvertAlreadySameKind(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_convert_already_same_kind")
}

// Python: test_cli.CliTest.test_convert_dry_run_then_write_and_back
func TestPythonTestCliClitestTestConvertDryRunThenWriteAndBack(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_convert_dry_run_then_write_and_back")
}

// Python: test_cli.CliTest.test_convert_failure_does_not_change_any_table
func TestPythonTestCliClitestTestConvertFailureDoesNotChangeAnyTable(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_convert_failure_does_not_change_any_table")
}

// Python: test_cli.CliTest.test_convert_raw_html_links_to_pipe_and_back
func TestPythonTestCliClitestTestConvertRawHtmlLinksToPipeAndBack(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_convert_raw_html_links_to_pipe_and_back")
}

// Python: test_cli.CliTest.test_convert_requires_table_and_target
func TestPythonTestCliClitestTestConvertRequiresTableAndTarget(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_convert_requires_table_and_target")
}

// Python: test_cli.CliTest.test_errors_without_traceback
func TestPythonTestCliClitestTestErrorsWithoutTraceback(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_errors_without_traceback")
}

// Python: test_cli.CliTest.test_export_import
func TestPythonTestCliClitestTestExportImport(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_export_import")
}

// Python: test_cli.CliTest.test_export_with_heading_and_import_with_number
func TestPythonTestCliClitestTestExportWithHeadingAndImportWithNumber(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_export_with_heading_and_import_with_number")
}

// Python: test_cli.CliTest.test_force_does_not_write_when_table_cannot_be_rendered
func TestPythonTestCliClitestTestForceDoesNotWriteWhenTableCannotBeRendered(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_force_does_not_write_when_table_cannot_be_rendered")
}

// Python: test_cli.CliTest.test_import_rejects_heading_or_line_selector
func TestPythonTestCliClitestTestImportRejectsHeadingOrLineSelector(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_import_rejects_heading_or_line_selector")
}

// Python: test_cli.CliTest.test_keep_kind_blocks_conversion_and_other_tables
func TestPythonTestCliClitestTestKeepKindBlocksConversionAndOtherTables(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_keep_kind_blocks_conversion_and_other_tables")
}

// Python: test_cli.CliTest.test_keep_kind_blocks_merged_cell
func TestPythonTestCliClitestTestKeepKindBlocksMergedCell(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_keep_kind_blocks_merged_cell")
}

// Python: test_cli.CliTest.test_list
func TestPythonTestCliClitestTestList(t *testing.T) { pythonRun(t, "test_cli.CliTest.test_list") }

// Python: test_cli.CliTest.test_list_json
func TestPythonTestCliClitestTestListJson(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_list_json")
}

// Python: test_cli.CliTest.test_list_json_excluded_and_without_heading
func TestPythonTestCliClitestTestListJsonExcludedAndWithoutHeading(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_list_json_excluded_and_without_heading")
}

// Python: test_cli.CliTest.test_list_json_without_tables
func TestPythonTestCliClitestTestListJsonWithoutTables(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_list_json_without_tables")
}

// Python: test_cli.CliTest.test_strict_text_and_force_are_incompatible
func TestPythonTestCliClitestTestStrictTextAndForceAreIncompatible(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_strict_text_and_force_are_incompatible")
}

// Python: test_cli.CliTest.test_strict_text_blocks_non_text_values
func TestPythonTestCliClitestTestStrictTextBlocksNonTextValues(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_strict_text_blocks_non_text_values")
}

// Python: test_cli.CliTest.test_strict_text_blocks_value_matching_original_text
func TestPythonTestCliClitestTestStrictTextBlocksValueMatchingOriginalText(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_strict_text_blocks_value_matching_original_text")
}

// Python: test_cli.CliTest.test_table_selection
func TestPythonTestCliClitestTestTableSelection(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_table_selection")
}

// Python: test_cli.CliTest.test_usage_error
func TestPythonTestCliClitestTestUsageError(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_usage_error")
}
