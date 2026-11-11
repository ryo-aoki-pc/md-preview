package cellfromhtml

import (
	"github.com/ryo-aoki-pc/md-table-excel/internal/htmltok"
	"testing"
)

// Python: test_cells.HtmlToExcelTest.test_markdown_chunk
func TestPythonMarkdownChunk(t *testing.T) {
	tree := htmltok.BuildTree([]*htmltok.Token{{Kind: "md", Text: "- a\n- b"}})
	if got := Convert(tree.Children, "<b>x</b>"); got != "- a\n- b" {
		t.Fatalf("Markdown の部分が変わった: %q", got)
	}
}
