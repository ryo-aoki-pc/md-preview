package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/ryo-aoki-pc/md-tools/engine"
	"github.com/ryo-aoki-pc/md-tools/internal/assets"
	"github.com/ryo-aoki-pc/md-tools/internal/cli"
	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"github.com/ryo-aoki-pc/md-tools/internal/plantuml"
	"github.com/ryo-aoki-pc/md-tools/internal/server"
)

const help = cli.Help

func run(args []string, stdout, stderr io.Writer) int {
	deps, closeRenderer := dependencies()
	defer closeRenderer()
	return cli.Run(context.Background(), args, cli.IO{Stdout: stdout, Stderr: stderr}, deps)
}

// 使い方や版を表示するだけの起動では、描画エンジンを作らない。
type commandRenderer struct {
	mu       sync.Mutex
	init     sync.Once
	initErr  error
	engine   *engine.Engine
	renderer *pipeline.Renderer
}

func (r *commandRenderer) prepare(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init.Do(func() {
		// 文書の描画期限が始まる前に、起動ごとに一度だけ準備する。
		r.engine, r.initErr = engine.New(ctx)
		if r.initErr == nil {
			r.renderer = pipeline.New(r.engine)
		}
	})
	return r.initErr
}
func (r *commandRenderer) Render(ctx context.Context, text string, c pipeline.Context) (pipeline.Result, error) {
	if err := r.prepare(ctx); err != nil {
		return pipeline.Result{}, err
	}
	return r.renderer.Render(ctx, text, c)
}
func (r *commandRenderer) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.engine != nil {
		_ = r.engine.Close(context.Background())
	}
}

func dependencies() (cli.Dependencies, func()) {
	r := &commandRenderer{}
	return cli.Dependencies{Renderer: r, InitializeRenderer: r.prepare, CreateDiagram: func(context.Context) (server.DiagramRenderer, error) {
		command, ok := plantuml.Find(nil, "")
		if !ok {
			return nil, nil
		}
		return plantuml.New(plantuml.Options{Command: command}), nil
	}, LoadAssets: func(dir string) (server.OfficialAssets, string) {
		loaded := assets.Load(dir)
		if loaded == nil {
			return nil, assets.Notice(nil, dir)
		}
		return loaded, assets.Notice(loaded, dir)
	}, DefaultAssetsDirectory: assets.DefaultDirectory}, r.close
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	deps, closeRenderer := dependencies()
	code := cli.Run(ctx, os.Args[1:], cli.IO{}, deps)
	closeRenderer()
	cancel()
	os.Exit(code)
}
