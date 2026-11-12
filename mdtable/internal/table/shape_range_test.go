package table

import "testing"

func TestPipeRowShapeRangeErrors(t *testing.T) {
	r := FindTables(NewMarkdown("", "| a |\n| --- |\n| b |", false))[0]
	for _, at := range []int{-1, 3} {
		if _, _, err := InsertPipeRow(r, at); err == nil || err.Error() != "行の位置が表の範囲外" {
			t.Fatalf("行の追加の範囲外の理由が違う: %v", err)
		}
	}
	for _, at := range []int{-1, 2} {
		if _, _, err := DeletePipeRow(r, at); err == nil || err.Error() != "行の位置が表の範囲外" {
			t.Fatalf("行の削除の範囲外の理由が違う: %v", err)
		}
	}
	if _, _, err := InsertPipeRow(r, 0); err == nil || err.Error() != "見出しの上には行を追加できない" {
		t.Fatalf("見出しの上の追加の理由が変わった: %v", err)
	}
	if _, _, err := DeletePipeRow(r, 0); err == nil || err.Error() != "見出し行は削除できない" {
		t.Fatalf("見出しの削除の理由が変わった: %v", err)
	}
}
