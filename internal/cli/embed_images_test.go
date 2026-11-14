package cli

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEmbedImagesAndHelp(t *testing.T) {
	for _, args := range [][]string{{"render", "--embed-images", "sample.md"}, {"render", "sample.md", "--embed-images"}} {
		o, err := ParseArgs(args)
		if err != nil || !o.EmbedImages || o.Command != "render" {
			t.Fatal("画像の埋め込みを受け付けませんでした", o, err)
		}
	}
	for _, args := range [][]string{{"sample.md", "--embed-images"}, {"--stdio", "--embed-images"}, {"install-style", "--embed-images"}} {
		if _, err := ParseArgs(args); err == nil {
			t.Fatal("書き出し以外で埋め込みを受け付けました", args)
		}
	}
	o, err := ParseArgs([]string{"render", "--", "--embed-images"})
	if err != nil || o.EmbedImages || o.File != "--embed-images" {
		t.Fatal("区切りの後ろをオプションとして扱いました", o, err)
	}
	var out bytes.Buffer
	if code := Run(t.Context(), []string{"--help"}, IO{Stdout: &out}, Dependencies{}); code != 0 {
		t.Fatal(code)
	}
	for _, text := range []string{"[--embed-images]", "PNG・JPEG・GIF・WebP", "8 MiB", "32 MiB", "警告して残す"} {
		if !strings.Contains(out.String(), text) {
			t.Fatal("ヘルプに説明がありません", text)
		}
	}
}

func TestRenderEmbedImagesWarningsAndSuccess(t *testing.T) {
	root := commandRoot(t)
	file := filepath.Join(root, "sample.md")
	png := []byte("\x89PNG\r\n\x1a\n試験用の画像")
	for name, data := range map[string][]byte{"good.png": png, "spoof.jpg": png, "image.svg": []byte(`<svg/>`)} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "image.png"), png, 0600); err != nil {
		t.Fatal(err)
	}
	large := make([]byte, 8*1024*1024+1)
	copy(large, png)
	if err := os.WriteFile(filepath.Join(root, "large.png"), large, 0600); err != nil {
		t.Fatal(err)
	}
	source := `<p><img src="good.png"><img src="image.svg"><img src="spoof.jpg"><img src="https://example.invalid/image.png"><img src="data:image/png;base64,AA=="><img src="../outside.png"><img src=".git/image.png"><img src="missing.png"><img src="large.png"></p>`
	if err := os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"render", "--no-plantuml", "--root", root, file}
	var plain, plainErr bytes.Buffer
	deps := Dependencies{Renderer: fakeRenderer{}}
	if code := Run(t.Context(), args, IO{Stdout: &plain, Stderr: &plainErr}, deps); code != 0 || plainErr.Len() != 0 {
		t.Fatal(code, plainErr.String())
	}
	var out, errout bytes.Buffer
	args = append(args, "--embed-images")
	if code := Run(t.Context(), args, IO{Stdout: &out, Stderr: &errout}, deps); code != 0 {
		t.Fatal("埋め込めない画像で失敗しました", code, errout.String())
	}
	data := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	if !strings.Contains(out.String(), `src="`+data+`"`) || strings.Contains(plain.String(), data) {
		t.Fatal("フラグによる埋め込みの切り替えが違います")
	}
	for _, text := range []string{"種類", "拡張子", "外部 URL", "ルートの外", "許可されないパス", "見つかりません", "8 MiB"} {
		if !strings.Contains(errout.String(), text) {
			t.Fatal("標準エラーに画像の警告がありません", text, errout.String())
		}
	}
	// 本文にもともとある data URL の画像は、そのまま残し、警告もしない。
	if strings.Contains(errout.String(), "data URL") || !strings.Contains(out.String(), `src="data:image/png;base64,AA=="`) {
		t.Fatal("data URL の画像の扱いが違います", errout.String())
	}
	if strings.Count(errout.String(), "1 件") != 7 || strings.Contains(errout.String(), root) || strings.Contains(out.String(), filepath.ToSlash(root)) {
		t.Fatal("警告の件数または絶対パスの扱いが違います", errout.String())
	}
	for _, line := range strings.Split(strings.TrimSpace(errout.String()), "\n") {
		if !strings.HasPrefix(line, "mdpreview: 画像を埋め込みませんでした") {
			t.Fatal("警告が日本語の一行ではありません", line)
		}
	}
	// 保存先を指定した場合も成功し、HTML を標準出力へ混ぜない。
	output := filepath.Join(root, "out.html")
	out.Reset()
	errout.Reset()
	if code := Run(t.Context(), append(args, "-o", output), IO{Stdout: &out, Stderr: &errout}, deps); code != 0 || out.Len() != 0 {
		t.Fatal(code, errout.String())
	}
	saved, err := os.ReadFile(output)
	if err != nil || !strings.Contains(string(saved), data) {
		t.Fatal("警告がある HTML を保存しませんでした", err)
	}
	t.Run("合計の上限の警告", func(t *testing.T) {
		var source strings.Builder
		for i := 0; i < 4; i++ {
			name := fmt.Sprintf("limit%d.png", i)
			if err := os.WriteFile(filepath.Join(root, name), large[:8*1024*1024], 0600); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&source, `<img src="%s">`, name)
		}
		source.WriteString(`<img src="good.png">`)
		if err := os.WriteFile(file, []byte(source.String()), 0600); err != nil {
			t.Fatal(err)
		}
		out.Reset()
		errout.Reset()
		if code := Run(t.Context(), append(args, "-o", output), IO{Stdout: &out, Stderr: &errout}, deps); code != 0 || out.Len() != 0 {
			t.Fatal(code, errout.String())
		}
		if !strings.Contains(errout.String(), "32 MiB") || !strings.Contains(errout.String(), "1 件") || strings.Contains(errout.String(), root) {
			t.Fatal("合計の上限を標準エラーへ知らせませんでした", errout.String())
		}
	})
}
