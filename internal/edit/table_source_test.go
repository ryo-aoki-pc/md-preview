package edit

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

func applyTableSource(t *testing.T, s *Service, h *testHost, source string) (any, *Error) {
	t.Helper()
	return s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "table": 0,
		"op": map[string]any{"type": "setTableSource", "source": source}})
}

func TestTableSourceEdit(t *testing.T) {
	e := testEngine(t)
	for _, source := range []string{
		"| 見出し | 二列 |\n| ---: | :---: |\n| 本文 | 値 |",
		"| 見出し | 二列 | 三列 |\n| --- | --- | --- |\n| 縦\\|棒 | 値 | 追加 |\n| 次 | 二 | 三 |",
		"| 一列 |\n| --- |",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			for _, bom := range []string{"", "\ufeff"} {
				t.Run(source+ending+bom, func(t *testing.T) {
					old := bom + strings.ReplaceAll("前\n\n"+pipeTable+"\n後\n", "\n", ending)
					s, h := testService(t, e, old, nil)
					table := getTables(t, s, h).Tables[0]
					value, err := s.Text(context.Background(), nil, h.src.Render, "table", table.Anchor)
					if err != nil || value.(map[string]any)["text"] != strings.TrimSuffix(pipeTable, "\n") {
						t.Fatalf("取得=%v、誤り=%v", value, err)
					}
					result, err := applyTableSource(t, s, h, source)
					if err != nil {
						t.Fatal(err)
					}
					want := bom + strings.ReplaceAll("前\n\n"+source+"\n\n後\n", "\n", ending)
					if disk(t, h) != want || len(getTables(t, s, h).Tables) != 1 {
						t.Fatalf("原文=%q、期待=%q", disk(t, h), want)
					}
					change := result.(map[string]any)["change"].(textChange)
					if len(change.Keep) != 2 || change.Keep[0] != (lineKeep{1, 1, 2}) || change.Keep[1][0] != 6 {
						t.Fatalf("表の外の行対応=%v", change.Keep)
					}
					if _, err := applyJSON(t, s, h, `{"undo":true}`); err != nil || disk(t, h) != old {
						t.Fatalf("一手の取り消し=%v、原文=%q", err, disk(t, h))
					}
					if _, err := applyJSON(t, s, h, `{"redo":true}`); err != nil || disk(t, h) != want {
						t.Fatalf("やり直し=%v、原文=%q", err, disk(t, h))
					}
				})
			}
		}
	}
}

func TestTableSourceRejectsOtherBlocks(t *testing.T) {
	e := testEngine(t)
	for _, source := range []string{
		"段落", "| 列 |\n| 区切りでない |\n| 値 |", "段落\n\n" + pipeTable,
		strings.TrimSuffix(pipeTable, "\n") + "\n\n段落", pipeTable + "\n" + pipeTable,
		htmlTable, "", "\n \n\t\n",
		"| 列 |\n| --- |\n| <table><tr><td>入れ子</td></tr></table> |",
	} {
		t.Run(source, func(t *testing.T) {
			old := "前\n\n" + pipeTable + "\n後\n"
			s, h := testService(t, e, old, nil)
			_, err := applyTableSource(t, s, h, source)
			if err == nil || err.Status != 422 || disk(t, h) != old || getTables(t, s, h).Undo.Undo {
				t.Fatalf("拒否=%v、原文=%q", err, disk(t, h))
			}
		})
	}
}

// 入力欄の前後の空行は取り除き、表の直後に空行なしで続くブロックは保つ。
func TestTableSourceTrimsBlankLinesAndKeepsFollowingBlocks(t *testing.T) {
	e := testEngine(t)
	source := "| 列 |\n| --- |\n| 値 |"
	for _, value := range []string{source + "\n", "\n" + source, "\n \n" + source + "\n\n", strings.ReplaceAll(source, "\n", "\r\n") + "\r\n"} {
		t.Run(value, func(t *testing.T) {
			s, h := testService(t, e, "前\n\n"+pipeTable+"\n後\n", nil)
			if _, err := applyTableSource(t, s, h, value); err != nil || disk(t, h) != "前\n\n"+source+"\n\n後\n" {
				t.Fatalf("前後の空行=%v、原文=%q", err, disk(t, h))
			}
		})
	}
	// 見出しは表を終えるので、空行なしで続いても表の外のまま残す。
	s, h := testService(t, e, "前\n\n"+pipeTable+"# 見出し\n\n後\n", nil)
	table := getTables(t, s, h).Tables[0]
	value, err := s.Text(context.Background(), nil, h.src.Render, "table", table.Anchor)
	if err != nil || value.(map[string]any)["text"] != strings.TrimSuffix(pipeTable, "\n") {
		t.Fatalf("見出しが続く表の取得=%v、誤り=%v", value, err)
	}
	if _, err := applyTableSource(t, s, h, source); err != nil || disk(t, h) != "前\n\n"+source+"\n# 見出し\n\n後\n" {
		t.Fatalf("見出しが続く表=%v、原文=%q", err, disk(t, h))
	}
	// 空行なしで続く文の行は GFM では表の行になるので、取得にも含め、置き換えで消える。
	s, h = testService(t, e, pipeTable+"続き\n\n後\n", nil)
	table = getTables(t, s, h).Tables[0]
	value, err = s.Text(context.Background(), nil, h.src.Render, "table", table.Anchor)
	if err != nil || value.(map[string]any)["text"] != pipeTable+"続き" {
		t.Fatalf("続く行の取得=%v、誤り=%v", value, err)
	}
	if _, err := applyTableSource(t, s, h, source); err != nil || disk(t, h) != source+"\n\n後\n" {
		t.Fatalf("続く行の置き換え=%v、原文=%q", err, disk(t, h))
	}
	// 置き換えた表の最後の行の後ろに空行なしで文を書くと、その文も表の行になるので受ける。
	s, h = testService(t, e, "前\n\n"+pipeTable+"\n後\n", nil)
	if _, err := applyTableSource(t, s, h, source+"\n追加の行"); err != nil || disk(t, h) != "前\n\n"+source+"\n追加の行\n\n後\n" {
		t.Fatalf("表の行になる文=%v、原文=%q", err, disk(t, h))
	}
}

func TestTableSourceTargets(t *testing.T) {
	e := testEngine(t)
	for _, old := range []string{
		htmlTable, "> " + strings.ReplaceAll(strings.TrimSuffix(pipeTable, "\n"), "\n", "\n> ") + "\n",
		"::include{file=part.md}\n", "---\n" + pipeTable + "---\n",
		"<table><tr><td><table><tr><td>中</td></tr></table></td></tr></table>\n",
	} {
		t.Run(old, func(t *testing.T) {
			s, h := testService(t, e, old, map[string]string{"part.md": pipeTable})
			if _, err := applyTableSource(t, s, h, strings.TrimSuffix(pipeTable, "\n")); err == nil || disk(t, h) != old {
				t.Fatalf("対象外を断らなかった: %v", err)
			}
			if _, err := s.Text(context.Background(), nil, h.src.Render, "table", "1:1-3:20"); err == nil {
				t.Fatal("偽の表の範囲を受けた")
			}
		})
	}
	for _, prefix := range []string{"---\ntitle: 題\n---\n\n", "::include{file=part.md}\n\n"} {
		s, h := testService(t, e, prefix+pipeTable, map[string]string{"part.md": "取り込み\n\n本文\n"})
		if _, err := applyTableSource(t, s, h, "| 列 |\n| --- |\n| 値 |"); err != nil {
			t.Fatalf("前に展開がある表の編集=%v", err)
		}
	}
}

func TestTableSourceEdgesAndSecondTable(t *testing.T) {
	e := testEngine(t)
	for _, ending := range []string{"\n", "\r\n"} {
		for _, bom := range []string{"", "\ufeff"} {
			old := bom + strings.ReplaceAll(strings.TrimSuffix(pipeTable, "\n"), "\n", ending)
			s, h := testService(t, e, old, nil)
			source := "| 列 |\n| --- |\n| 一 |\n| 二 |"
			if _, err := applyTableSource(t, s, h, source); err != nil || disk(t, h) != bom+strings.ReplaceAll(source, "\n", ending) {
				t.Fatalf("先頭と末尾の表=%v、原文=%q", err, disk(t, h))
			}
		}
	}
	old := pipeTable + "\n中間\n\n" + pipeTable + "\n後\n"
	s, h := testService(t, e, old, nil)
	second := getTables(t, s, h).Tables[1]
	value, err := s.Text(context.Background(), nil, h.src.Render, "table", second.Anchor)
	if err != nil || value.(map[string]any)["table"] != 1 {
		t.Fatalf("二番目の取得=%v、誤り=%v", value, err)
	}
	source := "| 変更 |\n| :---: |\n| 値 |"
	if _, err := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "table": 1,
		"op": map[string]any{"type": "setTableSource", "source": source}}); err != nil || disk(t, h) != pipeTable+"\n中間\n\n"+source+"\n\n後\n" {
		t.Fatalf("二番目の保存=%v、原文=%q", err, disk(t, h))
	}
}

func TestTableSourcePreservesOutside(t *testing.T) {
	e := testEngine(t)
	// 表の中の新しい脚注参照が、外の脚注の描画を増やす変更は断る。
	old := pipeTable + "\n後[^a]\n\n[^a]: 脚注\n"
	s, h := testService(t, e, old, nil)
	if _, err := applyTableSource(t, s, h, "| 列 |\n| --- |\n| 値[^a] |"); err == nil || err.Code != "verify" || disk(t, h) != old {
		t.Fatalf("表の外の変化=%v", err)
	}
	// 表以外の位置のない節点も、属性の変化を見落とさない。
	s, h = testService(t, e, pipeTable+"\n<div title=\"保持\">本文</div>\n", nil)
	getTables(t, s, h)
	h.trial = func(text string) (result pipeline.Result, err error) {
		result, err = h.renderer.Render(context.Background(), text, session.FileContext(h.src.Root, h.src.File, h.src.Dir, false))
		result.HTML = strings.Replace(result.HTML, `title="保持"`, `title="変化"`, 1)
		return
	}
	if _, err := applyTableSource(t, s, h, "| 列 |\n| --- |\n| 値 |"); err == nil || err.Code != "verify" {
		t.Fatalf("表の外の属性=%v", err)
	}
}

func TestTableSourceStaleAndBuffer(t *testing.T) {
	e := testEngine(t)
	s, h := testService(t, e, pipeTable, nil)
	anchor := getTables(t, s, h).Tables[0].Anchor
	h.src.Stale = true
	if _, err := s.Text(context.Background(), nil, h.src.Render, "table", anchor); err == nil || err.Code != "stale" {
		t.Fatalf("古い取得=%v", err)
	}
	if _, err := applyTableSource(t, s, h, "| 列 |\n| --- |"); err == nil || err.Code != "stale" || disk(t, h) != pipeTable {
		t.Fatalf("古い保存=%v", err)
	}
	for _, ending := range []string{"\n", "\r\n"} {
		old := strings.ReplaceAll("前\n\n"+pipeTable+"\n後\n", "\n", ending)
		s, h, sent := bufferService(t, e, old)
		source := "| 新列 |\n| ---: |\n| 一 |\n| 二 |"
		if _, err := applyTableSource(t, s, h, source); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.ReplaceAll(source, "\n", ending)+strings.TrimSuffix(ending, "\n"), "\n")
		want := BufferEdit{Path: h.src.DocPath, Buf: h.src.Buf, Rev: h.src.Rev, Start: 2, End: 5, Lines: lines}
		if len(*sent) != 1 || !reflect.DeepEqual((*sent)[0], want) {
			t.Fatalf("置き換え=%+v、期待=%+v", *sent, want)
		}
	}
}
