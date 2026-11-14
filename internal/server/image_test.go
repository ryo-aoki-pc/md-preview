package server

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/internal/edit"
)

func TestImageGuards(t *testing.T) {
	calls := 0
	s, _, _ := fixture(t, func(o *Options) {
		o.Edit = &testEditor{image: func(ctx context.Context, doc *string, render uint64, body []byte) (string, *edit.Error) {
			calls++
			if doc == nil || *doc != "docs/main.md" || render != 12 || !bytes.Equal(body, []byte{0, 255, 13, 10}) {
				t.Error(doc, render, body)
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Error("保存の期限がない")
			}
			return "images/img-test.png", nil
		}}
	})
	origin := fmt.Sprintf("http://127.0.0.1:%d", s.Port)
	for _, c := range []struct {
		name   string
		header map[string]string
		render string
		status int
	}{
		{"正常", nil, "12", 200},
		{"同じサイト", map[string]string{"Sec-Fetch-Site": "same-origin"}, "12", 200},
		{"Origin 無し", map[string]string{"Origin": ""}, "12", 403},
		{"別の Origin", map[string]string{"Origin": "http://example.invalid"}, "12", 403},
		{"別サイト", map[string]string{"Sec-Fetch-Site": "cross-site"}, "12", 403},
		{"none", map[string]string{"Sec-Fetch-Site": "none"}, "12", 403},
		{"Host", map[string]string{"Host": "example.invalid"}, "12", 403},
		{"種類", map[string]string{"Content-Type": "image/jpeg"}, "12", 415},
		{"種類なし", map[string]string{"Content-Type": ""}, "12", 415},
		{"描画なし", nil, "", 400},
		{"ゼロ", nil, "0", 400},
		{"負数", nil, "-1", 400},
		{"小数", nil, "1.5", 400},
	} {
		t.Run(c.name, func(t *testing.T) {
			header := map[string]string{"Origin": origin, "Content-Type": "image/png"}
			for k, v := range c.header {
				header[k] = v
			}
			r := request(t, s, "POST", "_/edit/image?doc=docs%2Fmain.md&render="+c.render, string([]byte{0, 255, 13, 10}), header)
			if r.status != c.status {
				t.Fatal(r)
			}
			protected(t, r)
			if r.header.Get("Cache-Control") != "no-store" {
				t.Fatal(r.header)
			}
			if c.status == 200 && r.body != `"images/img-test.png"` {
				t.Fatal(r)
			}
		})
	}
	if calls != 2 {
		t.Fatal("検査を通さず保存した", calls)
	}
	for _, extra := range []string{"Content-Length: 8388609\r\n", "Transfer-Encoding: chunked\r\n"} {
		body := ""
		if strings.HasPrefix(extra, "Transfer") {
			body = fmt.Sprintf("%x\r\n%s\r\n0\r\n\r\n", edit.MaxImageBytes+1, strings.Repeat("x", edit.MaxImageBytes+1))
		}
		r := raw(t, s, fmt.Sprintf("POST /%s/_/edit/image?render=12 HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nOrigin: %s\r\nContent-Type: image/png\r\n%s\r\n%s", s.Token, s.Port, origin, extra, body), "POST")
		if r.status != 413 {
			t.Fatal(r)
		}
	}
	// 8 MiB ちょうどは口の上限で断らず、PNG の検査へ渡す。
	s.opts.Edit = &testEditor{image: func(_ context.Context, _ *string, _ uint64, body []byte) (string, *edit.Error) {
		if len(body) != edit.MaxImageBytes {
			t.Error(len(body))
		}
		return "", &edit.Error{Status: 422, Code: "image", Message: "PNG ではありません。"}
	}}
	r := request(t, s, "POST", "_/edit/image?render=1", strings.Repeat("x", edit.MaxImageBytes), map[string]string{"Origin": origin, "Content-Type": "image/png"})
	if r.status != 422 {
		t.Fatal(r.status)
	}
	none, _, _ := fixture(t, nil)
	if r := request(t, none, "POST", "_/edit/image?render=1", "", nil); r.status != 405 {
		t.Fatal(r)
	}
	r = raw(t, s, fmt.Sprintf("POST /wrong/_/edit/image?render=1 HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nContent-Length: 0\r\n\r\n", s.Port), "POST")
	if r.status != 405 {
		t.Fatal(r)
	}
}
