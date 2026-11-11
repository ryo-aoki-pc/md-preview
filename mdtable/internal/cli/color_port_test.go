package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Python: test_cli.DiffColorTest.test_color_diff
func TestPythonColorDiff(t *testing.T) {
	diff := "--- 元\n+++ 新\n@@ -1 +1 @@\n-削除\n+追加\n そのまま\n+末尾"
	want := "\033[1m--- 元\033[0m\n\033[1m+++ 新\033[0m\n\033[36m@@ -1 +1 @@\033[0m\n\033[31m-削除\033[0m\n\033[32m+追加\033[0m\n そのまま\n\033[32m+末尾\033[0m"
	if got := colorDiff(diff); got != want {
		t.Fatalf("差分の色: %q", got)
	}
	if got := colorDiff("+追加\r\n"); got != "\033[32m+追加\033[0m\r\n" {
		t.Fatalf("CRLF の差分の色: %q", got)
	}
}

// Python: test_cli.DiffColorTest.test_pipe_and_no_color_disable_colors
// Python: test_cli.DiffColorTest.test_diff_color_does_not_reach_stderr
// Python: test_cli.CliTest.test_convert_dry_run_uses_colored_diff_writer
func TestPythonDiffOutputStreams(t *testing.T) {
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	defer w.Close()
	for _, value := range []string{"", "1"} {
		t.Setenv("NO_COLOR", value)
		if useColor(w) {
			t.Fatal("パイプに色を付けた")
		}
	}
	source := filepath.Join(t.TempDir(), "文書.md")
	if e = os.WriteFile(source, []byte(pythonDocument), 0600); e != nil {
		t.Fatal(e)
	}
	code, out, stderr := pythonCommand(t, []string{"convert", source, "--table", "1", "--to", "html", "--dry-run"})
	if code != 0 || !strings.Contains(out, "+<table>") || strings.Contains(out, "\033") || strings.Contains(stderr, "\033") || !strings.Contains(stderr, "dry-run") {
		t.Fatalf("差分の出力先: %d %q %q", code, out, stderr)
	}
}

// cli._prepare_streams と textStream.Write の Windows の改行変換を別に確かめる。
func TestPythonWindowsTextStreamNewlines(t *testing.T) {
	f, e := os.CreateTemp(t.TempDir(), "改行")
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	input := "日本語\n既存の CRLF\r\n"
	n, e := (textStream{f}).Write([]byte(input))
	if e != nil || n != len([]byte(input)) {
		t.Fatalf("書き込み: %d %v", n, e)
	}
	data, e := os.ReadFile(f.Name())
	if e != nil {
		t.Fatal(e)
	}
	want := input
	if runtime.GOOS == "windows" {
		want = "日本語\r\n既存の CRLF\r\r\n"
	}
	if string(data) != want {
		t.Fatalf("改行の変換: %q", data)
	}
}

// Python: test_cli.CliTest.test_os_error_message
func TestPythonOSErrorMessage(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("edit の開始時のエラーを確かめる")
	}
	for _, c := range []struct {
		err  error
		want string
	}{{fmt.Errorf("壊れた"), "エラー: 壊れた"}, {&os.PathError{Path: "x.md", Err: fmt.Errorf("見つからない")}, "エラー: x.md: 見つからない"}} {
		r := pythonEdit(t, pythonText(""), nil, func(path, md string) error { return c.err }, context.Background())
		if r.code != 1 || !strings.Contains(r.err, c.want+"\r\n") && !strings.Contains(r.err, c.want+"\n") {
			t.Fatalf("OS のエラー文: %d %q", r.code, r.err)
		}
	}
}
