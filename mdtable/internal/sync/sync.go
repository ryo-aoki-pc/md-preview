// Package sync は表の一覧・種類の変換・Excel との受け渡しと、結果の再解析を行う。
package sync

import (
	"context"
	"fmt"
	"github.com/ryo-aoki-pc/md-table-excel/internal/blocks"
	"github.com/ryo-aoki-pc/md-table-excel/internal/cellsyntax"
	"github.com/ryo-aoki-pc/md-table-excel/internal/inline"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
	"github.com/ryo-aoki-pc/md-table-excel/internal/verify"
	"path/filepath"
	"regexp"
	"strings"
)

type Report struct {
	Messages, Warnings, Errors []string
	Diff                       string
	Changed                    bool
	Output                     string
}

// sync.list_tables に対応する一覧。
func ListTables(path string) ([]*table.Region, error) {
	return ListTablesContext(context.Background(), path)
}

func ListTablesContext(ctx context.Context, path string) ([]*table.Region, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	md, e := table.Load(path)
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	if e != nil {
		return nil, e
	}
	regions := table.FindTables(md)
	return regions, ctx.Err()
}

// sync._range_text に対応する範囲の表示。
func rangeText(r *table.Region) string { return fmt.Sprintf("%d〜%d 行", r.Start+1, r.End+1) }

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

// sync._check_conversion_cells に対応する描画が変わるセルの事前の検査。
func checkConversionCells(region *table.Region, target string) error {
	var originalRows []*table.PipeRow
	if region.Kind == "pipe" {
		originalRows = append([]*table.PipeRow{region.Pipe.Header}, region.Pipe.Rows...)
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

// sync.convert に対応する表の種類の変換。
func Convert(path string, selectors []string, target string, dryRun bool) (*Report, error) {
	return ConvertContext(context.Background(), path, selectors, target, dryRun)
}

func ConvertContext(ctx context.Context, path string, selectors []string, target string, dryRun bool) (*Report, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if len(selectors) == 0 {
		return nil, fmt.Errorf("変える表を --table で指定してください")
	}
	if target != "html" && target != "pipe" {
		return nil, fmt.Errorf("変える先の種類は html か pipe を指定してください")
	}
	report := &Report{}
	md, e := table.Load(path)
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	if e != nil {
		return nil, e
	}
	report.Output, _ = filepath.Abs(path)
	regions := table.FindTables(md)
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	chosen, e := table.SelectTables(regions, selectors)
	if e != nil {
		return nil, e
	}
	var replacements []*table.Replacement
	kindName := "Markdown"
	if target == "html" {
		kindName = "HTML"
	}
	for _, r := range chosen {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		label := fmt.Sprintf("表 %d", r.Number)
		if r.Excluded != "" {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: 対象外なので変えられない（%s）", label, r.Excluded))
			continue
		}
		if r.Kind == target {
			report.Messages = append(report.Messages, fmt.Sprintf("%s（%s）: もう %s の表", label, rangeText(r), kindName))
			continue
		}
		var warnings, lines []string
		e = checkConversionCells(r, target)
		if e == nil {
			if target == "html" {
				lines, e = table.RenderFromPipe(r, r.Model(), &warnings)
			} else {
				lines, e = table.RenderNewPipe(r, r.Model(), &warnings)
			}
		}
		if e != nil {
			reason := e.Error()
			if nr, ok := e.(*cellsyntax.NotRepresentable); ok && nr.Row != 0 && nr.Col != 0 {
				reason = fmt.Sprintf("%d 行 %d 列: %s", nr.Row, nr.Col, reason)
			}
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %s の表にできない: %s", label, kindName, reason))
			continue
		}
		for _, warning := range append(append([]string{}, r.Warnings...), warnings...) {
			report.Warnings = append(report.Warnings, label+": "+warning)
		}
		if target == "html" && r.TightContainer {
			report.Warnings = append(report.Warnings, label+": 詰めたリストの中で HTML の表にしたので、GitLab では項目の間隔が広がる")
		}
		replacements = append(replacements, &table.Replacement{Region: r, Lines: lines, Model: r.Model(), TargetKind: target})
		report.Messages = append(report.Messages, fmt.Sprintf("%s（%s）: %s の表に変える", label, rangeText(r), kindName))
	}
	newText := ""
	if len(report.Errors) == 0 && len(replacements) > 0 {
		newText = table.ApplyReplacements(md, replacements)
		report.Errors = append(report.Errors, Verify(md, newText, replacements)...)
	}
	if len(report.Errors) > 0 {
		report.Messages = append(report.Messages, "問題があるので書き戻していません")
		return report, nil
	}
	if len(replacements) == 0 {
		return report, nil
	}
	report.Diff = pycompat.UnifiedDiff(pycompat.SplitLines(md.Text(), true), pycompat.SplitLines(newText, true), path, path)
	report.Changed = true
	if !dryRun {
		if e := table.WriteTextAtomicContext(ctx, path, newText, md.BOM); e != nil {
			return nil, e
		}
	}
	return report, ctx.Err()
}

type lineRange struct{ start, end int }

// sync._leaf_signature に対応する表の外の解釈の署名。
func leafSignature(d *blocks.Document, skip []lineRange, shift func(int) int) []string {
	ranges := make([]verify.LineRange, len(skip))
	for i, r := range skip {
		ranges[i] = verify.LineRange{Start: r.start, End: r.end}
	}
	return verify.LeafSignature(d, ranges, shift)
}

// sync.verify に対応する書き戻した表と前後の検証。
func Verify(md *table.MarkdownFile, newText string, replacements []*table.Replacement) []string {
	return verify.Verify(md, newText, replacements)
}
