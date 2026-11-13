package edit

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

func responseChange(t *testing.T, value any) textChange {
	t.Helper()
	c, ok := value.(map[string]any)["change"].(textChange)
	if !ok {
		t.Fatalf("編集の応答に変化がない: %+v", value)
	}
	for _, v := range []string{c.Before, c.After} {
		if _, err := hex.DecodeString(v); err != nil || len(v) != 32 {
			t.Fatalf("本文の版が 32 桁の 16 進ではない: %q", v)
		}
	}
	data, err := json.Marshal(c)
	if err != nil || strings.Contains(string(data), `"keep":null`) {
		t.Fatal("行の対応は空でも配列にする", string(data), err)
	}
	return c
}

func checkKeep(t *testing.T, got, want []lineKeep) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("行の対応=%v、期待=%v", got, want)
	}
	for i, k := range got {
		if k[0] < 1 || k[1] < 1 || k[2] < 1 || i > 0 && got[i-1][0]+got[i-1][2] > k[0] {
			t.Fatalf("旧の行が昇順で重ならない対応ではない: %v", got)
		}
		for _, prev := range got[:i] {
			if prev[1] < k[1]+k[2] && k[1] < prev[1]+prev[2] {
				t.Fatalf("新の行が重なっている: %v", got)
			}
		}
	}
}

func TestTextVersion(t *testing.T) {
	s := New(Options{})
	src := session.Source{Path: "main.md"}
	v := s.version(src, "本文\n")
	if len(v) != 32 || v != s.version(src, "本文\n") || v != s.version(src, "\ufeff本文\n") {
		t.Fatal("同じ本文と BOM を除いた本文で版が違う", v)
	}
	if _, err := hex.DecodeString(v); err != nil {
		t.Fatal(err)
	}
	for _, other := range []string{
		s.version(src, "本分\n"), s.version(src, "本文"),
		s.version(session.Source{Path: "other.md"}, "本文\n"),
		New(Options{}).version(src, "本文\n"),
		s.version(session.Source{Buf: 1}, "本文\n"),
	} {
		if v == other {
			t.Fatal("本文・文書・実体が違っても版が同じ", other)
		}
	}
	if s.version(session.Source{Buf: 1}, "本文") == s.version(session.Source{Buf: 2}, "本文") {
		t.Fatal("無名バッファの番号を区別しなかった")
	}
	s, h := testService(t, testEngine(t), "前\n\n本文\n\n後\n", nil)
	before := getTables(t, s, h).Version
	value, err := applyJSON(t, s, h, `{"range":"3:1-3:6","text":"変更"}`)
	if err != nil {
		t.Fatal(err)
	}
	c := responseChange(t, value)
	if c.Before != before || c.After != getTables(t, s, h).Version || c.After == before {
		t.Fatal("編集と次の描画の版が一致しない", before, c)
	}
	renderVersion := getTables(t, s, h).Version
	if err := os.WriteFile(h.src.Path, []byte("外の変更\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := getTables(t, s, h); got.Version != renderVersion || got.Document.Editable {
		t.Fatal("外の変更で描画の版が変わった", got)
	}
}

func TestCommonKeep(t *testing.T) {
	for _, c := range []struct {
		name, before, after string
		want                []lineKeep
	}{
		{"段落", "前\n\n本文\n\n後\n", "前\n\n変更\n\n後\n", []lineKeep{{1, 1, 2}, {4, 4, 2}}},
		{"追加", "前\n後\n", "前\n追加\n後\n", []lineKeep{{1, 1, 1}, {2, 3, 1}}},
		{"削除", "前\n削除\n後\n", "前\n後\n", []lineKeep{{1, 1, 1}, {3, 2, 1}}},
		{"CRLF", "前\r\n本文\r\n後\r\n", "前\r\n変更\r\n後\r\n", []lineKeep{{1, 1, 1}, {3, 3, 1}}},
		{"改行の違い", "前\r\n後\n", "前\n後\n", []lineKeep{{2, 2, 1}}},
		{"最後の行に改行なし", "前\n本文\n後", "前\n変更\n後", []lineKeep{{1, 1, 1}, {3, 3, 1}}},
		{"末尾の LF を付ける", "前\n後", "前\n後\n", []lineKeep{{1, 1, 1}}},
		{"末尾の LF を外す", "前\n後\n", "前\n後", []lineKeep{{1, 1, 1}}},
		{"最後に空行を追加", "前\n", "前\n\n", []lineKeep{{1, 1, 1}}},
		{"空の文書", "", "本文\n", []lineKeep{}},
		{"全削除", "本文\n", "", []lineKeep{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			keep := commonKeep(c.before, c.after)
			checkKeep(t, keep, c.want)
			checkKeep(t, publicKeep(c.before, c.after, keep, pipeline.SourceMap{}), c.want)
		})
	}
}

func TestApplyChangeKeep(t *testing.T) {
	for _, c := range []struct {
		name, before, body string
		want               []lineKeep
	}{
		{"段落", "前\n\n本文\n\n後\n", `{"range":"3:1-3:6","text":"変更"}`, []lineKeep{{1, 1, 2}, {4, 4, 2}}},
		{"ブロック追加", "前\n\n後\n", `{"insert":{"anchor":"1:1-1:3","side":"after"},"text":"追加"}`, []lineKeep{{1, 1, 2}, {3, 5, 1}}},
		{"範囲の削除", "前\n\n本文\n削除\n\n後\n", `{"range":"3:1-4:6","text":"本文"}`, []lineKeep{{1, 1, 2}, {5, 4, 2}}},
		{"CRLF", "前\r\n\r\n本文\r\n\r\n後\r\n", `{"range":"3:1-3:6","text":"変更"}`, []lineKeep{{1, 1, 2}, {4, 4, 2}}},
		{"最後の行に改行なし", "前\n\n本文\n\n後", `{"range":"3:1-3:6","text":"変更"}`, []lineKeep{{1, 1, 2}, {4, 4, 2}}},
		{"チェックボックス", "前\n\n- [ ] 項目\n\n後\n", `{"checkbox":"3:4-3:4","checked":true}`, []lineKeep{{1, 1, 2}, {4, 4, 2}}},
		{"表の行を追加", pipeTable + "\n後\n", `{"table":0,"op":{"type":"insertRows","row":1,"side":"after","count":1}}`, []lineKeep{{1, 1, 3}, {4, 5, 2}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h := testService(t, testEngine(t), c.before, nil)
			version := getTables(t, s, h).Version
			value, err := applyJSON(t, s, h, c.body)
			if err != nil {
				t.Fatal(err)
			}
			change := responseChange(t, value)
			checkKeep(t, change.Keep, c.want)
			if change.Before != version || change.After != getTables(t, s, h).Version {
				t.Fatal(change)
			}
		})
	}
	s, h := testService(t, testEngine(t), "本文\n", nil)
	value, err := applyJSON(t, s, h, `{"range":"1:1-1:6","text":"本文"}`)
	if err != nil || value.(map[string]any)["unchanged"] != true || value.(map[string]any)["change"] != nil {
		t.Fatal("変わらなかった応答に change がある", value, err)
	}
}

func TestHeadingChangeAndHistory(t *testing.T) {
	for _, c := range []struct {
		name, before string
		op           map[string]any
		want         []lineKeep
	}{
		{"節の入れ替え", "## A\n\nA\n\n## B\n\nB\n", map[string]any{"type": "move", "direction": "down"}, []lineKeep{{1, 5, 3}, {5, 1, 3}}},
		{"見出しだけ削除", "## H\n\n本文\n\n## 次\n", map[string]any{"type": "delete", "scope": "heading"}, []lineKeep{{3, 1, 3}}},
		{"レベル", "## H\n\n本文\n", levelHeading(-1, "heading"), []lineKeep{{2, 2, 2}}},
		{"setext", "見出し\n===\n\n本文\n", levelHeading(1, "heading"), []lineKeep{{3, 2, 2}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h := testService(t, testEngine(t), c.before, nil)
			value, err := headingRequest(t, s, h, 0, c.op)
			if err != nil {
				t.Fatal(err)
			}
			written := responseChange(t, value)
			checkKeep(t, written.Keep, c.want)
			for _, redo := range []bool{false, true} {
				body := `{"undo":true}`
				want := reverseKeep(c.want)
				if redo {
					body, want = `{"redo":true}`, c.want
				}
				value, err = applyJSON(t, s, h, body)
				if err != nil {
					t.Fatal(err)
				}
				got := responseChange(t, value)
				checkKeep(t, got.Keep, want)
				if redo && (got.Before != written.Before || got.After != written.After) || !redo && (got.Before != written.After || got.After != written.Before) {
					t.Fatal("履歴の版の前後が違う", written, got)
				}
			}
		})
	}
	// 別々の手の対応を保持し、連続して二手戻しても取り違えない。
	s, h := testService(t, testEngine(t), "## A\n\nA\n\n## B\n\nB\n", nil)
	v, err := headingRequest(t, s, h, 0, map[string]any{"type": "move", "direction": "down"})
	if err != nil {
		t.Fatal(err)
	}
	first := responseChange(t, v)
	v, err = headingRequest(t, s, h, 0, levelHeading(1, "heading"))
	if err != nil {
		t.Fatal(err)
	}
	second := responseChange(t, v)
	for _, step := range []textChange{second, first} {
		v, err = applyJSON(t, s, h, `{"undo":true}`)
		if err != nil {
			t.Fatal(err)
		}
		got := responseChange(t, v)
		checkKeep(t, got.Keep, reverseKeep(step.Keep))
		if got.Before != step.After || got.After != step.Before {
			t.Fatal(step, got)
		}
	}
}

func TestPublicKeepIncludes(t *testing.T) {
	renderer := pipeline.New(testEngine(t))
	ctx := pipeline.Context{ReadInclude: func(string) pipeline.IncludeRead {
		return pipeline.IncludeRead{Kind: pipeline.IncludeText, Text: "一\n二\n三"}
	}}
	for _, c := range []struct {
		name, before, after string
		want                []lineKeep
	}{
		{"変更の前に取り込み", "::include{file=part.md}\n\n本文\n\n後\n", "::include{file=part.md}\n\n変更\n追加\n\n後\n", []lineKeep{{1, 1, 3}, {4, 4, 1}, {6, 7, 2}}},
		{"変更の後に取り込み", "本文\n\n::include{file=part.md}\n\n後\n", "変更\n追加\n\n::include{file=part.md}\n\n後\n", []lineKeep{{2, 3, 1}, {3, 4, 3}, {6, 7, 2}}},
		{"指定を書き換え", "前\n::include{file=old.md}\n後\n", "前\n::include{file=new.md}\n後\n", []lineKeep{{1, 1, 1}}},
		{"指定を追加", "前\n後\n", "前\n::include{file=part.md}\n後\n", []lineKeep{{1, 1, 1}}},
		{"指定を削除", "前\n::include{file=part.md}\n後\n", "前\n後\n", []lineKeep{{1, 1, 1}}},
		{"複数の指定をまたぐ", "::include{file=part.md}\n\n前\n本文\n後\n\n::include{file=part.md}\n末尾", "::include{file=part.md}\n\n前\n変更\n追加\n後\n\n::include{file=part.md}\n末尾", []lineKeep{{1, 1, 3}, {4, 4, 2}, {7, 8, 2}, {9, 10, 3}, {12, 13, 1}}},
		{"front matter", "---\na: b\n---\n\n本文\n\n後\n", "---\na: b\n---\n\n変更\n\n後\n", []lineKeep{{4, 4, 1}, {6, 6, 2}}},
		{"単独の CR", "前\r本文\n後\n", "変更\r本文\n後\n", []lineKeep{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, renderErr := renderer.Render(context.Background(), c.before, ctx)
			if renderErr != nil {
				t.Fatal(renderErr)
			}
			checkKeep(t, publicKeep(c.before, c.after, commonKeep(c.before, c.after), r.Source), c.want)
		})
	}
	// include をまたぐ編集でも、応答・履歴の公開の位置は現在の展開から求める。
	s, h := testService(t, testEngine(t), "::include{file=part.md}\n\n本文\n\n後\n", map[string]string{"part.md": "一\n二\n三"})
	v, err := applyJSON(t, s, h, `{"range":"5:1-5:6","text":"変更\n追加"}`)
	if err != nil {
		t.Fatal(err)
	}
	written := responseChange(t, v)
	checkKeep(t, written.Keep, []lineKeep{{1, 1, 3}, {4, 4, 1}, {6, 7, 2}})
	v, err = applyJSON(t, s, h, `{"undo":true}`)
	if err != nil {
		t.Fatal(err)
	}
	checkKeep(t, responseChange(t, v).Keep, reverseKeep(written.Keep))
}

func TestBufferChangeVersion(t *testing.T) {
	for _, text := range []string{"前\n\n本文\n\n後\n", "前\n\n本文\n\n後", "前\r\n\r\n本文\r\n\r\n後\r\n"} {
		s, h, sent := bufferService(t, testEngine(t), text)
		before := getTables(t, s, h).Version
		v, err := applyJSON(t, s, h, `{"range":"3:1-3:6","text":"変更\n追加"}`)
		if err != nil || len(*sent) != 1 {
			t.Fatal(err, sent)
		}
		c := responseChange(t, v)
		lines := slicesForHeadingTest(bufferLines(text), (*sent)[0])
		after := strings.Join(lines, "\n") + "\n"
		if c.Before != before || c.After != s.version(h.src, after) {
			t.Fatal("版が Lua で置き換えた後の本文と違う", c)
		}
		checkKeep(t, c.Keep, commonKeep(text, after))
		r, renderErr := h.Trial(context.Background(), h.src, after)
		if renderErr != nil {
			t.Fatal(renderErr)
		}
		h.src.Text, h.src.HTML, h.src.Lines = after, r.HTML, r.Source
		h.src.HasSourcepos, h.src.SourceposInput = r.HasSourcepos, r.SourceposInput
		h.src.Render++
		if got := getTables(t, s, h).Version; got != c.After {
			t.Fatal(got, c.After)
		}
	}
}

func TestTablePublicStart(t *testing.T) {
	for _, c := range []struct {
		name, text string
		want       []int
	}{
		{"パイプ", pipeTable, []int{1}},
		{"取り込み", "::include{file=part.md}\n\n" + pipeTable, []int{1, 6}},
		{"front matter", "---\na: b\n---\n\n" + pipeTable, []int{5}},
		{"偽の位置", strings.Replace(htmlTable, "<table>", `<table data-sourcepos="99:1-99:3">`, 1), []int{1}},
		{"行のずれ", "前\r\n\r\n" + strings.ReplaceAll(pipeTable, "\n", "\r"), []int{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h := testService(t, testEngine(t), c.text, map[string]string{"part.md": pipeTable})
			info := getTables(t, s, h)
			if len(c.want) == 0 {
				for _, tab := range info.Tables {
					if tab.Start != 0 {
						t.Fatal(tab)
					}
				}
				return
			}
			if len(info.Tables) != len(c.want) {
				t.Fatal(info.Tables)
			}
			for i, want := range c.want {
				if info.Tables[i].Start != want {
					t.Fatal(info.Tables[i], want)
				}
			}
		})
	}
}
