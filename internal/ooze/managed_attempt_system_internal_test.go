package ooze

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeManagedAttemptSystemUsesManagedControlBounds(t *testing.T) {
	system, err := newNativeManagedAttemptSystem(newProcessRuntimeShell(1))
	if errors.Is(err, ErrUnsupportedPlatform) {
		t.Skip("native supervision is unavailable on this operating system")
	}
	require.NoError(t, err, "construct managed attempt system: %v", err)
	require.NotNil(t, system, "managed attempt system returned without its native driver")
	require.NotNil(t, system.driver, "managed attempt system returned without its native driver")
	assert.Equal(t, time.Second, system.driver.launchProgress, "managed control bounds = %v/%v, want 1s/5s", system.driver.launchProgress, system.driver.drainEpoch)
	assert.Equal(t, 5*time.Second, system.driver.drainEpoch, "managed control bounds = %v/%v, want 1s/5s", system.driver.launchProgress, system.driver.drainEpoch)
}
