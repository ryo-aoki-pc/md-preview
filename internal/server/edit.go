package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ryo-aoki-pc/md-tools/internal/edit"
)

// tagPattern は、原文を取得する対象の要素名の形。
var tagPattern = regexp.MustCompile(`^[a-z0-9]+$`)

type sourceReader interface {
	Tables(context.Context, *string, uint64) (any, *edit.Error)
	Text(context.Context, *string, uint64, string, edit.Range) (any, *edit.Error)
	HTMLText(context.Context, *string, uint64, *edit.HTMLBoundary, *edit.HTMLBoundary) (any, *edit.Error)
}
type Editor interface {
	sourceReader
	Config() map[string]any
	Apply(context.Context, *string, map[string]any) (any, *edit.Error)
	SaveImage(context.Context, *string, uint64, []byte) (string, *edit.Error)
}

func editJSON(conn net.Conn, r *http.Request, status int, value any) {
	b, err := Stringify(value)
	if err != nil {
		status = 500
		b = []byte(`{"error":{"code":"internal","message":"応答を作れません。"}}`)
	}
	send(conn, r, status, content("application/json; charset=utf-8", "no-store"), b)
}
func editError(conn net.Conn, r *http.Request, e *edit.Error) {
	// 原文の読み取りと境の検査は編集と共有するので、コメントの経路では形の誤りの文だけをコメントの文に替える。
	message := e.Message
	if e.Code == "invalid" && message == "編集の要求の形が正しくありません。" && isReviewPath(r) {
		message = reviewInvalid().Message
	}
	detail := map[string]any{"code": e.Code, "message": message}
	// コメントの経路では、断った対象の番号（0 始まり）を返し、ページがそのカードに印を付ける。
	if isReviewPath(r) && e.Index != nil {
		detail["index"] = *e.Index
	}
	// 断った対象が複数あれば、全部の番号と理由を返す。
	if isReviewPath(r) && len(e.Failures) > 0 {
		failures := make([]map[string]any, 0, len(e.Failures))
		for _, f := range e.Failures {
			failures = append(failures, map[string]any{"index": f.Index, "code": f.Code, "message": f.Message})
		}
		detail["failures"] = failures
	}
	value := map[string]any{"error": detail}
	if e.Outer != nil {
		value["outer"] = e.Outer
	}
	editJSON(conn, r, e.Status, value)
}
func isEditPath(r *http.Request) bool {
	p := pathOf(r)
	return strings.HasSuffix(p, "/_/edit") || strings.HasSuffix(p, "/_/edit/tables") || strings.HasSuffix(p, "/_/edit/source") || strings.HasSuffix(p, "/_/edit/image")
}
func (s *Server) reject(conn net.Conn, r *http.Request, status int) {
	review := s.opts.Review != nil && isReviewPath(r)
	if s.opts.Edit != nil && isEditPath(r) || review {
		message := map[int]string{400: "要求の形が正しくありません。", 403: "この要求は許可されていません。", 404: "編集の経路がありません。", 500: "編集の処理を終えられません。"}[status]
		if review && status == 404 {
			message = "コメントの経路がありません。"
		}
		if review && status == 500 {
			message = "コメントの処理を終えられません。"
		}
		editError(conn, r, &edit.Error{Status: status, Code: map[int]string{400: "invalid", 403: "forbidden", 404: "missing", 500: "internal"}[status], Message: message})
		return
	}
	send(conn, r, status, nil, nil)
}

// postJSON は本文を上限まで読み、jump・edit・review が同じ型と符号で検査する。
func postJSON(conn net.Conn, r *http.Request, limit int64) (map[string]any, *edit.Error) {
	_ = conn.SetReadDeadline(time.Now().Add(300 * time.Second))
	if !regexp.MustCompile(`(?i)^application/json(?:\s*;[^\r\n]*)?$`).MatchString(r.Header.Get("Content-Type")) {
		return nil, &edit.Error{Status: 415, Code: "media", Message: "Content-Type は application/json にしてください。"}
	}
	if r.ContentLength > limit {
		return nil, &edit.Error{Status: 413, Code: "large", Message: "要求の本文が大きすぎます。"}
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if int64(len(body)) > limit {
		return nil, &edit.Error{Status: 413, Code: "large", Message: "要求の本文が大きすぎます。"}
	}
	parsed, parseErr := DecodeJSON(body)
	value, _ := parsed.(map[string]any)
	if err != nil || !utf8.Valid(body) || parseErr != nil || value == nil {
		return nil, &edit.Error{Status: 400, Code: "invalid", Message: "本文は UTF-8 の JSON オブジェクトで指定してください。"}
	}
	return value, nil
}
func (s *Server) edit(conn net.Conn, r *http.Request, route string) {
	p := params(r)
	var doc *string
	if p.Has("doc") {
		value := p.Get("doc")
		doc = &value
	}
	if r.Method == "POST" {
		site := strings.Join(r.Header.Values("Sec-Fetch-Site"), ", ")
		origin := strings.Join(r.Header.Values("Origin"), ", ")
		if (site != "" && site != "same-origin") || origin != "http://"+r.Host {
			s.reject(conn, r, 403)
			return
		}
		if route == "/_/edit/image" {
			s.editImage(conn, r, doc)
			return
		}
		body, e := postJSON(conn, r, 2*1024*1024)
		if e != nil {
			editError(conn, r, e)
			return
		}
		// 本文を受け取った後は、タブの切断や終了によって入れ替えを中断しない。
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 60*time.Second)
		defer cancel()
		result, e := s.opts.Edit.Apply(ctx, doc, body)
		if e != nil {
			editError(conn, r, e)
		} else {
			editJSON(conn, r, 200, result)
		}
		return
	}
	s.readSource(conn, r, route, s.opts.Edit)
}

// readSource は、編集とコメントで原文の読み取りと位置の検査を共有する。
func (s *Server) readSource(conn net.Conn, r *http.Request, route string, reader sourceReader) {
	p := params(r)
	doc := requestDocument(r)
	value := p.Get("render")
	render, err := strconv.ParseUint(value, 10, 64)
	if err != nil || render == 0 || !regexp.MustCompile(`^[0-9]+$`).MatchString(value) {
		editError(conn, r, &edit.Error{Status: 400, Code: "invalid", Message: "描画の番号は正の整数で指定してください。"})
		return
	}
	var result any
	var e *edit.Error
	if strings.HasSuffix(route, "/tables") {
		result, e = reader.Tables(r.Context(), doc, render)
	} else {
		tag := p.Get("tag")
		if !tagPattern.MatchString(tag) {
			editError(conn, r, &edit.Error{Status: 400, Code: "invalid", Message: "編集の要求の形が正しくありません。"})
			return
		}
		if tag == "html" {
			var prev, next *edit.HTMLBoundary
			prev, next, e = htmlBoundaries(p)
			if e == nil {
				result, e = reader.HTMLText(r.Context(), doc, render, prev, next)
			}
		} else {
			var value edit.Range
			value, e = edit.ParseSourceRange(tag, p.Get("range"))
			if e == nil {
				result, e = reader.Text(r.Context(), doc, render, tag, value)
			}
		}
	}
	if e != nil {
		editError(conn, r, e)
	} else {
		editJSON(conn, r, 200, result)
	}
}

func requestDocument(r *http.Request) *string {
	p := params(r)
	if !p.Has("doc") {
		return nil
	}
	doc := p.Get("doc")
	return &doc
}

// 境は要素名と位置を組で受け取り、先頭と末尾では組ごと省く。
func htmlBoundaries(p url.Values) (*edit.HTMLBoundary, *edit.HTMLBoundary, *edit.Error) {
	invalid := func() *edit.Error {
		return &edit.Error{Status: 400, Code: "invalid", Message: "編集の要求の形が正しくありません。"}
	}
	if p.Has("range") {
		return nil, nil, invalid()
	}
	parse := func(prefix string) (*edit.HTMLBoundary, *edit.Error) {
		t, r := prefix+"Tag", prefix+"Range"
		if !p.Has(t) && !p.Has(r) {
			return nil, nil
		}
		if len(p[t]) != 1 || len(p[r]) != 1 {
			return nil, invalid()
		}
		return parseHTMLBoundary(p.Get(t), p.Get(r))
	}
	prev, e := parse("prev")
	if e != nil {
		return nil, nil, e
	}
	next, e := parse("next")
	return prev, next, e
}

func parseHTMLBoundary(tag, sourceRange string) (*edit.HTMLBoundary, *edit.Error) {
	if !tagPattern.MatchString(tag) {
		return nil, &edit.Error{Status: 400, Code: "invalid", Message: "編集の要求の形が正しくありません。"}
	}
	value, e := edit.ParseSourceRange(tag, sourceRange)
	if e != nil {
		return nil, e
	}
	return &edit.HTMLBoundary{Tag: tag, Range: value}, nil
}
