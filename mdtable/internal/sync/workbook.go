package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/ryo-aoki-pc/md-table-excel/internal/cellsyntax"
	"github.com/ryo-aoki-pc/md-table-excel/internal/changes"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
	"github.com/ryo-aoki-pc/md-table-excel/internal/version"
	"github.com/ryo-aoki-pc/md-table-excel/internal/workbook"
)

func DefaultOutput(path string) string {
	abs, _ := filepath.Abs(path)
	return filepath.Join(filepath.Dir(abs), pycompat.FileStem(abs)+".tables.xlsx")
}

// Export は元の表の位置と指紋を記録してブックへ書き出す。
func Export(path, output string, selectors []string, force bool) (*Report, error) {
	return ExportContext(context.Background(), path, output, selectors, force)
}

func ExportContext(ctx context.Context, path, output string, selectors []string, force bool) (*Report, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	report := &Report{}
	md, e := table.Load(path)
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	if e != nil {
		return nil, e
	}
	regions := table.FindTables(md)
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if len(regions) == 0 {
		return nil, fmt.Errorf("表がありません: %s", path)
	}
	chosen, e := table.SelectTables(regions, selectors)
	if e != nil {
		return nil, e
	}
	occurrences := map[int]int{}
	same := map[string]int{}
	for _, r := range regions {
		if r.Excluded == "" {
			hash := r.SourceHash()
			occurrences[r.Number] = same[hash]
			same[hash]++
		}
	}
	var tables []*workbook.ExportTable
	var excluded []*table.Region
	for _, r := range chosen {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		if r.Excluded == "" {
			for _, cell := range r.Model().Cells {
				if reason := workbook.CheckCellText(cell.Text); reason != "" {
					r.Excluded = fmt.Sprintf("%d 行 %d 列のセルに%s", cell.Row+1, cell.Col+1, reason)
					break
				}
			}
		}
		if r.Excluded != "" {
			excluded = append(excluded, r)
			report.Warnings = append(report.Warnings, fmt.Sprintf("表 %d（%s）は対象外: %s", r.Number, rangeText(r), r.Excluded))
			continue
		}
		for _, warning := range r.Warnings {
			report.Warnings = append(report.Warnings, fmt.Sprintf("表 %d: %s", r.Number, warning))
		}
		if r.Kind == "html" {
			for _, cell := range r.HTML.Cells {
				if cell.LinkDefinitions {
					report.Warnings = append(report.Warnings, fmt.Sprintf("表 %d: %d 行 %d 列のセルにリンク参照の定義がある（このセルを Excel で変えると、定義が文字になる）", r.Number, cell.Row+1, cell.Col+1))
				}
			}
		}
		tables = append(tables, &workbook.ExportTable{Region: r, Model: r.Model(), Occurrence: occurrences[r.Number], Same: same[r.SourceHash()]})
	}
	if len(tables) == 0 {
		return nil, fmt.Errorf("書き出せる表がありません")
	}
	if output == "" {
		output = DefaultOutput(path)
	}
	output, e = filepath.Abs(output)
	if e != nil {
		return nil, e
	}
	if _, e := os.Stat(output); e == nil && !force {
		return nil, fmt.Errorf("ブックが既にあります（上書きするなら --force）: %s", output)
	}
	mdPath, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	if e = workbook.WriteContext(ctx, output, mdPath, tables, excluded, version.Version); e != nil {
		return nil, e
	}
	for _, t := range tables {
		report.Messages = append(report.Messages, fmt.Sprintf("表 %d（%s）→ シート「%s」", t.Region.Number, rangeText(t.Region), t.SheetName))
	}
	report.Output = output
	return report, ctx.Err()
}

func cloneModel(m *model.TableModel, kind string) *model.TableModel {
	cells := make([]*model.Cell, 0, len(m.Cells))
	for _, c := range m.Cells {
		copy := *c
		cells = append(cells, &copy)
	}
	return model.New(kind, m.Rows, m.Cols, cells, nil)
}

func normalizedSheetModel(sheet *model.TableModel, r *table.Region) *model.TableModel {
	m := cloneModel(sheet, r.Kind)
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

func render(r *table.Region, m *model.TableModel, warnings *[]string, keepKind bool) ([]string, string, error) {
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

func alreadyApplied(sheet *model.TableModel, r *table.Region, md *table.MarkdownFile) bool {
	m := normalizedSheetModel(sheet, r)
	if m.SameContent(r.Model()) {
		return true
	}
	lines, _, e := render(r, m, &[]string{}, false)
	return e == nil && reflect.DeepEqual(lines, md.Lines[r.Start:r.End+1])
}

func trailingEmpty(m *model.TableModel) [2]int {
	rowEnd, colEnd := 0, 0
	if m.Kind == "pipe" {
		rowEnd = 1
	}
	for _, c := range m.Cells {
		if c.Text != "" || c.Rowspan > 1 || c.Colspan > 1 {
			rowEnd = max(rowEnd, c.Row+c.Rowspan)
			colEnd = max(colEnd, c.Col+c.Colspan)
		}
	}
	return [2]int{m.Rows - rowEnd, m.Cols - colEnd}
}

// ImportWorkbook は衝突を確かめ、全表を検証した後に文書を一括で置き換える。
func ImportWorkbook(xlsx, mdPath string, dryRun, force, keepKind, strict bool, tables []int) (*Report, error) {
	return ImportWorkbookContext(context.Background(), xlsx, mdPath, dryRun, force, keepKind, strict, tables)
}

func ImportWorkbookContext(ctx context.Context, xlsx, mdPath string, dryRun, force, keepKind, strict bool, tables []int) (*Report, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	report := &Report{}
	data, e := workbook.ReadContext(ctx, xlsx, force, strict, tables)
	if e != nil {
		return nil, e
	}
	report.Warnings = append(report.Warnings, data.Warnings...)
	if mdPath == "" {
		mdPath = data.MarkdownPath
		if data.MarkdownRelpath != "" {
			abs, e := filepath.Abs(xlsx)
			if e != nil {
				return nil, e
			}
			candidate := filepath.Join(filepath.Dir(abs), filepath.FromSlash(data.MarkdownRelpath))
			if _, e := os.Stat(candidate); e == nil {
				mdPath = candidate
			}
		}
	}
	if _, e := os.Stat(mdPath); mdPath == "" || e != nil {
		display := mdPath
		if display == "" {
			display = "None"
		}
		return nil, fmt.Errorf("書き戻す Markdown が見つかりません（--md で指定してください）: %s", display)
	}
	md, e := table.Load(mdPath)
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	if e != nil {
		return nil, e
	}
	report.Output, e = filepath.Abs(mdPath)
	if e != nil {
		return nil, e
	}
	regions := table.FindTables(md)
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	byHash := map[string][]*table.Region{}
	for _, r := range regions {
		if r.Excluded == "" {
			byHash[r.SourceHash()] = append(byHash[r.SourceHash()], r)
		}
	}
	var replacements []*table.Replacement
	for _, t := range data.Tables {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		label := fmt.Sprintf("表 %d", t.Number)
		if t.Missing != "" {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s: %s。この表は変えない", label, t.Missing))
			continue
		}
		for _, w := range t.Warnings {
			report.Warnings = append(report.Warnings, label+": "+w)
		}
		for _, err := range t.Errors {
			report.Errors = append(report.Errors, label+": "+err)
		}
		sheet := cloneModel(t.Model, "")
		if t.Kind == "pipe" {
			for _, c := range sheet.Cells {
				c.Header = c.Row == 0
			}
		}
		if t.Fingerprint != "" && sheet.Fingerprint() == t.Fingerprint {
			report.Messages = append(report.Messages, label+": 変更なし")
			continue
		}
		var positional *table.Region
		if t.Number > 0 && t.Number <= len(regions) {
			positional = regions[t.Number-1]
			if positional.Excluded != "" {
				positional = nil
			}
		}
		if positional != nil && positional.SourceHash() != t.SourceHash && !data.SourceHashes[positional.SourceHash()] && alreadyApplied(t.Model, positional, md) {
			report.Messages = append(report.Messages, label+": 適用済み（変更なし）")
			continue
		}
		candidates := byHash[t.SourceHash]
		countMatches := t.Same == nil || len(candidates) == *t.Same
		var region *table.Region
		if countMatches && t.Occurrence >= 0 && t.Occurrence < len(candidates) {
			region = candidates[t.Occurrence]
		}
		if region == nil {
			if positional != nil {
				if alreadyApplied(t.Model, positional, md) {
					report.Messages = append(report.Messages, label+": 適用済み（変更なし）")
					continue
				}
				if force {
					region = positional
					report.Warnings = append(report.Warnings, label+": 書き出した後に Markdown の表が変わっていたが、--force でブックの内容で上書きする")
				}
			}
			if region == nil {
				if !countMatches {
					report.Errors = append(report.Errors, fmt.Sprintf("%s: 衝突: 同じ内容の表の数が、書き出したとき（%d 個）と違う（今は %d 個）ので、どの表か決められない（同じ番号の表をブックの内容で上書きするなら --force）", label, *t.Same, len(candidates)))
				} else {
					report.Errors = append(report.Errors, label+": 衝突: 書き出した後に Markdown の表が変わっている（ブックの内容で上書きするなら --force）")
				}
				continue
			}
		}
		m := normalizedSheetModel(t.Model, region)
		if m.Rows == 0 || len(m.Cells) == 0 {
			report.Warnings = append(report.Warnings, label+": シートが空なので、この表は変えない")
			continue
		}
		if m.SameContent(region.Model()) {
			report.Messages = append(report.Messages, fmt.Sprintf("%s（%s）: 変更なし", label, rangeText(region)))
			continue
		}
		var warnings []string
		lines, converted, e := render(region, m, &warnings, keepKind)
		if e != nil {
			reason := e.Error()
			if nr, ok := e.(*cellsyntax.NotRepresentable); ok && nr.Row > 0 && nr.Col > 0 {
				reason = fmt.Sprintf("%d 行 %d 列: %s", nr.Row, nr.Col, reason)
			}
			if keepKind && region.Kind == "pipe" {
				report.Errors = append(report.Errors, fmt.Sprintf("%s: パイプ表のままでは書けない（%s）。HTML の表に変えてよければ --keep-kind を外す", label, reason))
			} else {
				report.Errors = append(report.Errors, label+": 書き戻せない: "+reason)
			}
			continue
		}
		for _, w := range warnings {
			report.Warnings = append(report.Warnings, label+": "+w)
		}
		if reflect.DeepEqual(lines, md.Lines[region.Start:region.End+1]) {
			report.Messages = append(report.Messages, fmt.Sprintf("%s（%s）: 変更なし", label, rangeText(region)))
			continue
		}
		old := region.Model()
		trailing, previous := trailingEmpty(m), trailingEmpty(old)
		for i, unit := range []string{"行", "列"} {
			kept := m.Cols >= old.Cols
			if i == 1 {
				kept = m.Rows >= old.Rows
			}
			if trailing[i] > previous[i] && kept {
				report.Warnings = append(report.Warnings, fmt.Sprintf("%s: 末尾の %d %sが空になっている（%sごと消すなら、Excel で%sを削除する）", label, trailing[i]-previous[i], unit, unit, unit))
			}
		}
		replacements = append(replacements, &table.Replacement{Region: region, Lines: lines, Model: m})
		change := changes.CompareModels(old, m)
		if converted != "" {
			report.Messages = append(report.Messages, fmt.Sprintf("%s（%s）: %sため HTML の表に変えて更新%s", label, rangeText(region), converted, change.Summary()))
			if region.TightContainer {
				report.Warnings = append(report.Warnings, label+": 詰めたリストの中で HTML の表にしたので、GitLab では項目の間隔が広がる")
			}
		} else {
			report.Messages = append(report.Messages, fmt.Sprintf("%s（%s）: 更新%s", label, rangeText(region), change.Summary()))
		}
		if dryRun {
			report.Messages = append(report.Messages, change.Details(20)...)
		}
	}
	if len(report.Errors) > 0 {
		report.Messages = append(report.Messages, "問題があるので書き戻していません")
		return report, nil
	}
	if len(replacements) == 0 {
		return report, nil
	}
	newText := table.ApplyReplacements(md, replacements)
	if problems := Verify(md, newText, replacements); len(problems) > 0 {
		report.Errors = append(report.Errors, problems...)
		report.Messages = append(report.Messages, "書き戻した結果の確認に失敗したので書き戻していません")
		return report, nil
	}
	report.Diff = pycompat.UnifiedDiff(pycompat.SplitLines(md.Text(), true), pycompat.SplitLines(newText, true), mdPath, mdPath)
	report.Changed = true
	if !dryRun {
		if e = table.WriteTextAtomicContext(ctx, mdPath, newText, md.BOM); e != nil {
			return nil, e
		}
	}
	return report, ctx.Err()
}
