package edit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/ryo-aoki-pc/md-preview/internal/session"
	"github.com/ryo-aoki-pc/md-table-excel/tableedit"
)

type moveSource struct {
	table, row, col, rows, cols, count int
	render                             uint64
	hash, origin                       string
}

func (s *Service) origin(src session.Source) string {
	key := pathKey(src.Path)
	if src.Path == "" {
		key = "buffer:" + fmt.Sprint(src.Buf)
	}
	h := hmac.New(sha256.New, s.secret[:])
	_, _ = h.Write([]byte("md-preview table origin\x00" + key))
	return hex.EncodeToString(h.Sum(nil)[:16])
}

func hexValue(v any, size int) (string, bool) {
	s, ok := stringValue(v)
	if !ok || len(s) != size {
		return "", false
	}
	_, err := hex.DecodeString(s)
	return s, err == nil
}

func parseFrom(v any, kind string) (moveSource, *Error) {
	f := moveSource{}
	m, ok := v.(map[string]any)
	names := []string{"table", "render", "hash", "origin"}
	switch kind {
	case "moveRows":
		names = append(names, "row", "count")
	case "moveColumns":
		names = append(names, "col", "count")
	case "moveCells":
		names = append(names, "row", "col", "rows", "cols")
	default:
		return f, invalid()
	}
	if !ok || !fields(m, names...) {
		return f, invalid()
	}
	get := func(name string, min int) int { n, valid := integer(m[name], min); ok = ok && valid; return n }
	f.table = get("table", 0)
	f.render = uint64(get("render", 1))
	var a, b bool
	f.hash, a = hexValue(m["hash"], 64)
	f.origin, b = hexValue(m["origin"], 32)
	if kind != "moveColumns" {
		f.row = get("row", 0)
	}
	if kind != "moveRows" {
		f.col = get("col", 0)
	}
	if kind == "moveCells" {
		f.rows, f.cols = get("rows", 1), get("cols", 1)
		ok = ok && fragmentDimensions(f.rows, f.cols)
	} else {
		f.count = get("count", 1)
		ok = ok && f.count <= maxOperationCount
	}
	if !ok || !a || !b {
		return f, invalid()
	}
	return f, nil
}

func fragmentDimensions(rows, cols int) bool {
	return rows >= 1 && rows <= maxOperationCount && cols >= 1 && cols <= maxOperationCount && rows <= maxOperationCells/cols
}

func parseValues(v any) ([][]string, *Error) {
	rows, ok := v.([]any)
	if !ok {
		return nil, invalid()
	}
	values := [][]string{}
	total := 0
	for _, value := range rows {
		row, ok := value.([]any)
		if !ok {
			return nil, invalid()
		}
		out := []string{}
		for _, item := range row {
			text, ok := stringValue(item)
			if !ok {
				return nil, invalid()
			}
			out = append(out, text)
		}
		if total += len(out); total > maxOperationCells {
			return nil, invalid()
		}
		values = append(values, out)
	}
	return values, nil
}

// 型と量はここで断り、原文・結合・タグの意味は公開の窓口で確かめる。
func parseFragment(v any) (*tableedit.Fragment, *Error) {
	m, ok := v.(map[string]any)
	if !ok || !fields(m, "version", "kind", "shape", "rows", "cols", "cells", "row_tags", "delimiters") {
		return nil, invalid()
	}
	rows, a := integer(m["rows"], 1)
	cols, b := integer(m["cols"], 1)
	if !a || !b || !fragmentDimensions(rows, cols) {
		return nil, invalid()
	}
	if _, ok := integer(m["version"], 0); !ok {
		return nil, invalid()
	}
	for _, name := range []string{"kind", "shape"} {
		if _, ok := stringValue(m[name]); !ok {
			return nil, invalid()
		}
	}
	for name, count := range map[string]int{"row_tags": rows, "delimiters": cols} {
		items, ok := m[name].([]any)
		if !ok || len(items) != 0 && len(items) != count {
			return nil, invalid()
		}
		for _, item := range items {
			if _, ok := stringValue(item); !ok {
				return nil, invalid()
			}
		}
	}
	cells, ok := m["cells"].([]any)
	if !ok || len(cells) > rows*cols {
		return nil, invalid()
	}
	for _, value := range cells {
		c, ok := value.(map[string]any)
		if !ok || !fields(c, "row", "col", "rowspan", "colspan", "text", "tag", "source") {
			return nil, invalid()
		}
		for _, name := range []string{"row", "col", "rowspan", "colspan"} {
			if _, ok := integer(c[name], 0); !ok {
				return nil, invalid()
			}
		}
		for _, name := range []string{"text", "tag", "source"} {
			if _, ok := stringValue(c[name]); !ok {
				return nil, invalid()
			}
		}
	}
	data, err := json.Marshal(m)
	if err != nil {
		return nil, invalid()
	}
	f, err := tableedit.ParseFragment(data)
	if err != nil {
		return nil, tableError(err)
	}
	return f, nil
}
