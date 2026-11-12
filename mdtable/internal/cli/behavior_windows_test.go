package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func pythonWindowsLock(t *testing.T, path string, directory bool, share uint32) func() {
	t.Helper()
	name, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		t.Fatal(e)
	}
	flags := uint32(0)
	if directory {
		flags = 0x02000000
	}
	handle, e := syscall.CreateFile(name, syscall.GENERIC_READ, share, nil, syscall.OPEN_EXISTING, flags, 0)
	if e != nil {
		t.Fatal(e)
	}
	return func() {
		if e := syscall.CloseHandle(handle); e != nil {
			t.Error(e)
		}
	}
}

// Python: test_cli.CliTest.test_edit_reports_temporary_directory_it_cannot_remove
func TestPythonEditReportsCleanupFailure(t *testing.T) {
	var release func()
	r := pythonEdit(t, pythonText("\ny\n"), nil, func(path, md string) error {
		if e := pythonEditBook(path, "A2", "lazygit2"); e != nil {
			return e
		}
		if e := os.WriteFile(pythonOwner(path), nil, 0600); e != nil {
			return e
		}
		release = pythonWindowsLock(t, filepath.Dir(path), true, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE)
		return nil
	}, context.Background())
	if release != nil {
		release()
	}
	if r.code != 0 || !strings.Contains(r.err, "一時フォルダーを消せませんでした（ブックを閉じてから消してください）:") {
		t.Fatalf("削除できない場所の案内がない: %d %s", r.code, r.err)
	}
}

// Python: test_cli.CliTest.test_edit_waits_for_readable_book_after_owner_disappears
func TestPythonWaitsForReadableWorkbook(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "book.xlsx")
	owner := pythonOwner(path)
	for _, p := range []string{path, owner} {
		if e := os.WriteFile(p, []byte("book"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	release := pythonWindowsLock(t, path, false, 0)
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	w.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	pythonLater(t, 100*time.Millisecond, func() error { return os.Remove(owner) })
	// 書き込みの共有を禁じる実際の Windows ハンドルで、読み取り可能になるのを待つ。
	pythonLater(t, 700*time.Millisecond, func() error { release(); return nil })
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e = waitUntilClosed(ctx, directory, path); e != nil {
		t.Fatal(e)
	}
	if time.Since(started) < 700*time.Millisecond {
		t.Fatal("ブックを読めるようになる前に進んだ")
	}
}

// Python: test_cli.DiffColorTest.test_terminal_enables_colors
// Python: test_cli.DiffColorTest.test_windows_console_mode
func TestPythonWindowsConsoleColors(t *testing.T) {
	if os.Getenv("MDTABLE_TEST_HIDDEN_CONSOLE") != "1" {
		command := exec.Command(os.Args[0], "-test.run=^TestPythonWindowsConsoleColors$")
		command.Env = append(os.Environ(), "MDTABLE_TEST_HIDDEN_CONSOLE=1")
		// CREATE_NO_WINDOW で利用者に画面を出さず、実際のコンソール API を確かめる。
		command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
		out, e := command.CombinedOutput()
		if e != nil {
			t.Fatalf("非表示のコンソールを検査できない: %v %s", e, out)
		}
		return
	}
	console, e := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer console.Close()
	t.Setenv("NO_COLOR", "")
	if !useColor(console) {
		t.Fatal("コンソールの色を有効にできない")
	}
	t.Setenv("NO_COLOR", "1")
	if useColor(console) {
		t.Fatal("NO_COLOR なのに色を付けた")
	}
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	defer w.Close()
	if enableTerminal(w) {
		t.Fatal("コンソールではないハンドルを有効にした")
	}
}
