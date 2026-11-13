package edit

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
	"golang.org/x/net/html"
)

var headingTagPattern = regexp.MustCompile(`^h[1-6]$`)
var atxHeadingPattern = regexp.MustCompile(`^( {0,3})(#{1,6})([ \t]+|$)`)
var closingHeadingPattern = regexp.MustCompile(`[ \t]+(#+)[ \t]*$`)

type headingOperation struct {
	kind, scope, direction string
	delta                  int
}

func parseHeadingOperation(m map[string]any) (headingOperation, *Error) {
	o := headingOperation{}
	o.kind, _ = m["type"].(string)
	switch o.kind {
	case "level":
		if !fields(m, "type", "delta", "scope") {
			return o, invalid()
		}
		var ok bool
		o.delta, ok = integer(m["delta"], -1)
		if !ok || o.delta != -1 && o.delta != 1 {
			return o, invalid()
		}
		o.scope, _ = m["scope"].(string)
		if o.scope != "heading" && o.scope != "section" {
			return o, invalid()
		}
	case "delete":
		if !fields(m, "type", "scope") {
			return o, invalid()
		}
		o.scope, _ = m["scope"].(string)
		if o.scope != "heading" && o.scope != "section" {
			return o, invalid()
		}
	case "move":
		if !fields(m, "type", "direction") {
			return o, invalid()
		}
		o.direction, _ = m["direction"].(string)
		if o.direction != "up" && o.direction != "down" {
			return o, invalid()
		}
	default:
		return o, invalid()
	}
	return o, nil
}

type headingFocus struct {
	Tag   string `json:"tag"`
	Range Range  `json:"range"`
}

type headingSection struct {
	Lines    int `json:"lines"`
	Headings int `json:"headings"`
}

type headingInfo struct {
	Index   int               `json:"index"`
	Tag     string            `json:"tag"`
	Range   Range             `json:"range"`
	ID      string            `json:"id"`
	Level   int               `json:"level"`
	Text    string            `json:"text"`
	Reason  string            `json:"reason"`
	Section *headingSection   `json:"section"`
	Ops     map[string]string `json:"ops"`
}

type headingEntry struct {
	info                        headingInfo
	node                        *html.Node
	block                       *block
	boundary                    bool
	first, end, sectionEnd      int // 原文の 0 起点、終端を含まない行範囲。
	next, outlineNext, previous int // 見出し一覧の番号。無いときは一覧の長さか -1。
	sectionReason               string
}

type headingMapping struct {
	root    *html.Node
	entries []*headingEntry
	targets map[positionAttribute]*headingEntry
	lines   []string
}

const (
	headingOutlineReason = "節の中に、見出し欄では同じか上のレベルに見える見出し（引用・リスト・HTML の中など）があるので、節の範囲を決められません。"
	headingIncludeReason = "節の中に取り込みの指定か取り込んだ行があるので、節ごとは操作できません。"
	headingChildReason   = "節の中に、引用・リスト・HTML の中か取り込んだファイルの見出しがあるので、節ごとはレベルを変えられません。"
	headingSetextReason  = "この見出しは # の形にすると表示が変わるので、レベルを変えられません。原文で 1 行にしてから操作してください。"
	headingCompareReason = "操作の結果、ほかの部分の表示が変わるので、書き込みませんでした（参照リンクや脚注の定義の重なりなど）。"
)

func headingNode(n *html.Node) bool {
	return n.Type == html.ElementNode && len(n.Data) == 2 && n.Data[0] == 'h' && n.Data[1] >= '1' && n.Data[1] <= '6'
}

func headingText(ctx context.Context, n *html.Node) string {
	var text strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Data != "a" || !htmltree.HasClass(c, "anchor") {
			text.WriteString(htmltree.ContentContext(ctx, c))
		}
	}
	return text.String()
}

// headingMapping は描画ごとに一度だけ作る。target と同じ重複検査を索引で行い、線形探索を繰り返さない。
func (m *blockMapping) headingMapping(ctx context.Context, src session.Source) (*headingMapping, *Error) {
	if m.headings != nil {
		return m.headings, nil
	}
	root, err := htmltree.Fragment(ctx, src.HTML)
	if err != nil {
		return nil, fault(422, "render", "描画した見出しを読み取れません。")
	}
	nodes, err := htmltree.Elements(ctx, root, headingNode)
	if err != nil {
		return nil, fault(422, "cancelled", "見出しの対応の確認を取り消しました。")
	}
	blocks := map[positionAttribute]*block{}
	counts := map[positionAttribute]int{}
	for i := range m.blocks {
		b := &m.blocks[i]
		key := positionAttribute{b.tag, "data-sourcepos", string(b.public)}
		blocks[key] = b
		counts[key]++
	}
	h := &headingMapping{root: root, targets: map[positionAttribute]*headingEntry{}, lines: headingLines(src.Text)}
	maxExpanded := src.Lines.Expanded(len(lineStarts(src.Text)))
	for i, n := range nodes {
		r := Range(htmltree.Get(n, "data-sourcepos"))
		key := positionAttribute{n.Data, "data-sourcepos", string(r)}
		b := blocks[key]
		e := &headingEntry{node: n, block: b, previous: -1, next: len(nodes), outlineNext: len(nodes)}
		e.info = headingInfo{Index: i, Tag: n.Data, Range: r, ID: strings.TrimPrefix(htmltree.Get(n, "id"), "user-content-"), Level: int(n.Data[1] - '0'), Text: headingText(ctx, n), Ops: map[string]string{}}
		positionReason := m.positionReason(n, "data-sourcepos")
		raw := r == "" || positionReason != nil && positionReason.Code == "untrusted"
		if raw {
			e.info.Tag, e.info.Range = "", ""
		}
		switch {
		case htmltree.Ancestor(n, func(p *html.Node) bool { return p.Data == "table" }):
			e.info.Reason = "表の中の見出しは、操作できません。"
		case htmltree.Ancestor(n, func(p *html.Node) bool {
			return p.Data == "blockquote" || p.Data == "li" || p.Data == "ul" || p.Data == "ol" || p.Data == "dl" || footnoteSection(p) || htmltree.HasClass(p, "markdown-alert")
		}):
			e.info.Reason = "引用・リスト・注記などの中の見出しは、操作できません。"
		case n.Parent != root:
			e.info.Reason = "HTML のブロックの中の見出しは、操作できません。"
		case raw:
			e.info.Reason = "HTML で書いた見出しは、操作できません。"
		case positionReason != nil:
			e.info.Reason = positionReason.Message
		case counts[key] > 1:
			e.info.Reason = blockFault("ambiguous").Message
		case b == nil:
			e.info.Reason = blockFault("target").Message
		case b.reason != nil:
			e.info.Reason = b.reason.Message
		}
		c, ce := coordinates(r)
		e.boundary = n.Parent == root && !raw && positionReason == nil && ce == nil && c[2] <= maxExpanded
		if e.info.Reason == "" {
			actual, _ := coordinates(b.actual)
			last, _ := src.Lines.Original(actual[2])
			e.first, e.end = b.line-1, last
		}
		if e.info.Tag != "" {
			if _, exists := h.targets[key]; exists {
				h.targets[key] = nil
			} else {
				h.targets[key] = e
			}
		}
		h.entries = append(h.entries, e)
	}
	// レベルは六つだけなので、一覧と境の次の項目を一定回数の走査で求める。
	nextOutline, nextBoundary := [7]int{}, [7]int{}
	for level := 1; level <= 6; level++ {
		nextOutline[level], nextBoundary[level] = len(nodes), len(nodes)
	}
	for i := len(h.entries) - 1; i >= 0; i-- {
		e := h.entries[i]
		e.outlineNext, e.next = nextOutline[e.info.Level], nextBoundary[e.info.Level]
		for level := e.info.Level; level <= 6; level++ {
			nextOutline[level] = i
			if e.boundary {
				nextBoundary[level] = i
			}
		}
	}
	previous := [7]int{}
	for level := 1; level <= 6; level++ {
		previous[level] = -1
	}
	for i, e := range h.entries {
		e.previous = previous[e.info.Level]
		// 兄弟は境の見出しだけでたどる（引用・リスト・HTML の中の見出しは、節の曖昧さとして sectionReason で断る）。
		if e.boundary {
			for level := e.info.Level; level <= 6; level++ {
				previous[level] = i
			}
		}
		if e.info.Reason != "" {
			continue
		}
		e.sectionEnd = len(h.lines)
		first, _ := coordinates(e.block.actual)
		expandedEnd := src.Lines.Expanded(len(h.lines)+1) - 1
		if e.next < len(h.entries) {
			c, _ := coordinates(Range(htmltree.Get(h.entries[e.next].node, "data-sourcepos")))
			expandedEnd = c[0] - 1
			orig, _ := src.Lines.Original(c[0])
			e.sectionEnd = orig - 1
		}
		if e.outlineNext != e.next {
			e.sectionReason = headingOutlineReason
		}
		for line := first[0]; line <= expandedEnd; line++ {
			orig, reason := src.Lines.Original(line)
			if reason != "" || orig != e.first+1+line-first[0] {
				if e.sectionReason == "" {
					e.sectionReason = reasonText(reason)
					if reason == "include" || reason == "directive" {
						e.sectionReason = headingIncludeReason
					}
				}
				break
			}
		}
		if e.sectionEnd < e.end || e.sectionEnd > len(h.lines) {
			e.sectionReason = headingIncludeReason
			e.sectionEnd = max(e.end, min(e.sectionEnd, len(h.lines)))
		}
		last := e.sectionEnd
		for last > e.first && headingBlank(h.lines[last-1]) {
			last--
		}
		e.info.Section = &headingSection{Lines: last - e.first, Headings: e.next - i}
	}
	for _, e := range h.entries {
		if e.info.Reason != "" {
			continue
		}
		for _, op := range []headingOperation{{kind: "level", delta: -1, scope: "heading"}, {kind: "level", delta: 1, scope: "heading"}, {kind: "level", delta: -1, scope: "section"}, {kind: "level", delta: 1, scope: "section"}, {kind: "move", direction: "up"}, {kind: "move", direction: "down"}, {kind: "delete", scope: "heading"}, {kind: "delete", scope: "section"}} {
			e.info.Ops[op.name()] = h.operationReason(e, op)
		}
	}
	m.headings = h
	return h, nil
}

func (h *headingMapping) information() []headingInfo {
	result := make([]headingInfo, len(h.entries))
	for i, e := range h.entries {
		result[i] = e.info
	}
	return result
}

func (o headingOperation) name() string {
	if o.kind == "delete" {
		if o.scope == "section" {
			return "deleteSection"
		}
		return "deleteHeading"
	}
	if o.kind == "move" {
		if o.direction == "up" {
			return "moveUp"
		}
		return "moveDown"
	}
	name := "demote"
	if o.delta < 0 {
		name = "promote"
	}
	if o.scope == "section" {
		name += "Section"
	}
	return name
}

func (h *headingMapping) sibling(e *headingEntry, direction string) *headingEntry {
	i := e.next
	if direction == "up" {
		i = e.previous
	}
	if i < 0 || i >= len(h.entries) {
		return nil
	}
	sibling := h.entries[i]
	if sibling.info.Level != e.info.Level {
		return nil
	}
	return sibling
}

func (h *headingMapping) operationReason(e *headingEntry, o headingOperation) string {
	if e.info.Reason != "" {
		return e.info.Reason
	}
	if o.scope == "section" || o.kind == "move" {
		if e.sectionReason != "" {
			return e.sectionReason
		}
	}
	if o.kind == "move" {
		sibling := h.sibling(e, o.direction)
		word := "後ろ"
		if o.direction == "up" {
			word = "前"
		}
		if sibling == nil {
			return "同じ親の中で、" + word + "に同じレベルの見出しがありません。"
		}
		if sibling.info.Reason != "" {
			return "入れ替える" + word + "の見出しは操作できないので、移動できません。"
		}
		return sibling.sectionReason
	}
	if o.kind != "level" {
		return ""
	}
	if o.scope == "heading" {
		if e.info.Level+o.delta < 1 {
			return "h1 より上のレベルはありません。"
		}
		if e.info.Level+o.delta > 6 {
			return "h6 より下のレベルはありません。"
		}
		return ""
	}
	for _, child := range h.entries[e.info.Index:e.next] {
		if child.info.Reason != "" {
			return headingChildReason
		}
		if child.info.Level+o.delta < 1 {
			return "節の中に h1 の見出しがあるので、節ごと上げられません。"
		}
		if child.info.Level+o.delta > 6 {
			return "節の中に h6 の見出しがあるので、節ごと下げられません。"
		}
	}
	return ""
}

func headingLines(text string) []string {
	if text == "" {
		return []string{}
	}
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func headingBlank(line string) bool { return strings.Trim(line, " \t\r\n") == "" }

// headingKeep は、変わらなかった原文の行の対応 [旧の行, 新の行, 行数]（1 起点）。
type headingKeep = lineKeep
type headingLine struct {
	text string
	old  int
}
type headingRewrite struct {
	text string
	keep []headingKeep
}

// 行の由来を組み立てと一緒に運び、移動した行も後のコメントの追従に使える形で返す。
func finishHeadingRewrite(old string, lines []headingLine, newline string) headingRewrite {
	bom := strings.HasPrefix(old, "\ufeff")
	var out strings.Builder
	if bom {
		out.WriteString("\ufeff")
	}
	keep := []headingKeep{}
	for i, line := range lines {
		text := line.text
		if line.old == 1 {
			text = strings.TrimPrefix(text, "\ufeff")
		}
		ending := ""
		if strings.HasSuffix(text, "\n") {
			ending = "\n"
			text = strings.TrimSuffix(text, "\n")
			if strings.HasSuffix(text, "\r") {
				text, ending = strings.TrimSuffix(text, "\r"), "\r\n"
			}
		}
		out.WriteString(text)
		if i < len(lines)-1 || strings.HasSuffix(old, "\n") {
			if ending == "" {
				ending = newline
			}
			out.WriteString(ending)
		}
		// 改行や BOM の付け替えがあっても、本文の変わらない行の対応は保持する。
		if line.old > 0 {
			if n := len(keep); n > 0 && keep[n-1][0]+keep[n-1][2] == line.old && keep[n-1][1]+keep[n-1][2] == i+1 {
				keep[n-1][2]++
			} else {
				keep = append(keep, headingKeep{line.old, i + 1, 1})
			}
		}
	}
	return headingRewrite{out.String(), keep}
}

func originalHeadingLines(text string) []headingLine {
	lines := headingLines(text)
	out := make([]headingLine, len(lines))
	for i, line := range lines {
		out[i] = headingLine{line, i + 1}
	}
	return out
}

func rewriteHeadingLevel(lines []headingLine, e *headingEntry, delta int, newline string) ([]headingLine, bool, *Error) {
	first := lines[e.first].text
	if e.first == 0 {
		first = strings.TrimPrefix(first, "\ufeff")
	}
	ending := ""
	if strings.HasSuffix(first, "\n") {
		ending, first = "\n", strings.TrimSuffix(first, "\n")
	}
	if strings.HasSuffix(first, "\r") {
		ending, first = "\r"+ending, strings.TrimSuffix(first, "\r")
	}
	level := e.info.Level + delta
	match := atxHeadingPattern.FindStringSubmatchIndex(first)
	setext := match == nil
	if !setext {
		if e.end != e.first+1 || match[5]-match[4] != e.info.Level {
			return nil, false, blockFault("boundary")
		}
		closing := closingHeadingPattern.FindStringSubmatchIndex(first[match[5]:])
		if closing != nil && closing[3]-closing[2] == e.info.Level {
			a, b := match[5]+closing[2], match[5]+closing[3]
			first = first[:a] + strings.Repeat("#", level) + first[b:]
		}
		first = first[:match[4]] + strings.Repeat("#", level) + first[match[5]:]
	} else {
		if e.end-e.first < 2 {
			return nil, false, blockFault("boundary")
		}
		parts := []string{}
		for _, line := range lines[e.first : e.end-1] {
			content := line.text
			if line.old == 1 {
				content = strings.TrimPrefix(content, "\ufeff")
			}
			parts = append(parts, strings.TrimSpace(content))
		}
		content := strings.Join(parts, " ")
		closing := closingHeadingPattern.FindStringSubmatchIndex(content)
		if strings.Trim(content, "#") == "" && content != "" {
			content = "\\" + content
		} else if closing != nil {
			content = content[:closing[2]] + "\\" + content[closing[2]:]
		}
		first = strings.Repeat("#", level) + " " + content
		if ending == "" && e.end < len(lines) {
			ending = newline
		}
	}
	lines = slices.Replace(lines, e.first, e.end, headingLine{first + ending, 0})
	return lines, setext, nil
}

func deleteHeadingLines(lines []headingLine, first, end int, newline string) []headingLine {
	previousBlank := first == 0 || headingBlank(lines[first-1].text)
	if end == len(lines) {
		for first > 0 && headingBlank(lines[first-1].text) {
			first--
		}
		return slices.Delete(lines, first, end)
	}
	if !previousBlank && !headingBlank(lines[end].text) {
		return slices.Replace(lines, first, end, headingLine{newline, 0})
	}
	if previousBlank && headingBlank(lines[end].text) {
		for end < len(lines) && headingBlank(lines[end].text) {
			end++
		}
	}
	return slices.Delete(lines, first, end)
}

func moveHeadingLines(lines []headingLine, a, b *headingEntry, newline string) []headingLine {
	aEnd, bEnd := a.sectionEnd, b.sectionEnd
	for aEnd > a.first && headingBlank(lines[aEnd-1].text) {
		aEnd--
	}
	for bEnd > b.first && headingBlank(lines[bEnd-1].text) {
		bEnd--
	}
	part := append([]headingLine{}, lines[b.first:bEnd]...)
	for range max(a.sectionEnd-aEnd, 1) {
		part = append(part, headingLine{newline, 0})
	}
	part = append(part, lines[a.first:aEnd]...)
	if b.sectionEnd < len(lines) {
		for range max(b.sectionEnd-bEnd, 1) {
			part = append(part, headingLine{newline, 0})
		}
	} else {
		// 文書の末尾の空行は、入れ替えた後も文書の末尾に残す。
		part = append(part, lines[bEnd:b.sectionEnd]...)
	}
	return slices.Replace(lines, a.first, b.sectionEnd, part...)
}

func (s *Service) editHeading(ctx context.Context, src session.Source, r request) (headingRewrite, []string, *headingFocus, *Error) {
	m, err := s.blocks(ctx, src)
	if err != nil {
		return headingRewrite{}, nil, nil, err
	}
	h, err := m.headingMapping(ctx, src)
	if err != nil {
		return headingRewrite{}, nil, nil, err
	}
	e := h.targets[positionAttribute{r.headingTag, "data-sourcepos", string(r.rangeValue)}]
	if e == nil {
		return headingRewrite{}, nil, nil, blockFault("target")
	}
	if reason := h.operationReason(e, r.headingOp); reason != "" {
		return headingRewrite{}, nil, nil, fault(422, "excluded", reason)
	}
	rewritten, setext, err := rewriteHeading(src.Text, h, e, r.headingOp)
	if err != nil {
		return headingRewrite{}, nil, nil, err
	}
	if len(rewritten.text) > maxDocumentBytes {
		return headingRewrite{}, nil, nil, fault(422, "large", "編集後の文書が 16 MiB を超えています。")
	}
	warnings, focus, verifyErr := s.verifyHeadingEdit(ctx, src, h, e, r.headingOp, rewritten.text, setext)
	return rewritten, warnings, focus, verifyErr
}

// rewriteHeading は本文と行の対応を一緒に返す。描画や書き込みを行わない。
func rewriteHeading(old string, h *headingMapping, e *headingEntry, op headingOperation) (headingRewrite, map[*html.Node]bool, *Error) {
	lines := originalHeadingLines(old)
	ending := newlineAt(old, e.first+1)
	setext := map[*html.Node]bool{}
	switch op.kind {
	case "level":
		last := e.info.Index + 1
		if op.scope == "section" {
			last = e.next
		}
		for i := last - 1; i >= e.info.Index; i-- {
			child := h.entries[i]
			var converted bool
			var err *Error
			lines, converted, err = rewriteHeadingLevel(lines, child, op.delta, ending)
			if err != nil {
				return headingRewrite{}, nil, err
			}
			if converted {
				setext[child.node] = child.end-child.first > 2
			}
		}
	case "delete":
		end := e.end
		if op.scope == "section" {
			end = e.sectionEnd
		}
		lines = deleteHeadingLines(lines, e.first, end, ending)
	case "move":
		sibling := h.sibling(e, op.direction)
		a, b := e, sibling
		if op.direction == "up" {
			a, b = sibling, e
		}
		lines = moveHeadingLines(lines, a, b, ending)
	}
	return finishHeadingRewrite(old, lines, ending), setext, nil
}

func headingTag(level int) string { return fmt.Sprintf("h%d", level) }
