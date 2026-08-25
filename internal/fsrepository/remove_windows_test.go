//go:build windows

package fsrepository_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/fsrepository"
	"golang.org/x/sys/windows"
)

func TestFSTemporaryRepositoryRetriesWindowsDeleteSharing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "held.txt")
	if err := os.WriteFile(path, []byte("held"), 0o600); err != nil {
		t.Fatal(err)
	}
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(
		pointer, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = windows.CloseHandle(handle)
		close(closed)
	}()

	if err := os.Remove(path); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("held-file removal error = %v, want sharing violation", err)
	}
	fsrepository.NewTemporary(dir).Remove()
	<-closed
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("repository remains after transient sharing violation: %v", err)
	}
}
