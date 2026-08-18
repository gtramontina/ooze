//go:build !windows

package fsrepository_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/gtramontina/ooze/internal/fsrepository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverwriteHonorsTheProcessUmask(t *testing.T) {
	dir := t.TempDir()
	fsrepository.NewTemporary(dir).Overwrite("mutant.go", nil)

	info, err := os.Stat(filepath.Join(dir, "mutant.go"))
	require.NoError(t, err)

	umask := syscall.Umask(0)
	defer syscall.Umask(umask)
	assert.Equal(t, os.ModePerm^os.FileMode(umask), info.Mode()) //nolint:gosec // Verifies umask behavior.
}
