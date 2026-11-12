package table

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
)

func TestReadPieceBoundaries(t *testing.T) {
	for _, tc := range []struct{ source, text string }{{"**a** \\| b", "**a** | b"}, {"z<br>w", "z\nw"}, {"", ""}} {
		got, err := ReadPipeSource(tc.source)
		if err != nil || got != tc.text {
			t.Fatalf("パイプの原文 %q: %q、%v", tc.source, got, err)
		}
	}
	for _, raw := range []string{"a|b", "a\\", "a\nb", " a", "<td>"} {
		if _, err := ReadPipeSource(raw); err == nil {
			t.Fatalf("不正な原文を受け取った: %q", raw)
		}
	}
	cell := &PieceCell{Rowspan: 1, Colspan: 2, Raw: "<TH colspan='2' class=x><b>a</b></TH>"}
	p, err := ReadHTMLPiece(1, 2, []*PieceCell{cell}, []string{"<tr class='r'>"})
	if err != nil || p.Cells[0].Raw != cell.Raw || !p.Cells[0].Header || p.Cells[0].Text != "**a**" {
		t.Fatalf("HTML の写し: %+v、%v", p, err)
	}
	for _, raw := range []string{"<td>a</td><td>b</td>", "<td>a</td></tr><tr><td>b</td>", "<td>a</td></table>", "<td>a<caption>x</caption></td>"} {
		_, err := ReadHTMLPiece(1, 1, []*PieceCell{{Rowspan: 1, Colspan: 1, Raw: raw}}, nil)
		if err == nil {
			t.Fatalf("境界を越す原文を受け取った: %q", raw)
		}
	}
	_, err = ReadHTMLPiece(1, 2, []*PieceCell{{Col: 1, Rowspan: 1, Colspan: 1, Raw: "<td>x</td>"}}, nil)
	if err == nil {
		t.Fatal("読み取りの置き方と違う穴を許した")
	}
}

func TestHTMLRowLayout(t *testing.T) {
	for _, tc := range []struct{ text, open, between, close string }{
		{"<table><tr>\n  <td>a</td>\n  <td>b</td>\n</tr></table>", "\n  ", "\n  ", "\n"},
		{"<table><tr> <td>a</td>\t<td>b</td> </tr></table>", " ", "\t", " "},
		{"<table><tr> <td>a</td> </tr><tr></tr></table>", " ", " ", " "},
	} {
		r := FindTables(NewMarkdown("", tc.text, false))[0]
		for _, row := range r.HTML.Rows {
			a, b, c := htmlRowLayout(r.HTML, row)
			if a != tc.open || b != tc.between || c != tc.close {
				t.Fatalf("行の字下げ: %q,%q,%q", a, b, c)
			}
		}
	}
}

func TestPatchPipeRawBytes(t *testing.T) {
	input := "| a   |\tb  | c |\n| --- | :----: | --- |\n| d |\n| e | f | g | 余分 |"
	r := FindTables(NewMarkdown("", input, false))[0]
	lines, err := PatchPipeRaw(r, map[model.Position]string{{Row: 0, Col: 1}: "__x__", {Row: 1, Col: 2}: "y\\|z", {Row: 2, Col: 0}: "q"})
	want := "| a   |\t__x__  | c |\n| --- | :----: | --- |\n| d |  | y\\|z |\n| q | f | g | 余分 |"
	if err != nil || strings.Join(lines, "\n") != want {
		t.Fatalf("原文の差し替え: %q、%v", strings.Join(lines, "\n"), err)
	}
	n := copyShape(r.Model())
	n.CellAt(0, 1).Text = "__x__"
	patched, err := PatchPipeCells(r, n, &[]string{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := PatchPipeRaw(r, map[model.Position]string{{Row: 0, Col: 1}: "__x__"})
	if err != nil || !reflect.DeepEqual(patched, raw) {
		t.Fatalf("既存の差し替えと違う: %v", err)
	}
}

func TestInsertPipeRowsRange(t *testing.T) {
	r := FindTables(NewMarkdown("", "| a |\n| --- |\n", false))[0]
	for _, tc := range []struct {
		at     int
		reason string
	}{{-1, "行の位置が表の範囲外"}, {2, "行の位置が表の範囲外"}, {0, "見出しの上には行を追加できない"}} {
		_, _, _, err := InsertPipeRows(r, tc.at, []string{"| x |"})
		if err == nil || err.Error() != tc.reason {
			t.Fatalf("位置 %d: %v", tc.at, err)
		}
	}
}
