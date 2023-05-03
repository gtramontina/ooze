package fakerepository

import (
	"sort"
	"strings"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/ooze"
)

type FS map[string][]byte

func (f FS) copy() FS {
	fsCopy := FS{}
	for k, v := range f {
		fsCopy[k] = v
	}

	return fsCopy
}

type FakeRepository struct {
	fs        FS
	temps     []*FakeTemporaryRepository
	tempCount int
}

func New(fs FS, temporaries ...*FakeTemporaryRepository) *FakeRepository {
	return &FakeRepository{
		fs:        fs,
		temps:     temporaries,
		tempCount: 0,
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
		sources = append(sources, gosourcefile.New("", filePath, r.fs[filePath]))
	}

	return sources
}

func (r *FakeRepository) LinkAllToTemporaryRepository(directoryPath string) ooze.TemporaryRepository {
	if r.tempCount >= len(r.temps) {
		panic("fakerepository: temporary repositories not setup")
	}

	tempRepository := r.temps[r.tempCount]
	tempRepository.root = directoryPath
	tempRepository.fs = r.fs.copy()
	r.tempCount++

	return tempRepository
}
