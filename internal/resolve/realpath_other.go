//go:build !windows

package resolve

import "path/filepath"

func RealPath(path string) (string, error) { return filepath.EvalSymlinks(path) }
