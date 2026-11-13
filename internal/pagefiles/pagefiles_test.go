package pagefiles

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ryo-aoki-pc/md-tools/page"
	"github.com/ryo-aoki-pc/md-tools/sticky"
)

func TestReviewConfig(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		p, e := Load(Options{Review: enabled})
		if e != nil {
			t.Fatal(e)
		}
		m := configPattern.FindStringSubmatch(p.HTML)
		var config map[string]any
		if len(m) != 4 || json.Unmarshal([]byte(m[2]), &config) != nil {
			t.Fatal("設定を読めない")
		}
		value, present := config["review"]
		if present != enabled || enabled && value != true {
			t.Fatal(config)
		}
		if _, present := config["edit"]; present {
			t.Fatal("コメントだけで編集が有効になった")
		}
	}
}

func TestEmbedded(t *testing.T) {
	libs, e := EmbeddedLibraries()
	if e != nil {
		t.Fatal(e)
	}
	if len(libs.records) < 20 {
		t.Fatal("固定情報を読めない")
	}
	for name := range libs.records {
		found, ok := libs.Read(name)
		if !ok || len(found.Body) == 0 {
			t.Fatal(name)
		}
	}
	found, ok := libs.Read("md-table-sticky/sticky-table.js")
	if !ok || !bytes.Equal(found.Body, []byte(sticky.Source)) {
		t.Fatal("表の固定の原文と異なります")
	}
	for _, name := range []string{"katex/../katex.min.js", "katex/%2e%2e/package.json", "lock.json", "katex/package.json", "katex/fonts/KaTeX_Main-Regular.ttf", "katex/unlisted.js", "md-table-sticky/LICENSE", "md-table-sticky/package.json", "md-table-sticky/unlisted.js"} {
		if _, ok := libs.Read(name); ok {
			t.Fatal(name)
		}
	}
	if _, e := fs.Stat(page.Files, "embed.go"); e == nil {
		t.Fatal("Goのソースを埋め込んだ")
	}
}
func TestTamper(t *testing.T) {
	sum := sha256.Sum256([]byte("good"))
	record := Record{hex.EncodeToString(sum[:]), 4}
	lock, _ := json.Marshal(map[string]any{"packages": map[string]any{"katex": map[string]any{"name": "katex", "files": map[string]Record{"a.js": record}}}})
	files := fstest.MapFS{"lock.json": &fstest.MapFile{Data: lock}, "katex/a.js": &fstest.MapFile{Data: []byte("evil")}}
	libs, e := NewLibraries(files)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := libs.Read("katex/a.js"); ok {
		t.Fatal("同じ長さの改変を見逃した")
	}
	files["katex/a.js"].Data = []byte("good")
	if _, ok := libs.Read("katex/a.js"); !ok {
		t.Fatal("正しい本文を拒否した")
	}
	delete(files, "katex/a.js")
	if _, ok := libs.Read("katex/a.js"); ok {
		t.Fatal("不足を見逃した")
	}
}
func TestEmbeddedStickyTamper(t *testing.T) {
	libs, err := EmbeddedLibraries()
	if err != nil {
		t.Fatal(err)
	}
	const name = "md-table-sticky/sticky-table.js"
	original := libs.embedded[name]
	libs.embedded[name] = append([]byte(nil), original...)
	libs.embedded[name][0] ^= 1
	if _, ok := libs.Read(name); ok {
		t.Fatal("埋め込んだ原文の同じ長さの改変を見逃した")
	}
	libs.embedded[name] = original[:len(original)-1]
	if _, ok := libs.Read(name); ok {
		t.Fatal("埋め込んだ原文の大きさの違いを見逃した")
	}
}

func TestRejectStickyLock(t *testing.T) {
	lock, _ := json.Marshal(map[string]any{"packages": map[string]any{"md-table-sticky": map[string]any{"name": "md-table-sticky", "files": map[string]Record{}}}})
	files := fstest.MapFS{"lock.json": &fstest.MapFile{Data: lock}}
	if _, err := NewLibraries(files); err == nil {
		t.Fatal("表の固定をライブラリの固定情報に含めました")
	}
}

func TestPageConfig(t *testing.T) {
	p, e := Load(Options{Theme: "dark", Width: "fixed", Jump: true})
	if e != nil {
		t.Fatal(e)
	}
	html := p.Configure(map[string]any{"notice": "</script><script>危険</script>", "hasDocument": true})
	if strings.Contains(html, "{{CONFIG}}") || strings.Contains(html, "<script>危険") || !strings.Contains(html, `"hasDocument":true`) {
		t.Fatal("設定の埋め込みが正しくない")
	}
}

func TestPageScripts(t *testing.T) {
	p, err := Load(Options{})
	if err != nil {
		t.Fatal(err)
	}
	previous := -1
	for _, script := range []struct{ name, body string }{
		{"preview.js", p.JS}, {"copy-code.js", p.CopyCodeJS}, {"edit-grid.js", p.EditGridJS}, {"edit-targets.js", p.EditTargetsJS},
		{"edit.js", p.EditJS}, {"review.js", p.ReviewJS},
	} {
		index := strings.Index(p.HTML, `<script src="_/`+script.name+`" defer></script>`)
		if index <= previous || script.body == "" {
			t.Fatal("スクリプトが無いか、読み込む順が違う", script.name)
		}
		previous = index
	}
}

func TestEditConfig(t *testing.T) {
	for _, c := range []struct {
		value any
		want  string
	}{{nil, ""}, {false, `"edit":false`}, {map[string]any{"scope": "all", "undo": true}, `"edit":{"scope":"all","undo":true}`}} {
		p, err := Load(Options{Edit: c.value})
		if err != nil {
			t.Fatal(err)
		}
		if c.want == "" {
			if strings.Contains(p.HTML, `"edit":`) {
				t.Fatal("編集を指定せず設定が増えた")
			}
		} else if !strings.Contains(p.HTML, c.want) {
			t.Fatal("編集の設定が無い", c.want)
		}
	}
}
