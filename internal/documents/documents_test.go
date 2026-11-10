package documents

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestList(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"z.MD", "a.markdown", "b.mdown", "c.mkd", "d.MKDN", "docs/日本語.md", "docs/z.txt", ".hidden/a.md", ".file.md", "node_modules/a.md"} {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0755)
		_ = os.WriteFile(filepath.Join(root, name), nil, 0644)
	}
	want := []string{"a.markdown", "b.mdown", "c.mkd", "d.MKDN", "docs/日本語.md", "z.MD"}
	out := ListMarkdown(root)
	if !reflect.DeepEqual(out.Files, want) || out.Truncated {
		t.Fatal(out)
	}
	if out := ListMarkdown(""); len(out.Files) != 0 || out.Files == nil || out.Truncated {
		t.Fatal(out)
	}
}
func TestLimits(t *testing.T) {
	root := t.TempDir()
	dir := root
	for i := 0; i < 21; i++ {
		dir = filepath.Join(dir, "d")
		_ = os.Mkdir(dir, 0755)
		_ = os.WriteFile(filepath.Join(dir, "a.md"), nil, 0644)
	}
	out := ListMarkdown(root)
	if len(out.Files) != 20 || !out.Truncated {
		t.Fatal(out)
	}
	out = ListMarkdown(root, Limits{20, 3, 50000})
	if len(out.Files) != 3 || !out.Truncated {
		t.Fatal(out)
	}
	out = ListMarkdown(root, Limits{20, 5000, 1})
	if !out.Truncated {
		t.Fatal(out)
	}
}
func TestEncoding(t *testing.T) {
	if Encode("a%/日本 #?.md") != "a%25/%E6%97%A5%E6%9C%AC%20%23%3F.md" {
		t.Fatal(Encode("a%/日本 #?.md"))
	}
	for _, name := range []string{"a.md", "a.MARKDOWN", "a.mdown", "a.MKD", "a.mkdn"} {
		if !IsMarkdown(name) {
			t.Fatal(name)
		}
	}
	if IsMarkdown("a.md.txt") {
		t.Fatal("拡張子を見誤った")
	}
}

func TestUnicodeOrderAndEncoding(t *testing.T) {
	if Encode("a:+!*.md") != "a%3A%2B!*.md" {
		t.Fatal(Encode("a:+!*.md"))
	}
	root := t.TempDir()
	for _, name := range []string{"\ue000.md", "😀.md"} {
		if e := os.WriteFile(filepath.Join(root, name), nil, 0644); e != nil {
			t.Fatal(e)
		}
	}
	out := ListMarkdown(root)
	if len(out.Files) != 2 || out.Files[0] != "😀.md" {
		t.Fatal(out)
	}
}
