package tableedit

import "github.com/ryo-aoki-pc/md-tools/mdtable/internal/table"

func formatBounds(r *table.Region, rect Rect) error {
	if reason := table.CellLocks(r); reason != "" {
		return fault("unsupported", reason)
	}
	if err := copyBounds(r, rect.Row, rect.Col, rect.Rows, rect.Cols); err != nil {
		if failure, ok := err.(*Error); ok && failure.Code == "merged" {
			return located(fault("merged", "変更する範囲で結合セルが切れるので、範囲を選び直してください"), failure.Row, failure.Col)
		}
		return err
	}
	return nil
}

// SetAlign はパイプの表の列、HTML の表のセルの揃えを変える。空文字は既定へ戻す。
func (d *Document) SetAlign(t Target, rect Rect, align string) (result *Result, err error) {
	defer catch(&result, &err)
	if align != "" && align != "left" && align != "center" && align != "right" {
		return nil, fault("invalid", "揃えは left・center・right・空文字で指定してください")
	}
	r, err := d.target(t)
	if err != nil {
		return nil, err
	}
	if err := formatBounds(r, rect); err != nil {
		return nil, err
	}
	if r.Kind == "pipe" {
		lines, expected, expect := table.SetPipeAlign(r, rect.Col, rect.Cols, align)
		return d.applyEdits([]regionEdit{{region: r, lines: lines, expected: expected, expect: expect}}, nil)
	}
	lines, expected, expect, err := table.SetHTMLFormat(r, rect.Row, rect.Col, rect.Rows, rect.Cols, align, nil)
	if err != nil {
		return nil, fault("unsupported", err.Error())
	}
	return d.applyEdits([]regionEdit{{region: r, lines: lines, expected: expected, expect: expect}}, nil)
}

// SetHeader は HTML の表のセルを th または td にする。結合は保つ。
func (d *Document) SetHeader(t Target, rect Rect, header bool) (result *Result, err error) {
	defer catch(&result, &err)
	r, err := d.target(t)
	if err != nil {
		return nil, err
	}
	if r.Kind != "html" {
		return nil, fault("unsupported", "パイプの表は見出し行だけが見出しセルなので、セルごとの見出しは変えられません")
	}
	if err := formatBounds(r, rect); err != nil {
		return nil, err
	}
	lines, expected, expect, err := table.SetHTMLFormat(r, rect.Row, rect.Col, rect.Rows, rect.Cols, "", &header)
	if err != nil {
		return nil, fault("unsupported", err.Error())
	}
	return d.applyEdits([]regionEdit{{region: r, lines: lines, expected: expected, expect: expect}}, nil)
}
