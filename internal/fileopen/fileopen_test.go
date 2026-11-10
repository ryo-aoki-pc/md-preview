package fileopen

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAndReadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "文書.md")
	write(t, path, "本文")
	b, err := ReadFile(path)
	if err != nil || string(b) != "本文" {
		t.Fatal(string(b), err)
	}
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() != int64(len("本文")) {
		t.Fatal(st, err)
	}
	_ = f.Close()
	if _, err := Open(filepath.Join(dir, "無い.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if _, err := ReadFile(filepath.Join(dir, "無い", "文書.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

// 読んでいる間も、保存の入れ替え (別名への移動・削除) ができる。
// 上書きの移動は、Windows では、どの開き方で読んでいる間も OS が拒むので、ほかの OS でだけ確かめる。
func TestReplaceWhileOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "文書.md")
	backup := filepath.Join(dir, "文書.md~")
	next := filepath.Join(dir, "文書.md.tmp")
	write(t, path, "古い")
	if runtime.GOOS == "windows" {
		// この部品が要る理由。標準の開き方が入れ替えを許すようになったら、ここで知らせる。
		std, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		err = os.Rename(path, backup)
		_ = std.Close()
		if err == nil {
			t.Fatal("標準の os.Open でも、開いている間に名前を変えられた。この部品は不要になった。")
		}
	}
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := os.Rename(path, backup); err != nil {
		t.Fatal("別名への移動:", err)
	}
	write(t, path, "新しい")
	if b, err := io.ReadAll(f); err != nil || string(b) != "古い" {
		t.Fatal(string(b), err)
	}
	_ = f.Close()
	f, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if runtime.GOOS != "windows" {
		write(t, next, "さらに新しい")
		if err := os.Rename(next, path); err != nil {
			t.Fatal("上書きの移動:", err)
		}
		_ = f.Close()
		if b, err := ReadFile(path); err != nil || string(b) != "さらに新しい" {
			t.Fatal(string(b), err)
		}
		f, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
	}
	if err := os.Remove(path); err != nil {
		t.Fatal("削除:", err)
	}
}
