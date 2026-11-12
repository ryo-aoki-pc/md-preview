package tableedit

import (
	"github.com/ryo-aoki-pc/md-table-excel/internal/cellsyntax"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
)

func (d *Document) moveTargets(from, to Target) (*table.Region, *table.Region, error) {
	a, err := d.target(from)
	if err != nil {
		return nil, nil, err
	}
	b, err := d.target(to)
	if err != nil {
		return nil, nil, err
	}
	if a != b && a.Start <= b.End && b.Start <= a.End {
		return nil, nil, fault("unsupported", "入れ子の表の間では移動できない")
	}
	return a, b, nil
}

func moveLock(r *table.Region, rows bool) error {
	row, col := table.ShapeLocks(r)
	lock := col
	if rows {
		lock = row
	}
	if lock != "" {
		return fault("unsupported", lock)
	}
	return nil
}

func insertionBounds(r *table.Region, gap int, rows bool) error {
	for _, c := range r.Model().Cells {
		if rows && c.Row < gap && gap < c.Row+c.Rowspan || !rows && c.Col < gap && gap < c.Col+c.Colspan {
			return located(fault("merged", "移動先の位置で結合セルが切れる"), c.Row, c.Col)
		}
	}
	return nil
}

func (d *Document) moveShape(from Target, first, count int, to Target, at int, side Side, rows bool) (*Result, error) {
	a, b, err := d.moveTargets(from, to)
	if err != nil {
		return nil, err
	}
	if side != Before && side != After {
		return nil, fault("invalid", "移動する側の指定が正しくない")
	}
	row, col, height, width := 0, first, a.Model().Rows, count
	size, destSize := a.Model().Cols, b.Model().Cols
	if rows {
		row, col, height, width = first, 0, count, a.Model().Cols
		size, destSize = a.Model().Rows, b.Model().Rows
	}
	if err = copyBounds(a, row, col, height, width); err != nil {
		return nil, err
	}
	if at < 0 || at >= destSize {
		return nil, fault("range", "移動先の行・列が表の範囲外")
	}
	if err = moveLock(a, rows); err != nil {
		return nil, err
	}
	if err = moveLock(b, rows); err != nil {
		return nil, err
	}
	gap := at
	if side == After {
		gap++
	}
	if rows && a.Kind == "pipe" && first == 0 {
		return nil, fault("unsupported", "見出し行は移動できない")
	}
	if rows && b.Kind == "pipe" && gap == 0 {
		return nil, fault("unsupported", "見出しの上には行を移動できない")
	}
	if a == b {
		if gap == first || gap == first+count {
			return d.applyEdits([]regionEdit{{region: a, lines: append([]string{}, d.md.Lines[a.Start:a.End+1]...), expected: clone(a.Model()), expect: table.SourceExpectation(a)}}, nil)
		}
		if first < gap && gap < first+count {
			return nil, fault("range", "移動先が動かす範囲の中にある")
		}
	}
	if err = insertionBounds(b, gap, rows); err != nil {
		return nil, err
	}
	var lines []string
	var expected *model.TableModel
	var expect *table.Expectation
	notes := []string{}
	if a == b {
		if rows && a.Kind == "html" {
			lines, expected, expect, err = table.MoveHTMLRows(a, first, count, at, side == After)
		} else if rows {
			lines, expected, expect, err = table.MovePipeRows(a, first, count, gap)
		} else if a.Kind == "html" {
			lines, expected, expect, err = table.MoveHTMLColumns(a, first, count, gap)
		} else {
			lines, expected, expect, err = table.MovePipeColumns(a, first, count, gap)
			notes = droppedCells(a.Warnings)
		}
		if err != nil {
			return nil, renderError(a, err)
		}
		return d.applyEdits([]regionEdit{{region: a, lines: lines, expected: expected, expect: expect}}, notes)
	}
	if count == size {
		if rows {
			return nil, fault("unsupported", "最後の 1 行は移動できない")
		}
		return nil, fault("unsupported", "最後の 1 列は移動できない")
	}
	if rows && width > b.Model().Cols || !rows && height > b.Model().Rows {
		return nil, fault("range", "動かす範囲の幅か高さが移動先の表の外にはみ出す")
	}
	p, err := table.CopyPiece(a, row, col, height, width, false)
	if err != nil {
		return nil, renderError(a, err)
	}
	p, notes, err = convertPiece(b, p, at, rows)
	if err != nil {
		return nil, err
	}
	if rows && b.Kind == "html" {
		lines, expected, expect, err = table.InsertHTMLRows(b, at, side == After, p)
	} else if rows {
		lines, expected, expect, err = table.InsertPipePieceRows(b, gap, p)
	} else if b.Kind == "html" {
		lines, expected, expect, err = table.InsertHTMLColumns(b, gap, p)
	} else {
		lines, expected, expect, err = table.InsertPipeColumns(b, gap, p)
	}
	if err != nil {
		return nil, renderError(b, err)
	}
	dest := regionEdit{region: b, lines: lines, expected: expected, expect: expect}
	lines, expected, expect, err = table.DeletePieceBlock(a, first, count, rows)
	if err != nil {
		return nil, renderError(a, err)
	}
	if !rows {
		if a.Kind == "pipe" {
			notes = append(notes, droppedCells(a.Warnings)...)
		}
		if b.Kind == "pipe" {
			notes = append(notes, droppedCells(b.Warnings)...)
		}
	}
	return d.applyEdits([]regionEdit{{region: a, lines: lines, expected: expected, expect: expect}, dest}, notes)
}

// MoveRows は元の座標の基準行の前後へ行の範囲を一度で移す。
func (d *Document) MoveRows(from Target, row, count int, to Target, at int, side Side) (result *Result, err error) {
	defer catch(&result, &err)
	return d.moveShape(from, row, count, to, at, side, true)
}

// MoveColumns は元の座標の基準列の前後へ列の範囲を一度で移す。
func (d *Document) MoveColumns(from Target, col, count int, to Target, at int, side Side) (result *Result, err error) {
	defer catch(&result, &err)
	return d.moveShape(from, col, count, to, at, side, false)
}

func replacePiece(r *table.Region, row, col int, p *table.Piece, clear []model.Position) (regionEdit, error) {
	var lines []string
	var m *model.TableModel
	var expect *table.Expectation
	var err error
	if r.Kind == "html" {
		lines, m, expect, err = table.ReplaceHTMLCells(r, row, col, p, clear)
	} else {
		lines, m, expect, err = table.ReplacePipeCells(r, row, col, p, clear)
	}
	if err != nil {
		return regionEdit{}, renderError(r, err)
	}
	return regionEdit{region: r, lines: lines, expected: m, expect: expect}, nil
}

// pastePieceValues は PasteFragment と同じ値の上書きを、適用前の置換として用意する。
func pastePieceValues(r *table.Region, row, col int, p *table.Piece) (regionEdit, []string, error) {
	m := clone(r.Model())
	covered := map[model.Position]bool{}
	for _, c := range p.Cells {
		for i := c.Row; i < c.Row+c.Rowspan; i++ {
			for j := c.Col; j < c.Col+c.Colspan; j++ {
				covered[model.Position{Row: i, Col: j}] = true
			}
		}
	}
	set := func(i, j int, text string) error {
		c := m.CellAt(row+i, col+j)
		if c == nil {
			return located(fault("range", "指定した位置にセルが無い"), row+i, col+j)
		}
		if c.Row != row+i || c.Col != col+j {
			return located(fault("merged", "結合セルは左上の位置で指定してください"), row+i, col+j)
		}
		c.Text = cellsyntax.NormalizeListSpacing(model.NormalizeText(text))
		return nil
	}
	for _, c := range p.Cells {
		if err := set(c.Row, c.Col, c.Text); err != nil {
			return regionEdit{}, nil, err
		}
	}
	for i := 0; i < p.Rows; i++ {
		for j := 0; j < p.Cols; j++ {
			if !covered[model.Position{Row: i, Col: j}] {
				if err := set(i, j, ""); err != nil {
					return regionEdit{}, nil, err
				}
			}
		}
	}
	notes := pieceNotes(p, r.Kind)
	var lines []string
	var expect *table.Expectation
	var err error
	if r.Kind == "html" {
		lines, expect, err = table.PatchHTMLValues(r, m, &notes)
	} else {
		lines, expect, err = table.PatchPipeValues(r, m, &notes)
	}
	if err != nil {
		return regionEdit{}, nil, renderError(r, err)
	}
	return regionEdit{region: r, lines: lines, expected: m, expect: expect}, notes, nil
}

// MoveCells は移動先を上書きし、元のうち移動先に重ならないセルの中身を空ける。
func (d *Document) MoveCells(from Target, rect Rect, to Target, row, col int) (result *Result, err error) {
	defer catch(&result, &err)
	a, b, err := d.moveTargets(from, to)
	if err != nil {
		return nil, err
	}
	if err = copyBounds(a, rect.Row, rect.Col, rect.Rows, rect.Cols); err != nil {
		return nil, err
	}
	if row < 0 || col < 0 || row >= b.Model().Rows || col >= b.Model().Cols || rect.Rows > b.Model().Rows-row || rect.Cols > b.Model().Cols-col {
		return nil, fault("range", "移動先の範囲が表の外にはみ出す")
	}
	p, err := table.CopyPiece(a, rect.Row, rect.Col, rect.Rows, rect.Cols, false)
	if err != nil {
		return nil, renderError(a, err)
	}
	if b.Kind == "pipe" && pieceMerged(p) {
		return nil, fault("unsupported", "パイプの表に結合は書けない")
	}
	if (a.Kind == b.Kind || pieceMerged(p)) && !samePieceShape(b, row, col, p) {
		return nil, fault("merged", "貼り付け先の結合の形が写しと違う")
	}
	clear := []model.Position{}
	for _, c := range p.Cells {
		i, j := rect.Row+c.Row, rect.Col+c.Col
		if a != b || i < row || i >= row+p.Rows || j < col || j >= col+p.Cols {
			clear = append(clear, model.Position{Row: i, Col: j})
		}
	}
	if a == b {
		edit, err := replacePiece(a, row, col, p, clear)
		if err != nil {
			return nil, err
		}
		return d.applyEdits([]regionEdit{edit}, pieceNotes(p, b.Kind))
	}
	var dest regionEdit
	notes := []string{}
	if a.Kind == b.Kind {
		dest, err = replacePiece(b, row, col, p, nil)
		notes = pieceNotes(p, b.Kind)
	} else {
		dest, notes, err = pastePieceValues(b, row, col, p)
	}
	if err != nil {
		return nil, err
	}
	source, err := replacePiece(a, 0, 0, &table.Piece{Kind: a.Kind}, clear)
	if err != nil {
		return nil, err
	}
	return d.applyEdits([]regionEdit{source, dest}, notes)
}
