package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadLineDoesNotReadAhead(t *testing.T) {
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	if _, e = w.Write([]byte("一行目\r\n二行目\n末尾")); e != nil {
		t.Fatal(e)
	}
	w.Close()
	for _, want := range []string{"一行目\r", "二行目", "末尾"} {
		got, e := readLine(r)
		if e != nil || got != want {
			t.Fatalf("got %q, %v, want %q", got, e, want)
		}
	}
	if _, e = readLine(r); e != io.EOF {
		t.Fatalf("EOF の代わりに %v", e)
	}
}

func TestOwnerFiles(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"~$book.xlsx", ".~lock.book.xlsx#"} {
		path := filepath.Join(directory, name)
		if e := os.WriteFile(path, nil, 0600); e != nil {
			t.Fatal(e)
		}
		got, e := ownerFiles(directory)
		if e != nil || !got {
			t.Fatalf("%s の検出 %t, %v", name, got, e)
		}
		os.Remove(path)
	}
	if e := os.WriteFile(filepath.Join(directory, "book.xlsx"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	if got, e := ownerFiles(directory); e != nil || got {
		t.Fatalf("通常のファイルの検出 %t, %v", got, e)
	}
}

func TestWaitUntilClosedInterrupt(t *testing.T) {
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	defer w.Close()
	previous := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = previous }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e = waitUntilClosed(ctx, t.TempDir(), "unused"); e != context.Canceled {
		t.Fatalf("中断の代わりに %v", e)
	}
}

func TestWaitUntilClosedOwnerDisappears(t *testing.T) {
	directory := t.TempDir()
	xlsx := filepath.Join(directory, "book.xlsx")
	owner := filepath.Join(directory, "~$book.xlsx")
	for _, path := range []string{xlsx, owner} {
		if e := os.WriteFile(path, nil, 0600); e != nil {
			t.Fatal(e)
		}
	}
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	defer w.Close()
	previous := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = previous }()
	go func() { time.Sleep(100 * time.Millisecond); os.Remove(owner) }()
	if e = waitUntilClosed(context.Background(), directory, xlsx); e != nil {
		t.Fatal(e)
	}
}
