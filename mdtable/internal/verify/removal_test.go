package verify_test

import (
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/table"
	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/verify"
)

func TestRemovalChecksOtherTablesAndContainers(t *testing.T) {
	source := "前\n\n| 消す |\n| --- |\n| 表 |\n\n後\n\n| 残す |\n| :--- |\n| 値 |\n"
	md := table.NewMarkdown("", source, false)
	r := table.FindTables(md)[0]
	want := "前\n\n後\n\n| 残す |\n| :--- |\n| 値 |\n"
	if out := verify.Removal(md, want, r, 1, 6, 1); len(out.Problems) != 0 {
		t.Fatal("安全な削除を断った", out.Problems)
	}
	for _, changed := range []string{
		strings.Replace(want, "| 値 |", "| 違う値 |", 1),
		strings.Replace(want, "| :--- |", "| ---: |", 1),
		strings.Replace(want, "前\n\n後", "前\n後", 1),
		strings.Replace(want, "後", "# 後", 1),
	} {
		if out := verify.Removal(md, changed, r, 1, 6, 1); len(out.Problems) == 0 {
			t.Fatalf("前後や別の表の変化を受けた: %q", changed)
		}
	}
}

func TestRemovalKeepsUnparsedTables(t *testing.T) {
	prefix := "<table><tfoot><tr><td>前</td></tr></tfoot><tbody><tr><td>本文</td></tr></tbody></table>\n"
	text := prefix + "\n| 消す |\n| --- |\n| 表 |\n"
	md := table.NewMarkdown("", text, false)
	regions := table.FindTables(md)
	if len(regions) < 2 || regions[0].Excluded == "" {
		t.Fatal("対象外の表の見本を読み取れない", regions)
	}
	r := regions[1]
	if out := verify.Removal(md, prefix, r, 1, len(md.Lines), 0); len(out.Problems) != 0 {
		t.Fatal("対象外の表を含む削除", out.Problems)
	}
}
