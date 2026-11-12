// Package cellfromhtml は HTML セルの中身を Excel 用の Markdown にする。
package cellfromhtml

import (
	"fmt"
	"github.com/ryo-aoki-pc/md-table-excel/internal/blocks"
	"github.com/ryo-aoki-pc/md-table-excel/internal/cellsyntax"
	"github.com/ryo-aoki-pc/md-table-excel/internal/htmltok"
	"github.com/ryo-aoki-pc/md-table-excel/internal/inline"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"regexp"
	"sort"
	"strings"
)

var ws = regexp.MustCompile(`[ \t\n\r\f]+`)
var entityLike = regexp.MustCompile(`^&#?[A-Za-z0-9]+;`)
var languageClass = regexp.MustCompile(`(?:^|[` + pycompat.ReSpace + `])(?:language|lang)-([^` + pycompat.ReSpace + `]+)`)
var spacedLink = regexp.MustCompile(`!?\[(.+?)\]\((.+?)( ".+?")?\)`)
var BlockTags = wordSet("p div ul ol dl pre blockquote h1 h2 h3 h4 h5 h6 hr table details summary figure figcaption section article aside nav header footer center form fieldset address main menu dir dialog hgroup search noscript video audio li dt dd caption thead tbody tfoot tr td th colgroup col iframe object")
var emphasis = map[string]string{"strong": "**", "b": "**", "em": "*", "i": "*", "del": "~~", "s": "~~", "strike": "~~"}

// cell_from_html.BLOCK_TAGS の frozenset に対応する集合の生成。
func wordSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(s) {
		out[w] = true
	}
	return out
}

type emphasisMark struct {
	Opening, Closing            rune
	Marker, OpenText, CloseText string
	Depth                       int
}
type context struct {
	source   string
	emphasis []emphasisMark
}

// cell_from_html._Context.raw に対応する元の文字。
func (c *context) raw(e *htmltok.Node) string { return c.source[e.Start:e.End] }

// cell_from_html._Context.placeholder に対応する強調の仮の印。
func (c *context) placeholder(inner, marker, open, close string, depth int) string {
	index := len(c.emphasis)
	opening, closing := rune(0xe000+2*index), rune(0xe000+2*index+1)
	c.emphasis = append(c.emphasis, emphasisMark{opening, closing, marker, open, close, depth})
	stripped := strings.Trim(inner, " ")
	if stripped == "" {
		return inner
	}
	lead := inner[:len(inner)-len(strings.TrimLeft(inner, " "))]
	trail := inner[len(strings.TrimRight(inner, " ")):]
	return lead + string(opening) + stripped + string(closing) + trail
}

// cell_from_html._is_placeholder に対応する仮の印の判定。
func isPlaceholder(ch rune) bool { return ch >= 0xe000 && ch < 0xf800 }

// cell_from_html._resolve_emphasis に対応する強調の解決。
func resolveEmphasis(s string, c *context) string {
	order := make([]int, len(c.emphasis))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return c.emphasis[order[i]].Depth > c.emphasis[order[j]].Depth })
	visible := func(ch rune) rune {
		if isPlaceholder(ch) {
			return '*'
		}
		return ch
	}
	for _, index := range order {
		m := c.emphasis[index]
		r := []rune(s)
		i, j := -1, -1
		for k, ch := range r {
			if ch == m.Opening && i < 0 {
				i = k
			}
			if ch == m.Closing && j < 0 {
				j = k
			}
		}
		if i < 0 || j < 0 {
			continue
		}
		inner := r[i+1 : j]
		before, after := rune(0), rune(0)
		if i > 0 {
			before = r[i-1]
		}
		if j+1 < len(r) {
			after = r[j+1]
		}
		check := make([]rune, len(inner))
		for k, ch := range inner {
			check[k] = visible(ch)
		}
		if inline.DelimitersWork(visible(before), string(check), visible(after), m.Marker) {
			s = string(r[:i]) + m.Marker + string(inner) + m.Marker + string(r[j+1:])
		} else {
			s = string(r[:i]) + m.OpenText + string(inner) + m.CloseText + string(r[j+1:])
		}
	}
	return s
}

type flags struct{ tilde, dollar bool }

// cell_from_html._paragraph_flags に対応する段落の特殊記号。
func paragraphFlags(nodes []*htmltok.Node) flags {
	var plain strings.Builder
	var collect func(*htmltok.Node)
	collect = func(n *htmltok.Node) {
		if n.Kind == "text" {
			plain.WriteString(pycompat.HTMLUnescape(ws.ReplaceAllString(n.Text, " ")))
		} else if n.Kind == "element" && n.Tag != "code" && n.Tag != "pre" {
			for _, c := range n.Children {
				collect(c)
			}
		}
	}
	for _, n := range nodes {
		collect(n)
	}
	s := plain.String()
	f := flags{tilde: strings.Count(s, "~") >= 2}
	for _, t := range inline.Tokenize(s, true) {
		if t.Kind == "math" {
			f.dollar = true
			break
		}
	}
	return f
}

// cell_from_html.escape_text に対応する HTML の文字のエスケープ。
func EscapeText(s string, tilde, dollar bool, links ...[2]int) string {
	var b strings.Builder
	r := []rune(s)
	matchIndex, preservedEnd := 0, 0
	for i, ch := range r {
		if matchIndex < len(links) && i == links[matchIndex][0] {
			end := links[matchIndex][1]
			b.WriteString(string(r[i:end]))
			preservedEnd = end
			matchIndex++
		}
		if i < preservedEnd {
			continue
		}
		prev, next := rune(0), rune(0)
		if i > 0 {
			prev = r[i-1]
		}
		if i+1 < len(r) {
			next = r[i+1]
		}
		switch {
		case ch == '\\':
			if next == 0 || strings.ContainsRune(inline.ASCIIPunctuation, next) {
				b.WriteString("\\\\")
			} else {
				b.WriteByte('\\')
			}
		case strings.ContainsRune("`*[", ch):
			b.WriteByte('\\')
			b.WriteRune(ch)
		case ch == '_':
			inside := prev != 0 && next != 0 && !inline.IsUnicodeWhitespace(prev) && !inline.IsUnicodeWhitespace(next) && !inline.IsPunctuation(prev, true) && !inline.IsPunctuation(next, true)
			if !inside {
				b.WriteByte('\\')
			}
			b.WriteRune(ch)
		case ch == '~' && tilde:
			b.WriteString("\\~")
		case ch == '$' && dollar:
			b.WriteString("\\$")
		case ch == '<':
			if next != 0 && (pycompat.IsAlpha(string(next)) || strings.ContainsRune("/!?", next)) {
				b.WriteByte('\\')
			}
			b.WriteRune(ch)
		case ch == '&':
			if entityLike.MatchString(string(r[i:])) {
				b.WriteString("&amp;")
			} else {
				b.WriteByte('&')
			}
		case ch == '\u00a0':
			b.WriteString("&nbsp;")
		default:
			b.WriteRune(ch)
		}
	}
	return b.String()
}

var backticks = regexp.MustCompile("`+")

// cell_from_html._code_span に対応するコードスパン。
func codeSpan(s string) (string, bool) {
	s = strings.ReplaceAll(s, "\n", " ")
	if strings.Trim(s, " ") == "" {
		return "", false
	}
	longest := 0
	for _, m := range backticks.FindAllString(s, -1) {
		longest = max(longest, len(m))
	}
	fence := strings.Repeat("`", longest+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") || strings.HasPrefix(s, " ") && strings.HasSuffix(s, " ") {
		s = " " + s + " "
	}
	return fence + s + fence, true
}

// cell_from_html._text_of に対応する文字の抽出。
func textOf(e *htmltok.Node) string {
	var b strings.Builder
	for _, c := range e.Children {
		if c.Kind == "text" {
			b.WriteString(c.Text)
		} else if c.Kind == "element" {
			if c.Tag == "br" {
				b.WriteByte('\n')
			} else {
				b.WriteString(textOf(c))
			}
		}
	}
	return b.String()
}

// cell_from_html._only_text に対応する文字だけの判定。
func onlyText(e *htmltok.Node) bool {
	for _, c := range e.Children {
		if c.Kind != "text" {
			return false
		}
	}
	return true
}

// cell_from_html._merge_adjacent に対応する隣の強調の結合。
func mergeAdjacent(nodes []*htmltok.Node) []*htmltok.Node {
	var result []*htmltok.Node
	for _, n := range nodes {
		if n.Kind == "element" && emphasis[n.Tag] != "" && len(n.Attributes) == 0 && len(result) > 0 {
			last := result[len(result)-1]
			if last.Kind == "element" && last.Tag == n.Tag && len(last.Attributes) == 0 {
				merged := &htmltok.Node{Kind: "element", Tag: n.Tag, StartText: last.StartText, Start: last.Start, End: n.End, Closed: n.Closed}
				merged.Children = append(append([]*htmltok.Node{}, last.Children...), n.Children...)
				result[len(result)-1] = merged
				continue
			}
		}
		result = append(result, n)
	}
	return result
}

// cell_from_html._link_destination に対応するリンク先。
func linkDestination(href string) string {
	dest := strings.NewReplacer("<", "%3C", ">", "%3E", "\n", "%0A").Replace(href)
	var b strings.Builder
	r := []rune(dest)
	for i, ch := range r {
		b.WriteRune(ch)
		if ch == '\\' && i+1 < len(r) && strings.ContainsRune(inline.ASCIIPunctuation, r[i+1]) {
			b.WriteByte('\\')
		}
	}
	dest = b.String()
	if strings.Contains(dest, " ") || strings.Count(dest, "(") != strings.Count(dest, ")") || dest == "" {
		return "<" + dest + ">"
	}
	return dest
}

// cell_from_html._render_text に対応する空白をまとめた文字とリンクの保持。
func renderText(source string, f flags, spacedLinks bool) string {
	text := pycompat.HTMLUnescape(ws.ReplaceAllString(source, " "))
	var links [][2]int
	if spacedLinks {
		matchText := pycompat.HTMLUnescape(ws.ReplaceAllStringFunc(source, func(s string) string {
			if strings.Contains(s, "\n") {
				return "\n"
			}
			return " "
		}))
		for _, match := range spacedLink.FindAllStringIndex(matchText, -1) {
			links = append(links, [2]int{pycompat.Len(matchText[:match[0]]), pycompat.Len(matchText[:match[1]])})
		}
	}
	return EscapeText(text, f.tilde, f.dollar, links...)
}

// cell_from_html._render_inline に対応するインラインの描画。
func renderInline(nodes []*htmltok.Node, c *context, f flags, depth int, spacedLinks bool) string {
	var b strings.Builder
	for _, n := range mergeAdjacent(nodes) {
		switch n.Kind {
		case "text":
			b.WriteString(renderText(n.Text, f, spacedLinks))
		case "raw", "md":
			b.WriteString(n.Text)
		case "element":
			b.WriteString(renderInlineElement(n, c, f, depth, spacedLinks))
		}
	}
	return b.String()
}

// cell_from_html._render_inline_element の attribute(...) or "" に対応する参照。
func attrString(e *htmltok.Node, name string) string {
	if p := e.Attribute(name); p != nil {
		return *p
	}
	return ""
}

// cell_from_html._render_inline_element などの属性名の部分集合の判定。
func namesWithin(e *htmltok.Node, allowed string) bool {
	names := wordSet(allowed)
	for _, a := range e.Attributes {
		if !names[a.Name] {
			return false
		}
	}
	return true
}

var uri = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]{1,31}:[^<>` + pycompat.ReSpace + `]*(?:\n)?$`)

// cell_from_html._render_inline_element に対応する要素の描画。
func renderInlineElement(e *htmltok.Node, c *context, f flags, depth int, spacedLinks bool) string {
	tag := e.Tag
	if tag == "br" {
		return "\n"
	}
	if marker := emphasis[tag]; marker != "" && len(e.Attributes) == 0 {
		inner := renderInline(e.Children, c, f, depth+1, spacedLinks)
		if strings.Trim(inner, " ") == "" {
			return inner
		}
		return c.placeholder(inner, marker, e.StartText, "</"+tag+">", depth)
	}
	if tag == "code" {
		if len(e.Attributes) == 0 && onlyText(e) {
			if span, ok := codeSpan(pycompat.HTMLUnescape(textOf(e))); ok {
				return span
			}
		}
		return c.raw(e)
	}
	if tag == "a" {
		href := e.Attribute("href")
		if href != nil && namesWithin(e, "href title") {
			text := renderInline(e.Children, c, f, depth+1, false)
			title := e.Attribute("title")
			if text == EscapeText(*href, false, false) && title == nil && uri.MatchString(*href) {
				return "<" + *href + ">"
			}
			text = strings.ReplaceAll(text, "]", "\\]")
			dest := linkDestination(*href)
			if title != nil {
				return "[" + text + "](" + dest + " \"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(*title) + "\")"
			}
			return "[" + text + "](" + dest + ")"
		}
		end := ""
		if e.Closed {
			end = "</a>"
		}
		return e.StartText + renderInline(e.Children, c, f, depth+1, false) + end
	}
	if tag == "img" {
		src := e.Attribute("src")
		if src != nil && namesWithin(e, "src alt title") {
			alt := strings.NewReplacer("[", "\\[", "]", "\\]").Replace(attrString(e, "alt"))
			title := e.Attribute("title")
			if title != nil {
				return "![" + alt + "](" + linkDestination(*src) + " \"" + strings.ReplaceAll(*title, "\"", "\\\"") + "\")"
			}
			return "![" + alt + "](" + linkDestination(*src) + ")"
		}
		return e.StartText
	}
	if tag == "input" {
		return e.StartText
	}
	if BlockTags[tag] {
		return c.raw(e)
	}
	if tag == "wbr" {
		return e.StartText
	}
	inner := renderInline(e.Children, c, f, depth+1, spacedLinks && tag != "kbd" && tag != "script" && tag != "style")
	end := ""
	if e.Closed {
		end = "</" + tag + ">"
	}
	return e.StartText + inner + end
}

// cell_from_html._finish_paragraph に対応する段落の仕上げ。
func finishParagraph(s string, c *context) []string {
	s = resolveEmphasis(s, c)
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.Trim(lines[i], " ")
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	escape := false
	for _, line := range lines {
		if blocks.IsDelimiterRow(line) && strings.Contains(line, "|") {
			escape = true
			break
		}
	}
	var out []string
	for _, line := range lines {
		if escape {
			line = strings.ReplaceAll(line, "|", "\\|")
		}
		if line != "" && !strings.HasPrefix(line, "`") {
			line = inline.EscapeLineStart(line)
		}
		out = append(out, line)
	}
	return out
}

type block struct {
	kind       string
	lines      []string
	ordered    bool
	marker     string
	interrupts bool
}

// cell_from_html._render_flow に対応するブロックの並び。
func renderFlow(nodes []*htmltok.Node, c *context) []block {
	var result []block
	var group []*htmltok.Node
	flush := func() {
		if len(group) == 0 {
			return
		}
		f := paragraphFlags(group)
		text := renderInline(group, c, f, 0, true)
		lines := finishParagraph(text, c)
		if len(lines) > 0 {
			result = append(result, block{kind: "para", lines: lines})
		}
		group = nil
	}
	for _, n := range nodes {
		if n.Kind == "md" {
			flush()
			result = append(result, block{kind: "md", lines: strings.Split(cellsyntax.ChunkToExcel(n.Text), "\n")})
		} else if n.Kind == "element" && BlockTags[n.Tag] {
			flush()
			var previous *block
			if len(result) > 0 {
				previous = &result[len(result)-1]
			}
			result = append(result, renderBlock(n, c, previous)...)
		} else {
			group = append(group, n)
		}
	}
	flush()
	return result
}

// cell_from_html._join に対応するブロックの行の連結。
func join(bs []block) []string {
	var lines []string
	var previous *block
	for i := range bs {
		b := &bs[i]
		if previous != nil {
			if previous.kind == "para" && b.kind == "list" && b.interrupts {
			} else if previous.kind == "list" && b.kind == "list" {
			} else {
				lines = append(lines, "")
			}
		}
		lines = append(lines, b.lines...)
		previous = b
	}
	return lines
}

// cell_from_html._raw_block に対応する元の HTML。
func rawBlock(e *htmltok.Node, c *context) []block {
	return []block{{kind: "raw", lines: strings.Split(c.raw(e), "\n")}}
}

// cell_from_html._list_items に対応するリスト項目の抽出。
func listItems(e *htmltok.Node) []*htmltok.Node {
	var items []*htmltok.Node
	for _, ch := range e.Children {
		if ch.Kind == "text" && strings.Trim(ch.Text, " \t\n\r\f") == "" || ch.Kind == "raw" {
			continue
		}
		if ch.Kind != "element" || ch.Tag != "li" {
			return nil
		}
		items = append(items, ch)
	}
	return items
}

var startNumber = regexp.MustCompile(`^[` + pycompat.ReSpace + `]*([` + pycompat.ReDigit + `]{1,9})`)

// cell_from_html._render_list に対応するリストの変換。
func renderList(e *htmltok.Node, c *context, previous *block) []block {
	ordered := e.Tag == "ol"
	if e.Attribute("type") != nil || e.Attribute("reversed") != nil || !namesWithin(e, "start") {
		return rawBlock(e, c)
	}
	items := listItems(e)
	if len(items) == 0 {
		return rawBlock(e, c)
	}
	start := 1
	if ordered && e.Attribute("start") != nil {
		m := startNumber.FindStringSubmatch(*e.Attribute("start"))
		if m == nil {
			return rawBlock(e, c)
		}
		start, _ = pycompat.Atoi(m[1])
	}
	marker, other := "-", "*"
	if ordered {
		marker, other = ".", ")"
	}
	if previous != nil && previous.kind == "list" && previous.ordered == ordered && previous.marker == marker {
		marker = other
	}
	loose := false
	for _, item := range items {
		for _, ch := range item.Children {
			if ch.Kind == "element" && ch.Tag == "p" {
				loose = true
			}
		}
	}
	var lines []string
	for i, item := range items {
		if len(item.Attributes) > 0 {
			return rawBlock(e, c)
		}
		children := append([]*htmltok.Node{}, item.Children...)
		task := ""
		for len(children) > 0 && children[0].Kind == "text" && strings.Trim(children[0].Text, " \t\n\r\f") == "" {
			children = children[1:]
		}
		if len(children) > 0 && children[0].Kind == "element" && children[0].Tag == "input" && pycompat.Lower(attrString(children[0], "type")) == "checkbox" {
			task = "[ ] "
			if children[0].Attribute("checked") != nil {
				task = "[x] "
			}
			children = children[1:]
		}
		body := join(renderFlow(children, c))
		label := marker + " "
		if ordered {
			label = fmt.Sprintf("%d%s ", start+i, marker)
		}
		if i > 0 && loose {
			lines = append(lines, "")
		}
		first := task
		if len(body) > 0 {
			first += body[0]
		}
		lines = append(lines, strings.TrimRight(label+first, " "))
		if len(body) > 1 {
			for _, line := range body[1:] {
				if line != "" {
					line = strings.Repeat(" ", pycompat.Len(label)) + line
				}
				lines = append(lines, line)
			}
		}
	}
	interrupts := (!ordered || start == 1) && len(lines) > 0 && strings.Trim(lines[0], " -*.)0123456789") != ""
	return []block{{kind: "list", lines: lines, ordered: ordered, marker: marker, interrupts: interrupts}}
}

// cell_from_html._render_dl に対応する説明リストの変換。
func renderDL(e *htmltok.Node, c *context) []block {
	var entries []*htmltok.Node
	for _, ch := range e.Children {
		if ch.Kind == "text" && strings.Trim(ch.Text, " \t\n\r\f") == "" || ch.Kind == "raw" {
			continue
		}
		if ch.Kind != "element" || (ch.Tag != "dt" && ch.Tag != "dd") || len(ch.Attributes) > 0 {
			return rawBlock(e, c)
		}
		entries = append(entries, ch)
	}
	if len(entries) == 0 || entries[0].Tag != "dt" || entries[len(entries)-1].Tag != "dd" || len(e.Attributes) > 0 {
		return rawBlock(e, c)
	}
	var lines []string
	previous := ""
	for _, entry := range entries {
		rendered := renderFlow(entry.Children, c)
		if entry.Tag == "dt" {
			if previous == "dt" || len(rendered) != 1 || rendered[0].kind != "para" {
				return rawBlock(e, c)
			}
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, rendered[0].lines...)
		} else {
			body := join(rendered)
			if len(body) == 0 {
				body = []string{""}
			}
			first := ":"
			if body[0] != "" {
				first = ": " + body[0]
			}
			lines = append(lines, first)
			for _, line := range body[1:] {
				if line != "" {
					line = "  " + line
				}
				lines = append(lines, line)
			}
		}
		previous = entry.Tag
	}
	return []block{{kind: "dl", lines: lines}}
}

var fenceRuns = regexp.MustCompile("(?m)^[ \\t]*(`{3,})")

// cell_from_html._render_pre に対応するコードブロックの変換。
func renderPre(e *htmltok.Node, c *context) []block {
	if !namesWithin(e, "lang") {
		return rawBlock(e, c)
	}
	language := attrString(e, "lang")
	contentElement := e
	var meaningful []*htmltok.Node
	for _, ch := range e.Children {
		if ch.Kind != "text" || strings.Trim(ch.Text, " \t\n\r\f") != "" {
			meaningful = append(meaningful, ch)
		}
	}
	if len(meaningful) == 1 && meaningful[0].Kind == "element" && meaningful[0].Tag == "code" {
		code := meaningful[0]
		if !onlyText(code) || !namesWithin(code, "class") {
			return rawBlock(e, c)
		}
		m := languageClass.FindStringSubmatch(attrString(code, "class"))
		if m != nil {
			language = m[1]
		} else if attrString(code, "class") != "" {
			return rawBlock(e, c)
		}
		contentElement = code
		for _, ch := range e.Children {
			if ch != code && ch.Kind == "text" && strings.Trim(ch.Text, " \t\n\r\f") != "" {
				return rawBlock(e, c)
			}
		}
	} else if !onlyText(e) {
		return rawBlock(e, c)
	}
	content := pycompat.HTMLUnescape(textOf(contentElement))
	if contentElement == e && strings.HasPrefix(content, "\n") {
		content = content[1:]
	}
	content = strings.TrimSuffix(content, "\n")
	longest := 0
	for _, m := range fenceRuns.FindAllStringSubmatch(content, -1) {
		longest = max(longest, len(m[1]))
	}
	fence := strings.Repeat("`", max(3, longest+1))
	lines := []string{fence + language}
	lines = append(lines, strings.Split(content, "\n")...)
	lines = append(lines, fence)
	return []block{{kind: "code", lines: lines}}
}

var trailingHashes = regexp.MustCompile(`(#+)$`)

// cell_from_html._render_block に対応するブロックの描画。
func renderBlock(e *htmltok.Node, c *context, previous *block) []block {
	tag := e.Tag
	if tag == "p" || tag == "div" {
		if len(e.Attributes) > 0 {
			return rawBlock(e, c)
		}
		return renderFlow(e.Children, c)
	}
	if tag == "ul" || tag == "ol" {
		return renderList(e, c, previous)
	}
	if tag == "dl" {
		return renderDL(e, c)
	}
	if tag == "pre" {
		return renderPre(e, c)
	}
	if tag == "blockquote" && len(e.Attributes) == 0 {
		body := join(renderFlow(e.Children, c))
		for i, line := range body {
			if line == "" {
				body[i] = ">"
			} else {
				body[i] = "> " + line
			}
		}
		return []block{{kind: "quote", lines: body}}
	}
	if len(tag) == 2 && tag[0] == 'h' && tag[1] >= '1' && tag[1] <= '6' && len(e.Attributes) == 0 {
		rendered := renderFlow(e.Children, c)
		if len(rendered) != 1 || rendered[0].kind != "para" || len(rendered[0].lines) != 1 {
			return rawBlock(e, c)
		}
		text := trailingHashes.ReplaceAllStringFunc(rendered[0].lines[0], func(s string) string { return "\\" + s })
		return []block{{kind: "heading", lines: []string{strings.Repeat("#", int(tag[1]-'0')) + " " + text}}}
	}
	if tag == "hr" && len(e.Attributes) == 0 {
		return []block{{kind: "hr", lines: []string{"---"}}}
	}
	return rawBlock(e, c)
}

// cell_from_html.convert に対応するセル全体の変換。
func Convert(nodes []*htmltok.Node, source string) string {
	c := &context{source: source}
	return cellsyntax.NormalizeListSpacing(model.NormalizeText(strings.Join(join(renderFlow(nodes, c)), "\n")))
}
