package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-preview/internal/version"
)

func TestRun(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		code   int
		stdout string
	}{
		{"版", []string{"--version"}, 0, version.Version + "\n"},
		{"使い方", []string{"--help"}, 0, help},
		{"引数なし", nil, 2, ""},
		{"未知の引数", []string{"--unknown"}, 2, ""},
		{"未対応のコマンド", []string{"render", "file.md"}, 2, ""},
		{"未対応のファイル", []string{"file.md"}, 2, ""},
		{"余分な引数", []string{"--version", "file.md"}, 2, ""},
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
			if item.code == 2 && !strings.Contains(stderr.String(), "まだ Go 版にありません。") {
				t.Errorf("未対応の案内がない: %q", &stderr)
			}
		})
	}
}

func TestMainProcess(t *testing.T) {
	// 作業ツリー内にだけ、試験用のバイナリーとビルドの一時ファイルを置く。
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	testRoot := filepath.Join(root, ".tests")
	if err := os.MkdirAll(testRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(testRoot, "go-cli-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
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
	for _, item := range []struct {
		arg    string
		code   int
		stdout string
	}{
		{"--version", 0, "0.2.0-test\n"},
		{"--help", 0, help},
		{"--stdio", 2, ""},
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
			if item.code == 2 && !strings.Contains(stderr.String(), "まだ Go 版にありません。") {
				t.Errorf("実行ファイルから未対応の案内が出ない: %q", &stderr)
			}
			if item.code == 0 && stderr.Len() != 0 {
				t.Errorf("成功時に標準エラーが出た: %q", &stderr)
			}
		})
	}
}
