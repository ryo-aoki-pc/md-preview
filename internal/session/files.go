package session

import (
	"bytes"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/ryo-aoki-pc/md-preview/internal/documents"
	"github.com/ryo-aoki-pc/md-preview/internal/pipeline"
	"github.com/ryo-aoki-pc/md-preview/internal/resolve"
)

const MaxIncludeBytes = 10 * 1024 * 1024

// FileContext は読み込みをルート内の実ファイルに限る。無名文書には関数を渡さない。
func FileContext(root, file, dir string, plantuml bool) pipeline.Context {
	c := pipeline.Context{File: &file, Dir: &dir, Plantuml: plantuml}
	if root == "" {
		return c
	}
	locate := func(name string) (string, bool) {
		r := resolve.Resolve(root, documents.Encode(name))
		if r.Status != 200 {
			return "", false
		}
		st, e := os.Stat(r.Path)
		return r.Path, e == nil && st.Mode().IsRegular()
	}
	c.Exists = func(name string) bool { _, ok := locate(name); return ok }
	c.ReadInclude = func(name string) pipeline.IncludeRead {
		path, ok := locate(name)
		if !ok {
			return pipeline.IncludeRead{Kind: pipeline.IncludeMissing}
		}
		f, e := os.Open(path)
		if e != nil {
			return pipeline.IncludeRead{Kind: pipeline.IncludeNotReadable}
		}
		defer f.Close()
		st, e := f.Stat()
		if e != nil {
			return pipeline.IncludeRead{Kind: pipeline.IncludeNotReadable}
		}
		if st.Size() > MaxIncludeBytes {
			return pipeline.IncludeRead{Kind: pipeline.IncludeTooLarge}
		}
		// stat の後で大きくなっても、上限を超えてメモリーへ載せない。
		b, e := io.ReadAll(io.LimitReader(f, MaxIncludeBytes+1))
		if e != nil {
			return pipeline.IncludeRead{Kind: pipeline.IncludeNotReadable}
		}
		if len(b) > MaxIncludeBytes {
			return pipeline.IncludeRead{Kind: pipeline.IncludeTooLarge}
		}
		if bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b) {
			return pipeline.IncludeRead{Kind: pipeline.IncludeNotReadable}
		}
		return pipeline.IncludeRead{Kind: pipeline.IncludeText, Text: strings.TrimPrefix(string(b), "\ufeff")}
	}
	return c
}
