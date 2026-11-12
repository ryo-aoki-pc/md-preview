package sticky

import (
	"bytes"
	"os"
	"testing"
)

func TestEmbeddedSource(t *testing.T) {
	if len(Source) == 0 {
		t.Fatal("埋め込んだ原文が空です")
	}
	source, err := os.ReadFile("src/sticky-table.js")
	if err != nil {
		t.Fatal(err)
	}
	if Source != string(source) {
		t.Fatal("埋め込んだ原文が元の場所と違います")
	}
}

func TestLicense(t *testing.T) {
	license, err := os.ReadFile("LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.ReadFile("../LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(license, root) {
		t.Fatal("ルートのライセンスと異なります")
	}
}
