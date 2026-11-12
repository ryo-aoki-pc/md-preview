package table

import (
	"testing"

	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/htmltok"
)

func TestCellLocks(t *testing.T) {
	const missingCell = "</td>・</th> が省略されているので、行・列は変えられない"
	const invalidSpan = "結合の値が 0・数字以外・区画の終わりで切られた値なので、行・列は変えられない"
	const missingRow = "<tr>・</tr> が省略されているので、行・列は変えられない"
	const colgroup = "<colgroup>・<col> があるので、列は変えられない"
	const holes = "セルの無い位置があるので、列は変えられない"
	const sections = "<thead> が <tbody> の後ろにあるので、行は変えられない"
	for _, test := range []struct{ name, source, cell, rows, cols string }{
		{"通常のセル", "<table><tr><td>a</td></tr></table>", "", "", ""},
		{"td の終了タグの省略", "<table><tr><td>a</tr></table>", missingCell, missingCell, missingCell},
		{"th の終了タグの省略", "<table><tr><th>a</tr></table>", missingCell, missingCell, missingCell},
		{"結合のゼロ", "<table><tr><td colspan=0>a</td></tr></table>", invalidSpan, invalidSpan, invalidSpan},
		{"結合が数字以外", "<table><tr><td rowspan=bad>a</td></tr></table>", invalidSpan, invalidSpan, invalidSpan},
		{"区画で切られた結合", "<table><tbody><tr><td rowspan=2>a</td></tr></tbody></table>", invalidSpan, invalidSpan, invalidSpan},
		{"結合の属性の重複", "<table><tr><td colspan=1 colspan=1>a</td></tr></table>", invalidSpan, invalidSpan, invalidSpan},
		{"同じセルの結合の理由を優先", "<table><tr><td colspan=0>a</tr></table>", invalidSpan, invalidSpan, invalidSpan},
		{"後ろのセルの省略を優先", "<table><tr><td colspan=0>a<td>b</tr></table>", missingCell, missingCell, missingCell},
		{"後ろのセルの結合を優先", "<table><tr><td>a<td colspan=0>b</tr></table>", invalidSpan, invalidSpan, invalidSpan},
		{"行の省略だけ", "<table><tr><td>a</td></table>", "", missingRow, missingRow},
		{"列の区画だけ", "<table><colgroup><col></colgroup><tr><td>a</td></tr></table>", "", "", colgroup},
		{"セルの無い位置だけ", "<table><tr><td>a</td><td>b</td></tr><tr><td>c</td></tr></table>", "", "", holes},
		{"区画の順だけ", "<table><tbody><tr><td>a</td></tr></tbody><thead><tr><th>b</th></tr></thead></table>", "", sections, ""},
		{"行と列の理由よりセルを優先", "<table><colgroup><col></colgroup><tr><td colspan=0>a</td></table>", invalidSpan, invalidSpan, invalidSpan},
	} {
		t.Run(test.name, func(t *testing.T) {
			regions := FindTables(NewMarkdown("test.md", test.source, false))
			if len(regions) != 1 || regions[0].HTML == nil {
				t.Fatalf("見本を HTML の表として読めない: %q", test.source)
			}
			r := regions[0]
			if got := CellLocks(r); got != test.cell {
				t.Errorf("セルを変えられない理由が違う: %q、期待値 %q", got, test.cell)
			}
			if rows, cols := ShapeLocks(r); rows != test.rows || cols != test.cols {
				t.Errorf("行・列を変えられない理由が違う: %q・%q、期待値 %q・%q", rows, cols, test.rows, test.cols)
			}
		})
	}
	for _, r := range []*Region{{Kind: "pipe"}, {Kind: "html"}} {
		if got := CellLocks(r); got != "" {
			t.Errorf("HTML のセルが無いのに断られた: %q", got)
		}
		if rows, cols := ShapeLocks(r); rows != "" || cols != "" {
			t.Errorf("HTML のセルが無いのに行・列を断られた: %q・%q", rows, cols)
		}
	}
}

func TestAttributeRanges(t *testing.T) {
	type attribute struct{ name, raw, value string }
	for _, test := range []struct {
		name, tag string
		want      []attribute
	}{
		{"二重引用符", `<td align="left" style="text-align: right">`, []attribute{{"align", `align="left"`, "left"}, {"style", `style="text-align: right"`, "text-align: right"}}},
		{"一重引用符", `<td align='center'>`, []attribute{{"align", "align='center'", "center"}}},
		{"引用符なし", `<td align=right colspan=02>`, []attribute{{"align", "align=right", "right"}, {"colspan", "colspan=02", "02"}}},
		{"大文字", `<TD ALIGN="LEFT">`, []attribute{{"align", `ALIGN="LEFT"`, "LEFT"}}},
		{"等号の前後の空白", "<td\tALIGN \t=\n ' center '\nstyle = \"text-align: left\">", []attribute{{"align", "ALIGN \t=\n ' center '", " center "}, {"style", `style = "text-align: left"`, "text-align: left"}}},
		{"同じ原文の属性の重複", `<td align=left align=left>`, []attribute{{"align", "align=left", "left"}, {"align", "align=left", "left"}}},
		{"大文字と値が違う属性の重複", `<td ALIGN='left' align="right">`, []attribute{{"align", "ALIGN='left'", "left"}, {"align", `align="right"`, "right"}}},
		{"空の値と値なし", `<td hidden align="">`, []attribute{{"hidden", "hidden", ""}, {"align", `align=""`, ""}}},
		{"値の多バイト文字と等号", `<td title="日本語=値" align=left>`, []attribute{{"title", `title="日本語=値"`, "日本語=値"}, {"align", "align=left", "left"}}},
		{"属性なし", `<td>`, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			prefix := "前\n"
			source := prefix + test.tag
			tokens := htmltok.Tokenize(source, len(prefix), len(source))
			if len(tokens) != 1 || tokens[0].Kind != "start" {
				t.Fatalf("見本の開始タグを読めない: %#v", tokens)
			}
			token := tokens[0]
			ranges := attributeRanges(token)
			if len(ranges) != len(test.want) {
				t.Fatalf("属性の数が違う: %d、期待値 %d", len(ranges), len(test.want))
			}
			lastEnd := 1 + len(token.Name)
			for i, r := range ranges {
				want := test.want[i]
				if r.start < lastEnd || r.end < r.start || r.end > len(token.Text) {
					t.Fatalf("属性の範囲が重なるか、開始タグを外れる: %+v", r)
				}
				if r.name != want.name || token.Text[r.start:r.end] != want.raw || source[token.Start+r.start:token.Start+r.end] != want.raw {
					t.Errorf("属性の名前か原文の範囲が違う: %+v、期待値 %+v", r, want)
				}
				if token.Attributes[i].Value == nil {
					if r.valueStart != -1 || r.valueEnd != -1 {
						t.Errorf("値なしの属性に値の範囲がある: %+v", r)
					}
				} else {
					if r.valueStart < r.start || r.valueEnd < r.valueStart || r.valueEnd > r.end {
						t.Fatalf("属性の値の範囲が原文を外れる: %+v", r)
					}
					if got := token.Text[r.valueStart:r.valueEnd]; got != want.value {
						t.Errorf("属性の値の原文の範囲が違う: %q、期待値 %q", got, want.value)
					}
				}
				lastEnd = r.end
			}
		})
	}
}

// spanEdit は属性の範囲を使っても、周囲の原文とゼロ埋めを保つ。
func TestSpanEditAttributeRanges(t *testing.T) {
	for _, test := range []struct {
		name, tag, attribute string
		value                int
		want                 string
	}{
		{"数字だけを変更", `<TD class='x' ROWSPAN = ' 002 ' align=left>`, "rowspan", 3, `<TD class='x' ROWSPAN = ' 003 ' align=left>`},
		{"引用符なし", `<td colspan=02>`, "colspan", 3, `<td colspan=03>`},
		{"属性の削除", "<td class=x\n\tCOLSPAN = \"02\" align=right>", "colspan", 1, `<td class=x align=right>`},
		{"重複の先頭だけを変更", `<td colspan=2 colspan=2>`, "colspan", 3, `<td colspan=3 colspan=2>`},
		{"属性の追加", `<td align=left>`, "rowspan", 2, `<td rowspan="2" align=left>`},
	} {
		t.Run(test.name, func(t *testing.T) {
			prefix := "前\n"
			source := prefix + test.tag
			tokens := htmltok.Tokenize(source, len(prefix), len(source))
			if len(tokens) != 1 || tokens[0].Kind != "start" {
				t.Fatalf("見本の開始タグを読めない: %#v", tokens)
			}
			edit := spanEdit(&HTMLCell{StartToken: tokens[0]}, test.attribute, test.value)
			if got := source[:edit.start] + edit.replacement + source[edit.end:]; got != prefix+test.want {
				t.Errorf("結合の属性の編集で原文が変わった: %q、期待値 %q", got, prefix+test.want)
			}
		})
	}
}
