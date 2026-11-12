package table

import (
	"sort"

	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/model"
)

// SortLocks は同じ区画の判定を使い回し、選択行ごとの拒否理由を返す。
func SortLocks(r *Region) []string {
	locks := make([]string, r.Model().Rows)
	if len(locks) == 0 {
		return locks
	}
	_, _, reason := SortRange(r, 0)
	for row := range locks {
		locks[row] = reason
	}
	if global, _ := ShapeLocks(r); global == "" && r.Kind == "html" {
		for _, s := range r.HTML.Sections {
			if len(s.Rows) == 0 {
				continue
			}
			_, _, reason := sortSectionRange(s)
			for _, row := range s.Rows {
				locks[row.Index] = reason
			}
		}
	}
	return locks
}

// SortRange は選んだ行の区画から、先頭の見出しを除いた範囲と拒否理由を返す。
func SortRange(r *Region, row int) (first, count int, reason string) {
	if reason, _ = ShapeLocks(r); reason != "" {
		return
	}
	first, count = 1, r.Model().Rows-1
	if r.Kind == "html" {
		return sortSectionRange(r.HTML.Rows[row].Section)
	}
	if count < 2 {
		reason = "並べ替える行が 2 行未満です"
	}
	return
}

func sortSectionRange(s *HTMLSection) (first, count int, reason string) {
	if s.Tag == "thead" {
		return 0, 0, "見出しの区画は並べ替えられません"
	}
	first, count = s.Rows[0].Index, len(s.Rows)
	for _, tr := range s.Rows {
		header := len(tr.Cells) > 0
		for _, c := range tr.Cells {
			header = header && c.Tag == "th"
		}
		if !header {
			break
		}
		first++
		count--
	}
	for _, tr := range s.Rows {
		for _, c := range tr.Cells {
			if c.Rowspan > 1 && c.Row < first+count && c.Row+c.Rowspan > first {
				return first, count, "行の結合があるので並べ替えられません"
			}
		}
	}
	if count < 2 {
		reason = "並べ替える行が 2 行未満です"
	}
	return
}

// PermuteRows は各位置に置く元の行番号に従い、行の原文だけを入れ替える。
func PermuteRows(r *Region, first int, rows []int) ([]string, *model.TableModel, *Expectation, error) {
	positions := make([]int, r.Model().Rows)
	for i := range positions {
		positions[i] = i
	}
	for dest, source := range rows {
		positions[source] = first + dest
	}
	index := func(i int) int { return positions[i] }
	n := remapShape(r.Model(), r.Model().Rows, true, index)
	e := remapSources(r, n.Rows, true, index)
	if r.Kind == "pipe" {
		lines := make([]string, len(r.Bodies))
		lines[1] = r.Prefixes[1] + r.Bodies[1]
		for old := range positions {
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
	t := r.HTML
	starts, position := make([]int, len(r.Bodies)), 0
	for i, body := range r.Bodies {
		starts[i] = position
		position += len(body) + 1
	}
	edits := []htmlEdit{}
	moved := map[int]int{}
	delta := 0
	for i, source := range rows {
		dest, old := t.Rows[first+i], t.Rows[source]
		start, end := old.Token.Start, old.EndToken.End
		edits = append(edits, htmlEdit{dest.Token.Start, dest.EndToken.End, t.Text[start:end]})
		for line := sort.SearchInts(starts, start); line < len(starts) && starts[line] < end; line++ {
			moved[dest.Token.Start+delta+starts[line]-start] = line
		}
		delta += end - start - (dest.EndToken.End - dest.Token.Start)
	}
	lines, err := applyHTMLEditsMapped(r, t, edits, moved)
	return lines, n, e, err
}

// PermuteRowEndings は混在した改行も行の内部と一緒に動かす。行の間の改行は元の位置に保つ。
func PermuteRowEndings(r *Region, first int, rows []int, original []string, fallback string) []string {
	if r.Kind == "pipe" {
		endings := append([]string{}, original...)
		for dest, source := range rows {
			ending := original[source+1]
			if ending == "" {
				ending = fallback
			}
			endings[first+dest+1] = ending
		}
		if original[len(original)-1] == "" {
			endings[len(endings)-1] = ""
		}
		return endings
	}
	newlines, position := []int{}, 0
	for _, body := range r.Bodies[:len(r.Bodies)-1] {
		position += len(body)
		newlines = append(newlines, position)
		position++
	}
	endings := make([]string, 0, len(original))
	copyRange := func(first, last int) {
		for i := sort.SearchInts(newlines, first); i < len(newlines) && newlines[i] < last; i++ {
			endings = append(endings, original[i])
		}
	}
	position = 0
	for dest, source := range rows {
		old, next := r.HTML.Rows[source], r.HTML.Rows[first+dest]
		copyRange(position, next.Token.Start)
		copyRange(old.Token.Start, old.EndToken.End)
		position = next.EndToken.End
	}
	copyRange(position, len(r.HTML.Text))
	return append(endings, original[len(original)-1])
}
