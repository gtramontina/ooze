package cmdtestrunner_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/oozetesting/fakerepository"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// supervisionOutcome carries what the runner did, including a panic. The runner
// panics on any supervision failure, and a panic on the goroutine driving it
// would kill the test binary before any cleanup ran, leaving every descendant
// behind. Recovering it keeps teardown reachable and turns the runner's own
// failure signal into an ordinary assertion.
type supervisionOutcome struct {
	output   result.Result[string]
	panicked any
}

// A supervised command that creates many descendants at once must still drain
// completely. Breadth had no coverage at all, and it is the dimension a
// descendant ceiling will later be measured against.
//
// The fixture verifies each descendant individually and independently of the
// supervisor's own census, and it is built so that none of the ways such a test
// can quietly become vacuous stay silent:
//
//   - Descendants must be distinct from each other and from the supervised root,
//     so a command that publishes one process's ID repeatedly cannot satisfy it.
//   - Each descendant must report, from inside itself, that it reached its own
//     code and that its parent is the supervised root. A process that was forked
//     but died during start-up never reports, so it cannot be counted as live.
//   - No descendant may record that its own timer expired. Otherwise a
//     supervisor that never contained anything would pass as soon as the
//     descendants retired themselves.
//
// Those three requirements are what make the final assertion mean containment.
// Without them this test passes against a supervisor whose termination is
// removed entirely.
func TestSupervisedDomainDrainsAWideFanout(t *testing.T) {
	if !descendantSupervisionSupported {
		t.Skip("process-tree supervision is unavailable on this operating system")
	}

	dir := t.TempDir()
	runner := newHelperCommand(t, "spawn-fanout")

	outcomes := make(chan supervisionOutcome, 1)
	go func() {
		outcome := supervisionOutcome{output: result.Err[string](""), panicked: nil}
		defer func() {
			outcome.panicked = recover()
			outcomes <- outcome
		}()
		outcome.output = runner.Test(fakerepository.NewTemporaryAt(dir))
	}()
	t.Cleanup(func() {
		_ = os.WriteFile(filepath.Join(dir, descendantObservedFile), nil, 0o600)
	})

	// Teardown covers each descendant from the moment it has been verified, so a
	// failure part-way through verification still tears down what it confirmed.
	// Descendants whose own report never arrived are left to the timer they carry
	// themselves; no path leaves anything running without a bound.
	verified := make([]int, 0, descendantFanoutBreadth)
	t.Cleanup(func() {
		for _, processID := range verified {
			terminateDescendant(t, processID)
		}
	})

	rootProcessID, published := awaitFanoutProcessIDs(t, dir)
	seen := make(map[int]bool, len(published))
	for _, processID := range published {
		require.NotEqual(t, rootProcessID, processID, "a descendant cannot be the supervised root itself")
		require.False(t, seen[processID], "descendant %d was published more than once", processID)
		seen[processID] = true

		parentProcessID := awaitDescendantParent(t, dir, processID)
		require.Equal(t, rootProcessID, parentProcessID,
			"descendant %d reported parent %d, not the supervised root", processID, parentProcessID)

		canExecute, err := descendantCanStillExecute(processID)
		require.NoError(t, err)
		require.True(t, canExecute, "descendant %d should be running before the root is released", processID)

		verified = append(verified, processID)
	}
	require.Len(t, verified, descendantFanoutBreadth)

	require.NoError(t, os.WriteFile(filepath.Join(dir, descendantObservedFile), nil, 0o600))

	select {
	case outcome := <-outcomes:
		require.Nil(t, outcome.panicked, "supervision failed: %v", outcome.panicked)
		assert.Equal(t, result.Err[string](""), outcome.output)
	case <-time.After(testProcessTimeout):
		require.FailNow(t, "Test remained blocked while supervising a wide fan-out")
	}

	// Root exit is never drainage: every descendant must be unable to execute by
	// the time the runner has returned, and none of them may have got there by
	// running out its own clock.
	for _, processID := range verified {
		canExecute, err := descendantCanStillExecute(processID)
		require.NoError(t, err)
		assert.False(t, canExecute, "descendant %d outlived its execution domain", processID)
		assert.NoFileExists(t, filepath.Join(dir, descendantRetiredPrefix+strconv.Itoa(processID)),
			"descendant %d retired itself, so this run proves nothing about containment", processID)
	}
}

// awaitFanoutProcessIDs reads the set the supervised command published: its own
// process ID first, then its descendants. A shorter list is treated as not yet
// written, because os.WriteFile creates the file before filling it and a reader
// can observe it empty or partial.
func awaitFanoutProcessIDs(t *testing.T, dir string) (int, []int) {
	t.Helper()

	deadline := time.Now().Add(testProcessTimeout)
	for {
		contents, err := os.ReadFile(filepath.Join(dir, descendantFanoutFile))
		if err == nil {
			fields := strings.Fields(string(contents))
			if len(fields) == descendantFanoutBreadth+1 {
				processIDs := make([]int, 0, len(fields))
				for _, field := range fields {
					processID, convertErr := strconv.Atoi(field)
					require.NoError(t, convertErr)
					require.Positive(t, processID)
					processIDs = append(processIDs, processID)
				}

				return processIDs[0], processIDs[1:]
			}
		}
		if time.Now().After(deadline) {
			require.FailNow(t, "Supervised command did not publish its descendant set")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// awaitDescendantParent waits for a descendant to report, from inside itself,
// that it is running, and returns the parent it named.
func awaitDescendantParent(t *testing.T, dir string, processID int) int {
	t.Helper()

	marker := filepath.Join(dir, descendantAlivePrefix+strconv.Itoa(processID))
	deadline := time.Now().Add(testProcessTimeout)
	for {
		contents, err := os.ReadFile(marker)
		if err == nil {
			parentProcessID, convertErr := strconv.Atoi(strings.TrimSpace(string(contents)))
			if convertErr == nil && parentProcessID > 0 {
				return parentProcessID
			}
		}
		if time.Now().After(deadline) {
			require.FailNow(t, fmt.Sprintf("Descendant %d never reported that it was running", processID))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
