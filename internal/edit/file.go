package edit

import (
	"bytes"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/ryo-aoki-pc/md-tools/internal/documents"
	"github.com/ryo-aoki-pc/md-tools/internal/fileopen"
	"github.com/ryo-aoki-pc/md-tools/internal/resolve"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

const maxDocumentBytes = 16 * 1024 * 1024

func pathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}
func (s *Service) readFile(src session.Source) (string, bool, *Error) {
	if s.opts.Mode == Buffer || s.opts.Mode == BufferReview {
		if s.opts.Mode == Buffer && !src.Current {
			return "", false, fault(403, "excluded", "Neovim で開いている文書だけ編集できます。")
		}
		if s.opts.Mode == Buffer && (src.Buf == nil || s.opts.Send == nil) {
			return "", false, fault(422, "unsupported", "この文書の編集には対応していません。")
		}
		if len(src.Text) > maxDocumentBytes {
			return "", false, fault(422, "large", "文書が 16 MiB を超えています。")
		}
		if !utf8.ValidString(src.Text) {
			return "", false, fault(422, "encoding", "UTF-8 でない文書は編集できません。")
		}
		return src.Text, false, nil
	}
	if s.opts.Mode != File {
		return "", false, fault(422, "unsupported", "この文書の編集には対応していません。")
	}
	return readDisk(src)
}

// readDisk は Resolve で実ファイルを確かめ、文書の上限まで読み取る。
func readDisk(src session.Source) (string, bool, *Error) {
	if src.Path == "" || src.Root == "" {
		return "", false, fault(422, "excluded", "保存先のあるファイルだけ編集できます。")
	}
	r := resolve.Resolve(src.Root, documents.Encode(src.File))
	if r.Status != 200 || pathKey(r.Path) != pathKey(src.Path) {
		return "", false, fault(422, "excluded", "文書の保存先を確かめられません。")
	}
	f, err := fileopen.Open(r.Path)
	if err != nil {
		return "", false, fault(422, "read", "文書を読み直せません。")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return "", false, fault(422, "read", "通常のファイルだけ編集できます。")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxDocumentBytes+4))
	if err != nil {
		return "", false, fault(422, "read", "文書を読み直せません。")
	}
	bom := bytes.HasPrefix(b, []byte{0xef, 0xbb, 0xbf})
	if bom {
		b = b[3:]
	}
	if len(b) > maxDocumentBytes {
		return "", false, fault(422, "large", "文書が 16 MiB を超えています。")
	}
	if !utf8.Valid(b) {
		return "", false, fault(422, "encoding", "UTF-8 でない文書は編集できません。")
	}
	return string(b), bom, nil
}
func (s *Service) editable(src session.Source, text string) *Error {
	if text != src.Text {
		return changed()
	}
	if _, reason := src.Lines.Original(1); reason == "shift" {
		return fault(422, "shift", reasonText(reason))
	}
	return nil
}
func (s *Service) read(src session.Source) (string, bool, *Error) {
	text, bom, e := s.readFile(src)
	if e != nil {
		return text, bom, e
	}
	return text, bom, s.editable(src, text)
}
