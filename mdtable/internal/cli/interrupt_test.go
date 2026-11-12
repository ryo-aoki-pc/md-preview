package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mdsync "github.com/ryo-aoki-pc/md-table-excel/internal/sync"
	"github.com/xuri/excelize/v2"
)

func captureOutput(t *testing.T, run func() int) (int, string, string) {
	t.Helper()
	directory := t.TempDir()
	out, e := os.Create(filepath.Join(directory, "stdout"))
	if e != nil {
		t.Fatal(e)
	}
	defer out.Close()
	err, e := os.Create(filepath.Join(directory, "stderr"))
	if e != nil {
		t.Fatal(e)
	}
	defer err.Close()
	previousOut, previousErr := stdout, stderr
	stdout, stderr = textStream{out}, textStream{err}
	defer func() { stdout, stderr = previousOut, previousErr }()
	code := run()
	output, e := os.ReadFile(out.Name())
	if e != nil {
		t.Fatal(e)
	}
	errors, e := os.ReadFile(err.Name())
	if e != nil {
		t.Fatal(e)
	}
	return code, strings.ReplaceAll(string(output), "\r\n", "\n"), strings.ReplaceAll(string(errors), "\r\n", "\n")
}

func TestMainCanceled(t *testing.T) {
	for _, command := range []string{"list", "export", "import", "convert"} {
		t.Run(command, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			code, out, err := captureOutput(t, func() int { return mainContext(ctx, []string{command, "unused"}, nil) })
			if code != 1 || out != "" || err != "中止しました\n" {
				t.Fatalf("終了 %d、標準出力 %q、標準エラー %q", code, out, err)
			}
		})
	}
}

func TestEditInterruptMessage(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("edit のコマンドは Windows 向け")
	}
	for _, options := range [][]string{nil, {"--keep-kind", "--strict-text"}} {
		t.Run(strings.Join(options, " "), func(t *testing.T) {
			directory := t.TempDir()
			markdown := filepath.Join(directory, "document.md")
			original := []byte("| a | b |\n| --- | --- |\n| x | y |\n")
			if e := os.WriteFile(markdown, original, 0600); e != nil {
				t.Fatal(e)
			}
			input, writer, e := os.Pipe()
			if e != nil {
				t.Fatal(e)
			}
			defer input.Close()
			defer writer.Close()
			previous := os.Stdin
			os.Stdin = input
			defer func() { os.Stdin = previous }()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			xlsx := ""
			open := func(path string) error {
				xlsx = path
				t.Cleanup(func() { os.RemoveAll(filepath.Dir(path)) })
				if e := os.WriteFile(filepath.Join(filepath.Dir(path), "~$book.xlsx"), nil, 0600); e != nil {
					return e
				}
				timer := time.AfterFunc(20*time.Millisecond, cancel)
				t.Cleanup(func() { timer.Stop() })
				return nil
			}
			argv := append([]string{"edit", markdown}, options...)
			code, out, err := captureOutput(t, func() int { return mainContext(ctx, argv, open) })
			optionText := ""
			if len(options) > 0 {
				optionText = " " + strings.Join(options, " ")
			}
			want := fmt.Sprintf("表 1（1〜3 行）→ シート「表1」\nExcel で編集して保存し、ブックを閉じると自動で書き戻します（Enter でも進めます。中止は Ctrl+C）\n\n中止しました。ブックを残しました: %s\n取り込むときは: mdtable import \"%s\" --md \"%s\"%s\n", xlsx, xlsx, markdown, optionText)
			if code != 1 || out != "" || err != want {
				t.Fatalf("終了 %d、標準出力 %q、標準エラー\n%s\n期待した標準エラー\n%s", code, out, err, want)
			}
			if _, e := os.Stat(xlsx); e != nil {
				t.Fatalf("残したブックを読めない %v", e)
			}
			after, e := os.ReadFile(markdown)
			if e != nil || !bytes.Equal(after, original) {
				t.Fatalf("中断で Markdown が変わった %v", e)
			}
			assertNoTempFiles(t, filepath.Dir(xlsx))
		})
	}
}

// 実際に書かれた一時ファイルが現れたところで Ctrl+C 相当の取り消しを送る。
type cancelAfterTempWrite struct {
	context.Context
	cancel    context.CancelFunc
	directory string
	observed  atomic.Bool
}

func (c *cancelAfterTempWrite) Err() error {
	names, _ := filepath.Glob(filepath.Join(c.directory, ".mdtable-*"))
	for _, name := range names {
		if info, e := os.Stat(name); e == nil && info.Size() > 0 {
			c.observed.Store(true)
			c.cancel()
		}
	}
	return c.Context.Err()
}

func assertNoTempFiles(t *testing.T, directory string) {
	t.Helper()
	names, e := filepath.Glob(filepath.Join(directory, ".mdtable-*"))
	if e != nil || len(names) != 0 {
		t.Fatalf("一時ファイルが残った %v、%v", names, e)
	}
}

func TestCommandInterruptBeforeReplace(t *testing.T) {
	for _, command := range []string{"export", "convert", "import"} {
		t.Run(command, func(t *testing.T) {
			directory := t.TempDir()
			markdown := filepath.Join(directory, "document.md")
			xlsx := filepath.Join(directory, "book.xlsx")
			if e := os.WriteFile(markdown, []byte("| a | b |\n| --- | --- |\n| x | y |\n"), 0600); e != nil {
				t.Fatal(e)
			}
			target := markdown
			argv := []string{"convert", markdown, "--table", "1", "--to", "html"}
			if command == "export" {
				target = xlsx
				argv = []string{"export", markdown, "-o", xlsx, "--force"}
				if e := os.WriteFile(xlsx, []byte("元のブック"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if command == "import" {
				if _, e := mdsync.Export(markdown, xlsx, nil, false); e != nil {
					t.Fatal(e)
				}
				book, e := excelize.OpenFile(xlsx)
				if e != nil {
					t.Fatal(e)
				}
				if e = book.SetCellStr(book.GetSheetList()[1], "A1", "changed"); e == nil {
					e = book.Save()
				}
				closeError := book.Close()
				if e != nil || closeError != nil {
					t.Fatalf("編集したブックの保存 %v、%v", e, closeError)
				}
				argv = []string{"import", xlsx, "--md", markdown}
			}
			before, e := os.ReadFile(target)
			if e != nil {
				t.Fatal(e)
			}
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := &cancelAfterTempWrite{Context: parent, cancel: cancel, directory: directory}
			code, out, err := captureOutput(t, func() int { return mainContext(ctx, argv, nil) })
			if code != 1 || out != "" || err != "中止しました\n" {
				t.Fatalf("終了 %d、標準出力 %q、標準エラー %q", code, out, err)
			}
			if !ctx.observed.Load() {
				t.Fatal("一時ファイルを書いた経路を通っていない")
			}
			after, e := os.ReadFile(target)
			if e != nil || !bytes.Equal(after, before) {
				t.Fatalf("置換の前の中断で元のファイルが変わった %v", e)
			}
			assertNoTempFiles(t, directory)
		})
	}
}
