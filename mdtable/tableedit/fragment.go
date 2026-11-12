package tableedit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/ryo-aoki-pc/md-table-excel/internal/cellsyntax"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
)

// FragmentVersion は写しの JSON の版。
const FragmentVersion = 1

// 上限は結合の被覆位置と穴も含む格子と、原文の文字の総バイト数に適用する。
const (
	MaxFragmentCells = 10000
	MaxFragmentBytes = 8 << 20
)

// Rect は表の中の長方形。行・列は 0 起点。
type Rect struct {
	Row  int `json:"row"`
	Col  int `json:"col"`
	Rows int `json:"rows"`
	Cols int `json:"cols"`
}

// Fragment は文書やプロセスの外へ持ち出せる範囲の写し。
type Fragment struct {
	Version    int            `json:"version"`
	Kind       Kind           `json:"kind"`
	Shape      string         `json:"shape"`
	Rows       int            `json:"rows"`
	Cols       int            `json:"cols"`
	Cells      []FragmentCell `json:"cells"`
	RowTags    []string       `json:"row_tags"`
	Delimiters []string       `json:"delimiters"`
}

type FragmentCell struct {
	Row     int    `json:"row"`
	Col     int    `json:"col"`
	Rowspan int    `json:"rowspan"`
	Colspan int    `json:"colspan"`
	Text    string `json:"text"`
	Tag     string `json:"tag"`
	Source  string `json:"source"`
}

func fragmentPanic(f **Fragment, err *error) {
	if recover() != nil {
		*f = nil
		*err = internalError()
	}
}

// Copy は独立したスナップショットから写しを作る。
func (t *Table) Copy(r Rect) (f *Fragment, err error) {
	defer fragmentPanic(&f, &err)
	if t == nil {
		return nil, fault("invalid", "表が無い")
	}
	if t.Excluded != "" {
		return nil, fault("excluded", t.Excluded)
	}
	if t.CopyLocked != "" {
		return nil, fault("unsupported", t.CopyLocked)
	}
	if r.Row < 0 || r.Col < 0 || r.Rows < 1 || r.Cols < 1 || r.Row >= t.Rows || r.Col >= t.Cols || r.Rows > t.Rows-r.Row || r.Cols > t.Cols-r.Col {
		return nil, fault("range", "写す範囲が表の外にはみ出す")
	}
	if r.Rows > MaxFragmentCells/r.Cols {
		return nil, fault("range", "写す範囲のセルが多すぎる")
	}
	f = &Fragment{Version: FragmentVersion, Kind: t.Kind, Shape: "cells", Rows: r.Rows, Cols: r.Cols, Cells: []FragmentCell{}, RowTags: []string{}, Delimiters: []string{}}
	if r.Col == 0 && r.Cols == t.Cols {
		f.Shape = "rows"
	}
	if r.Row == 0 && r.Rows == t.Rows {
		f.Shape = "cols"
	}
	if r.Row == 0 && r.Col == 0 && r.Rows == t.Rows && r.Cols == t.Cols {
		f.Shape = "table"
	}
	for _, c := range t.Cells {
		if c.Row >= r.Row+r.Rows || c.Col >= r.Col+r.Cols || c.Row+c.Rowspan <= r.Row || c.Col+c.Colspan <= r.Col {
			continue
		}
		if c.Row < r.Row || c.Col < r.Col || c.Row+c.Rowspan > r.Row+r.Rows || c.Col+c.Colspan > r.Col+r.Cols {
			return nil, located(fault("merged", "写す範囲で結合セルが切れるので、範囲を選び直してください"), c.Row, c.Col)
		}
		if c.CopyLocked != "" {
			return nil, located(fault("unsupported", c.CopyLocked), c.Row, c.Col)
		}
		f.Cells = append(f.Cells, FragmentCell{Row: c.Row - r.Row, Col: c.Col - r.Col, Rowspan: c.Rowspan, Colspan: c.Colspan, Tag: c.Tag, Source: c.Source})
	}
	if t.Kind == "html" && r.Col == 0 && r.Cols == t.Cols && len(t.RowTags) == t.Rows {
		f.RowTags = append(f.RowTags, t.RowTags[r.Row:r.Row+r.Rows]...)
	}
	if t.Kind == "pipe" && len(t.Delimiters) == t.Cols {
		f.Delimiters = append(f.Delimiters, t.Delimiters[r.Col:r.Col+r.Cols]...)
	}
	// 全体の大きさを検査する。構文の判定にはセルごとの印を使う。
	if fragmentSize(f) > MaxFragmentBytes {
		return nil, fault("range", "写しの原文が大きすぎる")
	}
	return f, nil
}

// cellCopyLock は元の文書の文脈から切り離すと受け取れない原文をセルごとに示す。
func cellCopyLock(kind Kind, c Cell) string {
	f := &Fragment{Version: FragmentVersion, Kind: kind, Shape: "cells", Rows: c.Rowspan, Cols: c.Colspan,
		Cells: []FragmentCell{{Rowspan: c.Rowspan, Colspan: c.Colspan, Tag: c.Tag, Source: c.Source}}}
	if _, _, err := validateFragment(f); err != nil {
		return "このセルの原文は写しとして受け取れない（" + err.Error() + "）"
	}
	return ""
}

// Copy は対象の照合後、スナップショットの Copy を呼ぶ。
func (d *Document) Copy(t Target, r Rect) (f *Fragment, err error) {
	defer fragmentPanic(&f, &err)
	region, err := d.target(t)
	if err != nil {
		return nil, err
	}
	return d.snapshot(region).Copy(r)
}

// ParseFragment は信用しない JSON を厳しく読む。
func ParseFragment(data []byte) (f *Fragment, err error) {
	defer fragmentPanic(&f, &err)
	if len(data) > MaxFragmentBytes || !utf8.Valid(data) {
		return nil, fault("invalid", "写しが大きすぎるか、UTF-8 ではない")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if e := dec.Decode(&f); e != nil || f == nil {
		return nil, fault("invalid", "写しの JSON が正しくない")
	}
	if _, e := dec.Token(); e != io.EOF {
		return nil, fault("invalid", "写しの JSON の後ろに続きがある")
	}
	f, _, err = validateFragment(f)
	return f, err
}

// ValuesFragment は長方形のセル用 Markdown から値だけの写しを作る。
func ValuesFragment(values [][]string) (f *Fragment, err error) {
	defer fragmentPanic(&f, &err)
	if len(values) == 0 || len(values[0]) == 0 {
		return nil, fault("invalid", "写しの行・列が空")
	}
	f = &Fragment{Version: FragmentVersion, Shape: "cells", Rows: len(values), Cols: len(values[0]), Cells: []FragmentCell{}, RowTags: []string{}, Delimiters: []string{}}
	if f.Rows > MaxFragmentCells/f.Cols {
		return nil, fault("invalid", "写しのセルが多すぎる")
	}
	for row, vs := range values {
		if len(vs) != f.Cols {
			return nil, fault("invalid", "写しの行ごとの列数が違う")
		}
		for col, v := range vs {
			f.Cells = append(f.Cells, FragmentCell{Row: row, Col: col, Rowspan: 1, Colspan: 1, Text: v})
		}
	}
	f, _, err = validateFragment(f)
	return f, err
}

func fragmentSize(f *Fragment) int {
	n := 0
	for _, c := range f.Cells {
		n += len(c.Tag) + len(c.Source) + len(c.Text)
		if n > MaxFragmentBytes {
			return n
		}
	}
	for _, s := range f.RowTags {
		n += len(s)
		if n > MaxFragmentBytes {
			return n
		}
	}
	for _, s := range f.Delimiters {
		n += len(s)
		if n > MaxFragmentBytes {
			return n
		}
	}
	return n
}

// validateFragment は呼び出し元の値を変えず、正規化した写しと検査済みの内部表現を返す。
func validateFragment(input *Fragment) (*Fragment, *table.Piece, error) {
	bad := func(s string) (*Fragment, *table.Piece, error) { return nil, nil, fault("invalid", s) }
	if input == nil {
		return bad("写しが無い")
	}
	if input.Version != FragmentVersion {
		return bad("写しの版が違う")
	}
	if input.Kind != "" && input.Kind != "pipe" && input.Kind != "html" {
		return bad("写しの表の種類が正しくない")
	}
	if input.Shape != "rows" && input.Shape != "cols" && input.Shape != "cells" && input.Shape != "table" {
		return bad("写しの範囲の種類が正しくない")
	}
	if input.Rows < 1 || input.Cols < 1 || input.Rows > MaxFragmentCells || input.Cols > MaxFragmentCells || input.Rows > MaxFragmentCells/input.Cols {
		return bad("写しの行・列の個数が上限を超えるか、空")
	}
	if len(input.Cells) > input.Rows*input.Cols || len(input.RowTags) > input.Rows || len(input.Delimiters) > input.Cols || fragmentSize(input) > MaxFragmentBytes {
		return bad("写しの大きさが上限を超える")
	}
	f := *input
	f.Cells = append([]FragmentCell{}, input.Cells...)
	f.RowTags = append([]string{}, input.RowTags...)
	f.Delimiters = append([]string{}, input.Delimiters...)
	normalize := func(s *string) error { *s = newlines(*s); return validInput(*s) }
	for i := range f.RowTags {
		if e := normalize(&f.RowTags[i]); e != nil {
			return bad(e.Error())
		}
	}
	for i := range f.Delimiters {
		if e := normalize(&f.Delimiters[i]); e != nil {
			return bad(e.Error())
		}
	}
	if f.Kind != "html" && len(f.RowTags) > 0 || f.Kind != "pipe" && len(f.Delimiters) > 0 {
		return bad("写しの種類と行のタグ・区切りが合わない")
	}
	if f.Kind == "html" && len(f.RowTags) != 0 && len(f.RowTags) != f.Rows {
		return bad("写しの行のタグの個数が違う")
	}
	for _, tag := range f.RowTags {
		if tag != "" {
			if _, e := table.PieceTag(tag, "tr"); e != nil {
				return bad(e.Error())
			}
		}
	}
	if len(f.Delimiters) != 0 && len(f.Delimiters) != f.Cols {
		return bad("写しの区切りの個数が違う")
	}
	for _, d := range f.Delimiters {
		if _, ok := table.PipeAlign(d); !ok {
			return bad("写しの区切り行のセルが正しくない")
		}
	}
	p := &table.Piece{Kind: string(f.Kind), Rows: f.Rows, Cols: f.Cols, Cells: []*table.PieceCell{}, RowTags: f.RowTags, Delimiters: f.Delimiters}
	grid := make([]bool, f.Rows*f.Cols)
	previous := -1
	for i := range f.Cells {
		c := &f.Cells[i]
		if c.Row < 0 || c.Col < 0 || c.Row >= f.Rows || c.Col >= f.Cols || c.Rowspan < 1 || c.Colspan < 1 || c.Rowspan > 65534 || c.Colspan > 1000 || c.Rowspan > f.Rows-c.Row || c.Colspan > f.Cols-c.Col {
			return bad("写しのセルの位置か結合が範囲外")
		}
		key := c.Row*f.Cols + c.Col
		if key <= previous {
			return bad("写しのセルの順序が違うか、重複している")
		}
		previous = key
		for row := c.Row; row < c.Row+c.Rowspan; row++ {
			for col := c.Col; col < c.Col+c.Colspan; col++ {
				at := row*f.Cols + col
				if grid[at] {
					return bad("写しのセルが重なる")
				}
				grid[at] = true
			}
		}
		for _, s := range []*string{&c.Tag, &c.Source, &c.Text} {
			if e := normalize(s); e != nil {
				return bad(e.Error())
			}
		}
		v := &table.PieceCell{Row: c.Row, Col: c.Col, Rowspan: c.Rowspan, Colspan: c.Colspan}
		switch f.Kind {
		case "":
			if c.Tag != "" || c.Source != "" {
				return bad("値だけの写しにタグや原文がある")
			}
			v.Text = cellsyntax.NormalizeListSpacing(model.NormalizeText(c.Text))
		case "pipe":
			if c.Tag != "" || c.Text != "" || c.Rowspan != 1 || c.Colspan != 1 {
				return bad("パイプの写しにタグ・値・結合がある")
			}
			text, e := table.ReadPipeSource(c.Source)
			if e != nil {
				return bad(fmt.Sprintf("写しの %d 行 %d 列: %s", c.Row+1, c.Col+1, e))
			}
			v.Text = text
			v.Raw = c.Source
			if len(f.Delimiters) > 0 {
				v.Align, _ = table.PipeAlign(f.Delimiters[c.Col])
			}
		case "html":
			if c.Text != "" {
				return bad("HTML の写しに値がある")
			}
			tok, e := table.PieceTag(c.Tag, "td", "th")
			if e != nil {
				return bad(e.Error())
			}
			if !table.ValidPieceSpan(tok, c.Rowspan, c.Colspan) {
				return bad("写しの結合とタグの属性が違うか、不正")
			}
			if e = checkCellSource(c.Source); e != nil {
				return bad(e.Error())
			}
			v.Raw = c.Tag + c.Source + table.PieceEndTag(c.Tag)
		}
		p.Cells = append(p.Cells, v)
	}
	if f.Kind == "pipe" {
		for _, covered := range grid {
			if !covered {
				return bad("パイプの写しにセルの無い位置がある")
			}
		}
	}
	if f.Kind == "html" {
		var e error
		p, e = table.ReadHTMLPiece(f.Rows, f.Cols, p.Cells, f.RowTags)
		if e != nil {
			return bad(e.Error())
		}
	}
	return &f, p, nil
}
