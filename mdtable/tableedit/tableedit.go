// Package tableedit は Markdown の表を、原文を保ちながら編集する公開の窓口。
package tableedit

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ryo-aoki-pc/md-table-excel/internal/cellsyntax"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
)

// Document は不変の解析結果。同じ文書への呼び出しは同時に行える。
type Document struct {
	text    string
	md      *table.MarkdownFile
	regions []*table.Region
	problem *Error
}

// Kind は表の種類（"pipe" または "html"）。
type Kind string

// Position は 0 起点の表の行・列。
type Position struct {
	Row int `json:"row"`
	Col int `json:"col"`
}
type Section struct {
	Tag      string `json:"tag"`
	Explicit bool   `json:"explicit"`
	FirstRow int    `json:"first_row"`
	Rows     int    `json:"rows"`
}
type Table struct {
	RowTags    []string     `json:"row_tags"`
	Delimiters []string     `json:"delimiters"`
	CopyLocked string       `json:"copy_locked"`
	Number     int          `json:"number"`
	Kind       Kind         `json:"kind"`
	StartLine  int          `json:"start_line"`
	EndLine    int          `json:"end_line"`
	Location   string       `json:"location"`
	Heading    string       `json:"heading"`
	Excluded   string       `json:"excluded"`
	Warnings   []string     `json:"warnings"`
	SourceHash string       `json:"source_hash"`
	Parent     int          `json:"parent"`
	Tag        *TagPosition `json:"tag"`
	Rows       int          `json:"rows"`
	Cols       int          `json:"cols"`
	Cells      []Cell       `json:"cells"`
	Holes      []Position   `json:"holes"`
	Sections   []Section    `json:"sections"`
	RowsLocked string       `json:"rows_locked"`
	ColsLocked string       `json:"cols_locked"`
}
type TagPosition struct {
	Start   int `json:"start"`
	NameEnd int `json:"name_end"`
	End     int `json:"end"`
}
type Cell struct {
	CopyLocked      string `json:"copy_locked"` // 原文を保った写しとして受け取れない理由。
	Tag             string `json:"tag"`
	Row             int    `json:"row"`
	Col             int    `json:"col"`
	Rowspan         int    `json:"rowspan"`
	Colspan         int    `json:"colspan"`
	Header          bool   `json:"header"`
	Align           string `json:"align"`
	Text            string `json:"text"` // セル用 Markdown（Excel 側の Markdown）。
	Line            int    `json:"line"`
	Source          string `json:"source"` // HTML はタグの間、パイプは縦棒の間の原文。
	Complex         bool   `json:"complex"`
	LinkDefinitions bool   `json:"link_definitions"`
}
type Target struct {
	Number     int    `json:"number"`
	SourceHash string `json:"source_hash"`
}
type CellEdit struct {
	Row  int    `json:"row"`
	Col  int    `json:"col"`
	Text string `json:"text"`
}
type Side int

const (
	Before Side = iota
	After
)

type Result struct {
	Text     string    `json:"text"`
	Changed  bool      `json:"changed"`
	Warnings []string  `json:"warnings"`
	Document *Document `json:"-"`
}
type Error struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Row     int      `json:"row"` // 0 起点。位置を特定できない場合は -1。
	Col     int      `json:"col"`
	Details []string `json:"details"`
	cause   error
}

func (e *Error) Error() (out string) {
	defer func() {
		if recover() != nil {
			out = "表の編集で想定外の失敗が起きた"
		}
	}()
	if e == nil {
		return ""
	}
	return e.Message
}

// Unwrap は書き込みの取り消しなど、元の誤りの判定を保つ。
func (e *Error) Unwrap() (cause error) {
	defer func() {
		if recover() != nil {
			cause = nil
		}
	}()
	if e != nil {
		return e.cause
	}
	return nil
}

func internalError() *Error {
	return &Error{Code: "internal", Message: "表の編集で想定外の失敗が起きた", Row: -1, Col: -1}
}
func fault(code, message string) *Error {
	return &Error{Code: code, Message: strings.ReplaceAll(strings.ReplaceAll(message, "\r", " "), "\n", " "), Row: -1, Col: -1}
}
func catch(result **Result, err *error) {
	if recover() != nil {
		*result = nil
		*err = internalError()
	}
}

// Parse は BOM を除いた UTF-8 の本文を解析する。解析時の想定外の失敗は編集時に返す。
func Parse(text string) (d *Document) {
	d = &Document{text: text}
	defer func() {
		if recover() != nil {
			d = &Document{text: text, problem: internalError()}
		}
	}()
	d.md = table.NewMarkdown("", text, false)
	d.md.Doc()
	d.regions = table.FindTables(d.md)
	return
}

func (d *Document) Text() (text string) {
	defer func() {
		if recover() != nil {
			text = ""
		}
	}()
	if d != nil {
		return d.text
	}
	return ""
}

// Tables は番号順の独立した写しを返す。返した値の変更は Document に影響しない。
func (d *Document) Tables() (tables []*Table) {
	defer func() {
		if recover() != nil {
			tables = nil
		}
	}()
	if d == nil {
		return nil
	}
	tables = make([]*Table, 0, len(d.regions))
	for _, r := range d.regions {
		tables = append(tables, d.snapshot(r))
	}
	return
}

func (d *Document) Table(number int) (out *Table) {
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()
	if d == nil {
		return nil
	}
	for _, r := range d.regions {
		if r.Number == number {
			return d.snapshot(r)
		}
	}
	return nil
}

// regionExclusion はセルと原文の対応を位置で確かめ、欠けている表だけを編集の対象外にする。
func regionExclusion(r *table.Region) string {
	if r.Excluded != "" {
		return r.Excluded
	}
	if r.Kind == "html" {
		for _, c := range r.Model().Cells {
			if r.HTML.CellAt[model.Position{Row: c.Row, Col: c.Col}] == nil {
				return fmt.Sprintf("HTML のセル（%d 行 %d 列）の原文を位置から見つけられない", c.Row+1, c.Col+1)
			}
		}
	}
	return ""
}

func (d *Document) bytePosition(r *table.Region, pos int) int {
	line, col := r.Locate(pos)
	for i := 0; i < line; i++ {
		col += len(d.md.Lines[i]) + len(d.md.Endings[i])
	}
	return col
}

func (d *Document) snapshot(r *table.Region) *Table {
	t := &Table{Number: r.Number, Kind: Kind(r.Kind), StartLine: r.Start, EndLine: r.End, Location: r.Location, Heading: r.Heading, Excluded: regionExclusion(r), Warnings: editWarnings(r.Warnings), SourceHash: r.SourceHash(), Cells: []Cell{}, Holes: []Position{}, Sections: []Section{}, RowTags: []string{}, Delimiters: []string{}}
	for _, other := range d.regions {
		if other != r && other.Kind == "html" && other.Start <= r.Start && other.End >= r.End {
			if t.Parent == 0 || other.LineCount() < d.regions[t.Parent-1].LineCount() {
				t.Parent = other.Number
			}
		}
	}
	if t.Excluded != "" {
		t.RowsLocked, t.ColsLocked, t.CopyLocked = t.Excluded, t.Excluded, t.Excluded
		return t
	}
	m := r.Model()
	t.Rows, t.Cols = m.Rows, m.Cols
	t.RowsLocked, t.ColsLocked = table.ShapeLocks(r)
	t.CopyLocked = table.CopyLock(r)
	if r.Kind == "pipe" {
		t.Delimiters = table.PipeDelimiterCells(r)
	} else {
		for _, row := range r.HTML.Rows {
			tag := ""
			if row.Token != nil {
				tag = row.Token.Text
			}
			t.RowTags = append(t.RowTags, tag)
		}
	}
	for _, c := range m.Cells {
		v := Cell{Row: c.Row, Col: c.Col, Rowspan: c.Rowspan, Colspan: c.Colspan, Header: c.Header, Align: c.Align, Text: c.Text}
		if r.Kind == "pipe" {
			v.Line = r.Start
			rows := append([]*table.PipeRow{r.Pipe.Header}, r.Pipe.Rows...)
			if c.Col < len(rows[c.Row].RawCells) {
				v.Source = rows[c.Row].RawCells[c.Col]
			}
			if c.Row > 0 {
				v.Line += c.Row + 1
			}
		} else {
			h := r.HTML.CellAt[model.Position{Row: c.Row, Col: c.Col}]
			v.Line, _ = r.Locate(h.StartToken.Start)
			v.Source = r.HTML.Text[h.ContentStart:h.ContentEnd]
			v.Tag = h.StartToken.Text
			v.LinkDefinitions = h.LinkDefinitions
			v.Complex = complexCell(c.Text, v.Source, h.LinkDefinitions)
		}
		if t.CopyLocked == "" {
			v.CopyLocked = cellCopyLock(t.Kind, v)
		}
		t.Cells = append(t.Cells, v)
	}
	for p := range m.Holes {
		t.Holes = append(t.Holes, Position{p.Row, p.Col})
	}
	sort.Slice(t.Holes, func(i, j int) bool {
		if t.Holes[i].Row != t.Holes[j].Row {
			return t.Holes[i].Row < t.Holes[j].Row
		}
		return t.Holes[i].Col < t.Holes[j].Col
	})
	if r.Kind == "html" {
		start := d.bytePosition(r, r.HTML.TableToken.Start)
		end := d.bytePosition(r, r.HTML.TableToken.End)
		t.Tag = &TagPosition{Start: start, NameEnd: start + 6, End: end}
		row := 0
		for _, s := range r.HTML.Sections {
			t.Sections = append(t.Sections, Section{s.Tag, s.Token != nil, row, len(s.Rows)})
			row += len(s.Rows)
		}
	}
	return t
}

func (d *Document) target(t Target) (*table.Region, error) {
	if d == nil || d.md == nil {
		if d != nil && d.problem != nil {
			v := *d.problem
			return nil, &v
		}
		return nil, fault("invalid", "文書が解析されていない")
	}
	if t.Number < 1 || t.Number > len(d.regions) {
		return nil, fault("range", "指定した番号の表が無い")
	}
	r := d.regions[t.Number-1]
	if t.SourceHash != "" && t.SourceHash != r.SourceHash() {
		return nil, fault("stale", "表の原文が変わっているので、画面を読み直してください")
	}
	if excluded := regionExclusion(r); excluded != "" {
		return nil, fault("excluded", excluded)
	}
	return r, nil
}

func clone(m *model.TableModel) *model.TableModel {
	var cells []*model.Cell
	for _, c := range m.Cells {
		v := *c
		cells = append(cells, &v)
	}
	holes := map[model.Position]bool{}
	for p := range m.Holes {
		holes[p] = true
	}
	return model.New(m.Kind, m.Rows, m.Cols, cells, holes)
}

// WriteFile は BOM と既存の権限を保ち、同じフォルダーの一時ファイルを置き換える。
func WriteFile(ctx context.Context, path, text string, bom bool) (err error) {
	defer func() {
		if recover() != nil {
			err = internalError()
		}
	}()
	if ctx == nil {
		return fault("invalid", "書き込みのコンテキストが無い")
	}
	if err = table.WriteTextAtomicContext(ctx, path, text, bom); err != nil {
		failure := fault("write", fmt.Sprintf("文書を書き込めない（%s）", err))
		failure.cause = err
		return failure
	}
	return nil
}

func complexCell(text, source string, links bool) bool {
	out, err := cellsyntax.ToHTMLCell(text)
	generated := out.Text
	if out.Kind != "inline" {
		generated = "\n\n" + strings.Join(out.Lines, "\n") + "\n\n"
	}
	chunk := strings.HasPrefix(strings.TrimLeft(source, " \t"), "\n\n") && strings.HasSuffix(strings.TrimRight(source, " \t"), "\n\n")
	return err != nil || links || (out.Kind != "inline") != chunk || strings.TrimSpace(generated) != strings.TrimSpace(source)
}
