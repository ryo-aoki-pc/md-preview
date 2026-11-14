package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strconv"

	"github.com/ryo-aoki-pc/md-tools/internal/documents"
	"github.com/ryo-aoki-pc/md-tools/internal/links"
	"github.com/ryo-aoki-pc/md-tools/internal/resolve"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

type linkChecker interface {
	CheckLinks(context.Context, *string, uint64) (links.Result, error)
}

var linksRenderPattern = regexp.MustCompile(`^[0-9]+$`)

func (s *Server) checkLinks(conn net.Conn, r *http.Request) {
	p := params(r)
	value := p.Get("render")
	render, err := strconv.ParseUint(value, 10, 64)
	if len(p["render"]) != 1 || len(p["doc"]) > 1 || err != nil || render == 0 || !linksRenderPattern.MatchString(value) {
		editJSON(conn, r, 400, map[string]any{"error": map[string]string{"code": "invalid", "message": "描画の番号は正の整数で指定してください。"}})
		return
	}
	doc := requestDocument(r)
	if doc != nil {
		root := s.opts.Session.Root()
		if root == "" {
			send(conn, r, 404, nil, nil)
			return
		}
		located := resolve.Resolve(root, documents.Encode(*doc))
		if located.Status != 200 {
			send(conn, r, located.Status, nil, nil)
			return
		}
	}
	checker, ok := s.opts.Session.(linkChecker)
	if !ok {
		send(conn, r, 404, nil, nil)
		return
	}
	result, err := checker.CheckLinks(r.Context(), doc, render)
	if errors.Is(err, session.ErrStaleLinks) {
		editJSON(conn, r, 409, map[string]any{"error": map[string]string{"code": "stale", "message": "描画が変わりました。新しい描画で点検してください。"}})
	} else if err != nil {
		editJSON(conn, r, 503, map[string]any{"error": map[string]string{"code": "unavailable", "message": "リンクを点検できませんでした。"}})
	} else {
		editJSON(conn, r, 200, result)
	}
}
