package export

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/internal/fileopen"
	h "github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"golang.org/x/net/html"
)

var pngImage = []byte("\x89PNG\r\n\x1a\n試験用の画像")

func imageTree(t *testing.T, source string) *html.Node {
	t.Helper()
	tree, err := h.Fragment(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func imageSources(t *testing.T, tree *html.Node) []string {
	t.Helper()
	nodes, err := h.Elements(t.Context(), tree, func(n *html.Node) bool { return n.Data == "img" })
	if err != nil {
		t.Fatal(err)
	}
	sources := []string{}
	for _, n := range nodes {
		sources = append(sources, h.Get(n, "src"))
	}
	return sources
}

func imageData(mime string, b []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
}

func TestEmbedImagesFormatsAndUnchangedDefault(t *testing.T) {
	for _, c := range []struct {
		ext, mime string
		data      []byte
	}{
		{"png", "image/png", pngImage},
		{"jpg", "image/jpeg", []byte{0xff, 0xd8, 0xff, 0xe0}},
		{"jpeg", "image/jpeg", []byte{0xff, 0xd8, 0xff, 0xe1}},
		{"gif", "image/gif", []byte("GIF89a試験用の画像")},
		{"GIF", "image/gif", []byte("GIF87a試験用の画像")},
		{"webp", "image/webp", []byte("RIFF\x10\x00\x00\x00WEBPVP8 ")},
	} {
		t.Run(c.ext, func(t *testing.T) {
			o := fixture(t, "![画像](pixel."+c.ext+")\n")
			write(t, filepath.Join(filepath.Dir(o.File), "pixel."+c.ext), string(c.data))
			plain := exported(t, o)
			mustContain(t, plain.HTML, `src="./pixel.`+c.ext+`"`)
			mustNotContain(t, plain.HTML, "data:"+c.mime+";base64,")
			o.EmbedImages = true
			r := exported(t, o)
			sources := imageSources(t, inspect(t, r.HTML).content)
			if len(sources) != 1 || sources[0] != imageData(c.mime, c.data) || len(r.Warnings) != 0 {
				t.Fatal("画像を埋め込みませんでした", sources, r.Warnings)
			}
			mustContain(t, inspect(t, r.HTML).csp, "img-src * data: blob:")
			mustNotContain(t, r.HTML, o.Root, filepath.ToSlash(o.Root))
			o.EmbedImages = false
			if again := exported(t, o); again.HTML != plain.HTML {
				t.Fatal("フラグを付けない書き出しのバイト列が変わりました")
			}
		})
	}
	// フラグなしの出力を、以前と同じ相対パスと本文で組み立てた HTML とバイト単位で比べる。
	o := fixture(t, "本文")
	o.Renderer = renderFunc(func(context.Context, string, pipeline.Context) (pipeline.Result, error) {
		return pipeline.Result{HTML: `<p><img src="pixel.png" alt="画像"></p>`}, nil
	})
	write(t, filepath.Join(filepath.Dir(o.File), "pixel.png"), string(pngImage))
	css, err := styles("system")
	if err != nil {
		t.Fatal(err)
	}
	want := assemble(o.File, "system", "fluid", css, `<p><img src="./pixel.png" alt="画像"/></p>`, nil)
	if got := exported(t, o).HTML; got != want {
		t.Fatal("フラグなしの出力が以前の HTML と違います")
	}
}

func TestEmbedImagesReadsRealPathOnce(t *testing.T) {
	root := realTemp(t)
	write(t, filepath.Join(root, "images", "a b.png"), string(pngImage))
	source := `<img src="../images/a%20b.png?size=1#part"><img src="/images/a%20b.png"><video src="../images/a%20b.png"></video><audio src="../images/a%20b.png"></audio>`
	tree := imageTree(t, source)
	reads := 0
	warnings, err := embedImages(t.Context(), tree, root, "docs", func(name string) (*os.File, error) {
		reads++
		return fileopen.Open(name)
	})
	if err != nil || len(warnings) != 0 || reads != 1 {
		t.Fatal("同じ実パスを一度だけ読みませんでした", reads, warnings, err)
	}
	for _, src := range imageSources(t, tree) {
		if src != imageData("image/png", pngImage) {
			t.Fatal("同じ data URL を使いませんでした")
		}
	}
	text, _ := h.HTML(tree)
	mustContain(t, text, `<video src="../images/a%20b.png">`, `<audio src="../images/a%20b.png">`)
	t.Run("ルート内のリンク", func(t *testing.T) {
		if err := os.Symlink(filepath.Join(root, "images", "a b.png"), filepath.Join(root, "alias.jpg")); err != nil {
			t.Skip("シンボリックリンクを作る権限がありません: ", err)
		}
		tree := imageTree(t, `<img src="alias.jpg"><img src="images/a%20b.png">`)
		reads := 0
		warnings, err := embedImages(t.Context(), tree, root, "", func(name string) (*os.File, error) {
			reads++
			return fileopen.Open(name)
		})
		sources := imageSources(t, tree)
		if err != nil || reads != 1 || sources[0] != "alias.jpg" || sources[1] != imageData("image/png", pngImage) {
			t.Fatal("拡張子の違うリンクを区別しませんでした", reads, warnings, err)
		}
		mustContain(t, strings.Join(warnings, "\n"), "拡張子と画像の種類が一致しません", "1 件")
	})
}

func TestEmbedImagesRefusalsAndRewriting(t *testing.T) {
	root := realTemp(t)
	write(t, filepath.Join(root, "good.png"), string(pngImage))
	write(t, filepath.Join(root, "image.svg"), `<svg onload="危険"/>`)
	write(t, filepath.Join(root, "spoof.jpg"), string(pngImage))
	write(t, filepath.Join(root, "spoof.png"), `<svg/>`)
	write(t, filepath.Join(root, "unknown.bmp"), "BM")
	write(t, filepath.Join(root, ".git", "hidden.png"), string(pngImage))
	if err := os.Mkdir(filepath.Join(root, "folder.png"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		src, reason string
	}{
		{"image.svg", "種類"}, {"spoof.jpg", "拡張子"}, {"spoof.png", "種類"}, {"unknown.bmp", "種類"},
		{"https://example.invalid/image.png", "外部 URL"}, {"//example.invalid/image.png", "外部 URL"},
		{"file:///outside/image.png", "外部 URL"},
		{"../outside.png", "ルートの外"}, {"%2e%2e/outside.png", "ルートの外"}, {".git/hidden.png", "許可されないパス"},
		{"missing.png", "見つかりません"}, {"folder.png", "画像"}, {"bad%ZZ.png", "パスが正しく"},
	} {
		t.Run(c.src, func(t *testing.T) {
			tree := imageTree(t, `<img src="`+c.src+`"><img src="good.png">`)
			legacy := imageTree(t, `<img src="`+c.src+`">`)
			warnings, err := embedImages(t.Context(), tree, root, "", fileopen.Open)
			if err != nil || len(warnings) != 1 {
				t.Fatal(warnings, err)
			}
			mustContain(t, warnings[0], c.reason, "1 件")
			mustNotContain(t, warnings[0], root, filepath.ToSlash(root), c.src)
			output := filepath.Join(root, "export")
			if err := RewriteURLs(t.Context(), tree, root, "", output); err != nil {
				t.Fatal(err)
			}
			if err := RewriteURLs(t.Context(), legacy, root, "", output); err != nil {
				t.Fatal(err)
			}
			sources := imageSources(t, tree)
			if sources[0] != imageSources(t, legacy)[0] || sources[1] != imageData("image/png", pngImage) {
				t.Fatal("断った画像の書き換えか残りの埋め込みが違います")
			}
		})
	}
	t.Run("ルート外へのリンク", func(t *testing.T) {
		outside := filepath.Join(realTemp(t), "outside.png")
		write(t, outside, string(pngImage))
		if err := os.Symlink(outside, filepath.Join(root, "escape.png")); err != nil {
			t.Skip("シンボリックリンクを作る権限がありません: ", err)
		}
		tree := imageTree(t, `<img src="escape.png"><img src="good.png">`)
		warnings, err := embedImages(t.Context(), tree, root, "", fileopen.Open)
		if err != nil || imageSources(t, tree)[0] != "escape.png" {
			t.Fatal(warnings, err)
		}
		mustContain(t, strings.Join(warnings, "\n"), "ルートの外", "1 件")
		mustNotContain(t, strings.Join(warnings, "\n"), outside)
	})
}

func TestEmbedImagesSizeLimits(t *testing.T) {
	root := realTemp(t)
	large := make([]byte, maximumImageBytes+1)
	copy(large, pngImage)
	write(t, filepath.Join(root, "large.png"), string(large))
	write(t, filepath.Join(root, "small.png"), string(pngImage))
	var source strings.Builder
	source.WriteString(`<img src="large.png">`)
	for i := 0; i < 4; i++ {
		name := fmt.Sprintf("limit%d.png", i)
		write(t, filepath.Join(root, name), string(large[:maximumImageBytes]))
		fmt.Fprintf(&source, `<img src="%s"><img src="%s?duplicate">`, name, name)
	}
	source.WriteString(`<img src="small.png">`)
	tree := imageTree(t, source.String())
	warnings, err := embedImages(t.Context(), tree, root, "", fileopen.Open)
	if err != nil || len(warnings) != 2 {
		t.Fatal(warnings, err)
	}
	mustContain(t, strings.Join(warnings, "\n"), "8 MiB", "32 MiB", "1 件")
	sources := imageSources(t, tree)
	if sources[0] != "large.png" || sources[9] != "small.png" {
		t.Fatal("上限を超えた画像を埋め込みました")
	}
	for i := 1; i < 9; i++ {
		if !strings.HasPrefix(sources[i], "data:image/png;base64,") {
			t.Fatal("上限以内の画像を埋め込みませんでした", i)
		}
	}
	// 同じ実パスは繰り返しても合計へ足さない。
	tree = imageTree(t, `<img src="limit0.png"><img src="limit1.png"><img src="limit2.png"><img src="small.png"><img src="limit3.png"><img src="small.png?again">`)
	warnings, err = embedImages(t.Context(), tree, root, "", fileopen.Open)
	sources = imageSources(t, tree)
	if err != nil || len(warnings) != 1 || sources[4] != "limit3.png" || sources[3] != sources[5] {
		t.Fatal("合計の上限と重複の扱いが違います", warnings, err)
	}
	// 合計を超える画像を断っても、その後の小さい画像は残りの容量に埋め込む。
	write(t, filepath.Join(root, "near.png"), string(large[:maximumImageBytes-16]))
	write(t, filepath.Join(root, "tiny.png"), string(pngImage[:8]))
	tree = imageTree(t, `<img src="limit0.png"><img src="limit1.png"><img src="limit2.png"><img src="near.png"><img src="small.png"><img src="tiny.png">`)
	warnings, err = embedImages(t.Context(), tree, root, "", fileopen.Open)
	sources = imageSources(t, tree)
	if err != nil || len(warnings) != 1 || sources[4] != "small.png" || sources[5] != imageData("image/png", pngImage[:8]) {
		t.Fatal("上限を超えた画像の後で埋め込みを続けませんでした", warnings, err)
	}
}
