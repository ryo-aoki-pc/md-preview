package table

import (
	"fmt"
	"strings"

	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
)

// formatPiecePipeRow はセルの原文が行頭の HTML や区切りとして解釈されないようにする。
func formatPiecePipeRow(raws []string, format *PipeTable) string {
	copy := *format
	if len(raws) > 0 && strings.HasPrefix(strings.TrimLeft(raws[0], " \t"), "<") {
		copy.LeadingPipe = true
	}
	for _, raw := range raws {
		if strings.HasSuffix(raw, "\\") {
			copy.SpacedCells = true
			break
		}
	}
	return formatRow(raws, &copy)
}

// InsertPipeRows は見出しより下に複数の行の本文を挿入する。
func InsertPipeRows(r *Region, at int, bodies []string) ([]string, *model.TableModel, *Expectation, error) {
	m := r.Model()
	if at < 0 || at > m.Rows {
		return nil, nil, nil, fmt.Errorf("行の位置が表の範囲外")
	}
	if at == 0 {
		return nil, nil, nil, fmt.Errorf("見出しの上には行を追加できない")
	}
	n := m
	for range bodies {
		n = shapeRow(n, at, true)
	}
	e := SourceExpectation(r)
	shiftedExpectation(e, at, len(bodies), true)
	nextBodies := map[int]string{}
	for row, body := range e.Bodies {
		if row >= at {
			row += len(bodies)
		}
		nextBodies[row] = body
	}
	e.Bodies = nextBodies
	for i, body := range bodies {
		pr := parsePipeRow(0, "", body)
		row := at + i
		e.Bodies[row] = body
		for col := 0; col < n.Cols; col++ {
			c := model.NewCell(row, col, "")
			raw := ""
			if col < len(pr.RawCells) {
				c.Text = pr.Texts[col]
				raw = pr.RawCells[col]
			}
			c.Align = r.Pipe.Alignments[col]
			n.Cells = append(n.Cells, c)
			e.Cells[model.Position{Row: row, Col: col}] = raw
		}
	}
	lines := make([]string, len(r.Bodies))
	for i, body := range r.Bodies {
		lines[i] = r.Prefixes[i] + body
	}
	add := make([]string, len(bodies))
	for i, body := range bodies {
		add[i] = r.ContPrefix + body
	}
	lines = insertStrings(lines, at+1, add)
	return lines, model.New(n.Kind, n.Rows, n.Cols, n.Cells, n.Holes), e, nil
}

func InsertPipePieceRows(r *Region, at int, p *Piece) ([]string, *model.TableModel, *Expectation, error) {
	if p.Cols > r.Model().Cols {
		return nil, nil, nil, fmt.Errorf("写しの幅が表の範囲外")
	}
	bodies := append([]string{}, p.RowRaws...)
	if len(bodies) == 0 {
		for row := 0; row < p.Rows; row++ {
			raws := make([]string, r.Model().Cols)
			for _, c := range p.Cells {
				if c.Row == row {
					raws[c.Col] = c.Raw
				}
			}
			bodies = append(bodies, formatPiecePipeRow(raws, r.Pipe))
		}
	}
	return InsertPipeRows(r, at, bodies)
}

// rebuildPipeColumns は pipeColumn と写しの挿入で共通の列の組み直し。
func rebuildPipeColumns(r *Region, n *model.TableModel, alter func(int, []string) []string, delimiter []string) ([]string, *Expectation) {
	e := &Expectation{Cells: map[model.Position]string{}, Bodies: map[int]string{}}
	format := *r.Pipe
	format.SpacedCells = r.Pipe.DelimiterSpaced
	if n.Cols == 1 && !format.LeadingPipe && !format.TrailingPipe {
		format.LeadingPipe = true
	}
	delim := formatRow(delimiter, &format)
	e.Delimiter = &delim
	lines := []string{}
	for row, old := range append([]*PipeRow{r.Pipe.Header}, r.Pipe.Rows...) {
		cells := make([]string, r.Model().Cols)
		copy(cells, old.RawCells)
		cells = alter(row, cells)
		rowFormat := *r.Pipe
		if n.Cols == 1 && !rowFormat.LeadingPipe && !rowFormat.TrailingPipe {
			rowFormat.LeadingPipe = true
		}
		body := formatPiecePipeRow(cells, &rowFormat)
		e.Bodies[row] = body
		for col, raw := range cells {
			e.Cells[model.Position{Row: row, Col: col}] = raw
		}
		lines = append(lines, old.Prefix+body)
		if row == 0 {
			lines = append(lines, r.Pipe.DelimiterPrefix+delim)
		}
	}
	return lines, e
}

func InsertPipeColumns(r *Region, at int, p *Piece) ([]string, *model.TableModel, *Expectation, error) {
	m := r.Model()
	if at < 0 || at > m.Cols || p.Rows > m.Rows {
		return nil, nil, nil, fmt.Errorf("列の位置か写しの高さが表の範囲外")
	}
	n := m
	for i := 0; i < p.Cols; i++ {
		n = shapeColumn(n, at, true)
	}
	added := make([][]string, m.Rows)
	delimiters := make([]string, p.Cols)
	for col := range delimiters {
		delimiters[col] = "---"
		if len(p.Delimiters) > 0 {
			delimiters[col] = p.Delimiters[col]
		}
	}
	byPosition := map[model.Position]*PieceCell{}
	for _, c := range p.Cells {
		byPosition[model.Position{Row: c.Row, Col: c.Col}] = c
	}
	for row := 0; row < m.Rows; row++ {
		added[row] = make([]string, p.Cols)
		for col := 0; col < p.Cols; col++ {
			v := model.NewCell(row, at+col, "")
			v.Header = row == 0
			v.Align, _ = PipeAlign(delimiters[col])
			if c := byPosition[model.Position{Row: row, Col: col}]; c != nil {
				v.Text = c.Text
				added[row][col] = c.Raw
			}
			n.Cells = append(n.Cells, v)
		}
	}
	delim := insertStrings(pipeDelimiterCells(r), at, delimiters)
	lines, e := rebuildPipeColumns(r, n, func(row int, raws []string) []string { return insertStrings(raws, at, added[row]) }, delim)
	return lines, model.New(n.Kind, n.Rows, n.Cols, n.Cells, n.Holes), e, nil
}

func DuplicatePipeRows(r *Region, row, count int) ([]string, *model.TableModel, *Expectation, error) {
	p, err := CopyPiece(r, row, 0, count, r.Model().Cols, true)
	if err != nil {
		return nil, nil, nil, err
	}
	return InsertPipePieceRows(r, row+count, p)
}
func DuplicatePipeColumns(r *Region, col, count int) ([]string, *model.TableModel, *Expectation, error) {
	p, err := CopyPiece(r, 0, col, r.Model().Rows, count, false)
	if err != nil {
		return nil, nil, nil, err
	}
	return InsertPipeColumns(r, col+count, p)
}

func ReplacePipeCells(r *Region, row, col int, p *Piece, clear []model.Position) ([]string, *model.TableModel, *Expectation, error) {
	n := copyShape(r.Model())
	e := SourceExpectation(r)
	raws := map[model.Position]string{}
	for _, c := range p.Cells {
		pos := model.Position{Row: row + c.Row, Col: col + c.Col}
		if e.Cells[pos] != c.Raw {
			raws[pos] = c.Raw
		}
		n.CellAt(pos.Row, pos.Col).Text = c.Text
		e.Cells[pos] = c.Raw
	}
	for _, pos := range clear {
		raws[pos] = ""
		e.Cells[pos] = ""
		n.CellAt(pos.Row, pos.Col).Text = ""
	}
	lines, err := PatchPipeRaw(r, raws)
	for pos := range raws {
		delete(e.Bodies, pos.Row)
	}
	return lines, n, e, err
}
