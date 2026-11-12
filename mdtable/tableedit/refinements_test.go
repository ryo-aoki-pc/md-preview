package tableedit_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-table-excel/tableedit"
)

func tableInContainer(body, container, ending string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		switch container {
		case "引用":
			lines[i] = "> " + line
		case "リスト":
			prefix := "  "
			if i == 0 {
				prefix = "- "
			}
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, ending)
}

const cellLinesHTML = `<table>
  <tr>
    <th rowspan="2">区分</th>
    <td>a</td>
    <td>b</td>
  </tr>
  <tr>
    <td>c</td>
    <td>d</td>
  </tr>
</table>`

func TestSeparateCellLines(t *testing.T) {
	for _, container := range []string{"本文", "引用", "リスト"} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(fmt.Sprintf("%s/%q", container, ending), func(t *testing.T) {
				text := tableInContainer(cellLinesHTML, container, ending)
				d := tableedit.Parse(text)
				for _, col := range []int{0, 2} {
					result, err := d.DeleteColumns(tableedit.Target{Number: 1}, col, 1)
					next := mustChange(t, result, err)
					want := cellLinesHTML
					if col == 0 {
						want = strings.Replace(want, "    <th rowspan=\"2\">区分</th>\n", "", 1)
					} else {
						want = strings.Replace(want, "    <td>b</td>\n", "", 1)
						want = strings.Replace(want, "    <td>d</td>\n", "", 1)
					}
					if next.Text() != tableInContainer(want, container, ending) {
						t.Fatalf("削除したセルの行が残った:\n%s", next.Text())
					}
				}
				result, err := d.InsertColumns(tableedit.Target{Number: 1}, 0, tableedit.Before, 1)
				next := mustChange(t, result, err)
				want := strings.Replace(cellLinesHTML, "    <th rowspan=\"2\">区分</th>", "    <td></td>\n    <th rowspan=\"2\">区分</th>", 1)
				want = strings.Replace(want, "    <td>c</td>", "    <td></td>\n    <td>c</td>", 1)
				if next.Text() != tableInContainer(want, container, ending) {
					t.Fatalf("追加したセルが独立した行にならない:\n%s", next.Text())
				}
				result, err = d.DeleteRows(tableedit.Target{Number: 1}, 0, 1)
				next = mustChange(t, result, err)
				want = "<table>\n  <tr>\n    <th>区分</th>\n    <td>c</td>\n    <td>d</td>\n  </tr>\n</table>"
				if next.Text() != tableInContainer(want, container, ending) {
					t.Fatalf("移動した結合セルが独立した行にならない:\n%s", next.Text())
				}
				for _, side := range []tableedit.Side{tableedit.Before, tableedit.After} {
					for _, count := range []int{1, 2} {
						for col := 0; col < 3; col++ {
							result, err = d.InsertColumns(tableedit.Target{Number: 1}, col, side, count)
							next = mustChange(t, result, err)
							at := col
							if side == tableedit.After {
								at++
							}
							result, err = next.DeleteColumns(tableedit.Target{Number: 1}, at, count)
							back := mustChange(t, result, err)
							if back.Text() != text {
								t.Fatalf("セルが行を占める表の列の往復で変わった:\n%s", back.Text())
							}
						}
						for row := 0; row < 2; row++ {
							result, err = d.InsertRows(tableedit.Target{Number: 1}, row, side, count)
							next = mustChange(t, result, err)
							at := row
							if side == tableedit.After {
								at++
							}
							result, err = next.DeleteRows(tableedit.Target{Number: 1}, at, count)
							back := mustChange(t, result, err)
							if back.Text() != text {
								t.Fatalf("セルが行を占める表の行の往復で変わった:\n%s", back.Text())
							}
						}
					}
				}
			})
		}
	}
}

func TestMultiLineCellRemovalAndMove(t *testing.T) {
	for _, container := range []string{"本文", "引用", "リスト"} {
		body := strings.Replace(cellLinesHTML, "<th rowspan=\"2\">区分</th>", "<th rowspan=\"2\">\n      区分\n    </th>", 1)
		d := tableedit.Parse(tableInContainer(body, container, "\r\n"))
		result, err := d.DeleteColumns(tableedit.Target{Number: 1}, 0, 1)
		next := mustChange(t, result, err)
		want := strings.Replace(body, "    <th rowspan=\"2\">\n      区分\n    </th>\n", "", 1)
		if next.Text() != tableInContainer(want, container, "\r\n") {
			t.Fatalf("複数行のセルの一部が残った:\n%s", next.Text())
		}
		result, err = d.DeleteRows(tableedit.Target{Number: 1}, 0, 1)
		next = mustChange(t, result, err)
		want = "<table>\n  <tr>\n    <th>\n      区分\n    </th>\n    <td>c</td>\n    <td>d</td>\n  </tr>\n</table>"
		if next.Text() != tableInContainer(want, container, "\r\n") {
			t.Fatalf("複数行のセルの移動で原文が変わった:\n%s", next.Text())
		}
		// 右のセルが無いときは、移したセルを次の行の最後のセルの後ろへ入れる。
		body = strings.Replace(cellLinesHTML, "    <th rowspan=\"2\">区分</th>\n", "", 1)
		body = strings.Replace(body, "    <td>b</td>", "    <td>b</td>\n    <th rowspan=\"2\">区分</th>", 1)
		d = tableedit.Parse(tableInContainer(body, container, "\r\n"))
		result, err = d.DeleteRows(tableedit.Target{Number: 1}, 0, 1)
		next = mustChange(t, result, err)
		want = "<table>\n  <tr>\n    <td>c</td>\n    <td>d</td>\n    <th>区分</th>\n  </tr>\n</table>"
		if next.Text() != tableInContainer(want, container, "\r\n") {
			t.Fatalf("末尾に移すセルが独立した行にならない:\n%s", next.Text())
		}
	}
}

func TestWhitespaceOnlyPipeCell(t *testing.T) {
	for _, container := range []string{"本文", "引用", "リスト"} {
		for _, test := range []struct{ body, want string }{
			{"| a | b | c |\n| --- | --- | --- |\n| 1 |   | 3 |", "| a | b | c |\n| --- | --- | --- |\n| 1 | 新 | 3 |"},
			{"a|b|c\n---|---|---\n1|   |3", "a|b|c\n---|---|---\n1|新|3"},
			{"| a | b | c |\n| --- | --- | --- |\n| 1 |\t \t| 3 |", "| a | b | c |\n| --- | --- | --- |\n| 1 | 新 | 3 |"},
		} {
			d := tableedit.Parse(tableInContainer(test.body, container, "\r\n"))
			result, err := d.SetCells(tableedit.Target{Number: 1}, []tableedit.CellEdit{{Row: 1, Col: 1, Text: "新"}})
			next := mustChange(t, result, err)
			if next.Text() != tableInContainer(test.want, container, "\r\n") {
				t.Fatalf("空白のセルへの入力の空白が違う: %q", next.Text())
			}
		}
	}
}

func TestSourceAutolinksAndErrorCodes(t *testing.T) {
	for _, container := range []string{"本文", "引用", "リスト"} {
		d := tableedit.Parse(tableInContainer("<table>\n  <tr>\n    <td>a</td>\n    <td data-x='1'>b</td>\n  </tr>\n</table>", container, "\r\n"))
		for _, source := range []string{"<https://example.com>", "<mailto:a@example.com>", "<a@example.com>", "前 <https://example.com> / <a@example.com> 後", "\n\n<https://example.com>\n\n"} {
			result, err := d.SetCellSource(tableedit.Target{Number: 1}, 0, 0, source)
			next := mustChange(t, result, err)
			if next.Table(1).Cells[0].Source != source || next.Table(1).Cells[1].Source != "b" {
				t.Fatal("自動リンクの差し替えでセルの範囲が変わった")
			}
		}
		for _, source := range []string{"x<y", "<!-- 閉じない", "<td", "<script>x</script>", "<style>x</style>", "<textarea>x</textarea>", "<https://example.com> x<y"} {
			result, err := d.SetCellSource(tableedit.Target{Number: 1}, 0, 0, source)
			code(t, err, "invalid")
			if result != nil {
				t.Fatal("字句の検査が失敗したのに本文が返った")
			}
		}
		_, err := d.SetCellSource(tableedit.Target{Number: 1}, 0, 0, "a</td>")
		code(t, err, "verify")
		_, err = d.SetCellSource(tableedit.Target{Number: 1}, 0, 99, "a")
		code(t, err, "range")
		_, err = d.SetCellSource(tableedit.Target{Number: 1, SourceHash: "古い"}, 0, 0, "a")
		code(t, err, "stale")
	}
	merged := tableedit.Parse("<table><tr><td rowspan='2'>a</td><td>b</td></tr><tr><td>c</td></tr></table>")
	_, err := merged.SetCellSource(tableedit.Target{Number: 1}, 1, 0, "a")
	code(t, err, "merged")
	pipe := tableedit.Parse("| a |\n| --- |")
	_, err = pipe.SetCellSource(tableedit.Target{Number: 1}, 0, 0, "a")
	code(t, err, "unsupported")
	excluded := tableedit.Parse("<table><tr><td>a</td></tr>")
	_, err = excluded.SetCellSource(tableedit.Target{Number: 1}, 0, 0, "a")
	code(t, err, "excluded")
}

func TestWarningsFollowTheTable(t *testing.T) {
	d := tableedit.Parse("| a | b |\n| --- | --- |\n| c | d | 余分 |\n| e | f |\n")
	result, err := d.DeleteRows(tableedit.Target{Number: 1}, 1, 1)
	mustChange(t, result, err)
	if len(result.Warnings) != 0 || len(result.Document.Table(1).Warnings) != 0 {
		t.Fatalf("消した行の警告が残った: %v", result.Warnings)
	}
	result, err = d.InsertRows(tableedit.Target{Number: 1}, 1, tableedit.Before, 2)
	mustChange(t, result, err)
	if len(result.Warnings) != 1 || !strings.HasPrefix(result.Warnings[0], "5 行目: ") {
		t.Fatalf("行を足した後の警告の行番号が古い: %v", result.Warnings)
	}
}

// 行末に縦棒の無い表で、行に無いセルを足すときは、余分な縦棒と空白を足さない。
func TestPatchPipeMissingCells(t *testing.T) {
	for _, c := range []struct{ text, row string }{
		{"a | b\n---|---\n1 | \t\n", "1 | 新"},
		{"a | b\n---|---\n1 | \n", "1 | 新"},
		{"a | b\n---|---\n1 |\n", "1 | 新"},
		{"a | b\n---|---\n1\n", "1 | 新"},
		{"a|b\n---|---\n1\n", "1|新"},
		{"| a | b |\n| --- | --- |\n| 1 |\n", "| 1 | 新 |"},
		{"| a | b | c |\n| --- | --- | --- |\n| 1 |\n", "| 1 |  | 新 |"},
	} {
		d := tableedit.Parse(c.text)
		col := d.Table(1).Cols - 1
		result, err := d.SetCells(tableedit.Target{Number: 1}, []tableedit.CellEdit{{Row: 1, Col: col, Text: "新"}})
		mustChange(t, result, err)
		lines := strings.Split(result.Text, "\n")
		if lines[2] != c.row {
			t.Errorf("%q: 行が %q、期待は %q", c.text, lines[2], c.row)
		}
		if got := result.Document.Table(1).Cells; got[len(got)-1].Text != "新" {
			t.Errorf("%q: 読み直したセルが %q", c.text, got[len(got)-1].Text)
		}
	}
}

func TestWarningsMatchOperation(t *testing.T) {
	for _, container := range []string{"本文", "引用", "リスト"} {
		d := tableedit.Parse(tableInContainer("| a | b |\n| --- | --- |\n| c | d | 余分 |\n| e | f |", container, "\r\n"))
		base := d.Table(1).Warnings[0]
		if !strings.HasSuffix(base, "見出しより多いセルがある（GitLab では表示されない）") {
			t.Fatalf("表の警告に誤った削除の案内がある: %s", base)
		}
		operations := []struct {
			name    string
			columns bool
			run     func() (*tableedit.Result, error)
		}{
			{"セル", false, func() (*tableedit.Result, error) {
				return d.SetCells(tableedit.Target{Number: 1}, []tableedit.CellEdit{{Row: 1, Col: 0, Text: "新"}})
			}},
			{"貼り付け", false, func() (*tableedit.Result, error) {
				return d.Paste(tableedit.Target{Number: 1}, 1, 0, [][]string{{"新"}})
			}},
			{"行の追加", false, func() (*tableedit.Result, error) {
				return d.InsertRows(tableedit.Target{Number: 1}, 1, tableedit.After, 2)
			}},
			{"行の前への追加", false, func() (*tableedit.Result, error) {
				return d.InsertRows(tableedit.Target{Number: 1}, 1, tableedit.Before, 2)
			}},
			{"行の削除", false, func() (*tableedit.Result, error) { return d.DeleteRows(tableedit.Target{Number: 1}, 2, 1) }},
			{"列の追加", true, func() (*tableedit.Result, error) {
				return d.InsertColumns(tableedit.Target{Number: 1}, 1, tableedit.After, 2)
			}},
			{"列の削除", true, func() (*tableedit.Result, error) { return d.DeleteColumns(tableedit.Target{Number: 1}, 1, 1) }},
		}
		for _, operation := range operations {
			t.Run(container+"/"+operation.name, func(t *testing.T) {
				result, err := operation.run()
				mustChange(t, result, err)
				after := result.Document.Table(1).Warnings
				if operation.columns {
					// 列の操作は全行を組み直す。消したことを 1 回だけ知らせ、読み直した表に警告は残らない。
					want := strings.Replace(base, "見出しより多いセルがある（GitLab では表示されない）", "見出しより多いセルを消した（GitLab では表示されていなかった）", 1)
					if len(result.Warnings) != 1 || result.Warnings[0] != want || len(after) != 0 || strings.Contains(result.Text, "余分") {
						t.Fatalf("列の操作の知らせが合わない: %v / %v", result.Warnings, after)
					}
					return
				}
				// セルと行の操作では余分なセルが残る。警告は読み直した表のもの（行番号も今の位置）。
				if len(result.Warnings) != 1 || len(after) != 1 || result.Warnings[0] != after[0] ||
					!strings.HasSuffix(after[0], "見出しより多いセルがある（GitLab では表示されない）") {
					t.Fatalf("操作に合わない警告、または重複: %v / %v", result.Warnings, after)
				}
				if !strings.Contains(result.Text, "余分") {
					t.Fatal("行・セルの操作で余分なセルが消えた")
				}
			})
		}
	}
}

func TestTagPositionsWithCommentAndLineBreaks(t *testing.T) {
	for _, container := range []string{"本文", "引用", "リスト"} {
		for _, opening := range []string{"<table class='t'>", "<table\n  class='t'\n  data-x='>'>"} {
			body := "<!-- 表の前 -->\n" + opening + "\n<tr><td>a</td></tr>\n</table>"
			text := tableInContainer(body, container, "\r\n")
			tab := tableedit.Parse(text).Table(1)
			if tab == nil || tab.Excluded != "" || tab.Tag == nil {
				t.Fatalf("表を読めない: %+v", tab)
			}
			start := strings.Index(text, "<table")
			endMarker := "class='t'>"
			if strings.Contains(opening, "data-x") {
				endMarker = "data-x='>'>"
			}
			end := strings.Index(text, endMarker) + len(endMarker)
			if tab.Tag.Start != start || tab.Tag.End != end || text[tab.Tag.Start:tab.Tag.NameEnd] != "<table" || text[tab.Tag.Start:tab.Tag.End] != text[start:end] {
				t.Fatalf("コメントまたは複数行のタグの位置が違う: %+v", tab.Tag)
			}
		}
	}
}
