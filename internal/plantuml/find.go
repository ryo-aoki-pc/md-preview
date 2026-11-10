package plantuml

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Find は、PATH の絶対パスの項目だけから plantuml を探す。カレントディレクトリーは見ない。
func Find(env func(string) string, goos string) (command string, ok bool) {
	if env == nil {
		env = os.Getenv
	}
	if goos == "" {
		goos = runtime.GOOS
	}
	windows := goos == "windows"
	separator := ":"
	extensions := []string{""}
	get := func(name string) string {
		value := env(name)
		if windows && value == "" {
			for _, variant := range []string{strings.ToLower(name), "Path", "PathExt"} {
				if strings.EqualFold(variant, name) {
					if v := env(variant); v != "" {
						return v
					}
				}
			}
		}
		return value
	}
	if windows {
		separator = ";"
		extensions = nil
		pathext := get("PATHEXT")
		if pathext == "" {
			pathext = ".EXE;.CMD;.BAT"
		}
		for _, ext := range strings.Split(strings.ToLower(pathext), ";") {
			if ext == ".exe" || ext == ".cmd" || ext == ".bat" {
				extensions = append(extensions, ext)
			}
		}
	}
	for _, folder := range strings.Split(get("PATH"), separator) {
		if windows {
			if unsafeCommand(folder) || !windowsAbsolute(folder) {
				continue
			}
		} else if !strings.HasPrefix(folder, "/") {
			continue
		}
		for _, ext := range extensions {
			candidate := filepath.Join(folder, "plantuml"+ext)
			stat, err := os.Stat(candidate)
			if err != nil || !stat.Mode().IsRegular() {
				continue
			}
			if !windows && !executable(candidate, stat) {
				continue
			}
			return candidate, true
		}
	}
	return "", false
}

func windowsAbsolute(path string) bool {
	path = strings.ReplaceAll(path, "/", "\\")
	if len(path) >= 3 && ((path[0] >= 'a' && path[0] <= 'z') || (path[0] >= 'A' && path[0] <= 'Z')) && path[1] == ':' && path[2] == '\\' {
		return true
	}
	if strings.HasPrefix(path, "\\\\") {
		parts := strings.Split(path[2:], "\\")
		return len(parts) >= 2 && parts[0] != "" && parts[1] != ""
	}
	return false
}
