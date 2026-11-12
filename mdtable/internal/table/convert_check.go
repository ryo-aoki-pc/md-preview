package table

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/blocks"
	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/cellsyntax"
	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/inline"
	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/model"
	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/pycompat"
)

var htmlBlockTag = regexp.MustCompile(`^</?([A-Za-z][A-Za-z0-9-]*)`)
var htmlBlockOpening = regexp.MustCompile(`^<[!?][A-Za-z\[]*`)
var leadingBR = regexp.MustCompile(`(?i)^<br[ \t]*/?>`)
var anyBR = regexp.MustCompile(`(?i)<br[ \t]*/?>`)
var blockMarkdown = regexp.MustCompile("[\\\\`*_~\\[\\]$]")

// sync._html_block_name に対応する HTML ブロックの名前。
func htmlBlockName(doc *blocks.Document, node *blocks.Node) string {
	line := node.Lines[0]
	first := strings.Trim(doc.Lines[line.Number][line.Offset:], " \t")
	if strings.HasPrefix(first, "<!--") {
		return "コメント"
	}
	if match := htmlBlockTag.FindStringSubmatch(first); match != nil {
		return "<" + pycompat.Lower(match[1]) + ">"
	}
	if match := htmlBlockOpening.FindString(first); match != "" {
		return match
	}
	return "HTML"
}

// CheckConversionCells は種類を変えると描画が変わるセルを検査する。
// sync._check_conversion_cells に対応する描画が変わるセルの事前の検査。
func CheckConversionCells(region *Region, target string) error {
	var originalRows []*PipeRow
	if region.Kind == "pipe" {
		originalRows = append([]*PipeRow{region.Pipe.Header}, region.Pipe.Rows...)
	}
	for _, cell := range region.Model().Cells {
		row, col := cell.Row+1, cell.Col+1
		if region.Kind == "html" && region.HTML.CellAt[model.Position{Row: cell.Row, Col: cell.Col}].LinkDefinitions {
			return &cellsyntax.NotRepresentable{Message: "セルの中にリンク参照の定義がある", Row: row, Col: col}
		}
		if target == "html" {
			original := originalRows[cell.Row]
			raw := ""
			if cell.Col < len(original.Contents) {
				raw = original.Contents[cell.Col]
			}
			if leadingBR.MatchString(pycompat.Strip(raw)) && pycompat.Strip(anyBR.ReplaceAllString(raw, "")) != "" {
				return &cellsyntax.NotRepresentable{Message: "セルの先頭の <br> は、HTML の表にすると消える", Row: row, Col: col}
			}
			if strings.Contains(cell.Text, "\n\n\n") {
				return &cellsyntax.NotRepresentable{Message: "3 つ以上続く <br> は、HTML の表では段落の区切り 1 つになる", Row: row, Col: col}
			}
		}
		doc := cellsyntax.Parse(cell.Text)
		for _, node := range doc.Root.Walk() {
			if node.Kind != blocks.HTMLBlock {
				continue
			}
			allowed := len(doc.Root.Children) == 1 && node == doc.Root.Children[0] && node.StartLine == node.EndLine
			if allowed {
				for _, token := range inline.Tokenize(cell.Text, true) {
					if (token.Kind != "html" && token.Kind != "text") || (token.Kind == "text" && blockMarkdown.MatchString(token.Text)) {
						allowed = false
						break
					}
				}
			}
			if !allowed {
				return &cellsyntax.NotRepresentable{Message: fmt.Sprintf("セルに HTML のブロック（%s）があり、種類を変えると描画が変わる", htmlBlockName(doc, node)), Row: row, Col: col}
			}
		}
	}
	return nil
}
