//go:build !windows

package plantuml

import (
	"os"
	"os/exec"
	"syscall"
)

func prepareCommand(command string, args []string) (*exec.Cmd, error) {
	cmd := exec.Command(command, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd, nil
}

func killTree(cmd *exec.Cmd) {
	if syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) != nil {
		_ = cmd.Process.Kill()
	}
}

func executable(path string, _ os.FileInfo) bool { return syscall.Access(path, 1) == nil }
