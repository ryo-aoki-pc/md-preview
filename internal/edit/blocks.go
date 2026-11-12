package edit

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
	"golang.org/x/net/html"
)

var tagPattern = regexp.MustCompile(`^[a-z0-9]+$`)

type block struct {
	tag, kind        string
	public, actual   Range
	start, end, line int
	reason           *Error
}

type blockMapping struct {
	render     uint64
	blocks     []block
	checkboxes map[Range]bool
}

func blockFault(code string) *Error {
	message := map[string]string{
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
		n.Data == "section" && htmltree.Has(n, "data-footnotes") ||
		htmltree.Has(n, "data-diagram") || htmltree.HasClass(n, "section-nav") ||
		htmltree.HasClass(n, "js-render-math"))
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
	m := &blockMapping{render: src.Render, checkboxes: map[Range]bool{}}
	lines := strings.Split(src.Text, "\n")
	starts := lineStarts(src.Text)
	err = htmltree.Walk(ctx, root, func(n *html.Node) error {
		if n.Type != html.ElementNode {
			return nil
		}
		if n.Data == "input" && htmltree.HasClass(n, "task-list-item-checkbox") {
			r := Range(htmltree.Get(n, "data-checkbox-sourcepos"))
			if rangePattern.MatchString(string(r)) {
				m.checkboxes[r] = true
			}
		}
		kind := blockKind(n.Data)
		r := Range(htmltree.Get(n, "data-sourcepos"))
		if kind == "" || !rangePattern.MatchString(string(r)) || excludedBlock(n) || htmltree.Ancestor(n, excludedBlock) {
			return nil
		}
		if n.Data == "pre" {
			mermaid, e := htmltree.Elements(ctx, n, func(c *html.Node) bool { return htmltree.HasClass(c, "js-render-mermaid") })
			if e != nil {
				return e
			}
			if len(mermaid) > 0 {
				return nil
			}
		}
		b := block{tag: n.Data, kind: kind, public: r}
		b.actual, b.reason = blockRange(src, lines, starts, n, r)
		if b.reason == nil {
			b.start, b.end, b.line, b.reason = offsetsAt(src, b.actual, starts)
		}
		m.blocks = append(m.blocks, b)
		return nil
	})
	if err != nil {
		return nil, fault(422, "cancelled", "ブロックの対応の確認を取り消しました。")
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
	if _, _, _, e = offsetsAt(src, actual, starts); e != nil {
		return actual, e
	}
	line, e := blockLine(src, lines, c[2])
	if e != nil {
		return actual, e
	}
	if strings.Trim(line[c[3]:], " \t") != "" {
		return actual, blockFault("boundary")
	}
	return actual, nil
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
	for i := range m.blocks {
		b := &m.blocks[i]
		if b.reason == nil && b.actual == r {
			return b, nil
		}
	}
	return nil, blockFault("target")
}
