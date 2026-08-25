package ooze

import (
	"errors"
	"testing"
	"time"
)

func TestNativeManagedAttemptSystemUsesManagedControlBounds(t *testing.T) {
	system, err := newNativeManagedAttemptSystem(newProcessRuntimeShell(1))
	if errors.Is(err, ErrUnsupportedPlatform) {
		t.Skip("native supervision is unavailable on this operating system")
	}
	if err != nil {
		t.Fatalf("construct managed attempt system: %v", err)
	}
	if system == nil || system.driver == nil {
		t.Fatal("managed attempt system returned without its native driver")
	}
	if system.driver.launchProgress != time.Second || system.driver.drainEpoch != 5*time.Second {
		t.Fatalf(
			"managed control bounds = %v/%v, want 1s/5s",
			system.driver.launchProgress,
			system.driver.drainEpoch,
		)
	}
}

func TestNativeManagedAttemptSystemAcceptsMatchingEmergencyEpoch(t *testing.T) {
	system := &nativeManagedAttemptSystem{emergencyResult: managedObservedEmergency{epoch: 7}}
	system.emergencyOnce.Do(func() {})

	observed := system.emergency(7)

	if observed.epoch != 7 {
		t.Fatalf("managed emergency epoch = %d, want 7", observed.epoch)
	}
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

		if called {
			t.Fatal("managed stop bypassed sealed owned-attempt admission")
		}
	})

	t.Run("owned peer", func(t *testing.T) {
		var request StopRequest
		owned := newOwnedAttempt(func(observed StopRequest) { request = observed }, func() Terminal {
			return Settled{}
		})

		system.stop(owned)

		if !request.At.Equal(now) || !request.DrainBy.Equal(now.Add(5*time.Second)) {
			t.Fatalf("managed stop request = %#v", request)
		}
	})
}
