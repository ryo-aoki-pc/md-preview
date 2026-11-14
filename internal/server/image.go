package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/ryo-aoki-pc/md-tools/internal/edit"
)

func (s *Server) editImage(conn net.Conn, r *http.Request, doc *string) {
	value := params(r).Get("render")
	render, err := strconv.ParseUint(value, 10, 64)
	if err != nil || render == 0 || !regexp.MustCompile(`^[0-9]+$`).MatchString(value) {
		editError(conn, r, &edit.Error{Status: 400, Code: "invalid", Message: "描画の番号は正の整数で指定してください。"})
		return
	}
	if r.Header.Get("Content-Type") != "image/png" {
		editError(conn, r, &edit.Error{Status: 415, Code: "media", Message: "Content-Type は image/png にしてください。"})
		return
	}
	large := &edit.Error{Status: 413, Code: "large", Message: "画像が 8 MiB を超えています。"}
	if r.ContentLength > edit.MaxImageBytes {
		editError(conn, r, large)
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(300 * time.Second))
	body, err := io.ReadAll(io.LimitReader(r.Body, edit.MaxImageBytes+1))
	if len(body) > edit.MaxImageBytes {
		editError(conn, r, large)
		return
	}
	if err != nil {
		editError(conn, r, &edit.Error{Status: 400, Code: "invalid", Message: "画像の本文を読み取れません。"})
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 60*time.Second)
	defer cancel()
	path, e := s.opts.Edit.SaveImage(ctx, doc, render, body)
	if e != nil {
		editError(conn, r, e)
		return
	}
	editJSON(conn, r, 200, path)
}
