package vscode

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const maxCodeCMDSize = 64 * 1024

var (
	errRootAbsolute  = errors.New("プレビューのルートは絶対パスで指定してください。")
	errRootDirectory = errors.New("プレビューのルートには存在するフォルダーを指定してください。")
)

// 今の配置は「<版のフォルダー>\resources\app\out\cli.js」。版のフォルダーの無い前の配置も受ける。
var windowsCLI = regexp.MustCompile(`(?m)^[\t ]*"%~dp0\.\.\\Code\.exe"[\t ]+"%~dp0\.\.\\(?:([^"\r\n]+)\\)?resources\\app\\out\\cli\.js"[\t ]+%\*[\t ]*\r?$`)

// AgentsCommand は、PATH の絶対パスの項目だけから code を探し、root を開くコマンドを組む。
// goos には runtime.GOOS を渡す。パスの表現と区切りは、実行中の OS のものを使う。
func AgentsCommand(goos, pathEnv string, environ []string, root string) (Command, error) {
	if !filepath.IsAbs(root) {
		return Command{}, errRootAbsolute
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return Command{}, errRootDirectory
	}
	for _, folder := range filepath.SplitList(pathEnv) {
		if folder == "" || !filepath.IsAbs(folder) {
			continue
		}
		if goos == "windows" {
			if command, ok := windowsCommand(folder, environ, root); ok {
				return command, nil
			}
			continue
		}
		candidate := filepath.Join(folder, "code")
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() || !executable(info) {
			continue
		}
		real, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		return Command{Path: real, Args: []string{"--agents", root}, Env: slices.Clone(environ)}, nil
	}
	return Command{}, ErrNotFound
}

func windowsCommand(bin string, environ []string, root string) (Command, bool) {
	candidate := filepath.Join(bin, "code.cmd")
	if !regularFile(candidate) {
		return Command{}, false
	}
	f, err := os.Open(candidate)
	if err != nil {
		return Command{}, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxCodeCMDSize {
		return Command{}, false
	}
	data, err := io.ReadAll(io.LimitReader(f, maxCodeCMDSize))
	if err != nil {
		return Command{}, false
	}
	install := filepath.Clean(filepath.Join(bin, ".."))
	exe := filepath.Join(install, "Code.exe")
	if !regularFile(exe) {
		return Command{}, false
	}
	for _, match := range windowsCLI.FindAllSubmatch(data, -1) {
		var parts []string
		if len(match[1]) > 0 {
			parts = strings.Split(string(match[1]), `\`)
			if !validRelative(parts) {
				continue
			}
		}
		cliParts := append([]string{install}, parts...)
		cli := filepath.Join(append(cliParts, "resources", "app", "out", "cli.js")...)
		if !regularFile(cli) || !inside(install, cli) {
			continue
		}
		// リンクやジャンクションでも、cli.js と Code.exe の実体をインストール先の外へ出さない。
		realInstall, err := filepath.EvalSymlinks(install)
		if err != nil {
			continue
		}
		realCLI, err := filepath.EvalSymlinks(cli)
		if err != nil || !inside(realInstall, realCLI) {
			continue
		}
		realExe, err := filepath.EvalSymlinks(exe)
		if err != nil || !inside(realInstall, realExe) {
			continue
		}
		env := make([]string, 0, len(environ)+1)
		for _, entry := range environ {
			key, _, _ := strings.Cut(entry, "=")
			if !strings.EqualFold(key, "ELECTRON_RUN_AS_NODE") && !strings.EqualFold(key, "VSCODE_DEV") {
				env = append(env, entry)
			}
		}
		env = append(env, "ELECTRON_RUN_AS_NODE=1")
		return Command{Path: exe, Args: []string{cli, "--agents", root}, Env: env}, true
	}
	return Command{}, false
}

func validRelative(parts []string) bool {
	for _, part := range parts {
		// Windows は名前の末尾の点と空白を落とすので、「.. 」のような名前で上へ出られないよう断る。
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "/:%\"\x00") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
	}
	return len(parts) != 0
}

func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
