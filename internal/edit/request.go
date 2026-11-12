package edit

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/ryo-aoki-pc/md-tools/mdtable/tableedit"
)

type request struct {
	render              uint64
	kind                string
	table               int
	operation           operation
	rangeValue          Range
	insertSide          string
	insertDocument      string
	text                string
	checked, undo, redo bool
}

// 1 回の要求で扱う行・列の個数とセルの数の上限。画面の操作では届かない大きさを断る。
const (
	maxOperationCount = 1000
	maxOperationCells = 10000
)

type operation struct {
	kind            string
	row, col, count int
	rows, cols      int
	side            tableedit.Side
	cells           []tableedit.CellEdit
	values          [][]string
	source          string
	to              string
	align           string
	header          bool
	fragment        *tableedit.Fragment
	from            moveSource
}

func integer(v any, min int) (int, bool) {
	var n float64
	switch x := v.(type) {
	case float64:
		n = x
	case int:
		n = float64(x)
	case uint64:
		n = float64(x)
	case json.Number:
		var e error
		n, e = strconv.ParseFloat(string(x), 64)
		if e != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	if math.IsNaN(n) || n < float64(min) || n > 9007199254740991 || n > float64(math.MaxInt) || math.Trunc(n) != n {
		return 0, false
	}
	return int(n), true
}
func fields(m map[string]any, names ...string) bool {
	if len(m) != len(names) {
		return false
	}
	for _, name := range names {
		if _, ok := m[name]; !ok {
			return false
		}
	}
	return true
}
func stringValue(v any) (string, bool) { s, ok := v.(string); return s, ok && utf8.ValidString(s) }
func parseRequest(m map[string]any) (request, *Error) {
	r := request{}
	if fields(m, "undo") || fields(m, "redo") {
		if v, ok := m["undo"].(bool); ok && v {
			r.undo = true
			return r, nil
		}
		if v, ok := m["redo"].(bool); ok && v {
			r.redo = true
			return r, nil
		}
		return r, invalid()
	}
	render, ok := integer(m["render"], 1)
	if !ok {
		return r, invalid()
	}
	r.render = uint64(render)
	switch {
	case fields(m, "render", "insert", "text"):
		r.kind = "insert"
		r.text, ok = stringValue(m["text"])
		if !ok {
			return r, invalid()
		}
		insert, ok := m["insert"].(map[string]any)
		if !ok {
			return r, invalid()
		}
		if fields(insert, "document") {
			r.insertDocument, ok = insert["document"].(string)
			if !ok || r.insertDocument != "start" && r.insertDocument != "end" {
				return r, invalid()
			}
			return r, nil
		}
		if !fields(insert, "anchor", "side") {
			return r, invalid()
		}
		r.insertSide, ok = insert["side"].(string)
		if !ok || r.insertSide != "before" && r.insertSide != "after" {
			return r, invalid()
		}
		value, ok := insert["anchor"].(string)
		if !ok {
			return r, invalid()
		}
		var e *Error
		r.rangeValue, e = ParseRange(value)
		return r, e
	case fields(m, "render", "table", "op"):
		r.kind = "table"
		r.table, ok = integer(m["table"], 0)
		if !ok {
			return r, invalid()
		}
		op, valid := m["op"].(map[string]any)
		if !valid {
			return r, invalid()
		}
		var e *Error
		r.operation, e = parseOperation(op)
		return r, e
	case fields(m, "render", "range", "text"):
		r.kind = "range"
		r.text, ok = stringValue(m["text"])
		if !ok {
			return r, invalid()
		}
		value, ok := m["range"].(string)
		if !ok {
			return r, invalid()
		}
		var e *Error
		r.rangeValue, e = ParseRange(value)
		return r, e
	case fields(m, "render", "checkbox", "checked"):
		r.kind = "checkbox"
		r.checked, ok = m["checked"].(bool)
		if !ok {
			return r, invalid()
		}
		value, ok := m["checkbox"].(string)
		if !ok {
			return r, invalid()
		}
		var e *Error
		r.rangeValue, e = ParseRange(value)
		return r, e
	}
	return r, invalid()
}
func parseOperation(m map[string]any) (operation, *Error) {
	o := operation{}
	o.kind, _ = m["type"].(string)
	var ok bool
	get := func(name string, min int) int { n, valid := integer(m[name], min); ok = ok && valid; return n }
	ok = true
	switch o.kind {
	case "convert":
		if !fields(m, "type", "to") {
			return o, invalid()
		}
		o.to, _ = m["to"].(string)
		if o.to != "html" && o.to != "pipe" {
			return o, invalid()
		}
	case "deleteTable":
		if !fields(m, "type") {
			return o, invalid()
		}
	case "mergeCells", "mergeCellsAsHTML", "setAlign", "setHeader":
		names := []string{"type", "row", "col", "rows", "cols"}
		if o.kind == "setAlign" {
			names = append(names, "align")
			var valid bool
			o.align, valid = m["align"].(string)
			if !valid || o.align != "" && o.align != "left" && o.align != "center" && o.align != "right" {
				return o, invalid()
			}
		}
		if o.kind == "setHeader" {
			names = append(names, "header")
			var valid bool
			o.header, valid = m["header"].(bool)
			if !valid {
				return o, invalid()
			}
		}
		if !fields(m, names...) {
			return o, invalid()
		}
		o.row, o.col = get("row", 0), get("col", 0)
		o.rows, o.cols = get("rows", 1), get("cols", 1)
		ok = ok && o.rows <= maxOperationCount && o.cols <= maxOperationCount && o.rows <= maxOperationCells/o.cols
	case "unmergeCells":
		if !fields(m, "type", "row", "col") {
			return o, invalid()
		}
		o.row, o.col = get("row", 0), get("col", 0)
	case "set":
		if !fields(m, "type", "cells") {
			return o, invalid()
		}
		cells, valid := m["cells"].([]any)
		if !valid {
			return o, invalid()
		}
		if len(cells) > maxOperationCells {
			return o, invalid()
		}
		for _, value := range cells {
			c, valid := value.(map[string]any)
			if !valid || !fields(c, "row", "col", "text") {
				return o, invalid()
			}
			row, a := integer(c["row"], 0)
			col, b := integer(c["col"], 0)
			text, cok := stringValue(c["text"])
			if !a || !b || !cok {
				return o, invalid()
			}
			o.cells = append(o.cells, tableedit.CellEdit{Row: row, Col: col, Text: text})
		}
	case "paste":
		if !fields(m, "type", "row", "col", "values") {
			return o, invalid()
		}
		o.row = get("row", 0)
		o.col = get("col", 0)
		var e *Error
		o.values, e = parseValues(m["values"])
		if e != nil {
			return o, e
		}
	case "duplicateRows", "duplicateColumns":
		axis := "row"
		if o.kind == "duplicateColumns" {
			axis = "col"
		}
		if !fields(m, "type", axis, "count") {
			return o, invalid()
		}
		if axis == "row" {
			o.row = get(axis, 0)
		} else {
			o.col = get(axis, 0)
		}
		o.count = get("count", 1)
		ok = ok && o.count <= maxOperationCount
	case "insertRowsFrom", "insertColumnsFrom", "pasteFragment", "moveRows", "moveColumns", "moveCells":
		names := []string{"type"}
		if o.kind != "insertColumnsFrom" && o.kind != "moveColumns" {
			names = append(names, "row")
			o.row = get("row", 0)
		}
		if o.kind != "insertRowsFrom" && o.kind != "moveRows" {
			names = append(names, "col")
			o.col = get("col", 0)
		}
		if o.kind != "pasteFragment" && o.kind != "moveCells" {
			names = append(names, "side")
			switch m["side"] {
			case "before":
				o.side = tableedit.Before
			case "after":
				o.side = tableedit.After
			default:
				return o, invalid()
			}
		}
		var e *Error
		switch o.kind {
		case "moveRows", "moveColumns", "moveCells":
			names = append(names, "from")
			if !fields(m, names...) {
				return o, invalid()
			}
			o.from, e = parseFrom(m["from"], o.kind)
		default:
			name := "fragment"
			if _, exists := m["values"]; exists && o.kind != "pasteFragment" {
				name = "values"
			}
			names = append(names, name)
			if !fields(m, names...) {
				return o, invalid()
			}
			if name == "fragment" {
				o.fragment, e = parseFragment(m[name])
			} else {
				o.values, e = parseValues(m[name])
				if e == nil {
					if len(o.values) == 0 || !fragmentDimensions(len(o.values), len(o.values[0])) {
						return o, invalid()
					}
					for _, row := range o.values {
						if len(row) > maxOperationCount {
							return o, invalid()
						}
					}
					var err error
					o.fragment, err = tableedit.ValuesFragment(o.values)
					if err != nil {
						e = tableError(err)
					}
				}
			}
		}
		if e != nil {
			return o, e
		}
	case "setSource":
		if !fields(m, "type", "row", "col", "source") {
			return o, invalid()
		}
		o.row = get("row", 0)
		o.col = get("col", 0)
		var valid bool
		o.source, valid = stringValue(m["source"])
		ok = ok && valid
	case "insertRows", "insertColumns", "deleteRows", "deleteColumns":
		axis := "row"
		if o.kind == "insertColumns" || o.kind == "deleteColumns" {
			axis = "col"
		}
		insert := o.kind == "insertRows" || o.kind == "insertColumns"
		if insert {
			if !fields(m, "type", axis, "side", "count") {
				return o, invalid()
			}
			switch m["side"] {
			case "before":
				o.side = tableedit.Before
			case "after":
				o.side = tableedit.After
			default:
				return o, invalid()
			}
		} else if !fields(m, "type", axis, "count") {
			return o, invalid()
		}
		if axis == "row" {
			o.row = get(axis, 0)
		} else {
			o.col = get(axis, 0)
		}
		o.count = get("count", 1)
		if o.count > maxOperationCount {
			return o, invalid()
		}
	default:
		return o, invalid()
	}
	if !ok {
		return o, invalid()
	}
	return o, nil
}
func (o operation) apply(ctx context.Context, d *tableedit.Document, t tableedit.Target, source func(moveSource) (tableedit.Target, *Error)) (*tableedit.Result, error) {
	if ctx.Err() != nil {
		return nil, fault(422, "cancelled", "表の編集を取り消しました。")
	}
	if o.count > 0 && (o.kind == "insertRows" || o.kind == "insertColumns" || o.kind == "deleteRows" || o.kind == "deleteColumns") {
		// 公開の表操作は途中の取り消しを受けないため、1 個ずつ呼んで期限も確認する。
		var result *tableedit.Result
		warnings := []string{}
		for i := 0; i < o.count; i++ {
			if ctx.Err() != nil {
				return nil, fault(422, "cancelled", "表の編集を取り消しました。")
			}
			one := o
			one.count = 0
			if o.kind == "insertRows" {
				one.row += i
			}
			if o.kind == "insertColumns" {
				one.col += i
			}
			var err error
			result, err = one.apply(ctx, d, t, source)
			if err != nil {
				return nil, err
			}
			if len(result.Text) > maxDocumentBytes {
				return nil, fault(422, "large", "編集後の文書が 16 MiB を超えています。")
			}
			for _, w := range result.Warnings {
				found := false
				for _, old := range warnings {
					if old == w {
						found = true
						break
					}
				}
				if !found {
					warnings = append(warnings, w)
				}
			}
			d = result.Document
			t.SourceHash = ""
		}
		result.Warnings = warnings
		return result, nil
	}
	switch o.kind {
	case "convert":
		return d.ConvertTable(t, o.to)
	case "deleteTable":
		return d.DeleteTable(t)
	case "mergeCells":
		return d.MergeCells(t, tableedit.Rect{Row: o.row, Col: o.col, Rows: o.rows, Cols: o.cols})
	case "mergeCellsAsHTML":
		return d.MergeCellsAsHTML(t, tableedit.Rect{Row: o.row, Col: o.col, Rows: o.rows, Cols: o.cols})
	case "unmergeCells":
		return d.UnmergeCells(t, o.row, o.col)
	case "setAlign":
		return d.SetAlign(t, tableedit.Rect{Row: o.row, Col: o.col, Rows: o.rows, Cols: o.cols}, o.align)
	case "setHeader":
		return d.SetHeader(t, tableedit.Rect{Row: o.row, Col: o.col, Rows: o.rows, Cols: o.cols}, o.header)
	case "set":
		return d.SetCells(t, o.cells)
	case "paste":
		return d.Paste(t, o.row, o.col, o.values)
	case "insertRows":
		return d.InsertRows(t, o.row, o.side, 1)
	case "deleteRows":
		return d.DeleteRows(t, o.row, 1)
	case "insertColumns":
		return d.InsertColumns(t, o.col, o.side, 1)
	case "deleteColumns":
		return d.DeleteColumns(t, o.col, 1)
	case "setSource":
		return d.SetCellSource(t, o.row, o.col, o.source)
	case "duplicateRows":
		return d.DuplicateRows(t, o.row, o.count)
	case "duplicateColumns":
		return d.DuplicateColumns(t, o.col, o.count)
	case "insertRowsFrom":
		return d.InsertRowsFrom(t, o.row, o.side, o.fragment)
	case "insertColumnsFrom":
		return d.InsertColumnsFrom(t, o.col, o.side, o.fragment)
	case "pasteFragment":
		return d.PasteFragment(t, o.row, o.col, o.fragment)
	case "moveRows", "moveColumns", "moveCells":
		from, e := source(o.from)
		if e != nil {
			return nil, e
		}
		switch o.kind {
		case "moveRows":
			return d.MoveRows(from, o.from.row, o.from.count, t, o.row, o.side)
		case "moveColumns":
			return d.MoveColumns(from, o.from.col, o.from.count, t, o.col, o.side)
		case "moveCells":
			return d.MoveCells(from, tableedit.Rect{Row: o.from.row, Col: o.from.col, Rows: o.from.rows, Cols: o.from.cols}, t, o.row, o.col)
		}
	}
	return nil, invalid()
}
