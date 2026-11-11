// Package pagefiles は埋め込みページと固定情報に一致するライブラリだけを返す。
package pagefiles

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"regexp"
	"strings"

	"github.com/ryo-aoki-pc/md-preview/page"
)

type Options struct {
	Theme, Width    string
	Jump, Directory bool
	Edit            any
}
type Page struct{ HTML, JS, EditGridJS, EditJS, CSS, Fallback string }

func Load(o Options) (Page, error) {
	if o.Theme == "" {
		o.Theme = "system"
	}
	if o.Width == "" {
		o.Width = "fluid"
	}
	config := map[string]any{"appearance": map[string]string{"theme": o.Theme, "width": o.Width}, "assets": nil, "notice": "", "jump": o.Jump, "directory": o.Directory}
	if o.Edit != nil {
		config["edit"] = o.Edit
	}
	p := Page{}
	for name, dest := range map[string]*string{"index.html": &p.HTML, "preview.js": &p.JS, "edit-grid.js": &p.EditGridJS, "edit.js": &p.EditJS, "preview.css": &p.CSS, "fallback.css": &p.Fallback} {
		b, e := page.Files.ReadFile(name)
		if e != nil {
			return p, e
		}
		*dest = string(b)
	}
	b, _ := json.Marshal(config)
	p.HTML = strings.Replace(p.HTML, "{{CONFIG}}", string(b), 1)
	return p, nil
}

var configPattern = regexp.MustCompile(`(<script id="gp-config"[^>]*>)([\s\S]*?)(</script>)`)

func (p Page) Configure(values map[string]any) string {
	indices := configPattern.FindStringSubmatchIndex(p.HTML)
	if indices == nil {
		return p.HTML
	}
	var config map[string]any
	if json.Unmarshal([]byte(p.HTML[indices[4]:indices[5]]), &config) != nil {
		return p.HTML
	}
	for k, v := range values {
		config[k] = v
	}
	b, err := json.Marshal(config)
	if err != nil {
		return p.HTML
	}
	return p.HTML[:indices[4]] + string(b) + p.HTML[indices[5]:]
}

type Record struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type Library struct {
	Body   []byte
	SHA256 string
}
type Libraries struct {
	files   fs.FS
	records map[string]Record
}

var fileName = regexp.MustCompile(`^[a-zA-Z0-9_.-]+(?:/[a-zA-Z0-9_.-]+)*$`)
var hash = regexp.MustCompile(`^[0-9a-f]{64}$`)

// NewLibraries は固定情報だけを読み、本文は利用時に検証する。files は lib/ を根とする。
func NewLibraries(files fs.FS) (*Libraries, error) {
	b, e := fs.ReadFile(files, "lock.json")
	if e != nil {
		return nil, e
	}
	var lock struct {
		Packages map[string]struct {
			Name  string `json:"name"`
			Files map[string]struct {
				SHA256 string `json:"sha256"`
				Size   *int64 `json:"size"`
			} `json:"files"`
		} `json:"packages"`
	}
	if json.Unmarshal(b, &lock) != nil || lock.Packages == nil {
		return nil, errors.New("ライブラリの固定情報が正しくありません")
	}
	l := &Libraries{files: files, records: map[string]Record{}}
	for name, p := range lock.Packages {
		if (name != "katex" && name != "mermaid" && name != "md-table-sticky") || name != p.Name {
			return nil, errors.New("ライブラリの固定情報が正しくありません")
		}
		for file, r := range p.Files {
			if !fileName.MatchString(file) {
				return nil, errors.New("ライブラリのファイル名が正しくありません")
			}
			for _, part := range strings.Split(file, "/") {
				if part == "." || part == ".." {
					return nil, errors.New("ライブラリのファイル名が正しくありません")
				}
			}
			if !hash.MatchString(r.SHA256) || r.Size == nil || *r.Size < 0 || *r.Size > 9007199254740991 {
				return nil, errors.New("ライブラリの固定情報が正しくありません")
			}
			l.records[name+"/"+file] = Record{r.SHA256, *r.Size}
		}
	}
	return l, nil
}

func EmbeddedLibraries() (*Libraries, error) {
	sub, e := fs.Sub(page.Files, "lib")
	if e != nil {
		return nil, e
	}
	return NewLibraries(sub)
}

func (l *Libraries) Read(name string) (Library, bool) {
	r, ok := l.records[name]
	if !ok {
		return Library{}, false
	}
	b, e := fs.ReadFile(l.files, name)
	if e != nil || int64(len(b)) != r.Size {
		return Library{}, false
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != r.SHA256 {
		return Library{}, false
	}
	return Library{b, r.SHA256}, true
}
