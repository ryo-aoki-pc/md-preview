// Package htmltree は、文字を HTML として挿入しない木の操作を提供する。
package htmltree

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var ErrDepth = errors.New("入れ子が深すぎるため、描画できません。")

func Text(s string) *html.Node { return &html.Node{Type: html.TextNode, Data: s} }
func Element(tag string, attrs []html.Attribute, children ...*html.Node) *html.Node {
	n := &html.Node{Type: html.ElementNode, Data: tag, DataAtom: atom.Lookup([]byte(tag)), Attr: attrs}
	for _, c := range children {
		Append(n, c)
	}
	return n
}
func Root(children ...*html.Node) *html.Node {
	n := &html.Node{Type: html.DocumentNode}
	for _, c := range children {
		Append(n, c)
	}
	return n
}
func Attr(pairs ...string) []html.Attribute {
	a := []html.Attribute{}
	for i := 0; i+1 < len(pairs); i += 2 {
		a = append(a, html.Attribute{Key: pairs[i], Val: pairs[i+1]})
	}
	return a
}
func Get(n *html.Node, k string) string {
	for _, a := range n.Attr {
		if a.Key == k && a.Namespace == "" {
			return a.Val
		}
	}
	return ""
}
func Has(n *html.Node, k string) bool {
	for _, a := range n.Attr {
		if a.Key == k && a.Namespace == "" {
			return true
		}
	}
	return false
}
func Set(n *html.Node, k, v string) {
	for i := range n.Attr {
		if n.Attr[i].Key == k && n.Attr[i].Namespace == "" {
			n.Attr[i].Val = v
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: k, Val: v})
}
func Del(n *html.Node, k string) {
	a := n.Attr[:0]
	for _, v := range n.Attr {
		if v.Key != k || v.Namespace != "" {
			a = append(a, v)
		}
	}
	n.Attr = a
}

// JavaScript の空白には NEL を含まず、BOM を含む。
func Space(r rune) bool    { return r == '\ufeff' || r != '\u0085' && unicode.IsSpace(r) }
func Trim(s string) string { return strings.TrimFunc(s, Space) }

// 小文字化で複数文字へ広がる U+0130 も JavaScript と合わせる。
func Lower(s string) string         { return strings.ToLower(strings.ReplaceAll(s, "\u0130", "i\u0307")) }
func Fields(s string) []string      { return strings.FieldsFunc(s, Space) }
func Classes(n *html.Node) []string { return Fields(Get(n, "class")) }
func HasClass(n *html.Node, k string) bool {
	for _, v := range Classes(n) {
		if v == k {
			return true
		}
	}
	return false
}
func AddClass(n *html.Node, names ...string) {
	c := Classes(n)
	for _, name := range names {
		for _, k := range Fields(name) {
			found := false
			for _, v := range c {
				if v == k {
					found = true
					break
				}
			}
			if !found {
				c = append(c, k)
			}
		}
	}
	Set(n, "class", strings.Join(c, " "))
}
func Children(n *html.Node) []*html.Node {
	a := []*html.Node{}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		a = append(a, c)
	}
	return a
}
func Append(n, c *html.Node) {
	if c.Parent != nil {
		c.Parent.RemoveChild(c)
	}
	n.AppendChild(c)
}
func Replace(n *html.Node, children ...*html.Node) {
	p := n.Parent
	if p == nil {
		return
	}
	for _, c := range children {
		if c.Parent != nil {
			c.Parent.RemoveChild(c)
		}
		p.InsertBefore(c, n)
	}
	p.RemoveChild(n)
}
func SetChildren(n *html.Node, children ...*html.Node) {
	for n.FirstChild != nil {
		n.RemoveChild(n.FirstChild)
	}
	for _, c := range children {
		Append(n, c)
	}
}
func OnlyCode(n *html.Node) *html.Node {
	var code *html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			if code != nil || c.Data != "code" {
				return nil
			}
			code = c
		}
	}
	return code
}
func Ancestor(n *html.Node, predicate func(*html.Node) bool) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if predicate(p) {
			return true
		}
	}
	return false
}
func Clone(n *html.Node) *html.Node {
	c, _ := CloneContext(context.Background(), n)
	return c
}
func CloneContext(ctx context.Context, n *html.Node) (*html.Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c := *n
	c.Parent = nil
	c.PrevSibling = nil
	c.NextSibling = nil
	c.FirstChild = nil
	c.LastChild = nil
	c.Attr = append([]html.Attribute(nil), n.Attr...)
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		copy, err := CloneContext(ctx, child)
		if err != nil {
			return nil, err
		}
		c.AppendChild(copy)
	}
	return &c, nil
}

// Walk は兄弟を一度に積まず、親へのポインターで戻る。幅の広い文書でも補助領域を増やさない。
func Walk(ctx context.Context, n *html.Node, visit func(*html.Node) error) error {
	current, depth := n, 0
	for current != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 512 {
			return ErrDepth
		}
		if err := visit(current); err != nil {
			return err
		}
		if current.FirstChild != nil {
			current = current.FirstChild
			depth++
			continue
		}
		for current != n && current.NextSibling == nil {
			current = current.Parent
			depth--
		}
		if current == n {
			break
		}
		current = current.NextSibling
	}
	return nil
}
func Nodes(ctx context.Context, n *html.Node, predicate func(*html.Node) bool) ([]*html.Node, error) {
	out := []*html.Node{}
	err := Walk(ctx, n, func(c *html.Node) error {
		if predicate(c) {
			out = append(out, c)
		}
		return nil
	})
	return out, err
}
func Elements(ctx context.Context, n *html.Node, predicate func(*html.Node) bool) ([]*html.Node, error) {
	return Nodes(ctx, n, func(c *html.Node) bool { return c.Type == html.ElementNode && (predicate == nil || predicate(c)) })
}
func Content(n *html.Node) string { return ContentContext(context.Background(), n) }
func ContentContext(ctx context.Context, n *html.Node) string {
	var b strings.Builder
	_ = Walk(ctx, n, func(v *html.Node) error {
		if v.Type == html.TextNode {
			b.WriteString(v.Data)
		}
		return nil
	})
	return b.String()
}
func Fragment(ctx context.Context, s string) (*html.Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// parse5 の断片解析と同じく、template を文脈にすると表の断片も保つ。
	nodes, err := html.ParseFragmentWithOptions(&contextReader{ctx: ctx, reader: strings.NewReader(s)}, &html.Node{Type: html.ElementNode, Data: "template", DataAtom: atom.Template}, html.ParseOptionEnableScripting(false))
	if err != nil {
		if strings.Contains(err.Error(), "too much") || strings.Contains(err.Error(), "too deep") || strings.Contains(err.Error(), "exceeds 512") {
			return nil, ErrDepth
		}
		return nil, err
	}
	root := Root(nodes...)
	if err = Walk(ctx, root, func(*html.Node) error { return nil }); err != nil {
		return nil, err
	}
	return root, nil
}
func HTML(n *html.Node) (string, error) {
	return HTMLContext(context.Background(), n)
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

type contextWriter struct {
	ctx    context.Context
	writer *bytes.Buffer
}

func (w *contextWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.writer.Write(p)
}
func (w *contextWriter) WriteString(s string) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.writer.WriteString(s)
}
func (w *contextWriter) WriteByte(b byte) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	return w.writer.WriteByte(b)
}
func HTMLContext(ctx context.Context, n *html.Node) (string, error) {
	var b bytes.Buffer
	err := html.Render(&contextWriter{ctx: ctx, writer: &b}, n)
	return b.String(), err
}
