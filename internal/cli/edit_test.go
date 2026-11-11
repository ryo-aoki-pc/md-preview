package cli

import (
	"strings"
	"testing"
)

func TestEditArguments(t *testing.T) {
	for _, args := range [][]string{{"--edit", "main.md"}, {"--edit", "folder"}} {
		o, e := ParseArgs(args)
		if e != nil || !o.Edit {
			t.Fatal(o, e)
		}
	}
	for _, args := range [][]string{{"--stdio", "--edit"}, {"render", "--edit", "main.md"}, {"install-style", "--edit"}} {
		if _, e := ParseArgs(args); e == nil {
			t.Fatal("使えない組み合わせを許した", args)
		}
	}
	if !strings.Contains(Help, "[--edit]") || !strings.Contains(Help, "プレビューの画面で編集して、Markdown のファイルへ書く") {
		t.Fatal("編集の案内が無い")
	}
}
