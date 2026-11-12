package table

import (
	"errors"
	"os"
	"syscall"
)

// IsPermissionError は Python の PermissionError に含まれる共有・領域ロックも扱う。
func IsPermissionError(e error) bool {
	return errors.Is(e, os.ErrPermission) || errors.Is(e, syscall.Errno(32)) || errors.Is(e, syscall.Errno(33))
}
