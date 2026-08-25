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

func TestNativeManagedAttemptSystemAcceptsMatchingEmergencyEpoch(t *testing.T) {
	system := &nativeManagedAttemptSystem{emergencyResult: managedObservedEmergency{epoch: 7}}
	system.emergencyOnce.Do(func() {})

	observed := system.emergency(7)

	assert.EqualValues(t, 7, observed.epoch, "managed emergency epoch = %d, want 7", observed.epoch)
}

func TestNativeManagedAttemptSystemStopsThroughOwnedAdmission(t *testing.T) {
	now := time.Unix(707, 0)
	system := &nativeManagedAttemptSystem{driver: &supervisorDriver{
		now: func() time.Time { return now }, drainEpoch: 5 * time.Second,
	}}

	t.Run("sealed peer", func(t *testing.T) {
		called := false
		owned := newOwnedAttempt(func(StopRequest) { called = true }, func() Terminal {
			return Settled{}
		})
		owned.sealStopAdmission()

		system.stop(owned)

		assert.False(t, called, "managed stop bypassed sealed owned-attempt admission")
	})

	t.Run("owned peer", func(t *testing.T) {
		var request StopRequest
		owned := newOwnedAttempt(func(observed StopRequest) { request = observed }, func() Terminal {
			return Settled{}
		})

		system.stop(owned)

		assert.True(t, request.At.Equal(now), "managed stop request = %#v", request)
		assert.True(t, request.DrainBy.Equal(now.Add(5*time.Second)), "managed stop request = %#v", request)
	})
}
