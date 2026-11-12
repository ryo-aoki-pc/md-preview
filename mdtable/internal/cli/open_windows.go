package cli

import (
	"fmt"
	"syscall"
	"unsafe"
)

func openWorkbook(path string) error {
	file, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return e
	}
	verb, _ := syscall.UTF16PtrFromString("open")
	result, _, callError := syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW").Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), 0, 0, 1)
	if result <= 32 {
		return fmt.Errorf("ブックを開けません: %s (%v)", path, callError)
	}
	return nil
}
