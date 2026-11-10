package paths

import (
	"reflect"
	"testing"
)

func TestResolve(t *testing.T) {
	for _, c := range []struct{ target, base, want string }{{"part.md", "docs", "docs/part.md"}, {"./sub/../part.md", "docs", "docs/part.md"}, {"../top.md", "docs", "top.md"}, {"/top.md", "docs", "top.md"}, {"part.md", "", "part.md"}, {"/../secret.md", "docs", "secret.md"}, {"%2e%2e/secret.md", "docs", "docs/%2e%2e/secret.md"}, {"a%2fb.md", "", "a%2fb.md"}} {
		if got := Resolve(c.target, c.base); !reflect.DeepEqual(got, Resolved{c.want, true}) {
			t.Errorf("%q + %q: %#v", c.base, c.target, got)
		}
	}
	for _, s := range []string{"../../secret.md", "../..", "a/../../../secret.md", "C:/secret.md", "c:secret.md", "..\\secret.md", "a\\b.md", "\\\\server\\share\\x.md", "a\x00b.md", ".. /.. /secret.md", ".. /secret.md", "a/.. /b.md", ".../x.md", ". ./x.md", "..  ./x.md", " /x.md"} {
		if Resolve(s, "docs").Safe {
			t.Errorf("危ないパスを許可: %q", s)
		}
	}
	for _, base := range []string{"C:/docs", "a\\b"} {
		if Resolve("x.md", base).Safe {
			t.Errorf("危ない基準: %q", base)
		}
	}
}
func TestIncludeBase(t *testing.T) {
	for _, c := range [][2]string{{"docs/main.md", "docs"}, {"main.md", "."}, {"docs/main", "docs/main"}, {"", ""}, {".hidden", ".hidden"}, {"docs/main.", "docs"}, {"..", ".."}, {"docs/topic.md/", "docs"}, {"docs/./topic.md", "docs/."}} {
		if got := IncludeBase(c[0]); got != c[1] {
			t.Errorf("%q: %q", c[0], got)
		}
	}
}
