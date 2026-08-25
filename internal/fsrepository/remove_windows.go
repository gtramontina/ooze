//go:build windows

package fsrepository

import (
	"errors"

	"golang.org/x/sys/windows"
)

func transientRepositoryRemoveError(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_LOCK_VIOLATION) ||
		errors.Is(err, windows.ERROR_DIR_NOT_EMPTY)
}
