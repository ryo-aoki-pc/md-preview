package cellfromhtml

import (
	"github.com/ryo-aoki-pc/md-table-excel/internal/htmltok"
	"testing"
)

// cell_from_html._render_text のリンク保持・除外要素・元の改行を確かめる。
func TestSpacedLinks(t *testing.T) {
	for _, test := range []struct{ source, want string }{
		{`<p>前 [表示*名](docs/a) と ![図](image.png)</p>`, `前 [表示*名](docs/a) と ![図](image.png)`},
		{`<kbd>[名](url)</kbd>`, `<kbd>\[名](url)</kbd>`},
		{`<a href="dest">[名](url)</a>`, `[\[名\](url)](dest)`},
		{"<p>[名](ur\nl)</p>", `\[名](ur l)`},
		{`<p>日本語 &#91;名](url) [次](u)</p>`, `日本語 [名](url) [次](u)`},
		{`<p>[名](url "title")</p>`, `[名](url "title")`},
	} {
		tree := htmltok.BuildTree(htmltok.Tokenize(test.source, 0, len(test.source)))
		if got := Convert(tree.Children, test.source); got != test.want {
			t.Errorf("%q の変換が違う: %q", test.source, got)
		}
	}
}
