package ooze

import (
	"testing"
	"time"
)

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

		system.stop(17, owned)

		if called {
			t.Fatal("managed stop bypassed sealed owned-attempt admission")
		}
	})

	t.Run("owned peer", func(t *testing.T) {
		var request StopRequest
		owned := newOwnedAttempt(func(observed StopRequest) { request = observed }, func() Terminal {
			return Settled{}
		})

		system.stop(18, owned)

		if !request.At.Equal(now) || !request.DrainBy.Equal(now.Add(5*time.Second)) {
			t.Fatalf("managed stop request = %#v", request)
		}
	})
}
