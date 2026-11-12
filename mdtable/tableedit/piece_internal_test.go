package tableedit

import (
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
)

// 同じ文書から写した HTML のセルは終了タグが省略されていることがある。
// パイプの表へ値だけを貼るときの知らせで、中身を取り出し損ねて止まらないこと。
func TestPieceNotesOmittedEndTag(t *testing.T) {
	for _, tc := range []struct {
		name, text, raw string
		complex         bool
	}{
		{"中身も終了タグも無い", "", "<td>", false},
		{"終了タグが無い", "foo", "<td>foo", false},
		{"終了タグがある", "foo", "<td>foo</td>", false},
		{"終了タグの無い複雑なセル", "foo", "<td>\n\n- a\n- b\n\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &table.Piece{Kind: "html", Rows: 1, Cols: 1, Cells: []*table.PieceCell{{Rowspan: 1, Colspan: 1, Text: tc.text, Raw: tc.raw}}}
			notes := pieceNotes(p, "pipe")
			got := strings.Contains(strings.Join(notes, "/"), "複雑なセル")
			if len(notes) == 0 || got != tc.complex {
				t.Fatalf("知らせが違う: %v", notes)
			}
		})
	}
}
