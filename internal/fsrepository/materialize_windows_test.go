//go:build windows

package fsrepository //nolint:testpackage // Exercises the private hard-link fallback seam.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaterializeFileCopiesWhenHardLinksAreUnavailable(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.go")
	destinationPath := filepath.Join(dir, "destination.go")
	require.NoError(t, os.WriteFile(sourcePath, []byte("package source\n"), 0o600))

	linkAttempts := 0
	createHardLink := func(actualSourcePath, actualDestinationPath string) error {
		linkAttempts++
		assert.Equal(t, sourcePath, actualSourcePath)
		assert.Equal(t, destinationPath, actualDestinationPath)

		return errors.New("hard links unavailable")
	}

	require.NoError(t, materializeFileUsing(sourcePath, destinationPath, createHardLink))
	assert.Equal(t, 1, linkAttempts)

	actual, err := os.ReadFile(destinationPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("package source\n"), actual)

	require.NoError(t, os.WriteFile(destinationPath, []byte("package destination\n"), 0o600))
	original, err := os.ReadFile(sourcePath)
	require.NoError(t, err)
	assert.Equal(t, []byte("package source\n"), original)
}
