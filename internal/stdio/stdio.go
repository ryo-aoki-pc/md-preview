// Package stdio は Neovim との版 1 の JSON 行の取り決めを扱う。
package stdio

import (
	"bufio"
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ryo-aoki-pc/md-preview/internal/pagefiles"
	"github.com/ryo-aoki-pc/md-preview/internal/resolve"
	"github.com/ryo-aoki-pc/md-preview/internal/server"
	"github.com/ryo-aoki-pc/md-preview/internal/session"
)

const MaxLineBytes = 96 * 1024 * 1024
const MaxOutputBytes = 1024 * 1024

// フィールドの順番も Node.js 版の JSON 行にそろえる。
type errorMessage struct {
	Type    string `json:"type"`
	Fatal   bool   `json:"fatal,omitempty"`
	Message string `json:"message"`
}
type readyMessage struct {
	Type     string `json:"type"`
	Protocol int    `json:"protocol"`
	Version  string `json:"version"`
	URL      string `json:"url"`
}
type clientsMessage struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}
type jumpMessage struct {
	Type string `json:"type"`
	Path any    `json:"path"`
	Line int64  `json:"line"`
}

// ReadLines は上限を超えた行を改行まで捨てる。最後の改行は省略できる。
func ReadLines(input io.Reader, onLine func([]byte) bool, onOversize func() bool, max int) error {
	r := bufio.NewReaderSize(input, 65536)
	buffer := []byte{}
	dropping := false
	for {
		part, e := r.ReadSlice('\n')
		complete := len(part) > 0 && part[len(part)-1] == '\n'
		if complete {
			part = part[:len(part)-1]
		}
		if !dropping {
			if len(buffer)+len(part) > max {
				buffer = nil
				dropping = true
				if !onOversize() {
					return nil
				}
			} else {
				buffer = append(buffer, part...)
			}
		}
		if complete {
			if dropping {
				dropping = false
			} else {
				if !onLine(buffer) {
					return nil
				}
			}
			buffer = nil
		}
		if errors.Is(e, bufio.ErrBufferFull) {
			continue
		}
		if e != nil {
			if e == io.EOF {
				if !dropping && len(buffer) > 0 {
					onLine(buffer)
				}
				return nil
			}
			return e
		}
	}
}

type output struct {
	mu              sync.Mutex
	queue           [][]byte
	bytes           int
	failed, closing bool
	wake            chan struct{}
	done            chan struct{}
	writer          io.Writer
}

func newOutput(w io.Writer) *output {
	o := &output{wake: make(chan struct{}, 1), done: make(chan struct{}), writer: w}
	go o.run()
	return o
}
func (o *output) send(v any) {
	b, err := server.Stringify(v)
	if err != nil {
		return
	}
	b = append(b, '\n')
	o.mu.Lock()
	if o.failed || o.closing || o.bytes > MaxOutputBytes {
		o.mu.Unlock()
		return
	}
	o.queue = append(o.queue, b)
	o.bytes += len(b)
	o.mu.Unlock()
	select {
	case o.wake <- struct{}{}:
	default:
	}
}
func (o *output) run() {
	defer close(o.done)
	for {
		<-o.wake
		for {
			o.mu.Lock()
			if len(o.queue) == 0 {
				closing := o.closing
				o.mu.Unlock()
				if closing {
					return
				}
				break
			}
			b := o.queue[0]
			o.queue = o.queue[1:]
			o.mu.Unlock()
			n, e := o.writer.Write(b)
			o.mu.Lock()
			o.bytes -= len(b)
			if e != nil || n != len(b) {
				o.failed = true
				o.queue = nil
				o.mu.Unlock()
				return
			}
			o.mu.Unlock()
		}
	}
}
func (o *output) finish() {
	o.mu.Lock()
	o.closing = true
	o.mu.Unlock()
	select {
	case o.wake <- struct{}{}:
	default:
	}
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-o.done:
		return
	case <-timer.C:
		if c, ok := o.writer.(io.Closer); ok {
			_ = c.Close()
		}
	}
}

type Options struct {
	Stdin        io.Reader
	Stdout       io.Writer
	Port         int
	Version      string
	Renderer     session.Renderer
	Plantuml     server.DiagramRenderer
	Assets       server.OfficialAssets
	AssetsNotice string
	OnStart      func(*server.Server, *session.Session) error
}

func finite(v any) bool { n, ok := v.(float64); return ok && !math.IsNaN(n) && !math.IsInf(n, 0) }

func Run(ctx context.Context, o Options) int {
	output := newOutput(o.Stdout)
	defer output.finish()
	var state *session.Session
	var httpServer *server.Server
	greeted, paused, closing := false, false, false
	code := 0
	var currentPath any
	var pathMu sync.Mutex
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-finished:
			return
		case <-ctx.Done():
			if c, ok := o.Stdin.(io.Closer); ok {
				_ = c.Close()
			}
		}
	}()
	report := func(message string, fatal bool) bool {
		if fatal {
			code = 2
			closing = true
		}
		output.send(errorMessage{"error", fatal, message})
		return !fatal
	}
	message := func(line []byte) bool {
		if closing || ctx.Err() != nil {
			return false
		}
		var value map[string]any
		if !utf8.Valid(line) {
			return report("JSON の行を読めません", !greeted)
		}
		raw, parseErr := server.DecodeJSON(line)
		if parseErr != nil {
			return report("JSON の行を読めません", !greeted)
		}
		var ok bool
		value, ok = raw.(map[string]any)
		if !ok || value == nil {
			return report("JSON のオブジェクトを送ってください", !greeted)
		}
		kind, _ := value["type"].(string)
		if kind == "hello" && value["protocol"] != float64(1) {
			return report("対応していない取り決めの版です。protocol は1にしてください", true)
		}
		if !greeted {
			if kind != "hello" {
				return report("最初に hello を送ってください", true)
			}
			appearance, _ := value["appearance"].(map[string]any)
			theme, width := "system", "fluid"
			if v, ok := appearance["theme"].(string); ok && (v == "light" || v == "dark" || v == "system") {
				theme = v
			}
			if v, ok := appearance["width"].(string); ok && (v == "fluid" || v == "fixed") {
				width = v
			}
			var e error
			state, e = session.New(session.Options{Renderer: o.Renderer, Plantuml: o.Plantuml != nil})
			if e != nil {
				return report("プレビューのサーバーを起動できません", true)
			}
			page, e := pagefiles.Load(pagefiles.Options{Theme: theme, Width: width, Jump: true})
			if e != nil {
				return report("プレビューのサーバーを起動できません", true)
			}
			httpServer, e = server.New(server.Options{Session: state, Page: page, Port: o.Port, Plantuml: o.Plantuml, Assets: o.Assets, AssetsNotice: o.AssetsNotice, OnJump: func(line int64) {
				pathMu.Lock()
				p := currentPath
				pathMu.Unlock()
				output.send(jumpMessage{"jump", p, line})
			}, OnClientCount: func(count int) { output.send(clientsMessage{"clients", count}) }})
			if e != nil {
				return report("プレビューのサーバーを起動できません", true)
			}
			// 状態側が通知と取り消しを直列化し、休止より後に古い描画を流さない。
			state.SetCallbacks(func(d session.Data) { _ = httpServer.Broadcast("render", d) }, func(d session.Data) { _ = httpServer.Broadcast("scroll", d) })
			greeted = true
			output.send(readyMessage{"ready", 1, o.Version, httpServer.URL})
			output.send(clientsMessage{"clients", httpServer.ClientCount()})
			if o.OnStart != nil {
				if o.OnStart(httpServer, state) != nil {
					return report("プレビューのサーバーを起動できません", true)
				}
			}
			return true
		}
		switch kind {
		case "doc":
			if paused {
				return true
			}
			path, pathOK := value["path"]
			p, pathString := path.(string)
			name, nameOK := value["name"].(string)
			text, textOK := value["text"].(string)
			if !pathOK || !(path == nil || (pathString && resolve.IsAbsolute(p) && !strings.ContainsRune(p, 0))) || !nameOK || !textOK || !finite(value["rev"]) {
				return report("doc の path・name・text・rev の型または値が正しくありません", false)
			}
			if state.SetDocument(session.Document{Path: p, Name: name, Text: text, Rev: value["rev"]}) != nil {
				return report("文書のパスまたはフォルダーを使えません", false)
			}
			pathMu.Lock()
			currentPath = path
			pathMu.Unlock()
			return true
		case "scroll":
			if paused {
				return true
			}
			if !server.SafeInteger(value["line"]) || !server.SafeInteger(value["lines"]) || !finite(value["ratio"]) {
				return report("scroll の line・ratio・lines の型または値が正しくありません", false)
			}
			ratio := value["ratio"].(float64)
			if ratio < 0 || ratio > 1 {
				return report("scroll の line・ratio・lines の型または値が正しくありません", false)
			}
			state.SetScroll(value)
			return true
		case "pause":
			paused = true
			state.CancelRender()
			_ = httpServer.Broadcast("stop", session.Data{"reason": "stop"})
			return true
		case "resume":
			paused = false
			return true
		case "hello":
			return report("hello は最初に1回だけ送ってください", false)
		default:
			return report("知らない行の種類です。type を確かめてください", false)
		}
	}
	if e := ReadLines(o.Stdin, message, func() bool { return report("1行の大きさが上限の96 MBを超えています", !greeted) }, MaxLineBytes); e != nil && ctx.Err() == nil && !closing {
		report("標準入力を読めません", true)
	}
	closing = true
	if c, ok := o.Stdin.(io.Closer); ok {
		_ = c.Close()
	}
	if httpServer != nil {
		_ = httpServer.Close("exit")
	}
	if state != nil {
		_ = state.Close()
	}
	return code
}
