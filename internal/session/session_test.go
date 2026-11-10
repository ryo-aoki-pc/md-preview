package session

import (
	"context"
	"errors"
	"fmt"
	"github.com/ryo-aoki-pc/md-preview/internal/pipeline"
	"github.com/ryo-aoki-pc/md-preview/internal/resolve"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type renderFunc func(context.Context, string, pipeline.Context) (pipeline.Result, error)

func (f renderFunc) Render(ctx context.Context, s string, c pipeline.Context) (pipeline.Result, error) {
	return f(ctx, s, c)
}
func simple(_ context.Context, text string, _ pipeline.Context) (pipeline.Result, error) {
	return pipeline.Result{HTML: text}, nil
}
func newSession(t *testing.T, o Options) *Session {
	t.Helper()
	if o.Renderer == nil {
		o.Renderer = renderFunc(simple)
	}
	s, e := New(o)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func wait(t *testing.T, p func() bool) {
	t.Helper()
	end := time.Now().Add(8 * time.Second)
	for time.Now().Before(end) {
		if p() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("状態の更新が時間内に届かない")
}
func expectRev(t *testing.T, s *Session, rev any) Data {
	t.Helper()
	var d Data
	wait(t, func() bool { d, _ = s.Last(); return d != nil && d["rev"] == rev })
	return d
}

type collector struct {
	mu     sync.Mutex
	values []Data
	events []string
}

func (c *collector) add(event string, d Data) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values = append(c.values, copyData(d))
	c.events = append(c.events, event)
}
func (c *collector) has(text string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, d := range c.values {
		if s, ok := d["html"].(string); ok && strings.Contains(s, text) {
			return true
		}
	}
	return false
}
func (c *collector) count() int { c.mu.Lock(); defer c.mu.Unlock(); return len(c.values) }
func (c *collector) last() Data {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.values) == 0 {
		return nil
	}
	return copyData(c.values[len(c.values)-1])
}
func (c *collector) event(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.events {
		if e == name {
			return true
		}
	}
	return false
}

func TestLatest(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var seen collector
	s := newSession(t, Options{OnRender: func(d Data) { seen.add("render", d) }, Renderer: renderFunc(func(ctx context.Context, text string, c pipeline.Context) (pipeline.Result, error) {
		if text == "最初" {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return pipeline.Result{}, ctx.Err()
			}
		}
		return simple(ctx, text, c)
	})})
	_ = s.SetDocument(Document{Name: "無名.md", Text: "最初", Rev: 1})
	<-started
	_ = s.SetDocument(Document{Name: "無名.md", Text: "中間", Rev: 2})
	_ = s.SetDocument(Document{Name: "無名.md", Text: "最新", Rev: 3})
	close(release)
	d := expectRev(t, s, 3)
	wait(t, func() bool { return seen.count() > 0 })
	if seen.count() != 1 || d["html"] != "最新" || s.Root() != "" {
		t.Fatal(d, seen.count())
	}
}
func TestTimeoutAndErrors(t *testing.T) {
	s := newSession(t, Options{RenderTimeout: 30 * time.Millisecond, Renderer: renderFunc(func(ctx context.Context, text string, c pipeline.Context) (pipeline.Result, error) {
		switch text {
		case "時間切れ":
			<-ctx.Done()
			return pipeline.Result{}, ctx.Err()
		case "内部":
			return pipeline.Result{}, errors.New("表示しない内部情報")
		case "文書":
			return pipeline.Result{}, &pipeline.DocumentError{Message: "入れ子が深すぎます。"}
		case "異常":
			panic("表示しない内部情報")
		}
		return simple(ctx, text, c)
	})})
	for i, item := range []struct{ text, want string }{{"時間切れ", "文書の描画が時間切れになりました。原文を表示します。"}, {"内部", "文書を描画できませんでした。原文を表示します。"}, {"文書", "入れ子が深すぎます。原文を表示します。"}, {"異常", "描画の処理が終了しました。原文を表示します。"}, {"復帰", ""}} {
		_ = s.SetDocument(Document{Name: "文書", Text: item.text, Rev: i})
		d := expectRev(t, s, i)
		if item.want == "" {
			if d["html"] != item.text {
				t.Fatal(d)
			}
		} else if d["error"] != item.want || d["markdown"] != item.text {
			t.Fatal(d)
		}
	}
}
func TestOldTimeout(t *testing.T) {
	started := make(chan struct{})
	s := newSession(t, Options{RenderTimeout: 30 * time.Millisecond, Renderer: renderFunc(func(ctx context.Context, text string, c pipeline.Context) (pipeline.Result, error) {
		if text == "前" {
			close(started)
			<-ctx.Done()
			return pipeline.Result{}, ctx.Err()
		}
		return simple(ctx, text, c)
	})})
	_ = s.SetDocument(Document{Name: "文書", Text: "前", Rev: 1})
	<-started
	_ = s.SetDocument(Document{Name: "文書", Text: "後", Rev: 2})
	d := expectRev(t, s, 2)
	if d["html"] != "後" {
		t.Fatal(d)
	}
}
func TestByteLimitAndClose(t *testing.T) {
	s := newSession(t, Options{MaxBytes: 5})
	_ = s.SetDocument(Document{Name: "大", Text: "日本", Rev: 1})
	d := expectRev(t, s, 1)
	if d["error"] != "文書が大きすぎます。原文を表示します。" || d["markdown"] != "日本" {
		t.Fatal(d)
	}
	_ = s.SetDocument(Document{Name: "小", Text: "ok", Rev: 2})
	expectRev(t, s, 2)
	s.SetScroll(Data{"line": 8, "ratio": 0.4, "lines": 50})
	_, scroll := s.Last()
	if scroll["line"] != 8 {
		t.Fatal(scroll)
	}
	_ = s.Close()
	if s.SetDocument(Document{}) == nil {
		t.Fatal("終了後も受け付けた")
	}
}
func TestRootsAndContext(t *testing.T) {
	root := t.TempDir()
	_ = os.Mkdir(filepath.Join(root, ".git"), 0755)
	_ = os.MkdirAll(filepath.Join(root, "docs", "日本語"), 0755)
	path := filepath.Join(root, "docs", "日本語", "new.md")
	s := newSession(t, Options{})
	_ = s.SetDocument(Document{Path: path, Text: "本文", Rev: "r1"})
	d := expectRev(t, s, "r1")
	real, _ := filepath.EvalSymlinks(root)
	if s.Root() != real || d["file"] != "docs/日本語/new.md" || d["dir"] != "docs/日本語/" {
		t.Fatal(s.Root(), d)
	}
	s2 := newSession(t, Options{Root: filepath.Dir(path)})
	_ = s2.SetDocument(Document{Path: path, Text: "本文", Rev: 1})
	if d := expectRev(t, s2, 1); d["file"] != "new.md" || d["dir"] != "" {
		t.Fatal(d)
	}
}
func TestSubscriptions(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a.md"), filepath.Join(root, "b.md")
	_ = os.WriteFile(a, []byte("ディスクA"), 0644)
	_ = os.WriteFile(b, []byte("ディスクB"), 0644)
	s := newSession(t, Options{Root: root, ReleaseDelay: 40 * time.Millisecond})
	_ = s.SetDocument(Document{Path: a, Text: "未保存A", Rev: 10})
	var seenA, seenB collector
	offA := s.SubscribeDocument("a.md", seenA.add)
	defer offA()
	offB := s.SubscribeDocument("b.md", seenB.add)
	defer offB()
	wait(t, func() bool { return seenA.has("未保存A") && seenB.has("ディスクB") })
	if s.DocumentCount() != 1 {
		t.Fatal(s.DocumentCount())
	}
	_ = os.WriteFile(b, []byte("保存B"), 0644)
	wait(t, func() bool { return seenB.has("保存B") })
	_ = s.SetDocument(Document{Path: b, Text: "未保存B", Rev: 11})
	wait(t, func() bool { return seenA.has("ディスクA") && seenB.has("未保存B") })
	s.SetScroll(Data{"line": 1, "ratio": 0, "lines": 2})
	if seenA.event("scroll") || seenB.event("scroll") {
		t.Fatal("ファイル文書へスクロールした")
	}
}
func TestDocumentCap(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 33; i++ {
		_ = os.WriteFile(filepath.Join(root, fmt.Sprintf("%d.md", i)), []byte(fmt.Sprint(i)), 0644)
	}
	s := newSession(t, Options{Root: root, ReleaseDelay: 40 * time.Millisecond})
	stops := []func(){}
	defer func() {
		for _, stop := range stops {
			stop()
		}
	}()
	for i := 0; i < 32; i++ {
		stops = append(stops, s.SubscribeDocument(fmt.Sprintf("%d.md", i), func(string, Data) {}))
	}
	var rejected collector
	off := s.SubscribeDocument("32.md", rejected.add)
	off()
	if rejected.last()["error"] != "この文書は開けない" || s.DocumentCount() != 32 {
		t.Fatal(rejected.last())
	}
	stops[0]()
	var accepted collector
	off = s.SubscribeDocument("32.md", accepted.add)
	wait(t, func() bool { return accepted.has("32") })
	if s.DocumentCount() != 32 {
		t.Fatal(s.DocumentCount())
	}
	off()
	wait(t, func() bool { return s.DocumentCount() == 31 })
}
func TestDefaultExpiry(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "a.md"), []byte("本文"), 0644)
	s := newSession(t, Options{Root: root})
	var first, second collector
	off := s.SubscribeDocument("a.md", first.add)
	wait(t, func() bool { return first.count() > 0 })
	off()
	if s.DocumentCount() != 1 {
		t.Fatal("すぐ捨てた")
	}
	off = s.SubscribeDocument("a.md", second.add)
	if second.count() == 0 || second.last()["rev"] != first.last()["rev"] {
		t.Fatal("再利用しない")
	}
	off()
	wait(t, func() bool { return s.DocumentCount() == 0 })
}
func TestFileLimitDeletion(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a.md")
	_ = os.WriteFile(file, []byte("日本"), 0644)
	s := newSession(t, Options{Root: root, MaxBytes: 5})
	var seen collector
	off := s.SubscribeDocument("a.md", seen.add)
	defer off()
	wait(t, func() bool { return seen.count() > 0 })
	if d := seen.last(); d["error"] != "文書が大きすぎます。" || d["markdown"] != "" {
		t.Fatal(d)
	}
	_ = os.Remove(file)
	wait(t, func() bool { return seen.last()["error"] == "この文書は開けない" })
	_ = os.WriteFile(file, []byte("ok"), 0644)
	wait(t, func() bool { return seen.has("ok") })
}
func TestSwitchRoot(t *testing.T) {
	base := t.TempDir()
	one, two := filepath.Join(base, "one"), filepath.Join(base, "two")
	for _, p := range []string{one, two} {
		_ = os.MkdirAll(filepath.Join(p, ".git"), 0755)
		_ = os.WriteFile(filepath.Join(p, "main.md"), nil, 0644)
	}
	_ = os.WriteFile(filepath.Join(one, "other.md"), []byte("別"), 0644)
	s := newSession(t, Options{})
	_ = s.SetDocument(Document{Path: filepath.Join(one, "main.md"), Text: "編集", Rev: 1})
	var seen collector
	off := s.SubscribeDocument("other.md", seen.add)
	defer off()
	wait(t, func() bool { return seen.has("別") })
	_ = s.SetDocument(Document{Path: filepath.Join(two, "main.md"), Text: "次", Rev: 2})
	if !seen.event("reload") || s.DocumentCount() != 0 {
		t.Fatal("ルート変更を通知しない")
	}
}
func TestSwitchAtCap(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 33; i++ {
		_ = os.WriteFile(filepath.Join(root, fmt.Sprintf("%d.md", i)), []byte(fmt.Sprintf("ディスク%d", i)), 0644)
	}
	s := newSession(t, Options{Root: root})
	_ = s.SetDocument(Document{Path: filepath.Join(root, "0.md"), Text: "未保存0", Rev: 1})
	seen := make([]collector, 33)
	for i := range seen {
		off := s.SubscribeDocument(fmt.Sprintf("%d.md", i), seen[i].add)
		defer off()
	}
	wait(t, func() bool {
		for i := range seen {
			if seen[i].count() == 0 {
				return false
			}
		}
		return true
	})
	_ = s.SetDocument(Document{Path: filepath.Join(root, "1.md"), Text: "未保存1", Rev: 2})
	wait(t, func() bool { return seen[0].has("ディスク0") && seen[1].has("未保存1") })
	for i := range seen {
		if seen[i].last()["error"] != nil {
			t.Fatal(i, seen[i].last())
		}
	}
}

func including(_ context.Context, text string, c pipeline.Context) (pipeline.Result, error) {
	result := pipeline.Result{HTML: text}
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "include:") {
			continue
		}
		name := strings.TrimPrefix(line, "include:")
		read := c.ReadInclude(name)
		result.Includes = append(result.Includes, pipeline.Include{Path: name, OK: read.Kind == pipeline.IncludeText})
		if read.Kind == pipeline.IncludeText {
			result.HTML += "\n" + read.Text
		}
	}
	return result, nil
}
func TestIncludeChanges(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "main.md")
	_ = os.WriteFile(main, []byte("ディスク"), 0644)
	s := newSession(t, Options{Root: root, Renderer: renderFunc(including)})
	_ = s.SetDocument(Document{Path: main, Text: "未保存\ninclude:new/deep/part.md", Rev: 42})
	expectRev(t, s, 42)
	_ = os.MkdirAll(filepath.Join(root, "new", "deep"), 0755)
	part := filepath.Join(root, "new", "deep", "part.md")
	_ = os.WriteFile(part, []byte("出現"), 0644)
	wait(t, func() bool { d, _ := s.Last(); return strings.Contains(fmt.Sprint(d["html"]), "出現") })
	_ = os.Remove(part)
	wait(t, func() bool { d, _ := s.Last(); return !strings.Contains(fmt.Sprint(d["html"]), "出現") })
	_ = os.WriteFile(part, []byte("再作成"), 0644)
	wait(t, func() bool { d, _ := s.Last(); return strings.Contains(fmt.Sprint(d["html"]), "再作成") })
	d, _ := s.Last()
	if d["rev"] != 42 || !strings.Contains(d["html"].(string), "未保存") || strings.Contains(d["html"].(string), "ディスク") {
		t.Fatal(d)
	}
}
func TestIncludeCapPauseAndDiagrams(t *testing.T) {
	root := t.TempDir()
	s := newSession(t, Options{Root: root, Renderer: renderFunc(func(ctx context.Context, text string, c pipeline.Context) (pipeline.Result, error) {
		r, e := including(ctx, text, c)
		r.Diagrams = []pipeline.Diagram{{ID: text, Source: "図"}}
		return r, e
	})})
	lines := []string{}
	for i := 0; i < 40; i++ {
		lines = append(lines, fmt.Sprintf("include:%d.md", i))
	}
	text := strings.Join(lines, "\n")
	_ = s.SetDocument(Document{Path: filepath.Join(root, "main.md"), Text: text, Rev: 1})
	d := expectRev(t, s, 1)
	s.mu.Lock()
	n := len(s.active.includes)
	s.mu.Unlock()
	if n != 32 {
		t.Fatal(n)
	}
	if _, ok := d["diagrams"]; ok {
		t.Fatal("原文の図をページへ送った")
	}
	if _, ok := s.PlantumlSource(text); !ok {
		t.Fatal("図を保持しない")
	}
	s.CancelRender()
	s.mu.Lock()
	n = len(s.active.includes)
	s.mu.Unlock()
	if n != 0 {
		t.Fatal("休止で見張りを残した")
	}
	_ = s.SetDocument(Document{Path: filepath.Join(root, "main.md"), Text: "include:next.md", Rev: 2})
	expectRev(t, s, 2)
	if _, ok := s.PlantumlSource(text); ok {
		t.Fatal("古い図を残した")
	}
}
func TestFileContext(t *testing.T) {
	// ルートは実パスで渡す決まり。一時フォルダーのパスには、8.3 形式の短い名前が入ることがある (Windows)。
	root, e := resolve.RealPath(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	for name, body := range map[string][]byte{"text.md": []byte("\ufeff本文"), "binary.md": {0, 1}, "invalid.md": {255}, "percent%25.md": []byte("文字通り")} {
		_ = os.WriteFile(filepath.Join(root, name), body, 0644)
	}
	c := FileContext(root, "main.md", "", true)
	if got := c.ReadInclude("text.md"); got.Kind != pipeline.IncludeText || got.Text != "本文" {
		t.Fatal(got)
	}
	for _, name := range []string{"binary.md", "invalid.md"} {
		if got := c.ReadInclude(name); got.Kind != pipeline.IncludeNotReadable {
			t.Fatal(got)
		}
	}
	if c.ReadInclude("../outside.md").Kind != pipeline.IncludeMissing || !c.Exists("percent%25.md") {
		t.Fatal("閉じ込めかエンコードが正しくない")
	}
	f, e := os.Create(filepath.Join(root, "large.md"))
	if e != nil {
		t.Fatal(e)
	}
	_ = f.Truncate(MaxIncludeBytes + 1)
	_ = f.Close()
	if got := c.ReadInclude("large.md"); got.Kind != pipeline.IncludeTooLarge {
		t.Fatal(got)
	}
	if c := FileContext("", "無名", "", false); c.ReadInclude != nil || c.Exists != nil {
		t.Fatal("無名の文書がファイルを読める")
	}
}

func TestIndexLowercase(t *testing.T) {
	for input, want := range map[string]string{"AİZ": "ai\u0307z", "ΟΣ": "ος", "ΟΣΑ": "οσα", "AΣ'": "aς'"} {
		if got := lowerJS(input); got != want {
			t.Errorf("%q: %q", input, got)
		}
	}
}
func TestFileIncludeIsolation(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		_ = os.WriteFile(filepath.Join(root, name+".md"), []byte("include:"+name+"-part.md"), 0644)
		_ = os.WriteFile(filepath.Join(root, name+"-part.md"), []byte("本文"+name), 0644)
	}
	s := newSession(t, Options{Root: root, ReleaseDelay: 30 * time.Millisecond, Renderer: renderFunc(including)})
	var a, b collector
	offA := s.SubscribeDocument("a.md", a.add)
	offB := s.SubscribeDocument("b.md", b.add)
	wait(t, func() bool { return a.has("本文a") && b.has("本文b") })
	countB := b.count()
	_ = os.WriteFile(filepath.Join(root, "a-part.md"), []byte("Aだけを変更"), 0644)
	wait(t, func() bool { return a.has("Aだけを変更") })
	if b.count() != countB {
		t.Fatal("別の文書も描いた")
	}
	offA()
	offB()
	wait(t, func() bool { return s.DocumentCount() == 0 })
	s.mu.Lock()
	live := 0
	for _, done := range s.watchDone {
		select {
		case <-done:
		default:
			live++
		}
	}
	s.mu.Unlock()
	wait(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, done := range s.watchDone {
			select {
			case <-done:
			default:
				return false
			}
		}
		return true
	})
	_ = live
}
