package edit

import (
	"context"
	"strings"

	"github.com/ryo-aoki-pc/md-tools/mdtable/tableedit"
	"golang.org/x/net/html"
)

// sameTree は位置の属性以外を厳密に比べ、印が表以外へ移った場合は断る。
func sameTree(ctx context.Context, a, b *html.Node, prefix string, models map[int]*tableedit.Table) bool {
	if ctx.Err() != nil || a.Type != b.Type || a.Data != b.Data || a.Namespace != b.Namespace {
		return false
	}
	if strings.Contains(b.Data, prefix) {
		return false
	}
	marked := false
	for _, attr := range b.Attr {
		if strings.Contains(attr.Val, prefix) {
			if b.Type != html.ElementNode || b.Data != "table" || attr.Key != "data-sourcepos" || attr.Namespace != "" {
				return false
			}
			if _, ok := marker(attr.Val, prefix, models); !ok {
				return false
			}
			marked = true
		}
	}
	attributes := func(n *html.Node) map[string]string {
		out := map[string]string{}
		for _, attr := range n.Attr {
			if attr.Namespace == "" && (attr.Key == "data-sourcepos" || attr.Key == "data-checkbox-sourcepos") {
				if marked && attr.Key == "data-sourcepos" {
					continue
				}
				attr.Val = ""
			}
			out[attr.Namespace+"\x00"+attr.Key] = attr.Val
		}
		return out
	}
	aa, bb := attributes(a), attributes(b)
	if len(aa) != len(bb) {
		return false
	}
	for k, v := range aa {
		if w, ok := bb[k]; !ok || w != v {
			return false
		}
	}
	x, y := a.FirstChild, b.FirstChild
	for x != nil && y != nil {
		if !sameTree(ctx, x, y, prefix, models) {
			return false
		}
		x, y = x.NextSibling, y.NextSibling
	}
	return x == nil && y == nil
}
