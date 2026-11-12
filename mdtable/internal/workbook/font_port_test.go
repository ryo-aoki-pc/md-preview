package workbook

import (
	"runtime"
	"testing"
)

// Python: test_workbook.FontTest.test_font_name
func TestPythonFontName(t *testing.T) {
	t.Setenv("MDTABLE_FONT", "Consolas")
	if got := FontName(); got != "Consolas" {
		t.Fatal(got)
	}
	t.Setenv("MDTABLE_FONT", "")
	fontCache.Lock()
	previous := fontCache.values
	fontCache.values = map[string]bool{}
	fontCache.Unlock()
	t.Cleanup(func() { fontCache.Lock(); fontCache.values = previous; fontCache.Unlock() })
	for _, test := range []struct {
		installed bool
		want      string
	}{{true, "HackGen Console NF"}, {false, "BIZ UDゴシック"}} {
		fontCache.Lock()
		fontCache.values["HackGen Console NF"] = test.installed
		fontCache.Unlock()
		if got := FontName(); got != test.want {
			t.Fatalf("書体の選択: %q", got)
		}
	}
}

// Python: test_workbook.FontTest.test_registered_fonts
func TestPythonRegisteredFonts(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows のフォントの登録を見る")
	}
	if fontInstalled("mdtable に無い書体 0123") {
		t.Fatal("無い書体が登録されている")
	}
	_ = fontInstalled("HackGen Console NF")
}
