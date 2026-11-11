// Package changes は表のモデルの差から変更箇所を説明する。
package changes

import (
	"fmt"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"sort"
	"strconv"
	"strings"
)

type CellChange struct {
	Row, Col               int
	OldText, NewText, Kind string
}
type Changes struct {
	AddedRows, DeletedRows, AddedCols, DeletedCols int
	Cells                                          []CellChange
}

// changes.Changes.summary に対応する変更の要約。
func (c *Changes) Summary() string {
	var parts []string
	positions := map[model.Position]bool{}
	for _, x := range c.Cells {
		positions[model.Position{Row: x.Row, Col: x.Col}] = true
	}
	if len(positions) > 0 {
		parts = append(parts, fmt.Sprintf("セル %d 個を変更", len(positions)))
	}
	for _, v := range []struct {
		count        int
		unit, action string
	}{{c.AddedRows, "行", "追加"}, {c.DeletedRows, "行", "削除"}, {c.AddedCols, "列", "追加"}, {c.DeletedCols, "列", "削除"}} {
		if v.count > 0 {
			parts = append(parts, fmt.Sprintf("%d %sを%s", v.count, v.unit, v.action))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "（" + strings.Join(parts, "、") + "）"
}

// changes.Changes.details に対応する位置ごとの説明。
func (c *Changes) Details(limit int) []string {
	var order []model.Position
	descriptions := map[model.Position][]string{}
	for _, cell := range c.Cells {
		p := model.Position{Row: cell.Row, Col: cell.Col}
		if _, ok := descriptions[p]; !ok {
			order = append(order, p)
		}
		desc := cell.Kind + "を変更"
		if cell.Kind == "文字" {
			before, after := shortPair(cell.OldText, cell.NewText)
			desc = fmt.Sprintf("「%s」→「%s」", before, after)
		}
		descriptions[p] = append(descriptions[p], desc)
	}
	var lines []string
	for _, p := range order[:min(limit, len(order))] {
		lines = append(lines, "  "+address(p.Row, p.Col)+": "+strings.Join(descriptions[p], "、"))
	}
	if len(order) > limit {
		lines = append(lines, fmt.Sprintf("  ほか %d 個", len(order)-limit))
	}
	return lines
}

// changes._address に対応するセル番地。
func address(row, col int) string {
	letters := ""
	number := col + 1
	for number > 0 {
		digit := (number - 1) % 26
		number = (number - 1) / 26
		letters = string(rune('A'+digit)) + letters
	}
	return fmt.Sprintf("%s%d", letters, row+1)
}

// changes._short_text に対応する短い表示。
func shortText(s string, start int) string {
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(s)
	s = strings.ReplaceAll(s, "\n", "⏎")
	r := []rune(s)
	text := string(r[min(start, len(r)):min(start+30, len(r))])
	if start != 0 && s != "" {
		text = "…" + text
	}
	if len(r) > start+30 {
		text += "…"
	}
	return text
}

// changes._short_pair に対応する最初の違いが見える共通の範囲。
func shortPair(before, after string) (string, string) {
	texts := [2]string{before, after}
	for i, text := range texts {
		texts[i] = strings.ReplaceAll(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text), "\n", "⏎")
	}
	r0, r1 := []rune(texts[0]), []rune(texts[1])
	if max(len(r0), len(r1)) <= 30 {
		return texts[0], texts[1]
	}
	difference := 0
	for difference < min(len(r0), len(r1)) && r0[difference] == r1[difference] {
		difference++
	}
	start := max(0, difference-10)
	return shortText(texts[0], start), shortText(texts[1], start)
}

type pair struct{ i, j int }

// changes._similarity に対応する同じ位置の数。
func similarity(a, b []string) int {
	n := 0
	for i := 0; i < min(len(a), len(b)); i++ {
		if a[i] == b[i] {
			n++
		}
	}
	return n
}

// changes._overlap に対応する多重集合の共通数。
func overlap(a, b []string) int {
	ca, cb := map[string]int{}, map[string]int{}
	for _, v := range a {
		ca[v]++
	}
	for _, v := range b {
		cb[v]++
	}
	n := 0
	for k, v := range ca {
		n += min(v, cb[k])
	}
	return n
}

// changes._align に対応する順番を保つ最大類似の対応。
func align(old, new [][]string, i1, i2, j1, j2 int, sim func([]string, []string) int) []pair {
	co, cn := i2-i1, j2-j1
	var pairs []pair
	if co == cn || co*cn > 10000 {
		for k := 0; k < min(co, cn); k++ {
			pairs = append(pairs, pair{i1 + k, j1 + k})
		}
		return pairs
	}
	swapped := co > cn
	short, long := old[i1:i2], new[j1:j2]
	if swapped {
		short, long = new[j1:j2], old[i1:i2]
	}
	rows, cols := len(short), len(long)
	best := make([][]int, rows+1)
	for i := range best {
		best[i] = make([]int, cols+1)
		for j := range best[i] {
			best[i][j] = -1
		}
	}
	for j := 0; j <= cols; j++ {
		best[rows][j] = 0
	}
	for i := rows - 1; i >= 0; i-- {
		for j := cols - 1; j >= 0; j-- {
			if cols-j < rows-i {
				continue
			}
			take := best[i+1][j+1]
			if take >= 0 {
				take += sim(short[i], long[j])
			}
			skip := best[i][j+1]
			if skip < 0 || take >= 0 && take >= skip {
				best[i][j] = take
			} else {
				best[i][j] = skip
			}
		}
	}
	i, j := 0, 0
	for i < rows {
		take := best[i+1][j+1]
		if take >= 0 && take+sim(short[i], long[j]) == best[i][j] {
			if swapped {
				pairs = append(pairs, pair{i1 + j, j1 + i})
			} else {
				pairs = append(pairs, pair{i1 + i, j1 + j})
			}
			i++
		}
		j++
	}
	return pairs
}

// changes._corresponding に対応する同じ並びを目印にした対応。
func corresponding(old, new [][]string) ([]pair, int, int) {
	encode := func(rows [][]string) []string {
		var out []string
		for _, row := range rows {
			var b strings.Builder
			for _, s := range row {
				b.WriteString(strconv.Itoa(len(s)) + ":" + s)
			}
			out = append(out, b.String())
		}
		return out
	}
	var pairs []pair
	added, deleted := 0, 0
	for _, op := range pycompat.Opcodes(encode(old), encode(new)) {
		if op.Tag == "equal" {
			for k := 0; k < op.I2-op.I1; k++ {
				pairs = append(pairs, pair{op.I1 + k, op.J1 + k})
			}
		} else if op.Tag == "replace" {
			pairs = append(pairs, align(old, new, op.I1, op.I2, op.J1, op.J2, similarity)...)
		}
		added += max(0, (op.J2-op.J1)-(op.I2-op.I1))
		deleted += max(0, (op.I2-op.I1)-(op.J2-op.J1))
	}
	return pairs, added, deleted
}

// changes._covered に対応する結合に隠れる位置。
func covered(m *model.TableModel) map[model.Position]bool {
	out := map[model.Position]bool{}
	for _, c := range m.Cells {
		if c.Rowspan > 1 || c.Colspan > 1 {
			for row := c.Row; row < c.Row+c.Rowspan; row++ {
				for col := c.Col; col < c.Col+c.Colspan; col++ {
					if row != c.Row || col != c.Col {
						out[model.Position{Row: row, Col: col}] = true
					}
				}
			}
		}
	}
	return out
}

// changes.compare_models に対応する列、次に行の比較。
func CompareModels(old, new *model.TableModel) *Changes {
	result := &Changes{}
	oa, na := old.Anchors(), new.Anchors()
	textAt := func(anchors map[model.Position]*model.Cell, row, col int) string {
		if c := anchors[model.Position{Row: row, Col: col}]; c != nil {
			return c.Text
		}
		return ""
	}
	matchRows := func(columns []pair) ([]pair, int, int) {
		or, nr := make([][]string, old.Rows), make([][]string, new.Rows)
		for r := range or {
			for _, p := range columns {
				or[r] = append(or[r], textAt(oa, r, p.i))
			}
		}
		for r := range nr {
			for _, p := range columns {
				nr[r] = append(nr[r], textAt(na, r, p.j))
			}
		}
		return corresponding(or, nr)
	}
	var columns, rows []pair
	if old.Cols == new.Cols {
		for col := 0; col < old.Cols; col++ {
			columns = append(columns, pair{col, col})
		}
		rows, result.AddedRows, result.DeletedRows = matchRows(columns)
	} else {
		oc, nc := make([][]string, old.Cols), make([][]string, new.Cols)
		for col := range oc {
			for row := 0; row < old.Rows; row++ {
				oc[col] = append(oc[col], textAt(oa, row, col))
			}
		}
		for col := range nc {
			for row := 0; row < new.Rows; row++ {
				nc[col] = append(nc[col], textAt(na, row, col))
			}
		}
		columns = align(oc, nc, 0, old.Cols, 0, new.Cols, overlap)
		rows, _, _ = matchRows(columns)
		oc, nc = make([][]string, old.Cols), make([][]string, new.Cols)
		for col := range oc {
			for _, p := range rows {
				oc[col] = append(oc[col], textAt(oa, p.i, col))
			}
		}
		for col := range nc {
			for _, p := range rows {
				nc[col] = append(nc[col], textAt(na, p.j, col))
			}
		}
		columns, result.AddedCols, result.DeletedCols = corresponding(oc, nc)
		rows, result.AddedRows, result.DeletedRows = matchRows(columns)
	}
	ocov, ncov := covered(old), covered(new)
	for _, rp := range rows {
		for _, cp := range columns {
			op, np := model.Position{Row: rp.i, Col: cp.i}, model.Position{Row: rp.j, Col: cp.j}
			previous, current := oa[op], na[np]
			if previous == nil && ocov[op] || current == nil && ncov[np] {
				continue
			}
			if previous == nil {
				previous = model.NewCell(op.Row, op.Col, "")
			}
			if current == nil {
				current = model.NewCell(np.Row, np.Col, "")
			}
			for _, v := range []struct {
				kind      string
				different bool
			}{{"文字", previous.Text != current.Text}, {"配置", previous.Align != current.Align}, {"見出し", previous.Header != current.Header}, {"結合", previous.Rowspan != current.Rowspan || previous.Colspan != current.Colspan}} {
				if v.different {
					result.Cells = append(result.Cells, CellChange{current.Row, current.Col, previous.Text, current.Text, v.kind})
				}
			}
		}
	}
	sort.SliceStable(result.Cells, func(i, j int) bool {
		a, b := result.Cells[i], result.Cells[j]
		if a.Row != b.Row {
			return a.Row < b.Row
		}
		return a.Col < b.Col
	})
	return result
}
