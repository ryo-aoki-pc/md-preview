package table

import (
	"fmt"
	"github.com/ryo-aoki-pc/md-table-excel/internal/blocks"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
)

// copyShape はセルと穴を共有しないモデルの写しを作る。
func copyShape(m *model.TableModel) *model.TableModel {
	cells := make([]*model.Cell, 0, len(m.Cells))
	for _, c := range m.Cells {
		v := *c
		cells = append(cells, &v)
	}
	holes := map[model.Position]bool{}
	for p := range m.Holes {
		holes[p] = true
	}
	return model.New(m.Kind, m.Rows, m.Cols, cells, holes)
}

// shapeRow は原文の編集とは独立して、行を増減した後のモデルを作る。
func shapeRow(m *model.TableModel, at int, insert bool) *model.TableModel {
	n := copyShape(m)
	n.Cells = nil
	n.Holes = map[model.Position]bool{}
	for _, c := range m.Cells {
		v := *c
		if insert {
			if v.Row >= at {
				v.Row++
			} else if v.Row+v.Rowspan > at {
				v.Rowspan++
			}
		} else {
			if v.Row == at && v.Rowspan == 1 {
				continue
			}
			if v.Row <= at && at < v.Row+v.Rowspan {
				v.Rowspan--
			}
			if v.Row > at {
				v.Row--
			}
		}
		n.Cells = append(n.Cells, &v)
	}
	for p := range m.Holes {
		if insert {
			if p.Row >= at {
				p.Row++
			}
		} else {
			if p.Row == at {
				continue
			}
			if p.Row > at {
				p.Row--
			}
		}
		n.Holes[p] = true
	}
	if insert {
		n.Rows++
	} else {
		n.Rows--
	}
	return model.New(n.Kind, n.Rows, n.Cols, n.Cells, n.Holes)
}

func shapeColumn(m *model.TableModel, at int, insert bool) *model.TableModel {
	n := copyShape(m)
	n.Cells = nil
	n.Holes = map[model.Position]bool{}
	for _, c := range m.Cells {
		v := *c
		if insert {
			if v.Col >= at {
				v.Col++
			} else if v.Col+v.Colspan > at {
				v.Colspan++
			}
		} else {
			if v.Col == at && v.Colspan == 1 {
				continue
			}
			if v.Col <= at && at < v.Col+v.Colspan {
				v.Colspan--
			}
			if v.Col > at {
				v.Col--
			}
		}
		n.Cells = append(n.Cells, &v)
	}
	for p := range m.Holes {
		if insert {
			if p.Col >= at {
				p.Col++
			}
		} else {
			if p.Col == at {
				continue
			}
			if p.Col > at {
				p.Col--
			}
		}
		n.Holes[p] = true
	}
	if insert {
		n.Cols++
	} else {
		n.Cols--
	}
	return model.New(n.Kind, n.Rows, n.Cols, n.Cells, n.Holes)
}

// InsertPipeRow は見出しより下の位置 at に空の行を差し込む。
func InsertPipeRow(r *Region, at int) ([]string, *model.TableModel, error) {
	lines, n, _, err := InsertPipeRows(r, at, []string{formatRow(make([]string, r.Model().Cols), r.Pipe)})
	return lines, n, err
}

func DeletePipeRow(r *Region, row int) ([]string, *model.TableModel, error) {
	if row < 0 || row >= r.Model().Rows {
		return nil, nil, fmt.Errorf("行の位置が表の範囲外")
	}
	if row == 0 {
		return nil, nil, fmt.Errorf("見出し行は削除できない")
	}
	var lines []string
	for i, b := range r.Bodies {
		if i != row+1 {
			lines = append(lines, r.Prefixes[i]+b)
		}
	}
	return lines, shapeRow(r.Model(), row, false), nil
}

func pipeColumn(r *Region, at int, insert bool) ([]string, *model.TableModel, error) {
	m := r.Model()
	if at < 0 || at >= m.Cols && (!insert || at != m.Cols) {
		return nil, nil, fmt.Errorf("列の位置が表の範囲外")
	}
	if !insert && m.Cols == 1 {
		return nil, nil, fmt.Errorf("最後の 1 列は削除できない")
	}
	n := shapeColumn(m, at, insert)
	if insert {
		for row := 0; row < n.Rows; row++ {
			c := model.NewCell(row, at, "")
			c.Header = row == 0
			n.Cells = append(n.Cells, c)
		}
	}
	n = model.New(n.Kind, n.Rows, n.Cols, n.Cells, n.Holes)
	alter := func(raw []string) []string {
		cells := make([]string, m.Cols)
		copy(cells, raw)
		if insert {
			return append(append(append([]string{}, cells[:at]...), ""), cells[at:]...)
		}
		return append(cells[:at], cells[at+1:]...)
	}
	delimiter := pipeDelimiterCells(r)
	delimiter = alter(delimiter)
	if insert {
		delimiter[at] = "---"
	}
	lines, _ := rebuildPipeColumns(r, n, func(_ int, raw []string) []string { return alter(raw) }, delimiter)
	return lines, n, nil
}

func InsertPipeColumn(r *Region, at int) ([]string, *model.TableModel, error) {
	return pipeColumn(r, at, true)
}
func DeletePipeColumn(r *Region, col int) ([]string, *model.TableModel, error) {
	return pipeColumn(r, col, false)
}

// pipeDelimiterCells は区切り行の原文を取り出す。
func pipeDelimiterCells(r *Region) []string {
	cells := []string{}
	for _, c := range blocks.ParseTableRow(r.Pipe.DelimiterBody + "\n").Cells {
		cells = append(cells, blocks.StripASCIISpace(c.Raw))
	}
	return cells
}

// PipeDelimiterCells は区切り行の原文を取り出す。
func PipeDelimiterCells(r *Region) []string { return pipeDelimiterCells(r) }
