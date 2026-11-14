package session

import (
	"context"
	"errors"

	"github.com/ryo-aoki-pc/md-tools/internal/links"
	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
)

var ErrStaleLinks = errors.New("点検する描画が古くなっています。")

// CheckLinks は成功した描画を読み取るだけで、文書の購読や編集の状態を変えない。
func (s *Session) CheckLinks(ctx context.Context, doc *string, render uint64) (links.Result, error) {
	src, ok := s.Source(doc)
	if !ok || src.Render != render || src.Stale {
		return links.Result{}, ErrStaleLinks
	}
	rendered := pipeline.Result{HTML: src.HTML, Headings: src.Headings, Source: src.Lines}
	result, err := links.Check(ctx, rendered, src.Root, FileContext(src.Root, src.File, src.Dir, s.opts.Plantuml), s.opts.Renderer)
	current, ok := s.Source(doc)
	if !ok || current.Render != src.Render || current.Stale {
		return links.Result{}, ErrStaleLinks
	}
	result.Render = src.Render
	return result, err
}
