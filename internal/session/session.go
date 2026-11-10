// Package session は編集中の文書と購読されたファイル文書の状態を管理する。
package session

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ryo-aoki-pc/md-preview/internal/documents"
	"github.com/ryo-aoki-pc/md-preview/internal/pipeline"
	"github.com/ryo-aoki-pc/md-preview/internal/resolve"
	"github.com/ryo-aoki-pc/md-preview/internal/watch"
)

type Renderer interface {
	Render(context.Context, string, pipeline.Context) (pipeline.Result, error)
}
type Data = map[string]any
type Listener func(string, Data)
type Options struct {
	Renderer                    Renderer
	OnRender, OnScroll          func(Data)
	Root                        string
	RenderTimeout, ReleaseDelay time.Duration
	MaxBytes, MaxDocuments      int
	Plantuml                    bool
}
type Document struct {
	Path, Name, Text string
	Rev              any
}
type entryContext struct{ root, path, file, dir string }
type entry struct {
	key, name                           string
	context                             entryContext
	generation                          uint64
	render                              Data
	diagrams                            map[string]string
	input                               *Document
	tabs                                map[*subscriber]bool
	includes                            map[string]includeWatch
	stop                                func()
	expiry                              *time.Timer
	touched, rev                        uint64
	disposed, suspended, loading, dirty bool
}
type includeWatch struct {
	name string
	stop func()
}
type subscriber struct {
	raw          string
	listener     Listener
	entry        *entry
	alias, stale bool
}
type job struct {
	entry      *entry
	generation uint64
	text       string
	rev        any
	context    entryContext
}
type Session struct {
	mu            sync.Mutex
	delivery      sync.Mutex
	reads         sync.WaitGroup
	opts          Options
	active        *entry
	documents     map[string]*entry
	subscribers   map[*subscriber]bool
	rootListeners map[uint64]func()
	root          string
	scroll        Data
	pending       []*job
	current       *job
	cancel        context.CancelFunc
	closed        bool
	sequence      uint64
	wake          chan struct{}
	done          chan struct{}
	watchDone     []<-chan struct{}
}

func New(o Options) (*Session, error) {
	if o.Renderer == nil {
		return nil, errors.New("描画する部品がありません")
	}
	if o.RenderTimeout <= 0 {
		o.RenderTimeout = 15 * time.Second
	}
	if o.ReleaseDelay <= 0 {
		o.ReleaseDelay = 5 * time.Second
	}
	if o.MaxBytes == 0 {
		o.MaxBytes = 16 * 1024 * 1024
	}
	if o.MaxDocuments <= 0 {
		o.MaxDocuments = 32
	}
	root := ""
	if o.Root != "" {
		var e error
		root, e = resolve.RealPath(o.Root)
		if e != nil {
			return nil, e
		}
		root, e = filepath.Abs(root)
		if e != nil {
			return nil, e
		}
		o.Root = root
	}
	s := &Session{opts: o, root: root, active: &entry{}, documents: map[string]*entry{}, subscribers: map[*subscriber]bool{}, rootListeners: map[uint64]func(){}, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go s.run()
	return s, nil
}

func key(path string) string {
	path = filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		return lowerJS(path)
	}
	return path
}
func copyData(d Data) Data {
	if d == nil {
		return nil
	}
	c := Data{}
	for k, v := range d {
		c[k] = v
	}
	return c
}
func emit(actions []func()) {
	for _, action := range actions {
		action()
	}
}
func notify(actions *[]func(), listener Listener, event string, data Data) {
	value := copyData(data)
	*actions = append(*actions, func() { listener(event, value) })
}
func unavailable(name string) Data {
	return Data{"file": name, "dir": "", "rev": 0, "error": "この文書は開けない", "markdown": "", "headings": []pipeline.Heading{}}
}
func (s *Session) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Session) clearIncludes(e *entry) {
	for _, w := range e.includes {
		w.stop()
	}
	e.includes = nil
}
func (s *Session) cancelEntry(e *entry) {
	s.clearIncludes(e)
	e.generation++
	for i := 0; i < len(s.pending); {
		if s.pending[i].entry == e {
			s.pending = append(s.pending[:i], s.pending[i+1:]...)
		} else {
			i++
		}
	}
	if s.current != nil && s.current.entry == e && s.cancel != nil {
		s.cancel()
	}
}
func (s *Session) enqueue(e *entry, text string, rev any) {
	if e == s.active {
		e.input = &Document{Text: text, Rev: rev}
	}
	e.generation++
	j := &job{e, e.generation, text, rev, e.context}
	for i, old := range s.pending {
		if old.entry == e {
			s.pending[i] = j
			s.signal()
			return
		}
	}
	s.pending = append(s.pending, j)
	s.signal()
}
func (s *Session) publish(j *job, data Data, actions *[]func()) {
	e := j.entry
	if s.closed || e.disposed || e.generation != j.generation {
		return
	}
	d := Data{"file": j.context.file, "dir": j.context.dir, "rev": j.rev}
	for k, v := range data {
		d[k] = v
	}
	e.render = d
	if e == s.active {
		if s.opts.OnRender != nil {
			value := copyData(d)
			listener := s.opts.OnRender
			*actions = append(*actions, func() { listener(value) })
		}
		for sub := range s.subscribers {
			if sub.alias && !sub.stale {
				notify(actions, sub.listener, "render", d)
			}
		}
	} else {
		for sub := range e.tabs {
			notify(actions, sub.listener, "render", d)
		}
	}
}
func (s *Session) fail(j *job, message string, actions *[]func()) {
	if j.generation == j.entry.generation {
		s.clearIncludes(j.entry)
		j.entry.diagrams = nil
	}
	s.publish(j, Data{"error": message, "markdown": j.text}, actions)
}

// 1 つの goroutine で文書を順番に描く。取り消した描画が戻るまで次を始めない。
func (s *Session) run() {
	defer close(s.done)
	for {
		s.delivery.Lock()
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			s.delivery.Unlock()
			return
		}
		if len(s.pending) == 0 {
			s.mu.Unlock()
			s.delivery.Unlock()
			<-s.wake
			continue
		}
		j := s.pending[0]
		s.pending = s.pending[1:]
		if j.entry.disposed || j.generation != j.entry.generation {
			s.mu.Unlock()
			s.delivery.Unlock()
			continue
		}
		actions := []func(){}
		if len(j.text) > s.opts.MaxBytes {
			s.fail(j, "文書が大きすぎます。原文を表示します。", &actions)
			s.mu.Unlock()
			emit(actions)
			s.delivery.Unlock()
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), s.opts.RenderTimeout)
		s.current, s.cancel = j, cancel
		s.mu.Unlock()
		s.delivery.Unlock()
		var result pipeline.Result
		var err error
		crashed := false
		func() {
			defer func() {
				if recover() != nil {
					crashed = true
				}
			}()
			result, err = s.opts.Renderer.Render(ctx, j.text, FileContext(j.context.root, j.context.file, j.context.dir, s.opts.Plantuml))
		}()
		ctxErr := ctx.Err()
		cancel()
		s.delivery.Lock()
		s.mu.Lock()
		s.current = nil
		s.cancel = nil
		if !s.closed && !j.entry.disposed && j.generation == j.entry.generation {
			switch {
			case errors.Is(ctxErr, context.DeadlineExceeded):
				s.fail(j, "文書の描画が時間切れになりました。原文を表示します。", &actions)
			case crashed:
				s.fail(j, "描画の処理が終了しました。原文を表示します。", &actions)
			case err != nil:
				message := "文書を描画できませんでした。"
				var de *pipeline.DocumentError
				if errors.As(err, &de) && de.Message != "" {
					message = de.Message
				}
				s.fail(j, message+"原文を表示します。", &actions)
			case ctxErr == nil:
				s.updateIncludes(j, result.Includes)
				j.entry.diagrams = map[string]string{}
				for _, d := range result.Diagrams {
					j.entry.diagrams[d.ID] = d.Source
				}
				if result.Headings == nil {
					result.Headings = []pipeline.Heading{}
				}
				if result.Includes == nil {
					result.Includes = []pipeline.Include{}
				}
				if result.Notices == nil {
					result.Notices = []pipeline.Notice{}
				}
				s.publish(j, Data{"html": result.HTML, "headings": result.Headings, "includes": result.Includes, "notices": result.Notices}, &actions)
			}
		}
		s.mu.Unlock()
		emit(actions)
		s.delivery.Unlock()
	}
}

func documentRoot(path, override string) (string, error) {
	if override != "" {
		return resolve.RealPath(override)
	}
	if path == "" {
		return "", nil
	}
	folder := filepath.Dir(path)
	for dir := folder; ; dir = filepath.Dir(dir) {
		if _, e := os.Stat(filepath.Join(dir, ".git")); e == nil {
			return resolve.RealPath(dir)
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return resolve.RealPath(folder)
}

type resolved struct{ key, name, path string }

func (s *Session) resolveDocument(raw string) *resolved {
	if s.root == "" || !documents.IsMarkdown(raw) {
		return nil
	}
	r := resolve.Resolve(s.root, documents.Encode(raw))
	if r.Status != 200 {
		return nil
	}
	st, e := os.Stat(r.Path)
	if e != nil || !st.Mode().IsRegular() {
		return nil
	}
	parts := []string{}
	for _, p := range strings.Split(raw, "/") {
		if p != "" && p != "." {
			parts = append(parts, p)
		}
	}
	name := strings.Join(parts, "/")
	return &resolved{key(name), name, r.Path}
}
func (s *Session) sameFile(r *resolved) bool {
	return r != nil && s.active.context.file != "" && s.active.context.path != "" && key(r.path) == key(s.active.context.path)
}
func (s *Session) drop(e *entry) {
	if e.expiry != nil {
		e.expiry.Stop()
	}
	if e.stop != nil {
		e.stop()
	}
	e.disposed = true
	e.diagrams = nil
	s.cancelEntry(e)
	delete(s.documents, e.key)
}
func (s *Session) release(sub *subscriber) {
	if sub.entry == nil {
		sub.alias = false
		return
	}
	e := sub.entry
	delete(e.tabs, sub)
	sub.entry = nil
	sub.alias = false
	if len(e.tabs) == 0 && !e.disposed {
		s.sequence++
		e.touched = s.sequence
		if e.expiry != nil {
			e.expiry.Stop()
		}
		e.expiry = time.AfterFunc(s.opts.ReleaseDelay, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if !e.disposed && len(e.tabs) == 0 {
				s.drop(e)
			}
		})
	}
}
func (s *Session) bind(sub *subscriber, r *resolved, actions *[]func()) {
	if sub.stale || s.closed {
		return
	}
	if r == nil {
		s.release(sub)
		notify(actions, sub.listener, "render", unavailable(sub.raw))
		return
	}
	if s.sameFile(r) {
		if sub.entry != nil {
			s.release(sub)
		}
		sub.alias = true
		if s.active.render != nil {
			notify(actions, sub.listener, "render", s.active.render)
		}
		return
	}
	if sub.entry != nil && sub.entry.key == r.key {
		return
	}
	s.release(sub)
	e := s.documents[r.key]
	if e == nil {
		if len(s.documents) >= s.opts.MaxDocuments {
			var idle *entry
			for _, candidate := range s.documents {
				if len(candidate.tabs) == 0 && (idle == nil || candidate.touched < idle.touched) {
					idle = candidate
				}
			}
			if idle != nil {
				s.drop(idle)
			} else {
				notify(actions, sub.listener, "render", unavailable(sub.raw))
				return
			}
		}
		dir := ""
		if i := strings.LastIndex(r.name, "/"); i >= 0 {
			dir = r.name[:i+1]
		}
		e = &entry{key: r.key, name: r.name, tabs: map[*subscriber]bool{}, context: entryContext{s.root, r.path, r.name, dir}}
		s.documents[e.key] = e
		target := filepath.Join(s.root, filepath.FromSlash(r.name))
		e.stop = s.addWatch(target, func() { s.refresh(e) }, watch.Options{OnMissing: func() { s.refresh(e) }})
		s.reads.Add(1)
		go func() { defer s.reads.Done(); s.refresh(e) }()
	}
	if e.expiry != nil {
		e.expiry.Stop()
	}
	e.tabs[sub] = true
	sub.entry = e
	if e.render != nil {
		notify(actions, sub.listener, "render", e.render)
	}
}
func (s *Session) addWatch(path string, change func(), o watch.Options) func() {
	live := s.watchDone[:0]
	for _, done := range s.watchDone {
		select {
		case <-done:
		default:
			live = append(live, done)
		}
	}
	s.watchDone = live
	stop, done := watch.FileWithDone(path, change, o)
	s.watchDone = append(s.watchDone, done)
	return stop
}
func (s *Session) refresh(e *entry) {
	s.mu.Lock()
	if e.disposed || s.closed {
		s.mu.Unlock()
		return
	}
	if e.loading {
		e.dirty = true
		s.mu.Unlock()
		return
	}
	e.loading = true
	for {
		e.dirty = false
		r := s.resolveDocument(e.name)
		max := s.opts.MaxBytes
		s.mu.Unlock()
		text := ""
		tooLarge := false
		var readErr error
		if r == nil {
			readErr = os.ErrNotExist
		} else {
			var f *os.File
			f, readErr = os.Open(r.path)
			if readErr == nil {
				var st os.FileInfo
				st, readErr = f.Stat()
				if readErr == nil {
					if st.Size() > int64(max) {
						tooLarge = true
					} else {
						var b []byte
						b, readErr = io.ReadAll(io.LimitReader(f, int64(max)+1))
						if len(b) > max {
							tooLarge = true
						} else {
							text = strings.TrimPrefix(resolve.DecodeUTF8(string(b)), "\ufeff")
						}
					}
				}
				_ = f.Close()
			}
		}
		s.delivery.Lock()
		s.mu.Lock()
		if s.closed || e.disposed {
			e.loading = false
			s.mu.Unlock()
			s.delivery.Unlock()
			return
		}
		actions := []func(){}
		if readErr != nil || tooLarge {
			s.cancelEntry(e)
			e.diagrams = nil
			e.render = unavailable(e.name)
			if tooLarge {
				e.render["error"] = "文書が大きすぎます。"
			}
			for sub := range e.tabs {
				notify(&actions, sub.listener, "render", e.render)
			}
		} else if !e.dirty {
			e.rev++
			s.enqueue(e, text, e.rev)
		}
		dirty := e.dirty
		if !dirty {
			e.loading = false
		}
		s.mu.Unlock()
		emit(actions)
		s.delivery.Unlock()
		if !dirty {
			return
		}
		s.mu.Lock()
	}
}
func (s *Session) updateIncludes(j *job, includes []pipeline.Include) {
	e := j.entry
	next := map[string]string{}
	targets := map[string]string{}
	for _, inc := range includes {
		if len(next) >= 32 {
			break
		}
		target := watch.IncludePath(j.context.root, inc.Path)
		if target != "" {
			k := key(target)
			next[k] = inc.Path
			targets[k] = target
		}
	}
	if e.includes == nil {
		e.includes = map[string]includeWatch{}
	}
	for k, w := range e.includes {
		if next[k] != w.name {
			w.stop()
			delete(e.includes, k)
		}
	}
	for k, name := range next {
		if _, ok := e.includes[k]; ok {
			continue
		}
		target := targets[k]
		root := j.context.root
		changed := func() {
			s.mu.Lock()
			if s.closed || e.disposed || e.suspended {
				s.mu.Unlock()
				return
			}
			record, ok := e.includes[k]
			if !ok || record.name != name {
				s.mu.Unlock()
				return
			}
			if e == s.active {
				if e.input != nil {
					s.enqueue(e, e.input.Text, e.input.Rev)
				}
				s.mu.Unlock()
			} else {
				s.mu.Unlock()
				s.refresh(e)
			}
		}
		stop := s.addWatch(target, changed, watch.Options{OnMissing: changed, IsAllowed: func() bool { return watch.IncludePath(root, name) != "" }})
		e.includes[k] = includeWatch{name, stop}
	}
}

func (s *Session) SetDocument(d Document) error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return errors.New("プレビューは終了しています")
	}
	if d.Path != "" && !resolve.IsAbsolute(d.Path) {
		return errors.New("文書には絶対パスを指定してください")
	}
	path := ""
	if d.Path != "" {
		parent, e := resolve.RealPath(filepath.Dir(d.Path))
		if e != nil {
			return e
		}
		path = filepath.Join(parent, filepath.Base(d.Path))
	}
	root, e := documentRoot(path, s.opts.Root)
	if e != nil {
		return e
	}
	file := d.Name
	if file == "" {
		file = "[無名]"
	}
	if path != "" {
		file, e = filepath.Rel(root, path)
		if e != nil {
			return e
		}
		file = filepath.ToSlash(file)
	}
	dir := ""
	if path != "" {
		if i := strings.LastIndex(file, "/"); i >= 0 {
			dir = file[:i+1]
		}
	}
	real := path
	if path != "" {
		if p, e := resolve.RealPath(path); e == nil {
			real = p
		}
	}
	s.delivery.Lock()
	defer s.delivery.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("プレビューは終了しています")
	}
	actions := []func(){}
	changedRoot := key(root) != key(s.root)
	if key(real) != key(s.active.context.path) || changedRoot {
		s.clearIncludes(s.active)
		s.active.render = nil
		s.active.diagrams = nil
		s.scroll = nil
	}
	s.active.suspended = false
	s.active.context = entryContext{root, real, file, dir}
	s.root = root
	if changedRoot {
		for sub := range s.subscribers {
			s.release(sub)
			sub.stale = true
			notify(&actions, sub.listener, "reload", Data{})
		}
		for _, ent := range s.documents {
			s.drop(ent)
		}
		for _, listener := range s.rootListeners {
			actions = append(actions, listener)
		}
	} else {
		resolved := map[*subscriber]*resolved{}
		for sub := range s.subscribers {
			if !sub.stale {
				resolved[sub] = s.resolveDocument(sub.raw)
			}
		}
		// 新しい編集対象の購読を先に外し、32 件の境界でも切り替えを可能にする。
		for sub, r := range resolved {
			if sub.entry != nil && (r == nil || s.sameFile(r) || sub.entry.key != r.key) {
				s.release(sub)
			}
		}
		for sub, r := range resolved {
			s.bind(sub, r, &actions)
		}
	}
	s.enqueue(s.active, d.Text, d.Rev)
	s.mu.Unlock()
	emit(actions)
	return nil
}

func (s *Session) SetScroll(d Data) {
	s.delivery.Lock()
	defer s.delivery.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.scroll = Data{"line": d["line"], "ratio": d["ratio"], "lines": d["lines"]}
	value := copyData(s.scroll)
	listener := s.opts.OnScroll
	s.mu.Unlock()
	if listener != nil {
		listener(value)
	}
}

// SetCallbacks は起動の配線を設定する。文書を送る前に呼ぶ。
func (s *Session) SetCallbacks(render, scroll func(Data)) {
	s.delivery.Lock()
	defer s.delivery.Unlock()
	s.mu.Lock()
	s.opts.OnRender = render
	s.opts.OnScroll = scroll
	s.mu.Unlock()
}
func (s *Session) CancelRender() {
	s.delivery.Lock()
	defer s.delivery.Unlock()
	s.mu.Lock()
	s.active.suspended = true
	s.cancelEntry(s.active)
	s.mu.Unlock()
}
func (s *Session) Root() string { s.mu.Lock(); defer s.mu.Unlock(); return s.root }
func (s *Session) HasDocument() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active.context.file != ""
}
func (s *Session) IsCurrentDocument(raw string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sameFile(s.resolveDocument(raw))
}
func (s *Session) Last() (Data, Data) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return copyData(s.active.render), copyData(s.scroll)
}

// WithSnapshot は初回の SSE と描画通知の前後をそろえる。コールバックでは状態を変更しない。
func (s *Session) WithSnapshot(listener func(Data, Data)) {
	s.delivery.Lock()
	defer s.delivery.Unlock()
	render, scroll := s.Last()
	listener(render, scroll)
}
func (s *Session) DocumentCount() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.documents) }
func (s *Session) PlantumlSource(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", false
	}
	if source, ok := s.active.diagrams[id]; ok {
		return source, true
	}
	for _, e := range s.documents {
		if source, ok := e.diagrams[id]; ok {
			return source, true
		}
	}
	return "", false
}
func (s *Session) OnRootChange(listener func()) func() {
	s.mu.Lock()
	s.sequence++
	id := s.sequence
	s.rootListeners[id] = listener
	s.mu.Unlock()
	return func() { s.mu.Lock(); delete(s.rootListeners, id); s.mu.Unlock() }
}
func (s *Session) SubscribeDocument(raw string, listener Listener) func() {
	s.delivery.Lock()
	defer s.delivery.Unlock()
	s.mu.Lock()
	sub := &subscriber{raw: raw, listener: listener}
	s.subscribers[sub] = true
	actions := []func(){}
	s.bind(sub, s.resolveDocument(raw), &actions)
	s.mu.Unlock()
	emit(actions)
	return func() {
		s.mu.Lock()
		if s.subscribers[sub] {
			delete(s.subscribers, sub)
			s.release(sub)
		}
		s.mu.Unlock()
	}
}
func (s *Session) Close() error {
	s.delivery.Lock()
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		s.active.diagrams = nil
		s.clearIncludes(s.active)
		if s.cancel != nil {
			s.cancel()
		}
		s.pending = nil
		for _, e := range s.documents {
			s.drop(e)
		}
		s.subscribers = map[*subscriber]bool{}
		s.rootListeners = map[uint64]func(){}
		s.signal()
	}
	watchers := append([]<-chan struct{}{}, s.watchDone...)
	s.mu.Unlock()
	s.delivery.Unlock()
	<-s.done
	s.reads.Wait()
	for _, done := range watchers {
		<-done
	}
	return nil
}
