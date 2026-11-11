package edit

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-table-excel/tableedit"
)

var updatePageFragments = flag.Bool("update", false, "ページと Go の写しの見本を作り直す")

func jsonObject(t *testing.T, value any) map[string]any {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTablesCopyMaterial(t *testing.T) {
	e := testEngine(t)
	text := `<table><tr class='行'><td style="color:red" rowspan="2">前</td><td>次</td></tr><tr><td>後</td></tr></table>` + "\n\n" + pipeTable
	s, h := testService(t, e, text, nil)
	r := getTables(t, s, h)
	html, pipe := r.Tables[0], r.Tables[1]
	if html.Hash == "" || html.RowTags[0] != "<tr class='行'>" || html.Cells[0].Tag != `<td style="color:red" rowspan="2">` || html.Cells[0].Source != "前" || html.Cells[0].Raw {
		t.Fatalf("HTML の写しの材料: %+v", html)
	}
	if !reflect.DeepEqual(pipe.Delimiters, []string{":---", "---:"}) || pipe.Cells[0].Source != "見出し" || pipe.Cells[0].Tag != "" || pipe.Cells[0].Raw {
		t.Fatalf("パイプの写しの材料: %+v", pipe)
	}
	if err := os.Remove(h.src.Path); err != nil {
		t.Fatal(err)
	}
	r = getTables(t, s, h)
	if r.Origin != "" || r.Document.Editable {
		t.Fatal(r)
	}
	for _, table := range r.Tables {
		if table.Editable || table.Hash != "" || table.CopyLocked != "" || len(table.Cells)+len(table.RowTags)+len(table.Delimiters) != 0 {
			t.Fatal(table)
		}
	}
}

func TestOriginDoesNotRevealPath(t *testing.T) {
	e := testEngine(t)
	s, h := testService(t, e, pipeTable, nil)
	origin := getTables(t, s, h).Origin
	if _, ok := hexValue(origin, 32); !ok || strings.Contains(origin, h.src.Path) || strings.Contains(origin, filepath.Base(h.src.Path)) {
		t.Fatal(origin)
	}
	if getTables(t, s, h).Origin != origin || s.origin(h.src) != origin {
		t.Fatal("同じ文書の照合値が変わった")
	}
	other := h.src
	other.Path = filepath.Join(h.src.Root, "other.md")
	if s.origin(other) == origin || New(s.opts).origin(h.src) == origin {
		t.Fatal("文書・起動の違いを区別しない")
	}
	other.Path, other.Buf = "", 3
	a := s.origin(other)
	other.Buf = 4
	if _, ok := hexValue(a, 32); !ok || a == s.origin(other) {
		t.Fatal("無名のバッファの照合値が正しくない")
	}
}

func TestPageFragmentsMatchCopy(t *testing.T) {
	e := testEngine(t)
	type item struct {
		Name     string              `json:"name"`
		Table    tableInfo           `json:"table"`
		Rect     tableedit.Rect      `json:"rect"`
		Shape    string              `json:"shape"`
		Fragment *tableedit.Fragment `json:"fragment"`
	}
	fixture := struct {
		Version int    `json:"version"`
		Cases   []item `json:"cases"`
	}{Version: 1, Cases: []item{}}
	html := "<table>\n<thead><tr class='見出し'><th>A</th><th>B</th></tr></thead>\n<tbody><tr data-x=\"行\"><td style='color:red' rowspan=\"2\"><strong>複雑</strong></td><td>B&amp;C</td></tr>\n<tr><td><em>後</em></td></tr></tbody>\n</table>\n"
	for _, c := range []struct {
		name, text string
		rect       tableedit.Rect
		locked     bool
	}{
		{"パイプ全体", pipeTable, tableedit.Rect{Rows: 2, Cols: 2}, false},
		{"パイプデータ行", pipeTable, tableedit.Rect{Row: 1, Rows: 1, Cols: 2}, false},
		{"パイプ一列", pipeTable, tableedit.Rect{Col: 1, Rows: 2, Cols: 1}, false},
		{"パイプ一セル", pipeTable, tableedit.Rect{Row: 1, Col: 1, Rows: 1, Cols: 1}, false},
		{"引用の中", "> " + strings.ReplaceAll(strings.TrimSuffix(pipeTable, "\n"), "\n", "\n> ") + "\n", tableedit.Rect{Rows: 2, Cols: 2}, false},
		{"リストの中", "- 項目\n\n  " + strings.ReplaceAll(strings.TrimSuffix(pipeTable, "\n"), "\n", "\n  ") + "\n", tableedit.Rect{Rows: 2, Cols: 2}, false},
		{"CRLF", strings.ReplaceAll(pipeTable, "\n", "\r\n"), tableedit.Rect{Rows: 2, Cols: 2}, false},
		{"HTML属性区画結合原文", html, tableedit.Rect{Rows: 3, Cols: 2}, false},
		{"HTML結合の行", html, tableedit.Rect{Row: 1, Rows: 2, Cols: 2}, false},
		{"HTML引用の中", "> " + strings.ReplaceAll(strings.TrimSuffix(html, "\n"), "\n", "\n> ") + "\n", tableedit.Rect{Rows: 3, Cols: 2}, false},
		{"HTMLリストの中", "- 項目\n\n  " + strings.ReplaceAll(strings.TrimSuffix(html, "\n"), "\n", "\n  ") + "\n", tableedit.Rect{Rows: 3, Cols: 2}, false},
		{"HTMLのCRLF", strings.ReplaceAll(html, "\n", "\r\n"), tableedit.Rect{Rows: 3, Cols: 2}, false},
		{"HTML一部の列", html, tableedit.Rect{Row: 1, Col: 1, Rows: 2, Cols: 1}, false},
		{"HTML結合を切る", html, tableedit.Rect{Row: 1, Rows: 1, Cols: 2}, true},
		{"写せないセル", "| good | tail\\ |\n| --- | --- |\n", tableedit.Rect{Rows: 1, Cols: 2}, true},
		{"写せないセルの隣", "| good | tail\\ |\n| --- | --- |\n", tableedit.Rect{Rows: 1, Cols: 1}, false},
	} {
		s, h := testService(t, e, c.text, nil)
		info := getTables(t, s, h).Tables[0]
		if !info.Editable {
			t.Fatalf("%s: %s", c.name, info.Reason)
		}
		f, err := tableedit.Parse(c.text).Copy(tableedit.Target{Number: info.Number, SourceHash: info.Hash}, c.rect)
		if (err != nil) != c.locked {
			t.Fatalf("%s: %v", c.name, err)
		}
		shape := "cells"
		if f != nil {
			shape = f.Shape
		}
		fixture.Cases = append(fixture.Cases, item{c.name, info, c.rect, shape, f})
	}
	data, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join("testdata", "page-fragments.json")
	if *updatePageFragments {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, want) {
		t.Fatal("写しの見本と一致しません。意図した変更なら -update で作り直してください。")
	}
}

func moveFrom(info tablesResponse, table int, coords map[string]any) map[string]any {
	f := map[string]any{"table": table, "render": info.Render, "hash": info.Tables[table].Hash, "origin": info.Origin}
	for key, value := range coords {
		f[key] = value
	}
	return f
}

func TestDuplicateInsertPasteMove(t *testing.T) {
	e := testEngine(t)
	p := "| A | B |\n| --- | --- |\n| C | D |\n| E | F |\n"
	h := "<table><tr class='r'><td style='color:red'>A</td><td>B</td></tr><tr><td>C</td><td>D</td></tr><tr><td>E</td><td>F</td></tr></table>\n"
	for _, kind := range []string{"pipe", "html"} {
		for _, op := range []string{"duplicateRows", "duplicateColumns", "insertRowsFrom", "insertColumnsFrom", "pasteFragment", "moveRows", "moveColumns", "moveCells", "別の表の行", "別の表の列", "別の表のセル", "値の行", "値の列"} {
			t.Run(kind+"/"+op, func(t *testing.T) {
				text := p
				if kind == "html" {
					text = h
				}
				cross := strings.HasPrefix(op, "別の表")
				if cross {
					text += "\n" + text
				}
				s, host := testService(t, e, text, nil)
				info := getTables(t, s, host)
				body := map[string]any{"render": info.Render, "table": 0}
				o := map[string]any{"type": op}
				want := ""
				r := tableedit.Rect{Row: 1, Rows: 1, Cols: 2}
				switch op {
				case "duplicateRows":
					o["row"], o["count"] = 1, 2
					if kind == "pipe" {
						want = p + "| C | D |\n| E | F |\n"
					} else {
						want = strings.Replace(h, "</table>", "<tr><td>C</td><td>D</td></tr><tr><td>E</td><td>F</td></tr></table>", 1)
					}
				case "duplicateColumns":
					o["col"], o["count"] = 0, 2
					if kind == "pipe" {
						want = "| A | B | A | B |\n| --- | --- | --- | --- |\n| C | D | C | D |\n| E | F | E | F |\n"
					} else {
						want = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(h, "<td>B</td>", "<td>B</td><td style='color:red'>A</td><td>B</td>"), "<td>D</td>", "<td>D</td><td>C</td><td>D</td>"), "<td>F</td>", "<td>F</td><td>E</td><td>F</td>")
					}
				case "insertRowsFrom", "insertColumnsFrom", "pasteFragment":
					if op == "insertColumnsFrom" {
						r = tableedit.Rect{Rows: 3, Cols: 1}
						o["col"], o["side"] = 1, "after"
					} else {
						o["row"] = 2
						if op == "pasteFragment" {
							o["col"] = 0
						} else {
							o["side"] = "after"
						}
					}
					f, err := tableedit.Parse(text).Copy(tableedit.Target{Number: 1}, r)
					if err != nil {
						t.Fatal(err)
					}
					o["fragment"] = f
					if op == "insertRowsFrom" {
						if kind == "pipe" {
							want = p + "| C | D |\n"
						} else {
							want = strings.Replace(h, "</table>", "<tr><td>C</td><td>D</td></tr></table>", 1)
						}
					}
					if op == "insertColumnsFrom" {
						if kind == "pipe" {
							want = "| A | B | A |\n| --- | --- | --- |\n| C | D | C |\n| E | F | E |\n"
						} else {
							want = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(h, "<td>B</td>", "<td>B</td><td style='color:red'>A</td>"), "<td>D</td>", "<td>D</td><td>C</td>"), "<td>F</td>", "<td>F</td><td>E</td>")
						}
					}
					if op == "pasteFragment" {
						want = strings.Replace(text, "| E | F |", "| C | D |", 1)
						if kind == "html" {
							want = strings.Replace(h, "<td>E</td><td>F</td>", "<td>C</td><td>D</td>", 1)
						}
					}
				case "値の行", "値の列":
					o["values"] = [][]string{{"X", "Y"}}
					if op == "値の行" {
						o["type"], o["row"], o["side"] = "insertRowsFrom", 2, "after"
						if kind == "pipe" {
							want = p + "| X | Y |\n"
						} else {
							want = strings.Replace(h, "</table>", "<tr><td>X</td><td>Y</td></tr></table>", 1)
						}
					} else {
						o["type"], o["col"], o["side"], o["values"] = "insertColumnsFrom", 1, "after", [][]string{{"X"}, {"Y"}, {"Z"}}
						if kind == "pipe" {
							want = "| A | B | X |\n| --- | --- | --- |\n| C | D | Y |\n| E | F | Z |\n"
						} else {
							want = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(h, "<td>B</td>", "<td>B</td><td>X</td>"), "<td>D</td>", "<td>D</td><td>Y</td>"), "<td>F</td>", "<td>F</td><td>Z</td>")
						}
					}
				default:
					coords := map[string]any{"row": 1, "count": 1}
					if op == "moveRows" || op == "別の表の行" {
						o["type"], o["row"], o["side"] = "moveRows", 2, "after"
						if cross {
							body["table"] = 1
							if kind == "pipe" {
								want = strings.Replace(p, "| C | D |\n", "", 1) + "\n" + p + "| C | D |\n"
							} else {
								want = strings.Replace(h, "<tr><td>C</td><td>D</td></tr>", "", 1) + "\n" + strings.Replace(h, "</table>", "<tr><td>C</td><td>D</td></tr></table>", 1)
							}
						} else {
							if kind == "pipe" {
								want = "| A | B |\n| --- | --- |\n| E | F |\n| C | D |\n"
							} else {
								want = strings.Replace(h, "<tr><td>C</td><td>D</td></tr><tr><td>E</td><td>F</td></tr>", "<tr><td>E</td><td>F</td></tr><tr><td>C</td><td>D</td></tr>", 1)
							}
						}
					}
					if op == "moveColumns" || op == "別の表の列" {
						coords = map[string]any{"col": 0, "count": 1}
						o["type"], o["col"], o["side"] = "moveColumns", 1, "after"
						if kind == "pipe" {
							want = "| B | A |\n| --- | --- |\n| D | C |\n| F | E |\n"
							if cross {
								body["table"] = 1
								want = "| B |\n| --- |\n| D |\n| F |\n\n| A | B | A |\n| --- | --- | --- |\n| C | D | C |\n| E | F | E |\n"
							}
						} else {
							want = "<table><tr class='r'><td>B</td><td style='color:red'>A</td></tr><tr><td>D</td><td>C</td></tr><tr><td>F</td><td>E</td></tr></table>\n"
							if cross {
								body["table"] = 1
								want = "<table><tr class='r'><td>B</td></tr><tr><td>D</td></tr><tr><td>F</td></tr></table>\n\n" + strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(h, "<td>B</td>", "<td>B</td><td style='color:red'>A</td>"), "<td>D</td>", "<td>D</td><td>C</td>"), "<td>F</td>", "<td>F</td><td>E</td>")
							}
						}
					}
					if op == "moveCells" || op == "別の表のセル" {
						coords = map[string]any{"row": 1, "col": 0, "rows": 2, "cols": 1}
						o["type"], o["row"], o["col"] = "moveCells", 1, 1
						if kind == "pipe" {
							want = "| A | B |\n| --- | --- |\n|  | C |\n|  | E |\n"
							if cross {
								body["table"] = 1
								want = "| A | B |\n| --- | --- |\n|  | D |\n|  | F |\n\n| A | B |\n| --- | --- |\n| C | C |\n| E | E |\n"
							}
						} else {
							want = strings.ReplaceAll(strings.ReplaceAll(h, "<td>C</td><td>D</td>", "<td></td><td>C</td>"), "<td>E</td><td>F</td>", "<td></td><td>E</td>")
							if cross {
								body["table"] = 1
								want = strings.ReplaceAll(strings.ReplaceAll(h, "<td>C</td>", "<td></td>"), "<td>E</td>", "<td></td>") + "\n" + strings.ReplaceAll(strings.ReplaceAll(h, "<td>D</td>", "<td>C</td>"), "<td>F</td>", "<td>E</td>")
							}
						}
					}
					o["from"] = moveFrom(info, 0, coords)
				}
				body["op"] = o
				if _, err := s.Apply(context.Background(), nil, jsonObject(t, body)); err != nil {
					t.Fatal(err)
				}
				if got := disk(t, host); got != want {
					t.Fatalf("結果=%q\n期待=%q", got, want)
				}
				if len(s.history.documents[pathKey(host.src.Path)].undo) != 1 {
					t.Fatal("履歴が一手でない")
				}
				if _, err := applyJSON(t, s, host, `{"undo":true}`); err != nil {
					t.Fatal(err)
				}
				if disk(t, host) != text {
					t.Fatal("一回で元のバイトに戻らない")
				}
			})
		}
	}
}

func TestFragmentFromOtherDocumentKeepsAttributes(t *testing.T) {
	e := testEngine(t)
	source := "<table><tr class='r'><td style='color:red' data-x=\"値\"><strong>原文</strong></td></tr></table>\n"
	s, h := testService(t, e, source, nil)
	info := getTables(t, s, h).Tables[0]
	// 別の文書へ渡せるのは、ページも受け取る JSON の表情報だけ。
	f := &tableedit.Fragment{Version: 1, Kind: info.Kind, Shape: "rows", Rows: 1, Cols: 1, RowTags: info.RowTags, Delimiters: info.Delimiters, Cells: []tableedit.FragmentCell{}}
	for _, c := range info.Cells {
		f.Cells = append(f.Cells, tableedit.FragmentCell{Row: c.Row, Col: c.Col, Rowspan: c.Rowspan, Colspan: c.Colspan, Tag: c.Tag, Source: c.Source})
	}
	target, host := testService(t, e, "<table><tr><td>先</td></tr></table>\n", nil)
	_, err := target.Apply(context.Background(), nil, jsonObject(t, map[string]any{"render": 1, "table": 0, "op": map[string]any{"type": "insertRowsFrom", "row": 0, "side": "after", "fragment": f}}))
	if err != nil {
		t.Fatal(err)
	}
	want := "<table><tr><td>先</td></tr><tr class='r'><td style='color:red' data-x=\"値\"><strong>原文</strong></td></tr></table>\n"
	if disk(t, host) != want || disk(t, h) != source {
		t.Fatal("別の文書への写しで属性か元の文書が変わった")
	}
}

func TestMoveCellsOverlapAndOriginalBytes(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct {
		name, text, want                   string
		row, col, rows, cols, toRow, toCol int
	}{
		{"パイプの重なり", "| A | B | C |\n| --- | --- | --- |\n| D | E | F |\n", "| A | B | C |\n| --- | --- | --- |\n|  | D | E |\n", 1, 0, 1, 2, 1, 1},
		{"HTMLの重なり", "<table><tr><td class='a'>A</td><td class='b'>B</td><td>C</td></tr></table>\n", "<table><tr><td class='a'></td><td class='a'>A</td><td class='b'>B</td></tr></table>\n", 0, 0, 1, 2, 0, 1},
		{"CRLFとBOM", "\ufeff| A | B |\r\n| --- | --- |\r\n| C | D |\r\n", "\ufeff| A | B |\r\n| --- | --- |\r\n|  | C |\r\n", 1, 0, 1, 1, 1, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h := testService(t, e, c.text, nil)
			from := moveFrom(getTables(t, s, h), 0, map[string]any{"row": c.row, "col": c.col, "rows": c.rows, "cols": c.cols})
			_, err := s.Apply(context.Background(), nil, jsonObject(t, map[string]any{"render": 1, "table": 0, "op": map[string]any{"type": "moveCells", "row": c.toRow, "col": c.toCol, "from": from}}))
			if err != nil {
				t.Fatal(err)
			}
			if disk(t, h) != c.want {
				t.Fatalf("結果=%q、期待=%q", disk(t, h), c.want)
			}
			if _, err := applyJSON(t, s, h, `{"undo":true}`); err != nil {
				t.Fatal(err)
			}
			if disk(t, h) != c.text {
				t.Fatal("一回で元のバイトへ戻らない")
			}
		})
	}
}

func TestMoveSourceChecks(t *testing.T) {
	e := testEngine(t)
	for _, field := range []string{"origin", "render", "hash", "table", "excluded"} {
		t.Run(field, func(t *testing.T) {
			s, h := testService(t, e, pipeTable+"\n::include{file=part.md}\n", map[string]string{"part.md": pipeTable})
			info := getTables(t, s, h)
			from := moveFrom(info, 0, map[string]any{"row": 1, "col": 0, "rows": 1, "cols": 1})
			want := 409
			switch field {
			case "origin":
				from[field] = strings.Repeat("0", 32)
			case "render":
				from[field] = 2
			case "hash":
				from[field] = strings.Repeat("0", 64)
			case "table":
				from[field] = 20
				want = 422
			case "excluded":
				from["table"] = 1
				want = 422
			}
			_, err := s.Apply(context.Background(), nil, jsonObject(t, map[string]any{"render": 1, "table": 0, "op": map[string]any{"type": "moveCells", "row": 1, "col": 1, "from": from}}))
			if err == nil || err.Status != want || want == 409 && err.Code != "stale" {
				t.Fatal(err)
			}
			if strings.Contains(err.Message, h.src.Path) || disk(t, h) != h.src.Text || len(s.history.documents) != 0 {
				t.Fatal("検査で文書を変えたか、パスを出した")
			}
		})
	}
}

func TestNewOperationsCancelled(t *testing.T) {
	e := testEngine(t)
	for _, kind := range []string{"duplicateRows", "duplicateColumns", "insertRowsFrom", "insertColumnsFrom", "pasteFragment", "moveRows", "moveColumns", "moveCells"} {
		t.Run(kind, func(t *testing.T) {
			s, h := testService(t, e, pipeTable, nil)
			f, err := tableedit.ValuesFragment([][]string{{"値"}})
			if err != nil {
				t.Fatal(err)
			}
			o := map[string]any{"type": kind, "row": 1, "col": 1, "count": 1, "side": "after", "fragment": f}
			switch kind {
			case "duplicateRows":
				delete(o, "col")
				delete(o, "side")
				delete(o, "fragment")
			case "duplicateColumns":
				delete(o, "row")
				delete(o, "side")
				delete(o, "fragment")
			case "insertRowsFrom":
				delete(o, "col")
				delete(o, "count")
			case "insertColumnsFrom":
				delete(o, "row")
				delete(o, "count")
			case "pasteFragment":
				delete(o, "count")
				delete(o, "side")
			default:
				delete(o, "count")
				delete(o, "fragment")
				coords := map[string]any{"row": 1, "count": 1}
				if kind == "moveColumns" {
					delete(o, "row")
					coords = map[string]any{"col": 0, "count": 1}
				} else if kind == "moveRows" {
					delete(o, "col")
				} else {
					delete(o, "side")
					coords = map[string]any{"row": 1, "col": 0, "rows": 1, "cols": 1}
				}
				o["from"] = moveFrom(getTables(t, s, h), 0, coords)
			}
			// 対応づけを先に作り、取り消し済みでも窓口を呼ばないことを調べる。
			_ = getTables(t, s, h)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, got := s.Apply(ctx, nil, jsonObject(t, map[string]any{"render": 1, "table": 0, "op": o}))
			if got == nil || got.Status != 422 || got.Code != "cancelled" || disk(t, h) != pipeTable {
				t.Fatal(got)
			}
		})
	}
}

func TestFragmentErrorStatuses(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct{ text, op, code string }{
		{`<table><tr><td rowspan="2">A</td><td>B</td></tr><tr><td>C</td></tr></table>`, `{"type":"duplicateRows","row":0,"count":1}`, "merged"},
		{pipeTable, `{"type":"duplicateRows","row":90,"count":1}`, "range"},
		{pipeTable, `{"type":"insertRowsFrom","row":0,"side":"before","values":[["A","B"]]}`, "unsupported"},
		{pipeTable, `{"type":"pasteFragment","row":1,"col":0,"fragment":{"version":1,"kind":"html","shape":"cells","rows":1,"cols":1,"cells":[{"row":0,"col":0,"rowspan":1,"colspan":1,"text":"","tag":"<script>","source":"x"}],"row_tags":[],"delimiters":[]}}`, "invalid"},
	} {
		s, h := testService(t, e, c.text, nil)
		_, err := applyJSON(t, s, h, fmt.Sprintf(`{"table":0,"op":%s}`, c.op))
		if err == nil || err.Status != 422 || err.Code != c.code || strings.Contains(err.Message, h.src.Path) || disk(t, h) != c.text {
			t.Fatal(err)
		}
	}
}
