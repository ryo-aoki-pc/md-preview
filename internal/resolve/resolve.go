// Package resolve は URL のパスをルート内の実パスに閉じ込める。
package resolve

import (
	"path/filepath"
	"runtime"
	"strings"
)

type Result struct {
	Status int
	Path   string
}

// IsAbsolute は Node.js と同じく、Windows ではドライブを省いたルート始まりも受け付ける。
func IsAbsolute(path string) bool {
	if runtime.GOOS == "windows" && len(path) > 0 && (path[0] == '/' || path[0] == '\\') {
		return true
	}
	return filepath.IsAbs(path)
}

// LowerASCII は Windows の実パス比較に限り、ASCII の大文字だけをそろえる。
func LowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

func unescape(s string) string {
	b := make([]byte, 0, len(s))
	hex := func(c byte) int {
		switch {
		case c >= '0' && c <= '9':
			return int(c - '0')
		case c >= 'a' && c <= 'f':
			return int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			return int(c-'A') + 10
		}
		return -1
	}
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			a, bv := hex(s[i+1]), hex(s[i+2])
			if a >= 0 && bv >= 0 {
				b = append(b, byte(a*16+bv))
				i += 2
				continue
			}
		}
		b = append(b, s[i])
	}
	return DecodeUTF8(string(b))
}

// Unescape は URLSearchParams と共通の、不完全な % を保持する復号。
func Unescape(s string) string { return unescape(s) }

// Resolve は root が実パスであることを前提とする。省略した分岐は現在の OS に合わせる。
func Resolve(root, raw string, windows ...bool) Result {
	win := runtime.GOOS == "windows"
	if len(windows) > 0 {
		win = windows[0]
	}
	if strings.Contains(raw, "%00") {
		return Result{Status: 400}
	}
	p := unescape(raw)
	for _, c := range []byte(p) {
		if c < 32 || c == 127 || c == '\\' || (win && c == ':') {
			return Result{Status: 400}
		}
	}
	segs := []string{}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." {
			continue
		}
		if seg == ".." {
			return Result{Status: 403}
		}
		if win {
			stem := strings.ToLower(strings.SplitN(seg, ".", 2)[0])
			device := stem == "con" || stem == "prn" || stem == "aux" || stem == "nul" || stem == "conin$" || stem == "conout$" || (len(stem) == 4 && (strings.HasPrefix(stem, "com") || strings.HasPrefix(stem, "lpt")) && stem[3] >= '0' && stem[3] <= '9')
			if strings.HasSuffix(seg, ".") || strings.HasSuffix(seg, " ") || device {
				return Result{Status: 400}
			}
		}
		if strings.EqualFold(seg, ".git") {
			return Result{Status: 403}
		}
		segs = append(segs, seg)
	}
	if len(segs) == 0 {
		return Result{Status: 404}
	}
	base := strings.TrimRight(strings.ReplaceAll(root, "\\", "/"), "/")
	real, err := RealPath(filepath.FromSlash(base + "/" + strings.Join(segs, "/")))
	if err != nil {
		return Result{Status: 404}
	}
	real = filepath.ToSlash(real)
	a, b := real, base+"/"
	if win {
		a, b = LowerASCII(a), LowerASCII(b)
	}
	if !strings.HasPrefix(a, b) {
		return Result{Status: 403}
	}
	for _, seg := range strings.Split(real[len(b):], "/") {
		if strings.EqualFold(seg, ".git") {
			return Result{Status: 403}
		}
	}
	return Result{Status: 200, Path: real}
}
