package highlight

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	t "github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"golang.org/x/net/html"
)

func TestLines(tst *testing.T) {
	for _, c := range []struct {
		in   []Token
		want [][]Token
	}{{[]Token{{"c", "a\nb\n"}}, [][]Token{{{"c", "a"}}, {{"c", "b"}}}}, {[]Token{{"c", "\n\n"}}, [][]Token{{}, {}}}, {[]Token{{"s", ""}}, [][]Token{}}, {[]Token{{"k", "a"}, {"s", "b\nc"}}, [][]Token{{{"k", "a"}, {"s", "b"}}, {{"s", "c"}}}}} {
		if got := TokenLines(c.in); !reflect.DeepEqual(got, c.want) {
			tst.Fatal(got, c.want)
		}
	}
	result, err := Code(context.Background(), "/* 一\n二 */\n", "js")
	if err != nil {
		tst.Fatal(err)
	}
	root := t.Root(result.Children...)
	lines, _ := t.Elements(context.Background(), root, func(n *html.Node) bool { return t.HasClass(n, "line") })
	if len(lines) != 2 {
		tst.Fatal(len(lines))
	}
	comments, _ := t.Elements(context.Background(), root, func(n *html.Node) bool { return t.Get(n, "class") == "cm" })
	if len(comments) != 2 {
		tst.Fatal("複数行のコメント", len(comments))
	}
	for _, n := range comments {
		if strings.Contains(t.Content(n), "\n") {
			tst.Fatal("字句が行をまたぎました。")
		}
	}
}
func TestLanguages(tst *testing.T) {
	for _, c := range [][2]string{{"js", "javascript"}, {"python", "python"}, {"py", "python"}, {"sh", "shell"}, {"text", "plaintext"}, {"unknown-language", "plaintext"}, {"", "plaintext"}, {"html", "html"}, {"HTML", "xml"}, {"toml", "toml"}, {"TOML", "ini"}} {
		r, err := Code(context.Background(), "x\n", c[0])
		if err != nil || r.Language != c[1] || t.Get(r.Children[0], "data-lang") != c[1] {
			tst.Fatal(c, r.Language, err)
		}
	}
	for _, language := range append([]string{"math", "suggestion"}, DiagramLanguages...) {
		r, err := Code(context.Background(), "if \"x\"\n", language)
		if err != nil || r.Language != language {
			tst.Fatal(language, err)
		}
		for c := r.Children[0].FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.TextNode {
				tst.Fatal("字句に分けた図", language)
			}
		}
	}
	for language, source := range map[string]string{"javascript": "const x = 42;", "python": "def f(x): return 1", "ruby": "class X\nend", "diff": "- old\n+ new", "go": "package main", "json": `{"x":true}`, "yaml": "key: value", "html": `<b class="x">x</b>`, "shell": "echo hi"} {
		r, err := Code(context.Background(), source, language)
		if err != nil {
			tst.Fatal(err)
		}
		nodes, _ := t.Elements(context.Background(), t.Root(r.Children...), func(n *html.Node) bool { return n.Data == "span" && !t.HasClass(n, "line") })
		if len(nodes) == 0 {
			tst.Fatal("配色がありません。", language)
		}
	}
}

// 登録名と全別名の見本は、Node.js 版の出力から採取した。
func TestAllLanguageNames(tst *testing.T) {
	raw, err := os.ReadFile("testdata/languages.json")
	if err != nil {
		tst.Fatal(err)
	}
	var rows []struct{ Requested, Language string }
	if err := json.Unmarshal(raw, &rows); err != nil {
		tst.Fatal(err)
	}
	for _, row := range rows {
		language, _ := selectLanguage(row.Requested)
		if language != row.Language {
			tst.Errorf("%q: 見本=%q Go=%q", row.Requested, row.Language, language)
		}
	}
}
func TestEscapesAndUnicode(tst *testing.T) {
	source := `<script onerror="x">& " '</script>`
	r, err := Code(context.Background(), source, "unknown")
	if err != nil {
		tst.Fatal(err)
	}
	root := t.Root(r.Children...)
	if t.Content(root) != source {
		tst.Fatal(t.Content(root))
	}
	s, _ := t.HTML(root)
	if strings.Contains(s, "<script") {
		tst.Fatal(s)
	}
	r, err = Code(context.Background(), "a\u00a0b\u202ec", "text")
	if err != nil {
		tst.Fatal(err)
	}
	root = t.Root(r.Children...)
	if t.Content(root) != "a b\u202ec" {
		tst.Fatal(t.Content(root))
	}
	nodes, _ := t.Elements(context.Background(), root, func(n *html.Node) bool { return t.HasClass(n, "unicode-bidi") })
	if len(nodes) != 1 || t.Get(nodes[0], "title") != "Potentially unwanted character detected: Unicode BiDi Control" {
		tst.Fatal(nodes)
	}
}
func TestCancelAndConcurrent(tst *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Code(ctx, "x", "ruby"); err != context.Canceled {
		tst.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	timer := time.AfterFunc(5*time.Millisecond, cancel)
	defer timer.Stop()
	if _, err := Code(ctx, strings.Repeat("const x = 1;\n", 200000), "javascript"); err != context.Canceled {
		tst.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, language := range []string{"ruby", "python", "diff", "java", "rust"} {
				r, err := Code(context.Background(), "class X\n1 + 2\n", language)
				if err != nil || len(r.Children) == 0 {
					tst.Errorf("同時の配色: %s %v", language, err)
				}
			}
		}()
	}
	wg.Wait()
}

type painted struct {
	r     rune
	class string
}

func paint(n *html.Node, inherited string) []painted {
	if n.Type == html.TextNode {
		out := []painted{}
		for _, r := range n.Data {
			out = append(out, painted{r, inherited})
		}
		return out
	}
	if n.Data == "span" && !t.HasClass(n, "line") {
		inherited = t.Get(n, "class")
	}
	out := []painted{}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		out = append(out, paint(c, inherited)...)
	}
	return out
}
func TestGitLabClasses(tst *testing.T) {
	raw, err := os.ReadFile("../../test/fixtures/code.gitlab.html")
	if err != nil {
		tst.Fatal(err)
	}
	root, err := t.Fragment(context.Background(), string(raw))
	if err != nil {
		tst.Fatal(err)
	}
	for _, language := range []string{"ruby", "python", "diff"} {
		nodes, _ := t.Elements(context.Background(), root, func(n *html.Node) bool { return n.Data == "pre" && t.Get(n, "data-canonical-lang") == language })
		if len(nodes) != 1 {
			tst.Fatal(language)
		}
		code := t.OnlyCode(nodes[0])
		source := t.Content(code)
		r, err := Code(context.Background(), source, language)
		if err != nil {
			tst.Fatal(err)
		}
		actual := paint(t.Root(r.Children...), "")
		expected := paint(code, "")
		if !reflect.DeepEqual(actual, expected) {
			tst.Fatalf("%s: 見本=%v Go=%v", language, expected, actual)
		}
	}
}
