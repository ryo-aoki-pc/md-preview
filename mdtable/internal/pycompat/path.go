package pycompat

import (
	"path/filepath"
	"strings"
)

// FileStem は先頭のドットを拡張子に含めず、os.path.splitext と同じ幹を返す。
func FileStem(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if strings.Trim(strings.TrimSuffix(base, ext), ".") == "" {
		return base
	}
	return strings.TrimSuffix(base, ext)
}
