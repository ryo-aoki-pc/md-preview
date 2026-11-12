package verify_test

import (
	"reflect"
	"testing"

	sheetsync "github.com/ryo-aoki-pc/md-table-excel/internal/sync"
	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
	"github.com/ryo-aoki-pc/md-table-excel/internal/verify"
)

func TestKeepHolesAndLegacyVerification(t *testing.T) {
	text := "<table><tr><td>a</td><td>b</td></tr><tr><td>c</td></tr></table>"
	md := table.NewMarkdown("", text, false)
	r := table.FindTables(md)[0]
	repls := []*table.Replacement{{Region: r, Lines: []string{text}, Model: r.Model()}}
	wanted := []string{"表 1: 書き戻した結果が安定しない"}
	if got := sheetsync.Verify(md, text, repls); !reflect.DeepEqual(got, wanted) {
		t.Fatalf("既存の検証結果が変わった: %v", got)
	}
	out := verify.Check(md, text, repls, verify.Options{KeepHoles: true})
	if len(out.Problems) != 0 || out.Markdown == nil || len(out.Regions) != 1 || len(out.Again) != 1 || out.Again[0] != out.Regions[0] {
		t.Fatalf("穴を保つ検証: %+v", out)
	}
}
