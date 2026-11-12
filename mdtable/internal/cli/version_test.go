package cli

import (
	"context"
	"testing"

	"github.com/ryo-aoki-pc/md-table-excel/internal/version"
)

// cli.main の --version が共通の版を表示することを確かめる。
func TestSharedVersionDisplay(t *testing.T) {
	previous := version.Version
	version.Version = "test-version"
	defer func() { version.Version = previous }()
	code, out, err := captureOutput(t, func() int { return mainContext(context.Background(), []string{"--version"}, nil) })
	if code != 0 || out != "mdtable test-version\n" || err != "" {
		t.Fatalf("終了 %d、表示 %q、標準エラー %q", code, out, err)
	}
}
