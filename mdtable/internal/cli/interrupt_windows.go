package cli

import (
	"errors"
	"syscall"
)

// Ctrl+C によるコンソール読み取りの終了が、シグナル通知より先に届く場合も扱う。
func inputInterrupted(e error) bool { return errors.Is(e, syscall.ERROR_OPERATION_ABORTED) }
