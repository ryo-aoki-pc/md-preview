package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ryo-aoki-pc/md-preview/internal/pipeline"
	"github.com/ryo-aoki-pc/md-preview/internal/resolve"
)

func sourceRoot(t *testing.T) string {
	t.Helper()
	root, err := resolve.RealPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func setSourceDocument(t *testing.T, s *Session, d Document) {
	t.Helper()
	if err := s.SetDocument(d); err != nil {
		t.Fatal(err)
	}
	expectRev(t, s, d.Rev)
}

func TestSourceAndRenderNumbers(t *testing.T) {
	root := sourceRoot(t)
	path := filepath.Join(root, "a.md")
	if err := os.WriteFile(path, []byte("保存済み"), 0644); err != nil {
		t.Fatal(err)
	}
	var active, alias, file collector
	s := newSession(t, Options{Root: root, OnRender: func(d Data) { active.add("render", d) }})
	if _, ok := s.Source(nil); ok {
		t.Fatal("描画前に原文がある")
	}
	// 元の綴りを、そのまま後の編集処理へ渡せるようにする。
	rawPath := root + string(os.PathSeparator) + "." + string(os.PathSeparator) + "a.md"
	d := Document{Path: rawPath, Text: "未保存\r\n本文", Rev: "初回", Buf: 42}
	setSourceDocument(t, s, d)
	src, ok := s.Source(nil)
	if !ok || src.Render != 1 || src.Rev != d.Rev || src.Buf != d.Buf || src.DocPath != rawPath || src.Text != d.Text || src.HTML != d.Text || src.Root != root || src.Path != path || src.File != "a.md" || src.Dir != "" || !src.Current || src.Stale {
		t.Fatal(src, ok)
	}
	if orig, reason := src.Lines.Original(2); orig != 2 || reason != "" {
		t.Fatal("偽の描画器の行対応", orig, reason)
	}
	name := "./a.md"
	off := s.SubscribeDocument(name, alias.add)
	defer off()
	other, ok := s.Source(&name)
	if !ok || other.Render != src.Render || other.DocPath != rawPath || !other.Current {
		t.Fatal(other, ok)
	}
	wait(t, func() bool { return active.count() == 1 && alias.count() == 1 })
	if active.last()["render"] != src.Render || alias.last()["render"] != src.Render {
		t.Fatal(active.last(), alias.last())
	}
	encoded, err := json.Marshal(active.last())
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(encoded, &event); err != nil || event["render"] != float64(src.Render) {
		t.Fatal(string(encoded), err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.md"), []byte("別の本文"), 0644); err != nil {
		t.Fatal(err)
	}
	nameB := "b.md"
	offB := s.SubscribeDocument(nameB, file.add)
	defer offB()
	wait(t, func() bool { return file.has("別の本文") })
	fileSource, ok := s.Source(&nameB)
	if !ok || fileSource.Render != 2 || fileSource.Current || fileSource.Stale || fileSource.Text != "別の本文" || key(fileSource.Path) != key(filepath.Join(root, nameB)) || fileSource.File != nameB || fileSource.DocPath != "" || fileSource.Buf != nil {
		t.Fatal(fileSource, ok)
	}
	d.Text, d.Rev = "次の本文", "更新"
	setSourceDocument(t, s, d)
	next, ok := s.Source(nil)
	if !ok || next.Render != 3 || next.Buf != 42 || next.DocPath != rawPath {
		t.Fatal(next, ok)
	}
	invalid := "../a.md"
	if _, ok := s.Source(&invalid); ok {
		t.Fatal("ルート外の原文を返した")
	}
	_ = s.Close()
	if _, ok := s.Source(nil); ok {
		t.Fatal("終了後に原文を返した")
	}
}

func TestSourceStaleErrorsAndSwitch(t *testing.T) {
	root := sourceRoot(t)
	started := make(chan string, 4)
	release := make(chan struct{}, 4)
	s := newSession(t, Options{Root: root, Renderer: renderFunc(func(ctx context.Context, text string, c pipeline.Context) (pipeline.Result, error) {
		if text != "最初" {
			started <- text
			select {
			case <-release:
			case <-ctx.Done():
				return pipeline.Result{}, ctx.Err()
			}
		}
		if text == "失敗" {
			return pipeline.Result{}, errors.New("内部の誤り")
		}
		return simple(ctx, text, c)
	})})
	path := filepath.Join(root, "a.md")
	setSourceDocument(t, s, Document{Path: path, Text: "最初", Rev: 1})
	if err := s.SetDocument(Document{Path: path, Text: "待機", Rev: 2}); err != nil {
		t.Fatal(err)
	}
	<-started
	src, ok := s.Source(nil)
	if !ok || !src.Stale || src.Render != 1 || src.Text != "最初" {
		t.Fatal(src, ok)
	}
	release <- struct{}{}
	expectRev(t, s, 2)
	if src, ok := s.Source(nil); !ok || src.Stale || src.Render != 2 {
		t.Fatal(src, ok)
	}
	// 休止による世代の更新だけでは、新しい本文が積まれたことにしない。
	s.CancelRender()
	if src, ok := s.Source(nil); !ok || src.Stale {
		t.Fatal(src, ok)
	}
	if err := s.SetDocument(Document{Path: path, Text: "失敗", Rev: 3}); err != nil {
		t.Fatal(err)
	}
	<-started
	release <- struct{}{}
	if d := expectRev(t, s, 3); d["render"] != uint64(3) || d["error"] == nil {
		t.Fatal(d)
	}
	if _, ok := s.Source(nil); ok {
		t.Fatal("誤りの描画の原文を返した")
	}
	setSourceDocument(t, s, Document{Path: path, Text: "最初", Rev: 4})
	if err := s.SetDocument(Document{Path: filepath.Join(root, "b.md"), Text: "切り替え", Rev: 5}); err != nil {
		t.Fatal(err)
	}
	<-started
	if _, ok := s.Source(nil); ok {
		t.Fatal("文書の切り替え後に前の原文を返した")
	}
	release <- struct{}{}
	expectRev(t, s, 5)
	if src, ok := s.Source(nil); !ok || src.Render != 5 || src.Text != "切り替え" {
		t.Fatal(src, ok)
	}
}

func TestTrialDoesNotChangeSession(t *testing.T) {
	root := sourceRoot(t)
	if err := os.Mkdir(filepath.Join(root, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "part.md"), []byte("取り込み"), 0644); err != nil {
		t.Fatal(err)
	}
	var seen collector
	s := newSession(t, Options{Root: root, Plantuml: true, OnRender: func(d Data) { seen.add("render", d) }, Renderer: renderFunc(func(ctx context.Context, text string, c pipeline.Context) (pipeline.Result, error) {
		if c.File == nil || *c.File != "docs/main.md" || c.Dir == nil || *c.Dir != "docs/" || !c.Plantuml || c.ReadInclude("docs/part.md").Text != "取り込み" || !c.Exists("docs/part.md") {
			return pipeline.Result{}, errors.New("置き場所か描画の条件が違う")
		}
		if _, ok := ctx.Deadline(); !ok {
			return pipeline.Result{}, errors.New("期限が無い")
		}
		return simple(ctx, text, c)
	})})
	setSourceDocument(t, s, Document{Path: filepath.Join(root, "docs", "main.md"), Text: "本文", Rev: 1, Buf: 8})
	src, ok := s.Source(nil)
	if !ok {
		t.Fatal("原文が無い")
	}
	wait(t, func() bool { return seen.count() == 1 })
	s.mu.Lock()
	generation, renders, watches := s.active.generation, s.renders, len(s.watchDone)
	s.mu.Unlock()
	result, err := s.Trial(context.Background(), src, "試しの本文")
	if err != nil || result.HTML != "試しの本文" {
		t.Fatal(result, err)
	}
	after, ok := s.Source(nil)
	if !ok || after.Render != src.Render || after.Text != src.Text || after.Stale || after.Buf != src.Buf {
		t.Fatal(after, ok)
	}
	s.mu.Lock()
	unchanged := s.active.generation == generation && s.renders == renders && len(s.watchDone) == watches && len(s.pending) == 0 && len(s.active.includes) == 0 && len(s.documents) == 0
	s.mu.Unlock()
	if !unchanged || seen.count() != 1 {
		t.Fatal("試しの描画がセッションを変えた")
	}
}

func TestTrialTimeoutPanicAndCancellation(t *testing.T) {
	s := newSession(t, Options{RenderTimeout: 20 * time.Millisecond, Renderer: renderFunc(func(ctx context.Context, text string, _ pipeline.Context) (pipeline.Result, error) {
		switch text {
		case "異常":
			panic("内部の情報")
		case "時間切れ":
			<-ctx.Done()
			return pipeline.Result{HTML: "使ってはいけない結果"}, nil
		default:
			return pipeline.Result{}, &pipeline.DocumentError{Message: "文書の誤り"}
		}
	})})
	result, err := s.Trial(context.Background(), Source{}, "異常")
	if err == nil || strings.Contains(err.Error(), "内部") || result.HTML != "" {
		t.Fatal(result, err)
	}
	if result, err = s.Trial(context.Background(), Source{}, "時間切れ"); !errors.Is(err, context.DeadlineExceeded) || result.HTML != "" {
		t.Fatal(result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.Trial(ctx, Source{}, "時間切れ"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var de *pipeline.DocumentError
	if _, err = s.Trial(context.Background(), Source{}, "文書"); !errors.As(err, &de) {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.renders != 0 || s.active.source != nil || s.active.generation != 0 {
		t.Fatal("失敗した試しの描画が状態を変えた")
	}
}

func TestReloadDeduplicatesAndIncludeForcesRender(t *testing.T) {
	root := sourceRoot(t)
	main, part := filepath.Join(root, "main.md"), filepath.Join(root, "part.md")
	if err := os.WriteFile(main, []byte("include:part.md"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(part, []byte("最初の取り込み"), 0644); err != nil {
		t.Fatal(err)
	}
	var seen collector
	var mu sync.Mutex
	calls := 0
	s := newSession(t, Options{Root: root, Renderer: renderFunc(func(ctx context.Context, text string, c pipeline.Context) (pipeline.Result, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return including(ctx, text, c)
	})})
	name := "main.md"
	off := s.SubscribeDocument(name, seen.add)
	defer off()
	wait(t, func() bool { return seen.has("最初の取り込み") })
	s.Reload(&name)
	if err := os.WriteFile(main, []byte("変更\ninclude:part.md"), 0644); err != nil {
		t.Fatal(err)
	}
	s.Reload(&name)
	wait(t, func() bool { return seen.has("変更") })
	src, ok := s.Source(&name)
	if !ok || src.Stale || src.Text != "変更\ninclude:part.md" {
		t.Fatal(src, ok)
	}
	// 読み直した後の見張りの通知も、同じ本文なら描画を増やさない。
	s.mu.Lock()
	ent := s.documents[key(name)]
	s.mu.Unlock()
	s.refresh(ent, false)
	time.Sleep(250 * time.Millisecond)
	mu.Lock()
	n := calls
	mu.Unlock()
	if n != 2 || seen.count() != 2 {
		t.Fatal("同じ本文を重ねて描いた", n, seen.count())
	}
	if err := os.WriteFile(part, []byte("変えた取り込み"), 0644); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return seen.has("変えた取り込み") })
	next, ok := s.Source(&name)
	if !ok || next.Render <= src.Render || next.Text != src.Text {
		t.Fatal("include の変更で描き直さない", next, ok)
	}
	if err := os.Remove(main); err != nil {
		t.Fatal(err)
	}
	s.Reload(&name)
	s.mu.Lock()
	cleared := ent.source == nil && !ent.textOK
	s.mu.Unlock()
	if !cleared {
		t.Fatal("削除された文書を即時に無効にしない")
	}
	wait(t, func() bool { return seen.last()["error"] != nil })
	if _, ok := s.Source(&name); ok {
		t.Fatal("読めないファイルの原文を返した")
	}
	// 削除前と同じ本文でも、復帰時には描き直す。
	if err := os.WriteFile(main, []byte(src.Text), 0644); err != nil {
		t.Fatal(err)
	}
	s.Reload(&name)
	wait(t, func() bool { source, ok := s.Source(&name); return ok && source.Render > next.Render })
}

func TestReloadCurrentAndAliasDoesNothing(t *testing.T) {
	root := sourceRoot(t)
	path := filepath.Join(root, "main.md")
	if err := os.WriteFile(path, []byte("保存済み"), 0644); err != nil {
		t.Fatal(err)
	}
	s := newSession(t, Options{Root: root})
	setSourceDocument(t, s, Document{Path: path, Text: "未保存", Rev: 10})
	src, _ := s.Source(nil)
	name := "main.md"
	s.Reload(nil)
	s.Reload(&name)
	// 開かれていないファイルと、無いファイルを渡しても、何も読まず、文書を増やさない。
	if err := os.WriteFile(filepath.Join(root, "other.md"), []byte("別の文書"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, unopened := range []string{"other.md", "missing.md", "../outside.md", "note.txt"} {
		s.Reload(&unopened)
		if _, ok := s.Source(&unopened); ok {
			t.Fatal("開かれていない文書の原文がある", unopened)
		}
	}
	after, ok := s.Source(nil)
	if !ok || after.Render != src.Render || after.Text != "未保存" || after.Stale || s.DocumentCount() != 0 {
		t.Fatal(after, ok)
	}
}
