package fsrepository_test

import (
	"os"
	"testing"

	"github.com/gtramontina/ooze/internal/fsrepository"
	"github.com/gtramontina/ooze/internal/gosourcefile"

	"github.com/stretchr/testify/assert"
)

func TestFSRepository(t *testing.T) {
	t.Run("panics when given root does not exist", func(t *testing.T) {
		repository := fsrepository.New("nonexistent")
		assert.PanicsWithValue(t, "nonexistent: no such file or directory", func() {
			repository.ListGoSourceFiles()
		})
	})

	t.Run("empty source files", func(t *testing.T) {
		repository := fsrepository.New(t.TempDir())
		files := repository.ListGoSourceFiles()
		assert.Equal(t, []*gosourcefile.GoSourceFile{}, files)
	})

	t.Run("single source file", func(t *testing.T) {
		dir := t.TempDir()
		assert.NoError(t, os.WriteFile(dir+"/source.go", []byte("source data"), 0644))

		repository := fsrepository.New(dir)
		files := repository.ListGoSourceFiles()
		assert.Equal(t, []*gosourcefile.GoSourceFile{
			gosourcefile.New("source.go", []byte("source data")),
		}, files)
	})

	t.Run("multiple source files", func(t *testing.T) {
		dir := t.TempDir()
		assert.NoError(t, os.WriteFile(dir+"/source1.go", []byte("source data 1"), 0644))
		assert.NoError(t, os.WriteFile(dir+"/source2.go", []byte("source data 2"), 0644))
		assert.NoError(t, os.WriteFile(dir+"/source3.go", []byte("source data 3"), 0644))

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
		assert.NoError(t, os.WriteFile(dir+"/source1.go", []byte("source data 1"), 0644))
		assert.NoError(t, os.WriteFile(dir+"/source2.rs", []byte("source data 2"), 0644))

		repository := fsrepository.New(dir)
		files := repository.ListGoSourceFiles()
		assert.Equal(t, []*gosourcefile.GoSourceFile{
			gosourcefile.New("source1.go", []byte("source data 1")),
		}, files)
	})

	t.Run("does not include Go test files", func(t *testing.T) {
		dir := t.TempDir()
		assert.NoError(t, os.WriteFile(dir+"/source1.go", []byte("source data 1"), 0644))
		assert.NoError(t, os.WriteFile(dir+"/source1_test.go", []byte("test data 1"), 0644))

		repository := fsrepository.New(dir)
		files := repository.ListGoSourceFiles()
		assert.Equal(t, []*gosourcefile.GoSourceFile{
			gosourcefile.New("source1.go", []byte("source data 1")),
		}, files)
	})

	t.Run("recursive directories", func(t *testing.T) {
		dir := t.TempDir()
		assert.NoError(t, os.MkdirAll(dir+"/a/b", 0700))
		assert.NoError(t, os.WriteFile(dir+"/source1.go", []byte("source data 1"), 0644))
		assert.NoError(t, os.WriteFile(dir+"/a/source2.go", []byte("source data 2"), 0644))
		assert.NoError(t, os.WriteFile(dir+"/a/b/source3.go", []byte("source data 3"), 0644))

		repository := fsrepository.New(dir)
		files := repository.ListGoSourceFiles()
		assert.Equal(t, []*gosourcefile.GoSourceFile{
			gosourcefile.New("a/b/source3.go", []byte("source data 3")),
			gosourcefile.New("a/source2.go", []byte("source data 2")),
			gosourcefile.New("source1.go", []byte("source data 1")),
		}, files)
	})

	t.Run("relative root", func(t *testing.T) {
		assert.NoError(t, os.MkdirAll(".testdata/a/b", 0700))
		defer func() { assert.NoError(t, os.RemoveAll(".testdata")) }()

		assert.NoError(t, os.WriteFile(".testdata/readme.md", []byte("read me"), 0644))
		assert.NoError(t, os.WriteFile(".testdata/source1.go", []byte("source data 1"), 0644))
		assert.NoError(t, os.WriteFile(".testdata/source1_test.go", []byte("test data 1"), 0644))
		assert.NoError(t, os.WriteFile(".testdata/a/source2.go", []byte("source data 2"), 0644))
		assert.NoError(t, os.WriteFile(".testdata/a/source2_test.go", []byte("test data 2"), 0644))
		assert.NoError(t, os.WriteFile(".testdata/a/b/source3.go", []byte("source data 3"), 0644))
		assert.NoError(t, os.WriteFile(".testdata/a/b/source3_test.go", []byte("test data 3"), 0644))

		repository := fsrepository.New(".testdata")
		files := repository.ListGoSourceFiles()
		assert.Equal(t, []*gosourcefile.GoSourceFile{
			gosourcefile.New("a/b/source3.go", []byte("source data 3")),
			gosourcefile.New("a/source2.go", []byte("source data 2")),
			gosourcefile.New("source1.go", []byte("source data 1")),
		}, files)
	})
}
