package fsrepository

import (
	"errors"
	"fmt"
	"go/build"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/ooze"
)

type FSRepository struct {
	root         string
	buildContext build.Context
}

func New(root string) *FSRepository {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		panic(err)
	}

	stat, err := os.Stat(absRoot)
	if errors.Is(err, fs.ErrNotExist) {
		panic(root + ": no such directory")
	}

	if err != nil {
		panic(err)
	}

	if !stat.IsDir() {
		panic(root + ": not a directory")
	}

	return &FSRepository{
		root:         absRoot,
		buildContext: build.Default,
	}
}

func (r *FSRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	var paths []string

	err := filepath.WalkDir(r.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}

		matches, err := r.buildContext.MatchFile(filepath.Dir(path), entry.Name())
		if err != nil {
			return fmt.Errorf("match Go source file '%s' to build context: %w", path, err)
		}
		if !matches {
			return nil
		}

		paths = append(paths, path)

		return nil
	})
	if err != nil {
		panic(err)
	}

	sort.Strings(paths)

	sourceFiles := make([]*gosourcefile.GoSourceFile, len(paths))

	for index, filePath := range paths {
		data, err := os.ReadFile(filePath)
		if err != nil {
			panic(fmt.Errorf("failed reading source file '%s': %w", filePath, err))
		}

		sourceFiles[index] = gosourcefile.New(r.logicalPath(filePath), data)
	}

	return sourceFiles
}

func (r *FSRepository) MaterializeTemporaryRepository(temporaryPath string) ooze.TemporaryRepository {
	err := filepath.WalkDir(r.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return err
		}

		relativePath, err := filepath.Rel(r.root, path)
		if err != nil {
			return fmt.Errorf("failed getting path relative to '%s' for '%s': %w", r.root, path, err)
		}

		materializedPath := filepath.Join(temporaryPath, relativePath)
		err = os.MkdirAll(filepath.Dir(materializedPath), os.ModePerm)
		if err != nil {
			return fmt.Errorf("failed creating directory tree for '%s': %w", materializedPath, err)
		}

		err = materializeFile(path, materializedPath)
		if err != nil {
			return fmt.Errorf("failed materializing '%s' at '%s': %w", path, materializedPath, err)
		}

		return nil
	})
	if err != nil {
		panic(fmt.Errorf("failed scanning '%s': %w", r.root, err))
	}

	return NewTemporary(temporaryPath)
}

func (r *FSRepository) logicalPath(filePath string) string {
	relativePath, err := filepath.Rel(r.root, filePath)
	if err != nil {
		panic(fmt.Errorf("failed resolving source path '%s' from root '%s': %w", filePath, r.root, err))
	}

	return filepath.ToSlash(relativePath)
}
