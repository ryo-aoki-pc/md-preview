package export

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ryo-aoki-pc/md-tools/engine"
	h "github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"github.com/ryo-aoki-pc/md-tools/internal/resolve"
	"github.com/ryo-aoki-pc/md-tools/internal/sanitize"
	"golang.org/x/net/html"
)

var testRenderer *pipeline.Renderer

func TestMain(m *testing.M) {
	e, err := engine.New(context.Background())
	if err != nil {
		panic(err)
	}
	testRenderer = pipeline.New(e)
	code := m.Run()
	_ = e.Close(context.Background())
	os.Exit(code)
}
func realTemp(t *testing.T) string {
	t.Helper()
	p, err := resolve.RealPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func write(t *testing.T, file, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T, text string) Options {
	t.Helper()
	root := realTemp(t)
	o := Options{Root: root, File: filepath.Join(root, "docs", "sample.md"), Renderer: testRenderer}
	write(t, o.File, text)
	return o
}
func exported(t *testing.T, o Options) Result {
	t.Helper()
	r, err := Export(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

type inspected struct {
	tree, content *html.Node
	scripts       []string
	csp           string
}

func inspect(t *testing.T, text string) inspected {
	t.Helper()
	tree, err := html.Parse(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	r := inspected{tree: tree}
	err = h.Walk(t.Context(), tree, func(n *html.Node) error {
		if n.Type != html.ElementNode {
			return nil
		}
		if n.Data == "script" {
			r.scripts = append(r.scripts, h.Content(n))
		}
		if n.Data == "meta" && h.Get(n, "http-equiv") == "Content-Security-Policy" {
			r.csp = h.Get(n, "content")
		}
		if h.Get(n, "id") == "gp-content" {
			r.content = n
		}
		return nil
	})
	if err != nil || r.content == nil {
		t.Fatal("書き出した本文がありません", err)
	}
	return r
}
func mustContain(t *testing.T, s string, values ...string) {
	t.Helper()
	for _, v := range values {
		if !strings.Contains(s, v) {
			t.Errorf("必要な文字がありません: %q", v)
		}
	}
}
func mustNotContain(t *testing.T, s string, values ...string) {
	t.Helper()
	for _, v := range values {
		if strings.Contains(s, v) {
			t.Errorf("不要な文字があります: %q", v)
		}
	}
}
func TestPlainThemesAndStyles(t *testing.T) {
	o := fixture(t, "\ufeff# 文書\n\n本文")
	for _, theme := range []string{"system", "light", "dark"} {
		o.Theme, o.Width = theme, "fixed"
		r := exported(t, o)
		p := inspect(t, r.HTML)
		if len(p.scripts) != 0 {
			t.Fatal("通常の文書にスクリプトがあります")
		}
		mustContain(t, p.csp, "script-src 'none'", "default-src 'none'", "style-src 'unsafe-inline'", "font-src data:", "base-uri 'none'", "form-action 'none'", "img-src * data: blob:", "media-src * data:")
		mustContain(t, r.HTML, `data-width="fixed"`, `data-theme="`+theme+`"`, `name="referrer" content="no-referrer"`, "本文", ":root{--gp-toolbar-height:0px}", "#gp-page.file-holder {")
		mustNotContain(t, r.HTML, "<header", "<aside", `id="gp-notices"`, "#gp-controls", "#gp-outline", "gp-outline-resizer", "#gp-notices", "本文のスタイル: ここから", "127.0.0.1", "/_/assets/", "?token=", o.Root, filepath.ToSlash(o.Root), "\ufeff")
		if theme == "system" {
			mustContain(t, r.HTML, "@media screen and (prefers-color-scheme:dark){")
			mustNotContain(t, r.HTML, "@media(prefers-color-scheme:dark)")
		} else {
			mustNotContain(t, r.HTML, "prefers-color-scheme")
		}
	}
}
func TestExtrasAndCSP(t *testing.T) {
	math, diagram := "$`x^2`$\n", "```mermaid\ngraph TD; A-->B\n```\n"
	for _, c := range []struct {
		source        string
		math, diagram bool
	}{{math, true, false}, {diagram, false, true}, {math + "\n" + diagram, true, true}} {
		o := fixture(t, c.source)
		r := exported(t, o)
		p := inspect(t, r.HTML)
		count := 1
		if c.math {
			count++
		}
		if c.diagram {
			count++
		}
		if len(p.scripts) != count || strings.Count(p.csp, "sha256-") != count {
			t.Fatal("スクリプトと CSP の数が一致しません")
		}
		for _, s := range p.scripts {
			hash := sha256.Sum256([]byte(s))
			mustContain(t, p.csp, "'sha256-"+base64.StdEncoding.EncodeToString(hash[:])+"'")
		}
		if strings.Contains(r.HTML, "data:font/woff2;base64,") != c.math || strings.Contains(r.HTML, "var __esbuild_esm_mermaid_nm") != c.diagram {
			t.Fatal("不要な資産を埋め込みました")
		}
		mustNotContain(t, r.HTML, "url(fonts/", "<script src=", "<link", o.Root, filepath.ToSlash(o.Root), "127.0.0.1", "?token=", "/_/assets/")
	}
}
func TestIncludesAndNotices(t *testing.T) {
	o := fixture(t, "::include{file=part.md}\n\n::include{file=/missing.md}\n")
	write(t, filepath.Join(filepath.Dir(o.File), "part.md"), "## 取り込んだ本文\n")
	r := exported(t, o)
	mustContain(t, r.HTML, "取り込んだ本文")
	if len(r.Notices) != 1 || r.Notices[0] != (NoticeCount{"include", 1}) {
		t.Fatal(r.Notices)
	}
}
func TestRelativeURLsAndOutsideOutput(t *testing.T) {
	o := fixture(t, "[文書](other.md#heading)\n\n![絵](../images/a%20b.png)\n\n[ルート](/other.md?x=1#part)\n\n[外](../../outside.md)\n\n[未作成](new.md)\n")
	write(t, filepath.Join(o.Root, "images", "a b.png"), "画像")
	write(t, filepath.Join(o.Root, "other.md"), "本文")
	write(t, filepath.Join(filepath.Dir(o.File), "other.md"), "本文")
	o.Output = filepath.Join(o.Root, "output", "out.html")
	write(t, o.Output, "前の文書")
	r := exported(t, o)
	mustContain(t, r.HTML, `href="../docs/other.md#heading"`, `src="../images/a%20b.png"`, `href="../other.md?x=1#part"`, `href="../../outside.md"`, `href="../docs/new.md"`)
	b, err := os.ReadFile(o.Output)
	if err != nil || string(b) != r.HTML {
		t.Fatal("保存した HTML が違います", err)
	}
	o.Output = ""
	r = exported(t, o)
	mustContain(t, r.HTML, `href="./other.md#heading"`, `href="../other.md?x=1#part"`)
	if len(r.Warnings) != 0 {
		t.Fatal(r.Warnings)
	}
	o.Output = filepath.Join(realTemp(t), "out.html")
	r = exported(t, o)
	mustContain(t, r.HTML, `href="./other.md#heading"`, `src="../images/a%20b.png"`)
	mustNotContain(t, r.HTML, filepath.Base(o.Root))
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "書き出し先がルートの外") {
		t.Fatal(r.Warnings)
	}
	o.Output = filepath.Join(o.Root, "out.html")
	if warnings := exported(t, o).Warnings; len(warnings) != 0 {
		t.Fatal(warnings)
	}
}
func TestRootsAndEscapes(t *testing.T) {
	o := fixture(t, "::include{file=/part.md}\n")
	if err := os.Mkdir(filepath.Join(o.Root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(o.Root, "part.md"), "ルートの本文")
	o.Root = ""
	mustContain(t, exported(t, o).HTML, "ルートの本文")
	plain := fixture(t, "::include{file=/part.md}\n")
	write(t, filepath.Join(filepath.Dir(plain.File), "part.md"), "フォルダーの本文")
	plain.Root = ""
	mustContain(t, exported(t, plain).HTML, "フォルダーの本文")
	for _, root := range []string{realTemp(t), filepath.Join(filepath.Dir(plain.File), "missing"), plain.File} {
		plain.Root = root
		if _, err := Export(t.Context(), plain); err == nil || !strings.Contains(err.Error(), "ルート") {
			t.Fatal(err)
		}
	}
	o = fixture(t, "::include{file=../../secret.md}\n")
	outside := filepath.Join(realTemp(t), "secret.md")
	write(t, outside, "外側の本文")
	mustNotContain(t, exported(t, o).HTML, "外側の本文")
	link := filepath.Join(filepath.Dir(o.File), "link.md")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("シンボリックリンクを作る権限がありません: ", err)
	}
	write(t, o.File, "::include{file=link.md}\n\n[外](link.md)\n")
	r := exported(t, o)
	mustNotContain(t, r.HTML, "外側の本文")
	mustContain(t, r.HTML, `href="link.md"`)
}
func TestOutputProtectionAndFailures(t *testing.T) {
	o := fixture(t, "# 原稿\n")
	for _, output := range []string{o.File, filepath.Join(filepath.Dir(o.File), ".", "sample.md"), filepath.Join(o.Root, "docs", "..", "docs", "sample.md")} {
		o.Output = output
		if _, err := Export(t.Context(), o); err == nil || err.Error() != "書き出し先が、入力のファイルと同じです" {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(o.File)
		if string(b) != "# 原稿\n" {
			t.Fatal("入力を壊しました")
		}
	}
	o.Output = filepath.Join(o.Root, "out.html")
	for _, text := range []string{string([]byte{255}), "\x00", strings.Repeat("a", maximumBytes+1), strings.Repeat(">", 20000) + " x"} {
		write(t, o.File, text)
		for _, previous := range []bool{false, true} {
			if previous {
				write(t, o.Output, "前の文書")
			} else {
				_ = os.Remove(o.Output)
			}
			if _, err := Export(t.Context(), o); err == nil {
				t.Fatal("失敗する入力を受け付けました")
			}
			b, err := os.ReadFile(o.Output)
			if previous && (err != nil || string(b) != "前の文書") || !previous && !os.IsNotExist(err) {
				t.Fatal("失敗時に出力を変えました", err)
			}
		}
	}
	o.File = filepath.Join(o.Root, "missing.md")
	if _, err := Export(t.Context(), o); err == nil {
		t.Fatal("無い入力を受け付けました")
	}
	o.File = filepath.Join(o.Root, "docs", "sample.md")
	write(t, o.File, "# 文書")
	for _, output := range []string{filepath.Join(o.Root, "missing", "out.html"), filepath.Join(o.Root, "docs")} {
		o.Output = output
		if _, err := Export(t.Context(), o); err == nil {
			t.Fatal("書けない先を受け付けました")
		}
	}
	for _, dir := range []string{o.Root, filepath.Dir(o.File)} {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".tmp") {
				t.Fatal("一時ファイルが残りました")
			}
		}
	}
	o.File = o.Root
	if _, err := Export(t.Context(), o); err == nil || err.Error() != "Markdown ファイルを指定してください" {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"bad", "fluid"}, {"system", "bad"}} {
		o.Theme, o.Width = pair[0], pair[1]
		if _, err := Export(t.Context(), o); err == nil || err.Error() != "配色または本文幅が正しくありません" {
			t.Fatal(err)
		}
	}
}

func TestHardlinkToInputIsProtected(t *testing.T) {
	o := fixture(t, "# 原稿\n")
	o.Output = filepath.Join(o.Root, "alias.html")
	if err := os.Link(o.File, o.Output); err != nil {
		t.Skip("ハードリンクを作れません: ", err)
	}
	if _, err := Export(t.Context(), o); err == nil || err.Error() != "書き出し先が、入力のファイルと同じです" {
		t.Fatal(err)
	}
	for _, file := range []string{o.File, o.Output} {
		b, err := os.ReadFile(file)
		if err != nil || string(b) != "# 原稿\n" {
			t.Fatal("同じ実体のファイルを変えました", err)
		}
	}
}

type renderFunc func(context.Context, string, pipeline.Context) (pipeline.Result, error)

func (f renderFunc) Render(ctx context.Context, s string, c pipeline.Context) (pipeline.Result, error) {
	return f(ctx, s, c)
}
func TestRenderDeadlineAndErrors(t *testing.T) {
	stopped := make(chan struct{})
	r := renderFunc(func(ctx context.Context, _ string, _ pipeline.Context) (pipeline.Result, error) {
		defer close(stopped)
		<-ctx.Done()
		return pipeline.Result{}, ctx.Err()
	})
	_, err := RenderForExport(t.Context(), r, "", pipeline.Context{}, time.Millisecond)
	if err == nil || err.Error() != "描画が時間切れになりました" {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("描画が終わっていません")
	}
	for _, cause := range []error{errors.New("秘密の絶対パス"), context.Canceled} {
		r := renderFunc(func(context.Context, string, pipeline.Context) (pipeline.Result, error) {
			return pipeline.Result{}, cause
		})
		if _, err := RenderForExport(t.Context(), r, "", pipeline.Context{}, 0); err == nil || err.Error() != "文書を描画できません" {
			t.Fatal(err)
		}
	}
}

type diagramFunc func(context.Context, string) ([]byte, error)

func (f diagramFunc) Render(ctx context.Context, s string) ([]byte, error) { return f(ctx, s) }

const svg = `<svg xmlns="http://www.w3.org/2000/svg"><text>試験の図</text></svg>`

func fence(s string) string { return "```plantuml\n" + s + "\n```\n" }
func TestPlantumlEmbeddingAndFallback(t *testing.T) {
	o := fixture(t, fence("export 日本語"))
	o.Plantuml = diagramFunc(func(_ context.Context, s string) ([]byte, error) { return []byte(svg), nil })
	r := exported(t, o)
	p := inspect(t, r.HTML)
	anchors, _ := h.Elements(t.Context(), p.content, func(n *html.Node) bool { return n.Data == "a" && h.Get(n, "data-diagram") == "plantuml" })
	if len(anchors) != 1 {
		t.Fatal("図の包みがありません")
	}
	for _, k := range []string{"href", "target", "rel"} {
		if h.Has(anchors[0], k) {
			t.Fatal("図にリンクが残りました", k)
		}
	}
	mustContain(t, r.HTML, "data:image/svg+xml;base64,"+base64.StdEncoding.EncodeToString([]byte(svg)))
	mustNotContain(t, r.HTML, "/_/plantuml/", "<script", "<svg", "href=\"data:")
	if len(r.Warnings) != 0 {
		t.Fatal(r.Warnings)
	}
	source := `plain 日本語 "><script>x</script>`
	write(t, o.File, fence(source)+fence(source))
	o.Plantuml = diagramFunc(func(context.Context, string) ([]byte, error) { return nil, errors.New("描けません") })
	r = exported(t, o)
	p = inspect(t, r.HTML)
	codes, _ := h.Elements(t.Context(), p.content, func(n *html.Node) bool { return n.Data == "pre" && h.HasClass(n, "gp-plantuml-error") })
	if len(codes) != 2 || h.Content(codes[0]) != source {
		t.Fatal("図を原文へ戻しませんでした", len(codes))
	}
	if len(r.Warnings) != 1 || r.Warnings[0] != "PlantUML の図を描けませんでした（2 件）" {
		t.Fatal(r.Warnings)
	}
	mustNotContain(t, r.HTML, "/_/plantuml/", "<script")
}
func TestPlantumlInsideUserLinksAndUnmarkedImages(t *testing.T) {
	inside := func(s string) string { return "<a href=\"https://example.com/\">\n\n" + fence(s) + "\n</a>\n\n" }
	o := fixture(t, inside("inside 日本語")+inside("plain 失敗")+"![本文の画像](/_/plantuml/"+strings.Repeat("a", 64)+".svg)\n")
	o.Plantuml = diagramFunc(func(_ context.Context, s string) ([]byte, error) {
		if strings.Contains(s, "plain") {
			return nil, errors.New("失敗")
		}
		return []byte(svg), nil
	})
	r := exported(t, o)
	p := inspect(t, r.HTML)
	imgs, _ := h.Elements(t.Context(), p.content, func(n *html.Node) bool { return n.Data == "img" })
	if len(imgs) != 2 || !strings.HasPrefix(h.Get(imgs[0], "src"), "data:image/svg+xml;base64,") || strings.HasPrefix(h.Get(imgs[1], "src"), "data:") || h.Get(imgs[1], "alt") != "本文の画像" {
		t.Fatal("図と本文の画像を区別しませんでした")
	}
	codes, _ := h.Elements(t.Context(), p.content, func(n *html.Node) bool { return n.Data == "pre" && h.HasClass(n, "gp-plantuml-error") })
	if len(codes) != 1 || h.Content(codes[0]) != "plain 失敗" {
		t.Fatal("利用者のリンク内で失敗した図を戻しませんでした")
	}
	links, _ := h.Elements(t.Context(), p.content, func(n *html.Node) bool { return n.Data == "a" && h.Get(n, "href") == "https://example.com/" })
	if len(links) != 2 {
		t.Fatal("利用者のリンクを変えました")
	}
	if len(r.Warnings) == 0 || r.Warnings[0] != "PlantUML の図を描けませんでした（1 件）" {
		t.Fatal(r.Warnings)
	}
}

var dangerousURL = regexp.MustCompile(`^(javascript|vbscript|data|livescript|mocha|jar|blob|filesystem):`)
var cellStyle = regexp.MustCompile(`^text-align: (center|left|right)$`)
var chipStyle = regexp.MustCompile(`^background-color: [#A-Za-z0-9(),.% ]+;$`)

func TestAllXSSReparsedDirectAndInclude(t *testing.T) {
	b, err := os.ReadFile("testdata/xss.json")
	if err != nil {
		t.Fatal(err)
	}
	var payloads []string
	if err := json.Unmarshal(b, &payloads); err != nil {
		t.Fatal(err)
	}
	texts := []string{}
	for _, s := range payloads {
		if !strings.ContainsRune(s, 0) {
			texts = append(texts, s)
		}
	}
	for _, included := range []bool{false, true} {
		o := fixture(t, strings.Join(texts, "\n\n---\n\n"))
		if included {
			write(t, filepath.Join(filepath.Dir(o.File), "input.md"), strings.Join(texts, "\n\n---\n\n"))
			write(t, o.File, "::include{file=input.md}\n")
		}
		p := inspect(t, exported(t, o).HTML)
		// ブラウザーの再解析と同じく、完成した HTML の本文を再び木として読む。
		fragment := h.Root()
		for child := p.content.FirstChild; child != nil; child = child.NextSibling {
			h.Append(fragment, h.Clone(child))
		}
		body, err := h.HTML(fragment)
		if err != nil {
			t.Fatal(err)
		}
		tree, err := h.Fragment(t.Context(), body)
		if err != nil {
			t.Fatal(err)
		}
		err = h.Walk(t.Context(), tree, func(n *html.Node) error {
			if n.Type == html.CommentNode {
				t.Error("コメントが残りました")
			}
			if n.Type != html.ElementNode {
				return nil
			}
			if !sanitize.Tags[n.Data] && n.Data != "video" && n.Data != "audio" && n.Data != "copy-code" && n.Data != "insert-code-snippet" {
				t.Errorf("許可しない要素: %s", n.Data)
			}
			for _, a := range n.Attr {
				if strings.HasPrefix(strings.ToLower(a.Key), "on") {
					t.Error("イベント属性: ", a.Key)
				}
				switch strings.ToLower(a.Key) {
				case "srcdoc", "formaction", "action", "data", "xlink:href", "ping", "background", "manifest":
					t.Error("許可しない属性: ", a.Key)
				case "href", "src", "cite", "longdesc", "data-src", "data-canonical-src", "poster":
					compact := strings.Map(func(r rune) rune {
						if r <= 32 {
							return -1
						}
						return r
					}, strings.ToLower(a.Val))
					if dangerousURL.MatchString(compact) {
						t.Errorf("危険な URL: %s=%q", a.Key, a.Val)
					}
				case "style":
					cell := (n.Data == "th" || n.Data == "td") && cellStyle.MatchString(a.Val)
					chip := n.Data == "span" && n.Parent != nil && n.Parent.Data == "span" && h.HasClass(n.Parent, "gfm-color_chip") && chipStyle.MatchString(a.Val)
					if !cell && !chip {
						t.Errorf("許可しないスタイル: %s", a.Val)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
