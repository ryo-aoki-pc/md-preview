//go:build windows

package cli

import (
	"errors"
	"os/exec"
	"syscall"
)

func hideWindow(cmd *exec.Cmd)      { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
func isAddressInUse(err error) bool { return errors.Is(err, syscall.Errno(10048)) }
