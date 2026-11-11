package table

import (
	"fmt"
	"github.com/ryo-aoki-pc/md-table-excel/internal/cellsyntax"
	"github.com/ryo-aoki-pc/md-table-excel/internal/htmltok"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"sort"
	"strings"
)

// html_table._inner_sources に対応する元の中身の最初の対応。
func innerSources(t *HTMLTable) map[string]string {
	out := map[string]string{}
	if t == nil {
		return out
	}
	for _, c := range t.Cells {
		if _, ok := out[c.Text]; !ok {
			out[c.Text] = t.Text[c.ContentStart:c.ContentEnd]
		}
	}
	return out
}

// html_table._new_inner に対応する元の中身の再利用と新しい中身。
func reusableInner(text string, reuse map[string]string, warnings *[]string, row, col int) (string, bool, bool, error) {
	if s, ok := reuse[text]; ok {
		return s, false, true, nil
	}
	out, e := cellsyntax.ToHTMLCell(text)
	if e != nil {
		return "", false, false, &cellsyntax.NotRepresentable{Message: e.Error(), Row: row + 1, Col: col + 1}
	}
	*warnings = append(*warnings, out.Warnings...)
	if out.Kind == "inline" {
		return out.Text, false, false, nil
	}
	return "\n\n" + strings.Join(out.Lines, "\n") + "\n\n", true, false, nil
}

type htmlEdit struct {
	start, end  int
	replacement string
}

// html_table._apply_edits に対応する元の行の接頭辞を保った差し替え。
func applyHTMLEdits(r *Region, t *HTMLTable, edits []htmlEdit) ([]string, error) {
	text := t.Text
	var lineStarts []int
	position := 0
	for _, body := range r.Bodies {
		lineStarts = append(lineStarts, position)
		position += len(body) + 1
	}
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].start != edits[j].start {
			return edits[i].start < edits[j].start
		}
		return edits[i].end < edits[j].end
	})
	for i := 1; i < len(edits); i++ {
		if edits[i].start < edits[i-1].end {
			return nil, cellsyntax.Cannot("差し替える範囲が重なる")
		}
	}
	survivors := map[int]int{}
	for index, start := range lineStarts {
		if index == 0 {
			continue
		}
		inside := false
		for _, e := range edits {
			if e.start <= start && start < e.end || e.start == start && start == e.end {
				inside = true
				break
			}
		}
		if !inside {
			delta := 0
			for _, e := range edits {
				if e.end <= start {
					delta += len(e.replacement) - (e.end - e.start)
				}
			}
			survivors[start+delta] = index
		}
	}
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		text = text[:e.start] + e.replacement + text[e.end:]
	}
	var lines []string
	position = 0
	for index, body := range strings.Split(text, "\n") {
		prefix := ""
		if index == 0 {
			prefix = r.Prefixes[0]
		} else if old, ok := survivors[position]; ok {
			prefix = r.Prefixes[old]
		} else if body != "" {
			prefix = r.ContPrefix
		} else {
			prefix = r.BlankPrefix()
		}
		lines = append(lines, prefix+body)
		position += len(body) + 1
	}
	return lines, nil
}

// html_table._patch に対応する形を変えないセルの差し替え。
func patchHTML(r *Region, m *model.TableModel, warnings *[]string) ([]string, error) {
	t := r.HTML
	reuse := innerSources(t)
	var edits []htmlEdit
	anchors := m.Anchors()
	for _, cell := range t.Cells {
		new := anchors[model.Position{Row: cell.Row, Col: cell.Col}]
		oldAlign, byStyle := cellAlign(cell)
		tag := "td"
		if new.Header {
			tag = "th"
		}
		if tag != cell.Tag || new.Align != oldAlign {
			start := startTag(cell.StartToken.Attributes, tag, new.Align, t.AlignStyle || byStyle, 0, 0, true)
			edits = append(edits, htmlEdit{cell.StartToken.Start, cell.StartToken.End, start})
			if cell.EndToken != nil && tag != cell.Tag {
				edits = append(edits, htmlEdit{cell.EndToken.Start, cell.EndToken.End, "</" + tag + ">"})
			}
		}
		if new.Text != cell.Text {
			inner, block, reused, e := reusableInner(new.Text, reuse, warnings, new.Row, new.Col)
			if e != nil {
				return nil, e
			}
			block = block || reused && strings.Contains(inner, "\n")
			start, end := cell.ContentStart, cell.ContentEnd
			if block {
				for end < len(t.Text) && strings.ContainsRune(" \t\n", rune(t.Text[end])) {
					end++
				}
				if !strings.HasSuffix(inner, "\n") {
					inner += "\n"
				}
			}
			edits = append(edits, htmlEdit{start, end, inner})
		}
	}
	return applyHTMLEdits(r, t, edits)
}

// html_table._row_signature に対応する衝突しないセル列の符号化。
func rowSignature(cells []*model.Cell, row int) string {
	var values []any
	for _, c := range cells {
		if c.Row == row {
			var align any
			if c.Align != "" {
				align = c.Align
			}
			values = append(values, []any{c.Col, c.Rowspan, c.Colspan, c.Header, align, c.Text})
		}
	}
	if values == nil {
		values = []any{}
	}
	return pycompat.JSONDumps(values, false)
}

type sectionKey struct {
	name  string
	index int
}

// html_table._sections_for に対応する新しい行の区画と元の行の対応。
func sectionsFor(m *model.TableModel, t *HTMLTable) ([]sectionKey, []*HTMLRow) {
	rows := m.Rows
	mapping := make([]*HTMLRow, rows)
	if t == nil {
		sections := make([]sectionKey, rows)
		for i := range sections {
			name := "tbody"
			if i == 0 {
				name = "thead"
			}
			sections[i] = sectionKey{name: name}
		}
		return sections, mapping
	}
	oldRows := t.Rows
	oldSig := make([]string, len(oldRows))
	newSig := make([]string, rows)
	for i, row := range oldRows {
		oldSig[i] = rowSignature(t.Model.Cells, row.Index)
	}
	for row := 0; row < rows; row++ {
		newSig[row] = rowSignature(m.Cells, row)
	}
	for _, op := range pycompat.Opcodes(oldSig, newSig) {
		if op.Tag == "equal" || op.Tag == "replace" {
			for k := 0; k < min(op.I2-op.I1, op.J2-op.J1); k++ {
				mapping[op.J1+k] = oldRows[op.I1+k]
			}
		}
	}
	sections := make([]sectionKey, rows)
	previous := -1
	for row := 0; row < rows; row++ {
		index := 0
		if old := mapping[row]; old != nil {
			for i, s := range t.Sections {
				if s == old.Section {
					index = i
					break
				}
			}
		} else if previous >= 0 {
			index = previous
		} else {
			for i, s := range t.Sections {
				if s.Tag == "tbody" {
					index = i
					break
				}
			}
		}
		sections[row] = sectionKey{index: index}
		previous = index
	}
	changed := true
	for changed {
		changed = false
		for _, c := range m.Cells {
			first := sections[c.Row]
			for row := c.Row + 1; row < c.Row+c.Rowspan; row++ {
				if sections[row] != first {
					sections[row] = first
					changed = true
				}
			}
		}
	}
	return sections, mapping
}

// html_table._original_cell に対応する同じ列・同じ行の中身・同じ中身の探索。
func originalHTMLCell(t *HTMLTable, mapping []*HTMLRow, c *model.Cell) *HTMLCell {
	if t == nil {
		return nil
	}
	if old := mapping[c.Row]; old != nil {
		for _, cell := range old.Cells {
			if cell.Col == c.Col {
				return cell
			}
		}
		for _, cell := range old.Cells {
			if cell.Text == c.Text && c.Text != "" {
				return cell
			}
		}
	}
	if c.Text != "" {
		for _, cell := range t.Cells {
			if cell.Text == c.Text {
				return cell
			}
		}
	}
	return nil
}

// html_table._regenerate に対応する形が変わった表の生成。
func regenerateHTML(r *Region, m *model.TableModel, warnings *[]string, fromPipe bool) ([]string, error) {
	var t *HTMLTable
	if !fromPipe {
		t = r.HTML
	}
	reuse := innerSources(t)
	alignStyle := t != nil && t.AlignStyle
	var lines []string
	if t != nil {
		lines = append(lines, t.TableToken.Text)
		if t.Caption != nil && *t.Caption != "" {
			lines = append(lines, strings.Split(*t.Caption, "\n")...)
		}
		if len(t.Colgroups) > 0 && m.Cols == t.Model.Cols {
			for _, colgroup := range t.Colgroups {
				lines = append(lines, strings.Split(colgroup, "\n")...)
			}
		}
	} else {
		lines = append(lines, "<table>")
	}
	sections, mapping := sectionsFor(m, t)
	explicit := fromPipe || t != nil && t.ExplicitSections
	if fromPipe && m.Rows == 1 {
		sections = []sectionKey{{name: "thead"}}
	}
	type group struct {
		key  sectionKey
		rows []int
	}
	var groups []group
	for row := 0; row < m.Rows; row++ {
		key := sections[row]
		if len(groups) > 0 && groups[len(groups)-1].key == key {
			groups[len(groups)-1].rows = append(groups[len(groups)-1].rows, row)
		} else {
			groups = append(groups, group{key, []int{row}})
		}
	}
	for _, g := range groups {
		closeTag := ""
		if explicit {
			openTag := ""
			if g.key.name != "" {
				openTag, closeTag = "<"+g.key.name+">", "</"+g.key.name+">"
			} else {
				s := t.Sections[g.key.index]
				openTag = "<" + s.Tag + ">"
				if s.Token != nil {
					openTag = s.Token.Text
				}
				closeTag = "</" + s.Tag + ">"
			}
			lines = append(lines, openTag)
		}
		for _, row := range g.rows {
			old := mapping[row]
			if old != nil && old.Token != nil && old.EndToken != nil && rowSignature(t.Model.Cells, old.Index) == rowSignature(m.Cells, row) {
				lines = append(lines, strings.Split(t.Text[old.Token.Start:old.EndToken.End], "\n")...)
				continue
			}
			openRow := "<tr>"
			if old != nil && old.Token != nil {
				openRow = old.Token.Text
			}
			lines = append(lines, openRow)
			var anchors []*model.Cell
			for _, c := range m.Cells {
				if c.Row == row {
					anchors = append(anchors, c)
				}
			}
			sort.SliceStable(anchors, func(i, j int) bool { return anchors[i].Col < anchors[j].Col })
			for _, c := range anchors {
				tag := "td"
				if c.Header {
					tag = "th"
				}
				if fromPipe {
					tag = "td"
					if c.Row == 0 {
						tag = "th"
					}
				}
				original := originalHTMLCell(t, mapping, c)
				var attrs []htmltok.Attribute
				if original != nil {
					attrs = original.StartToken.Attributes
				}
				if original != nil && original.Text != c.Text {
					var extra, kept []htmltok.Attribute
					for _, a := range attrs {
						if a.Name != "rowspan" && a.Name != "colspan" && a.Name != "align" && a.Name != "style" {
							extra = append(extra, a)
						}
						if a.Name == "style" {
							kept = append(kept, a)
						}
					}
					if len(extra) > 0 {
						var parts []string
						for _, a := range extra {
							parts = append(parts, a.Raw)
						}
						*warnings = append(*warnings, fmt.Sprintf("中身を変えたセル（%d 行 %d 列）の HTML の属性（%s）は引き継がない", c.Row+1, c.Col+1, strings.Join(parts, " ")))
					}
					attrs = kept
				}
				start := startTag(attrs, tag, c.Align, alignStyle, c.Rowspan, c.Colspan, false)
				cellReuse := reuse
				if fromPipe {
					cellReuse = nil
				}
				inner, _, _, e := reusableInner(c.Text, cellReuse, warnings, c.Row, c.Col)
				if e != nil {
					return nil, e
				}
				lines = append(lines, strings.Split(start+inner+"</"+tag+">", "\n")...)
			}
			lines = append(lines, "</tr>")
		}
		if explicit {
			lines = append(lines, closeTag)
		}
	}
	lines = append(lines, "</table>")
	lines[0] = r.Head + lines[0]
	lines[len(lines)-1] += r.Tail
	var result []string
	for i, line := range lines {
		prefix := r.ContPrefix
		if i == 0 {
			prefix = r.FirstPrefix()
		} else if line == "" {
			prefix = r.BlankPrefix()
		}
		result = append(result, prefix+line)
	}
	return result, nil
}

// html_table.render に対応する差し替えと作り直しの選択。
func RenderHTML(r *Region, m *model.TableModel, warnings *[]string) ([]string, error) {
	t := r.HTML
	if m.SameShape(t.Model) {
		return patchHTML(r, m, warnings)
	}
	if t.Interstitial {
		return nil, cellsyntax.Cannot("表の構造の間にコメントなどがあるので、行・列・結合は変えられない")
	}
	return regenerateHTML(r, m, warnings, false)
}
