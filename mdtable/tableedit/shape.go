package tableedit

import (
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
)

func (d *Document) shape(t Target, index int, side Side, count int, rows, insert bool) (*Result, error) {
	r, err := d.target(t)
	if err != nil {
		return nil, err
	}
	if count < 1 {
		return nil, fault("range", "行・列の個数は 1 以上で指定してください")
	}
	if insert && side != Before && side != After {
		return nil, fault("invalid", "追加する側の指定が正しくない")
	}
	m := r.Model()
	size := m.Cols
	if rows {
		size = m.Rows
	}
	if index < 0 || index >= size || !insert && count > size-index {
		return nil, fault("range", "行・列の範囲が表の外にはみ出す")
	}
	if insert && count > int(^uint(0)>>1)-size {
		return nil, fault("range", "追加する行・列の個数が多すぎる")
	}
	rowLock, colLock := table.ShapeLocks(r)
	lock := colLock
	if rows {
		lock = rowLock
	}
	if lock != "" {
		return nil, fault("unsupported", lock)
	}
	if rows && r.Kind == "pipe" && index == 0 && (!insert || side == Before) {
		return nil, fault("unsupported", "見出し行の削除と、見出しの上への追加はできない")
	}
	if !insert && count == size {
		if rows {
			return nil, fault("unsupported", "最後の 1 行は削除できない")
		}
		return nil, fault("unsupported", "最後の 1 列は削除できない")
	}
	// 複製した結合を含む範囲は一度に消し、途中の穴へのセルの移動を避ける。
	if rows && !insert && r.Kind == "html" && count > 1 && copyBounds(r, index, 0, count, m.Cols) == nil {
		lines, expected, err := table.DeleteHTMLRowBlock(r, index, count)
		if err != nil {
			return nil, fault("unsupported", err.Error())
		}
		return d.apply(r, lines, expected, true, false, nil)
	}
	current := d
	var result *Result
	// 列の操作では全行を組み直すので、見出しより多いセルは最初の 1 個目で消える。
	notes := []string{}
	if !rows && r.Kind == "pipe" {
		notes = droppedCells(r.Warnings)
	}
	for i := 0; i < count; i++ {
		at := index
		if insert {
			at += i
		}
		r, err = current.target(Target{Number: t.Number})
		if err != nil {
			return nil, err
		}
		var lines []string
		var expected *model.TableModel
		if rows {
			if insert {
				if r.Kind == "pipe" {
					pos := at
					if side == After {
						pos++
					}
					lines, expected, err = table.InsertPipeRow(r, pos)
				} else {
					lines, expected, err = table.InsertHTMLRow(r, at, side == After)
				}
			} else if r.Kind == "pipe" {
				lines, expected, err = table.DeletePipeRow(r, at)
			} else {
				lines, expected, err = table.DeleteHTMLRow(r, at)
			}
		} else {
			if insert {
				if side == After {
					at++
				}
				if r.Kind == "pipe" {
					lines, expected, err = table.InsertPipeColumn(r, at)
				} else {
					lines, expected, err = table.InsertHTMLColumn(r, at)
				}
			} else if r.Kind == "pipe" {
				lines, expected, err = table.DeletePipeColumn(r, at)
			} else {
				lines, expected, err = table.DeleteHTMLColumn(r, at)
			}
		}
		if err != nil {
			return nil, fault("unsupported", err.Error())
		}
		result, err = current.apply(r, lines, expected, true, false, notes)
		if err != nil {
			return nil, err
		}
		current = result.Document
	}
	result.Changed = result.Text != d.text
	return result, nil
}

func (d *Document) InsertRows(t Target, row int, side Side, count int) (result *Result, err error) {
	defer catch(&result, &err)
	return d.shape(t, row, side, count, true, true)
}
func (d *Document) DeleteRows(t Target, row, count int) (result *Result, err error) {
	defer catch(&result, &err)
	return d.shape(t, row, Before, count, true, false)
}
func (d *Document) InsertColumns(t Target, col int, side Side, count int) (result *Result, err error) {
	defer catch(&result, &err)
	return d.shape(t, col, side, count, false, true)
}
func (d *Document) DeleteColumns(t Target, col, count int) (result *Result, err error) {
	defer catch(&result, &err)
	return d.shape(t, col, Before, count, false, false)
}
