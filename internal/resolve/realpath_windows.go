//go:build windows

package resolve

import (
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var finalPathName = syscall.NewLazyDLL("kernel32.dll").NewProc("GetFinalPathNameByHandleW")

// RealPath はジャンクションも含め、開いたハンドルの最終パスを返す。
// データの読み取り権限は要求せず、フォルダーにも使える。
func RealPath(path string) (string, error) {
	abs, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	path = abs
	if !strings.HasPrefix(path, `\\?\`) {
		if strings.HasPrefix(path, `\\`) {
			path = `\\?\UNC\` + path[2:]
		} else {
			path = `\\?\` + path
		}
	}
	name, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return "", e
	}
	h, e := syscall.CreateFile(name, 0, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if e != nil {
		return "", e
	}
	defer syscall.CloseHandle(h)
	buffer := make([]uint16, 512)
	for {
		n, _, err := finalPathName.Call(uintptr(h), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), 0)
		if n == 0 {
			return "", err
		}
		if n >= uintptr(len(buffer)) {
			buffer = make([]uint16, n+1)
			continue
		}
		value := syscall.UTF16ToString(buffer[:n])
		if strings.HasPrefix(value, `\\?\UNC\`) {
			value = `\\` + value[len(`\\?\UNC\`):]
		} else if strings.HasPrefix(value, `\\?\`) {
			value = value[len(`\\?\`):]
		} else {
			return "", fmt.Errorf("実パスの形式が正しくありません")
		}
		return filepath.Clean(value), nil
	}
}
