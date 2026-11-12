package pycompat

import (
	"fmt"
	"strings"
)

// StringRepr は例外の説明で使う Python の文字列の repr を返す。
func StringRepr(s string) string {
	quote := '\''
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var out strings.Builder
	out.WriteRune(quote)
	for _, ch := range s {
		switch ch {
		case '\\':
			out.WriteString(`\\`)
		case '\t':
			out.WriteString(`\t`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		default:
			category := Category(ch)
			switch {
			case ch == quote:
				out.WriteByte('\\')
				out.WriteRune(ch)
			case ch == ' ' || category[0] != 'C' && category[0] != 'Z':
				out.WriteRune(ch)
			case ch < 256:
				fmt.Fprintf(&out, `\x%02x`, ch)
			case ch < 65536:
				fmt.Fprintf(&out, `\u%04x`, ch)
			default:
				fmt.Fprintf(&out, `\U%08x`, ch)
			}
		}
	}
	out.WriteRune(quote)
	return out.String()
}
