// Package cli は mdpreview の引数とプレビューの起動を扱う。
package cli

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const Help = "使い方: mdpreview [--port <番号>] [--root <フォルダー>] [--no-open] [--browser <実行ファイル>] [--assets-dir <保存先>] [--no-official-style] [--no-plantuml] [--edit] [--review] <file.md | フォルダー>\n       mdpreview --stdio [--port <番号>] [--assets-dir <保存先>] [--no-official-style] [--no-plantuml]\n       mdpreview install-style [--dir <保存先>] [--check]\n       mdpreview render [--root <フォルダー>] [--theme system|light|dark] [--width fluid|fixed] [--no-plantuml] [-o <out.html>] <file.md>\nPATH に plantuml があれば、PlantUML の図を手元で描きます。\n--no-plantuml を付けると、原文のコードブロックのまま表示します。\n--edit: プレビューの画面で編集して、Markdown のファイルへ書く。\n       mdpreview --help | --version | --licenses\n"

type Options struct {
	Command, Action, File, Root, Browser, AssetsDir, Dir, Theme, Width, Output string
	Port                                                                       int
	Open, OfficialStyle, Plantuml, Stdio, Check, Edit, Review                  bool
}

var reportNewlines = regexp.MustCompile(`[\r\n]+`)

func ParseArgs(argv []string) (Options, error) {
	o := Options{Open: true, OfficialStyle: true, Plantuml: true}
	positional := false
	if len(argv) > 0 && argv[0] == "render" {
		o.Command = "render"
		o.Theme = "system"
		o.Width = "fluid"
		for i := 1; i < len(argv); i++ {
			arg := argv[i]
			if !positional && arg == "--" {
				positional = true
				continue
			}
			if !positional && arg == "--no-plantuml" {
				o.Plantuml = false
				continue
			}
			if !positional && (arg == "--root" || arg == "--theme" || arg == "--width" || arg == "-o") {
				i++
				if i == len(argv) || argv[i] == "" || strings.HasPrefix(argv[i], "-") {
					return o, fmt.Errorf("%s の値を指定してください", arg)
				}
				switch arg {
				case "--root":
					o.Root = argv[i]
				case "--theme":
					o.Theme = argv[i]
				case "--width":
					o.Width = argv[i]
				case "-o":
					o.Output = argv[i]
				}
			} else {
				if !positional && strings.HasPrefix(arg, "-") {
					return o, fmt.Errorf("不明なオプションです: %s", arg)
				}
				if o.File != "" {
					return o, errors.New("文書は1つだけ指定してください")
				}
				o.File = arg
			}
		}
		if o.File == "" {
			return o, errors.New("Markdown ファイルを指定してください")
		}
		if o.Theme != "system" && o.Theme != "light" && o.Theme != "dark" {
			return o, errors.New("配色は system・light・dark で指定してください")
		}
		if o.Width != "fluid" && o.Width != "fixed" {
			return o, errors.New("本文幅は fluid・fixed で指定してください")
		}
		return o, nil
	}
	if len(argv) > 0 && argv[0] == "install-style" {
		o.Command = "install-style"
		for i := 1; i < len(argv); i++ {
			if argv[i] == "--check" {
				o.Check = true
			} else if argv[i] == "--dir" && i+1 < len(argv) && argv[i+1] != "" && !strings.HasPrefix(argv[i+1], "-") {
				i++
				o.Dir = argv[i]
			} else {
				return o, errors.New("使い方: mdpreview install-style [--dir <保存先>] [--check]")
			}
		}
		return o, nil
	}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if !positional && arg == "--" {
			positional = true
			continue
		}
		if !positional && (arg == "--help" || arg == "--version" || arg == "--licenses") {
			if len(argv) != 1 {
				return o, errors.New("--help・--version・--licenses は単独で指定してください")
			}
			o.Action = arg[2:]
			return o, nil
		}
		if !positional {
			switch arg {
			case "--review":
				o.Review = true
				continue
			case "--edit":
				o.Edit = true
				continue
			case "--stdio":
				o.Stdio = true
				continue
			case "--no-open":
				o.Open = false
				continue
			case "--no-official-style":
				o.OfficialStyle = false
				continue
			case "--no-plantuml":
				o.Plantuml = false
				continue
			}
			if arg == "--port" || arg == "--root" || arg == "--browser" || arg == "--assets-dir" {
				i++
				if i == len(argv) || argv[i] == "" || strings.HasPrefix(argv[i], "--") {
					return o, fmt.Errorf("%s の値を指定してください", arg)
				}
				value := argv[i]
				switch arg {
				case "--port":
					n, e := strconv.ParseUint(value, 10, 16)
					if e != nil || !regexp.MustCompile(`^\d+$`).MatchString(value) {
						return o, errors.New("ポート番号は 0 から 65535 の整数で指定してください")
					}
					o.Port = int(n)
				case "--root":
					o.Root = value
				case "--browser":
					o.Browser = value
				case "--assets-dir":
					o.AssetsDir = value
				}
				continue
			}
			if strings.HasPrefix(arg, "-") {
				return o, fmt.Errorf("不明なオプションです: %s", arg)
			}
		}
		if o.File != "" {
			return o, errors.New("文書は1つだけ指定してください")
		}
		o.File = arg
	}
	if o.Stdio {
		if o.File != "" || !o.Open || o.Browser != "" || o.Root != "" || o.Edit || o.Review {
			return o, errors.New("--stdio は --port・--assets-dir・--no-official-style・--no-plantuml 以外のオプションやファイルと一緒に指定できません")
		}
		return o, nil
	}
	if o.File == "" {
		return o, errors.New("Markdown ファイルまたはフォルダーを指定してください")
	}
	return o, nil
}
