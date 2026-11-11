package workbook

import (
	"syscall"
	"unsafe"
)

func fontInstalled(family string) bool {
	advapi := syscall.NewLazyDLL("advapi32.dll")
	open := advapi.NewProc("RegOpenKeyExW")
	enum := advapi.NewProc("RegEnumValueW")
	closeKey := advapi.NewProc("RegCloseKey")
	path, _ := syscall.UTF16PtrFromString(`SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`)
	for _, root := range []uintptr{0x80000002, 0x80000001} {
		var key uintptr
		result, _, _ := open.Call(root, uintptr(unsafe.Pointer(path)), 0, 0x20019, uintptr(unsafe.Pointer(&key)))
		if result != 0 {
			continue
		}
		found := false
		for index := uintptr(0); ; index++ {
			name := make([]uint16, 16384)
			length := uint32(len(name))
			result, _, _ = enum.Call(key, index, uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&length)), 0, 0, 0, 0)
			if result != 0 {
				break
			}
			if FontEntryMatches(syscall.UTF16ToString(name[:length]), family) {
				found = true
				break
			}
		}
		closeKey.Call(key)
		if found {
			return true
		}
	}
	return false
}
