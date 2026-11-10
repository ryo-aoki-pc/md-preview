// Package export は消毒済みの本文を単一の HTML に書き出す。
package export

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ryo-aoki-pc/md-preview/internal/documents"
	"github.com/ryo-aoki-pc/md-preview/internal/fileopen"
	"github.com/ryo-aoki-pc/md-preview/internal/htmltree"
	"github.com/ryo-aoki-pc/md-preview/internal/pagefiles"
	"github.com/ryo-aoki-pc/md-preview/internal/paths"
	"github.com/ryo-aoki-pc/md-preview/internal/pipeline"
	"github.com/ryo-aoki-pc/md-preview/internal/resolve"
	"github.com/ryo-aoki-pc/md-preview/internal/session"
	"golang.org/x/net/html"
)

const maximumBytes = 16 * 1024 * 1024

type Renderer interface {
	Render(context.Context, string, pipeline.Context) (pipeline.Result, error)
}
type DiagramRenderer interface {
	Render(context.Context, string) ([]byte, error)
}
type Options struct {
	File, Root, Output, Theme, Width string
	Timeout                          time.Duration
	Renderer                         Renderer
	Plantuml                         DiagramRenderer
}
type NoticeCount struct {
	Kind  string
	Count int
}
type Result struct {
	HTML     string
	Notices  []NoticeCount
	Warnings []string
}
type exportError struct{ message string }

func (e *exportError) Error() string { return e.message }
func failure(s string) error         { return &exportError{s} }

//go:embed extras.js
var extras string

// RenderForExport は描画の全処理に既定で 60 秒の期限を渡す。
func RenderForExport(ctx context.Context, renderer Renderer, text string, c pipeline.Context, timeout time.Duration) (pipeline.Result, error) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if renderer == nil {
		return pipeline.Result{}, failure("文書を描画できません")
	}
	r, err := renderer.Render(ctx, text, c)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return pipeline.Result{}, failure("描画が時間切れになりました")
	}
	if err != nil || ctx.Err() != nil {
		var doc *pipeline.DocumentError
		if errors.As(err, &doc) {
			return pipeline.Result{}, failure(doc.Message)
		}
		return pipeline.Result{}, failure("文書を描画できません")
	}
	return r, nil
}

func documentContext(file, override string, plantuml bool) (string, pipeline.Context, error) {
	root := filepath.Dir(file)
	if override != "" {
		absolute, err := filepath.Abs(override)
		if err != nil {
			return "", pipeline.Context{}, failure("ルートにはフォルダーを指定してください")
		}
		root, err = resolve.RealPath(absolute)
		if err != nil {
			return "", pipeline.Context{}, failure("ルートにはフォルダーを指定してください")
		}
	} else {
		for folder := root; ; folder = filepath.Dir(folder) {
			if _, err := os.Stat(filepath.Join(folder, ".git")); err == nil {
				root = folder
				break
			}
			if folder == filepath.Dir(folder) {
				break
			}
		}
	}
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return "", pipeline.Context{}, failure("ルートにはフォルダーを指定してください")
	}
	name, err := filepath.Rel(root, file)
	if err != nil {
		return "", pipeline.Context{}, failure("文書はルートの内側のファイルを指定してください")
	}
	name = filepath.ToSlash(name)
	found := resolve.Resolve(root, documents.Encode(name))
	if filepath.IsAbs(name) || found.Status != 200 {
		return "", pipeline.Context{}, failure("文書はルートの内側のファイルを指定してください")
	}
	st, err = os.Stat(found.Path)
	if err != nil || !st.Mode().IsRegular() {
		return "", pipeline.Context{}, failure("文書はルートの内側のファイルを指定してください")
	}
	dir, _ := filepath.Rel(root, filepath.Dir(file))
	if dir == "." {
		dir = ""
	}
	return root, session.FileContext(root, name, filepath.ToSlash(dir), plantuml), nil
}

func safeTarget(root, name string) bool {
	if name == "" {
		return true
	}
	for current := name; current != ""; {
		r := resolve.Resolve(root, documents.Encode(current))
		if r.Status == 200 {
			return true
		}
		if r.Status != 404 {
			return false
		}
		i := strings.LastIndexByte(current, '/')
		if i <= 0 {
			return resolve.Resolve(root, documents.Encode(name), true).Status == 404
		}
		current = current[:i]
	}
	return false
}

var scheme = regexp.MustCompile(`(?i)^[a-z][a-z\d+.-]*:`)

// RewriteURLs は安全な相対 URL だけを書き出し先からの相対に直す。
func RewriteURLs(ctx context.Context, tree *html.Node, root, dir, outputDirectory string) error {
	return htmltree.Walk(ctx, tree, func(n *html.Node) error {
		if n.Type != html.ElementNode {
			return nil
		}
		for _, key := range []string{"href", "src", "poster", "data-src", "data-canonical-src"} {
			raw := htmltree.Get(n, key)
			if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "//") || scheme.MatchString(raw) {
				continue
			}
			name, suffix := raw, ""
			if i := strings.IndexAny(raw, "?#"); i >= 0 {
				name, suffix = raw[:i], raw[i:]
			}
			if name == "" {
				continue
			}
			target, err := url.PathUnescape(name)
			if err != nil || !utf8.ValidString(target) {
				continue
			}
			local := paths.Resolve(target, dir)
			if !local.Safe || !safeTarget(root, local.Path) {
				continue
			}
			rewritten, err := filepath.Rel(outputDirectory, filepath.Join(root, filepath.FromSlash(local.Path)))
			if err != nil || filepath.IsAbs(rewritten) {
				htmltree.Del(n, key)
				continue
			}
			rewritten = documents.Encode(filepath.ToSlash(rewritten))
			if !strings.HasPrefix(rewritten, ".") {
				rewritten = "./" + rewritten
			}
			htmltree.Set(n, key, rewritten+suffix)
		}
		return nil
	})
}

var diagramURL = regexp.MustCompile(`^/_/plantuml/([0-9a-f]{64})\.svg$`)

func embedDiagrams(ctx context.Context, tree *html.Node, diagrams []pipeline.Diagram, renderer DiagramRenderer) (int, error) {
	type drawn struct {
		source string
		svg    []byte
	}
	rendered := map[string]drawn{}
	for _, d := range diagrams {
		var svg []byte
		if renderer != nil {
			b, err := renderer.Render(ctx, d.Source)
			if err == nil {
				svg = b
			}
		}
		rendered[d.ID] = drawn{d.Source, svg}
	}
	images, err := htmltree.Elements(ctx, tree, func(n *html.Node) bool { return n.Data == "img" && diagramURL.MatchString(htmltree.Get(n, "src")) })
	if err != nil {
		return 0, err
	}
	failed := 0
	for _, img := range images {
		var anchor *html.Node
		if p := img.Parent; p != nil && p.Data == "a" && htmltree.Get(p, "data-diagram") == "plantuml" {
			anchor = p
		}
		if anchor == nil && htmltree.Get(img, "data-diagram") != "plantuml" {
			continue
		}
		d := rendered[diagramURL.FindStringSubmatch(htmltree.Get(img, "src"))[1]]
		if len(d.svg) > 0 {
			htmltree.Set(img, "src", "data:image/svg+xml;base64,"+base64.StdEncoding.EncodeToString(d.svg))
			htmltree.Set(img, "class", "gp-plantuml")
			htmltree.Set(img, "alt", "PlantUML の図")
			if anchor != nil {
				for _, key := range []string{"href", "target", "rel"} {
					htmltree.Del(anchor, key)
				}
			}
		} else {
			failed++
			attrs := htmltree.Attr("class", "gp-plantuml-error")
			if htmltree.Has(img, "data-sourcepos") {
				attrs = append(attrs, html.Attribute{Key: "data-sourcepos", Val: htmltree.Get(img, "data-sourcepos")})
			}
			pre := htmltree.Element("pre", attrs, htmltree.Element("code", nil, htmltree.Text(d.source)))
			if anchor != nil {
				htmltree.Replace(anchor, pre)
			} else {
				htmltree.Replace(img, pre)
			}
		}
	}
	return failed, nil
}

// Export は入力を検査し、完成した HTML だけを返す。Output があれば原子的に保存する。
func Export(ctx context.Context, o Options) (result Result, err error) {
	defer func() {
		if err != nil {
			var known *exportError
			if !errors.As(err, &known) {
				err = failure("HTML への書き出しに失敗しました")
			}
			result = Result{}
		}
	}()
	if o.Theme == "" {
		o.Theme = "system"
	}
	if o.Width == "" {
		o.Width = "fluid"
	}
	if o.Theme != "system" && o.Theme != "light" && o.Theme != "dark" || o.Width != "fluid" && o.Width != "fixed" {
		return result, failure("配色または本文幅が正しくありません")
	}
	path, err := filepath.Abs(o.File)
	if err == nil {
		path, err = resolve.RealPath(path)
	}
	if err != nil {
		return result, failure("指定したファイルを読めません")
	}
	info, err := os.Stat(path)
	if err != nil {
		return result, failure("指定したファイルを読めません")
	}
	if !info.Mode().IsRegular() {
		return result, failure("Markdown ファイルを指定してください")
	}
	f, err := fileopen.Open(path)
	if err != nil {
		return result, failure("指定したファイルを読めません")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return result, failure("指定したファイルを読めません")
	}
	if !st.Mode().IsRegular() {
		return result, failure("Markdown ファイルを指定してください")
	}
	if st.Size() > maximumBytes {
		return result, failure("文書の大きさが上限の16 MBを超えています")
	}
	b, err := io.ReadAll(io.LimitReader(f, maximumBytes+1))
	if err != nil {
		return result, failure("指定したファイルを読めません")
	}
	if len(b) > maximumBytes {
		return result, failure("文書の大きさが上限の16 MBを超えています")
	}
	if bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b) {
		return result, failure("文書は UTF-8 のテキストで指定してください")
	}
	root, c, err := documentContext(path, o.Root, o.Plantuml != nil)
	if err != nil {
		return result, err
	}
	rendered, err := RenderForExport(ctx, o.Renderer, strings.TrimPrefix(string(b), "\ufeff"), c, o.Timeout)
	if err != nil {
		return result, err
	}
	tree, err := htmltree.Fragment(ctx, rendered.HTML)
	if err != nil {
		return result, err
	}
	failed, err := embedDiagrams(ctx, tree, rendered.Diagrams, o.Plantuml)
	if err != nil {
		return result, err
	}
	if failed > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("PlantUML の図を描けませんでした（%d 件）", failed))
	}
	base := filepath.Dir(path)
	if o.Output != "" {
		target, e := filepath.Abs(o.Output)
		if e != nil {
			return result, failure("HTML を書き込めません")
		}
		directory, e := resolve.RealPath(filepath.Dir(target))
		if e != nil {
			return result, failure("書き出し先のフォルダーがありません")
		}
		if existing, e := resolve.RealPath(target); e == nil {
			same := existing == path || runtime.GOOS == "windows" && strings.EqualFold(existing, path)
			if info, e := os.Stat(existing); e == nil {
				same = same || os.SameFile(st, info)
			}
			if same {
				return result, failure("書き出し先が、入力のファイルと同じです")
			}
		}
		outside, e := filepath.Rel(root, directory)
		if e != nil || outside == ".." || strings.HasPrefix(outside, ".."+string(filepath.Separator)) || filepath.IsAbs(outside) {
			result.Warnings = append(result.Warnings, "書き出し先がルートの外にあるので、相対パスのリンクと画像は、文書のフォルダーから見た形のままにしました（ルートの中へ書き出すと、書き出し先から見た形に直します）")
		} else {
			base = directory
		}
	}
	if err = RewriteURLs(ctx, tree, root, *c.Dir, base); err != nil {
		return result, err
	}
	math, mermaid := false, false
	err = htmltree.Walk(ctx, tree, func(n *html.Node) error {
		if n.Type == html.ElementNode {
			math = math || htmltree.HasClass(n, "js-render-math")
			mermaid = mermaid || htmltree.HasClass(n, "js-render-mermaid")
			if htmltree.HasClass(n, "highlight") {
				htmltree.AddClass(n, "code-syntax-highlight-theme")
			}
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	css, err := styles(o.Theme)
	if err != nil {
		return result, err
	}
	scripts := []string{}
	if math || mermaid {
		libs, e := pagefiles.EmbeddedLibraries()
		if e != nil {
			return result, e
		}
		if math {
			kcss, e := katexStyles(libs)
			if e != nil {
				return result, e
			}
			css += "\n" + kcss
			js, e := library(libs, "katex/katex.min.js")
			if e != nil {
				return result, e
			}
			scripts = append(scripts, js)
		}
		if mermaid {
			js, e := library(libs, "mermaid/mermaid.min.js")
			if e != nil {
				return result, e
			}
			scripts = append(scripts, js)
		}
		scripts = append(scripts, extras)
	}
	body, err := htmltree.HTMLContext(ctx, tree)
	if err != nil {
		return result, err
	}
	result.HTML = assemble(path, o.Theme, o.Width, css, body, scripts)
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if o.Output != "" {
		if err = writeAtomic(o.Output, result.HTML); err != nil {
			return result, err
		}
	}
	indices := map[string]int{}
	for _, n := range rendered.Notices {
		if i, ok := indices[n.Kind]; ok {
			result.Notices[i].Count++
		} else {
			indices[n.Kind] = len(result.Notices)
			result.Notices = append(result.Notices, NoticeCount{n.Kind, 1})
		}
	}
	return result, nil
}

func writeAtomic(target, content string) error {
	full, err := filepath.Abs(target)
	if err != nil {
		return failure("HTML を書き込めません")
	}
	temporary := filepath.Join(filepath.Dir(full), "."+filepath.Base(full)+"."+rand.Text()+".tmp")
	f, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
	if err != nil {
		return failure("HTML を書き込めません")
	}
	defer os.Remove(f.Name())
	_, err = io.WriteString(f, content)
	if err == nil {
		err = f.Sync()
	}
	closed := f.Close()
	if err == nil {
		err = closed
	}
	if err == nil {
		err = os.Rename(f.Name(), full)
	}
	if err != nil {
		return failure("HTML を書き込めません")
	}
	return nil
}
