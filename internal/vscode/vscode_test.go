package vscode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

const fakeMode = "MDPREVIEW_TEST_VSCODE_MODE"

type processRecord struct {
	Args   []string
	Cwd    string
	Node   string
	Dev    string
	Input  string
	Marker string
}

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeMode); mode != "" {
		// 試験の実行ファイルを偽の VS Code として使い、本物の VS Code は起こさない。
		if mode == "hang" {
			for {
				time.Sleep(time.Hour)
			}
		}
		cwd, _ := os.Getwd()
		input, _ := io.ReadAll(os.Stdin)
		record := processRecord{
			Args: os.Args[1:], Cwd: cwd, Input: string(input),
			Node: os.Getenv("ELECTRON_RUN_AS_NODE"), Dev: os.Getenv("VSCODE_DEV"),
			Marker: os.Getenv("MDPREVIEW_TEST_VSCODE_MARKER"),
		}
		data, err := json.Marshal(record)
		if err != nil || os.WriteFile(os.Getenv("MDPREVIEW_TEST_VSCODE_LOG"), data, 0600) != nil {
			os.Exit(3)
		}
		_, _ = os.Stdout.WriteString("試験用の標準出力")
		_, _ = os.Stderr.WriteString("試験用の標準エラー")
		if mode == "exit" {
			os.Exit(23)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0700); err != nil {
		t.Fatal(err)
	}
}

func batchContents(relative string) string {
	return "@echo off\r\nsetlocal\r\nset VSCODE_DEV=\r\nset ELECTRON_RUN_AS_NODE=1\r\n" +
		`"%~dp0..\Code.exe" "%~dp0..\` + relative + `\resources\app\out\cli.js" %*` + "\r\n" +
		"IF %ERRORLEVEL% NEQ 0 EXIT /b %ERRORLEVEL%\r\nendlocal\r\n"
}

type windowsInstall struct {
	bin, exe, cli string
}

func fakeWindowsInstall(t *testing.T) windowsInstall {
	t.Helper()
	install := t.TempDir()
	fixture := windowsInstall{
		bin: filepath.Join(install, "bin"), exe: filepath.Join(install, "Code.exe"),
		cli: filepath.Join(install, "版の名前", "nested", "resources", "app", "out", "cli.js"),
	}
	writeFile(t, filepath.Join(fixture.bin, "code.cmd"), batchContents(`版の名前\nested`))
	writeFile(t, fixture.exe, "偽の実行ファイル")
	writeFile(t, fixture.cli, "偽の CLI")
	return fixture
}

func pathList(items ...string) string { return strings.Join(items, string(os.PathListSeparator)) }

func specialRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "空白 日本語 & ^ % ! ( ) ' ; +,=[]{}$@#")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestAgentsCommandWindows(t *testing.T) {
	fixture := fakeWindowsInstall(t)
	root := specialRoot(t)
	backing := []string{"KEEP=そのまま", "ELECTRON_RUN_AS_NODE=0", "electron_run_as_node=x", "VSCODE_DEV=1", "vscode_dev=2", "PADDING=変更しない"}
	environ := backing[:5]
	before := append([]string(nil), backing...)
	command, err := AgentsCommand("windows", fixture.bin, environ, root)
	if err != nil {
		t.Fatal(err)
	}
	want := Command{
		Path: fixture.exe, Args: []string{fixture.cli, "--agents", root},
		Env: []string{"KEEP=そのまま", "ELECTRON_RUN_AS_NODE=1"},
	}
	if !reflect.DeepEqual(command, want) {
		t.Fatalf("起動コマンドが違います: %#v、期待値: %#v", command, want)
	}
	if !reflect.DeepEqual(backing, before) {
		t.Fatalf("呼び出し元の環境が変わりました: %#v", backing)
	}
}

func TestAgentsCommandWindowsSkipsRelativePATH(t *testing.T) {
	fixture := fakeWindowsInstall(t)
	t.Chdir(filepath.Dir(fixture.exe))
	writeFile(t, "code.cmd", batchContents(`版の名前\nested`))
	for _, path := range []string{"", ".", "bin", pathList("", ".", "bin", "")} {
		if _, err := AgentsCommand("windows", path, nil, filepath.Dir(fixture.exe)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("相対の PATH を飛ばしませんでした: %q、%v", path, err)
		}
	}
	command, err := AgentsCommand("windows", pathList("", ".", "bin", fixture.bin), nil, filepath.Dir(fixture.exe))
	if err != nil || command.Path != fixture.exe {
		t.Fatalf("絶対パスの項目を見つけませんでした: %#v、%v", command, err)
	}
}

func TestAgentsCommandWindowsInvalidCandidates(t *testing.T) {
	cases := []struct {
		name   string
		change func(*testing.T, windowsInstall)
	}{
		{"形が違う", func(t *testing.T, f windowsInstall) {
			writeFile(t, filepath.Join(f.bin, "code.cmd"), `start Code.exe --agents %*`)
		}},
		{"余分な実行文", func(t *testing.T, f windowsInstall) {
			writeFile(t, filepath.Join(f.bin, "code.cmd"), strings.Replace(batchContents(`版の名前\nested`), `%*`, `%* && echo extra`, 1))
		}},
		{"親の名前", func(t *testing.T, f windowsInstall) {
			writeFile(t, filepath.Join(f.bin, "code.cmd"), batchContents(`..\版の名前`))
		}},
		{"現在の名前", func(t *testing.T, f windowsInstall) {
			writeFile(t, filepath.Join(f.bin, "code.cmd"), batchContents(`版の名前\.\nested`))
		}},
		{"空の名前", func(t *testing.T, f windowsInstall) {
			writeFile(t, filepath.Join(f.bin, "code.cmd"), batchContents(`版の名前\\nested`))
		}},
		{"環境変数", func(t *testing.T, f windowsInstall) {
			writeFile(t, filepath.Join(f.bin, "code.cmd"), batchContents(`%VERSION%`))
		}},
		{"コロン", func(t *testing.T, f windowsInstall) {
			writeFile(t, filepath.Join(f.bin, "code.cmd"), batchContents(`C:\版の名前`))
		}},
		{"引用符", func(t *testing.T, f windowsInstall) {
			writeFile(t, filepath.Join(f.bin, "code.cmd"), batchContents(`版"の名前`))
		}},
		{"別の区切り", func(t *testing.T, f windowsInstall) {
			writeFile(t, filepath.Join(f.bin, "code.cmd"), batchContents(`../版の名前`))
		}},
		{"実行ファイルがない", func(t *testing.T, f windowsInstall) {
			if err := os.Remove(f.exe); err != nil {
				t.Fatal(err)
			}
		}},
		{"CLI がない", func(t *testing.T, f windowsInstall) {
			if err := os.Remove(f.cli); err != nil {
				t.Fatal(err)
			}
		}},
		{"実行ファイルがフォルダー", func(t *testing.T, f windowsInstall) {
			if err := os.Remove(f.exe); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(f.exe, 0700); err != nil {
				t.Fatal(err)
			}
		}},
		{"バッチがフォルダー", func(t *testing.T, f windowsInstall) {
			path := filepath.Join(f.bin, "code.cmd")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
		}},
		{"上限を超えるバッチ", func(t *testing.T, f windowsInstall) {
			writeFile(t, filepath.Join(f.bin, "code.cmd"), batchContents(`版の名前\nested`)+strings.Repeat("x", maxCodeCMDSize))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			invalid := fakeWindowsInstall(t)
			tc.change(t, invalid)
			root := t.TempDir()
			if _, err := AgentsCommand("windows", invalid.bin, nil, root); !errors.Is(err, ErrNotFound) {
				t.Fatalf("無効な候補を断りませんでした: %v", err)
			}
			valid := fakeWindowsInstall(t)
			command, err := AgentsCommand("windows", pathList(invalid.bin, valid.bin), nil, root)
			if err != nil || command.Path != valid.exe {
				t.Fatalf("次の候補を使いませんでした: %#v、%v", command, err)
			}
		})
	}
}

func TestAgentsCommandWindowsBatchBoundary(t *testing.T) {
	fixture := fakeWindowsInstall(t)
	line := " \t" + strings.TrimSuffix(strings.Split(batchContents(`版の名前\nested`), "\r\n")[4], "\r") + "\t \r\n"
	contents := strings.Repeat(" ", maxCodeCMDSize-len(line)-2) + "\r\n" + line
	writeFile(t, filepath.Join(fixture.bin, "code.cmd"), contents)
	if len(contents) != maxCodeCMDSize {
		t.Fatal("バッチの試験用の大きさが違います")
	}
	if _, err := AgentsCommand("windows", fixture.bin, nil, t.TempDir()); err != nil {
		t.Fatalf("64 KiB ちょうどのバッチを断りました: %v", err)
	}
}

func TestAgentsCommandWindowsRejectsEscapingLink(t *testing.T) {
	fixture := fakeWindowsInstall(t)
	outside := filepath.Join(t.TempDir(), "cli.js")
	writeFile(t, outside, "外側の CLI")
	if err := os.Remove(fixture.cli); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, fixture.cli); err != nil {
		t.Skipf("シンボリックリンクを作れない環境です: %v", err)
	}
	if _, err := AgentsCommand("windows", fixture.bin, nil, t.TempDir()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("インストール先の外へのリンクを断りませんでした: %v", err)
	}
}

func TestAgentsCommandInvalidRoot(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "file")
	writeFile(t, file, "フォルダーではありません")
	for _, goos := range []string{"windows", "linux"} {
		for _, root := range []string{"", ".", "relative", filepath.Join(base, "ない"), file} {
			command, err := AgentsCommand(goos, "", nil, root)
			if err == nil || errors.Is(err, ErrNotFound) || !reflect.DeepEqual(command, Command{}) {
				t.Fatalf("不正なルートを断りませんでした: %s、%q、%#v、%v", goos, root, command, err)
			}
		}
	}
}

func TestAgentsCommandUnix(t *testing.T) {
	bin := t.TempDir()
	code := filepath.Join(bin, "code")
	writeFile(t, code, "偽の code")
	root := specialRoot(t)
	environ := []string{"KEEP=そのまま", "ELECTRON_RUN_AS_NODE=0", "VSCODE_DEV=1"}
	command, err := AgentsCommand("linux", pathList("", ".", "bin", bin), environ, root)
	if err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(code)
	if err != nil {
		t.Fatal(err)
	}
	want := Command{Path: real, Args: []string{"--agents", root}, Env: environ}
	if !reflect.DeepEqual(command, want) {
		t.Fatalf("Linux のコマンドが違います: %#v、期待値: %#v", command, want)
	}
	command.Env[0] = "KEEP=子の変更"
	if environ[0] != "KEEP=そのまま" {
		t.Fatal("呼び出し元の環境が変わりました")
	}
	for _, env := range [][]string{nil, {}} {
		command, err := AgentsCommand("linux", bin, env, root)
		if err != nil || !reflect.DeepEqual(command.Env, env) {
			t.Fatalf("nil と空の子の環境を区別しませんでした: %#v、%v", command.Env, err)
		}
	}
	t.Chdir(bin)
	if _, err := AgentsCommand("linux", pathList("", ".", "bin"), nil, root); !errors.Is(err, ErrNotFound) {
		t.Fatalf("相対の PATH を飛ばしませんでした: %v", err)
	}
}

func TestAgentsCommandUnixSymlink(t *testing.T) {
	target := filepath.Join(t.TempDir(), "本体")
	writeFile(t, target, "偽の code の本体")
	bin := t.TempDir()
	if err := os.Symlink(target, filepath.Join(bin, "code")); err != nil {
		t.Skipf("シンボリックリンクを作れない環境です: %v", err)
	}
	command, err := AgentsCommand("linux", bin, nil, t.TempDir())
	real, realErr := filepath.EvalSymlinks(target)
	if err != nil || realErr != nil || command.Path != real {
		t.Fatalf("シンボリックリンクを解決しませんでした: %#v、%v、%v", command, err, realErr)
	}
}

func TestAgentsCommandUnixSkipsNonExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows のファイルモードには実行の権限がありません")
	}
	invalid, valid := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(invalid, "code"), "実行できません")
	if err := os.Chmod(filepath.Join(invalid, "code"), 0600); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(valid, "code"), "実行できます")
	root := t.TempDir()
	if _, err := AgentsCommand("linux", invalid, nil, root); !errors.Is(err, ErrNotFound) {
		t.Fatalf("実行できない候補を断りませんでした: %v", err)
	}
	command, err := AgentsCommand("linux", pathList(invalid, valid), nil, root)
	real, _ := filepath.EvalSymlinks(filepath.Join(valid, "code"))
	if err != nil || command.Path != real {
		t.Fatalf("次の実行できる候補を見つけませんでした: %#v、%v", command, err)
	}
}

func nativeInstall(t *testing.T) (root string, want Command) {
	t.Helper()
	root = specialRoot(t)
	var bin string
	if runtime.GOOS == "windows" {
		fixture := fakeWindowsInstall(t)
		bin = fixture.bin
	} else {
		bin = t.TempDir()
		writeFile(t, filepath.Join(bin, "code"), "偽の code")
	}
	t.Setenv("PATH", bin)
	var err error
	want, err = AgentsCommand(runtime.GOOS, bin, os.Environ(), root)
	if err != nil {
		t.Fatal(err)
	}
	return root, want
}

func TestLauncherCommandAndDeadline(t *testing.T) {
	root, want := nativeInstall(t)
	called := false
	l := Launcher{Run: func(ctx context.Context, command Command) error {
		called = true
		if !reflect.DeepEqual(command, want) {
			t.Fatalf("渡されたコマンドが違います: %#v、期待値: %#v", command, want)
		}
		deadline, ok := ctx.Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining > 30*time.Second || remaining < 29*time.Second {
			t.Fatalf("30秒の期限がありません: %v、%v", deadline, ok)
		}
		return nil
	}}
	if err := l.Open(context.Background(), root); err != nil || !called {
		t.Fatalf("差し替えた関数を呼びませんでした: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	wantDeadline, _ := ctx.Deadline()
	l.Run = func(ctx context.Context, _ Command) error {
		deadline, ok := ctx.Deadline()
		if !ok || deadline != wantDeadline {
			t.Fatalf("親の短い期限を引き継ぎませんでした: %v", deadline)
		}
		return nil
	}
	if err := l.Open(ctx, root); err != nil {
		t.Fatal(err)
	}
}

func TestLauncherErrors(t *testing.T) {
	root, _ := nativeInstall(t)
	cause := errors.New("試験用の起動失敗")
	for _, failure := range []error{cause, &exec.ExitError{}, context.DeadlineExceeded, context.Canceled} {
		l := Launcher{Run: func(context.Context, Command) error { return failure }}
		err := l.Open(context.Background(), root)
		if !errors.Is(err, ErrLaunch) || !errors.Is(err, failure) || !strings.HasPrefix(err.Error(), ErrLaunch.Error()+"（") || !strings.HasSuffix(err.Error(), "）。") {
			t.Fatalf("起動失敗を包みませんでした: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l := Launcher{Run: func(ctx context.Context, _ Command) error {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("親の取り消しが伝わりませんでした: %v", ctx.Err())
		}
		return &exec.ExitError{}
	}}
	if err := l.Open(ctx, root); !errors.Is(err, ErrLaunch) || !errors.Is(err, context.Canceled) {
		t.Fatalf("取り消しを包みませんでした: %v", err)
	}
	called := false
	l.Run = func(context.Context, Command) error { called = true; return nil }
	t.Setenv("PATH", "")
	if err := l.Open(context.Background(), root); !errors.Is(err, ErrNotFound) || errors.Is(err, ErrLaunch) || called {
		t.Fatalf("見つからないコマンドを起こそうとしました: %v", err)
	}
	if err := l.Open(context.Background(), "relative"); err == nil || called {
		t.Fatalf("不正なルートで起動関数を呼びました: %v", err)
	}
}

func TestMessageDoesNotExposePaths(t *testing.T) {
	private := `C:\private\Code.exe`
	cause := &os.PathError{Op: "open", Path: private, Err: os.ErrPermission}
	for _, err := range []error{cause, errors.New("起動に失敗: /private/code"), fmt.Errorf("%s: %w", private, ErrNotFound),
		fmt.Errorf("%s: %w", private, &launchError{cause: cause, reason: "コマンドを開始できませんでした"})} {
		message := Message(err)
		if strings.Contains(message, "private") || strings.Contains(message, private) || message == "" {
			t.Fatalf("起動の知らせに端末のパスが入りました: %q", message)
		}
	}
}

func copyTestExecutable(t *testing.T, target string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0700); err != nil {
		t.Fatal(err)
	}
}

func fakeEnvironment(t *testing.T, mode string) string {
	t.Helper()
	log := filepath.Join(t.TempDir(), "起動.json")
	t.Setenv(fakeMode, mode)
	t.Setenv("MDPREVIEW_TEST_VSCODE_LOG", log)
	t.Setenv("MDPREVIEW_TEST_VSCODE_MARKER", "子に渡す値")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	return log
}

func readRecord(t *testing.T, log string) processRecord {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	var record processRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func TestWindowsCommandRealProcess(t *testing.T) {
	log := fakeEnvironment(t, "success")
	t.Setenv("ELECTRON_RUN_AS_NODE", "0")
	t.Setenv("VSCODE_DEV", "親の値")
	fixture := fakeWindowsInstall(t)
	copyTestExecutable(t, fixture.exe)
	root := specialRoot(t)
	command, err := AgentsCommand("windows", fixture.bin, os.Environ(), root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runCommand(ctx, command); err != nil {
		t.Fatal(err)
	}
	record := readRecord(t, log)
	if !reflect.DeepEqual(record.Args, []string{fixture.cli, "--agents", root}) || record.Node != "1" || record.Dev != "" || record.Input != "" || record.Marker != "子に渡す値" {
		t.Fatalf("子に渡った値が違います: %#v", record)
	}
	if runtime.GOOS == "windows" {
		cwd, err := filepath.EvalSymlinks(record.Cwd)
		install, installErr := filepath.EvalSymlinks(filepath.Dir(fixture.exe))
		if err != nil || installErr != nil || !strings.EqualFold(cwd, install) {
			t.Fatalf("作業フォルダーがインストール先ではありません: %q、%q、%v、%v", cwd, install, err, installErr)
		}
	}
	if os.Getenv("ELECTRON_RUN_AS_NODE") != "0" || os.Getenv("VSCODE_DEV") != "親の値" {
		t.Fatal("親の環境が変わりました")
	}
}

func TestOpenRealProcess(t *testing.T) {
	for _, mode := range []string{"success", "exit", "start", "hang"} {
		t.Run(mode, func(t *testing.T) {
			log := fakeEnvironment(t, mode)
			root, command := nativeInstall(t)
			if mode != "start" {
				copyTestExecutable(t, command.Path)
			}
			l := Launcher{}
			if mode == "start" {
				// 探索の後、起動する前に実行ファイルが消えた場合を再現する。
				l.Run = func(ctx context.Context, command Command) error {
					if err := os.Remove(command.Path); err != nil {
						return err
					}
					return runCommand(ctx, command)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if mode == "hang" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 150*time.Millisecond)
				defer cancel()
			}
			var err error
			if mode == "start" {
				err = l.Open(ctx, root)
			} else {
				err = Open(ctx, root)
			}
			if mode == "success" {
				if err != nil {
					t.Fatal(err)
				}
				record := readRecord(t, log)
				if !reflect.DeepEqual(record.Args, command.Args) || record.Input != "" || record.Marker != "子に渡す値" {
					t.Fatalf("実際のプロセスの引数・環境・標準入力が違います: %#v", record)
				}
			} else if !errors.Is(err, ErrLaunch) {
				t.Fatalf("実際の起動失敗を包みませんでした: %v", err)
			}
			if mode == "exit" {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 23 || !strings.Contains(err.Error(), "23") {
					t.Fatalf("終了コードを保持しませんでした: %v", err)
				}
			}
			if mode == "hang" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("時間切れを保持しませんでした: %v", err)
			}
		})
	}
}
