package filters

import (
	"context"
	"encoding/base64"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-preview/engine"
	t "github.com/ryo-aoki-pc/md-preview/internal/htmltree"
	"github.com/ryo-aoki-pc/md-preview/internal/plantumlsource"
	"golang.org/x/net/html"
)

func apply(tb testing.TB, f Filter, source string) *html.Node {
	tb.Helper()
	root, err := t.Fragment(context.Background(), source)
	if err != nil {
		tb.Fatal(err)
	}
	state := &State{}
	if err := f(context.Background(), root, state); err != nil {
		tb.Fatal(err)
	}
	return root
}
func nodes(tb testing.TB, root *html.Node, tag string) []*html.Node {
	tb.Helper()
	found, err := tagged(context.Background(), root, tag)
	if err != nil {
		tb.Fatal(err)
	}
	return found
}
func first(tb testing.TB, root *html.Node, tag string) *html.Node {
	tb.Helper()
	found := nodes(tb, root, tag)
	if len(found) == 0 {
		tb.Fatalf("要素がありません: %s", tag)
	}
	return found[0]
}
func canonical(n *html.Node) string {
	c := t.Clone(n)
	_ = t.Walk(context.Background(), c, func(n *html.Node) error {
		sort.Slice(n.Attr, func(i, j int) bool { return n.Attr[i].Key < n.Attr[j].Key })
		return nil
	})
	s, _ := t.HTML(c)
	return s
}
func wantHTML(tb testing.TB, root *html.Node, source string) {
	tb.Helper()
	expected, err := t.Fragment(context.Background(), source)
	if err != nil {
		tb.Fatal(err)
	}
	if a, b := canonical(root), canonical(expected); a != b {
		tb.Fatalf("見本=%s\nGo=%s", b, a)
	}
}
func TestCodeLanguage(tst *testing.T) {
	root := apply(tst, CodeLanguage, `<pre data-lang="ruby:red file" data-meta="ignored" lang="python"><code data-lang="js" class="language-html keep" lang="css">x</code></pre>`)
	pre, code := first(tst, root, "pre"), first(tst, root, "code")
	if t.Get(pre, "data-lang") != "ruby" || t.Get(pre, "data-canonical-lang") != "ruby" || t.Get(pre, "data-lang-params") != "red file" || len(pre.Attr) != 3 || len(code.Attr) != 1 || t.Get(code, "class") != "keep" {
		tst.Fatal(canonical(root))
	}
	for _, c := range [][3]string{{`<pre><code data-lang="python">x</code></pre>`, "python", ""}, {`<pre><code class="language-js">x</code></pre>`, "js", ""}, {`<pre lang="ruby"><code lang="js">x</code></pre>`, "ruby", ""}, {`<pre><code lang="ruby">x</code></pre>`, "ruby", ""}, {`<pre data-meta="line"><code data-lang="ruby:">x</code></pre>`, "ruby", "line"}, {`<pre><code lang="ruby" data-meta="meta">x</code></pre>`, "ruby", "meta"}, {`<pre data-meta="   "><code lang="ruby:">x</code></pre>`, "ruby", ""}} {
		root := apply(tst, CodeLanguage, c[0])
		pre := first(tst, root, "pre")
		if t.Get(pre, "data-canonical-lang") != c[1] || t.Get(pre, "data-lang-params") != c[2] || t.Has(first(tst, root, "code"), "lang") {
			tst.Fatal(c, canonical(root))
		}
	}
	for _, c := range [][2]string{{`<pre lang="ruby">a<b>b</b></pre>`, `<pre data-lang="ruby" data-canonical-lang="ruby"><code>a<b>b</b></code></pre>`}, {`<pre lang="ruby"><code>x</code><b>y</b></pre>`, `<pre lang="ruby"><code>x</code><b>y</b></pre>`}, {`<pre><code>x</code></pre>`, `<pre><code>x</code></pre>`}} {
		wantHTML(tst, apply(tst, CodeLanguage, c[0]), c[1])
	}
}
func TestMarkdownFilters(tst *testing.T) {
	e, err := engine.New(context.Background())
	if err != nil {
		tst.Fatal(err)
	}
	defer e.Close(context.Background())
	state := &State{Markdown: e.Render}
	jsonRoot := func(source string) *html.Node {
		return t.Root(t.Element("pre", t.Attr("data-canonical-lang", "json", "data-lang-params", "table"), t.Element("code", nil, t.Text(source))))
	}
	root := jsonRoot(`{"fields":[{"key":"a","label":"<b>A</b>","sortable":true}],"items":[{"a":"**太字**"}],"caption":"*題*","filter":true,"markdown":true}`)
	if err := JSONTable(context.Background(), root, state); err != nil {
		tst.Fatal(err)
	}
	if t.Content(first(tst, root, "th")) != "<b>A</b>" || canonical(first(tst, root, "td")) != "<td><strong>太字</strong></td>" || canonical(first(tst, root, "caption")) != "<caption><p><em>題</em></p></caption>" || t.Get(first(tst, root, "table"), "data-table-filter") != "true" || strings.Contains(canonical(root), "data-sourcepos") {
		tst.Fatal(canonical(root))
	}
	root = jsonRoot(`{"items":[{"a":"一\n\n二","b":3},{"b":false,"c":"余分"}],"markdown":true}`)
	if err := JSONTable(context.Background(), root, state); err != nil {
		tst.Fatal(err)
	}
	if len(nodes(tst, root, "th")) != 2 || len(nodes(tst, first(tst, root, "td"), "p")) != 2 || nodes(tst, root, "td")[2].FirstChild != nil {
		tst.Fatal(canonical(root))
	}
	for _, input := range []string{"oops", "null", "[]", `{"items":[{}]}`, `{"markdown":false,"items":[{}]}`, `{"markdown":true,"items":[]}`, `{"markdown":true,"items":[1]}`, `{"markdown":true,"items":[{}],"fields":[1]}`} {
		root = jsonRoot(input)
		if err := JSONTable(context.Background(), root, state); err != nil || len(nodes(tst, root, "pre")) == 0 {
			tst.Fatal(input, err)
		}
	}
	root = jsonRoot(`{"markdown":true,"caption":false,"items":[{"a":[1,null],"b":{"x":true}}]}`)
	if err := JSONTable(context.Background(), root, state); err != nil {
		tst.Fatal(err)
	}
	cells := nodes(tst, root, "td")
	if len(nodes(tst, root, "caption")) != 0 || t.Content(cells[0]) != "[1, nil]" || t.Content(cells[1]) != `{"x"=>true}` {
		tst.Fatal(canonical(root))
	}
	root, err = t.Fragment(context.Background(), `<p>[頁](page slug "題") ![図](test image.png)</p>`)
	if err != nil {
		tst.Fatal(err)
	}
	if err := SpacedLink(context.Background(), root, state); err != nil {
		tst.Fatal(err)
	}
	if t.Get(first(tst, root, "a"), "href") != "page%20slug" || t.Get(first(tst, root, "a"), "title") != "題" || t.Get(first(tst, root, "img"), "src") != "test%20image.png" {
		tst.Fatal(canonical(root))
	}
	for _, wrapper := range []string{"a", "code", "kbd", "pre", "script", "style", `span data-math-style="inline"`} {
		tag := strings.Fields(wrapper)[0]
		root, _ = t.Fragment(context.Background(), "<"+wrapper+">[頁](page slug)</"+tag+">")
		before := canonical(root)
		if err := SpacedLink(context.Background(), root, state); err != nil || canonical(root) != before {
			tst.Fatal(wrapper, err)
		}
	}
}
func TestAttributesMathColorMermaid(tst *testing.T) {
	root := apply(tst, Attributes, `<p><img src="x">{width="50%" height=120px .float-right onclick=x bad=1}後</p>`)
	img := first(tst, root, "img")
	if t.Get(img, "width") != "50%" || t.Get(img, "height") != "120px" || t.Get(img, "class") != "glfm-float-right" || t.Content(first(tst, root, "p")) != "後" {
		tst.Fatal(canonical(root))
	}
	for _, source := range []string{`<img src="x"> {width=1}`, "<img src=\"x\">{width=1\nheight=2}", `<img src="x">{` + strings.Repeat("x", 101) + "}"} {
		root := apply(tst, Attributes, source)
		if !strings.Contains(t.Content(root), "}") {
			tst.Fatal(source)
		}
	}
	img = first(tst, apply(tst, Attributes, `<img src="x">{width=10000 height=-1 .other}`), "img")
	if len(img.Attr) != 1 {
		tst.Fatal(img.Attr)
	}
	root = apply(tst, Math, `<pre data-canonical-lang="math"><code>x</code></pre><span data-math-style="inline">x</span><code data-math-style="inline">y</code>`)
	if t.Get(first(tst, root, "pre"), "data-math-style") != "display" {
		tst.Fatal(canonical(root))
	}
	for _, tag := range []string{"pre", "span"} {
		if t.Get(first(tst, root, tag), "class") != "js-render-math" {
			tst.Fatal(tag)
		}
	}
	root = apply(tst, Math, strings.Repeat(`<span data-math-style="inline">x</span>`, 1001))
	count := 0
	for _, n := range nodes(tst, root, "span") {
		if t.HasClass(n, "js-render-math") {
			count++
		}
	}
	if count != 1000 {
		tst.Fatal(count)
	}
	for _, source := range []string{"#abc", "#abcd", "#abcdef", "#abcdef12", "RGB(0,255,99)", "rgba(0%,100%,00%,.5)", "rgb(1, 2, 3,100%)", "hsl(-90deg,50%,100%)", "hsla(.5rad,0%,99%,1.00)"} {
		if !ParseColor(source) {
			tst.Fatal(source)
		}
	}
	for _, source := range []string{"red", "#ab", "#abcde", "#abc\n", "rgb(256,0,0)", "rgb(1%,2,3)", "rgba(1,2,3,1.1)", "hsl(1.5,1%,1%)", "hsl(0,101%,1%)", "rgb( 1,2,3)", "rgb(1 2 3)", "url(javascript:x)", "rgb(1,\u00a02,3)", "#abc\u2028"} {
		if ParseColor(source) {
			tst.Fatal(source)
		}
	}
	root = apply(tst, Color, `<code>#abc</code><code>\#abc</code><code>無効</code>`)
	if !strings.Contains(canonical(root), "background-color: #abc;") || t.Content(nodes(tst, root, "code")[1]) != "#abc" || len(t.Children(nodes(tst, root, "code")[2])) != 1 {
		tst.Fatal(canonical(root))
	}
	wantHTML(tst, apply(tst, Mermaid, `<pre data-canonical-lang="mermaid"><code>x</code></pre><pre><code>y</code></pre>`), `<pre data-canonical-lang="mermaid"><code class="js-render-mermaid">x</code></pre><pre><code>y</code></pre>`)
}
func TestMedia(tst *testing.T) {
	for _, c := range []struct {
		f        Filter
		tag, ext string
	}{{Video, "video", "mp4"}, {Video, "video", "m4v"}, {Video, "video", "mov"}, {Video, "video", "webm"}, {Video, "video", "ogv"}, {Audio, "audio", "mp3"}, {Audio, "audio", "oga"}, {Audio, "audio", "ogg"}, {Audio, "audio", "spx"}, {Audio, "audio", "wav"}} {
		root := apply(tst, c.f, `<img src="clip.`+strings.ToUpper(c.ext)+`" alt="題" width="50" height="40">`)
		media := first(tst, root, c.tag)
		if t.Get(media, "data-title") != "題" || t.Get(media, "controls") != "true" {
			tst.Fatal(c.tag)
		}
		if c.tag == "video" {
			if t.Get(media, "width") != "50" || t.Get(media, "preload") != "metadata" {
				tst.Fatal(media.Attr)
			}
		} else if t.Has(media, "width") {
			tst.Fatal(media.Attr)
		}
	}
	root := apply(tst, Video, `<a href="x"><img src="x.mp4"></a><img src="x.exe"><img src="x.mp4?v=1"><img>`)
	if len(nodes(tst, root, "img")) != 4 {
		tst.Fatal(canonical(root))
	}
	root = apply(tst, Video, `<img src="/proxy" data-canonical-src="https://example.com/x.mp4" title="題" alt="代替">`)
	media := first(tst, root, "video")
	if t.Get(media, "src") != "/proxy" || t.Get(media, "data-canonical-src") != "https://example.com/x.mp4" || t.Get(media, "data-title") != "題" {
		tst.Fatal(media.Attr)
	}
	root = apply(tst, Image, `<img src="x.png"><a href="x"><img src="y"></a><img data-src="" src="z">`)
	if len(nodes(tst, root, "a")) != 2 || t.Get(first(tst, root, "a"), "href") != "x.png" || t.Get(first(tst, root, "a"), "rel") != "noopener noreferrer" {
		tst.Fatal(canonical(root))
	}
	root = apply(tst, Image, `<img src="proxy" data-src="lazy" data-canonical-src="origin" data-diagram="mermaid" data-diagram-src="source">`)
	a := first(tst, root, "a")
	if t.Get(a, "href") != "lazy" || t.Get(a, "data-canonical-src") != "origin" || t.Get(a, "data-diagram") != "mermaid" || t.Has(first(tst, root, "img"), "data-diagram") {
		tst.Fatal(canonical(root))
	}
}
func TestTocAndFootnotes(tst *testing.T) {
	heading := func(level, id, title string) string {
		return "<h" + level + ` id="user-content-` + id + `">` + title + `<a class="anchor" href="#` + id + `"></a></h` + level + ">"
	}
	root := apply(tst, TableOfContents, "<p>[tOc]</p>"+heading("2", "a", "<b>A</b>")+heading("4", "b", "B")+heading("2", "c", "C")+heading("1", "d", "D"))
	expected, _ := t.Fragment(context.Background(), `<ul class="section-nav"><li><a href="#a">A</a><ul><li><a href="#b">B</a></li></ul></li><li><a href="#c">C</a></li><li><a href="#d">D</a></li></ul>`)
	if canonical(root.FirstChild) != canonical(expected.FirstChild) {
		tst.Fatal(canonical(root.FirstChild))
	}
	root = apply(tst, TableOfContents, `<p><a data-wikilink="true">_TOC_</a></p><p>[TOC]</p><p>文中 [TOC]</p>`+heading("1", "a", "A"))
	count := 0
	for _, n := range nodes(tst, root, "ul") {
		if t.HasClass(n, "section-nav") {
			count++
		}
	}
	if count != 2 || !strings.Contains(t.Content(root), "文中 [TOC]") {
		tst.Fatal(canonical(root))
	}
	wantHTML(tst, apply(tst, TableOfContents, "<p>[TOC]</p><h1>見出し</h1>"), "<h1>見出し</h1>")
	notes := `<p><sup><a data-footnote-ref id="fnref-a" href="#fn-a">1</a></sup></p><section data-footnotes><ol><li id="fn-a"><a href="#fnref-a" data-footnote-backref-idx="1">戻る</a></li></ol></section>`
	root = apply(tst, Footnote, notes)
	back := nodes(tst, root, "a")[1]
	if !t.HasClass(first(tst, root, "section"), "footnotes") || !t.HasClass(first(tst, root, "sup"), "footnote-ref") || !t.HasClass(back, "footnote-backref") || t.Get(back, "title") != "Back to reference 1" || t.Get(back, "aria-label") != t.Get(back, "title") {
		tst.Fatal(canonical(root))
	}
	wantHTML(tst, apply(tst, Footnote, `<sup><a data-footnote-ref>1</a></sup>`), `<sup><a data-footnote-ref>1</a></sup>`)
	root = apply(tst, Footnote, `<sup><a data-footnote-ref id="fnref-missing">1</a></sup><section data-footnotes></section>`)
	if t.Has(first(tst, root, "sup"), "class") {
		tst.Fatal(canonical(root))
	}
	root = apply(tst, Footnote, strings.Repeat(`<sup><a data-footnote-ref>1</a></sup>`, 1001)+`<section data-footnotes></section>`)
	if t.Has(first(tst, root, "section"), "class") {
		tst.Fatal(canonical(root))
	}
}
func TestDiffAndExternal(tst *testing.T) {
	root := apply(tst, InlineDiff, `<p>{+a+} [+b+] {-c-} [-d-] {++} {-x [+y+]-} {+&lt;script&gt;+}</p>`)
	if len(nodes(tst, root, "span")) != 8 || strings.Contains(canonical(root), "<script>") {
		tst.Fatal(canonical(root))
	}
	for _, tag := range []string{"pre", "code", "tt"} {
		source := "<" + tag + ">{+x+}</" + tag + ">"
		wantHTML(tst, apply(tst, InlineDiff, source), source)
	}
	wantHTML(tst, apply(tst, InlineDiff, "<p>[+前 {-中-} 後+]</p>"), `<p><span class="idiff left right addition">前 <span class="idiff left right deletion">中</span> 後</span></p>`)
	if len(nodes(tst, apply(tst, InlineDiff, "<p>{+a\u2028b+}</p>"), "span")) != 1 {
		tst.Fatal("Ruby の改行")
	}
	for _, href := range []string{"https://example.com/a", "http://example.com", "//example.com"} {
		a := first(tst, apply(tst, External, `<a href="`+href+`" rel="license">文字</a>`), "a")
		if t.Get(a, "target") != "_blank" || t.Get(a, "rel") != "nofollow noreferrer noopener license" {
			tst.Fatal(a.Attr)
		}
	}
	for _, href := range []string{"/a", "../a", "#a", "mailto:a@example.com", "ftp://example.com/a"} {
		a := first(tst, apply(tst, External, `<a href="`+href+`">文字</a>`), "a")
		if t.Has(a, "target") {
			tst.Fatal(a.Attr)
		}
	}
	a := first(tst, apply(tst, External, "<a href=\"http://[broken\">危\u202e険</a>"), "a")
	if t.Get(a, "target") != "_blank" || t.Content(a) != "危%E2%80%AE険" {
		tst.Fatal(canonical(a))
	}
	for _, href := range []string{"https://xn--r8jz45g.xn--zckzah/a", "https://example.com/%E2%80%AE"} {
		a := first(tst, apply(tst, External, `<a href="`+href+`">文字</a>`), "a")
		if !t.HasClass(a, "has-tooltip") || t.Get(a, "title") != href {
			tst.Fatal(a.Attr)
		}
	}
	a = first(tst, apply(tst, External, `<a href="/proxy" data-canonical-src="https://example.com/a">文字</a>`), "a")
	if t.Get(a, "target") != "_blank" || t.Get(a, "href") != "/proxy" {
		tst.Fatal(a.Attr)
	}
	root = apply(tst, External, "<a href=\"HTTPS://example.com\"><img alt=\"危\u202e険\" src=\"x\"></a>")
	if t.Get(first(tst, root, "a"), "rel") != "nofollow noreferrer noopener" || t.Get(first(tst, root, "img"), "alt") != "危%E2%80%AE険" {
		tst.Fatal(canonical(root))
	}
}
func TestTasksAndDirection(tst *testing.T) {
	for _, c := range [][2]string{{`<ul><li><input class="task-list-item-checkbox"> 本文 <b>強調</b><ul><li>子</li></ul></li></ul>`, "Check option: 本文 強調"}, {`<table><tr><th>名前</th><th>完了</th></tr><tr><td>A</td><td class="task-table-item"><input class="task-list-item-checkbox"></td></tr></table>`, `Check option in column "完了"`}, {`<ul><li><input class="task-list-item-checkbox"></li></ul>`, "Check option"}, {`<ul><li><input class="task-list-item-checkbox">` + strings.Repeat("😀", 80) + "</li></ul>", "Check option: " + strings.Repeat("😀", 80)}, {`<ul><li><input class="task-list-item-checkbox">` + strings.Repeat("😀", 101) + "</li></ul>", "Check option: " + strings.Repeat("😀", 99) + "…"}} {
		got := t.Get(first(tst, apply(tst, TaskList, c[0]), "input"), "aria-label")
		if got != c[1] {
			tst.Fatal(got, c[1])
		}
	}
	label := t.Get(first(tst, apply(tst, TaskList, `<ul><li><input class="task-list-item-checkbox">`+strings.Repeat("word ", 30)+"</li></ul>"), "input"), "aria-label")
	if !strings.HasSuffix(label, "…") || len([]rune(label)) > 114 {
		tst.Fatal(label)
	}
	root := apply(tst, TaskList, `<ul><li><input class="task-list-item-checkbox" data-inapplicable> 本文 <b>強調</b><p>段落</p><div>区画</div><ul><li>子</li></ul></li></ul>`)
	if len(nodes(tst, root, "s")) != 2 || first(tst, root, "p").FirstChild.Type != html.TextNode || first(tst, root, "input").NextSibling.Data != " " {
		tst.Fatal(canonical(root))
	}
	root = apply(tst, SetDirection, `<p dir="rtl">本文</p><div><p>中</p></div><ul class="section-nav"><li>目次</li></ul><table></table>`)
	if t.Get(first(tst, root, "p"), "dir") != "auto" || t.Has(nodes(tst, root, "p")[1], "dir") || t.Has(first(tst, root, "ul"), "dir") || t.Get(first(tst, root, "table"), "dir") != "auto" {
		tst.Fatal(canonical(root))
	}
}
func TestSyntaxAndPlantUML(tst *testing.T) {
	root := apply(tst, SyntaxHighlight, `<pre data-sourcepos="1:1-3:3" data-canonical-lang="js">余分<code>const x = 1;
</code>余分</pre>`)
	pre := first(tst, root, "pre")
	if t.Get(root.FirstChild, "class") != "gl-relative markdown-code-block js-markdown-code" || t.Get(pre, "v-pre") != "true" || t.Get(pre, "data-sourcepos") != "1:1-3:3" || len(t.Children(pre)) != 1 || strings.Contains(t.Content(root), "余分") {
		tst.Fatal(canonical(root))
	}
	children := []string{}
	for _, c := range t.Children(root.FirstChild) {
		children = append(children, c.Data)
	}
	if !reflect.DeepEqual(children, []string{"pre", "copy-code", "insert-code-snippet"}) {
		tst.Fatal(children)
	}
	line := first(tst, root, "span")
	if t.Get(line, "id") != "LC1" || t.Get(line, "data-lang") != "javascript" {
		tst.Fatal(line.Attr)
	}
	root = apply(tst, SyntaxHighlight, `<pre data-canonical-lang="suggestion"><code>x</code></pre><pre data-kroki-style="display"><code>y</code></pre>`)
	if len(nodes(tst, root, "copy-code")) != 0 || root.LastChild.Data != "pre" || t.Has(root.LastChild, "class") {
		tst.Fatal(canonical(root))
	}
	source := "a -> b"
	root = t.Root(t.Element("pre", t.Attr("data-canonical-lang", "plantuml", "data-sourcepos", "1:1-3:3"), t.Element("code", nil, t.Text(source+"\r\n"))))
	state := &State{Plantuml: true}
	if err := PlantUML(context.Background(), root, state); err != nil {
		tst.Fatal(err)
	}
	img := first(tst, root, "img")
	id := plantumlsource.ID(source)
	if t.Get(img, "src") != "/_/plantuml/"+id+".svg" || t.Get(img, "data-diagram-src") != "data:text/plain;base64,"+base64.StdEncoding.EncodeToString([]byte(source)) || t.Get(img, "data-sourcepos") != "1:1-3:3" || !reflect.DeepEqual(state.Diagrams, []Diagram{{id, source}}) {
		tst.Fatal(canonical(root), state.Diagrams)
	}
}
