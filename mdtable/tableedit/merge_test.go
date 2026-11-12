package tableedit_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/mdtable/tableedit"
)

// mergeContainers は表の外の原文とコンテナ接頭辞・改行を一緒に確かめる。
func mergeContainers(t *testing.T, source, want string, edit func(*tableedit.Document) (*tableedit.Result, error)) {
	t.Helper()
	for _, ending := range []string{"\n", "\r\n"} {
		for _, container := range []struct{ name, before, prefix, after string }{
			{"本文", "前\n\n", "", "\n後\n\n| 別 |\n| --- |\n| 表 |\n"},
			{"引用", "前\n\n", "> ", "\n後\n"},
			{"リスト", "前\n\n- 項目\n\n", "  ", "\n- 次\n\n後\n"},
		} {
			t.Run(container.name+strings.ReplaceAll(ending, "\n", "LF"), func(t *testing.T) {
				wrap := func(body string) string {
					return strings.ReplaceAll(container.before+container.prefix+strings.ReplaceAll(strings.TrimSuffix(body, "\n"), "\n", "\n"+container.prefix)+"\n"+container.after, "\n", ending)
				}
				original := wrap(source)
				d := tableedit.Parse(original)
				r, err := edit(d)
				mustChange(t, r, err)
				if want != "" && r.Text != wrap(want) {
					t.Fatalf("書き換えのバイトが違う:\n得た %q\n期待 %q", r.Text, wrap(want))
				}
				before, after := strings.ReplaceAll(container.before, "\n", ending), strings.ReplaceAll(container.after, "\n", ending)
				if !strings.HasPrefix(r.Text, before) || !strings.HasSuffix(r.Text, after) || d.Text() != original {
					t.Fatalf("範囲外か元の文書を変えた: %q", r.Text)
				}
				if ending == "\r\n" && strings.Contains(strings.ReplaceAll(r.Text, "\r\n", ""), "\n") {
					t.Fatal("CRLF に LF を混ぜた")
				}
			})
		}
	}
}

func TestMergeHTMLCellsInContainers(t *testing.T) {
	for _, c := range []struct {
		name, source, want string
		rect               tableedit.Rect
	}{
		{"td", "<table data-x='保持'>\n<tr class='行'><td id='残す'>左<b>上</b></td><td>捨てる</td><td>外</td></tr>\n<tr><td>捨てる2</td><td>捨てる3</td><td>外2</td></tr>\n</table>\n",
			"<table data-x='保持'>\n<tr class='行'><td rowspan=\"2\" colspan=\"2\" id='残す'>左<b>上</b></td><td>外</td></tr>\n<tr><td>外2</td></tr>\n</table>\n", tableedit.Rect{Rows: 2, Cols: 2}},
		{"th と td", "<table><tr><th align='center'>左上</th><td>消す</td><th>外</th></tr></table>\n",
			"<table><tr><th colspan=\"2\" align='center'>左上</th><th>外</th></tr></table>\n", tableedit.Rect{Rows: 1, Cols: 2}},
		{"td と th", "<table><tr><td>左上</td><th>消す</th></tr></table>\n",
			"<table><tr><td colspan=\"2\">左上</td></tr></table>\n", tableedit.Rect{Rows: 1, Cols: 2}},
		{"結合を飲み込む", "<table>\n<tr><td rowspan='02'>左上</td><td colspan='2'>消す</td></tr>\n<tr><td>消す2</td><td>消す3</td></tr>\n</table>\n",
			"<table>\n<tr><td colspan=\"3\" rowspan='02'>左上</td></tr>\n<tr></tr>\n</table>\n", tableedit.Rect{Rows: 2, Cols: 3}},
		{"空の tr", "<table>\n<tr><td>左上</td><td>消す</td></tr>\n<tr class='空でも保持'><td>消す2</td><td>消す3</td></tr>\n</table>\n",
			"<table>\n<tr><td rowspan=\"2\" colspan=\"2\">左上</td></tr>\n<tr class='空でも保持'></tr>\n</table>\n", tableedit.Rect{Rows: 2, Cols: 2}},
		{"セルごとの行", "<table>\n<tr>\n  <td title='残す'>左上</td>\n  <td>消す</td>\n  <td>外</td>\n</tr>\n</table>\n",
			"<table>\n<tr>\n  <td colspan=\"2\" title='残す'>左上</td>\n  <td>外</td>\n</tr>\n</table>\n", tableedit.Rect{Rows: 1, Cols: 2}},
		{"中央の範囲", "<table>\n<tr><td>前1</td><td>前2</td><td>前3</td></tr>\n<tr><td>左</td><td colspan='01' rowspan='01' title='保持'>左上</td><td>消す</td></tr>\n</table>\n",
			"<table>\n<tr><td>前1</td><td>前2</td><td>前3</td></tr>\n<tr><td>左</td><td colspan='02' title='保持'>左上</td></tr>\n</table>\n", tableedit.Rect{Row: 1, Col: 1, Rows: 1, Cols: 2}},
		{"左上の定義を保持", "<table><tr><td>\n\n[ref]: /example\n\n[左上][ref]\n\n</td><td>消す</td></tr></table>\n",
			"<table><tr><td colspan=\"2\">\n\n[ref]: /example\n\n[左上][ref]\n\n</td></tr></table>\n", tableedit.Rect{Rows: 1, Cols: 2}},
	} {
		t.Run(c.name, func(t *testing.T) {
			mergeContainers(t, c.source, c.want, func(d *tableedit.Document) (*tableedit.Result, error) {
				return d.MergeCells(target(d.Table(1)), c.rect)
			})
		})
	}
}

func TestUnmergeHTMLCellsInContainers(t *testing.T) {
	for _, c := range []struct{ name, tag, empty string }{
		{"align", "<td class='左上' align='right' rowspan='2' colspan=2>", `<td align="right"></td>`},
		{"style", `<td rowspan="2" style='color:red; text-align: center; padding:1px' colspan="2">`, `<td style="text-align: center"></td>`},
		{"th", `<th rowspan="2" colspan="2" align="left">`, `<th align="left"></th>`},
		{"揃えなし", `<td rowspan="2" colspan="2">`, `<td></td>`},
	} {
		t.Run(c.name, func(t *testing.T) {
			end := "</td>"
			if strings.HasPrefix(c.tag, "<th") {
				end = "</th>"
			}
			tag := strings.NewReplacer(" rowspan='2'", "", ` rowspan="2"`, "", ` colspan="2"`, "", " colspan=2", "").Replace(c.tag)
			source := "<table>\n<tr>" + c.tag + "左上" + end + "<td>外</td></tr>\n<tr class='保持'><td>外2</td></tr>\n</table>\n"
			want := "<table>\n<tr>" + tag + "左上" + end + c.empty + "<td>外</td></tr>\n<tr class='保持'>" + c.empty + c.empty + "<td>外2</td></tr>\n</table>\n"
			mergeContainers(t, source, want, func(d *tableedit.Document) (*tableedit.Result, error) {
				return d.UnmergeCells(target(d.Table(1)), 0, 0)
			})
		})
	}
	for _, source := range []string{
		"<table>\n<tr><td rowspan='2' colspan='2'>残す</td></tr>\n<tr></tr>\n</table>\n",
		"<table>\n<tr>\n  <td colspan='2'>残す</td>\n  <td>外</td>\n</tr>\n</table>\n",
		"<table><tr><td rowspan='2'>残す</td><td>外</td></tr><tr><td>外2</td></tr></table>\n",
	} {
		mergeContainers(t, source, "", func(d *tableedit.Document) (*tableedit.Result, error) {
			r, err := d.UnmergeCells(target(d.Table(1)), 0, 0)
			if err == nil && (r.Document.Table(1).Rows != d.Table(1).Rows || r.Document.Table(1).Cols != d.Table(1).Cols || len(r.Document.Table(1).Holes) != 0) {
				t.Fatal("解除で行・列か穴の数が変わった")
			}
			return r, err
		})
	}
	mergeContainers(t, "<table>\n<tr><td>前1</td><td>前2</td><td>前3</td></tr>\n<tr><td>左</td><td colspan='2' align='right'>残す</td></tr>\n</table>\n",
		"<table>\n<tr><td>前1</td><td>前2</td><td>前3</td></tr>\n<tr><td>左</td><td align='right'>残す</td><td align=\"right\"></td></tr>\n</table>\n",
		func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.UnmergeCells(target(d.Table(1)), 1, 1)
		})
}

func TestMergePipeAsHTMLInContainers(t *testing.T) {
	source := "| 見出し | 次 | 外 |\n| :--- | ---: | --- |\n| **残す** | 消す | 外1 |\n| 消す2 | 消す3 | 外2 |\n"
	for _, rect := range []tableedit.Rect{{Rows: 1, Cols: 2}, {Row: 1, Rows: 2, Cols: 2}} {
		mergeContainers(t, source, "", func(d *tableedit.Document) (*tableedit.Result, error) {
			r, err := d.MergeCellsAsHTML(target(d.Table(1)), rect)
			if err == nil {
				tab := r.Document.Table(1)
				if tab.Kind != "html" || tab.Rows != 3 || tab.Cols != 3 || len(tab.Cells) != 9-(rect.Rows*rect.Cols-1) {
					t.Fatalf("変換と結合のモデルが違う: %+v", tab)
				}
				for _, cell := range tab.Cells {
					if cell.Row == rect.Row && cell.Col == rect.Col && (cell.Rowspan != rect.Rows || cell.Colspan != rect.Cols || cell.Align != "left") {
						t.Fatal("左上の結合と揃えが違う", cell)
					}
				}
			}
			return r, err
		})
	}
}

func TestMergeAndUnmergeRefusals(t *testing.T) {
	for _, c := range []struct {
		name, source, reason string
		rect                 tableedit.Rect
	}{
		{"区画", "<table><thead><tr><th>A</th><th>B</th></tr></thead><tbody><tr><td>C</td><td>D</td></tr></tbody></table>\n", "区画", tableedit.Rect{Rows: 2, Cols: 2}},
		{"別の tbody", "<table><tbody><tr><td>A</td><td>B</td></tr></tbody><tbody><tr><td>C</td><td>D</td></tr></tbody></table>\n", "区画", tableedit.Rect{Rows: 2, Cols: 2}},
		{"tfoot", "<table><tbody><tr><td>A</td><td>B</td></tr></tbody><tfoot><tr><td>C</td><td>D</td></tr></tfoot></table>\n", "区画", tableedit.Rect{Rows: 2, Cols: 2}},
		{"見出しと本文", "| A | B |\n| --- | --- |\n| C | D |\n", "見出し", tableedit.Rect{Rows: 2, Cols: 2}},
		{"切れる結合", "<table><tr><td>A</td><td rowspan='2'>B</td></tr><tr><td>C</td></tr></table>\n", "結合セルが切れる", tableedit.Rect{Rows: 1, Cols: 2}},
		{"一セル", "<table><tr><td>A</td><td>B</td></tr></table>\n", "1 セル", tableedit.Rect{Rows: 1, Cols: 1}},
		{"穴", "<table><tr><td>A</td><td>B</td></tr><tr><td>C</td></tr></table>\n", "セルの無い", tableedit.Rect{Rows: 2, Cols: 2}},
		{"参照の定義", "<table><tr><td>A</td><td>\n\n[ref]: /example\n\nB\n\n</td></tr></table>\n", "参照の定義", tableedit.Rect{Rows: 1, Cols: 2}},
		{"入れ子の HTML", "<table><tr><td>A</td><td><table><tr><td>B</td></tr></table></td></tr></table>\n", "入れ子", tableedit.Rect{Rows: 1, Cols: 2}},
		{"入れ子のパイプ", "<table><tr><td>A</td><td>\n\n| B |\n| --- |\n| C |\n\n</td></tr></table>\n", "入れ子", tableedit.Rect{Rows: 1, Cols: 2}},
		{"省略タグ", "<table><tr><td>A<td>B</tr></table>\n", "省略", tableedit.Rect{Rows: 1, Cols: 2}},
		{"不正な結合", "<table><tr><td colspan='0'>A</td><td>B</td></tr></table>\n", "結合の値", tableedit.Rect{Rows: 1, Cols: 2}},
		{"区画で切れる結合", "<table><tr><td rowspan='3'>A</td><td>B</td></tr><tr><td>C</td></tr></table>\n", "結合の値", tableedit.Rect{Rows: 2, Cols: 2}},
		{"タスクの印だけ", "| A | B |\n| --- | --- |\n| [ ] | C |\n", "タスクの印だけ", tableedit.Rect{Row: 1, Rows: 1, Cols: 2}},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := tableedit.Parse(c.source)
			var r *tableedit.Result
			var err error
			if d.Table(1).Kind == "pipe" {
				r, err = d.MergeCellsAsHTML(target(d.Table(1)), c.rect)
			} else {
				r, err = d.MergeCells(target(d.Table(1)), c.rect)
			}
			var failure *tableedit.Error
			if r != nil || !errors.As(err, &failure) || !strings.Contains(err.Error(), c.reason) || d.Text() != c.source {
				t.Fatalf("断る理由が違う: %+v %v", r, err)
			}
		})
	}
	d := tableedit.Parse("<table><tr><td colspan='2'>A</td><td>B</td></tr></table>\n")
	for _, pos := range []tableedit.Position{{Row: -1}, {Col: 1}, {Col: 2}, {Row: 3}} {
		if r, err := d.UnmergeCells(target(d.Table(1)), pos.Row, pos.Col); r != nil || err == nil {
			t.Fatal("解除できない位置を受けた", pos, err)
		}
	}
	for _, rect := range []tableedit.Rect{{Rows: 0, Cols: 2}, {Row: -1, Rows: 1, Cols: 2}, {Rows: 1, Cols: 4}, {Row: int(^uint(0) >> 1), Rows: 2, Cols: 2}, {Rows: 1, Cols: 2}} {
		if r, err := d.MergeCells(target(d.Table(1)), rect); r != nil || err == nil {
			t.Fatal("範囲外か一個の結合セルを受けた", rect, err)
		}
	}
}

func TestCellMergeKindsAndTargets(t *testing.T) {
	html := tableedit.Parse("<table><tr><td colspan='2'>A</td><td>B</td></tr></table>\n")
	pipe := tableedit.Parse("| A | B |\n| --- | --- |\n| C | D |\n")
	rect := tableedit.Rect{Rows: 1, Cols: 2}
	for _, c := range []struct {
		name string
		edit func() (*tableedit.Result, error)
		code string
	}{
		{"パイプを直接結合", func() (*tableedit.Result, error) { return pipe.MergeCells(target(pipe.Table(1)), rect) }, "unsupported"},
		{"HTML を変換して結合", func() (*tableedit.Result, error) { return html.MergeCellsAsHTML(target(html.Table(1)), rect) }, "unsupported"},
		{"パイプを解除", func() (*tableedit.Result, error) { return pipe.UnmergeCells(target(pipe.Table(1)), 0, 0) }, "unsupported"},
		{"古い結合", func() (*tableedit.Result, error) {
			return html.MergeCells(tableedit.Target{Number: 1, SourceHash: "古い"}, rect)
		}, "stale"},
		{"古い変換して結合", func() (*tableedit.Result, error) {
			return pipe.MergeCellsAsHTML(tableedit.Target{Number: 1, SourceHash: "古い"}, rect)
		}, "stale"},
		{"古い解除", func() (*tableedit.Result, error) {
			return html.UnmergeCells(tableedit.Target{Number: 1, SourceHash: "古い"}, 0, 0)
		}, "stale"},
		{"存在しない表", func() (*tableedit.Result, error) { return html.MergeCells(tableedit.Target{Number: 2}, rect) }, "range"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, err := c.edit()
			var failure *tableedit.Error
			if r != nil || !errors.As(err, &failure) || failure.Code != c.code {
				t.Fatalf("結果かエラーが違う: %+v %v", r, err)
			}
		})
	}
}
