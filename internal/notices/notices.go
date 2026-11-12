// Package notices は、完成した木を変えずに表示の差を知らせる。
package notices

import (
	"context"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ryo-aoki-pc/md-tools/internal/highlight"
	t "github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/include"
	"github.com/ryo-aoki-pc/md-tools/internal/paths"
	"golang.org/x/net/html"
)

type Notice struct {
	Kind string `json:"kind"`
	Line *int   `json:"line"`
	Text string `json:"text"`
}
type Context struct {
	Dir    string
	Exists func(string) bool
}

var diagram = func() map[string]bool {
	m := map[string]bool{}
	for _, v := range highlight.DiagramLanguages {
		if v != "mermaid" {
			m[v] = true
		}
	}
	return m
}()
var blocks = func() map[string]bool {
	m := map[string]bool{}
	for _, s := range strings.Fields("p div section blockquote li td th tr table pre ul ol dl dt dd caption h1 h2 h3 h4 h5 h6") {
		m[s] = true
	}
	return m
}()
var emoji = regexp.MustCompile(`:[A-Za-z_][A-Za-z0-9_+-]*:`)
var schemes = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
var position = regexp.MustCompile(`^(\d+):\d+-(\d+):\d+$`)
var digit = regexp.MustCompile(`\d`)

func beforeWord(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || strings.ContainsRune("_./%+@&-", r)
}
func afterWord(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || strings.ContainsRune("_/@;", r)
}
func excerpt(s string) string {
	count := 0
	for i := range s {
		if count == 80 {
			return s[:i]
		}
		count++
	}
	return s
}
func characterAt(s string, i int) rune {
	if i >= len(s) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return r
}
func characterBefore(s string, i int) rune {
	if i <= 0 {
		return 0
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return r
}
func call(exists func(string) bool, path string) (result bool) {
	defer func() {
		if recover() != nil {
			result = false
		}
	}()
	return exists(path)
}
func Collect(ctx context.Context, root *html.Node, original string, expansion include.Expansion, c Context) ([]Notice, error) {
	sourceLines := strings.Split(original, "\n")
	seen := map[string]bool{}
	kept := []Notice{}
	existence := map[string]bool{}
	add := func(kind string, line int, value string) {
		var p *int
		if line > 0 {
			n := line
			p = &n
		}
		notice := Notice{kind, p, excerpt(value)}
		key := t.JSON([]any{kind, p, notice.Text})
		if seen[key] {
			return
		}
		seen[key] = true
		rank := line
		if rank == 0 {
			rank = int(^uint(0) >> 1)
		}
		at := sort.Search(len(kept), func(i int) bool {
			r := int(^uint(0) >> 1)
			if kept[i].Line != nil {
				r = *kept[i].Line
			}
			return r > rank
		})
		if at < 200 {
			kept = append(kept, Notice{})
			copy(kept[at+1:], kept[at:])
			kept[at] = notice
			if len(kept) > 200 {
				kept = kept[:200]
			}
		}
	}
	addAt := func(kind string, line int, value string, whole bool) {
		mapped := expansion.OriginalLine(line)
		if mapped > len(sourceLines) {
			mapped = 0
		}
		snippet := value
		if mapped > 0 {
			source := sourceLines[mapped-1]
			if whole || expansion.IsIncludedLine(line) || !strings.Contains(source, value) {
				snippet = t.Trim(source)
			}
		}
		add(kind, mapped, snippet)
	}
	for _, item := range expansion.Includes {
		if !item.OK {
			snippet := item.Path
			if item.Line > 0 && item.Line <= len(sourceLines) {
				if s := t.Trim(sourceLines[item.Line-1]); s != "" {
					snippet = s
				}
			}
			add("include", item.Line, snippet)
		}
	}
	textNotices := func(value string, line int, previous rune) error {
		cursor, current := 0, line
		matches, err := referenceMatches(ctx, value)
		if err != nil {
			return err
		}
		for _, m := range matches {
			if err := ctx.Err(); err != nil {
				return err
			}
			if current > 0 {
				current += strings.Count(value[cursor:m[0]], "\n")
			}
			cursor = m[0]
			before := previous
			if m[0] > 0 {
				before = characterBefore(value, m[0])
			}
			after := characterAt(value, m[1])
			if before != 0 && beforeWord(before) || after != 0 && afterWord(after) {
				continue
			}
			match := value[m[0]:m[1]]
			if strings.HasPrefix(match, "$") {
				continue
			}
			if percent := strings.LastIndexByte(match, '%'); percent >= 0 && !strings.HasPrefix(match, "[") {
				name := match[percent+1:]
				if after == '%' || !strings.HasPrefix(name, `"`) && !digit.MatchString(name) {
					continue
				}
			}
			addAt("reference", current, match, false)
		}
		cursor, current = 0, line
		for _, m := range emoji.FindAllStringIndex(value, -1) {
			if err := ctx.Err(); err != nil {
				return err
			}
			if current > 0 {
				current += strings.Count(value[cursor:m[0]], "\n")
			}
			cursor = m[0]
			before := previous
			if m[0] > 0 {
				before = characterBefore(value, m[0])
			}
			after := characterAt(value, m[1])
			if before != 0 && (unicode.IsLetter(before) || unicode.IsNumber(before) || strings.ContainsRune("_:/", before)) || after != 0 && (unicode.IsLetter(after) || unicode.IsNumber(after) || strings.ContainsRune("_:", after)) {
				continue
			}
			addAt("emoji", current, value[m[0]:m[1]], false)
		}
		return nil
	}
	missing := func(n *html.Node, line int) {
		if c.Exists == nil {
			return
		}
		value := ""
		found := false
		for _, k := range []string{"data-canonical-src", "data-src", "src"} {
			if t.Has(n, k) {
				value = t.Get(n, k)
				found = true
				break
			}
		}
		if !found || strings.HasPrefix(value, "/_/plantuml/") || strings.HasPrefix(value, "#") || strings.HasPrefix(value, "//") || schemes.MatchString(value) {
			return
		}
		part := value
		if i := strings.IndexAny(part, "?#"); i >= 0 {
			part = part[:i]
		}
		if part == "" {
			return
		}
		decoded, err := url.PathUnescape(part)
		if err != nil || !utf8.ValidString(decoded) {
			addAt("missing", line, value, true)
			return
		}
		if strings.HasPrefix(decoded, "//") || schemes.MatchString(decoded) {
			return
		}
		resolved := paths.Resolve(decoded, c.Dir)
		exists := false
		if resolved.Safe {
			v, ok := existence[resolved.Path]
			if !ok {
				v = call(c.Exists, resolved.Path)
				existence[resolved.Path] = v
			}
			exists = v
		}
		if !exists {
			addAt("missing", line, value, true)
		}
	}
	type result struct {
		line int
		last rune
	}
	var visit func(*html.Node, int, bool, rune, int) (result, error)
	visit = func(n *html.Node, inherited int, excluded bool, previous rune, depth int) (result, error) {
		if err := ctx.Err(); err != nil {
			return result{}, err
		}
		if depth > 512 {
			return result{}, t.ErrDepth
		}
		if n.Type == html.TextNode {
			if !excluded {
				if err := textNotices(n.Data, inherited, previous); err != nil {
					return result{}, err
				}
			}
			line := inherited
			if line > 0 {
				line += strings.Count(n.Data, "\n")
			}
			last := previous
			if n.Data != "" {
				last = characterBefore(n.Data, len(n.Data))
			}
			return result{line, last}, nil
		}
		m := position.FindStringSubmatch(t.Get(n, "data-sourcepos"))
		start := inherited
		end := 0
		if len(m) > 0 {
			v, err := strconv.ParseInt(m[1], 10, 64)
			if err == nil && v <= 9007199254740991 {
				start = int(v)
			}
			v, _ = strconv.ParseInt(m[2], 10, 64)
			end = int(v)
		}
		cursor, last := start, previous
		if blocks[n.Data] {
			last = 0
		}
		if n.Type == html.ElementNode {
			if n.Data == "pre" {
				language := t.Get(n, "data-canonical-lang")
				first := strings.SplitN(t.ContentContext(ctx, n), "\n", 2)[0]
				if diagram[language] {
					addAt("diagram", start, first, true)
				}
				if language == "suggestion" {
					addAt("suggestion", start, first, true)
				}
			}
			if n.Data == "img" || n.Data == "video" || n.Data == "audio" {
				missing(n, start)
			}
		}
		ignore := excluded || n.Data == "pre" || n.Data == "code" || n.Data == "a" || t.Has(n, "data-escaped-char") || t.Has(n, "data-math-style")
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			r, err := visit(child, cursor, ignore, last, depth+1)
			if err != nil {
				return result{}, err
			}
			if cursor > 0 {
				cursor = r.line
			}
			last = r.last
		}
		if len(m) > 0 {
			cursor = end
		}
		if blocks[n.Data] {
			last = '\n'
		}
		return result{cursor, last}, nil
	}
	if _, err := visit(root, 0, false, 0, 0); err != nil {
		return nil, err
	}
	if len(seen) > 200 {
		kept = kept[:199]
		kept = append(kept, Notice{Kind: "more", Text: strconv.Itoa(len(seen) - 199)})
	}
	return kept, nil
}
