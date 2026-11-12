package tableedit_test

import (
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-table-excel/tableedit"
)

var moveOne = tableedit.Target{Number: 1}
var moveTwo = tableedit.Target{Number: 2}

// 既にある表の間の空行も、接頭辞を含む元のバイトで比較する。
func checkMoveText(t *testing.T, input, want string, op func(*tableedit.Document) (*tableedit.Result, error)) {
	t.Helper()
	for _, container := range []string{"本文", "引用", "リスト"} {
		for _, ending := range []string{"\n", "\r\n"} {
			text := tableInContainer(input, container, ending) + ending
			d := tableedit.Parse(text)
			r, err := op(d)
			if err != nil {
				t.Fatalf("%s/%q: %v", container, ending, err)
			}
			expected := tableInContainer(want, container, ending) + ending
			if r.Text != expected {
				t.Fatalf("%s/%q: 期待: %q、実際: %q", container, ending, expected, r.Text)
			}
			if d.Text() != text {
				t.Fatal("元の文書が変わった")
			}
			seen := map[string]bool{}
			for _, warning := range r.Warnings {
				if seen[warning] {
					t.Fatalf("警告が重なった: %q", warning)
				}
				seen[warning] = true
			}
		}
	}
}

func TestMoveRawRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name, text, want               string
		rows                           bool
		first, count, at, back, backAt int
	}{
		{"HTML の行", "<table><tr class='a'><TD>A</TD ></tr><tr><td>B</td></tr><tr><td>C</td></tr></table>", "<table><tr><td>B</td></tr><tr><td>C</td></tr><tr class='a'><TD>A</TD ></tr></table>", true, 0, 1, 2, 2, 0},
		{"HTML の列", "<table><tr><TH class='a'>A</TH ><td>B</td><td>C</td></tr><tr><td>D</td><td>E</td><td>F</td></tr></table>", "<table><tr><td>B</td><td>C</td><TH class='a'>A</TH ></tr><tr><td>E</td><td>F</td><td>D</td></tr></table>", false, 0, 1, 2, 2, 0},
		{"行を占める行", "<table>\n  <tr class='a'><td>A</td></tr>  \n\t<tr><td>B</td></tr>\n  <tr><td>C</td></tr>\n</table>", "<table>  \n\t<tr><td>B</td></tr>\n  <tr><td>C</td></tr>\n  <tr class='a'><td>A</td></tr>\n</table>", true, 0, 1, 2, 2, 0},
		{"行の並び方の混在", "<table>\n  <tr><td>A</td></tr>\n<tr><td>B</td></tr><tr><td>C</td></tr></table>", "<table>\n<tr><td>B</td></tr><tr><td>C</td></tr>\n  <tr><td>A</td></tr></table>", true, 0, 1, 2, 2, 0},
		{"セルの並び方の混在", "<table><tr>\n  <td>A</td>\n<td>B</td><td>C</td></tr></table>", "<table><tr>\n<td>B</td><td>C</td>\n  <td>A</td></tr></table>", false, 0, 1, 2, 2, 0},
		{"セルが 1 行ずつ", cellLinesHTML, "<table>\n  <tr>\n    <td>a</td>\n    <td>b</td>\n    <th rowspan=\"2\">区分</th>\n  </tr>\n  <tr>\n    <td>c</td>\n    <td>d</td>\n  </tr>\n</table>", false, 0, 1, 2, 2, 0},
		{"パイプの行", "| A | B |\n| :----: | ---: |\n| __c__ | d |\n| e | f |\n| g | h |", "| A | B |\n| :----: | ---: |\n| e | f |\n| g | h |\n| __c__ | d |", true, 1, 1, 3, 3, 1},
		{"パイプの列と空白", "|  **A**\t|__B__ | C |\n| :----:| ---:  |--- |\n| c |d\t| e |", "|  __B__\t|C | **A** |\n| ---:| ---  |:----: |\n| d |e\t| c |", false, 0, 1, 2, 2, 0},
		{"区画をまたぐ行", "<table>\n<thead class='h'><tr><th>A</th></tr><tr><th>H</th></tr></thead>\n<tbody><tr><td>B</td></tr></tbody>\n</table>", "<table>\n<thead class='h'><tr><th>H</th></tr></thead>\n<tbody><tr><td>B</td></tr><tr><th>A</th></tr></tbody>\n</table>", true, 0, 1, 2, 2, 0},
		{"縦結合を含む行", "<table><tr><td rowspan='2'>A</td><td>B</td></tr><tr><td>C</td></tr><tr><td>D</td><td>E</td></tr></table>", "<table><tr><td>D</td><td>E</td></tr><tr><td rowspan='2'>A</td><td>B</td></tr><tr><td>C</td></tr></table>", true, 0, 2, 2, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkMoveText(t, tc.text, tc.want, func(d *tableedit.Document) (*tableedit.Result, error) {
				if tc.rows {
					return d.MoveRows(moveOne, tc.first, tc.count, moveOne, tc.at, tableedit.After)
				}
				return d.MoveColumns(moveOne, tc.first, tc.count, moveOne, tc.at, tableedit.After)
			})
			checkMoveText(t, tc.text, tc.text, func(d *tableedit.Document) (*tableedit.Result, error) {
				var r *tableedit.Result
				var err error
				if tc.rows {
					r, err = d.MoveRows(moveOne, tc.first, tc.count, moveOne, tc.at, tableedit.After)
				} else {
					r, err = d.MoveColumns(moveOne, tc.first, tc.count, moveOne, tc.at, tableedit.After)
				}
				if err != nil {
					return nil, err
				}
				if tc.rows {
					return r.Document.MoveRows(moveOne, tc.back, tc.count, moveOne, tc.backAt, tableedit.Before)
				}
				return r.Document.MoveColumns(moveOne, tc.back, tc.count, moveOne, tc.backAt, tableedit.Before)
			})
		})
	}
}

func TestMoveRules(t *testing.T) {
	plain := "<table><tr><td>A</td><td>B</td><td>C</td></tr><tr><td>D</td><td>E</td><td>F</td></tr><tr><td>G</td><td>H</td><td>I</td></tr></table>"
	for _, rows := range []bool{true, false} {
		for _, end := range []int{0, 2} {
			d := tableedit.Parse(plain)
			var r *tableedit.Result
			var err error
			if rows {
				r, err = d.MoveRows(moveOne, 0, 2, moveOne, end, tableedit.Before)
			} else {
				r, err = d.MoveColumns(moveOne, 0, 2, moveOne, end, tableedit.Before)
			}
			if err != nil || r.Changed || r.Text != plain {
				t.Fatalf("端の移動: %v、%+v", err, r)
			}
		}
	}
	for _, tc := range []struct {
		name, text, want string
		op               func(*tableedit.Document) (*tableedit.Result, error)
	}{
		{"行の中", plain, "range", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveRows(moveOne, 0, 2, moveOne, 1, tableedit.Before)
		}},
		{"列の中", plain, "range", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveColumns(moveOne, 0, 2, moveOne, 1, tableedit.Before)
		}},
		{"行の結合", "<table><tr><td rowspan='2'>A</td></tr><tr></tr><tr><td>B</td></tr></table>", "merged", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveRows(moveOne, 0, 1, moveOne, 2, tableedit.After)
		}},
		{"先の行の結合", "<table><tr><td rowspan='2'>A</td></tr><tr></tr><tr><td>B</td></tr></table>", "merged", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveRows(moveOne, 2, 1, moveOne, 0, tableedit.After)
		}},
		{"列の結合", "<table><tr><td colspan='2'>A</td><td>B</td></tr></table>", "merged", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveColumns(moveOne, 0, 1, moveOne, 2, tableedit.After)
		}},
		{"先の列の結合", "<table><tr><td colspan='2'>A</td><td>B</td></tr></table>", "merged", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveColumns(moveOne, 2, 1, moveOne, 0, tableedit.After)
		}},
		{"見出し行", "| A |\n| --- |\n| B |", "unsupported", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveRows(moveOne, 0, 1, moveOne, 1, tableedit.After)
		}},
		{"見出しの上", "| A |\n| --- |\n| B |", "unsupported", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveRows(moveOne, 1, 1, moveOne, 0, tableedit.Before)
		}},
		{"最後の行", "<table><tr><td>A</td></tr></table>\n\n<table><tr><td>B</td></tr></table>", "unsupported", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveRows(moveOne, 0, 1, moveTwo, 0, tableedit.After)
		}},
		{"最後の列", "| A |\n| --- |\n\n| B |\n| --- |", "unsupported", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveColumns(moveOne, 0, 1, moveTwo, 0, tableedit.After)
		}},
		{"個数", plain, "range", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveRows(moveOne, 0, 0, moveOne, 2, tableedit.After)
		}},
		{"先の位置", plain, "range", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveColumns(moveOne, 0, 1, moveOne, 3, tableedit.After)
		}},
		{"側", plain, "invalid", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveRows(moveOne, 0, 1, moveOne, 2, 99)
		}},
		{"元の照合", plain, "stale", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveRows(tableedit.Target{Number: 1, SourceHash: "古い"}, 0, 1, moveOne, 2, tableedit.After)
		}},
		{"先の照合", plain, "stale", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveCells(moveOne, tableedit.Rect{Rows: 1, Cols: 1}, tableedit.Target{Number: 1, SourceHash: "古い"}, 1, 1)
		}},
		{"上書きの結合", "<table><tr><td>A</td><td colspan='2'>B</td></tr></table>", "merged", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveCells(moveOne, tableedit.Rect{Rows: 1, Cols: 1}, moveOne, 0, 1)
		}},
		{"セルの範囲", plain, "range", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveCells(moveOne, tableedit.Rect{Rows: 2, Cols: 2}, moveOne, 2, 2)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := tableedit.Parse(tc.text)
			r, err := tc.op(d)
			code(t, err, tc.want)
			if r != nil || d.Text() != tc.text {
				t.Fatal("失敗で文書が変わった")
			}
		})
	}
}

func TestMoveCellsOverlap(t *testing.T) {
	for _, tc := range []struct{ text, right, left string }{
		{"<table><tr><td class='a'>A</td><td>B</td><td>C</td></tr></table>", "<table><tr><td class='a'></td><td class='a'>A</td><td>B</td></tr></table>", "<table><tr><td>B</td><td>C</td><td></td></tr></table>"},
		{"| __A__ | B | C |\n| --- | --- | --- |", "|  | __A__ | B |\n| --- | --- | --- |", "| B | C |  |\n| --- | --- | --- |"},
	} {
		checkMoveText(t, tc.text, tc.right, func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveCells(moveOne, tableedit.Rect{Rows: 1, Cols: 2}, moveOne, 0, 1)
		})
		checkMoveText(t, tc.text, tc.left, func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveCells(moveOne, tableedit.Rect{Col: 1, Rows: 1, Cols: 2}, moveOne, 0, 0)
		})
		d := tableedit.Parse(tc.text)
		r, err := d.MoveCells(moveOne, tableedit.Rect{Rows: 1, Cols: 3}, moveOne, 0, 0)
		if err != nil || r.Changed || r.Text != tc.text {
			t.Fatalf("同じ位置の移動: %v", err)
		}
	}
	checkMoveText(t, "<table><tr><td>A</td><td>B</td></tr><tr><td>C</td><td>D</td></tr><tr><td>E</td><td>F</td></tr></table>", "<table><tr><td></td><td></td></tr><tr><td>A</td><td>B</td></tr><tr><td>C</td><td>D</td></tr></table>", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.MoveCells(moveOne, tableedit.Rect{Rows: 2, Cols: 2}, moveOne, 1, 0)
	})
}

func TestMoveBetweenTables(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
		op               func(*tableedit.Document) (*tableedit.Result, error)
	}{
		{"HTML 行", "<table><tr class='a'><TD>A</TD ></tr><tr><td>B</td></tr></table>\n\n<table><tr><td>C</td><td>D</td></tr></table>", "<table><tr><td>B</td></tr></table>\n\n<table><tr><td>C</td><td>D</td></tr><tr class='a'><TD>A</TD ><td></td></tr></table>", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveRows(moveOne, 0, 1, moveTwo, 0, tableedit.After)
		}},
		{"HTML 列", "<table><tr><TD class='a'>A</TD ><td>B</td></tr></table>\n\n<table><tr><td>C</td></tr></table>", "<table><tr><td>B</td></tr></table>\n\n<table><tr><td>C</td><TD class='a'>A</TD ></tr></table>", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveColumns(moveOne, 0, 1, moveTwo, 0, tableedit.After)
		}},
		{"HTML セル", "<table><tr><TD class='a'>A</TD ><td>B</td></tr></table>\n\n<table><tr><td>C</td></tr></table>", "<table><tr><TD class='a'></TD ><td>B</td></tr></table>\n\n<table><tr><TD class='a'>A</TD ></tr></table>", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveCells(moveOne, tableedit.Rect{Rows: 1, Cols: 1}, moveTwo, 0, 0)
		}},
		{"パイプ 行", "| A |\n| ---: |\n| __B__ |\n\n| C | D |\n| --- | --- |", "| A |\n| ---: |\n\n| C | D |\n| --- | --- |\n| __B__ | |", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveRows(moveOne, 1, 1, moveTwo, 0, tableedit.After)
		}},
		{"パイプ 列", "| A | B |\n| ---: | :----: |\n| C | D |\n\n| E |\n| --- |\n| F |", "| B |\n| :----: |\n| D |\n\n| E | A |\n| --- | ---: |\n| F | C |", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveColumns(moveOne, 0, 1, moveTwo, 0, tableedit.After)
		}},
		{"先から前へ", "<table><tr><td>A</td></tr></table>\n\n<table><tr><td>B</td></tr><tr><td>C</td></tr></table>", "<table><tr><td>B</td></tr><tr><td>A</td></tr></table>\n\n<table><tr><td>C</td></tr></table>", func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.MoveRows(moveTwo, 0, 1, moveOne, 0, tableedit.Before)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) { checkMoveText(t, tc.text, tc.want, tc.op) })
	}
}

func TestMoveNestedTable(t *testing.T) {
	nested := "<tr class='nested'><td>\n\n| A |\n| --- |\n| B |\n\n</td></tr>"
	same := "<table>" + nested + "\n<tr><td>残す</td></tr></table>"
	checkMoveText(t, same, same, func(d *tableedit.Document) (*tableedit.Result, error) {
		r, err := d.MoveRows(moveOne, 0, 1, moveOne, 1, tableedit.After)
		if err != nil {
			return nil, err
		}
		return r.Document.MoveRows(moveOne, 1, 1, moveOne, 0, tableedit.Before)
	})
	input := "<table>" + nested + "\n<tr><td>残す</td></tr></table>\n\n<table><tr><td>先</td></tr></table>"
	want := "<table>\n<tr><td>残す</td></tr></table>\n\n<table><tr><td>先</td></tr>" + nested + "</table>"
	d := tableedit.Parse(input)
	if len(d.Tables()) != 3 {
		t.Fatalf("入れ子の表: %+v", d.Tables())
	}
	r, err := d.MoveRows(moveOne, 0, 1, tableedit.Target{Number: 3}, 0, tableedit.After)
	if err != nil || r.Text != want {
		t.Fatalf("入れ子を含む行: %v、%+v", err, r)
	}
	if len(r.Document.Tables()) != 3 || r.Document.Table(3).Parent != 2 {
		t.Fatalf("移動後の表の番号: %+v", r.Document.Tables())
	}
	for _, op := range []func() (*tableedit.Result, error){
		func() (*tableedit.Result, error) { return d.MoveRows(moveOne, 1, 1, moveTwo, 0, tableedit.After) },
		func() (*tableedit.Result, error) { return d.MoveColumns(moveTwo, 0, 1, moveOne, 0, tableedit.After) },
		func() (*tableedit.Result, error) {
			return d.MoveCells(moveTwo, tableedit.Rect{Rows: 1, Cols: 1}, moveOne, 1, 0)
		},
	} {
		_, err := op()
		code(t, err, "unsupported")
	}
}

func TestMoveDifferentKindsAndWarnings(t *testing.T) {
	input := "<table><tr><td class='x'><b>A</b></td><td>B</td></tr><tr><td>C</td><td>D</td></tr></table>\n\n| E | F |\n| --- | --- |"
	checkMoveText(t, input, "<table><tr><td>C</td><td>D</td></tr></table>\n\n| E | F |\n| --- | --- |\n| **A** | B |", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.MoveRows(moveOne, 0, 1, moveTwo, 0, tableedit.After)
	})
	checkMoveText(t, input, "<table><tr><td class='x'></td><td>B</td></tr><tr><td>C</td><td>D</td></tr></table>\n\n| **A** | F |\n| --- | --- |", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.MoveCells(moveOne, tableedit.Rect{Rows: 1, Cols: 1}, moveTwo, 0, 0)
	})
	checkMoveText(t, input, "<table><tr><td class='x'>E</td><td>B</td></tr><tr><td>C</td><td>D</td></tr></table>\n\n|  | F |\n| --- | --- |", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.MoveCells(moveTwo, tableedit.Rect{Rows: 1, Cols: 1}, moveOne, 0, 0)
	})
	d := tableedit.Parse(input)
	r, err := d.MoveColumns(moveOne, 0, 1, moveTwo, 0, tableedit.After)
	if err == nil {
		t.Fatal("高すぎる列を受け付けた")
	}
	code(t, err, "range")
	r, err = d.MoveRows(moveOne, 0, 1, moveTwo, 0, tableedit.After)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, s := range r.Warnings {
		if seen[s] {
			t.Fatalf("警告が重なった: %q", s)
		}
		seen[s] = true
	}
	if !strings.Contains(strings.Join(r.Warnings, "/"), "値だけ") || !strings.Contains(strings.Join(r.Warnings, "/"), "複雑なセル") {
		t.Fatalf("変換の警告: %v", r.Warnings)
	}
	d = tableedit.Parse("| A | B |\n| --- | --- |\n| C | D | 余分 |")
	r, err = d.MoveColumns(moveOne, 0, 1, moveOne, 1, tableedit.After)
	if err != nil || strings.Contains(r.Text, "余分") || !strings.Contains(strings.Join(r.Warnings, "/"), "多いセルを消した") {
		t.Fatalf("列移動の警告: %v、%+v", err, r)
	}
}

func TestMoveMergedBlocksAndEmptySections(t *testing.T) {
	checkMoveText(t, "<table><thead><tr><th>A</th></tr></thead><tbody><tr><td>B</td></tr></tbody></table>", "<table><thead></thead><tbody><tr><td>B</td></tr><tr><th>A</th></tr></tbody></table>", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.MoveRows(moveOne, 0, 1, moveOne, 1, tableedit.After)
	})
	checkMoveText(t, "<table><tr><td colspan='2'>A</td><td colspan='2'>B</td><td colspan='2'>C</td></tr></table>", "<table><tr><td colspan='2'></td><td colspan='2'>A</td><td colspan='2'>B</td></tr></table>", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.MoveCells(moveOne, tableedit.Rect{Rows: 1, Cols: 4}, moveOne, 0, 2)
	})
	checkMoveText(t, "<table><tr><td colspan='2'>A</td><td>B</td><td>C</td></tr></table>", "<table><tr><td>B</td><td>C</td><td colspan='2'>A</td></tr></table>", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.MoveColumns(moveOne, 0, 2, moveOne, 3, tableedit.After)
	})
	checkMoveText(t, "<table><thead><tr><th rowspan='2' class='x'>A</th></tr><tr></tr></thead><tbody><tr><td>B</td></tr></tbody></table>\n\n<table><tr><td>C</td></tr></table>", "<table><thead></thead><tbody><tr><td>B</td></tr></tbody></table>\n\n<table><tr><td>C</td></tr><tr><th rowspan='2' class='x'>A</th></tr><tr></tr></table>", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.MoveRows(moveOne, 0, 2, moveTwo, 0, tableedit.After)
	})
	checkMoveText(t, "<table><tr><th class='x'>A</th><td>B</td></tr><tr><td>C</td><td>D</td></tr></table>\n\n| E |\n| --- |\n| F |", "<table><tr><td>B</td></tr><tr><td>D</td></tr></table>\n\n| E | A |\n| --- | --- |\n| F | C |", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.MoveColumns(moveOne, 0, 1, moveTwo, 0, tableedit.After)
	})
	input := "<table><tr><td>\n\n[x]: /x\n\n[x]\n\n</td></tr><tr><td>残す</td></tr></table>\n\n<table><tr><td>先</td></tr></table>"
	want := "<table><tr><td>残す</td></tr></table>\n\n<table><tr><td>先</td></tr><tr><td>\n\n[x]: /x\n\n[x]\n\n</td></tr></table>"
	// 参照の定義の知らせと読み直した警告を合わせても同じ文は一つだけ。
	r, err := tableedit.Parse(input).MoveRows(moveOne, 0, 1, moveTwo, 0, tableedit.After)
	if err != nil || r.Text != want {
		t.Fatalf("定義を含む行: %v、%+v", err, r)
	}
	seen := map[string]bool{}
	for _, warning := range r.Warnings {
		if seen[warning] {
			t.Fatalf("警告が重なった: %q", warning)
		}
		seen[warning] = true
	}
	if !strings.Contains(strings.Join(r.Warnings, "/"), "リンク参照の定義") {
		t.Fatalf("定義の警告がない: %v", r.Warnings)
	}
}
