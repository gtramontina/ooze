package fsrepository_test

import (
	"go/build"
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
		filePath := filepath.Join(dir, "not-a-dir")
		assert.NoError(t, os.WriteFile(filePath, []byte("source data"), 0o600))

		assert.PanicsWithValue(t, filePath+": not a directory", func() {
			fsrepository.New(filePath)
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

	t.Run("only includes source files matching the current build context", func(t *testing.T) {
		dir := t.TempDir()
		currentOS := build.Default.GOOS
		currentArch := build.Default.GOARCH
		otherOS := "windows"
		if currentOS == otherOS {
			otherOS = "linux"
		}
		otherArch := "amd64"
		if currentArch == otherArch {
			otherArch = "arm64"
		}

		writeSourceFile(t, dir, "architecture_"+currentArch+".go", "package fixture\n")
		writeSourceFile(t, dir, "architecture_"+otherArch+".go", "package fixture\n")
		writeSourceFile(t, dir, "combined_"+currentOS+"_"+currentArch+".go", "package fixture\n")
		writeSourceFile(t, dir, "common.go", "package fixture\n")
		writeSourceFile(t, dir, "filename_"+currentOS+".go", "package fixture\n")
		writeSourceFile(t, dir, "filename_"+otherOS+".go", "package fixture\n")
		writeSourceFile(t, dir, "constraint_current.go", "//go:build "+currentOS+"\n\npackage fixture\n")
		writeSourceFile(t, dir, "constraint_other.go", "//go:build "+otherOS+"\n\npackage fixture\n")

		repository := fsrepository.New(dir)
		files := repository.ListGoSourceFiles()
		assert.Equal(t, []*gosourcefile.GoSourceFile{
			gosourcefile.New("architecture_"+currentArch+".go", []byte("package fixture\n")),
			gosourcefile.New("combined_"+currentOS+"_"+currentArch+".go", []byte("package fixture\n")),
			gosourcefile.New("common.go", []byte("package fixture\n")),
			gosourcefile.New("constraint_current.go", []byte("//go:build "+currentOS+"\n\npackage fixture\n")),
			gosourcefile.New("filename_"+currentOS+".go", []byte("package fixture\n")),
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

func writeSourceFile(t *testing.T, root, name, contents string) {
	t.Helper()
	assert.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600))
}

func TestFSRepository_MaterializeTemporaryRepository(t *testing.T) {
	dir := t.TempDir()
	assert.NoError(t, os.MkdirAll(filepath.Join(dir, "to-link", "child_a", "child_b"), 0o700))

	assert.NoError(t, os.WriteFile(filepath.Join(dir, "to-link", "readme.md"), []byte("readme"), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "to-link", "makefile"), []byte("makefile"), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "to-link", "test_a.go"), []byte("test a"), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "to-link", "test_b.go"), []byte("test b"), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "to-link", "child_a", "test_c.go"), []byte("test c"), 0o600))
	nestedSourcePath := filepath.Join(dir, "to-link", "child_a", "child_b", "test_d.go")
	assert.NoError(t, os.WriteFile(nestedSourcePath, []byte("test d"), 0o600))

	sourceRoot := filepath.Join(dir, "to-link")
	materializedRoot := filepath.Join(dir, "materialized")
	repository := fsrepository.New(sourceRoot)
	temporaryRepository := repository.MaterializeTemporaryRepository(materializedRoot)

	t.Run("materializes all files recursively", func(t *testing.T) {
		var files []string
		err := filepath.WalkDir(materializedRoot, func(path string, entry fs.DirEntry, err error) error {
			assert.NoError(t, err)
			if entry.IsDir() {
				return nil
			}

			relativePath, err := filepath.Rel(materializedRoot, path)
			assert.NoError(t, err)
			assertMaterializedFile(t, filepath.Join(sourceRoot, relativePath), path)

			files = append(files, filepath.ToSlash(relativePath))

			return nil
		})
		assert.NoError(t, err)
		assert.Equal(t, []string{
			"child_a/child_b/test_d.go",
			"child_a/test_c.go",
			"makefile",
			"readme.md",
			"test_a.go",
			"test_b.go",
		}, files)
	})

	t.Run("results in a new temporary repository", func(t *testing.T) {
		assert.Equal(t, fsrepository.NewTemporary(materializedRoot), temporaryRepository)
	})

	t.Run("isolates overwritten files from their sources", func(t *testing.T) {
		temporaryRepository.Overwrite("test_a.go", []byte("mutated"))

		materialized, err := os.ReadFile(filepath.Join(materializedRoot, "test_a.go"))
		assert.NoError(t, err)
		assert.Equal(t, []byte("mutated"), materialized)

		source, err := os.ReadFile(filepath.Join(sourceRoot, "test_a.go"))
		assert.NoError(t, err)
		assert.Equal(t, []byte("test a"), source)
	})
}
