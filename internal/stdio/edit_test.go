package stdio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ryo-aoki-pc/md-preview/internal/edit"
	"github.com/ryo-aoki-pc/md-preview/internal/server"
	"github.com/ryo-aoki-pc/md-preview/internal/session"
)

type editApp struct {
	out    *outputBuffer
	input  *io.PipeWriter
	server *server.Server
	state  *session.Session
	done   chan struct{}
	code   int
}

func startEditApp(t *testing.T, hello string, timeout time.Duration) *editApp {
	t.Helper()
	input, writer := io.Pipe()
	a := &editApp{out: &outputBuffer{}, input: writer, done: make(chan struct{})}
	started := make(chan struct{}, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	go func() {
		a.code = Run(ctx, Options{Stdin: input, Stdout: a.out, Renderer: renderer{}, Version: "試験版", EditTimeout: timeout, OnStart: func(s *server.Server, state *session.Session) error {
			a.server, a.state = s, state
			started <- struct{}{}
			return nil
		}})
		close(a.done)
	}()
	t.Cleanup(func() { _ = writer.Close(); cancel(); a.finish(t) })
	if _, err := io.WriteString(writer, hello+"\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("hello で起動しない")
	}
	wait(t, func() bool { return len(a.out.messages()) >= 2 })
	return a
}
func (a *editApp) finish(t *testing.T) {
	t.Helper()
	select {
	case <-a.done:
		if a.code != 0 {
			t.Error("終了コード", a.code)
		}
	case <-time.After(6 * time.Second):
		t.Error("応答待ちを終えずに Run が止まった")
	}
}
func (a *editApp) send(t *testing.T, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.input.Write(append(b, '\n')); err != nil {
		t.Fatal(err)
	}
}
func (a *editApp) document(t *testing.T, path any, buf any, rev float64) session.Source {
	t.Helper()
	doc := map[string]any{"type": "doc", "path": path, "name": "文書.md", "text": "本文\n", "rev": rev}
	if buf != nil {
		doc["buf"] = buf
	}
	a.send(t, doc)
	var src session.Source
	wait(t, func() bool { src, _ = a.state.Source(nil); return src.Render != 0 && src.Rev == rev && !src.Stale })
	return src
}
func (a *editApp) edits() []map[string]any {
	edits := []map[string]any{}
	for _, m := range a.out.messages() {
		if m["type"] == "edit" {
			edits = append(edits, m)
		}
	}
	return edits
}
func (a *editApp) nextEdit(t *testing.T, number int) map[string]any {
	t.Helper()
	wait(t, func() bool { return len(a.edits()) >= number })
	return a.edits()[number-1]
}

type editResponse struct {
	status int
	body   string
	err    error
}

func (a *editApp) post(body string) <-chan editResponse {
	ch := make(chan editResponse, 1)
	go func() {
		req, _ := http.NewRequest("POST", a.server.URL+"_/edit", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", fmt.Sprintf("http://127.0.0.1:%d", a.server.Port))
		r, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
		if err != nil {
			ch <- editResponse{err: err}
			return
		}
		defer r.Body.Close()
		b, err := io.ReadAll(r.Body)
		ch <- editResponse{status: r.StatusCode, body: string(b), err: err}
	}()
	return ch
}
func checkEditResponse(t *testing.T, ch <-chan editResponse, status int, code, message string) {
	t.Helper()
	select {
	case r := <-ch:
		if r.err != nil || r.status != status {
			t.Fatal(r)
		}
		if code != "" && !strings.Contains(r.body, `"code":"`+code+`"`) || message != "" && !strings.Contains(r.body, message) {
			t.Fatal(r.body)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("編集の HTTP 応答が届かない")
	}
}
func rangeRequest(src session.Source) string {
	return fmt.Sprintf(`{"render":%d,"range":"1:1-1:6","text":"変更"}`, src.Render)
}

func TestEditNegotiation(t *testing.T) {
	for _, suffix := range []string{"", `,"edit":true`, `,"edit":"1"`, `,"edit":2`, `,"edit":null`, `,"edit":{}`, `,"edit":1`} {
		t.Run(suffix, func(t *testing.T) {
			a := startEditApp(t, `{"type":"hello","protocol":1`+suffix+`}`, 0)
			ready := a.out.messages()[0]
			enabled := suffix == `,"edit":1`
			if enabled != (ready["edit"] == float64(1)) {
				t.Fatal(ready)
			}
			if !enabled {
				a.out.Lock()
				first := strings.SplitN(a.out.data.String(), "\n", 2)[0]
				a.out.Unlock()
				want := fmt.Sprintf(`{"type":"ready","protocol":1,"version":"試験版","url":%q}`, a.server.URL)
				if first != want {
					t.Fatalf("古い相手への ready が変わった: %s", first)
				}
				checkEditResponse(t, a.post(`{}`), 405, "", "")
			}
			r, err := http.Get(a.server.URL)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			if enabled && !strings.Contains(string(b), `"edit":{"scope":"current","undo":false}`) || !enabled && strings.Contains(string(b), `"edit":{`) {
				t.Fatal("ページの編集設定が違う", string(b))
			}
			if !enabled {
				n := len(a.out.messages())
				a.send(t, map[string]any{"type": "edit_result", "id": 1, "ok": true})
				wait(t, func() bool { return len(a.out.messages()) > n })
				if a.out.messages()[n]["message"] != "知らない行の種類です。type を確かめてください" {
					t.Fatal(a.out.messages())
				}
			}
		})
	}
}

func TestDocumentBufferValidation(t *testing.T) {
	a := startEditApp(t, `{"type":"hello","protocol":1,"edit":1}`, 0)
	for i, buf := range []any{nil, 1, 9007199254740991} {
		src := a.document(t, nil, buf, float64(i+1))
		if buf == nil && src.Buf != nil || buf != nil && src.Buf != float64(buf.(int)) {
			t.Fatal(src.Buf, buf)
		}
		if buf == nil {
			checkEditResponse(t, a.post(rangeRequest(src)), 422, "unsupported", "")
		}
	}
	for _, buf := range []any{0, -1, 1.5, 9007199254740992, "1", true, nil} {
		n := len(a.out.messages())
		a.send(t, map[string]any{"type": "doc", "path": nil, "name": "文書", "text": "本文\n", "rev": 9, "buf": buf})
		wait(t, func() bool { return len(a.out.messages()) > n })
		if got := a.out.messages()[n]; got["type"] != "error" || got["message"] != "doc の path・name・text・rev・buf の型または値が正しくありません" {
			t.Fatal(got)
		}
	}
}

func TestEditRoundTrip(t *testing.T) {
	for _, named := range []bool{false, true} {
		t.Run(fmt.Sprint(named), func(t *testing.T) {
			a := startEditApp(t, `{"type":"hello","protocol":1,"edit":1}`, 0)
			var path any
			if named {
				path = filepath.Join(t.TempDir(), "missing.md")
			}
			src := a.document(t, path, 7, 1.25)
			request := a.post(rangeRequest(src))
			m := a.nextEdit(t, 1)
			want := map[string]any{"type": "edit", "id": float64(1), "path": path, "buf": float64(7), "rev": 1.25, "edits": []any{map[string]any{"start": float64(0), "end": float64(1), "lines": []any{"変更"}}}}
			if !reflect.DeepEqual(m, want) {
				t.Fatal(m, want)
			}
			a.send(t, map[string]any{"type": "edit_result", "id": m["id"], "ok": true})
			checkEditResponse(t, request, 200, "", `"ok":true`)
			for i, message := range []any{"拒否しました\n後ろは捨てる", strings.Repeat("あ", 201) + "\r後ろ", 123, "\n後ろ"} {
				request = a.post(rangeRequest(src))
				m = a.nextEdit(t, i+2)
				a.send(t, map[string]any{"type": "edit_result", "id": m["id"], "ok": false, "message": message})
				want := []string{"拒否しました", strings.Repeat("あ", 200), "Neovim が編集を受け付けませんでした。", "Neovim が編集を受け付けませんでした。"}[i]
				checkEditResponse(t, request, 409, "rejected", want)
			}
		})
	}
}

func TestEditTimeoutPauseAndClose(t *testing.T) {
	for _, action := range []string{"timeout", "pause", "close"} {
		t.Run(action, func(t *testing.T) {
			timeout := time.Duration(0)
			if action == "timeout" {
				timeout = 40 * time.Millisecond
			}
			a := startEditApp(t, `{"type":"hello","protocol":1,"edit":1}`, timeout)
			src := a.document(t, nil, 1, 1)
			request := a.post(rangeRequest(src))
			m := a.nextEdit(t, 1)
			switch action {
			case "timeout":
				checkEditResponse(t, request, 504, "timeout", "Neovim から応答がありません。")
			case "pause":
				a.send(t, map[string]any{"type": "pause"})
				checkEditResponse(t, request, 409, "paused", "プレビューは休止中です。Neovim で再開してください。")
				checkEditResponse(t, a.post(rangeRequest(src)), 409, "paused", "")
			case "close":
				_ = a.input.Close()
				checkEditResponse(t, request, 503, "closed", "プレビューを終了しています。")
				a.finish(t)
				return
			}
			// 遅い応答・重複した応答は、次の要求を完了させない。
			a.send(t, map[string]any{"type": "edit_result", "id": m["id"], "ok": true})
			if action == "pause" {
				a.send(t, map[string]any{"type": "resume"})
			}
			request = a.post(rangeRequest(src))
			next := a.nextEdit(t, 2)
			if next["id"].(float64) <= m["id"].(float64) {
				t.Fatal(next)
			}
			a.send(t, map[string]any{"type": "edit_result", "id": next["id"], "ok": true})
			checkEditResponse(t, request, 200, "", "")
		})
	}
}

func TestInvalidEditResultsAndSerialization(t *testing.T) {
	a := startEditApp(t, `{"type":"hello","protocol":1,"edit":1}`, 0)
	src := a.document(t, nil, 1, 1)
	request := a.post(rangeRequest(src))
	first := a.nextEdit(t, 1)
	for _, value := range []map[string]any{{"id": "1", "ok": true}, {"id": 1, "ok": 1}, {"id": 0, "ok": true}, {"id": 9007199254740992, "ok": false}} {
		n := len(a.out.messages())
		value["type"] = "edit_result"
		a.send(t, value)
		wait(t, func() bool { return len(a.out.messages()) > n })
		if a.out.messages()[n]["type"] != "error" {
			t.Fatal(a.out.messages())
		}
	}
	n := len(a.out.messages())
	a.send(t, map[string]any{"type": "edit_result", "id": 99, "ok": true})
	// 次の行の処理まで進め、知らない番号の応答が何も出さないことを確かめる。
	a.send(t, map[string]any{"type": "hello", "protocol": 1})
	wait(t, func() bool { return len(a.out.messages()) > n })
	if len(a.out.messages()) != n+1 || a.out.messages()[n]["message"] != "hello は最初に1回だけ送ってください" {
		t.Fatal(a.out.messages())
	}
	second := a.post(rangeRequest(src))
	select {
	case r := <-second:
		t.Fatal("前の要求が終わる前に次を終えた", r)
	case <-time.After(30 * time.Millisecond):
	}
	if len(a.edits()) != 1 {
		t.Fatal("編集を直列にしない", a.edits())
	}
	a.send(t, map[string]any{"type": "edit_result", "id": first["id"], "ok": true})
	checkEditResponse(t, request, 200, "", "")
	next := a.nextEdit(t, 2)
	if next["id"] != float64(2) {
		t.Fatal(next)
	}
	a.send(t, map[string]any{"type": "edit_result", "id": first["id"], "ok": true})
	a.send(t, map[string]any{"type": "edit_result", "id": next["id"], "ok": true})
	checkEditResponse(t, second, 200, "", "")
}

func TestEditBridgeLimitsAndCancellation(t *testing.T) {
	for _, mode := range []string{"large", "busy", "cancelled", "closed", "paused"} {
		t.Run(mode, func(t *testing.T) {
			out := newOutput(io.Discard)
			defer out.finish()
			b := newEditBridge(out, 0)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			e := edit.BufferEdit{Buf: 1, Rev: 1, Lines: []string{"本文"}}
			status, code := 422, mode
			switch mode {
			case "large":
				e.Lines = []string{strings.Repeat("a", maxEditBytes)}
				status = 413
			case "busy":
				out.finish()
				status = 503
			case "closed":
				b.close()
				status = 503
			case "paused":
				b.pause(true)
				status = 409
			case "cancelled":
				cancel()
			}
			if err := b.send(ctx, e); err == nil || err.Status != status || err.Code != code {
				t.Fatal(err)
			}
			if len(b.waiting) != 0 {
				t.Fatal("応答待ちが残った")
			}
		})
	}
}
