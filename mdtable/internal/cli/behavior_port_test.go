package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/xuri/excelize/v2"
)

const pythonDocument = "# 文書\n\n| ツール | 説明 |\n|---|---|\n| lazygit | `scoop install lazygit` |\n"

type pythonEditResult struct {
	code               int
	out, err, md, book string
}

func pythonEditBook(path, ref string, value any) error {
	book, e := excelize.OpenFile(path)
	if e != nil {
		return e
	}
	defer book.Close()
	if e = book.SetCellValue(book.GetSheetList()[1], ref, value); e != nil {
		return e
	}
	return book.Save()
}

func pythonLater(t *testing.T, delay time.Duration, fn func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { time.Sleep(delay); done <- fn() }()
	t.Cleanup(func() {
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(5 * time.Second):
			t.Error("裏で行うブックの操作が終わらない")
		}
	})
}

func pythonEdit(t *testing.T, input *string, options []string, prepare func(string, string) error, ctx context.Context) pythonEditResult {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("edit は Windows だけで使える")
	}
	t.Setenv("NO_COLOR", "1")
	t.Setenv("MDTABLE_FONT", "BIZ UDゴシック")
	temporary := t.TempDir()
	t.Setenv("TMP", temporary)
	t.Setenv("TEMP", temporary)
	directory := t.TempDir()
	md := filepath.Join(directory, "日本語 の 文書.md")
	if e := os.WriteFile(md, []byte(pythonDocument), 0600); e != nil {
		t.Fatal(e)
	}
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	previous := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = previous; r.Close(); w.Close() })
	if input != nil {
		if *input == "<入力エラー>" {
			r.Close()
		} else {
			if _, e = io.WriteString(w, *input); e != nil {
				t.Fatal(e)
			}
			w.Close()
		}
	}
	result := pythonEditResult{md: md}
	argv := append([]string{"edit", md}, options...)
	result.code, result.out, result.err = pythonInvoke(t, ctx, argv, func(path string) error { result.book = path; return prepare(path, md) })
	return result
}

func pythonText(value string) *string { return &value }
func pythonExists(path string) bool   { _, e := os.Stat(path); return e == nil }
func pythonOwner(path string) string {
	return filepath.Join(filepath.Dir(path), "~$"+filepath.Base(path))
}

// Python: test_cli.CliTest.test_edit_flow
// Python: test_cli.CliTest.test_edit_confirms_import_while_owner_exists
func TestPythonEditFlowAndConfirmation(t *testing.T) {
	r := pythonEdit(t, pythonText("\n Y \n"), nil, func(path, md string) error {
		if e := os.WriteFile(pythonOwner(path), nil, 0600); e != nil {
			return e
		}
		return pythonEditBook(path, "A2", "lazygit2")
	}, context.Background())
	if r.code != 0 || !strings.Contains(r.err, "ブックがまだ開いているようです") || strings.Contains(r.err, "ブックが閉じられたので") {
		t.Fatalf("確かめて取り込めなかった: %d %s", r.code, r.err)
	}
	if pythonExists(filepath.Dir(r.book)) {
		t.Fatal("一時フォルダーが残った")
	}
	data, e := os.ReadFile(r.md)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(data), "| lazygit2 |") {
		t.Fatal(string(data))
	}
}

// Python: test_cli.CliTest.test_edit_imports_when_closed_without_enter
// Python: test_cli.CliTest.test_edit_enter_with_owner_then_closes_automatically
// Python: test_cli.CliTest.test_edit_eof_with_owner_waits_past_grace
// Python: test_cli.CliTest.test_edit_owner_appears_during_eof_grace
// Python: test_cli.CliTest.test_edit_enter_before_excel_opens_waits_for_owner
func TestPythonEditWaitsForOwner(t *testing.T) {
	for _, c := range []struct {
		name          string
		input         *string
		appear, close time.Duration
	}{
		{"入力を待ったまま閉じる", nil, 0, 600 * time.Millisecond},
		{"Enter と n の後に閉じる", pythonText("\nn\n"), 0, 900 * time.Millisecond},
		{"EOF の猶予を過ぎて閉じる", pythonText(""), 0, eofGrace + 600*time.Millisecond},
		{"EOF の間に所有者が現れる", pythonText(""), 400 * time.Millisecond, 1200 * time.Millisecond},
		{"Enter の後で所有者が現れる", pythonText("\n"), 400 * time.Millisecond, 1200 * time.Millisecond},
	} {
		t.Run(c.name, func(t *testing.T) {
			check := func(t *testing.T) {
				r := pythonEdit(t, c.input, nil, func(path, md string) error {
					if e := pythonEditBook(path, "A2", "lazygit2"); e != nil {
						return e
					}
					if c.appear == 0 {
						if e := os.WriteFile(pythonOwner(path), nil, 0600); e != nil {
							return e
						}
					} else {
						pythonLater(t, c.appear, func() error { return os.WriteFile(pythonOwner(path), nil, 0600) })
					}
					pythonLater(t, c.close, func() error { return os.Remove(pythonOwner(path)) })
					return nil
				}, context.Background())
				if r.code != 0 || !strings.Contains(r.err, "ブックが閉じられたので取り込みます") {
					t.Fatalf("閉じた後に取り込めない: %d %s", r.code, r.err)
				}
				if strings.Count(r.err, "ブックがまだ開いているようです") > 1 {
					t.Fatal("確かめの文を繰り返した")
				}
				if pythonExists(filepath.Dir(r.book)) {
					t.Fatal("一時フォルダーが残った")
				}
			}
			if c.close > eofGrace {
				// EOF の猶予時間は実時間を待たず、入力が閉じた状態の仮想時計で検査する。
				synctest.Test(t, check)
			} else {
				check(t)
			}
		})
	}
}

// Python: test_cli.CliTest.test_edit_enter_without_owner_proceeds_after_open_grace
// Python: test_cli.CliTest.test_edit_not_saved
func TestPythonEditOpenGraceAndNotSaved(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		r := pythonEdit(t, pythonText("\n"), nil, func(path, md string) error { return nil }, context.Background())
		if time.Since(started) < openGrace {
			t.Fatal("開くのを待たずに進んだ")
		}
		if r.code != 0 || !strings.Contains(r.err, "保存されていない") {
			t.Fatalf("未保存の扱い: %d %s", r.code, r.err)
		}
		if pythonExists(filepath.Dir(r.book)) {
			t.Fatal("未保存のブックが残った")
		}
	})
}

// Python: test_cli.CliTest.test_edit_eof_without_owner_keeps_workbook
func TestPythonEditEOF(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := pythonEdit(t, pythonText(""), []string{"--keep-kind", "--strict-text"}, func(path, md string) error { return nil }, context.Background())
		if r.code != 1 || !pythonExists(r.book) || !strings.Contains(r.err, "中止しました。ブックを残しました:") || !strings.Contains(r.err, "--keep-kind --strict-text") {
			t.Fatalf("EOF の後に復旧用のブックを残していない: %d %s", r.code, r.err)
		}
	})
}

// Python: test_cli.CliTest.test_edit_input_error_keeps_workbook_and_command
// Python: test_cli.CliTest.test_edit_interrupted_keeps_workbook
// Python: test_cli.CliTest.test_edit_missing_markdown_keeps_workbook_and_command
func TestPythonEditFailuresKeepWorkbook(t *testing.T) {
	for _, name := range []string{"入力エラー", "中断", "Markdown が消えた"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			input := pythonText("\ny\n")
			if name == "入力エラー" {
				input = pythonText("<入力エラー>")
			}
			r := pythonEdit(t, input, []string{"--keep-kind", "--strict-text"}, func(path, md string) error {
				if name == "中断" {
					cancel()
					return nil
				}
				if name == "Markdown が消えた" {
					if e := os.Remove(md); e != nil {
						return e
					}
				}
				return os.WriteFile(pythonOwner(path), nil, 0600)
			}, ctx)
			if r.code != 1 || !pythonExists(r.book) || !strings.Contains(r.err, "ブックを残しました") || !strings.Contains(r.err, "mdtable import") || !strings.Contains(r.err, "--keep-kind --strict-text") {
				t.Fatalf("失敗後の案内: %d %s", r.code, r.err)
			}
		})
	}
}

// Python: test_cli.CliTest.test_edit_export_failure_removes_temporary_directory
// Python: test_cli.CliTest.test_edit_startfile_failure_removes_temporary_directory
func TestPythonEditOpenFailureCleansUp(t *testing.T) {
	r := pythonEdit(t, pythonText(""), nil, func(path, md string) error { return errors.New("開けません") }, context.Background())
	if r.code != 1 || !strings.Contains(r.err, "開けません") || strings.Contains(r.err, "ブックを残しました") || pythonExists(filepath.Dir(r.book)) {
		t.Fatalf("開けないときの後始末: %d %s", r.code, r.err)
	}
	directory := t.TempDir()
	t.Setenv("TMP", directory)
	t.Setenv("TEMP", directory)
	md := filepath.Join(t.TempDir(), "文書.md")
	if e := os.WriteFile(md, []byte("表がない文書\n"), 0600); e != nil {
		t.Fatal(e)
	}
	opened := false
	code, _, stderr := pythonInvoke(t, context.Background(), []string{"edit", md}, func(string) error { opened = true; return nil })
	if code != 1 || opened || !strings.Contains(stderr, "表がありません") {
		t.Fatalf("書き出せないときに開いた: %d %s", code, stderr)
	}
	entries, e := os.ReadDir(directory)
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 0 {
		t.Fatal("一時フォルダーが残った")
	}
}

// Python: test_cli.CliTest.test_edit_dry_run_keeps_diff_and_workbook
// Python: test_cli.CliTest.test_edit_dry_run_without_content_changes_removes_workbook
func TestPythonEditDryRun(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint(changed), func(t *testing.T) {
			r := pythonEdit(t, pythonText("\ny\n"), []string{"--dry-run", "--keep-kind", "--strict-text"}, func(path, md string) error {
				if e := os.WriteFile(pythonOwner(path), nil, 0600); e != nil {
					return e
				}
				if changed {
					return pythonEditBook(path, "A2", "lazygit2")
				}
				book, e := excelize.OpenFile(path)
				if e != nil {
					return e
				}
				defer book.Close()
				if e = book.SetColWidth(book.GetSheetList()[1], "A", "A", 42); e != nil {
					return e
				}
				return book.Save()
			}, context.Background())
			if r.code != 0 {
				t.Fatal(r.err)
			}
			data, e := os.ReadFile(r.md)
			if e != nil {
				t.Fatal(e)
			}
			if string(data) != pythonDocument {
				t.Fatal("dry-run で文書が変わった")
			}
			if changed {
				if !strings.Contains(r.out, "+| lazygit2 |") || !pythonExists(r.book) || !strings.Contains(r.err, "--keep-kind --strict-text") {
					t.Fatal(r.out + r.err)
				}
			} else if r.out != "" || pythonExists(filepath.Dir(r.book)) || !strings.Contains(r.err, "Markdown は変わりません") {
				t.Fatal(r.out + r.err)
			}
		})
	}
}

// Python: test_cli.CliTest.test_edit_forwards_import_protections
func TestPythonEditProtections(t *testing.T) {
	for _, c := range []struct {
		option string
		value  any
		reason string
	}{{"--keep-kind", "```\ncode\n```", "パイプ表のままでは書けない"}, {"--strict-text", 3.1, "Excel が数値に変えたセルがある"}} {
		t.Run(c.option, func(t *testing.T) {
			r := pythonEdit(t, pythonText("\ny\n"), []string{c.option}, func(path, md string) error {
				if e := os.WriteFile(pythonOwner(path), nil, 0600); e != nil {
					return e
				}
				return pythonEditBook(path, "B2", c.value)
			}, context.Background())
			if r.code != 1 || !strings.Contains(r.err, c.reason) || !strings.Contains(r.err, c.option) || !pythonExists(r.book) {
				t.Fatal(r.err)
			}
			data, e := os.ReadFile(r.md)
			if e != nil {
				t.Fatal(e)
			}
			if string(data) != pythonDocument {
				t.Fatal("保護した文書が変わった")
			}
		})
	}
}

// Python: test_cli.CliTest.test_edit_retries_unreadable_workbook_once
// Python: test_cli.CliTest.test_edit_repeated_workbook_error_keeps_workbook
func TestPythonEditWorkbookRetry(t *testing.T) {
	for _, repair := range []bool{false, true} {
		t.Run(fmt.Sprint(repair), func(t *testing.T) {
			r := pythonEdit(t, pythonText("\ny\n"), nil, func(path, md string) error {
				if e := os.WriteFile(pythonOwner(path), nil, 0600); e != nil {
					return e
				}
				if e := pythonEditBook(path, "A2", "lazygit2"); e != nil {
					return e
				}
				valid, e := os.ReadFile(path)
				if e != nil {
					return e
				}
				if e = os.WriteFile(path, []byte("壊れたブック"), 0600); e != nil {
					return e
				}
				if repair {
					pythonLater(t, 700*time.Millisecond, func() error { return os.WriteFile(path, valid, 0600) })
				}
				return nil
			}, context.Background())
			if repair {
				if r.code != 0 || pythonExists(filepath.Dir(r.book)) {
					t.Fatal(r.err)
				}
			} else if r.code != 1 || !pythonExists(r.book) || !strings.Contains(r.err, "mdtable import") {
				t.Fatal(r.err)
			}
		})
	}
}

// Python: test_cli.CliTest.test_edit_needs_windows
func TestPythonEditRequiresWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 以外で拒否を確かめる")
	}
	_, e := cmdEdit(context.Background(), args{markdown: "unused"}, func(string) error { t.Fatal("Windows 以外で開いた"); return nil })
	if e == nil || !strings.Contains(e.Error(), "Windows") {
		t.Fatalf("環境の制限: %v", e)
	}
}

// Python: test_cli.CliTest.test_read_line_from_pipe_reads_the_descriptor
func TestPythonReadLineFromPipe(t *testing.T) {
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	if _, e = io.WriteString(w, "あ y\r\n次の行\n終わり"); e != nil {
		t.Fatal(e)
	}
	w.Close()
	for _, want := range []string{"あ y", "次の行", "終わり"} {
		got, e := readLine(r)
		got = strings.TrimSuffix(got, "\r")
		if e != nil || got != want {
			t.Fatalf("行の読み取り: %q %v", got, e)
		}
	}
	if _, e = readLine(r); e != io.EOF {
		t.Fatalf("末尾が EOF ではない: %v", e)
	}
}

// Python: test_cli.CliTest.test_edit_ends_normally_with_open_stdin_pipe
func TestPythonEditPipeProcess(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("edit は Windows だけで使える")
	}
	if os.Getenv("MDTABLE_TEST_OPEN_PIPE") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPythonEditPipeProcess$")
		command.Env = append(os.Environ(), "MDTABLE_TEST_OPEN_PIPE=1")
		stdin, e := command.StdinPipe()
		if e != nil {
			t.Fatal(e)
		}
		defer stdin.Close()
		output, e := command.CombinedOutput()
		if e != nil {
			t.Fatalf("入力が開いたままの子プロセスが終わらない: %v %s", e, output)
		}
		return
	}
	directory := t.TempDir()
	md := filepath.Join(directory, "文書.md")
	if e := os.WriteFile(md, []byte(pythonDocument), 0600); e != nil {
		t.Fatal(e)
	}
	code, _, stderr := pythonInvoke(t, context.Background(), []string{"edit", md}, func(path string) error {
		if e := pythonEditBook(path, "A2", "lazygit2"); e != nil {
			return e
		}
		if e := os.WriteFile(pythonOwner(path), nil, 0600); e != nil {
			return e
		}
		pythonLater(t, 600*time.Millisecond, func() error { return os.Remove(pythonOwner(path)) })
		return nil
	})
	if code != 0 || !strings.Contains(stderr, "ブックが閉じられたので取り込みます") {
		t.Fatalf("子プロセスの取り込み: %d %s", code, stderr)
	}
}
