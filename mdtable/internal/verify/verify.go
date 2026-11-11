// Package verify は Excel に依存せず、表の書き戻しと前後の解釈を検査する。
package verify

import (
	"fmt"
	"github.com/ryo-aoki-pc/md-table-excel/internal/blocks"
	"github.com/ryo-aoki-pc/md-table-excel/internal/cellsyntax"
	"github.com/ryo-aoki-pc/md-table-excel/internal/changes"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
	"sort"
	"strconv"
	"strings"
)

type LineRange struct{ Start, End int }

// sync._leaf_signature に対応する表の外の解釈の署名。
func LeafSignature(d *blocks.Document, skip []LineRange, shift func(int) int) []string {
	var result []string
	for _, n := range d.Root.Walk() {
		switch n.Kind {
		case blocks.Paragraph, blocks.Heading, blocks.CodeBlock, blocks.HTMLBlock, blocks.ThematicBreak, blocks.Table:
		default:
			continue
		}
		inside := false
		for _, r := range skip {
			if r.Start <= n.StartLine && n.StartLine <= r.End || r.Start <= n.EndLine && n.EndLine <= r.End {
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
	return Check(md, newText, replacements, Options{}).Problems
}

type Options struct{ KeepHoles, SkipStability bool }
type Outcome struct {
	Problems []string
	Markdown *table.MarkdownFile
	Regions  []*table.Region
	Again    []*table.Region
}

// Check は置換を読み直し、表の内容と前後の解釈を確かめる。
func Check(md *table.MarkdownFile, newText string, replacements []*table.Replacement, o Options) Outcome {
	var problems []string
	newMD := table.NewMarkdown(md.Path, newText, md.BOM)
	ordered := append([]*table.Replacement{}, replacements...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Region.Start < ordered[j].Region.Start })
	var oldRanges, newRanges []LineRange
	delta := 0
	for _, repl := range ordered {
		r := repl.Region
		oldRanges = append(oldRanges, LineRange{r.Start, r.End})
		start := r.Start + delta
		end := start + len(repl.Lines) - 1
		newRanges = append(newRanges, LineRange{start, end})
		delta += len(repl.Lines) - r.LineCount()
	}
	shift := func(line int) int {
		offset := 0
		for i, r := range oldRanges {
			if line > r.End {
				offset += len(ordered[i].Lines) - (r.End - r.Start + 1)
			}
		}
		return line + offset
	}
	oldSig := LeafSignature(md.Doc(), oldRanges, shift)
	newSig := LeafSignature(newMD.Doc(), newRanges, func(i int) int { return i })
	if strings.Join(oldSig, "\x02") != strings.Join(newSig, "\x02") {
		problems = append(problems, "書き戻すと、表の前後の Markdown の解釈が変わる")
	}
	newRegions := table.FindTables(newMD)
	againRegions := make([]*table.Region, len(replacements))
	for i, repl := range ordered {
		span := newRanges[i]
		r := repl.Region
		var found []*table.Region
		for _, n := range newRegions {
			if n.Start == span.Start && n.End <= span.End {
				blank := true
				for j := n.End + 1; j <= span.End; j++ {
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
		for j, original := range replacements {
			if original == repl {
				againRegions[j] = again
			}
		}
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
			got, wanted := CloneModel(again.Model(), ""), CloneModel(repl.Model, "")
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
		if o.SkipStability {
			continue
		}
		m := NormalizedSheetModel(again.Model(), r)
		if o.KeepHoles {
			for p := range again.Model().Holes {
				m.Holes[p] = true
			}
		}
		var lines []string
		var e error
		if o.KeepHoles && r.Kind == "pipe" {
			lines, e = table.PatchPipeCells(r, m, &[]string{})
		} else {
			lines, _, e = Render(r, m, &[]string{}, false)
		}
		if e != nil {
			problems = append(problems, fmt.Sprintf("表 %d: 書き戻した表をもう一度書き戻せない（%s）", r.Number, e))
			continue
		}
		if strings.Join(lines, "\n") != strings.Join(repl.Lines, "\n") {
			problems = append(problems, fmt.Sprintf("表 %d: 書き戻した結果が安定しない", r.Number))
		}
	}
	return Outcome{problems, newMD, newRegions, againRegions}
}

func CloneModel(m *model.TableModel, kind string) *model.TableModel {
	cells := make([]*model.Cell, 0, len(m.Cells))
	for _, c := range m.Cells {
		copy := *c
		cells = append(cells, &copy)
	}
	return model.New(kind, m.Rows, m.Cols, cells, nil)
}

func NormalizedSheetModel(sheet *model.TableModel, r *table.Region) *model.TableModel {
	m := CloneModel(sheet, r.Kind)
	var cells []*model.Cell
	for _, c := range m.Cells {
		c.Text = cellsyntax.NormalizeListSpacing(c.Text)
		if r.Kind == "pipe" {
			c.Header = c.Row == 0
		} else {
			p := model.Position{Row: c.Row, Col: c.Col}
			if r.Model().Holes[p] && c.Text == "" && c.Rowspan == 1 && c.Colspan == 1 && !c.Header && c.Align == "" {
				m.Holes[p] = true
				continue
			}
		}
		cells = append(cells, c)
	}
	m.Cells = cells
	return m
}

func Render(r *table.Region, m *model.TableModel, warnings *[]string, keepKind bool) ([]string, string, error) {
	if r.Kind == "pipe" {
		lines, e := table.RenderPipe(r, m, warnings)
		if e == nil {
			return lines, "", nil
		}
		if keepKind {
			return nil, "", e
		}
		converted := e.Error()
		lines, e = table.RenderFromPipe(r, m, warnings)
		return lines, converted, e
	}
	lines, e := table.RenderHTML(r, m, warnings)
	return lines, "", e
}
