package edit

import (
	"context"
	"strings"

	"github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
	"github.com/ryo-aoki-pc/md-tools/mdtable/tableedit"
	"golang.org/x/net/html"
)

// verifyConversion は書き込み前の一回の試しの描画で、格子・左上の文字を比べる。
// 種類だけを変えるときはタスクの個数も比べ、結合で覆われたセルの削除は許す。
func (s *Service) verifyConversion(ctx context.Context, src session.Source, index int, converted *tableedit.Table, text string, keepTasks bool) *Error {
	trial, err := s.opts.Host.Trial(ctx, src, text)
	if err != nil {
		return fault(422, "render", "変換後の文書を描画できません。")
	}
	before, err := htmltree.Fragment(ctx, src.HTML)
	if err != nil {
		return fault(422, "render", "変換前の表を読み取れません。")
	}
	a, err := tableNodes(ctx, before)
	if err != nil {
		return fault(422, "render", "変換前の表を読み取れません。")
	}
	after, err := htmltree.Fragment(ctx, trial.HTML)
	if err != nil {
		return fault(422, "render", "変換後の表を読み取れません。")
	}
	b, err := tableNodes(ctx, after)
	if err != nil || len(a) != len(b) || index >= len(b) {
		return fault(422, "verify", "変換すると、描画した表の数が変わります。")
	}
	// 変換後の原文で確かめたモデルを使い、HTML の行・列と結合の形も確かめる。
	if converted == nil || !sameGrid(b[index], converted) || firstCellText(a[index]) != firstCellText(b[index]) || keepTasks && checkboxCount(a[index]) != checkboxCount(b[index]) {
		return fault(422, "verify", "変換すると、表の行・列、左上のセルの文字、またはチェックボックスの数が変わります。")
	}
	return nil
}

func firstCellText(n *html.Node) string {
	var first func(*html.Node) *html.Node
	first = func(p *html.Node) *html.Node {
		if p.Type == html.ElementNode && (p.Data == "td" || p.Data == "th") {
			return p
		}
		for c := p.FirstChild; c != nil; c = c.NextSibling {
			if found := first(c); found != nil {
				return found
			}
		}
		return nil
	}
	var text strings.Builder
	var collect func(*html.Node)
	collect = func(p *html.Node) {
		if p.Type == html.TextNode {
			text.WriteString(p.Data)
		}
		for c := p.FirstChild; c != nil; c = c.NextSibling {
			collect(c)
		}
	}
	if cell := first(n); cell != nil {
		collect(cell)
	}
	return strings.TrimSpace(text.String())
}

func checkboxCount(n *html.Node) int {
	count := 0
	if n.Type == html.ElementNode && n.Data == "input" && strings.EqualFold(htmltree.Get(n, "type"), "checkbox") {
		count++
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		count += checkboxCount(c)
	}
	return count
}
