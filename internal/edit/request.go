package edit

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/ryo-aoki-pc/md-table-excel/tableedit"
)

type request struct {
	render              uint64
	kind                string
	table               int
	operation           operation
	rangeValue          Range
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
	side            tableedit.Side
	cells           []tableedit.CellEdit
	values          [][]string
	source          string
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
		rows, valid := m["values"].([]any)
		if !valid {
			return o, invalid()
		}
		total := 0
		for _, value := range rows {
			row, valid := value.([]any)
			if !valid {
				return o, invalid()
			}
			out := []string{}
			for _, item := range row {
				text, valid := stringValue(item)
				if !valid {
					return o, invalid()
				}
				out = append(out, text)
			}
			o.values = append(o.values, out)
			if total += len(out); total > maxOperationCells {
				return o, invalid()
			}
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
func (o operation) apply(ctx context.Context, d *tableedit.Document, t tableedit.Target) (*tableedit.Result, error) {
	if o.count > 0 {
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
			result, err = one.apply(ctx, d, t)
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
	}
	return nil, invalid()
}
