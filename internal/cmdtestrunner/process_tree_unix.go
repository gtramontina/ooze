//go:build !windows && !darwin && !linux

package cmdtestrunner

import (
	"fmt"
	"os/exec"
)

// runProcessTree retains the pre-supervision behavior on operating systems
// where Ooze does not yet provide an identity-stable process-tree barrier.
func runProcessTree(command *exec.Cmd) (error, error) {
	err := command.Start()
	if err != nil {
		return nil, fmt.Errorf("start test command: %w", err)
	}

	commandErr, waitErr := classifyCommandWait(command.Wait())

	return commandErr, waitErr
}
