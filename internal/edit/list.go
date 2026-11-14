package edit

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
	"golang.org/x/net/html"
)

type listIndentOperation struct {
	target    blockMoveTarget
	direction string
}

type listIndentInfo struct {
	Target  blockMoveTarget `json:"target"`
	Indent  string          `json:"indent"`
	Outdent string          `json:"outdent"`
}

const listIndentCompareReason = "所属が変わるか、本文・リストの種類・前後の解釈が変わるので、段を変えられません。"

func parseListIndent(m map[string]any) (listIndentOperation, *Error) {
	o := listIndentOperation{}
	if !fields(m, "type", "target", "direction") || m["type"] != "listIndent" {
		return o, invalid()
	}
	o.direction, _ = m["direction"].(string)
	if o.direction != "indent" && o.direction != "outdent" {
		return o, invalid()
	}
	t, ok := m["target"].(map[string]any)
	if !ok || !fields(t, "tag", "range") || t["tag"] != "li" {
		return o, invalid()
	}
	r, ok := t["range"].(string)
	if !ok {
		return o, invalid()
	}
	var e *Error
	o.target.Tag = "li"
	o.target.Range, e = ParseSourceRange("li", r)
	return o, e
}

func listNode(n *html.Node) bool {
	return n != nil && n.Type == html.ElementNode && (n.Data == "ul" || n.Data == "ol")
}

func listSibling(n *html.Node, forward bool) *html.Node {
	for {
		if forward {
			n = n.NextSibling
		} else {
			n = n.PrevSibling
		}
		if n == nil || n.Type == html.ElementNode {
			return n
		}
		if n.Type != html.TextNode || strings.TrimSpace(n.Data) != "" {
			return nil
		}
	}
}

var listMarker = regexp.MustCompile(`^(?:[-+*]|[0-9]{1,9}[.)])(?: +|$)`)

type listIndentPlan struct {
	root, node, parent, previous *html.Node
	actual                       Range // 対象の項目の実際の範囲。描画の属性の位置ではなく、一覧で補正した範囲を使う。
	first, end, delta            int
}

// 範囲は公開の位置から一覧を引き直す。ページが送る行や桁は使わない。
func listItem(ctx context.Context, src session.Source, m *blockMapping, root *html.Node, target blockMoveTarget) (*html.Node, *block, int, int, int, *Error) {
	b, e := m.target(target.Tag, target.Range)
	if e != nil {
		return nil, nil, 0, 0, 0, e
	}
	if b.kind != "item" {
		return nil, nil, 0, 0, 0, fault(422, "excluded", "脚注の定義の段は変えられません。")
	}
	nodes, err := htmltree.Elements(ctx, root, func(n *html.Node) bool {
		return n.Data == "li" && htmltree.Get(n, "data-sourcepos") == string(b.public)
	})
	if err != nil || len(nodes) != 1 || !listNode(nodes[0].Parent) {
		return nil, nil, 0, 0, 0, blockFault("target")
	}
	c, e := coordinates(b.actual)
	if e != nil {
		return nil, nil, 0, 0, 0, e
	}
	first, reason := src.Lines.Original(c[0])
	last, endReason := src.Lines.Original(c[2])
	if reason != "" || endReason != "" {
		return nil, nil, 0, 0, 0, blockFault("boundary")
	}
	lines := headingLines(src.Text)
	if first < 1 || last > len(lines) || c[1] < 1 || c[1] > len(lines[first-1]) {
		return nil, nil, 0, 0, 0, blockFault("boundary")
	}
	line := lines[first-1]
	start := c[1] - 1
	prefix := line[:start]
	if strings.Trim(prefix, " >") != "" {
		return nil, nil, 0, 0, 0, fault(422, "excluded", "空白だけで字下げしたリストの項目を選んでください。")
	}
	marker := listMarker.FindString(line[start:])
	if marker == "" {
		return nil, nil, 0, 0, 0, blockFault("boundary")
	}
	// CommonMark では印の後ろが 5 桁以上なら本文の字下げは 1 桁になる。
	width := len(marker) - len(strings.TrimRight(marker, " "))
	if width > 4 {
		width = 1
	}
	content := start + len(strings.TrimRight(marker, " ")) + max(1, width)
	return nodes[0], b, first - 1, last, content, nil
}

func (s *Service) planListIndent(ctx context.Context, src session.Source, m *blockMapping, o listIndentOperation) (*listIndentPlan, *Error) {
	root, err := htmltree.Fragment(ctx, src.HTML)
	if err != nil {
		return nil, blockFault("boundary")
	}
	n, b, first, end, _, e := listItem(ctx, src, m, root, o.target)
	if e != nil {
		return nil, e
	}
	p := &listIndentPlan{root: root, node: n, actual: b.actual, first: first, end: end}
	for _, b := range m.blocks {
		if b.kind == "footnote" && b.reason == nil && b.line > first && b.line <= end {
			return nil, fault(422, "excluded", "脚注の定義を含む項目の段は変えられません。")
		}
	}
	var relative *html.Node
	if o.direction == "indent" {
		p.previous = listSibling(n, false)
		if p.previous == nil || p.previous.Data != "li" {
			return nil, fault(422, "excluded", "リストの先頭の項目は下げられません。直前の項目が必要です。")
		}
		relative = p.previous
	} else {
		p.parent = n.Parent.Parent
		if p.parent == nil || p.parent.Data != "li" || !listNode(p.parent.Parent) {
			return nil, fault(422, "excluded", "いちばん外側の項目は上げられません。")
		}
		if listSibling(n, true) != nil {
			return nil, fault(422, "excluded", "後ろの項目の所属が変わるので、後ろの項目から上げてください。")
		}
		relative = p.parent
	}
	_, rb, rf, re, content, e := listItem(ctx, src, m, root, blockMoveTarget{Tag: "li", Range: Range(htmltree.Get(relative, "data-sourcepos"))})
	if e != nil {
		return nil, e
	}
	c, _ := coordinates(p.actual)
	rc, _ := coordinates(rb.actual)
	// 相手の項目の行も、引用の印の後ろの空白で桁を数えるので、同じ書き方であることを確かめる。
	lines := headingLines(src.Text)
	if _, e := listQuotePrefix(lines[rf], strings.Count(lines[first][:c[1]-1], ">")); e != nil {
		return nil, e
	}
	p.delta = content - (c[1] - 1)
	if o.direction == "outdent" {
		p.delta = rc[1] - c[1]
	}
	if o.direction == "indent" && p.delta <= 0 || o.direction == "outdent" && p.delta >= 0 {
		return nil, blockFault("boundary")
	}
	// 相手と対象の間にある非表示の定義や取り込みも、所属を変えてしまわないよう断る。
	for i := min(first, rf) + 1; i <= max(end, re); i++ {
		original, reason := src.Lines.Original(src.Lines.Expanded(i))
		if original != i || reason != "" {
			return nil, fault(422, "excluded", "取り込みの指定・取り込んだ行・front matter を含む範囲の段は変えられません。")
		}
	}
	if _, e := p.rewrite(src); e != nil {
		return nil, e
	}
	return p, nil
}

// 対象を包む引用の印（depth 個の > と、それぞれの後ろの空白 1 個）の幅を返す。印の後ろに空白が無い行では、
// 足した空白の最初の 1 個が印の一部として読まれて字下げの桁が合わなくなるので断る。
func listQuotePrefix(line string, depth int) (int, *Error) {
	quote := 0
	for range depth {
		for quote < len(line) && line[quote] == ' ' {
			quote++
		}
		if quote >= len(line) || line[quote] != '>' {
			return 0, fault(422, "excluded", "引用の続きの範囲を確かめられないので、段を変えられません。")
		}
		quote++
		if quote < len(line) && line[quote] == ' ' {
			quote++
		} else if quote < len(line) {
			return 0, fault(422, "excluded", "引用の印の後ろに空白が無い項目の段は変えられません。")
		}
	}
	return quote, nil
}

func (p *listIndentPlan) rewrite(src session.Source) (headingRewrite, *Error) {
	lines := originalHeadingLines(src.Text)
	c, _ := coordinates(p.actual)
	depth := strings.Count(lines[p.first].text[:c[1]-1], ">")
	for i := p.first; i < p.end; i++ {
		line := lines[i].text
		if headingBlank(line) {
			continue
		}
		// 対象を包む引用の印だけを残す。項目の本文にある引用は、本文と一緒にずらす。
		quote, e := listQuotePrefix(line, depth)
		if e != nil {
			return headingRewrite{}, e
		}
		if strings.HasPrefix(strings.TrimLeft(line[quote:], " "), "\t") {
			return headingRewrite{}, fault(422, "excluded", "タブで字下げした項目の段は変えられません。")
		}
		if headingBlank(line[quote:]) {
			continue
		}
		if p.delta > 0 {
			line = line[:quote] + strings.Repeat(" ", p.delta) + line[quote:]
		} else {
			width := -p.delta
			if len(line)-quote < width || strings.Trim(line[quote:quote+width], " ") != "" {
				return headingRewrite{}, fault(422, "excluded", "続きの行を同じ幅だけ戻せないので、段を変えられません。")
			}
			line = line[:quote] + line[quote+width:]
		}
		lines[i].text = line
	}
	return finishHeadingRewrite(src.Text, lines, newlineAt(src.Text, p.first+1)), nil
}

// ゆるいリストでは項目の本文が段落に包まれ、右クリックの対象は段落になる。
// 印と同じ行から始まる項目の最初の段落からも、その項目の段を変えられるようにする。
func listIndentTarget(m *blockMapping, b *block, target blockMoveTarget) blockMoveTarget {
	if target.Tag != "li" && b.kind == "paragraph" && b.outer >= 0 {
		if item := &m.blocks[b.outer]; item.tag == "li" && item.kind == "item" && item.line == b.line {
			return blockMoveTarget{Tag: "li", Range: item.public}
		}
	}
	return target
}

func (s *Service) listIndentInformation(ctx context.Context, src session.Source, m *blockMapping, target blockMoveTarget) *listIndentInfo {
	if target.Tag != "li" {
		return nil
	}
	info := &listIndentInfo{Target: target}
	if _, e := s.planListIndent(ctx, src, m, listIndentOperation{target, "indent"}); e != nil {
		info.Indent = e.Message
	}
	if _, e := s.planListIndent(ctx, src, m, listIndentOperation{target, "outdent"}); e != nil {
		info.Outdent = e.Message
	}
	return info
}

// 項目だけを動かした期待の木を作る。きつい・ゆるいリストの段落は変えない。
func (p *listIndentPlan) expected(direction string, src session.Source) {
	n, list := p.node, p.node.Parent
	list.RemoveChild(n)
	if direction == "outdent" {
		outer := p.parent.Parent
		outer.InsertBefore(n, p.parent.NextSibling)
		listTaskClass(outer)
		if list.FirstChild == nil || listSiblingCount(list) == 0 {
			list.Parent.RemoveChild(list)
		} else {
			listTaskClass(list)
		}
		return
	}
	var dest *html.Node
	for c := p.previous.LastChild; c != nil; c = c.PrevSibling {
		if c.Type == html.TextNode && strings.TrimSpace(c.Data) == "" {
			continue
		}
		if listNode(c) && c.Data == list.Data {
			dest = c
		}
		break
	}
	if dest == nil {
		dest = &html.Node{Type: html.ElementNode, Data: list.Data}
		if list.Data == "ol" {
			c, _ := coordinates(p.actual)
			line := headingLines(src.Text)[p.first][c[1]-1:]
			end := strings.IndexAny(line, ".)")
			if end > 0 {
				number, _ := strconv.Atoi(line[:end])
				if number != 1 {
					htmltree.Set(dest, "start", strconv.Itoa(number))
				}
			}
		}
		p.previous.AppendChild(dest)
	}
	dest.AppendChild(n)
	listTaskClass(list)
	listTaskClass(dest)
}

// task-list は直下のタスクの有無から描画器が付ける。所属を変える二つのリストだけを更新する。
func listTaskClass(n *html.Node) {
	task := false
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "li" && htmltree.HasClass(c, "task-list-item") {
			task = true
		}
	}
	classes := strings.Fields(htmltree.Get(n, "class"))
	classes = slices.DeleteFunc(classes, func(c string) bool { return c == "task-list" })
	if task {
		classes = append(classes, "task-list")
	}
	if len(classes) > 0 {
		htmltree.Set(n, "class", strings.Join(classes, " "))
	} else {
		n.Attr = slices.DeleteFunc(n.Attr, func(a html.Attribute) bool { return a.Namespace == "" && a.Key == "class" })
	}
}

func listSiblingCount(n *html.Node) int {
	count := 0
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "li" {
			count++
		}
	}
	return count
}

// リストの入れ子の境に描画器が足す改行と、項目の直下の段落の包みを除く。包みは、項目の所属が変わって
// リストがきつい・ゆるいのどちらになるかで付け外しされるだけなので比べない。本文・ほかの段落・属性は保持し、
// 属性は名前順で比較する。
func listIndentSignature(root *html.Node) string {
	root = htmltree.Clone(root)
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Data == "li" && n.Type == html.ElementNode {
			for c := n.FirstChild; c != nil; {
				next := c.NextSibling
				if c.Type == html.ElementNode && c.Data == "p" {
					for c.FirstChild != nil {
						child := c.FirstChild
						c.RemoveChild(child)
						n.InsertBefore(child, c)
					}
					n.RemoveChild(c)
				}
				c = next
			}
		}
		attrs := n.Attr[:0]
		for _, a := range n.Attr {
			if a.Namespace == "" && (a.Key == "data-sourcepos" || a.Key == "data-checkbox-sourcepos") {
				continue
			}
			attrs = append(attrs, a)
		}
		n.Attr = attrs
		slices.SortFunc(n.Attr, func(a, b html.Attribute) int {
			if c := strings.Compare(a.Namespace, b.Namespace); c != 0 {
				return c
			}
			if c := strings.Compare(a.Key, b.Key); c != 0 {
				return c
			}
			return strings.Compare(a.Val, b.Val)
		})
		for c := n.FirstChild; c != nil; {
			next := c.NextSibling
			if c.Type == html.TextNode && (listNode(n) || n.Data == "li") {
				if n.Data == "li" && strings.TrimSpace(c.Data) == "" {
					c.Data = strings.Trim(c.Data, "\n")
				}
				if c.Data == "" || listNode(n) && strings.TrimSpace(c.Data) == "" {
					n.RemoveChild(c)
					c = next
					continue
				}
			}
			visit(c)
			c = next
		}
		if n.Data == "li" {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode && (c.NextSibling == nil || listNode(c.NextSibling)) {
					c.Data = strings.TrimSuffix(c.Data, "\n")
				}
			}
		}
	}
	visit(root)
	var out strings.Builder
	_ = html.Render(&out, root)
	return out.String()
}

func (s *Service) indentList(ctx context.Context, src session.Source, o listIndentOperation) (headingRewrite, *Error) {
	m, e := s.blocks(ctx, src)
	if e != nil {
		return headingRewrite{}, e
	}
	p, e := s.planListIndent(ctx, src, m, o)
	if e != nil {
		return headingRewrite{}, e
	}
	rewritten, e := p.rewrite(src)
	if e != nil {
		return headingRewrite{}, e
	}
	trial, err := s.opts.Host.Trial(ctx, src, rewritten.text)
	if err != nil {
		return headingRewrite{}, fault(422, "render", "段を変えた文書を描画できません。")
	}
	root, err := htmltree.Fragment(ctx, trial.HTML)
	if err != nil {
		return headingRewrite{}, fault(422, "render", "段を変えた文書を描画できません。")
	}
	p.expected(o.direction, src)
	if ctx.Err() != nil || listIndentSignature(p.root) != listIndentSignature(root) {
		return headingRewrite{}, fault(422, "render", listIndentCompareReason)
	}
	return rewritten, nil
}
