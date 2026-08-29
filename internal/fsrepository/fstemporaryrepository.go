package fsrepository

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/ooze"
)

var (
	errNotAllowed = errors.New("not allowed")
	errRemoved    = errors.New("repository has been removed")
)

const (
	temporaryRepositoryRemoveAttempts = 10
	temporaryRepositoryRemoveDelay    = 20 * time.Millisecond
)

type FSTemporaryRepository struct {
	root    string
	removed bool
}

func (r *FSTemporaryRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	return New(r.Root()).ListGoSourceFiles()
}

func (r *FSTemporaryRepository) MaterializeTemporaryRepository(temporaryPath string) ooze.TemporaryRepository {
	return New(r.Root()).MaterializeTemporaryRepository(temporaryPath)
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

	relativePath := filepath.Clean(filepath.FromSlash(filePath))
	if relativePath == "." || !filepath.IsLocal(relativePath) {
		panic(fmt.Errorf("%w: path '%s' does not identify a file within root '%s'", errNotAllowed, filePath, r.root))
	}

	root, err := os.OpenRoot(r.root)
	if err != nil {
		panic(fmt.Errorf("failed opening repository root '%s': %w", r.root, err))
	}
	defer func() { _ = root.Close() }()

	_, statErr := root.Lstat(relativePath)
	if statErr == nil {
		removeErr := root.Remove(relativePath)
		if removeErr != nil {
			panic(fmt.Errorf("failed removing existing file '%s': %w", filePath, removeErr))
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		panic(fmt.Errorf("failed inspecting file '%s': %w", filePath, statErr))
	}

	err = root.WriteFile(relativePath, data, os.ModePerm)
	if err != nil {
		panic(fmt.Errorf("failed writing data to file '%s', %w", filePath, err))
	}
}

func (r *FSTemporaryRepository) Remove() {
	if r.removed {
		panic(errRemoved)
	}

	err := removeRepositoryUsing(r.root, os.RemoveAll, transientRepositoryRemoveError, time.Sleep)
	if err != nil {
		panic(fmt.Errorf("failed removing repository at '%s': %w", r.root, err))
	}

	r.removed = true
}

func removeRepositoryUsing(
	path string,
	remove func(string) error,
	retryable func(error) bool,
	wait func(time.Duration),
) error {
	var err error
	for attempt := 0; attempt < temporaryRepositoryRemoveAttempts; attempt++ {
		err = remove(path)
		if err == nil || !retryable(err) {
			return err
		}
		if attempt+1 < temporaryRepositoryRemoveAttempts {
			wait(temporaryRepositoryRemoveDelay)
		}
	}
	return err
}
