package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ryo-aoki-pc/md-preview/internal/cli"
	h "github.com/ryo-aoki-pc/md-preview/internal/htmltree"
	"github.com/ryo-aoki-pc/md-preview/internal/resolve"
	"golang.org/x/net/html"
)

const fakeSVG = `<svg xmlns="http://www.w3.org/2000/svg"><text>試験の図</text></svg>`

func TestMain(m *testing.M) {
	if os.Getenv("MDPREVIEW_COMMAND_FAKE_CHILD") == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}
	if log := os.Getenv("MDPREVIEW_COMMAND_FAKE_LOG"); log != "" {
		data, _ := io.ReadAll(os.Stdin)
		if os.Getenv("PLANTUML_SECURITY_PROFILE") != "SANDBOX" {
			os.Exit(9)
		}
		record := func(pid int) {
			f, err := os.OpenFile(log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				os.Exit(8)
			}
			_, _ = fmt.Fprintln(f, pid)
			_ = f.Close()
		}
		record(os.Getpid())
		if strings.Contains(string(data), "tree") {
			exe, _ := os.Executable()
			child := exec.Command(exe)
			child.Env = append(os.Environ(), "MDPREVIEW_COMMAND_FAKE_CHILD=1")
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
			if err := child.Start(); err != nil {
				os.Exit(7)
			}
			record(child.Process.Pid)
			for {
				time.Sleep(time.Hour)
			}
		}
		delimiter := ""
		for i, a := range os.Args {
			if a == "-pipedelimitor" && i+1 < len(os.Args) {
				delimiter = os.Args[i+1]
			}
		}
		fmt.Fprint(os.Stdout, fakeSVG+"\n"+delimiter+"\n")
		return
	}
	os.Exit(m.Run())
}

func isolateEnvironment(t *testing.T) string {
	t.Helper()
	root, err := resolve.RealPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"PATH", "LOCALAPPDATA", "USERPROFILE", "HOME", "XDG_DATA_HOME"} {
		t.Setenv(name, root)
	}
	return root
}
func fakeCommand(t *testing.T) (string, string) {
	t.Helper()
	root := isolateEnvironment(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(root, "plantuml")
	body := "#!/bin/sh\n'" + strings.ReplaceAll(exe, "'", `'\''`) + "' \"$@\"\n"
	if runtime.GOOS == "windows" {
		command += ".cmd"
		body = `@"` + exe + `" %*` + "\r\n"
		t.Setenv("PATHEXT", ".CMD")
	}
	if err := os.WriteFile(command, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "calls.txt")
	t.Setenv("MDPREVIEW_COMMAND_FAKE_LOG", log)
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	return root, log
}
func waitCondition(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatal("条件の待ち時間を超えました")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func TestAutomaticPlantumlPreviewAndShutdown(t *testing.T) {
	for _, tree := range []bool{false, true} {
		t.Run(strconv.FormatBool(tree), func(t *testing.T) {
			root, log := fakeCommand(t)
			source := "Alice -> Bob"
			if tree {
				source = "tree"
			}
			file := filepath.Join(root, "doc.md")
			if err := os.WriteFile(file, []byte("```plantuml\n"+source+"\n```\n"), 0600); err != nil {
				t.Fatal(err)
			}
			deps, closeRenderer := dependencies()
			defer closeRenderer()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			started := make(chan *cli.App, 1)
			deps.OnStart = func(a *cli.App) error { started <- a; return nil }
			done := make(chan int, 1)
			go func() {
				done <- cli.Run(ctx, []string{"--no-open", file}, cli.IO{Stdout: io.Discard, Stderr: io.Discard}, deps)
			}()
			var app *cli.App
			select {
			case app = <-started:
			case <-ctx.Done():
				t.Fatal("起動しませんでした")
			}
			defer app.Close()
			var body string
			waitCondition(t, func() bool {
				d, _ := app.Session.Last()
				if d != nil {
					body, _ = d["html"].(string)
				}
				return strings.Contains(body, "/_/plantuml/")
			})
			treeHTML, err := h.Fragment(ctx, body)
			if err != nil {
				t.Fatal(err)
			}
			imgs, _ := h.Elements(ctx, treeHTML, func(n *html.Node) bool { return n.Data == "img" })
			if len(imgs) != 1 {
				t.Fatal("図がありません")
			}
			url := app.URL + strings.TrimPrefix(h.Get(imgs[0], "src"), "/")
			response := make(chan error, 1)
			go func() {
				r, err := http.Get(url)
				if err == nil {
					b, e := io.ReadAll(r.Body)
					_ = r.Body.Close()
					err = e
					if !tree && (r.StatusCode != 200 || string(b) != fakeSVG) {
						err = fmt.Errorf("図の応答: %d", r.StatusCode)
					}
				}
				response <- err
			}()
			var pids []int
			waitCondition(t, func() bool {
				b, _ := os.ReadFile(log)
				pids = nil
				for _, s := range strings.Fields(string(b)) {
					if pid, err := strconv.Atoi(s); err == nil {
						pids = append(pids, pid)
					}
				}
				return len(pids) == 1 && !tree || len(pids) == 2 && tree
			})
			if !tree {
				if err := <-response; err != nil {
					t.Fatal(err)
				}
			}
			if err := app.Close(); err != nil {
				t.Fatal(err)
			}
			if code := <-done; code != 0 {
				t.Fatal(code)
			}
			if tree {
				select {
				case <-response:
				case <-ctx.Done():
					t.Fatal("図の要求が終了しませんでした")
				}
			}
			waitCondition(t, func() bool {
				for _, pid := range pids {
					if childAlive(pid) {
						return false
					}
				}
				return true
			})
		})
	}
}
func TestRealRenderWithAndWithoutPlantuml(t *testing.T) {
	root, log := fakeCommand(t)
	file := filepath.Join(root, "doc.md")
	if err := os.WriteFile(file, []byte("```plantuml\nAlice -> Bob\n```"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, disabled := range []bool{true, false} {
		var out, stderr bytes.Buffer
		args := []string{"render"}
		if disabled {
			args = append(args, "--no-plantuml")
		}
		args = append(args, file)
		if code := run(args, &out, &stderr); code != 0 {
			t.Fatal(code, stderr.String())
		}
		if disabled {
			if _, err := os.Stat(log); !os.IsNotExist(err) {
				t.Fatal("--no-plantuml で実行しました")
			}
			if strings.Contains(out.String(), "data:image/svg+xml;base64,") {
				t.Fatal("無効な図を埋めました")
			}
		} else {
			if !strings.Contains(out.String(), "data:image/svg+xml;base64,"+base64.StdEncoding.EncodeToString([]byte(fakeSVG))) {
				t.Fatal("図を埋めませんでした")
			}
			if strings.Contains(out.String(), "/_/plantuml/") || strings.Contains(out.String(), `href="data:`) {
				t.Fatal("図のリンクが残りました")
			}
		}
	}
}
func TestUnusedEngineForUtilityCommands(t *testing.T) {
	root := isolateEnvironment(t)
	for _, args := range [][]string{{"--help"}, {"--version"}, {"install-style", "--dir", root, "--check"}} {
		deps, closeRenderer := dependencies()
		code := cli.Run(t.Context(), args, cli.IO{Stdout: io.Discard, Stderr: io.Discard}, deps)
		if code > 1 || deps.Renderer.(*commandRenderer).engine != nil {
			t.Fatal("不要なエンジンを準備しました", args)
		}
		closeRenderer()
	}
}

func TestStdioAutomaticPlantuml(t *testing.T) {
	root, _ := fakeCommand(t)
	file := filepath.Join(root, "doc.md")
	source := "```plantuml\nAlice -> Bob\n```"
	if err := os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	deps, closeRenderer := dependencies()
	defer closeRenderer()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	started := make(chan *cli.App, 1)
	deps.OnStart = func(a *cli.App) error { started <- a; return nil }
	done := make(chan int, 1)
	go func() {
		done <- cli.Run(ctx, []string{"--stdio"}, cli.IO{Stdin: input, Stdout: io.Discard, Stderr: io.Discard}, deps)
	}()
	if _, err := io.WriteString(writer, "{\"type\":\"hello\",\"protocol\":1}\n"); err != nil {
		t.Fatal(err)
	}
	var app *cli.App
	select {
	case app = <-started:
	case <-ctx.Done():
		t.Fatal("標準入出力で起動しませんでした")
	}
	message, _ := json.Marshal(map[string]any{"type": "doc", "path": file, "name": "doc.md", "text": source, "rev": 1})
	if _, err := writer.Write(append(message, '\n')); err != nil {
		t.Fatal(err)
	}
	var body string
	waitCondition(t, func() bool {
		d, _ := app.Session.Last()
		if d != nil {
			body, _ = d["html"].(string)
		}
		return strings.Contains(body, "/_/plantuml/")
	})
	tree, err := h.Fragment(ctx, body)
	if err != nil {
		t.Fatal(err)
	}
	imgs, _ := h.Elements(ctx, tree, func(n *html.Node) bool { return n.Data == "img" })
	if len(imgs) != 1 {
		t.Fatal("標準入出力の図がありません")
	}
	r, err := http.Get(app.URL + strings.TrimPrefix(h.Get(imgs[0], "src"), "/"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil || r.StatusCode != 200 || string(b) != fakeSVG {
		t.Fatal("標準入出力の図の応答が違います", err)
	}
	_ = writer.Close()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatal(code)
		}
	case <-ctx.Done():
		t.Fatal("標準入力の終了後も動いています")
	}
}
