package edit

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/mdtable/tableedit"
	"golang.org/x/net/html"
)

// canonical は見出しの操作で変わりうる位置・見出し ID・脚注番号だけを除く署名。
// tag を渡すと、その節点の要素名だけを差し替えて比べられる。
func canonical(n *html.Node, tag string) string {
	return canonicalHeading(n, tag, false)
}

func canonicalHeading(n *html.Node, tag string, spaces bool) string {
	return headingSignature(n, tag, spaces, false)
}

func headingBackref(n *html.Node) bool {
	return n != nil && n.Type == html.ElementNode && n.Data == "a" && htmltree.HasClass(n, "footnote-backref")
}

func headingSignatureExcluded(n *html.Node, emptyTOC bool) bool {
	return headingBackref(n) ||
		// 複数の戻りリンクの間に描画器が足す区切りも、戻りリンクと一緒に除く。
		n.Type == html.TextNode && strings.TrimSpace(n.Data) == "" && headingBackref(n.PrevSibling) && headingBackref(n.NextSibling) ||
		emptyTOC && n.Type == html.ElementNode && n.Data == "ul" && htmltree.HasClass(n, "section-nav")
}

func headingSignature(n *html.Node, tag string, spaces, emptyTOC bool) string {
	if spaces {
		n = htmltree.Clone(n)
		normalizeHeadingEscapes(n)
	}
	var out strings.Builder
	var visit func(*html.Node, string)
	visit = func(n *html.Node, tag string) {
		if headingSignatureExcluded(n, emptyTOC) {
			return
		}
		if n.Type == html.ElementNode && n.Data == "ul" && htmltree.HasClass(n, "section-nav") {
			if !emptyTOC {
				out.WriteString("目次;")
			}
			return
		}
		name := n.Data
		if n.Type == html.ElementNode && tag != "" {
			name = tag
		}
		if n.Type == html.TextNode && spaces {
			name = strings.Join(strings.Fields(name), " ")
		}
		fmt.Fprintf(&out, "%d:%s:%s[", n.Type, strconv.Quote(n.Namespace), strconv.Quote(name))
		attrs := []string{}
		ref := n.Type == html.ElementNode && n.Data == "a" && htmltree.Has(n, "data-footnote-ref")
		for _, a := range n.Attr {
			if a.Namespace == "" && (a.Key == "data-sourcepos" || a.Key == "data-checkbox-sourcepos" ||
				a.Key == "id" && (headingNode(n) || ref) ||
				a.Key == "href" && n.Data == "a" && htmltree.HasClass(n, "anchor")) {
				continue
			}
			attrs = append(attrs, strconv.Quote(a.Namespace)+":"+strconv.Quote(a.Key)+":"+strconv.Quote(a.Val))
		}
		sort.Strings(attrs)
		out.WriteString(strings.Join(attrs, ","))
		out.WriteString("]{")
		// 除いた目次や戻りリンクの前後の文字は、新しい木では一つの文字節点になる。
		var pending strings.Builder
		flush := func() {
			if pending.Len() > 0 {
				visit(htmltree.Text(pending.String()), "")
				pending.Reset()
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if headingSignatureExcluded(c, emptyTOC) || ref && c.Type == html.TextNode {
				continue
			}
			if c.Type == html.TextNode {
				pending.WriteString(c.Data)
				continue
			}
			flush()
			visit(c, "")
		}
		flush()
		out.WriteString("};")
	}
	visit(n, tag)
	return out.String()
}

// setext の末尾の # をエスケープすると、描画器は印だけの span を足す。
// この印には表示の属性がないので、変換する見出しの比較に限って文字へ戻す。
func normalizeHeadingEscapes(n *html.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		normalizeHeadingEscapes(c)
		if c.Type == html.ElementNode && c.Data == "span" && htmltree.Has(c, "data-escaped-char") &&
			c.FirstChild != nil && c.FirstChild == c.LastChild && c.FirstChild.Type == html.TextNode && c.FirstChild.Data == "#" {
			plain := true
			for _, a := range c.Attr {
				if a.Namespace != "" || a.Key != "data-escaped-char" && a.Key != "data-sourcepos" {
					plain = false
				}
			}
			if plain {
				htmltree.Replace(c, htmltree.Text("#"))
			}
		}
		c = next
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		for c.Type == html.TextNode && c.NextSibling != nil && c.NextSibling.Type == html.TextNode {
			next := c.NextSibling
			c.Data += next.Data
			n.RemoveChild(next)
		}
	}
}

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
