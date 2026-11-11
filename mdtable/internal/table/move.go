package table

import (
	"strings"

	"github.com/ryo-aoki-pc/md-table-excel/internal/blocks"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
)

// moveIndex は元の座標から並べ替え後の座標への対応。gap も元の座標。
func moveIndex(index, first, count, gap int) int {
	if gap < first {
		if index >= first && index < first+count {
			return gap + index - first
		}
		if index >= gap && index < first {
			return index + count
		}
	} else {
		if index >= first && index < first+count {
			return gap - count + index - first
		}
		if index >= first+count && index < gap {
			return index - count
		}
	}
	return index
}

// remapShape は原文と別にセル・穴の期待する位置を組み立てる。負の位置は削除。
func remapShape(m *model.TableModel, size int, rows bool, index func(int) int) *model.TableModel {
	cells := []*model.Cell{}
	holes := map[model.Position]bool{}
	position := func(p model.Position) model.Position {
		if rows {
			p.Row = index(p.Row)
		} else {
			p.Col = index(p.Col)
		}
		return p
	}
	for _, c := range m.Cells {
		p := position(model.Position{Row: c.Row, Col: c.Col})
		if p.Row < 0 || p.Col < 0 {
			continue
		}
		v := *c
		v.Row, v.Col = p.Row, p.Col
		cells = append(cells, &v)
	}
	for p := range m.Holes {
		p = position(p)
		if p.Row >= 0 && p.Col >= 0 {
			holes[p] = true
		}
	}
	height, width := m.Rows, m.Cols
	if rows {
		height = size
	} else {
		width = size
	}
	return model.New(m.Kind, height, width, cells, holes)
}

func remapSources(r *Region, size int, rows bool, index func(int) int) *Expectation {
	e := SourceExpectation(r)
	cells := map[model.Position]string{}
	for p, raw := range e.Cells {
		if rows {
			p.Row = index(p.Row)
		} else {
			p.Col = index(p.Col)
		}
		if p.Row >= 0 && p.Col >= 0 {
			cells[p] = raw
		}
	}
	e.Cells = cells
	if rows {
		if e.RowTags != nil {
			tags := make([]string, size)
			for old, tag := range e.RowTags {
				if next := index(old); next >= 0 {
					tags[next] = tag
				}
			}
			e.RowTags = tags
		}
		if e.Bodies != nil {
			bodies := map[int]string{}
			for old, body := range e.Bodies {
				if next := index(old); next >= 0 {
					bodies[next] = body
				}
			}
			e.Bodies = bodies
		}
	}
	return e
}

// removeHTMLRange は行を占める要素なら、その空白と改行も取り除く。
func removeHTMLRange(t *HTMLTable, first, last int) htmlEdit {
	start, end, _, own := ownHTMLRange(t, first, last)
	if own && end < len(t.Text) {
		end++
	} else if !own {
		start, end = first, last
	}
	return htmlEdit{start, end, ""}
}

// moveHTMLStart は直前の空白を要素と一組にする。改行のある要素と同じ行に
// 並ぶ要素を混ぜて動かしても、逆の操作で空白まで同じバイトに戻せる。
func moveHTMLStart(t *HTMLTable, first int) int {
	for first > 0 && strings.ContainsRune(" \t\n", rune(t.Text[first-1])) {
		first--
	}
	return first
}

func moveHTMLInsertion(t *HTMLTable, first, last int, before bool, ranges [][2]int) htmlEdit {
	pos := last
	if before {
		pos = moveHTMLStart(t, first)
	}
	var b strings.Builder
	for _, span := range ranges {
		b.WriteString(t.Text[moveHTMLStart(t, span[0]):span[1]])
	}
	return htmlEdit{pos, pos, b.String()}
}

type htmlMove struct {
	insertion htmlEdit
	ranges    [][2]int
}

// applyHTMLMoves は写しに含めないコンテナ接頭辞も、同じ表の移動では持ち運ぶ。
func applyHTMLMoves(r *Region, edits []htmlEdit, moves []htmlMove) ([]string, error) {
	starts := []int{}
	position := 0
	for _, body := range r.Bodies {
		starts = append(starts, position)
		position += len(body) + 1
	}
	moved := map[int]int{}
	for _, move := range moves {
		insertion := move.insertion
		position = insertion.start
		for _, edit := range edits {
			if edit.end <= insertion.start && edit != insertion {
				position += len(edit.replacement) - (edit.end - edit.start)
			}
		}
		for _, span := range move.ranges {
			start := moveHTMLStart(r.HTML, span[0])
			for index, old := range starts {
				if old >= start && old < span[1] {
					moved[position+old-start] = index
				}
			}
			position += span[1] - start
		}
	}
	return applyHTMLEditsMapped(r, r.HTML, edits, moved)
}

// MoveHTMLRows は削除と差し込みを元のバイト位置に対する一度の編集で行う。
func MoveHTMLRows(r *Region, row, count, base int, after bool) ([]string, *model.TableModel, *Expectation, error) {
	t := r.HTML
	gap := base
	if after {
		gap++
	}
	index := func(i int) int { return moveIndex(i, row, count, gap) }
	n := remapShape(t.Model, t.Model.Rows, true, index)
	e := remapSources(r, n.Rows, true, index)
	edits := []htmlEdit{}
	ranges := [][2]int{}
	for _, moved := range t.Rows[row : row+count] {
		ranges = append(ranges, [2]int{moved.Token.Start, moved.EndToken.End})
		edits = append(edits, htmlEdit{moveHTMLStart(t, moved.Token.Start), moved.EndToken.End, ""})
		for i, s := range t.Sections {
			if s == moved.Section {
				e.Sections[i]--
			}
		}
	}
	for i, s := range t.Sections {
		if s == t.Rows[base].Section {
			e.Sections[i] += count
		}
	}
	insertion := moveHTMLInsertion(t, t.Rows[base].Token.Start, t.Rows[base].EndToken.End, !after, ranges)
	edits = append(edits, insertion)
	lines, err := applyHTMLMoves(r, edits, []htmlMove{{insertion: insertion, ranges: ranges}})
	return lines, n, e, err
}

func MoveHTMLColumns(r *Region, col, count, gap int) ([]string, *model.TableModel, *Expectation, error) {
	t := r.HTML
	index := func(i int) int { return moveIndex(i, col, count, gap) }
	n := remapShape(t.Model, t.Model.Cols, false, index)
	e := remapSources(r, n.Cols, false, index)
	edits := []htmlEdit{}
	moves := []htmlMove{}
	for _, row := range t.Rows {
		ranges := [][2]int{}
		var left, right *HTMLCell
		for _, c := range row.Cells {
			if c.Col >= col && c.Col < col+count {
				ranges = append(ranges, [2]int{c.StartToken.Start, c.EndToken.End})
				edits = append(edits, htmlEdit{moveHTMLStart(t, c.StartToken.Start), c.EndToken.End, ""})
			} else if c.Col+c.Colspan <= gap {
				left = c
			} else if right == nil {
				right = c
			}
		}
		if len(ranges) == 0 {
			continue
		}
		if left != nil {
			insertion := moveHTMLInsertion(t, left.StartToken.Start, left.EndToken.End, false, ranges)
			edits = append(edits, insertion)
			moves = append(moves, htmlMove{insertion: insertion, ranges: ranges})
		} else if right != nil {
			insertion := moveHTMLInsertion(t, right.StartToken.Start, right.EndToken.End, true, ranges)
			edits = append(edits, insertion)
			moves = append(moves, htmlMove{insertion: insertion, ranges: ranges})
		} else {
			// 他のセルが上からの結合だけの行は、原文を動かさずに位置が決まる。
			edits = edits[:len(edits)-len(ranges)]
		}
	}
	lines, err := applyHTMLMoves(r, edits, moves)
	return lines, n, e, err
}

func MovePipeRows(r *Region, row, count, gap int) ([]string, *model.TableModel, *Expectation, error) {
	index := func(i int) int { return moveIndex(i, row, count, gap) }
	n := remapShape(r.Model(), r.Model().Rows, true, index)
	e := remapSources(r, n.Rows, true, index)
	lines := make([]string, len(r.Bodies))
	lines[1] = r.Prefixes[1] + r.Bodies[1]
	for old := 0; old < n.Rows; old++ {
		source, dest := old, index(old)
		if source > 0 {
			source++
		}
		if dest > 0 {
			dest++
		}
		lines[dest] = r.Prefixes[source] + r.Bodies[source]
	}
	return lines, n, e, nil
}

// permutePipeBody はセルの原文を入れ替え、縦棒と各位置の空白はそのまま残す。
// 短い行は不足を補い、見出しより多いセルは列の操作の既存の規則で落とす。
func permutePipeBody(body string, size int, index func(int) int, format *PipeTable) string {
	parsed := blocks.ParseTableRow(body + "\n")
	if len(parsed.Cells) != size {
		raws := make([]string, size)
		for old, c := range parsed.Cells {
			if old < size {
				raws[index(old)] = blocks.StripASCIISpace(c.Raw)
			}
		}
		return formatPiecePipeRow(raws, format)
	}
	raws := make([]string, size)
	for old, c := range parsed.Cells {
		raws[index(old)] = blocks.StripASCIISpace(c.Raw)
	}
	for col := size - 1; col >= 0; col-- {
		c := parsed.Cells[col]
		start, end := c.Start, c.End
		for start < end && (body[start] == ' ' || body[start] == '\t') {
			start++
		}
		for end > start && (body[end-1] == ' ' || body[end-1] == '\t') {
			end--
		}
		body = body[:start] + raws[col] + body[end:]
	}
	return body
}

func MovePipeColumns(r *Region, col, count, gap int) ([]string, *model.TableModel, *Expectation, error) {
	index := func(i int) int { return moveIndex(i, col, count, gap) }
	n := remapShape(r.Model(), r.Model().Cols, false, index)
	e := remapSources(r, n.Cols, false, index)
	lines := make([]string, len(r.Bodies))
	for line, body := range r.Bodies {
		format := *r.Pipe
		if line == 1 {
			format.SpacedCells = format.DelimiterSpaced
		}
		body = permutePipeBody(body, n.Cols, index, &format)
		lines[line] = r.Prefixes[line] + body
		if line == 1 {
			e.Delimiter = &body
		} else {
			row := line
			if row > 0 {
				row--
			}
			e.Bodies[row] = body
		}
	}
	return lines, n, e, nil
}

// DeletePieceBlock は結合を切らない範囲を削除し、残る原文の期待値も返す。
func DeletePieceBlock(r *Region, first, count int, rows bool) ([]string, *model.TableModel, *Expectation, error) {
	size := r.Model().Cols
	if rows {
		size = r.Model().Rows
	}
	index := func(i int) int {
		if i >= first && i < first+count {
			return -1
		}
		if i >= first+count {
			return i - count
		}
		return i
	}
	n := remapShape(r.Model(), size-count, rows, index)
	e := remapSources(r, size-count, rows, index)
	if r.Kind == "html" {
		if rows {
			for _, row := range r.HTML.Rows[first : first+count] {
				for i, s := range r.HTML.Sections {
					if s == row.Section {
						e.Sections[i]--
					}
				}
			}
			lines, _, err := DeleteHTMLRowBlock(r, first, count)
			return lines, n, e, err
		}
		edits := []htmlEdit{}
		for _, c := range r.HTML.Cells {
			if c.Col >= first && c.Col < first+count {
				edits = append(edits, removeHTMLRange(r.HTML, c.StartToken.Start, c.EndToken.End))
			}
		}
		lines, err := applyHTMLEdits(r, r.HTML, edits)
		return lines, n, e, err
	}
	if rows {
		lines := []string{}
		for line, body := range r.Bodies {
			if line <= first || line > first+count {
				lines = append(lines, r.Prefixes[line]+body)
			}
		}
		return lines, n, e, nil
	}
	alter := func(raw []string) []string { return append(append([]string{}, raw[:first]...), raw[first+count:]...) }
	lines, expect := rebuildPipeColumns(r, n, func(_ int, raw []string) []string { return alter(raw) }, alter(pipeDelimiterCells(r)))
	return lines, n, expect, nil
}
