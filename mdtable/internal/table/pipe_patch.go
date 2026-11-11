package table

import (
	"strings"

	"github.com/ryo-aoki-pc/md-table-excel/internal/blocks"
	"github.com/ryo-aoki-pc/md-table-excel/internal/cellsyntax"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
)

// PatchPipeCells は既存の書き戻しで表現可能かを調べ、変えたセルの本文だけを差し替える。
// 空白や表示されない余分なセルを含め、ほかの文字には触れない。
func PatchPipeCells(r *Region, m *model.TableModel, warnings *[]string) ([]string, error) {
	lines, _, err := PatchPipeValues(r, m, warnings)
	return lines, err
}

// PatchPipeValues は PatchPipeCells の編集と、原文の期待値を返す。
func PatchPipeValues(r *Region, m *model.TableModel, warnings *[]string) ([]string, *Expectation, error) {
	if !m.SameShape(r.Model()) {
		return nil, nil, cellsyntax.Cannot("セルの編集では表の形を変えられない")
	}
	rendered, err := RenderPipe(r, m, warnings)
	if err != nil {
		return nil, nil, err
	}

	raws := map[model.Position]string{}
	rows := append([]*PipeRow{r.Pipe.Header}, r.Pipe.Rows...)
	for row, old := range rows {
		line := row
		if row > 0 {
			line++
		}
		fresh := blocks.ParseTableRow(strings.TrimPrefix(rendered[line], old.Prefix) + "\n")
		for col := 0; col < m.Cols; col++ {
			if m.CellAt(row, col).Text != r.Model().CellAt(row, col).Text {
				raws[model.Position{Row: row, Col: col}] = blocks.StripASCIISpace(fresh.Cells[col].Raw)
			}
		}
	}
	expect := SourceExpectation(r)
	for pos, raw := range raws {
		expect.Cells[pos] = raw
		delete(expect.Bodies, pos.Row)
	}
	lines, err := PatchPipeRaw(r, raws)
	return lines, expect, err
}

// PatchPipeRaw は PatchPipeCells から取り出した原文の差し替え。
func PatchPipeRaw(r *Region, raws map[model.Position]string) ([]string, error) {
	rows := append([]*PipeRow{r.Pipe.Header}, r.Pipe.Rows...)
	lines := make([]string, len(r.Bodies))
	for i, body := range r.Bodies {
		lines[i] = r.Prefixes[i] + body
	}
	for row, old := range rows {
		line := row
		if row > 0 {
			line++
		}
		parsed := blocks.ParseTableRow(old.Body + "\n")

		var edits []htmlEdit
		last := -1
		for col := 0; col < r.Model().Cols; col++ {
			if _, ok := raws[model.Position{Row: row, Col: col}]; !ok {
				continue
			}
			last = col
			if col >= len(parsed.Cells) {
				continue
			}
			c := parsed.Cells[col]
			start, end := c.Start, c.End
			for start < end && (old.Body[start] == ' ' || old.Body[start] == '\t') {
				start++
			}
			for end > start && (old.Body[end-1] == ' ' || old.Body[end-1] == '\t') {
				end--
			}
			text := raws[model.Position{Row: row, Col: col}]
			if start == end {
				start, end = c.Start, c.End
				for start > 0 && (old.Body[start-1] == ' ' || old.Body[start-1] == '\t') {
					start--
				}
				if r.Pipe.SpacedCells {
					text = " " + text + " "
				}
			}
			edits = append(edits, htmlEdit{start, end, text})
		}
		if last < 0 {
			continue
		}
		body := old.Body
		for i := len(edits) - 1; i >= 0; i-- {
			e := edits[i]
			body = body[:e.start] + e.replacement + body[e.end:]
		}
		if last >= len(parsed.Cells) {
			// 行に無いセルは行末へ足す。行末の縦棒の後ろの空白は持ち越さず、行末の縦棒は表の書き方に合わせる。
			open := strings.Contains(old.Body, "|")
			if n := len(parsed.Cells); n > 0 {
				c := parsed.Cells[n-1]
				open = c.End < len(old.Body) && old.Body[c.End] == '|'
			}
			var texts []string
			for col := len(parsed.Cells); col <= last; col++ {
				raw, ok := raws[model.Position{Row: row, Col: col}]
				if !ok && col < len(old.RawCells) {
					raw = old.RawCells[col]
				}
				texts = append(texts, raw)
			}
			separator, padding := "|", ""
			if r.Pipe.SpacedCells {
				separator, padding = " | ", " "
			}
			body = strings.TrimRight(body, " 	")
			switch {
			case open:
				body += padding
			case body == "":
				body += "|" + padding
			default:
				body += separator
			}
			body += strings.Join(texts, separator)
			if r.Pipe.TrailingPipe || texts[len(texts)-1] == "" {
				body += padding + "|"
			}
		}
		lines[line] = old.Prefix + body
	}
	return lines, nil
}
