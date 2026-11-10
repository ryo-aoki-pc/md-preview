//go:build !windows

package assets

import (
	"os"
	"testing"
)

func createTestLink(t *testing.T, target, name string, _ bool) {
	t.Helper()
	if err := os.Symlink(target, name); err != nil {
		t.Fatal(err)
	}
}
