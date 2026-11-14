// Package server は 127.0.0.1 の HTTP と文書別 SSE を提供する。
package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ryo-aoki-pc/md-tools/internal/documents"
	"github.com/ryo-aoki-pc/md-tools/internal/fileopen"
	"github.com/ryo-aoki-pc/md-tools/internal/pagefiles"
	"github.com/ryo-aoki-pc/md-tools/internal/resolve"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

const CSP = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self' data:; img-src * data: blob:; media-src *; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
const MaxQueue = 8 * 1024 * 1024

type DiagramRenderer interface {
	Render(context.Context, string) ([]byte, error)
	Close() error
}
type OfficialAssets interface {
	ID() string
	PageConfig() any
	File(string) ([]byte, string, bool)
}
type State interface {
	Root() string
	Last() (session.Data, session.Data)
	HasDocument() bool
	IsCurrentDocument(string) bool
	SubscribeDocument(string, session.Listener) func()
	PlantumlSource(string) (string, bool)
	OnRootChange(func()) func()
}
type Options struct {
	Session       State
	Page          pagefiles.Page
	Port          int
	Token         string
	Plantuml      DiagramRenderer
	Assets        OfficialAssets
	AssetsNotice  string
	OnJump        func(int64)
	OnClientCount func(int)
	Libraries     *pagefiles.Libraries
	Edit          Editor
	Review        Reviewer
	Launch        func(ctx context.Context, root string) error
}
type Server struct {
	URL         string
	Port        int
	Token       string
	opts        Options
	listener    net.Listener
	mu          sync.Mutex
	counts      sync.Mutex
	clients     map[*client]bool
	connections map[net.Conn]context.CancelFunc
	remembered  map[string]session.Data
	closing     bool
	offRoot     func()
	closeOnce   sync.Once
	done        chan struct{}
	wg          sync.WaitGroup
	reviews     reviewPreparations
}
type client struct {
	conn   net.Conn
	doc    *string
	mu     sync.Mutex
	queue  []string
	bytes  int
	signal chan struct{}
	ending bool
	done   chan struct{}
	once   sync.Once
}

type responseConn struct {
	net.Conn
	headersSent bool
}

func New(o Options) (*Server, error) {
	if o.Session == nil {
		return nil, errors.New("文書の状態がありません")
	}
	if o.Token == "" {
		b := make([]byte, 16)
		if _, e := rand.Read(b); e != nil {
			return nil, e
		}
		o.Token = hex.EncodeToString(b)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(o.Token) {
		return nil, errors.New("token は16バイトの16進文字列にしてください")
	}
	if o.Libraries == nil {
		var e error
		o.Libraries, e = pagefiles.EmbeddedLibraries()
		if e != nil {
			return nil, e
		}
	}
	l, e := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(o.Port)))
	if e != nil {
		return nil, e
	}
	s := &Server{opts: o, listener: l, Token: o.Token, Port: l.Addr().(*net.TCPAddr).Port, clients: map[*client]bool{}, connections: map[net.Conn]context.CancelFunc{}, remembered: map[string]session.Data{}, done: make(chan struct{})}
	s.URL = fmt.Sprintf("http://127.0.0.1:%d/%s/", s.Port, s.Token)
	s.offRoot = o.Session.OnRootChange(func() { s.mu.Lock(); delete(s.remembered, "render"); delete(s.remembered, "scroll"); s.mu.Unlock() })
	s.wg.Add(1)
	go s.accept()
	return s, nil
}

func (s *Server) accept() {
	defer s.wg.Done()
	for {
		conn, e := s.listener.Accept()
		if e != nil {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.mu.Lock()
		if s.closing {
			s.mu.Unlock()
			cancel()
			_ = conn.Close()
			continue
		}
		s.connections[conn] = cancel
		s.wg.Add(1)
		s.mu.Unlock()
		go s.serve(ctx, conn)
	}
}

// ReadRequest だけを使い、ServeMux のパス整理・Expect 応答・HTTP/2・接続再利用を経由しない。
// HTTP の解析前にヘッダーを上限まで読み、解析の誤りにも共通ヘッダーを付ける。
func (s *Server) serve(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		cancel := s.connections[conn]
		delete(s.connections, conn)
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		_ = conn.Close()
	}()
	reader := bufio.NewReader(conn)
	// 未読の大きなヘッダーを残して Close すると、Windows は応答より先に RST を送ることがある。
	// 拒否の応答を半閉鎖で届け、残りは大きさと時間を限って捨てる。
	rejectParser := func(status int) {
		send(conn, nil, status, nil, []byte{})
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		_, _ = io.Copy(io.Discard, io.LimitReader(reader, 64*1024))
	}
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	header := []byte{}
	for {
		line, e := reader.ReadSlice('\n')
		if len(header)+len(line) > 16*1024 {
			rejectParser(431)
			return
		}
		header = append(header, line...)
		if errors.Is(e, bufio.ErrBufferFull) {
			continue
		}
		if e != nil {
			if len(header) > 0 {
				rejectParser(400)
			}
			return
		}
		if bytes.HasSuffix(header, []byte("\r\n\r\n")) || bytes.HasSuffix(header, []byte("\n\n")) {
			break
		}
	}
	_ = conn.SetReadDeadline(time.Time{})
	// 行頭の空白でヘッダーを次の行へ続ける古い書き方は、前の行の値にせず拒む。
	for _, line := range bytes.Split(header, []byte("\n"))[1:] {
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			rejectParser(400)
			return
		}
	}
	// URL はルーターだけが解釈する。Go の URL 解析が不完全な % を拒む前に、解析用の対象を置き換える。
	lineEnd := bytes.IndexByte(header, '\n')
	if lineEnd < 0 {
		rejectParser(400)
		return
	}
	first := strings.TrimSuffix(string(header[:lineEnd]), "\r")
	fields := strings.Split(first, " ")
	if len(fields) != 3 || fields[1] == "" {
		rejectParser(400)
		return
	}
	method := fields[0]
	if fields[2] != "HTTP/1.0" && fields[2] != "HTTP/1.1" {
		rejectParser(400)
		return
	}
	for _, c := range []byte(fields[1]) {
		if c <= 32 || c == 127 {
			rejectParser(400)
			return
		}
	}
	if method != "CONNECT" && !strings.HasPrefix(fields[1], "/") && fields[1] != "*" && !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`).MatchString(fields[1]) {
		rejectParser(400)
		return
	}
	parseMethod := method
	if method == "CONNECT" {
		parseMethod = "GET"
	}
	parsedHeader := []byte(parseMethod + " / " + fields[2] + "\r\n")
	parsedHeader = append(parsedHeader, header[lineEnd+1:]...)
	r, e := http.ReadRequest(bufio.NewReader(io.MultiReader(bytes.NewReader(parsedHeader), reader)))
	if e != nil {
		rejectParser(400)
		return
	}
	for name := range r.Header {
		for _, c := range []byte(name) {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c))) {
				rejectParser(400)
				return
			}
		}
	}
	r.Method = method
	r.RequestURI = fields[1]
	r = r.WithContext(ctx)
	// GET の切断は図の描画中にも取り消しへ伝える。POST は本文の読み取りと競合させない。
	if r.Method == "GET" || r.Method == "HEAD" {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			_, _ = io.Copy(io.Discard, reader)
			s.mu.Lock()
			cancel := s.connections[conn]
			s.mu.Unlock()
			if cancel != nil {
				cancel()
			}
		}()
	}
	s.handle(&responseConn{Conn: conn}, r)
	// POST の本文を読み残したまま Close すると、Windows は応答より先に RST を送ることがある
	// (上限を超える本文を断ったときなど)。応答を半閉鎖で届け、残りは大きさと時間を限って捨てる。
	if r.Method == "POST" {
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		_, _ = io.Copy(io.Discard, io.LimitReader(reader, 4*1024*1024))
	}
}

func statusText(status int) string {
	if status == 413 {
		return "Payload Too Large"
	}
	return http.StatusText(status)
}
func headers(conn net.Conn, status int, h http.Header) {
	if response, ok := conn.(*responseConn); ok {
		response.headersSent = true
	}
	if h == nil {
		h = http.Header{}
	}
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Connection", "close")
	_, _ = fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\n", status, statusText(status))
	_ = h.Write(conn)
	_, _ = io.WriteString(conn, "\r\n")
}
func send(conn net.Conn, r *http.Request, status int, h http.Header, body []byte) {
	if h == nil {
		h = http.Header{}
	}
	if body == nil && status >= 400 {
		body = []byte(statusText(status))
	}
	if status != 304 {
		h.Set("Content-Length", strconv.Itoa(len(body)))
	}
	if len(body) > 0 && h.Get("Content-Type") == "" {
		h.Set("Content-Type", "text/plain; charset=utf-8")
	}
	headers(conn, status, h)
	if (r == nil || r.Method != "HEAD") && status != 304 {
		_, _ = conn.Write(body)
	}
}
func content(mime, cache string) http.Header {
	h := http.Header{}
	if mime != "" {
		h.Set("Content-Type", mime)
	}
	if cache != "" {
		h.Set("Cache-Control", cache)
	}
	return h
}
func params(r *http.Request) url.Values {
	i := strings.IndexByte(r.RequestURI, '?')
	if i < 0 {
		return url.Values{}
	}
	value := url.Values{}
	for _, pair := range strings.Split(r.RequestURI[i+1:], "&") {
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		name := resolve.Unescape(strings.ReplaceAll(parts[0], "+", " "))
		v := ""
		if len(parts) > 1 {
			v = resolve.Unescape(strings.ReplaceAll(parts[1], "+", " "))
		}
		value.Add(name, v)
	}
	return value
}
func pathOf(r *http.Request) string {
	if i := strings.IndexAny(r.RequestURI, "?#"); i >= 0 {
		return r.RequestURI[:i]
	}
	return r.RequestURI
}

func (s *Server) handle(conn net.Conn, r *http.Request) {
	defer func() {
		if recover() != nil {
			if response, ok := conn.(*responseConn); ok && response.headersSent {
				_ = conn.Close()
			} else {
				s.reject(conn, r, 500)
			}
		}
	}()
	if r.Host == "" {
		s.reject(conn, r, 400)
		return
	}
	if r.Host != fmt.Sprintf("127.0.0.1:%d", s.Port) && r.Host != fmt.Sprintf("localhost:%d", s.Port) {
		s.reject(conn, r, 403)
		return
	}
	site := strings.Join(r.Header.Values("Sec-Fetch-Site"), ", ")
	if site != "" && site != "none" && site != "same-origin" {
		s.reject(conn, r, 403)
		return
	}
	path := pathOf(r)
	base := "/" + s.Token
	// token は最初の区切りまでを取り出し、どこまで合ったかで時間が変わらない比べ方をする。
	segment, rest := "", ""
	if strings.HasPrefix(path, "/") {
		segment = path[1:]
		if i := strings.IndexByte(segment, '/'); i >= 0 {
			segment, rest = segment[:i], segment[i:]
		}
	}
	authorized := subtle.ConstantTimeCompare([]byte(segment), []byte(s.Token)) == 1
	jumpRoute := s.opts.OnJump != nil && authorized && rest == "/_/jump"
	editRoute := s.opts.Edit != nil && authorized && rest == "/_/edit"
	editGet := s.opts.Edit != nil && authorized && (rest == "/_/edit/tables" || rest == "/_/edit/source")
	reviewRoute := s.opts.Review != nil && authorized && rest == "/_/review/prepare"
	reviewGet := s.opts.Review != nil && authorized && (rest == "/_/review/tables" || rest == "/_/review/source")
	launchRoute := s.opts.Review != nil && s.opts.Launch != nil && authorized && rest == "/_/review/launch"
	if r.Method == "POST" && launchRoute {
		s.launchReview(conn, r)
		return
	}
	if r.Method == "POST" && reviewRoute {
		s.prepareReview(conn, r)
		return
	}
	if (r.Method == "GET" || r.Method == "HEAD") && reviewGet {
		s.readSource(conn, r, rest, s.opts.Review)
		return
	}
	if r.Method == "POST" && editRoute || (r.Method == "GET" || r.Method == "HEAD") && editGet {
		s.edit(conn, r, rest)
		return
	}
	if r.Method == "POST" && jumpRoute {
		s.jump(conn, r)
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		h := http.Header{"Allow": []string{"GET, HEAD"}}
		if jumpRoute || editRoute || reviewRoute || launchRoute {
			h.Set("Allow", "GET, HEAD, POST")
		}
		send(conn, r, 405, h, nil)
		return
	}
	if authorized && rest == "" {
		query := ""
		if i := strings.IndexByte(r.RequestURI, '?'); i >= 0 {
			query = r.RequestURI[i:]
		}
		send(conn, r, 301, http.Header{"Location": []string{base + "/" + query}}, []byte{})
		return
	}
	if !authorized {
		s.reject(conn, r, 404)
		return
	}
	rel := rest[1:]
	if rel == "" {
		p := params(r)
		config := map[string]any{"assets": nil, "notice": s.opts.AssetsNotice, "hasDocument": s.opts.Session.HasDocument(), "currentDocument": !p.Has("doc") || s.opts.Session.IsCurrentDocument(p.Get("doc"))}
		if s.opts.Assets != nil {
			config["assets"] = s.opts.Assets.PageConfig()
		}
		// directory は埋め込んだ設定から取り、doc のあるタブでは一覧にしない。
		config["list"] = p.Has("list") || (!p.Has("doc") && strings.Contains(s.opts.Page.HTML, `"directory":true`))
		h := content("text/html; charset=utf-8", "no-store")
		h.Set("Content-Security-Policy", CSP)
		send(conn, r, 200, h, []byte(s.opts.Page.Configure(config)))
		return
	}
	for route, item := range map[string]struct{ body, mime string }{"_/preview.js": {s.opts.Page.JS, "text/javascript; charset=utf-8"}, "_/copy-code.js": {s.opts.Page.CopyCodeJS, "text/javascript; charset=utf-8"}, "_/edit-grid.js": {s.opts.Page.EditGridJS, "text/javascript; charset=utf-8"}, "_/edit-targets.js": {s.opts.Page.EditTargetsJS, "text/javascript; charset=utf-8"}, "_/edit.js": {s.opts.Page.EditJS, "text/javascript; charset=utf-8"}, "_/review.js": {s.opts.Page.ReviewJS, "text/javascript; charset=utf-8"}, "_/preview.css": {s.opts.Page.CSS, "text/css; charset=utf-8"}, "_/fallback.css": {s.opts.Page.Fallback, "text/css; charset=utf-8"}} {
		if rel == route {
			send(conn, r, 200, content(item.mime, "no-store"), []byte(item.body))
			return
		}
	}
	if strings.HasPrefix(rel, "_/lib/") {
		found, ok := s.opts.Libraries.Read(strings.TrimPrefix(rel, "_/lib/"))
		if !ok {
			send(conn, r, 404, nil, nil)
			return
		}
		mime := map[string]string{".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8", ".woff2": "font/woff2"}[filepath.Ext(rel)]
		if mime == "" {
			mime = "text/plain; charset=utf-8"
		}
		h := content(mime, "no-cache")
		h.Set("ETag", `"`+found.SHA256+`"`)
		h.Set("Content-Security-Policy", "sandbox")
		s.cached(conn, r, h, found.Body)
		return
	}
	if strings.HasPrefix(rel, "_/plantuml/") {
		m := regexp.MustCompile(`^_/plantuml/([0-9a-f]{64})\.svg$`).FindStringSubmatch(rel)
		if m == nil {
			send(conn, r, 404, nil, nil)
		} else {
			s.diagram(conn, r, m[1])
		}
		return
	}
	if strings.HasPrefix(rel, "_/assets/") {
		if s.opts.Assets != nil {
			prefix := "_/assets/" + s.opts.Assets.ID() + "/"
			if strings.HasPrefix(rel, prefix) {
				body, mime, ok := s.opts.Assets.File(strings.TrimPrefix(rel, prefix))
				if ok {
					sum := sha256.Sum256(body)
					h := content(mime, "private, max-age=31536000, immutable")
					h.Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
					h.Set("Content-Security-Policy", "sandbox")
					s.cached(conn, r, h, body)
					return
				}
			}
		}
		send(conn, r, 404, nil, nil)
		return
	}
	if rel == "_/links" {
		s.checkLinks(conn, r)
		return
	}
	if rel == "_/list" {
		b, e := Stringify(documents.ListMarkdown(s.opts.Session.Root()))
		if e != nil {
			send(conn, r, 500, nil, nil)
		} else {
			send(conn, r, 200, content("application/json; charset=utf-8", "no-store"), b)
		}
		return
	}
	if rel == "_/events" {
		if r.Method == "HEAD" {
			send(conn, r, 200, content("text/event-stream; charset=utf-8", "no-store"), []byte{})
		} else {
			s.events(conn, r)
		}
		return
	}
	if s.opts.Review != nil && isReviewPath(r) {
		s.reject(conn, r, 404)
		return
	}
	root := s.opts.Session.Root()
	if root == "" {
		send(conn, r, 404, nil, nil)
		return
	}
	result := resolve.Resolve(root, rel)
	if result.Status != 200 {
		send(conn, r, result.Status, nil, nil)
		return
	}
	s.file(conn, r, result.Path)
}
func (s *Server) cached(conn net.Conn, r *http.Request, h http.Header, body []byte) {
	status := 200
	if r.Header.Get("If-None-Match") == h.Get("ETag") {
		status = 304
	}
	send(conn, r, status, h, body)
}

var mimeTypes = map[string]string{"png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg", "gif": "image/gif", "webp": "image/webp", "avif": "image/avif", "svg": "image/svg+xml", "bmp": "image/bmp", "ico": "image/x-icon", "mp4": "video/mp4", "webm": "video/webm", "mov": "video/quicktime", "mp3": "audio/mpeg", "ogg": "audio/ogg", "wav": "audio/wav", "pdf": "application/pdf", "txt": "text/plain; charset=utf-8", "md": "text/plain; charset=utf-8", "csv": "text/plain; charset=utf-8"}

func (s *Server) file(conn net.Conn, r *http.Request, path string) {
	f, e := fileopen.Open(path)
	if e != nil {
		send(conn, r, 404, nil, nil)
		return
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		send(conn, r, 404, nil, nil)
		return
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	mime := mimeTypes[ext]
	if mime == "" {
		mime = "application/octet-stream"
	}
	h := content(mime, "no-cache")
	ns := st.ModTime().UnixNano()
	h.Set("ETag", fmt.Sprintf(`W/"%d-%d-%d"`, st.Size(), ns/1e9, ns%1e9))
	if ext != "pdf" {
		h.Set("Content-Security-Policy", "sandbox")
	}
	if r.Header.Get("If-None-Match") == h.Get("ETag") {
		send(conn, r, 304, h, []byte{})
		return
	}
	h.Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	headers(conn, 200, h)
	if r.Method != "HEAD" {
		_, _ = io.CopyN(conn, f, st.Size())
	}
}
func (s *Server) diagram(conn net.Conn, r *http.Request, id string) {
	source, ok := s.opts.Session.PlantumlSource(id)
	if s.opts.Plantuml == nil || !ok {
		send(conn, r, 404, nil, nil)
		return
	}
	svg, e := s.opts.Plantuml.Render(r.Context(), source)
	if e != nil {
		send(conn, r, 502, content("text/plain; charset=utf-8", "no-store"), []byte("PlantUML の図を描けませんでした\n"))
		return
	}
	h := content("image/svg+xml", "private, max-age=3600")
	h.Set("Content-Security-Policy", "sandbox")
	send(conn, r, 200, h, svg)
}
func SafeInteger(v any) bool {
	n, ok := v.(float64)
	return ok && n >= 1 && n <= 9007199254740991 && n == float64(int64(n))
}
func (s *Server) jump(conn net.Conn, r *http.Request) {
	origin := strings.Join(r.Header.Values("Origin"), ", ")
	if origin != fmt.Sprintf("http://127.0.0.1:%d", s.Port) && origin != fmt.Sprintf("http://localhost:%d", s.Port) {
		send(conn, r, 403, nil, nil)
		return
	}
	v, e := postJSON(conn, r, 1024)
	if e != nil {
		send(conn, r, e.Status, nil, nil)
		return
	}
	if !SafeInteger(v["line"]) {
		send(conn, r, 400, nil, nil)
		return
	}
	p := params(r)
	if p.Has("doc") && !s.opts.Session.IsCurrentDocument(p.Get("doc")) {
		send(conn, r, 403, nil, nil)
		return
	}
	s.opts.OnJump(int64(v["line"].(float64)))
	send(conn, r, 204, nil, []byte{})
}

func event(name string, data session.Data) string {
	b, _ := Stringify(data)
	return "event: " + name + "\ndata: " + string(b) + "\n\n"
}
func (s *Server) remove(c *client) {
	c.once.Do(func() {
		close(c.done)
		_ = c.conn.Close()
		s.counts.Lock()
		defer s.counts.Unlock()
		s.mu.Lock()
		existed := s.clients[c]
		delete(s.clients, c)
		count := len(s.clients)
		s.mu.Unlock()
		if existed && s.opts.OnClientCount != nil {
			s.opts.OnClientCount(count)
		}
	})
}
func (s *Server) write(c *client, text string) {
	c.mu.Lock()
	if c.bytes+len(text) > MaxQueue {
		c.mu.Unlock()
		s.remove(c)
		return
	}
	select {
	case <-c.done:
		c.mu.Unlock()
		return
	default:
	}
	c.queue = append(c.queue, text)
	c.bytes += len(text)
	c.mu.Unlock()
	select {
	case c.signal <- struct{}{}:
	default:
	}
}
func (s *Server) events(conn net.Conn, r *http.Request) {
	h := content("text/event-stream; charset=utf-8", "no-store")
	h.Set("Transfer-Encoding", "chunked")
	headers(conn, 200, h)
	p := params(r)
	c := &client{conn: conn, signal: make(chan struct{}, 1), done: make(chan struct{})}
	if p.Has("doc") {
		doc := p.Get("doc")
		c.doc = &doc
	}
	added := false
	add := func(render, scroll session.Data) {
		s.counts.Lock()
		defer s.counts.Unlock()
		s.mu.Lock()
		if s.closing {
			s.mu.Unlock()
			return
		}
		initial := []string{"retry: 1000\n\n"}
		if c.doc == nil {
			last := map[string]session.Data{"render": render, "scroll": scroll}
			for k, v := range s.remembered {
				last[k] = v
			}
			for _, name := range []string{"render", "scroll", "stop"} {
				if last[name] != nil {
					initial = append(initial, event(name, last[name]))
				}
			}
		}
		c.mu.Lock()
		for _, frame := range initial {
			c.queue = append(c.queue, frame)
			c.bytes += len(frame)
		}
		c.mu.Unlock()
		s.clients[c] = true
		added = true
		count := len(s.clients)
		s.mu.Unlock()
		if s.opts.OnClientCount != nil {
			s.opts.OnClientCount(count)
		}
	}
	if c.doc == nil {
		if snapshots, ok := s.opts.Session.(interface {
			WithSnapshot(func(session.Data, session.Data))
		}); ok {
			snapshots.WithSnapshot(add)
		} else {
			render, scroll := s.opts.Session.Last()
			add(render, scroll)
		}
	} else {
		add(nil, nil)
	}
	if !added {
		return
	}
	defer s.remove(c)
	c.mu.Lock()
	tooLarge := c.bytes > MaxQueue
	c.mu.Unlock()
	if tooLarge {
		return
	}
	select {
	case c.signal <- struct{}{}:
	default:
	}
	if c.doc != nil {
		release := s.opts.Session.SubscribeDocument(*c.doc, func(name string, data session.Data) {
			if name == "render" {
				copy := session.Data{}
				for k, v := range data {
					copy[k] = v
				}
				copy["currentDocument"] = s.opts.Session.IsCurrentDocument(*c.doc)
				data = copy
			}
			s.write(c, event(name, data))
		})
		defer release()
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-c.done:
			return
		case <-c.signal:
		}
		for {
			c.mu.Lock()
			if len(c.queue) == 0 {
				ending := c.ending
				c.mu.Unlock()
				if ending {
					_, _ = io.WriteString(conn, "0\r\n\r\n")
					return
				}
				break
			}
			text := c.queue[0]
			c.queue = c.queue[1:]
			c.mu.Unlock()
			if _, e := fmt.Fprintf(conn, "%x\r\n%s\r\n", len(text), text); e != nil {
				return
			}
			c.mu.Lock()
			c.bytes -= len(text)
			c.mu.Unlock()
		}
	}
}
func (s *Server) ClientCount() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.clients) }
func (s *Server) Broadcast(name string, data session.Data) error {
	if name != "render" && name != "scroll" && name != "stop" && name != "reload" {
		return errors.New("未対応のイベントです")
	}
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return nil
	}
	if name == "render" || name == "scroll" || name == "stop" {
		s.remembered[name] = data
	}
	if name == "render" {
		delete(s.remembered, "stop")
	}
	clients := []*client{}
	for c := range s.clients {
		if (name == "render" || name == "scroll") && c.doc != nil {
			continue
		}
		clients = append(clients, c)
	}
	s.mu.Unlock()
	text := event(name, data)
	for _, c := range clients {
		s.write(c, text)
	}
	return nil
}
func (s *Server) Close(reason string) error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closing = true
		clients := []*client{}
		for c := range s.clients {
			clients = append(clients, c)
		}
		s.mu.Unlock()
		if s.offRoot != nil {
			s.offRoot()
		}
		if reason != "exit" {
			reason = "stop"
		}
		for _, c := range clients {
			s.write(c, event("stop", session.Data{"reason": reason}))
			c.mu.Lock()
			c.ending = true
			c.mu.Unlock()
			select {
			case c.signal <- struct{}{}:
			default:
			}
		}
		_ = s.listener.Close()
		force := time.AfterFunc(250*time.Millisecond, func() {
			s.mu.Lock()
			for conn, cancel := range s.connections {
				cancel()
				_ = conn.Close()
			}
			s.mu.Unlock()
		})
		s.wg.Wait()
		force.Stop()
		if s.opts.Plantuml != nil {
			_ = s.opts.Plantuml.Close()
		}
		close(s.done)
	})
	<-s.done
	return nil
}
