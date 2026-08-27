package processruntime

import (
	"testing"
	"time"

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

func automaticDeadlineTrip() attemptTripped {
	return attemptTripped{kind: deadlineTrip, profile: AutomaticProfile, deadline: 31 * time.Second}
}
