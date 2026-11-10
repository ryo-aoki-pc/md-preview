package plantuml

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func testProcessAlive(pid int) bool {
	h, err := syscall.OpenProcess(0x1000, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	return syscall.GetExitCodeProcess(h, &code) == nil && code == 259
}
func checkProcessOptions(t *testing.T, cmd *exec.Cmd, command string) {
	t.Helper()
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow || !strings.Contains(cmd.SysProcAttr.CmdLine, ` /d /s /c ""`+command+`" `) {
		t.Fatalf("窓の非表示または cmd の引用が違います: %+v", cmd.SysProcAttr)
	}
}

func TestNoComSpec(t *testing.T) {
	f := fakePlantuml(t, Options{}, false)
	t.Setenv("ComSpec", "")
	cmd, err := prepareCommand(f.command, []string{"-pipe"})
	if err != nil || !strings.EqualFold(cmd.Path, filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")) {
		t.Fatalf("ComSpec 無しの実行先: %v %v", cmd, err)
	}
	renderOK(t, f.renderer, "comspec 無し")
	t.Setenv("ComSpec", "cmd.exe")
	if _, err := prepareCommand(f.command, nil); err == nil {
		t.Fatal("名前だけの cmd.exe を使ってはいけません")
	}
}

func TestWindowsExtensionsAndUnsafePaths(t *testing.T) {
	root := t.TempDir()
	unsafe := filepath.Join(root, "%unsafe%")
	if err := os.Mkdir(unsafe, 0700); err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{root, unsafe} {
		for _, ext := range []string{"exe", "cmd", "bat"} {
			if err := os.WriteFile(filepath.Join(folder, "plantuml."+ext), []byte("試験用"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, item := range []struct{ path, ext, want string }{
		{root, ".BAT;.CMD;.EXE", filepath.Join(root, "plantuml.bat")},
		{root, ".COM;.CMD;.EXE", filepath.Join(root, "plantuml.cmd")},
		{unsafe + ";" + root, ".CMD", filepath.Join(root, "plantuml.cmd")},
		{unsafe, ".CMD", ""}, {`"` + root + `"`, ".CMD", ""}, {`\` + strings.TrimLeft(root[2:], `\`), ".CMD", ""},
	} {
		env := map[string]string{"Path": item.path, "PATHEXT": item.ext}
		got, ok := Find(func(key string) string { return env[key] }, "windows")
		if got != item.want || ok != (item.want != "") {
			t.Errorf("PATHEXT とパスの検査: %q, %v; 期待 %q", got, ok, item.want)
		}
	}
	for _, ext := range []string{".cmd", ".bat", ".exe"} {
		r := New(Options{Command: filepath.Join(unsafe, "plantuml"+ext)})
		_, err := r.Render(t.Context(), "秘密")
		requireError(t, err, "描けませんでした")
		_ = r.Close()
	}
	bat := fakePlantuml(t, Options{}, false)
	batCommand := strings.TrimSuffix(bat.command, ".cmd") + ".bat"
	data, err := os.ReadFile(bat.command)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(batCommand, data, 0600); err != nil {
		t.Fatal(err)
	}
	r := New(Options{Command: batCommand})
	defer r.Close()
	renderOK(t, r, "bat の経路")
}
