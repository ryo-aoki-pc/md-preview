// Package paths は、ファイルを開かずルート内の相対パスを組み立てる。
package paths

import (
	"path"
	"regexp"
	"strings"
)

var drive = regexp.MustCompile(`^[a-zA-Z]:`)
var odd = regexp.MustCompile(`^[. ]+$`)

type Resolved struct {
	Path string
	Safe bool
}

func IncludeBase(file string) string {
	if file == "" {
		return ""
	}
	trimmed := strings.TrimRight(file, "/")
	base := trimmed[strings.LastIndexByte(trimmed, '/')+1:]
	if i := strings.LastIndexByte(base, '.'); i > 0 && base != ".." {
		// POSIX の dirname は、末尾の / を読み飛ばすが途中の . や重複した / を正規化しない。
		i := strings.LastIndexByte(trimmed, '/')
		if i < 0 {
			return "."
		}
		if i == 0 {
			return "/"
		}
		if i == 1 && strings.HasPrefix(trimmed, "//") {
			return "//"
		}
		return trimmed[:i]
	}
	return file
}
func Resolve(target, base string) Resolved {
	if strings.ContainsAny(target, "\\\x00") || strings.ContainsAny(base, "\\\x00") || drive.MatchString(target) || drive.MatchString(base) {
		return Resolved{target, false}
	}
	s := target
	if !strings.HasPrefix(s, "/") {
		if target == "" {
			s = base
		} else if base != "" {
			s = base + "/" + target
		}
	}
	trailing := strings.HasSuffix(s, "/")
	s = path.Clean(s)
	if trailing && s != "/" {
		s += "/"
	}
	s = strings.TrimLeft(s, "/")
	safe := s != ".." && !strings.HasPrefix(s, "../") && !drive.MatchString(s)
	for _, part := range strings.Split(s, "/") {
		if part != "." && odd.MatchString(part) {
			safe = false
		}
	}
	return Resolved{s, safe}
}
