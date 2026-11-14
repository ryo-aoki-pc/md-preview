package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
)

func TestLinksRejectChangedRender(t *testing.T) {
	root := sourceRoot(t)
	file := filepath.Join(root, "main.md")
	for _, name := range []string{"main.md", "other.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}
	started, release := make(chan struct{}), make(chan struct{})
	s := newSession(t, Options{Root: root, Renderer: renderFunc(func(ctx context.Context, text string, c pipeline.Context) (pipeline.Result, error) {
		if text == "other.md" {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return pipeline.Result{}, ctx.Err()
			}
		}
		return pipeline.Result{HTML: text, Headings: []pipeline.Heading{{ID: "title"}}}, nil
	})})
	setSourceDocument(t, s, Document{Path: file, Text: `<a href="other.md#title">題</a>`, Rev: 1})
	src, _ := s.Source(nil)
	if _, err := s.CheckLinks(context.Background(), nil, src.Render+1); !errors.Is(err, ErrStaleLinks) {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.CheckLinks(context.Background(), nil, src.Render); done <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("別の文書の点検が始まりませんでした")
	}
	setSourceDocument(t, s, Document{Path: file, Text: "新しい本文", Rev: 2})
	close(release)
	select {
	case err := <-done:
		if !errors.Is(err, ErrStaleLinks) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("古い描画の点検が終わりませんでした")
	}
}
