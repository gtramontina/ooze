//go:build windows

package fsrepository_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/fsrepository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestFSTemporaryRepositoryRetriesWindowsDeleteSharing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "held.txt")
	{
		err := os.WriteFile(path, []byte("held"), 0o600)
		require.NoError(t, err)
	}
	pointer, err := windows.UTF16PtrFromString(path)
	require.NoError(t, err)
	handle, err := windows.CreateFile(
		pointer, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0,
	)
	require.NoError(t, err)
	closed := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = windows.CloseHandle(handle)
		close(closed)
	}()

	{
		err := os.Remove(path)
		require.ErrorIs(t, err, windows.ERROR_SHARING_VIOLATION, "held-file removal error = %v, want sharing violation", err)
	}
	fsrepository.NewTemporary(dir).Remove()
	<-closed
	{
		_, err := os.Stat(dir)
		assert.ErrorIs(t, err, os.ErrNotExist, "repository remains after transient sharing violation: %v", err)
	}
}
