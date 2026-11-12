package filters

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"github.com/ryo-aoki-pc/md-tools/internal/highlight"
	t "github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/sanitize"
	"golang.org/x/net/html"
)

var attrValue = regexp.MustCompile(`^([\w-]+)="?([^"\s]+)"?$`)
var attrSize = regexp.MustCompile(`^\d{1,4}(%|px)?$`)

// 元の正規表現には u フラグがないため、最長100文字は UTF-16 の符号単位で数える。
func imageAttributeText(s string) (string, int) {
	if !strings.HasPrefix(s, "{") {
		return "", 0
	}
	units, end := 0, 0
	for i, r := range s[1:] {
		if r == '\n' {
			break
		}
		if r == '}' && units >= 1 && units <= 100 {
			end = i + 2
		}
		if r > 0xffff {
			units += 2
		} else {
			units++
		}
		if units > 100 {
			break
		}
	}
	if end == 0 {
		return "", 0
	}
	return s[1 : end-1], end
}

func Attributes(ctx context.Context, root *html.Node, _ *State) error {
	nodes, err := tagged(ctx, root, "img")
	if err != nil {
		return err
	}
	for _, img := range nodes {
		sibling := img.NextSibling
		if sibling == nil || sibling.Type != html.TextNode {
			continue
		}
		body, end := imageAttributeText(sibling.Data)
		if end == 0 {
			continue
		}
		for _, a := range t.Fields(t.Trim(body)) {
			v := attrValue.FindStringSubmatch(a)
			if len(v) > 0 && (v[1] == "width" || v[1] == "height") && attrSize.MatchString(v[2]) {
				t.Set(img, v[1], v[2])
			}
			if a == ".float-right" || a == ".float-left" || a == ".float-none" {
				classes := t.Classes(img)
				classes = append(classes, "glfm-"+a[1:])
				t.Set(img, "class", strings.Join(classes, " "))
			}
		}
		sibling.Data = sibling.Data[end:]
	}
	return ctx.Err()
}
func Math(ctx context.Context, root *html.Node, _ *State) error {
	nodes, err := t.Elements(ctx, root, func(n *html.Node) bool {
		return contains([]string{"pre", "code", "span"}, n.Data) && (t.Has(n, "data-math-style") || n.Data == "pre" && t.Get(n, "data-canonical-lang") == "math")
	})
	if err != nil {
		return err
	}
	for i, n := range nodes {
		if i >= 1000 {
			break
		}
		if n.Data == "pre" && t.Get(n, "data-canonical-lang") == "math" && !t.Has(n, "data-math-style") {
			t.Set(n, "data-math-style", "display")
		}
		t.Set(n, "class", "js-render-math")
	}
	return ctx.Err()
}

var colorPattern = func() *regexp.Regexp {
	alpha := `(?:0(?:\.\d+)?|\.\d+|1(?:\.0+)?)`
	percents := `(?:\d{1,2}|100)%`
	channel := `(?:,[\t\n\v\f\r ]*(?:` + alpha + `|` + percents + `))?`
	bits := `(?:\d{1,2}|1\d\d|2(?:[0-4]\d|5[0-5]))`
	degs := `-?\d+(?:deg)?`
	rads := `-?(?:\d+(?:\.\d+)?|\.\d+)rad`
	rgb := `rgba?\((?:(?:` + bits + `,[\t\n\v\f\r ]*){2}` + bits + `|(?:` + percents + `,[\t\n\v\f\r ]*){2}` + percents + `)` + channel + `\)`
	hsl := `hsla?\((?:` + degs + `|` + rads + `),[\t\n\v\f\r ]*` + percents + `,[\t\n\v\f\r ]*` + percents + channel + `\)`
	return regexp.MustCompile(`(?i)^(?:#[a-f\d]{3}|#[a-f\d]{4}|#[a-f\d]{6}|#[a-f\d]{8}|` + rgb + `|` + hsl + `)$`)
}()

func ParseColor(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return colorPattern.MatchString(s)
}
func Color(ctx context.Context, root *html.Node, _ *State) error {
	nodes, err := tagged(ctx, root, "code")
	if err != nil {
		return err
	}
	for _, n := range nodes {
		source := t.ContentContext(ctx, n)
		unescaped := strings.TrimPrefix(source, "\\")
		if !ParseColor(unescaped) {
			continue
		}
		if strings.HasPrefix(source, "\\") {
			t.SetChildren(n, t.Text(unescaped))
		} else {
			t.Append(n, t.Element("span", t.Attr("class", "gfm-color_chip"), t.Element("span", t.Attr("style", "background-color: "+unescaped+";"))))
		}
	}
	return ctx.Err()
}
func Mermaid(ctx context.Context, root *html.Node, _ *State) error {
	nodes, err := t.Elements(ctx, root, func(n *html.Node) bool { return n.Data == "pre" && t.Get(n, "data-canonical-lang") == "mermaid" })
	if err != nil {
		return err
	}
	for _, n := range nodes {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "code" {
				t.AddClass(c, "js-render-mermaid")
			}
		}
	}
	return ctx.Err()
}
func playable(ctx context.Context, root *html.Node, kind string) error {
	exts := []string{"mp4", "m4v", "mov", "webm", "ogv"}
	if kind == "audio" {
		exts = []string{"mp3", "oga", "ogg", "spx", "wav"}
	}
	nodes, err := tagged(ctx, root, "img")
	if err != nil {
		return err
	}
	for _, img := range nodes {
		if t.Ancestor(img, func(a *html.Node) bool { return a.Data == "a" }) {
			continue
		}
		src := t.Get(img, "data-canonical-src")
		if src == "" {
			src = t.Get(img, "src")
		}
		last := src[strings.LastIndexByte(src, '/')+1:]
		i := strings.LastIndexByte(last, '.')
		if i < 0 || !contains(exts, strings.ToLower(last[i+1:])) {
			continue
		}
		media := t.Element(kind, t.Attr("controls", "true", "data-setup", "{}"))
		if t.Has(img, "src") {
			t.Set(media, "src", t.Get(img, "src"))
		}
		if title, ok := firstAttr([]*html.Node{img, img}, []string{"title", "alt"}); ok {
			t.Set(media, "data-title", title)
		}
		if t.Has(img, "data-canonical-src") {
			t.Set(media, "data-canonical-src", t.Get(img, "data-canonical-src"))
		}
		if kind == "video" {
			t.Set(media, "preload", "metadata")
			t.Set(media, "class", "gl-rounded-lg")
			for _, k := range []string{"height", "width"} {
				if t.Has(img, k) {
					t.Set(media, k, t.Get(img, k))
				}
			}
		}
		t.Replace(img, t.Element("span", t.Attr("class", "media-container "+kind+"-container"), media))
	}
	return ctx.Err()
}
func Video(ctx context.Context, root *html.Node, _ *State) error { return playable(ctx, root, "video") }
func Audio(ctx context.Context, root *html.Node, _ *State) error { return playable(ctx, root, "audio") }
func Image(ctx context.Context, root *html.Node, _ *State) error {
	nodes, err := tagged(ctx, root, "img")
	if err != nil {
		return err
	}
	for _, img := range nodes {
		if t.Ancestor(img, func(a *html.Node) bool { return a.Data == "a" }) || t.Has(img, "data-src") && t.Get(img, "data-src") == "" || t.Has(img, "src") && t.Get(img, "src") == "" {
			continue
		}
		a := t.Element("a", t.Attr("class", "no-attachment-icon", "target", "_blank", "rel", "noopener noreferrer"))
		if href, ok := firstAttr([]*html.Node{img, img}, []string{"data-src", "src"}); ok {
			t.Set(a, "href", href)
		}
		if t.Has(img, "data-canonical-src") {
			t.Set(a, "data-canonical-src", t.Get(img, "data-canonical-src"))
		}
		if t.Has(img, "data-diagram") && t.Has(img, "data-diagram-src") {
			for _, k := range []string{"data-diagram", "data-diagram-src"} {
				t.Set(a, k, t.Get(img, k))
				t.Del(img, k)
			}
		}
		t.Replace(img, a)
		t.Append(a, img)
	}
	return ctx.Err()
}

var urlScheme = regexp.MustCompile(`^([A-Za-z][A-Za-z\d+.-]*):`)
var invalidPercent = regexp.MustCompile(`%([^a-fA-F0-9]|[a-fA-F0-9]([^a-fA-F0-9]|$)|$)`)

func strictURL(s string) (*url.URL, bool) {
	for _, r := range s {
		if r < 0x21 || r > 0x7e {
			return nil, false
		}
	}
	if invalidPercent.MatchString(s) || !sanitize.PermitURL(s) {
		return nil, false
	}
	base, _ := url.Parse("https://markdown.invalid/")
	u, err := base.Parse(s)
	return u, err == nil
}
func External(ctx context.Context, root *html.Node, _ *State) error {
	nodes, err := t.Elements(ctx, root, func(n *html.Node) bool { return n.Data == "a" && t.Get(n, "href") != "" })
	if err != nil {
		return err
	}
	for _, a := range nodes {
		source := t.Get(a, "href")
		if t.Has(a, "data-canonical-src") {
			source = t.Get(a, "data-canonical-src")
		}
		if strings.HasPrefix(source, "#") {
			continue
		}
		uri, valid := strictURL(source)
		m := urlScheme.FindStringSubmatch(source)
		scheme := ""
		if len(m) > 0 {
			scheme = strings.ToLower(m[1])
		}
		if valid && scheme == "" && !strings.HasPrefix(source, "//") {
			continue
		}
		if err := t.Walk(ctx, a, func(n *html.Node) error {
			if n == a {
				return nil
			}
			if n.Type == html.TextNode {
				n.Data = strings.ReplaceAll(n.Data, "\u202e", "%E2%80%AE")
			}
			for i := range n.Attr {
				n.Attr[i].Val = strings.ReplaceAll(n.Attr[i].Val, "\u202e", "%E2%80%AE")
			}
			return nil
		}); err != nil {
			return err
		}
		if valid && (strings.HasPrefix(uri.Hostname(), "xn--") || strings.Contains(source, "%E2%80%AE")) {
			t.AddClass(a, "has-tooltip")
			title := uri.String()
			if uri.Path == "" && uri.Host != "" {
				u := *uri
				u.Path = "/"
				title = u.String()
			}
			t.Set(a, "title", title)
		}
		if !valid || scheme == "" || scheme == "http" || scheme == "https" {
			rel := "nofollow noreferrer noopener"
			if t.Get(a, "rel") == "license" {
				rel += " license"
			}
			t.Set(a, "rel", rel)
			t.Set(a, "target", "_blank")
		}
	}
	return ctx.Err()
}
func SetDirection(ctx context.Context, root *html.Node, _ *State) error {
	for n := root.FirstChild; n != nil; n = n.NextSibling {
		if n.Type == html.ElementNode && contains([]string{"p", "h1", "h2", "h3", "h4", "h5", "h6", "ol", "ul", "blockquote", "table"}, n.Data) && !(n.Data == "ul" && strings.Join(t.Classes(n), " ") == "section-nav") {
			t.Set(n, "dir", "auto")
		}
	}
	return ctx.Err()
}
func SyntaxHighlight(ctx context.Context, root *html.Node, _ *State) error {
	nodes, err := t.Elements(ctx, root, func(n *html.Node) bool { return n.Data == "pre" && !t.Has(n, "data-kroki-style") })
	if err != nil {
		return err
	}
	for _, pre := range nodes {
		code := t.OnlyCode(pre)
		if code == nil {
			continue
		}
		result, err := highlight.Code(ctx, t.ContentContext(ctx, code), t.Get(pre, "data-canonical-lang"))
		if err != nil {
			return err
		}
		t.SetChildren(code, result.Children...)
		t.SetChildren(pre, code)
		t.AddClass(pre, "code", "highlight", "js-syntax-highlight")
		if result.Language != "" {
			t.AddClass(pre, "language-"+result.Language)
		}
		t.Set(pre, "v-pre", "true")
		wrapper := t.Element("div", t.Attr("class", "gl-relative markdown-code-block js-markdown-code"))
		t.Replace(pre, wrapper)
		t.Append(wrapper, pre)
		if result.Language != "suggestion" {
			t.Append(wrapper, t.Element("copy-code", nil))
			t.Append(wrapper, t.Element("insert-code-snippet", nil))
		}
	}
	return ctx.Err()
}
func heading(n *html.Node) bool {
	return len(n.Data) == 2 && n.Data[0] == 'h' && n.Data[1] >= '1' && n.Data[1] <= '6'
}
func TableOfContents(ctx context.Context, root *html.Node, _ *State) error {
	type entry struct {
		level      int
		parent     *entry
		children   []*entry
		href, text string
	}
	top := &entry{}
	current := top
	nodes, err := t.Elements(ctx, root, heading)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		anchors, err := t.Elements(ctx, n, func(a *html.Node) bool { return a.Data == "a" && t.HasClass(a, "anchor") })
		if err != nil {
			return err
		}
		if len(anchors) == 0 {
			continue
		}
		level := int(n.Data[1] - '0')
		for current.level >= level {
			current = current.parent
		}
		href := t.Get(anchors[0], "href")
		if len(href) > 0 {
			href = href[1:]
		}
		next := &entry{level: level, parent: current, href: href, text: t.Trim(t.ContentContext(ctx, n))}
		current.children = append(current.children, next)
		current = next
	}
	var list func([]*entry, bool) *html.Node
	list = func(items []*entry, top bool) *html.Node {
		ul := t.Element("ul", nil)
		if top {
			t.Set(ul, "class", "section-nav")
		}
		for _, child := range items {
			li := t.Element("li", nil, t.Element("a", t.Attr("href", "#"+child.href), t.Text(child.text)))
			if len(child.children) > 0 {
				t.Append(li, list(child.children, false))
			}
			t.Append(ul, li)
		}
		return ul
	}
	var toc *html.Node
	if len(top.children) > 0 {
		toc = list(top.children, true)
	}
	paragraphs, err := tagged(ctx, root, "p")
	if err != nil {
		return err
	}
	for _, p := range paragraphs {
		if err := ctx.Err(); err != nil {
			return err
		}
		standard := p.FirstChild != nil && p.FirstChild == p.LastChild && p.FirstChild.Type == html.TextNode && strings.HasPrefix(strings.ToLower(p.FirstChild.Data), "[toc]") && strings.ToLower(t.Trim(p.FirstChild.Data)) == "[toc]"
		gollum := false
		if strings.ToLower(t.ContentContext(ctx, p)) == "_toc_" {
			for c := p.FirstChild; c != nil; c = c.NextSibling {
				if c.Data == "a" && t.Get(c, "data-wikilink") == "true" {
					for cc := c.FirstChild; cc != nil; cc = cc.NextSibling {
						if cc.Type == html.TextNode && strings.HasPrefix(strings.ToLower(cc.Data), "_toc_") {
							gollum = true
						}
					}
				}
			}
		}
		if standard || gollum {
			if toc == nil {
				t.Replace(p)
			} else {
				copy, err := t.CloneContext(ctx, toc)
				if err != nil {
					return err
				}
				t.Replace(p, copy)
			}
		}
	}
	return nil
}
func Footnote(ctx context.Context, root *html.Node, _ *State) error {
	links, err := t.Elements(ctx, root, func(n *html.Node) bool {
		return n.Data == "a" && t.Has(n, "data-footnote-ref") && n.Parent != nil && n.Parent.Data == "sup"
	})
	if err != nil {
		return err
	}
	if len(links) > 1000 {
		return nil
	}
	sections, err := t.Elements(ctx, root, func(n *html.Node) bool { return n.Data == "section" && t.Has(n, "data-footnotes") })
	if err != nil {
		return err
	}
	if len(sections) == 0 {
		return nil
	}
	t.AddClass(sections[0], "footnotes")
	for _, link := range links {
		if t.Get(link, "id") == "" {
			continue
		}
		target := strings.TrimPrefix(t.Get(link, "href"), "#")
		var item *html.Node
		for _, section := range sections {
			items, err := t.Elements(ctx, section, func(n *html.Node) bool { return n.Data == "li" && t.Get(n, "id") == target })
			if err != nil {
				return err
			}
			if len(items) > 0 {
				item = items[0]
				break
			}
		}
		if item == nil {
			continue
		}
		t.AddClass(link.Parent, "footnote-ref")
		backs, err := t.Elements(ctx, item, func(n *html.Node) bool { return n.Data == "a" && t.Get(n, "href") == "#"+t.Get(link, "id") })
		if err != nil {
			return err
		}
		if len(backs) > 0 {
			back := backs[0]
			title := "Back to reference " + t.Get(back, "data-footnote-backref-idx")
			t.Set(back, "title", title)
			t.Set(back, "aria-label", title)
			t.AddClass(back, "footnote-backref")
		}
	}
	return nil
}

var deletion = regexp.MustCompile(`(?:\[\-([^\n]*?)\-\]|\{\-([^\n]*?)\-\})`)
var addition = regexp.MustCompile(`(?:\[\+([^\n]*?)\+\]|\{\+([^\n]*?)\+\})`)

func InlineDiff(ctx context.Context, root *html.Node, _ *State) error {
	nodes, err := t.Nodes(ctx, root, func(n *html.Node) bool { return n.Type == html.TextNode })
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if t.Ancestor(n, func(a *html.Node) bool { return a.Data == "pre" || a.Data == "code" || a.Data == "tt" }) {
			continue
		}
		if !strings.Contains(n.Data, "[-") && !strings.Contains(n.Data, "{-") && !strings.Contains(n.Data, "[+") && !strings.Contains(n.Data, "{+") {
			continue
		}
		before, err := t.HTML(n)
		if err != nil {
			return err
		}
		after := before
		for _, spec := range []struct {
			re   *regexp.Regexp
			kind string
		}{{deletion, "deletion"}, {addition, "addition"}} {
			after = spec.re.ReplaceAllStringFunc(after, func(s string) string {
				m := spec.re.FindStringSubmatch(s)
				return `<span class="idiff left right ` + spec.kind + `">` + m[1] + m[2] + `</span>`
			})
		}
		if before != after {
			fragment, err := t.Fragment(ctx, after)
			if err != nil {
				return err
			}
			t.Replace(n, t.Children(fragment)...)
		}
	}
	return ctx.Err()
}
func TaskList(ctx context.Context, root *html.Node, _ *State) error {
	nodes, err := t.Elements(ctx, root, func(n *html.Node) bool { return n.Data == "input" && t.HasClass(n, "task-list-item-checkbox") })
	if err != nil {
		return err
	}
	for _, input := range nodes {
		if err := ctx.Err(); err != nil {
			return err
		}
		parent := input.Parent
		if parent == nil {
			continue
		}
		label := ""
		if t.HasClass(parent, "task-table-item") {
			row := parent.Parent
			var table *html.Node
			for a := parent.Parent; a != nil; a = a.Parent {
				if a.Data == "table" {
					table = a
					break
				}
			}
			if row != nil && table != nil {
				index := 0
				for c := row.FirstChild; c != nil && c != parent; c = c.NextSibling {
					if c.Type == html.ElementNode {
						index++
					}
				}
				rows, err := tagged(ctx, table, "tr")
				if err != nil {
					return err
				}
				if len(rows) > 0 {
					col := 0
					for c := rows[0].FirstChild; c != nil; c = c.NextSibling {
						if c.Type != html.ElementNode {
							continue
						}
						if col == index {
							if name := t.Trim(t.ContentContext(ctx, c)); name != "" {
								label = `Check option in column "` + name + `"`
							}
							break
						}
						col++
					}
				}
			}
		} else {
			var b strings.Builder
			for c := input.NextSibling; c != nil; c = c.NextSibling {
				if c.Data == "ol" || c.Data == "ul" {
					break
				}
				b.WriteString(t.ContentContext(ctx, c))
			}
			name := t.Trim(b.String())
			characters := []rune(name)
			if len(characters) > 100 {
				prefix := string(characters[:100])
				stop := strings.LastIndexByte(prefix, ' ')
				if stop < 0 {
					name = string(characters[:99]) + "…"
				} else {
					name = prefix[:stop] + "…"
				}
			}
			if name != "" {
				label = "Check option: " + name
			}
		}
		if label == "" {
			label = t.Get(input, "aria-label")
			if label == "" {
				label = "Check option"
			}
		}
		t.Set(input, "aria-label", label)
		if !t.Has(input, "data-inapplicable") {
			continue
		}
		tail := []*html.Node{}
		for c := input.NextSibling; c != nil; c = c.NextSibling {
			tail = append(tail, c)
		}
		parent.InsertBefore(t.Text(" "), input.NextSibling)
		var strike func(*html.Node) error
		strike = func(n *html.Node) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if n.Type == html.TextNode && t.Trim(n.Data) != "" {
				s := t.Element("s", t.Attr("class", "inapplicable"))
				t.Replace(n, s)
				t.Append(s, n)
			} else if !contains([]string{"p", "div", "ul", "ol"}, n.Data) {
				for _, c := range t.Children(n) {
					if err := strike(c); err != nil {
						return err
					}
				}
			}
			return nil
		}
		for _, n := range tail {
			if err := strike(n); err != nil {
				return err
			}
		}
	}
	return nil
}
