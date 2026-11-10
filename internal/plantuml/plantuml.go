// Package plantuml は、原文を標準入力だけに渡し、手元の PlantUML で SVG を描く。
package plantuml

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

var (
	errFailed   = errors.New("PlantUML の図を描けませんでした")
	errCanceled = errors.New("PlantUML の描画を中止しました")
	errClosed   = errors.New("PlantUML の描画は終了しています")
	errTimeout  = errors.New("PlantUML の描画が時間切れになりました")
	errTooLarge = errors.New("PlantUML の図が大きすぎます")
)

// Options は、コマンドと、実行時間・出力・成功のキャッシュの上限。
type Options struct {
	Command       string
	Timeout       time.Duration
	MaxSVGBytes   int
	MaxCacheBytes int
}

type job struct {
	id, source string
	ctx        context.Context
	cancel     context.CancelFunc
	waiters    int
	done       chan struct{}
	finished   bool
	svg        []byte
	err        error
}

type cached struct {
	id  string
	svg []byte
}

// Renderer は、同じ原文の実行を共有し、異なる図を受け付けた順に 1 つずつ描く。
type Renderer struct {
	options    Options
	mu         sync.Mutex
	jobs       map[string]*job
	queue      []*job
	active     *job
	closed     bool
	cache      []cached
	cacheBytes int
	wake       chan struct{}
	done       chan struct{}
}

// New は、0 の制限を既定値で補い、描画する部品を作る。
func New(options Options) *Renderer {
	if options.Timeout <= 0 {
		options.Timeout = 30 * time.Second
	}
	if options.MaxSVGBytes <= 0 {
		options.MaxSVGBytes = 8 * 1024 * 1024
	}
	if options.MaxCacheBytes <= 0 {
		options.MaxCacheBytes = 32 * 1024 * 1024
	}
	r := &Renderer{options: options, jobs: make(map[string]*job), wake: make(chan struct{}, 1), done: make(chan struct{})}
	go r.work()
	return r
}

func sourceID(source string) string {
	hash := sha256.Sum256([]byte(source))
	return hex.EncodeToString(hash[:])
}

// 先頭の空行と字下げを保ち、PlantUML のサーバーと同じ条件で開始・終了を補う。
func sourceInput(source string) string {
	terminated := source
	if !strings.HasSuffix(source, "\n") && !strings.HasSuffix(source, "\r") {
		terminated += "\n"
	}
	if strings.HasPrefix(source, "@start") {
		return terminated
	}
	return "@startuml\n" + terminated + "@enduml\n"
}

// Render は、検証した SVG、または原文と端末のパスを含まない日本語 1 行の誤りを返す。
func (r *Renderer) Render(ctx context.Context, source string) ([]byte, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, errClosed
	}
	if ctx.Err() != nil {
		r.mu.Unlock()
		return nil, errCanceled
	}
	id := sourceID(source)
	for _, entry := range r.cache {
		if entry.id == id {
			svg := bytes.Clone(entry.svg)
			r.mu.Unlock()
			return svg, nil
		}
	}
	j := r.jobs[id]
	if j == nil {
		workCtx, cancel := context.WithCancel(context.Background())
		j = &job{id: id, source: source, ctx: workCtx, cancel: cancel, done: make(chan struct{})}
		r.jobs[id] = j
		r.queue = append(r.queue, j)
	}
	j.waiters++
	r.signal()
	r.mu.Unlock()
	select {
	case <-j.done:
		return bytes.Clone(j.svg), j.err
	case <-ctx.Done():
		r.mu.Lock()
		if !j.finished {
			j.waiters--
			if j.waiters == 0 {
				if r.jobs[id] == j {
					delete(r.jobs, id)
				}
				j.cancel()
			}
		}
		r.mu.Unlock()
		return nil, errCanceled
	}
}

func (r *Renderer) signal() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *Renderer) settle(j *job, svg []byte, err error) {
	if r.jobs[j.id] == j {
		delete(r.jobs, j.id)
	}
	j.svg, j.err, j.finished = svg, err, true
	j.cancel()
	close(j.done)
}

func (r *Renderer) work() {
	defer close(r.done)
	for {
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return
		}
		if len(r.queue) == 0 {
			r.mu.Unlock()
			<-r.wake
			continue
		}
		j := r.queue[0]
		r.queue[0] = nil
		r.queue = r.queue[1:]
		if j.waiters == 0 {
			r.settle(j, nil, errCanceled)
			r.mu.Unlock()
			continue
		}
		r.active = j
		r.mu.Unlock()
		svg, err := r.execute(j.ctx, j.source)
		r.mu.Lock()
		if j.ctx.Err() != nil || r.closed {
			svg, err = nil, errCanceled
		}
		if err == nil && j.waiters > 0 && len(svg) <= r.options.MaxCacheBytes {
			r.cache = append(r.cache, cached{j.id, svg})
			r.cacheBytes += len(svg)
			for r.cacheBytes > r.options.MaxCacheBytes {
				r.cacheBytes -= len(r.cache[0].svg)
				r.cache[0] = cached{}
				r.cache = r.cache[1:]
			}
		}
		r.settle(j, svg, err)
		r.active = nil
		r.mu.Unlock()
	}
}

// Close は、待ち行列と実行を中止し、子のプロセスの木の終了を待つ。同時・複数回に呼んでよい。
func (r *Renderer) Close() error {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		for _, j := range r.queue {
			r.settle(j, nil, errCanceled)
		}
		r.queue = nil
		if r.active != nil {
			r.active.cancel()
		}
		r.cache = nil
		r.cacheBytes = 0
		r.signal()
	}
	r.mu.Unlock()
	<-r.done
	return nil
}

type limitedOutput struct {
	buffer   bytes.Buffer
	max      int
	overflow atomic.Bool
	cancel   context.CancelFunc
}

func (w *limitedOutput) Write(p []byte) (int, error) {
	if w.overflow.Load() {
		return len(p), nil
	}
	if len(p) > w.max-w.buffer.Len() {
		w.overflow.Store(true)
		w.cancel()
		return len(p), nil
	}
	return w.buffer.Write(p)
}

func (r *Renderer) execute(parent context.Context, source string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, r.options.Timeout)
	defer cancel()
	if !filepath.IsAbs(r.options.Command) || (runtime.GOOS == "windows" && unsafeCommand(r.options.Command)) {
		return nil, errFailed
	}
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return nil, errFailed
	}
	delimiter := "mdpreview-" + hex.EncodeToString(random)
	args := []string{"-pipe", "-tsvg", "-charset", "UTF-8", "-pipedelimitor", delimiter}
	cmd, err := prepareCommand(r.options.Command, args)
	if err != nil {
		return nil, errFailed
	}
	cmd.Dir = os.TempDir()
	cmd.Env = plantumlEnvironment(os.Environ(), runtime.GOOS)
	cmd.Stdin = strings.NewReader(sourceInput(source))
	out := &limitedOutput{max: r.options.MaxSVGBytes, cancel: cancel}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	if ctx.Err() != nil {
		return nil, errCanceled
	}
	if err := cmd.Start(); err != nil {
		return nil, errFailed
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-wait:
	case <-ctx.Done():
		killTree(cmd)
		waitErr = <-wait
	}
	if out.overflow.Load() {
		return nil, errTooLarge
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, errTimeout
	}
	if ctx.Err() != nil {
		return nil, errCanceled
	}
	// PlantUML は構文の誤りの SVG と終了コード 200 を返す。SVG が取れたときは成功にする。
	var exitError *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitError) {
		return nil, errFailed
	}
	value := out.buffer.Bytes()
	if split := bytes.Index(value, []byte(delimiter)); split >= 0 {
		value = value[:split]
	}
	if !utf8.Valid(value) {
		return nil, errFailed
	}
	value = []byte(strings.Trim(string(value), "\t\n\v\f\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff"))
	if !bytes.Contains(value[:min(len(value), 1024)], []byte("<svg")) || !bytes.HasSuffix(value, []byte("</svg>")) {
		return nil, errFailed
	}
	return bytes.Clone(value), nil
}

func unsafeCommand(path string) bool {
	for _, c := range path {
		if c == '%' || c == '"' || c < 32 || c == 127 {
			return true
		}
	}
	return false
}

func plantumlEnvironment(env []string, goos string) []string {
	result := make([]string, 0, len(env)+2)
	var java string
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			result = append(result, entry)
			continue
		}
		match := func(want string) bool {
			if goos == "windows" {
				return strings.EqualFold(name, want)
			}
			return name == want
		}
		if match("JAVA_TOOL_OPTIONS") {
			java = value
			continue
		}
		if match("PLANTUML_SECURITY_PROFILE") {
			continue
		}
		result = append(result, entry)
	}
	if java != "" {
		java += " "
	}
	return append(result, "PLANTUML_SECURITY_PROFILE=SANDBOX", "JAVA_TOOL_OPTIONS="+java+"-Djava.awt.headless=true -DPLANTUML_SECURITY_PROFILE=SANDBOX")
}
