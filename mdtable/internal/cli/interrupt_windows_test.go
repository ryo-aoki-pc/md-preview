package cli

import (
	"io"
	"os"
	"syscall"
	"testing"
)

func TestAbortedConsoleRead(t *testing.T) {
	e := &os.PathError{Op: "read", Path: "stdin", Err: syscall.ERROR_OPERATION_ABORTED}
	if !inputInterrupted(e) {
		t.Fatal("コンソールの読み取り中断を検出できない")
	}
	if inputInterrupted(io.EOF) {
		t.Fatal("EOF を Ctrl+C として扱った")
	}
}
