package fsrepository

import (
	"errors"
	"testing"
	"time"
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
	if err != nil || calls != 3 || waits != 2 {
		t.Fatalf("retry result err=%v calls=%d waits=%d, want nil/3/2", err, calls, waits)
	}

	permanent := errors.New("permanent")
	calls = 0
	waits = 0
	err = removeRepositoryUsing("workspace", func(string) error {
		calls++
		return permanent
	}, func(error) bool { return false }, func(time.Duration) { waits++ })
	if !errors.Is(err, permanent) || calls != 1 || waits != 0 {
		t.Fatalf("permanent result err=%v calls=%d waits=%d, want permanent/1/0", err, calls, waits)
	}
}
