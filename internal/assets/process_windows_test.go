package assets

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func createTestLink(t *testing.T, target, name string, directory bool) {
	t.Helper()
	if directory {
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		cmd := exec.Command(filepath.Join(root, "System32", "cmd.exe"), "/d", "/c", "mklink", "/J", name, target)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("試験用のジャンクションを作れません: %v %s", err, output)
		}
		return
	}
	if err := os.Symlink(target, name); err != nil {
		if os.IsPermission(err) {
			t.Skip("シンボリックリンクを作る権限がありません")
		}
		t.Fatal(err)
	}
}
