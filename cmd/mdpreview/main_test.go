package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ryo-aoki-pc/md-preview/internal/version"
)

func TestRun(t *testing.T) {
	isolateEnvironment(t)
	cases := []struct {
		name    string
		args    []string
		code    int
		stdout  string
		message string
	}{
		{"版", []string{"--version"}, 0, version.Version + "\n", ""},
		{"使い方", []string{"--help"}, 0, help, ""},
		{"引数なし", nil, 2, "", "Markdown ファイルまたはフォルダーを指定してください"},
		{"未知の引数", []string{"--unknown"}, 2, "", "不明なオプションです"},
		{"書き出す文書が無い", []string{"render", "--no-plantuml", "file.md"}, 1, "", "指定したファイルを読めません"},
		{"無いファイル", []string{"file.md"}, 2, "", "指定したファイルを読めません"},
		{"余分な引数", []string{"--version", "file.md"}, 2, "", "単独で指定してください"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(item.args, &stdout, &stderr)
			if code != item.code || stdout.String() != item.stdout {
				t.Fatalf("コマンドの応答: code=%d, stdout=%q", code, &stdout)
			}
			if item.code == 0 && stderr.Len() != 0 {
				t.Errorf("成功時に標準エラーが出た: %q", &stderr)
			}
			if item.code != 0 && !strings.Contains(stderr.String(), item.message) {
				t.Errorf("誤りの案内がない: %q", &stderr)
			}
		})
	}
}

func TestMainProcess(t *testing.T) {
	// 試験用のバイナリーとビルドの一時ファイルは t.TempDir で解放する。
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "mdpreview")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-ldflags", "-X github.com/ryo-aoki-pc/md-preview/internal/version.Version=0.2.0-test", "-o", binary, "./cmd/mdpreview")
	build.Dir = root
	build.Env = append(os.Environ(), "GOTMPDIR="+dir)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("コマンドをビルドできない: %v\n%s", err, output)
	}
	isolateEnvironment(t)
	for _, item := range []struct {
		arg    string
		code   int
		stdout string
	}{
		{"--version", 0, "0.2.0-test\n"},
		{"--help", 0, help},
		{"--stdio", 0, ""},
	} {
		t.Run(item.arg, func(t *testing.T) {
			command := exec.Command(binary, item.arg)
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			err := command.Run()
			code := 0
			if err != nil {
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					t.Fatal(err)
				}
				code = exit.ExitCode()
			}
			if code != item.code || stdout.String() != item.stdout {
				t.Fatalf("実行ファイルの応答: code=%d, stdout=%q, stderr=%q", code, &stdout, &stderr)
			}
			if item.code == 0 && stderr.Len() != 0 {
				t.Errorf("成功時に標準エラーが出た: %q", &stderr)
			}
		})
	}
	t.Run("実プロセスの標準入出力", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary, "--stdio", "--no-plantuml")
		input, e := command.StdinPipe()
		if e != nil {
			t.Fatal(e)
		}
		output, e := command.StdoutPipe()
		if e != nil {
			t.Fatal(e)
		}
		var stderr bytes.Buffer
		command.Stderr = &stderr
		if e = command.Start(); e != nil {
			t.Fatal(e)
		}
		defer func() {
			_ = input.Close()
			if command.ProcessState == nil {
				_ = command.Process.Kill()
				_ = command.Wait()
			}
		}()
		_, _ = io.WriteString(input, "{\"type\":\"hello\",\"protocol\":1}\n")
		reader := bufio.NewReader(output)
		line, e := reader.ReadBytes('\n')
		if e != nil {
			t.Fatal(e)
		}
		var ready struct {
			Type, Version, URL string
			Protocol           int
		}
		if json.Unmarshal(line, &ready) != nil || ready.Type != "ready" || ready.Version != "0.2.0-test" || ready.Protocol != 1 {
			t.Fatal(string(line))
		}
		line, e = reader.ReadBytes('\n')
		if e != nil || !bytes.Contains(line, []byte(`"count":0`)) {
			t.Fatal(string(line), e)
		}
		sse, e := http.Get(ready.URL + "_/events")
		if e != nil {
			t.Fatal(e)
		}
		defer sse.Body.Close()
		_, _ = io.WriteString(input, "{\"type\":\"doc\",\"path\":null,\"name\":\"無名\",\"text\":\"原文の本文\",\"rev\":7}\n")
		scanner := bufio.NewScanner(sse.Body)
		rendered := false
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				var d map[string]any
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &d) == nil && d["rev"] == float64(7) {
					// 本物の描画エンジンと描画の流れを通った HTML が届く
					html, _ := d["html"].(string)
					if _, failed := d["error"]; failed || !strings.Contains(html, "原文の本文</p>") || !strings.Contains(html, `data-sourcepos="1:1-1:15"`) {
						t.Fatal(d)
					}
					rendered = true
					break
				}
			}
		}
		if !rendered {
			t.Fatal("描画の結果が届かない")
		}
		_ = input.Close()
		if e = command.Wait(); e != nil {
			t.Fatal(e, stderr.String())
		}
		if stderr.Len() != 0 {
			t.Fatal(stderr.String())
		}
		if r, e := http.Get(ready.URL); e == nil {
			_ = r.Body.Close()
			t.Fatal("標準入力終了後も待ち受けた")
		}
	})
	t.Run("実プロセスのプレビューと割り込み", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "文書.md")
		if e := os.WriteFile(file, []byte("# 本文"), 0644); e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary, "--no-open", "--no-plantuml", file)
		configureInterrupt(command)
		output, e := command.StdoutPipe()
		if e != nil {
			t.Fatal(e)
		}
		var stderr bytes.Buffer
		command.Stderr = &stderr
		if e = command.Start(); e != nil {
			t.Fatal(e)
		}
		defer func() {
			if command.ProcessState == nil {
				_ = command.Process.Kill()
				_ = command.Wait()
			}
		}()
		line, e := bufio.NewReader(output).ReadString('\n')
		if e != nil {
			t.Fatal(e, stderr.String())
		}
		url := strings.TrimSpace(strings.TrimPrefix(line, "プレビュー: "))
		if !strings.HasPrefix(url, "http://127.0.0.1:") {
			t.Fatal(line)
		}
		for _, rel := range []string{"", "_/preview.js"} {
			r, e := http.Get(url + rel)
			if e != nil {
				t.Fatal(e)
			}
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
			if r.StatusCode != 200 {
				t.Fatal(fmt.Sprint(r.StatusCode))
			}
		}
		if e := interruptProcess(command); e != nil {
			t.Fatal(e)
		}
		if e := command.Wait(); e != nil {
			t.Fatal(e, stderr.String())
		}
		if r, e := http.Get(url); e == nil {
			_ = r.Body.Close()
			t.Fatal("割り込み終了後も待ち受けた")
		}
	})
}
