package processruntime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func assertInvariantViolation(t *testing.T, action func()) {
	t.Helper()
	defer func() {
		_, ok := recover().(Violation)
		require.True(t, ok)
	}()
	action()
}
