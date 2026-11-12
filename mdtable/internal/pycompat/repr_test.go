package pycompat

import "testing"

func TestStringReprPython(t *testing.T) {
	for _, c := range [][2]string{{"bad", `'bad'`}, {"a'b", `"a'b"`}, {"a'b\"", `'a\'b"'`}, {"\x00\b\t\r\n", `'\x00\x08\t\r\n'`}, {"\u00a0\u200b", `'\xa0\u200b'`}, {"文字", `'文字'`}} {
		if got := StringRepr(c[0]); got != c[1] {
			t.Errorf("%q => %q, want %q", c[0], got, c[1])
		}
	}
}
