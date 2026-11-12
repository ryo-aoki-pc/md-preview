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

type Editor interface {
	Config() map[string]any
	Tables(context.Context, *string, uint64) (any, *edit.Error)
	Text(context.Context, *string, uint64, string, edit.Range) (any, *edit.Error)
	HTMLText(context.Context, *string, uint64, *edit.HTMLBoundary, *edit.HTMLBoundary) (any, *edit.Error)
	Apply(context.Context, *string, map[string]any) (any, *edit.Error)
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
	value := map[string]any{"error": map[string]string{"code": e.Code, "message": e.Message}}
	if e.Outer != nil {
		value["outer"] = e.Outer
	}
	editJSON(conn, r, e.Status, value)
}
func isEditPath(r *http.Request) bool {
	p := pathOf(r)
	return strings.HasSuffix(p, "/_/edit") || strings.HasSuffix(p, "/_/edit/tables") || strings.HasSuffix(p, "/_/edit/source")
}
func (s *Server) reject(conn net.Conn, r *http.Request, status int) {
	if s.opts.Edit != nil && isEditPath(r) {
		message := map[int]string{400: "要求の形が正しくありません。", 403: "この要求は許可されていません。", 404: "編集の経路がありません。", 500: "編集の処理を終えられません。"}[status]
		editError(conn, r, &edit.Error{Status: status, Code: map[int]string{400: "invalid", 403: "forbidden", 404: "missing", 500: "internal"}[status], Message: message})
		return
	}
	send(conn, r, status, nil, nil)
}

// postJSON は本文を上限まで読み、jump と edit が同じ型と符号で検査する。
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
	value := p.Get("render")
	render, err := strconv.ParseUint(value, 10, 64)
	if err != nil || render == 0 || !regexp.MustCompile(`^[0-9]+$`).MatchString(value) {
		editError(conn, r, &edit.Error{Status: 400, Code: "invalid", Message: "描画の番号は正の整数で指定してください。"})
		return
	}
	var result any
	var e *edit.Error
	if route == "/_/edit/tables" {
		result, e = s.opts.Edit.Tables(r.Context(), doc, render)
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
				result, e = s.opts.Edit.HTMLText(r.Context(), doc, render, prev, next)
			}
		} else {
			var value edit.Range
			value, e = edit.ParseSourceRange(tag, p.Get("range"))
			if e == nil {
				result, e = s.opts.Edit.Text(r.Context(), doc, render, tag, value)
			}
		}
	}
	if e != nil {
		editError(conn, r, e)
	} else {
		editJSON(conn, r, 200, result)
	}
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
		if len(p[t]) != 1 || len(p[r]) != 1 || !tagPattern.MatchString(p.Get(t)) {
			return nil, invalid()
		}
		value, e := edit.ParseSourceRange(p.Get(t), p.Get(r))
		if e != nil {
			return nil, e
		}
		return &edit.HTMLBoundary{Tag: p.Get(t), Range: value}, nil
	}
	prev, e := parse("prev")
	if e != nil {
		return nil, nil, e
	}
	next, e := parse("next")
	return prev, next, e
}
