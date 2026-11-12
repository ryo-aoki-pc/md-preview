package tableedit

import (
	"fmt"

	"github.com/ryo-aoki-pc/md-table-excel/internal/cellsyntax"
	"github.com/ryo-aoki-pc/md-table-excel/internal/htmltok"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
)

// copyBounds は範囲を切る結合の左上を報告する。同じ表の操作に JSON の検査は挟まない。
func copyBounds(r *table.Region, row, col, rows, cols int) error {
	m := r.Model()
	if row < 0 || col < 0 || rows < 1 || cols < 1 || row >= m.Rows || col >= m.Cols || rows > m.Rows-row || cols > m.Cols-col {
		return fault("range", "行・列の範囲が表の外にはみ出す")
	}
	for _, c := range m.Cells {
		if c.Row >= row+rows || c.Col >= col+cols || c.Row+c.Rowspan <= row || c.Col+c.Colspan <= col {
			continue
		}
		if c.Row < row || c.Col < col || c.Row+c.Rowspan > row+rows || c.Col+c.Colspan > col+cols {
			return located(fault("merged", "写す範囲で結合セルが切れるので、範囲を選び直してください"), c.Row, c.Col)
		}
	}
	return nil
}

func (d *Document) duplicate(t Target, index, count int, rows bool) (*Result, error) {
	r, err := d.target(t)
	if err != nil {
		return nil, err
	}
	m := r.Model()
	row, col, height, width := 0, index, m.Rows, count
	if rows {
		row, col, height, width = index, 0, count, m.Cols
	}
	if err = copyBounds(r, row, col, height, width); err != nil {
		return nil, err
	}
	rowLock, colLock := table.ShapeLocks(r)
	lock := colLock
	if rows {
		lock = rowLock
	}
	if lock != "" {
		return nil, fault("unsupported", lock)
	}
	var lines []string
	var expected *model.TableModel
	var expect *table.Expectation
	notes := []string{}
	if rows {
		if r.Kind == "html" {
			lines, expected, expect, err = table.DuplicateHTMLRows(r, index, count)
		} else {
			lines, expected, expect, err = table.DuplicatePipeRows(r, index, count)
			if index == 0 {
				notes = append(notes, "見出し行の写しは、見出しの下のデータ行にした")
			}
		}
	} else if r.Kind == "html" {
		lines, expected, expect, err = table.DuplicateHTMLColumns(r, index, count)
	} else {
		lines, expected, expect, err = table.DuplicatePipeColumns(r, index, count)
		notes = append(notes, droppedCells(r.Warnings)...)
	}
	if err != nil {
		return nil, renderError(r, err)
	}
	return d.applyEdits([]regionEdit{{region: r, lines: lines, expected: expected, expect: expect}}, notes)
}

// DuplicateRows は範囲の直後へ一組の行を原文のまま複製する。
func (d *Document) DuplicateRows(t Target, row, count int) (result *Result, err error) {
	defer catch(&result, &err)
	return d.duplicate(t, row, count, true)
}

// DuplicateColumns は範囲の直後へ一組の列を原文のまま複製する。
func (d *Document) DuplicateColumns(t Target, col, count int) (result *Result, err error) {
	defer catch(&result, &err)
	return d.duplicate(t, col, count, false)
}

func pieceMerged(p *table.Piece) bool {
	for _, c := range p.Cells {
		if c.Rowspan != 1 || c.Colspan != 1 {
			return true
		}
	}
	return false
}

func pieceNotes(p *table.Piece, kind string) []string {
	notes := []string{}
	if p.Kind != "" && p.Kind != kind {
		notes = append(notes, "種類の違う表からの写しなので、値だけを貼った（属性・結合・原文の書き方は引き継がない）")
	}
	if p.Kind == "html" {
		for _, c := range p.Cells {
			if c.Links && kind == "html" {
				notes = append(notes, fmt.Sprintf("写したセル（%d 行 %d 列）にリンク参照の定義がある。同じ名前の定義は文書の最初のものが使われる", c.Row+1, c.Col+1))
			}
			if kind != "html" {
				// 同じ文書から写したセルは終了タグが省略されていることがある。中身は開始タグの後ろから、
				// 最後の字句が同じ名前の終了タグならその前まで、無ければ原文の終わりまでとする。
				tokens := htmltok.Tokenize(c.Raw, 0, len(c.Raw))
				source := ""
				if len(tokens) > 0 && tokens[0].Kind == "start" {
					end, last := len(c.Raw), tokens[len(tokens)-1]
					if len(tokens) > 1 && last.Kind == "end" && last.Name == tokens[0].Name {
						end = last.Start
					}
					source = c.Raw[tokens[0].End:end]
				}
				if complexCell(c.Text, source, c.Links) {
					notes = append(notes, fmt.Sprintf("写した複雑なセル（%d 行 %d 列）は、値だけを貼った", c.Row+1, c.Col+1))
				}
			}
		}
	}
	return notes
}

func convertPiece(r *table.Region, p *table.Piece, base int, rows bool) (*table.Piece, []string, error) {
	notes := pieceNotes(p, r.Kind)
	if p.Kind == r.Kind {
		return p, notes, nil
	}
	if r.Kind == "pipe" && pieceMerged(p) {
		return nil, nil, fault("unsupported", "パイプの表に結合は書けない")
	}
	n := &table.Piece{Kind: r.Kind, Rows: p.Rows, Cols: p.Cols, Cells: []*table.PieceCell{}}
	for _, c := range p.Cells {
		v := *c
		if r.Kind == "pipe" {
			raw, e := cellsyntax.ToPipeCell(c.Text)
			if e != nil {
				return nil, nil, renderError(r, &cellsyntax.NotRepresentable{Message: e.Error(), Row: c.Row + 1, Col: c.Col + 1})
			}
			v.Raw = raw
			v.Header = false
			v.Align = ""
		} else {
			header := false
			align := ""
			if rows {
				if old := r.Model().CellAt(base, c.Col); old != nil {
					header, align = old.Header, old.Align
				}
			} else if c.Row < len(r.HTML.Rows) {
				baseRow := r.HTML.Rows[c.Row]
				header = len(baseRow.Cells) > 0
				for _, old := range baseRow.Cells {
					header = header && old.Tag == "th"
				}
			}
			cell, more, e := table.HTMLValueCell(c, header, align)
			if e != nil {
				return nil, nil, renderError(r, e)
			}
			notes = append(notes, more...)
			v = *cell
		}
		n.Cells = append(n.Cells, &v)
	}
	return n, notes, nil
}

func (d *Document) insertFrom(t Target, index int, side Side, f *Fragment, rows bool) (*Result, error) {
	r, err := d.target(t)
	if err != nil {
		return nil, err
	}
	_, p, err := validateFragment(f)
	if err != nil {
		return nil, err
	}
	if side != Before && side != After {
		return nil, fault("invalid", "追加する側の指定が正しくない")
	}
	size := r.Model().Cols
	if rows {
		size = r.Model().Rows
	}
	if index < 0 || index >= size {
		return nil, fault("range", "行・列の位置が表の範囲外")
	}
	if rows && p.Cols > r.Model().Cols || !rows && p.Rows > r.Model().Rows {
		return nil, fault("range", "写しの幅か高さが表の外にはみ出す")
	}
	rowLock, colLock := table.ShapeLocks(r)
	lock := colLock
	if rows {
		lock = rowLock
	}
	if lock != "" {
		return nil, fault("unsupported", lock)
	}
	at := index
	if side == After {
		at++
	}
	if rows && r.Kind == "pipe" && at == 0 {
		return nil, fault("unsupported", "見出しの上には行を追加できない")
	}
	for _, c := range r.Model().Cells {
		if rows && c.Row < at && at < c.Row+c.Rowspan || !rows && c.Col < at && at < c.Col+c.Colspan {
			return nil, located(fault("merged", "挿入する位置で結合セルが切れる"), c.Row, c.Col)
		}
	}
	p, notes, err := convertPiece(r, p, index, rows)
	if err != nil {
		return nil, err
	}
	var lines []string
	var expected *model.TableModel
	var expect *table.Expectation
	if rows {
		if r.Kind == "html" {
			lines, expected, expect, err = table.InsertHTMLRows(r, index, side == After, p)
		} else {
			lines, expected, expect, err = table.InsertPipePieceRows(r, at, p)
		}
	} else if r.Kind == "html" {
		lines, expected, expect, err = table.InsertHTMLColumns(r, at, p)
	} else {
		lines, expected, expect, err = table.InsertPipeColumns(r, at, p)
		notes = append(notes, droppedCells(r.Warnings)...)
	}
	if err != nil {
		return nil, renderError(r, err)
	}
	return d.applyEdits([]regionEdit{{region: r, lines: lines, expected: expected, expect: expect}}, notes)
}

// InsertRowsFrom は写しを列 0 から配置し、足りない幅を空のセルで補う。
func (d *Document) InsertRowsFrom(t Target, row int, side Side, f *Fragment) (result *Result, err error) {
	defer catch(&result, &err)
	return d.insertFrom(t, row, side, f, true)
}

// InsertColumnsFrom は写しを行 0 から配置し、足りない高さを空のセルで補う。
func (d *Document) InsertColumnsFrom(t Target, col int, side Side, f *Fragment) (result *Result, err error) {
	defer catch(&result, &err)
	return d.insertFrom(t, col, side, f, false)
}

func samePieceShape(r *table.Region, row, col int, p *table.Piece) bool {
	old, e := table.CopyPiece(r, row, col, p.Rows, p.Cols, false)
	if e != nil || len(old.Cells) != len(p.Cells) {
		return false
	}
	for i, c := range old.Cells {
		n := p.Cells[i]
		if c.Row != n.Row || c.Col != n.Col || c.Rowspan != n.Rowspan || c.Colspan != n.Colspan {
			return false
		}
	}
	return true
}

// PasteFragment は表の種類を保ち、同じ種類なら原文ごと上書きする。
func (d *Document) PasteFragment(t Target, row, col int, f *Fragment) (result *Result, err error) {
	defer catch(&result, &err)
	r, err := d.target(t)
	if err != nil {
		return nil, err
	}
	_, p, err := validateFragment(f)
	if err != nil {
		return nil, err
	}
	m := r.Model()
	if row < 0 || col < 0 || row >= m.Rows || col >= m.Cols || p.Rows > m.Rows-row || p.Cols > m.Cols-col {
		return nil, fault("range", "貼り付けの範囲が表の外にはみ出す")
	}
	if r.Kind == "pipe" && pieceMerged(p) {
		return nil, fault("unsupported", "パイプの表に結合は書けない")
	}
	if p.Kind != r.Kind {
		if pieceMerged(p) && !samePieceShape(r, row, col, p) {
			return nil, fault("merged", "貼り付け先の結合の形が写しと違う")
		}
		notes := pieceNotes(p, r.Kind)
		edits := []CellEdit{}
		grid := map[model.Position]bool{}
		for _, c := range p.Cells {
			edits = append(edits, CellEdit{Row: row + c.Row, Col: col + c.Col, Text: c.Text})
			for i := c.Row; i < c.Row+c.Rowspan; i++ {
				for j := c.Col; j < c.Col+c.Colspan; j++ {
					grid[model.Position{Row: i, Col: j}] = true
				}
			}
		}
		for i := 0; i < p.Rows; i++ {
			for j := 0; j < p.Cols; j++ {
				// 写しの穴は空のセルとして貼る。貼り付け先も穴なら、書くセルが無いので飛ばす。
				if !grid[model.Position{Row: i, Col: j}] && !m.Holes[model.Position{Row: row + i, Col: col + j}] {
					edits = append(edits, CellEdit{Row: row + i, Col: col + j, Text: ""})
				}
			}
		}
		return d.setCellsChecked(r, edits, true, notes)
	}
	if !samePieceShape(r, row, col, p) {
		return nil, fault("merged", "貼り付け先の結合の形が写しと違う")
	}
	var lines []string
	var expected *model.TableModel
	var expect *table.Expectation
	if r.Kind == "html" {
		lines, expected, expect, err = table.ReplaceHTMLCells(r, row, col, p, nil)
	} else {
		lines, expected, expect, err = table.ReplacePipeCells(r, row, col, p, nil)
	}
	if err != nil {
		return nil, renderError(r, err)
	}
	return d.applyEdits([]regionEdit{{region: r, lines: lines, expected: expected, expect: expect}}, pieceNotes(p, r.Kind))
}
