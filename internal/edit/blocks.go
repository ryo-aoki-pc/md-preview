package edit

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
	"golang.org/x/net/html"
)

var tagPattern = regexp.MustCompile(`^[a-z0-9]+$`)
var diagramFencePattern = regexp.MustCompile("^(?:`{3,}|~{3,})")

type block struct {
	tag, kind        string
	public, actual   Range
	start, end, line int
	reason           *Error
	outer            int
}

// OuterBlock は、公開の位置で取り直せる外側の編集対象。
type OuterBlock struct {
	Tag   string `json:"tag"`
	Range Range  `json:"range"`
	Kind  string `json:"kind"`
}

type positionAttribute struct {
	tag, name, value string
}

type blockMapping struct {
	render      uint64
	blocks      []block
	checkboxes  map[Range]*Error
	untrusted   map[positionAttribute]bool
	positionErr *Error
}

func (m *blockMapping) positionReason(n *html.Node, name string) *Error {
	if m.positionErr != nil {
		return m.positionErr
	}
	if m.untrusted[positionAttribute{n.Data, name, htmltree.Get(n, name)}] {
		return blockFault("untrusted")
	}
	return nil
}

// エンジンの位置付けを止めても残る属性は、利用者が書いたものとして区別する。
func (s *Service) untrustedPositions(ctx context.Context, src session.Source, m *blockMapping) {
	if !src.HasSourcepos {
		return
	}
	result, err := s.opts.Host.Unpositioned(ctx, src)
	if err == nil && result.SourceposInput != src.SourceposInput {
		// 取り込んだファイルが描画の後に変わると、もとの描画と違う入力を描くことになる。
		err = errors.New("位置なしの描画の入力が、もとの描画と違う")
	}
	if err == nil {
		var root *html.Node
		root, err = htmltree.Fragment(ctx, result.HTML)
		if err == nil {
			err = htmltree.Walk(ctx, root, func(n *html.Node) error {
				if n.Type == html.ElementNode {
					for _, a := range n.Attr {
						if a.Namespace == "" && (a.Key == "data-sourcepos" || a.Key == "data-checkbox-sourcepos") {
							m.untrusted[positionAttribute{n.Data, a.Key, a.Val}] = true
						}
					}
				}
				return nil
			})
		}
	}
	if err != nil {
		m.positionErr = fault(422, "untrusted", "原文の位置を確かめられません。")
	}
}

func blockFault(code string) *Error {
	message := map[string]string{
		"untrusted": "この位置は、文書に書かれた属性と区別できないので、編集できません。",
		"target":    "この範囲は、画面のブロックと対応していません。もう一度操作してください。",
		"boundary":  "このブロックは、原文の範囲を確かめられません。",
		"term":      "この用語は、原文の範囲を決められません。",
		"ambiguous": "同じ位置のブロックが複数あるので、原文の範囲を決められません。",
	}[code]
	return fault(422, code, message)
}

func blockKind(tag string) string {
	switch tag {
	case "p":
		return "paragraph"
	case "h1", "h2", "h3", "h4", "h5", "h6":
		return "heading"
	case "li":
		return "item"
	case "ul", "ol":
		return "list"
	case "dl":
		return "deflist"
	case "dt":
		return "term"
	case "dd":
		return "description"
	case "blockquote":
		return "quote"
	case "pre":
		return "code"
	}
	return ""
}

func excludedBlock(n *html.Node) bool {
	return n.Type == html.ElementNode && (n.Data == "table" ||
		htmltree.Has(n, "data-diagram") && htmltree.Get(n, "data-diagram") != "plantuml" || htmltree.HasClass(n, "section-nav") ||
		htmltree.HasClass(n, "js-render-math") && !(n.Data == "pre" && htmltree.Get(n, "data-math-style") == "display"))
}

func footnoteSection(n *html.Node) bool {
	return n.Type == html.ElementNode && n.Data == "section" && htmltree.Has(n, "data-footnotes")
}

func footnoteDefinition(n *html.Node) bool {
	return n.Type == html.ElementNode && n.Data == "li" && n.Parent != nil && n.Parent.Data == "ol" &&
		n.Parent.Parent != nil && footnoteSection(n.Parent.Parent)
}

func footnoteLabel(text string) string {
	if !strings.HasPrefix(text, "[^") {
		return ""
	}
	end := strings.IndexByte(text[2:], ']')
	if end < 1 || !strings.HasPrefix(text[2+end:], "]:") {
		return ""
	}
	label := text[2 : 2+end]
	if strings.ContainsFunc(label, unicode.IsSpace) {
		return ""
	}
	return label
}

func excludedCheckbox(n *html.Node) bool {
	return n.Type == html.ElementNode && (n.Data == "table" || n.Data == "section" && htmltree.Has(n, "data-footnotes"))
}

func (s *Service) blocks(ctx context.Context, src session.Source) (*blockMapping, *Error) {
	key := pathKey(src.Path)
	if m := s.blockCache[key]; m != nil && m.render == src.Render {
		return m, nil
	}
	root, err := htmltree.Fragment(ctx, src.HTML)
	if err != nil {
		return nil, fault(422, "render", "描画したブロックを読み取れません。")
	}
	m := &blockMapping{render: src.Render, checkboxes: map[Range]*Error{}, untrusted: map[positionAttribute]bool{}}
	s.untrustedPositions(ctx, src, m)
	lines := strings.Split(src.Text, "\n")
	starts := lineStarts(src.Text)
	nodes := []*html.Node{}
	indices := map[*html.Node]int{}
	err = htmltree.Walk(ctx, root, func(n *html.Node) error {
		if n.Type != html.ElementNode {
			return nil
		}
		// ページは表と脚注の中のチェックボックスを扱わない。JSON の表のセルには利用者が属性を書けるので、一覧にも入れない。
		if n.Data == "input" && htmltree.HasClass(n, "task-list-item-checkbox") && !htmltree.Ancestor(n, excludedCheckbox) {
			r := Range(htmltree.Get(n, "data-checkbox-sourcepos"))
			if rangePattern.MatchString(string(r)) {
				m.checkboxes[r] = m.positionReason(n, "data-checkbox-sourcepos")
			}
		}
		kind := blockKind(n.Data)
		if n.Data == "img" && (htmltree.Get(n, "data-diagram") == "plantuml" ||
			n.Parent != nil && n.Parent.Data == "a" && htmltree.Get(n.Parent, "data-diagram") == "plantuml") {
			kind = "plantuml"
		}
		if n.Data == "div" && htmltree.HasClass(n, "markdown-alert") {
			kind = "alert"
		}
		if footnoteDefinition(n) {
			kind = "footnote"
		}
		r := Range(htmltree.Get(n, "data-sourcepos"))
		if kind == "" || !rangePattern.MatchString(string(r)) || excludedBlock(n) || htmltree.Ancestor(n, excludedBlock) ||
			htmltree.Ancestor(n, footnoteSection) && kind != "footnote" {
			return nil
		}
		if n.Data == "pre" {
			if htmltree.HasClass(n, "js-render-math") && htmltree.Get(n, "data-math-style") == "display" {
				kind = "math"
			}
			mermaid, e := htmltree.Elements(ctx, n, func(c *html.Node) bool {
				return c.Data == "code" && htmltree.HasClass(c, "js-render-mermaid")
			})
			if e != nil {
				return e
			}
			if len(mermaid) > 0 {
				kind = "mermaid"
			}
		}
		b := block{tag: n.Data, kind: kind, public: r, outer: -1}
		for parent := n.Parent; kind != "footnote" && parent != nil; parent = parent.Parent {
			if i, ok := indices[parent]; ok {
				b.outer = i
				break
			}
		}
		b.actual, b.reason = blockRange(src, lines, starts, n, r)
		if reason := m.positionReason(n, "data-sourcepos"); reason != nil {
			b.reason = reason
		}
		if b.reason == nil && n.Data == "dt" {
			// 用語の終端を決めるために読む定義の位置も、利用者の属性なら信用しない。
			for dd := n.NextSibling; dd != nil; dd = dd.NextSibling {
				if dd.Type == html.ElementNode {
					b.reason = m.positionReason(dd, "data-sourcepos")
					break
				}
			}
		}
		if b.reason == nil {
			b.start, b.end, b.line, b.reason = offsetsAt(src, b.actual, starts)
		}
		if b.reason == nil && b.kind == "footnote" && footnoteLabel(src.Text[b.start:b.end]) == "" {
			b.reason = blockFault("boundary")
		}
		if b.reason == nil && b.kind == "plantuml" && !diagramFencePattern.MatchString(src.Text[b.start:b.end]) {
			b.reason = blockFault("boundary")
		}
		indices[n] = len(m.blocks)
		nodes = append(nodes, n)
		m.blocks = append(m.blocks, b)
		return nil
	})
	if err != nil {
		return nil, fault(422, "cancelled", "ブロックの対応の確認を取り消しました。")
	}
	// 注記の閉じの行が欠けた祖先も、公開の終端が同じで、注記が最後にある場合だけ補う。
	// 注記の終端を決められなかったときは、同じ終端の祖先も決められないので断る。
	for i := len(m.blocks) - 1; i >= 0; i-- {
		b := &m.blocks[i]
		if b.kind != "alert" {
			continue
		}
		public, e := coordinates(b.public)
		if e != nil {
			continue
		}
		var actual [4]int
		refused := b.reason != nil && b.reason.Code == "boundary"
		if b.reason == nil {
			actual, _ = coordinates(b.actual)
		}
		if !refused && (b.reason != nil || actual[2] < public[2] || actual[2] == public[2] && actual[3] <= public[3]) {
			continue
		}
		for child := nodes[i]; child.Parent != nil; child = child.Parent {
			last := true
			for sibling := child.NextSibling; sibling != nil; sibling = sibling.NextSibling {
				if sibling.Type == html.ElementNode || sibling.Type == html.TextNode && strings.TrimSpace(sibling.Data) != "" {
					last = false
					break
				}
			}
			if !last {
				break
			}
			if index, ok := indices[child.Parent]; ok {
				ancestor := &m.blocks[index]
				end, _ := coordinates(ancestor.public)
				if end[2] == public[2] && end[3] == public[3] && (ancestor.reason == nil || ancestor.reason.Code == "boundary") {
					if refused {
						ancestor.reason = blockFault("boundary")
						continue
					}
					end[2], end[3] = actual[2], actual[3]
					ancestor.actual = rangeOf(end)
					ancestor.reason = blockBoundary(src, lines, starts, ancestor.actual)
					if ancestor.reason == nil {
						ancestor.start, ancestor.end, ancestor.line, ancestor.reason = offsetsAt(src, ancestor.actual, starts)
					}
				}
			}
		}
	}
	// 表と同じく、文書ごとに最新の描画だけを上限付きで保持する。
	if len(s.blockCache) >= historyDocuments {
		for k := range s.blockCache {
			delete(s.blockCache, k)
			break
		}
	}
	s.blockCache[key] = m
	return m, nil
}

func blockLine(src session.Source, lines []string, line int) (string, *Error) {
	if line < 1 || line > src.Lines.Expanded(len(lines)) {
		return "", fault(422, "range", "原文の行が範囲外です。")
	}
	original, reason := src.Lines.Original(line)
	if reason != "" {
		return "", fault(422, reason, reasonText(reason))
	}
	if original < 1 || original > len(lines) {
		return "", fault(422, "range", "原文の行が範囲外です。")
	}
	return strings.TrimSuffix(lines[original-1], "\r"), nil
}

func rangeOf(c [4]int) Range {
	return Range(fmt.Sprintf("%d:%d-%d:%d", c[0], c[1], c[2], c[3]))
}

func blockRange(src session.Source, lines []string, starts []int, n *html.Node, r Range) (Range, *Error) {
	c, e := coordinates(r)
	if e != nil {
		return r, e
	}
	if c[3] == 0 {
		// 終了列 0 は前の行までなので、最後の行の次の行を指していてもよい。
		if c[2]-1 > src.Lines.Expanded(len(lines)) {
			return r, fault(422, "range", "原文の行が範囲外です。")
		}
		c[2]--
		for ; c[2] >= c[0]; c[2]-- {
			line, err := blockLine(src, lines, c[2])
			if err != nil {
				return r, err
			}
			if strings.Trim(line, " \t") != "" {
				c[3] = len(line)
				break
			}
		}
		if c[2] < c[0] {
			return r, blockFault("boundary")
		}
	}
	if n.Data == "dt" && c[0] == c[2] && c[1] == c[3] {
		c, e = termRange(src, lines, n, c)
		if e != nil {
			return r, e
		}
	}
	actual := rangeOf(c)
	if n.Data == "div" && htmltree.HasClass(n, "markdown-alert") {
		if _, _, _, e = offsetsAt(src, actual, starts); e != nil {
			return actual, e
		}
		c, e = alertRange(src, lines, c)
		if e != nil {
			return actual, e
		}
		actual = rangeOf(c)
	}
	return actual, blockBoundary(src, lines, starts, actual)
}

func blockBoundary(src session.Source, lines []string, starts []int, actual Range) *Error {
	c, e := coordinates(actual)
	if e != nil {
		return e
	}
	if _, _, _, e = offsetsAt(src, actual, starts); e != nil {
		return e
	}
	line, e := blockLine(src, lines, c[2])
	if e != nil {
		return e
	}
	if strings.Trim(line[c[3]:], " \t") != "" {
		return blockFault("boundary")
	}
	return nil
}

func alertRange(src session.Source, lines []string, c [4]int) ([4]int, *Error) {
	first, e := blockLine(src, lines, c[0])
	if e != nil {
		return c, e
	}
	if !strings.HasPrefix(first[c[1]-1:], ">>>") {
		return c, nil
	}
	// 閉じの行の前に来るのは、開始行の前の文字と同じ幅のインデントか引用の印。
	// リストや説明の印（- や 1. や :）は、続きの行ではインデントになる。
	prefix := []byte(first[:c[1]-1])
	for i, ch := range prefix {
		if ch != ' ' && ch != '\t' && ch != '>' {
			prefix[i] = ' '
		}
	}
	closing := string(prefix) + ">>>"
	// 公開の範囲が閉じの行まで含んでいれば、そのまま使う（上流の位置が直ったときに断らない）。
	if c[2] > c[0] {
		last, e := blockLine(src, lines, c[2])
		if e != nil {
			return c, e
		}
		if strings.TrimRight(last, " \t") == closing {
			return c, nil
		}
	}
	for line := c[2] + 1; line <= src.Lines.Expanded(len(lines)); line++ {
		text, e := blockLine(src, lines, line)
		if e != nil {
			return c, e
		}
		if strings.Trim(text, " \t") == "" {
			continue
		}
		if strings.TrimRight(text, " \t") != closing {
			return c, blockFault("boundary")
		}
		c[2], c[3] = line, len(closing)
		return c, nil
	}
	// 閉じずに文書が終わる注記は、公開された終端のまま扱う。
	return c, nil
}

func termRange(src session.Source, lines []string, n *html.Node, c [4]int) ([4]int, *Error) {
	dd := n.NextSibling
	for dd != nil && dd.Type != html.ElementNode {
		dd = dd.NextSibling
	}
	if dd == nil || dd.Data != "dd" {
		return c, blockFault("term")
	}
	d, e := coordinates(Range(htmltree.Get(dd, "data-sourcepos")))
	if e != nil || d[0] <= c[0] {
		return c, blockFault("term")
	}
	line, e := blockLine(src, lines, d[0])
	if e != nil {
		return c, e
	}
	if d[1] > len(line) || (line[d[1]-1] != ':' && line[d[1]-1] != '~') {
		return c, blockFault("term")
	}
	blank := " \t"
	if htmltree.Ancestor(n, func(p *html.Node) bool { return p.Type == html.ElementNode && p.Data == "blockquote" }) {
		blank += ">"
	}
	c[2] = d[0] - 1
	for ; c[2] >= c[0]; c[2]-- {
		line, e = blockLine(src, lines, c[2])
		if e != nil {
			return c, e
		}
		if strings.Trim(line, blank) != "" {
			c[3] = len(line)
			break
		}
	}
	if c[2] < c[0] {
		return c, blockFault("term")
	}
	for i := c[0]; i < c[2]; i++ {
		line, e = blockLine(src, lines, i)
		if e != nil {
			return c, e
		}
		if strings.Trim(line, blank) == "" {
			return c, blockFault("term")
		}
	}
	return c, nil
}

func (m *blockMapping) target(tag string, r Range) (*block, *Error) {
	var found *block
	for i := range m.blocks {
		b := &m.blocks[i]
		if b.tag == tag && b.public == r {
			if found != nil {
				return nil, blockFault("ambiguous")
			}
			found = b
		}
	}
	if found == nil {
		return nil, blockFault("target")
	}
	return found, found.reason
}

func (m *blockMapping) writable(r Range) (*block, *Error) {
	if m.positionErr != nil {
		return nil, m.positionErr
	}
	var denied *Error
	for i := range m.blocks {
		b := &m.blocks[i]
		if b.reason == nil && b.actual == r {
			return b, nil
		}
		if b.reason != nil && b.reason.Code == "untrusted" && (b.actual == r || b.public == r) {
			denied = b.reason
		}
	}
	if denied != nil {
		return nil, denied
	}
	return nil, blockFault("target")
}

func (m *blockMapping) outerBlocks(b *block) []OuterBlock {
	outer := []OuterBlock{}
	last := Range("")
	if b.reason == nil {
		last = b.actual
	}
	for i := b.outer; i >= 0; i = m.blocks[i].outer {
		ancestor := &m.blocks[i]
		if _, e := m.target(ancestor.tag, ancestor.public); e != nil {
			continue
		}
		// 項目が 1 つだけのリストのように、内側と同じ範囲の対象は、移っても原文が変わらないので飛ばす。
		if ancestor.actual == last {
			continue
		}
		outer = append(outer, OuterBlock{ancestor.tag, ancestor.public, ancestor.kind})
		last = ancestor.actual
	}
	return outer
}
