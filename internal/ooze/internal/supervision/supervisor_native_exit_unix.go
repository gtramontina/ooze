//go:build darwin || linux

package supervision

import (
	"os/exec"
	"syscall"
)

func nativeExitStatusFromError(exitErr *exec.ExitError) ExitStatus {
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		return ExitStatus{Code: exitErr.ExitCode()}
	}
	if status.Signaled() {
		return ExitStatus{Signal: int(status.Signal())}
	}

	return ExitStatus{Code: status.ExitStatus()}
}
