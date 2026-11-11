package tableedit_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ryo-aoki-pc/md-table-excel/tableedit"
)

func copyRect(t *testing.T, d *tableedit.Document, r tableedit.Rect) *tableedit.Fragment {
	t.Helper()
	f, err := d.Copy(target(d.Table(1)), r)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestFragmentFixtureRoundTrip(t *testing.T) {
	count := 0
	err := filepath.WalkDir("../testdata/documents", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, ending := range []string{"\n", "\r\n"} {
			text := strings.ReplaceAll(strings.ReplaceAll(strings.TrimPrefix(string(data), "\xef\xbb\xbf"), "\r\n", "\n"), "\r", "\n")
			text = strings.ReplaceAll(text, "\n", ending)
			d := tableedit.Parse(text)
			for _, tab := range d.Tables() {
				if tab.Excluded != "" || tab.CopyLocked != "" {
					continue
				}
				count++
				// -race では 10 倍以上遅くなり、CI の時間の上限を超える。この試験は並行の処理を含まないので、
				// race の検査では 8 表に 1 表だけを回す（race 無しの試験では全表を回す）。
				if raceEnabled && count%8 != 1 {
					continue
				}
				t.Run(fmt.Sprintf("%s/%d", filepath.Base(path), tab.Number), func(t *testing.T) {
					blocked := false
					for _, c := range tab.Cells {
						single := &tableedit.Fragment{Version: 1, Kind: tab.Kind, Shape: "cells", Rows: c.Rowspan, Cols: c.Colspan, Cells: []tableedit.FragmentCell{{Rowspan: c.Rowspan, Colspan: c.Colspan, Tag: c.Tag, Source: c.Source}}}
						data, _ := json.Marshal(single)
						_, parseErr := tableedit.ParseFragment(data)
						if (c.CopyLocked != "") != (parseErr != nil) {
							t.Fatalf("%d,%d: 印と受け取りの検査が一致しない", c.Row, c.Col)
						}
						if c.CopyLocked != "" {
							blocked = true
						}
					}
					f, err := d.Copy(target(tab), tableedit.Rect{Rows: tab.Rows, Cols: tab.Cols})
					if blocked {
						code(t, err, "unsupported")
						if err.Error() == "" {
							t.Fatal("断った理由が無い")
						}
					} else {
						if err != nil {
							t.Fatal(err)
						}
						data, err := json.Marshal(f)
						if err != nil {
							t.Fatal(err)
						}
						parsed, err := tableedit.ParseFragment(data)
						if err != nil {
							t.Fatalf("写しを受け取れない: %v", err)
						}
						if !reflect.DeepEqual(f, parsed) {
							t.Fatal("JSON の往復で写しが変わった")
						}
						result, err := d.PasteFragment(target(tab), 0, 0, parsed)
						if err != nil || result.Changed || result.Text != text {
							t.Fatalf("無編集の上書き: %v", err)
						}
					}
					var result *tableedit.Result

					if tab.RowsLocked == "" {
						// 末尾の 1 行が結合を切るときは merged を確かめる。
						start := tab.Rows - 1

						result, err = d.DuplicateRows(target(tab), start, tab.Rows-start)
						if err != nil {
							code(t, err, "merged")
							for {
								next := start
								for _, c := range tab.Cells {
									if c.Row < start && c.Row+c.Rowspan > start {
										next = min(next, c.Row)
									}
								}
								if start == next {
									break
								}
								start = next
							}
							result, err = d.DuplicateRows(target(tab), start, tab.Rows-start)
							if err != nil {
								t.Fatal(err)
							}
						}
						back, err := result.Document.DeleteRows(tableedit.Target{Number: tab.Number}, tab.Rows, tab.Rows-start)
						if err != nil || back.Text != text {
							t.Fatalf("行の複製の往復: %v", err)
						}
					}
					if tab.ColsLocked == "" {
						width := 1

						result, err = d.DuplicateColumns(target(tab), 0, width)
						if err != nil {
							code(t, err, "merged")
						} else {
							back, err := result.Document.DeleteColumns(tableedit.Target{Number: tab.Number}, width, width)
							if err != nil {
								t.Fatal(err)
							}
							if tab.Kind == "html" && back.Text != text {
								t.Fatalf("列の複製の往復で原文が変わった: \n元: %q\n後: %q", text, back.Text)
							}
							for i, c := range tab.Cells {
								got := back.Document.Table(tab.Number).Cells[i]
								if c.Row != got.Row || c.Col != got.Col || c.Text != got.Text || c.Header != got.Header || c.Align != got.Align {
									t.Fatal("列の複製の往復で値が変わった")
								}
							}
						}
					}
				})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("試験する表が無い")
	}
	t.Logf("%d 個の表を確認", count)
}

func TestFragmentJSONAndCopy(t *testing.T) {
	d := tableedit.Parse("<table><tr class='x'><th>A</th><td>B</td></tr><tr><td>C</td><td>D</td></tr></table>")
	for _, tc := range []struct {
		r     tableedit.Rect
		shape string
	}{{tableedit.Rect{Rows: 2, Cols: 2}, "table"}, {tableedit.Rect{Rows: 1, Cols: 2}, "rows"}, {tableedit.Rect{Rows: 2, Cols: 1}, "cols"}, {tableedit.Rect{Rows: 1, Cols: 1}, "cells"}} {
		f := copyRect(t, d, tc.r)
		if f.Shape != tc.shape {
			t.Fatalf("範囲の形: %s", f.Shape)
		}
		data, _ := json.Marshal(d.Table(1))
		var tab tableedit.Table
		if err := json.Unmarshal(data, &tab); err != nil {
			t.Fatal(err)
		}
		got, err := tab.Copy(tc.r)
		if err != nil || !reflect.DeepEqual(f, got) {
			t.Fatalf("スナップショットの写しが違う: %v", err)
		}
		data, _ = json.Marshal(f)
		got, err = tableedit.ParseFragment(data)
		if err != nil || !reflect.DeepEqual(f, got) {
			t.Fatalf("写しの JSON: %v", err)
		}
	}
}

func TestFragmentCopyBoundsAndLocks(t *testing.T) {
	d := tableedit.Parse("<table><tr><td rowspan='2'>a</td><td>b</td></tr><tr><td>c</td></tr><tr><td colspan='2'>d</td></tr></table>")
	for _, r := range []tableedit.Rect{{Rows: 1, Cols: 2}, {Row: 1, Rows: 1, Cols: 2}, {Row: 2, Rows: 1, Cols: 1}} {
		_, err := d.Copy(target(d.Table(1)), r)
		code(t, err, "merged")
		if err.(*tableedit.Error).Col != 0 {
			t.Fatal("結合の左上を報告しない")
		}
	}
	f := copyRect(t, d, tableedit.Rect{Rows: 2, Cols: 2})
	if len(f.Cells) != 3 || f.Cells[0].Rowspan != 2 {
		t.Fatal("収まる結合を写せない")
	}
	for _, text := range []string{"<table><tr><td rowspan='0'>a</td></tr></table>", "<table><tr><td colspan='x'>a</td></tr></table>", "<table><tbody><tr><td rowspan='2'>a</td></tr></tbody></table>"} {
		d := tableedit.Parse(text)
		if d.Table(1).CopyLocked == "" {
			t.Fatal("不正な結合の写しを許す")
		}
		_, err := d.Copy(target(d.Table(1)), tableedit.Rect{Rows: 1, Cols: 1})
		code(t, err, "unsupported")
	}
	d = tableedit.Parse("<table><td>a<td>b</table>")
	f = copyRect(t, d, tableedit.Rect{Rows: 1, Cols: 2})
	data, _ := json.Marshal(f)
	if _, err := tableedit.ParseFragment(data); err != nil {
		t.Fatalf("省略タグの写し: %v", err)
	}
	d = tableedit.Parse("<table><tr><td>a</td><td>b</td></tr><tr><td>c</td></tr></table>")
	f = copyRect(t, d, tableedit.Rect{Rows: 2, Cols: 2})
	if len(f.Cells) != 3 {
		t.Fatal("穴にセルを作った")
	}
}

func TestFragmentRejectsUntrustedInput(t *testing.T) {
	good := copyRect(t, tableedit.Parse("<table><tr><td>a</td></tr></table>"), tableedit.Rect{Rows: 1, Cols: 1})
	mutations := map[string]func(*tableedit.Fragment){
		"版": func(f *tableedit.Fragment) { f.Version = 2 }, "種類": func(f *tableedit.Fragment) { f.Kind = "x" }, "形": func(f *tableedit.Fragment) { f.Shape = "x" }, "空": func(f *tableedit.Fragment) { f.Rows = 0 }, "上限": func(f *tableedit.Fragment) { f.Rows = 10001 }, "溢れ": func(f *tableedit.Fragment) { f.Cols = int(^uint(0) >> 1) },
		"重複": func(f *tableedit.Fragment) { f.Cells = append(f.Cells, f.Cells[0]) }, "範囲外": func(f *tableedit.Fragment) { f.Cells[0].Rowspan = 2 }, "値": func(f *tableedit.Fragment) { f.Cells[0].Text = "a" }, "制御文字": func(f *tableedit.Fragment) { f.Cells[0].Source = "\x01" }, "行タグ": func(f *tableedit.Fragment) { f.RowTags = []string{"<td>"} }, "行の続き": func(f *tableedit.Fragment) { f.RowTags = []string{"<tr><td>"} },
	}
	for _, tag := range []string{"<div>", "<td><b>", "</td>", "<td", "<td><td>", "<td/>", "<td\n\n class='x'>", "<td rowspan='2x'>", "<td rowspan='1' ROWSPAN='1'>", "<td colspan='2'>"} {
		value := tag
		mutations["タグ/"+tag] = func(f *tableedit.Fragment) { f.Cells[0].Tag = value }
	}
	for _, source := range []string{"a</td>", "</tr><tr>", "</table>", "<script>x</script>", "<!--", "<caption>a</caption>"} {
		value := source
		mutations["原文/"+source] = func(f *tableedit.Fragment) { f.Cells[0].Source = value }
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			f := *good
			f.Cells = append([]tableedit.FragmentCell{}, good.Cells...)
			mutate(&f)
			data, _ := json.Marshal(f)
			_, err := tableedit.ParseFragment(data)
			code(t, err, "invalid")
			_, err = tableedit.Parse("<table><tr><td>x</td></tr></table>").PasteFragment(tableedit.Target{Number: 1}, 0, 0, &f)
			code(t, err, "invalid")
		})
	}
	for _, data := range [][]byte{[]byte("null"), []byte("[]"), []byte(`{"version":1,"unknown":true}`), []byte(`{} {}`), {0xff}, []byte(`{"version":1.0}`), []byte(`{"version":1e0}`)} {
		_, err := tableedit.ParseFragment(data)
		code(t, err, "invalid")
	}
	for _, raw := range []string{"a|b", "a\\", "a\nb", " a", "a ", "<td>"} {
		f := &tableedit.Fragment{Version: 1, Kind: "pipe", Shape: "cells", Rows: 1, Cols: 1, Cells: []tableedit.FragmentCell{{Rowspan: 1, Colspan: 1, Source: raw}}}
		data, _ := json.Marshal(f)
		_, err := tableedit.ParseFragment(data)
		code(t, err, "invalid")
	}
	for _, raw := range []string{":-:-", "---x"} {
		f := &tableedit.Fragment{Version: 1, Kind: "pipe", Shape: "cells", Rows: 1, Cols: 1, Cells: []tableedit.FragmentCell{{Rowspan: 1, Colspan: 1, Source: "a"}}, Delimiters: []string{raw}}
		data, _ := json.Marshal(f)
		_, err := tableedit.ParseFragment(data)
		code(t, err, "invalid")
	}
	f := *good
	f.Cells = append([]tableedit.FragmentCell{}, good.Cells...)
	f.Cells[0].Source = "a\r\nb\rc"
	data, _ := json.Marshal(f)
	parsed, err := tableedit.ParseFragment(data)
	if err != nil || parsed.Cells[0].Source != "a\nb\nc" {
		t.Fatalf("改行の正規化: %v", err)
	}
}

func TestFragmentConcurrentCalls(t *testing.T) {
	d := tableedit.Parse("| A | B |\n| --- | --- |\n| C | D |\n")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if _, err := d.Copy(tableedit.Target{Number: 1}, tableedit.Rect{Rows: 2, Cols: 2}); err != nil {
					t.Error(err)
				}
				if _, err := d.DuplicateRows(tableedit.Target{Number: 1}, 1, 1); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
}

func TestFragmentCellCopyLocks(t *testing.T) {
	for _, input := range []string{
		"| good | tail\\ |\n| --- | --- |\n",
		"- 項目\n\n\t<table>\n\t<tr>\n\t  <td>good</td>\n\t  <td>\n\t\n\t- a\n\t- b\n\t\n\t</td>\n\t</tr>\n\t</table>\n",
	} {
		d := tableedit.Parse(input)
		tab := d.Table(1)
		if tab == nil || tab.Excluded != "" || tab.CopyLocked != "" || tab.Cells[0].CopyLocked != "" || tab.Cells[1].CopyLocked == "" {
			t.Fatalf("セルごとの印が正しくない: %+v", tab)
		}
		_, err := d.Copy(target(tab), tableedit.Rect{Rows: 1, Cols: 2})
		code(t, err, "unsupported")
		if err.Error() != tab.Cells[1].CopyLocked {
			t.Fatal("断る理由が印と違う")
		}
		f, err := d.Copy(target(tab), tableedit.Rect{Rows: 1, Cols: 1})
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(f)
		if _, err := tableedit.ParseFragment(data); err != nil {
			t.Fatalf("印のないセルを写せない: %v", err)
		}
		result, err := d.DuplicateRows(target(tab), 0, 1)
		next := mustChange(t, result, err)
		back, err := next.DeleteRows(tableedit.Target{Number: 1}, 1, 1)
		if err != nil || back.Text != input {
			t.Fatalf("印のセルのある行の複製の往復: %v", err)
		}
	}
}

func TestFragmentInputLimitsAndOrder(t *testing.T) {
	base := copyRect(t, tableedit.Parse("| a | b |\n| --- | --- |\n| c | d |"), tableedit.Rect{Rows: 2, Cols: 2})
	for name, mutate := range map[string]func(*tableedit.Fragment){
		"順序": func(f *tableedit.Fragment) { f.Cells[0], f.Cells[1] = f.Cells[1], f.Cells[0] },
		"重なり": func(f *tableedit.Fragment) {
			f.Kind = ""
			for i := range f.Cells {
				f.Cells[i].Text = f.Cells[i].Source
				f.Cells[i].Source = ""
			}
			f.Cells[0].Colspan = 2
		},
		"穴":         func(f *tableedit.Fragment) { f.Cells = f.Cells[1:] },
		"バイト数":      func(f *tableedit.Fragment) { f.Cells[0].Source = strings.Repeat("a", tableedit.MaxFragmentBytes+1) },
		"異種の項目":     func(f *tableedit.Fragment) { f.RowTags = []string{"<tr>", "<tr>"} },
		"不正な UTF-8": func(f *tableedit.Fragment) { f.Cells[0].Source = string([]byte{0xff}) },
	} {
		t.Run(name, func(t *testing.T) {
			f := *base
			f.Cells = append([]tableedit.FragmentCell{}, base.Cells...)
			mutate(&f)
			_, err := tableedit.Parse("| x | y |\n| --- | --- |").InsertRowsFrom(tableedit.Target{Number: 1}, 0, tableedit.After, &f)
			code(t, err, "invalid")
		})
	}
	_, err := tableedit.ParseFragment([]byte(strings.Repeat(" ", tableedit.MaxFragmentBytes+1)))
	code(t, err, "invalid")
	var nilTable *tableedit.Table
	_, err = nilTable.Copy(tableedit.Rect{Rows: 1, Cols: 1})
	code(t, err, "invalid")
	var nilDoc *tableedit.Document
	_, err = nilDoc.Copy(tableedit.Target{Number: 1}, tableedit.Rect{Rows: 1, Cols: 1})
	code(t, err, "invalid")
	d := tableedit.Parse("| a |\n| --- |")
	_, err = d.Copy(tableedit.Target{Number: 1, SourceHash: "古い値"}, tableedit.Rect{Rows: 1, Cols: 1})
	code(t, err, "stale")
}

var benchmarkSnapshot *tableedit.Table

// セルごとの原文の検査を含む、見本で最大の表の表情報の作成時間を測る。
func BenchmarkFragmentSnapshotLargest(b *testing.B) {
	var largest *tableedit.Document
	number, size, cells := 0, 0, 0
	name := ""
	err := filepath.WalkDir("../testdata/documents", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		d := tableedit.Parse(strings.TrimPrefix(string(data), "\xef\xbb\xbf"))
		for _, tab := range d.Tables() {
			if tab.Excluded == "" && tab.Rows*tab.Cols > size {
				largest = d
				number = tab.Number
				size = tab.Rows * tab.Cols
				cells = len(tab.Cells)
				name = path
			}
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	if largest == nil {
		b.Fatal("試験する表が無い")
	}
	b.Logf("対象: %s 表 %d、格子 %d、セル %d", name, number, size, cells)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkSnapshot = largest.Table(number)
	}
}
