package tableedit

import (
	"testing"

	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
)

// 入力の順序と違う位置にある二つの表も、一度の差し替えと検査で編集する。
func TestApplyEditsMultipleRegions(t *testing.T) {
	input := "前\n\n| a |\n| --- |\n\n<table><tr><td>b</td></tr></table>\n\n後\n"
	d := Parse(input)
	pipe, html := d.regions[0], d.regions[1]
	pl, pm, pe, err := table.InsertPipeRows(pipe, 1, []string{"| x |"})
	if err != nil {
		t.Fatal(err)
	}
	hl, hm, he, err := table.DuplicateHTMLRows(html, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	result, err := d.applyEdits([]regionEdit{{region: html, lines: hl, expected: hm, expect: he}, {region: pipe, lines: pl, expected: pm, expect: pe}}, []string{"知らせ", "知らせ"})
	want := "前\n\n| a |\n| --- |\n| x |\n\n<table><tr><td>b</td></tr><tr><td>b</td></tr></table>\n\n後\n"
	if err != nil || result.Text != want || !result.Changed {
		t.Fatalf("複数の表の差し替え: %+v、%v", result, err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0] != "知らせ" || d.Text() != input {
		t.Fatal("警告が重複するか、元の文書が変わった")
	}
}
