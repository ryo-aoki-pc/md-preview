package pagefiles

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/ryo-aoki-pc/md-preview/page"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

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
	for _, name := range []string{"katex/../katex.min.js", "katex/%2e%2e/package.json", "lock.json", "katex/package.json", "katex/fonts/KaTeX_Main-Regular.ttf", "katex/unlisted.js"} {
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
