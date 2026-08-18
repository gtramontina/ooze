//go:build windows

package fsrepository_test

import (
	"io/fs"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertMaterializedFile(t *testing.T, sourcePath, materializedPath string) {
	t.Helper()

	info, err := os.Lstat(materializedPath)
	require.NoError(t, err)
	assert.Zero(t, info.Mode()&fs.ModeSymlink, "Windows sandboxes must not require symlink privileges")

	source, err := os.ReadFile(sourcePath)
	require.NoError(t, err)
	materialized, err := os.ReadFile(materializedPath)
	require.NoError(t, err)
	assert.Equal(t, source, materialized)
}
