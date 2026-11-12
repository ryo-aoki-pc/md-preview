//go:build linux

package cli

import (
	"os"
	"syscall"
	"unsafe"
)

// cli._use_color の isatty に対応する Linux の端末判定。
func enableTerminal(f *os.File) bool {
	var termios syscall.Termios
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&termios)))
	return e == 0
}
