package stdio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"github.com/ryo-aoki-pc/md-tools/internal/server"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type renderer struct{}

func (renderer) Render(ctx context.Context, text string, _ pipeline.Context) (pipeline.Result, error) {
	if text == "重い" {
		<-ctx.Done()
		return pipeline.Result{}, ctx.Err()
	}
	return pipeline.Result{HTML: text}, nil
}

type outputBuffer struct {
	sync.Mutex
	data bytes.Buffer
}

func (b *outputBuffer) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	return b.data.Write(p)
}
func (b *outputBuffer) messages() []map[string]any {
	b.Lock()
	defer b.Unlock()
	out := []map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(b.data.String()), "\n") {
		if line == "" {
			continue
		}
		var v map[string]any
		if json.Unmarshal([]byte(line), &v) == nil {
			out = append(out, v)
		}
	}
	return out
}
func wait(t *testing.T, p func() bool) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if p() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("標準入出力の応答が時間内に届かない")
}
func TestReadLines(t *testing.T) {
	seen := []string{}
	oversize := 0
	e := ReadLines(strings.NewReader("日本語\n1234567890\nok\nlast"), func(b []byte) bool { seen = append(seen, string(b)); return true }, func() bool { oversize++; return true }, 9)
	if e != nil || fmt.Sprint(seen) != "[日本語 ok last]" || oversize != 1 {
		t.Fatal(seen, oversize, e)
	}
	seen = nil
	e = ReadLines(strings.NewReader("\n\n"), func(b []byte) bool { seen = append(seen, string(b)); return false }, func() bool { return true }, 9)
	if e != nil || len(seen) != 1 {
		t.Fatal(seen, e)
	}
}
func TestFatal(t *testing.T) {
	for _, line := range []string{`{"type":"doc"}`, `{"type":"hello","protocol":2}`, `{"type":"hello","protocol":"1"}`, `{broken`, `null`, `[]`, string([]byte{255})} {
		var out bytes.Buffer
		code := Run(context.Background(), Options{Stdin: strings.NewReader(line + "\n"), Stdout: &out, Renderer: renderer{}})
		var v map[string]any
		if code != 2 || json.Unmarshal(out.Bytes(), &v) != nil || v["fatal"] != true || v["type"] != "error" {
			t.Fatal(line, code, out.String())
		}
	}
}
func TestProtocol(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.md")
	_ = os.WriteFile(path, nil, 0644)
	input, writer := io.Pipe()
	var out outputBuffer
	started := make(chan struct {
		server *server.Server
		state  *session.Session
	}, 1)
	result := make(chan int, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	go func() {
		result <- Run(ctx, Options{Stdin: input, Stdout: &out, Version: "試験版", Renderer: renderer{}, OnStart: func(s *server.Server, state *session.Session) error {
			started <- struct {
				server *server.Server
				state  *session.Session
			}{s, state}
			return nil
		}})
	}()
	t.Cleanup(func() { _ = writer.Close(); cancel() })
	send := func(v any) {
		b, _ := json.Marshal(v)
		if _, e := writer.Write(append(b, '\n')); e != nil {
			t.Fatal(e)
		}
	}
	_, _ = writer.Write([]byte(`{"type":"hel`))
	_, _ = writer.Write([]byte("lo\",\"protocol\":1,\"appearance\":{\"theme\":\"dark\",\"width\":\"fixed\"}}\n"))
	var app struct {
		server *server.Server
		state  *session.Session
	}
	select {
	case app = <-started:
	case <-ctx.Done():
		t.Fatal("helloで起動しない")
	}
	wait(t, func() bool { return len(out.messages()) >= 2 })
	messages := out.messages()
	if messages[0]["type"] != "ready" || messages[0]["version"] != "試験版" || messages[1]["type"] != "clients" || messages[1]["count"] != float64(0) {
		t.Fatal(messages)
	}
	r, e := http.Get(app.server.URL)
	if e != nil {
		t.Fatal(e)
	}
	body, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if !strings.Contains(string(body), `"theme":"dark"`) || !strings.Contains(string(body), `"jump":true`) {
		t.Fatal(string(body))
	}
	send(map[string]any{"type": "doc", "path": path, "name": "文書", "text": "重い", "rev": 1})
	send(map[string]any{"type": "pause"})
	send(map[string]any{"type": "doc", "path": nil, "name": "捨てる", "text": "捨てる", "rev": 2})
	send(map[string]any{"type": "resume"})
	send(map[string]any{"type": "doc", "path": path, "name": "文書", "text": "再開", "rev": 3})
	wait(t, func() bool { d, _ := app.state.Last(); return d != nil && d["rev"] == float64(3) })
	send(map[string]any{"type": "scroll", "line": 2, "ratio": 0.5, "lines": 10})
	wait(t, func() bool { _, scroll := app.state.Last(); return scroll["line"] == float64(2) })
	_, scroll := app.state.Last()
	if scroll["line"] != float64(2) {
		t.Fatal(scroll)
	}
	req, _ := http.NewRequest("POST", app.server.URL+"_/jump", strings.NewReader(`{"line":7}`))
	req.Header.Set("Origin", fmt.Sprintf("http://127.0.0.1:%d", app.server.Port))
	req.Header.Set("Content-Type", "application/json")
	r, e = http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	_ = r.Body.Close()
	if r.StatusCode != 204 {
		t.Fatal(r.StatusCode)
	}
	wait(t, func() bool {
		for _, v := range out.messages() {
			if v["type"] == "jump" && v["line"] == float64(7) && v["path"] == path {
				return true
			}
		}
		return false
	})
	_ = writer.Close()
	if code := <-result; code != 0 {
		t.Fatal(code)
	}
	if r, e := http.Get(app.server.URL); e == nil {
		_ = r.Body.Close()
		t.Fatal("入力終了後も待ち受けた")
	}
}
func TestInvalidRowsRecover(t *testing.T) {
	lines := []string{`{"type":"hello","protocol":1}`, `{broken`, `null`, `[]`, `{"type":"unknown"}`, `{"type":"hello","protocol":1}`, `{"type":"doc","path":"relative.md","name":"文書","text":"本文","rev":1}`, `{"type":"scroll","line":0,"ratio":0,"lines":1}`, `{"type":"scroll","line":1,"ratio":1.1,"lines":1}`, string([]byte{255}), `{"type":"doc","path":null,"name":"復帰","text":"本文","rev":1}`}
	var out bytes.Buffer
	code := Run(context.Background(), Options{Stdin: strings.NewReader(strings.Join(lines, "\n")), Stdout: &out, Renderer: renderer{}})
	if code != 0 {
		t.Fatal(code)
	}
	errors := 0
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var v map[string]any
		if json.Unmarshal([]byte(line), &v) != nil {
			t.Fatal(line)
		}
		if v["type"] == "error" {
			errors++
			if v["fatal"] != nil {
				t.Fatal(v)
			}
		}
	}
	if errors != 9 {
		t.Fatal(errors, out.String())
	}
}
func TestOversizeLine(t *testing.T) {
	oversize := 0
	received := 0
	large := io.MultiReader(io.LimitReader(repeatedByte('x'), MaxLineBytes+1), strings.NewReader("\nok\n"))
	if e := ReadLines(large, func(b []byte) bool {
		received++
		if string(b) != "ok" {
			t.Fatal("大きい行を渡した")
		}
		return true
	}, func() bool { oversize++; return true }, MaxLineBytes); e != nil || oversize != 1 || received != 1 {
		t.Fatal(e, oversize, received)
	}
}

type repeatedByte byte

func (b repeatedByte) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(b)
	}
	return len(p), nil
}
func TestEmptyInput(t *testing.T) {
	var out bytes.Buffer
	if code := Run(context.Background(), Options{Stdin: strings.NewReader(""), Stdout: &out, Renderer: renderer{}}); code != 0 || out.Len() != 0 {
		t.Fatal(code, out.String())
	}
}

type blockedWriter struct {
	closed chan struct{}
	once   sync.Once
}

func (w *blockedWriter) Write([]byte) (int, error) { <-w.closed; return 0, io.ErrClosedPipe }
func (w *blockedWriter) Close() error              { w.once.Do(func() { close(w.closed) }); return nil }
func TestBlockedOutput(t *testing.T) {
	out := &blockedWriter{closed: make(chan struct{})}
	text := `{"type":"hello","protocol":1}` + "\n" + strings.Repeat(`{"type":"unknown"}`+"\n", 25000)
	started := time.Now()
	code := Run(context.Background(), Options{Stdin: strings.NewReader(text), Stdout: out, Renderer: renderer{}})
	if code != 0 || time.Since(started) > 5*time.Second {
		t.Fatal(code)
	}
	select {
	case <-out.closed:
	default:
		t.Fatal("詰まった出力を閉じない")
	}
}
func TestExactErrors(t *testing.T) {
	for _, item := range []struct{ input, want string }{{`{"type":"doc"}`, "最初に hello を送ってください"}, {`{"type":"hello","protocol":2}`, "対応していない取り決めの版です。protocol は1にしてください"}, {`null`, "JSON のオブジェクトを送ってください"}, {`{`, "JSON の行を読めません"}} {
		var out bytes.Buffer
		_ = Run(context.Background(), Options{Stdin: strings.NewReader(item.input), Stdout: &out, Renderer: renderer{}})
		want := `{"type":"error","fatal":true,"message":"` + item.want + `"}` + "\n"
		if out.String() != want {
			t.Fatalf("取り決めの応答が異なる: %q", out.String())
		}
	}
}

func TestOutputEscaping(t *testing.T) {
	var buffer bytes.Buffer
	o := newOutput(&buffer)
	o.send(jumpMessage{"jump", "<>&\u2028\\u2028", 7})
	o.finish()
	want := "{\"type\":\"jump\",\"path\":\"<>&\u2028\\\\u2028\",\"line\":7}\n"
	if buffer.String() != want {
		t.Fatalf("JSON 行のエスケープが異なる: %q", buffer.String())
	}
}
