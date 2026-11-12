package tableedit

import (
	"errors"
	"testing"

	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
)

// 原文の対応は並びに依存せず、対応が欠けた表だけを対象外にする。
func TestHTMLCellLocationMapping(t *testing.T) {
	d := Parse("<table><tr><td>a</td><td>b</td></tr></table>\n\n<table><tr><td>c</td></tr></table>")
	html := d.regions[0].HTML
	html.Cells[0], html.Cells[1] = html.Cells[1], html.Cells[0]
	if tab := d.Table(1); tab == nil || tab.Cells[0].Source != "a" || tab.Cells[1].Source != "b" {
		t.Fatal("セルの原文が配列の並びに依存している")
	}
	delete(html.CellAt, model.Position{Row: 0, Col: 0})
	tab := d.Table(1)
	if tab == nil || tab.Excluded == "" || tab.Tag != nil || len(tab.Cells) != 0 || tab.RowsLocked != tab.Excluded || tab.ColsLocked != tab.Excluded {
		t.Fatalf("原文の対応が欠けた表を対象外として公開していない: %+v", tab)
	}
	for _, operation := range []func() (*Result, error){
		func() (*Result, error) {
			return d.SetCells(Target{Number: 1}, []CellEdit{{Row: 0, Col: 1, Text: "新"}})
		},
		func() (*Result, error) { return d.Paste(Target{Number: 1}, 0, 1, [][]string{{"新"}}) },
		func() (*Result, error) { return d.InsertRows(Target{Number: 1}, 0, After, 1) },
		func() (*Result, error) { return d.DeleteRows(Target{Number: 1}, 0, 1) },
		func() (*Result, error) { return d.InsertColumns(Target{Number: 1}, 0, After, 1) },
		func() (*Result, error) { return d.DeleteColumns(Target{Number: 1}, 0, 1) },
		func() (*Result, error) { return d.SetCellSource(Target{Number: 1}, 0, 1, "新") },
	} {
		result, err := operation()
		var failure *Error
		if result != nil || !errors.As(err, &failure) || failure.Code != "excluded" || failure.Message != tab.Excluded {
			t.Fatalf("原文の対応が無い表を編集できた: %+v %v", result, err)
		}
	}
	if d.problem != nil || d.Table(2).Excluded != "" {
		t.Fatal("対応がある別の表まで編集できなくなった")
	}
	if result, err := d.SetCellSource(Target{Number: 2}, 0, 0, "新"); err != nil || !result.Changed {
		t.Fatalf("別の表を編集できない: %v", err)
	}
}
