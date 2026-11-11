package table

import (
	"fmt"
	"strings"

	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
)

// htmlRowLayout は基準の行のセルの間にある空白だけを取り出す。
func htmlRowLayout(t *HTMLTable, base *HTMLRow) (open, between, close string) {
	if len(base.Cells) == 0 {
		for distance := 1; distance < len(t.Rows); distance++ {
			for _, i := range []int{base.Index - distance, base.Index + distance} {
				if i >= 0 && i < len(t.Rows) && len(t.Rows[i].Cells) > 0 {
					return htmlRowLayout(t, t.Rows[i])
				}
			}
		}
		return
	}
	space := func(s string) string {
		if strings.Trim(s, " \t\r\n") == "" {
			return s
		}
		return ""
	}
	first, last := base.Cells[0], base.Cells[len(base.Cells)-1]
	open = space(t.Text[base.Token.End:first.StartToken.Start])
	close = space(t.Text[last.EndToken.End:base.EndToken.Start])
	between = open
	if len(base.Cells) > 1 {
		between = space(t.Text[first.EndToken.End:base.Cells[1].StartToken.Start])
	}
	return
}

// newHTMLCell は値から作るセル。既存の原文を組み直すためには使わない。
func newHTMLCell(header bool, align, inner string, rowspan, colspan int) string {
	tag := "td"
	if header {
		tag = "th"
	}
	attrs := ""
	if align != "" {
		attrs += " align=\"" + align + "\""
	}
	if rowspan > 1 {
		attrs += fmt.Sprintf(" rowspan=\"%d\"", rowspan)
	}
	if colspan > 1 {
		attrs += fmt.Sprintf(" colspan=\"%d\"", colspan)
	}
	return "<" + tag + attrs + ">" + inner + "</" + tag + ">"
}

// HTMLValueCell は値だけの写しを貼り付け先の見出し・配置でセルにする。
func HTMLValueCell(c *PieceCell, header bool, align string) (*PieceCell, []string, error) {
	notes := []string{}
	inner, _, _, err := reusableInner(c.Text, nil, &notes, c.Row, c.Col)
	if err != nil {
		return nil, nil, err
	}
	v := *c
	v.Header = header
	v.Align = align
	v.Raw = newHTMLCell(header, align, inner, c.Rowspan, c.Colspan)
	return &v, notes, nil
}

func pieceModelCell(c *PieceCell, row, col int) *model.Cell {
	return &model.Cell{Row: row + c.Row, Col: col + c.Col, Rowspan: c.Rowspan, Colspan: c.Colspan, Text: c.Text, Header: c.Header, Align: c.Align}
}

func pieceGrid(p *Piece) map[model.Position]bool {
	grid := map[model.Position]bool{}
	for _, c := range p.Cells {
		for row := c.Row; row < c.Row+c.Rowspan; row++ {
			for col := c.Col; col < c.Col+c.Colspan; col++ {
				grid[model.Position{Row: row, Col: col}] = true
			}
		}
	}
	return grid
}

func shiftedExpectation(e *Expectation, at, count int, rows bool) {
	cells := map[model.Position]string{}
	for p, raw := range e.Cells {
		if rows {
			if p.Row >= at {
				p.Row += count
			}
		} else if p.Col >= at {
			p.Col += count
		}
		cells[p] = raw
	}
	e.Cells = cells
}

func insertStrings(old []string, at int, add []string) []string {
	result := append([]string{}, old[:at]...)
	result = append(result, add...)
	return append(result, old[at:]...)
}

// InsertHTMLRows は基準の行の区画へ写しを一度で挿入する。
func InsertHTMLRows(r *Region, base int, after bool, p *Piece) ([]string, *model.TableModel, *Expectation, error) {
	if reason, _ := ShapeLocks(r); reason != "" {
		return nil, nil, nil, fmt.Errorf("%s", reason)
	}
	t := r.HTML
	if base < 0 || base >= len(t.Rows) || p.Cols > t.Model.Cols {
		return nil, nil, nil, fmt.Errorf("行の位置か写しの幅が表の範囲外")
	}
	at := base
	if after {
		at++
	}
	for _, c := range t.Model.Cells {
		if c.Row < at && c.Row+c.Rowspan > at {
			return nil, nil, nil, fmt.Errorf("挿入する位置で結合セルが切れる")
		}
	}
	n := t.Model
	for i := 0; i < p.Rows; i++ {
		n = shapeRow(n, at, true)
	}
	e := SourceExpectation(r)
	shiftedExpectation(e, at, p.Rows, true)
	filled := &Piece{Rows: p.Rows, Cols: n.Cols, Cells: append([]*PieceCell{}, p.Cells...)}
	grid := pieceGrid(p)
	preserve := len(p.RowRaws) > 0
	for row := 0; row < p.Rows; row++ {
		for col := 0; col < n.Cols; col++ {
			pos := model.Position{Row: row, Col: col}
			if grid[pos] {
				continue
			}
			if preserve {
				n.Holes[model.Position{Row: at + row, Col: col}] = true
				continue
			}
			v := &PieceCell{Row: row, Col: col, Rowspan: 1, Colspan: 1}
			if old := t.Model.CellAt(base, col); old != nil {
				v.Header, v.Align = old.Header, old.Align
			}
			v.Raw = emptyHTMLCell(v.Header, v.Align)
			filled.Cells = append(filled.Cells, v)
		}
	}
	// 追加した空セルも列の順にする。
	orderPiece(filled)
	for _, c := range filled.Cells {
		n.Cells = append(n.Cells, pieceModelCell(c, at, 0))
		e.Cells[model.Position{Row: at + c.Row, Col: c.Col}] = c.Raw
	}
	tags := make([]string, p.Rows)
	raws := make([]string, p.Rows)
	open, between, close := htmlRowLayout(t, t.Rows[base])
	for row := 0; row < p.Rows; row++ {
		tag := "<tr>"
		if len(p.RowTags) > 0 && p.RowTags[row] != "" {
			tag = p.RowTags[row]
		}
		tags[row] = tag
		if preserve {
			raws[row] = p.RowRaws[row]
			continue
		}
		cells := []string{}
		for _, c := range filled.Cells {
			if c.Row == row {
				cells = append(cells, c.Raw)
			}
		}
		raws[row] = tag + open + strings.Join(cells, between) + close + PieceEndTag(tag)
	}
	e.RowTags = insertStrings(e.RowTags, at, tags)
	for i, s := range t.Sections {
		if s == t.Rows[base].Section {
			e.Sections[i] += p.Rows
			break
		}
	}
	lines, err := applyHTMLEdits(r, t, []htmlEdit{placeHTMLRows(t, t.Rows[base], after, raws)})
	return lines, model.New(n.Kind, n.Rows, n.Cols, n.Cells, n.Holes), e, err
}

// htmlColumnInsertion は既存の列挿入と写しで共通の挿入位置を求める。
func htmlColumnInsertion(t *HTMLTable, base *HTMLRow, at int, raws []string) htmlEdit {
	var left *HTMLCell
	for _, c := range base.Cells {
		if c.Col+c.Colspan <= at {
			left = c
		}
	}
	if left != nil {
		e, _ := besideHTMLCells(t, left, false, raws)
		return e
	}
	if len(base.Cells) > 0 {
		if e, own := besideHTMLCells(t, base.Cells[0], true, raws); own {
			return e
		}
	}
	return htmlEdit{base.Token.End, base.Token.End, strings.Join(raws, "")}
}

// InsertHTMLColumns はセルの外側の原文を保ったまま列を挿入する。
func InsertHTMLColumns(r *Region, at int, p *Piece) ([]string, *model.TableModel, *Expectation, error) {
	if _, reason := ShapeLocks(r); reason != "" {
		return nil, nil, nil, fmt.Errorf("%s", reason)
	}
	t := r.HTML
	if at < 0 || at > t.Model.Cols || p.Rows > t.Model.Rows {
		return nil, nil, nil, fmt.Errorf("列の位置か写しの高さが表の範囲外")
	}
	for _, c := range t.Model.Cells {
		if c.Col < at && c.Col+c.Colspan > at {
			return nil, nil, nil, fmt.Errorf("挿入する位置で結合セルが切れる")
		}
	}
	n := t.Model
	for i := 0; i < p.Cols; i++ {
		n = shapeColumn(n, at, true)
	}
	e := SourceExpectation(r)
	shiftedExpectation(e, at, p.Cols, false)
	filled := &Piece{Rows: n.Rows, Cols: p.Cols, Cells: append([]*PieceCell{}, p.Cells...)}
	grid := pieceGrid(p)
	for row, base := range t.Rows {
		header := len(base.Cells) > 0
		for _, c := range base.Cells {
			header = header && c.Tag == "th"
		}
		for col := 0; col < p.Cols; col++ {
			if !grid[model.Position{Row: row, Col: col}] {
				filled.Cells = append(filled.Cells, &PieceCell{Row: row, Col: col, Rowspan: 1, Colspan: 1, Header: header, Raw: emptyHTMLCell(header, "")})
			}
		}
	}
	orderPiece(filled)
	for _, c := range filled.Cells {
		n.Cells = append(n.Cells, pieceModelCell(c, 0, at))
		e.Cells[model.Position{Row: c.Row, Col: at + c.Col}] = c.Raw
	}
	edits := []htmlEdit{}
	for row, base := range t.Rows {
		raws := []string{}
		for _, c := range filled.Cells {
			if c.Row == row {
				raws = append(raws, c.Raw)
			}
		}
		if len(raws) > 0 {
			edits = append(edits, htmlColumnInsertion(t, base, at, raws))
		}
	}
	lines, err := applyHTMLEdits(r, t, edits)
	return lines, model.New(n.Kind, n.Rows, n.Cols, n.Cells, n.Holes), e, err
}

func DuplicateHTMLRows(r *Region, row, count int) ([]string, *model.TableModel, *Expectation, error) {
	p, err := CopyPiece(r, row, 0, count, r.Model().Cols, true)
	if err != nil {
		return nil, nil, nil, err
	}
	return InsertHTMLRows(r, row+count-1, true, p)
}
func DuplicateHTMLColumns(r *Region, col, count int) ([]string, *model.TableModel, *Expectation, error) {
	p, err := CopyPiece(r, 0, col, r.Model().Rows, count, false)
	if err != nil {
		return nil, nil, nil, err
	}
	return InsertHTMLColumns(r, col+count, p)
}

// DeleteHTMLRowBlock は結合を切らない複数行を一度に取り除く。
// DeleteHTMLRow の途中で結合を縮めると穴へセルが移る表でも、残すセルの置き方を保つ。
func DeleteHTMLRowBlock(r *Region, row, count int) ([]string, *model.TableModel, error) {
	if reason, _ := ShapeLocks(r); reason != "" {
		return nil, nil, fmt.Errorf("%s", reason)
	}
	m := r.Model()
	if _, err := CopyPiece(r, row, 0, count, m.Cols, false); err != nil {
		return nil, nil, err
	}
	if count == m.Rows {
		return nil, nil, fmt.Errorf("最後の 1 行は削除できない")
	}
	cells := []*model.Cell{}
	holes := map[model.Position]bool{}
	for _, c := range m.Cells {
		if c.Row >= row && c.Row < row+count {
			continue
		}
		v := *c
		if v.Row >= row+count {
			v.Row -= count
		}
		cells = append(cells, &v)
	}
	for p := range m.Holes {
		if p.Row >= row && p.Row < row+count {
			continue
		}
		if p.Row >= row+count {
			p.Row -= count
		}
		holes[p] = true
	}
	edits := []htmlEdit{}
	for _, base := range r.HTML.Rows[row : row+count] {
		start, end, _, own := ownHTMLRow(r.HTML, base)
		if own && end < len(r.HTML.Text) {
			end++
		} else if !own {
			start, end = base.Token.Start, base.EndToken.End
		}
		edits = append(edits, htmlEdit{start, end, ""})
	}
	lines, err := applyHTMLEdits(r, r.HTML, edits)
	return lines, model.New(m.Kind, m.Rows-count, m.Cols, cells, holes), err
}

// ReplaceHTMLCells は形の検査後にセル全体を差し替える。clear は次の移動処理でも使う。
func ReplaceHTMLCells(r *Region, row, col int, p *Piece, clear []model.Position) ([]string, *model.TableModel, *Expectation, error) {
	t := r.HTML
	n := copyShape(t.Model)
	e := SourceExpectation(r)
	edits := []htmlEdit{}
	for _, c := range p.Cells {
		pos := model.Position{Row: row + c.Row, Col: col + c.Col}
		old := t.CellAt[pos]
		if old == nil {
			return nil, nil, nil, fmt.Errorf("貼り付け先の結合の形が写しと違う")
		}
		canonical := old.StartToken.Text + t.Text[old.ContentStart:old.ContentEnd] + PieceEndTag(old.StartToken.Text)
		if c.Raw != canonical && c.Raw != cellRaw(t, old) {
			end := old.ContentEnd
			if old.EndToken != nil {
				end = old.EndToken.End
			}
			edits = append(edits, htmlEdit{old.StartToken.Start, end, c.Raw})
			e.Cells[pos] = c.Raw
		}
		for i, other := range n.Cells {
			if other.Row == pos.Row && other.Col == pos.Col {
				n.Cells[i] = pieceModelCell(c, row, col)
				break
			}
		}
	}
	for _, pos := range clear {
		old := t.CellAt[pos]
		if old == nil {
			continue
		}
		edits = append(edits, htmlEdit{old.ContentStart, old.ContentEnd, ""})
		raw := t.Text[old.StartToken.Start:old.ContentStart]
		if old.EndToken != nil {
			raw += old.EndToken.Text
		}
		e.Cells[pos] = raw
		for _, c := range n.Cells {
			if c.Row == pos.Row && c.Col == pos.Col {
				c.Text = ""
			}
		}
	}
	lines, err := applyHTMLEdits(r, t, edits)
	return lines, model.New(n.Kind, n.Rows, n.Cols, n.Cells, n.Holes), e, err
}
