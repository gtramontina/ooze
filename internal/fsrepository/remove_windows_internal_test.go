//go:build windows

package fsrepository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/windows"
)

func TestTransientRepositoryRemoveErrorClassifiesOnlyBoundedWindowsFailures(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "sharing violation", err: windows.ERROR_SHARING_VIOLATION, want: true},
		{name: "lock violation", err: windows.ERROR_LOCK_VIOLATION, want: true},
		{name: "directory not empty", err: windows.ERROR_DIR_NOT_EMPTY, want: true},
		{name: "access denied", err: windows.ERROR_ACCESS_DENIED, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			{
				got := transientRepositoryRemoveError(test.err)
				assert.Equal(t, test.want, got, "transientRepositoryRemoveError(%v) = %t, want %t", test.err, got, test.want)
			}
		})
	}
}
