//go:build windows

package cmdtestrunner

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNextJobProcessCapacity(t *testing.T) {
	t.Run("uses the assigned process count when it grew", func(t *testing.T) {
		capacity, err := nextJobProcessCapacity(16, 23)

		require.NoError(t, err)
		assert.Equal(t, uint32(23), capacity)
	})

	t.Run("grows monotonically when the assigned process count did not grow", func(t *testing.T) {
		capacity, err := nextJobProcessCapacity(16, 16)

		require.NoError(t, err)
		assert.Equal(t, uint32(32), capacity)
	})

	t.Run("rejects capacity overflow", func(t *testing.T) {
		_, err := nextJobProcessCapacity(math.MaxUint32, 0)

		assert.ErrorIs(t, err, errJobProcessCapacityOverflow)
	})

	t.Run("doubles the largest capacity that still fits", func(t *testing.T) {
		capacity, err := nextJobProcessCapacity(math.MaxUint32/2, 0)

		require.NoError(t, err)
		assert.Equal(t, uint32(math.MaxUint32-1), capacity)
	})

	t.Run("rejects the smallest capacity that overflows", func(t *testing.T) {
		_, err := nextJobProcessCapacity(math.MaxUint32/2+1, 0)

		assert.ErrorIs(t, err, errJobProcessCapacityOverflow)
	})
}

func TestProcessWaitTimeoutMilliseconds(t *testing.T) {
	assert.Equal(t, uint32(0), processWaitTimeoutMilliseconds(-time.Nanosecond))
	assert.Equal(t, uint32(0), processWaitTimeoutMilliseconds(0))
	assert.Equal(t, uint32(1), processWaitTimeoutMilliseconds(time.Nanosecond))
	assert.Equal(t, uint32(1), processWaitTimeoutMilliseconds(time.Millisecond))
	assert.Equal(t, uint32(2), processWaitTimeoutMilliseconds(time.Millisecond+time.Nanosecond))
	assert.Equal(
		t,
		processTreeTerminationTimeoutMilliseconds,
		processWaitTimeoutMilliseconds(processTreeTerminationTimeout+time.Second),
	)
}
