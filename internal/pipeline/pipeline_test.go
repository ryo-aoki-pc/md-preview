package pipeline

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ryo-aoki-pc/md-preview/engine"
	t "github.com/ryo-aoki-pc/md-preview/internal/htmltree"
)

func TestFrontMatter(tst *testing.T) {
	for _, c := range [][2]string{{"---\na: b\n---\n本文", "```yaml:frontmatter\na: b\n```\n本文"}, {"+++\na=1\n+++\n本文", "```toml:frontmatter\na=1\n```\n本文"}, {";;;\n{}\n;;;\n本文", "```json:frontmatter\n{}\n```\n本文"}, {"---ruby\nx\n...\n本文", "```ruby:frontmatter\nx\n```\n本文"}, {"# coding: utf-8\n\n---\nx\n+++\n本文", "\n\n```yaml:frontmatter\nx\n```\n本文"}, {"---\nx\n---", "```yaml:frontmatter\nx\n```\n"}, {"---ruby\u2028x\n---\n", "```ruby\u2028x:frontmatter\n```\n"}} {
		if got := FrontMatter(c[0]); got != c[1] {
			tst.Fatalf("入力=%q\n見本=%q\nGo=%q", c[0], c[1], got)
		}
	}
	for _, source := range []string{"本文\n---\nx\n---\n", "---\nx\n", "---\nx\n--- trailing\n", "\u00a0---\nx\n---\n", "\u2028---\nx\n---\n", "---\nx\u2028---\n"} {
		if got := FrontMatter(source); got != source {
			tst.Fatal(source, got)
		}
	}
	if !strings.Contains(FrontMatter("\n---\nx\n---\n"), "yaml:frontmatter") {
		tst.Fatal("前置きの LF")
	}
}
func TestConcurrentRenderAndCancellation(tst *testing.T) {
	e, err := engine.New(context.Background())
	if err != nil {
		tst.Fatal(err)
	}
	defer e.Close(context.Background())
	r := New(e)
	source := "[TOC]\n\n# 題\n\n~~~python\ndef f(x):\n    return x + 1\n~~~\n\n::include{file=part.md}"
	c := Context{ReadInclude: func(string) IncludeRead { return IncludeRead{Kind: IncludeText, Text: "参照 #123\n\n![図](x.png)"} }, Exists: func(string) bool { return false }}
	expected, err := r.Render(context.Background(), source, c)
	if err != nil {
		tst.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			actual, err := r.Render(context.Background(), source, c)
			if err != nil {
				tst.Error(err)
				return
			}
			if diff, err := compareHTML(expected.HTML, actual.HTML, false); diff != "" || err != nil {
				tst.Error(diff, err)
			}
			if t.JSON(expected.Headings) != t.JSON(actual.Headings) || t.JSON(expected.Notices) != t.JSON(actual.Notices) {
				tst.Error("同時の呼び出しで状態を共有しました。")
			}
		}()
	}
	wg.Wait()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := r.Render(ctx, source, c); err != context.Canceled || result.HTML != "" {
		tst.Fatal(result, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	c.ReadInclude = func(string) IncludeRead { cancel(); return IncludeRead{Kind: IncludeText, Text: "本文"} }
	if result, err := r.Render(ctx, source, c); err != context.Canceled || result.HTML != "" {
		tst.Fatal(result, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	timer := time.AfterFunc(time.Millisecond, cancel)
	_, err = r.Render(ctx, strings.Repeat("# 題\n\n本文\n\n", 200000), Context{})
	timer.Stop()
	cancel()
	if !errors.Is(err, context.Canceled) {
		tst.Fatal(err)
	}
	if _, err := r.Render(context.Background(), "復帰", Context{}); err != nil {
		tst.Fatal(err)
	}
}

// template の中身は、GitLab と同じく残して消毒する (gitlab.com の Markdown API も、div と表の中の
// template の中身を残して返す)。Node.js 版は、木を作る部品が中身を別の場所に持つので落としていた。
func TestTemplateContent(tst *testing.T) {
	e, err := engine.New(context.Background())
	if err != nil {
		tst.Fatal(err)
	}
	defer e.Close(context.Background())
	r := New(e)
	for _, c := range [][2]string{
		{"<div><template>文字<b>太字</b></template></div>", "<div>文字<b>太字</b></div>"},
		{"<table><template><tr><td>表の中</td></tr></template></table>", `<table dir="auto"><tr><td>表の中</td></tr></table>`},
		{"前\n\n<template>\n\n段落の中身\n\n</template>\n\n後", `<p data-sourcepos="1:1-1:3" dir="auto">前</p>` + "\n\n" + `<p data-sourcepos="5:1-5:15" dir="auto">段落の中身</p>` + "\n\n" + `<p data-sourcepos="9:1-9:3" dir="auto">後</p>`},
	} {
		result, err := r.Render(context.Background(), c[0], Context{})
		if err != nil {
			tst.Fatal(c[0], err)
		}
		if diff, err := compareHTML(c[1], result.HTML, false); diff != "" || err != nil {
			tst.Fatalf("入力=%q\nGo=%q\n%s %v", c[0], result.HTML, diff, err)
		}
	}
	result, err := r.Render(context.Background(), `<template><img src="x.png" onerror="alert(1)"><script>alert(1)</script><a href="javascript:alert(1)">リンク</a></template>`, Context{})
	if err != nil {
		tst.Fatal(err)
	}
	root, err := t.Fragment(context.Background(), result.HTML)
	if err != nil {
		tst.Fatal(err)
	}
	if problems := safetyProblems(root); len(problems) != 0 || !strings.Contains(result.HTML, "リンク") || strings.Contains(result.HTML, "alert") {
		tst.Fatal(problems, result.HTML)
	}
}
func TestTextAttributeOrderScope(tst *testing.T) {
	a := `前 <pre lang="html" data-sourcepos="1:1-1:3">後`
	b := `前 <pre data-sourcepos="1:1-1:3" lang="html">後`
	if canonicalTextTags(a) != canonicalTextTags(b) {
		tst.Fatal("属性順の正規化")
	}
	for _, different := range []string{`前 <code lang="html" data-sourcepos="1:1-1:3">後`, `前 <pre lang="ruby" data-sourcepos="1:1-1:3">後`, `別 <pre lang="html" data-sourcepos="1:1-1:3">後`, `前 <pre LANG="html" data-sourcepos="1:1-1:3">後`, `前 <pre lang="html" data-sourcepos="1:1-1:3">別`} {
		if canonicalTextTags(a) == canonicalTextTags(different) {
			tst.Fatal("属性順以外の差を無視しました。", different)
		}
	}
	if textOrderException("ほかの入力") {
		tst.Fatal("例外の範囲")
	}
	if canonicalTextTags(`<img src=x/>`) == canonicalTextTags(`<img src=x>`) {
		tst.Fatal("値の末尾の / を落としました。")
	}
	strict, err := compareHTML("<p>&lt;i a=\"1\" b=\"2\"&gt;</p>", "<p>&lt;i b=\"2\" a=\"1\"&gt;</p>", false)
	if err != nil || strict == "" {
		tst.Fatal("厳密な比較を緩めました。")
	}
}

func TestSafetyChecker(tst *testing.T) {
	for _, source := range []string{`<p onclick="x">a</p>`, `<a href="java&#9;script:alert(1)">a</a>`, `<svg></svg>`, `<p style="color:red">a</p>`, `<!-- x -->`, `<img src="data:image/png;base64,AAAA">`, `<span class="gfm-color_chip"><span style="background-color: red; position: fixed;">x</span></span>`} {
		root, err := t.Fragment(context.Background(), source)
		if err != nil {
			tst.Fatal(err)
		}
		if len(safetyProblems(root)) == 0 {
			tst.Fatal("危ない出力を見逃しました。", source)
		}
	}
}
func BenchmarkLargeDocument(b *testing.B) {
	e, err := engine.New(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close(context.Background())
	r := New(e)
	source := strings.Repeat("本文 abcdefghijklmnopqrstuvwxyz\n\n", 70000)
	if _, err := r.Render(context.Background(), "準備", Context{}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Render(context.Background(), source, Context{}); err != nil {
			b.Fatal(err)
		}
	}
}
