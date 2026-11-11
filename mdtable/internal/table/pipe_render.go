package table

import (
	"fmt"
	"github.com/ryo-aoki-pc/md-table-excel/internal/cellsyntax"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"strconv"
	"strings"
)

// pipe_table.column_alignments に対応する列の配置の多数決。
func ColumnAlignments(m *model.TableModel, warnings *[]string, number int) []string {
	var result []string
	for col := 0; col < m.Cols; col++ {
		counts := map[string]int{}
		var order []string
		for _, c := range m.Cells {
			if c.Col != col {
				continue
			}
			a := c.Align
			if a == "justify" {
				a = ""
			}
			if _, ok := counts[a]; !ok {
				order = append(order, a)
			}
			counts[a]++
		}
		if len(order) == 0 {
			result = append(result, "")
			continue
		}
		best := order[0]
		for _, a := range order[1:] {
			if counts[a] > counts[best] || counts[a] == counts[best] && a == "" && best != "" {
				best = a
			}
		}
		if len(order) > 1 {
			*warnings = append(*warnings, fmt.Sprintf("表 %d の %d 列目: 配置が混在しているので、多いほうにそろえた", number, col+1))
		}
		result = append(result, best)
	}
	return result
}

// pipe_table.render の old_key に対応する、衝突しないタプルの符号化。
func stringTuple(items []string) string {
	var b strings.Builder
	for _, s := range items {
		b.WriteString(strconv.Itoa(len(s)) + ":" + s)
	}
	return b.String()
}

// pipe_table.render に対応する元の行とセルの再利用。
func RenderPipe(r *Region, m *model.TableModel, warnings *[]string) ([]string, error) {
	t := r.Pipe
	if m.Rows < 1 || m.Cols < 1 {
		return nil, cellsyntax.Cannot("見出し行と 1 列以上の列が要る")
	}
	for _, c := range m.Cells {
		if c.Rowspan > 1 || c.Colspan > 1 {
			return nil, &cellsyntax.NotRepresentable{Message: "セル結合がある", Row: c.Row + 1, Col: c.Col + 1}
		}
	}
	columns := m.Cols
	grid := make([][]string, m.Rows)
	for i := range grid {
		grid[i] = make([]string, columns)
	}
	for _, c := range m.Cells {
		grid[c.Row][c.Col] = c.Text
	}
	oldRows := append([]*PipeRow{t.Header}, t.Rows...)
	originalColumns := len(t.Alignments)
	oldKeys := make([]string, len(oldRows))
	newKeys := make([]string, len(grid))
	for i, row := range oldRows {
		values := make([]string, columns)
		copy(values, row.Texts)
		oldKeys[i] = stringTuple(values)
	}
	for i, row := range grid {
		newKeys[i] = stringTuple(row)
	}
	reused, paired := map[int]*PipeRow{}, map[int]*PipeRow{}
	for _, op := range pycompat.Opcodes(oldKeys, newKeys) {
		if op.Tag == "equal" {
			for k := 0; k < op.I2-op.I1; k++ {
				old := oldRows[op.I1+k]
				if len(old.Texts) <= columns || columns == originalColumns {
					reused[op.J1+k] = old
				} else {
					paired[op.J1+k] = old
				}
			}
		} else if op.Tag == "replace" {
			for k := 0; k < min(op.I2-op.I1, op.J2-op.J1); k++ {
				paired[op.J1+k] = oldRows[op.I1+k]
			}
		}
	}
	anywhere := map[string]string{}
	for _, row := range oldRows {
		for i, text := range row.Texts {
			if _, ok := anywhere[text]; !ok {
				anywhere[text] = row.RawCells[i]
			}
		}
	}
	cellSource := func(newRow, col int, text string) (string, error) {
		if old := paired[newRow]; old != nil {
			if col < len(old.Texts) && old.Texts[col] == text {
				return old.RawCells[col], nil
			}
			for i, oldText := range old.Texts {
				if oldText == text {
					return old.RawCells[i], nil
				}
			}
		}
		if raw, ok := anywhere[text]; ok {
			return raw, nil
		}
		s, e := cellsyntax.ToPipeCell(text)
		if e != nil {
			return "", &cellsyntax.NotRepresentable{Message: e.Error(), Row: newRow + 1, Col: col + 1}
		}
		return s, nil
	}
	type renderedRow struct {
		old  *PipeRow
		body string
	}
	var rendered []renderedRow
	for i, row := range grid {
		if old := reused[i]; old != nil {
			rendered = append(rendered, renderedRow{old: old})
		} else {
			var cells []string
			for col, text := range row {
				s, e := cellSource(i, col, text)
				if e != nil {
					return nil, e
				}
				cells = append(cells, s)
			}
			rendered = append(rendered, renderedRow{body: formatRow(cells, t)})
		}
	}
	aligns := ColumnAlignments(m, warnings, r.Number)
	delimiter := ""
	if stringTuple(aligns) == stringTuple(t.Alignments) && columns == len(t.Alignments) {
		delimiter = t.DelimiterPrefix + t.DelimiterBody
	} else {
		delimiter = r.ContPrefix + formatDelimiter(aligns, t)
	}
	var out []string
	for i, row := range rendered {
		if row.old != nil {
			prefix := row.old.Prefix
			if i == 0 {
				prefix = r.FirstPrefix()
			} else if row.old == oldRows[0] {
				prefix = r.ContPrefix
			}
			out = append(out, prefix+row.old.Body)
		} else {
			prefix := r.ContPrefix
			if i == 0 {
				prefix = r.FirstPrefix()
			}
			out = append(out, prefix+row.body)
		}
		if i == 0 {
			out = append(out, delimiter)
		}
	}
	return out, nil
}
