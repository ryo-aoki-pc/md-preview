package table

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
)

// ShapeLocks は原文を保った行・列の変更を決められない理由を返す。
func ShapeLocks(r *Region) (rows, cols string) {
	if r.Kind != "html" || r.HTML == nil {
		return "", ""
	}
	t := r.HTML
	for _, row := range t.Rows {
		if row.Token == nil || row.EndToken == nil {
			rows = "<tr>・</tr> が省略されているので、行・列は変えられない"
			cols = rows
		}
	}
	for _, c := range t.Cells {
		if c.EndToken == nil {
			rows = "</td>・</th> が省略されているので、行・列は変えられない"
			cols = rows
		}
		for _, key := range []string{"rowspan", "colspan"} {
			seen := false
			for _, a := range c.StartToken.Attributes {
				if a.Name != key {
					continue
				}
				value := strings.TrimSpace(attrValue(a))
				n, e := strconv.Atoi(value)
				valid := value != ""
				for _, ch := range value {
					valid = valid && ch >= '0' && ch <= '9'
				}
				span := c.Colspan
				if key == "rowspan" {
					span = c.Rowspan
				}
				if seen || !valid || e != nil || n < 1 || span != n {
					rows = "結合の値が 0・数字以外・区画の終わりで切られた値なので、行・列は変えられない"
					cols = rows
				}
				seen = true
			}
		}
	}
	if cols == "" && len(t.Colgroups) > 0 {
		cols = "<colgroup>・<col> があるので、列は変えられない"
	}
	if cols == "" && len(t.Model.Holes) > 0 {
		cols = "セルの無い位置があるので、列は変えられない"
	}
	body := false
	for _, s := range t.Sections {
		if s.Tag == "tbody" {
			body = true
		}
		if body && s.Tag == "thead" && rows == "" {
			rows = "<thead> が <tbody> の後ろにあるので、行は変えられない"
		}
	}
	return
}

// spanEdit は属性を組み直さず、結合の値の数字だけを書き換える。
func spanEdit(c *HTMLCell, name string, value int) htmlEdit {
	token := c.StartToken
	pos := len("<") + len(token.Name)
	for _, a := range token.Attributes {
		i := strings.Index(token.Text[pos:], a.Raw)
		if i < 0 {
			continue
		}
		start := pos + i
		pos = start + len(a.Raw)
		if a.Name != name {
			continue
		}
		if value == 1 {
			for start > 0 && (token.Text[start-1] == ' ' || token.Text[start-1] == '\t' || token.Text[start-1] == '\n') {
				start--
			}
			return htmlEdit{token.Start + start, token.Start + pos, ""}
		}
		eq := strings.IndexByte(a.Raw, '=') + 1
		for eq < len(a.Raw) && (a.Raw[eq] < '0' || a.Raw[eq] > '9') {
			eq++
		}
		end := eq
		for end < len(a.Raw) && a.Raw[end] >= '0' && a.Raw[end] <= '9' {
			end++
		}
		zeros := 0
		for eq+zeros < end-1 && a.Raw[eq+zeros] == '0' {
			zeros++
		}
		return htmlEdit{token.Start + start + eq, token.Start + start + end, strings.Repeat("0", zeros) + strconv.Itoa(value)}
	}
	return htmlEdit{token.Start + 1 + len(token.Name), token.Start + 1 + len(token.Name), " " + name + "=\"" + strconv.Itoa(value) + "\""}
}

func emptyHTMLCell(header bool, align string) string {
	tag := "td"
	if header {
		tag = "th"
	}
	attrs := ""
	if align != "" {
		attrs = " align=\"" + align + "\""
	}
	return "<" + tag + attrs + "></" + tag + ">"
}

// ownHTMLRange は要素の前後が空白だけなら、要素を含む行全体の境界を返す。
func ownHTMLRange(t *HTMLTable, first, last int) (start, end int, indent string, own bool) {
	start = strings.LastIndex(t.Text[:first], "\n") + 1
	end = strings.IndexByte(t.Text[last:], '\n')
	if end < 0 {
		end = len(t.Text)
	} else {
		end += last
	}
	indent = t.Text[start:first]
	own = strings.Trim(indent, " \t") == "" && strings.Trim(t.Text[last:end], " \t") == ""
	return
}

// ownHTMLRow は行の前後が空白だけなら、その行全体の境界を返す。
func ownHTMLRow(t *HTMLTable, row *HTMLRow) (start, end int, indent string, own bool) {
	return ownHTMLRange(t, row.Token.Start, row.EndToken.End)
}

// besideHTMLCell は隣のセルが行を占めていれば、同じ字下げの行として差し込む。
// 行頭への幅 0 の差し込みを避け、元の行のコンテナ接頭辞を残す。
func besideHTMLCell(t *HTMLTable, neighbor *HTMLCell, before bool, raw string) (htmlEdit, bool) {
	start, end, indent, own := ownHTMLRange(t, neighbor.StartToken.Start, neighbor.EndToken.End)
	pos := neighbor.EndToken.End
	if before {
		pos = neighbor.StartToken.Start
	}
	if own {
		if before {
			if start == 0 {
				return htmlEdit{pos, pos, raw}, false
			}
			pos = start - 1
		} else {
			pos = end
		}
		raw = "\n" + indent + raw
	}
	return htmlEdit{pos, pos, raw}, own
}

// InsertHTMLRow は基準行と同じ区画へ空の行を差し込む。
func InsertHTMLRow(r *Region, row int, after bool) ([]string, *model.TableModel, error) {
	if reason, _ := ShapeLocks(r); reason != "" {
		return nil, nil, fmt.Errorf("%s", reason)
	}
	t := r.HTML
	if row < 0 || row >= len(t.Rows) {
		return nil, nil, fmt.Errorf("行の位置が表の範囲外")
	}
	at := row
	if after {
		at++
	}
	n := shapeRow(t.Model, at, true)
	var edits []htmlEdit
	var cells strings.Builder
	for _, c := range t.Cells {
		if c.Row < at && c.Row+c.Rowspan > at {
			edits = append(edits, spanEdit(c, "rowspan", c.Rowspan+1))
		}
	}
	for col := 0; col < n.Cols; col++ {
		if n.CellAt(at, col) != nil {
			continue
		}
		old := t.Model.CellAt(row, col)
		c := model.NewCell(at, col, "")
		if old != nil {
			c.Header, c.Align = old.Header, old.Align
		}
		n.Cells = append(n.Cells, c)
		cells.WriteString(emptyHTMLCell(c.Header, c.Align))
	}
	newRow := "<tr>" + cells.String() + "</tr>"
	base := t.Rows[row]
	start, end, indent, own := ownHTMLRow(t, base)
	pos := base.Token.Start
	if after {
		pos = base.EndToken.End
	}
	if own {
		if after {
			pos = end
			newRow = "\n" + indent + newRow
		} else if start > 0 {
			pos = start - 1
			newRow = "\n" + indent + newRow
		} else {
			pos = start
			newRow += "\n" + indent
		}
	}
	edits = append(edits, htmlEdit{pos, pos, newRow})
	lines, e := applyHTMLEdits(r, t, edits)
	return lines, model.New(n.Kind, n.Rows, n.Cols, n.Cells, n.Holes), e
}

func DeleteHTMLRow(r *Region, row int) ([]string, *model.TableModel, error) {
	if reason, _ := ShapeLocks(r); reason != "" {
		return nil, nil, fmt.Errorf("%s", reason)
	}
	t := r.HTML
	if row < 0 || row >= len(t.Rows) {
		return nil, nil, fmt.Errorf("行の位置が表の範囲外")
	}
	if len(t.Rows) == 1 {
		return nil, nil, fmt.Errorf("最後の 1 行は削除できない")
	}
	base := t.Rows[row]
	start, end, _, own := ownHTMLRow(t, base)
	if own && end < len(t.Text) {
		end++
	} else if !own {
		start, end = base.Token.Start, base.EndToken.End
	}
	edits := []htmlEdit{{start, end, ""}}
	// 移すセルは同じ位置へまとめて差し込む。列の順番を保つ。
	moves := map[int]string{}
	var positions []int
	for _, c := range t.Cells {
		if c.Row < row && c.Row+c.Rowspan > row {
			edits = append(edits, spanEdit(c, "rowspan", c.Rowspan-1))
		}
		if c.Row == row && c.Rowspan > 1 {
			next := t.Rows[row+1]
			e := spanEdit(c, "rowspan", c.Rowspan-1)
			raw := t.Text[c.StartToken.Start:e.start] + e.replacement + t.Text[e.end:c.EndToken.End]
			insertion := htmlEdit{next.EndToken.Start, next.EndToken.Start, raw}
			var right *HTMLCell
			for _, other := range next.Cells {
				if other.Col > c.Col {
					right = other
					break
				}
			}
			if right != nil {
				insertion, _ = besideHTMLCell(t, right, true, raw)
			} else if len(next.Cells) > 0 {
				if after, own := besideHTMLCell(t, next.Cells[len(next.Cells)-1], false, raw); own {
					insertion = after
				}
			}
			if _, ok := moves[insertion.start]; !ok {
				positions = append(positions, insertion.start)
			}
			moves[insertion.start] += insertion.replacement
		}
	}
	for _, pos := range positions {
		edits = append(edits, htmlEdit{pos, pos, moves[pos]})
	}
	lines, e := applyHTMLEdits(r, t, edits)
	return lines, shapeRow(t.Model, row, false), e
}

func htmlColumn(r *Region, at int, insert bool) ([]string, *model.TableModel, error) {
	if _, reason := ShapeLocks(r); reason != "" {
		return nil, nil, fmt.Errorf("%s", reason)
	}
	t := r.HTML
	if at < 0 || at >= t.Model.Cols && (!insert || at != t.Model.Cols) {
		return nil, nil, fmt.Errorf("列の位置が表の範囲外")
	}
	if !insert && t.Model.Cols == 1 {
		return nil, nil, fmt.Errorf("最後の 1 列は削除できない")
	}
	n := shapeColumn(t.Model, at, insert)
	var edits []htmlEdit
	for _, c := range t.Cells {
		if insert {
			if c.Col < at && at < c.Col+c.Colspan {
				edits = append(edits, spanEdit(c, "colspan", c.Colspan+1))
			}
		} else if c.Col <= at && at < c.Col+c.Colspan {
			if c.Colspan > 1 {
				edits = append(edits, spanEdit(c, "colspan", c.Colspan-1))
			} else {
				start, end, _, own := ownHTMLRange(t, c.StartToken.Start, c.EndToken.End)
				if own && end < len(t.Text) {
					end++
				} else if !own {
					start, end = c.StartToken.Start, c.EndToken.End
				}
				edits = append(edits, htmlEdit{start, end, ""})
			}
		}
	}
	if insert {
		for row, base := range t.Rows {
			if n.CellAt(row, at) != nil {
				continue
			}
			pos := base.Token.End
			var left *HTMLCell
			header := len(base.Cells) > 0
			for _, c := range base.Cells {
				header = header && c.Tag == "th"
				if c.Col+c.Colspan <= at {
					pos = c.EndToken.End
					left = c
				}
			}
			c := model.NewCell(row, at, "")
			c.Header = header
			n.Cells = append(n.Cells, c)
			raw := emptyHTMLCell(header, "")
			insertion := htmlEdit{pos, pos, raw}
			if left != nil {
				insertion, _ = besideHTMLCell(t, left, false, raw)
			} else if len(base.Cells) > 0 {
				if before, own := besideHTMLCell(t, base.Cells[0], true, raw); own {
					insertion = before
				}
			}
			edits = append(edits, insertion)
		}
	}
	lines, e := applyHTMLEdits(r, t, edits)
	return lines, model.New(n.Kind, n.Rows, n.Cols, n.Cells, n.Holes), e
}

func InsertHTMLColumn(r *Region, at int) ([]string, *model.TableModel, error) {
	return htmlColumn(r, at, true)
}
func DeleteHTMLColumn(r *Region, col int) ([]string, *model.TableModel, error) {
	return htmlColumn(r, col, false)
}
