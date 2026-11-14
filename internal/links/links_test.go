package links_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ryo-aoki-pc/md-tools/engine"
	"github.com/ryo-aoki-pc/md-tools/internal/links"
	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"github.com/ryo-aoki-pc/md-tools/internal/resolve"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

type renderer func(context.Context, string, pipeline.Context) (pipeline.Result, error)

func (r renderer) Render(ctx context.Context, text string, c pipeline.Context) (pipeline.Result, error) {
	return r(ctx, text, c)
}

func rootFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root, err := resolve.RealPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range files {
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestRenderedLinks(t *testing.T) {
	e, err := engine.New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close(context.Background())
	r := pipeline.New(e)
	check := func(t *testing.T, text string, files map[string]string) links.Result {
		t.Helper()
		root := rootFiles(t, files)
		c := session.FileContext(root, "docs/main.md", "docs", false)
		rendered, err := r.Render(context.Background(), text, c)
		if err != nil {
			t.Fatal(err)
		}
		got, err := links.Check(context.Background(), rendered, root, c, r)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	t.Run("同じ題と日本語と符号化したID", func(t *testing.T) {
		got := check(t, "# 題\n\n# 題\n\n[一](#題) [二](#%E9%A1%8C-1) [三](#user-content-題) [ない](#題-2)\n", nil)
		if len(got.Broken) != 1 || got.Broken[0].Href != "#%E9%A1%8C-2" || *got.Broken[0].Line != 5 || len(got.Unchecked) != 0 {
			t.Fatal(got)
		}
	})
	t.Run("親の相対パスと符号化と見つからないファイルと外側", func(t *testing.T) {
		got := check(t, "[ある](../file.txt) [日本語](../%E9%A1%8C.txt) [ない](../missing.txt) [外](../../outside.txt)\n", map[string]string{"file.txt": "本文", "題.txt": "本文"})
		if len(got.Broken) != 1 || got.Broken[0].Href != "../missing.txt" || len(got.Unchecked) != 1 || got.Unchecked[0].Reason != "ルートの外" {
			t.Fatal(got)
		}
	})
	t.Run("別文書のIDを一度だけ描き循環しない", func(t *testing.T) {
		root := rootFiles(t, map[string]string{"a.md": "# 題\n\n[b](docs/main.md#題)\n"})
		c := session.FileContext(root, "docs/main.md", "docs", false)
		text := "# 題\n\n[a](../a.md#題) [a](../a.md#%E9%A1%8C) [ない](../a.md#無い)\n"
		rendered, err := r.Render(context.Background(), text, c)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		counted := renderer(func(ctx context.Context, text string, c pipeline.Context) (pipeline.Result, error) {
			count++
			return r.Render(ctx, text, c)
		})
		got, err := links.Check(context.Background(), rendered, root, c, counted)
		if err != nil || count != 1 || len(got.Broken) != 1 || got.Broken[0].Href != "../a.md#%E7%84%A1%E3%81%84" || len(got.Unchecked) != 0 {
			t.Fatal(got, err, count)
		}
	})
	t.Run("includeとfrontmatterの公開行", func(t *testing.T) {
		text := "---\na: b\n---\n\n::include{file=part.md}\n\n[後](#missing-after)\n"
		got := check(t, text, map[string]string{"docs/part.md": "[中](#missing-included)\n\n本文\n"})
		if len(got.Broken) != 2 || *got.Broken[0].Line != 5 || *got.Broken[1].Line != 7 {
			t.Fatal(got)
		}
	})
	t.Run("改行した段落のリンクの行", func(t *testing.T) {
		got := check(t, "最初の行\n[後](#missing)\n", nil)
		if len(got.Broken) != 1 || *got.Broken[0].Line != 2 {
			t.Fatal(got)
		}
	})
	t.Run("表のセルのリンクの行", func(t *testing.T) {
		got := check(t, "# 題\n\n| [列](#missing-header) | 先 |\n| --- | --- |\n| 一 | [ない](#missing-first) |\n| 二 | [ない](#missing-second) |\n", nil)
		want := []links.Item{
			{Line: new(3), Href: "#missing-header", Reason: "行き先の ID がありません"},
			{Line: new(5), Href: "#missing-first", Reason: "行き先の ID がありません"},
			{Line: new(6), Href: "#missing-second", Reason: "行き先の ID がありません"},
		}
		if !reflect.DeepEqual(got.Broken, want) || len(got.Unchecked) != 0 {
			t.Fatal(got)
		}
	})
	t.Run("Markdown以外はフラグメントによらずファイルの存在だけを調べる", func(t *testing.T) {
		got := check(t, "[ある](doc.pdf) [ページ](doc.pdf#page=1) [ない](missing.pdf) [ないページ](missing.pdf#page=1)\n", map[string]string{"docs/doc.pdf": "PDFの見本"})
		want := []links.Item{
			{Line: new(1), Href: "missing.pdf", Reason: "行き先のファイルがありません"},
			{Line: new(1), Href: "missing.pdf#page=1", Reason: "行き先のファイルがありません"},
		}
		if !reflect.DeepEqual(got.Broken, want) || len(got.Unchecked) != 0 {
			t.Fatal(got)
		}
	})
	t.Run("画像は既存の知らせに任せる", func(t *testing.T) {
		got := check(t, "![図](missing.png)\n", nil)
		if len(got.Broken)+len(got.Unchecked) != 0 {
			t.Fatal(got)
		}
	})
	t.Run("同じ文書へのリンクは未保存の見出しを使う", func(t *testing.T) {
		got := check(t, "# 未保存\n\n[自分](main.md#未保存)\n", map[string]string{"docs/main.md": "# 保存済み\n"})
		if len(got.Broken)+len(got.Unchecked) != 0 {
			t.Fatal(got)
		}
	})
	t.Run("一度も保存していない自分の文書へのリンクも今の見出しを使う", func(t *testing.T) {
		got := check(t, "# 未保存\n\n[文書](main.md) [空](main.md#) [見出し](./main.md#未保存) [別名](../docs/main.md#user-content-未保存) [ない](main.md#missing)\n", nil)
		want := []links.Item{{Line: new(3), Href: "main.md#missing", Reason: "行き先の ID がありません"}}
		if !reflect.DeepEqual(got.Broken, want) || len(got.Unchecked) != 0 {
			t.Fatal(got)
		}
	})
}

func TestAnchorsAndExcludedLinks(t *testing.T) {
	rendered := pipeline.Result{HTML: `<a name="名前"></a><div id="場所"></div><h1 id="user-content-title">題</h1>
<p data-sourcepos="3:1-3:80"><a href="#%E5%90%8D%E5%89%8D">名前</a><a href="#場所">場所</a><a href="#title">題</a><a href="#missing">ない</a></p>
<a class="anchor" href="#anchor-missing"></a><ul class="section-nav"><li><a href="#toc-missing">目次</a></li></ul>
<a data-footnote-ref href="#footnote-missing">注</a><a class="footnote-backref" href="#back-missing">戻る</a>`}
	rendered.Headings = []pipeline.Heading{{ID: "title"}}
	got, err := links.Check(context.Background(), rendered, "", pipeline.Context{}, nil)
	if err != nil || len(got.Broken) != 1 || got.Broken[0].Href != "#missing" || len(got.Unchecked) != 0 {
		t.Fatal(got, err)
	}
}

func TestExternalLinksDoNotRequest(t *testing.T) {
	var hits atomic.Int32
	trap := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer trap.Close()
	rendered := pipeline.Result{HTML: `<a href="` + trap.URL + `/missing">外</a><a href="https://example.invalid">外</a><a href="mailto:a@example.invalid">メール</a><a href="//example.invalid/path">外</a><a href="ftp://example.invalid/path">外</a>`}
	untouched := pipeline.Context{Exists: func(string) bool { t.Fatal("外部リンクの存在を調べた"); return false }}
	got, err := links.Check(context.Background(), rendered, "", untouched, nil)
	if err != nil || hits.Load() != 0 || len(got.Broken)+len(got.Unchecked) != 0 {
		t.Fatal(got, err, hits.Load())
	}
}

func TestLimitsAndCancellation(t *testing.T) {
	files := map[string]string{}
	var body strings.Builder
	for i := 0; i < 34; i++ {
		name := fmt.Sprintf("%d.md", i)
		files[name] = "題"
		fmt.Fprintf(&body, `<a href="%s#title">題</a>`, name)
	}
	root := rootFiles(t, files)
	c := session.FileContext(root, "main.md", "", false)
	count := 0
	r := renderer(func(context.Context, string, pipeline.Context) (pipeline.Result, error) {
		count++
		return pipeline.Result{Headings: []pipeline.Heading{{ID: "title"}}}, nil
	})
	got, err := links.Check(context.Background(), pipeline.Result{HTML: body.String()}, root, c, r)
	if err != nil || count != 32 || len(got.Broken) != 0 || len(got.Unchecked) != 2 || !strings.Contains(got.Unchecked[0].Reason, "上限") {
		t.Fatal(got, err, count)
	}
	t.Run("大きすぎる文書は描かない", func(t *testing.T) {
		file := filepath.Join(root, "large.md")
		f, err := os.Create(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(links.MaxBytes + 1); err != nil {
			t.Fatal(err)
		}
		f.Close()
		before := count
		got, err := links.Check(context.Background(), pipeline.Result{HTML: `<a href="large.md#title">大きい</a>`}, root, c, r)
		if err != nil || count != before || len(got.Unchecked) != 1 || !strings.Contains(got.Unchecked[0].Reason, "10 MiB") {
			t.Fatal(got, err, count)
		}
	})
	t.Run("期限と取り消しを描画へ伝え残りを未点検にする", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		calls := 0
		blocking := renderer(func(ctx context.Context, _ string, _ pipeline.Context) (pipeline.Result, error) {
			calls++
			<-ctx.Done()
			return pipeline.Result{}, ctx.Err()
		})
		got, err := links.Check(ctx, pipeline.Result{HTML: `<a href="0.md#title">一</a><a href="1.md#title">二</a>`}, root, c, blocking)
		if err != nil || calls != 1 || len(got.Unchecked) != 2 || !strings.Contains(got.Unchecked[0].Reason, "期限") {
			t.Fatal(got, err, calls)
		}
	})
}

func TestOutsideSymlinkIsNotRead(t *testing.T) {
	outside := rootFiles(t, map[string]string{"outside.md": "秘密ではない試験の本文"})
	root := rootFiles(t, nil)
	if err := os.Symlink(filepath.Join(outside, "outside.md"), filepath.Join(root, "link.md")); err != nil {
		t.Skip("ファイルのリンクを作れないため未実施: ", err)
	}
	c := session.FileContext(root, "main.md", "", false)
	reads := []string{}
	c.ReadInclude = func(name string) pipeline.IncludeRead {
		reads = append(reads, name)
		return pipeline.IncludeRead{}
	}
	got, err := links.Check(context.Background(), pipeline.Result{HTML: `<a href="link.md#title">外</a>`}, root, c, nil)
	if err != nil || !reflect.DeepEqual(reads, []string{}) || len(got.Unchecked) != 1 || !strings.Contains(got.Unchecked[0].Reason, "ルートの外") {
		t.Fatal(got, err, reads)
	}
}
