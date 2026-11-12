package table

import (
	"fmt"
	"sort"

	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/blocks"
	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/model"
)

// CoveredCellLock は結合で消すと、ほかのセルや表の解釈まで変わる内容を断る。
func CoveredCellLock(c *HTMLCell) string {
	if c.LinkDefinitions {
		return "覆われるセルにリンクの参照の定義があるので、結合できません"
	}
	for _, token := range c.Tokens {
		if token.Kind == "start" && token.Name == "table" {
			return "覆われるセルに入れ子の表があるので、結合できません"
		}
		if token.Kind == "md" {
			for _, node := range blocks.ParseText(token.Text, false, true).Root.Walk() {
				if node.Kind == blocks.Table {
					return "覆われるセルに入れ子の表があるので、結合できません"
				}
			}
		}
	}
	return ""
}

// cellSpanTag は開始タグのほかの属性と引用符を保ち、結合の属性だけを変える。
func cellSpanTag(c *HTMLCell, rows, cols int) string {
	var edits []htmlEdit
	for _, v := range []struct {
		name string
		n    int
	}{{"rowspan", rows}, {"colspan", cols}} {
		if v.n > 1 || c.Attribute(v.name) != nil {
			edits = append(edits, spanEdit(c, v.name, v.n))
		}
	}
	// 後ろから当てるので、同じ位置では削除を挿入より後ろに並べる（先に当てる）。
	// 最初の属性が rowspan="1" のとき、その削除とタグ名の直後への colspan の挿入が重なり、
	// 挿入を先に当てると、削除が挿入した文字を消してしまう。
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].start != edits[j].start {
			return edits[i].start < edits[j].start
		}
		return edits[i].end-edits[i].start < edits[j].end-edits[j].start
	})
	raw := c.StartToken.Text
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		raw = raw[:e.start-c.StartToken.Start] + e.replacement + raw[e.end-c.StartToken.Start:]
	}
	return raw
}

// MergeHTMLCells は左上の原文を残し、覆われるセルだけを消す。空になった tr は残す。
// 範囲と結合の境界は呼び出し側で検査済みとする。
func MergeHTMLCells(r *Region, row, col, rows, cols int) ([]string, *model.TableModel, *Expectation, error) {
	t := r.HTML
	first := t.CellAt[model.Position{Row: row, Col: col}]
	if first == nil {
		return nil, nil, nil, fmt.Errorf("左上にセルが無いので、結合できません")
	}
	section := t.Rows[row].Section
	for _, base := range t.Rows[row : row+rows] {
		if base.Section != section {
			return nil, nil, nil, fmt.Errorf("thead・tbody・tfoot の区画をまたいでは結合できません")
		}
	}
	n := copyShape(t.Model)
	e := SourceExpectation(r)
	tag := cellSpanTag(first, rows, cols)
	edits := []htmlEdit{{first.StartToken.Start, first.StartToken.End, tag}}
	cells := []*model.Cell{}
	for _, c := range n.Cells {
		if c.Row < row || c.Row >= row+rows || c.Col < col || c.Col >= col+cols {
			cells = append(cells, c)
			continue
		}
		pos := model.Position{Row: c.Row, Col: c.Col}
		h := t.CellAt[pos]
		if h == first {
			c.Rowspan, c.Colspan = rows, cols
			cells = append(cells, c)
			e.Cells[pos] = tag + t.Text[first.StartToken.End:first.EndToken.End]
			continue
		}
		if reason := CoveredCellLock(h); reason != "" {
			return nil, nil, nil, fmt.Errorf("%s", reason)
		}
		edits = append(edits, removeHTMLRange(t, h.StartToken.Start, h.EndToken.End))
		delete(e.Cells, pos)
	}
	lines, err := applyHTMLEdits(r, t, edits)
	return lines, model.New(n.Kind, n.Rows, n.Cols, cells, n.Holes), e, err
}

// unmergedCell は左上のタグと有効な揃えだけを空セルへ引き継ぐ。
func unmergedCell(c *HTMLCell) string {
	align, style := cellAlign(c)
	attrs := ""
	if align != "" {
		if style {
			attrs = " style=\"text-align: " + align + "\""
		} else {
			attrs = " align=\"" + align + "\""
		}
	}
	return "<" + c.Tag + attrs + "></" + c.Tag + ">"
}

// UnmergeHTMLCell は左上以外を同じタグと揃えの空セルへ戻す。
func UnmergeHTMLCell(r *Region, row, col int) ([]string, *model.TableModel, *Expectation, error) {
	t := r.HTML
	c := t.CellAt[model.Position{Row: row, Col: col}]
	// 下の行の空のセルは、その行で左にあるセルの後ろへ入れる。左にセルの無い位置があると、
	// HTML の解釈ではその穴に入り、覆っていた列に置けない。
	for y := row + 1; y < row+c.Rowspan; y++ {
		for x := 0; x < col; x++ {
			if t.Model.Holes[model.Position{Row: y, Col: x}] {
				return nil, nil, nil, fmt.Errorf("結合の下の行の左にセルの無い位置があるので、結合を解除できません")
			}
		}
	}
	n := copyShape(t.Model)
	e := SourceExpectation(r)
	tag, raw := cellSpanTag(c, 1, 1), unmergedCell(c)
	edits := []htmlEdit{{c.StartToken.Start, c.StartToken.End, tag}}
	e.Cells[model.Position{Row: row, Col: col}] = tag + t.Text[c.StartToken.End:c.EndToken.End]
	anchor := n.CellAt(row, col)
	anchor.Rowspan, anchor.Colspan = 1, 1
	for y := row; y < row+c.Rowspan; y++ {
		base := t.Rows[y]
		if base.Token == nil || base.EndToken == nil {
			return nil, nil, nil, fmt.Errorf("<tr>・</tr> が省略されているので、結合を解除できません")
		}
		raws := []string{}
		at := col
		if y == row {
			at++
		}
		for x := at; x < col+c.Colspan; x++ {
			v := model.NewCell(y, x, "")
			v.Header, v.Align = anchor.Header, anchor.Align
			n.Cells = append(n.Cells, v)
			e.Cells[model.Position{Row: y, Col: x}] = raw
			raws = append(raws, raw)
		}
		if len(raws) == 0 {
			continue
		}
		// 同じ行の左上は、解除前の幅が at より大きいので明示して隣へ入れる。
		if y == row {
			insertion, _ := besideHTMLCells(t, c, false, raws)
			edits = append(edits, insertion)
		} else {
			edits = append(edits, htmlColumnInsertion(t, base, at, raws))
		}
	}
	lines, err := applyHTMLEdits(r, t, edits)
	return lines, model.New(n.Kind, n.Rows, n.Cols, n.Cells, n.Holes), e, err
}
