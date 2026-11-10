//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

func configureInterrupt(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP, HideWindow: true}
}
func interruptProcess(command *exec.Cmd) error {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GenerateConsoleCtrlEvent")
	result, _, err := proc.Call(syscall.CTRL_BREAK_EVENT, uintptr(command.Process.Pid))
	if result == 0 {
		return err
	}
	return nil
}
