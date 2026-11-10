package plantuml

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ryo-aoki-pc/md-preview/internal/plantumlsource"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const testSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="160" height="40"><text y="20">試験の図</text></svg>`

type call struct {
	Kind     string   `json:"kind"`
	PID      int      `json:"pid"`
	Source   string   `json:"source"`
	Args     []string `json:"args"`
	Cwd      string   `json:"cwd"`
	Security string   `json:"security"`
	Java     string   `json:"java"`
}

func TestMain(m *testing.M) {
	if os.Getenv("MDPREVIEW_TEST_PLANTUML_CHILD") == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}
	if os.Getenv("MDPREVIEW_TEST_PLANTUML_LOG") != "" {
		fakeMain()
		return
	}
	os.Exit(m.Run())
}

// go test の実行ファイルを偽の plantuml として使い、別の言語や Java を起こさない。
func fakeMain() {
	data, _ := io.ReadAll(os.Stdin)
	source := string(data)
	record := func(c call) {
		f, err := os.OpenFile(os.Getenv("MDPREVIEW_TEST_PLANTUML_LOG"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
		if err != nil {
			os.Exit(3)
		}
		_ = json.NewEncoder(f).Encode(c)
		_ = f.Close()
	}
	cwd, _ := os.Getwd()
	record(call{Kind: "start", PID: os.Getpid(), Source: source, Args: os.Args[1:], Cwd: cwd, Security: os.Getenv("PLANTUML_SECURITY_PROFILE"), Java: os.Getenv("JAVA_TOOL_OPTIONS")})
	if strings.Contains(source, "tree") {
		exe, _ := os.Executable()
		child := exec.Command(exe)
		child.Env = append(os.Environ(), "MDPREVIEW_TEST_PLANTUML_CHILD=1")
		if err := child.Start(); err != nil {
			os.Exit(4)
		}
		record(call{Kind: "child", PID: child.Process.Pid})
	}
	if strings.Contains(source, "hang") || strings.Contains(source, "tree") {
		for {
			time.Sleep(time.Hour)
		}
	}
	if strings.Contains(source, "slow") {
		time.Sleep(300 * time.Millisecond)
	}
	delimiter := ""
	for i, arg := range os.Args {
		if arg == "-pipedelimitor" && i+1 < len(os.Args) {
			delimiter = os.Args[i+1]
		}
	}
	switch {
	case strings.Contains(source, "png"):
		_, _ = os.Stdout.Write([]byte{137, 80, 78, 71, 13, 10})
	case strings.Contains(source, "plain"):
		fmt.Fprint(os.Stdout, "画像ではありません")
	case strings.Contains(source, "invalid-utf8"):
		_, _ = os.Stdout.Write(append(append([]byte("<svg>"), 255), []byte("</svg>")...))
	case strings.Contains(source, "oversize"):
		fmt.Fprint(os.Stdout, strings.Repeat("x", 40000))
	case strings.Contains(source, "empty"):
	case strings.Contains(source, "late-svg"):
		fmt.Fprint(os.Stdout, strings.Repeat("x", 1024)+testSVG)
	case strings.Contains(source, "trailing"):
		fmt.Fprint(os.Stdout, testSVG+"後ろの文字")
	case strings.Contains(source, "no-delimiter"):
		fmt.Fprint(os.Stdout, testSVG)
	default:
		fmt.Fprint(os.Stdout, " \n"+testSVG+"\n"+delimiter+"\r\n")
		if strings.Contains(source, "multiple") {
			fmt.Fprint(os.Stdout, "<svg>二つ目</svg>\n"+delimiter+"\n")
			_, _ = os.Stdout.Write([]byte{255})
		}
	}
	fmt.Fprint(os.Stderr, strings.Repeat("試験用の標準エラー", 20000))
	record(call{Kind: "end", PID: os.Getpid()})
	if strings.Contains(source, "error200") {
		os.Exit(200)
	}
}

type fake struct {
	renderer           *Renderer
	root, command, log string
}

func fakePlantuml(t *testing.T, options Options, direct bool) *fake {
	t.Helper()
	root := filepath.Join(t.TempDir(), "空白 & (試験)")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "calls.jsonl")
	t.Setenv("MDPREVIEW_TEST_PLANTUML_LOG", log)
	t.Setenv("JAVA_TOOL_OPTIONS", "")
	// race 付きの試験用実行ファイルでも、終了後の診断待ちを加えない。
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exe
	if !direct {
		if runtime.GOOS == "windows" {
			command = filepath.Join(root, "plantuml.cmd")
			if err := os.WriteFile(command, []byte(`@"`+exe+`" %*`+"\r\n"), 0600); err != nil {
				t.Fatal(err)
			}
		} else {
			command = filepath.Join(root, "plantuml")
			quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
			if err := os.WriteFile(command, []byte("#!/bin/sh\n"+quote(exe)+" \"$@\"\n"), 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	options.Command = command
	r := New(options)
	t.Cleanup(func() { _ = r.Close() })
	return &fake{r, root, command, log}
}
func (f *fake) calls() []call {
	data, err := os.ReadFile(f.log)
	if err != nil {
		return nil
	}
	var result []call
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		var c call
		if json.Unmarshal(scanner.Bytes(), &c) == nil {
			result = append(result, c)
		}
	}
	return result
}
func (f *fake) starts() []call {
	var out []call
	for _, c := range f.calls() {
		if c.Kind == "start" {
			out = append(out, c)
		}
	}
	return out
}
func until(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("条件の待ち時間を超えました")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func renderOK(t *testing.T, r *Renderer, source string) []byte {
	t.Helper()
	svg, err := r.Render(context.Background(), source)
	if err != nil || string(svg) != testSVG {
		t.Fatalf("描画結果: %q, %v", svg, err)
	}
	return svg
}
func requireError(t *testing.T, err error, word string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), word) || strings.ContainsAny(err.Error(), "\r\n") {
		t.Fatalf("期待する日本語 1 行の誤り (%s): %v", word, err)
	}
}

func TestSource(t *testing.T) {
	if plantumlsource.ID("abc") != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(plantumlsource.ID("日本語")) || plantumlsource.ID("a") == plantumlsource.ID("a\n") {
		t.Fatal("原文の ID が一致しません")
	}
	for _, pair := range [][2]string{{"a", "@startuml\na\n@enduml\n"}, {"a\n", "@startuml\na\n@enduml\n"}, {"a\r", "@startuml\na\r@enduml\n"}, {"@startuml\na\n@enduml", "@startuml\na\n@enduml\n"}, {"@startmindmap\n@endmindmap\n", "@startmindmap\n@endmindmap\n"}, {"\n@startuml\na\n@enduml", "@startuml\n\n@startuml\na\n@enduml\n@enduml\n"}, {"  @startuml\n  a\n  @enduml", "@startuml\n  @startuml\n  a\n  @enduml\n@enduml\n"}} {
		if got := plantumlsource.Input(pair[0]); got != pair[1] {
			t.Errorf("標準入力: %q != %q", got, pair[1])
		}
	}
}

func TestSharedCacheAndInvocation(t *testing.T) {
	f := fakePlantuml(t, Options{}, false)
	t.Setenv("JAVA_TOOL_OPTIONS", "-Dexample.test=true -DPLANTUML_SECURITY_PROFILE=UNSECURE")
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			svg, err := f.renderer.Render(context.Background(), "slow 日本語 <秘密の原文>")
			if err != nil || string(svg) != testSVG {
				t.Errorf("共有の描画: %q %v", svg, err)
			}
		})
	}
	wg.Wait()
	svg := renderOK(t, f.renderer, "slow 日本語 <秘密の原文>")
	svg[0] = 'x'
	renderOK(t, f.renderer, "slow 日本語 <秘密の原文>")
	starts := f.starts()
	if len(starts) != 1 {
		t.Fatalf("共有とキャッシュの実行回数: %d", len(starts))
	}
	c := starts[0]
	if c.Source != plantumlsource.Input("slow 日本語 <秘密の原文>") || c.Security != "SANDBOX" || c.Java != "-Dexample.test=true -DPLANTUML_SECURITY_PROFILE=UNSECURE -Djava.awt.headless=true -DPLANTUML_SECURITY_PROFILE=SANDBOX" {
		t.Fatalf("標準入力または安全の設定が一致しません: %+v", c)
	}
	if !sameTestPath(c.Cwd, os.TempDir()) {
		t.Errorf("作業ディレクトリーが一時領域ではありません: %q", c.Cwd)
	}
	if len(c.Args) != 6 || !reflect.DeepEqual(c.Args[:5], []string{"-pipe", "-tsvg", "-charset", "UTF-8", "-pipedelimitor"}) || !regexp.MustCompile(`^mdpreview-[0-9a-f]{48}$`).MatchString(c.Args[5]) || strings.Contains(strings.Join(c.Args, " "), "日本語") {
		t.Fatalf("引数が一致しません: %q", c.Args)
	}
	cmd, err := prepareCommand(f.command, c.Args)
	if err != nil {
		t.Fatal(err)
	}
	checkProcessOptions(t, cmd, f.command)
}
func sameTestPath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func TestDirectCommand(t *testing.T) {
	f := fakePlantuml(t, Options{}, true)
	renderOK(t, f.renderer, "直接実行")
}

func TestSerialAndFIFOEviction(t *testing.T) {
	f := fakePlantuml(t, Options{MaxCacheBytes: len(testSVG) * 2}, false)
	first := make(chan error, 1)
	go func() { _, err := f.renderer.Render(context.Background(), "slow first"); first <- err }()
	until(t, func() bool { return len(f.starts()) == 1 })
	renderOK(t, f.renderer, "second")
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, c := range f.calls() {
		kinds = append(kinds, c.Kind)
	}
	if !reflect.DeepEqual(kinds, []string{"start", "end", "start", "end"}) {
		t.Fatalf("直列の順序: %v", kinds)
	}
	renderOK(t, f.renderer, "slow first")
	renderOK(t, f.renderer, "third")
	renderOK(t, f.renderer, "second")
	if len(f.starts()) != 3 {
		t.Fatal("新しいキャッシュが失われました")
	}
	renderOK(t, f.renderer, "slow first")
	if len(f.starts()) != 4 {
		t.Fatal("参照されても古いキャッシュから捨てる必要があります")
	}
}

func TestDelimiterAndErrorSVG(t *testing.T) {
	f := fakePlantuml(t, Options{}, false)
	for _, source := range []string{"multiple\n@startuml\nsecond\n@enduml", "error200", "no-delimiter"} {
		renderOK(t, f.renderer, source)
	}
	starts := f.starts()
	if starts[0].Args[5] == starts[1].Args[5] {
		t.Fatal("区切りを使い回しました")
	}
}

func TestInvalidOutputsAndRecovery(t *testing.T) {
	f := fakePlantuml(t, Options{MaxSVGBytes: 2048, MaxCacheBytes: 1}, false)
	for _, source := range []string{"png", "plain", "empty", "invalid-utf8", "oversize", "late-svg", "trailing", "plain"} {
		_, err := f.renderer.Render(context.Background(), source)
		requireError(t, err, "PlantUML")
		if strings.Contains(err.Error(), f.root) || strings.Contains(err.Error(), source) {
			t.Fatal("誤りに端末のパスか原文が含まれました")
		}
	}
	if len(f.starts()) != 8 {
		t.Fatal("失敗を覚えてはいけません")
	}
	renderOK(t, f.renderer, "recovered")
	renderOK(t, f.renderer, "recovered")
	if len(f.starts()) != 10 {
		t.Fatal("キャッシュ上限より大きい成功を覚えてはいけません")
	}
}

func TestTimeoutKillsTreeAndRecovers(t *testing.T) {
	f := fakePlantuml(t, Options{Timeout: 1500 * time.Millisecond}, false)
	_, err := f.renderer.Render(context.Background(), "tree")
	requireError(t, err, "時間切れ")
	calls := f.calls()
	if len(calls) != 2 {
		t.Fatalf("子と子孫の記録: %+v", calls)
	}
	until(t, func() bool {
		for _, c := range calls {
			if testProcessAlive(c.PID) {
				return false
			}
		}
		return true
	})
	renderOK(t, f.renderer, "recovered")
}

func TestCancellationSharingQueueAndRunning(t *testing.T) {
	f := fakePlantuml(t, Options{}, false)
	a, cancelA := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := f.renderer.Render(a, "slow shared"); first <- err }()
	until(t, func() bool { return len(f.starts()) == 1 })
	second := make(chan error, 1)
	go func() { _, err := f.renderer.Render(context.Background(), "slow shared"); second <- err }()
	until(t, func() bool {
		f.renderer.mu.Lock()
		defer f.renderer.mu.Unlock()
		j := f.renderer.jobs[plantumlsource.ID("slow shared")]
		return j != nil && j.waiters == 2
	})
	cancelA()
	requireError(t, <-first, "中止")
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	running, cancelRun := context.WithCancel(context.Background())
	queued, cancelQueue := context.WithCancel(context.Background())
	defer cancelRun()
	defer cancelQueue()
	third, fourth := make(chan error, 1), make(chan error, 1)
	go func() { _, err := f.renderer.Render(running, "hang"); third <- err }()
	until(t, func() bool {
		for _, c := range f.starts() {
			if strings.Contains(c.Source, "hang") {
				return true
			}
		}
		return false
	})
	go func() { _, err := f.renderer.Render(queued, "never-start"); fourth <- err }()
	until(t, func() bool { f.renderer.mu.Lock(); defer f.renderer.mu.Unlock(); return len(f.renderer.queue) > 0 })
	cancelQueue()
	cancelRun()
	requireError(t, <-third, "中止")
	requireError(t, <-fourth, "中止")
	renderOK(t, f.renderer, "after-abort")
	for _, c := range f.starts() {
		if strings.Contains(c.Source, "never-start") {
			t.Fatal("取り消した待ち行列を実行しました")
		}
	}
	pre, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.renderer.Render(pre, "pre-aborted")
	requireError(t, err, "中止")
}

func TestCloseKillsTreeAndQueue(t *testing.T) {
	f := fakePlantuml(t, Options{}, false)
	running, queued := make(chan error, 1), make(chan error, 1)
	go func() { _, err := f.renderer.Render(context.Background(), "tree"); running <- err }()
	until(t, func() bool {
		for _, c := range f.calls() {
			if c.Kind == "child" {
				return true
			}
		}
		return false
	})
	go func() { _, err := f.renderer.Render(context.Background(), "queued"); queued <- err }()
	until(t, func() bool { f.renderer.mu.Lock(); defer f.renderer.mu.Unlock(); return len(f.renderer.queue) > 0 })
	if err := f.renderer.Close(); err != nil {
		t.Fatal(err)
	}
	requireError(t, <-running, "中止")
	requireError(t, <-queued, "中止")
	for _, c := range f.calls() {
		requireGone(t, c.PID, "終了後にプロセスが残っています")
		if strings.Contains(c.Source, "queued") {
			t.Fatal("終了後に待ち行列を実行しました")
		}
	}
	_, err := f.renderer.Render(context.Background(), "after-close")
	requireError(t, err, "終了")
}

func TestStartFailureAndUnsafeCommand(t *testing.T) {
	for _, command := range []string{filepath.Join(t.TempDir(), "missing"), "plantuml"} {
		r := New(Options{Command: command})
		_, err := r.Render(context.Background(), "秘密の原文")
		requireError(t, err, "描けませんでした")
		_ = r.Close()
	}
}

func TestEnvironmentCase(t *testing.T) {
	env := plantumlEnvironment([]string{"Path=保持", "java_tool_options=-Dold=1", "JAVA_TOOL_OPTIONS=-Dold=2", "plantuml_security_profile=UNSECURE", "PLANTUML_SECURITY_PROFILE=LEGACY"}, "windows")
	counts := map[string]int{}
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		counts[strings.ToUpper(name)]++
	}
	if counts["JAVA_TOOL_OPTIONS"] != 1 || counts["PLANTUML_SECURITY_PROFILE"] != 1 || !strings.Contains(strings.Join(env, "\n"), "JAVA_TOOL_OPTIONS=-Dold=2 -Djava.awt.headless=true -DPLANTUML_SECURITY_PROFILE=SANDBOX") {
		t.Fatalf("環境変数を重複させました: %q", env)
	}
}

func TestFindAbsoluteAndRegular(t *testing.T) {
	root := t.TempDir()
	empty, bin := filepath.Join(root, "empty"), filepath.Join(root, "bin")
	for _, folder := range []string{empty, bin} {
		if err := os.Mkdir(folder, 0700); err != nil {
			t.Fatal(err)
		}
	}
	name := "plantuml"
	sep := ":"
	if runtime.GOOS == "windows" {
		name += ".cmd"
		sep = ";"
	}
	command := filepath.Join(bin, name)
	if err := os.WriteFile(command, []byte("試験用"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, name), []byte("カレントディレクトリーの偽のコマンド"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	env := map[string]string{"PATH": strings.Join([]string{"", ".", "bin", empty, bin}, sep), "PATHEXT": ".CMD;.EXE;.BAT"}
	lookup := func(key string) string { return env[key] }
	if got, ok := Find(lookup, runtime.GOOS); !ok || got != command {
		t.Fatalf("絶対パスの探索: %q %v", got, ok)
	}
	env["PATH"] = strings.Join([]string{"", ".", "bin", empty}, sep)
	if _, ok := Find(lookup, runtime.GOOS); ok {
		t.Fatal("空・相対の項目から見つけました")
	}
	if err := os.Mkdir(filepath.Join(empty, name), 0700); err != nil {
		t.Fatal(err)
	}
	env["PATH"] = empty
	if _, ok := Find(lookup, runtime.GOOS); ok {
		t.Fatal("通常ファイル以外を見つけました")
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(command, 0600); err != nil {
			t.Fatal(err)
		}
		env["PATH"] = bin
		if _, ok := Find(lookup, runtime.GOOS); ok {
			t.Fatal("実行権限の無いファイルを見つけました")
		}
	}
}

func TestCancelKillsTree(t *testing.T) {
	f := fakePlantuml(t, Options{}, false)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := f.renderer.Render(ctx, "tree"); result <- err }()
	until(t, func() bool {
		for _, c := range f.calls() {
			if c.Kind == "child" {
				return true
			}
		}
		return false
	})
	cancel()
	requireError(t, <-result, "中止")
	if err := f.renderer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.calls() {
		requireGone(t, c.PID, "取り消し後に子孫が残りました")
	}
}

func TestNewWaiterAfterAllCanceled(t *testing.T) {
	f := fakePlantuml(t, Options{}, false)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := f.renderer.Render(ctx, "slow same"); result <- err }()
	until(t, func() bool { return len(f.starts()) == 1 })
	cancel()
	requireError(t, <-result, "中止")
	renderOK(t, f.renderer, "slow same")
	if len(f.starts()) != 2 {
		t.Fatal("全員の取り消し後の新しい呼び出しが古い実行を共有しました")
	}
}

func TestWindowsAbsolutePathRules(t *testing.T) {
	for _, name := range []string{"", ".", "bin", `\root`, `/root`, `C:relative`} {
		if windowsAbsolute(name) {
			t.Errorf("絶対パスとして受け付けました: %q", name)
		}
	}
	for _, name := range []string{`C:\bin`, `c:/bin`, `\\server\share`, `//server/share/bin`, `\\?\C:\bin`, `\\.\pipe`} {
		if !windowsAbsolute(name) {
			t.Errorf("絶対パスを拒みました: %q", name)
		}
	}
}

// requireGone は、プロセスがいなくなったことを確かめる。POSIX では、止めた直後の孫は、親が回収されて
// init に引き取られるまで「いる」ように見える (kill(pid, 0) は、回収前のゾンビにも成功する) ので、少し待つ。
func requireGone(t *testing.T, pid int, message string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for testProcessAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("%s: %d", message, pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
