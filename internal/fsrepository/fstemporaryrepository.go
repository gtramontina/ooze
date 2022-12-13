package fsrepository

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var (
	errNotAllowed = errors.New("not allowed")
	errRemoved    = errors.New("repository has been removed")
)

type FSTemporaryRepository struct {
	root    string
	removed bool
}

func NewTemporary(root string) *FSTemporaryRepository {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		panic(err)
	}

	return &FSTemporaryRepository{
		root:    absRoot,
		removed: false,
	}
}

func (r *FSTemporaryRepository) Root() string {
	if r.removed {
		panic(errRemoved)
	}

	return r.root
}

func (r *FSTemporaryRepository) Overwrite(filePath string, data []byte) {
	if r.removed {
		panic(errRemoved)
	}

	fullPath := path.Join(r.root, filePath)

	if !strings.HasPrefix(fullPath, r.root) {
		panic(fmt.Errorf("%w: resolved path '%s' does not belong to root '%s'", errNotAllowed, fullPath, r.root))
	}

	if _, err := os.Stat(fullPath); err == nil && os.Remove(fullPath) != nil {
		panic(fmt.Errorf("failed removing existing file '%s': %w", filePath, err))
	}

	err := os.WriteFile(fullPath, data, os.ModePerm)
	if err != nil {
		panic(fmt.Errorf("failed writing data to file '%s', %w", filePath, err))
	}
}

func (r *FSTemporaryRepository) Remove() {
	if r.removed {
		panic(errRemoved)
	}

	err := os.RemoveAll(r.root)
	if err != nil {
		panic(fmt.Errorf("failed removing repository at '%s': %w", r.root, err))
	}

	r.removed = true
}
