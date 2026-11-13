package server

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ryo-aoki-pc/md-tools/internal/edit"
	"github.com/ryo-aoki-pc/md-tools/internal/vscode"
)

type testReviewer struct {
	testEditor
	review func(*string, uint64, []edit.ReviewTarget) (edit.ReviewDocument, []edit.ReviewItem, *edit.Error)
}

func (v *testReviewer) Review(_ context.Context, doc *string, render uint64, targets []edit.ReviewTarget) (edit.ReviewDocument, []edit.ReviewItem, *edit.Error) {
	if v.review != nil {
		return v.review(doc, render, targets)
	}
	items := make([]edit.ReviewItem, len(targets))
	for i := range items {
		items[i] = edit.ReviewItem{Kind: "段落", StartLine: 3, EndLine: 4, Excerpt: "サーバーの原文"}
	}
	return edit.ReviewDocument{Root: "/private-root", RelPath: "docs/main.md", Render: render, Hash: strings.Repeat("a", 64)}, items, nil
}

const reviewBody = `{"render":12,"comments":[{"target":{"kind":"block","tag":"p","range":"3:1-4:10"},"selection":"選択😀","text":"  修正してください  "}]}`

func reviewHeaders(s *Server) map[string]string {
	return map[string]string{"Origin": fmt.Sprintf("http://127.0.0.1:%d", s.Port), "Content-Type": "application/json"}
}

func TestReviewPrepare(t *testing.T) {
	called := false
	v := &testReviewer{review: func(doc *string, render uint64, targets []edit.ReviewTarget) (edit.ReviewDocument, []edit.ReviewItem, *edit.Error) {
		called = true
		if doc == nil || *doc != "docs/main.md" || render != 12 || !reflect.DeepEqual(targets, []edit.ReviewTarget{{Kind: "block", Tag: "p", Range: "3:1-4:10"}}) {
			t.Error(doc, render, targets)
		}
		return edit.ReviewDocument{Root: "/private-root", RelPath: "docs/main.md", Render: render, Hash: strings.Repeat("a", 64)}, []edit.ReviewItem{{Kind: "段落", StartLine: 3, EndLine: 4, Excerpt: "サーバーの原文"}}, nil
	}}
	s, _, _ := fixture(t, func(o *Options) { o.Review = v })
	r := request(t, s, "POST", "_/review/prepare?doc=docs/main.md", reviewBody, reviewHeaders(s))
	if r.status != 200 || !called {
		t.Fatal(r, called)
	}
	protected(t, r)
	if r.header.Get("Cache-Control") != "no-store" || !strings.HasPrefix(r.header.Get("Content-Type"), "application/json") {
		t.Fatal(r.header)
	}
	var result struct {
		ID       string `json:"id"`
		Prompt   string `json:"prompt"`
		Comments int    `json:"comments"`
		Bytes    int    `json:"bytes"`
	}
	if e := json.Unmarshal([]byte(r.body), &result); e != nil {
		t.Fatal(e)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(result.ID) || result.Comments != 1 || result.Bytes != len(result.Prompt) || !strings.HasPrefix(result.Prompt, "/plan\n") || !strings.Contains(result.Prompt, "サーバーの原文") || !strings.Contains(result.Prompt, "選択😀") || !strings.Contains(result.Prompt, "\n修正してください\n") {
		t.Fatal(result)
	}
	for _, forbidden := range []string{"/private-root", s.Token, s.URL, "http://"} {
		if strings.Contains(result.Prompt, forbidden) {
			t.Fatal("依頼文にサーバーの情報がある", forbidden)
		}
	}
	entry, ok := s.reviews.get(result.ID, time.Now())
	sum := sha256.Sum256([]byte(result.Prompt))
	if !ok || entry.Doc.Current || entry.Doc.Path != "docs/main.md" || entry.Document.Root != "/private-root" || entry.Document.RelPath != "docs/main.md" || entry.Document.Hash != strings.Repeat("a", 64) || entry.Document.Render != 12 || entry.PromptHash != hex.EncodeToString(sum[:]) || entry.Launched {
		t.Fatal(entry, ok)
	}
}

func TestReviewModes(t *testing.T) {
	s, _, _ := fixture(t, func(o *Options) { o.Review = &testReviewer{} })
	plan := "次のレビューのコメントに沿って、`docs/main.md` を直す計画を立ててください。\nまず計画だけを示し、承認されるまでファイルを変えないでください。\n\n"
	for _, tc := range []struct {
		name, mode, prefix string
		status             int
	}{
		{"省略", "", "/plan\n" + plan, 200},
		{"計画", `"plan"`, "/plan\n" + plan, 200},
		{"日本語で計画", `"plan-text"`, plan, 200},
		{"直接直す", `"direct"`, "次のレビューのコメントに沿って、`docs/main.md` を直してください。\n計画を示して承認を待つ必要はありません。\n\n", 200},
		{"知らない値", `"other"`, "", 400},
		{"空の値", `""`, "", 400},
		{"大文字", `"PLAN"`, "", 400},
		{"数値", `1`, "", 400},
		{"真偽値", `true`, "", 400},
		{"null", `null`, "", 400},
		{"配列", `[]`, "", 400},
		{"オブジェクト", `{}`, "", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := reviewBody
			if tc.mode != "" {
				body = body[:len(body)-1] + `,"mode":` + tc.mode + `}`
			}
			r := request(t, s, "POST", "_/review/prepare", body, reviewHeaders(s))
			if r.status != tc.status {
				t.Fatal(r)
			}
			if tc.status == 400 {
				if !strings.Contains(r.body, `"code":"invalid"`) {
					t.Fatal(r)
				}
				return
			}
			var result struct{ ID, Prompt string }
			if e := json.Unmarshal([]byte(r.body), &result); e != nil || !strings.HasPrefix(result.Prompt, tc.prefix+"対象のファイル:") {
				t.Fatal(result, e)
			}
			entry, ok := s.reviews.get(result.ID, time.Now())
			sum := sha256.Sum256([]byte(result.Prompt))
			if !ok || entry.PromptHash != hex.EncodeToString(sum[:]) {
				t.Fatal("選んだ形の依頼文を保存していない", entry, ok)
			}
		})
	}
}

func TestReviewGuards(t *testing.T) {
	s, _, _ := fixture(t, func(o *Options) { o.Review = &testReviewer{} })
	for _, c := range []struct {
		name, body string
		header     map[string]string
		status     int
	}{
		{"正常", reviewBody, nil, 200},
		{"同じサイト", reviewBody, map[string]string{"Sec-Fetch-Site": "same-origin"}, 200},
		{"Origin 無し", reviewBody, map[string]string{"Origin": ""}, 403},
		{"null", reviewBody, map[string]string{"Origin": "null"}, 403},
		{"違う Host", reviewBody, map[string]string{"Origin": fmt.Sprintf("http://localhost:%d", s.Port)}, 403},
		{"別サイト", reviewBody, map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"none", reviewBody, map[string]string{"Sec-Fetch-Site": "none"}, 403},
		{"Host", reviewBody, map[string]string{"Host": "example.invalid"}, 403},
		{"JSON 以外", reviewBody, map[string]string{"Content-Type": "text/plain"}, 415},
		{"JSON の型", "[]", nil, 400},
		{"余分な JSON", reviewBody + " {}", nil, 400},
		{"UTF-8", "{\"text\":\"\xff\"}", nil, 400},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := reviewHeaders(s)
			for k, v := range c.header {
				h[k] = v
			}
			r := request(t, s, "POST", "_/review/prepare", c.body, h)
			if r.status != c.status || !strings.HasPrefix(r.header.Get("Content-Type"), "application/json") {
				t.Fatal(r)
			}
		})
	}
	for _, chunked := range []bool{false, true} {
		// 本文の上限（2 MiB）を 1 バイト超える。
		const size = 2*1024*1024 + 1
		extra, body := fmt.Sprintf("Content-Length: %d\r\n", size), ""
		if chunked {
			extra = "Transfer-Encoding: chunked\r\n"
			body = fmt.Sprintf("%x\r\n%s\r\n0\r\n\r\n", size, strings.Repeat(" ", size))
		}
		r := raw(t, s, fmt.Sprintf("POST /%s/_/review/prepare HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nOrigin: http://127.0.0.1:%d\r\nContent-Type: application/json\r\n%s\r\n%s", s.Token, s.Port, s.Port, extra, body), "POST")
		if r.status != 413 || !strings.Contains(r.body, `"code":"large"`) {
			t.Fatal(r)
		}
	}
	wrong := strings.Repeat("f", 32)
	if wrong == s.Token {
		wrong = strings.Repeat("e", 32)
	}
	r := raw(t, s, fmt.Sprintf("GET /%s/_/review/tables?render=1 HTTP/1.1\r\nHost: 127.0.0.1:%d\r\n\r\n", wrong, s.Port), "GET")
	if r.status != 404 || !strings.Contains(r.body, "コメントの経路がありません。") {
		t.Fatal(r)
	}
}

func TestReviewRoutes(t *testing.T) {
	s, _, _ := fixture(t, func(o *Options) { o.Review = &testReviewer{} })
	// 読み取りの口は、編集と同じ関数の結果（文書・描画・要素名・範囲）をそのまま返す。
	for route, want := range map[string]string{
		"_/review/tables?render=1&doc=main.md":                          `{"doc":"main.md","render":1}`,
		"_/review/source?render=1&tag=p&range=1:1-1:5":                  `{"doc":null,"range":"1:1-1:5","render":1,"tag":"p"}`,
		"_/review/source?render=1&tag=html&prevTag=p&prevRange=1:1-1:5": `{"next":null,"prev":{"tag":"p","range":"1:1-1:5"},"render":1}`,
	} {
		for _, method := range []string{"GET", "HEAD"} {
			r := request(t, s, method, route, "", nil)
			if r.status != 200 || method == "HEAD" && r.body != "" || method == "GET" && r.body != want {
				t.Fatal(route, r)
			}
		}
	}
	for _, route := range []string{"_/review/tables?render=0", "_/review/source?render=1&tag=p&range=2:1-1:1", "_/review/source?render=1&tag=html&prevTag=p", "_/review/source?render=1&tag=html&range=1:1-1:2"} {
		if r := request(t, s, "GET", route, "", nil); r.status != 400 {
			t.Fatal(route, r)
		}
	}
	// 編集と共有する形の検査でも、コメントの経路ではコメントの文で返す。
	for _, route := range []string{"_/review/source?render=1&tag=html&prevTag=p", "_/review/source?render=1&tag=bad!&range=1:1-1:1"} {
		if r := request(t, s, "GET", route, "", nil); r.status != 400 || !strings.Contains(r.body, "コメントの要求の形が正しくありません。") || strings.Contains(r.body, "編集の") {
			t.Fatal(route, r)
		}
	}
	for _, route := range []string{"_/edit", "_/edit/tables?render=1", "_/edit/source?render=1&tag=p&range=1:1-1:1"} {
		if r := request(t, s, "GET", route, "", nil); r.status != 404 || strings.Contains(r.body, "error") {
			t.Fatal("コメントだけで編集の経路を配った", route, r)
		}
	}
	if r := request(t, s, "POST", "_/edit", "{}", reviewHeaders(s)); r.status != 405 || r.header.Get("Allow") != "GET, HEAD" {
		t.Fatal(r)
	}
	for _, method := range []string{"GET", "HEAD", "PUT"} {
		r := request(t, s, method, "_/review/prepare", "", nil)
		if method == "PUT" {
			if r.status != 405 || r.header.Get("Allow") != "GET, HEAD, POST" {
				t.Fatal(r)
			}
		} else if r.status != 404 {
			t.Fatal(r)
		}
	}
	// 似た名前のほかの経路は、ふつうの 404 にする。
	for _, route := range []string{"_/review/unknown", "docs/_/review/prepare.md"} {
		if r := request(t, s, "GET", route, "", nil); r.status != 404 || strings.Contains(r.body, "error") {
			t.Fatal(route, r)
		}
	}
	none, _, _ := fixture(t, nil)
	for _, route := range []string{"_/review/prepare", "_/review/tables?render=1", "_/review/source?render=1&tag=p&range=1:1-1:1"} {
		if r := request(t, none, "GET", route, "", nil); r.status != 404 || strings.Contains(r.body, "error") {
			t.Fatal(route, r)
		}
		if r := request(t, none, "POST", route, reviewBody, reviewHeaders(none)); r.status != 405 || r.header.Get("Allow") != "GET, HEAD" {
			t.Fatal(route, r)
		}
	}
}

func TestReviewInput(t *testing.T) {
	var original map[string]any
	if e := json.Unmarshal([]byte(reviewBody), &original); e != nil {
		t.Fatal(e)
	}
	for _, c := range []struct {
		name   string
		mutate func(map[string]any, map[string]any, map[string]any)
	}{
		{"未知の項目", func(b, _, _ map[string]any) { b["extra"] = true }},
		{"描画なし", func(b, _, _ map[string]any) { delete(b, "render") }},
		{"描画の型", func(b, _, _ map[string]any) { b["render"] = "12" }},
		{"描画 0", func(b, _, _ map[string]any) { b["render"] = float64(0) }},
		{"描画の端数", func(b, _, _ map[string]any) { b["render"] = 1.5 }},
		{"コメントなし", func(b, _, _ map[string]any) { b["comments"] = []any{} }},
		{"コメントの型", func(b, _, _ map[string]any) { b["comments"] = nil }},
		{"コメント数", func(b, c, _ map[string]any) { b["comments"] = repeatComment(c, 51) }},
		{"未知のコメント項目", func(_, c, _ map[string]any) { c["extra"] = 1 }},
		{"本文なし", func(_, c, _ map[string]any) { delete(c, "text") }},
		{"本文の型", func(_, c, _ map[string]any) { c["text"] = nil }},
		{"空の本文", func(_, c, _ map[string]any) { c["text"] = " \t\n　" }},
		{"本文の長さ", func(_, c, _ map[string]any) { c["text"] = strings.Repeat("😀", 2001) }},
		{"本文の制御文字", func(_, c, _ map[string]any) { c["text"] = "本文\r" }},
		{"本文の削除文字", func(_, c, _ map[string]any) { c["text"] = "本文\x7f" }},
		{"選択の型", func(_, c, _ map[string]any) { c["selection"] = nil }},
		{"空の選択", func(_, c, _ map[string]any) { c["selection"] = "" }},
		{"選択の長さ", func(_, c, _ map[string]any) { c["selection"] = strings.Repeat("日", 2001) }},
		{"選択の制御文字", func(_, c, _ map[string]any) { c["selection"] = "文字\x00" }},
		{"対象なし", func(_, c, _ map[string]any) { delete(c, "target") }},
		{"未知の対象項目", func(_, _, target map[string]any) { target["extra"] = 1 }},
		{"未知の対象", func(_, _, target map[string]any) { target["kind"] = "other" }},
		{"範囲の型", func(_, _, target map[string]any) { target["range"] = 1 }},
		{"要素名の型", func(_, _, target map[string]any) { target["tag"] = nil }},
		{"要素名", func(_, _, target map[string]any) { target["tag"] = "P" }},
		{"範囲", func(_, _, target map[string]any) { target["range"] = "2:1-1:1" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			var b map[string]any
			_ = json.Unmarshal([]byte(reviewBody), &b)
			comment := b["comments"].([]any)[0].(map[string]any)
			target := comment["target"].(map[string]any)
			c.mutate(b, comment, target)
			if _, _, _, _, e := parseReview(b); e == nil || e.Status != 400 || e.Code != "invalid" {
				t.Fatal(e)
			}
		})
	}
	comment := original["comments"].([]any)[0].(map[string]any)
	comment["text"], comment["selection"] = "  "+strings.Repeat("😀", 2000)+"  ", strings.Repeat("日", 2000)
	original["comments"] = repeatComment(comment, 50)
	if _, _, comments, _, e := parseReview(original); e != nil || len(comments) != 50 || comments[0].Text != strings.Repeat("😀", 2000) {
		t.Fatal(comments, e)
	}
	for _, value := range []string{
		`{"kind":"html","prev":{"tag":"hr","range":"4:5-4:3"},"next":{"tag":"p","range":"6:1-6:5"}}`,
		`{"kind":"html"}`, `{"kind":"cell","table":0,"row":1,"col":2}`,
	} {
		var valueMap any
		_ = json.Unmarshal([]byte(value), &valueMap)
		if _, e := reviewTarget(valueMap); e != nil {
			t.Fatal(value, e)
		}
	}
	for _, value := range []string{
		`null`, `{"kind":"html","prev":null}`, `{"kind":"html","prev":{"tag":"p"}}`,
		`{"kind":"html","prev":{"tag":"p","range":"1:1-1:2","extra":true}}`,
		`{"kind":"html","prev":{"tag":"p","range":12}}`, `{"kind":"html","range":"1:1-1:2"}`,
		`{"kind":"cell","table":0,"row":-1,"col":0}`, `{"kind":"cell","table":0,"row":1.5,"col":0}`,
		`{"kind":"cell","table":0,"row":1,"col":"0"}`, `{"kind":"cell","table":1e99,"row":1,"col":0}`,
	} {
		var valueMap any
		_ = json.Unmarshal([]byte(value), &valueMap)
		if _, e := reviewTarget(valueMap); e == nil || e.Code != "invalid" {
			t.Fatal(value, e)
		}
	}
}

func repeatComment(c map[string]any, n int) []any {
	values := make([]any, n)
	for i := range values {
		values[i] = c
	}
	return values
}

func TestReviewErrors(t *testing.T) {
	s, _, _ := fixture(t, func(o *Options) {
		o.Review = &testReviewer{review: func(*string, uint64, []edit.ReviewTarget) (edit.ReviewDocument, []edit.ReviewItem, *edit.Error) {
			return edit.ReviewDocument{}, nil, &edit.Error{Status: 409, Code: "stale", Message: "描画が古くなりました。"}
		}}
	})
	if r := request(t, s, "POST", "_/review/prepare", reviewBody, reviewHeaders(s)); r.status != 409 || !strings.Contains(r.body, `"code":"stale"`) {
		t.Fatal(r)
	}
	v := &testReviewer{}
	large, _, _ := fixture(t, func(o *Options) { o.Review = v })
	var body map[string]any
	_ = json.Unmarshal([]byte(reviewBody), &body)
	c := body["comments"].([]any)[0].(map[string]any)
	c["text"] = strings.Repeat("日", 1000)
	body["comments"] = repeatComment(c, 30)
	encoded, _ := json.Marshal(body)
	r := request(t, large, "POST", "_/review/prepare", string(encoded), reviewHeaders(large))
	if r.status != 422 || !strings.Contains(r.body, `"code":"large"`) || len(large.reviews.entries) != 0 {
		t.Fatal(r)
	}
	// GET の HTML の境と JSON の境は、同じ要素名・公開の範囲で検査する。
	query := url.Values{"render": {"1"}, "tag": {"html"}, "prevTag": {"p"}, "prevRange": {"2:1-1:1"}}
	if r := request(t, large, "GET", "_/review/source?"+query.Encode(), "", nil); r.status != 400 {
		t.Fatal(r)
	}
}

func TestReviewErrorIndex(t *testing.T) {
	index := 0
	rejected := &edit.Error{Status: 422, Code: "target", Message: "対象がありません。", Index: &index,
		Failures: []edit.Failure{{Index: 0, Code: "target", Message: "対象がありません。"}, {Index: 2, Code: "range", Message: "指定したセルが範囲外です。"}}}
	s, _, _ := fixture(t, func(o *Options) {
		o.Review = &testReviewer{review: func(*string, uint64, []edit.ReviewTarget) (edit.ReviewDocument, []edit.ReviewItem, *edit.Error) {
			return edit.ReviewDocument{}, nil, rejected
		}}
		o.Edit = &testEditor{apply: func(context.Context, *string, map[string]any) (any, *edit.Error) {
			return nil, rejected
		}}
	})
	for _, route := range []string{"_/review/prepare", "_/edit"} {
		r := request(t, s, "POST", route, reviewBody, reviewHeaders(s))
		var value struct {
			Error struct {
				Index    *int `json:"index"`
				Failures []struct {
					Index   int    `json:"index"`
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"failures"`
			} `json:"error"`
		}
		if r.status != 422 || json.Unmarshal([]byte(r.body), &value) != nil {
			t.Fatal(r)
		}
		if route == "_/edit" {
			if value.Error.Index != nil || value.Error.Failures != nil {
				t.Fatal("編集の応答にコメントの番号がある", r.body)
			}
		} else if value.Error.Index == nil || *value.Error.Index != 0 {
			t.Fatal("0 番目のコメントの誤りを返していない", r.body)
		} else if len(value.Error.Failures) != 2 || value.Error.Failures[1].Index != 2 || value.Error.Failures[1].Code != "range" ||
			value.Error.Failures[1].Message != "指定したセルが範囲外です。" {
			t.Fatal("断った対象の全部を返していない", r.body)
		}
	}
}

func TestReviewPreparationRetention(t *testing.T) {
	var p reviewPreparations
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ids := make([]string, 35)
	empty := ""
	for i := range ids {
		ids[i] = p.add(&empty, edit.ReviewDocument{Render: uint64(i + 1)}, "依頼文", now.Add(time.Duration(i)*time.Second))
	}
	for i, id := range ids {
		entry, ok := p.get(id, now.Add(35*time.Second))
		if ok != (i >= 3) || ok && (entry.Doc.Current || entry.Doc.Path != "" || entry.Launched) {
			t.Fatal(i, entry, ok)
		}
	}
	id := p.add(nil, edit.ReviewDocument{}, "依頼文", now.Add(time.Minute))
	if entry, ok := p.get(id, now.Add(10*time.Minute)); !ok || !entry.Doc.Current {
		t.Fatal(entry, ok)
	}
	if _, ok := p.get(id, now.Add(11*time.Minute)); ok || len(p.entries) != 0 {
		t.Fatal("10 分で期限切れにならない", p.entries)
	}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := p.add(nil, edit.ReviewDocument{}, "並行の依頼文", now)
			_, _ = p.get(id, now)
		}()
	}
	wg.Wait()
	if len(p.entries) != 32 {
		t.Fatal("並行の保存で上限が変わった", len(p.entries))
	}
}

func prepareLaunch(t *testing.T, s *Server, query string) string {
	t.Helper()
	r := request(t, s, "POST", "_/review/prepare"+query, reviewBody, reviewHeaders(s))
	var value struct{ ID string }
	if r.status != 200 || json.Unmarshal([]byte(r.body), &value) != nil || value.ID == "" {
		t.Fatal("起動の試験用の依頼文を作れません", r)
	}
	return value.ID
}

func TestReviewLaunch(t *testing.T) {
	var calls atomic.Int32
	s, _, _ := fixture(t, func(o *Options) {
		o.Review = &testReviewer{}
		o.Launch = func(ctx context.Context, root string) error {
			calls.Add(1)
			if root != "/private-root" {
				t.Errorf("prepare のルートと違います: %q", root)
			}
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 40*time.Second || time.Until(deadline) < 39*time.Second {
				t.Errorf("起動の期限が40秒ではありません: %v", deadline)
			}
			return nil
		}
	})
	id := prepareLaunch(t, s, "?doc=docs/main.md")
	body := fmt.Sprintf(`{"id":%q}`, id)
	for i := 0; i < 2; i++ {
		headers := reviewHeaders(s)
		if i == 0 {
			headers["Sec-Fetch-Site"] = "same-origin"
			body = fmt.Sprintf(`{"id":%q}`, strings.ToUpper(id))
		} else {
			body = fmt.Sprintf(`{"id":%q}`, id)
		}
		r := request(t, s, "POST", "_/review/launch?doc=docs/main.md", body, headers)
		want := `{"launched":true}`
		if i == 1 {
			want = `{"again":true,"launched":true}`
		}
		if r.status != 200 || r.body != want || calls.Load() != 1 {
			t.Fatal(r, calls.Load())
		}
		protected(t, r)
		if r.header.Get("Cache-Control") != "no-store" {
			t.Fatal(r.header)
		}
	}
	entry, ok := s.reviews.get(id, time.Now())
	if !ok || !entry.Launched || entry.Launching {
		t.Fatal(entry, ok)
	}
	for _, method := range []string{"GET", "HEAD", "PUT"} {
		r := request(t, s, method, "_/review/launch", "", nil)
		want := 404
		if method == "PUT" {
			want = 405
			if r.header.Get("Allow") != "GET, HEAD, POST" {
				t.Fatal(r)
			}
		}
		if r.status != want || calls.Load() != 1 {
			t.Fatal(r, calls.Load())
		}
	}
}

func TestReviewLaunchConcurrent(t *testing.T) {
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	s, _, _ := fixture(t, func(o *Options) {
		o.Review = &testReviewer{}
		o.Launch = func(context.Context, string) error {
			if calls.Add(1) == 1 {
				close(started)
			}
			<-release
			return nil
		}
	})
	id := prepareLaunch(t, s, "")
	results := make(chan response, 2)
	launch := func() {
		results <- request(t, s, "POST", "_/review/launch", fmt.Sprintf(`{"id":%q}`, id), reviewHeaders(s))
	}
	go launch()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("起動の要求が届きません")
	}
	entry, ok := s.reviews.get(id, time.Now())
	if !ok || !entry.Launching || entry.Launched {
		t.Fatal("起動中の印がありません", entry)
	}
	go launch()
	// 起動中の2回目は、先の起動を待たずに「開いています」として返し、開いたことにはしない。
	select {
	case r := <-results:
		if r.status != 409 || !strings.Contains(r.body, `"code":"launching"`) || calls.Load() != 1 {
			t.Fatal(r, calls.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("起動中の2回目の要求が返りません")
	}
	once.Do(func() { close(release) })
	select {
	case r := <-results:
		if r.status != 200 || r.body != `{"launched":true}` || calls.Load() != 1 {
			t.Fatal(r, calls.Load())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("最初の起動要求が終わりません")
	}
	// 起動が済んだ後の要求は、起こし直さずに「もう開いている」と返す。
	if r := request(t, s, "POST", "_/review/launch", fmt.Sprintf(`{"id":%q}`, id), reviewHeaders(s)); r.status != 200 || r.body != `{"again":true,"launched":true}` || calls.Load() != 1 {
		t.Fatal(r, calls.Load())
	}
}

func TestReviewLaunchInputAndGuards(t *testing.T) {
	var calls atomic.Int32
	s, _, _ := fixture(t, func(o *Options) {
		o.Review = &testReviewer{}
		o.Launch = func(context.Context, string) error { calls.Add(1); return nil }
	})
	id := prepareLaunch(t, s, "")
	valid := fmt.Sprintf(`{"id":%q}`, id)
	for _, tc := range []struct {
		name, body, query, code string
		headers                 map[string]string
		status                  int
	}{
		{name: "知らない ID", body: `{"id":"ffffffffffffffffffffffffffffffff"}`, code: "expired", status: 404},
		{name: "空の ID", body: `{"id":""}`, code: "invalid", status: 400},
		{name: "ID が無い", body: `{}`, code: "invalid", status: 400},
		{name: "短い ID", body: `{"id":"abc"}`, code: "invalid", status: 400},
		{name: "16進以外", body: `{"id":"gggggggggggggggggggggggggggggggg"}`, code: "invalid", status: 400},
		{name: "ID の型", body: `{"id":1}`, code: "invalid", status: 400},
		{name: "null の ID", body: `{"id":null}`, code: "invalid", status: 400},
		{name: "JSON の型", body: `[]`, code: "invalid", status: 400},
		{name: "余分な項目", body: valid[:len(valid)-1] + `,"root":"/other"}`, code: "invalid", status: 400},
		{name: "余分な JSON", body: valid + `{}`, code: "invalid", status: 400},
		{name: "空の文書と編集中は別", body: valid, query: "?doc=", code: "changed", status: 409},
		{name: "別の文書", body: valid, query: "?doc=other.md", code: "changed", status: 409},
		{name: "Origin なし", body: valid, headers: map[string]string{"Origin": ""}, code: "forbidden", status: 403},
		{name: "別の Origin", body: valid, headers: map[string]string{"Origin": "http://example.invalid"}, code: "forbidden", status: 403},
		{name: "同じサイト", body: valid, headers: map[string]string{"Sec-Fetch-Site": "same-site"}, code: "forbidden", status: 403},
		{name: "別のサイト", body: valid, headers: map[string]string{"Sec-Fetch-Site": "cross-site"}, code: "forbidden", status: 403},
		{name: "直接の要求", body: valid, headers: map[string]string{"Sec-Fetch-Site": "none"}, code: "forbidden", status: 403},
		{name: "別の Host", body: valid, headers: map[string]string{"Host": "example.invalid"}, code: "forbidden", status: 403},
		{name: "JSON 以外", body: valid, headers: map[string]string{"Content-Type": "text/plain"}, code: "media", status: 415},
		{name: "本文の上限", body: strings.Repeat(" ", 1025), code: "large", status: 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := reviewHeaders(s)
			for key, value := range tc.headers {
				headers[key] = value
			}
			r := request(t, s, "POST", "_/review/launch"+tc.query, tc.body, headers)
			if r.status != tc.status || !strings.Contains(r.body, `"code":"`+tc.code+`"`) || calls.Load() != 0 {
				t.Fatal(r, calls.Load())
			}
		})
	}
	// ファイル文書で作った ID は、編集中の文書にも別のファイルにも使えない。
	fileID := prepareLaunch(t, s, "?doc=docs/main.md")
	for _, query := range []string{"", "?doc=", "?doc=other.md"} {
		if r := request(t, s, "POST", "_/review/launch"+query, fmt.Sprintf(`{"id":%q}`, fileID), reviewHeaders(s)); r.status != 409 {
			t.Fatal(r)
		}
	}
	wrong := strings.Repeat("f", 32)
	if wrong == s.Token {
		wrong = strings.Repeat("e", 32)
	}
	body := fmt.Sprintf(`{"id":%q}`, fileID)
	r := raw(t, s, fmt.Sprintf("POST /%s/_/review/launch?doc=docs/main.md HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nOrigin: http://127.0.0.1:%d\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", wrong, s.Port, s.Port, len(body), body), "POST")
	if r.status != 405 || calls.Load() != 0 {
		t.Fatal("token の違う要求で起動しました", r, calls.Load())
	}
	s.reviews.mu.Lock()
	entry := s.reviews.entries[id]
	entry.Created = time.Now().Add(-10 * time.Minute)
	s.reviews.entries[id] = entry
	s.reviews.mu.Unlock()
	r = request(t, s, "POST", "_/review/launch", valid, reviewHeaders(s))
	if r.status != 404 || !strings.Contains(r.body, `"code":"expired"`) || !strings.Contains(r.body, "もう一度「依頼文を作る」を押してください。") || calls.Load() != 0 {
		t.Fatal(r, calls.Load())
	}
	for _, chunked := range []bool{false, true} {
		extra, body := "Content-Length: 1025\r\n", ""
		if chunked {
			extra, body = "Transfer-Encoding: chunked\r\n", "401\r\n"+strings.Repeat(" ", 1025)+"\r\n0\r\n\r\n"
		}
		r := raw(t, s, fmt.Sprintf("POST /%s/_/review/launch HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nOrigin: http://127.0.0.1:%d\r\nContent-Type: application/json\r\n%s\r\n%s", s.Token, s.Port, s.Port, extra, body), "POST")
		if r.status != 413 {
			t.Fatal(r)
		}
	}
}

func TestReviewLaunchUnavailable(t *testing.T) {
	for _, review := range []bool{false, true} {
		s, _, _ := fixture(t, func(o *Options) {
			if review {
				o.Review = &testReviewer{}
			} else {
				o.Launch = func(context.Context, string) error { t.Error("コメントなしで起動しました"); return nil }
			}
		})
		for _, method := range []string{"GET", "HEAD", "POST", "PUT"} {
			r := request(t, s, method, "_/review/launch", `{}`, reviewHeaders(s))
			want := 404
			if method == "POST" || method == "PUT" {
				want = 405
				if r.header.Get("Allow") != "GET, HEAD" {
					t.Fatal(r)
				}
			}
			if r.status != want {
				t.Fatal(r)
			}
		}
	}
}

func TestReviewLaunchRetryAndPrivateErrors(t *testing.T) {
	for _, failure := range []error{vscode.ErrNotFound, errors.New("試験用の失敗 /private-root"), &os.PathError{Op: "open", Path: `C:\private\Code.exe`, Err: os.ErrPermission}} {
		var calls atomic.Int32
		s, _, _ := fixture(t, func(o *Options) {
			o.Review = &testReviewer{}
			o.Launch = func(context.Context, string) error {
				if calls.Add(1) == 1 {
					return failure
				}
				return nil
			}
		})
		id := prepareLaunch(t, s, "")
		body := fmt.Sprintf(`{"id":%q}`, id)
		r := request(t, s, "POST", "_/review/launch", body, reviewHeaders(s))
		code := "launch"
		if errors.Is(failure, vscode.ErrNotFound) {
			code = "notfound"
		}
		if r.status != 503 || !strings.Contains(r.body, `"code":"`+code+`"`) || !strings.Contains(r.body, vscode.Message(failure)) || strings.Contains(r.body, "/private-root") || strings.Contains(r.body, "private") || strings.Contains(r.body, s.Token) {
			t.Fatal(r)
		}
		entry, ok := s.reviews.get(id, time.Now())
		if !ok || entry.Launched || entry.Launching {
			t.Fatal("失敗後も起動中の印が残りました", entry)
		}
		if r = request(t, s, "POST", "_/review/launch", body, reviewHeaders(s)); r.status != 200 || r.body != `{"launched":true}` || calls.Load() != 2 {
			t.Fatal("同じ ID で再試行できません", r, calls.Load())
		}
	}
}

func TestReviewLaunchDetachedContext(t *testing.T) {
	type key struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "要求の値"))
	cancel()
	s, _, _ := fixture(t, func(o *Options) {
		o.Review = &testReviewer{}
		o.Launch = func(ctx context.Context, _ string) error {
			if ctx.Err() != nil || ctx.Value(key{}) != "要求の値" {
				t.Error("起動が要求の取り消しから切り離されていません", ctx.Err())
			}
			return nil
		}
	})
	id := prepareLaunch(t, s, "")
	r := httptest.NewRequest("POST", s.URL+"_/review/launch", strings.NewReader(fmt.Sprintf(`{"id":%q}`, id))).WithContext(ctx)
	r.Header.Set("Origin", "http://"+r.Host)
	r.Header.Set("Content-Type", "application/json")
	client, conn := net.Pipe()
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	go func() { defer conn.Close(); s.launchReview(conn, r) }()
	response, err := http.ReadResponse(bufio.NewReader(client), r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 || string(body) != `{"launched":true}` {
		t.Fatal(response.StatusCode, string(body), err)
	}
}
