//go:build !windows

package fileopen

import "os"

func open(path string) (*os.File, error) { return os.Open(path) }
