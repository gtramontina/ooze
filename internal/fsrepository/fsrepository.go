package fsrepository

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/ooze"
)

type FSRepository struct {
	root string
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
		root: absRoot,
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

		paths = append(paths, path)

		return nil
	})
	if err != nil {
		panic(err)
	}

	sort.Strings(paths)

	sourceFiles := make([]*gosourcefile.GoSourceFile, len(paths))

	for i, path := range paths {
		data, _ := os.ReadFile(path)
		relativePath, _ := filepath.Rel(r.root, path)
		sourceFiles[i] = gosourcefile.New(relativePath, data)
	}

	return sourceFiles
}

func (r *FSRepository) LinkAllToTemporaryRepository(temporaryPath string) ooze.TemporaryRepository {
	rootSize := len(strings.Split(r.root, string(os.PathSeparator)))

	err := filepath.WalkDir(r.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return err
		}

		absolutePath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("failed getting absolute path for '%s': %w", path, err)
		}

		linkPath := filepath.Join(temporaryPath, filepath.Join(strings.Split(path, string(os.PathSeparator))[rootSize:]...))
		err = os.MkdirAll(filepath.Dir(linkPath), os.ModePerm)
		if err != nil {
			return fmt.Errorf("failed creating directory tree for '%s': %w", linkPath, err)
		}

		err = os.Symlink(absolutePath, linkPath)
		if err != nil {
			return fmt.Errorf("failed creating link from '%s' to '%s': %w", path, linkPath, err)
		}

		return nil
	})
	if err != nil {
		panic(fmt.Errorf("failed scanning '%s': %w", r.root, err))
	}

	return NewTemporary(temporaryPath)
}
