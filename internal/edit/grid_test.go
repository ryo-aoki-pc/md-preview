package edit

import (
	"context"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/mdtable/tableedit"
)

func TestDOMGridSectionsAndSpans(t *testing.T) {
	for _, c := range []struct {
		name, html string
		model      tableedit.Table
		want       bool
	}{
		{"区画までの結合", `<table><tbody><tr><th rowspan="0">A</th><td>B</td></tr><tr><td>C</td></tr></tbody><tfoot><tr><td>D</td><td>E</td></tr></tfoot></table>`, tableedit.Table{Rows: 3, Cols: 2, Cells: []tableedit.Cell{{Row: 0, Col: 0, Rowspan: 2, Colspan: 1, Header: true}, {Row: 0, Col: 1, Rowspan: 1, Colspan: 1}, {Row: 1, Col: 1, Rowspan: 1, Colspan: 1}, {Row: 2, Col: 0, Rowspan: 1, Colspan: 1}, {Row: 2, Col: 1, Rowspan: 1, Colspan: 1}}}, true},
		{"列の上限", `<table><tr><td colspan="1001">A</td></tr></table>`, tableedit.Table{Rows: 1, Cols: 1000, Cells: []tableedit.Cell{{Row: 0, Col: 0, Rowspan: 1, Colspan: 1000}}}, true},
		{"不正な結合", `<table><tr><td rowspan="-1" colspan="0">A</td></tr></table>`, tableedit.Table{Rows: 1, Cols: 1, Cells: []tableedit.Cell{{Row: 0, Col: 0, Rowspan: 1, Colspan: 1}}}, true},
		{"省略した tr", `<table><td>A</td><td>B</td></table>`, tableedit.Table{Rows: 0, Cols: 0}, false},
		{"入れ子のセルを数えない", `<table><tr><td><table><tr><td>内側</td></tr></table></td></tr></table>`, tableedit.Table{Rows: 1, Cols: 1, Cells: []tableedit.Cell{{Row: 0, Col: 0, Rowspan: 1, Colspan: 1}}}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			root, err := htmltree.Fragment(context.Background(), c.html)
			if err != nil {
				t.Fatal(err)
			}
			nodes, err := tableNodes(context.Background(), root)
			if err != nil || len(nodes) == 0 {
				t.Fatal(err)
			}
			if got := sameGrid(nodes[0], &c.model); got != c.want {
				t.Fatal(got, c.want)
			}
		})
	}
}
