//go:build !windows

package fsrepository_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gtramontina/ooze/internal/fsrepository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverwriteDoesNotFollowParentSymlinksOutsideTheRepository(t *testing.T) {
	externalDir := t.TempDir()
	externalFile := filepath.Join(externalDir, "source.go")
	require.NoError(t, os.WriteFile(externalFile, []byte("original"), 0o600))

	repositoryDir := t.TempDir()
	require.NoError(t, os.Symlink(externalDir, filepath.Join(repositoryDir, "escape")))
	repository := fsrepository.NewTemporary(repositoryDir)

	assert.Panics(t, func() {
		repository.Overwrite("escape/source.go", []byte("mutated"))
	})

	content, err := os.ReadFile(externalFile)
	require.NoError(t, err)
	assert.Equal(t, []byte("original"), content)
}
