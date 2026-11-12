package pycompat

import (
	"strings"
	"unicode/utf8"
)

// DecodeUTF8 は Python の decode("utf-8", "replace") と同じ単位で置き換える。
func DecodeUTF8(data []byte) string {
	var out strings.Builder
	for len(data) > 0 {
		ch, n := utf8.DecodeRune(data)
		if ch != utf8.RuneError || n > 1 {
			out.Write(data[:n])
			data = data[n:]
			continue
		}
		length := 1
		first := data[0]
		expected := 0
		switch {
		case first >= 0xc2 && first <= 0xdf:
			expected = 2
		case first >= 0xe0 && first <= 0xef:
			expected = 3
		case first >= 0xf0 && first <= 0xf4:
			expected = 4
		}
		for length < expected && length < len(data) {
			b := data[length]
			if b < 0x80 || b > 0xbf {
				break
			}
			if length == 1 && (first == 0xe0 && b < 0xa0 || first == 0xed && b > 0x9f || first == 0xf0 && b < 0x90 || first == 0xf4 && b > 0x8f) {
				break
			}
			length++
		}
		out.WriteRune(utf8.RuneError)
		data = data[length:]
	}
	return out.String()
}
