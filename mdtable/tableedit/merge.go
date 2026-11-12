package tableedit

import "github.com/ryo-aoki-pc/md-tools/mdtable/internal/table"

func mergeBounds(r *table.Region, rect Rect) error {
	if err := copyBounds(r, rect.Row, rect.Col, rect.Rows, rect.Cols); err != nil {
		if failure, ok := err.(*Error); ok && failure.Code == "merged" {
			return located(fault("merged", "結合する範囲で結合セルが切れるので、範囲を選び直してください"), failure.Row, failure.Col)
		}
		return err
	}
	if rect.Rows == 1 && rect.Cols == 1 {
		return fault("unsupported", "1 セルだけの範囲は結合できません")
	}
	count := 0
	for _, c := range r.Model().Cells {
		if c.Row >= rect.Row && c.Row < rect.Row+rect.Rows && c.Col >= rect.Col && c.Col < rect.Col+rect.Cols {
			count++
		}
	}
	if count < 2 {
		return fault("unsupported", "1 セルだけの範囲は結合できません")
	}
	for p := range r.Model().Holes {
		if p.Row >= rect.Row && p.Row < rect.Row+rect.Rows && p.Col >= rect.Col && p.Col < rect.Col+rect.Cols {
			return located(fault("unsupported", "セルの無い位置を含む範囲は結合できません"), p.Row, p.Col)
		}
	}
	if r.Kind == "pipe" && rect.Row == 0 && rect.Rows > 1 {
		return fault("unsupported", "パイプの表の見出し行と本文をまたいでは結合できません")
	}
	return nil
}

func (d *Document) mergeCells(r *table.Region, rect Rect) (*Result, error) {
	if r.Kind != "html" {
		return nil, fault("unsupported", "パイプの表は、HTML の表にしてから結合してください")
	}
	if reason := table.CellLocks(r); reason != "" {
		return nil, fault("unsupported", reason)
	}
	if err := mergeBounds(r, rect); err != nil {
		return nil, err
	}
	lines, expected, expect, err := table.MergeHTMLCells(r, rect.Row, rect.Col, rect.Rows, rect.Cols)
	if err != nil {
		return nil, fault("unsupported", err.Error())
	}
	return d.applyEdits([]regionEdit{{region: r, lines: lines, expected: expected, expect: expect}}, nil)
}

// MergeCells は HTML の表の範囲を結合し、左上以外のセルの内容を捨てる。
func (d *Document) MergeCells(t Target, rect Rect) (result *Result, err error) {
	defer catch(&result, &err)
	r, err := d.target(t)
	if err != nil {
		return nil, err
	}
	return d.mergeCells(r, rect)
}

// MergeCellsAsHTML はパイプの表の変換と結合をメモリー上で済ませ、一個の本文を返す。
func (d *Document) MergeCellsAsHTML(t Target, rect Rect) (result *Result, err error) {
	defer catch(&result, &err)
	r, err := d.target(t)
	if err != nil {
		return nil, err
	}
	if r.Kind != "pipe" {
		return nil, fault("unsupported", "HTML の表は、そのまま結合してください")
	}
	if err := mergeBounds(r, rect); err != nil {
		return nil, err
	}
	converted, err := d.ConvertTable(t, "html")
	if err != nil {
		return nil, err
	}
	next := converted.Document
	r, err = next.target(Target{Number: t.Number})
	if err != nil {
		return nil, err
	}
	result, err = next.mergeCells(r, rect)
	if err == nil {
		result.Warnings = editWarnings(append(converted.Warnings, result.Warnings...))
		result.Changed = result.Text != d.text
	}
	return result, err
}

// UnmergeCells は結合の左上を指定して解除し、覆われていた位置へ空セルを入れる。
func (d *Document) UnmergeCells(t Target, row, col int) (result *Result, err error) {
	defer catch(&result, &err)
	r, err := d.target(t)
	if err != nil {
		return nil, err
	}
	if r.Kind != "html" {
		return nil, fault("unsupported", "パイプの表には結合したセルがありません")
	}
	if reason := table.CellLocks(r); reason != "" {
		return nil, fault("unsupported", reason)
	}
	c := r.Model().CellAt(row, col)
	if c == nil {
		return nil, located(fault("range", "指定した位置にセルが無い"), row, col)
	}
	if c.Row != row || c.Col != col {
		return nil, located(fault("merged", "結合したセルの左上を指定してください"), c.Row, c.Col)
	}
	if c.Rowspan == 1 && c.Colspan == 1 {
		return nil, located(fault("unsupported", "結合していないセルは解除できません"), row, col)
	}
	lines, expected, expect, err := table.UnmergeHTMLCell(r, row, col)
	if err != nil {
		return nil, fault("unsupported", err.Error())
	}
	return d.applyEdits([]regionEdit{{region: r, lines: lines, expected: expected, expect: expect}}, nil)
}
