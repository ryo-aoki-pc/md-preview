package notices

import (
	"context"
	"strings"
	"testing"

	t "github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/include"
)

func TestReadOnly(tst *testing.T) {
	ctx := context.Background()
	root, err := t.Fragment(ctx, `<p data-sourcepos="1:1-1:34">#123 :tanuki: <img src="x.png"></p>`)
	if err != nil {
		tst.Fatal(err)
	}
	before, _ := t.HTML(root)
	expansion, err := include.Expand(ctx, "#123 :tanuki: ![x](x.png)", "", nil)
	if err != nil {
		tst.Fatal(err)
	}
	found, err := Collect(ctx, root, "#123 :tanuki: ![x](x.png)", expansion, Context{Exists: func(string) bool { return false }})
	if err != nil || len(found) != 3 {
		tst.Fatal(found, err)
	}
	after, _ := t.HTML(root)
	if before != after {
		tst.Fatal("木を書き換えました。")
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Collect(ctx, root, "", expansion, Context{}); err != context.Canceled {
		tst.Fatal(err)
	}
}
func TestASCIISchemeAndEmoji(tst *testing.T) {
	ctx := context.Background()
	root, err := t.Fragment(ctx, `<p data-sourcepos="1:1-1:30">:K: :ſ: <img src="%E2%84%AA:x.png"></p>`)
	if err != nil {
		tst.Fatal(err)
	}
	source := `:K: :ſ: ![x](%E2%84%AA:x.png)`
	expansion, err := include.Expand(ctx, source, "", nil)
	if err != nil {
		tst.Fatal(err)
	}
	asked := false
	found, err := Collect(ctx, root, source, expansion, Context{Exists: func(path string) bool {
		asked = true
		if path != "K:x.png" {
			tst.Fatal(path)
		}
		return false
	}})
	if err != nil || !asked || len(found) != 1 || found[0].Kind != "missing" {
		tst.Fatal(found, err)
	}
}
func TestReferences(tst *testing.T) {
	for _, value := range []string{"#123", "!123", "&123", "@alice", "@group/sub", "~bug", `~"multi word"`, "~不具合", "%v1.0", `%"release 1"`, "group/project#123", "group/sub/project!123", "group/project$1", `group/project~"multi word"`, "[issue:123]", "[merge_request:123]", "[epic:123]", "[snippet:123]"} {
		matches, err := referenceMatches(context.Background(), "前 "+value+" 後")
		if err != nil || len(matches) != 1 {
			tst.Fatal(value, matches, err)
		}
	}
	for _, source := range []string{strings.Repeat("a", 200000), strings.Repeat("a/", 100000), strings.Repeat("a.b-", 50000) + "c", strings.Repeat(`~"`, 50000), strings.Repeat("a", 200000) + " #1"} {
		root := t.Root(t.Element("p", t.Attr("data-sourcepos", "1:1-1:200010"), t.Text(source)))
		e, _ := include.Expand(context.Background(), source, "", nil)
		found, err := Collect(context.Background(), root, source, e, Context{})
		if err != nil {
			tst.Fatal(err)
		}
		if strings.HasSuffix(source, " #1") && (len(found) != 1 || found[0].Text != "#1") {
			tst.Fatal(found)
		}
	}
}
