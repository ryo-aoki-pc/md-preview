package tableedit_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/ryo-aoki-pc/md-table-excel/tableedit"
)

func mustChange(t *testing.T, result *tableedit.Result, err error) *tableedit.Document {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Document.Text() != result.Text {
		t.Fatal("変更結果と読み直した文書が一致しない")
	}
	return result.Document
}
func target(t *tableedit.Table) tableedit.Target {
	return tableedit.Target{Number: t.Number, SourceHash: t.SourceHash}
}
func code(t *testing.T, err error, want string) {
	t.Helper()
	var got *tableedit.Error
	if !errors.As(err, &got) || got.Code != want {
		t.Fatalf("誤り: %v、期待する符号: %s", err, want)
	}
	if strings.ContainsAny(got.Message, "\r\n") {
		t.Fatal("誤りの文に改行がある")
	}
}

func TestFixtureNoEdit(t *testing.T) {
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
		text := strings.TrimPrefix(string(data), "\xef\xbb\xbf")
		d := tableedit.Parse(text)
		if d.Text() != text {
			t.Fatalf("%s: 本文が変わった", path)
		}
		for _, tab := range d.Tables() {
			if tab.Excluded != "" {
				if tab.Tag != nil {
					t.Fatal("対象外の表にタグの位置がある")
				}
				continue
			}
			count++
			if _, err := json.Marshal(tab); err != nil {
				t.Fatal(err)
			}
			var edits []tableedit.CellEdit
			for _, c := range tab.Cells {
				edits = append(edits, tableedit.CellEdit{Row: c.Row, Col: c.Col, Text: c.Text})
			}
			result, err := d.SetCells(target(tab), edits)
			if err != nil || result.Changed || result.Text != text {
				t.Fatalf("%s 表 %d: 無編集で変わる: %v", path, tab.Number, err)
			}
			if tab.Kind == "html" {
				if tab.Tag == nil || !strings.EqualFold(text[tab.Tag.Start:tab.Tag.NameEnd], "<table") || text[tab.Tag.End-1] != '>' {
					t.Fatalf("%s 表 %d: タグの位置が違う: %+v", path, tab.Number, tab.Tag)
				}
				for _, c := range tab.Cells {
					result, err = d.SetCellSource(target(tab), c.Row, c.Col, c.Source)
					if err != nil || result.Changed || result.Text != text {
						t.Fatalf("%s 表 %d (%d,%d): 無編集の原文で変わる: %v", path, tab.Number, c.Row, c.Col, err)
					}
				}
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
	t.Logf("%d 個の表で無編集を確認", count)
}

func TestOneCellPreservesSurroundings(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		for _, body := range []string{
			"| a | b |\n| --- | --- |\n| c | d |",
			"<table class='x'><tr><td>a</td><td data-x='1'>b</td></tr><tr><td>c</td><td>d</td></tr></table>",
			"> | a | b |\n> | --- | --- |\n> | c | d |",
			"- | a | b |\n  | --- | --- |\n  | c | d |",
			"> <table>\n> <tr><td>a</td><td>b</td></tr>\n> </table>",
			"- <table>\n  <tr><td>a</td><td data-x='1'>b</td></tr>\n  </table>",
		} {
			text := strings.ReplaceAll("前\n\n"+body+"\n\n後\n\n| 独立 |\n| --- |", "\n", ending)
			d := tableedit.Parse(text)
			tabs := d.Tables()
			tab := tabs[0]
			result, err := d.SetCells(target(tab), []tableedit.CellEdit{{Row: 0, Col: 0, Text: "変更"}})
			next := mustChange(t, result, err)
			oldLines := strings.Split(text, ending)
			newLines := strings.Split(next.Text(), ending)
			before := strings.Join(oldLines[:tab.StartLine], ending)
			after := strings.Join(oldLines[tab.EndLine+1:], ending)
			if !strings.HasPrefix(next.Text(), before) || !strings.HasSuffix(next.Text(), after) {
				t.Fatal("表の外の本文が変わった")
			}
			if len(newLines) != len(oldLines) {
				t.Fatal("単純な編集で行数が変わった")
			}
			if next.Tables()[len(tabs)-1].SourceHash != tabs[len(tabs)-1].SourceHash {
				t.Fatal("ほかの表の指紋が変わった")
			}
			if tab.Kind == "html" {
				for i, c := range tab.Cells {
					if i > 0 && c.Source != next.Tables()[0].Cells[i].Source {
						t.Fatal("ほかのセルの原文が変わった")
					}
				}
			}
			if d.Text() != text {
				t.Fatal("元の文書が変わった")
			}
		}
	}
}

func TestShapeRoundTrip(t *testing.T) {
	texts := []string{
		"| a | b |\n| --- | :---: |\n| c | d |\n",
		"> | a | b |\n> | --- | :---: |\n> | c | d |",
		"- | a | b |\n  | --- | :---: |\n  | c | d |\n",
		"<table><tr><th>a</th><th>b</th></tr><tr><td>c</td><td>d</td></tr></table>",
		"<table class='t'>\n<colgroup><col span='2'></colgroup>\n<thead>\n  <tr><th>a</th><th>b</th></tr>\n</thead>\n<tbody class='b'>\n  <tr><td>c</td><td>d</td></tr>\n</tbody>\n</table>\n",
		"> <table>\n>   <tr><td rowspan='2' class='x'>a</td><td>b</td></tr>\n>   <tr><td>c</td></tr>\n> </table>\n",
		"- <table>\n  <thead><tr><th>a</th><th>b</th></tr></thead>\n  <tbody><tr><td>c</td><td>d</td></tr></tbody>\n  </table>",
		"<table><tr><td colspan='2'>a</td></tr><tr><td>b</td><td>c</td></tr></table>",
		"<table><tr><td class = 'x' ROWSPAN = '02' colspan='02' style='text-align:right'>a</td><td>b</td></tr><tr><td>c</td></tr><tr><td>d</td><td>e</td><td>f</td></tr></table>",
	}
	for i, original := range texts {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(fmt.Sprintf("%d/%q", i, ending), func(t *testing.T) {
				text := strings.ReplaceAll(original, "\n", ending)
				d := tableedit.Parse(text)
				tab := d.Table(1)
				if tab == nil || tab.Excluded != "" {
					t.Fatalf("表を読めない: %+v", tab)
				}
				for _, side := range []tableedit.Side{tableedit.Before, tableedit.After} {
					row := 1
					at := row
					if side == tableedit.After {
						at++
					}
					result, err := d.InsertRows(target(tab), row, side, 2)
					next := mustChange(t, result, err)
					result, err = next.DeleteRows(tableedit.Target{Number: 1}, at, 2)
					back := mustChange(t, result, err)
					if back.Text() != text {
						t.Fatalf("行の往復で変わった:\n元: %q\n後: %q", text, back.Text())
					}
					if tab.ColsLocked == "" {
						for col := 0; col < tab.Cols; col++ {
							at := col
							if side == tableedit.After {
								at++
							}
							result, err = d.InsertColumns(target(tab), col, side, 1)
							next = mustChange(t, result, err)
							result, err = next.DeleteColumns(tableedit.Target{Number: 1}, at, 1)
							back = mustChange(t, result, err)
							if back.Text() != text {
								t.Fatalf("列の往復で変わった: 元 %q、後 %q", text, back.Text())
							}
						}
					}
				}
			})
		}
	}
}

func TestMergedRowDeletion(t *testing.T) {
	d := tableedit.Parse("<table><tr><td class='x' rowspan='2'>残す</td><td>a</td></tr><tr><td>b</td></tr><tr><td>c</td><td>d</td></tr></table>")
	result, err := d.DeleteRows(tableedit.Target{Number: 1}, 0, 1)
	next := mustChange(t, result, err)
	if next.Table(1).Cells[0].Text != "残す" || next.Table(1).Cells[0].Rowspan != 1 || !strings.Contains(next.Text(), "<td class='x'>残す</td><td>b</td>") {
		t.Fatalf("結合の始まりの行を削除すると中身が失われる: %s", next.Text())
	}
	result, err = d.InsertRows(tableedit.Target{Number: 1}, 0, tableedit.After, 1)
	next = mustChange(t, result, err)
	if next.Table(1).Cells[0].Rowspan != 3 {
		t.Fatal("またぐ結合の行数が増えない")
	}
}

func TestPipeCellWhitespaceAndShortRows(t *testing.T) {
	for _, test := range []struct {
		text, want string
		row, col   int
	}{
		{"| a   |\tb  | c |\n| --- | --- | --- |\n| d | e | f |", "| 変更   |\tb  | c |\n| --- | --- | --- |\n| d | e | f |", 0, 0},
		{"| a | b | c |\n| --- | --- | --- |\n| d   |", "| a | b | c |\n| --- | --- | --- |\n| d   |  | 変更 |", 1, 2},
		{"a|b|c\n---|---|---\nd", "a|b|c\n---|---|---\nd||変更", 1, 2},
		{"| a | b |\n| --- | --- |\n| c | d | 隠れたセル |", "| a | b |\n| --- | --- |\n| 変更 | d | 隠れたセル |", 1, 0},
	} {
		d := tableedit.Parse(test.text)
		result, err := d.SetCells(tableedit.Target{Number: 1}, []tableedit.CellEdit{{Row: test.row, Col: test.col, Text: "変更"}})
		next := mustChange(t, result, err)
		if next.Text() != test.want {
			t.Fatalf("未編集のセルの空白が変わった:\n元 %q\n後 %q\n期待 %q", test.text, next.Text(), test.want)
		}
	}
	d := tableedit.Parse("a|b\n---|---\nc|d")
	result, err := d.DeleteColumns(tableedit.Target{Number: 1}, 1, 1)
	next := mustChange(t, result, err)
	if next.Table(1).Cols != 1 {
		t.Fatal("外側のパイプの無い表を 1 列にできない")
	}
}

func TestHTMLSectionsAndMultiLineTag(t *testing.T) {
	text := "- <TABLE\r\n    class='t'>\r\n  <thead><tr><th>a</th></tr></thead>\r\n  <tbody><tr><td>b</td></tr></tbody>\r\n  </TABLE>"
	d := tableedit.Parse(text)
	tab := d.Table(1)
	if tab.Excluded != "" || text[tab.Tag.End-1] != '>' || !strings.Contains(text[tab.Tag.Start:tab.Tag.End], "class='t'") {
		t.Fatalf("複数行のタグの位置が違う: %+v", tab)
	}
	result, err := d.InsertRows(target(tab), 1, tableedit.Before, 1)
	next := mustChange(t, result, err)
	if len(next.Table(1).Sections) != 2 || next.Table(1).Sections[0].Rows != 1 || next.Table(1).Sections[1].Rows != 2 {
		t.Fatal("本文の先頭へ足した行が見出しの区画に入った")
	}
	result, err = d.DeleteRows(target(tab), 0, 1)
	next = mustChange(t, result, err)
	if !strings.Contains(next.Text(), "<thead></thead>") {
		t.Fatalf("空になった区画を消した: %s", next.Text())
	}
}

func TestHoleCellAndTightListWarning(t *testing.T) {
	d := tableedit.Parse("<table><tr><td>a</td><td>b</td></tr><tr><td>c</td></tr></table>")
	result, err := d.SetCells(tableedit.Target{Number: 1}, []tableedit.CellEdit{{Row: 1, Col: 0, Text: "変更"}})
	next := mustChange(t, result, err)
	if len(next.Table(1).Holes) != 1 {
		t.Fatal("穴のある 1 行の HTML の表を編集すると穴が失われる")
	}
	d = tableedit.Parse("- <table><tr><td>a</td></tr></table>\n- 次\n")
	result, err = d.SetCells(tableedit.Target{Number: 1}, []tableedit.CellEdit{{Row: 0, Col: 0, Text: "**太字**"}})
	mustChange(t, result, err)
	if len(result.Warnings) == 0 {
		t.Fatal("詰めたリストが広がる警告が無い")
	}
}

func TestLocksAndErrors(t *testing.T) {
	for _, test := range []struct {
		text       string
		rows, cols bool
	}{
		{"<table><tr><td>a</tr></table>", true, true},
		{"<table><td>a</td></table>", true, true},
		{"<table><tr><td rowspan='0'>a</td></tr></table>", true, true},
		{"<table><tr><td rowspan='9'>a</td></tr></table>", true, true},
		{"<table><tr><td colspan='x'>a</td></tr></table>", true, true},
		{"<table><col><tr><td>a</td></tr></table>", false, true},
		{"<table><tr><td>a</td><td>b</td></tr><tr><td>c</td></tr></table>", false, true},
		{"<table><tbody><tr><td>a</td></tr></tbody><thead><tr><th>b</th></tr></thead></table>", true, false},
	} {
		d := tableedit.Parse(test.text)
		tab := d.Table(1)
		if tab == nil || tab.Excluded != "" {
			t.Fatalf("表を読めない: %+v", tab)
		}
		if (tab.RowsLocked != "") != test.rows || (tab.ColsLocked != "") != test.cols {
			t.Fatalf("変更できない理由が違う: %+v", tab)
		}
		if test.rows {
			_, err := d.InsertRows(target(tab), 0, tableedit.After, 1)
			code(t, err, "unsupported")
		}
		if test.cols {
			_, err := d.InsertColumns(target(tab), 0, tableedit.After, 1)
			code(t, err, "unsupported")
		}
	}
	d := tableedit.Parse("| a | b |\n| --- | --- |\n| c | d |")
	_, err := d.SetCells(tableedit.Target{Number: 1, SourceHash: "古い指紋"}, nil)
	code(t, err, "stale")
	_, err = d.InsertRows(tableedit.Target{Number: 1}, 0, tableedit.Before, 1)
	code(t, err, "unsupported")
	_, err = d.DeleteRows(tableedit.Target{Number: 1}, 0, 1)
	code(t, err, "unsupported")
	_, err = d.DeleteColumns(tableedit.Target{Number: 1}, 0, 2)
	code(t, err, "unsupported")
	_, err = d.SetCells(tableedit.Target{Number: 1}, []tableedit.CellEdit{{Row: 0, Col: 0, Text: "a\x00b"}})
	code(t, err, "invalid")
	_, err = d.SetCells(tableedit.Target{Number: 1}, []tableedit.CellEdit{{Row: 0, Col: 0, Text: "> 引用"}})
	code(t, err, "unsupported")
	_, err = d.Paste(tableedit.Target{Number: 1}, 0, 0, [][]string{{"a"}, {"b", "c"}})
	code(t, err, "invalid")
	_, err = d.Paste(tableedit.Target{Number: 1}, 1, 1, [][]string{{"a", "b"}})
	code(t, err, "range")
	result, err := d.DeleteRows(tableedit.Target{Number: 1}, 1, 1)
	next := mustChange(t, result, err)
	if next.Table(1).Rows != 1 {
		t.Fatal("本文の行を 0 行にできない")
	}
	_, err = next.DeleteRows(tableedit.Target{Number: 1}, 0, 1)
	code(t, err, "unsupported")
	htmlOne := tableedit.Parse("<table><tr><td>a</td></tr></table>")
	_, err = htmlOne.DeleteRows(tableedit.Target{Number: 1}, 0, 1)
	code(t, err, "unsupported")
	h := tableedit.Parse("<table><tr><td rowspan='2'>a</td><td>b</td></tr><tr><td>c</td></tr></table>")
	_, err = h.Paste(tableedit.Target{Number: 1}, 0, 0, [][]string{{"x"}, {"y"}})
	code(t, err, "merged")
	result, err = h.Paste(tableedit.Target{Number: 1}, 0, 0, [][]string{{"x"}})
	mustChange(t, result, err)
	result, err = d.Paste(tableedit.Target{Number: 1}, 0, 0, [][]string{{"x", "y"}, {"z", "w"}})
	mustChange(t, result, err)
	extra := tableedit.Parse("| a | b |\n| --- | --- |\n| c | d | 余分 |")
	result, err = extra.InsertColumns(tableedit.Target{Number: 1}, 1, tableedit.After, 1)
	mustChange(t, result, err)
	if len(result.Warnings) == 0 || strings.Contains(result.Text, "余分") {
		t.Fatal("列の操作で消える余分なセルの警告が無い")
	}
}

func TestSourceSafetyAndParent(t *testing.T) {
	d := tableedit.Parse("<table><tr><td class='x'>a</td><td>b</td></tr></table>")
	for _, source := range []string{"a</td>", "x<y", "<!--", "<script>x</script>", "<style>x</style>", "<td>x</td>"} {
		result, err := d.SetCellSource(tableedit.Target{Number: 1}, 0, 0, source)
		if err == nil || result != nil {
			t.Fatalf("危険な原文を受け付けた: %q", source)
		}
	}
	result, err := d.SetCellSource(tableedit.Target{Number: 1}, 0, 0, "<strong>変える</strong>")
	next := mustChange(t, result, err)
	if next.Table(1).Cells[1].Source != "b" || !strings.Contains(next.Text(), "<td class='x'>") {
		t.Fatal("対象以外が変わった")
	}
	text := "<table><tr><td>\n\n| a |\n| --- |\n| b |\n\n</td></tr></table>"
	d = tableedit.Parse(text)
	if len(d.Tables()) != 2 || d.Table(2).Parent != 1 {
		t.Fatalf("親の表が違う: %+v", d.Tables())
	}
	result, err = d.SetCells(tableedit.Target{Number: 2}, []tableedit.CellEdit{{Row: 1, Col: 0, Text: "変える"}})
	next = mustChange(t, result, err)
	if next.Table(1).Excluded != "" {
		t.Fatal("親の表が対象外になった")
	}
	// 公開したスナップショットは内部の可変データを共有しない。
	snapshot := next.Table(1)
	snapshot.Cells[0].Text = "壊す"
	snapshot.Warnings = append(snapshot.Warnings, "壊す")
	if next.Table(1).Cells[0].Text == "壊す" {
		t.Fatal("公開値から内部が変わった")
	}
}

func TestConcurrentDocument(t *testing.T) {
	d := tableedit.Parse("<table><tr><td>a</td><td>b</td></tr></table>")
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Go(func() {
			for j := 0; j < 8; j++ {
				if len(d.Tables()) != 1 {
					t.Error("表の数が変わった")
				}
				result, err := d.SetCells(tableedit.Target{Number: 1}, []tableedit.CellEdit{{Row: 0, Col: 0, Text: "変更"}})
				if err != nil || !result.Changed {
					t.Errorf("並行の編集: %v", err)
				}
			}
		})
	}
	wg.Wait()
}

func TestDependencies(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", ".")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"github.com/xuri/", "/internal/workbook", "/internal/sync"} {
		if strings.Contains(string(out), bad) {
			t.Fatalf("公開 API に不要な依存がある: %s", bad)
		}
	}
}

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "文書.md")
	text := "本文\r\n末尾"
	if err := os.WriteFile(path, []byte("元"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := tableedit.WriteFile(context.Background(), path, text, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "\xef\xbb\xbf"+text {
		t.Fatal("BOM と本文が違う")
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0600 {
		t.Fatal("権限が変わった")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = tableedit.WriteFile(ctx, path, "変更", false); err == nil {
		t.Fatal("取り消された書き込みを受け付けた")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取り消しの原因を判定できない: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatal("一時ファイルが残った")
	}
	destination := filepath.Join(dir, "保存先")
	if err = os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	if err = tableedit.WriteFile(context.Background(), destination, "失敗する書き込み", false); err == nil {
		t.Fatal("フォルダーへ本文を書き込んだ")
	}
	entries, err = os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatal("失敗した書き込みの一時ファイルが残った")
	}
}
