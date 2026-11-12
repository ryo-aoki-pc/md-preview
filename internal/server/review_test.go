package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ryo-aoki-pc/md-tools/internal/edit"
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
	// コメントの口は 3 つだけ。似た名前のほかの経路は、ふつうの 404 にする。
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
			if _, _, _, e := parseReview(b); e == nil || e.Status != 400 || e.Code != "invalid" {
				t.Fatal(e)
			}
		})
	}
	comment := original["comments"].([]any)[0].(map[string]any)
	comment["text"], comment["selection"] = "  "+strings.Repeat("😀", 2000)+"  ", strings.Repeat("日", 2000)
	original["comments"] = repeatComment(comment, 50)
	if _, _, comments, e := parseReview(original); e != nil || len(comments) != 50 || comments[0].Text != strings.Repeat("😀", 2000) {
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
	rejected := &edit.Error{Status: 422, Code: "target", Message: "対象がありません。", Index: &index}
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
				Index *int `json:"index"`
			} `json:"error"`
		}
		if r.status != 422 || json.Unmarshal([]byte(r.body), &value) != nil {
			t.Fatal(r)
		}
		if route == "_/edit" {
			if value.Error.Index != nil {
				t.Fatal("編集の応答にコメントの番号がある", r.body)
			}
		} else if value.Error.Index == nil || *value.Error.Index != 0 {
			t.Fatal("0 番目のコメントの誤りを返していない", r.body)
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
