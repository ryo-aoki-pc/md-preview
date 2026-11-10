package resolve

import (
	"strings"
	"unicode/utf8"
)

// DecodeUTF8 は Buffer.toString と同じく、不正な並びの最長の有効な接頭部分ごとに置換する。
// strings.ToValidUTF8 は連続する不正バイトをまとめるため、URL とファイル名の対応が変わる。
func DecodeUTF8(text string) string {
	if utf8.ValidString(text) {
		return text
	}
	var out strings.Builder
	for i := 0; i < len(text); {
		lead := text[i]
		if lead < 0x80 {
			out.WriteByte(lead)
			i++
			continue
		}
		size := 0
		switch {
		case lead >= 0xc2 && lead <= 0xdf:
			size = 2
		case lead >= 0xe0 && lead <= 0xef:
			size = 3
		case lead >= 0xf0 && lead <= 0xf4:
			size = 4
		}
		if size == 0 {
			out.WriteRune(utf8.RuneError)
			i++
			continue
		}
		j := 1
		for j < size && i+j < len(text) {
			c := text[i+j]
			if c < 0x80 || c > 0xbf || j == 1 && (lead == 0xe0 && c < 0xa0 || lead == 0xed && c > 0x9f || lead == 0xf0 && c < 0x90 || lead == 0xf4 && c > 0x8f) {
				break
			}
			j++
		}
		if j == size {
			out.WriteString(text[i : i+size])
		} else {
			out.WriteRune(utf8.RuneError)
		}
		i += j
	}
	return out.String()
}
