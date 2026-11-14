package edit

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/ryo-aoki-pc/md-tools/internal/documents"
	"github.com/ryo-aoki-pc/md-tools/internal/resolve"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

// MaxImageBytes は貼り付ける PNG のバイト列の上限。
const MaxImageBytes = 8 * 1024 * 1024

// SaveImage は描画中の文書の隣へ PNG だけを新しく作る。Markdown と編集履歴は変えない。
func (s *Service) SaveImage(ctx context.Context, doc *string, render uint64, body []byte) (string, *Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.opts.Mode == BufferReview {
		return "", fault(403, "excluded", "コメントの経路からは画像を保存できません。")
	}
	if render == 0 {
		return "", invalid()
	}
	src, e := s.source(doc, render)
	if e != nil {
		return "", e
	}
	if _, _, e = s.read(src); e != nil {
		return "", e
	}
	if s.opts.Mode == Buffer && src.DocPath == "" {
		return "", fault(422, "excluded", "名前のある文書だけ画像を保存できます。")
	}
	if e = validatePNG(body); e != nil {
		return "", e
	}
	if ctx.Err() != nil {
		return "", fault(422, "cancelled", "画像の保存を取り消しました。")
	}
	// PNG 全体の検査中に文書が変わった場合も、前の文書へ画像を作らない。
	if _, e = s.source(doc, render); e != nil {
		return "", e
	}
	if _, _, e = s.read(src); e != nil {
		return "", e
	}
	return (imageStore{}).save(src, body)
}

func validatePNG(body []byte) *Error {
	if len(body) > MaxImageBytes {
		return fault(413, "large", "画像が 8 MiB を超えています。")
	}
	invalid := func() *Error { return fault(422, "image", "正しい PNG の画像だけ保存できます。") }
	if !bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")) {
		return invalid()
	}
	c, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || format != "png" {
		return invalid()
	}
	if c.Width < 1 || c.Height < 1 || c.Width > 8192 || c.Height > 8192 || int64(c.Width)*int64(c.Height) > 16000000 {
		return fault(422, "dimensions", "画像は 1 辺 8192 px・合計 1600 万画素までにしてください。")
	}
	// 寸法の確認だけでは欠けた IDAT や CRC の誤りを見逃すので、全体も検査する。
	if _, err := png.Decode(bytes.NewReader(body)); err != nil {
		return invalid()
	}
	return nil
}

// 試験では名前の衝突と、作成の直前・直後のリンクの差し替えを再現する。
type imageStore struct {
	name         func() string
	beforeCreate func()
	afterCreate  func()
}

func imageName() string {
	var random [12]byte
	_, _ = rand.Read(random[:])
	return "img-" + time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(random[:]) + ".png"
}

func (store imageStore) save(src session.Source, body []byte) (string, *Error) {
	excluded := func() *Error { return fault(422, "excluded", "画像の保存先を確かめられません。") }
	failed := func() *Error { return fault(422, "write", "画像を保存できません。") }
	if src.Root == "" || src.Path == "" || src.File == "" {
		return "", fault(422, "excluded", "名前のある文書だけ画像を保存できます。")
	}
	doc := resolve.Resolve(src.Root, documents.Encode(src.File))
	if doc.Status != 200 || pathKey(doc.Path) != pathKey(src.Path) {
		return "", excluded()
	}
	st, err := os.Stat(doc.Path)
	if err != nil || !st.Mode().IsRegular() {
		return "", excluded()
	}
	// 文書の実際の隣を使い、存在しない親は作らない。Root は作成時のリンクによる脱出も拒む。
	rel, err := filepath.Rel(src.Root, filepath.Dir(doc.Path))
	if err != nil {
		return "", excluded()
	}
	dir := filepath.Join(rel, "images")
	root, err := os.OpenRoot(src.Root)
	if err != nil {
		return "", excluded()
	}
	defer root.Close()
	if err = root.Mkdir(dir, 0755); err != nil && !os.IsExist(err) {
		return "", failed()
	}
	if r := resolve.Resolve(src.Root, documents.Encode(filepath.ToSlash(dir))); r.Status != 200 {
		return "", excluded()
	}
	images, err := root.OpenRoot(dir)
	if err != nil {
		return "", excluded()
	}
	defer images.Close()
	name := store.name
	if name == nil {
		name = imageName
	}
	if store.beforeCreate != nil {
		store.beforeCreate()
	}
	for range 5 {
		filename := name()
		f, err := images.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", failed()
		}
		if store.afterCreate != nil {
			store.afterCreate()
		}
		// 作成後も同じ実ファイルがルート内にあることを確かめ、失敗時は固定した親から消す。
		r := resolve.Resolve(src.Root, documents.Encode(filepath.ToSlash(filepath.Join(dir, filename))))
		created, statErr := f.Stat()
		var actual os.FileInfo
		if r.Status == 200 {
			actual, err = os.Stat(r.Path)
		}
		if r.Status != 200 || statErr != nil || err != nil || actual == nil || !os.SameFile(created, actual) {
			_ = f.Close()
			_ = images.Remove(filename)
			return "", excluded()
		}
		_, writeErr := f.Write(body)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			_ = images.Remove(filename)
			return "", failed()
		}
		// 書いている間に親が動いた場合にも、外のファイルを残さない。
		r = resolve.Resolve(src.Root, documents.Encode(filepath.ToSlash(filepath.Join(dir, filename))))
		actual = nil
		if r.Status == 200 {
			actual, err = os.Stat(r.Path)
		}
		if r.Status != 200 || err != nil || actual == nil || !os.SameFile(created, actual) {
			_ = images.Remove(filename)
			return "", excluded()
		}
		return "images/" + filename, nil
	}
	return "", fault(422, "write", "画像の新しい名前を作れません。")
}
