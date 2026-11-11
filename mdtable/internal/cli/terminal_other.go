//go:build !windows && !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly

package cli

import (
	"os"
)

// cli._use_color に対応する端末の判定。
func enableTerminal(f *os.File) bool {
	st, e := f.Stat()
	return e == nil && st.Mode()&os.ModeCharDevice != 0
}
