package sanitize

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	t "github.com/ryo-aoki-pc/md-preview/internal/htmltree"
	"golang.org/x/net/html"
)

func clean(tb testing.TB, source string) *html.Node {
	tb.Helper()
	ctx := context.Background()
	root, err := t.Fragment(ctx, source)
	if err != nil {
		tb.Fatal(err)
	}
	root, err = Document(ctx, root)
	if err != nil {
		tb.Fatal(err)
	}
	root, err = Links(ctx, root)
	if err != nil {
		tb.Fatal(err)
	}
	return root
}
func serialized(tb testing.TB, n *html.Node) string {
	tb.Helper()
	s, err := t.HTML(n)
	if err != nil {
		tb.Fatal(err)
	}
	return s
}
func TestElementsAndAttributes(tst *testing.T) {
	if got := serialized(tst, clean(tst, "<script>alert(1)</script><p>安全<!--秘密--></p>")); got != "<p>安全</p>" {
		tst.Fatal(got)
	}
	for _, tag := range []string{"iframe", "object", "embed", "form", "style", "link", "meta", "base", "svg", "math"} {
		root := clean(tst, "<"+tag+` src="x" onload="alert(1)">文字</`+tag+">")
		nodes, _ := t.Elements(context.Background(), root, func(n *html.Node) bool { return n.Data == tag })
		if len(nodes) > 0 || strings.Contains(serialized(tst, root), "onload=") {
			tst.Fatal(tag)
		}
	}
	if got := serialized(tst, clean(tst, `<p onclick="x" onmouseover="x">文字<img src="x" onerror="x"></p>`)); got != `<p>文字<img src="x"/></p>` {
		tst.Fatal(got)
	}
	root := t.Root(t.Element("a", t.Attr("href", "/outer"), t.Element("div", nil, t.Element("a", t.Attr("href", "/inner"), t.Text("文字")))))
	root, err := Document(context.Background(), root)
	if err != nil || serialized(tst, root) != `<a href="/outer"><div>文字</div></a>` {
		tst.Fatal(serialized(tst, root), err)
	}
	root = clean(tst, `<a href="/outer"><svg><a xlink:href="javascript:x">内側</a></svg></a>`)
	nodes, _ := t.Elements(context.Background(), root, func(n *html.Node) bool { return n.Data == "a" })
	for _, n := range nodes {
		if t.Ancestor(n, func(a *html.Node) bool { return a.Data == "a" }) {
			tst.Fatal("入れ子のリンク")
		}
	}
	root = t.Root(t.Element("a", t.Attr("href", "/safe", "xLinkHref", "javascript:x", "xlink:href", "javascript:x", "xmlLang", "ja", "xmlns", "x"), t.Text("文字")))
	root, err = Document(context.Background(), root)
	if err != nil || serialized(tst, root) != `<a href="/safe">文字</a>` {
		tst.Fatal(serialized(tst, root), err)
	}
	root = t.Root(t.Element("li", nil, t.Text("項目")), t.Element("tbody", nil, t.Element("tr", nil, t.Element("td", nil, t.Text("セル")))))
	root, err = Document(context.Background(), root)
	if err != nil || serialized(tst, root) != "項目セル" {
		tst.Fatal(serialized(tst, root), err)
	}
}
func TestURLs(tst *testing.T) {
	for _, source := range []string{"javascript:alert(1)", "JaVaScRiPt:alert(1)", " javascript:alert(1) ", "java\tscript:alert(1)", "java\nscript:alert(1)", "java\rscript:alert(1)", "java1script:alert(1)", "data:text/html,x", "DATA:text/html,x", "vbscript:msgbox(1)", "vB\tscript:x", "javascript&#58;alert(1)", "&#106;avascript:alert(1)"} {
		root := clean(tst, `<a href="`+source+`">文字</a><img src="`+source+`">`)
		nodes, _ := t.Elements(context.Background(), root, nil)
		for _, n := range nodes {
			if t.Has(n, "href") || t.Has(n, "src") {
				tst.Fatalf("危ない URL: %q", source)
			}
		}
	}
	for _, c := range [][2]string{{`<a href="  https://example.com/x  ">文字</a>`, `<a href="https://example.com/x">文字</a>`}, {`<a href="http://[broken">文字</a>`, `<a>文字</a>`}, {`<a href="custom:thing">文字</a>`, `<a href="custom:thing">文字</a>`}, {`<img src="ftp://example.com/a" longdesc="javascript:x"><q cite="ftp://example.com">文字</q>`, `<img/><q>文字</q>`}, {`<img src="HTTPS://example.com/a">`, `<img src="https://example.com/a"/>`}} {
		if got := serialized(tst, clean(tst, c[0])); got != c[1] {
			tst.Fatal(c[0], got)
		}
	}
	root := t.Root(t.Element("a", t.Attr("href", " x ", "src", "JavaScript:x", "data-src", "data:x", "data-canonical-src", "vbscript:x")))
	root, err := Links(context.Background(), root)
	if err != nil || serialized(tst, root) != `<a href="x"></a>` {
		tst.Fatal(serialized(tst, root), err)
	}
	if SafeProtocol("Java1Script") || !SafeProtocol("https") || PermitURL("https://[broken") || PermitURL(string([]byte{0xed, 0xa0, 0x80})) {
		tst.Fatal("URL の直接検査")
	}
	root = t.Root()
	for i := 0; i < 10001; i++ {
		t.Append(root, t.Element("a", t.Attr("href", "/safe")))
	}
	root, err = Links(context.Background(), root)
	if err != nil || !strings.Contains(t.Content(root), "Timeout while sanitizing links") {
		tst.Fatal(err)
	}
}
func TestURLSyntax(tst *testing.T) {
	raw, err := os.ReadFile("testdata/urls.json")
	if err != nil {
		tst.Fatal(err)
	}
	var rows []struct {
		Value   string
		Allowed bool
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		tst.Fatal(err)
	}
	for _, row := range rows {
		if got := PermitURL(row.Value); got != row.Allowed {
			tst.Errorf("%q: 見本=%v Go=%v", row.Value, row.Allowed, got)
		}
	}
}
func TestTableFieldsStylesAndIDs(tst *testing.T) {
	root := clean(tst, `<p style="text-align: center">外</p><table><tr><td style="color:red;text-align: right; background:url(javascript:x)">右</td><th style="text-align:justify">消す</th></tr></table>`)
	if got := serialized(tst, root); got != `<p>外</p><table><tbody><tr><td style="text-align: right">右</td><th>消す</th></tr></tbody></table>` {
		tst.Fatal(got)
	}
	root = t.Root(t.Element("table", t.Attr("data-table-fields", `[{"key":"x","label":"X","sortable":true,"formatter":"alert"}]`, "data-table-markdown", "true", "data-table-filter", "true")))
	root, err := Document(context.Background(), root)
	if err != nil || t.Get(root.FirstChild, "data-table-fields") != `[{"key":"x","label":"X","sortable":true}]` || t.Get(root.FirstChild, "data-table-markdown") != "true" || t.Get(root.FirstChild, "data-table-filter") != "true" {
		tst.Fatal(serialized(tst, root), err)
	}
	for _, fields := range []string{"oops", "{}", "[1]", "[null]"} {
		root = t.Root(t.Element("table", t.Attr("data-table-fields", fields)))
		root, err = Document(context.Background(), root)
		if err != nil || t.Has(root.FirstChild, "data-table-fields") {
			tst.Fatal(fields, err)
		}
	}
	root = clean(tst, `<h1 id="user-content-ok">見出し</h1><h2 id="bad">見出し</h2><a id="fnref-a" name="keep">脚注</a><a id="user-content-no">消す</a><ul><li id="fn-a">注</li><li id="bad">消す</li></ul><div id="user-content-no" name="no">消す</div>`)
	nodes, _ := t.Elements(context.Background(), root, nil)
	ids, names := []string{}, []string{}
	for _, n := range nodes {
		if t.Has(n, "id") {
			ids = append(ids, t.Get(n, "id"))
		}
		if t.Has(n, "name") {
			names = append(names, t.Get(n, "name"))
		}
	}
	if strings.Join(ids, ",") != "user-content-ok,fnref-a,fn-a" || strings.Join(names, ",") != "keep" {
		tst.Fatal(ids, names)
	}
	if got := serialized(tst, clean(tst, `<a href="/x" rel="license">残す</a><a rel="nofollow license">消す</a>`)); got != `<a href="/x" rel="license">残す</a><a>消す</a>` {
		tst.Fatal(got)
	}
}
func TestClassesAndInputs(tst *testing.T) {
	for _, c := range []struct {
		tag, class string
		keep       bool
	}{{"a", "anchor", true}, {"a", "anchor extra", false}, {"div", "markdown-alert markdown-alert-note", true}, {"div", "markdown-alert markdown-alert-unknown", false}, {"p", "markdown-alert-title", true}, {"p", "note", false}, {"span", "idiff left right addition", true}, {"span", "idiff evil", false}, {"span", "addition", false}, {"code", "idiff", true}, {"code", "language-js", false}, {"ul", "task-list", true}, {"li", "inapplicable task-list-item", true}, {"ol", "task-list extra", false}, {"li", "task-list-item", true}, {"li", "task-list-item inapplicable", false}, {"th", "task-table-item extra", false}, {"img", "glfm-float-left glfm-float-none", true}, {"img", "glfm-float-right evil", false}, {"td", "task-table-item", true}, {"img", "glfm-float-right", true}, {"img", "lazy", false}} {
		target := t.Element(c.tag, t.Attr("class", c.class))
		wrapper := target
		if c.tag == "li" {
			wrapper = t.Element("ul", nil, target)
		}
		if c.tag == "td" || c.tag == "th" {
			wrapper = t.Element("table", nil, t.Element("tr", nil, target))
		}
		root, err := Document(context.Background(), t.Root(wrapper))
		if err != nil {
			tst.Fatal(err)
		}
		nodes, _ := t.Elements(context.Background(), root, func(n *html.Node) bool { return n.Data == c.tag })
		if len(nodes) == 0 || t.Has(nodes[0], "class") != c.keep {
			tst.Fatal(c, serialized(tst, root))
		}
	}
	for _, c := range []struct {
		parent, typ, class string
		keep               bool
	}{{"li", "checkbox", "task-list-item-checkbox", true}, {"td", "checkbox", "task-list-item-checkbox", true}, {"th", "checkbox", "task-list-item-checkbox", true}, {"p", "checkbox", "task-list-item-checkbox", false}, {"li", "radio", "task-list-item-checkbox", false}, {"li", "checkbox", "task-list-item-checkbox evil", false}, {"li", "checkbox", "", false}} {
		input := t.Element("input", t.Attr("type", c.typ, "class", c.class, "disabled", "", "data-inapplicable", "", "data-indeterminate", "", "data-checkbox-sourcepos", "1:1-1:1"))
		wrapper := t.Element(c.parent, nil, input)
		if c.parent == "li" {
			wrapper = t.Element("ul", nil, wrapper)
		}
		if c.parent == "td" || c.parent == "th" {
			wrapper = t.Element("table", nil, t.Element("tr", nil, wrapper))
		}
		root, err := Document(context.Background(), t.Root(wrapper))
		if err != nil {
			tst.Fatal(err)
		}
		nodes, _ := t.Elements(context.Background(), root, func(n *html.Node) bool { return n.Data == "input" })
		if (len(nodes) > 0) != c.keep {
			tst.Fatal(c, serialized(tst, root))
		}
		if c.keep && (!t.Has(nodes[0], "data-indeterminate") || t.Get(nodes[0], "data-checkbox-sourcepos") != "1:1-1:1") {
			tst.Fatal(c)
		}
	}
	root := clean(tst, `<span data-math-style="inline" data-placeholder="name" data-sourcepos="1:1-1:2" data-escaped-char="#">x</span><section data-footnotes><a data-footnote-ref data-footnote-backref data-footnote-backref-idx="1" data-heading-content="題">注</a></section>`)
	s := serialized(tst, root)
	for _, attr := range []string{"data-math-style", "data-placeholder", "data-sourcepos", "data-escaped-char", "data-footnotes", "data-footnote-ref", "data-footnote-backref", "data-footnote-backref-idx", "data-heading-content"} {
		if !strings.Contains(s, attr) {
			tst.Fatal(attr)
		}
	}
}
