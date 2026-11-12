package changes

import (
	"strings"
	"testing"
)

// changes._short_pair の Unicode の共通の窓と改行の置き換えを確かめる。
func TestShortPair(t *testing.T) {
	prefix := strings.Repeat("あ", 40)
	a, b := shortPair(prefix+"前\r\n末", prefix+"後\n末")
	if a != "…"+strings.Repeat("あ", 10)+"前⏎末" || b != "…"+strings.Repeat("あ", 10)+"後⏎末" {
		t.Fatalf("最初の違いが表示されない: %q %q", a, b)
	}
	a, b = shortPair("", strings.Repeat("字", 31))
	if a != "" || b != strings.Repeat("字", 30)+"…" {
		t.Fatalf("空の文字との比較が違う: %q %q", a, b)
	}
}
