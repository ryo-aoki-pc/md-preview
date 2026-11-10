package assets

import (
	"errors"
	"syscall"
)

func processAlive(pid int) (bool, error) {
	handle, err := syscall.OpenProcess(0x1000, false, uint32(pid))
	if errors.Is(err, syscall.Errno(87)) {
		return false, nil
	}
	if errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	defer syscall.CloseHandle(handle)
	var code uint32
	if err := syscall.GetExitCodeProcess(handle, &code); err != nil {
		return false, err
	}
	return code == 259, nil
}
