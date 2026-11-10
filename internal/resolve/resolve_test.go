package resolve

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPaths(t *testing.T) {
	root := t.TempDir()
	if e := os.Mkdir(filepath.Join(root, "docs"), 0755); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"docs/画像 a.png", "percent%Ʊ", "percent%ZZ"} {
		if e := os.WriteFile(filepath.Join(root, name), []byte("本文"), 0644); e != nil {
			t.Fatal(e)
		}
	}
	var rootErr error
	root, rootErr = filepath.EvalSymlinks(root)
	if rootErr != nil {
		t.Fatal(rootErr)
	}
	for _, win := range []bool{false, true} {
		t.Run(map[bool]string{false: "POSIXの検査", true: "Windowsの検査"}[win], func(t *testing.T) {
			for _, p := range []string{"docs/画像 a.png", "docs/%E7%94%BB%E5%83%8F%20a.png", "./docs//画像 a.png", "/docs/画像 a.png", "percent%Ʊ", "percent%ZZ"} {
				if r := Resolve(root, p, win); r.Status != 200 {
					t.Errorf("%q: %+v", p, r)
				}
			}
			for _, p := range []string{"", ".", "/", "missing", "COM10", "lpt10", "console"} {
				if r := Resolve(root, p, win); r.Status != 404 {
					t.Errorf("%q: %+v", p, r)
				}
			}
			for _, p := range []string{"%00", "docs/a%00.png", "a\x00", "a\x01", "a\x1f", "a\x7f", "a%0A", "a%7F", "a\\b", "a%5cb"} {
				if r := Resolve(root, p, win); r.Status != 400 {
					t.Errorf("%q: %+v", p, r)
				}
			}
			for _, p := range []string{"..", "../x", "docs/../x", "%2e%2e/x", "docs/%2E%2e/x", ".git/config", "docs/.GIT/config", "%2egit/config"} {
				if r := Resolve(root, p, win); r.Status != 403 {
					t.Errorf("%q: %+v", p, r)
				}
			}
			if win {
				for _, p := range []string{"x:y", "C:/file", "file.", "file ", "docs. /x", "CON", "con.txt", "PRN", "aux.dat", "NUL", "conin$", "conout$.txt", "COM0", "COM9.txt", "LPT0", "lpt9.log"} {
					if r := Resolve(root, p, win); r.Status != 400 {
						t.Errorf("%q: %+v", p, r)
					}
				}
			}
		})
	}
}
func makeLink(t *testing.T, target, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if out, e := exec.Command("cmd", "/c", "mklink", "/J", path, target).CombinedOutput(); e != nil {
			t.Skipf("ジャンクションを作れない: %v %s", e, out)
		}
	} else if e := os.Symlink(target, path); e != nil {
		t.Skipf("リンクを作れない: %v", e)
	}
	if _, e := os.Stat(path); e != nil {
		t.Fatalf("作ったリンクを開けない: %v", e)
	}
	if _, e := filepath.EvalSymlinks(path); e != nil {
		t.Fatalf("作ったリンクの実パスを取得できない: %v", e)
	}
}
func TestLinks(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	_ = os.MkdirAll(filepath.Join(root, ".git"), 0755)
	_ = os.WriteFile(filepath.Join(base, "outside.txt"), []byte("外"), 0644)
	_ = os.WriteFile(filepath.Join(root, ".git", "config"), []byte("秘密ではない見本"), 0644)
	_ = os.WriteFile(filepath.Join(root, "safe.txt"), []byte("内"), 0644)
	makeLink(t, base, filepath.Join(root, "outside"))
	makeLink(t, filepath.Join(root, ".git"), filepath.Join(root, "hidden"))
	makeLink(t, root, filepath.Join(root, "inside"))
	for _, p := range []string{filepath.Join(base, "outside.txt"), filepath.Join(root, ".git", "config"), filepath.Join(root, "safe.txt")} {
		if _, e := os.Stat(p); e != nil {
			t.Fatal(p, e)
		}
	}
	var rootErr error
	root, rootErr = filepath.EvalSymlinks(root)
	if rootErr != nil {
		t.Fatal(rootErr)
	}
	for name, want := range map[string]int{"outside/outside.txt": 403, "hidden/config": 403, "inside/safe.txt": 200} {
		if r := Resolve(root, name); r.Status != want {
			t.Errorf("%s: %+v", name, r)
		}
	}
}
func TestUnicodeBoundary(t *testing.T) {
	if LowerASCII("AİZ") != "aİz" {
		t.Fatal("ASCII以外まで変えた")
	}
	base := t.TempDir()
	root := filepath.Join(base, "repoİ")
	outside := filepath.Join(base, "repoi\u0307")
	_ = os.Mkdir(root, 0755)
	_ = os.Mkdir(outside, 0755)
	_ = os.WriteFile(filepath.Join(outside, "file.txt"), []byte("外"), 0644)
	makeLink(t, outside, filepath.Join(root, "escape"))
	root, _ = filepath.EvalSymlinks(root)
	if r := Resolve(root, "escape/file.txt", true); r.Status != 403 {
		t.Fatalf("隣を内側と見誤った: %+v", r)
	}
}
func TestPOSIXNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("コロンや末尾の点を含む実ファイルはPOSIXでだけ確認する")
	}
	root := t.TempDir()
	for _, p := range []string{"x:y", "file.", "file ", "CON"} {
		if e := os.WriteFile(filepath.Join(root, p), []byte("内"), 0644); e != nil {
			t.Fatal(e)
		}
		if r := Resolve(root, p, false); r.Status != 200 {
			t.Fatal(r)
		}
	}
}

func TestUTF8Replacement(t *testing.T) {
	for input, want := range map[string]string{string([]byte{255, 255}): "\ufffd\ufffd", string([]byte{0xe2, 0x82}): "\ufffd", string([]byte{0xe2, 0x82, 255}): "\ufffd\ufffd", string([]byte{0xe0, 0x80, 0x80}): "\ufffd\ufffd\ufffd", "日本語": "日本語"} {
		if got := DecodeUTF8(input); got != want {
			t.Fatalf("UTF-8の復号が異なる: %q", got)
		}
	}
}
