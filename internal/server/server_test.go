package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ryo-aoki-pc/md-tools/internal/pagefiles"
	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type renderer func(context.Context, string, pipeline.Context) (pipeline.Result, error)

func (f renderer) Render(c context.Context, s string, p pipeline.Context) (pipeline.Result, error) {
	return f(c, s, p)
}
func fixture(t *testing.T, alter func(*Options)) (*Server, *session.Session, string) {
	t.Helper()
	root := t.TempDir()
	for name, body := range map[string]string{"image.png": "image", "file.pdf": "pdf", "page.html": "<script>危険</script>", "project.txt": "本文", "main.md": "ディスク", "other.MD": "別の文書", "percent%ZZ": "不完全なエスケープ"} {
		if e := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); e != nil {
			t.Fatal(e)
		}
	}
	_ = os.Mkdir(filepath.Join(root, "directory"), 0755)
	_ = os.Mkdir(filepath.Join(root, ".git"), 0755)
	_ = os.WriteFile(filepath.Join(root, ".git", "config"), []byte("非機密の見本"), 0644)
	state, e := session.New(session.Options{Root: root, Renderer: renderer(func(_ context.Context, s string, _ pipeline.Context) (pipeline.Result, error) {
		return pipeline.Result{HTML: s, Diagrams: []pipeline.Diagram{{ID: strings.Repeat("a", 64), Source: "図の原文"}}}, nil
	})})
	if e != nil {
		t.Fatal(e)
	}
	p, e := pagefiles.Load(pagefiles.Options{})
	if e != nil {
		t.Fatal(e)
	}
	o := Options{Session: state, Page: p}
	if alter != nil {
		alter(&o)
	}
	s, e := New(o)
	if e != nil {
		t.Fatal(e)
	}
	state.SetCallbacks(func(d session.Data) { _ = s.Broadcast("render", d) }, func(d session.Data) { _ = s.Broadcast("scroll", d) })
	t.Cleanup(func() { _ = s.Close("exit"); _ = state.Close() })
	return s, state, root
}
func eventually(t *testing.T, p func() bool) {
	t.Helper()
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		if p() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("HTTPの状態が時間内に変わらない")
}

type response struct {
	status int
	header http.Header
	body   string
	close  bool
}

func raw(t *testing.T, s *Server, text, method string) response {
	t.Helper()
	c, e := net.Dial("tcp4", fmt.Sprintf("127.0.0.1:%d", s.Port))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, e = io.WriteString(c, text); e != nil {
		t.Fatal(e)
	}
	r, e := http.ReadResponse(bufio.NewReader(c), &http.Request{Method: method})
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	b, e := io.ReadAll(r.Body)
	if e != nil {
		t.Fatal(e)
	}
	return response{r.StatusCode, r.Header, string(b), r.Close}
}
func request(t *testing.T, s *Server, method, rel, body string, h map[string]string) response {
	t.Helper()
	header := fmt.Sprintf("Host: 127.0.0.1:%d\r\n", s.Port)
	if _, ok := h["Host"]; ok {
		header = ""
	}
	for k, v := range h {
		header += k + ": " + v + "\r\n"
	}
	if method == "POST" {
		header += fmt.Sprintf("Content-Length: %d\r\n", len(body))
	}
	return raw(t, s, fmt.Sprintf("%s /%s/%s HTTP/1.1\r\n%s\r\n%s", method, s.Token, rel, header, body), method)
}
func protected(t *testing.T, r response) {
	t.Helper()
	if r.header.Get("X-Content-Type-Options") != "nosniff" || r.header.Get("Referrer-Policy") != "no-referrer" || r.header.Get("Access-Control-Allow-Origin") != "" || !r.close {
		t.Fatal(r.header)
	}
}

func TestHTTPGuards(t *testing.T) {
	s, _, _ := fixture(t, nil)
	for _, host := range []string{"example.invalid", "localhost", fmt.Sprintf("127.0.0.1:%d", s.Port+1)} {
		r := request(t, s, "GET", "", "", map[string]string{"Host": host})
		if r.status != 403 {
			t.Fatal(r)
		}
		protected(t, r)
	}
	for _, site := range []string{"same-site", "cross-site", "unknown", "same-origin", "none"} {
		want := 403
		if site == "none" || site == "same-origin" {
			want = 200
		}
		r := request(t, s, "GET", "", "", map[string]string{"Sec-Fetch-Site": site})
		if r.status != want {
			t.Fatal(site, r)
		}
	}
	last := "0"
	if strings.HasSuffix(s.Token, "0") {
		last = "1"
	}
	for _, target := range []string{"/", "/wrong/", "/" + s.Token + "x/", "/" + s.Token[:31] + "/", "/" + s.Token[:31] + last + "/", "//" + s.Token + "/", "/x/" + s.Token + "/"} {
		r := raw(t, s, fmt.Sprintf("GET %s HTTP/1.1\r\nHost: localhost:%d\r\n\r\n", target, s.Port), "GET")
		if r.status != 404 {
			t.Fatal(r)
		}
	}
	r := raw(t, s, fmt.Sprintf("GET /%s?list HTTP/1.1\r\nHost: localhost:%d\r\n\r\n", s.Token, s.Port), "GET")
	if r.status != 301 || r.header.Get("Location") != "/"+s.Token+"/?list" {
		t.Fatal(r)
	}
	r = request(t, s, "POST", "", "", nil)
	if r.status != 405 || r.header.Get("Allow") != "GET, HEAD" {
		t.Fatal(r)
	}
	protected(t, r)
}
func TestPageAndLibraries(t *testing.T) {
	s, _, _ := fixture(t, nil)
	r := request(t, s, "GET", "", "", nil)
	if r.status != 200 || r.header.Get("Content-Security-Policy") != CSP || r.header.Get("Cache-Control") != "no-store" || strings.Contains(r.body, "{{CONFIG}}") || !strings.Contains(r.body, `"assets":null`) {
		t.Fatal(r)
	}
	for _, rel := range []string{"_/preview.js", "_/copy-code.js", "_/edit-grid.js", "_/edit-targets.js", "_/edit.js", "_/review.js", "_/preview.css", "_/fallback.css"} {
		r := request(t, s, "GET", rel, "", nil)
		if r.status != 200 || r.body == "" {
			t.Fatal(rel, r)
		}
	}
	for _, rel := range []string{"katex/katex.min.js", "katex/katex.min.css", "katex/fonts/KaTeX_Main-Regular.woff2", "mermaid/mermaid.min.js", "md-table-sticky/sticky-table.js", "katex/LICENSE", "mermaid/LICENSE"} {
		r := request(t, s, "GET", "_/lib/"+rel, "", nil)
		if r.status != 200 || r.header.Get("Content-Security-Policy") != "sandbox" || r.header.Get("Cache-Control") != "no-cache" {
			t.Fatal(rel, r.status, r.header)
		}
		cached := request(t, s, "GET", "_/lib/"+rel, "", map[string]string{"If-None-Match": r.header.Get("ETag")})
		if cached.status != 304 || cached.body != "" {
			t.Fatal(cached)
		}
		head := request(t, s, "HEAD", "_/lib/"+rel, "", nil)
		if head.body != "" || head.header.Get("Content-Length") != fmt.Sprint(len(r.body)) {
			t.Fatal(head)
		}
	}
	for _, rel := range []string{"katex/../katex.min.js", "katex/%2e%2e/package.json", "katex/package.json", "katex/fonts/a.css", "katex/fonts/KaTeX_Main-Regular.ttf", "lock.json", "md-table-sticky/LICENSE"} {
		if r := request(t, s, "GET", "_/lib/"+rel, "", nil); r.status != 404 {
			t.Fatal(rel, r.status)
		}
	}
}
func TestRepositoryFiles(t *testing.T) {
	s, _, _ := fixture(t, nil)
	for _, rel := range []string{"image.png", "page.html", "project.txt", "percent%ZZ"} {
		r := request(t, s, "GET", rel, "", nil)
		if r.status != 200 || r.header.Get("Content-Security-Policy") != "sandbox" {
			t.Fatal(rel, r)
		}
		cached := request(t, s, "GET", rel, "", map[string]string{"If-None-Match": r.header.Get("ETag")})
		if cached.status != 304 || cached.body != "" {
			t.Fatal(cached)
		}
	}
	if r := request(t, s, "GET", "file.pdf", "", nil); r.header.Get("Content-Security-Policy") != "" || r.header.Get("Content-Type") != "application/pdf" {
		t.Fatal(r)
	}
	if r := request(t, s, "GET", "directory", "", nil); r.status != 404 {
		t.Fatal(r)
	}
	for rel, want := range map[string]int{"../project.txt": 403, "%2e%2e/project.txt": 403, ".git/config": 403, "directory/.GIT/config": 403, "a%00b": 400, "a%5cb": 400, "a%0Ab": 400} {
		if r := request(t, s, "GET", rel, "", nil); r.status != want {
			t.Fatal(rel, r)
		}
	}
}
func TestRawParser(t *testing.T) {
	s, _, _ := fixture(t, nil)
	for _, item := range []struct {
		raw    string
		status int
	}{{fmt.Sprintf("POST /%s/ HTTP/1.1\r\nHost: localhost:%d\r\nExpect: 100-continue\r\n\r\n", s.Token, s.Port), 405}, {fmt.Sprintf("GET /%s/ HTTP/1.1\r\nHost: localhost:%d\r\nExpect: something\r\n\r\n", s.Token, s.Port), 200}, {fmt.Sprintf("CONNECT 127.0.0.1:80 HTTP/1.1\r\nHost: localhost:%d\r\n\r\n", s.Port), 405}, {"GET / HTTP/1.1\r\n\r\n", 400}, {fmt.Sprintf("GET / HTTP/1.1\r\nHost: localhost:%d\r\nBad Header: value\r\n\r\n", s.Port), 400}, {fmt.Sprintf("GET / HTTP/1.1\r\nHost: localhost:%d\r\nX-Large: %s\r\n\r\n", s.Port, strings.Repeat("x", 17000)), 431}} {
		r := raw(t, s, item.raw, "GET")
		if r.status != item.status {
			t.Fatal(r.status, item.status)
		}
		protected(t, r)
	}
	// Host が複数ある要求と、行頭の空白で続けたヘッダーは、どの値を使うかを決めずに拒む。
	host := fmt.Sprintf("127.0.0.1:%d", s.Port)
	for _, header := range []string{
		"Host: " + host + "\r\nHost: attacker.example\r\n",
		"Host: attacker.example\r\nHost: " + host + "\r\n",
		"Host: " + host + "\r\nhOST: " + host + "\r\n",
		"Host: " + host + "\r\nX-A: 1\r\n Host: attacker.example\r\n",
		"Host: " + host + "\r\nX-A: 1\r\n\tSec-Fetch-Site: same-origin\r\n",
	} {
		r := raw(t, s, fmt.Sprintf("GET /%s/ HTTP/1.1\r\n%s\r\n", s.Token, header), "GET")
		if r.status != 400 {
			t.Fatalf("%q: %d", header, r.status)
		}
		protected(t, r)
	}
}
func TestJump(t *testing.T) {
	var mu sync.Mutex
	var jumps []int64
	s, _, _ := fixture(t, func(o *Options) { o.OnJump = func(line int64) { mu.Lock(); jumps = append(jumps, line); mu.Unlock() } })
	origin := fmt.Sprintf("http://127.0.0.1:%d", s.Port)
	valid := map[string]string{"Origin": origin, "Content-Type": "application/json"}
	for _, body := range []string{`{"line":7}`, strings.Repeat(" ", 1014) + `{"line":7}`} {
		if r := request(t, s, "POST", "_/jump", body, valid); r.status != 204 {
			t.Fatal(r)
		}
	}
	for _, item := range []struct {
		body, origin, mime string
		status             int
	}{{`{"line":7}`, "null", "application/json", 403}, {`{"line":7}`, origin, "application/jsonx", 415}, {strings.Repeat(" ", 1025), origin, "application/json", 413}, {"broken", origin, "application/json", 400}, {"null", origin, "application/json", 400}, {"[]", origin, "application/json", 400}, {`{"line":0}`, origin, "application/json", 400}, {`{"line":1.5}`, origin, "application/json", 400}, {`{"line":"7"}`, origin, "application/json", 400}, {`{"line":1e100}`, origin, "application/json", 400}, {string([]byte{255}), origin, "application/json", 400}} {
		r := request(t, s, "POST", "_/jump", item.body, map[string]string{"Origin": item.origin, "Content-Type": item.mime})
		if r.status != item.status {
			t.Fatal(item, r)
		}
	}
	r := raw(t, s, fmt.Sprintf("POST /%s/_/jump HTTP/1.1\r\nHost: localhost:%d\r\nOrigin: %s\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\n\r\n258\r\n%s\r\n258\r\n%s\r\n0\r\n\r\n", s.Token, s.Port, origin, strings.Repeat(" ", 600), strings.Repeat(" ", 600)), "POST")
	if r.status != 413 {
		t.Fatal(r)
	}
	if r := request(t, s, "PUT", "_/jump", "", nil); r.status != 405 || r.header.Get("Allow") != "GET, HEAD, POST" {
		t.Fatal(r)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(jumps) != 2 {
		t.Fatal(jumps)
	}
}

type stream struct {
	body   io.ReadCloser
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	events []string
	data   []session.Data
}

func openEvents(t *testing.T, s *Server, doc *string) *stream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	url := s.URL + "_/events"
	if doc != nil {
		url += "?doc=" + documentsEscape(*doc)
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	transport := &http.Transport{DisableKeepAlives: true}
	r, e := transport.RoundTrip(req)
	if e != nil {
		cancel()
		t.Fatal(e)
	}
	out := &stream{body: r.Body, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(out.done)
		scanner := bufio.NewScanner(r.Body)
		scanner.Buffer(make([]byte, 65536), 16*1024*1024)
		name := ""
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				name = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") {
				var d session.Data
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &d) == nil {
					out.mu.Lock()
					out.events = append(out.events, name)
					out.data = append(out.data, d)
					out.mu.Unlock()
				}
			}
		}
	}()
	t.Cleanup(func() { out.close(); transport.CloseIdleConnections() })
	return out
}
func documentsEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s, "%", "%25"), " ", "%20"), "?", "%3F")
}
func (s *stream) close() { s.cancel(); _ = s.body.Close(); <-s.done }
func (s *stream) has(name, text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, event := range s.events {
		if name == event && strings.Contains(fmt.Sprint(s.data[i]), text) {
			return true
		}
	}
	return false
}
func TestSSE(t *testing.T) {
	s, state, root := fixture(t, nil)
	_ = state.SetDocument(session.Document{Path: filepath.Join(root, "main.md"), Text: "未保存", Rev: 1})
	state.SetScroll(session.Data{"line": 3, "ratio": 0.5, "lines": 10})
	active := openEvents(t, s, nil)
	doc := "other.MD"
	linked := openEvents(t, s, &doc)
	sameDoc := "main.md"
	same := openEvents(t, s, &sameDoc)
	eventually(t, func() bool {
		return active.has("render", "未保存") && linked.has("render", "別の文書") && same.has("render", "未保存")
	})
	state.SetScroll(session.Data{"line": 7, "ratio": 0.7, "lines": 10})
	eventually(t, func() bool { return active.has("scroll", "0.7") })
	if linked.has("scroll", "") || same.has("scroll", "") {
		t.Fatal("文書別SSEへスクロールした")
	}
	_ = os.WriteFile(filepath.Join(root, "other.MD"), []byte("保存し直した別の文書"), 0644)
	eventually(t, func() bool { return linked.has("render", "保存し直した") })
	if active.has("render", "保存し直した") || same.has("render", "保存し直した") {
		t.Fatal("別文書へ描画を流した")
	}
	_ = s.Close("exit")
	// Close は、サーバーの側の書き込みが終わるまでしか待たない。受ける側が読み終わるのを待って確かめる
	// (届かなければ、ここで時間切れになる)。
	eventually(t, func() bool { return active.has("stop", "exit") })
	if s.ClientCount() != 0 {
		t.Fatal(s.ClientCount())
	}
}
func TestSlowSSE(t *testing.T) {
	s, _, _ := fixture(t, nil)
	stream := openEvents(t, s, nil)
	eventually(t, func() bool { return s.ClientCount() == 1 })
	_ = s.Broadcast("render", session.Data{"html": strings.Repeat("x", MaxQueue)})
	eventually(t, func() bool { return s.ClientCount() == 0 })
	stream.close()
}
func TestListAndUnavailable(t *testing.T) {
	s, _, _ := fixture(t, nil)
	r := request(t, s, "GET", "_/list", "", nil)
	if r.status != 200 || !strings.Contains(r.body, `"main.md"`) || strings.Contains(r.body, ".git") {
		t.Fatal(r)
	}
	for _, name := range []string{"missing.md", "../main.md", "image.png", ".git/private.md", ""} {
		tab := openEvents(t, s, &name)
		eventually(t, func() bool { return tab.has("render", "この文書は開けない") })
		tab.close()
	}
}

type assets struct{}

func (assets) ID() string { return "19.5.1-0123456789ab" }
func (assets) PageConfig() any {
	return map[string]any{"id": "19.5.1-0123456789ab", "version": "19.5.1"}
}
func (assets) File(name string) ([]byte, string, bool) {
	return []byte("css"), "text/css; charset=utf-8", name == "style.css"
}

type diagram struct{}

func (diagram) Render(context.Context, string) ([]byte, error) { return []byte("<svg/>"), nil }
func (diagram) Close() error                                   { return nil }
func TestAssetsAndDiagrams(t *testing.T) {
	s, state, _ := fixture(t, func(o *Options) { o.Assets = assets{}; o.Plantuml = diagram{} })
	_ = state.SetDocument(session.Document{Name: "無名", Text: "図", Rev: 1})
	eventually(t, func() bool { d, _ := state.Last(); return d != nil })
	for _, rel := range []string{"_/assets/19.5.1-0123456789ab/style.css", "_/plantuml/" + strings.Repeat("a", 64) + ".svg"} {
		r := request(t, s, "GET", rel, "", nil)
		if r.status != 200 || r.header.Get("Content-Security-Policy") != "sandbox" {
			t.Fatal(r)
		}
	}
	for _, rel := range []string{"_/assets/other/style.css", "_/assets/19.5.1-0123456789ab/../style.css", "_/plantuml/a.svg", "_/plantuml/" + strings.Repeat("b", 64) + ".svg"} {
		if r := request(t, s, "GET", rel, "", nil); r.status != 404 {
			t.Fatal(rel, r)
		}
	}
}

func TestProtocolAndConnection(t *testing.T) {
	s, _, _ := fixture(t, nil)
	for _, version := range []string{"HTTP/1.2", "HTTP/2.0"} {
		r := raw(t, s, fmt.Sprintf("GET /%s/ %s\r\nHost: localhost:%d\r\n\r\n", s.Token, version, s.Port), "GET")
		if r.status != 400 {
			t.Fatal(version, r.status)
		}
		protected(t, r)
	}
	r := raw(t, s, fmt.Sprintf("GET ? HTTP/1.1\r\nHost: localhost:%d\r\n\r\n", s.Port), "GET")
	if r.status != 400 {
		t.Fatal(r)
	}
	conn, e := net.Dial("tcp4", fmt.Sprintf("127.0.0.1:%d", s.Port))
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	one := fmt.Sprintf("GET /%s/ HTTP/1.1\r\nHost: localhost:%d\r\nConnection: keep-alive\r\n\r\n", s.Token, s.Port)
	_, _ = io.WriteString(conn, one+one)
	b, e := io.ReadAll(conn)
	if e != nil || strings.Count(string(b), "HTTP/1.1 200") != 1 {
		t.Fatal(e, "接続を再利用した")
	}
}
func TestClientCountOrder(t *testing.T) {
	var mu sync.Mutex
	counts := []int{}
	s, _, _ := fixture(t, func(o *Options) { o.OnClientCount = func(n int) { mu.Lock(); counts = append(counts, n); mu.Unlock() } })
	first := openEvents(t, s, nil)
	second := openEvents(t, s, nil)
	eventually(t, func() bool { return s.ClientCount() == 2 })
	first.close()
	eventually(t, func() bool { return s.ClientCount() == 1 })
	second.close()
	eventually(t, func() bool { return s.ClientCount() == 0 })
	mu.Lock()
	defer mu.Unlock()
	if fmt.Sprint(counts) != "[1 2 1 0]" {
		t.Fatal(counts)
	}
}

func TestSSELargeText(t *testing.T) {
	s, _, _ := fixture(t, nil)
	tab := openEvents(t, s, nil)
	eventually(t, func() bool { return s.ClientCount() == 1 })
	text := strings.Repeat("<", 5*1024*1024)
	_ = s.Broadcast("render", session.Data{"markdown": text})
	eventually(t, func() bool { return tab.has("render", "<<<") })
	if s.ClientCount() != 1 {
		t.Fatal("8 MiB 未満の本文をエスケープで膨らませて切断した")
	}
}

type panicState struct {
	State
	afterHeaders bool
}

func (p panicState) Last() (session.Data, session.Data) {
	if p.afterHeaders {
		panic("試験用の内部の誤り")
	}
	return p.State.Last()
}
func (p panicState) HasDocument() bool {
	if !p.afterHeaders {
		panic("試験用の内部の誤り")
	}
	return p.State.HasDocument()
}
func TestFailureAfterHeaders(t *testing.T) {
	for _, after := range []bool{false, true} {
		s, _, _ := fixture(t, func(o *Options) { o.Session = panicState{o.Session, after} })
		rel := ""
		if after {
			rel = "_/events"
		}
		conn, e := net.Dial("tcp4", fmt.Sprintf("127.0.0.1:%d", s.Port))
		if e != nil {
			t.Fatal(e)
		}
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		_, _ = fmt.Fprintf(conn, "GET /%s/%s HTTP/1.1\r\nHost: localhost:%d\r\n\r\n", s.Token, rel, s.Port)
		body, e := io.ReadAll(conn)
		_ = conn.Close()
		if e != nil {
			t.Fatal(e)
		}
		want := "HTTP/1.1 500"
		if after {
			want = "HTTP/1.1 200"
		}
		if !strings.HasPrefix(string(body), want) || strings.Count(string(body), "HTTP/1.1") != 1 {
			t.Fatal("応答の開始後に別の状態行を追送した", string(body))
		}
	}
}
