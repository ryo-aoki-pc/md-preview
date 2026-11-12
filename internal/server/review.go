package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ryo-aoki-pc/md-tools/internal/edit"
	"github.com/ryo-aoki-pc/md-tools/internal/review"
	"github.com/ryo-aoki-pc/md-tools/internal/vscode"
)

type Reviewer interface {
	sourceReader
	Review(context.Context, *string, uint64, []edit.ReviewTarget) (edit.ReviewDocument, []edit.ReviewItem, *edit.Error)
}

// isReviewPath はコメントの口だけを見分ける。ほかのファイルの経路（docs/_/review/a.md など）には当てない。
func isReviewPath(r *http.Request) bool {
	p := pathOf(r)
	for _, route := range []string{"/_/review/prepare", "/_/review/tables", "/_/review/source", "/_/review/launch"} {
		if strings.HasSuffix(p, route) {
			return true
		}
	}
	return false
}

func reviewInvalid() *edit.Error {
	return &edit.Error{Status: 400, Code: "invalid", Message: "コメントの要求の形が正しくありません。"}
}

func onlyKeys(value map[string]any, keys ...string) bool {
	for key := range value {
		found := false
		for _, allowed := range keys {
			found = found || key == allowed
		}
		if !found {
			return false
		}
	}
	return true
}

func reviewString(value any, trim bool) (string, bool) {
	text, ok := value.(string)
	if !ok || !utf8.ValidString(text) || strings.ContainsFunc(text, func(c rune) bool { return unicode.IsControl(c) && c != '\t' && c != '\n' }) {
		return "", false
	}
	if trim {
		text = strings.TrimSpace(text)
	}
	n := utf8.RuneCountInString(text)
	return text, n >= 1 && n <= 2000
}

func reviewTarget(value any) (edit.ReviewTarget, *edit.Error) {
	var target edit.ReviewTarget
	m, ok := value.(map[string]any)
	if !ok {
		return target, reviewInvalid()
	}
	target.Kind, _ = m["kind"].(string)
	switch target.Kind {
	case "block":
		if len(m) != 3 || !onlyKeys(m, "kind", "tag", "range") {
			return target, reviewInvalid()
		}
		tag, tagOK := m["tag"].(string)
		r, rangeOK := m["range"].(string)
		if !tagOK || !rangeOK || !tagPattern.MatchString(tag) {
			return target, reviewInvalid()
		}
		var e *edit.Error
		target.Tag = tag
		target.Range, e = edit.ParseSourceRange(tag, r)
		return target, e
	case "html":
		if !onlyKeys(m, "kind", "prev", "next") {
			return target, reviewInvalid()
		}
		for name, dest := range map[string]**edit.HTMLBoundary{"prev": &target.Prev, "next": &target.Next} {
			value, present := m[name]
			if !present {
				continue
			}
			boundary, ok := value.(map[string]any)
			if !ok || len(boundary) != 2 || !onlyKeys(boundary, "tag", "range") {
				return target, reviewInvalid()
			}
			tag, tagOK := boundary["tag"].(string)
			r, rangeOK := boundary["range"].(string)
			if !tagOK || !rangeOK {
				return target, reviewInvalid()
			}
			var e *edit.Error
			*dest, e = parseHTMLBoundary(tag, r)
			if e != nil {
				return target, e
			}
		}
	case "cell":
		if len(m) != 4 || !onlyKeys(m, "kind", "table", "row", "col") {
			return target, reviewInvalid()
		}
		for name, dest := range map[string]*int{"table": &target.Table, "row": &target.Row, "col": &target.Col} {
			n, ok := m[name].(float64)
			if !ok || n < 0 || n > 2147483647 || n != float64(int64(n)) {
				return target, reviewInvalid()
			}
			*dest = int(n)
		}
	default:
		return target, reviewInvalid()
	}
	return target, nil
}

func parseReview(body map[string]any) (uint64, []edit.ReviewTarget, []review.Comment, *edit.Error) {
	values, ok := body["comments"].([]any)
	if len(body) != 2 || !onlyKeys(body, "render", "comments") || !SafeInteger(body["render"]) || !ok || len(values) < 1 || len(values) > 50 {
		return 0, nil, nil, reviewInvalid()
	}
	targets := make([]edit.ReviewTarget, 0, len(values))
	comments := make([]review.Comment, 0, len(values))
	for _, value := range values {
		m, ok := value.(map[string]any)
		if !ok || !onlyKeys(m, "target", "selection", "text") {
			return 0, nil, nil, reviewInvalid()
		}
		target, e := reviewTarget(m["target"])
		if e != nil {
			return 0, nil, nil, e
		}
		text, ok := reviewString(m["text"], true)
		if !ok {
			return 0, nil, nil, reviewInvalid()
		}
		selection := ""
		if value, present := m["selection"]; present {
			selection, ok = reviewString(value, false)
			if !ok {
				return 0, nil, nil, reviewInvalid()
			}
		}
		targets = append(targets, target)
		comments = append(comments, review.Comment{Selection: selection, Text: text})
	}
	return uint64(body["render"].(float64)), targets, comments, nil
}

func (s *Server) prepareReview(conn net.Conn, r *http.Request) {
	site := strings.Join(r.Header.Values("Sec-Fetch-Site"), ", ")
	origin := strings.Join(r.Header.Values("Origin"), ", ")
	if (site != "" && site != "same-origin") || origin != "http://"+r.Host {
		s.reject(conn, r, 403)
		return
	}
	// コメント 50 個・本文と選んだ文字の各 2,000 文字（UTF-8 で 1 文字 4 バイトまで）が入る大きさにする。
	body, e := postJSON(conn, r, 2*1024*1024)
	if e != nil {
		editError(conn, r, e)
		return
	}
	render, targets, comments, e := parseReview(body)
	if e != nil {
		editError(conn, r, e)
		return
	}
	doc := requestDocument(r)
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	document, items, e := s.opts.Review.Review(ctx, doc, render, targets)
	if e != nil {
		editError(conn, r, e)
		return
	}
	if len(items) != len(comments) {
		s.reject(conn, r, 500)
		return
	}
	for i, item := range items {
		comments[i].Item = review.Item{Kind: item.Kind, StartLine: item.StartLine, EndLine: item.EndLine, Excerpt: item.Excerpt, Table: item.Table, Row: item.Row, Col: item.Col}
	}
	prompt, err := review.BuildPrompt(document.RelPath, comments)
	if err != nil {
		code := "invalid"
		if errors.Is(err, review.ErrTooLarge) {
			code = "large"
		}
		editError(conn, r, &edit.Error{Status: 422, Code: code, Message: err.Error()})
		return
	}
	id := s.reviews.add(doc, document, prompt, time.Now())
	editJSON(conn, r, 200, map[string]any{"id": id, "prompt": prompt, "comments": len(comments), "bytes": len(prompt)})
}

// 文書の鍵では、編集中の文書と ?doc= の空文字列を区別する。
type reviewDocumentKey struct {
	Current bool
	Path    string
}

type preparedReview struct {
	Doc        reviewDocumentKey
	Document   edit.ReviewDocument
	PromptHash string
	Created    time.Time
	Launched   bool
	Launching  bool
}

var reviewID = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

func (s *Server) launchReview(conn net.Conn, r *http.Request) {
	site := strings.Join(r.Header.Values("Sec-Fetch-Site"), ", ")
	origin := strings.Join(r.Header.Values("Origin"), ", ")
	if (site != "" && site != "same-origin") || origin != "http://"+r.Host {
		s.reject(conn, r, 403)
		return
	}
	body, e := postJSON(conn, r, 1024)
	if e != nil {
		editError(conn, r, e)
		return
	}
	id, valid := body["id"].(string)
	if len(body) != 1 || !onlyKeys(body, "id") || !valid || !reviewID.MatchString(id) {
		editError(conn, r, reviewInvalid())
		return
	}
	id = strings.ToLower(id)
	entry, owner, e := s.reviews.beginLaunch(id, requestDocument(r), time.Now())
	if e != nil {
		editError(conn, r, e)
		return
	}
	if owner {
		// タブを閉じても起動の途中で止めない。期限は要求の取り消しから切り離す。
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 40*time.Second)
		defer cancel()
		err := s.opts.Launch(ctx, entry.Document.Root)
		s.reviews.finishLaunch(id, err)
		if err != nil {
			code := "launch"
			if errors.Is(err, vscode.ErrNotFound) {
				code = "notfound"
			}
			editError(conn, r, &edit.Error{Status: 503, Code: code, Message: vscode.Message(err)})
			return
		}
	}
	result := map[string]any{"launched": true}
	if !owner {
		result["again"] = true
	}
	editJSON(conn, r, 200, result)
}

func (p *reviewPreparations) beginLaunch(id string, doc *string, now time.Time) (preparedReview, bool, *edit.Error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expire(now)
	entry, ok := p.entries[id]
	if !ok {
		return entry, false, &edit.Error{Status: 404, Code: "expired", Message: "依頼文の期限が切れました。もう一度「依頼文を作る」を押してください。"}
	}
	key := reviewKey(doc)
	if entry.Doc != key {
		return entry, false, &edit.Error{Status: 409, Code: "changed", Message: "依頼文の文書が違います。もう一度「依頼文を作る」を押してください。"}
	}
	if entry.Launched || entry.Launching {
		return entry, false, nil
	}
	entry.Launching = true
	p.entries[id] = entry
	return entry, true, nil
}

func (p *reviewPreparations) finishLaunch(id string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry, ok := p.entries[id]; ok {
		entry.Launched = err == nil
		entry.Launching = false
		p.entries[id] = entry
	}
}

func reviewKey(doc *string) reviewDocumentKey {
	key := reviewDocumentKey{Current: doc == nil}
	if doc != nil {
		key.Path = *doc
	}
	return key
}

type reviewPreparations struct {
	mu      sync.Mutex
	entries map[string]preparedReview
}

func (p *reviewPreparations) expire(now time.Time) {
	for id, entry := range p.entries {
		if !now.Before(entry.Created.Add(10 * time.Minute)) {
			delete(p.entries, id)
		}
	}
}

func (p *reviewPreparations) add(doc *string, document edit.ReviewDocument, prompt string, now time.Time) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.entries == nil {
		p.entries = map[string]preparedReview{}
	}
	p.expire(now)
	if len(p.entries) >= 32 {
		oldest := ""
		for id, entry := range p.entries {
			if oldest == "" || entry.Created.Before(p.entries[oldest].Created) {
				oldest = id
			}
		}
		delete(p.entries, oldest)
	}
	key := reviewKey(doc)
	sum := sha256.Sum256([]byte(prompt))
	for {
		var bytes [16]byte
		_, _ = rand.Read(bytes[:])
		id := hex.EncodeToString(bytes[:])
		if _, exists := p.entries[id]; !exists {
			p.entries[id] = preparedReview{Doc: key, Document: document, PromptHash: hex.EncodeToString(sum[:]), Created: now}
			return id
		}
	}
}

func (p *reviewPreparations) get(id string, now time.Time) (preparedReview, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expire(now)
	entry, ok := p.entries[id]
	return entry, ok
}
