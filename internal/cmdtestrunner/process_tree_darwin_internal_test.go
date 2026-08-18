//go:build darwin

package cmdtestrunner

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaitForDarwinLauncherStopRecognizesAnExitedLauncher(t *testing.T) {
	launcher := exec.Command("/usr/bin/true") //nolint:noctx // The command exits immediately and is reaped by the test.
	require.NoError(t, launcher.Start())

	state, err := waitForDarwinLauncherStop(launcher.Process.Pid)

	assert.Equal(t, darwinLauncherWaitStateExited, state)
	assert.ErrorIs(t, err, errDarwinLauncherExited)
	statusReader, statusWriter, pipeErr := os.Pipe()
	require.NoError(t, pipeErr)
	require.NoError(t, statusWriter.Close())

	finishErr := finishExitedDarwinLauncher(launcher, statusReader, err)

	assert.ErrorIs(t, finishErr, errDarwinLauncherExited)
	assert.Equal(t, -1, launcher.Process.Pid)
	assert.Error(t, launcher.Process.Kill())
}
