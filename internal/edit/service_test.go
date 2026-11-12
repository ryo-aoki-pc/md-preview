package edit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/engine"
	"github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"github.com/ryo-aoki-pc/md-tools/internal/resolve"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
	"golang.org/x/net/html"
)

type testHost struct {
	src             session.Source
	renderer        *pipeline.Renderer
	trials, reloads int
	trial           func(string) (pipeline.Result, error)
}

func (h *testHost) Source(*string) (session.Source, bool) { return h.src, h.src.Render != 0 }
func (h *testHost) Trial(ctx context.Context, src session.Source, text string) (pipeline.Result, error) {
	h.trials++
	if h.trial != nil {
		return h.trial(text)
	}
	return h.renderer.Render(ctx, text, session.FileContext(src.Root, src.File, src.Dir, false))
}
func (h *testHost) Reload(*string) { h.reloads++ }
func testEngine(t *testing.T) *engine.Engine {
	t.Helper()
	e, err := engine.New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close(context.Background()) })
	return e
}
func testService(t *testing.T, e *engine.Engine, text string, files map[string]string) (*Service, *testHost) {
	t.Helper()
	root, err := resolve.RealPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "main.md")
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
	h := &testHost{renderer: pipeline.New(e), src: session.Source{Root: root, Path: path, File: "main.md", Current: true}}
	refresh := func(text string) {
		result, err := h.renderer.Render(context.Background(), text, session.FileContext(root, "main.md", "", false))
		if err != nil {
			t.Fatal(err)
		}
		h.src.Render++
		h.src.Text, h.src.HTML, h.src.Lines = text, result.HTML, result.Source
		h.src.Stale = false
	}
	refresh(strings.TrimPrefix(text, "\ufeff"))
	s := New(Options{Host: h, Mode: File, Written: func(_ session.Source, text string) { refresh(text) }})
	return s, h
}
func getTables(t *testing.T, s *Service, h *testHost) tablesResponse {
	t.Helper()
	v, e := s.Tables(context.Background(), nil, h.src.Render)
	if e != nil {
		t.Fatal(e)
	}
	return v.(tablesResponse)
}
func applyJSON(t *testing.T, s *Service, h *testHost, body string) (any, *Error) {
	t.Helper()
	var m map[string]any
	if e := json.Unmarshal([]byte(body), &m); e != nil {
		t.Fatal(e)
	}
	if _, ok := m["render"]; !ok && m["undo"] == nil && m["redo"] == nil {
		m["render"] = h.src.Render
	}
	return s.Apply(context.Background(), nil, m)
}
func disk(t *testing.T, h *testHost) string {
	t.Helper()
	b, e := os.ReadFile(h.src.Path)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}

const pipeTable = "| 見出し | 二列 |\n| :--- | ---: |\n| 本文 | 値 |\n"
const htmlTable = "<table><tbody><tr><th>A</th><td>B</td></tr><tr><td>C</td><td>D</td></tr></tbody></table>\n"

func TestTableMatching(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct {
		name, text string
		editable   []bool
		trials     int
		reason     string
	}{
		{"パイプ", pipeTable, []bool{true}, 0, ""},
		{"HTML", htmlTable, []bool{true}, 1, ""},
		{"利用者の位置属性", strings.Replace(htmlTable, "<table>", `<table data-sourcepos="利用者">`, 1), []bool{true}, 1, ""},
		{"引用", "> " + strings.ReplaceAll(strings.TrimSuffix(pipeTable, "\n"), "\n", "\n> ") + "\n", []bool{true}, 0, ""},
		{"リスト", "- 項目\n\n  " + strings.ReplaceAll(strings.TrimSuffix(pipeTable, "\n"), "\n", "\n  ") + "\n", []bool{true}, 0, ""},
		{"HTML のリスト", "- 項目\n\n  " + htmlTable, []bool{true}, 1, ""},
		{"脚注", "参照[^1]\n\n[^1]: 注\n\n    " + strings.ReplaceAll(strings.TrimSuffix(pipeTable, "\n"), "\n", "\n    ") + "\n", []bool{true}, 0, ""},
		{"HTML の脚注", "参照[^1]\n\n[^1]: 注\n\n    " + htmlTable, []bool{true}, 1, ""},
		{"取り込み", "::include{file=part.md}\n\n" + pipeTable, []bool{false, true}, 0, "取り込んだファイルの表"},
		{"HTML の取り込み", "::include{file=part.md}\n\n" + htmlTable, []bool{false, true}, 1, "原文の表と対応づけられない"},
		{"JSON", "```json:table\n{\"markdown\":true,\"fields\":[{\"key\":\"a\",\"label\":\"列\"}],\"items\":[{\"a\":\"値\"}]}\n```\n", []bool{false}, 0, "JSON から作った表"},
		{"入れ子", "<table><tr><td><table><tr><td>中</td></tr></table></td></tr></table>\n", []bool{true, false}, 1, ""},
		{"コード", "```html\n" + htmlTable + "```\n\n" + pipeTable, []bool{true}, 0, ""},
		{"偽造", "gpe-00000000000000000000000000000000-1\n\n<table data-test=\"gpe-fake-1\"><tr><td>値</td></tr></table>\n", []bool{true}, 1, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			part := pipeTable
			if c.name == "HTML の取り込み" {
				part = htmlTable
			}
			s, h := testService(t, e, c.text, map[string]string{"part.md": part})
			r := getTables(t, s, h)
			if len(r.Tables) != len(c.editable) {
				t.Fatalf("表の数=%d、期待=%d: %s", len(r.Tables), len(c.editable), h.src.HTML)
			}
			for i, want := range c.editable {
				if r.Tables[i].Editable != want {
					t.Errorf("表%d=%+v、期待=%v", i, r.Tables[i], want)
				}
			}
			if c.reason != "" && r.Tables[0].Reason != c.reason {
				t.Error(r.Tables[0].Reason)
			}
			_ = getTables(t, s, h)
			if h.trials != c.trials {
				t.Errorf("試しの描画=%d、期待=%d", h.trials, c.trials)
			}
		})
	}
}
func TestManyTablesAndMismatch(t *testing.T) {
	e := testEngine(t)
	s, h := testService(t, e, strings.Repeat(htmlTable+"\n"+pipeTable+"\n", 60), nil)
	r := getTables(t, s, h)
	if len(r.Tables) != 120 || h.trials != 1 {
		t.Fatal(len(r.Tables), h.trials)
	}
	for _, table := range r.Tables {
		if !table.Editable {
			t.Fatal(table)
		}
	}
	s, h = testService(t, e, htmlTable+"\n"+pipeTable, nil)
	old := h.src.HTML
	h.src.HTML = strings.Replace(old, ">A<", ">違う<", 1)
	r = getTables(t, s, h)
	if r.Tables[0].Editable || !r.Tables[1].Editable {
		t.Fatal(r.Tables)
	}
	s, h = testService(t, e, pipeTable, nil)
	h.src.HTML = strings.ReplaceAll(h.src.HTML, "<th ", "<td ")
	h.src.HTML = strings.ReplaceAll(h.src.HTML, "</th>", "</td>")
	if getTables(t, s, h).Tables[0].Editable {
		t.Fatal("格子の不一致を許した")
	}
	s, h = testService(t, e, pipeTable, nil)
	h.src.HTML += h.src.HTML
	r = getTables(t, s, h)
	if r.Tables[0].Editable || r.Tables[1].Editable {
		t.Fatal("対応の重複を許した")
	}
}
func TestMarkerCannotEscapeTable(t *testing.T) {
	e := testEngine(t)
	for _, where := range []string{"文字", "属性", "表"} {
		t.Run(where, func(t *testing.T) {
			s, h := testService(t, e, htmlTable, nil)
			h.trial = func(text string) (pipeline.Result, error) {
				start := strings.Index(text, `data-sourcepos="`) + len(`data-sourcepos="`)
				end := strings.IndexByte(text[start:], '"')
				value := text[start : start+end]
				out := h.src.HTML
				if where == "文字" {
					out += value
				}
				if where == "属性" {
					out = `<p title="` + value + `"></p>` + out
				}
				if where == "表" {
					out = strings.Replace(out, "<table", `<table title="`+value+`"`, 1)
				}
				return pipeline.Result{HTML: out}, nil
			}
			if getTables(t, s, h).Tables[0].Editable {
				t.Fatal("表から外れた印を許した")
			}
		})
	}
}

func TestTextRangesAndWrites(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct{ name, text, tag, r, repl, want string }{
		{"CRLF", "前\r\n\r\n本文\r\n\r\n後", "p", "3:1-3:6", "変更\n二行", "前\r\n\r\n変更\r\n二行\r\n\r\n後"},
		{"末尾", "前\r\n\r\n末尾", "p", "3:1-3:6", "新\r次", "前\r\n\r\n新\r\n次"},
		{"一行", "本文", "p", "1:1-1:6", "新\r\n次", "新\n次"},
		{"水平線で終わる項目", "- item\r\n\r\n  ---\r\n", "li", "1:1-3:5", "- 新", "- 新\r\n"},
		{"取り込みの後ろ", "::include{file=part.md}\n\n後ろ", "p", "5:1-5:6", "変更", "::include{file=part.md}\n\n変更"},
		{"BOM", "\ufeff本文\r\n", "p", "1:1-1:6", "新", "\ufeff新\r\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h := testService(t, e, c.text, map[string]string{"part.md": "一\n二\n三"})
			value, err := s.Text(context.Background(), nil, h.src.Render, c.tag, Range(c.r))
			if err != nil {
				t.Fatal(err)
			}
			v, err := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "range": string(value.(map[string]any)["range"].(Range)), "text": c.repl})
			if err != nil {
				t.Fatal(v, err)
			}
			if got := disk(t, h); got != c.want {
				t.Fatalf("原文=%q、期待=%q", got, c.want)
			}
		})
	}
	for _, c := range []struct{ text, r, code string }{
		{"本文", "1:2-1:6", "range"}, {"本文", "1:1-1:2", "range"}, {"本文", "1:1-2:0", "range"},
		{"::include{file=part.md}\n後", "1:1-4:3", "include"},
		{"---\na: b\n---\n本文", "1:1-1:3", "frontmatter"},
		{"前\r本文", "1:1-1:3", "shift"},
	} {
		s, h := testService(t, e, c.text, map[string]string{"part.md": "一\n二\n三"})
		// 消毒後の木に位置がある場合も、従来の原文の検査で断る。
		h.src.HTML = `<p data-sourcepos="` + c.r + `">本文</p>`
		if _, err := s.Text(context.Background(), nil, h.src.Render, "p", Range(c.r)); err == nil || err.Code != c.code {
			t.Errorf("%q %s: %v", c.text, c.r, err)
		}
	}
}
func TestCheckboxesAndUnchanged(t *testing.T) {
	e := testEngine(t)
	for _, text := range []string{"- [ ] 項目\n", "- [X] 項目\n"} {
		s, h := testService(t, e, text, nil)
		root, err := htmltree.Fragment(context.Background(), h.src.HTML)
		if err != nil {
			t.Fatal(err)
		}
		nodes, err := htmltree.Elements(context.Background(), root, func(n *html.Node) bool { return htmltree.Has(n, "data-checkbox-sourcepos") })
		if err != nil {
			t.Fatal(h.src.HTML, err)
		}
		r := "3:4-3:4"
		if len(nodes) == 1 {
			r = htmltree.Get(nodes[0], "data-checkbox-sourcepos")
		}
		v, failure := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "checkbox": r, "checked": true})
		if failure != nil {
			t.Fatal(v, failure)
		}
		if !strings.Contains(disk(t, h), "[x]") {
			t.Fatal(disk(t, h))
		}
		v, failure = s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "checkbox": r, "checked": true})
		if failure != nil || v.(map[string]any)["unchanged"] != true {
			t.Fatal(v, failure)
		}
	}
}
func TestConflictEncodingAndTrialFailure(t *testing.T) {
	e := testEngine(t)
	s, h := testService(t, e, "本文", nil)
	for _, stale := range []bool{false, true} {
		h.src.Stale = stale
		render := h.src.Render + 1
		if stale {
			render = h.src.Render
		}
		if _, err := s.Tables(context.Background(), nil, render); err == nil || err.Code != "stale" || err.Status != 409 {
			t.Fatal(err)
		}
	}
	h.src.Stale = false
	if err := os.WriteFile(h.src.Path, []byte("外の変更"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := applyJSON(t, s, h, `{"range":"1:1-1:6","text":"新"}`); err == nil || err.Code != "changed" {
		t.Fatal(err)
	}
	if getTables(t, s, h).Document.Editable {
		t.Fatal("外の変更を許した")
	}
	if err := os.WriteFile(h.src.Path, []byte{0xff}, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := applyJSON(t, s, h, `{"range":"1:1-1:6","text":"新"}`); err == nil || err.Code != "encoding" || err.Status != 422 {
		t.Fatal(err)
	}
	s, h = testService(t, e, "本文", nil)
	h.trial = func(string) (pipeline.Result, error) { return pipeline.Result{}, errors.New("内部の絶対パス") }
	if _, err := applyJSON(t, s, h, `{"range":"1:1-1:6","text":"新"}`); err == nil || strings.Contains(err.Message, "絶対パス") {
		t.Fatal(err)
	}
	if disk(t, h) != "本文" {
		t.Fatal("描画が失敗した本文を書いた")
	}
}
func TestAllTableOperations(t *testing.T) {
	e := testEngine(t)
	for _, op := range []string{
		`{"type":"set","cells":[{"row":1,"col":0,"text":"変更"}]}`,
		`{"type":"paste","row":1,"col":0,"values":[["変更","値"]]}`,
		`{"type":"insertRows","row":1,"side":"after","count":1}`,
		`{"type":"deleteRows","row":1,"count":1}`,
		`{"type":"insertColumns","col":0,"side":"before","count":1}`,
		`{"type":"deleteColumns","col":1,"count":1}`,
		`{"type":"setSource","row":1,"col":0,"source":"<strong>変更</strong>"}`,
	} {
		t.Run(op, func(t *testing.T) {
			s, h := testService(t, e, htmlTable, nil)
			if _, err := applyJSON(t, s, h, `{"table":0,"op":`+op+`}`); err != nil {
				t.Fatal(err)
			}
			if disk(t, h) == htmlTable {
				t.Fatal("ファイルが変わらない")
			}
		})
	}
	s, h := testService(t, e, pipeTable, nil)
	_, err := applyJSON(t, s, h, `{"table":0,"op":{"type":"set","cells":[{"row":99,"col":0,"text":"変更"}]}}`)
	if err == nil || err.Status != 422 || err.Code != "range" || !strings.Contains(err.Message, "範囲外") {
		t.Fatal(err)
	}
}
func TestUndoRedoAndExternalChange(t *testing.T) {
	e := testEngine(t)
	s, h := testService(t, e, "本文", nil)
	if _, err := applyJSON(t, s, h, `{"range":"1:1-1:6","text":"変更"}`); err != nil {
		t.Fatal(err)
	}
	if !getTables(t, s, h).Undo.Undo {
		t.Fatal("履歴が無い")
	}
	if _, err := applyJSON(t, s, h, `{"undo":true}`); err != nil || disk(t, h) != "本文" {
		t.Fatal(err, disk(t, h))
	}
	if !getTables(t, s, h).Undo.Redo {
		t.Fatal("やり直せない")
	}
	if _, err := applyJSON(t, s, h, `{"redo":true}`); err != nil || disk(t, h) != "変更" {
		t.Fatal(err, disk(t, h))
	}
	if _, err := applyJSON(t, s, h, `{"undo":true}`); err != nil {
		t.Fatal(err)
	}
	if _, err := applyJSON(t, s, h, `{"range":"1:1-1:6","text":"新規"}`); err != nil {
		t.Fatal(err)
	}
	if getTables(t, s, h).Undo.Redo {
		t.Fatal("新しい編集で redo が残った")
	}
	if err := os.WriteFile(h.src.Path, []byte("外"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := applyJSON(t, s, h, `{"undo":true}`); err == nil || err.Code != "changed" {
		t.Fatal(err)
	}
	if len(s.history.documents) != 0 {
		t.Fatal("外の変更で履歴が残った")
	}
	if disk(t, h) != "外" {
		t.Fatal("外の変更を上書きした")
	}
}
func TestHistoryLimits(t *testing.T) {
	var h histories
	for i := 0; i < 205; i++ {
		h.push("文書", fmt.Sprint(i), fmt.Sprint(i+1))
	}
	if len(h.documents["文書"].undo) != 200 {
		t.Fatal("手数の上限")
	}
	for i := 0; i < 40; i++ {
		h.push(fmt.Sprint(i), "前", "後")
	}
	if len(h.documents) != 32 {
		t.Fatal("文書の上限")
	}
	h.push("大きい差分", strings.Repeat("a", 17*1024*1024), strings.Repeat("b", 17*1024*1024))
	if h.documents["大きい差分"] != nil {
		t.Fatal("差分の上限")
	}
}
