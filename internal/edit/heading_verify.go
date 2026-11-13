package edit

import (
	"context"
	"net/url"
	"slices"
	"strings"

	"github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
	"golang.org/x/net/html"
)

func headingTop(root *html.Node) []*html.Node {
	out := []*html.Node{}
	for n := root.FirstChild; n != nil; n = n.NextSibling {
		if footnoteSection(n) || n.Type == html.TextNode && strings.TrimSpace(n.Data) == "" {
			continue
		}
		out = append(out, n)
	}
	return out
}

func headingFootnotes(ctx context.Context, root *html.Node) (map[string]*html.Node, bool) {
	set := map[string]*html.Node{}
	nodes, err := htmltree.Elements(ctx, root, func(n *html.Node) bool {
		return n.Data == "li" && htmltree.Has(n, "id") && htmltree.Ancestor(n, footnoteSection)
	})
	if err != nil {
		return nil, false
	}
	for _, n := range nodes {
		id := htmltree.Get(n, "id")
		if _, exists := set[id]; exists {
			return nil, false
		}
		set[id] = n
	}
	return set, true
}

func sameHeadingFootnotes(ctx context.Context, old, next *html.Node, deletion bool, correspondence map[*html.Node]*html.Node) bool {
	a, okA := headingFootnotes(ctx, old)
	b, okB := headingFootnotes(ctx, next)
	if !okA || !okB || !deletion && len(a) != len(b) {
		return false
	}
	for id, after := range b {
		before, exists := a[id]
		if !exists || canonical(before, "") != canonical(after, "") {
			return false
		}
		beforeHeadings, _ := htmltree.Elements(ctx, before, headingNode)
		afterHeadings, _ := htmltree.Elements(ctx, after, headingNode)
		if len(beforeHeadings) != len(afterHeadings) {
			return false
		}
		for i, heading := range beforeHeadings {
			correspondence[heading] = afterHeadings[i]
		}
	}
	return true
}

func (s *Service) verifyHeadingEdit(ctx context.Context, src session.Source, h *headingMapping, e *headingEntry, op headingOperation, text string, setext map[*html.Node]bool) ([]string, *headingFocus, *Error) {
	trial, err := s.opts.Host.Trial(ctx, src, text)
	if err != nil {
		return nil, nil, fault(422, "render", "編集後の文書を描画できません。")
	}
	root, err := htmltree.Fragment(ctx, trial.HTML)
	if err != nil {
		return nil, nil, fault(422, "render", "編集後の文書を描画できません。")
	}
	old, next := headingTop(h.root), headingTop(root)
	positions := map[*html.Node]int{}
	for i, n := range old {
		positions[n] = i
	}
	start := positions[e.node]
	end := len(old)
	if e.next < len(h.entries) {
		end = positions[h.entries[e.next].node]
	}
	expected := append([]*html.Node{}, old...)
	tags := map[*html.Node]string{}
	focusAt := start
	switch op.kind {
	case "level":
		tags[e.node] = headingTag(e.info.Level + op.delta)
		if op.scope == "section" {
			for _, child := range h.entries[e.info.Index:e.next] {
				tags[child.node] = headingTag(child.info.Level + op.delta)
			}
		}
	case "delete":
		if op.scope == "heading" {
			end = start + 1
		}
		expected = slices.Delete(expected, start, end)
	case "move":
		sibling := h.sibling(e, op.direction)
		if op.direction == "up" {
			before := positions[sibling.node]
			part := append([]*html.Node{}, old[start:end]...)
			part = append(part, old[before:start]...)
			expected = slices.Replace(expected, before, end, part...)
			focusAt = before
		} else {
			after := len(old)
			if sibling.next < len(h.entries) {
				after = positions[h.entries[sibling.next].node]
			}
			part := append([]*html.Node{}, old[end:after]...)
			part = append(part, old[start:end]...)
			expected = slices.Replace(expected, start, after, part...)
			focusAt = start + after - end
		}
	}
	// 最後の目次に載る見出しを消すと、描画器は空の目次そのものも除く。
	anchors, anchorErr := htmltree.Elements(ctx, root, func(n *html.Node) bool {
		return n.Data == "a" && htmltree.HasClass(n, "anchor") && htmltree.Ancestor(n, headingNode)
	})
	if anchorErr != nil {
		return nil, nil, fault(422, "cancelled", "見出しの対応の確認を取り消しました。")
	}
	emptyTOC := op.kind == "delete" && len(anchors) == 0
	if emptyTOC {
		for i := len(expected) - 1; i >= 0; i-- {
			if expected[i].Data == "ul" && htmltree.HasClass(expected[i], "section-nav") {
				expected = slices.Delete(expected, i, i+1)
				if i < focusAt {
					focusAt--
				}
			}
		}
	}
	compareFault := func() *Error {
		message := headingCompareReason
		if op.kind == "delete" && len(expected) != len(next) {
			message = "見出しを消すと前後のブロックがつながるので、書き込みませんでした。原文を直してください。"
		}
		return fault(422, "render", message)
	}
	if len(expected) != len(next) {
		return nil, nil, compareFault()
	}
	correspondence := map[*html.Node]*html.Node{}
	for i, before := range expected {
		after := next[i]
		if multiline, converted := setext[before]; converted {
			if canonicalHeading(before, tags[before], true) != canonicalHeading(after, "", true) || multiline && htmltree.Get(before, "id") != htmltree.Get(after, "id") {
				return nil, nil, fault(422, "render", headingSetextReason)
			}
		} else if headingSignature(before, tags[before], false, emptyTOC) != canonical(after, "") {
			return nil, nil, compareFault()
		}
		// 署名が同じ木では、前順の見出しを対応させられる。重複した題の移動も節点の同一性で追う。
		beforeHeadings, _ := htmltree.Elements(ctx, before, headingNode)
		afterHeadings, _ := htmltree.Elements(ctx, after, headingNode)
		if len(beforeHeadings) != len(afterHeadings) {
			return nil, nil, compareFault()
		}
		for j, heading := range beforeHeadings {
			correspondence[heading] = afterHeadings[j]
		}
	}
	if ctx.Err() != nil || !sameHeadingFootnotes(ctx, h.root, root, op.kind == "delete", correspondence) {
		return nil, nil, fault(422, "render", headingCompareReason)
	}
	var focus *headingFocus
	if op.kind == "delete" {
		for i := focusAt; i < len(next); i++ {
			if headingNode(next[i]) {
				focus = &headingFocus{next[i].Data, Range(htmltree.Get(next[i], "data-sourcepos"))}
				break
			}
		}
		if focus == nil {
			for i := min(focusAt, len(next)) - 1; i >= 0; i-- {
				if headingNode(next[i]) {
					focus = &headingFocus{next[i].Data, Range(htmltree.Get(next[i], "data-sourcepos"))}
					break
				}
			}
		}
	} else if focusAt < len(next) {
		n := next[focusAt]
		focus = &headingFocus{n.Data, Range(htmltree.Get(n, "data-sourcepos"))}
	}
	return headingWarnings(ctx, h.root, root, correspondence), focus, nil
}

func headingLinks(ctx context.Context, root *html.Node) []string {
	links := []string{}
	_ = htmltree.Walk(ctx, root, func(n *html.Node) error {
		if n.Type != html.ElementNode || n.Data != "a" || htmltree.HasClass(n, "anchor") || htmltree.HasClass(n, "footnote-backref") || htmltree.Has(n, "data-footnote-ref") ||
			htmltree.Ancestor(n, func(p *html.Node) bool { return footnoteSection(p) || htmltree.HasClass(p, "section-nav") }) {
			return nil
		}
		href := htmltree.Get(n, "href")
		if strings.HasPrefix(href, "#") {
			if id, err := url.PathUnescape(href[1:]); err == nil {
				links = append(links, strings.TrimPrefix(id, "user-content-"))
			}
		}
		return nil
	})
	return links
}

func headingWarnings(ctx context.Context, old, next *html.Node, correspondence map[*html.Node]*html.Node) []string {
	before, _ := htmltree.Elements(ctx, old, headingNode)
	after, _ := htmltree.Elements(ctx, next, headingNode)
	byID, nextByID := map[string]*html.Node{}, map[string]*html.Node{}
	for _, n := range before {
		id := strings.TrimPrefix(htmltree.Get(n, "id"), "user-content-")
		if id != "" && byID[id] == nil {
			byID[id] = n
		}
	}
	for _, n := range after {
		id := strings.TrimPrefix(htmltree.Get(n, "id"), "user-content-")
		if id != "" && nextByID[id] == nil {
			nextByID[id] = n
		}
	}
	changedIDs, deletedIDs := []string{}, []string{}
	for _, id := range headingLinks(ctx, next) {
		n := byID[id]
		if n == nil {
			continue
		}
		mapped := correspondence[n]
		if mapped == nil {
			if !slices.Contains(deletedIDs, id) {
				deletedIDs = append(deletedIDs, id)
			}
		} else if nextByID[id] != mapped && !slices.Contains(changedIDs, id) {
			changedIDs = append(changedIDs, id)
		}
	}
	warnings := []string{}
	if len(changedIDs) > 0 {
		label := "#" + changedIDs[0]
		if len(changedIDs) > 1 {
			label += " ほか"
		}
		warnings = append(warnings, "見出しの ID が変わりました（"+label+"）。この文書の [文字](#…) のリンク先を確かめてください。")
	}
	if len(deletedIDs) > 0 {
		label := "#" + deletedIDs[0]
		if len(deletedIDs) > 1 {
			label += " ほか"
		}
		warnings = append(warnings, "消した見出しへのリンク（"+label+"）が文書に残っています。")
	}
	return warnings
}
