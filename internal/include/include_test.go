package include

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/engine"
	t "github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"golang.org/x/net/html"
)

func directive(file string) string { return "::include{file=" + file + "}" }
func expand(tb testing.TB, source, file string, reader func(string) Read) Expansion {
	tb.Helper()
	e, err := Expand(context.Background(), source, file, reader)
	if err != nil {
		tb.Fatal(err)
	}
	return e
}
func TestExpandedLine(t *testing.T) {
	source := "前\n" + directive("one.md") + "\n間\n" + directive("missing.md") + "\n" + directive("two.md") + "\n後"
	e := expand(t, source, "", func(path string) Read {
		switch path {
		case "one.md":
			return Read{Kind: Text, Text: "一\n二\n"}
		case "two.md":
			return Read{Kind: Text, Text: "三\n四"}
		}
		return Read{Kind: Missing}
	})
	for i, want := range []int{1, 2, 5, 6, 7, 9} {
		orig := i + 1
		if got := e.ExpandedLine(orig); got != want || e.OriginalLine(got) != orig {
			t.Errorf("原文の %d 行: 展開後=%d、期待=%d", orig, got, want)
		}
	}
	for line := 1; line <= 9; line++ {
		orig := e.OriginalLine(line)
		start := e.ExpandedLine(orig)
		if start > line || e.OriginalLine(start) != orig || (!e.IsIncludedLine(line) && start != line) {
			t.Errorf("展開後の %d 行: 原文=%d、範囲の先頭=%d", line, orig, start)
		}
	}
	if e.ExpandedLine(0) != 0 || e.ExpandedLine(-1) != 0 {
		t.Fatal("正でない行を受け入れた")
	}
	for _, body := range []string{"", "一行", "一行\n"} {
		e := expand(t, directive("one.md")+"\n後", "", func(string) Read { return Read{Kind: Text, Text: body} })
		if e.ExpandedLine(1) != 1 || e.ExpandedLine(2) != 2+strings.Count(body, "\n") {
			t.Fatal(body, e)
		}
	}
}
func TestSyntax(t *testing.T) {
	if !IsHTTP(`http://example.com\path`) {
		t.Fatal("WHATWG URL が受け入れる逆斜線付きの HTTP URL")
	}
	for _, source := range []string{"文中 " + directive("x.md"), " " + directive("x.md"), "\t" + directive("x.md"), "> " + directive("x.md"), "- " + directive("x.md"), directive("x.md") + " 後", directive("x.md") + " ", "::include{file=}", directive(strings.Repeat("a", 1025)), "前\u2028" + directive("x.md")} {
		e := expand(t, source, "", func(string) Read { t.Fatal("読んではいけません。"); return Read{} })
		if e.Text != source || len(e.Includes) != 0 {
			t.Errorf("記法外を変更: %q", source)
		}
	}
	for _, end := range []string{"\n", "\r\n", "", "\v", "\f", "\u0085", "\u2028", "\u2029"} {
		e := expand(t, directive("x.md")+end, "", func(string) Read { return Read{Kind: Text, Text: "本文"} })
		if e.Text != "本文"+end || !e.Includes[0].OK {
			t.Errorf("境界 %q: %#v", end, e)
		}
	}
	for _, name := range []string{strings.Repeat("a", 1024), strings.Repeat("😀", 1024), "a}b.md"} {
		e := expand(t, directive(name), "", func(path string) Read {
			if path != name {
				t.Fatal(path)
			}
			return Read{Kind: Text, Text: "本文"}
		})
		if e.Text != "本文" {
			t.Fatal(e.Text)
		}
	}
}
func TestLimitsAndCache(t *testing.T) {
	calls := 0
	source := strings.TrimSuffix(strings.Repeat(directive("a.md")+"\n", 33), "\n")
	e := expand(t, source, "", func(string) Read { calls++; return Read{Kind: Missing} })
	if calls != 1 || len(e.Includes) != 33 || !strings.HasSuffix(e.Text, directive("a.md")) {
		t.Fatal(calls, len(e.Includes))
	}
	expand(t, directive("a.md"), "", func(string) Read { calls++; return Read{Kind: Text} })
	if calls != 2 {
		t.Fatal(calls)
	}
	paths := []string{}
	expand(t, directive("x.md")+"\n"+directive("./x.md"), "", func(p string) Read { paths = append(paths, p); return Read{Kind: Text} })
	if !reflect.DeepEqual(paths, []string{"x.md", "x.md"}) {
		t.Fatal(paths)
	}
	e = expand(t, strings.Repeat(directive("https://example.com/a")+"\n", 33), "", func(string) Read { t.Fatal("URL を読んではいけません。"); return Read{} })
	if !strings.HasSuffix(e.Text, directive("https://example.com/a")+"\n") {
		t.Fatal("URL の上限")
	}
	source = "~~~text\n" + directive("a.md") + "\n~~~"
	e = expand(t, source, "", func(string) Read { return Read{Kind: Text, Text: directive("b.md")} })
	if len(e.Includes) != 1 || !strings.Contains(e.Text, directive("b.md")) {
		t.Fatal(e.Text)
	}
	e = expand(t, directive("a.md"), "", nil)
	if e.Text != directive("a.md") || e.Includes[0].OK {
		t.Fatal(e)
	}
}
func TestPathsAndFailures(t *testing.T) {
	for _, c := range [][3]string{{"doc/api/README.adoc", "users.adoc", "doc/api/users.adoc"}, {"doc/api", "users.adoc", "doc/api/users.adoc"}, {"doc/api/README.adoc", "../update/x.adoc", "doc/update/x.adoc"}, {"doc/api/README.adoc", "./a/../b.md", "doc/api/b.md"}, {"doc/api/README.adoc", "/shared/x.md", "shared/x.md"}, {"doc/api/README.adoc", "/../../x.md", "x.md"}, {"", "x.md", "x.md"}, {".hidden", "x.md", ".hidden/x.md"}, {"docs/main.", "x.md", "docs/x.md"}} {
		e := expand(t, directive(c[1]), c[0], func(p string) Read {
			if p != c[2] {
				t.Fatal(p, c)
			}
			return Read{Kind: Text, Text: "本文"}
		})
		if e.Includes[0].Path != c[2] || !e.Includes[0].OK {
			t.Fatal(e)
		}
	}
	for _, name := range []string{"../secret.md", "../../secret.md", "a/../../secret.md", "..\\secret.md", "C:/secret.md", "./C:/secret.md", "/C:/secret.md", "a/../C:/secret.md"} {
		e := expand(t, directive(name), "", func(string) Read { t.Fatal("外を読んではいけません。"); return Read{} })
		if e.Includes[0].OK || !strings.Contains(e.Text, "not found") {
			t.Fatal(e)
		}
	}
	for _, c := range []struct {
		read   Read
		reason string
	}{{Read{Kind: Missing}, "not found"}, {Read{Kind: NotReadable}, "not readable"}, {Read{Kind: TooLarge}, "too large"}, {Read{Kind: Invalid}, "not readable"}} {
		e := expand(t, directive("a.md"), "", func(string) Read { return c.read })
		if !strings.Contains(e.Text, c.reason) || e.Includes[0].OK {
			t.Fatal(e)
		}
	}
	e := expand(t, directive("a.md"), "", func(string) Read { panic("失敗") })
	if !strings.Contains(e.Text, "not readable") {
		t.Fatal(e)
	}
}
func TestBytesAndLines(t *testing.T) {
	for _, c := range []struct {
		text  string
		lines int
	}{{"", 0}, {"一", 1}, {"一\n", 1}, {"一\n二", 2}, {"一\n\n", 2}} {
		e := expand(t, directive("a.md")+"\n後", "", func(string) Read { return Read{Kind: Text, Text: c.text} })
		if e.Text != c.text+"\n後" || e.Includes[0].Lines != c.lines || !e.Includes[0].OK {
			t.Fatal(e)
		}
	}
	e := expand(t, directive("a.md"), "", func(string) Read { return Read{Kind: Text, Text: strings.Repeat("x", MaxExpandedBytes)} })
	if !e.Includes[0].OK {
		t.Fatal("ちょうどの上限")
	}
	e = expand(t, directive("a.md"), "", func(string) Read { return Read{Kind: Text, Text: strings.Repeat("あ", MaxExpandedBytes/3+1)} })
	if e.Includes[0].OK || !strings.Contains(e.Text, "too large") {
		t.Fatal("容量上限")
	}
	e = expand(t, directive("a.md")+"\n"+directive("b.md"), "", func(string) Read { return Read{Kind: Text, Text: strings.Repeat("x", MaxExpandedBytes/2)} })
	if !e.Includes[0].OK || e.Includes[1].OK {
		t.Fatal("合計の上限")
	}
	e = expand(t, directive("a.md")+"\n\n後", "", func(string) Read { return Read{Kind: Text, Text: "一\n二\n"} })
	for _, c := range [][3]int{{1, 1, 1}, {2, 1, 1}, {3, 1, 1}, {4, 2, 0}, {5, 3, 0}} {
		included := 0
		if e.IsIncludedLine(c[0]) {
			included = 1
		}
		if e.OriginalLine(c[0]) != c[1] || included != c[2] {
			t.Fatal(c, e.OriginalLine(c[0]), included)
		}
	}
	if e.OriginalLine(0) != 0 {
		t.Fatal("不正な行番号")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Expand(ctx, directive("a.md"), "", nil); err != context.Canceled {
		t.Fatal(err)
	}
}
func TestEscapes(tst *testing.T) {
	e, err := engine.New(context.Background())
	if err != nil {
		tst.Fatal(err)
	}
	defer e.Close(context.Background())
	for _, c := range [][2]string{{"abcdefg", "abcdefg"}, {"*hello*", `\*hello\*`}, {"[A link](https://link.com)", `\[A link\]\(https://link\.com\)`}, {"some <\"complicated\"> & '/problematic\\' input", `some \<\"complicated\"\> \& '/problematic\\' input`}, {"~~text~~", `\~\~text\~\~`}} {
		if EscapeInline(c[0]) != c[1] {
			tst.Fatal(c, EscapeInline(c[0]))
		}
		raw, err := e.Render(context.Background(), c[1], map[string]any{"autolink": false, "relaxed_autolinks": false})
		if err != nil {
			tst.Fatal(err)
		}
		tree, err := t.Fragment(context.Background(), strings.TrimRight(raw, "\n"))
		if err != nil || t.Content(tree) != c[0] {
			tst.Fatal(raw, err)
		}
	}
	target := "rabbits) <cup\rcakes\n> [%7Bhya%cinth%7d]("
	want := "<rabbits) \\<cup%0Dcakes%0A\\> [%7Bhya%cinth%7d](>"
	if EscapeLinkDestination(target) != want {
		tst.Fatal(EscapeLinkDestination(target))
	}
	raw, err := e.Render(context.Background(), "[link]("+want+")", nil)
	if err != nil {
		tst.Fatal(err)
	}
	root, err := t.Fragment(context.Background(), raw)
	if err != nil {
		tst.Fatal(err)
	}
	links, err := t.Elements(context.Background(), root, func(n *html.Node) bool { return n.Data == "a" })
	if err != nil || len(links) != 1 || t.Get(links[0], "href") != "rabbits)%20%3Ccup%0Dcakes%0A%3E%20%5B%7Bhya%25cinth%7d%5D(" {
		tst.Fatal(raw, err)
	}
	var controls, escaped strings.Builder
	for i := 0; i < 32; i++ {
		controls.WriteByte(byte(i))
		fmt.Fprintf(&escaped, "&#%d;", i)
	}
	if EscapeInline(controls.String()) != escaped.String() {
		tst.Fatal("制御文字")
	}
	unchanged := "é尾😀 $ % ' : ; ? / @ { | }"
	if EscapeInline(unchanged) != unchanged || EscapeInline("`") != "\\`" || EscapeLinkDestination("a b(c)\\d") != "<a b(c)\\d>" {
		tst.Fatal("対象外の文字")
	}
}
