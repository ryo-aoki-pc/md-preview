package table

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ryo-aoki-pc/md-table-excel/internal/blocks"
	"github.com/ryo-aoki-pc/md-table-excel/internal/cellsyntax"
	"github.com/ryo-aoki-pc/md-table-excel/internal/htmltok"
	"github.com/ryo-aoki-pc/md-table-excel/internal/inline"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
)

// Piece は公開の写しを検査した後の内部の形。
type Piece struct {
	Kind                         string
	Rows, Cols                   int
	Cells                        []*PieceCell
	RowTags, Delimiters, RowRaws []string
}
type PieceCell struct {
	Row, Col, Rowspan, Colspan int
	Header                     bool
	Align, Text, Raw           string
	Links                      bool
}

func orderPiece(p *Piece) {
	sort.Slice(p.Cells, func(i, j int) bool {
		a, b := p.Cells[i], p.Cells[j]
		return a.Row < b.Row || a.Row == b.Row && a.Col < b.Col
	})
}

var pieceDelimiter = regexp.MustCompile(`^:?-+:?$`)

// PipeAlign は区切り行の原文を検査し、配置を返す。
func PipeAlign(delimiter string) (string, bool) {
	if !pieceDelimiter.MatchString(delimiter) {
		return "", false
	}
	left, right := strings.HasPrefix(delimiter, ":"), strings.HasSuffix(delimiter, ":")
	if left && right {
		return "center", true
	}
	if left {
		return "left", true
	}
	if right {
		return "right", true
	}
	return "", true
}

// ReadPipeSource は縦棒の間の原文が一つのセルとして完結することを確かめる。
func ReadPipeSource(s string) (string, error) {
	if blocks.StripASCIISpace(s) != s || strings.ContainsAny(s, "\r\n") {
		return "", fmt.Errorf("パイプのセルの原文に改行や前後の空白がある")
	}
	row := blocks.ParseTableRow("| " + s + " |\n")
	if len(row.Cells) != 1 || blocks.StripASCIISpace(row.Cells[0].Raw) != s {
		return "", fmt.Errorf("パイプのセルの原文が一つのセルにならない")
	}
	// 空白を挟まない区切りも消えないことを確かめる（末尾の逆斜線）。
	probe := blocks.ParseTableRow("|" + s + "|x|\n")
	if len(probe.Cells) != 2 {
		return "", fmt.Errorf("パイプのセルの原文が区切りを消す")
	}
	for _, tok := range inline.Tokenize(s, true) {
		if tok.Kind == "html" && cellsyntax.Structural[tok.Tag] {
			return "", fmt.Errorf("パイプのセルの原文に表の構造のタグがある")
		}
	}
	return PipeCellToExcel(row.Cells[0].Content), nil
}

// PieceEndTag は開始タグと同じ大文字小文字の終了タグを作る。
func PieceEndTag(tag string) string {
	end := 1
	for end < len(tag) && (tag[end] >= 'a' && tag[end] <= 'z' || tag[end] >= 'A' && tag[end] <= 'Z' || tag[end] >= '0' && tag[end] <= '9') {
		end++
	}
	return "</" + tag[1:end] + ">"
}

// PieceTag は余分な字句や空行のない開始タグ一つを検査する。
func PieceTag(raw string, names ...string) (*htmltok.Token, error) {
	toks := htmltok.Tokenize(raw, 0, len(raw))
	if len(toks) != 1 || toks[0].Kind != "start" || toks[0].Start != 0 || toks[0].End != len(raw) || toks[0].SelfClosing || strings.Contains(raw, "\n\n") {
		return nil, fmt.Errorf("写しの開始タグが正しくない")
	}
	for _, name := range names {
		if toks[0].Name == name {
			return toks[0], nil
		}
	}
	return nil, fmt.Errorf("写しの開始タグの種類が正しくない")
}

// ValidPieceSpan は結合の属性と宣言の一致を検査する。
func ValidPieceSpan(tok *htmltok.Token, rows, cols int) bool {
	if !spanAttributesValid(&HTMLCell{StartToken: tok, Rowspan: rows, Colspan: cols}) {
		return false
	}
	return (tok.Attribute("rowspan") != nil || rows == 1) && (tok.Attribute("colspan") != nil || cols == 1)
}

// ReadHTMLPiece は小さな表を読み直し、セルの原文と置き方の両方を検査する。
func ReadHTMLPiece(rows, cols int, cells []*PieceCell, rowTags []string) (*Piece, error) {
	var b strings.Builder
	b.WriteString("<table>\n")
	for row := 0; row < rows; row++ {
		tag := "<tr>"
		if len(rowTags) > 0 && rowTags[row] != "" {
			tag = rowTags[row]
		}
		b.WriteString(tag)
		for _, c := range cells {
			if c.Row == row {
				b.WriteString(c.Raw)
			}
		}
		b.WriteString(PieceEndTag(tag) + "\n")
	}
	b.WriteString("</table>\n")
	regions := FindTables(NewMarkdown("", b.String(), false))
	if len(regions) == 0 || regions[0].Start != 0 || regions[0].Kind != "html" {
		return nil, fmt.Errorf("写しを HTML の表として読み直せない")
	}
	r := regions[0]
	if r.Excluded != "" {
		return nil, fmt.Errorf("%s", r.Excluded)
	}
	t := r.HTML
	if t.Model.Rows != rows || t.Model.Cols > cols || len(t.Cells) != len(cells) {
		return nil, fmt.Errorf("写しの行・列の形が原文と違う")
	}
	for i, row := range t.Rows {
		tag := "<tr>"
		if len(rowTags) > 0 && rowTags[i] != "" {
			tag = rowTags[i]
		}
		if row.Token == nil || row.Token.Text != tag || row.EndToken == nil {
			return nil, fmt.Errorf("写しの行のタグが原文と違う")
		}
	}
	p := &Piece{Kind: "html", Rows: rows, Cols: cols, RowTags: append([]string{}, rowTags...), Cells: []*PieceCell{}}
	for i, c := range cells {
		h := t.Cells[i]
		if h.Row != c.Row || h.Col != c.Col || h.Rowspan != c.Rowspan || h.Colspan != c.Colspan || h.EndToken == nil || cellRaw(t, h) != c.Raw {
			return nil, fmt.Errorf("写しの %d 行 %d 列の原文か結合が宣言と違う", c.Row+1, c.Col+1)
		}
		v := *c
		v.Text = h.Text
		v.Header = h.Tag == "th"
		v.Align = t.Model.CellAt(h.Row, h.Col).Align
		v.Links = h.LinkDefinitions
		p.Cells = append(p.Cells, &v)
	}
	return p, nil
}

// CopyPiece は同じ文書の原文を、そのまま扱える内部の写しにする。
func CopyPiece(r *Region, row, col, rows, cols int, rowRaws bool) (*Piece, error) {
	m := r.Model()
	if row < 0 || col < 0 || rows < 1 || cols < 1 || row >= m.Rows || col >= m.Cols || rows > m.Rows-row || cols > m.Cols-col {
		return nil, fmt.Errorf("写す範囲が表の外にはみ出す")
	}
	p := &Piece{Kind: r.Kind, Rows: rows, Cols: cols, Cells: []*PieceCell{}}
	for _, c := range m.Cells {
		if c.Row >= row+rows || c.Col >= col+cols || c.Row+c.Rowspan <= row || c.Col+c.Colspan <= col {
			continue
		}
		if c.Row < row || c.Col < col || c.Row+c.Rowspan > row+rows || c.Col+c.Colspan > col+cols {
			return nil, fmt.Errorf("写す範囲で結合セルが切れる")
		}
		v := &PieceCell{Row: c.Row - row, Col: c.Col - col, Rowspan: c.Rowspan, Colspan: c.Colspan, Header: c.Header, Align: c.Align, Text: c.Text}
		if r.Kind == "html" {
			h := r.HTML.CellAt[model.Position{Row: c.Row, Col: c.Col}]
			v.Raw = cellRaw(r.HTML, h)
			v.Links = h.LinkDefinitions
		} else {
			pr := r.Pipe.Header
			if c.Row > 0 {
				pr = r.Pipe.Rows[c.Row-1]
			}
			if c.Col < len(pr.RawCells) {
				v.Raw = pr.RawCells[c.Col]
			}
		}
		p.Cells = append(p.Cells, v)
	}
	if r.Kind == "pipe" {
		p.Delimiters = append([]string{}, pipeDelimiterCells(r)[col:col+cols]...)
	}
	if r.Kind == "html" && col == 0 && cols == m.Cols {
		p.RowTags = []string{}
		for _, r := range r.HTML.Rows[row : row+rows] {
			tag := ""
			if r.Token != nil {
				tag = r.Token.Text
			}
			p.RowTags = append(p.RowTags, tag)
		}
	}
	if rowRaws {
		for i := row; i < row+rows; i++ {
			if r.Kind == "html" {
				base := r.HTML.Rows[i]
				p.RowRaws = append(p.RowRaws, r.HTML.Text[base.Token.Start:base.EndToken.End])
			} else {
				pr := r.Pipe.Header
				if i > 0 {
					pr = r.Pipe.Rows[i-1]
				}
				p.RowRaws = append(p.RowRaws, pr.Body)
			}
		}
	}
	return p, nil
}
