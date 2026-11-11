package sync

import (
	"github.com/ryo-aoki-pc/md-table-excel/internal/cellsyntax"
	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
	"testing"
)

// sync._check_conversion_cells の理由とセルの位置を確かめる。
func TestConversionChecks(t *testing.T) {
	for _, test := range []struct{ source, target, reason string }{
		{"| h |\n|---|\n| <br>text |\n", "html", "セルの先頭の <br> は、HTML の表にすると消える"},
		{"| h |\n|---|\n| a<br><br><br>b |\n", "html", "3 つ以上続く <br> は、HTML の表では段落の区切り 1 つになる"},
		{"| h |\n|---|\n| <div>hello<br>world</div> |\n", "html", "セルに HTML のブロック（<div>）があり、種類を変えると描画が変わる"},
		{"<table>\n<tr><th>h</th></tr>\n<tr><td>\n\n[a]: /url\n\n</td></tr>\n</table>\n", "pipe", "セルの中にリンク参照の定義がある"},
	} {
		regions := table.FindTables(table.NewMarkdown("test.md", test.source, false))
		if len(regions) != 1 || regions[0].Excluded != "" {
			t.Fatalf("見本を表として読めない: %q", test.source)
		}
		err := checkConversionCells(regions[0], test.target)
		reason, ok := err.(*cellsyntax.NotRepresentable)
		if !ok || reason.Message != test.reason || reason.Row != 2 || reason.Col != 1 {
			t.Errorf("変換を断る理由か位置が違う: %#v", err)
		}
	}
}
