package table

import (
	"testing"

	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/cellsyntax"
)

// sync._check_conversion_cells の検査を、移した先からも確かめる。
func TestCheckConversionCells(t *testing.T) {
	for _, test := range []struct{ name, source, target, reason string }{
		{"先頭の改行", "| h |\n|---|\n| <br>text |\n", "html", "セルの先頭の <br> は、HTML の表にすると消える"},
		{"続く改行", "| h |\n|---|\n| a<br><br><br>b |\n", "html", "3 つ以上続く <br> は、HTML の表では段落の区切り 1 つになる"},
		{"HTML のブロック", "| h |\n|---|\n| <div>hello<br>world</div> |\n", "html", "セルに HTML のブロック（<div>）があり、種類を変えると描画が変わる"},
		{"リンク参照の定義", "<table>\n<tr><th>h</th></tr>\n<tr><td>\n\n[a]: /url\n\n</td></tr>\n</table>\n", "pipe", "セルの中にリンク参照の定義がある"},
		{"通常のパイプのセル", "| h |\n|---|\n| **text** |\n", "html", ""},
		{"改行だけのセル", "| h |\n|---|\n| <br> |\n", "html", ""},
		{"一行の HTML", "| h |\n|---|\n| <div>text</div> |\n", "html", ""},
		{"通常の HTML のセル", "<table><tr><th>h</th></tr><tr><td>text</td></tr></table>\n", "pipe", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			regions := FindTables(NewMarkdown("test.md", test.source, false))
			if len(regions) != 1 || regions[0].Excluded != "" {
				t.Fatalf("見本を表として読めない: %q", test.source)
			}
			err := CheckConversionCells(regions[0], test.target)
			if test.reason == "" {
				if err != nil {
					t.Fatalf("変換できるセルが断られた: %v", err)
				}
				return
			}
			reason, ok := err.(*cellsyntax.NotRepresentable)
			if !ok || reason.Message != test.reason || reason.Row != 2 || reason.Col != 1 {
				t.Errorf("変換を断る理由か位置が違う: %#v", err)
			}
		})
	}
}
