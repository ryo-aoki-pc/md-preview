package plantuml

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func systemCommand(name string) string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	if !filepath.IsAbs(root) {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32", name)
}

func prepareCommand(command string, args []string) (*exec.Cmd, error) {
	cmd := exec.Command(command, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	ext := filepath.Ext(command)
	if strings.EqualFold(ext, ".cmd") || strings.EqualFold(ext, ".bat") {
		if unsafeCommand(command) {
			return nil, errFailed
		}
		shell := os.Getenv("ComSpec")
		if shell == "" {
			shell = systemCommand("cmd.exe")
		}
		if !filepath.IsAbs(shell) || unsafeCommand(shell) {
			return nil, errFailed
		}
		cmd = exec.Command(shell)
		// cmd の /s に必要な外側の引用も含め、Go に引数を引用し直させない。
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CmdLine: `"` + shell + `" /d /s /c ""` + command + `" ` + strings.Join(args, " ") + `"`}
	}
	return cmd, nil
}

func killTree(cmd *exec.Cmd) {
	killer := exec.Command(systemCommand("taskkill.exe"), "/pid", strconv.Itoa(cmd.Process.Pid), "/t", "/f")
	killer.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if killer.Run() != nil {
		_ = cmd.Process.Kill()
	}
}

func executable(_ string, stat os.FileInfo) bool { return stat.Mode().IsRegular() }
