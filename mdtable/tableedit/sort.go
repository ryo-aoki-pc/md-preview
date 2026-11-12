package tableedit

import "github.com/ryo-aoki-pc/md-tools/mdtable/internal/table"

// SortRows は選んだセルの区画のデータ行を、指定した列の値で安定に並べ替える。
func (d *Document) SortRows(t Target, row, col int, desc bool) (result *Result, err error) {
	defer catch(&result, &err)
	r, err := d.target(t)
	if err != nil {
		return nil, err
	}
	if row < 0 || row >= r.Model().Rows || col < 0 || col >= r.Model().Cols {
		return nil, fault("range", "セルの位置が表の範囲外")
	}
	first, count, reason := table.SortRange(r, row)
	if reason != "" {
		return nil, fault("unsupported", reason)
	}
	rows := table.SortedRows(r, first, count, col, desc)
	changed := false
	for i, old := range rows {
		changed = changed || old != first+i
	}
	if !changed {
		return d.unchanged(r)
	}
	lines, expected, expect, err := table.PermuteRows(r, first, rows)
	if err != nil {
		return nil, renderError(r, err)
	}
	endings := table.PermuteRowEndings(r, first, rows, d.md.Endings[r.Start:r.End+1])
	return d.applyEdits([]regionEdit{{region: r, lines: lines, endings: endings, expected: expected, expect: expect}}, nil)
}
