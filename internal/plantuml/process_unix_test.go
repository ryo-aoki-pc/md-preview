//go:build !windows

package plantuml

import (
	"os/exec"
	"syscall"
	"testing"
)

func testProcessAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }
func checkProcessOptions(t *testing.T, cmd *exec.Cmd, _ string) {
	t.Helper()
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("子のプロセスグループを作っていません")
	}
}
