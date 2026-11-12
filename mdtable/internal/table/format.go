package table

import (
	"sort"
	"strings"

	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/blocks"
	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/model"
)

// SetPipeAlign は選んだ列の区切りだけを変え、ハイフンの数と空白を保つ。
func SetPipeAlign(r *Region, col, cols int, align string) ([]string, *model.TableModel, *Expectation) {
	n, e := copyShape(r.Model()), SourceExpectation(r)
	body := r.Pipe.DelimiterBody
	cells := blocks.ParseTableRow(body + "\n").Cells
	for x := col + cols - 1; x >= col; x-- {
		c := cells[x]
		spec := blocks.StripASCIISpace(c.Raw)
		start := c.Start + strings.Index(c.Raw, spec)
		next := strings.Repeat("-", strings.Count(spec, "-"))
		if align == "left" || align == "center" {
			next = ":" + next
		}
		if align == "right" || align == "center" {
			next += ":"
		}
		body = body[:start] + next + body[start+len(spec):]
	}
	for _, c := range n.Cells {
		if c.Col >= col && c.Col < col+cols {
			c.Align = align
		}
	}
	lines := make([]string, len(r.Bodies))
	for i, body := range r.Bodies {
		lines[i] = r.Prefixes[i] + body
	}
	lines[1] = r.Pipe.DelimiterPrefix + body
	e.Delimiter = &body
	return lines, n, e
}

// styleAlign は引用符・括弧・コメント内のセミコロンを区切りとせず、ほかの宣言を保つ。
func styleAlign(raw, align string, add bool) (string, bool) {
	var out strings.Builder
	start, depth := 0, 0
	var quote byte
	comment, found := false, false
	appendPart := func(end int, semicolon bool) {
		part := raw[start:end]
		trimmed := strings.TrimSpace(part)
		colon := strings.IndexByte(trimmed, ':')
		if colon >= 0 && strings.EqualFold(strings.TrimSpace(trimmed[:colon]), "text-align") {
			found = true
			if align == "" {
				return
			}
			leading := len(part) - len(strings.TrimLeft(part, " \t\n\r\f"))
			out.WriteString(part[:leading])
			out.WriteString("text-align: " + align)
		} else {
			out.WriteString(part)
		}
		if semicolon {
			out.WriteByte(';')
		}
	}
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if comment {
			if ch == '*' && i+1 < len(raw) && raw[i+1] == '/' {
				comment = false
				i++
			}
			continue
		}
		if ch == '\\' {
			i++
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case '/':
			if i+1 < len(raw) && raw[i+1] == '*' {
				comment = true
				i++
			}
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ';':
			if depth == 0 {
				appendPart(i, true)
				start = i + 1
			}
		}
	}
	appendPart(len(raw), false)
	value := out.String()
	if !found && add && align != "" {
		if strings.TrimSpace(value) != "" {
			if !strings.HasSuffix(strings.TrimSpace(value), ";") {
				value += ";"
			}
			value += " "
		}
		value += "text-align: " + align
	}
	return value, found
}

func removeAttribute(raw string, a attributeRange) htmlEdit {
	start := a.start
	for start > 0 && strings.ContainsRune(" \t\n\r\f", rune(raw[start-1])) {
		start--
	}
	return htmlEdit{start, a.end, ""}
}

// attributeValueEdit は引用符があれば保ち、空白を含む非引用の値にだけ引用符を足す。
func attributeValueEdit(a attributeRange, value string) htmlEdit {
	if a.valueStart < 0 {
		return htmlEdit{a.end, a.end, "=\"" + value + "\""}
	}
	if a.valueEnd == a.end && strings.ContainsAny(value, " \t\n\r\f") {
		value = "\"" + value + "\""
	}
	return htmlEdit{a.valueStart, a.valueEnd, value}
}

func alignedCellTag(c *HTMLCell, align string, byStyle bool) (string, []htmlEdit) {
	raw := c.StartToken.Text
	attrs := attributeRanges(c.StartToken)
	hasAlign, hasStyleAlign := false, false
	for _, a := range attrs {
		if a.name == "align" {
			hasAlign = true
		}
		if a.name == "style" && a.valueStart >= 0 {
			_, found := styleAlign(raw[a.valueStart:a.valueEnd], align, false)
			hasStyleAlign = hasStyleAlign || found
		}
	}
	var edits []htmlEdit
	styleAdded := false
	for _, a := range attrs {
		switch a.name {
		case "align":
			if align == "" {
				edits = append(edits, removeAttribute(raw, a))
			} else {
				edits = append(edits, attributeValueEdit(a, align))
			}
		case "style":
			value := ""
			if a.valueStart >= 0 {
				value = raw[a.valueStart:a.valueEnd]
			}
			add := !hasAlign && !hasStyleAlign && byStyle && !styleAdded
			next, found := styleAlign(value, align, add)
			if !found && !(add && align != "") {
				continue
			}
			styleAdded = true
			if strings.Trim(next, " \t\n\r\f;") == "" {
				edits = append(edits, removeAttribute(raw, a))
			} else if next != value {
				edits = append(edits, attributeValueEdit(a, next))
			}
		}
	}
	if align != "" && !hasAlign && !hasStyleAlign && !styleAdded {
		attr := " align=\"" + align + "\""
		if byStyle {
			attr = " style=\"text-align: " + align + "\""
		}
		edits = append(edits, htmlEdit{1 + len(c.Tag), 1 + len(c.Tag), attr})
	}
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		raw = raw[:e.start] + e.replacement + raw[e.end:]
	}
	return raw, edits
}

// SetHTMLFormat は選んだセルの属性かタグ名だけを変え、モデルと原文の期待値を作る。
// 範囲と CellLocks は呼び出し側で検査済みとする。
func SetHTMLFormat(r *Region, row, col, rows, cols int, align string, header *bool) ([]string, *model.TableModel, *Expectation, error) {
	t := r.HTML
	n, e := copyShape(t.Model), SourceExpectation(r)
	var edits []htmlEdit
	for _, c := range n.Cells {
		if c.Row < row || c.Row >= row+rows || c.Col < col || c.Col >= col+cols {
			continue
		}
		pos := model.Position{Row: c.Row, Col: c.Col}
		h := t.CellAt[pos]
		start, end := h.StartToken.Text, h.EndToken.Text
		if header == nil {
			var local []htmlEdit
			start, local = alignedCellTag(h, align, t.AlignStyle)
			for _, edit := range local {
				edit.start += h.StartToken.Start
				edit.end += h.StartToken.Start
				edits = append(edits, edit)
			}
			c.Align = align
		} else {
			tag := "td"
			if *header {
				tag = "th"
			}
			if c.Header != *header {
				start = start[:1] + tag + start[3:]
				end = end[:2] + tag + end[4:]
				edits = append(edits, htmlEdit{h.StartToken.Start + 1, h.StartToken.Start + 3, tag},
					htmlEdit{h.EndToken.Start + 2, h.EndToken.Start + 4, tag})
			}
			c.Header = *header
		}
		e.Cells[pos] = start + t.Text[h.StartToken.End:h.EndToken.Start] + end
	}
	lines, err := applyHTMLEdits(r, t, edits)
	return lines, n, e, err
}
