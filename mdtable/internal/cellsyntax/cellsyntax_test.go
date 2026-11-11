package cellsyntax

import "testing"

// cell_syntax.normalize_list_spacing の、段落を中断できる最外側のリストだけを確かめる。
func TestNormalizeListSpacing(t *testing.T) {
	for _, test := range []struct{ source, want string }{
		{"段落\n\n- 項目", "段落\n- 項目"},
		{"段落\n \n\n1. 項目", "段落\n1. 項目"},
		{"段落\n\n2. 項目", "段落\n\n2. 項目"},
		{"段落\n\n-", "段落\n\n-"},
		{"> 段落\n>\n> - 項目", "> 段落\n>\n> - 項目"},
		{"# 見出し\n\n- 項目", "# 見出し\n\n- 項目"},
	} {
		if got := NormalizeListSpacing(test.source); got != test.want {
			t.Errorf("%q の空行が違う: %q", test.source, got)
		}
	}
}

// cell_syntax.task_marker の Unicode の印と Python の末尾の改行の扱いを確かめる。
func TestTaskMarker(t *testing.T) {
	for _, source := range []string{"[ ] task", "[x] task", "[X]", "[注] task", "[注]\n"} {
		if !TaskMarker(source) {
			t.Errorf("タスクの印を読めない: %q", source)
		}
	}
	if TaskMarker("[注]text") || TaskMarker("\\[x] task") {
		t.Fatal("タスクでない文字を印と読んだ")
	}
}
