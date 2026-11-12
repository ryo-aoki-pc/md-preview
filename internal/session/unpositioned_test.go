package session

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
)

func TestUnpositionedContextAndSource(t *testing.T) {
	root := sourceRoot(t)
	var calls int
	s := newSession(t, Options{Root: root, Plantuml: true, Renderer: renderFunc(func(ctx context.Context, text string, c pipeline.Context) (pipeline.Result, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok || c.File == nil || *c.File != "main.md" || c.Dir == nil || *c.Dir != "" || !c.Plantuml {
			t.Error("描画の条件が違う", c)
		}
		if c.NoSourcepos != (calls > 1) || text != "sourcepos" {
			t.Error("位置なし描画の条件が違う", c.NoSourcepos, text)
		}
		return pipeline.Result{HTML: text, HasSourcepos: true}, nil
	})})
	setSourceDocument(t, s, Document{Path: filepath.Join(root, "main.md"), Text: "sourcepos", Rev: 1, Buf: 8})
	src, ok := s.Source(nil)
	if !ok || !src.HasSourcepos {
		t.Fatal("結果の印を保持していない", src)
	}
	result, err := s.Unpositioned(context.Background(), src)
	if err != nil || result.HTML != src.Text || !result.HasSourcepos || calls != 2 {
		t.Fatal(result, err, calls)
	}
	after, ok := s.Source(nil)
	if !ok || after.Render != src.Render || after.Text != src.Text || after.HTML != src.HTML || after.Buf != src.Buf || after.Stale || !after.HasSourcepos {
		t.Fatal("位置なし描画がセッションを変えた", after, ok)
	}
}

func TestUnpositionedTimeoutPanicAndCancellation(t *testing.T) {
	s := newSession(t, Options{RenderTimeout: 20 * time.Millisecond, Renderer: renderFunc(func(ctx context.Context, text string, c pipeline.Context) (pipeline.Result, error) {
		if !c.NoSourcepos {
			t.Error("位置なし描画になっていない")
		}
		switch text {
		case "異常":
			panic("内部の情報")
		case "時間切れ":
			<-ctx.Done()
			return pipeline.Result{HTML: "使ってはいけない結果"}, nil
		default:
			return pipeline.Result{HTML: "復帰"}, nil
		}
	})})
	if result, err := s.Unpositioned(context.Background(), Source{Text: "異常"}); err == nil || strings.Contains(err.Error(), "内部") || result.HTML != "" {
		t.Fatal(result, err)
	}
	if result, err := s.Unpositioned(context.Background(), Source{Text: "時間切れ"}); !errors.Is(err, context.DeadlineExceeded) || result.HTML != "" {
		t.Fatal(result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := s.Unpositioned(ctx, Source{Text: "時間切れ"}); !errors.Is(err, context.Canceled) || result.HTML != "" {
		t.Fatal(result, err)
	}
	if result, err := s.Unpositioned(context.Background(), Source{Text: "正常"}); err != nil || result.HTML != "復帰" {
		t.Fatal(result, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.renders != 0 || s.active.source != nil || s.active.generation != 0 {
		t.Fatal("位置なし描画がセッションを変えた")
	}
}
