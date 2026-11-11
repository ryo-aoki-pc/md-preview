package verify

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
)

// sameHoles は穴の集まりを比べる。nil と空を同じに扱い、偽の値の項目は穴に数えない。
func sameHoles(a, b map[model.Position]bool) bool {
	count := func(m map[model.Position]bool) int {
		n := 0
		for _, hole := range m {
			if hole {
				n++
			}
		}
		return n
	}
	if count(a) != count(b) {
		return false
	}
	for p, hole := range a {
		if hole && !b[p] {
			return false
		}
	}
	return true
}

// checkSources はモデルが持たない原文の属性・区画・穴を検査する。
func checkSources(r, again *table.Region, repl *table.Replacement) []string {
	e := repl.Expect
	got := table.SourceExpectation(again)
	problems := []string{}
	if r.Kind != again.Kind {
		return []string{fmt.Sprintf("表 %d: 原文の検査で表の種類が変わる", r.Number)}
	}
	if !reflect.DeepEqual(e.Cells, got.Cells) {
		// 誤りの文が毎回同じになるよう、行・列の順に比べる。期待に無いセルの位置も示す。
		positions := []model.Position{}
		for p := range e.Cells {
			positions = append(positions, p)
		}
		for p := range got.Cells {
			if _, ok := e.Cells[p]; !ok {
				positions = append(positions, p)
			}
		}
		sort.Slice(positions, func(i, j int) bool {
			a, b := positions[i], positions[j]
			return a.Row < b.Row || a.Row == b.Row && a.Col < b.Col
		})
		for _, p := range positions {
			wanted, expected := e.Cells[p]
			raw, ok := got.Cells[p]
			if !expected {
				problems = append(problems, fmt.Sprintf("表 %d: 期待していないセル（%d 行 %d 列）がある", r.Number, p.Row+1, p.Col+1))
			} else if !ok || raw != wanted {
				problems = append(problems, fmt.Sprintf("表 %d: セル（%d 行 %d 列）の原文が期待したバイトと違う", r.Number, p.Row+1, p.Col+1))
			}
		}
	}
	if r.Kind == "pipe" {
		rows := []int{}
		for row := range e.Bodies {
			rows = append(rows, row)
		}
		sort.Ints(rows)
		for _, row := range rows {
			if got.Bodies[row] != e.Bodies[row] {
				problems = append(problems, fmt.Sprintf("表 %d: %d 行目の原文が変わる", r.Number, row+1))
			}
		}
		if e.Delimiter != nil && *e.Delimiter != *got.Delimiter {
			problems = append(problems, fmt.Sprintf("表 %d: 区切り行の原文が変わる", r.Number))
		}
		return problems
	}
	if e.RowTags != nil && !reflect.DeepEqual(e.RowTags, got.RowTags) {
		problems = append(problems, fmt.Sprintf("表 %d: 行の <tr> の開始タグが変わる", r.Number))
	}
	old, new := r.HTML, again.HTML
	sectionsSame := len(old.Sections) == len(new.Sections)
	if sectionsSame {
		for i, s := range old.Sections {
			n := new.Sections[i]
			if s.Tag != n.Tag || (s.Token == nil) != (n.Token == nil) || s.Token != nil && s.Token.Text != n.Token.Text {
				sectionsSame = false
				break
			}
		}
	}
	if !sectionsSame || e.Sections != nil && !reflect.DeepEqual(e.Sections, got.Sections) {
		problems = append(problems, fmt.Sprintf("表 %d: 足した行が別の区画に入るか、区画の原文が変わる", r.Number))
	}
	if old.TableToken.Text != new.TableToken.Text || !reflect.DeepEqual(old.Caption, new.Caption) || !reflect.DeepEqual(old.Colgroups, new.Colgroups) || r.Head != again.Head || r.Tail != again.Tail {
		problems = append(problems, fmt.Sprintf("表 %d: 表の開始タグ・説明・列の指定・前後の原文が変わる", r.Number))
	}
	if repl.Model != nil && !sameHoles(repl.Model.Holes, again.Model().Holes) {
		problems = append(problems, fmt.Sprintf("表 %d: セルの無い位置が期待した形と違う", r.Number))
	}
	return problems
}
