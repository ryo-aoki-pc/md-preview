package edit

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/engine"
	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"github.com/ryo-aoki-pc/md-tools/internal/resolve"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

// 保存されていないパスを使い、編集のためにファイルを開かないことも確かめる。
func bufferService(t *testing.T, engine *engine.Engine, text string) (*Service, *testHost, *[]BufferEdit) {
	t.Helper()
	root, err := resolve.RealPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "missing.md")
	h := &testHost{renderer: pipeline.New(engine), src: session.Source{Render: 1, Root: root, Path: path, DocPath: path, File: "missing.md", Current: true, Text: text, Buf: 7, Rev: 12.5}}
	result, err := h.renderer.Render(context.Background(), text, session.FileContext(root, "missing.md", "", false))
	if err != nil {
		t.Fatal(err)
	}
	h.src.HTML, h.src.Lines = result.HTML, result.Source
	h.src.HasSourcepos, h.src.SourceposInput = result.HasSourcepos, result.SourceposInput
	sent := []BufferEdit{}
	s := New(Options{Host: h, Mode: Buffer, Send: func(_ context.Context, e BufferEdit) *Error {
		sent = append(sent, e)
		return nil
	}, Written: func(session.Source, string) { t.Error("バッファの編集で Written を呼んだ") }})
	t.Cleanup(func() {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("存在しないファイルへ書いた", err)
		}
		if h.reloads != 0 || len(s.history.documents) != 0 {
			t.Error("バッファの編集で読み直しや履歴を使った")
		}
	})
	return s, h, &sent
}

func TestBufferPipeAndConfig(t *testing.T) {
	e := testEngine(t)
	s, h, sent := bufferService(t, e, pipeTable)
	if !reflect.DeepEqual(s.Config(), map[string]any{"scope": "current", "undo": false}) {
		t.Fatal(s.Config())
	}
	info := getTables(t, s, h)
	if !info.Document.Editable || !info.Tables[0].Editable || info.Undo != (undoState{}) {
		t.Fatal(info)
	}
	if _, err := applyJSON(t, s, h, `{"table":0,"op":{"type":"set","cells":[{"row":1,"col":0,"text":"更新"}]}}`); err != nil {
		t.Fatal(err)
	}
	want := BufferEdit{Path: h.src.DocPath, Buf: 7, Rev: 12.5, Start: 2, End: 3, Lines: []string{"| 更新 | 値 |"}}
	if len(*sent) != 1 || !reflect.DeepEqual((*sent)[0], want) {
		t.Fatalf("置き換え=%+v、期待=%+v", *sent, want)
	}
}

func TestBufferOperations(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct {
		name, text, body string
		start, end       int
		lines            []string
		want             string
	}{
		{"範囲と末尾の改行", "前\n\n本文\n\n後\n", `{"range":"3:1-3:6","text":"変更"}`, 2, 3, []string{"変更"}, "前\n\n変更\n\n後\n"},
		{"行の追加", "前\n\n本文\n\n後\n", `{"range":"3:1-3:6","text":"本文\n追加"}`, 3, 3, []string{"追加"}, "前\n\n本文\n追加\n\n後\n"},
		{"行の削除", "前\n\n本文\n削除\n\n後\n", `{"range":"3:1-4:6","text":"本文"}`, 3, 4, []string{}, "前\n\n本文\n\n後\n"},
		{"CRLF", "前\r\n\r\n本文\r\n\r\n後\r\n", `{"range":"3:1-3:6","text":"変更\n追加"}`, 2, 3, []string{"変更\r", "追加\r"}, "前\r\n\r\n変更\r\n追加\r\n\r\n後\r\n"},
		{"チェックボックス", "- [ ] 項目\n", `{"checkbox":"1:4-1:4","checked":true}`, 0, 1, []string{"- [x] 項目"}, "- [x] 項目\n"},
		{"HTML の複数行", "<table>\n<tr><td>前</td></tr>\n<tr><td>後</td></tr>\n</table>\n", `{"table":0,"op":{"type":"paste","row":0,"col":0,"values":[["新"],["次"]]}}`, 1, 3, []string{"<tr><td>新</td></tr>", "<tr><td>次</td></tr>"}, "<table>\n<tr><td>新</td></tr>\n<tr><td>次</td></tr>\n</table>\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h, sent := bufferService(t, e, c.text)
			if c.name == "範囲と末尾の改行" {
				h.src.DocPath, h.src.Path, h.src.Root, h.src.File = "", "", "", ""
				v, err := s.Text(context.Background(), nil, 1, "p", "3:1-3:6")
				if err != nil || v.(map[string]any)["text"] != "本文" {
					t.Fatal(v, err)
				}
			}
			if _, err := applyJSON(t, s, h, c.body); err != nil {
				t.Fatal(err)
			}
			if len(*sent) != 1 {
				t.Fatal(*sent)
			}
			r := (*sent)[0]
			if r.Path != h.src.DocPath || r.Start != c.start || r.End != c.end || !reflect.DeepEqual(r.Lines, c.lines) {
				t.Fatalf("置き換え=%+v、期待=%d:%d %q", r, c.start, c.end, c.lines)
			}
			lines := bufferLines(c.text)
			lines = append(append(append([]string{}, lines[:r.Start]...), r.Lines...), lines[r.End:]...)
			if got := strings.Join(lines, "\n") + "\n"; got != c.want {
				t.Fatalf("反映後の本文=%q、期待=%q", got, c.want)
			}
		})
	}
	for _, op := range []string{`{"type":"insertRows","row":1,"side":"after","count":2}`, `{"type":"deleteRows","row":1,"count":1}`} {
		s, h, sent := bufferService(t, e, pipeTable)
		if _, err := applyJSON(t, s, h, `{"table":0,"op":`+op+`}`); err != nil || len(*sent) != 1 {
			t.Fatal(*sent, err)
		}
		r := (*sent)[0]
		if strings.Contains(op, "insertRows") && (r.Start != 3 || r.End != 3 || len(r.Lines) != 2) || strings.Contains(op, "deleteRows") && (r.Start != 2 || r.End != 3 || len(r.Lines) != 0) {
			t.Fatal(r)
		}
	}
}

func TestBufferFragmentOperations(t *testing.T) {
	e := testEngine(t)
	for _, kind := range []string{"duplicateRows", "moveRows"} {
		t.Run(kind, func(t *testing.T) {
			text := pipeTable + "\n間の段落\n\n" + pipeTable
			s, h, sent := bufferService(t, e, text)
			info := getTables(t, s, h)
			op := map[string]any{"type": kind, "row": 1, "count": 1}
			table := 0
			want := pipeTable + "| 本文 | 値 |\n\n間の段落\n\n" + pipeTable
			if kind == "moveRows" {
				table = 1
				delete(op, "count")
				op["side"] = "after"
				op["from"] = moveFrom(info, 0, map[string]any{"row": 1, "count": 1})
				want = "| 見出し | 二列 |\n| :--- | ---: |\n\n間の段落\n\n" + pipeTable + "| 本文 | 値 |\n"
			}
			_, err := s.Apply(context.Background(), nil, jsonObject(t, map[string]any{"render": 1, "table": table, "op": op}))
			if err != nil || len(*sent) != 1 {
				t.Fatal(err, *sent)
			}
			r := (*sent)[0]
			lines := bufferLines(text)
			lines = append(append(append([]string{}, lines[:r.Start]...), r.Lines...), lines[r.End:]...)
			if strings.Join(lines, "\n")+"\n" != want {
				t.Fatalf("置換=%+v、期待=%q", r, want)
			}
			if kind == "moveRows" && r.End-r.Start < 5 {
				t.Fatal("表の間を含む一個の置換でない")
			}
		})
	}
}

func TestBufferUnchangedAndRejection(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct{ text, body string }{
		{"本文\n", `{"range":"1:1-1:6","text":"本文"}`},
		{pipeTable, `{"table":0,"op":{"type":"set","cells":[{"row":1,"col":0,"text":"本文"}]}}`},
	} {
		s, h, sent := bufferService(t, e, c.text)
		v, err := applyJSON(t, s, h, c.body)
		if err != nil || v.(map[string]any)["unchanged"] != true || len(*sent) != 0 {
			t.Fatal(v, err, *sent)
		}
	}
	s, h, _ := bufferService(t, e, "本文\n")
	want := &Error{Status: 409, Code: "rejected", Message: "Neovim の拒否"}
	s.opts.Send = func(context.Context, BufferEdit) *Error { return want }
	if _, got := applyJSON(t, s, h, `{"range":"1:1-1:6","text":"新"}`); got != want {
		t.Fatal(got)
	}
	h.trial = func(string) (pipeline.Result, error) { h.src.Stale = true; return pipeline.Result{}, nil }
	if _, got := applyJSON(t, s, h, `{"range":"1:1-1:6","text":"新"}`); got == nil || got.Code != "stale" {
		t.Fatal(got)
	}
}

func TestBufferUnsupported(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct {
		name, text, body, code string
		status                 int
	}{
		{"Send なし", "本文\n", `{"range":"1:1-1:6","text":"新"}`, "unsupported", 422},
		{"Buf なし", "本文\n", `{"range":"1:1-1:6","text":"新"}`, "unsupported", 422},
		{"ファイル文書", "本文\n", `{"range":"1:1-1:6","text":"新"}`, "excluded", 403},
		{"行のずれ", "本文\r次\n", `{"range":"1:1-1:6","text":"新"}`, "shift", 422},
		{"undo", "本文\n", `{"undo":true}`, "unsupported", 404},
		{"redo", "本文\n", `{"redo":true}`, "unsupported", 404},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h, sent := bufferService(t, e, c.text)
			switch c.name {
			case "Send なし":
				s.opts.Send = nil
			case "Buf なし":
				h.src.Buf = nil
			case "ファイル文書":
				h.src.Current = false
			}
			if _, err := applyJSON(t, s, h, c.body); err == nil || err.Status != c.status || err.Code != c.code {
				t.Fatal(err)
			}
			if len(*sent) != 0 {
				t.Fatal(*sent)
			}
			if c.status != 404 && getTables(t, s, h).Document.Editable {
				t.Fatal("編集できない文書を許した")
			}
		})
	}
}
