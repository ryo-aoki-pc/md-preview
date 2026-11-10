package engine

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tetratelabs/wazero/api"
)

func testEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := e.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return e
}

func renderOK(t *testing.T, e *Engine) {
	t.Helper()
	html, err := e.Render(context.Background(), "ok\n", map[string]any{"sourcepos": false})
	if err != nil || html != "<p>ok</p>\n" {
		t.Fatalf("復帰後の描画: html=%q, err=%v", html, err)
	}
}

func TestGolden(t *testing.T) {
	var golden struct {
		Options map[string]any `json:"options"`
		Cases   []struct {
			Name      string         `json:"name"`
			Markdown  string         `json:"markdown"`
			Overrides map[string]any `json:"overrides"`
			HTML      string         `json:"html"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	assertOptions(t, GitLabOptions(), golden.Options)
	if len(golden.Cases) != 23 {
		t.Fatalf("見本の件数: %d、期待値: 23", len(golden.Cases))
	}
	e := testEngine(t)
	for _, item := range golden.Cases {
		t.Run(item.Name, func(t *testing.T) {
			html, err := e.Render(context.Background(), item.Markdown, item.Overrides)
			if err != nil {
				t.Fatal(err)
			}
			if html != item.HTML {
				t.Fatalf("見本と出力が異なる\n取得: %q\n期待: %q", html, item.HTML)
			}
		})
	}
}

func assertOptions(t *testing.T, actual, expected map[string]any) {
	t.Helper()
	got, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("オプションが異なる\n取得: %s\n期待: %s", got, want)
	}
}

func TestGitLabOptions(t *testing.T) {
	data, err := os.ReadFile("gitlab/glfm_markdown.rb")
	if err != nil {
		t.Fatal(err)
	}
	ruby := string(data)
	chars := regexp.MustCompile(`REFERENCE_CHARS = %w\[([^\]]+)\]\.freeze`).FindStringSubmatch(ruby)
	body := regexp.MustCompile(`(?ms)^ {8}OPTIONS = \{\n(.*?)\n {8}\}\.freeze$`).FindStringSubmatch(ruby)
	if len(chars) != 2 || len(body) != 2 {
		t.Fatal("Ruby の OPTIONS を読み取れない")
	}
	expected := map[string]any{}
	linePattern := regexp.MustCompile(`^\s*(\w+): (.+?)\s*$`)
	for _, line := range strings.Split(body[1], "\n") {
		parts := linePattern.FindStringSubmatch(strings.TrimSuffix(line, ","))
		if len(parts) != 3 {
			t.Fatalf("OPTIONS の読めない行: %s", line)
		}
		switch parts[2] {
		case "true", "false":
			expected[parts[1]] = parts[2] == "true"
		case "REFERENCE_CHARS":
			expected[parts[1]] = strings.Fields(chars[1])
		case "Banzai::Renderer::USER_CONTENT_ID_PREFIX":
			expected[parts[1]] = "user-content-"
		default:
			t.Fatalf("OPTIONS の読めない値: %s", line)
		}
	}
	if len(expected) < 25 {
		t.Fatalf("OPTIONS の読めた件数が少ない: %d", len(expected))
	}
	if !strings.Contains(ruby, "github_pre_lang: Feature.disabled?(:use_css_language_classes, resolve_project)") {
		t.Fatal("github_pre_lang の設定が変わった")
	}
	ours := GitLabOptions()
	if ours["github_pre_lang"] != true {
		t.Fatal("github_pre_lang が true ではない")
	}
	delete(ours, "github_pre_lang")
	assertOptions(t, ours, expected)
	// 呼び出し側が map やスライスを書き換えても、次の既定値には影響しない。
	ours["sourcepos"] = false
	ours["only_escape_chars"].([]string)[0] = "変更"
	fresh := GitLabOptions()
	if fresh["sourcepos"] != true || fresh["only_escape_chars"].([]string)[0] != "$" {
		t.Fatal("オプションが呼び出し間で共有されている")
	}
}

type testFunction struct {
	api.Function
	call func(context.Context, ...uint64) ([]uint64, error)
}

func (f testFunction) Call(ctx context.Context, args ...uint64) ([]uint64, error) {
	return f.call(ctx, args...)
}

type testModule struct {
	api.Module
	functions map[string]api.Function
}

func (m *testModule) ExportedFunction(name string) api.Function {
	if f, ok := m.functions[name]; ok {
		return f
	}
	return m.Module.ExportedFunction(name)
}

type callCounts struct {
	instances int
	alloc     int
	free      int
	live      map[region]bool
}

type callHook func(context.Context, api.Module, int, string, []uint64) (bool, []uint64, error)

// 本物の WebAssembly を包み、確保した長さと返した回数も照合する。
func instrument(t *testing.T, e *Engine, hook callHook) *callCounts {
	t.Helper()
	counts := &callCounts{live: make(map[region]bool)}
	instantiate := e.instantiate
	e.instantiate = func(ctx context.Context) (api.Module, error) {
		real, err := instantiate(ctx)
		if err != nil {
			return nil, err
		}
		counts.instances++
		instance := counts.instances
		wrapped := &testModule{Module: real, functions: make(map[string]api.Function)}
		for _, name := range []string{"glfm_alloc", "glfm_free", "glfm_render"} {
			function := real.ExportedFunction(name)
			wrapped.functions[name] = testFunction{Function: function, call: func(ctx context.Context, args ...uint64) ([]uint64, error) {
				if name == "glfm_alloc" {
					counts.alloc++
				}
				if name == "glfm_free" {
					counts.free++
					r := region{uint32(args[0]), uint32(args[1])}
					if !counts.live[r] {
						t.Errorf("確保していない領域か、二度目の解放: %+v", r)
					}
					delete(counts.live, r)
				}
				var result []uint64
				var err error
				handled := false
				if hook != nil {
					handled, result, err = hook(ctx, real, instance, name, args)
				}
				if !handled {
					result, err = function.Call(ctx, args...)
				}
				if err == nil && len(result) == 1 && result[0] != 0 {
					pointer := uint32(result[0])
					if name == "glfm_alloc" {
						counts.live[region{pointer, uint32(args[0])}] = true
					} else if name == "glfm_render" {
						if header, ok := real.Memory().Read(pointer, 5); ok {
							counts.live[region{pointer, 5 + binary.LittleEndian.Uint32(header[1:])}] = true
						}
					}
				}
				return result, err
			}}
		}
		return wrapped, nil
	}
	return counts
}

func TestEmptyInput(t *testing.T) {
	e := testEngine(t)
	counts := instrument(t, e, nil)
	html, err := e.Render(context.Background(), "", map[string]any{"無効": make(chan int)})
	if err != nil || html != "" || counts.instances != 0 {
		t.Fatalf("空の入力でエンジンを呼んだ: html=%q, err=%v, 回数=%d", html, err, counts.instances)
	}
}

func TestOptionErrors(t *testing.T) {
	e := testEngine(t)
	counts := instrument(t, e, nil)
	html, err := e.Render(context.Background(), "x", map[string]any{"sourcepos": "yes"})
	if err == nil || !strings.HasPrefix(err.Error(), "オプションを読めません: ") || html != "" {
		t.Fatalf("誤ったオプションの応答: html=%q, err=%v", html, err)
	}
	if counts.free != 3 || len(counts.live) != 0 {
		t.Fatalf("誤りの応答後の解放: 回数=%d, 残り=%v", counts.free, counts.live)
	}
	renderOK(t, e)
	if counts.instances != 1 || counts.free != 6 || len(counts.live) != 0 {
		t.Fatalf("オプションの誤りで作り直したか、解放が不正: %+v", counts)
	}
	_, err = e.Render(context.Background(), "x", map[string]any{"無効": make(chan int)})
	var jsonError *json.UnsupportedTypeError
	if !errors.As(err, &jsonError) {
		t.Fatalf("JSON にできない値を受け入れた: %v", err)
	}
	renderOK(t, e)
}

func TestBrokenInstance(t *testing.T) {
	trap := errors.New("試験用の異常終了")
	cases := []struct {
		name       string
		operation  string
		occurrence int
		result     []uint64
		cause      error
		message    string
	}{
		{"入力の確保中の異常", "glfm_alloc", 1, nil, trap, abortedMessage},
		{"設定の確保の失敗", "glfm_alloc", 2, []uint64{0}, nil, allocationMessage},
		{"入力へ書けない領域", "glfm_alloc", 1, []uint64{0xffffffff}, nil, abortedMessage},
		{"描画中の異常", "glfm_render", 1, nil, trap, abortedMessage},
		{"結果の確保の失敗", "glfm_render", 1, []uint64{0}, nil, allocationMessage},
		{"結果のヘッダーが範囲外", "glfm_render", 1, []uint64{0xffffffff}, nil, abortedMessage},
		{"解放中の異常", "glfm_free", 1, nil, trap, abortedMessage},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			e := testEngine(t)
			calls := 0
			counts := instrument(t, e, func(_ context.Context, _ api.Module, instance int, name string, _ []uint64) (bool, []uint64, error) {
				if instance == 1 && name == item.operation {
					calls++
					if calls == item.occurrence {
						return true, item.result, item.cause
					}
				}
				return false, nil, nil
			})
			html, err := e.Render(context.Background(), "x\n", nil)
			if err == nil || err.Error() != item.message || html != "" {
				t.Fatalf("異常時の応答: html=%q, err=%v", html, err)
			}
			if item.cause != nil && !errors.Is(err, item.cause) {
				t.Fatalf("元の異常が保持されていない: %v", err)
			}
			if e.module != nil {
				t.Fatal("壊れたインスタンスが残った")
			}
			if item.operation != "glfm_free" && counts.free != 0 {
				t.Fatalf("壊れたインスタンスの領域を返した: %d", counts.free)
			}
			freed := counts.free
			renderOK(t, e)
			if counts.instances != 2 || counts.free != freed+3 {
				t.Fatalf("異常後の作り直し・解放の回数が不正: %+v", counts)
			}
		})
	}
}

func TestInvalidResultUTF8(t *testing.T) {
	e := testEngine(t)
	counts := instrument(t, e, func(ctx context.Context, real api.Module, instance int, name string, args []uint64) (bool, []uint64, error) {
		if instance != 1 || name != "glfm_render" {
			return false, nil, nil
		}
		result, err := real.ExportedFunction(name).Call(ctx, args...)
		if err == nil {
			real.Memory().WriteByte(uint32(result[0])+5, 0xff)
		}
		return true, result, err
	})
	_, err := e.Render(context.Background(), "x", nil)
	if err == nil || err.Error() != "描画エンジンの結果が UTF-8 ではありません。" {
		t.Fatalf("UTF-8 でない結果の応答: %v", err)
	}
	if counts.free != 3 || len(counts.live) != 0 {
		t.Fatalf("UTF-8 の誤りの後に領域が残った: %+v", counts)
	}
}

func TestMemoryLimit(t *testing.T) {
	e := testEngine(t)
	counts := instrument(t, e, nil)
	renderOK(t, e)
	first := e.module
	renderOK(t, e)
	if counts.instances != 1 || first.IsClosed() {
		t.Fatal("上限以下のインスタンスを作り直した")
	}
	e.memoryLimit = 1
	renderOK(t, e)
	if counts.instances != 2 || !first.IsClosed() || e.module == first {
		t.Fatal("上限を超えたインスタンスを捨てていない")
	}
}

func TestDeadlineAndRecovery(t *testing.T) {
	e := testEngine(t)
	renderOK(t, e)
	old := e.module
	markdown := strings.Repeat("> ", 4_000_000)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := e.Render(ctx, markdown, nil)
	elapsed := time.Since(started)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("時間切れを見分けられない: %v", err)
	}
	if elapsed >= time.Second {
		t.Fatalf("時間切れから戻るまで長すぎる: %v", elapsed)
	}
	if e.module != nil || !old.IsClosed() {
		t.Fatal("時間切れ後のインスタンスを捨てていない")
	}
	t.Logf("300 ミリ秒の期限から %v で復帰", elapsed)
	renderOK(t, e)
}

func TestCancellationAndWaiting(t *testing.T) {
	e := testEngine(t)
	renderOK(t, e)
	entered := make(chan struct{})
	instantiate := e.instantiate
	// 描画に入った時点で取り消す。固定時間の待機に依存しない。
	e.discard()
	e.instantiate = func(ctx context.Context) (api.Module, error) {
		real, err := instantiate(ctx)
		if err != nil {
			return nil, err
		}
		wrapped := &testModule{Module: real, functions: map[string]api.Function{}}
		function := real.ExportedFunction("glfm_render")
		wrapped.functions["glfm_render"] = testFunction{Function: function, call: func(ctx context.Context, args ...uint64) ([]uint64, error) {
			close(entered)
			return function.Call(ctx, args...)
		}}
		return wrapped, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := e.Render(ctx, strings.Repeat("> ", 4_000_000), nil)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("描画が始まらなかった")
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer waitCancel()
	_, err := e.Render(waitCtx, "待機中", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("順番待ちを取り消せない: %v", err)
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer closeCancel()
	if err := e.Close(closeCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Close の順番待ちを取り消せない: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("描画の取り消しを見分けられない: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("取り消した描画が戻らなかった")
	}
	e.instantiate = instantiate
	renderOK(t, e)
}

func TestConcurrentRender(t *testing.T) {
	e := testEngine(t)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			markdown := fmt.Sprintf("日本語 %d **強調** 😀\n", i)
			want := fmt.Sprintf("<p>日本語 %d <strong>強調</strong> 😀</p>\n", i)
			for range 3 {
				html, err := e.Render(context.Background(), markdown, map[string]any{"sourcepos": false})
				if err != nil || html != want {
					t.Errorf("同時の描画 %d: html=%q, err=%v", i, html, err)
				}
			}
		})
	}
	wg.Wait()
}

func TestInvalidInputUTF8(t *testing.T) {
	e := testEngine(t)
	counts := instrument(t, e, nil)
	// Go の文字列は、UTF-8 として正しくないバイト列も持てる。エンジンが誤りで返し、領域を返して、次も描けること。
	html, err := e.Render(context.Background(), "# 題\n\xff\xfe\n", nil)
	if err == nil || err.Error() != "Markdown が UTF-8 ではありません。" || html != "" {
		t.Fatalf("UTF-8 でない入力の応答: html=%q, err=%v", html, err)
	}
	if len(counts.live) != 0 {
		t.Fatalf("UTF-8 でない入力の後に領域が残った: %v", counts.live)
	}
	renderOK(t, e)
	if counts.instances != 1 {
		t.Fatalf("UTF-8 でない入力で作り直した: %+v", counts)
	}
}

func TestClose(t *testing.T) {
	e := testEngine(t)
	renderOK(t, e)
	current := e.module
	// 取り消し済みの ctx では、閉じずに戻る。その後に閉じ直せる。
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := e.Close(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("取り消し済みの Close: %v", err)
	}
	if current.IsClosed() {
		t.Fatal("取り消し済みの Close でインスタンスが閉じた")
	}
	renderOK(t, e)
	if err := e.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 空の入力はエンジンを使わないので、閉じた後も空を返す (エンジンを呼ぶ前に返す)。
	if html, err := e.Render(context.Background(), "", nil); err != nil || html != "" {
		t.Fatalf("Close 後の空の入力: html=%q, err=%v", html, err)
	}
	if !current.IsClosed() {
		t.Fatal("Close 後もインスタンスが開いている")
	}
	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("二度目の Close: %v", err)
	}
	if _, err := e.Render(context.Background(), "閉じた後", nil); err == nil {
		t.Fatal("Close 後も描画できた")
	}
}
