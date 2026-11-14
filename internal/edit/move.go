package edit

import (
	"context"
	"slices"
	"strings"

	"github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
	"golang.org/x/net/html"
)

// 移動の対象は、公開のブロックの位置、対応づけた表の番号、検証した HTML の範囲で指す。
type blockMoveTarget struct {
	Tag   string `json:"tag,omitempty"`
	Range Range  `json:"range,omitempty"`
	Table *int   `json:"table,omitempty"`
}

type blockMoveOperation struct {
	target    blockMoveTarget
	direction string
}

type blockMoveInfo struct {
	Target blockMoveTarget `json:"target"`
	Up     string          `json:"up"`
	Down   string          `json:"down"`
}

const blockMoveOuterReason = "いちばん外側のブロックだけを移動できます。外側の原文を開いてください。"
const blockMoveHeadingReason = "見出しはまたげません。見出しは「節を上へ移動」「節を下へ移動」で操作してください。"
const blockMoveCompareReason = "移動するとブロックがつながるか、前後・表・生の HTML の解釈が変わるので、書き込みませんでした。"

func parseBlockMove(m map[string]any) (blockMoveOperation, *Error) {
	o := blockMoveOperation{}
	if !fields(m, "type", "target", "direction") || m["type"] != "moveBlock" {
		return o, invalid()
	}
	o.direction, _ = m["direction"].(string)
	if o.direction != "up" && o.direction != "down" {
		return o, invalid()
	}
	target, ok := m["target"].(map[string]any)
	if !ok {
		return o, invalid()
	}
	if fields(target, "table") {
		n, ok := integer(target["table"], 0)
		if !ok {
			return o, invalid()
		}
		o.target.Table = &n
		return o, nil
	}
	if !fields(target, "tag", "range") {
		return o, invalid()
	}
	o.target.Tag, ok = target["tag"].(string)
	value, valid := target["range"].(string)
	if !ok || !tagPattern.MatchString(o.target.Tag) || !valid {
		return o, invalid()
	}
	var e *Error
	o.target.Range, e = ParseSourceRange(o.target.Tag, value)
	return o, e
}

type movableBlock struct {
	target     blockMoveTarget
	block      *block
	raw        *htmlRun
	first, end int // 原文の 0 起点、終端を含まない行範囲。
	nodes      []*html.Node
	reason     string
}

type blockMoveMapping struct {
	root    *html.Node
	entries []*movableBlock
	lines   []string
}

func sameMoveTarget(a, b blockMoveTarget) bool {
	if a.Table != nil || b.Table != nil {
		return a.Table != nil && b.Table != nil && *a.Table == *b.Table
	}
	return a.Tag == b.Tag && a.Range == b.Range
}

// 情報の読み取りでは試しに描画しない。HTML の範囲と入れ替えた後の解釈は、確定するときに検証する。
func (s *Service) blockMoves(ctx context.Context, src session.Source, m *blockMapping, tables *mapping) (*blockMoveMapping, *Error) {
	root, err := htmltree.Fragment(ctx, src.HTML)
	if err != nil {
		return nil, blockFault("boundary")
	}
	h := &blockMoveMapping{root: root, lines: headingLines(src.Text)}
	runs, e := m.htmlRuns(ctx, src)
	if e != nil {
		return nil, e
	}
	// htmlRuns の木は別の写しなので、節点そのものではなく前後の境で対応させる。
	var previous *HTMLBoundary
	tableIndex := 0
	for n := root.FirstChild; n != nil; n = n.NextSibling {
		if footnoteSection(n) || n.Type == html.TextNode && strings.TrimSpace(n.Data) == "" {
			continue
		}
		boundary := m.htmlBoundary(n)
		entry := &movableBlock{nodes: []*html.Node{n}, reason: blockFault("boundary").Message}
		if n.Data == "table" {
			index := tableIndex
			entry.target.Table = &index
			if tables != nil && index < len(tables.response.Tables) {
				info := tables.response.Tables[index]
				entry.reason = info.AnchorReason
				if info.Anchor != "" {
					entry.block = &block{actual: info.Anchor, public: info.Anchor, tag: "table", outer: -1}
				} else if entry.reason == "" {
					entry.reason = "この表の原文の範囲を確かめられません。"
				}
			}
		} else if boundary != nil {
			entry.target = blockMoveTarget{Tag: boundary.Tag, Range: boundary.Range}
			b, e := m.target(boundary.Tag, boundary.Range)
			if e == nil && b.outer < 0 && b.kind != "footnote" {
				entry.block, entry.reason = b, ""
			} else if e != nil {
				entry.reason = e.Message
			}
		} else {
			// 表は位置なしの連なりにも含まれる。最上位の単独の表は、表の対応を優先する。
			for _, run := range runs {
				if !sameBoundary(run.prev, previous) {
					continue
				}
				entry.raw = run
				entry.nodes = nil
				for c := n; c != nil && !footnoteSection(c) && m.htmlBoundary(c) == nil; c = c.NextSibling {
					if c.Type == html.ElementNode || c.Type == html.TextNode && strings.TrimSpace(c.Data) != "" {
						entry.nodes = append(entry.nodes, c)
					}
					n = c
				}
				b, _, e := m.htmlCandidate(src, run, true)
				if run.checked {
					b, e = &run.block, run.reason
				}
				if e == nil {
					entry.block, entry.reason = b, ""
					entry.target = blockMoveTarget{Tag: "html", Range: b.actual}
				}
				break
			}
		}
		// 番号は入れ子の表も数え、表の一覧と同じ前順を使う。
		for _, node := range entry.nodes {
			children, _ := tableNodes(ctx, node)
			tableIndex += len(children)
		}
		if boundary != nil {
			previous = boundary
		}
		if entry.block != nil {
			c, e := coordinates(entry.block.actual)
			if e != nil {
				entry.reason = blockFault("boundary").Message
			} else {
				first, a := src.Lines.Original(c[0])
				last, b := src.Lines.Original(c[2])
				entry.first, entry.end = first-1, last
				if c[3] == 0 {
					entry.end--
				}
				if a != "" || b != "" || entry.first < 0 || entry.end <= entry.first || entry.end > len(h.lines) {
					entry.reason = blockFault("boundary").Message
				}
			}
		}
		for _, node := range entry.nodes {
			if headingNode(node) {
				entry.reason = blockMoveHeadingReason
			}
		}
		h.entries = append(h.entries, entry)
	}
	return h, nil
}

func (h *blockMoveMapping) index(target blockMoveTarget) int {
	for i, e := range h.entries {
		if sameMoveTarget(e.target, target) {
			return i
		}
	}
	return -1
}

// HTML の連なりは、最上位の表も含むことがある。連なりそのものを指したときだけ、
// その範囲の最上位の節点をまとめ、表を別の移動対象として二重に数えない。
func (h *blockMoveMapping) groupHTML(ctx context.Context, src session.Source, m *blockMapping, target blockMoveTarget) {
	if target.Tag != "html" {
		return
	}
	runs, e := m.htmlRuns(ctx, src)
	if e != nil {
		return
	}
	for _, run := range runs {
		if !run.checked || run.reason != nil || run.block.actual != target.Range {
			continue
		}
		nodes := []*html.Node{}
		inside := run.prev == nil
		for n := h.root.FirstChild; n != nil; n = n.NextSibling {
			if footnoteSection(n) {
				continue
			}
			if boundary := m.htmlBoundary(n); boundary != nil {
				if inside {
					break
				}
				inside = sameBoundary(boundary, run.prev)
				continue
			}
			if inside && (n.Type == html.ElementNode || n.Type == html.TextNode && strings.TrimSpace(n.Data) != "") {
				nodes = append(nodes, n)
			}
		}
		first, last := -1, -1
		for i, entry := range h.entries {
			if len(entry.nodes) > 0 && slices.Contains(nodes, entry.nodes[0]) {
				if first < 0 {
					first = i
				}
				last = i
			}
		}
		if first < 0 || len(nodes) == 0 {
			return
		}
		c, e := coordinates(run.block.actual)
		if e != nil {
			return
		}
		a, _ := src.Lines.Original(c[0])
		b, _ := src.Lines.Original(c[2])
		entry := &movableBlock{target: target, block: &run.block, raw: run, first: a - 1, end: b, nodes: nodes}
		for _, n := range nodes {
			if headingNode(n) {
				entry.reason = blockMoveHeadingReason
			}
		}
		h.entries = slices.Replace(h.entries, first, last+1, entry)
		return
	}
}

func (h *blockMoveMapping) pair(src session.Source, target blockMoveTarget, direction string) (*movableBlock, *movableBlock, *Error) {
	i := h.index(target)
	if i < 0 {
		return nil, nil, fault(422, "excluded", blockMoveOuterReason)
	}
	j := i + 1
	if direction == "up" {
		j = i - 1
	}
	if h.entries[i].reason != "" {
		return nil, nil, fault(422, "excluded", h.entries[i].reason)
	}
	if j < 0 || j >= len(h.entries) {
		return nil, nil, fault(422, "excluded", "同じ節の中に、入れ替えるブロックがありません。")
	}
	a, b := h.entries[min(i, j)], h.entries[max(i, j)]
	if h.entries[j].reason != "" {
		return nil, nil, fault(422, "excluded", h.entries[j].reason)
	}
	if a.end > b.first {
		return nil, nil, blockFault("boundary")
	}
	// 描画されない参照の定義なども、隣のブロックと一緒に運ばない。
	for _, line := range h.lines[a.end:b.first] {
		if !headingBlank(line) {
			return nil, nil, fault(422, "excluded", "ブロックの間に取り込みの指定か別の原文があるので、移動できません。")
		}
	}
	for line := a.first + 1; line <= b.end; line++ {
		original, reason := src.Lines.Original(src.Lines.Expanded(line))
		if reason != "" || original != line {
			return nil, nil, fault(422, "excluded", "取り込みの指定・取り込んだ行・front matter を含む範囲は移動できません。")
		}
	}
	return a, b, nil
}

func (h *blockMoveMapping) information(src session.Source, target blockMoveTarget) *blockMoveInfo {
	info := &blockMoveInfo{Target: target}
	if _, _, e := h.pair(src, target, "up"); e != nil {
		info.Up = e.Message
	}
	if _, _, e := h.pair(src, target, "down"); e != nil {
		info.Down = e.Message
	}
	return info
}

func (s *Service) blockMoveInformation(ctx context.Context, src session.Source, m *blockMapping, target blockMoveTarget) *blockMoveInfo {
	tables := s.cache[pathKey(src.Path)]
	if tables != nil && tables.response.Render != src.Render {
		tables = nil
	}
	h, e := s.blockMoves(ctx, src, m, tables)
	if e != nil {
		return &blockMoveInfo{target, e.Message, e.Message}
	}
	h.groupHTML(ctx, src, m, target)
	return h.information(src, target)
}

func (s *Service) moveBlock(ctx context.Context, src session.Source, o blockMoveOperation) (headingRewrite, []string, *Error) {
	m, e := s.blocks(ctx, src)
	if e != nil {
		return headingRewrite{}, nil, e
	}
	tables, e := s.tables(ctx, src)
	if e != nil {
		return headingRewrite{}, nil, e
	}
	if o.target.Tag == "html" {
		if _, e := s.htmlWritable(ctx, src, m, o.target.Range); e != nil {
			return headingRewrite{}, nil, e
		}
	}
	h, e := s.blockMoves(ctx, src, m, tables)
	if e != nil {
		return headingRewrite{}, nil, e
	}
	h.groupHTML(ctx, src, m, o.target)
	a, b, e := h.pair(src, o.target, o.direction)
	if e != nil {
		return headingRewrite{}, nil, e
	}
	for _, entry := range []*movableBlock{a, b} {
		if entry.raw != nil {
			verified, e := s.verifyHTML(ctx, src, m, entry.raw)
			if e != nil {
				return headingRewrite{}, nil, e
			}
			if verified.actual != entry.block.actual {
				return headingRewrite{}, nil, blockFault("boundary")
			}
		}
	}
	lines := originalHeadingLines(src.Text)
	end := b.end
	for end < len(lines) && headingBlank(lines[end].text) {
		end++
	}
	ending := newlineAt(src.Text, a.first+1)
	lines = swapLineRegions(lines, a.first, b.first, end, ending)
	rewritten := finishHeadingRewrite(src.Text, lines, ending)
	expected := append([]*html.Node{}, headingTop(h.root)...)
	if len(a.nodes) == 0 || len(b.nodes) == 0 {
		return headingRewrite{}, nil, blockFault("boundary")
	}
	first := slices.Index(expected, a.nodes[0])
	middle := slices.Index(expected, b.nodes[0])
	last := middle + len(b.nodes)
	if first < 0 || middle <= first || last > len(expected) {
		return headingRewrite{}, nil, blockFault("boundary")
	}
	part := append([]*html.Node{}, expected[middle:last]...)
	part = append(part, expected[first:middle]...)
	expected = slices.Replace(expected, first, last, part...)
	warnings, e := s.verifyBlockMove(ctx, src, h.root, expected, rewritten.text)
	return rewritten, warnings, e
}

func (s *Service) verifyBlockMove(ctx context.Context, src session.Source, old *html.Node, expected []*html.Node, text string) ([]string, *Error) {
	trial, err := s.opts.Host.Trial(ctx, src, text)
	if err != nil {
		return nil, fault(422, "render", "移動後の文書を描画できません。")
	}
	root, err := htmltree.Fragment(ctx, trial.HTML)
	if err != nil {
		return nil, fault(422, "render", "移動後の文書を描画できません。")
	}
	next := headingTop(root)
	if len(expected) != len(next) {
		return nil, fault(422, "render", blockMoveCompareReason)
	}
	correspondence := map[*html.Node]*html.Node{}
	for i, n := range expected {
		if canonical(n, "") != canonical(next[i], "") {
			return nil, fault(422, "render", blockMoveCompareReason)
		}
		before, a := htmltree.Elements(ctx, n, headingNode)
		after, b := htmltree.Elements(ctx, next[i], headingNode)
		if a != nil || b != nil || len(before) != len(after) {
			return nil, fault(422, "render", blockMoveCompareReason)
		}
		for j, heading := range before {
			correspondence[heading] = after[j]
		}
	}
	if ctx.Err() != nil || !sameHeadingFootnotes(ctx, old, root, false, correspondence) {
		return nil, fault(422, "render", blockMoveCompareReason)
	}
	return headingWarnings(ctx, old, root, correspondence), nil
}
