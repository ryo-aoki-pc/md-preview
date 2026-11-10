// Package sanitize は、GitLab の許可リストで木とリンクを消毒する。
package sanitize

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ryo-aoki-pc/md-preview/internal/htmltree"
	"golang.org/x/net/html"
)

func words(s string) map[string]bool {
	m := map[string]bool{}
	for _, k := range strings.Fields(s) {
		m[k] = true
	}
	return m
}

var Tags = words("h1 h2 h3 h4 h5 h6 h7 h8 br b i strong em a pre code img tt div ins del sup sub p ol ul table thead tbody tfoot blockquote dl dt dd kbd q samp var hr ruby rt rp li tr td th s strike summary details caption figure figcaption abbr bdo cite dfn mark small span time wbr section input")
var global = words("abbr accept accept-charset accesskey action align alt aria-describedby aria-hidden aria-label aria-labelledby axis border cellpadding cellspacing char charoff charset checked clear cols colspan color compact coords datetime dir disabled enctype for frame headers height hreflang hspace ismap label lang maxlength media method multiple nohref noshade nowrap open progress prompt readonly rel rev role rows rowspan rules scope selected shape size span start summary tabindex target title type usemap valign value vspace width itemprop data-sourcepos data-escaped-char")
var attributes = map[string]map[string]bool{
	"a": words("href name data-wikilink data-placeholder id class data-footnote-ref data-footnote-backref data-footnote-backref-idx data-heading-content"), "img": words("src longdesc data-diagram data-diagram-src data-placeholder class"),
	"blockquote": words("cite"), "del": words("cite"), "ins": words("cite"), "q": words("cite"), "span": words("data-math-style data-placeholder class"), "code": words("data-math-style class"), "pre": words("data-canonical-lang data-lang-params data-math-style data-mermaid-style data-kroki-style"), "abbr": words("title"), "th": words("style class"), "td": words("style class"), "table": words("data-table-fields data-table-filter data-table-markdown"),
	"li": words("id class"), "h1": words("id"), "h2": words("id"), "h3": words("id"), "h4": words("id"), "h5": words("id"), "h6": words("id"), "div": words("class"), "p": words("class"), "ul": words("class"), "ol": words("class"), "input": words("class data-inapplicable data-indeterminate data-checkbox-sourcepos"), "section": words("data-footnotes"),
}
var exact = map[string]map[string]bool{"a": words("anchor"), "p": words("markdown-alert-title"), "code": words("idiff"), "ul": words("task-list"), "ol": words("task-list"), "li": {"task-list-item": true, "inapplicable task-list-item": true}, "th": words("task-table-item"), "td": words("task-table-item"), "input": words("task-list-item-checkbox"), "div": {"markdown-alert markdown-alert-note": true, "markdown-alert markdown-alert-tip": true, "markdown-alert markdown-alert-important": true, "markdown-alert markdown-alert-warning": true, "markdown-alert markdown-alert-caution": true}}
var style = regexp.MustCompile(`text-align: (center|left|right)`)
var scheme = regexp.MustCompile(`(?i)^([a-z][a-z\d+.-]*):`)
var validScheme = regexp.MustCompile(`^[A-Za-z][A-Za-z\d+.-]*$`)

func transform(ctx context.Context, root *html.Node) error {
	nodes, err := htmltree.Elements(ctx, root, nil)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if err := ctx.Err(); err != nil {
			return err
		}
		a := n.Attr[:0]
		for _, v := range n.Attr {
			if v.Namespace != "" || strings.Contains(v.Key, ":") || strings.HasPrefix(v.Key, "xlink") || strings.HasPrefix(v.Key, "xml") {
				continue
			}
			a = append(a, v)
		}
		n.Attr = a
		if n.Data == "a" {
			if htmltree.Get(n, "rel") != "license" {
				htmltree.Del(n, "rel")
			}
			if htmltree.Ancestor(n, func(p *html.Node) bool { return p.Data == "a" }) {
				htmltree.Replace(n, htmltree.Children(n)...)
				continue
			}
		}
		for _, k := range []string{"cite", "src", "longdesc"} {
			if htmltree.Has(n, k) {
				v := htmltree.Get(n, k)
				m := scheme.FindStringSubmatch(htmltree.Trim(v))
				if len(m) > 0 && (strings.EqualFold(m[1], "http") || strings.EqualFold(m[1], "https")) {
					htmltree.Set(n, k, htmltree.Trim(strings.Replace(v, m[1]+":", strings.ToLower(m[1])+":", 1)))
				}
			}
		}
		if (n.Data == "th" || n.Data == "td") && htmltree.Has(n, "style") {
			m := style.FindStringSubmatch(htmltree.Get(n, "style"))
			if len(m) > 0 {
				htmltree.Set(n, "style", m[0])
			} else {
				htmltree.Del(n, "style")
			}
		}
		if htmltree.Has(n, "data-table-fields") {
			value, e := htmltree.ParseJSONContext(ctx, htmltree.Get(n, "data-table-fields"))
			list, ok := value.([]any)
			valid := e == nil && ok
			for i, v := range list {
				obj, ok := v.(htmltree.Object)
				if !ok {
					valid = false
					break
				}
				filtered := htmltree.Object{}
				for _, p := range obj {
					if p.Key == "key" || p.Key == "label" || p.Key == "sortable" {
						filtered = append(filtered, p)
					}
				}
				list[i] = filtered
			}
			if valid {
				htmltree.Set(n, "data-table-fields", htmltree.JSON(list))
			} else {
				htmltree.Del(n, "data-table-fields")
			}
		}
		if htmltree.Has(n, "id") {
			prefix := ""
			if n.Data == "a" {
				prefix = "fnref-"
			} else if n.Data == "li" {
				prefix = "fn-"
			} else if len(n.Data) == 2 && n.Data[0] == 'h' && n.Data[1] >= '1' && n.Data[1] <= '6' {
				prefix = "user-content-"
			}
			if prefix != "" && !strings.HasPrefix(htmltree.Get(n, "id"), prefix) {
				htmltree.Del(n, "id")
			}
		}
		if htmltree.Has(n, "class") {
			classes := htmltree.Classes(n)
			value := strings.Join(classes, " ")
			allowed := exact[n.Data][value]
			if n.Data == "span" {
				allowed = strings.Contains(value, "idiff")
				for _, c := range classes {
					if c != "idiff" && c != "left" && c != "right" && c != "deletion" && c != "addition" {
						allowed = false
					}
				}
			} else if n.Data == "img" {
				allowed = true
				for _, c := range classes {
					if c != "glfm-float-right" && c != "glfm-float-left" && c != "glfm-float-none" {
						allowed = false
					}
				}
			}
			if !allowed {
				htmltree.Del(n, "class")
			} else {
				htmltree.Set(n, "class", value)
			}
		}
		if n.Data == "input" && !(htmltree.Get(n, "type") == "checkbox" && strings.Join(htmltree.Classes(n), " ") == "task-list-item-checkbox" && n.Parent != nil && (n.Parent.Data == "li" || n.Parent.Data == "td" || n.Parent.Data == "th")) {
			htmltree.Replace(n)
		}
	}
	return nil
}
func protocolOK(k, v string) bool {
	if k != "src" && k != "cite" && k != "longdesc" {
		return true
	}
	i := strings.IndexByte(v, ':')
	if i < 0 {
		return true
	}
	for _, c := range "/?#" {
		j := strings.IndexRune(v, c)
		if j >= 0 && j < i {
			return true
		}
	}
	return v[:i] == "http" || v[:i] == "https"
}
func Document(ctx context.Context, root *html.Node) (*html.Node, error) {
	if err := transform(ctx, root); err != nil {
		return nil, err
	}
	var visit func(*html.Node, int) error
	visit = func(n *html.Node, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 512 {
			return htmltree.ErrDepth
		}
		for _, c := range htmltree.Children(n) {
			if c.Type == html.CommentNode || c.Type == html.DoctypeNode {
				htmltree.Replace(c)
				continue
			}
			if c.Type != html.ElementNode {
				continue
			}
			if c.Data == "script" {
				htmltree.Replace(c)
				continue
			}
			if err := visit(c, depth+1); err != nil {
				return err
			}
			allowed := Tags[c.Data]
			ancestors := ""
			switch c.Data {
			case "li":
				ancestors = "ul ol"
			case "thead", "tbody", "tfoot", "tr", "td", "th":
				ancestors = "table"
			}
			if ancestors != "" && !htmltree.Ancestor(c, func(p *html.Node) bool { return words(ancestors)[p.Data] }) {
				allowed = false
			}
			if !allowed {
				htmltree.Replace(c, htmltree.Children(c)...)
				continue
			}
			attrs := c.Attr[:0]
			for _, a := range c.Attr {
				if (global[a.Key] || attributes[c.Data][a.Key]) && protocolOK(a.Key, a.Val) {
					attrs = append(attrs, a)
				}
			}
			c.Attr = attrs
			c.Namespace = ""
		}
		return nil
	}
	if err := visit(root, 0); err != nil {
		return nil, err
	}
	if err := transform(ctx, root); err != nil {
		return nil, err
	}
	return root, nil
}
func SafeProtocol(s string) bool {
	var b strings.Builder
	for _, r := range strings.ToLower(htmltree.Trim(s)) {
		if r >= 'a' && r <= 'z' || strings.ContainsRune("+.-", r) {
			b.WriteRune(r)
		}
	}
	s = b.String()
	return s != "data" && s != "javascript" && s != "vbscript"
}

// URL のスキーム判定は net/url に委ねず、元の文字列で検査する。
func PermitURL(value string) bool {
	s := htmltree.Trim(value)
	if !utf8.ValidString(s) {
		return false
	}
	stop := strings.IndexAny(s, "/?#")
	prefix := s
	if stop >= 0 {
		prefix = s[:stop]
	}
	// 元の ^([^/?#]*): は貪欲なので、区切りより前の最後の : を見る。
	colon := strings.LastIndexByte(prefix, ':')
	has := colon >= 0
	if has && (!SafeProtocol(s[:colon]) || !validScheme.MatchString(s[:colon])) {
		return false
	}
	schemeName, rest := "", s
	if has {
		schemeName = strings.ToLower(s[:colon])
		rest = s[colon+1:]
	}
	special := schemeName == "http" || schemeName == "https" || schemeName == "ftp" || schemeName == "ws" || schemeName == "wss"
	normalized := strings.NewReplacer("\t", "", "\n", "", "\r", "", "\\", "/").Replace(rest)
	// 基準が https://markdown.invalid/ なので、同じ https: の単独パスは相対として有効。
	if schemeName == "https" && !strings.HasPrefix(normalized, "//") {
		return true
	}
	if special || strings.HasPrefix(normalized, "//") {
		host := strings.TrimLeft(normalized, "/")
		if i := strings.IndexAny(host, "/?#"); i >= 0 {
			host = host[:i]
		}
		if i := strings.LastIndexByte(host, '@'); i >= 0 {
			host = host[i+1:]
		}
		if !validHost(host) {
			return false
		}
	}

	return true
}
func Links(ctx context.Context, root *html.Node) (*html.Node, error) {
	keys := []string{"href", "src", "data-src", "data-canonical-src"}
	nodes, err := htmltree.Elements(ctx, root, func(n *html.Node) bool {
		for _, k := range keys {
			if htmltree.Has(n, k) {
				return true
			}
		}
		return false
	})
	if err != nil {
		return nil, err
	}
	if len(nodes) > 10000 {
		return htmltree.Fragment(ctx, "<p>Timeout while sanitizing links - rendering aborted. Please reduce the number of links if possible.</p>\n")
	}
	for _, n := range nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, k := range keys {
			if htmltree.Has(n, k) {
				v := htmltree.Trim(htmltree.Get(n, k))
				if PermitURL(v) {
					htmltree.Set(n, k, v)
				} else {
					htmltree.Del(n, k)
				}
			}
		}
	}
	return root, nil
}
