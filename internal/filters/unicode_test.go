package filters

import (
	t "github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"strings"
	"testing"
)

func TestUTF16AttributeLimit(tst *testing.T) {
	tail := strings.Repeat("😀", 60) + "}"
	root := apply(tst, Attributes, `<img src="x">{width=1}`+tail)
	if t.Get(first(tst, root, "img"), "width") != "1" || t.Content(root) != tail {
		tst.Fatal(canonical(root))
	}
	if ParseColor("hſl(0,0%,0%)") {
		tst.Fatal("ASCII の色名に Unicode の折り畳みを適用しました。")
	}
}
