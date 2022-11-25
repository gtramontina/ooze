package fsrepository

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gtramontina/ooze/gosourcefile"
)

type FSRepository struct {
	root string
}

func New(root string) *FSRepository {
	return &FSRepository{
		root: root,
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
		if errors.Is(err, fs.ErrNotExist) {
			panic(err.(*fs.PathError).Path + ": no such file or directory")
		}

		panic(err.Error())
	}

	sort.Strings(paths)

	var sourceFiles = make([]*gosourcefile.GoSourceFile, len(paths))
	for i, path := range paths {
		data, _ := os.ReadFile(path)
		relativePath, _ := filepath.Rel(r.root, path)
		sourceFiles[i] = gosourcefile.New(relativePath, data)
	}

	return sourceFiles
}
