package page

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"strings"
	"testing"
)

func TestEmbeddedFiles(t *testing.T) {
	data, err := Files.ReadFile("lib/lock.json")
	if err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Packages map[string]struct {
			Files map[string]struct {
				Size   int    `json:"size"`
				SHA256 string `json:"sha256"`
			} `json:"files"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatal(err)
	}
	if len(lock.Packages) == 0 {
		t.Fatal("資産の固定値にパッケージがない")
	}
	expected := map[string]bool{"lib/lock.json": true}
	for _, name := range []string{"index.html", "preview.js", "edit.js", "preview.css", "fallback.css"} {
		expected[name] = true
		got, err := Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("埋め込んだページが原文と異なる: %s", name)
		}
	}
	for packageName, pkg := range lock.Packages {
		if len(pkg.Files) == 0 {
			t.Errorf("資産の一覧が空: %s", packageName)
		}
		for name, record := range pkg.Files {
			filename := path.Join("lib", packageName, name)
			expected[filename] = true
			data, err := Files.ReadFile(filename)
			if err != nil {
				t.Errorf("埋め込んだ資産を読めない: %s: %v", filename, err)
				continue
			}
			if len(data) != record.Size {
				t.Errorf("資産の大きさ: %s: 取得=%d、期待=%d", filename, len(data), record.Size)
			}
			digest := sha256.Sum256(data)
			if hex.EncodeToString(digest[:]) != record.SHA256 {
				t.Errorf("資産の sha256 が異なる: %s", filename)
			}
		}
	}
	if err := fs.WalkDir(Files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			if strings.HasSuffix(name, ".go") || !expected[name] {
				t.Errorf("予定外の埋め込み: %s", name)
			}
			delete(expected, name)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(expected) != 0 {
		t.Errorf("埋め込みが足りない: %v", expected)
	}
}
