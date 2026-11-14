package edit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
	"golang.org/x/net/html"
)

// HTMLBoundary は、最上位の連なりを挟む公開の位置。包みは内側の pre で指す。
type HTMLBoundary struct {
	Tag   string `json:"tag"`
	Range Range  `json:"range"`
}

// definitionLine は、リンクの参照と脚注の定義の行の始まり（`[名前]:`）。
var definitionLine = regexp.MustCompile(`^ {0,3}\[[^\]]+\]:`)

type htmlRun struct {
	prev, next *HTMLBoundary
	nodes      []*html.Node
	checked    bool
	block      block
	reason     *Error
}

func (m *blockMapping) htmlBoundary(n *html.Node) *HTMLBoundary {
	if n.Type != html.ElementNode {
		return nil
	}
	if n.Data == "div" && htmltree.HasClass(n, "markdown-code-block") {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "pre" {
				n = c
				break
			}
		}
	}
	// PlantUML の図の包みも、原文を持つ内側の画像で指す。
	if n.Data == "a" && htmltree.Get(n, "data-diagram") == "plantuml" {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "img" {
				n = c
				break
			}
		}
	}
	r := Range(htmltree.Get(n, "data-sourcepos"))
	if _, e := ParseSourceRange(n.Data, string(r)); e != nil || m.positionReason(n, "data-sourcepos") != nil {
		return nil
	}
	return &HTMLBoundary{n.Data, r}
}

func sameBoundary(a, b *HTMLBoundary) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func (m *blockMapping) htmlRuns(ctx context.Context, src session.Source) ([]*htmlRun, *Error) {
	if m.htmlReady {
		return m.html, nil
	}
	root, err := htmltree.Fragment(ctx, src.HTML)
	if err != nil {
		return nil, blockFault("boundary")
	}
	var prev *HTMLBoundary
	var nodes []*html.Node
	flush := func(next *HTMLBoundary) {
		element, toc := false, false
		for _, n := range nodes {
			if n.Type == html.ElementNode {
				element = true
				toc = toc || n.Data == "ul" && htmltree.HasClass(n, "section-nav")
			}
		}
		if element && !toc {
			m.html = append(m.html, &htmlRun{prev: prev, next: next, nodes: nodes})
		}
		nodes = nil
	}
	for n := root.FirstChild; n != nil; n = n.NextSibling {
		if footnoteSection(n) {
			continue
		}
		if boundary := m.htmlBoundary(n); boundary != nil {
			flush(boundary)
			prev = boundary
		} else if n.Type == html.ElementNode || n.Type == html.TextNode && strings.TrimSpace(n.Data) != "" {
			nodes = append(nodes, n)
		}
	}
	flush(nil)
	m.htmlReady = true
	return m.html, nil
}

func (m *blockMapping) boundaryRange(b *HTMLBoundary) (Range, *Error) {
	if b == nil {
		return "", nil
	}
	if m.untrusted[positionAttribute{b.Tag, "data-sourcepos", string(b.Range)}] {
		return "", blockFault("boundary")
	}
	if actual, ok := m.correctedRanges()[positionAttribute{b.Tag, "data-sourcepos", string(b.Range)}]; ok {
		return actual, nil
	}
	for _, candidate := range m.blocks {
		if candidate.tag == b.Tag && candidate.public == b.Range && candidate.reason != nil {
			return "", blockFault("boundary")
		}
	}
	return b.Range, nil
}

func renderHTMLRoot(root *html.Node) string {
	var out strings.Builder
	for n := root.FirstChild; n != nil; n = n.NextSibling {
		_ = html.Render(&out, n)
	}
	return out.String()
}

// 位置のある要素は別に比較し、最上位の位置なし要素と印の並びも保つ。
func (m *blockMapping) htmlShape(root *html.Node, marker *html.Node) []string {
	out := []string{}
	for n := root.FirstChild; n != nil; n = n.NextSibling {
		if footnoteSection(n) {
			continue
		}
		if n == marker {
			out = append(out, "marker")
			continue
		}
		if m.htmlBoundary(n) != nil {
			out = append(out, "boundary")
			continue
		}
		if n.Type == html.ElementNode || n.Type == html.TextNode && strings.TrimSpace(n.Data) != "" {
			out = append(out, m.htmlSignature(n))
		}
	}
	return out
}

// htmlSignature は、位置を持たない節点の形（要素名・入れ子・文字）を表す。
// 位置を持つ子孫（エンジンの包みを含む）は別に比べるので飛ばす。属性は、並びが描画ごとに変わるので入れない。
// 要素名だけで比べると、既存の節点が新しい要素の中へ取り込まれても（`<div>` の中の `<div>`）同じに見える。
func (m *blockMapping) htmlSignature(n *html.Node) string {
	var out strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			if text := strings.Join(strings.Fields(n.Data), " "); text != "" {
				out.WriteString(strconv.Quote(text))
			}
		case html.ElementNode:
			if m.htmlBoundary(n) != nil {
				return
			}
			out.WriteString("<" + n.Data + ">")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			out.WriteString("</>")
		}
	}
	walk(n)
	return out.String()
}

// rawRunOf は、対象を中に持つ最上位の連なりを返す。無ければ nil。
func (m *blockMapping) rawRunOf(ctx context.Context, src session.Source, b *block) *htmlRun {
	runs, e := m.htmlRuns(ctx, src)
	if e != nil {
		return nil
	}
	for _, run := range runs {
		inside := false
		for _, node := range run.nodes {
			_ = htmltree.Walk(ctx, node, func(n *html.Node) error {
				if n.Type == html.ElementNode && n.Data == b.tag && htmltree.Get(n, "data-sourcepos") == string(b.public) {
					inside = true
				}
				return nil
			})
		}
		if inside {
			return run
		}
	}
	return nil
}

// htmlCandidate は、連なりの候補の行を返す。
// trim が真なら、両端の「リンクの参照や脚注の定義だけの段」を外し、外した行（展開後の最初と最後の行。無ければ 0）も返す。
// 外した行が本当に連なりの外にあるかは、呼ぶ側が試しの描画で確かめる（definitionsOutside）。
func (m *blockMapping) htmlCandidate(src session.Source, run *htmlRun, trim bool) (*block, [2][2]int, *Error) {
	var trimmed [2][2]int
	candidate, e := m.htmlCandidateLines(src, run, trim, &trimmed)
	return candidate, trimmed, e
}

func (m *blockMapping) htmlCandidateLines(src session.Source, run *htmlRun, trim bool, trimmed *[2][2]int) (*block, *Error) {
	lines := strings.Split(src.Text, "\n")
	first, last := 1, src.Lines.Expanded(len(lines))
	if run.prev != nil {
		r, e := m.boundaryRange(run.prev)
		if e != nil {
			return nil, blockFault("boundary")
		}
		c, e := positionCoordinates(r)
		if e != nil {
			return nil, blockFault("boundary")
		}
		first = c[2] + 1
		if c[3] == 0 {
			first--
		}
	}
	if run.next != nil {
		r, e := m.boundaryRange(run.next)
		if e != nil {
			return nil, blockFault("boundary")
		}
		c, e := positionCoordinates(r)
		if e != nil {
			return nil, blockFault("boundary")
		}
		last = c[0] - 1
	}
	// 空行を除くときも、元の文書へ戻せない行は読み飛ばさない。
	for first <= last {
		line, e := blockLine(src, lines, first)
		if e != nil {
			return nil, blockFault("boundary")
		}
		if strings.TrimSpace(line) != "" {
			break
		}
		first++
	}
	for first <= last {
		line, e := blockLine(src, lines, last)
		if e != nil {
			return nil, blockFault("boundary")
		}
		if strings.TrimSpace(line) != "" {
			break
		}
		last--
	}
	if first > last {
		return nil, blockFault("boundary")
	}
	// 両端の、リンクの参照や脚注の定義だけの段（空行で区切った行のまとまり）は、連なりを作らないので候補から外す。
	// 候補に入れると、試しの描画で参照や脚注が消えて、連なりの全部を断ることになる。
	// 定義の形の行が、連なりの中の文字として描かれている場合（閉じていない pre の中など）は外さない。
	// 閉じていないコメントの中のように、描かれない形で連なりの一部になっている行は、ここでは見分けられない。
	// 外した行が連なりの外にあることは、呼ぶ側が試しの描画で確かめる。
	outerFirst, outerLast := first, last
	runText := ""
	for _, n := range run.nodes {
		runText += htmltree.Content(n)
	}
	definitions := func(from, step int) (int, *Error) {
		for {
			n, only := from, true
			for n >= first && n <= last {
				line, e := blockLine(src, lines, n)
				if e != nil {
					return 0, e
				}
				if strings.TrimSpace(line) == "" {
					break
				}
				label := definitionLine.FindString(line)
				only = only && label != "" && !strings.Contains(runText, strings.TrimSpace(label))
				n += step
			}
			if !only || n < first || n > last {
				return from, nil
			}
			for n >= first && n <= last {
				line, e := blockLine(src, lines, n)
				if e != nil {
					return 0, e
				}
				if strings.TrimSpace(line) != "" {
					break
				}
				n += step
			}
			from = n
		}
	}
	if trim {
		head, e := definitions(first, 1)
		if e != nil {
			return nil, blockFault("boundary")
		}
		tail, e := definitions(last, -1)
		if e != nil || head > tail {
			return nil, blockFault("boundary")
		}
		first, last = head, tail
		// 外した行は、空行でない最初と最後の行で表す。
		blank := func(n int) bool {
			line, e := blockLine(src, lines, n)
			return e == nil && strings.TrimSpace(line) == ""
		}
		if first > outerFirst {
			end := first - 1
			for end > outerFirst && blank(end) {
				end--
			}
			trimmed[0] = [2]int{outerFirst, end}
		}
		if last < outerLast {
			start := last + 1
			for start < outerLast && blank(start) {
				start++
			}
			trimmed[1] = [2]int{start, outerLast}
		}
	}
	head, e := blockLine(src, lines, first)
	if e != nil {
		return nil, blockFault("boundary")
	}
	spaces := len(head) - len(strings.TrimLeft(head, " "))
	if spaces > 3 || !strings.HasPrefix(head[spaces:], "<") {
		return nil, blockFault("boundary")
	}
	tail, e := blockLine(src, lines, last)
	if e != nil {
		return nil, blockFault("boundary")
	}
	actual := rangeOf([4]int{first, 1, last, len(strings.TrimRight(tail, " \t"))})
	start, end, line, e := offsets(src, actual)
	if e != nil {
		return nil, blockFault("boundary")
	}
	return &block{tag: "html", kind: "html", public: actual, actual: actual, start: start, end: end, line: line, outer: -1}, nil
}

func (s *Service) verifyHTML(ctx context.Context, src session.Source, m *blockMapping, run *htmlRun) (*block, *Error) {
	if run.checked {
		return &run.block, run.reason
	}
	run.checked = true
	run.reason = blockFault("boundary")
	if m.positionErr != nil {
		return nil, run.reason
	}
	// 利用者の位置を含む連なりは、範囲の誤認を避けて全体を断る。
	for _, n := range run.nodes {
		if err := htmltree.Walk(ctx, n, func(c *html.Node) error {
			if c.Type == html.ElementNode && htmltree.Has(c, "data-sourcepos") && m.positionReason(c, "data-sourcepos") != nil {
				return blockFault("boundary")
			}
			return nil
		}); err != nil {
			return nil, run.reason
		}
	}
	// 定義だけの段を外した候補を先に試す。外した行が連なりの外にあると確かめられなければ、外さない候補で確かめ直す。
	var tried Range
	for _, trim := range []bool{true, false} {
		candidate, trimmed, e := m.htmlCandidate(src, run, trim)
		if e != nil || candidate.actual == tried {
			continue
		}
		tried = candidate.actual
		if !s.verifyCandidate(ctx, src, m, run, candidate) {
			continue
		}
		if trimmed != [2][2]int{} && !s.definitionsOutside(ctx, src, trimmed) {
			continue
		}
		run.block, run.reason = *candidate, nil
		return &run.block, nil
	}
	return nil, run.reason
}

// verifyCandidate は、候補の行を印の段落に置き換えた試しの描画で、候補が連なりだけを作ることを確かめる。
func (s *Service) verifyCandidate(ctx context.Context, src session.Source, m *blockMapping, run *htmlRun, candidate *block) bool {
	actual, start, end, line := candidate.actual, candidate.start, candidate.end, candidate.line
	c, _ := coordinates(actual)
	first, last := c[0], c[2]
	lines := strings.Split(src.Text, "\n")
	marker := htmlMarker(src.Text)
	replacement := marker
	if first > 1 {
		prev, e := blockLine(src, lines, first-1)
		if e != nil {
			return false
		}
		if strings.TrimSpace(prev) != "" {
			replacement = "\n" + replacement
		}
	}
	if last < src.Lines.Expanded(len(lines)) {
		next, e := blockLine(src, lines, last+1)
		if e != nil {
			return false
		}
		if strings.TrimSpace(next) != "" {
			replacement += "\n"
		}
	}
	replacement = normalizeNewlines(replacement, newlineAt(src.Text, line))
	text := src.Text[:start] + replacement + src.Text[end:]
	trial, err := s.opts.Host.Trial(ctx, src, text)
	if err != nil {
		return false
	}
	next := src
	next.Text, next.HTML, next.Lines = text, trial.HTML, trial.Source
	next.HasSourcepos, next.SourceposInput = trial.HasSourcepos, trial.SourceposInput
	after, e := s.buildBlocks(ctx, next)
	if e != nil {
		return false
	}
	oldRoot, err := htmltree.Fragment(ctx, src.HTML)
	if err != nil {
		return false
	}
	newRoot, err := htmltree.Fragment(ctx, trial.HTML)
	if err != nil {
		return false
	}
	var mark *html.Node
	for n := newRoot.FirstChild; n != nil; n = n.NextSibling {
		if n.Type == html.ElementNode && n.Data == "p" && n.FirstChild != nil && n.FirstChild == n.LastChild && n.FirstChild.Type == html.TextNode && n.FirstChild.Data == marker && after.htmlBoundary(n) != nil {
			if mark != nil {
				return false
			}
			mark = n
		}
	}
	if mark == nil {
		return false
	}
	// 同じ断片を読み直した木から、連なりの節点を同じ最上位の番号で取り除く。
	remove := map[int]bool{}
	for i, n := 0, run.nodes[0].Parent.FirstChild; n != nil; i, n = i+1, n.NextSibling {
		if slices.Contains(run.nodes, n) {
			remove[i] = true
		}
	}
	var oldMark *html.Node
	for i, n := 0, oldRoot.FirstChild; n != nil; i++ {
		following := n.NextSibling
		if remove[i] {
			if oldMark == nil {
				oldMark = &html.Node{Type: html.TextNode, Data: marker}
				oldRoot.InsertBefore(oldMark, n)
			}
			oldRoot.RemoveChild(n)
		}
		n = following
	}
	if !slices.Equal(m.htmlShape(oldRoot, oldMark), after.htmlShape(newRoot, mark)) {
		return false
	}
	oldRoot.RemoveChild(oldMark)
	newRoot.RemoveChild(mark)
	delta := strings.Count(replacement, "\n") - strings.Count(src.Text[start:end], "\n")
	if !insertionPreserves(ctx, m, after, renderHTMLRoot(oldRoot), renderHTMLRoot(newRoot), last+1, delta, 0) {
		return false
	}
	return true
}

// htmlMarker は、文書に無い英数字だけの語を作る。
func htmlMarker(text string) string {
	var entropy [16]byte
	_, _ = rand.Read(entropy[:])
	marker := "gphtml" + hex.EncodeToString(entropy[:])
	for strings.Contains(text, marker) {
		marker += "x"
	}
	return marker
}

// definitionsOutside は、候補から外した定義の行が、連なりの HTML のブロックの外にあることを確かめる。
// 外した行を、空行で挟んだ印の語に置き換えて試しに描く。印が位置を持つ段落として出れば、
// その行は Markdown のブロックとして読まれている（連なりとは別のブロック）。
// 閉じていないコメントや pre・script の中の行なら、印は段落にならない。その行は連なりの原文の一部なので、候補から外せない。
func (s *Service) definitionsOutside(ctx context.Context, src session.Source, trimmed [2][2]int) bool {
	lines := strings.Split(src.Text, "\n")
	text := src.Text
	markers := []string{}
	// 後ろから置き換えて、前の位置をずらさない。
	for i := 1; i >= 0; i-- {
		if trimmed[i][0] == 0 {
			continue
		}
		tail, e := blockLine(src, lines, trimmed[i][1])
		if e != nil {
			return false
		}
		start, end, line, e := offsets(src, rangeOf([4]int{trimmed[i][0], 1, trimmed[i][1], len(tail)}))
		if e != nil {
			return false
		}
		marker, ending := htmlMarker(text), newlineAt(src.Text, line)
		text = text[:start] + ending + marker + ending + text[end:]
		markers = append(markers, marker)
	}
	trial, err := s.opts.Host.Trial(ctx, src, text)
	if err != nil {
		return false
	}
	root, err := htmltree.Fragment(ctx, trial.HTML)
	if err != nil {
		return false
	}
	found := map[string]int{}
	if err := htmltree.Walk(ctx, root, func(n *html.Node) error {
		if n.Type == html.ElementNode && n.Data == "p" && htmltree.Has(n, "data-sourcepos") &&
			n.FirstChild != nil && n.FirstChild == n.LastChild && n.FirstChild.Type == html.TextNode {
			found[n.FirstChild.Data]++
		}
		return nil
	}); err != nil {
		return false
	}
	for _, marker := range markers {
		if found[marker] != 1 {
			return false
		}
	}
	return true
}

func (s *Service) htmlWritable(ctx context.Context, src session.Source, m *blockMapping, r Range) (*block, *Error) {
	runs, e := m.htmlRuns(ctx, src)
	if e != nil {
		return nil, e
	}
	for _, run := range runs {
		// 定義だけの段を外した候補と、外さない候補のどちらかが一致する連なりだけを確かめる。
		match := false
		for _, trim := range []bool{true, false} {
			if candidate, _, e := m.htmlCandidate(src, run, trim); e == nil && candidate.actual == r {
				match = true
			}
		}
		if !match {
			continue
		}
		b, e := s.verifyHTML(ctx, src, m, run)
		if e == nil && b.actual != r {
			return nil, blockFault("target")
		}
		return b, e
	}
	return nil, blockFault("target")
}

// HTMLText は、前後の境が一意に指す最上位の連なりだけを取得する。
func (s *Service) HTMLText(ctx context.Context, doc *string, render uint64, prev, next *HTMLBoundary) (any, *Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if render == 0 {
		return nil, invalid()
	}
	if e := validateHTMLBoundaries(prev, next); e != nil {
		return nil, e
	}
	src, e := s.source(doc, render)
	if e != nil {
		return nil, e
	}
	if _, _, e = s.read(src); e != nil {
		return nil, e
	}
	m, e := s.blocks(ctx, src)
	if e != nil {
		return nil, e
	}
	b, e := s.htmlTarget(ctx, src, m, prev, next)
	if e != nil {
		return nil, e
	}
	return map[string]any{"text": removeCR(src.Text[b.start:b.end]), "render": src.Render, "range": b.actual, "kind": "html", "outer": []OuterBlock{}, "anchor": &InsertAnchor{"html", b.actual, b.actual}, "move": s.blockMoveInformation(ctx, src, m, blockMoveTarget{Tag: "html", Range: b.actual})}, nil
}

func validateHTMLBoundaries(prev, next *HTMLBoundary) *Error {
	for _, b := range []*HTMLBoundary{prev, next} {
		if b == nil {
			continue
		}
		if !tagPattern.MatchString(b.Tag) {
			return invalid()
		}
		if _, e := ParseSourceRange(b.Tag, string(b.Range)); e != nil {
			return e
		}
	}
	return nil
}

// htmlTarget は、編集とコメントで同じ最上位の連なりを検証する。呼ぶ側で s.mu を持つ。
func (s *Service) htmlTarget(ctx context.Context, src session.Source, m *blockMapping, prev, next *HTMLBoundary) (*block, *Error) {
	runs, e := m.htmlRuns(ctx, src)
	if e != nil {
		return nil, e
	}
	// 要求した境は、最上位で一意でなければならない。
	root, err := htmltree.Fragment(ctx, src.HTML)
	if err != nil {
		return nil, blockFault("target")
	}
	for _, wanted := range []*HTMLBoundary{prev, next} {
		if wanted == nil {
			continue
		}
		if m.untrusted[positionAttribute{wanted.Tag, "data-sourcepos", string(wanted.Range)}] {
			return nil, blockFault("boundary")
		}
		count := 0
		for n := root.FirstChild; n != nil; n = n.NextSibling {
			if !footnoteSection(n) && sameBoundary(m.htmlBoundary(n), wanted) {
				count++
			}
		}
		if count != 1 {
			return nil, blockFault("target")
		}
	}
	var found *htmlRun
	for _, run := range runs {
		if sameBoundary(run.prev, prev) && sameBoundary(run.next, next) {
			if found != nil {
				return nil, blockFault("target")
			}
			found = run
		}
	}
	if found == nil {
		return nil, blockFault("target")
	}
	return s.verifyHTML(ctx, src, m, found)
}

func (s *Service) htmlOuter(ctx context.Context, src session.Source, m *blockMapping, b *block, outer []OuterBlock) []OuterBlock {
	if run := m.rawRunOf(ctx, src, b); run != nil {
		if raw, e := s.verifyHTML(ctx, src, m, run); e == nil {
			return append(outer, OuterBlock{"html", raw.actual, "html"})
		}
	}
	return outer
}
