package table

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestWindowsPermissionErrors(t *testing.T) {
	for _, code := range []syscall.Errno{5, 32, 33} {
		e := &os.PathError{Op: "open", Path: "book.xlsx", Err: code}
		if !IsPermissionError(e) {
			t.Errorf("%d が権限エラーにならない", code)
		}
	}
	if IsPermissionError(syscall.Errno(2)) {
		t.Fatal("存在しないファイルが権限エラーになった")
	}
}

func TestReplaceInterruptDuringRetry(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "temporary")
	target := filepath.Join(directory, "original")
	if e := os.WriteFile(source, []byte("new"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(target, []byte("old"), 0600); e != nil {
		t.Fatal(e)
	}
	name, e := syscall.UTF16PtrFromString(target)
	if e != nil {
		t.Fatal(e)
	}
	handle, e := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer syscall.CloseHandle(handle)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(30*time.Millisecond, cancel)
	defer timer.Stop()
	if e = ReplaceWithRetryContext(ctx, source, target, 5); !errors.Is(e, context.Canceled) {
		t.Fatalf("取り消しの代わりに %v", e)
	}
	data, e := os.ReadFile(target)
	if e != nil || string(data) != "old" {
		t.Fatalf("再試行中の中断で元のファイルが変わった %q、%v", data, e)
	}
}
