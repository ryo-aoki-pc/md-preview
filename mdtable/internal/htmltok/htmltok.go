// Package htmltok は元の文字の位置を保った HTML の字句と木を作る。
package htmltok

import (
	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"regexp"
	"strings"
)

const space = `[` + pycompat.ReSpace + `]`
const notSpace = `[^` + pycompat.ReSpace

var comment = regexp.MustCompile(`^<!--(?:>|->|[\s\S]*?-->)`)
var cdata = regexp.MustCompile(`^<!\[CDATA\[[\s\S]*?\]\]>`)
var declaration = regexp.MustCompile(`^<![A-Za-z][^>]*>`)
var processing = regexp.MustCompile(`^<\?[\s\S]*?\?>`)
var startRE = regexp.MustCompile(`^<([A-Za-z][A-Za-z0-9-]*)((?:` + space + `+` + notSpace + `"'>/=]+(?:` + space + `*=` + space + `*(?:"[^"]*"|'[^']*'|` + notSpace + `"'=<>` + "`" + `]+))?)*)` + space + `*(/?)>`)
var endRE = regexp.MustCompile(`^</([A-Za-z][A-Za-z0-9-]*)` + space + `*>`)
var attributeRE = regexp.MustCompile(space + `*(` + notSpace + `"'>/=]+)(?:(` + space + `*=` + space + `*)("[^"]*"|'[^']*'|` + notSpace + `"'=<>` + "`" + `]+))?`)
var VoidElements = wordSet("area base br col embed hr img input link meta param source track wbr")
var RawTextElements = wordSet("script style textarea title xmp iframe noembed noframes plaintext")
var pClosers = wordSet("address article aside blockquote details dialog div dl fieldset figcaption figure footer form h1 h2 h3 h4 h5 h6 header hgroup hr main menu nav ol p pre search section table ul")

// htmltok.VOID_ELEMENTS などの frozenset に対応する集合の生成。
func wordSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, x := range strings.Fields(s) {
		m[x] = true
	}
	return m
}

type Attribute struct {
	Name  string
	Value *string
	Raw   string
}

// htmltok.parse_attributes に対応する属性解析。
func ParseAttributes(s string) []Attribute {
	var out []Attribute
	for _, m := range attributeRE.FindAllStringSubmatchIndex(s, -1) {
		if m[2] < 0 {
			continue
		}
		var value *string
		if m[6] >= 0 {
			v := s[m[6]:m[7]]
			if strings.HasPrefix(v, "'") || strings.HasPrefix(v, "\"") {
				v = v[1 : len(v)-1]
			}
			value = &v
		}
		out = append(out, Attribute{pycompat.Lower(s[m[2]:m[3]]), value, pycompat.LStrip(s[m[0]:m[1]])})
	}
	return out
}

type Token struct {
	Kind        string
	Start, End  int
	Text, Name  string
	Attributes  []Attribute
	SelfClosing bool
}

// htmltok.Token.attribute に対応する属性検索。
func (t *Token) Attribute(name string) *string {
	if t == nil {
		return nil
	}
	return attr(t.Attributes, name)
}

// htmltok.Token.attribute と Element.attribute に対応する共通の検索。
func attr(as []Attribute, name string) *string {
	for _, a := range as {
		if a.Name == name {
			if a.Value == nil {
				s := ""
				return &s
			}
			return a.Value
		}
	}
	return nil
}

// htmltok.tokenize に対応する字句解析。
func Tokenize(s string, start, end int) []*Token {
	var out []*Token
	pos, textStart := start, start
	flush := func(upto int) {
		if upto > textStart {
			out = append(out, &Token{Kind: "text", Start: textStart, End: upto, Text: s[textStart:upto]})
		}
	}
	for pos < end {
		lt := strings.IndexByte(s[pos:end], '<')
		if lt < 0 {
			break
		}
		lt += pos
		var token *Token
		for i, r := range []*regexp.Regexp{comment, cdata, declaration, processing} {
			if m := r.FindString(s[lt:end]); m != "" {
				kind := "other"
				if i == 0 {
					kind = "comment"
				}
				token = &Token{Kind: kind, Start: lt, End: lt + len(m), Text: m}
				break
			}
		}
		if token == nil {
			if m := startRE.FindStringSubmatch(s[lt:end]); m != nil {
				token = &Token{Kind: "start", Start: lt, End: lt + len(m[0]), Text: m[0], Name: pycompat.Lower(m[1]), Attributes: ParseAttributes(m[2]), SelfClosing: m[3] != ""}
			}
		}
		if token == nil {
			if m := endRE.FindStringSubmatch(s[lt:end]); m != nil {
				token = &Token{Kind: "end", Start: lt, End: lt + len(m[0]), Text: m[0], Name: pycompat.Lower(m[1])}
			}
		}
		if token == nil {
			pos = lt + 1
			continue
		}
		flush(lt)
		out = append(out, token)
		pos = token.End
		textStart = pos
	}
	flush(end)
	return out
}

type Node struct {
	Kind, Tag, Text, StartText string
	Attributes                 []Attribute
	Children                   []*Node
	Start, End                 int
	Closed                     bool
	Parent                     *Node
}

// htmltok.Element.attribute に対応する属性検索。
func (n *Node) Attribute(name string) *string { return attr(n.Attributes, name) }

// htmltok.build_tree に対応する省略された終了タグも扱う木の構築。
func BuildTree(tokens []*Token) *Node {
	root := &Node{Kind: "element", Tag: "#root"}
	stack := []*Node{root}
	closeTo := func(index, position int) {
		for _, e := range stack[index:] {
			if !e.Closed {
				e.End = position
			}
		}
		stack = stack[:index]
	}
	openIndex := func(names, boundary map[string]bool) int {
		for i := len(stack) - 1; i > 0; i-- {
			if names[stack[i].Tag] {
				return i
			}
			if boundary[stack[i].Tag] {
				return -1
			}
		}
		return -1
	}
	for _, t := range tokens {
		top := stack[len(stack)-1]
		switch t.Kind {
		case "text":
			top.Children = append(top.Children, &Node{Kind: "text", Text: t.Text, Start: t.Start, End: t.End})
		case "md":
			if top.Tag == "p" {
				closeTo(len(stack)-1, t.Start)
				top = stack[len(stack)-1]
			}
			top.Children = append(top.Children, &Node{Kind: "md", Text: t.Text, Start: t.Start, End: t.End})
		case "comment", "other":
			top.Children = append(top.Children, &Node{Kind: "raw", Text: t.Text, Start: t.Start, End: t.End})
		case "start":
			name := t.Name
			if name == "li" {
				if i := openIndex(wordSet("li"), wordSet("ul ol")); i >= 0 {
					closeTo(i, t.Start)
				}
			} else if name == "dt" || name == "dd" {
				if i := openIndex(wordSet("dt dd"), wordSet("dl")); i >= 0 {
					closeTo(i, t.Start)
				}
			}
			if pClosers[name] {
				if i := openIndex(wordSet("p"), wordSet("li dd dt td th blockquote div")); i >= 0 {
					closeTo(i, t.Start)
				}
			}
			e := &Node{Kind: "element", Tag: name, Attributes: t.Attributes, StartText: t.Text, Start: t.Start, End: t.End, Parent: stack[len(stack)-1]}
			e.Parent.Children = append(e.Parent.Children, e)
			if !VoidElements[name] && !t.SelfClosing {
				stack = append(stack, e)
			} else {
				e.Closed = true
			}
		case "end":
			if i := openIndex(map[string]bool{t.Name: true}, nil); i >= 0 {
				closeTo(i+1, t.Start)
				e := stack[i]
				e.End = t.End
				e.Closed = true
				stack = stack[:i]
			}
		}
	}
	if len(stack) > 1 {
		last := 0
		if len(tokens) > 0 {
			last = tokens[len(tokens)-1].End
		}
		closeTo(1, last)
	}
	return root
}
