package table

import (
	"github.com/ryo-aoki-pc/md-table-excel/internal/cellsyntax"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pipe_table._render_list と cell_syntax._render_pipe_block のタスクの印の往復を確かめる。
func TestTaskMarkerRoundTrip(t *testing.T) {
	for _, marker := range []string{"[ ]", "[x]", "[注]"} {
		source := "<ul><li>" + marker + " task</li></ul>"
		text := PipeCellToExcel(source)
		if text != "- \\"+marker+" task" {
			t.Fatalf("タスクの印をエスケープしていない: %q", text)
		}
		got, err := cellsyntax.ToPipeCell(text)
		if err != nil || got != source {
			t.Fatalf("タスクの印の往復が違う: %q %v", got, err)
		}
	}
}

// pipe_table._tag の、開始番号 0 と省略の違いを確かめる。
func TestZeroStart(t *testing.T) {
	got := PipeCellToExcel(`<ol start="0"><li>zero</li><li>one</li></ol>`)
	if got != "0. zero\n1. one" {
		t.Fatalf("開始番号 0 が変わった: %q", got)
	}
}

// pipe_table.render と html_table.render の元の文字の再利用を確かめる。
func TestSourceReuse(t *testing.T) {
	for _, source := range []string{
		"| a | b |\n| :-- | --: |\n| old | `x` |\n",
		"<table class='t'><tr><th id=\"a\">A</th><th>B</th></tr>\n<tr><td>old</td><td>Y</td></tr></table>\n",
	} {
		md := NewMarkdown("test.md", source, false)
		r := FindTables(md)[0]
		var cells []*model.Cell
		for _, c := range r.Model().Cells {
			d := *c
			cells = append(cells, &d)
		}
		m := model.New(r.Kind, r.Model().Rows, r.Model().Cols, cells, nil)
		m.CellAt(1, 0).Text = "new"
		var warnings, lines []string
		var e error
		if r.Kind == "pipe" {
			lines, e = RenderPipe(r, m, &warnings)
		} else {
			lines, e = RenderHTML(r, m, &warnings)
		}
		if e != nil {
			t.Fatal(e)
		}
		got := strings.Join(lines, "\n") + "\n"
		want := strings.Replace(source, "old", "new", 1)
		if got != want {
			t.Fatalf("%s の変更していない文字が変わった:\n%s", r.Kind, got)
		}
		if r.Kind == "html" {
			cells = append(cells, model.NewCell(2, 0, "added"), model.NewCell(2, 1, "z"))
			grown := model.New("html", 3, 2, cells, nil)
			lines, e = RenderHTML(r, grown, &warnings)
			if e != nil {
				t.Fatal(e)
			}
			again := FindTables(NewMarkdown("test.md", strings.Join(lines, "\n")+"\n", false))
			if len(again) != 1 || again[0].Excluded != "" || !again[0].Model().SameContent(grown) {
				t.Fatal("作り直した表の中身が違う")
			}
		}
	}
}

// document.apply_replacements と write_text_atomic の BOM・改行の保持を確かめる。
func TestAtomicBOMAndEndings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "日本語 の 文書.md")
	original := "# 見出し\r\n\r\n| a |\r\n|---|\r\n| b |"
	if e := os.WriteFile(path, append([]byte{0xef, 0xbb, 0xbf}, []byte(original)...), 0600); e != nil {
		t.Fatal(e)
	}
	md, e := Load(path)
	if e != nil {
		t.Fatal(e)
	}
	region := FindTables(md)[0]
	text := ApplyReplacements(md, []*Replacement{{Region: region, Lines: []string{"| a |", "|---|", "| b |", "| c |"}}})
	expected := strings.Replace(original, "| b |", "| b |\r\n| c |", 1)
	if text != expected {
		t.Fatalf("改行が変わった: %q", text)
	}
	if e := WriteTextAtomic(path, text, md.BOM); e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if string(data) != "\xef\xbb\xbf"+expected {
		t.Fatalf("BOM または内容が変わった: %q", data)
	}
	entries, e := os.ReadDir(filepath.Dir(path))
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 1 {
		t.Fatalf("一時ファイルが残った: %v", entries)
	}
}

// document.select_tables の Unicode 数字と見出しへの再検索を確かめる。
func TestUnicodeSelectors(t *testing.T) {
	md := NewMarkdown("test.md", "# L999\n\n| a |\n|---|\n", false)
	regions := FindTables(md)
	for _, s := range []string{"１", "L４", "L999"} {
		selected, e := SelectTables(regions, []string{s})
		if e != nil || len(selected) != 1 || selected[0] != regions[0] {
			t.Fatalf("%s の選択が違う: %v %v", s, selected, e)
		}
	}
	huge := strings.Repeat("９", 30)
	hugeRegions := FindTables(NewMarkdown("test.md", "# "+huge+"\n\n| a |\n|---|\n", false))
	selected, e := SelectTables(hugeRegions, []string{huge})
	if e != nil || len(selected) != 1 {
		t.Fatalf("大きな数字の見出しを選べない: %v", e)
	}
}
