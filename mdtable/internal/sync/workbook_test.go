package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ryo-aoki-pc/md-table-excel/internal/version"
	"github.com/xuri/excelize/v2"
)

// sync.export で検証用の文書とブックを用意する。
func exportTestWorkbook(t *testing.T) (string, string) {
	t.Helper()
	directory := t.TempDir()
	markdown := filepath.Join(directory, "document.md")
	book := filepath.Join(directory, "book.xlsx")
	if e := os.WriteFile(markdown, []byte("| h |\n| --- |\n| value |\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := Export(markdown, book, nil, false); e != nil {
		t.Fatal(e)
	}
	return markdown, book
}

// sync.export が管理シートにも共通の版を記録することを確かめる。
func TestSharedVersionInWorkbook(t *testing.T) {
	previous := version.Version
	version.Version = "test-version"
	defer func() { version.Version = previous }()
	_, filename := exportTestWorkbook(t)
	book, e := excelize.OpenFile(filename)
	if e != nil {
		t.Fatal(e)
	}
	defer book.Close()
	value, e := book.GetCellValue("_mdtable", "C1")
	if e != nil || value != "test-version" {
		t.Fatalf("ブックに記録した版 %q、%v", value, e)
	}
}

// sync.import_workbook は os.path.exists と同じく、stat の失敗を「無い」として扱う。
func TestImportMarkdownStatFailure(t *testing.T) {
	_, book := exportTestWorkbook(t)
	path := filepath.Join(t.TempDir(), "invalid\x00.md")
	if _, e := os.Stat(path); e == nil || os.IsNotExist(e) {
		t.Fatalf("存在しない場合以外の stat の失敗を用意できない %v", e)
	}
	_, e := ImportWorkbook(book, path, true, false, false, false, nil)
	want := "書き戻す Markdown が見つかりません（--md で指定してください）: " + path
	if e == nil || e.Error() != want {
		t.Fatalf("エラー %v、期待した文 %q", e, want)
	}
}
