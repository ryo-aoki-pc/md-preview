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

func (m *blockMapping) htmlCandidate(src session.Source, run *htmlRun) (*block, *Error) {
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
	var e *Error
	if first, e = definitions(first, 1); e != nil {
		return nil, blockFault("boundary")
	}
	if last, e = definitions(last, -1); e != nil {
		return nil, blockFault("boundary")
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
	candidate, e := m.htmlCandidate(src, run)
	if e != nil {
		return nil, run.reason
	}
	actual, start, end, line := candidate.actual, candidate.start, candidate.end, candidate.line
	c, _ := coordinates(actual)
	first, last := c[0], c[2]
	lines := strings.Split(src.Text, "\n")
	var entropy [16]byte
	_, _ = rand.Read(entropy[:])
	marker := "gphtml" + hex.EncodeToString(entropy[:])
	for strings.Contains(src.Text, marker) {
		marker += "x"
	}
	replacement := marker
	if first > 1 {
		prev, e := blockLine(src, lines, first-1)
		if e != nil {
			return nil, run.reason
		}
		if strings.TrimSpace(prev) != "" {
			replacement = "\n" + replacement
		}
	}
	if last < src.Lines.Expanded(len(lines)) {
		next, e := blockLine(src, lines, last+1)
		if e != nil {
			return nil, run.reason
		}
		if strings.TrimSpace(next) != "" {
			replacement += "\n"
		}
	}
	replacement = normalizeNewlines(replacement, newlineAt(src.Text, line))
	text := src.Text[:start] + replacement + src.Text[end:]
	trial, err := s.opts.Host.Trial(ctx, src, text)
	if err != nil {
		return nil, run.reason
	}
	next := src
	next.Text, next.HTML, next.Lines = text, trial.HTML, trial.Source
	next.HasSourcepos, next.SourceposInput = trial.HasSourcepos, trial.SourceposInput
	after, e := s.buildBlocks(ctx, next)
	if e != nil {
		return nil, run.reason
	}
	oldRoot, err := htmltree.Fragment(ctx, src.HTML)
	if err != nil {
		return nil, run.reason
	}
	newRoot, err := htmltree.Fragment(ctx, trial.HTML)
	if err != nil {
		return nil, run.reason
	}
	var mark *html.Node
	for n := newRoot.FirstChild; n != nil; n = n.NextSibling {
		if n.Type == html.ElementNode && n.Data == "p" && n.FirstChild != nil && n.FirstChild == n.LastChild && n.FirstChild.Type == html.TextNode && n.FirstChild.Data == marker && after.htmlBoundary(n) != nil {
			if mark != nil {
				return nil, run.reason
			}
			mark = n
		}
	}
	if mark == nil {
		return nil, run.reason
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
		return nil, run.reason
	}
	oldRoot.RemoveChild(oldMark)
	newRoot.RemoveChild(mark)
	delta := strings.Count(replacement, "\n") - strings.Count(src.Text[start:end], "\n")
	if !insertionPreserves(ctx, m, after, renderHTMLRoot(oldRoot), renderHTMLRoot(newRoot), last+1, delta) {
		return nil, run.reason
	}
	run.block = block{tag: "html", kind: "html", public: actual, actual: actual, start: start, end: end, line: line, outer: -1}
	run.reason = nil
	return &run.block, nil
}

func (s *Service) htmlWritable(ctx context.Context, src session.Source, m *blockMapping, r Range) (*block, *Error) {
	runs, e := m.htmlRuns(ctx, src)
	if e != nil {
		return nil, e
	}
	for _, run := range runs {
		candidate, e := m.htmlCandidate(src, run)
		if e != nil || candidate.actual != r {
			continue
		}
		b, e := s.verifyHTML(ctx, src, m, run)
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
	for _, b := range []*HTMLBoundary{prev, next} {
		if b != nil {
			if !tagPattern.MatchString(b.Tag) {
				return nil, invalid()
			}
			if _, e := ParseSourceRange(b.Tag, string(b.Range)); e != nil {
				return nil, e
			}
		}
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
	b, e := s.verifyHTML(ctx, src, m, found)
	if e != nil {
		return nil, e
	}
	return map[string]any{"text": removeCR(src.Text[b.start:b.end]), "render": src.Render, "range": b.actual, "kind": "html", "outer": []OuterBlock{}, "anchor": &InsertAnchor{"html", b.actual, b.actual}}, nil
}

func (s *Service) htmlOuter(ctx context.Context, src session.Source, m *blockMapping, b *block, outer []OuterBlock) []OuterBlock {
	if run := m.rawRunOf(ctx, src, b); run != nil {
		if raw, e := s.verifyHTML(ctx, src, m, run); e == nil {
			return append(outer, OuterBlock{"html", raw.actual, "html"})
		}
	}
	return outer
}
