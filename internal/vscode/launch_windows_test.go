package vscode

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPrepareCommandWithoutConsole(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "Code.exe")
	cmd := exec.Command(exe, "cli.js", "--agents", "フォルダー")
	prepareCommand(cmd)
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.HideWindow || cmd.SysProcAttr.CreationFlags != 0x08000000 || cmd.SysProcAttr.CmdLine != "" || cmd.Dir != filepath.Dir(exe) {
		t.Fatalf("コンソール非表示・作業フォルダー・配列の引数の設定が違います: %#v", cmd)
	}
}
