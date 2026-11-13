package edit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

func headingRequest(t *testing.T, s *Service, h *testHost, index int, op map[string]any) (any, *Error) {
	t.Helper()
	info := getTables(t, s, h).Headings[index]
	return s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "heading": map[string]any{"tag": info.Tag, "range": string(info.Range)}, "op": op})
}

func levelHeading(delta int, scope string) map[string]any {
	return map[string]any{"type": "level", "delta": delta, "scope": scope}
}

func TestHeadingInformation(t *testing.T) {
	e := testEngine(t)
	text := "# 先頭\n\n本文\n\n  ## 二番 ##\n\n子\n===\n\n孫\n---\n\n> ## 引用\n\n- ### 項目\n\n<details>\n\n## 詳細\n\n</details>\n\n<h2>生</h2>\n\n<h2 data-sourcepos=\"1:1-1:8\">偽</h2>\n\n::include{file=part.md}\n\n<table><tr><td><h3>表</h3></td></tr></table>\n"
	s, h := testService(t, e, text, map[string]string{"part.md": "## 取り込み\n"})
	info := getTables(t, s, h).Headings
	rendered, err := h.renderer.Render(context.Background(), h.src.Text, session.FileContext(h.src.Root, h.src.File, h.src.Dir, false))
	if err != nil || len(info) != len(rendered.Headings) {
		t.Fatal(info, rendered.Headings, err)
	}
	for i, v := range info {
		p := rendered.Headings[i]
		if v.Index != i || v.ID != p.ID || v.Level != p.Level || v.Text != p.Text {
			t.Fatalf("一覧の対応: %+v / %+v", v, p)
		}
	}
	for i := range 4 {
		if info[i].Reason != "" || info[i].Section == nil || len(info[i].Ops) != 8 {
			t.Fatal(info[i])
		}
	}
	for i, word := range []string{"引用・リスト", "引用・リスト", "HTML のブロック", "HTML で書いた", "HTML で書いた", "取り込んだファイル", "表の中"} {
		v := info[i+4]
		if !strings.Contains(v.Reason, word) || v.Section != nil || len(v.Ops) != 0 {
			t.Fatal(v, word)
		}
	}
	if info[7].Tag != "" || info[7].Range != "" || info[8].Tag != "" || info[8].Range != "" {
		t.Fatal("生の見出しに位置を公開した", info[7:9])
	}
	// 表の情報を作る試しの描画とは別に、見出し欄だけのためには試しに描かない。
	s, h = testService(t, e, "# A\n\n## B\n\n本文\n\n## C\n\n末尾\n\n", nil)
	info = getTables(t, s, h).Headings
	if h.trials != 0 || info[1].Section.Lines != 3 || info[0].Section.Headings != 3 || info[1].Section.Headings != 1 || info[1].Ops["moveUp"] == "" || info[1].Ops["moveDown"] != "" || info[2].Ops["moveUp"] != "" || info[2].Ops["moveDown"] == "" {
		t.Fatal(h.trials, info)
	}
	m, _ := s.blocks(context.Background(), h.src)
	before := m.headings
	getTables(t, s, h)
	if before != m.headings {
		t.Fatal("描画ごとの一覧を再作成した")
	}
	s, h = testService(t, e, "# A\n\n> # 中\n\n# B\n", nil)
	info = getTables(t, s, h).Headings
	if info[0].Ops["deleteSection"] != headingOutlineReason || info[0].Ops["demote"] != "" {
		t.Fatal(info)
	}
	s, h = testService(t, e, "本文\n", nil)
	value, _ := json.Marshal(getTables(t, s, h))
	if !strings.Contains(string(value), `"headings":[]`) {
		t.Fatal(string(value))
	}
}

func TestHeadingLevels(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct {
		name, before, after, scope string
		delta                      int
	}{
		{"上げる", "## H\n\n本文\n", "# H\n\n本文\n", "heading", -1},
		{"下げる", "## H\n", "### H\n", "heading", 1},
		{"h1 から", "# H\n", "## H\n", "heading", 1},
		{"h3 から", "### H\n", "## H\n", "heading", -1},
		{"字下げと閉じ", "  ## H ##  \n", "  ### H ###  \n", "heading", 1},
		{"閉じが違う", "## H ####\n", "# H ####\n", "heading", -1},
		{"CRLF", "## H\r\n\r\n本文\r\n", "# H\r\n\r\n本文\r\n", "heading", -1},
		{"setext h1", "見出し\n===\n\n本文\n", "## 見出し\n\n本文\n", "heading", 1},
		{"setext h2", "見出し\n---\n", "# 見出し\n", "heading", -1},
		{"setext 閉じ", "foo #\n---\n", "# foo \\#\n", "heading", -1},
		{"setext のエスケープ済みの #", "foo \\#\n---\n", "# foo \\#\n", "heading", -1},
		{"ATX のエスケープ済みの #", "## 題 \\##\n", "### 題 \\##\n", "heading", 1},
		{"中身のない ATX", "#\n---\n", "##\n---\n", "heading", 1},
		{"複数行", "foo\nbar\n---\n", "# foo bar\n", "heading", -1},
		{"中身の BOM", "foo\n\ufeffbar\n---\n", "# foo \ufeffbar\n", "heading", -1},
		{"節ごと", "# H\n\n子\n---\n\n### 孫\n\n本文\n\n# 次\n", "## H\n\n### 子\n\n#### 孫\n\n本文\n\n# 次\n", "section", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h := testService(t, e, c.before, nil)
			v, err := headingRequest(t, s, h, 0, levelHeading(c.delta, c.scope))
			if err != nil || disk(t, h) != c.after {
				t.Fatalf("結果=%q、期待=%q、応答=%+v、誤り=%v", disk(t, h), c.after, v, err)
			}
			focus := v.(map[string]any)["focus"].(*headingFocus)
			if focus == nil || focus.Tag != headingTag(getTables(t, s, h).Headings[0].Level) || focus.Range != getTables(t, s, h).Headings[0].Range {
				t.Fatal(focus)
			}
		})
	}
}

func TestHeadingLevelRejections(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct {
		text, scope, reason string
		delta               int
	}{
		{"# H\n", "heading", "h1 より上", -1},
		{"###### H\n", "heading", "h6 より下", 1},
		{"## H\n\n###### 子\n", "section", "節の中に h6", 1},
		{"# H\n", "section", "節の中に h1", -1},
		{"## H\n\n> ### 子\n", "section", headingChildReason, 1},
		{"foo  \nbar\n---\n", "heading", headingSetextReason, -1},
		{"## H\n\n::include{file=part.md}\n\n## 次\n", "section", headingIncludeReason, 1},
	} {
		t.Run(c.reason+c.text, func(t *testing.T) {
			s, h := testService(t, e, c.text, map[string]string{"part.md": "### 子\n"})
			_, err := headingRequest(t, s, h, 0, levelHeading(c.delta, c.scope))
			if err == nil || err.Status != 422 || !strings.Contains(err.Message, c.reason) || disk(t, h) != c.text {
				t.Fatal(err, disk(t, h))
			}
		})
	}
}

func TestHeadingDeletion(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct{ name, before, after, scope string }{
		{"段落の境", "前\n## H\n後\n", "前\n\n後\n", "heading"},
		{"空行", "前\n\n## H\n\n\n後\n", "前\n\n後\n", "heading"},
		{"先頭", "## H\n\n\n本文\n", "本文\n", "heading"},
		{"末尾", "本文\n\n## H\n", "本文\n", "heading"},
		{"末尾の改行なし", "本文\n\n## H", "本文", "heading"},
		{"front matter", "---\ntitle: 題\n---\n## H\n\n本文\n", "---\ntitle: 題\n---\n\n本文\n", "heading"},
		{"setext", "見出し\n---\n\n本文\n", "本文\n", "heading"},
		{"水平線", "para\n## H\n---\n", "para\n\n---\n", "heading"},
		{"節", "## H\n\n本文\n\n### 子\n\n中\n\n## 次\n", "## 次\n", "section"},
		{"目次", "[[_TOC_]]\n\n## H\n\n本文\n", "[[_TOC_]]\n\n本文\n", "heading"},
		{"内側の目次", "<details>\n\n[TOC]\n\n</details>\n\n## H\n\n本文\n", "<details>\n\n[TOC]\n\n</details>\n\n本文\n", "heading"},
		{"参照が減る脚注", "## H[^a]\n\n本文[^a]\n\n[^a]: 脚注\n", "本文[^a]\n\n[^a]: 脚注\n", "heading"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h := testService(t, e, c.before, nil)
			v, err := headingRequest(t, s, h, 0, map[string]any{"type": "delete", "scope": c.scope})
			if err != nil || disk(t, h) != c.after {
				t.Fatal(v, err, disk(t, h), c.after)
			}
		})
	}
	for _, text := range []string{
		"- 前\n\n## H\n\n- 後\n",
		"前[^a]\n\n## H\n\n[^a]: 定義\n",
		"[前][a]\n\n## H\n\n[a]: https://example.com\n",
	} {
		s, h := testService(t, e, text, nil)
		scope := "section"
		if strings.HasPrefix(text, "- ") {
			scope = "heading"
		}
		if _, err := headingRequest(t, s, h, 0, map[string]any{"type": "delete", "scope": scope}); err == nil || err.Status != 422 || disk(t, h) != text {
			t.Fatal(err, disk(t, h))
		}
	}
}

func TestHeadingMoves(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct{ name, before, after string }{
		{"子の節", "## A\n\nA\n\n### 子\n\n中\n\n## B\n\nB\n", "## B\n\nB\n\n## A\n\nA\n\n### 子\n\n中\n"},
		{"空行なし", "## A\n## B\n", "## B\n\n## A\n"},
		{"末尾の改行なし", "## A\n\nA\n\n## B\n\nB", "## B\n\nB\n\n## A\n\nA"},
		{"末尾の空行", "## A\n\nA\n\n## B\n\nB\n\n\n", "## B\n\nB\n\n## A\n\nA\n\n\n"},
		{"CRLF", "## A\r\n\r\nA\r\n\r\n## B\r\n\r\nB\r\n", "## B\r\n\r\nB\r\n\r\n## A\r\n\r\nA\r\n"},
		{"脚注", "## A\n\nA[^a]\n\n[^a]: あ\n\n## B\n\nB[^b]\n\n[^b]: い\n", "## B\n\nB[^b]\n\n[^b]: い\n\n## A\n\nA[^a]\n\n[^a]: あ\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h := testService(t, e, c.before, nil)
			v, err := headingRequest(t, s, h, 0, map[string]any{"type": "move", "direction": "down"})
			if err != nil || disk(t, h) != c.after {
				t.Fatal(v, err, disk(t, h), c.after)
			}
			info := getTables(t, s, h).Headings
			index := len(info) - 1
			if c.name == "子の節" {
				index = 1
			}
			if focus := v.(map[string]any)["focus"].(*headingFocus); focus == nil || focus.Range != info[index].Range {
				t.Fatal(focus)
			}
		})
	}
	s, h := testService(t, e, "## A\n\nA\n\n## B\n\nB\n\n## C\n", nil)
	if _, err := headingRequest(t, s, h, 1, map[string]any{"type": "move", "direction": "up"}); err != nil || disk(t, h) != "## B\n\nB\n\n## A\n\nA\n\n## C\n" {
		t.Fatal(err, disk(t, h))
	}
	s, h = testService(t, e, "## A\n\n[x][same]\n\n[same]: https://example.com/a\n\n## B\n\n[y][same]\n\n[same]: https://example.com/b\n", nil)
	if _, err := headingRequest(t, s, h, 0, map[string]any{"type": "move", "direction": "down"}); err == nil || err.Status != 422 {
		t.Fatal(err)
	}
}

func TestHeadingWarningsAndHistory(t *testing.T) {
	e := testEngine(t)
	before := "[先](#同じ) [次](#同じ-1)\n\n## 同じ\n\nA\n\n## 同じ\n\nB\n"
	s, h := testService(t, e, before, nil)
	v, err := headingRequest(t, s, h, 0, map[string]any{"type": "move", "direction": "down"})
	if err != nil || len(v.(map[string]any)["warnings"].([]string)) != 1 {
		t.Fatal(v, err)
	}
	after := disk(t, h)
	if len(s.history.documents[pathKey(h.src.Path)].undo) != 1 {
		t.Fatal("一手の履歴ではない")
	}
	if _, err := applyJSON(t, s, h, `{"undo":true}`); err != nil || disk(t, h) != before {
		t.Fatal(err, disk(t, h))
	}
	if _, err := applyJSON(t, s, h, `{"redo":true}`); err != nil || disk(t, h) != after {
		t.Fatal(err, disk(t, h))
	}
	s, h = testService(t, e, "[先](#消す)\n\n## 消す\n\n本文\n", nil)
	v, err = headingRequest(t, s, h, 0, map[string]any{"type": "delete", "scope": "heading"})
	if err != nil || len(v.(map[string]any)["warnings"].([]string)) != 1 || !strings.Contains(v.(map[string]any)["warnings"].([]string)[0], "消した見出し") {
		t.Fatal(v, err)
	}
}

func TestHeadingRequestsAndConflicts(t *testing.T) {
	valid := map[string]any{"render": 1, "heading": map[string]any{"tag": "h2", "range": "1:1-1:4"}, "op": levelHeading(-1, "heading")}
	for _, change := range []func(map[string]any){
		func(m map[string]any) { m["extra"] = true },
		func(m map[string]any) { m["heading"].(map[string]any)["extra"] = true },
		func(m map[string]any) { m["heading"].(map[string]any)["tag"] = "h7" },
		func(m map[string]any) { m["heading"].(map[string]any)["tag"] = "p" },
		func(m map[string]any) { m["heading"].(map[string]any)["range"] = "1:1-0:2" },
		func(m map[string]any) { m["op"].(map[string]any)["delta"] = 0 },
		func(m map[string]any) { m["op"].(map[string]any)["delta"] = 2 },
		func(m map[string]any) { m["op"].(map[string]any)["delta"] = "1" },
		func(m map[string]any) { m["op"].(map[string]any)["scope"] = "all" },
		func(m map[string]any) { m["op"] = map[string]any{"type": "move", "direction": "left"} },
		func(m map[string]any) { m["op"] = map[string]any{"type": "delete", "scope": "heading", "delta": 1} },
		func(m map[string]any) {
			m["op"] = map[string]any{"type": "move", "direction": "up", "scope": "section"}
		},
	} {
		data, _ := json.Marshal(valid)
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		change(m)
		if _, err := parseRequest(m); err == nil || err.Status != 400 {
			t.Fatal(m, err)
		}
	}
	e := testEngine(t)
	s, h := testService(t, e, "## H\n", nil)
	h.src.Stale = true
	if _, err := s.Apply(context.Background(), nil, valid); err == nil || err.Status != 409 || err.Code != "stale" {
		t.Fatal(err)
	}
	h.src.Stale = false
	if err := os.WriteFile(h.src.Path, []byte("外の変更\n"), 0644); err != nil {
		t.Fatal(err)
	}
	info := getTables(t, s, h).Headings[0]
	if info.Reason != changed().Message || info.Section != nil || len(info.Ops) != 0 {
		t.Fatal(info)
	}
	if _, err := s.Apply(context.Background(), nil, valid); err == nil || err.Status != 409 || err.Code != "changed" {
		t.Fatal(err)
	}
}

func TestHeadingBufferAndKeep(t *testing.T) {
	e := testEngine(t)
	for _, operation := range []map[string]any{levelHeading(1, "heading"), {"type": "move", "direction": "down"}, {"type": "delete", "scope": "heading"}} {
		before := "\ufeff## A\n\nA\n\n## B\n\nB\n"
		s, h, sent := bufferService(t, e, before)
		_, err := headingRequest(t, s, h, 0, operation)
		if err != nil || len(*sent) != 1 {
			t.Fatal(err, *sent)
		}
		r := (*sent)[0]
		lines := bufferLines(before)
		lines = slicesForHeadingTest(lines, r)
		if !strings.HasPrefix(strings.Join(lines, "\n"), "\ufeff") || strings.Count(strings.Join(lines, "\n"), "\ufeff") != 1 {
			t.Fatal(lines)
		}
	}
	old := "## A\n\nA\n\n## B\n\nB\n"
	lines := originalHeadingLines(old)
	a, b := &headingEntry{first: 0, sectionEnd: 4}, &headingEntry{first: 4, sectionEnd: 7}
	r := finishHeadingRewrite(old, moveHeadingLines(lines, a, b, "\n"), "\n")
	if !reflect.DeepEqual(r.keep, []headingKeep{{5, 1, 3}, {1, 5, 3}}) {
		t.Fatal(r.keep)
	}
}

func TestHeadingSectionBoundariesAndFocus(t *testing.T) {
	e := testEngine(t)
	for _, text := range []string{
		"## H\n\n::include{file=part.md}\n\n## 次\n",
		"## H\n\n::include{file=missing.md}\n\n## 次\n",
	} {
		s, h := testService(t, e, text, map[string]string{"part.md": "### 子\n"})
		info := getTables(t, s, h).Headings[0]
		for _, op := range []string{"promoteSection", "demoteSection", "deleteSection", "moveDown"} {
			if info.Ops[op] != headingIncludeReason {
				t.Fatal(op, info)
			}
		}
		if _, err := headingRequest(t, s, h, 0, map[string]any{"type": "delete", "scope": "section"}); err == nil || err.Message != headingIncludeReason {
			t.Fatal(err)
		}
	}
	// 取り込んだ同レベルの見出しも節の境。取り込みの直前までの削除なら、取り込みは残る。
	s, h := testService(t, e, "## H\n\n本文\n\n::include{file=part.md}\n\n## 次\n", map[string]string{"part.md": "## 境\n\n中\n"})
	info := getTables(t, s, h).Headings
	if info[0].Section.Headings != 1 || info[0].Section.Lines != 3 || info[0].Ops["deleteSection"] != "" || info[0].Ops["moveDown"] == "" {
		t.Fatal(info)
	}
	if _, err := headingRequest(t, s, h, 0, map[string]any{"type": "delete", "scope": "section"}); err != nil || disk(t, h) != "::include{file=part.md}\n\n## 次\n" {
		t.Fatal(err, disk(t, h))
	}
	// 引用の中の同じレベルの見出しは兄弟にせず、前の兄弟の節が曖昧なことを理由にする。
	s, h = testService(t, e, "## A\n\n> ## 中\n\n## B\n", nil)
	info = getTables(t, s, h).Headings
	if info[2].Ops["moveUp"] != headingOutlineReason || info[0].Ops["moveDown"] != headingOutlineReason || info[1].Reason == "" {
		t.Fatal(info)
	}
	// 取り込んだ同じレベルの見出しとは入れ替えない。
	s, h = testService(t, e, "::include{file=part.md}\n\n## B\n", map[string]string{"part.md": "## 境\n"})
	if info = getTables(t, s, h).Headings; info[1].Ops["moveUp"] != "入れ替える前の見出しは操作できないので、移動できません。" {
		t.Fatal(info)
	}
	for _, c := range []struct {
		text, scope  string
		index, focus int
	}{
		{"# A\n\n## B\n\n### 子\n\n# C\n", "heading", 1, 1},
		{"# A\n\n## B\n\n### 子\n\n# C\n", "section", 1, 1},
		{"# A\n\n本文\n\n## B\n", "heading", 1, 0},
	} {
		s, h := testService(t, e, c.text, nil)
		v, err := headingRequest(t, s, h, c.index, map[string]any{"type": "delete", "scope": c.scope})
		if err != nil {
			t.Fatal(err)
		}
		info := getTables(t, s, h).Headings[c.focus]
		if got := v.(map[string]any)["focus"].(*headingFocus); got == nil || got.Tag != info.Tag || got.Range != info.Range {
			t.Fatal(got, info)
		}
	}
	// 本文が同じ兄弟を入れ替えてバイト列が変わらなくても、応答の項目はそろう。
	s, h = testService(t, e, "## 同じ\n\n## 同じ\n", nil)
	v, err := headingRequest(t, s, h, 0, map[string]any{"type": "move", "direction": "down"})
	if err != nil || v.(map[string]any)["focus"] == nil || v.(map[string]any)["warnings"] == nil || v.(map[string]any)["unchanged"] != true {
		t.Fatal(v, err)
	}
}

func TestHeadingTrialFailureAndChanges(t *testing.T) {
	e := testEngine(t)
	s, h := testService(t, e, "## H\n\n本文\n", nil)
	h.trial = func(string) (pipeline.Result, error) { return pipeline.Result{}, fmt.Errorf("試験用の失敗") }
	if _, err := headingRequest(t, s, h, 0, levelHeading(1, "heading")); err == nil || err.Status != 422 || err.Code != "render" || disk(t, h) != "## H\n\n本文\n" {
		t.Fatal(err)
	}
	s, h = testService(t, e, "## H\n\n本文\n", nil)
	h.trial = func(text string) (pipeline.Result, error) {
		if err := os.WriteFile(h.src.Path, []byte("外の変更\n"), 0644); err != nil {
			t.Fatal(err)
		}
		return h.renderer.Render(context.Background(), text, session.FileContext(h.src.Root, h.src.File, h.src.Dir, false))
	}
	if _, err := headingRequest(t, s, h, 0, levelHeading(1, "heading")); err == nil || err.Status != 409 || err.Code != "changed" || disk(t, h) != "外の変更\n" {
		t.Fatal(err)
	}
}

func slicesForHeadingTest(lines []string, r BufferEdit) []string {
	return append(append(append([]string{}, lines[:r.Start]...), r.Lines...), lines[r.End:]...)
}
