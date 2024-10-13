package fsrepository_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gtramontina/ooze/internal/fsrepository"
	"github.com/stretchr/testify/assert"
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
			assert.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("original data"), 0o600))

			repository := fsrepository.NewTemporary(dir)
			repository.Overwrite("file.txt", []byte("new data"))

			data, err := os.ReadFile(filepath.Join(dir, "file.txt"))
			assert.NoError(t, err)
			assert.Equal(t, []byte("new data"), data)


			//assert.NoError(t, err)
			//mask := syscall.Umask(0)
			//defer syscall.Umask(mask)
			//assert.Equal(t, os.ModePerm^os.FileMode(mask), stat.Mode()) //nolint:gosec
		})

		t.Run("an existing linked file", func(t *testing.T) {
			dir := t.TempDir()
			assert.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("original data"), 0o600))
			assert.NoError(t, os.Symlink(filepath.Join(dir, "file.txt"), filepath.Join(dir, "linked.txt")))

			repository := fsrepository.NewTemporary(dir)
			repository.Overwrite("linked.txt", []byte("new data"))

			data, err := os.ReadFile(filepath.Join(dir, "linked.txt"))
			assert.NoError(t, err)
			assert.Equal(t, []byte("new data"), data)
		})

		t.Run("does not allow writing past the given root path", func(t *testing.T) {
			dir := t.TempDir()
			assert.NoError(t, os.MkdirAll(filepath.Join(dir, "cant-overwrite", "child"), 0o700))
			assert.NoError(t, os.WriteFile(filepath.Join(dir, "cant-overwrite", "original.txt"), []byte("original data"), 0o600))

			repository := fsrepository.NewTemporary(filepath.Join(dir, "cant-overwrite", "child"))
			assert.Panics(t, func() {
				repository.Overwrite(filepath.Join("..", "original.txt"), []byte("new data"))
			})
			data, err := os.ReadFile(filepath.Join(dir, "cant-overwrite", "original.txt"))
			assert.NoError(t, err)
			assert.Equal(t, []byte("original data"), data)
		})
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
