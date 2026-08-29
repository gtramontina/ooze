package fsrepository

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoveRepositoryUsingRetriesOnlyTransientFailures(t *testing.T) {
	transient := errors.New("transient")
	calls := 0
	waits := 0
	err := removeRepositoryUsing("workspace", func(string) error {
		calls++
		if calls < 3 {
			return transient
		}
		return nil
	}, func(err error) bool { return errors.Is(err, transient) }, func(time.Duration) { waits++ })
	require.NoError(t, err, "retry result err=%v calls=%d waits=%d, want nil/3/2", err, calls, waits)
	assert.EqualValues(t, 3, calls, "retry result err=%v calls=%d waits=%d, want nil/3/2", err, calls, waits)
	assert.EqualValues(t, 2, waits, "retry result err=%v calls=%d waits=%d, want nil/3/2", err, calls, waits)

	permanent := errors.New("permanent")
	calls = 0
	waits = 0
	err = removeRepositoryUsing("workspace", func(string) error {
		calls++
		return permanent
	}, func(error) bool { return false }, func(time.Duration) { waits++ })
	require.ErrorIs(t, err, permanent, "permanent result err=%v calls=%d waits=%d, want permanent/1/0", err, calls, waits)
	assert.EqualValues(t, 1, calls, "permanent result err=%v calls=%d waits=%d, want permanent/1/0", err, calls, waits)
	assert.EqualValues(t, 0, waits, "permanent result err=%v calls=%d waits=%d, want permanent/1/0", err, calls, waits)

	t.Run("bounded exhaustion", func(t *testing.T) {
		calls := 0
		var delays []time.Duration
		err := removeRepositoryUsing("workspace", func(string) error {
			calls++
			return transient
		}, func(error) bool { return true }, func(delay time.Duration) {
			delays = append(delays, delay)
		})
		require.ErrorIs(t, err, transient, "exhaustion err=%v calls=%d waits=%d", err, calls, len(delays))
		assert.Equal(t, temporaryRepositoryRemoveAttempts, calls, "exhaustion err=%v calls=%d waits=%d", err, calls, len(delays))
		assert.Equal(t, temporaryRepositoryRemoveAttempts-1, len(delays), "exhaustion err=%v calls=%d waits=%d", err, calls, len(delays))
		for index, delay := range delays {
			assert.Equal(t, temporaryRepositoryRemoveDelay, delay, "wait %d = %s, want %s", index, delay, temporaryRepositoryRemoveDelay)
		}
	})
}
