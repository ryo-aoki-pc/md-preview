// Package watch は標準ライブラリの定期確認で置き換え保存と出現・削除を見張る。
package watch

import (
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ryo-aoki-pc/md-preview/internal/documents"
	"github.com/ryo-aoki-pc/md-preview/internal/fileopen"
	"github.com/ryo-aoki-pc/md-preview/internal/resolve"
)

// 更新時刻には刻みがあり、同じ刻みの中で同じ大きさに書き換えられると、stat は変わらない。
// 書かれてから racyWindow の内に見たファイルは、中身のハッシュを覚え、刻みが過ぎてから 1 回だけ
// 確かめ直す。読むのは racyMaxBytes までの大きさのファイルに限る。
const (
	racyWindow   = 2 * time.Second
	racyMaxBytes = 32 << 20
)

type observed struct {
	info    os.FileInfo
	sum     [sha256.Size]byte
	recheck time.Time // ゼロでなければ、この時刻を過ぎてから中身を確かめ直す
}

func digest(path string) (sum [sha256.Size]byte, ok bool) {
	f, e := fileopen.Open(path)
	if e != nil {
		return sum, false
	}
	defer f.Close()
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(f, racyMaxBytes+1))
	if e != nil || n > racyMaxBytes {
		return sum, false
	}
	copy(sum[:], h.Sum(nil))
	return sum, true
}

// remember は、書かれた直後に見たファイルの中身のハッシュを添える。
func remember(path string, info os.FileInfo) observed {
	o := observed{info: info}
	if info == nil || info.Size() > racyMaxBytes {
		return o
	}
	if age := time.Since(info.ModTime()); age > -racyWindow && age < racyWindow {
		if sum, ok := digest(path); ok {
			o.sum, o.recheck = sum, info.ModTime().Add(racyWindow)
		}
	}
	return o
}

type Options struct {
	OnMissing          func()
	IsAllowed          func() bool
	Interval, Debounce time.Duration
}

// File の停止関数は通知中にも呼べる。終了まで待つ場合は FileWithDone を使う。
func File(path string, changed func(), opts Options) func() {
	stop, _ := FileWithDone(path, changed, opts)
	return stop
}

// FileWithDone は終了の待ち合わせも返す。停止と待ち合わせを分け、通知との相互待ちを避ける。
func FileWithDone(path string, changed func(), opts Options) (func(), <-chan struct{}) {
	if opts.Interval <= 0 {
		opts.Interval = 100 * time.Millisecond
	}
	if opts.Debounce <= 0 {
		opts.Debounce = 100 * time.Millisecond
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	stat := func() os.FileInfo {
		if opts.IsAllowed != nil && !opts.IsAllowed() {
			return nil
		}
		st, e := os.Stat(path)
		if e != nil || !st.Mode().IsRegular() {
			return nil
		}
		return st
	}
	previous := remember(path, stat())
	go func() {
		defer close(done)
		ticker := time.NewTicker(opts.Interval)
		defer ticker.Stop()
		var due time.Time
		for {
			select {
			case <-stop:
				return
			case now := <-ticker.C:
				next := stat()
				different := (previous.info == nil) != (next == nil)
				if previous.info != nil && next != nil {
					different = !previous.info.ModTime().Equal(next.ModTime()) || previous.info.Size() != next.Size() || !os.SameFile(previous.info, next)
				}
				if different {
					previous = remember(path, next)
				} else if !previous.recheck.IsZero() && !time.Now().Before(previous.recheck) {
					previous.recheck = time.Time{}
					if sum, ok := digest(path); ok && sum != previous.sum {
						different = true
					}
				}
				if different {
					if next == nil {
						due = time.Time{}
						select {
						case <-stop:
							return
						default:
						}
						if opts.OnMissing != nil {
							opts.OnMissing()
						}
					} else {
						due = now.Add(opts.Debounce)
					}
				}
				if !due.IsZero() && !now.Before(due) {
					due = time.Time{}
					select {
					case <-stop:
						return
					default:
					}
					changed()
				}
			}
		}
	}()
	return func() { once.Do(func() { close(stop) }) }, done
}

// IncludePath は無いファイルについても、最も近い実在の親を検査する。
func IncludePath(root, name string) string {
	if root == "" {
		return ""
	}
	parts := []string{}
	for _, p := range strings.Split(name, "/") {
		if p != "" && p != "." {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	r := resolve.Resolve(root, documents.Encode(name))
	if r.Status == 200 {
		return r.Path
	}
	if r.Status != 404 {
		return ""
	}
	for n := len(parts) - 1; n > 0; n-- {
		parent := resolve.Resolve(root, documents.Encode(strings.Join(parts[:n], "/")))
		if parent.Status == 404 {
			continue
		}
		if parent.Status != 200 {
			return ""
		}
		if st, e := os.Stat(parent.Path); e == nil && st.IsDir() {
			return filepath.Join(append([]string{parent.Path}, parts[n:]...)...)
		}
	}
	if st, e := os.Stat(root); e == nil && st.IsDir() {
		return filepath.Join(append([]string{root}, parts...)...)
	}
	return ""
}
