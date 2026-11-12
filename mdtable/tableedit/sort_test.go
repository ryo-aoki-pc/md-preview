package tableedit_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/mdtable/tableedit"
)

func TestSortRowsPreservesRawInContainers(t *testing.T) {
	for _, c := range []struct {
		name, source, want string
		row, col           int
		desc               bool
	}{
		{"パイプ昇順", "| 鍵 | 内容 |\n| :----- | ----: |\n|  10\t| **十** | 余り |\n| ２ | 二  |\n| -1.5 | [負](/x) |\n| | 空 |\n", "| 鍵 | 内容 |\n| :----- | ----: |\n| -1.5 | [負](/x) |\n| ２ | 二  |\n|  10\t| **十** | 余り |\n| | 空 |\n", 0, 0, false},
		{"パイプ降順と安定", "鍵 | 内容\n--- | ---\n1.0 | 一番\n100% | 二番\n2 | 三番\n| | 空 |\n項目2 | 文字\n", "鍵 | 内容\n--- | ---\n項目2 | 文字\n2 | 三番\n1.0 | 一番\n100% | 二番\n| | 空 |\n", 2, 0, true},
		{"HTML区画なし", "<table class='保持'>\n<TR title='十'><TD>10</TD><td><b>十</b></td></TR >\n<tr data-x=二><td>2</td><td>二&amp;保持</td></tr>\n</table>\n", "<table class='保持'>\n<tr data-x=二><td>2</td><td>二&amp;保持</td></tr>\n<TR title='十'><TD>10</TD><td><b>十</b></td></TR >\n</table>\n", 0, 0, false},
		{"HTML複数行", "<table>\n<tr>\n  <td>10</td>\n  <td><em>十</em></td>\n</tr>\n<tr><td>2</td><td>二</td></tr>\n</table>\n", "<table>\n<tr><td>2</td><td>二</td></tr>\n<tr>\n  <td>10</td>\n  <td><em>十</em></td>\n</tr>\n</table>\n", 1, 0, false},
		{"HTML同行とコメント", "<table><tr><td>10</td></tr><!--位置を保持--><tr title='二'><td>2</td></tr></table>\n", "<table><tr title='二'><td>2</td></tr><!--位置を保持--><tr><td>10</td></tr></table>\n", 0, 0, false},
		{"先頭のth行", "<table>\n<tr><th colspan='2'>見出し1</th></tr>\n<tr><th>見出し2</th><th>外</th></tr>\n<tr><td>10</td><td>十</td></tr>\n<tr><th>2</th><td>二</td></tr>\n<tr><th>1</th><th>本文中のth</th></tr>\n</table>\n", "<table>\n<tr><th colspan='2'>見出し1</th></tr>\n<tr><th>見出し2</th><th>外</th></tr>\n<tr><th>1</th><th>本文中のth</th></tr>\n<tr><th>2</th><td>二</td></tr>\n<tr><td>10</td><td>十</td></tr>\n</table>\n", 0, 0, false},
		{"列の結合を鍵にする", "<table><colgroup><col span='2'></colgroup>\n<tr><td colspan='2'>10</td></tr>\n<tr><td>外</td><td>2</td></tr>\n</table>\n", "<table><colgroup><col span='2'></colgroup>\n<tr><td>外</td><td>2</td></tr>\n<tr><td colspan='2'>10</td></tr>\n</table>\n", 1, 1, false},
		{"穴の鍵は空", "<table>\n<tr><td>穴</td></tr>\n<tr><td>外</td><td>2</td></tr>\n</table>\n", "<table>\n<tr><td>外</td><td>2</td></tr>\n<tr><td>穴</td></tr>\n</table>\n", 0, 1, false},
		{"区画の無い行の並びを分ける", "<table>\n<tr><td>30</td></tr><tr><td>3</td></tr>\n<tbody><tr><td>20</td></tr><tr><td>2</td></tr></tbody>\n<tr><td>10</td></tr><tr><td>1</td></tr>\n</table>\n", "<table>\n<tr><td>30</td></tr><tr><td>3</td></tr>\n<tbody><tr><td>20</td></tr><tr><td>2</td></tr></tbody>\n<tr><td>1</td></tr><tr><td>10</td></tr>\n</table>\n", 4, 0, false},
		{"複数のtbodyを分ける", "<table>\n<tbody><tr><td>30</td></tr><tr><td>3</td></tr></tbody>\n<tbody><tr><td>20</td></tr><tr><td>2</td></tr></tbody>\n</table>\n", "<table>\n<tbody><tr><td>30</td></tr><tr><td>3</td></tr></tbody>\n<tbody><tr><td>2</td></tr><tr><td>20</td></tr></tbody>\n</table>\n", 2, 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			mergeContainers(t, c.source, c.want, func(d *tableedit.Document) (*tableedit.Result, error) {
				return d.SortRows(target(d.Table(1)), c.row, c.col, c.desc)
			})
		})
	}
	for _, tag := range []string{"tbody", "tfoot"} {
		head := "<thead><tr><th>見出し</th></tr></thead>\n"
		body := "<tbody><tr><th>本文見出し</th></tr><tr><td>10</td></tr><tr><td>2</td></tr></tbody>\n"
		foot := "<tfoot><tr><th>脚見出し</th></tr><tr><td>30</td></tr><tr><td>3</td></tr></tfoot>\n"
		source := "<table>\n" + head + body + foot + "</table>\n"
		want, row := strings.Replace(source, "<tr><td>10</td></tr><tr><td>2</td></tr>", "<tr><td>2</td></tr><tr><td>10</td></tr>", 1), 2
		if tag == "tfoot" {
			want = strings.Replace(source, "<tr><td>30</td></tr><tr><td>3</td></tr>", "<tr><td>3</td></tr><tr><td>30</td></tr>", 1)
			row = 5
		}
		t.Run(tag, func(t *testing.T) {
			mergeContainers(t, source, want, func(d *tableedit.Document) (*tableedit.Result, error) {
				return d.SortRows(target(d.Table(1)), row, 0, false)
			})
		})
	}
}

func TestSortRowsKeepsMixedEndingsAndFinalNewline(t *testing.T) {
	for _, c := range []struct {
		source, want string
		row          int
	}{
		{"前\r\n\n| 鍵 |\n| --- |\r\n| 10 |\n| 2 |\r\n\r\n後\n", "前\r\n\n| 鍵 |\n| --- |\r\n| 2 |\r\n| 10 |\n\r\n後\n", 1},
		{"| 鍵 |\n| --- |\n| 10 |\n| 2 |", "| 鍵 |\n| --- |\n| 2 |\n| 10 |", 0},
		// 改行の無い末尾の行が途中へ移るときは、末尾へ来た行の改行を受け取り、末尾は改行なしのままにする。
		{"| 鍵 |\n| --- |\n| 10 |\r\n| 2 |", "| 鍵 |\n| --- |\n| 2 |\r\n| 10 |", 0},
		{"前\r\n| 鍵 |\n| --- |\n| 10 |\n| 2 |", "前\r\n| 鍵 |\n| --- |\n| 2 |\n| 10 |", 1},
		{"> | 鍵 |\n> | --- |\n> | 10 |\r\n> | 30 |\n> | 2 |", "> | 鍵 |\n> | --- |\n> | 2 |\n> | 10 |\r\n> | 30 |", 1},
		{"前\r\n\n<table>\r\n<tr>\n<td>10</td>\r\n</tr>\r\n<tr>\r\n<td>2</td>\n</tr>\n</table>\r\n\n後\n", "前\r\n\n<table>\r\n<tr>\r\n<td>2</td>\n</tr>\r\n<tr>\n<td>10</td>\r\n</tr>\n</table>\r\n\n後\n", 0},
		{"<table>\n<tr><td>10</td></tr>\r\n<tr><td>2</td></tr>\n</table>", "<table>\n<tr><td>2</td></tr>\r\n<tr><td>10</td></tr>\n</table>", 1},
	} {
		d := tableedit.Parse(c.source)
		r, err := d.SortRows(target(d.Table(1)), c.row, 0, false)
		mustChange(t, r, err)
		if r.Text != c.want {
			t.Fatalf("行の内外の改行が違う: 得た %q 期待 %q", r.Text, c.want)
		}
	}
}

func TestSortRowsLocksAndUnchanged(t *testing.T) {
	for _, c := range []struct {
		source, reason string
		row            int
	}{
		{"| 鍵 |\n| --- |\n| 1 |\n", "2 行未満", 0},
		{"<table><tr><th>見出し</th></tr><tr><td>1</td></tr></table>\n", "2 行未満", 1},
		{"<table><tr><th>見出し</th></tr><tr><th>次</th></tr></table>\n", "2 行未満", 0},
		{"<table><thead><tr><th>見出し</th></tr></thead><tbody><tr><td>2</td></tr><tr><td>1</td></tr></tbody></table>\n", "見出しの区画は並べ替えられません", 0},
		{"<table><tr><td rowspan='2'>1</td><td>2</td></tr><tr><td>1</td></tr></table>\n", "行の結合があるので並べ替えられません", 1},
		{"<table><tr><th rowspan='2'>見出し</th><th>外</th></tr><tr><td>2</td></tr><tr><td>1</td><td>外</td></tr></table>\n", "行の結合があるので並べ替えられません", 2},
		{"<table><tr><td colspan='0'>2</td></tr><tr><td>1</td></tr></table>\n", "結合の値", 0},
		{"<table><tr><td>2</tr><tr><td>1</tr></table>\n", "省略", 0},
		{"<table><tr><td>2</td><tr><td>1</td></table>\n", "省略", 0},
		{"<table><tbody><tr><td>2</td></tr><tr><td>1</td></tr></tbody><thead><tr><th>見出し</th></tr></thead></table>\n", "後ろ", 0},
	} {
		d := tableedit.Parse(c.source)
		tab := d.Table(1)
		if tab == nil || tab.Excluded != "" {
			t.Fatal("入力の表が対象外", tab)
		}
		if len(tab.SortLocked) != tab.Rows || !strings.Contains(tab.SortLocked[c.row], c.reason) {
			t.Fatal("理由が表情報にない", tab.SortLocked, c.reason)
		}
		for _, desc := range []bool{false, true} {
			_, err := d.SortRows(target(tab), c.row, 0, desc)
			var e *tableedit.Error
			if !errors.As(err, &e) || e.Code != "unsupported" || !strings.Contains(e.Message, c.reason) {
				t.Fatal("断る理由が違う", err, c.reason)
			}
			if d.Text() != c.source {
				t.Fatal("拒否時に原文を変えた")
			}
		}
	}
	for _, source := range []string{"| 鍵 |\n| --- |\n| 1.0 |\n| 100% |\n| ２ |\n", "<table><tr><th>見出し</th></tr><tr><td>1</td></tr><tr><td>2</td></tr></table>\n"} {
		d := tableedit.Parse(source)
		result, err := d.SortRows(target(d.Table(1)), 0, 0, false)
		if err != nil || result.Changed || result.Text != source {
			t.Fatal("整列済みの表を変えた", result, err)
		}
		for _, p := range [][2]int{{-1, 0}, {0, -1}, {d.Table(1).Rows, 0}, {0, d.Table(1).Cols}} {
			if _, err := d.SortRows(target(d.Table(1)), p[0], p[1], false); err == nil {
				t.Fatal("範囲外を受けた", p)
			}
		}
	}
	// ほかの区画の行結合は、選んだ区画の並べ替えを妨げない。
	source := "<table><thead><tr><th rowspan='2'>見出し</th><th>外</th></tr><tr><th>次</th></tr></thead><tbody><tr><td>2</td><td>外</td></tr><tr><td>1</td><td>外</td></tr></tbody></table>\n"
	d := tableedit.Parse(source)
	if got := d.Table(1).SortLocked; !reflect.DeepEqual(got, []string{"見出しの区画は並べ替えられません", "見出しの区画は並べ替えられません", "", ""}) {
		t.Fatal("区画ごとの理由が違う", got)
	}
	if _, err := d.SortRows(target(d.Table(1)), 2, 0, false); err != nil {
		t.Fatal("無関係な区画の結合で断った", err)
	}
}
