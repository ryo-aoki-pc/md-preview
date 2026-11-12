package table

import "github.com/ryo-aoki-pc/md-table-excel/internal/model"

// Expectation は書き戻す前に組み立てる原文の期待値。
type Expectation struct {
	Cells     map[model.Position]string
	RowTags   []string
	Sections  []int
	Bodies    map[int]string
	Delimiter *string
}

// cellRaw は終了タグの省略されたセルも含め、セル全体の原文を返す。
func cellRaw(t *HTMLTable, c *HTMLCell) string {
	end := c.ContentEnd
	if c.EndToken != nil {
		end = c.EndToken.End
	}
	return t.Text[c.StartToken.Start:end]
}

// CellRaw は検査側がセルの原文を取り出す窓口。
func CellRaw(t *HTMLTable, c *HTMLCell) string { return cellRaw(t, c) }

// SourceExpectation は変更前の原文を独立した期待値にする。
func SourceExpectation(r *Region) *Expectation {
	e := &Expectation{Cells: map[model.Position]string{}}
	if r.Kind == "html" {
		e.RowTags = make([]string, len(r.HTML.Rows))
		for i, row := range r.HTML.Rows {
			if row.Token != nil {
				e.RowTags[i] = row.Token.Text
			}
		}
		e.Sections = []int{}
		for _, s := range r.HTML.Sections {
			e.Sections = append(e.Sections, len(s.Rows))
		}
		for _, c := range r.HTML.Cells {
			e.Cells[model.Position{Row: c.Row, Col: c.Col}] = cellRaw(r.HTML, c)
		}
	} else {
		e.Bodies = map[int]string{}
		for i, row := range append([]*PipeRow{r.Pipe.Header}, r.Pipe.Rows...) {
			e.Bodies[i] = row.Body
			for col := 0; col < r.Model().Cols; col++ {
				raw := ""
				if col < len(row.RawCells) {
					raw = row.RawCells[col]
				}
				e.Cells[model.Position{Row: i, Col: col}] = raw
			}
		}
		d := r.Pipe.DelimiterBody
		e.Delimiter = &d
	}
	return e
}
