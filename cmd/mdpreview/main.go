package main

import (
	"fmt"
	"io"
	"os"

	"github.com/ryo-aoki-pc/md-preview/internal/version"
)

const help = `使い方: mdpreview --version | --help

Markdown を GitLab の描画でプレビューするアプリです。
Go 版への移行中です。この段階で使えるのは次の引数だけです。

  --version  版を表示する
  --help     この使い方を表示する
`

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 {
		switch args[0] {
		case "--version":
			fmt.Fprintln(stdout, version.Version)
			return 0
		case "--help":
			fmt.Fprint(stdout, help)
			return 0
		}
	}
	fmt.Fprintln(stderr, "この引数での起動はまだ Go 版にありません。--help を参照してください。")
	return 2
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
