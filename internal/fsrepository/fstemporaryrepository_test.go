package fsrepository_test

import (
	"os"
	"syscall"
	"testing"

	"github.com/gtramontina/ooze/internal/fsrepository"
	"github.com/stretchr/testify/assert"
)

func TestFSTemporaryRepository(t *testing.T) {
	t.Parallel()

	t.Run("exposes the root path", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		repository := fsrepository.NewTemporary(dir)
		assert.Equal(t, dir, repository.Root())
	})

	t.Run("root path is absolute", func(t *testing.T) {
		t.Parallel()

		cwd, err := os.Getwd()
		assert.NoError(t, err)

		repository := fsrepository.NewTemporary(".")
		assert.Equal(t, cwd, repository.Root())
	})

	t.Run("overwriting", func(t *testing.T) {
		t.Parallel()

		t.Run("creates a new file when it doesn't exist", func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			repository := fsrepository.NewTemporary(dir)
			repository.Overwrite("file.txt", []byte("some data"))

			data, err := os.ReadFile(dir + "/file.txt")
			assert.NoError(t, err)
			assert.Equal(t, []byte("some data"), data)
		})

		t.Run("an existing regular file", func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			assert.NoError(t, os.WriteFile(dir+"/file.txt", []byte("original data"), 0o600))

			repository := fsrepository.NewTemporary(dir)
			repository.Overwrite("file.txt", []byte("new data"))

			data, err := os.ReadFile(dir + "/file.txt")
			assert.NoError(t, err)
			assert.Equal(t, []byte("new data"), data)

			stat, err := os.Stat(dir + "/file.txt")
			assert.NoError(t, err)
			mask := syscall.Umask(0)
			defer syscall.Umask(mask)
			assert.Equal(t, os.ModePerm^os.FileMode(mask), stat.Mode())
		})

		t.Run("an existing linked file", func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			assert.NoError(t, os.WriteFile(dir+"/file.txt", []byte("original data"), 0o600))
			assert.NoError(t, os.Symlink(dir+"/file.txt", dir+"/linked.txt"))

			repository := fsrepository.NewTemporary(dir)
			repository.Overwrite("linked.txt", []byte("new data"))

			data, err := os.ReadFile(dir + "/linked.txt")
			assert.NoError(t, err)
			assert.Equal(t, []byte("new data"), data)
		})

		t.Run("does not allow writing past the given root path", func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			assert.NoError(t, os.MkdirAll(dir+"/cant-overwrite/child", 0o700))
			assert.NoError(t, os.WriteFile(dir+"/cant-overwrite/original.txt", []byte("original data"), 0o600))

			repository := fsrepository.NewTemporary(dir + "/cant-overwrite/child")
			assert.Panics(t, func() {
				repository.Overwrite("../original.txt", []byte("new data"))
			})
			data, err := os.ReadFile(dir + "/cant-overwrite/original.txt")
			assert.NoError(t, err)
			assert.Equal(t, []byte("original data"), data)
		})
	})

	t.Run("deleting", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		repository := fsrepository.NewTemporary(dir)
		repository.Remove()

		t.Run("removes the entire directory", func(t *testing.T) {
			t.Parallel()

			_, err := os.ReadDir(dir)
			assert.ErrorIs(t, err, os.ErrNotExist)
		})

		t.Run("fails all other actions", func(t *testing.T) {
			t.Parallel()

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
