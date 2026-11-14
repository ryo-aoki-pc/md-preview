package server

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/internal/links"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

func TestLinksEndpoint(t *testing.T) {
	s, state, root := fixture(t, nil)
	if err := state.SetDocument(session.Document{Path: filepath.Join(root, "main.md"), Text: `<p data-sourcepos="2:1-2:30"><a href="#missing">ない</a></p>`, Rev: 1}); err != nil {
		t.Fatal(err)
	}
	var render uint64
	eventually(t, func() bool { src, ok := state.Source(nil); render = src.Render; return ok })
	query := "_/links?render=" + fmt.Sprint(render)
	for _, suffix := range []string{"", "&doc=main.md"} {
		r := request(t, s, "GET", query+suffix, "", nil)
		protected(t, r)
		var value links.Result
		if err := json.Unmarshal([]byte(r.body), &value); err != nil || r.status != 200 || value.Render != render || len(value.Broken) != 1 || value.Broken[0].Href != "#missing" || *value.Broken[0].Line != 2 || value.Unchecked == nil {
			t.Fatal(r, value, err)
		}
		if r.header.Get("Cache-Control") != "no-store" {
			t.Fatal(r.header)
		}
	}
	for _, test := range []struct {
		query  string
		status int
	}{
		{"_/links?render=" + fmt.Sprint(render+1), 409},
		{"_/links?render=0", 400},
		{"_/links?render=-1", 400},
		{"_/links?render=1&render=2", 400},
		{query + "&doc=../main.md", 403},
		{query + "&doc=.git/config", 403},
		{query + "&doc=missing.md", 404},
	} {
		r := request(t, s, "GET", test.query, "", nil)
		if r.status != test.status || test.status == 409 && !strings.Contains(r.body, `"code":"stale"`) {
			t.Fatal(test, r)
		}
	}
	for _, h := range []map[string]string{{"Host": "example.invalid"}, {"Sec-Fetch-Site": "cross-site"}} {
		r := request(t, s, "GET", query, "", h)
		if r.status != 403 {
			t.Fatal(r)
		}
		protected(t, r)
	}
	r := raw(t, s, fmt.Sprintf("GET /_/links?render=%d HTTP/1.1\r\nHost: 127.0.0.1:%d\r\n\r\n", render, s.Port), "GET")
	if r.status != 404 {
		t.Fatal(r)
	}
	r = request(t, s, "POST", query, "", nil)
	if r.status != 405 {
		t.Fatal(r)
	}
}

type rootlessLinksState struct {
	*session.Session
}

func (rootlessLinksState) Root() string { return "" }

func TestLinksEndpointWithoutRoot(t *testing.T) {
	s, state, root := fixture(t, func(o *Options) {
		o.Session = rootlessLinksState{o.Session.(*session.Session)}
	})
	if err := state.SetDocument(session.Document{Path: filepath.Join(root, "main.md"), Text: "本文", Rev: 1}); err != nil {
		t.Fatal(err)
	}
	var render uint64
	eventually(t, func() bool { src, ok := state.Source(nil); render = src.Render; return ok })
	query := "_/links?render=" + fmt.Sprint(render)
	if r := request(t, s, "GET", query, "", nil); r.status != 200 {
		t.Fatal(r)
	}
	// ルートがなければ、Resolve が 403 を返すパスも含めて一律に 404 にする。
	for _, doc := range []string{"main.md", "../main.md", ".git/config"} {
		r := request(t, s, "GET", query+"&doc="+doc, "", nil)
		if r.status != 404 {
			t.Fatal(doc, r)
		}
	}
}
