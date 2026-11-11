// Package model は表の格子とセルの正規化を持つ。
package model

import (
	"crypto/sha256"
	"fmt"
	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"regexp"
	"sort"
	"strings"
)

type Cell struct {
	Row, Col, Rowspan, Colspan int
	Text                       string
	Header                     bool
	Align                      string
}
type Position struct{ Row, Col int }
type TableModel struct {
	Kind       string
	Rows, Cols int
	Cells      []*Cell
	Holes      map[Position]bool
}

// model.Cell.__init__ に対応する通常セルの生成。
func NewCell(row, col int, text string) *Cell {
	return &Cell{Row: row, Col: col, Text: text, Rowspan: 1, Colspan: 1}
}

// model.Cell.key に対応するキー。
func (c *Cell) Key() []any {
	var align any
	if c.Align != "" {
		align = c.Align
	}
	return []any{c.Row, c.Col, c.Rowspan, c.Colspan, c.Header, align, c.Text}
}

// model.TableModel.__init__ に対応するモデル生成。
func New(kind string, rows, cols int, cells []*Cell, holes map[Position]bool) *TableModel {
	sort.SliceStable(cells, func(i, j int) bool {
		if cells[i].Row != cells[j].Row {
			return cells[i].Row < cells[j].Row
		}
		return cells[i].Col < cells[j].Col
	})
	if holes == nil {
		holes = map[Position]bool{}
	}
	return &TableModel{kind, rows, cols, cells, holes}
}

// model.TableModel.cell_at に対応する位置の検索。
func (m *TableModel) CellAt(row, col int) *Cell {
	for _, c := range m.Cells {
		if c.Row <= row && row < c.Row+c.Rowspan && c.Col <= col && col < c.Col+c.Colspan {
			return c
		}
	}
	return nil
}

// model.TableModel.anchors に対応する左上位置の辞書。
func (m *TableModel) Anchors() map[Position]*Cell {
	out := map[Position]*Cell{}
	for _, c := range m.Cells {
		out[Position{c.Row, c.Col}] = c
	}
	return out
}

// model.TableModel.signature に対応する内容の署名。
func (m *TableModel) Signature() map[string]any {
	cells := []any{}
	for _, c := range m.Cells {
		cells = append(cells, c.Key())
	}
	return map[string]any{"rows": m.Rows, "cols": m.Cols, "cells": cells}
}

// model.TableModel.fingerprint に対応する指紋。
func (m *TableModel) Fingerprint() string { return SHA256Text(pycompat.JSONDumps(m.Signature(), true)) }

// model.TableModel.same_content に対応する内容比較。
func (m *TableModel) SameContent(o *TableModel) bool {
	if m.Rows != o.Rows || m.Cols != o.Cols || len(m.Cells) != len(o.Cells) {
		return false
	}
	for i, c := range m.Cells {
		if *c != *o.Cells[i] {
			return false
		}
	}
	return true
}

// model.TableModel.shape に対応する形の比較。
func (m *TableModel) SameShape(o *TableModel) bool {
	if m.Rows != o.Rows || m.Cols != o.Cols || len(m.Cells) != len(o.Cells) || len(m.Holes) != len(o.Holes) {
		return false
	}
	for i, c := range m.Cells {
		d := o.Cells[i]
		if c.Row != d.Row || c.Col != d.Col || c.Rowspan != d.Rowspan || c.Colspan != d.Colspan {
			return false
		}
	}
	for p := range m.Holes {
		if !o.Holes[p] {
			return false
		}
	}
	return true
}

// model.sheet_form に対応する空セルの補完。
func SheetForm(m *TableModel) *TableModel {
	var cells []*Cell
	for _, c := range m.Cells {
		d := *c
		cells = append(cells, &d)
	}
	for p := range m.Holes {
		cells = append(cells, NewCell(p.Row, p.Col, ""))
	}
	return New("", m.Rows, m.Cols, cells, nil)
}

var trailingBlank = regexp.MustCompile(`[ \t]*(?:\n[ \t]*)*$`)
var leadingBlank = regexp.MustCompile(`^(?:[ \t]*\n)+`)

// model.normalize_text に対応するセルの正規化。
func NormalizeText(s string) string {
	s = strings.ReplaceAll(s, "_x000D_\n", "\n")
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	return trailingBlank.ReplaceAllString(leadingBlank.ReplaceAllString(s, ""), "")
}

// model.sha256_text に対応する SHA256。
func SHA256Text(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
