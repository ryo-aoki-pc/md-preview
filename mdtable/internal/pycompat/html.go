package pycompat

import (
	"regexp"
	"strings"
)

var charref = regexp.MustCompile(`&(#[0-9]+;?|#[xX][0-9a-fA-F]+;?|[^\t\n\f <&#;]{1,32};?)`)

// HTMLUnescape は属性値の制限を加えず Python の html.unescape と同じ置換を行う。
func HTMLUnescape(s string) string {
	return charref.ReplaceAllStringFunc(s, func(ref string) string {
		name := ref[1:]
		if name[0] != '#' {
			if value, ok := htmlEntities[name]; ok {
				return value
			}
			// 文字参照名は ASCII なのでバイト位置で接頭辞を照合できる。
			for i := len(name) - 1; i > 1; i-- {
				if value, ok := htmlEntities[name[:i]]; ok {
					return value + name[i:]
				}
			}
			return ref
		}
		digits := strings.TrimSuffix(name[1:], ";")
		base := 16
		if digits[0] == 'x' || digits[0] == 'X' {
			digits = digits[1:]
		} else {
			base = 10
			if pythonIntMaxStrDigits > 0 && len(digits) > pythonIntMaxStrDigits {
				panic("Python の整数文字列の桁数制限を超えました")
			}
		}
		n := 0
		for _, c := range digits {
			d := int(c - '0')
			if c >= 'a' && c <= 'f' {
				d = int(c-'a') + 10
			}
			if c >= 'A' && c <= 'F' {
				d = int(c-'A') + 10
			}
			if n > 0x10ffff {
				continue
			}
			n = n*base + d
		}
		if value, ok := invalidCharrefs[n]; ok {
			return value
		}
		if n > 0x10ffff || n >= 0xd800 && n <= 0xdfff {
			return "\ufffd"
		}
		if n >= 1 && n <= 8 || n == 0xb || n >= 0xe && n <= 0x1f || n >= 0x7f && n <= 0x9f || n >= 0xfdd0 && n <= 0xfdef || n&0xffff >= 0xfffe {
			return ""
		}
		return string(rune(n))
	})
}
