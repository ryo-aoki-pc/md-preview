package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Python: test_oracles.CorpusRoundTripTest.test_no_edit_round_trip
func TestPythonCorpusRoundTrip(t *testing.T) {
	root := os.Getenv("MDTABLE_CORPUS")
	if root == "" {
		t.Skip("MDTABLE_CORPUS が要る")
	}
	e := filepath.WalkDir(root, func(path string, entry os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".claude", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			return nil
		}
		t.Run(path, func(t *testing.T) {
			before, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			directory := t.TempDir()
			md := filepath.Join(directory, "doc.md")
			book := filepath.Join(directory, "doc.tables.xlsx")
			if e = os.WriteFile(md, before, 0600); e != nil {
				t.Fatal(e)
			}
			if _, e = Export(md, book, nil, true); e != nil {
				return
			}
			r, e := ImportWorkbook(book, md, true, false, false, false, nil)
			if e != nil {
				t.Fatal(e)
			}
			if len(r.Errors) > 0 || r.Changed {
				t.Fatalf("無編集の往復で変わった: %v %s", r.Errors, r.Diff)
			}
		})
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}
