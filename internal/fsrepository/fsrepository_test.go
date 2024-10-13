package fsrepository_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/gtramontina/ooze/internal/fsrepository"
	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/stretchr/testify/assert"
)

func TestFSRepository(t *testing.T) {
	t.Run("panics when given root does not exist", func(t *testing.T) {
		assert.PanicsWithValue(t, "nonexistent: no such directory", func() {
			fsrepository.New("nonexistent")
		})
	})

	t.Run("panics when given root isn't a directory", func(t *testing.T) {
		dir := t.TempDir()
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "not-a-dir"), []byte("source data"), 0o600))

		assert.PanicsWithValue(t, filepath.Join(dir, "not-a-dir: not a directory"), func() {
			fsrepository.New(filepath.Join(dir, "not-a-dir"))
		})
	})
}

func TestFSRepository_ListGoSourceFiles(t *testing.T) {
	t.Run("empty source files", func(t *testing.T) {
		repository := fsrepository.New(t.TempDir())
		files := repository.ListGoSourceFiles()
		assert.Equal(t, []*gosourcefile.GoSourceFile{}, files)
	})

	t.Run("single source file", func(t *testing.T) {
		dir := t.TempDir()
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "source.go"), []byte("source data"), 0o600))

		repository := fsrepository.New(dir)
		files := repository.ListGoSourceFiles()
		assert.Equal(t, []*gosourcefile.GoSourceFile{
			gosourcefile.New("source.go", []byte("source data")),
		}, files)
	})

	t.Run("multiple source files", func(t *testing.T) {
		dir := t.TempDir()
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "source1.go"), []byte("source data 1"), 0o600))
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "source2.go"), []byte("source data 2"), 0o600))
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "source3.go"), []byte("source data 3"), 0o600))

		repository := fsrepository.New(dir)
		files := repository.ListGoSourceFiles()
		assert.Equal(t, []*gosourcefile.GoSourceFile{
			gosourcefile.New("source1.go", []byte("source data 1")),
			gosourcefile.New("source2.go", []byte("source data 2")),
			gosourcefile.New("source3.go", []byte("source data 3")),
		}, files)
	})

	t.Run("does not include non Go files", func(t *testing.T) {
		dir := t.TempDir()
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "source1.go"), []byte("source data 1"), 0o600))
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "source2.rs"), []byte("source data 2"), 0o600))

		repository := fsrepository.New(dir)
		files := repository.ListGoSourceFiles()
		assert.Equal(t, []*gosourcefile.GoSourceFile{
			gosourcefile.New("source1.go", []byte("source data 1")),
		}, files)
	})

	t.Run("does not include Go test files", func(t *testing.T) {
		dir := t.TempDir()
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "source1.go"), []byte("source data 1"), 0o600))
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "source1_test.go"), []byte("test data 1"), 0o600))

		repository := fsrepository.New(dir)
		files := repository.ListGoSourceFiles()
		assert.Equal(t, []*gosourcefile.GoSourceFile{
			gosourcefile.New("source1.go", []byte("source data 1")),
		}, files)
	})

	t.Run("recursive directories", func(t *testing.T) {
		dir := t.TempDir()
		assert.NoError(t, os.MkdirAll(filepath.Join(dir, "a", "b"), 0o700))
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "source1.go"), []byte("source data 1"), 0o600))
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "a", "source2.go"), []byte("source data 2"), 0o600))
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "a", "b", "source3.go"), []byte("source data 3"), 0o600))

		repository := fsrepository.New(dir)
		files := repository.ListGoSourceFiles()
		assert.Equal(t, []*gosourcefile.GoSourceFile{
			gosourcefile.New("a/b/source3.go", []byte("source data 3")),
			gosourcefile.New("a/source2.go", []byte("source data 2")),
			gosourcefile.New("source1.go", []byte("source data 1")),
		}, files)
	})

	t.Run("relative root", func(t *testing.T) {
		dir := t.TempDir()
		assert.NoError(t, os.MkdirAll(filepath.Join(dir, "a", "b"), 0o700))

		assert.NoError(t, os.WriteFile(filepath.Join(dir, "readme.md"), []byte("read me"), 0o600))
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "source1.go"), []byte("source data 1"), 0o600))
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "source1_test.go"), []byte("test data 1"), 0o600))
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "a", "source2.go"), []byte("source data 2"), 0o600))
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "a", "source2_test.go"), []byte("test data 2"), 0o600))
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "a", "b", "source3.go"), []byte("source data 3"), 0o600))
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "a", "b", "source3_test.go"), []byte("test data 3"), 0o600))

		repository := fsrepository.New(dir)
		files := repository.ListGoSourceFiles()
		assert.Equal(t, []*gosourcefile.GoSourceFile{
			gosourcefile.New("a/b/source3.go", []byte("source data 3")),
			gosourcefile.New("a/source2.go", []byte("source data 2")),
			gosourcefile.New("source1.go", []byte("source data 1")),
		}, files)
	})
}

func TestFSRepository_LinkAllToTemporaryRepository(t *testing.T) {
	dir := t.TempDir()
	assert.NoError(t, os.MkdirAll(filepath.Join(dir, "to-link", "child_a", "child_b"), 0o700))

	assert.NoError(t, os.MkdirAll(filepath.Join(dir, "to-link", "child_a", "child_b"), 0o700))
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "to-link", "readme.md"), []byte(""), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "to-link", "makefile"), []byte(""), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "to-link", "test_a.go"), []byte(""), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "to-link", "test_b.go"), []byte(""), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "to-link", "child_a", "test_c.go"), []byte(""), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "to-link", "child_a", "child_b", "test_d.go"), []byte(""), 0o600))

	repository := fsrepository.New(filepath.Join(dir, "to-link"))
	temporaryRepository := repository.LinkAllToTemporaryRepository(filepath.Join(dir, "linked"))

	t.Run("creates a link of all files recursively", func(t *testing.T) {
		var files []string
		err := filepath.WalkDir(filepath.Join(dir, "linked"), func(path string, entry fs.DirEntry, err error) error {
			assert.NoError(t, err)
			if entry.IsDir() {
				return nil
			}

			info, err := entry.Info()
			assert.NoError(t, err)
			assert.True(t, info.Mode()&fs.ModeSymlink != 0)

			files = append(files, path)

			return nil
		})
		assert.NoError(t, err)
		assert.Equal(t, []string{
			filepath.Join(dir, "linked", "child_a", "child_b", "test_d.go"),
			filepath.Join(dir, "linked", "child_a", "test_c.go"),
			filepath.Join(dir, "linked", "makefile"),
			filepath.Join(dir, "linked", "readme.md"),
			filepath.Join(dir, "linked", "test_a.go"),
			filepath.Join(dir, "linked", "test_b.go"),
		}, files)
	})

	t.Run("results in a new temporary repository", func(t *testing.T) {
		assert.Equal(t, fsrepository.NewTemporary(filepath.Join(dir, "linked")), temporaryRepository)
	})
}
