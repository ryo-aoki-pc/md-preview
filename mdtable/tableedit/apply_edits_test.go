package tableedit

import (
	"testing"

	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/table"
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

// 明示した種類は、行・列の編集の検査があっても差し替え先へ引き継ぐ。
func TestApplyEditsTargetKind(t *testing.T) {
	for _, target := range []string{"html", "pipe"} {
		t.Run(target, func(t *testing.T) {
			input := "前\n\n| h |\n| --- |\n| a |\n\n後\n"
			if target == "pipe" {
				input = "前\n\n<table><tr><th>h</th></tr><tr><td>a</td></tr></table>\n\n後\n"
			}
			d := Parse(input)
			r := d.regions[0]
			var lines, warnings []string
			var err error
			if target == "html" {
				lines, err = table.RenderFromPipe(r, r.Model(), &warnings)
			} else {
				lines, err = table.RenderNewPipe(r, r.Model(), &warnings)
			}
			if err != nil {
				t.Fatal(err)
			}
			result, err := d.applyEdits([]regionEdit{{region: r, lines: lines, kind: target, expected: r.Model(), shape: true}}, warnings)
			if err != nil {
				t.Fatalf("表の種類の差し替え: %v", err)
			}
			if !result.Changed || len(result.Document.regions) != 1 || result.Document.regions[0].Kind != target || d.Text() != input {
				t.Fatalf("種類が変わらないか、元の文書が変わった: %+v", result)
			}
		})
	}
}
