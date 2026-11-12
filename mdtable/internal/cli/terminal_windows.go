//go:build windows

package cli

import (
	"os"
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var getConsoleMode = kernel32.NewProc("GetConsoleMode")
var setConsoleMode = kernel32.NewProc("SetConsoleMode")

// cli._enable_virtual_terminal に対応する Windows の端末判定と設定。
func enableTerminal(f *os.File) bool {
	var mode uint32
	ok, _, _ := getConsoleMode.Call(f.Fd(), uintptr(unsafe.Pointer(&mode)))
	if ok == 0 {
		return false
	}
	ok, _, _ = setConsoleMode.Call(f.Fd(), uintptr(mode|0x0004))
	return ok != 0
}
