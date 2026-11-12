//go:build !windows

package vscode

import (
	"os"
	"os/exec"
)

func prepareCommand(cmd *exec.Cmd) {}

func executable(info os.FileInfo) bool { return info.Mode().Perm()&0111 != 0 }
