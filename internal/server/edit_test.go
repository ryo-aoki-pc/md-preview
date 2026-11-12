package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ryo-aoki-pc/md-tools/internal/edit"
)

type testEditor struct {
	apply  func(context.Context, *string, map[string]any) (any, *edit.Error)
	tables func(context.Context) (any, *edit.Error)
}

func (*testEditor) Config() map[string]any { return map[string]any{"scope": "all", "undo": true} }
func (e *testEditor) Tables(ctx context.Context, doc *string, render uint64) (any, *edit.Error) {
	if e.tables != nil {
		return e.tables(ctx)
	}
	return map[string]any{"render": render, "doc": doc}, nil
}
func (*testEditor) Text(_ context.Context, doc *string, render uint64, tag string, r edit.Range) (any, *edit.Error) {
	return map[string]any{"render": render, "range": r, "tag": tag, "doc": doc}, nil
}
func (e *testEditor) Apply(ctx context.Context, doc *string, body map[string]any) (any, *edit.Error) {
	if e.apply != nil {
		return e.apply(ctx, doc, body)
	}
	return map[string]any{"ok": true, "doc": doc}, nil
}

func TestEditGuards(t *testing.T) {
	s, _, _ := fixture(t, func(o *Options) { o.Edit = &testEditor{} })
	origin := fmt.Sprintf("http://127.0.0.1:%d", s.Port)
	for _, c := range []struct {
		name, body string
		header     map[string]string
		status     int
	}{
		{"正常", "{}", nil, 200},
		{"同じサイト", "{}", map[string]string{"Sec-Fetch-Site": "same-origin"}, 200},
		{"Origin 無し", "{}", map[string]string{"Origin": ""}, 403},
		{"null", "{}", map[string]string{"Origin": "null"}, 403},
		{"別の Host", "{}", map[string]string{"Origin": fmt.Sprintf("http://localhost:%d", s.Port)}, 403},
		{"別サイト", "{}", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"none", "{}", map[string]string{"Sec-Fetch-Site": "none"}, 403},
		{"JSON 以外", "{}", map[string]string{"Content-Type": "text/plain"}, 415},
		{"JSON の型", "[]", nil, 400},
		{"JSON の null", "null", nil, 400},
		{"余分な JSON", "{} {}", nil, 400},
		{"UTF-8", "{\"text\":\"\xff\"}", nil, 400},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := map[string]string{"Origin": origin, "Content-Type": "application/json"}
			for k, v := range c.header {
				h[k] = v
			}
			r := request(t, s, "POST", "_/edit?doc=main.md", c.body, h)
			if r.status != c.status {
				t.Fatal(r)
			}
			protected(t, r)
			if r.header.Get("Cache-Control") != "no-store" || !strings.HasPrefix(r.header.Get("Content-Type"), "application/json") {
				t.Fatal(r.header)
			}
			var value map[string]any
			if err := json.Unmarshal([]byte(r.body), &value); err != nil {
				t.Fatal(err)
			}
			if c.status != 200 && value["error"] == nil {
				t.Fatal(value)
			}
		})
	}
	for _, extra := range []string{"Content-Length: 2097153\r\n", "Transfer-Encoding: chunked\r\n"} {
		body := ""
		if strings.HasPrefix(extra, "Transfer") {
			body = fmt.Sprintf("%x\r\n%s\r\n0\r\n\r\n", 2097153, strings.Repeat(" ", 2097153))
		}
		r := raw(t, s, fmt.Sprintf("POST /%s/_/edit HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nOrigin: %s\r\nContent-Type: application/json\r\n%s\r\n%s", s.Token, s.Port, origin, extra, body), "POST")
		if r.status != 413 {
			t.Fatal(r)
		}
	}
	r := request(t, s, "PUT", "_/edit", "", nil)
	if r.status != 405 || r.header.Get("Allow") != "GET, HEAD, POST" {
		t.Fatal(r)
	}
	none, _, _ := fixture(t, nil)
	r = request(t, none, "POST", "_/edit", "{}", nil)
	if r.status != 405 || r.header.Get("Allow") != "GET, HEAD" {
		t.Fatal(r)
	}
	// 編集を有効にしていなければ、GET の経路も無い（リポジトリのファイルの解決へ進み、無いので 404）。
	for _, route := range []string{"_/edit/tables?render=1", "_/edit/source?render=1&range=1:1-1:1"} {
		if r = request(t, none, "GET", route, "", nil); r.status != 404 || strings.Contains(r.body, "error") {
			t.Fatal(route, r)
		}
	}
}
func TestEditGetParametersAndErrors(t *testing.T) {
	s, _, _ := fixture(t, func(o *Options) { o.Edit = &testEditor{} })
	for _, render := range []string{"", "0", "-1", "+1", "1.5", "1x", "18446744073709551616"} {
		r := request(t, s, "GET", "_/edit/tables?render="+render, "", nil)
		if r.status != 400 {
			t.Fatal(render, r)
		}
	}
	for _, r := range []string{"", "1:0-1:1", "1:1-0:0", "2:1-1:1", "a"} {
		resp := request(t, s, "GET", "_/edit/source?render=1&tag=p&range="+r, "", nil)
		if resp.status != 400 {
			t.Fatal(r, resp)
		}
	}
	for _, tag := range []string{"", "P", "p-", "p%20", "%3Cp%3E"} {
		r := request(t, s, "GET", "_/edit/source?render=1&tag="+tag+"&range=1:1-1:5", "", nil)
		if r.status != 400 || !strings.Contains(r.body, `"code":"invalid"`) {
			t.Fatal(tag, r)
		}
	}
	for _, route := range []string{"_/edit/tables?render=12&doc=main.md", "_/edit/source?render=12&tag=p&doc=main.md&range=1:1-2:0"} {
		r := request(t, s, "GET", route, "", nil)
		if r.status != 200 || !strings.Contains(r.body, `"render":12`) || !strings.Contains(r.body, `"doc":"main.md"`) {
			t.Fatal(r)
		}
		if strings.Contains(route, "/source") && !strings.Contains(r.body, `"tag":"p"`) {
			t.Fatal("要素名を渡していない", r)
		}
	}
	for _, status := range []int{404, 409, 422, 500} {
		s.opts.Edit = &testEditor{apply: func(context.Context, *string, map[string]any) (any, *edit.Error) {
			return nil, &edit.Error{Status: status, Code: "試験", Message: "試験の誤り"}
		}}
		r := request(t, s, "POST", "_/edit", "{}", map[string]string{"Origin": fmt.Sprintf("http://127.0.0.1:%d", s.Port), "Content-Type": "application/json"})
		if r.status != status || !strings.Contains(r.body, "試験の誤り") {
			t.Fatal(r)
		}
	}
}
func TestEditContexts(t *testing.T) {
	started, release, completed := make(chan context.Context, 1), make(chan struct{}), make(chan error, 1)
	editor := &testEditor{apply: func(ctx context.Context, _ *string, _ map[string]any) (any, *edit.Error) {
		started <- ctx
		<-release
		completed <- ctx.Err()
		return map[string]any{"ok": true}, nil
	}}
	s, _, _ := fixture(t, func(o *Options) { o.Edit = editor })
	conn, err := net.Dial("tcp4", fmt.Sprintf("127.0.0.1:%d", s.Port))
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(conn, "POST /%s/_/edit HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nOrigin: http://127.0.0.1:%d\r\nContent-Type: application/json\r\nContent-Length: 2\r\n\r\n{}", s.Token, s.Port, s.Port)
	if err != nil {
		t.Fatal(err)
	}
	ctx := <-started
	_ = conn.Close()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("書き込みに期限が無い")
	}
	close(release)
	if err := <-completed; err != nil {
		t.Fatal("切断で書き込みを中断した", err)
	}
	cancelled := make(chan struct{})
	editor.tables = func(ctx context.Context) (any, *edit.Error) {
		close(cancelled)
		<-ctx.Done()
		return nil, &edit.Error{Status: 422, Code: "cancelled", Message: "取り消しました。"}
	}
	conn, err = net.Dial("tcp4", fmt.Sprintf("127.0.0.1:%d", s.Port))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(conn, fmt.Sprintf("GET /%s/_/edit/tables?render=1 HTTP/1.1\r\nHost: 127.0.0.1:%d\r\n\r\n", s.Token, s.Port))
	<-cancelled
	_ = conn.Close()
	// 終了が GET の取り消し待ちで止まらないことを確かめる。
	done := make(chan struct{})
	go func() { _ = s.Close("exit"); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("GET の切断が伝わらない")
	}
}
