package cli

import (
	"bytes"
	"context"
	"errors"
	"github.com/ryo-aoki-pc/md-preview/internal/pipeline"
	"github.com/ryo-aoki-pc/md-preview/internal/server"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRenderer struct{}

func (fakeRenderer) Render(_ context.Context, text string, _ pipeline.Context) (pipeline.Result, error) {
	return pipeline.Result{HTML: text}, nil
}
func TestParseErrors(t *testing.T) {
	for _, item := range []struct {
		args    []string
		message string
	}{{nil, "Markdown ファイルまたはフォルダーを指定してください"}, {[]string{"--unknown"}, "不明なオプションです: --unknown"}, {[]string{"--port"}, "--port の値を指定してください"}, {[]string{"--port", "-1", "a.md"}, "ポート番号は 0 から 65535 の整数で指定してください"}, {[]string{"--port", "65536", "a.md"}, "ポート番号は 0 から 65535 の整数で指定してください"}, {[]string{"--port", "3.1", "a.md"}, "ポート番号は 0 から 65535 の整数で指定してください"}, {[]string{"a.md", "b.md"}, "文書は1つだけ指定してください"}, {[]string{"--version", "a.md"}, "--help と --version は単独で指定してください"}, {[]string{"--stdio", "--no-open"}, "--stdio は --port・--assets-dir・--no-official-style・--no-plantuml 以外のオプションやファイルと一緒に指定できません"}, {[]string{"render"}, "Markdown ファイルを指定してください"}, {[]string{"render", "--theme", "bad", "a.md"}, "配色は system・light・dark で指定してください"}, {[]string{"render", "--width", "bad", "a.md"}, "本文幅は fluid・fixed で指定してください"}, {[]string{"render", "-o"}, "-o の値を指定してください"}, {[]string{"install-style", "--dir"}, "使い方: mdpreview install-style [--dir <保存先>] [--check]"}} {
		_, e := ParseArgs(item.args)
		if e == nil || e.Error() != item.message {
			t.Errorf("%q: %v", item.args, e)
		}
	}
}
func TestParseSuccess(t *testing.T) {
	for _, args := range [][]string{{"--stdio", "--port", "0", "--assets-dir", "here", "--no-official-style", "--no-plantuml"}, {"--", "-file.md"}, {"--port", "0000", "a.md"}, {"render", "--root", "here", "--theme", "dark", "--width", "fixed", "--no-plantuml", "-o", "out.html", "--", "-file.md"}, {"install-style", "--dir", "here", "--check"}} {
		if _, e := ParseArgs(args); e != nil {
			t.Fatal(args, e)
		}
	}
}
func TestPreviewLifecycle(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "文書.md")
	_ = os.WriteFile(file, []byte("\ufeff最初"), 0644)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var out, errout bytes.Buffer
	started := make(chan *App, 1)
	result := make(chan int, 1)
	go func() {
		result <- Run(ctx, []string{"--no-open", "--root", root, file}, IO{Stdout: &out, Stderr: &errout}, Dependencies{Renderer: fakeRenderer{}, OpenBrowser: func(string, []string) error { t.Error("ブラウザを開いた"); return nil }, OnStart: func(a *App) error { started <- a; return nil }})
	}()
	var a *App
	select {
	case a = <-started:
	case <-ctx.Done():
		t.Fatal("起動しない")
	}
	for _, rel := range []string{"", "_/preview.js"} {
		r, e := http.Get(a.URL + rel)
		if e != nil {
			t.Fatal(e)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatal(r.StatusCode)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		d, _ := a.Session.Last()
		if d != nil && d["html"] == "最初" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("最初を描かない")
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = os.WriteFile(file, []byte("保存後"), 0644)
	for {
		d, _ := a.Session.Last()
		if d != nil && d["html"] == "保存後" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("変更を描かない")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if code := <-result; code != 0 {
		t.Fatal(code)
	}
	if out.String() != "プレビュー: "+a.URL+"\n" || errout.Len() != 0 {
		t.Fatal(out.String(), errout.String())
	}
	if r, e := http.Get(a.URL); e == nil {
		_ = r.Body.Close()
		t.Fatal("終了後も待ち受けた")
	}
}
func TestDirectoryAndBrowser(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "README.md"), []byte("本文"), 0644)
	for _, directory := range []bool{false, true} {
		var out, errout bytes.Buffer
		file := filepath.Join(root, "README.md")
		if directory {
			file = root
		}
		called := false
		code := Run(context.Background(), []string{"--browser", "browser executable", file}, IO{Stdout: &out, Stderr: &errout}, Dependencies{Renderer: fakeRenderer{}, OpenBrowser: func(command string, args []string) error {
			called = true
			if command != "browser executable" || len(args) != 1 {
				t.Fatal(command, args)
			}
			return errors.New("試験用の起動失敗")
		}, OnStart: func(a *App) error {
			if directory {
				r, e := http.Get(a.URL)
				if e != nil {
					return e
				}
				b, _ := io.ReadAll(r.Body)
				_ = r.Body.Close()
				if !strings.Contains(string(b), `"directory":true`) || !strings.Contains(string(b), `"list":true`) || a.Session.HasDocument() {
					t.Fatal(string(b))
				}
			}
			return a.Close()
		}})
		if code != 0 || !called || !strings.Contains(errout.String(), "ブラウザを開けません。") {
			t.Fatal(code, errout.String())
		}
	}
}
func TestHooksAndCommands(t *testing.T) {
	var out, errout bytes.Buffer
	calls := 0
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	deps := Dependencies{CreateDiagram: func(context.Context) (server.DiagramRenderer, error) { calls++; return nil, nil }}
	for _, args := range [][]string{{"render", "a.md"}, {"install-style", "--check"}} {
		if code := Run(context.Background(), args, IO{Stdout: &out, Stderr: &errout}, deps); code != 1 {
			t.Fatal(code)
		}
	}
	if calls != 1 || !strings.Contains(errout.String(), "公式スタイルが未導入または破損しています") {
		t.Fatal(calls, errout.String())
	}
	for platform, want := range map[string]string{"windows": "rundll32", "darwin": "open", "linux": "xdg-open"} {
		cmd, args := BrowserCommand("url", "", platform, nil)
		if cmd != want || len(args) == 0 {
			t.Fatal(cmd, args)
		}
	}
	if cmd, _ := BrowserCommand("url", "", "linux", map[string]string{"TERMUX_VERSION": "1"}); cmd != "termux-open-url" {
		t.Fatal(cmd)
	}
}
