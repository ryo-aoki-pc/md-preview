//go:build !windows

package cli

import (
	"errors"
	"os/exec"
	"syscall"
)

func hideWindow(_ *exec.Cmd)        {}
func isAddressInUse(err error) bool { return errors.Is(err, syscall.EADDRINUSE) }
