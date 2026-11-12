package tableedit

import (
	"errors"
	"strings"

	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/cellsyntax"
	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/table"
	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/verify"
)

func conversionError(err error) error {
	var cell *cellsyntax.NotRepresentable
	if errors.As(err, &cell) {
		return located(fault("unsupported", cell.Message), cell.Row-1, cell.Col-1)
	}
	return fault("unsupported", err.Error())
}

// ConvertLocked は、指定した種類へ変換できない理由を返す。空なら変換できる。
func (t *Table) ConvertLocked(to string) string {
	if to != "html" && to != "pipe" {
		return "変える先の種類は html か pipe を指定してください"
	}
	if t == nil {
		return "指定した表が無い"
	}
	if t.Excluded != "" {
		return t.Excluded
	}
	if string(t.Kind) == to {
		if to == "html" {
			return "すでに HTML の表です"
		}
		return "すでにパイプの表です"
	}
	if t.region == nil {
		return "表の原文を確かめられない"
	}
	if _, err := conversionLines(t.region, to, &[]string{}); err != nil {
		return err.Error()
	}
	return ""
}

// DeleteLocked は、表を削除できない理由を返す。空なら削除できる。
func (t *Table) DeleteLocked() string {
	if t == nil {
		return "指定した表が無い"
	}
	if t.Excluded != "" {
		return t.Excluded
	}
	if t.document == nil || t.region == nil {
		return "表の原文を確かめられない"
	}
	// 削除した文書の解釈の確かめは文書全体を読み直すので、表の数だけ行うと重い。
	// ここでは原文の形だけを見て、前後の解釈は DeleteTable のときに確かめる。
	if err := t.document.deletable(t.region); err != nil {
		return err.Error()
	}
	return ""
}

// deletable は、文書を読み直さずに分かる、表を削除できない理由を返す。
func (d *Document) deletable(r *table.Region) error {
	if strings.Trim(r.FirstPrefix(), " \t>") != "" || strings.TrimSpace(r.Head) != "" || strings.TrimSpace(r.Tail) != "" {
		return fault("unsupported", "表と同じ行に項目の印やほかの文字があるため、項目全体を編集して消してください")
	}
	for _, other := range d.regions {
		if other != r && other.Start <= r.Start && r.End <= other.End {
			return fault("unsupported", "入れ子の表は、外側の表のセルの原文を編集して消してください")
		}
	}
	return nil
}

func conversionLines(r *table.Region, to string, warnings *[]string) ([]string, error) {
	for _, c := range r.Model().Cells {
		text := c.Text
		if r.Kind == "html" {
			// HTML の文字の角括弧は、セル用 Markdown ではエスケープされる。
			text = strings.NewReplacer(`\[`, "[", `\]`, "]").Replace(text)
		}
		switch strings.TrimSpace(text) {
		case "[ ]", "[x]", "[X]":
			return nil, located(fault("unsupported", "タスクの印だけのセルがあり、種類を変えるとチェックボックスの描画が変わる"), c.Row, c.Col)
		}
	}
	if err := table.CheckConversionCells(r, to); err != nil {
		return nil, conversionError(err)
	}
	var lines []string
	var err error
	if to == "html" {
		lines, err = table.RenderFromPipe(r, r.Model(), warnings)
	} else {
		lines, err = table.RenderNewPipe(r, r.Model(), warnings)
	}
	if err != nil {
		return nil, conversionError(err)
	}
	return lines, nil
}

// ConvertTable は表の種類を変える。同じ種類なら本文を変えない。
func (d *Document) ConvertTable(t Target, to string) (result *Result, err error) {
	defer catch(&result, &err)
	if to != "html" && to != "pipe" {
		return nil, fault("invalid", "変える先の種類は html か pipe を指定してください")
	}
	r, err := d.target(t)
	if err != nil {
		return nil, err
	}
	if r.Kind == to {
		return d.unchanged(r)
	}
	warnings := []string{}
	lines, err := conversionLines(r, to, &warnings)
	if err != nil {
		return nil, err
	}
	return d.applyEdits([]regionEdit{{region: r, lines: lines, kind: to, expected: clone(r.Model())}}, warnings)
}

func (d *Document) removal(r *table.Region) (*Result, error) {
	if err := d.deletable(r); err != nil {
		return nil, err
	}
	start, end := r.Start, r.End+1
	blank := func(line string) bool {
		return strings.TrimSpace(line) == "" || r.BlankPrefix() != "" && strings.TrimRight(line, " \t") == r.BlankPrefix()
	}
	for start > 0 && blank(d.md.Lines[start-1]) {
		start--
	}
	for end < len(d.md.Lines) && blank(d.md.Lines[end]) {
		end++
	}
	var lines []string
	if start > 0 && end < len(d.md.Lines) {
		lines = []string{""}
	}
	span := &table.Region{Start: start, End: end - 1}
	text := table.ApplyReplacements(d.md, []*table.Replacement{{Region: span, Lines: lines}})
	out := verify.Removal(d.md, text, r, start, end, len(lines))
	if len(out.Problems) != 0 {
		err := fault("verify", out.Problems[0])
		err.Details = out.Problems
		return nil, err
	}
	return &Result{Text: text, Changed: text != d.text, Warnings: []string{}, Document: &Document{text: text, md: out.Markdown, regions: out.Regions}}, nil
}

// DeleteTable は表の原文を消し、前後の空行を一行にまとめる。
func (d *Document) DeleteTable(t Target) (result *Result, err error) {
	defer catch(&result, &err)
	r, err := d.target(t)
	if err != nil {
		return nil, err
	}
	return d.removal(r)
}
