// Package documents は Markdown の拡張子と、安全な文書一覧を扱う。
package documents

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/ryo-aoki-pc/md-tools/internal/resolve"
)

func IsMarkdown(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown", ".mdown", ".mkd", ".mkdn":
		return true
	}
	return false
}

// Encode は区切りごとに名前をエンコードし、文字どおりの % を保持する。
func Encode(name string) string {
	var out strings.Builder
	const hex = "0123456789ABCDEF"
	for _, c := range []byte(name) {
		if c == '/' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.!~*'()", rune(c)) {
			out.WriteByte(c)
		} else {
			out.WriteByte('%')
			out.WriteByte(hex[c>>4])
			out.WriteByte(hex[c&15])
		}
	}
	return out.String()
}

// JavaScript の文字列比較は UTF-16 の符号単位で行う。
func less(a, b string) bool {
	left, right := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(left) && i < len(right); i++ {
		if left[i] != right[i] {
			return left[i] < right[i]
		}
	}
	return len(left) < len(right)
}

type List struct {
	Files     []string `json:"files"`
	Truncated bool     `json:"truncated"`
}
type Limits struct{ MaxDepth, MaxFiles, MaxEntries int }

func ListMarkdown(root string, limits ...Limits) List {
	l := Limits{20, 5000, 50000}
	if len(limits) > 0 {
		l = limits[0]
	}
	out := List{Files: []string{}}
	if root == "" {
		return out
	}
	var walk func(string, string, int)
	walk = func(dir, rel string, depth int) {
		f, err := os.Open(dir)
		if err != nil {
			return
		}
		defer f.Close()
		entries := []os.DirEntry{}
		for {
			batch, e := f.ReadDir(128)
			for _, entry := range batch {
				if len(entries) == l.MaxEntries {
					out.Truncated = true
					break
				}
				entries = append(entries, entry)
			}
			if len(entries) == l.MaxEntries && (len(batch) > 0 || e != io.EOF) {
				extra, _ := f.ReadDir(1)
				if len(extra) > 0 {
					out.Truncated = true
				}
			}
			if e != nil || out.Truncated && len(entries) == l.MaxEntries {
				break
			}
		}
		sort.Slice(entries, func(i, j int) bool {
			a, b := entries[i].Name(), entries[j].Name()
			if entries[i].IsDir() {
				a += "/"
			}
			if entries[j].IsDir() {
				b += "/"
			}
			return less(a, b)
		})
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") || strings.EqualFold(entry.Name(), "node_modules") {
				continue
			}
			name := rel + entry.Name()
			path := filepath.Join(dir, entry.Name())
			info, e := os.Lstat(path)
			if e != nil {
				continue
			}
			if entry.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				if depth >= l.MaxDepth {
					out.Truncated = true
					continue
				}
				walk(path, name+"/", depth+1)
			} else if IsMarkdown(name) {
				r := resolve.Resolve(root, Encode(name))
				if r.Status != 200 {
					continue
				}
				relative, e := filepath.Rel(root, r.Path)
				if e != nil {
					continue
				}
				hidden := false
				for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
					if strings.HasPrefix(part, ".") || strings.EqualFold(part, "node_modules") {
						hidden = true
					}
				}
				if hidden {
					continue
				}
				st, e := os.Stat(r.Path)
				if e != nil || !st.Mode().IsRegular() {
					continue
				}
				if len(out.Files) == l.MaxFiles {
					out.Truncated = true
					return
				}
				out.Files = append(out.Files, name)
			}
			if len(out.Files) == l.MaxFiles && out.Truncated {
				return
			}
		}
	}
	walk(root, "", 0)
	sort.Slice(out.Files, func(i, j int) bool { return less(out.Files[i], out.Files[j]) })
	return out
}
