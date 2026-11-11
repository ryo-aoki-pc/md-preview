package tableedit

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ryo-aoki-pc/md-table-excel/internal/blocks"
	"github.com/ryo-aoki-pc/md-table-excel/internal/cellsyntax"
	"github.com/ryo-aoki-pc/md-table-excel/internal/htmltok"
	"github.com/ryo-aoki-pc/md-table-excel/internal/inline"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
	"github.com/ryo-aoki-pc/md-table-excel/internal/verify"
)

func newlines(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}
func validInput(s string) error {
	if !utf8.ValidString(s) {
		return fault("invalid", "入力が UTF-8 ではない")
	}
	for _, ch := range s {
		if unicode.IsControl(ch) && ch != '\t' && ch != '\n' {
			return fault("invalid", "タブ・改行以外の制御文字は入力できない")
		}
	}
	return nil
}

func located(err *Error, row, col int) *Error { err.Row, err.Col = row, col; return err }

func renderError(r *table.Region, err error) error {
	row, col, reason := -1, -1, err.Error()
	var nr *cellsyntax.NotRepresentable
	if errors.As(err, &nr) {
		row, col, reason = nr.Row-1, nr.Col-1, nr.Message
	}
	if r.Kind == "pipe" {
		if row >= 0 && col >= 0 {
			reason = fmt.Sprintf("パイプの表のままでは書けない（%d 行 %d 列: %s）", row+1, col+1, reason)
		} else {
			reason = "パイプの表のままでは書けない（" + reason + "）"
		}
	}
	return located(fault("unsupported", reason), row, col)
}

// apply は読み直した結果だけを公開する。元の Document は変更しない。
// notes は操作で起きたことの知らせ。結果の警告には、読み直した表の警告を続ける。
func (d *Document) apply(r *table.Region, lines []string, expected *model.TableModel, shape, source bool, notes []string) (*Result, error) {
	warnings := append([]string{}, notes...)
	repl := &table.Replacement{Region: r, Lines: lines, Model: expected}
	if shape {
		repl.TargetKind = r.Kind
	}
	text := table.ApplyReplacements(d.md, []*table.Replacement{repl})
	out := verify.Check(d.md, text, []*table.Replacement{repl}, verify.Options{KeepHoles: true, SkipStability: source || text == d.text})
	if len(out.Problems) > 0 {
		problems := make([]string, len(out.Problems))
		for i, p := range out.Problems {
			p = strings.ReplaceAll(p, "種類を変えると、表の中身が変わる", "編集すると、表の中身が期待した内容と変わる")
			problems[i] = strings.ReplaceAll(p, "ブックの内容と違う", "編集前の内容と違う")
		}
		err := fault("verify", problems[0])
		if source && out.Again[0] != nil && out.Again[0].Excluded != "" {
			err = fault("verify", out.Again[0].Excluded)
		}
		err.Details = problems
		return nil, err
	}
	// 入れ子の表を編集したときも、その HTML の親が対象外になっていないことを確かめる。
	parent := d.snapshot(r).Parent
	for parent != 0 {
		old := d.regions[parent-1]
		var again *table.Region
		for _, candidate := range out.Regions {
			if candidate.Kind == "html" && candidate.Start == old.Start {
				again = candidate
				break
			}
		}
		if again == nil {
			return nil, fault("verify", "編集すると、親の HTML の表を読み直せない")
		}
		if old.Excluded == "" && again.Excluded != "" {
			return nil, fault("verify", again.Excluded)
		}
		parent = d.snapshot(old).Parent
	}
	if out.Again[0] != nil && loosenedList(r, out.Again[0]) {
		warnings = append(warnings, "セルの空行で、詰めたリストの項目の間隔が広がる")
	}
	if out.Again[0] != nil {
		warnings = append(warnings, out.Again[0].Warnings...)
	}
	next := &Document{text: text, md: out.Markdown, regions: out.Regions}
	return &Result{Text: text, Changed: text != d.text, Warnings: editWarnings(warnings), Document: next}, nil
}

func loosenedList(old, new *table.Region) bool {
	lists := func(r *table.Region) []bool {
		var tight []bool
		for n := r.Container; n != nil; n = n.Parent {
			if n.Kind == blocks.List || n.Kind == blocks.DescriptionList {
				tight = append(tight, n.Info.Tight)
			}
		}
		return tight
	}
	a, b := lists(old), lists(new)
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] && !b[i] {
			return true
		}
	}
	return false
}

func (d *Document) unchanged(r *table.Region) (*Result, error) {
	return d.apply(r, append([]string{}, d.md.Lines[r.Start:r.End+1]...), clone(r.Model()), false, true, nil)
}

func (d *Document) setCells(r *table.Region, edits []CellEdit) (*Result, error) {
	m := clone(r.Model())
	seen := map[model.Position]bool{}
	changed := false
	for _, edit := range edits {
		p := model.Position{Row: edit.Row, Col: edit.Col}
		if seen[p] {
			return nil, located(fault("invalid", "同じセルへの変更が重複している"), edit.Row, edit.Col)
		}
		seen[p] = true
		if edit.Row < 0 || edit.Row >= m.Rows || edit.Col < 0 || edit.Col >= m.Cols {
			return nil, located(fault("range", "セルの位置が表の範囲外"), edit.Row, edit.Col)
		}
		cell := m.CellAt(edit.Row, edit.Col)
		if cell == nil {
			return nil, located(fault("range", "指定した位置にセルが無い"), edit.Row, edit.Col)
		}
		if cell.Row != edit.Row || cell.Col != edit.Col {
			return nil, located(fault("merged", "結合セルは左上の位置で指定してください"), edit.Row, edit.Col)
		}
		input := newlines(edit.Text)
		if err := validInput(input); err != nil {
			return nil, err
		}
		text := cellsyntax.NormalizeListSpacing(model.NormalizeText(input))
		if text != cell.Text {
			cell.Text = text
			changed = true
		}
	}
	if !changed {
		return d.unchanged(r)
	}
	// 表の今の警告は、読み直した表から取る。ここでは書き戻しで出た知らせだけを集める。
	warnings := []string{}
	var lines []string
	var err error
	if r.Kind == "pipe" {
		lines, err = table.PatchPipeCells(r, m, &warnings)
	} else {
		lines, err = table.RenderHTML(r, m, &warnings)
	}
	if err != nil {
		return nil, renderError(r, err)
	}
	return d.apply(r, lines, m, false, false, warnings)
}

func (d *Document) SetCells(t Target, edits []CellEdit) (result *Result, err error) {
	defer catch(&result, &err)
	r, err := d.target(t)
	if err != nil {
		return nil, err
	}
	return d.setCells(r, edits)
}

func (d *Document) Paste(t Target, row, col int, values [][]string) (result *Result, err error) {
	defer catch(&result, &err)
	r, err := d.target(t)
	if err != nil {
		return nil, err
	}
	m := r.Model()
	if row < 0 || col < 0 || row >= m.Rows || col >= m.Cols {
		return nil, fault("range", "貼り付けの開始位置が表の範囲外")
	}
	width := 0
	if len(values) > 0 {
		width = len(values[0])
	}
	if len(values) > m.Rows-row || width > m.Cols-col {
		return nil, fault("range", "貼り付けの範囲が表の外にはみ出す")
	}
	var edits []CellEdit
	for i, items := range values {
		if len(items) != width {
			return nil, fault("invalid", "貼り付けの行ごとの列数が違う")
		}
		for j, value := range items {
			edits = append(edits, CellEdit{row + i, col + j, value})
		}
	}
	return d.setCells(r, edits)
}

var brokenTag = regexp.MustCompile(`<[A-Za-z/!?]`)

// checkCellSource は原文を差し替える前に字句を調べる。閉じた自動リンクはタグと区別する。
func checkCellSource(source string) error {
	for _, tok := range htmltok.Tokenize(source, 0, len(source)) {
		if tok.Kind == "start" && htmltok.RawTextElements[tok.Name] {
			return fault("invalid", "セルの原文に <"+tok.Name+"> は書けない")
		}
		if tok.Kind != "text" {
			continue
		}
		for pos := 0; pos < len(tok.Text); {
			match := brokenTag.FindStringIndex(tok.Text[pos:])
			if match == nil {
				break
			}
			pos += match[0]
			end := inline.MatchAutolink(tok.Text, pos)
			if end < 0 {
				return fault("invalid", "セルの原文に閉じていない HTML のタグやコメントがある")
			}
			pos = end
		}
	}
	return nil
}

func (d *Document) SetCellSource(t Target, row, col int, source string) (result *Result, err error) {
	defer catch(&result, &err)
	r, err := d.target(t)
	if err != nil {
		return nil, err
	}
	if r.Kind != "html" {
		return nil, fault("unsupported", "原文を差し替えられるのは HTML の表だけ")
	}
	cell := r.HTML.CellAt[model.Position{Row: row, Col: col}]
	if cell == nil {
		if r.Model().CellAt(row, col) != nil {
			return nil, located(fault("merged", "結合セルは左上の位置で指定してください"), row, col)
		}
		return nil, located(fault("range", "指定した位置にセルが無い"), row, col)
	}
	source = newlines(source)
	if err = validInput(source); err != nil {
		return nil, err
	}
	if err = checkCellSource(source); err != nil {
		return nil, err
	}
	if source == r.HTML.Text[cell.ContentStart:cell.ContentEnd] {
		return d.unchanged(r)
	}
	lines, err := table.ReplaceHTMLCellSource(r, cell, source)
	if err != nil {
		return nil, renderError(r, err)
	}
	result, err = d.apply(r, lines, clone(r.Model()), false, true, nil)
	if err != nil {
		return nil, err
	}
	again := result.Document.regions[t.Number-1]
	if again.HTML == nil || len(again.HTML.Cells) != len(r.HTML.Cells) {
		return nil, fault("verify", "原文を差し替えると、セルの形が変わる")
	}
	for i, old := range r.HTML.Cells {
		new := again.HTML.Cells[i]
		wanted := r.HTML.Text[old.ContentStart:old.ContentEnd]
		if old == cell {
			wanted = source
		}
		if again.HTML.Text[new.ContentStart:new.ContentEnd] != wanted {
			return nil, fault("verify", "原文を差し替えると、セルの内容の範囲が変わる")
		}
	}
	return result, nil
}
