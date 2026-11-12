// Package cellsyntax はセルの Markdown と表の記法を相互に変換する。
package cellsyntax

import (
	"fmt"
	"github.com/ryo-aoki-pc/md-table-excel/internal/blocks"
	"github.com/ryo-aoki-pc/md-table-excel/internal/inline"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"regexp"
	"strings"
)

type NotRepresentable struct {
	Message  string
	Row, Col int
}

// cell_syntax.NotRepresentable の str(error) に対応するエラーの文。
func (e *NotRepresentable) Error() string { return e.Message }

// cell_syntax.NotRepresentable.__init__ に対応する変換不能の理由。
func Cannot(s string) *NotRepresentable { return &NotRepresentable{Message: s} }

var trailingBR = regexp.MustCompile(`(?i)(?:<br[ \t]*/?>)[ \t]*(?:\n)?$`)
var taskMarker = regexp.MustCompile(`^\[[ xX]\](?:[ \t]|\n?$)`)
var relaxedTask = regexp.MustCompile(`^\[([^\]` + pycompat.ReSpace + `])\](?:[ \t]|\n?$)`)
var plainForbidden = regexp.MustCompile("[\\\\`*_~\\[\\]<>&$]")
var Structural = map[string]bool{"table": true, "caption": true, "colgroup": true, "col": true, "thead": true, "tbody": true, "tfoot": true, "tr": true, "td": true, "th": true}
var htmlEnds = map[int]*regexp.Regexp{1: regexp.MustCompile(`(?i)</(?:script|pre|textarea|style)>`), 2: regexp.MustCompile(`-->`), 3: regexp.MustCompile(`\?>`), 4: regexp.MustCompile(`>`), 5: regexp.MustCompile(`\]\]>`)}

// cell_syntax.parse に対応するセルのブロック解析。
func Parse(s string) *blocks.Document { return blocks.ParseText(s, false, true) }

// cell_syntax.normalize_list_spacing に対応する最外側のリストの前の空行の除去。
func NormalizeListSpacing(text string) string {
	if !strings.Contains(text, "\n\n") {
		return text
	}
	doc := Parse(text)
	removed := map[int]bool{}
	children := doc.Root.Children
	for i := 1; i < len(children); i++ {
		previous, node := children[i-1], children[i]
		if previous.Kind != blocks.Paragraph || node.Kind != blocks.List {
			continue
		}
		line := doc.Lines[node.StartLine]
		offset := len(line) - len(strings.TrimLeft(line, " \t"))
		if _, _, ok := blocks.ParseListMarker(line, offset, true); !ok {
			continue
		}
		blank := true
		for n := previous.EndLine + 1; n < node.StartLine; n++ {
			if strings.Trim(doc.Lines[n], " \t") != "" {
				blank = false
				break
			}
		}
		if blank {
			for n := previous.EndLine + 1; n < node.StartLine; n++ {
				removed[n] = true
			}
		}
	}
	var result []string
	for n, line := range strings.Split(text, "\n") {
		if !removed[n] {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}

// cell_syntax.task_marker に対応する通常・拡張のタスクの印の判定。
func TaskMarker(text string) bool {
	return taskMarker.MatchString(text) || relaxedTask.MatchString(text)
}

// cell_syntax._paragraph_lines に対応する段落の行。
func paragraphLines(d *blocks.Document, n *blocks.Node) []string {
	var out []string
	for _, l := range n.Lines {
		out = append(out, d.Lines[l.Number][l.Offset:])
	}
	return out
}

// cell_syntax._trailing_backslashes に対応する末尾の数。
func trailingBackslashes(s string) int { return len(s) - len(strings.TrimRight(s, "\\")) }

// cell_syntax.strip_break_marker に対応する改行マーク除去。
func StripBreakMarker(s string) (string, bool) {
	stripped := strings.TrimRight(s, " ")
	if len(s)-len(stripped) >= 2 {
		return stripped, true
	}
	if trailingBackslashes(stripped)%2 == 1 {
		return strings.TrimRight(stripped[:len(stripped)-1], " \t"), true
	}
	if m := trailingBR.FindStringIndex(stripped); m != nil && !inline.PositionInSpans(m[0], inline.AtomSpans(stripped)) {
		return strings.TrimRight(stripped[:m[0]], " \t"), true
	}
	return strings.TrimRight(stripped, " \t"), false
}

// cell_syntax._join_paragraph に対応する段落の連結。
func joinParagraph(lines []string, breakText string) string {
	if len(lines) == 0 {
		return ""
	}
	lines = append([]string{}, lines...)
	for i := 1; i < len(lines); i++ {
		lines[i] = strings.TrimLeft(lines[i], " \t")
	}
	spans := inline.AtomSpans(strings.Join(lines, "\n"))
	position := 0
	var unescaped []string
	for _, line := range lines {
		if inline.PositionInSpans(position, spans) {
			unescaped = append(unescaped, line)
		} else {
			unescaped = append(unescaped, inline.UnescapeLineStart(line))
		}
		position += len(line) + 1
	}
	joined := strings.Join(unescaped, "\n")
	spans = inline.AtomSpans(joined)
	var out strings.Builder
	pos := 0
	for {
		newline := strings.IndexByte(joined[pos:], '\n')
		if newline < 0 {
			out.WriteString(joined[pos:])
			break
		}
		newline += pos
		segment := joined[pos:newline]
		if inline.PositionInSpans(newline, spans) {
			out.WriteString(segment + " ")
		} else {
			segment, _ = StripBreakMarker(segment)
			if trailingBackslashes(segment)%2 == 1 {
				segment += " "
			}
			out.WriteString(segment + breakText)
		}
		pos = newline + 1
	}
	return strings.Trim(out.String(), " \t")
}

// cell_syntax.escape_pipes に対応するパイプのエスケープ。
func EscapePipes(s string) (string, error) {
	spans := inline.AtomSpans(s)
	var out strings.Builder
	for i, ch := range s {
		if ch == '|' {
			count := 0
			for i-count-1 >= 0 && s[i-count-1] == '\\' {
				count++
			}
			if count%2 == 0 {
				out.WriteString("\\|")
				continue
			}
			if inline.PositionInSpans(i, spans) {
				return "", Cannot("コードの中の「\\|」はパイプ表のセルに書けない")
			}
		}
		out.WriteRune(ch)
	}
	return out.String(), nil
}

// cell_syntax._blank_between に対応する空行判定。
func blankBetween(d *blocks.Document, a, b *blocks.Node) bool {
	for i := a.EndLine + 1; i < b.StartLine; i++ {
		if strings.Trim(d.Lines[i], " \t>") == "" {
			return true
		}
	}
	return false
}

// cell_syntax._render_pipe_blocks と _render_item_content に対応する子の連結。
func renderPipeBlocks(d *blocks.Document, nodes []*blocks.Node) (string, error) {
	var out strings.Builder
	var previous *blocks.Node
	for _, n := range nodes {
		piece, e := renderPipeBlock(d, n)
		if e != nil {
			return "", e
		}
		if previous != nil {
			if previous.Kind == blocks.List || previous.Kind == blocks.DescriptionList || n.Kind == blocks.List || n.Kind == blocks.DescriptionList {
			} else if blankBetween(d, previous, n) {
				out.WriteString("<br><br>")
			} else {
				out.WriteString("<br>")
			}
		}
		out.WriteString(piece)
		previous = n
	}
	return out.String(), nil
}

// cell_syntax._render_pipe_block に対応するブロックの変換。
func renderPipeBlock(d *blocks.Document, n *blocks.Node) (string, error) {
	switch n.Kind {
	case blocks.Paragraph:
		return joinParagraph(paragraphLines(d, n), "<br>"), nil
	case blocks.HTMLBlock:
		var lines []string
		for _, l := range n.Lines {
			if s := strings.Trim(d.Lines[l.Number][l.Offset:], " \t"); s != "" {
				lines = append(lines, s)
			}
		}
		return strings.Join(lines, "<br>"), nil
	case blocks.List:
		open, close := "<ul>", "</ul>"
		if n.Info.Type == "ordered" {
			open, close = "<ol>", "</ol>"
			if n.Info.Start != 1 {
				open = fmt.Sprintf(`<ol start="%d">`, n.Info.Start)
			}
		}
		var b strings.Builder
		b.WriteString(open)
		for _, item := range n.Children {
			if len(item.Children) > 0 && item.Children[0].Kind == blocks.Paragraph {
				first := paragraphLines(d, item.Children[0])[0]
				if taskMarker.MatchString(first) {
					return "", Cannot("タスクリストはパイプ表のセルに書けない")
				}
			}
			piece, e := renderPipeBlocks(d, item.Children)
			if e != nil {
				return "", e
			}
			if len(item.Children) > 0 && item.Children[0].Kind == blocks.Paragraph {
				first := paragraphLines(d, item.Children[0])[0]
				if strings.HasPrefix(first, "\\[") && TaskMarker(first[1:]) {
					piece = piece[1:]
				}
			}
			b.WriteString("<li>" + piece + "</li>")
		}
		b.WriteString(close)
		return b.String(), nil
	case blocks.DescriptionList:
		var b strings.Builder
		b.WriteString("<dl>")
		for _, item := range n.Children {
			for _, child := range item.Children {
				if child.Kind == blocks.DescriptionTerm {
					text := ""
					if len(child.Children) > 0 {
						text = joinParagraph(paragraphLines(d, child.Children[0]), "<br>")
					}
					b.WriteString("<dt>" + text + "</dt>")
				} else if child.Kind == blocks.DescriptionDetails {
					piece, e := renderPipeBlocks(d, child.Children)
					if e != nil {
						return "", e
					}
					b.WriteString("<dd>" + piece + "</dd>")
				}
			}
		}
		b.WriteString("</dl>")
		return b.String(), nil
	}
	names := map[string]string{blocks.CodeBlock: "コードブロック", blocks.Heading: "見出し", blocks.BlockQuote: "引用", blocks.Alert: "アラート", blocks.MultilineBlockQuote: "引用", blocks.ThematicBreak: "区切り線", blocks.Table: "表", blocks.FootnoteDefinition: "脚注の定義"}
	name := names[n.Kind]
	if name == "" {
		name = n.Kind
	}
	return "", Cannot(name + "はパイプ表のセルに書けない")
}

// cell_syntax.to_pipe_cell に対応するパイプ表への変換。
func ToPipeCell(s string) (string, error) {
	if pycompat.Strip(s) == "" {
		return "", nil
	}
	d := Parse(s)
	rendered, e := renderPipeBlocks(d, d.Root.Children)
	if e != nil {
		return "", e
	}
	rendered, e = EscapePipes(rendered)
	return strings.Trim(rendered, " \t"), e
}

// cell_syntax.is_plain に対応する書式なしの判定。
func IsPlain(s string) bool {
	if s == "" || plainForbidden.MatchString(s) {
		return false
	}
	for _, line := range strings.Split(s, "\n") {
		if line == "" || line != strings.Trim(line, " \t\u3000") || inline.EscapeLineStart(line) != line {
			return false
		}
	}
	return true
}

// cell_syntax._paragraph_like に対応する段落に似た節。
func paragraphLike(d *blocks.Document) []*blocks.Node {
	var out []*blocks.Node
	for _, n := range d.Root.Walk() {
		if n.Kind == blocks.Paragraph || n.Kind == blocks.Heading && n.Info.Setext {
			out = append(out, n)
		}
	}
	return out
}

type lineBreak struct {
	Line           int
	InAtom, Marked bool
}

// cell_syntax._line_breaks に対応する改行情報。
func lineBreaks(d *blocks.Document, n *blocks.Node) []lineBreak {
	parts := paragraphLines(d, n)
	spans := inline.AtomSpans(strings.Join(parts, "\n"))
	var result []lineBreak
	position := 0
	for i := 0; i < len(parts)-1; i++ {
		position += len(parts[i])
		line := n.Lines[i].Number
		_, marked := StripBreakMarker(d.Lines[line])
		result = append(result, lineBreak{line, inline.PositionInSpans(position, spans), marked})
		position++
	}
	return result
}

// cell_syntax._structural_tags_outside に対応する構造タグ判定。
func structuralTagsOutside(d *blocks.Document) bool {
	code := map[int]bool{}
	for _, n := range d.Root.Walk() {
		if n.Kind == blocks.CodeBlock {
			for i := n.StartLine; i <= n.EndLine; i++ {
				code[i] = true
			}
		}
	}
	rest := make([]string, len(d.Lines))
	for i, line := range d.Lines {
		if !code[i] {
			rest[i] = line
		}
	}
	depth := 0
	for _, t := range inline.Tokenize(strings.Join(rest, "\n"), true) {
		if t.Kind != "html" || !Structural[t.Tag] {
			continue
		}
		if t.Tag == "table" {
			if t.Closing {
				depth--
			} else {
				depth++
			}
			if depth < 0 {
				return true
			}
			continue
		}
		if depth == 0 {
			return true
		}
	}
	return depth != 0
}

type HTMLCell struct {
	Kind, Text      string
	Lines, Warnings []string
}

// cell_syntax.to_html_cell に対応する HTML セルの中身。
func ToHTMLCell(s string) (HTMLCell, error) {
	out := HTMLCell{Kind: "inline", Text: s}
	if s == "" {
		return out, nil
	}
	var d *blocks.Document
	if strings.Contains(s, "\n") && strings.Contains(s, "|") {
		d = Parse(s)
		for _, node := range d.Root.Walk() {
			if node.Kind == blocks.Table {
				return out, Cannot("セルの中に表（| a | b | の形）は書けない")
			}
		}
	}
	if IsPlain(s) {
		out.Text = strings.ReplaceAll(s, "\n", "<br>")
		return out, nil
	}
	if d == nil {
		d = Parse(s)
	}
	lines := append([]string{}, d.Lines...)
	if structuralTagsOutside(d) {
		return out, Cannot("セルの中に表の構造のタグ（<td> など）がある")
	}
	for _, n := range d.Root.Walk() {
		if n.Kind == blocks.HTMLBlock && htmlEnds[n.Info.BlockType] != nil {
			l := n.Lines[len(n.Lines)-1]
			if !htmlEnds[n.Info.BlockType].MatchString(lines[l.Number][l.Offset:]) {
				return out, Cannot("セルの中に閉じていない HTML（<pre>・<!-- など）がある")
			}
		}
	}
	suffix := map[int]string{}
	for _, n := range paragraphLike(d) {
		for _, b := range lineBreaks(d, n) {
			if b.InAtom || b.Marked {
				continue
			}
			body := strings.TrimRight(lines[b.Line], " ")
			ending := "<br>"
			if trailingBackslashes(body)%2 == 1 {
				ending = " <br>"
			}
			suffix[b.Line] = ending
		}
	}
	for l, ending := range suffix {
		lines[l] = strings.TrimRight(lines[l], " ") + ending
	}
	for _, n := range d.Root.Walk() {
		if n.Kind == blocks.Paragraph && n.Info.OnlyLinkDefinitions {
			for _, l := range n.Lines {
				if strings.HasPrefix(lines[l.Number][l.Offset:], "[") {
					lines[l.Number] = lines[l.Number][:l.Offset] + "\\" + lines[l.Number][l.Offset:]
				}
			}
		}
		if n.Kind == blocks.Item && len(n.Children) > 0 && n.Children[0].Kind == blocks.Paragraph {
			l := n.Children[0].Lines[0]
			m := relaxedTask.FindStringSubmatch(lines[l.Number][l.Offset:])
			if m != nil && m[1] != " " && m[1] != "x" && m[1] != "X" {
				lines[l.Number] = lines[l.Number][:l.Offset] + "\\" + lines[l.Number][l.Offset:]
			}
		}
	}
	var closing []string
	for _, n := range d.Root.Walk() {
		if n.Kind == blocks.CodeBlock && n.Info.Fenced && !n.Info.Closed {
			indent := pycompat.Len(d.Lines[n.StartLine][:n.StartOffset])
			closing = append(closing, strings.Repeat(" ", indent)+strings.Repeat(n.Info.FenceChar, n.Info.FenceLength))
			out.Warnings = append(out.Warnings, "閉じていないコードブロックを閉じた")
		}
	}
	for i := len(closing) - 1; i >= 0; i-- {
		lines = append(lines, closing[i])
	}
	before, after := map[int]bool{}, map[int]bool{}
	children := d.Root.Children
	for i, n := range children {
		if n.Kind == blocks.List || n.Kind == blocks.CodeBlock {
			if i > 0 && n.StartLine > 0 && pycompat.Strip(lines[n.StartLine-1]) != "" {
				before[n.StartLine] = true
			}
			if n.Kind == blocks.CodeBlock && i+1 < len(children) && children[i+1].StartLine == n.EndLine+1 {
				after[n.EndLine] = true
			}
		}
	}
	var result []string
	for i, line := range lines {
		if before[i] {
			result = append(result, "")
		}
		result = append(result, line)
		if after[i] {
			result = append(result, "")
		}
	}
	out.Kind = "chunk"
	out.Lines = result
	out.Text = ""
	return out, nil
}

// cell_syntax.chunk_to_excel に対応する Markdown 部分の逆変換。
func ChunkToExcel(s string) string {
	d := Parse(s)
	lines := append([]string{}, d.Lines...)
	joinNext, markerLines := map[int]bool{}, map[int]bool{}
	nextOffset := map[int]int{}
	for _, n := range paragraphLike(d) {
		for i, b := range lineBreaks(d, n) {
			if b.Marked && !b.InAtom {
				markerLines[b.Line] = true
			} else {
				joinNext[b.Line] = true
				nextOffset[b.Line] = n.Lines[i+1].Offset
			}
		}
	}
	var out []string
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		for joinNext[i] && i+1 < len(lines) {
			following := strings.TrimLeft(lines[i+1][nextOffset[i]:], " \t")
			line = strings.TrimRight(line, " \t") + " " + following
			i++
		}
		if markerLines[i] {
			line, _ = StripBreakMarker(line)
		}
		out = append(out, line)
	}
	return model.NormalizeText(strings.Join(out, "\n"))
}
