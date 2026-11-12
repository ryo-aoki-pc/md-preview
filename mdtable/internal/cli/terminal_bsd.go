//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package cli

import (
	"os"
	"syscall"
	"unsafe"
)

// cli._use_color の isatty に対応する BSD 系 OS の端末判定。
func enableTerminal(f *os.File) bool {
	var buffer [256]byte
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGETA, uintptr(unsafe.Pointer(&buffer[0])))
	return e == 0
}
