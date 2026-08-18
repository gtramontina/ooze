//go:build !windows

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
	assert.NotZero(t, info.Mode()&fs.ModeSymlink)

	sourceInfo, err := os.Stat(sourcePath)
	require.NoError(t, err)
	materializedInfo, err := os.Stat(materializedPath)
	require.NoError(t, err)
	assert.True(t, os.SameFile(sourceInfo, materializedInfo), "materialized file must link to its matching source")
}
