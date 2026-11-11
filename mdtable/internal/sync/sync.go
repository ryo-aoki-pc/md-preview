// Package sync は表の一覧・種類の変換・Excel との受け渡しと、結果の再解析を行う。
package sync

import (
	"context"
	"fmt"
	"github.com/ryo-aoki-pc/md-table-excel/internal/blocks"
	"github.com/ryo-aoki-pc/md-table-excel/internal/cellsyntax"
	"github.com/ryo-aoki-pc/md-table-excel/internal/changes"
	"github.com/ryo-aoki-pc/md-table-excel/internal/inline"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
	var result []string
	for _, n := range d.Root.Walk() {
		switch n.Kind {
		case blocks.Paragraph, blocks.Heading, blocks.CodeBlock, blocks.HTMLBlock, blocks.ThematicBreak, blocks.Table:
		default:
			continue
		}
		inside := false
		for _, r := range skip {
			if r.start <= n.StartLine && n.StartLine <= r.end || r.start <= n.EndLine && n.EndLine <= r.end {
				inside = true
				break
			}
		}
		if inside {
			continue
		}
		var chain []string
		for a := n.Parent; a != nil; a = a.Parent {
			chain = append(chain, a.Kind)
		}
		result = append(result, n.Kind+"\x00"+strings.Join(chain, "\x00")+"\x01"+strconv.Itoa(shift(n.StartLine))+":"+strconv.Itoa(shift(n.EndLine)))
	}
	return result
}

// sync.verify に対応する書き戻した表と前後の検証。
func Verify(md *table.MarkdownFile, newText string, replacements []*table.Replacement) []string {
	var problems []string
	newMD := table.NewMarkdown(md.Path, newText, md.BOM)
	ordered := append([]*table.Replacement{}, replacements...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Region.Start < ordered[j].Region.Start })
	var oldRanges, newRanges []lineRange
	delta := 0
	for _, repl := range ordered {
		r := repl.Region
		oldRanges = append(oldRanges, lineRange{r.Start, r.End})
		start := r.Start + delta
		end := start + len(repl.Lines) - 1
		newRanges = append(newRanges, lineRange{start, end})
		delta += len(repl.Lines) - r.LineCount()
	}
	shift := func(line int) int {
		offset := 0
		for i, r := range oldRanges {
			if line > r.end {
				offset += len(ordered[i].Lines) - (r.end - r.start + 1)
			}
		}
		return line + offset
	}
	oldSig := leafSignature(md.Doc(), oldRanges, shift)
	newSig := leafSignature(newMD.Doc(), newRanges, func(i int) int { return i })
	if strings.Join(oldSig, "\x02") != strings.Join(newSig, "\x02") {
		problems = append(problems, "書き戻すと、表の前後の Markdown の解釈が変わる")
	}
	newRegions := table.FindTables(newMD)
	for i, repl := range ordered {
		span := newRanges[i]
		r := repl.Region
		var found []*table.Region
		for _, n := range newRegions {
			if n.Start == span.start && n.End <= span.end {
				blank := true
				for j := n.End + 1; j <= span.end; j++ {
					if strings.Trim(newMD.Lines[j], " \t>") != "" {
						blank = false
						break
					}
				}
				if blank {
					found = append(found, n)
				}
			}
		}
		if len(found) == 0 || repl.TargetKind != "" && (len(found) != 1 || found[0].Kind != repl.TargetKind) {
			problems = append(problems, fmt.Sprintf("表 %d: 書き戻した表を読み直せない", r.Number))
			continue
		}
		again := found[0]
		if again.Excluded != "" {
			problems = append(problems, fmt.Sprintf("表 %d: 書き戻した表が対象外になる（%s）", r.Number, again.Excluded))
			continue
		}
		if again.Location != r.Location {
			location := r.Location
			if location == "" {
				location = "本文"
			}
			problems = append(problems, fmt.Sprintf("表 %d: 書き戻すと表の場所（%s）が変わる", r.Number, location))
			continue
		}
		if repl.TargetKind != "" {
			if !again.Model().SameContent(repl.Model) {
				changes := changes.CompareModels(repl.Model, again.Model())
				details := changes.Details(2)
				for i := range details {
					details[i] = pycompat.Strip(details[i])
				}
				suffix := changes.Summary()
				if len(details) > 0 {
					suffix = "（" + strings.Join(details, "、") + "）"
				}
				problems = append(problems, fmt.Sprintf("表 %d: 種類を変えると、表の中身が変わる%s", r.Number, suffix))
			}
			continue
		}
		if r.Kind == "html" && again.Kind == "html" && repl.Model != nil {
			got, wanted := cloneModel(again.Model(), ""), cloneModel(repl.Model, "")
			for _, m := range []*model.TableModel{got, wanted} {
				for _, c := range m.Cells {
					c.Text = ""
				}
			}
			if !got.SameContent(wanted) {
				problems = append(problems, fmt.Sprintf("表 %d: 書き戻した表の形・見出し・配置が、ブックの内容と違う", r.Number))
				continue
			}
		}
		m := normalizedSheetModel(again.Model(), r)
		lines, _, e := render(r, m, &[]string{}, false)
		if e != nil {
			problems = append(problems, fmt.Sprintf("表 %d: 書き戻した表をもう一度書き戻せない（%s）", r.Number, e))
			continue
		}
		if strings.Join(lines, "\n") != strings.Join(repl.Lines, "\n") {
			problems = append(problems, fmt.Sprintf("表 %d: 書き戻した結果が安定しない", r.Number))
		}
	}
	return problems
}
