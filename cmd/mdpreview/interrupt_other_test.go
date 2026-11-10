//go:build !windows

package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func configureInterrupt(_ *exec.Cmd)           {}
func interruptProcess(command *exec.Cmd) error { return command.Process.Signal(os.Interrupt) }

func childAlive(pid int) bool {
	if data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
		if i := strings.LastIndexByte(string(data), ')'); i >= 0 && strings.HasPrefix(string(data[i+1:]), " Z") {
			return false
		}
	}
	return syscall.Kill(pid, 0) == nil
}
