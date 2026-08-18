//go:build linux

package cmdtestrunner

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinuxGuardianChildProcessIDsFallsBackToProcessStatus(t *testing.T) {
	processRoot := t.TempDir()
	taskRoot := filepath.Join(processRoot, "self", "task")
	require.NoError(t, os.MkdirAll(filepath.Join(taskRoot, "1"), 0o700))
	writeLinuxProcessStatus(t, processRoot, "101", "guardian-child", 42)
	writeLinuxProcessStatus(t, processRoot, "202", "unrelated", 7)
	require.NoError(t, os.MkdirAll(filepath.Join(processRoot, "303"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(processRoot, "404"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(processRoot, "404", "status"), []byte("Name:\tmissing-parent\n"), 0o600))

	processIDs, err := linuxGuardianChildProcessIDsAt(taskRoot, processRoot, 42)

	require.NoError(t, err)
	assert.ElementsMatch(t, []int{101}, processIDs)
}

func TestLinuxProcessStatusIsUninspectable(t *testing.T) {
	for _, statusErr := range []error{
		os.ErrNotExist,
		os.ErrPermission,
		syscall.ESRCH,
		fmt.Errorf("inspect status: %w", syscall.EPERM),
	} {
		assert.True(t, linuxProcessStatusIsUninspectable(statusErr))
	}

	assert.False(t, linuxProcessStatusIsUninspectable(syscall.EIO))
}

func TestLinuxGuardianChildProcessIDsPrefersTheKernelChildrenInterface(t *testing.T) {
	processRoot := t.TempDir()
	taskRoot := filepath.Join(processRoot, "self", "task")
	require.NoError(t, os.MkdirAll(filepath.Join(taskRoot, "1"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(taskRoot, "1", "children"), []byte("303 "), 0o600))
	writeLinuxProcessStatus(t, processRoot, "101", "status-only-child", 42)

	processIDs, err := linuxGuardianChildProcessIDsAt(taskRoot, processRoot, 42)

	require.NoError(t, err)
	assert.ElementsMatch(t, []int{303}, processIDs)
}

func writeLinuxProcessStatus(t *testing.T, processRoot, processID, name string, parentProcessID int) {
	t.Helper()

	processDirectory := filepath.Join(processRoot, processID)
	require.NoError(t, os.MkdirAll(processDirectory, 0o700))
	contents := []byte("Name:\t" + name + "\nPPid:\t" + strconv.Itoa(parentProcessID) + "\n")
	require.NoError(t, os.WriteFile(filepath.Join(processDirectory, "status"), contents, 0o600))
}
