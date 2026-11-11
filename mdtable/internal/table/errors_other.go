//go:build !windows

package table

import (
	"errors"
	"os"
)

func IsPermissionError(e error) bool { return errors.Is(e, os.ErrPermission) }
