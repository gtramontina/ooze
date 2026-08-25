package attempt

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)

func at(d time.Duration) time.Time { return t0.Add(d) }

func automaticSpec() Spec {
	return Spec{
		ID:       "attempt-1",
		Command:  []string{"go", "test", "./..."},
		Dir:      "/workspace",
		Env:      []string{"GOMAXPROCS=1"},
		Profile:  Automatic,
		Deadline: 20 * time.Second,
	}
}

func serialSpec() Spec {
	s := automaticSpec()
	s.Profile = Serial

	return s
}

func TestSpecContainsOnlyCallerFactsAndRequiresExecutableInput(t *testing.T) {
	err := automaticSpec().Validate()
	require.NoError(t, err)

	tests := map[string]func(*Spec){
		"identity": func(s *Spec) { s.ID = "" },
		"command":  func(s *Spec) { s.Command = nil },
		"profile":  func(s *Spec) { s.Profile = 0 },
		"deadline": func(s *Spec) { s.Deadline = 0 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			s := automaticSpec()
			mutate(&s)
			assert.ErrorIs(t, s.Validate(), ErrInvalidSpec, "invalid %s was accepted", name)
		})
	}
}

func TestUnsupportedPlatformFailsDuringConstructionBeforeLaunch(t *testing.T) {
	calls := 0
	supervisor, err := newSupervisor(false, func(AttemptID) generation {
		calls++

		return 1
	}, func(Spec, generation) LaunchResult {
		calls++

		return NotReleased{}
	}, func(EmergencyRequest) SweepResult { return SweepDrained{} })
	assert.ErrorIs(t, err, ErrUnsupportedPlatform, "unsupported construction leaked work: supervisor=%#v err=%v calls=%d", supervisor, err, calls)
	assert.Nil(t, supervisor, "unsupported construction leaked work: supervisor=%#v err=%v calls=%d", supervisor, err, calls)
	assert.EqualValues(t, 0, calls, "unsupported construction leaked work: supervisor=%#v err=%v calls=%d", supervisor, err, calls)
}

func TestConcreteContractSnapshotsCallerFactsAndWaitsExactlyOnce(t *testing.T) {
	waits := 0
	order := make([]string, 0, 2)
	stops := make([]StopRequest, 0, 1)
	owned := &OwnedAttempt{
		stop: func(request StopRequest) { stops = append(stops, request) },
		wait: func(seal func()) Terminal {
			waits++
			seal()
			return Stopped{}
		},
	}
	var admitted Spec
	supervisor, err := newSupervisor(true, func(AttemptID) generation {
		order = append(order, "register")

		return 7
	}, func(spec Spec, gen generation) LaunchResult {
		order = append(order, "native")
		assert.EqualValues(t, 7, gen, "native launch received generation %d", gen)
		admitted = spec

		return Owned{Attempt: owned, LaunchDuration: 3 * time.Millisecond}
	}, func(EmergencyRequest) SweepResult { return SweepDrained{} })
	require.NoError(t, err)

	spec := automaticSpec()
	launch := supervisor.Launch(spec).(Owned)
	spec.Command[0], spec.Env[0] = "changed", "changed"
	request := StopRequest{At: at(time.Second), DrainBy: at(2 * time.Second)}
	launch.Attempt.Stop(request)
	first, second := launch.Attempt.Wait(), launch.Attempt.Wait()

	assert.EqualValues(t, "go", admitted.Command[0], "pending launch retained mutable caller slices: %#v", admitted)
	assert.EqualValues(t, "GOMAXPROCS=1", admitted.Env[0], "pending launch retained mutable caller slices: %#v", admitted)
	assert.Equal(t, []string{"register", "native"}, order, "native launch preceded prospective registration: %v", order)
	assert.EqualValues(t, 1, waits, "wait/stop contract violated: waits=%d first=%#v second=%#v stops=%#v", waits, first, second, stops)
	assert.Equal(t, second, first, "wait/stop contract violated: waits=%d first=%#v second=%#v stops=%#v", waits, first, second, stops)
	assert.Equal(t, []StopRequest{request}, stops, "wait/stop contract violated: waits=%d first=%#v second=%#v stops=%#v", waits, first, second, stops)
}

func TestStopAndRepeatedWaitAreConcurrencySafeWithoutSchedulingAssumptions(t *testing.T) {
	waitEntered := make(chan struct{})
	releaseWait := make(chan struct{})
	results := make(chan Terminal, 2)
	stopped := make(chan StopRequest, 1)
	owned := &OwnedAttempt{
		stop: func(request StopRequest) { stopped <- request },
		wait: func(seal func()) Terminal {
			close(waitEntered)
			<-releaseWait
			seal()

			return Stopped{}
		},
	}

	go func() { results <- owned.Wait() }()
	<-waitEntered
	go func() { results <- owned.Wait() }()
	request := StopRequest{At: at(time.Second), DrainBy: at(2 * time.Second)}
	owned.Stop(request)
	close(releaseWait)

	{
		got := <-stopped
		assert.Equal(t, request, got, "stop=%#v", got)
	}
	first, second := <-results, <-results
	assert.Equal(t, second, first, "wait results differ: %#v %#v", first, second)
}

func TestStopAfterTerminalDoesNoWork(t *testing.T) {
	stops := 0
	owned := &OwnedAttempt{
		stop: func(StopRequest) { stops++ },
		wait: func(seal func()) Terminal {
			seal()
			return Settled{}
		},
	}
	_ = owned.Wait()
	owned.Stop(StopRequest{At: at(time.Second), DrainBy: at(2 * time.Second)})
	assert.EqualValues(t, 0, stops, "stop reached native work after terminal delivery: %d", stops)
}

func TestStopAdmissionClosesBeforeReleaseEvenWhenWaitHasNotReturned(t *testing.T) {
	sealed := make(chan struct{})
	returnWait := make(chan struct{})
	result := make(chan Terminal, 1)
	stops := 0
	owned := &OwnedAttempt{
		stop: func(StopRequest) { stops++ },
		wait: func(seal func()) Terminal {
			seal() // Production calls this at the release linearization point.
			close(sealed)
			<-returnWait

			return Settled{}
		},
	}
	go func() { result <- owned.Wait() }()
	<-sealed
	owned.Stop(StopRequest{At: at(time.Second), DrainBy: at(2 * time.Second)})
	close(returnWait)
	<-result
	assert.EqualValues(t, 0, stops, "stop crossed the release cut: %d", stops)
}

func TestStopAdmissionClosesBeforeUnconfirmedCustodyTransfersWithoutRelease(t *testing.T) {
	stops := 0
	releases := 0
	owned := &OwnedAttempt{
		stop: func(StopRequest) { stops++ },
		wait: func(seal func()) Terminal {
			m := deadlineMachine(t, Automatic)
			m, _ = advanceDrain(m, nil)
			m, _ = advanceDrain(m, &drainEvent{
				action: m.awaiting, kind: forceCompleted, at: m.drainBy,
			})
			_, next, terminal := acceptOutput(m, outputAt(m, "prefix", 6, nil))
			if next.kind == releaseDomain {
				releases++
			}
			assert.Equal(t, deliverTerminal, next.kind, "unconfirmed path dispatched unexpected action: %#v", next)
			seal() // Supervisor takes exclusive residual custody; no release occurs.
			return terminal
		},
	}
	terminal := owned.Wait()
	owned.Stop(StopRequest{At: at(time.Second), DrainBy: at(2 * time.Second)})
	unconfirmed, ok := terminal.(DrainUnconfirmed)
	require.True(t, ok, "unconfirmed custody cut failed: terminal=%#v stops=%d releases=%d", terminal, stops, releases)
	assert.Equal(t, OwnedUndrained, unconfirmed.Residual, "unconfirmed custody cut failed: terminal=%#v stops=%d releases=%d", terminal, stops, releases)
	assert.EqualValues(t, 0, stops, "unconfirmed custody cut failed: terminal=%#v stops=%d releases=%d", terminal, stops, releases)
	assert.EqualValues(t, 0, releases, "unconfirmed custody cut failed: terminal=%#v stops=%d releases=%d", terminal, stops, releases)
}

func TestInvalidLaunchAndStopInputCannotReachNativeWork(t *testing.T) {
	launchCalls, stopCalls := 0, 0
	supervisor := &Supervisor{
		commitStart: func(AttemptID) generation {
			launchCalls++
			return 1
		},
		launch: func(Spec, generation) LaunchResult {
			launchCalls++
			return NotReleased{}
		},
	}
	owned := &OwnedAttempt{stop: func(StopRequest) { stopCalls++ }}

	t.Run("launch", func(t *testing.T) {
		defer expectPanic(t)
		s := automaticSpec()
		s.Deadline = 0
		supervisor.Launch(s)
	})
	t.Run("stop", func(t *testing.T) {
		defer expectPanic(t)
		owned.Stop(StopRequest{At: at(time.Second), DrainBy: at(time.Second)})
	})
	assert.EqualValues(t, 0, launchCalls, "invalid input reached native work: launch=%d stop=%d", launchCalls, stopCalls)
	assert.EqualValues(t, 0, stopCalls, "invalid input reached native work: launch=%d stop=%d", launchCalls, stopCalls)
}

func TestLaunchClassificationUsesTheExactReleaseStage(t *testing.T) {
	type tuple struct {
		name      string
		platform  launchPlatform
		operation launchOperation
		code      launchCode
	}
	eligible := make([]tuple, 0, 37)
	platformName := map[launchPlatform]string{
		platformLinux:   "linux",
		platformDarwin:  "darwin",
		platformWindows: "windows",
	}
	operationName := map[launchOperation]string{
		acquireInternalDescriptor:   "acquire_internal_descriptor",
		startLauncher:               "start_launcher",
		createExitTracker:           "create_exit_tracker",
		registerExitTracker:         "register_exit_tracker",
		execTarget:                  "exec_target",
		configureWindowsContainment: "configure_windows_containment",
	}
	codeName := map[launchCode]string{
		codeEAGAIN:               "eagain",
		codeENOMEM:               "enomem",
		codeEMFILE:               "emfile",
		codeENFILE:               "enfile",
		codeWinTooManyOpenFiles:  "too_many_open_files",
		codeWinNotEnoughMemory:   "not_enough_memory",
		codeWinOutOfMemory:       "out_of_memory",
		codeWinNoProcessSlots:    "no_process_slots",
		codeWinNoSystemResources: "no_system_resources",
		codeWinCommitmentLimit:   "commitment_limit",
	}
	addEligible := func(platform launchPlatform, operation launchOperation, code launchCode) {
		eligible = append(eligible, tuple{
			name:     platformName[platform] + "_" + operationName[operation] + "_" + codeName[code],
			platform: platform, operation: operation, code: code,
		})
	}
	unixFour := []launchCode{codeEAGAIN, codeENOMEM, codeEMFILE, codeENFILE}
	for _, code := range []launchCode{codeEMFILE, codeENFILE} {
		addEligible(platformLinux, acquireInternalDescriptor, code)
		addEligible(platformDarwin, acquireInternalDescriptor, code)
	}
	for _, code := range unixFour {
		addEligible(platformLinux, startLauncher, code)
		addEligible(platformDarwin, startLauncher, code)
	}
	for _, code := range []launchCode{codeEAGAIN, codeENOMEM} {
		addEligible(platformLinux, execTarget, code)
	}
	for _, code := range []launchCode{codeENOMEM, codeEMFILE, codeENFILE} {
		addEligible(platformDarwin, createExitTracker, code)
	}
	addEligible(platformDarwin, registerExitTracker, codeENOMEM)
	addEligible(platformDarwin, execTarget, codeENOMEM)
	windowsCodes := []launchCode{
		codeWinTooManyOpenFiles, codeWinNotEnoughMemory, codeWinOutOfMemory,
		codeWinNoProcessSlots, codeWinNoSystemResources, codeWinCommitmentLimit,
	}
	for _, operation := range []launchOperation{
		acquireInternalDescriptor, startLauncher, configureWindowsContainment,
	} {
		for _, code := range windowsCodes {
			addEligible(platformWindows, operation, code)
		}
	}
	for _, test := range eligible {
		t.Run(test.name, func(t *testing.T) {
			got := classifyLaunch(nativeLaunchFailure{
				platform: test.platform, operation: test.operation, stage: preRelease,
				code: test.code, closureProven: true, duration: time.Millisecond, err: errors.New("launch"),
			}).(NotReleased)
			assert.Equal(t, LaunchResourceExhausted, got.Kind, "%#v: want resource exhaustion, got %#v", test, got)
		})
	}
	t.Run("ordinary_pre_release_failure", func(t *testing.T) {
		ordinary := classifyLaunch(nativeLaunchFailure{
			platform: platformLinux, operation: startLauncher, stage: preRelease,
			code: codeOther, closureProven: true, err: errors.New("ENOENT"),
		}).(NotReleased)
		assert.Equal(t, LaunchFailed, ordinary.Kind, "ordinary launch became %#v", ordinary)
	})
	t.Run("pre_release_without_closure_is_unconfirmed", func(t *testing.T) {
		unclosed := classifyLaunch(nativeLaunchFailure{
			platform: platformWindows, operation: configureWindowsContainment, stage: preRelease,
			code: codeWinNoSystemResources, closureProven: false, err: errors.New("cleanup unconfirmed"),
		})
		_, ok := unclosed.(LaunchUnconfirmed)
		require.True(t, ok, "unclosed suspended process became NotReleased: %#v", unclosed)
	})
	t.Run("unknown_release_is_unconfirmed", func(t *testing.T) {
		unknown := classifyLaunch(nativeLaunchFailure{
			platform: platformDarwin, operation: startLauncher, stage: unknownRelease,
			code: codeEMFILE, err: errors.New("unknown"),
		})
		_, ok := unknown.(LaunchUnconfirmed)
		require.True(t, ok, "unknown release manufactured closure: %#v", unknown)
	})
	t.Run("post_release_is_owned", func(t *testing.T) {
		owned := &OwnedAttempt{wait: func(seal func()) Terminal {
			seal()
			return Infrastructure{Cause: ReleaseFailed}
		}}
		post := classifyLaunch(nativeLaunchFailure{
			platform: platformDarwin, operation: startLauncher, stage: postRelease,
			code: codeEMFILE, err: errors.New("after release"), owned: owned,
		}).(Owned)
		assert.Equal(t, owned, post.Attempt, "post-release target was not adopted: %#v", post)
	})
}

func TestLaunchResourceWhitelistsAreFalseNegativeBiasedAndOperationScoped(t *testing.T) {
	for _, test := range []struct {
		name      string
		platform  launchPlatform
		operation launchOperation
		code      launchCode
	}{
		{"darwin_register_tracker_emfile", platformDarwin, registerExitTracker, codeEMFILE},
		{"darwin_exec_eagain", platformDarwin, execTarget, codeEAGAIN},
		{"windows_exec_no_system_resources", platformWindows, execTarget, codeWinNoSystemResources},
		{"windows_launcher_other", platformWindows, startLauncher, codeOther},
		{"linux_internal_descriptor_enomem", platformLinux, acquireInternalDescriptor, codeENOMEM},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := classifyLaunch(nativeLaunchFailure{
				platform: test.platform, operation: test.operation, stage: preRelease,
				code: test.code, closureProven: true, err: errors.New("excluded"),
			}).(NotReleased)
			assert.Equal(t, LaunchFailed, got.Kind, "excluded tuple became resource pressure: %#v -> %#v", test, got)
		})
	}
}

func TestLaunchCompletionAlreadyObservableAtTheBoundaryWins(t *testing.T) {
	by := at(5 * time.Second)
	for _, test := range []struct {
		name   string
		kind   launchCompletionKind
		at     time.Time
		action pendingAction
		state  pendingState
	}{
		{"not released before", completedNotReleased, by.Add(-time.Nanosecond), returnNotReleased, closedNotReleased},
		{"released before", completedReleased, by.Add(-time.Nanosecond), returnOwned, adoptedOwned},
		{"not released at equality", completedNotReleased, by, returnNotReleased, closedNotReleased},
		{"released at equality", completedReleased, by, returnOwned, adoptedOwned},
	} {
		t.Run(test.name, func(t *testing.T) {
			completion := launchCompletion{kind: test.kind, at: test.at}
			event := pendingEvent{generation: 1, kind: launchCompleted, at: test.at, completion: &completion}
			if test.at.Equal(by) {
				event = pendingEvent{generation: 1, kind: launchBoundary, at: by, completion: &completion}
			}
			got, action := advancePending(beginPending(1, by), event)
			assert.Equal(t, test.action, action, "completion lost boundary: state=%#v action=%d", got, action)
			assert.Equal(t, test.state, got.state, "completion lost boundary: state=%#v action=%d", got, action)
		})
	}
}

func TestLaunchBoundaryWithoutObservableCompletionLatchesAndRetainsCustody(t *testing.T) {
	by := at(5 * time.Second)
	pending, action := advancePending(beginPending(1, by), pendingEvent{generation: 1, kind: launchBoundary, at: by})
	assert.Equal(t, reportLaunchUnconfirmed, action, "launch boundary lost custody: %#v action=%d", pending, action)
	assert.Equal(t, reportedUnconfirmed, pending.state, "launch boundary lost custody: %#v action=%d", pending, action)
	completion := launchCompletion{kind: completedReleased, at: by}
	pending, action = advancePending(pending, pendingEvent{
		generation: 1, kind: launchCompleted, at: completion.at, completion: &completion,
	})
	assert.Equal(t, adoptAndForceDrain, action, "same-time completion rewrote or escaped custody: %#v action=%d", pending, action)
	assert.Equal(t, adoptedOwned, pending.state, "same-time completion rewrote or escaped custody: %#v action=%d", pending, action)

	notReleased, _ := advancePending(beginPending(2, by), pendingEvent{generation: 2, kind: launchBoundary, at: by})
	closed := launchCompletion{kind: completedNotReleased, at: by}
	notReleased, action = advancePending(notReleased, pendingEvent{
		generation: 2, kind: launchCompleted, at: closed.at, completion: &closed,
	})
	assert.Equal(t, closeProspective, action, "late closure rewrote or retained custody: %#v action=%d", notReleased, action)
	assert.Equal(t, closedNotReleased, notReleased.state, "late closure rewrote or retained custody: %#v action=%d", notReleased, action)
}

func TestLaunchBoundaryRevokesReleaseOfAControllableUnreleasedIdentity(t *testing.T) {
	by := at(5 * time.Second)
	pending, action := advancePending(beginPending(1, by), pendingEvent{
		generation: 1,
		kind:       nativeIdentityAcquired,
		at:         by.Add(-time.Nanosecond),
	})
	assert.Equal(t, continueLaunchEstablishment, action, "native identity was not retained: %#v action=%d", pending, action)
	assert.True(t, pending.nativeHeld, "native identity was not retained: %#v action=%d", pending, action)
	pending, action = advancePending(pending, pendingEvent{generation: 1, kind: launchBoundary, at: by})
	assert.Equal(t, reportUnconfirmedAndCloseUnreleased, action, "boundary permitted later target release: %#v action=%d", pending, action)
	assert.Equal(t, reportedUnconfirmed, pending.state, "boundary permitted later target release: %#v action=%d", pending, action)
	t.Run("revoked controllable identity cannot later release", func(t *testing.T) {
		defer expectPanic(t)
		released := launchCompletion{kind: completedReleased, at: by.Add(time.Nanosecond)}
		advancePending(pending, pendingEvent{
			generation: 1, kind: launchCompleted, at: released.at, completion: &released,
		})
	})

	late, _ := advancePending(beginPending(2, by), pendingEvent{generation: 2, kind: launchBoundary, at: by})
	late, action = advancePending(late, pendingEvent{
		generation: 2,
		kind:       nativeIdentityAcquired,
		at:         by.Add(time.Nanosecond),
	})
	assert.Equal(t, closeUnreleasedIdentity, action, "late controllable identity was not closed before release: %#v action=%d", late, action)
	assert.True(t, late.nativeHeld, "late controllable identity was not closed before release: %#v action=%d", late, action)

	emergency, _ := advancePending(beginPending(4, by), pendingEvent{
		generation: 4, kind: nativeIdentityAcquired, at: by.Add(-2 * time.Second),
	})
	emergency, action = advancePending(emergency, pendingEvent{
		generation: 4, kind: launchReleaseRevoked, at: by.Add(-time.Second),
	})
	assert.Equal(t, reportUnconfirmedAndCloseUnreleased, action, "emergency closure did not revoke target release: %#v action=%d", emergency, action)
	assert.True(t, emergency.releaseDenied, "emergency closure did not revoke target release: %#v action=%d", emergency, action)
	closed := launchCompletion{kind: completedNotReleased, at: by.Add(-time.Nanosecond)}
	emergency, action = advancePending(emergency, pendingEvent{
		generation: 4, kind: launchCompleted, at: closed.at, completion: &closed,
	})
	assert.Equal(t, closeProspective, action, "emergency could not close before LaunchBy: %#v action=%d", emergency, action)
	assert.Equal(t, closedNotReleased, emergency.state, "emergency could not close before LaunchBy: %#v action=%d", emergency, action)

	withoutIdentity, _ := advancePending(beginPending(5, by), pendingEvent{
		generation: 5, kind: launchReleaseRevoked, at: by.Add(-2 * time.Second),
	})
	withoutIdentity, action = advancePending(withoutIdentity, pendingEvent{
		generation: 5, kind: nativeIdentityAcquired, at: by.Add(-time.Second),
	})
	assert.Equal(t, closeUnreleasedIdentity, action, "post-emergency identity was not closed: %#v action=%d", withoutIdentity, action)
	assert.True(t, withoutIdentity.releaseDenied, "post-emergency identity was not closed: %#v action=%d", withoutIdentity, action)

	published := launchCompletion{kind: completedNotReleased, at: by.Add(-3 * time.Second)}
	preclosed, action := advancePending(beginPending(6, by), pendingEvent{
		generation: 6, kind: launchReleaseRevoked, at: by.Add(-2 * time.Second), completion: &published,
	})
	assert.Equal(t, returnNotReleased, action, "emergency ignored its completion snapshot: %#v action=%d", preclosed, action)
	assert.Equal(t, closedNotReleased, preclosed.state, "emergency ignored its completion snapshot: %#v action=%d", preclosed, action)
	releasedSnapshot := launchCompletion{kind: completedReleased, at: by.Add(-3 * time.Second)}
	preowned, action := advancePending(beginPending(8, by), pendingEvent{
		generation: 8, kind: launchReleaseRevoked, at: by.Add(-2 * time.Second), completion: &releasedSnapshot,
	})
	assert.Equal(t, returnOwnedAndForceDrain, action, "emergency failed to return and drain its owned snapshot: %#v action=%d", preowned, action)
	assert.Equal(t, adoptedOwned, preowned.state, "emergency failed to return and drain its owned snapshot: %#v action=%d", preowned, action)

	t.Run("explicit launch events cannot move backward", func(t *testing.T) {
		defer expectPanic(t)
		started, _ := advancePending(beginPending(7, by), pendingEvent{
			generation: 7, kind: nativeIdentityAcquired, at: by.Add(-time.Second),
		})
		advancePending(started, pendingEvent{
			generation: 7, kind: launchReleaseRevoked, at: by.Add(-2 * time.Second),
		})
	})

	t.Run("late delivery cannot move behind the boundary", func(t *testing.T) {
		defer expectPanic(t)
		unconfirmed, _ := advancePending(beginPending(3, by), pendingEvent{generation: 3, kind: launchBoundary, at: by})
		advancePending(unconfirmed, pendingEvent{
			generation: 3,
			kind:       nativeIdentityAcquired,
			at:         by.Add(-time.Nanosecond),
		})
	})
}

func TestLaunchBoundarySnapshotIsIndependentOfNotificationAndTimerDelivery(t *testing.T) {
	by := at(5 * time.Second)
	completion := launchCompletion{kind: completedNotReleased, at: by.Add(-time.Nanosecond)}
	pending, action := advancePending(beginPending(1, by), pendingEvent{
		generation: 1, kind: launchBoundary, at: by, completion: &completion,
	})
	assert.Equal(t, returnNotReleased, action, "delayed notification hid published completion: %#v action=%d", pending, action)
	assert.Equal(t, closedNotReleased, pending.state, "delayed notification hid published completion: %#v action=%d", pending, action)

	t.Run("late timer cannot extend bound", func(t *testing.T) {
		defer expectPanic(t)
		late := launchCompletion{kind: completedReleased, at: by.Add(time.Nanosecond)}
		advancePending(beginPending(2, by), pendingEvent{generation: 2, kind: launchBoundary, at: by, completion: &late})
	})
	t.Run("duplicate boundary", func(t *testing.T) {
		defer expectPanic(t)
		unconfirmed, _ := advancePending(beginPending(3, by), pendingEvent{generation: 3, kind: launchBoundary, at: by})
		advancePending(unconfirmed, pendingEvent{generation: 3, kind: launchBoundary, at: by})
	})
	t.Run("post-bound completion cannot skip boundary", func(t *testing.T) {
		defer expectPanic(t)
		late := launchCompletion{kind: completedReleased, at: by.Add(time.Nanosecond)}
		advancePending(beginPending(4, by), pendingEvent{
			generation: 4, kind: launchCompleted, at: late.at, completion: &late,
		})
	})
	t.Run("stale generation", func(t *testing.T) {
		defer expectPanic(t)
		advancePending(beginPending(5, by), pendingEvent{generation: 4, kind: launchBoundary, at: by})
	})
}

func TestPrivateGenerationRejectsAStaleFactWhenPublicIDIsReused(t *testing.T) {
	spec := automaticSpec()
	stale := terminalFact{generation: 1, kind: factExit, at: at(time.Second), exit: ExitStatus{}}
	{
		got, ok := chooseIntent(spec, 2, t0, at(2*time.Second), []terminalFact{stale})
		assert.False(t, ok, "reused public ID accepted old generation: %#v", got)
	}
	fresh := stale
	fresh.generation = 2
	{
		got, ok := chooseIntent(spec, 2, t0, at(2*time.Second), []terminalFact{fresh})
		assert.True(t, ok, "fresh generation was rejected: %#v", got)
		assert.Equal(t, factExit, got.kind, "fresh generation was rejected: %#v", got)
	}
}

func TestEarliestValidTerminalFactWinsRegardlessOfTiePriority(t *testing.T) {
	tests := []struct {
		name  string
		facts []terminalFact
		want  factKind
	}{
		{"exit before fuse", []terminalFact{
			{generation: 1, kind: factFuse, at: at(2 * time.Second), rootLive: true, live: 90},
			{generation: 1, kind: factExit, at: at(time.Second)},
		}, factExit},
		{"fuse before exit", []terminalFact{
			{generation: 1, kind: factExit, at: at(2 * time.Second)},
			{generation: 1, kind: factFuse, at: at(time.Second), rootLive: true, live: 90},
		}, factFuse},
		{"stop before deadline", []terminalFact{{
			generation: 1, kind: factStop, at: at(19 * time.Second),
			stop: StopRequest{At: at(19 * time.Second), DrainBy: at(30 * time.Second)},
		}}, factStop},
		{"deadline before stop", []terminalFact{{
			generation: 1, kind: factStop, at: at(21 * time.Second),
			stop: StopRequest{At: at(21 * time.Second), DrainBy: at(30 * time.Second)},
		}}, factDeadline},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := chooseIntent(automaticSpec(), 1, t0, at(25*time.Second), test.facts)
			assert.True(t, ok, "want %d, got %#v", test.want, got)
			assert.Equal(t, test.want, got.kind, "want %d, got %#v", test.want, got)
		})
	}
}

func TestPriorityAppliesOnlyAtTheExactSameInstant(t *testing.T) {
	tie := at(20 * time.Second)
	all := []terminalFact{
		{generation: 1, kind: factStop, at: tie, stop: StopRequest{At: tie, DrainBy: at(21 * time.Second)}},
		{generation: 1, kind: factSupervisionFailure, at: tie, cause: CensusFailed, err: errors.New("census")},
		{generation: 1, kind: factExit, at: tie},
		{generation: 1, kind: factFuse, at: tie, rootLive: true, live: 65},
	}
	tests := []struct {
		name  string
		facts []terminalFact
		want  factKind
	}{
		{"fuse", all, factFuse},
		{"exit", all[:3], factExit},
		{"fault", all[:2], factSupervisionFailure},
		{"deadline", all[:1], factDeadline},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := chooseIntent(automaticSpec(), 1, t0, tie, test.facts)
			assert.True(t, ok, "tie lost cause or cleanup constraint: %#v", got)
			assert.Equal(t, test.want, got.kind, "tie lost cause or cleanup constraint: %#v", got)
			assert.True(t, got.stop.DrainBy.Equal(at(21*time.Second)), "tie lost cause or cleanup constraint: %#v", got)
		})
	}
}

func TestSameInstantFactsAreCanonicalOrRejected(t *testing.T) {
	tie := at(time.Second)
	t.Run("fuse_uses_highest_live_count", func(t *testing.T) {
		fuse, _ := chooseIntent(automaticSpec(), 1, t0, tie, []terminalFact{
			{generation: 1, kind: factFuse, at: tie, rootLive: true, live: 70},
			{generation: 1, kind: factFuse, at: tie, rootLive: true, live: 90},
		})
		assert.EqualValues(t, 90, fuse.live, "same-time fuse depends on delivery order: %#v", fuse)
	})
	t.Run("stop_uses_earliest_drain_bound", func(t *testing.T) {
		stop, _ := chooseIntent(automaticSpec(), 1, t0, tie, []terminalFact{
			{generation: 1, kind: factStop, at: tie, stop: StopRequest{At: tie, DrainBy: at(9 * time.Second)}},
			{generation: 1, kind: factStop, at: tie, stop: StopRequest{At: tie, DrainBy: at(8 * time.Second)}},
		})
		assert.True(t, stop.stop.DrainBy.Equal(at(8*time.Second)), "same-time stop depends on delivery order: %#v", stop)
	})
	t.Run("duplicate_exit_is_rejected", func(t *testing.T) {
		defer expectPanic(t)
		chooseIntent(automaticSpec(), 1, t0, tie, []terminalFact{
			{generation: 1, kind: factExit, at: tie},
			{generation: 1, kind: factExit, at: tie},
		})
	})
}

func TestFuseSampleRequiresMatchingGenerationAndLiveRoot(t *testing.T) {
	for _, test := range []struct {
		name string
		fact terminalFact
	}{
		{"stale_generation", terminalFact{generation: 2, kind: factFuse, at: at(time.Second), rootLive: true, live: 100}},
		{"root_not_live", terminalFact{generation: 1, kind: factFuse, at: at(time.Second), rootLive: false, live: 100}},
		{"below_threshold", terminalFact{generation: 1, kind: factFuse, at: at(time.Second), rootLive: true, live: 64}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := chooseIntent(automaticSpec(), 1, t0, at(2*time.Second), []terminalFact{test.fact})
			assert.False(t, ok, "invalid fuse selected: %#v", got)
		})
	}
	t.Run("serial_profile_ignores_fuse", func(t *testing.T) {
		got, ok := chooseIntent(serialSpec(), 1, t0, at(2*time.Second), []terminalFact{{
			generation: 1, kind: factFuse, at: at(time.Second), rootLive: true, live: 100,
		}})
		assert.False(t, ok, "serial attempt selected fuse: %#v", got)
	})
}

func TestDeadlineIsInclusiveAndAnObservableExitWinsTheTie(t *testing.T) {
	t.Run("before_deadline", func(t *testing.T) {
		_, ok := chooseIntent(automaticSpec(), 1, t0, at(20*time.Second-time.Nanosecond), nil)
		assert.False(t, ok, "deadline fired early")
	})
	t.Run("at_deadline", func(t *testing.T) {
		got, _ := chooseIntent(automaticSpec(), 1, t0, at(20*time.Second), nil)
		assert.Equal(t, factDeadline, got.kind, "deadline missed equality: %#v", got)
	})
	t.Run("observable_exit_wins_equality", func(t *testing.T) {
		got, _ := chooseIntent(automaticSpec(), 1, t0, at(20*time.Second), []terminalFact{{
			generation: 1, kind: factExit, at: at(20 * time.Second), exit: ExitStatus{Code: 1},
		}})
		assert.Equal(t, factExit, got.kind, "observable exit lost tie: %#v", got)
	})
}

func TestNativeFactUsesItsPostOperationInstant(t *testing.T) {
	lateFuse := terminalFact{
		generation: 1, kind: factFuse, at: at(21 * time.Second), rootLive: true, live: 100,
	}
	got, _ := chooseIntent(automaticSpec(), 1, t0, at(21*time.Second), []terminalFact{lateFuse})
	assert.Equal(t, factDeadline, got.kind, "late census inherited an earlier instant: %#v", got)
}

func TestAutomaticDeadlineNeverFabricatesAndRetainsARealRunningSample(t *testing.T) {
	m, _ := begin(automaticSpec(), 1, t0, 4*time.Millisecond)
	facts := []terminalFact{
		{generation: 1, kind: factFuse, at: at(time.Second), rootLive: true, live: 3},
		{generation: 1, kind: factFuse, at: at(21 * time.Second), rootLive: true, live: 200},
	}
	m = observeRunning(m, at(21*time.Second), at(25*time.Second), facts)
	assert.Equal(t, factDeadline, m.intent.kind, "post-intent sample changed timeout evidence: %#v", m)
	assert.EqualValues(t, 3, m.intent.peak, "post-intent sample changed timeout evidence: %#v", m)
	original := *m.intent
	m = observeRunning(m, at(30*time.Second), at(35*time.Second), []terminalFact{{
		generation: 1, kind: factExit, at: at(22 * time.Second),
	}})
	assert.Equal(t, original, *m.intent, "late exit rewrote intent: before=%#v after=%#v", original, *m.intent)

	noSample, _ := begin(automaticSpec(), 1, t0, 0)
	noSample = observeRunning(noSample, at(20*time.Second), at(25*time.Second), nil)
	terminal := finishAlreadyDrained(t, noSample, "").(Tripped)
	assert.False(t, terminal.Trip.(AutomaticDeadlineTrip).Peak.Present, "missing census became a measured zero: %#v", terminal)
}

func TestTimeoutForcesFirstAndCarriesOneAbsoluteDeadline(t *testing.T) {
	m := deadlineMachine(t, Automatic)
	m, first := advanceDrain(m, nil)
	assert.Equal(t, forceDomain, first.kind, "first action=%#v", first)
	assert.True(t, first.by.Equal(m.drainBy), "first action=%#v", first)
	m, second := advanceDrain(m, &drainEvent{action: m.awaiting, kind: forceCompleted, at: at(21 * time.Second)})
	assert.Equal(t, observeDomain, second.kind, "drain reset its deadline: first=%#v second=%#v", first, second)
	assert.True(t, second.by.Equal(first.by), "drain reset its deadline: first=%#v second=%#v", first, second)
}

func TestNaturalExitObservesBeforeForcingAndSkipsForceWhenEmpty(t *testing.T) {
	m := exitMachine(t)
	m, first := advanceDrain(m, nil)
	assert.Equal(t, observeDomain, first.kind, "clean exit forced before census: action=%#v state=%#v", first, m)
	assert.False(t, m.forced, "clean exit forced before census: action=%#v state=%#v", first, m)
	m, next := advanceDrain(m, &drainEvent{action: m.awaiting, kind: domainObserved, at: at(2 * time.Second), empty: true})
	assert.Equal(t, captureOutput, next.kind, "empty exit domain was forced: action=%#v state=%#v", next, m)
	assert.True(t, m.drained, "empty exit domain was forced: action=%#v state=%#v", next, m)
	assert.False(t, m.forced, "empty exit domain was forced: action=%#v state=%#v", next, m)
}

func TestNaturalExitWithResidualForcesEvenWhenObservationReachesDeadline(t *testing.T) {
	m := exitMachine(t)
	m.drainBy = at(2 * time.Second)
	m, _ = advanceDrain(m, nil)
	m, force := advanceDrain(m, &drainEvent{action: m.awaiting, kind: domainObserved, at: m.drainBy, empty: false})
	assert.Equal(t, forceDomain, force.kind, "expired residual escaped without force: action=%#v state=%#v", force, m)
	assert.True(t, m.forced, "expired residual escaped without force: action=%#v state=%#v", force, m)
	m, capture := advanceDrain(m, &drainEvent{action: m.awaiting, kind: forceCompleted, at: at(3 * time.Second)})
	assert.Equal(t, captureOutput, capture.kind, "forced expired residual did not remain unconfirmed: action=%#v state=%#v", capture, m)
	assert.True(t, m.unconfirmed, "forced expired residual did not remain unconfirmed: action=%#v state=%#v", capture, m)
}

func TestEmergencyClampCannotBeLostDuringTheFirstAction(t *testing.T) {
	m := deadlineMachine(t, Automatic)
	clamp := at(22 * time.Second)
	m = shortenDrain(m, clamp)
	original := *m.intent
	m, first := advanceDrain(m, nil)
	assert.Equal(t, forceDomain, first.kind, "clamp rewrote intent or missed first action: %#v %#v", m, first)
	assert.True(t, first.by.Equal(clamp), "clamp rewrote intent or missed first action: %#v %#v", m, first)
	assert.Equal(t, original, *m.intent, "clamp rewrote intent or missed first action: %#v %#v", m, first)
}

func TestExpiryAtEqualityCapturesOutputThenDeliversUnconfirmedWithoutRelease(t *testing.T) {
	m := deadlineMachine(t, Automatic)
	m, _ = advanceDrain(m, nil)
	m, _ = advanceDrain(m, &drainEvent{action: m.awaiting, kind: forceCompleted, at: at(21 * time.Second)})
	m, capture := advanceDrain(m, &drainEvent{
		action: m.awaiting,
		kind:   domainObserved, at: m.drainBy, empty: true,
	})
	assert.Equal(t, captureOutput, capture.kind, "deadline manufactured timely drainage: action=%#v state=%#v", capture, m)
	assert.True(t, m.unconfirmed, "deadline manufactured timely drainage: action=%#v state=%#v", capture, m)
	assert.False(t, m.drained, "deadline manufactured timely drainage: action=%#v state=%#v", capture, m)
	_, next, terminal := acceptOutput(m, outputAt(m, "partial mutant output", 40, errors.New("partial read")))
	unconfirmed, ok := terminal.(DrainUnconfirmed)
	require.True(t, ok, "unconfirmed path lost output or attempted release: action=%#v terminal=%#v", next, terminal)
	assert.Equal(t, deliverTerminal, next.kind, "unconfirmed path lost output or attempted release: action=%#v terminal=%#v", next, terminal)
	assert.EqualValues(t, "partial mutant output", unconfirmed.Output.Bytes, "unconfirmed path lost output or attempted release: action=%#v terminal=%#v", next, terminal)
	assert.False(t, unconfirmed.Output.CompleteThroughCutoff, "unconfirmed path lost output or attempted release: action=%#v terminal=%#v", next, terminal)
	assert.False(t, unconfirmed.Output.Final, "unconfirmed path lost output or attempted release: action=%#v terminal=%#v", next, terminal)
	assert.NotEqual(t, "", unconfirmed.Failures.Output, "unconfirmed path lost output or attempted release: action=%#v terminal=%#v", next, terminal)
	assert.Equal(t, CommandDeadlineFired, unconfirmed.BoundFired, "unconfirmed path lost output or attempted release: action=%#v terminal=%#v", next, terminal)
}

func TestDrainedPathCapturesOutputBeforeReleaseAndThenDelivers(t *testing.T) {
	m := deadlineMachine(t, Automatic)
	m, _ = advanceDrain(m, nil)
	m, _ = advanceDrain(m, &drainEvent{action: m.awaiting, kind: forceCompleted, at: at(21 * time.Second)})
	m, capture := advanceDrain(m, &drainEvent{action: m.awaiting, kind: domainObserved, at: at(22 * time.Second), empty: true})
	assert.Equal(t, captureOutput, capture.kind, "want capture, got %#v", capture)
	m, release, terminal := acceptOutput(m, outputAt(m, "captured", 8, nil))
	assert.Equal(t, releaseDomain, release.kind, "output skipped release phase: action=%#v terminal=%#v", release, terminal)
	assert.Nil(t, terminal, "output skipped release phase: action=%#v terminal=%#v", release, terminal)
	_, deliver, terminal := acceptRelease(m, m.awaiting, nil)
	assert.Equal(t, deliverTerminal, deliver.kind, "release did not deliver: action=%#v terminal=%#v", deliver, terminal)
	assert.NotNil(t, terminal, "release did not deliver: action=%#v terminal=%#v", deliver, terminal)
}

func TestNativeDrainCompletionsMustMatchGenerationStepAndKind(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func()
	}{
		{"skip force", func() {
			m := deadlineMachine(t, Automatic)
			m, force := advanceDrain(m, nil)
			advanceDrain(m, &drainEvent{action: force, kind: domainObserved, at: at(21 * time.Second), empty: true})
		}},
		{"wrong generation", func() {
			m := deadlineMachine(t, Automatic)
			m, force := advanceDrain(m, nil)
			force.generation++
			advanceDrain(m, &drainEvent{action: force, kind: forceCompleted, at: at(21 * time.Second)})
		}},
		{"stale step", func() {
			m := deadlineMachine(t, Automatic)
			m, force := advanceDrain(m, nil)
			m, _ = advanceDrain(m, &drainEvent{action: force, kind: forceCompleted, at: at(21 * time.Second)})
			advanceDrain(m, &drainEvent{action: force, kind: domainObserved, at: at(22 * time.Second), empty: true})
		}},
		{"wrong output token", func() {
			m := drainedBeforeOutput(t)
			wrong := m.awaiting
			wrong.sequence++
			acceptOutput(m, outputObservation{action: wrong})
		}},
		{"wrong release token", func() {
			m := drainedBeforeOutput(t)
			m, _, _ = acceptOutput(m, outputAt(m, "", 0, nil))
			wrong := m.awaiting
			wrong.generation++
			acceptRelease(m, wrong, nil)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer expectPanic(t)
			test.run()
		})
	}
}

func TestAuthoritativeDrainProducesTheLatchedTerminalVariant(t *testing.T) {
	tests := []struct {
		name  string
		spec  Spec
		facts []terminalFact
		want  any
	}{
		{"settled", automaticSpec(), []terminalFact{{generation: 1, kind: factExit, at: at(time.Second), exit: ExitStatus{Code: 1}}}, Settled{}},
		{"runaway", automaticSpec(), []terminalFact{{generation: 1, kind: factFuse, at: at(time.Second), rootLive: true, live: 70}}, Tripped{}},
		{"stopped", automaticSpec(), []terminalFact{{
			generation: 1, kind: factStop, at: at(time.Second),
			stop: StopRequest{At: at(time.Second), DrainBy: at(5 * time.Second)},
		}}, Stopped{}},
		{"census fault", automaticSpec(), []terminalFact{{generation: 1, kind: factSupervisionFailure, at: at(time.Second), cause: CensusFailed, err: errors.New("sysctl")}}, Infrastructure{}},
		{"wait fault", serialSpec(), []terminalFact{{generation: 1, kind: factSupervisionFailure, at: at(time.Second), cause: WaitFailed, err: errors.New("wait")}}, Infrastructure{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := finishDrained(t, test.spec, test.facts, "captured")
			assert.Equal(t, reflect.TypeOf(test.want), reflect.TypeOf(got), "want %T, got %T (%#v)", test.want, got, got)
		})
	}
}

func TestTripCountExposureIsVariantSpecific(t *testing.T) {
	t.Run("fuse_trip_exposes_live", func(t *testing.T) {
		runaway := finishDrained(t, automaticSpec(), []terminalFact{{
			generation: 1, kind: factFuse, at: at(time.Second), rootLive: true, live: 70,
		}}, "").(Tripped)
		assert.EqualValues(t, 70, runaway.Trip.(FuseTrip).Live, "runaway=%#v", runaway)
	})
	t.Run("automatic_deadline_exposes_peak", func(t *testing.T) {
		autoTimeout := finishDrained(t, automaticSpec(), []terminalFact{{
			generation: 1, kind: factFuse, at: at(time.Second), rootLive: true, live: 7,
		}}, "").(Tripped)
		peak := autoTimeout.Trip.(AutomaticDeadlineTrip).Peak
		assert.True(t, peak.Present, "timeout=%#v", autoTimeout)
		assert.EqualValues(t, 7, peak.Value, "timeout=%#v", autoTimeout)
	})
	t.Run("serial_deadline_has_no_count", func(t *testing.T) {
		_, ok := finishDrained(t, serialSpec(), nil, "").(Tripped).Trip.(SerialDeadlineTrip)
		require.True(t, ok, "serial timeout grew count evidence")
	})
}

func TestTerminationControlFailureNeedsDrainageProofForInfrastructure(t *testing.T) {
	controlErr := errors.New("TerminateJobObject")
	m := deadlineMachine(t, Automatic)
	m, _ = advanceDrain(m, nil)
	m, _ = advanceDrain(m, &drainEvent{action: m.awaiting, kind: forceCompleted, at: at(21 * time.Second), controlErr: controlErr})
	m, _ = advanceDrain(m, &drainEvent{action: m.awaiting, kind: domainObserved, at: at(22 * time.Second), empty: true})
	m, _, _ = acceptOutput(m, outputAt(m, "", 0, nil))
	_, _, terminal := acceptRelease(m, m.awaiting, nil)
	infra, ok := terminal.(Infrastructure)
	require.True(t, ok, "want termination infrastructure, got %#v", terminal)
	assert.Equal(t, TerminationControlFailed, infra.Cause, "want termination infrastructure, got %#v", terminal)
	assert.ErrorIs(t, infra.Err, controlErr, "want termination infrastructure, got %#v", terminal)
	assert.Equal(t, CommandDeadlineFired, infra.BoundFired, "infrastructure overlay lost the fired command bound: %#v", infra)

	m = deadlineMachine(t, Automatic)
	m, _ = advanceDrain(m, nil)
	m, _ = advanceDrain(m, &drainEvent{action: m.awaiting, kind: forceCompleted, at: m.drainBy, controlErr: controlErr})
	_, _, terminal = acceptOutput(m, outputAt(m, "", 0, nil))
	unconfirmed, ok := terminal.(DrainUnconfirmed)
	require.True(t, ok, "control failure manufactured emptiness: %#v", terminal)
	assert.Equal(t, CommandDeadlineFired, unconfirmed.BoundFired, "unconfirmed overlay lost the fired command bound: %#v", unconfirmed)
}

func TestGroupESRCHAndLatestCensusFailureCannotManufactureEmptiness(t *testing.T) {
	m := deadlineMachine(t, Automatic)
	m, _ = advanceDrain(m, nil)
	m, _ = advanceDrain(m, &drainEvent{
		action: m.awaiting,
		kind:   forceCompleted, at: at(21 * time.Second), controlErr: errors.New("kill(-pgid): ESRCH"),
	})
	m, _ = advanceDrain(m, &drainEvent{
		action: m.awaiting,
		kind:   domainObserved, at: at(22 * time.Second), err: errors.New("domain unobservable"),
	})
	m, _ = advanceDrain(m, &drainEvent{action: m.awaiting, kind: domainObserved, at: m.drainBy, empty: false})
	_, _, terminal := acceptOutput(m, outputAt(m, "partial", 20, errors.New("read output")))
	unconfirmed := terminal.(DrainUnconfirmed)
	assert.NotEqual(t, "", unconfirmed.Failures.Termination, "useful diagnoses were lost: %#v", unconfirmed.Failures)
	assert.NotEqual(t, "", unconfirmed.Failures.DrainCensus, "useful diagnoses were lost: %#v", unconfirmed.Failures)
	assert.NotEqual(t, "", unconfirmed.Failures.Output, "useful diagnoses were lost: %#v", unconfirmed.Failures)
}

func TestSimultaneousWaitAndCensusFailuresAreStableAndRetained(t *testing.T) {
	facts := []terminalFact{
		{generation: 1, kind: factSupervisionFailure, at: at(time.Second), cause: CensusFailed, err: errors.New("running census")},
		{generation: 1, kind: factSupervisionFailure, at: at(time.Second), cause: WaitFailed, err: errors.New("root wait")},
	}
	terminal := finishDrained(t, automaticSpec(), facts, "").(Infrastructure)
	assert.Equal(t, WaitFailed, terminal.Cause, "simultaneous supervision failures were unstable or lost: %#v", terminal)
	assert.NotEqual(t, "", terminal.Failures.Wait, "simultaneous supervision failures were unstable or lost: %#v", terminal)
	assert.NotEqual(t, "", terminal.Failures.RunningCensus, "simultaneous supervision failures were unstable or lost: %#v", terminal)

	m, err := begin(automaticSpec(), 1, t0, 0)
	require.NoError(t, err)
	m = observeRunning(m, at(time.Second), at(5*time.Second), facts[1:])
	m, _ = advanceDrain(m, nil)
	m, _ = advanceDrain(m, &drainEvent{action: m.awaiting, kind: forceCompleted, at: m.drainBy})
	_, _, got := acceptOutput(m, outputAt(m, "", 0, nil))
	unconfirmed := got.(DrainUnconfirmed)
	assert.NotEqual(t, "", unconfirmed.Failures.Wait, "unconfirmed overlay lost initiating wait failure: %#v", unconfirmed)
}

func TestDuplicateSupervisionAxisIsRejectedEvenUnderAHigherPriorityTie(t *testing.T) {
	defer expectPanic(t)
	tie := at(time.Second)
	chooseIntent(automaticSpec(), 1, t0, tie, []terminalFact{
		{generation: 1, kind: factFuse, at: tie, rootLive: true, live: 70},
		{generation: 1, kind: factSupervisionFailure, at: tie, cause: WaitFailed, err: errors.New("first")},
		{generation: 1, kind: factSupervisionFailure, at: tie, cause: WaitFailed, err: errors.New("second")},
	})
}

func TestOutputIsImmutableAndCommandDurationEndsAtIntent(t *testing.T) {
	bytes := []byte("panic: test timed out")
	terminal := finishDrained(t, automaticSpec(), []terminalFact{{
		generation: 1, kind: factExit, at: at(time.Second), exit: ExitStatus{Code: 1},
	}}, string(bytes)).(Settled)
	bytes[0] = 'X'
	assert.EqualValues(t, "panic: test timed out", terminal.Output.Bytes, "output/duration changed after delivery: %#v", terminal)
	assert.True(t, terminal.Output.Final, "output/duration changed after delivery: %#v", terminal)
	assert.Equal(t, time.Second, terminal.CommandDuration, "output/duration changed after delivery: %#v", terminal)
	assert.False(t, terminal.Exit.Passed(), "inner timeout-looking exit was classified by #61")
}

func TestOutputSnapshotExcludesLaterAppendsAndRetainsAShortPrefix(t *testing.T) {
	m := drainedBeforeOutput(t)
	source := []byte("before-after")
	m, _, _ = acceptOutput(m, outputAt(m, string(source[:6]), 10, errors.New("short read")))
	source[0] = 'X'
	assert.EqualValues(t, "before", m.output.Bytes, "output prefix/cutoff changed: %#v", m.output)
	assert.EqualValues(t, 10, m.output.Cutoff, "output prefix/cutoff changed: %#v", m.output)
	assert.False(t, m.output.CompleteThroughCutoff, "output prefix/cutoff changed: %#v", m.output)
	assert.True(t, m.output.Final, "output prefix/cutoff changed: %#v", m.output)
}

func TestCausalOutputAndReleaseFailuresBecomeInfrastructure(t *testing.T) {
	for _, test := range []struct {
		name string
		fail func(machine) Terminal
		want Cause
	}{
		{"output", func(m machine) Terminal {
			m, _, _ = acceptOutput(m, outputAt(m, "partial", 20, errors.New("read output")))
			_, _, terminal := acceptRelease(m, m.awaiting, nil)

			return terminal
		}, OutputCaptureFailed},
		{"release", func(m machine) Terminal {
			m, _, _ = acceptOutput(m, outputAt(m, "", 0, nil))
			_, _, terminal := acceptRelease(m, m.awaiting, errors.New("close handle"))

			return terminal
		}, ReleaseFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := drainedBeforeOutput(t)
			terminal := test.fail(m).(Infrastructure)
			{
				got := terminal.Cause
				assert.Equal(t, test.want, got, "want %d, got %d", test.want, got)
			}
			assert.False(t, test.name == "output" && terminal.Output.Bytes != "partial", "partial output was discarded: %#v", terminal)
		})
	}
}

func TestStopAtSetsIntentAndTiedStopConstrainsCleanup(t *testing.T) {
	m, _ := begin(automaticSpec(), 1, t0, 0)
	request := StopRequest{At: at(3 * time.Second), DrainBy: at(8 * time.Second)}
	m = observeRunning(m, at(7*time.Second), at(20*time.Second), []terminalFact{{
		generation: 1, kind: factStop, at: request.At, stop: request,
	}})
	assert.Equal(t, factStop, m.intent.kind, "stop instants conflated: %#v", m)
	assert.True(t, m.drainBy.Equal(request.DrainBy), "stop instants conflated: %#v", m)

	deadlineTie, _ := begin(serialSpec(), 2, t0, 0)
	tied := StopRequest{At: at(20 * time.Second), DrainBy: at(21 * time.Second)}
	deadlineTie = observeRunning(deadlineTie, tied.At, at(25*time.Second), []terminalFact{{
		generation: 2, kind: factStop, at: tied.At, stop: tied,
	}})
	assert.Equal(t, factDeadline, deadlineTie.intent.kind, "deadline tie discarded stop cleanup bound: %#v", deadlineTie)
	assert.True(t, deadlineTie.drainBy.Equal(tied.DrainBy), "deadline tie discarded stop cleanup bound: %#v", deadlineTie)
}

func TestEmergencyEpochUsesOneRequestForOwnedProspectiveAndLateAdoptedObligations(t *testing.T) {
	request := EmergencyRequest{At: at(30 * time.Second), DrainBy: at(40 * time.Second)}
	initial := []obligation{
		{ID: "pending", generation: 2, Kind: ProspectiveUnresolved},
		{ID: "owned", generation: 1, Kind: OwnedUndrained},
	}
	ledger, dispatches := beginEmergency(request, initial)
	require.Len(t, dispatches, 2, "emergency did not close/dispatch: %#v %#v", ledger, dispatches)
	assert.EqualValues(t, 1, dispatches[0].obligation.generation, "emergency did not close/dispatch: %#v %#v", ledger, dispatches)
	for _, dispatch := range dispatches {
		assert.Equal(t, request, dispatch.request, "emergency multiplied/reset epoch: %#v", dispatch)
	}
	ledger, late := ledger.adoptLate(
		obligation{ID: "pending", generation: 2, Kind: OwnedUndrained},
		at(35*time.Second),
	)
	assert.Equal(t, request, late.request, "late start escaped emergency epoch: %#v", late)
	ledger = ledger.resolve(1, OwnedUndrained, at(36*time.Second))
	{
		_, result, ready := ledger.settle(at(39 * time.Second))
		require.False(t, ready, "emergency settled before its bound with a residual: %#v", result)
		assert.Nil(t, result, "emergency settled before its bound with a residual: %#v", result)
	}
	ledger, result, ready := ledger.settle(request.DrainBy)
	settlement, ok := result.(SweepUnconfirmed)
	require.True(t, ready, "emergency did not settle at equality: ready=%v result=%#v", ready, result)
	assert.True(t, ok, "emergency did not settle at equality: ready=%v result=%#v", ready, result)
	first := settlement.Residuals()
	first[0].ID = "mutated"
	second := settlement.Residuals()
	require.Len(t, second, 1, "residual settlement unstable/mutable: %#v", second)
	assert.EqualValues(t, "pending", second[0].ID, "residual settlement unstable/mutable: %#v", second)
	assert.Equal(t, OwnedUndrained, second[0].Kind, "residual settlement unstable/mutable: %#v", second)
	ledger = ledger.resolve(2, OwnedUndrained, at(41*time.Second))
	_, stable, ready := ledger.settle(at(41 * time.Second))
	assert.True(t, ready, "post-settlement custody rewrote result: before=%#v after=%#v", settlement, stable)
	assert.Equal(t, settlement, stable, "post-settlement custody rewrote result: before=%#v after=%#v", settlement, stable)
}

func TestEmergencyCanCloseALateProvenNotReleasedObligation(t *testing.T) {
	request := EmergencyRequest{At: at(30 * time.Second), DrainBy: at(40 * time.Second)}
	ledger, _ := beginEmergency(request, []obligation{{
		ID: "pending", generation: 2, Kind: ProspectiveUnresolved,
	}})
	ledger = ledger.resolve(2, ProspectiveUnresolved, at(35*time.Second))
	_, result, ready := ledger.settle(at(35 * time.Second))
	{
		_, ok := result.(SweepDrained)
		assert.True(t, ready, "proven not-released obligation stayed residual: %#v", result)
		assert.True(t, ok, "proven not-released obligation stayed residual: %#v", result)
	}
}

func TestEmergencyResolutionMustBeStrictlyBeforeTheInclusiveBoundary(t *testing.T) {
	request := EmergencyRequest{At: at(30 * time.Second), DrainBy: at(40 * time.Second)}
	newLedger := func() emergencyLedger {
		ledger, _ := beginEmergency(request, []obligation{{
			ID: "owned", generation: 1, Kind: OwnedUndrained,
		}})

		return ledger
	}

	t.Run("resolved_before_boundary", func(t *testing.T) {
		before := newLedger().resolve(1, OwnedUndrained, request.DrainBy.Add(-time.Nanosecond))
		_, result, ready := before.settle(request.DrainBy.Add(-time.Nanosecond))
		_, ok := result.(SweepDrained)
		require.True(t, ready, "timely resolution did not drain: ready=%v result=%#v", ready, result)
		assert.True(t, ok, "timely resolution did not drain: ready=%v result=%#v", ready, result)
	})

	for _, test := range []struct {
		name string
		when time.Time
	}{
		{"resolved_at_boundary_is_unconfirmed", request.DrainBy},
		{"resolved_after_boundary_is_unconfirmed", request.DrainBy.Add(time.Nanosecond)},
	} {
		t.Run(test.name, func(t *testing.T) {
			late := newLedger().resolve(1, OwnedUndrained, test.when)
			_, result, ready := late.settle(test.when)
			_, ok := result.(SweepUnconfirmed)
			assert.True(t, ready, "late resolution rewrote inclusive expiry at %s: %#v", test.when, result)
			assert.True(t, ok, "late resolution rewrote inclusive expiry at %s: %#v", test.when, result)
		})
	}
}

func TestEmergencyAdoptionAtTheBoundaryCannotRewriteTheResidualSnapshot(t *testing.T) {
	request := EmergencyRequest{At: at(30 * time.Second), DrainBy: at(40 * time.Second)}
	ledger, _ := beginEmergency(request, []obligation{{
		ID: "pending", generation: 2, Kind: ProspectiveUnresolved,
	}})
	ledger, _ = ledger.adoptLate(
		obligation{ID: "pending", generation: 2, Kind: OwnedUndrained},
		request.DrainBy,
	)
	_, result, ready := ledger.settle(request.DrainBy)
	residual := result.(SweepUnconfirmed).Residuals()
	assert.True(t, ready, "at-boundary adoption rewrote the stable snapshot: %#v", residual)
	require.Len(t, residual, 1, "at-boundary adoption rewrote the stable snapshot: %#v", residual)
	assert.Equal(t, ProspectiveUnresolved, residual[0].Kind, "at-boundary adoption rewrote the stable snapshot: %#v", residual)
}

func TestDarwinDestructiveActionsFollowCaptureAndFreeze(t *testing.T) {
	want := []darwinTerminationStep{
		captureLiveMembersAndClosure,
		freezeProcessGroup,
		revalidateCapturedIdentityBeforeFreeze,
		freezeCapturedEscapeesByPID,
		convergeFrozenClosure,
		killProcessGroup,
		revalidateCapturedIdentityBeforeKill,
		killCapturedEscapeesByPID,
	}
	{
		got := darwinTerminationScript()
		assert.Equal(t, want, got, "Darwin drain can destroy its census handle: want=%v got=%v", want, got)
	}
}

func TestDarwinRememberedPIDMustStillNameTheCapturedProcess(t *testing.T) {
	captured := processIdentity{pid: 42, birthToken: 100}
	assert.True(t, sameProcess(captured, processIdentity{pid: 42, birthToken: 100}), "same process identity was rejected")
	assert.False(t, sameProcess(captured, processIdentity{pid: 42, birthToken: 101}), "reused pid was accepted as captured process")
}

func TestTwoPolicyClassesProduceThreeFreshAbsoluteDeadlines(t *testing.T) {
	resolver := policyResolver{launchProgress: 7 * time.Second, drainEpoch: 11 * time.Second}
	got := resolver.resolve(at(time.Second), at(20*time.Second), at(40*time.Second))
	want := resolvedDeadlines{
		LaunchBy:         at(8 * time.Second),
		LocalDrainBy:     at(31 * time.Second),
		EmergencyDrainBy: at(51 * time.Second),
	}
	assert.Equal(t, want, got, "policy classes/epochs conflated: want=%#v got=%#v", want, got)
	assert.False(t, got.LocalDrainBy.Equal(got.EmergencyDrainBy), "policy classes/epochs conflated: want=%#v got=%#v", want, got)
	t.Run("unbounded", func(t *testing.T) {
		defer expectPanic(t)
		policyResolver{drainEpoch: time.Second}.resolve(t0, t0, t0)
	})
}

func TestFusePolicyIsPrivateNominalAndFixed(t *testing.T) {
	assert.EqualValues(t, 64, fuseCeiling, "ceiling=%d cadence=%s", fuseCeiling, nominalFuseCadence)
	assert.Equal(t, 50*time.Millisecond, nominalFuseCadence, "ceiling=%d cadence=%s", fuseCeiling, nominalFuseCadence)
}

func deadlineMachine(t *testing.T, profile Profile) machine {
	t.Helper()
	spec := automaticSpec()
	spec.Profile = profile
	m, err := begin(spec, 1, t0, 0)
	require.NoError(t, err)
	var facts []terminalFact
	if profile == Automatic {
		facts = []terminalFact{{generation: 1, kind: factFuse, at: at(time.Second), rootLive: true, live: 7}}
	}

	return observeRunning(m, at(20*time.Second), at(25*time.Second), facts)
}

func exitMachine(t *testing.T) machine {
	t.Helper()
	m, err := begin(automaticSpec(), 1, t0, 0)
	require.NoError(t, err)

	return observeRunning(m, at(time.Second), at(5*time.Second), []terminalFact{{
		generation: 1, kind: factExit, at: at(time.Second), exit: ExitStatus{Code: 1},
	}})
}

func drainedBeforeOutput(t *testing.T) machine {
	t.Helper()
	m := deadlineMachine(t, Automatic)
	m, _ = advanceDrain(m, nil)
	m, _ = advanceDrain(m, &drainEvent{action: m.awaiting, kind: forceCompleted, at: at(21 * time.Second)})
	m, next := advanceDrain(m, &drainEvent{action: m.awaiting, kind: domainObserved, at: at(22 * time.Second), empty: true})
	assert.Equal(t, captureOutput, next.kind, "want output capture, got %#v", next)

	return m
}

func finishDrained(t *testing.T, spec Spec, facts []terminalFact, output string) Terminal {
	t.Helper()
	m, err := begin(spec, 1, t0, 4*time.Millisecond)
	require.NoError(t, err)
	if spec.Profile == Automatic && len(facts) == 0 {
		facts = []terminalFact{{generation: 1, kind: factFuse, at: at(time.Second), rootLive: true, live: 0}}
	}
	m = observeRunning(m, at(20*time.Second), at(25*time.Second), facts)
	m, next := advanceDrain(m, nil)
	if next.kind == forceDomain {
		m, next = advanceDrain(m, &drainEvent{action: m.awaiting, kind: forceCompleted, at: m.intent.at.Add(time.Millisecond)})
	}
	assert.Equal(t, observeDomain, next.kind, "want domain observation, got %#v", next)
	m, next = advanceDrain(m, &drainEvent{action: m.awaiting, kind: domainObserved, at: m.intent.at.Add(2 * time.Millisecond), empty: true})
	assert.Equal(t, captureOutput, next.kind, "want output capture, got %#v", next)
	m, next, terminal := acceptOutput(m, outputAt(m, output, uint64(len(output)), nil))
	assert.Equal(t, releaseDomain, next.kind, "want release, got %#v terminal=%#v", next, terminal)
	assert.Nil(t, terminal, "want release, got %#v terminal=%#v", next, terminal)
	_, next, terminal = acceptRelease(m, m.awaiting, nil)
	assert.Equal(t, deliverTerminal, next.kind, "want delivery, got %#v terminal=%#v", next, terminal)
	assert.NotNil(t, terminal, "want delivery, got %#v terminal=%#v", next, terminal)

	return terminal
}

func finishAlreadyDrained(t *testing.T, m machine, output string) Terminal {
	t.Helper()
	m.drainStarted, m.drained = true, true
	m, _ = issue(m, captureOutput)
	m, next, terminal := acceptOutput(m, outputAt(m, output, uint64(len(output)), nil))
	assert.Equal(t, releaseDomain, next.kind, "want release, got %#v terminal=%#v", next, terminal)
	assert.Nil(t, terminal, "want release, got %#v terminal=%#v", next, terminal)
	_, _, terminal = acceptRelease(m, m.awaiting, nil)

	return terminal
}

func expectPanic(t *testing.T) {
	t.Helper()
	assert.NotNil(t, recover(), "expected invariant panic")
}

func outputAt(m machine, prefix string, cutoff uint64, err error) outputObservation {
	return outputObservation{action: m.awaiting, cutoff: cutoff, prefix: prefix, err: err}
}
