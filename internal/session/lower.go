package session

import (
	"strings"
	"unicode"
)

// JavaScript の既定の小文字化には、İ の展開と、文脈による最後の Σ が含まれる。
// これは文書の索引だけに使う。閉じ込めの実パス比較は resolve.LowerASCII に限る。
func lowerJS(text string) string {
	runes := []rune(text)
	var out strings.Builder
	cased := func(c rune) bool {
		return unicode.IsUpper(c) || unicode.IsLower(c) || unicode.IsTitle(c) || unicode.Is(unicode.Other_Lowercase, c) || unicode.Is(unicode.Other_Uppercase, c)
	}
	ignorable := func(c rune) bool {
		return unicode.In(c, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk) || strings.ContainsRune("'\u002e\u003a\u00b7\u0387\u055f\u05f4\u2018\u2019\u2024\u2027\ufe13\ufe52\ufe55\uff07\uff0e\uff1a", c)
	}
	for i, c := range runes {
		if c == 'İ' {
			out.WriteString("i\u0307")
			continue
		}
		if c == 'Σ' {
			before, after := false, false
			for j := i - 1; j >= 0; j-- {
				if cased(runes[j]) {
					before = true
					break
				}
				if !ignorable(runes[j]) {
					break
				}
			}
			for j := i + 1; j < len(runes); j++ {
				if cased(runes[j]) {
					after = true
					break
				}
				if !ignorable(runes[j]) {
					break
				}
			}
			if before && !after {
				out.WriteRune('ς')
			} else {
				out.WriteRune('σ')
			}
			continue
		}
		out.WriteRune(unicode.ToLower(c))
	}
	return out.String()
}
