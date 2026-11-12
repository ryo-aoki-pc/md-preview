package vscode

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func prepareCommand(cmd *exec.Cmd) {
	// SW_HIDE は子が開く画面にも響くため、コンソールだけを作らない。
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	cmd.Dir = filepath.Dir(cmd.Path)
}

// Windows のファイルモードには実行の権限がないため、通常のファイルであることだけを確かめる。
func executable(info os.FileInfo) bool { return info.Mode().IsRegular() }
