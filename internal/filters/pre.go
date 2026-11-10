// Package filters は、GitLab のフィルターを消毒の前後に分けて適用する。
package filters

import (
	"context"
	"encoding/base64"
	"regexp"
	"strings"

	t "github.com/ryo-aoki-pc/md-preview/internal/htmltree"
	"github.com/ryo-aoki-pc/md-preview/internal/plantumlsource"
	"golang.org/x/net/html"
)

type Diagram struct{ ID, Source string }
type State struct {
	Plantuml bool
	Diagrams []Diagram
	Markdown func(context.Context, string, map[string]any) (string, error)
}
type Filter func(context.Context, *html.Node, *State) error

func Before(ctx context.Context, root *html.Node, state *State) error {
	for _, f := range []Filter{CodeLanguage, JSONTable, PlantUML, SpacedLink} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := f(ctx, root, state); err != nil {
			return err
		}
	}
	return nil
}
func After(ctx context.Context, root *html.Node, state *State) error {
	for _, f := range []Filter{Attributes, Math, Color, Mermaid, Video, Audio, TableOfContents, Footnote, InlineDiff, Image, External, TaskList, SetDirection, SyntaxHighlight} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := f(ctx, root, state); err != nil {
			return err
		}
	}
	return nil
}
func tagged(ctx context.Context, root *html.Node, tag string) ([]*html.Node, error) {
	return t.Elements(ctx, root, func(n *html.Node) bool { return n.Data == tag })
}
func firstAttr(nodes []*html.Node, keys []string) (string, bool) {
	for i, n := range nodes {
		if t.Has(n, keys[i]) {
			return t.Get(n, keys[i]), true
		}
	}
	return "", false
}
func CodeLanguage(ctx context.Context, root *html.Node, _ *State) error {
	nodes, err := tagged(ctx, root, "pre")
	if err != nil {
		return err
	}
	for _, pre := range nodes {
		codes, err := tagged(ctx, pre, "code")
		if err != nil {
			return err
		}
		if t.Has(pre, "lang") && len(codes) == 0 {
			t.SetChildren(pre, t.Element("code", nil, t.Children(pre)...))
		}
		code := t.OnlyCode(pre)
		if code == nil {
			continue
		}
		language, found := firstAttr([]*html.Node{pre, code}, []string{"data-lang", "data-lang"})
		if !found {
			for _, c := range t.Classes(code) {
				if strings.HasPrefix(c, "language-") {
					language = c[9:]
					found = true
					break
				}
			}
		}
		if !found {
			language, _ = firstAttr([]*html.Node{pre, code}, []string{"lang", "lang"})
		}
		meta, _ := firstAttr([]*html.Node{pre, code}, []string{"data-meta", "data-meta"})
		lang, params := language, ""
		if i := strings.IndexByte(language, ':'); i >= 0 {
			lang = language[:i]
			params = t.Trim(language[i+1:])
		}
		if params == "" && t.Trim(meta) != "" {
			params = meta
		}
		for _, key := range []string{"lang", "data-lang", "data-meta"} {
			t.Del(code, key)
		}
		t.Del(pre, "lang")
		t.Del(pre, "data-meta")
		remaining := []string{}
		removed := false
		for _, c := range t.Classes(code) {
			if strings.HasPrefix(c, "language-") {
				removed = true
			} else {
				remaining = append(remaining, c)
			}
		}
		if removed {
			if len(remaining) > 0 {
				t.Set(code, "class", strings.Join(remaining, " "))
			} else {
				t.Del(code, "class")
			}
		}
		if t.Trim(lang) != "" {
			t.Set(pre, "data-lang", lang)
			t.Set(pre, "data-canonical-lang", lang)
			if params != "" {
				t.Set(pre, "data-lang-params", params)
			}
		}
	}
	return nil
}
func markdown(ctx context.Context, state *State, value any, opts map[string]any) ([]*html.Node, error) {
	s, err := state.Markdown(ctx, strings.ReplaceAll(t.RubyStringContext(ctx, value), "\r", ""), opts)
	if err != nil {
		return nil, err
	}
	tree, err := t.Fragment(ctx, strings.TrimRightFunc(s, t.Space))
	if err != nil {
		return nil, err
	}
	return t.Children(tree), nil
}
func JSONTable(ctx context.Context, root *html.Node, state *State) error {
	nodes, err := t.Elements(ctx, root, func(n *html.Node) bool {
		return t.Get(n, "data-canonical-lang") == "json" && contains(t.Fields(t.Get(n, "data-lang-params")), "table")
	})
	if err != nil {
		return err
	}
	opts := map[string]any{"sourcepos": false, "gemojis": true}
	for _, pre := range nodes {
		code := t.OnlyCode(pre)
		if code == nil {
			continue
		}
		raw, err := t.ParseJSONContext(ctx, t.ContentContext(ctx, code))
		if err != nil {
			continue
		}
		data, ok := raw.(t.Object)
		if !ok || !t.RubyTruthy(data.Get("markdown")) {
			continue
		}
		items, ok := data.Get("items").([]any)
		if !ok || len(items) == 0 {
			continue
		}
		valid := true
		for _, item := range items {
			if _, ok := item.(t.Object); !ok {
				valid = false
			}
		}
		if !valid {
			continue
		}
		var fields []any
		if t.RubyTruthy(data.Get("fields")) {
			fields, ok = data.Get("fields").([]any)
			if !ok {
				continue
			}
			for _, f := range fields {
				if _, ok := f.(t.Object); !ok {
					valid = false
				}
			}
			if !valid {
				continue
			}
		} else {
			fields = []any{}
			for _, p := range items[0].(t.Object) {
				fields = append(fields, t.Object{{Key: "key", Value: p.Key}})
			}
		}
		table := t.Element("table", t.Attr("data-table-fields", t.JSON(fields), "data-table-markdown", "true"))
		if t.RubyTruthy(data.Get("filter")) {
			t.Set(table, "data-table-filter", "true")
		}
		caption := data.Get("caption")
		present := t.RubyTruthy(caption)
		switch v := caption.(type) {
		case string:
			present = t.Trim(v) != ""
		case []any:
			present = len(v) > 0
		case t.Object:
			present = len(v) > 0
		}
		if present {
			children, err := markdown(ctx, state, caption, opts)
			if err != nil {
				return err
			}
			t.Append(table, t.Element("caption", nil, children...))
		}
		row := t.Element("tr", nil)
		for _, raw := range fields {
			f := raw.(t.Object)
			label := f.Get("label")
			if !t.RubyTruthy(label) {
				label = f.Get("key")
			}
			t.Append(row, t.Element("th", nil, t.Text(t.RubyStringContext(ctx, label))))
		}
		t.Append(table, t.Element("thead", nil, row))
		body := t.Element("tbody", nil)
		for _, raw := range items {
			if err := ctx.Err(); err != nil {
				return err
			}
			item := raw.(t.Object)
			row := t.Element("tr", nil)
			for _, raw := range fields {
				field := raw.(t.Object)
				key := t.JSStringContext(ctx, field.Get("key"))
				if !field.Has("key") {
					key = "undefined"
				}
				value := item.Get(key)
				children, err := markdown(ctx, state, value, opts)
				if err != nil {
					return err
				}
				if len(children) == 1 && children[0].Type == html.ElementNode && children[0].Data == "p" {
					children = t.Children(children[0])
				}
				t.Append(row, t.Element("td", nil, children...))
			}
			t.Append(body, row)
		}
		t.Append(table, body)
		t.Replace(pre, t.Element("div", nil, table))
	}
	return nil
}
func contains(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}
func PlantUML(ctx context.Context, root *html.Node, state *State) error {
	if !state.Plantuml {
		return nil
	}
	nodes, err := tagged(ctx, root, "pre")
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, d := range state.Diagrams {
		seen[d.ID] = true
	}
	for _, pre := range nodes {
		if err := ctx.Err(); err != nil {
			return err
		}
		var code *html.Node
		for c := pre.FirstChild; c != nil; c = c.NextSibling {
			if c.Data == "code" && (t.Get(pre, "data-canonical-lang") == "plantuml" || t.Get(c, "data-canonical-lang") == "plantuml") {
				code = c
				break
			}
		}
		if code == nil {
			continue
		}
		source := t.ContentContext(ctx, code)
		if strings.HasSuffix(source, "\r\n") {
			source = source[:len(source)-2]
		} else if strings.HasSuffix(source, "\n") || strings.HasSuffix(source, "\r") {
			source = source[:len(source)-1]
		}
		if len(source) > 256*1024 {
			continue
		}
		id := plantumlsource.ID(source)
		img := t.Element("img", t.Attr("class", "plantuml", "src", "/_/plantuml/"+id+".svg", "data-diagram", "plantuml", "data-diagram-src", "data:text/plain;base64,"+base64.StdEncoding.EncodeToString([]byte(source))))
		if t.Has(pre, "data-sourcepos") {
			t.Set(img, "data-sourcepos", t.Get(pre, "data-sourcepos"))
		}
		t.Replace(pre, img)
		if !seen[id] {
			state.Diagrams = append(state.Diagrams, Diagram{id, source})
			seen[id] = true
		}
	}
	return nil
}

var spacedPattern = regexp.MustCompile(`(!)?\[([^\n]+?)\]\(([^\n]+?)( ".+?")?\)`)

func SpacedLink(ctx context.Context, root *html.Node, state *State) error {
	nodes, err := t.Nodes(ctx, root, func(n *html.Node) bool { return n.Type == html.TextNode && strings.Contains(n.Data, "](") })
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if t.Ancestor(n, func(a *html.Node) bool {
			return contains([]string{"a", "code", "kbd", "pre", "script", "style"}, a.Data) || a.Data == "span" && t.Has(a, "data-math-style")
		}) {
			continue
		}
		children := []*html.Node{}
		last := 0
		for _, m := range spacedPattern.FindAllStringSubmatchIndex(n.Data, -1) {
			if err := ctx.Err(); err != nil {
				return err
			}
			if m[0] > last {
				children = append(children, t.Text(n.Data[last:m[0]]))
			}
			group := func(i int) string {
				if m[i*2] < 0 {
					return ""
				}
				return n.Data[m[i*2]:m[i*2+1]]
			}
			source := group(1) + "[" + group(2) + "](" + strings.ReplaceAll(group(3), " ", "%20") + group(4) + ")"
			rendered, err := markdown(ctx, state, source, map[string]any{"gemojis": true})
			if err != nil {
				return err
			}
			var paragraph *html.Node
			for _, p := range rendered {
				if p.Data == "p" {
					paragraph = p
					break
				}
			}
			if paragraph != nil {
				children = append(children, t.Children(paragraph)...)
			} else {
				children = append(children, rendered...)
			}
			last = m[1]
		}
		if last == 0 {
			continue
		}
		if last < len(n.Data) {
			children = append(children, t.Text(n.Data[last:]))
		}
		t.Replace(n, children...)
	}
	return nil
}
