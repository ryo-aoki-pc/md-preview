package pycompat

import "testing"

func TestFileStemPython(t *testing.T) {
	for _, c := range [][2]string{{"file.md", "file"}, {".md", ".md"}, {"...name", "...name"}, {".name.md", ".name"}, {"name.", "name"}, {"...", "..."}} {
		if got := FileStem(c[0]); got != c[1] {
			t.Errorf("%q => %q, want %q", c[0], got, c[1])
		}
	}
}
