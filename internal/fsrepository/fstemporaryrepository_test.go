package fsrepository_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gtramontina/ooze/internal/fsrepository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFSTemporaryRepository(t *testing.T) {
	t.Run("exposes the root path", func(t *testing.T) {
		dir := t.TempDir()
		repository := fsrepository.NewTemporary(dir)
		assert.Equal(t, dir, repository.Root())
	})

	t.Run("root path is absolute", func(t *testing.T) {
		cwd, err := os.Getwd()
		assert.NoError(t, err)

		repository := fsrepository.NewTemporary(".")
		assert.Equal(t, cwd, repository.Root())
	})

	t.Run("overwriting", func(t *testing.T) {
		t.Run("creates a new file when it doesn't exist", func(t *testing.T) {
			dir := t.TempDir()
			repository := fsrepository.NewTemporary(dir)
			repository.Overwrite("file.txt", []byte("some data"))

			data, err := os.ReadFile(filepath.Join(dir, "file.txt"))
			assert.NoError(t, err)
			assert.Equal(t, []byte("some data"), data)
		})

		t.Run("an existing regular file", func(t *testing.T) {
			dir := t.TempDir()
			filePath := filepath.Join(dir, "file.txt")
			assert.NoError(t, os.WriteFile(filePath, []byte("original data"), 0o600))

			repository := fsrepository.NewTemporary(dir)
			repository.Overwrite("file.txt", []byte("new data"))

			data, err := os.ReadFile(filePath)
			assert.NoError(t, err)
			assert.Equal(t, []byte("new data"), data)
		})

		t.Run("an existing hard-linked file", func(t *testing.T) {
			dir := t.TempDir()
			sourcePath := filepath.Join(dir, "file.txt")
			linkedPath := filepath.Join(dir, "linked.txt")
			assert.NoError(t, os.WriteFile(sourcePath, []byte("original data"), 0o600))
			assert.NoError(t, os.Link(sourcePath, linkedPath))

			repository := fsrepository.NewTemporary(dir)
			repository.Overwrite("linked.txt", []byte("new data"))

			data, err := os.ReadFile(linkedPath)
			assert.NoError(t, err)
			assert.Equal(t, []byte("new data"), data)

			source, err := os.ReadFile(sourcePath)
			assert.NoError(t, err)
			assert.Equal(t, []byte("original data"), source)
		})

		t.Run("does not allow writing past the given root path", func(t *testing.T) {
			dir := t.TempDir()
			assert.NoError(t, os.MkdirAll(filepath.Join(dir, "cant-overwrite", "child"), 0o700))
			assert.NoError(t, os.WriteFile(filepath.Join(dir, "cant-overwrite", "original.txt"), []byte("original data"), 0o600))

			repository := fsrepository.NewTemporary(filepath.Join(dir, "cant-overwrite", "child"))
			assert.Panics(t, func() {
				repository.Overwrite("../original.txt", []byte("new data"))
			})
			data, err := os.ReadFile(filepath.Join(dir, "cant-overwrite", "original.txt"))
			assert.NoError(t, err)
			assert.Equal(t, []byte("original data"), data)
		})

		for _, rootPath := range []struct {
			name string
			path string
		}{
			{name: "an empty path does not overwrite the repository root", path: ""},
			{name: "a dot path does not overwrite the repository root", path: "."},
		} {
			t.Run(rootPath.name, func(t *testing.T) {
				dir := t.TempDir()
				repository := fsrepository.NewTemporary(dir)

				assert.Panics(t, func() {
					repository.Overwrite(rootPath.path, []byte("new data"))
				})

				info, err := os.Stat(dir)
				require.NoError(t, err)
				assert.True(t, info.IsDir())
			})
		}
	})

	t.Run("deleting", func(t *testing.T) {
		dir := t.TempDir()
		repository := fsrepository.NewTemporary(dir)
		repository.Remove()

		t.Run("removes the entire directory", func(t *testing.T) {
			_, err := os.ReadDir(dir)
			assert.ErrorIs(t, err, os.ErrNotExist)
		})

		t.Run("fails all other actions", func(t *testing.T) {
			assert.PanicsWithError(t, "repository has been removed", func() {
				repository.Root()
			})

			assert.PanicsWithError(t, "repository has been removed", func() {
				repository.Overwrite("dummy.txt", []byte("dummy data"))
			})

			assert.PanicsWithError(t, "repository has been removed", func() {
				repository.Remove()
			})
		})
	})
}
