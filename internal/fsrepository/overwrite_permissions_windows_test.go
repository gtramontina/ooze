//go:build windows

package fsrepository_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gtramontina/ooze/internal/fsrepository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverwriteLeavesMutantsWritable(t *testing.T) {
	dir := t.TempDir()
	fsrepository.NewTemporary(dir).Overwrite("mutant.go", nil)

	info, err := os.Stat(filepath.Join(dir, "mutant.go"))
	require.NoError(t, err)
	assert.NotZero(t, info.Mode().Perm()&0o200)
}
