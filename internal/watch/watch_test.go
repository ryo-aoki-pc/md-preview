package watch

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func until(t *testing.T, predicate func() bool) {
	t.Helper()
	end := time.Now().Add(4 * time.Second)
	for time.Now().Before(end) {
		if predicate() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("見張りの通知が時間内に届かない")
}
func TestFile(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a.md")
	_ = os.WriteFile(file, []byte("最初"), 0644)
	var changed, missing atomic.Int32
	stop, done := FileWithDone(file, func() { changed.Add(1) }, Options{OnMissing: func() { missing.Add(1) }})
	t.Cleanup(func() { stop(); <-done })
	_ = os.WriteFile(filepath.Join(root, "other.md"), []byte("別"), 0644)
	time.Sleep(250 * time.Millisecond)
	if changed.Load() != 0 {
		t.Fatal("別ファイルに反応した")
	}
	for _, s := range []string{"2", "3", "最後の変更"} {
		_ = os.WriteFile(file, []byte(s), 0644)
	}
	until(t, func() bool { return changed.Load() == 1 })
	time.Sleep(250 * time.Millisecond)
	if changed.Load() != 1 {
		t.Fatal(changed.Load())
	}
	_ = os.Remove(file)
	until(t, func() bool { return missing.Load() == 1 })
	if changed.Load() != 1 {
		t.Fatal("削除を変更で通知した")
	}
	_ = os.WriteFile(file, []byte("再作成"), 0644)
	until(t, func() bool { return changed.Load() == 2 })
	stop()
	<-done
	_ = os.WriteFile(file, []byte("停止後"), 0644)
	time.Sleep(250 * time.Millisecond)
	if changed.Load() != 2 {
		t.Fatal("停止後に通知した")
	}
}
func TestReplace(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a.md")
	_ = os.WriteFile(file, []byte("最初"), 0644)
	var count atomic.Int32
	stop, done := FileWithDone(file, func() { count.Add(1) }, Options{})
	defer func() { stop(); <-done }()
	next := filepath.Join(root, "next.md")
	_ = os.WriteFile(next, []byte("置き換え"), 0644)
	if e := os.Rename(next, file); e != nil {
		t.Fatal(e)
	}
	until(t, func() bool { return count.Load() == 1 })
	_ = os.WriteFile(file, []byte("置き換え後の変更"), 0644)
	until(t, func() bool { return count.Load() == 2 })
}

// 更新時刻の刻みの中で、同じ大きさに書き換えられても気づく。書き換えられていなければ通知しない。
func TestSameSizeSameTime(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a.md")
	quiet := filepath.Join(root, "b.md")
	_ = os.WriteFile(file, []byte("最初の文"), 0644)
	_ = os.WriteFile(quiet, []byte("変えない"), 0644)
	before, e := os.Stat(file)
	if e != nil {
		t.Fatal(e)
	}
	var count, silent atomic.Int32
	// 最初の確認までに書き換えを終えるよう、確認の間を空ける。
	stop, done := FileWithDone(file, func() { count.Add(1) }, Options{Interval: 400 * time.Millisecond})
	defer func() { stop(); <-done }()
	stopQuiet, doneQuiet := FileWithDone(quiet, func() { silent.Add(1) }, Options{Interval: 400 * time.Millisecond})
	defer func() { stopQuiet(); <-doneQuiet }()
	_ = os.WriteFile(file, []byte("保存の後"), 0644)
	if e := os.Chtimes(file, time.Time{}, before.ModTime()); e != nil {
		t.Fatal(e)
	}
	after, e := os.Stat(file)
	if e != nil || !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() || !os.SameFile(before, after) {
		t.Fatal("同じ大きさ・同じ更新時刻にできない", e)
	}
	until(t, func() bool { return count.Load() == 1 })
	time.Sleep(900 * time.Millisecond)
	if count.Load() != 1 || silent.Load() != 0 {
		t.Fatal(count.Load(), silent.Load())
	}
}
func TestIncludePath(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"../bad.md", ".git/a.md", "a\\b.md"} {
		if p := IncludePath(root, name); p != "" {
			t.Fatalf("拒否漏れ: %s", p)
		}
	}
	if p := IncludePath(root, "new/deep/part.md"); p != filepath.Join(root, "new", "deep", "part.md") {
		t.Fatal(p)
	}
	file := filepath.Join(root, "blocked")
	_ = os.WriteFile(file, nil, 0644)
	if p := IncludePath(root, "blocked/part.md"); p != filepath.Join(root, "blocked", "part.md") {
		t.Fatal(p)
	}
}
