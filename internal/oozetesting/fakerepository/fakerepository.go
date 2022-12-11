package fakerepository

import (
	"sort"
	"strings"

	"github.com/gtramontina/ooze/internal/gosourcefile"
)

type FS map[string][]byte

type FakeRepository struct {
	fs FS
}

func New(fs FS) *FakeRepository {
	return &FakeRepository{
		fs: fs,
	}
}

func (r *FakeRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	var filePaths []string

	for filePath := range r.fs {
		if strings.HasSuffix(filePath, ".go") && !strings.HasSuffix(filePath, "_test.go") {
			filePaths = append(filePaths, filePath)
		}
	}

	sort.Strings(filePaths)

	sources := make([]*gosourcefile.GoSourceFile, 0, len(filePaths))
	for _, filePath := range filePaths {
		sources = append(sources, gosourcefile.New(filePath, r.fs[filePath]))
	}

	return sources
}
