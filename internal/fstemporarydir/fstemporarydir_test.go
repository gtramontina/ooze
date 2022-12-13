package fstemporarydir_test

import (
	"os"
	"strings"
	"testing"

	"github.com/gtramontina/ooze/internal/fstemporarydir"
	"github.com/stretchr/testify/assert"
)

func TestFSTemporaryDir(t *testing.T) {
	t.Parallel()

	t.Run("creates a temporary directory under the OS temporary directory", func(t *testing.T) {
		t.Parallel()

		temporaryDir := fstemporarydir.New("fs-temporary-dir-test-")

		temporaryDirPathA := temporaryDir.New()
		temporaryDirPathB := temporaryDir.New()

		assert.NotEqual(t, temporaryDirPathA, temporaryDirPathB)

		assert.True(t, strings.HasPrefix(temporaryDirPathA, os.TempDir()))
		assert.True(t, strings.HasPrefix(temporaryDirPathB, os.TempDir()))

		statDirPathA, err := os.Stat(temporaryDirPathA)
		assert.NoError(t, err)
		assert.True(t, statDirPathA.IsDir())
		statDirPathB, err := os.Stat(temporaryDirPathB)
		assert.NoError(t, err)
		assert.True(t, statDirPathB.IsDir())
	})
}
