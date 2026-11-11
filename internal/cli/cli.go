package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	mdpreview "github.com/ryo-aoki-pc/md-preview"
	"github.com/ryo-aoki-pc/md-preview/internal/assets"
	"github.com/ryo-aoki-pc/md-preview/internal/edit"
	exporthtml "github.com/ryo-aoki-pc/md-preview/internal/export"
	"github.com/ryo-aoki-pc/md-preview/internal/fileopen"
	"github.com/ryo-aoki-pc/md-preview/internal/pagefiles"
	"github.com/ryo-aoki-pc/md-preview/internal/resolve"
	"github.com/ryo-aoki-pc/md-preview/internal/server"
	"github.com/ryo-aoki-pc/md-preview/internal/session"
	"github.com/ryo-aoki-pc/md-preview/internal/stdio"
	"github.com/ryo-aoki-pc/md-preview/internal/version"
	"github.com/ryo-aoki-pc/md-preview/internal/watch"
)

type Dependencies struct {
	Renderer               session.Renderer
	InitializeRenderer     func(context.Context) error
	CreateDiagram          func(context.Context) (server.DiagramRenderer, error)
	LoadAssets             func(string) (server.OfficialAssets, string)
	DefaultAssetsDirectory func() string
	OpenBrowser            func(string, []string) error
	OnStart                func(*App) error
	Render                 func(context.Context, Options, io.Writer, io.Writer) int
	InstallStyle           func(context.Context, Options, io.Writer, io.Writer) int
	// StyleInstallOptions は試験・組み込み用。コマンドの引数では取得元を変えない。
	StyleInstallOptions assets.InstallOptions
}
type IO struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
}
type App struct {
	Server  *server.Server
	Session *session.Session
	URL     string
	Close   func() error
}

// liveFile は画面からの書き込みと見張りの通知を同じ本文で重ねて描かない。
type liveFile struct {
	mu    sync.Mutex
	rev   uint64
	text  string
	state *session.Session
	path  string
}

func (f *liveFile) set(text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rev != 0 && text == f.text {
		return nil
	}
	f.rev++
	if err := f.state.SetDocument(session.Document{Path: f.path, Name: filepath.Base(f.path), Text: text, Rev: f.rev}); err != nil {
		return err
	}
	f.text = text
	return nil
}

func BrowserCommand(url, browser, platform string, env map[string]string) (string, []string) {
	if browser != "" {
		return browser, []string{url}
	}
	switch platform {
	case "windows", "win32":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		return "open", []string{url}
	}
	if env["TERMUX_VERSION"] != "" {
		return "termux-open-url", []string{url}
	}
	return "xdg-open", []string{url}
}
func DefaultAssetsDirectory() string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "md-preview-assets")
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "md-preview-assets")
	}
	base := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(base) {
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "md-preview-assets")
}

func Run(ctx context.Context, args []string, streams IO, deps Dependencies) int {
	if streams.Stdin == nil {
		streams.Stdin = os.Stdin
	}
	if streams.Stdout == nil {
		streams.Stdout = os.Stdout
	}
	if streams.Stderr == nil {
		streams.Stderr = os.Stderr
	}
	var reportMu sync.Mutex
	report := func(text string) {
		reportMu.Lock()
		defer reportMu.Unlock()
		text = reportNewlines.ReplaceAllString(text, " ")
		fmt.Fprintln(streams.Stderr, "mdpreview: "+text)
	}
	o, e := ParseArgs(args)
	if e != nil {
		report(e.Error())
		return 2
	}
	if o.Action != "" {
		switch o.Action {
		case "help":
			fmt.Fprint(streams.Stdout, Help)
		case "version":
			fmt.Fprintln(streams.Stdout, version.Version)
		case "licenses":
			if err := mdpreview.WriteLicenses(streams.Stdout); err != nil {
				report("ライセンスを書き込めません")
				return 1
			}
		}
		return 0
	}
	if o.Command != "install-style" && deps.InitializeRenderer != nil {
		if e = deps.InitializeRenderer(ctx); e != nil {
			if o.Command == "render" {
				report("文書を描画できません")
				return 1
			}
			report("ファイルまたはサーバーを開けません")
			return 2
		}
	}
	var plantuml server.DiagramRenderer
	if o.Plantuml && o.Command != "install-style" && deps.CreateDiagram != nil {
		plantuml, e = deps.CreateDiagram(ctx)
		if e != nil {
			report("ファイルまたはサーバーを開けません")
			return 2
		}
	}
	if plantuml != nil {
		defer plantuml.Close()
	}
	if o.Command == "render" {
		fn := deps.Render
		if fn == nil {
			fn = func(ctx context.Context, o Options, stdout, stderr io.Writer) int {
				result, err := exporthtml.Export(ctx, exporthtml.Options{File: o.File, Root: o.Root, Output: o.Output, Theme: o.Theme, Width: o.Width, Renderer: deps.Renderer, Plantuml: plantuml})
				if err != nil {
					report(err.Error())
					return 1
				}
				if o.Output == "" {
					if _, err := io.WriteString(stdout, result.HTML); err != nil {
						report("HTML を書き込めません")
						return 1
					}
				}
				labels := map[string]string{"reference": "参照", "diagram": "図", "suggestion": "提案", "include": "取り込み", "emoji": "絵文字", "missing": "見つからないメディア", "more": "省略した知らせ"}
				for _, warning := range result.Warnings {
					report(warning)
				}
				for _, notice := range result.Notices {
					label := labels[notice.Kind]
					if label == "" {
						label = "表示の違い"
					}
					report(fmt.Sprintf("%s: %d 件", label, notice.Count))
				}
				return 0
			}
		}
		return fn(ctx, o, streams.Stdout, streams.Stderr)
	}
	if o.Command == "install-style" {
		fn := deps.InstallStyle
		if fn == nil {
			fn = func(ctx context.Context, o Options, stdout, stderr io.Writer) int {
				if o.Check {
					var code int
					var message string
					if deps.StyleInstallOptions.LockBytes == nil {
						code, message = assets.Check(o.Dir)
					} else {
						code, message = assets.CheckWithLock(o.Dir, deps.StyleInstallOptions.LockBytes)
					}
					report(message)
					return code
				}
				options := deps.StyleInstallOptions
				options.Dir, options.Stdout, options.Stderr = o.Dir, stdout, stderr
				return assets.Install(ctx, options)
			}
		}
		return fn(ctx, o, streams.Stdout, streams.Stderr)
	}
	var assets server.OfficialAssets
	notice := ""
	if o.OfficialStyle && deps.LoadAssets != nil {
		dir := o.AssetsDir
		if dir == "" {
			fn := deps.DefaultAssetsDirectory
			if fn == nil {
				fn = DefaultAssetsDirectory
			}
			dir = fn()
		}
		assets, notice = deps.LoadAssets(dir)
	}
	if o.Stdio {
		return stdio.Run(ctx, stdio.Options{Stdin: streams.Stdin, Stdout: streams.Stdout, Port: o.Port, Version: version.Version, Renderer: deps.Renderer, Plantuml: plantuml, Assets: assets, AssetsNotice: notice, OnStart: func(s *server.Server, state *session.Session) error {
			if deps.OnStart != nil {
				return deps.OnStart(&App{Server: s, Session: state, URL: s.URL, Close: func() error {
					if c, ok := streams.Stdin.(io.Closer); ok {
						return c.Close()
					}
					return nil
				}})
			}
			return nil
		}})
	}
	path, e := filepath.Abs(o.File)
	if e == nil {
		path, e = resolve.RealPath(path)
	}
	if e != nil {
		report("指定したファイルを読めません")
		return 2
	}
	info, e := os.Stat(path)
	if e != nil {
		report("ファイルまたはサーバーを開けません")
		return 2
	}
	directory := info.IsDir()
	if !directory && !info.Mode().IsRegular() {
		report("ファイルまたはフォルダーを指定してください")
		return 2
	}
	if directory && o.Root != "" {
		report("フォルダーと --root は一緒に指定できません")
		return 2
	}
	root := ""
	if directory {
		root = path
	}
	if o.Root != "" {
		root, e = filepath.Abs(o.Root)
		if e == nil {
			root, e = resolve.RealPath(root)
		}
		if e != nil {
			report("指定したルートが見つかりません")
			return 2
		}
		st, e := os.Stat(root)
		if e != nil || !st.IsDir() {
			report("ルートにはフォルダーを指定してください")
			return 2
		}
	}
	read := func() (string, error) {
		b, e := fileopen.ReadFile(path)
		return strings.TrimPrefix(resolve.DecodeUTF8(string(b)), "\ufeff"), e
	}
	text := ""
	if !directory {
		text, e = read()
		if e != nil {
			report("ファイルまたはサーバーを開けません")
			return 2
		}
	}
	state, e := session.New(session.Options{Renderer: deps.Renderer, Root: root, Plantuml: plantuml != nil})
	if e != nil {
		report("ファイルまたはサーバーを開けません")
		return 2
	}
	defer state.Close()
	live := &liveFile{state: state, path: path}
	var editor *edit.Service
	pageOptions := pagefiles.Options{Directory: directory}
	if o.Edit {
		options := edit.Options{Host: state, Mode: edit.File}
		if !directory {
			options.Written = func(_ session.Source, text string) { _ = live.set(text) }
		}
		editor = edit.New(options)
		pageOptions.Edit = editor.Config()
	}
	p, e := pagefiles.Load(pageOptions)
	if e != nil {
		report("ファイルまたはサーバーを開けません")
		return 2
	}
	serverOptions := server.Options{Session: state, Page: p, Port: o.Port, Plantuml: plantuml, Assets: assets, AssetsNotice: notice}
	if editor != nil {
		serverOptions.Edit = editor
	}
	httpServer, e := server.New(serverOptions)
	if e != nil {
		if isAddressInUse(e) {
			report("指定したポートは使用中です")
		} else {
			report("ファイルまたはサーバーを開けません")
		}
		return 2
	}
	defer httpServer.Close("exit")
	state.SetCallbacks(func(d session.Data) { _ = httpServer.Broadcast("render", d) }, func(d session.Data) { _ = httpServer.Broadcast("scroll", d) })
	if !directory {
		if live.set(text) != nil {
			report("ファイルまたはサーバーを開けません")
			return 2
		}
	}
	appCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := func() {}
	var watchDone <-chan struct{}
	if !directory {
		stop, watchDone = watch.FileWithDone(path, func() {
			next, err := read()
			if err == nil {
				_ = live.set(next)
			} else if !errors.Is(err, os.ErrNotExist) && appCtx.Err() == nil {
				report("ファイルの変更を読めません")
			}
		}, watch.Options{})
	}
	defer func() {
		stop()
		if watchDone != nil {
			<-watchDone
		}
	}()
	var closeOnce sync.Once
	closeApp := func() error {
		closeOnce.Do(func() { cancel(); stop(); _ = httpServer.Close("exit"); _ = state.Close() })
		return nil
	}
	fmt.Fprintln(streams.Stdout, "プレビュー: "+httpServer.URL)
	if o.Open {
		command, argv := BrowserCommand(httpServer.URL, o.Browser, runtime.GOOS, map[string]string{"TERMUX_VERSION": os.Getenv("TERMUX_VERSION")})
		open := deps.OpenBrowser
		if open == nil {
			open = func(command string, args []string) error {
				cmd := exec.Command(command, args...)
				hideWindow(cmd)
				if err := cmd.Start(); err != nil {
					return err
				}
				go func() {
					if cmd.Wait() != nil {
						report("ブラウザを開けません。表示された URL を自分で開いてください")
					}
				}()
				return nil
			}
		}
		if open(command, argv) != nil {
			report("ブラウザを開けません。表示された URL を自分で開いてください")
		}
	}
	if deps.OnStart != nil {
		if deps.OnStart(&App{Server: httpServer, Session: state, URL: httpServer.URL, Close: closeApp}) != nil {
			_ = closeApp()
			report("ファイルまたはサーバーを開けません")
			return 2
		}
	}
	<-appCtx.Done()
	_ = closeApp()
	return 0
}
