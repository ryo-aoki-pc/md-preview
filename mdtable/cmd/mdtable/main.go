package main

import (
	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/cli"
	"os"
)

// cli.main と __main__ に対応する入口。
func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
