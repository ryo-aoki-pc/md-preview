// ページのブラウザ試験だけで使う HTTP / SSE サーバー。製品のコマンドからは呼ばない。
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ryo-aoki-pc/md-tools/engine"
	"github.com/ryo-aoki-pc/md-tools/internal/assets"
	"github.com/ryo-aoki-pc/md-tools/internal/edit"
	"github.com/ryo-aoki-pc/md-tools/internal/pagefiles"
	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"github.com/ryo-aoki-pc/md-tools/internal/resolve"
	"github.com/ryo-aoki-pc/md-tools/internal/server"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

type request struct {
	ID         int              `json:"id"`
	Op         string           `json:"op"`
	Root       string           `json:"root"`
	Live       bool             `json:"live"`
	Edit       string           `json:"edit"`
	Review     bool             `json:"review"`
	Jump       bool             `json:"jump"`
	Directory  bool             `json:"directory"`
	Plantuml   bool             `json:"plantuml"`
	Render     session.Data     `json:"render"`
	Scroll     session.Data     `json:"scroll"`
	AssetsDir  string           `json:"assetsDir"`
	AssetsLock string           `json:"assetsLock"`
	Document   session.Document `json:"document"`
	Name       string           `json:"name"`
	Data       session.Data     `json:"data"`
	Reason     string           `json:"reason"`
}

// ジャンプは HTTP の goroutine からも届くので、JSON の行が混ざらないようにする。
type output struct {
	mu      sync.Mutex
	encoder *json.Encoder
}

func (o *output) send(value any) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.encoder.Encode(value)
}

type fixtureState struct {
	mu             sync.Mutex
	root           string
	render, scroll session.Data
}

func (s *fixtureState) Root() string { return s.root }
func (s *fixtureState) Last() (session.Data, session.Data) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.render, s.scroll
}
func (s *fixtureState) HasDocument() bool {
	render, _ := s.Last()
	return render != nil
}
func (s *fixtureState) IsCurrentDocument(string) bool                     { return false }
func (s *fixtureState) SubscribeDocument(string, session.Listener) func() { return func() {} }
func (s *fixtureState) PlantumlSource(string) (string, bool)              { return "", false }
func (s *fixtureState) OnRootChange(func()) func()                        { return func() {} }
func (s *fixtureState) remember(name string, data session.Data) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if name == "render" {
		s.render = data
	}
	if name == "scroll" {
		s.scroll = data
	}
}

const diagramSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="160" height="40"><text y="20">試験の図</text></svg>`

// plantuml-helper.mjs と同じ原文で、成功・失敗・180 ms の遅延・応答待ちを選ぶ。
// Java や手元の PlantUML は起動しない。multiple と error200 は最初の SVG が成功する。
type fixtureDiagram struct {
	done chan struct{}
	once sync.Once
}

func (d *fixtureDiagram) Render(ctx context.Context, source string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var wait <-chan time.Time
	if strings.Contains(source, "hang") || strings.Contains(source, "tree") {
		wait = nil
	} else {
		delay := time.Duration(0)
		if strings.Contains(source, "slow") {
			delay = 180 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		defer timer.Stop()
		wait = timer.C
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-d.done:
		return nil, errors.New("試験用の図の描画は終了しています")
	case <-wait:
	}
	for _, word := range []string{"png", "plain", "invalid-utf8", "oversize", "empty"} {
		if strings.Contains(source, word) {
			return nil, errors.New("PlantUML の図を描けませんでした")
		}
	}
	return []byte(diagramSVG), nil
}

func (d *fixtureDiagram) Close() error {
	d.once.Do(func() { close(d.done) })
	return nil
}

type application struct {
	server  *server.Server
	live    *session.Session
	fixture *fixtureState
	engine  *engine.Engine
}

func (a *application) start(r request, out *output) error {
	if r.Review && !r.Live {
		return errors.New("画面のコメントの試験には live: true が必要です")
	}
	if r.Edit != "" && (r.Edit != "file" || !r.Live) {
		return errors.New("画面の編集の試験には live: true と edit: file が必要です")
	}
	var state server.State
	if r.Live {
		var err error
		a.engine, err = engine.New(context.Background())
		if err != nil {
			return err
		}
		a.live, err = session.New(session.Options{Root: r.Root, Renderer: pipeline.New(a.engine), Plantuml: r.Plantuml})
		if err != nil {
			return err
		}
		state = a.live
	} else {
		root := r.Root
		if root != "" {
			var err error
			root, err = resolve.RealPath(root)
			if err != nil {
				return err
			}
		}
		a.fixture = &fixtureState{root: root, render: r.Render, scroll: r.Scroll}
		state = a.fixture
	}
	pageOptions := pagefiles.Options{Jump: r.Jump, Directory: r.Directory, Review: r.Review}
	var editor *edit.Service
	if r.Edit == "file" {
		var mu sync.Mutex
		var rev uint64
		editor = edit.New(edit.Options{Host: a.live, Mode: edit.File, Written: func(src session.Source, text string) {
			mu.Lock()
			defer mu.Unlock()
			rev++
			_ = a.live.SetDocument(session.Document{Path: src.DocPath, Text: text, Rev: rev, Buf: src.Buf})
		}})
		pageOptions.Edit = editor.Config()
	}
	page, err := pagefiles.Load(pageOptions)
	if err != nil {
		return err
	}
	opts := server.Options{Session: state, Page: page}
	if editor != nil {
		opts.Edit = editor
	}
	if r.Review {
		opts.Review = edit.New(edit.Options{Host: a.live, Mode: edit.File})
	}
	if r.Jump {
		opts.OnJump = func(line int64) { _ = out.send(map[string]any{"type": "jump", "line": line}) }
	}
	if r.Plantuml {
		opts.Plantuml = &fixtureDiagram{done: make(chan struct{})}
	}
	// 保存先と元のロックのバイト列を明示した場合だけ合成資産を読み、利用者の保存先には触れない。
	if r.AssetsDir != "" {
		if r.AssetsLock == "" {
			return errors.New("試験用の公式資産には元のロックの文字列が必要です")
		}
		loaded := assets.LoadWithLock(r.AssetsDir, []byte(r.AssetsLock))
		if loaded == nil {
			return errors.New("試験用の公式資産を検証できませんでした")
		}
		opts.Assets = loaded
	}
	a.server, err = server.New(opts)
	if err != nil {
		return err
	}
	if a.live != nil {
		a.live.SetCallbacks(func(data session.Data) { _ = a.server.Broadcast("render", data) },
			func(data session.Data) { _ = a.server.Broadcast("scroll", data) })
	}
	return nil
}

func (a *application) close(reason string) {
	if a == nil {
		return
	}
	if a.server != nil {
		_ = a.server.Close(reason)
	}
	if a.live != nil {
		_ = a.live.Close()
	}
	if a.engine != nil {
		_ = a.engine.Close(context.Background())
	}
}

func run(input io.Reader, stdout io.Writer) error {
	out := &output{encoder: json.NewEncoder(stdout)}
	var app *application
	defer func() { app.close("exit") }()
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 96*1024*1024)
	for scanner.Scan() {
		var r request
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			if err := out.send(map[string]any{"type": "error", "error": "JSON の行を読めません"}); err != nil {
				return err
			}
			continue
		}
		response := map[string]any{"id": r.ID, "type": "ok"}
		var err error
		switch {
		case r.Op == "start" && app == nil:
			next := &application{}
			err = next.start(r, out)
			if err != nil {
				next.close("exit")
			} else {
				app = next
				response["type"], response["url"], response["port"], response["token"] = "ready", app.server.URL, app.server.Port, app.server.Token
			}
		case app == nil:
			err = errors.New("先に start を送ってください")
		case r.Op == "document" && app.live != nil:
			err = app.live.SetDocument(r.Document)
		case r.Op == "broadcast":
			if app.fixture != nil {
				app.fixture.remember(r.Name, r.Data)
			}
			err = app.server.Broadcast(r.Name, r.Data)
		case r.Op == "sync":
			// この行より前に標準出力へ出たジャンプも、応答までに試験側で受け取れる。
		case r.Op == "close":
			app.close(r.Reason)
			if err := out.send(response); err != nil {
				return err
			}
			return nil
		default:
			err = errors.New("未対応の試験用の操作です")
		}
		if err != nil {
			response["type"], response["error"] = "error", err.Error()
		}
		if err := out.send(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "ページ試験用サーバー:", err)
		os.Exit(1)
	}
}
