package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/ryo-aoki-pc/md-preview/engine"
	"github.com/ryo-aoki-pc/md-preview/internal/cli"
	"github.com/ryo-aoki-pc/md-preview/internal/pipeline"
	"github.com/ryo-aoki-pc/md-preview/internal/server"
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
	engine   *engine.Engine
	renderer *pipeline.Renderer
}

func (r *commandRenderer) Render(ctx context.Context, text string, c pipeline.Context) (pipeline.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.engine == nil {
		e, err := engine.New(ctx)
		if err != nil {
			return pipeline.Result{}, err
		}
		r.engine = e
		r.renderer = pipeline.New(e)
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

// 担当パッケージの統合時に、この 2 つの関数を実物へ差し替える。
func dependencies() (cli.Dependencies, func()) {
	r := &commandRenderer{}
	return cli.Dependencies{Renderer: r, CreateDiagram: func(context.Context) (server.DiagramRenderer, error) { return nil, nil }, LoadAssets: func(string) (server.OfficialAssets, string) { return nil, "" }}, r.close
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	deps, closeRenderer := dependencies()
	code := cli.Run(ctx, os.Args[1:], cli.IO{}, deps)
	closeRenderer()
	cancel()
	os.Exit(code)
}
