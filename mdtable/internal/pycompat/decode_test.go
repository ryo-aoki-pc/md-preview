package pycompat

import "testing"

func TestDecodeUTF8Python(t *testing.T) {
	for _, c := range [][2]string{{"\xff\xff", "��"}, {"\xe1\x80", "�"}, {"\xe1\x80A", "�A"}, {"\xed\xa0\x80", "���"}, {"\xf0\x90\x80", "�"}, {"\xe0\xa0\x80", "ࠀ"}, {"文字", "文字"}} {
		if got := DecodeUTF8([]byte(c[0])); got != c[1] {
			t.Errorf("%x => %q, want %q", c[0], got, c[1])
		}
	}
}
