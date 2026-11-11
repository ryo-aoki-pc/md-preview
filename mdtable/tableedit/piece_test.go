package tableedit_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-table-excel/tableedit"
)

func checkPieceText(t *testing.T, input, want string, op func(*tableedit.Document) (*tableedit.Result, error)) {
	t.Helper()
	for _, container := range []string{"本文", "引用", "リスト"} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(fmt.Sprintf("%s/%q", container, ending), func(t *testing.T) {
				text := tableInContainer(input, container, ending) + ending
				d := tableedit.Parse(text)
				result, err := op(d)
				if err != nil {
					t.Fatal(err)
				}
				expected := tableInContainer(want, container, ending) + ending
				// 新しい空行にはコンテナの空行用の接頭辞を付ける。
				expected = strings.ReplaceAll(expected, ending+"> "+ending, ending+">"+ending)
				expected = strings.ReplaceAll(expected, ending+"  "+ending, ending+ending)
				if result.Text != expected {
					t.Fatalf("原文が期待値と違う:\n期待: %q\n実際: %q", expected, result.Text)
				}
				if d.Text() != text {
					t.Fatal("元の文書が変わった")
				}
			})
		}
	}
}

func TestFragmentInsertHTMLRows(t *testing.T) {
	src := tableedit.Parse("<table><TR class='r' style=\"color:red\" disabled><TD\n class=x data-v='y' title=\"z\">__写し__</TD><th>右</th></TR></table>")
	f := copyRect(t, src, tableedit.Rect{Rows: 1, Cols: 2})
	for _, section := range []string{"thead", "tbody", "tfoot", ""} {
		t.Run(section, func(t *testing.T) {
			open, close := "", ""
			if section != "" {
				open = "<" + section + " class='s'>\n"
				close = "</" + section + ">\n"
			}
			input := "<table data-t='1'>\n" + open + "  <tr class='keep'>\n    <th>A</th>\n    <td>B</td>\n  </tr>\n" + close + "</table>"
			want := "<table data-t='1'>\n" + open + "  <tr class='keep'>\n    <th>A</th>\n    <td>B</td>\n  </tr>\n  <TR class='r' style=\"color:red\" disabled>\n    <TD\n class=x data-v='y' title=\"z\">__写し__</TD>\n    <th>右</th>\n  </TR>\n" + close + "</table>"
			checkPieceText(t, input, want, func(d *tableedit.Document) (*tableedit.Result, error) {
				return d.InsertRowsFrom(tableedit.Target{Number: 1}, 0, tableedit.After, f)
			})
		})
	}
	// 写しの穴と足りない右の列を、基準行の見出し・配置の空セルで補う。
	src = tableedit.Parse("<table><tr><td>x</td><td>y</td></tr><tr><td>z</td></tr></table>")
	f = copyRect(t, src, tableedit.Rect{Rows: 2, Cols: 2})
	checkPieceText(t, "<table><tr><th>A</th><th align='right'>B</th><td>C</td></tr></table>", "<table><tr><th>A</th><th align='right'>B</th><td>C</td></tr><tr><td>x</td><td>y</td><td></td></tr><tr><td>z</td><th align=\"right\"></th><td></td></tr></table>", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.InsertRowsFrom(tableedit.Target{Number: 1}, 0, tableedit.After, f)
	})
}

func TestFragmentInsertHTMLColumns(t *testing.T) {
	f := copyRect(t, tableedit.Parse("<table><tr><td rowspan='2' class='copy'>X</td></tr><tr></tr></table>"), tableedit.Rect{Rows: 2, Cols: 1})
	want := "<table>\n  <tr>\n    <th rowspan=\"2\">区分</th>\n    <td rowspan='2' class='copy'>X</td>\n    <td>a</td>\n    <td>b</td>\n  </tr>\n  <tr>\n    <td>c</td>\n    <td>d</td>\n  </tr>\n</table>"
	checkPieceText(t, cellLinesHTML, want, func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.InsertColumnsFrom(tableedit.Target{Number: 1}, 0, tableedit.After, f)
	})
	f = copyRect(t, tableedit.Parse("<table><tr><td>X</td></tr></table>"), tableedit.Rect{Rows: 1, Cols: 1})
	checkPieceText(t, "<table><tr><th>A</th></tr><tr><td>B</td></tr></table>", "<table><tr><td>X</td><th>A</th></tr><tr><td></td><td>B</td></tr></table>", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.InsertColumnsFrom(tableedit.Target{Number: 1}, 0, tableedit.Before, f)
	})
}

func TestFragmentPipeSourcesAndShortRows(t *testing.T) {
	input := "| **a** | __b__ |\n| :----: | ---: |\n| c |\n| d | e | 余分 |"
	checkPieceText(t, input, "| **a** | __b__ |\n| :----: | ---: |\n| **a** | __b__ |\n| c |\n| d | e | 余分 |", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.DuplicateRows(tableedit.Target{Number: 1}, 0, 1)
	})
	checkPieceText(t, input, "| **a** | __b__ |\n| :----: | ---: |\n| c |\n| d | e | 余分 |\n| d | e | 余分 |", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.DuplicateRows(tableedit.Target{Number: 1}, 2, 1)
	})
	checkPieceText(t, input, "| **a** | **a** | __b__ |\n| :----: | :----: | ---: |\n| c | c | |\n| d | d | e |", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.DuplicateColumns(tableedit.Target{Number: 1}, 0, 1)
	})
	src := tableedit.Parse("| **x** \\| y | z<br>w |\n| :----: | ---: |")
	f := copyRect(t, src, tableedit.Rect{Rows: 1, Cols: 2})
	checkPieceText(t, input, "| **a** | __b__ |\n| :----: | ---: |\n| c |\n| **x** \\| y | z<br>w |\n| d | e | 余分 |", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.InsertRowsFrom(tableedit.Target{Number: 1}, 1, tableedit.After, f)
	})
	checkPieceText(t, input, "| **a** | __b__ |\n| :----: | ---: |\n| **x** \\| y | z<br>w |\n| d | e | 余分 |", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.PasteFragment(tableedit.Target{Number: 1}, 1, 0, f)
	})
	checkPieceText(t, input, "| **x** \\| y | z<br>w |\n| :----: | ---: |\n| c |\n| d | e | 余分 |", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.PasteFragment(tableedit.Target{Number: 1}, 0, 0, f)
	})
	f = copyRect(t, tableedit.Parse("| - |\n| --- |"), tableedit.Rect{Rows: 1, Cols: 1})
	checkPieceText(t, "a|b\n---|---\nc|d", "a|b\n---|---\nc|d\n|-||", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.InsertRowsFrom(tableedit.Target{Number: 1}, 1, tableedit.After, f)
	})
}

func TestFragmentDifferentKindsAndValues(t *testing.T) {
	f := copyRect(t, tableedit.Parse("| __x__ | y |\n| --- | --- |"), tableedit.Rect{Rows: 1, Cols: 2})
	checkPieceText(t, "<table><tr><th>A</th><td>B</td></tr></table>", "<table><tr><th>A</th><td>B</td></tr><tr><th>\n\n__x__\n\n</th><td>y</td></tr></table>", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.InsertRowsFrom(tableedit.Target{Number: 1}, 0, tableedit.After, f)
	})
	checkPieceText(t, "<table><tr><th>A</th><td>B</td></tr></table>", "<table><tr><th>\n\n__x__\n\n</th><td>y</td></tr></table>", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.PasteFragment(tableedit.Target{Number: 1}, 0, 0, f)
	})
	f = copyRect(t, tableedit.Parse("<table><tr><td class='x'><b>X</b></td><td>Y</td></tr></table>"), tableedit.Rect{Rows: 1, Cols: 2})
	checkPieceText(t, "| A | B |\n| --- | --- |", "| A | B |\n| --- | --- |\n| **X** | Y |", func(d *tableedit.Document) (*tableedit.Result, error) {
		r, e := d.InsertRowsFrom(tableedit.Target{Number: 1}, 0, tableedit.After, f)
		if e == nil && len(r.Warnings) == 0 {
			t.Error("値だけの貼り付けの知らせが無い")
		}
		return r, e
	})
	v, err := tableedit.ValuesFragment([][]string{{"one", "two"}, {"three", "four"}})
	if err != nil {
		t.Fatal(err)
	}
	checkPieceText(t, "| A | B |\n| --- | --- |", "| A | B |\n| --- | --- |\n| one | two |\n| three | four |", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.InsertRowsFrom(tableedit.Target{Number: 1}, 0, tableedit.After, v)
	})
	_, err = tableedit.ValuesFragment([][]string{{"a"}, {"b", "c"}})
	code(t, err, "invalid")
	one, _ := tableedit.ValuesFragment([][]string{{"値"}})
	checkPieceText(t, "<table><tr><td colspan='2' style='color:red'>元</td></tr></table>", "<table><tr><td colspan='2' style='color:red'>値</td></tr></table>", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.PasteFragment(tableedit.Target{Number: 1}, 0, 0, one)
	})
	codeBlock, _ := tableedit.ValuesFragment([][]string{{"```\nx\n```"}})
	_, err = tableedit.Parse("| A |\n| --- |").InsertRowsFrom(tableedit.Target{Number: 1}, 0, tableedit.After, codeBlock)
	code(t, err, "unsupported")
	if !strings.Contains(err.Error(), "パイプの表のままでは書けない") {
		t.Fatal(err)
	}
}

func TestFragmentHTMLPastePreservesRaw(t *testing.T) {
	f := copyRect(t, tableedit.Parse("<table><tr><TH colspan='2' style=\"color:red\">__写し__</TH></tr></table>"), tableedit.Rect{Rows: 1, Cols: 2})
	checkPieceText(t, "<table><tr class='keep'><td colspan='2'>元</td></tr></table>", "<table><tr class='keep'><TH colspan='2' style=\"color:red\">__写し__</TH></tr></table>", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.PasteFragment(tableedit.Target{Number: 1}, 0, 0, f)
	})
	f = copyRect(t, tableedit.Parse("<table><tr><th>X</th><td>Y</td></tr></table>"), tableedit.Rect{Rows: 1, Cols: 2})
	checkPieceText(t, "<table><td>a<td>b</table>", "<table><th>X</th><td>Y</td></table>", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.PasteFragment(tableedit.Target{Number: 1}, 0, 0, f)
	})
	// 穴が同じなら穴を残し、違えば merged にする。
	f = copyRect(t, tableedit.Parse("<table><tr><td>X</td><td>Y</td></tr><tr><td>Z</td></tr></table>"), tableedit.Rect{Rows: 2, Cols: 2})
	checkPieceText(t, "<table><tr><td>a</td><td>b</td></tr><tr><td>c</td></tr></table>", "<table><tr><td>X</td><td>Y</td></tr><tr><td>Z</td></tr></table>", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.PasteFragment(tableedit.Target{Number: 1}, 0, 0, f)
	})
}

// 値だけの写し（信用しない JSON）の穴は空のセルとして貼り、貼り付け先も穴なら飛ばす。
func TestFragmentValuesHoleOverHole(t *testing.T) {
	f, err := tableedit.ParseFragment([]byte(`{"version":1,"kind":"","shape":"cells","rows":2,"cols":2,"cells":[` +
		`{"row":0,"col":0,"rowspan":1,"colspan":1,"text":"X","tag":"","source":""},` +
		`{"row":0,"col":1,"rowspan":1,"colspan":1,"text":"Y","tag":"","source":""},` +
		`{"row":1,"col":0,"rowspan":1,"colspan":1,"text":"Z","tag":"","source":""}],"row_tags":[],"delimiters":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	checkPieceText(t, "<table><tr><td>a</td><td>b</td></tr><tr><td>c</td></tr></table>", "<table><tr><td>X</td><td>Y</td></tr><tr><td>Z</td></tr></table>", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.PasteFragment(tableedit.Target{Number: 1}, 0, 0, f)
	})
}

func TestFragmentOperationRejections(t *testing.T) {
	merged := copyRect(t, tableedit.Parse("<table><tr><td colspan='2'>x</td></tr></table>"), tableedit.Rect{Rows: 1, Cols: 2})
	one := copyRect(t, tableedit.Parse("<table><tr><td>x</td></tr></table>"), tableedit.Rect{Rows: 1, Cols: 1})
	twoRows := copyRect(t, tableedit.Parse("<table><tr><td>x</td></tr><tr><td>y</td></tr></table>"), tableedit.Rect{Rows: 2, Cols: 1})
	for _, tc := range []struct {
		name, text, code string
		op               func(*tableedit.Document) (*tableedit.Result, error)
	}{
		{"行の境界", "<table><tr><td rowspan='2'>a</td></tr><tr></tr></table>", "merged", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.InsertRowsFrom(tableedit.Target{Number: 1}, 0, tableedit.After, one)
		}},
		{"列の境界", "<table><tr><td colspan='2'>a</td></tr></table>", "merged", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.InsertColumnsFrom(tableedit.Target{Number: 1}, 0, tableedit.After, one)
		}},
		{"幅", "<table><tr><td>a</td></tr></table>", "range", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.InsertRowsFrom(tableedit.Target{Number: 1}, 0, tableedit.After, merged)
		}},
		{"高さ", "<table><tr><td>a</td></tr></table>", "range", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.InsertColumnsFrom(tableedit.Target{Number: 1}, 0, tableedit.After, twoRows)
		}},
		{"列の指定", "<table><colgroup><col></colgroup><tr><td>a</td></tr></table>", "unsupported", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.InsertColumnsFrom(tableedit.Target{Number: 1}, 0, tableedit.After, one)
		}},
		{"穴の列", "<table><tr><td>a</td><td>b</td></tr><tr><td>c</td></tr></table>", "unsupported", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.DuplicateColumns(tableedit.Target{Number: 1}, 0, 1)
		}},
		{"上書きの形", "<table><tr><td>a</td><td>b</td></tr></table>", "merged", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.PasteFragment(tableedit.Target{Number: 1}, 0, 0, merged)
		}},
		{"上書きの範囲", "<table><tr><td>a</td></tr></table>", "range", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.PasteFragment(tableedit.Target{Number: 1}, 0, 0, merged)
		}},
		{"パイプの結合", "| a | b |\n| --- | --- |", "unsupported", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.InsertRowsFrom(tableedit.Target{Number: 1}, 0, tableedit.After, merged)
		}},
		{"見出しの上", "| a |\n| --- |", "unsupported", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.InsertRowsFrom(tableedit.Target{Number: 1}, 0, tableedit.Before, one)
		}},
		{"複製の範囲", "| a |\n| --- |", "range", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.DuplicateRows(tableedit.Target{Number: 1}, 0, 2)
		}},
		{"側", "| a |\n| --- |", "invalid", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.InsertRowsFrom(tableedit.Target{Number: 1}, 0, 42, one)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) { _, err := tc.op(tableedit.Parse(tc.text)); code(t, err, tc.code) })
	}
}

func TestFragmentDuplicateRawRoundTrip(t *testing.T) {
	for _, input := range []string{cellLinesHTML, "<table><tr class='r'><TD class='x'>a</TD ></tr><tr><td>b</td></tr></table>", "<table>\n<thead><tr><th>A</th></tr></thead>\n<tbody><tr><td>B</td></tr></tbody>\n</table>"} {
		for _, container := range []string{"本文", "引用", "リスト"} {
			for _, ending := range []string{"\n", "\r\n"} {
				t.Run(fmt.Sprintf("%s/%q/%d", container, ending, len(input)), func(t *testing.T) {
					text := tableInContainer(input, container, ending)
					d := tableedit.Parse(text)
					row := 0
					count := 1
					if input == cellLinesHTML {
						count = 2
					}
					result, err := d.DuplicateRows(tableedit.Target{Number: 1}, row, count)
					next := mustChange(t, result, err)
					back, err := next.DeleteRows(tableedit.Target{Number: 1}, row+count, count)
					if err != nil || back.Text != text {
						t.Fatalf("行の原文の往復: %v", err)
					}
					result, err = d.DuplicateColumns(tableedit.Target{Number: 1}, 0, 1)
					next = mustChange(t, result, err)
					back, err = next.DeleteColumns(tableedit.Target{Number: 1}, 1, 1)
					if err != nil || back.Text != text {
						t.Fatalf("列の原文の往復: %v", err)
					}
				})
			}
		}
	}
}

// 穴と複数の縦結合がある範囲も、結合を切らず一組として消せば原文に戻る。
func TestFragmentDuplicateHoleBlockRoundTrip(t *testing.T) {
	input := "<table><tr><td>x</td></tr><tr><td rowspan='3'>a</td></tr><tr><td>b</td><td rowspan='3'>c</td></tr><tr></tr><tr><td>d</td></tr></table>"
	checkPieceText(t, input, input, func(d *tableedit.Document) (*tableedit.Result, error) {
		result, err := d.DuplicateRows(tableedit.Target{Number: 1}, 1, 4)
		if err != nil {
			return nil, err
		}
		return result.Document.DeleteRows(tableedit.Target{Number: 1}, 5, 4)
	})
}

func TestFragmentWarningsAndMixedEndings(t *testing.T) {
	d := tableedit.Parse("| A |\n| --- |\n| B | 余分 |")
	r, err := d.DuplicateRows(tableedit.Target{Number: 1}, 0, 1)
	if err != nil || !strings.Contains(strings.Join(r.Warnings, "/"), "見出し行の写し") {
		t.Fatalf("見出しの知らせ: %v", err)
	}
	r, err = d.DuplicateColumns(tableedit.Target{Number: 1}, 0, 1)
	if err != nil || !strings.Contains(strings.Join(r.Warnings, "/"), "多いセルを消した") {
		t.Fatalf("余分なセルの知らせ: %v", err)
	}
	f := copyRect(t, tableedit.Parse("<table><tr><td>\n\n[x]: /x\n\n[x]\n\n</td></tr></table>"), tableedit.Rect{Rows: 1, Cols: 1})
	r, err = tableedit.Parse("<table><tr><td>元</td></tr></table>").InsertRowsFrom(tableedit.Target{Number: 1}, 0, tableedit.After, f)
	if err != nil || !strings.Contains(strings.Join(r.Warnings, "/"), "リンク参照の定義") {
		t.Fatalf("定義の知らせ: %v", err)
	}
	seen := map[string]bool{}
	for _, s := range r.Warnings {
		if seen[s] {
			t.Fatal("警告が重なった")
		}
		seen[s] = true
	}
	d = tableedit.Parse("前\n\n| A |\r\n| --- |\n| B |\r\n\n後\n")
	r, err = d.DuplicateRows(tableedit.Target{Number: 1}, 1, 1)
	want := "前\n\n| A |\r\n| --- |\n| B |\r\n| B |\r\n\n後\n"
	if err != nil || r.Text != want {
		t.Fatalf("改行の混在: %v、%+v", err, r)
	}
}
