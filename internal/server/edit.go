package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ryo-aoki-pc/md-preview/internal/edit"
)

type Editor interface {
	Config() map[string]any
	Tables(context.Context, *string, uint64) (any, *edit.Error)
	Text(context.Context, *string, uint64, edit.Range) (any, *edit.Error)
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
	editJSON(conn, r, e.Status, map[string]any{"error": map[string]string{"code": e.Code, "message": e.Message}})
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
		var value edit.Range
		value, e = edit.ParseRange(p.Get("range"))
		if e == nil {
			result, e = s.opts.Edit.Text(r.Context(), doc, render, value)
		}
	}
	if e != nil {
		editError(conn, r, e)
	} else {
		editJSON(conn, r, 200, result)
	}
}
