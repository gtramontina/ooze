package fakerepository

import (
	"sort"
	"strings"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/ooze"
)

type FakeTemporaryRepository struct {
	root    string
	fs      FS
	removed bool
}

func NewTemporary() *FakeTemporaryRepository {
	return NewTemporaryAt("<unset>")
}

func NewTemporaryAt(root string) *FakeTemporaryRepository {
	return &FakeTemporaryRepository{
		root:    root,
		fs:      FS{},
		removed: false,
	}
}

func (r *FakeTemporaryRepository) Root() string {
	if r.removed {
		panic("repository already removed!")
	}

	return r.root
}

func (r *FakeTemporaryRepository) Overwrite(filePath string, data []byte) {
	if r.removed {
		panic("repository already removed!")
	}

	r.fs[filePath] = data
}

func (r *FakeTemporaryRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	if r.removed {
		panic("repository already removed!")
	}
	paths := make([]string, 0, len(r.fs))
	for path := range r.fs {
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	sources := make([]*gosourcefile.GoSourceFile, 0, len(paths))
	for _, path := range paths {
		sources = append(sources, gosourcefile.New(path, r.fs[path]))
	}

	return sources
}

func (r *FakeTemporaryRepository) MaterializeTemporaryRepository(path string) ooze.TemporaryRepository {
	if r.removed {
		panic("repository already removed!")
	}
	materialized := NewTemporaryAt(path)
	materialized.fs = r.fs.copy()

	return materialized
}

func (r *FakeTemporaryRepository) Remove() {
	if r.removed {
		panic("repository already removed!")
	}

	r.removed = true
}

func (r *FakeTemporaryRepository) Removed() bool {
	return r.removed
}

func (r *FakeTemporaryRepository) ListFiles() FS {
	return r.fs.copy()
}
