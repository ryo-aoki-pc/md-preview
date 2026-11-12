package sync

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
)

// Python: test_workbook.ExportImportTest.test_merged_cells_keep_text_format
func TestPythonMergedCellsKeepTextFormat(t *testing.T) {
	directory := t.TempDir()
	md := filepath.Join(directory, "文書.md")
	book := filepath.Join(directory, "book.xlsx")
	source := "<table>\n<tr><th rowspan=\"2\">a</th><th>b</th></tr>\n<tr><td>c</td></tr>\n</table>\n"
	if e := os.WriteFile(md, []byte(source), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := Export(md, book, nil, false); e != nil {
		t.Fatal(e)
	}
	archive, e := zip.OpenReader(book)
	if e != nil {
		t.Fatal(e)
	}
	defer archive.Close()
	parts := map[string][]byte{}
	for _, f := range archive.File {
		if f.Name == "xl/worksheets/sheet2.xml" || f.Name == "xl/styles.xml" {
			r, e := f.Open()
			if e != nil {
				t.Fatal(e)
			}
			parts[f.Name], e = io.ReadAll(r)
			r.Close()
			if e != nil {
				t.Fatal(e)
			}
		}
	}
	var sheet struct {
		Cells []struct {
			Ref   string `xml:"r,attr"`
			Style int    `xml:"s,attr"`
		} `xml:"sheetData>row>c"`
		Merges []struct {
			Ref string `xml:"ref,attr"`
		} `xml:"mergeCells>mergeCell"`
	}
	var styles struct {
		Formats []struct {
			Number int `xml:"numFmtId,attr"`
		} `xml:"cellXfs>xf"`
	}
	if e = xml.Unmarshal(parts["xl/worksheets/sheet2.xml"], &sheet); e != nil {
		t.Fatal(e)
	}
	if e = xml.Unmarshal(parts["xl/styles.xml"], &styles); e != nil {
		t.Fatal(e)
	}
	ids := map[string]int{}
	for _, c := range sheet.Cells {
		ids[c.Ref] = c.Style
	}
	if len(sheet.Merges) != 1 || sheet.Merges[0].Ref != "A1:A2" || ids["A1"] != ids["A2"] || styles.Formats[ids["A2"]].Number != 49 {
		t.Fatal("結合に隠れるセルの文字列の書式が失われた")
	}
	report, e := ImportWorkbook(book, md, true, false, false, false, nil)
	if e != nil {
		t.Fatal(e)
	}
	if report.Changed || len(report.Errors) > 0 {
		t.Fatalf("結合した表の無編集の往復: %v", report)
	}
}

// Python: test_convert.ConversionVerifyTest.test_changed_cell_text_prevents_file_write
func TestPythonVerifyRejectsChangedText(t *testing.T) {
	source := "| 左 | 中央 | 右 |\n| :--- | :---: | ---: |\n| a | b | c |\n"
	path := filepath.Join(t.TempDir(), "文書.md")
	if e := os.WriteFile(path, []byte(source), 0600); e != nil {
		t.Fatal(e)
	}
	md := table.NewMarkdown(path, source, false)
	r := table.FindTables(md)[0]
	warnings := []string{}
	lines, e := table.RenderFromPipe(r, r.Model(), &warnings)
	if e != nil {
		t.Fatal(e)
	}
	for i := range lines {
		lines[i] = strings.ReplaceAll(lines[i], ">a</td>", ">違う</td>")
	}
	replacement := &table.Replacement{Region: r, Lines: lines, Model: r.Model(), TargetKind: "html"}
	got := Verify(md, strings.Join(lines, "\n")+"\n", []*table.Replacement{replacement})
	want := []string{"表 1: 種類を変えると、表の中身が変わる（A2: 「a」→「違う」）"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("内容の違いを止めない: %v", got)
	}
	before, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if string(before) != source {
		t.Fatal("確認中に元文書が変わった")
	}
}

// Python: test_workbook.ExportImportTest.test_html_verification_rejects_lost_structure
func TestPythonVerifyRejectsLostAlignment(t *testing.T) {
	source := "<table>\n<tr><td>a</td></tr>\n</table>\n"
	md := table.NewMarkdown("<test>", source, false)
	r := table.FindTables(md)[0]
	m := cloneModel(r.Model(), "html")
	m.Cells[0].Text = "x"
	m.Cells[0].Align = "right"
	warnings := []string{}
	lines, _, e := render(r, m, &warnings, false)
	if e != nil {
		t.Fatal(e)
	}
	for i := range lines {
		lines[i] = strings.ReplaceAll(lines[i], ` align="right"`, "")
	}
	repl := &table.Replacement{Region: r, Lines: lines, Model: m}
	want := []string{"表 1: 書き戻した表の形・見出し・配置が、ブックの内容と違う"}
	if got := Verify(md, strings.Join(lines, "\n")+"\n", []*table.Replacement{repl}); !reflect.DeepEqual(got, want) {
		t.Fatalf("配置の消失を止めない: %v", got)
	}
}
