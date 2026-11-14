package edit

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/internal/resolve"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

func imageFixture(t *testing.T) (*Service, *testHost, []byte) {
	t.Helper()
	root, err := resolve.RealPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(root, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "docs", "main.md")
	if err = os.WriteFile(path, []byte("本文\n"), 0644); err != nil {
		t.Fatal(err)
	}
	h := &testHost{src: session.Source{Render: 1, Root: root, Path: path, DocPath: path, File: "docs/main.md", Text: "本文\n", Current: true, Buf: 7}}
	var body bytes.Buffer
	if err = png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	return New(Options{Host: h, Mode: File}), h, body.Bytes()
}

func TestImageSave(t *testing.T) {
	s, h, body := imageFixture(t)
	for range 2 {
		path, e := s.SaveImage(context.Background(), nil, 1, body)
		if e != nil || !regexp.MustCompile(`^images/img-[0-9]{8}-[0-9]{6}-[0-9a-f]{24}\.png$`).MatchString(path) {
			t.Fatal(path, e)
		}
		if filepath.IsAbs(path) || strings.Contains(path, h.src.Root) {
			t.Fatal("絶対パスを返した", path)
		}
		got, err := os.ReadFile(filepath.Join(filepath.Dir(h.src.Path), filepath.FromSlash(path)))
		if err != nil || !bytes.Equal(got, body) {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(h.src.Path), "images"))
	if err != nil || len(entries) != 2 {
		t.Fatal(entries, err)
	}
	text, _ := os.ReadFile(h.src.Path)
	if string(text) != h.src.Text || h.reloads != 0 || len(s.history.documents) != 0 {
		t.Fatal("Markdown や履歴を変えた")
	}
}

func TestImagePNG(t *testing.T) {
	_, _, body := imageFixture(t)
	dimensions := func(w, h uint32) []byte {
		b := bytes.Clone(body)
		binary.BigEndian.PutUint32(b[16:20], w)
		binary.BigEndian.PutUint32(b[20:24], h)
		binary.BigEndian.PutUint32(b[29:33], crc32.ChecksumIEEE(b[12:29]))
		return b
	}
	for _, c := range []struct {
		name string
		body []byte
		code string
	}{
		{"偽装", []byte("GIF89a"), "image"},
		{"署名だけ", []byte("\x89PNG\r\n\x1a\n"), "image"},
		{"途中で切れた PNG", body[:len(body)-10], "image"},
		{"CRC の誤り", append(bytes.Clone(body[:len(body)-1]), body[len(body)-1]^1), "image"},
		{"幅", dimensions(8193, 1), "dimensions"},
		{"高さ", dimensions(1, 8193), "dimensions"},
		{"画素数", dimensions(4001, 4000), "dimensions"},
		{"本文の上限", make([]byte, MaxImageBytes+1), "large"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if e := validatePNG(c.body); e == nil || e.Code != c.code {
				t.Fatal(e)
			}
		})
	}
	// 上限ちょうどの寸法を実際に符号化し、ヘッダーだけの合否にしない。
	for _, size := range []image.Point{{8192, 1}, {1, 8192}, {4000, 4000}} {
		var b bytes.Buffer
		if err := png.Encode(&b, image.NewGray(image.Rectangle{Max: size})); err != nil {
			t.Fatal(err)
		}
		if e := validatePNG(b.Bytes()); e != nil {
			t.Fatal(size, e)
		}
	}
}

func TestImageRejectDocument(t *testing.T) {
	for _, name := range []string{"描画なし", "古い番号", "次の描画待ち", "無名", "文書なし", "文書の親なし", "ルート外", ".git", "images がファイル", "外の変更", "バッファに名前なし", "別のバッファ", "コメント"} {
		t.Run(name, func(t *testing.T) {
			s, h, body := imageFixture(t)
			render := uint64(1)
			switch name {
			case "描画なし":
				render = 0
			case "古い番号":
				render = 2
			case "次の描画待ち":
				h.src.Stale = true
			case "無名":
				h.src.Path, h.src.File = "", ""
			case "文書なし":
				_ = os.Remove(h.src.Path)
			case "文書の親なし":
				_ = os.RemoveAll(filepath.Dir(h.src.Path))
			case "ルート外":
				h.src.Path = filepath.Join(t.TempDir(), "main.md")
			case ".git":
				_ = os.Mkdir(filepath.Join(h.src.Root, ".git"), 0755)
				h.src.File, h.src.Path = ".git/main.md", filepath.Join(h.src.Root, ".git", "main.md")
				_ = os.WriteFile(h.src.Path, []byte(h.src.Text), 0644)
			case "images がファイル":
				_ = os.WriteFile(filepath.Join(filepath.Dir(h.src.Path), "images"), []byte("保持"), 0644)
			case "外の変更":
				_ = os.WriteFile(h.src.Path, []byte("変更"), 0644)
			case "バッファに名前なし":
				s.opts.Mode, h.src.DocPath = Buffer, ""
				s.opts.Send = func(context.Context, BufferEdit) *Error { return nil }
			case "別のバッファ":
				s.opts.Mode, h.src.Current = Buffer, false
			case "コメント":
				s.opts.Mode = BufferReview
			}
			path, e := s.SaveImage(context.Background(), nil, render, body)
			if e == nil || path != "" || strings.ContainsAny(e.Message, "\r\n") {
				t.Fatal(path, e)
			}
			if name == "古い番号" || name == "次の描画待ち" {
				if e.Status != 409 {
					t.Fatal(e)
				}
			}
		})
	}
}

func TestImageBufferOnlyWritesPNG(t *testing.T) {
	s, h, body := imageFixture(t)
	s.opts.Mode = Buffer
	s.opts.Send = func(context.Context, BufferEdit) *Error {
		t.Fatal("画像の保存でバッファを編集した")
		return nil
	}
	h.src.Text = "未保存の本文\n"
	path, e := s.SaveImage(context.Background(), nil, 1, body)
	if e != nil || path == "" {
		t.Fatal(path, e)
	}
	text, err := os.ReadFile(h.src.Path)
	if err != nil || string(text) != "本文\n" {
		t.Fatal("Markdown を上書きした", err)
	}
}

func TestImageExclusiveNames(t *testing.T) {
	_, h, body := imageFixture(t)
	const first = "img-first.png"
	store := imageStore{name: func() string { return first }}
	if _, e := store.save(h.src, body); e != nil {
		t.Fatal(e)
	}
	if path, e := store.save(h.src, []byte("上書き")); e == nil || path != "" {
		t.Fatal(path, e)
	}
	got, _ := os.ReadFile(filepath.Join(filepath.Dir(h.src.Path), "images", first))
	if !bytes.Equal(got, body) {
		t.Fatal("名前の衝突で上書きした")
	}
	calls := 0
	store.name = func() string {
		calls++
		if calls == 1 {
			return first
		}
		return "img-second.png"
	}
	if path, e := store.save(h.src, body); e != nil || path != "images/img-second.png" || calls != 2 {
		t.Fatal(path, e, calls)
	}
}

func imageLink(t *testing.T, target, link string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
			t.Fatalf("ジャンクションを作れない: %v %s", err, out)
		}
	} else if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func TestImageLinks(t *testing.T) {
	for _, stage := range []string{"作成前から外", "作成前から.git", "作成直前", "作成直後"} {
		t.Run(stage, func(t *testing.T) {
			_, h, body := imageFixture(t)
			outside := t.TempDir()
			inside := filepath.Join(h.src.Root, "safe")
			if err := os.Mkdir(inside, 0755); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(filepath.Dir(h.src.Path), "images")
			target := inside
			if stage == "作成前から外" {
				target = outside
			}
			if stage == "作成前から.git" {
				target = filepath.Join(h.src.Root, ".git")
				if err := os.Mkdir(target, 0755); err != nil {
					t.Fatal(err)
				}
			}
			imageLink(t, target, link)
			store := imageStore{}
			swap := func() {
				if err := os.Remove(link); err != nil {
					t.Fatal(err)
				}
				imageLink(t, outside, link)
			}
			if stage == "作成直前" {
				store.beforeCreate = swap
			}
			if stage == "作成直後" {
				store.afterCreate = swap
			}
			if path, e := store.save(h.src, body); e == nil || path != "" {
				t.Fatal(path, e)
			}
			for _, dir := range []string{outside, inside, target} {
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 0 {
					t.Fatal("拒否した画像を残した", dir, entries, err)
				}
			}
		})
	}
}
