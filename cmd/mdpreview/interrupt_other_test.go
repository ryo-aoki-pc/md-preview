//go:build !windows

package main

import (
	"os"
	"os/exec"
)

func configureInterrupt(_ *exec.Cmd)           {}
func interruptProcess(command *exec.Cmd) error { return command.Process.Signal(os.Interrupt) }
