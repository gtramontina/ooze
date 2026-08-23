package attempt

import (
	"errors"
	"reflect"
	"testing"
	"time"
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
	if err != nil {
		t.Fatal(err)
	}

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
			if !errors.Is(s.Validate(), ErrInvalidSpec) {
				t.Fatalf("invalid %s was accepted", name)
			}
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
	if !errors.Is(err, ErrUnsupportedPlatform) || supervisor != nil || calls != 0 {
		t.Fatalf("unsupported construction leaked work: supervisor=%#v err=%v calls=%d", supervisor, err, calls)
	}
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
		if gen != 7 {
			t.Fatalf("native launch received generation %d", gen)
		}
		admitted = spec

		return Owned{Attempt: owned, LaunchDuration: 3 * time.Millisecond}
	}, func(EmergencyRequest) SweepResult { return SweepDrained{} })
	if err != nil {
		t.Fatal(err)
	}

	spec := automaticSpec()
	launch := supervisor.Launch(spec).(Owned)
	spec.Command[0], spec.Env[0] = "changed", "changed"
	request := StopRequest{At: at(time.Second), DrainBy: at(2 * time.Second)}
	launch.Attempt.Stop(request)
	first, second := launch.Attempt.Wait(), launch.Attempt.Wait()

	if admitted.Command[0] != "go" || admitted.Env[0] != "GOMAXPROCS=1" {
		t.Fatalf("pending launch retained mutable caller slices: %#v", admitted)
	}
	if !reflect.DeepEqual(order, []string{"register", "native"}) {
		t.Fatalf("native launch preceded prospective registration: %v", order)
	}
	if waits != 1 || !reflect.DeepEqual(first, second) || !reflect.DeepEqual(stops, []StopRequest{request}) {
		t.Fatalf("wait/stop contract violated: waits=%d first=%#v second=%#v stops=%#v", waits, first, second, stops)
	}
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

	if got := <-stopped; !reflect.DeepEqual(got, request) {
		t.Fatalf("stop=%#v", got)
	}
	first, second := <-results, <-results
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("wait results differ: %#v %#v", first, second)
	}
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
	if stops != 0 {
		t.Fatalf("stop reached native work after terminal delivery: %d", stops)
	}
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
	if stops != 0 {
		t.Fatalf("stop crossed the release cut: %d", stops)
	}
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
			if next.kind != deliverTerminal {
				t.Fatalf("unconfirmed path dispatched unexpected action: %#v", next)
			}
			seal() // Supervisor takes exclusive residual custody; no release occurs.
			return terminal
		},
	}
	terminal := owned.Wait()
	owned.Stop(StopRequest{At: at(time.Second), DrainBy: at(2 * time.Second)})
	unconfirmed, ok := terminal.(DrainUnconfirmed)
	if !ok || unconfirmed.Residual != OwnedUndrained || stops != 0 || releases != 0 {
		t.Fatalf("unconfirmed custody cut failed: terminal=%#v stops=%d releases=%d", terminal, stops, releases)
	}
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
	if launchCalls != 0 || stopCalls != 0 {
		t.Fatalf("invalid input reached native work: launch=%d stop=%d", launchCalls, stopCalls)
	}
}

func TestLaunchClassificationUsesTheExactReleaseStage(t *testing.T) {
	type tuple struct {
		platform  launchPlatform
		operation launchOperation
		code      launchCode
	}
	eligible := make([]tuple, 0, 37)
	unixFour := []launchCode{codeEAGAIN, codeENOMEM, codeEMFILE, codeENFILE}
	for _, code := range []launchCode{codeEMFILE, codeENFILE} {
		eligible = append(eligible, tuple{platformLinux, acquireInternalDescriptor, code})
		eligible = append(eligible, tuple{platformDarwin, acquireInternalDescriptor, code})
	}
	for _, code := range unixFour {
		eligible = append(eligible, tuple{platformLinux, startLauncher, code})
		eligible = append(eligible, tuple{platformDarwin, startLauncher, code})
	}
	for _, code := range []launchCode{codeEAGAIN, codeENOMEM} {
		eligible = append(eligible, tuple{platformLinux, execTarget, code})
	}
	for _, code := range []launchCode{codeENOMEM, codeEMFILE, codeENFILE} {
		eligible = append(eligible, tuple{platformDarwin, createExitTracker, code})
	}
	eligible = append(eligible,
		tuple{platformDarwin, registerExitTracker, codeENOMEM},
		tuple{platformDarwin, execTarget, codeENOMEM},
	)
	windowsCodes := []launchCode{
		codeWinTooManyOpenFiles, codeWinNotEnoughMemory, codeWinOutOfMemory,
		codeWinNoProcessSlots, codeWinNoSystemResources, codeWinCommitmentLimit,
	}
	for _, operation := range []launchOperation{
		acquireInternalDescriptor, startLauncher, configureWindowsContainment,
	} {
		for _, code := range windowsCodes {
			eligible = append(eligible, tuple{platformWindows, operation, code})
		}
	}
	for _, test := range eligible {
		got := classifyLaunch(nativeLaunchFailure{
			platform: test.platform, operation: test.operation, stage: preRelease,
			code: test.code, closureProven: true, duration: time.Millisecond, err: errors.New("launch"),
		}).(NotReleased)
		if got.Kind != LaunchResourceExhausted {
			t.Fatalf("%#v: want resource exhaustion, got %#v", test, got)
		}
	}
	ordinary := classifyLaunch(nativeLaunchFailure{
		platform: platformLinux, operation: startLauncher, stage: preRelease,
		code: codeOther, closureProven: true, err: errors.New("ENOENT"),
	}).(NotReleased)
	if ordinary.Kind != LaunchFailed {
		t.Fatalf("ordinary launch became %#v", ordinary)
	}
	unclosed := classifyLaunch(nativeLaunchFailure{
		platform: platformWindows, operation: configureWindowsContainment, stage: preRelease,
		code: codeWinNoSystemResources, closureProven: false, err: errors.New("cleanup unconfirmed"),
	})
	if _, ok := unclosed.(LaunchUnconfirmed); !ok {
		t.Fatalf("unclosed suspended process became NotReleased: %#v", unclosed)
	}
	unknown := classifyLaunch(nativeLaunchFailure{
		platform: platformDarwin, operation: startLauncher, stage: unknownRelease,
		code: codeEMFILE, err: errors.New("unknown"),
	})
	if _, ok := unknown.(LaunchUnconfirmed); !ok {
		t.Fatalf("unknown release manufactured closure: %#v", unknown)
	}
	owned := &OwnedAttempt{wait: func(seal func()) Terminal {
		seal()
		return Infrastructure{Cause: ReleaseFailed}
	}}
	post := classifyLaunch(nativeLaunchFailure{
		platform: platformDarwin, operation: startLauncher, stage: postRelease,
		code: codeEMFILE, err: errors.New("after release"), owned: owned,
	}).(Owned)
	if post.Attempt != owned {
		t.Fatalf("post-release target was not adopted: %#v", post)
	}
}

func TestLaunchResourceWhitelistsAreFalseNegativeBiasedAndOperationScoped(t *testing.T) {
	for _, test := range []struct {
		platform  launchPlatform
		operation launchOperation
		code      launchCode
	}{
		{platformDarwin, registerExitTracker, codeEMFILE},
		{platformDarwin, execTarget, codeEAGAIN},
		{platformWindows, execTarget, codeWinNoSystemResources},
		{platformWindows, startLauncher, codeOther},
		{platformLinux, acquireInternalDescriptor, codeENOMEM},
	} {
		got := classifyLaunch(nativeLaunchFailure{
			platform: test.platform, operation: test.operation, stage: preRelease,
			code: test.code, closureProven: true, err: errors.New("excluded"),
		}).(NotReleased)
		if got.Kind != LaunchFailed {
			t.Fatalf("excluded tuple became resource pressure: %#v -> %#v", test, got)
		}
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
			if action != test.action || got.state != test.state {
				t.Fatalf("completion lost boundary: state=%#v action=%d", got, action)
			}
		})
	}
}

func TestLaunchBoundaryWithoutObservableCompletionLatchesAndRetainsCustody(t *testing.T) {
	by := at(5 * time.Second)
	pending, action := advancePending(beginPending(1, by), pendingEvent{generation: 1, kind: launchBoundary, at: by})
	if action != reportLaunchUnconfirmed || pending.state != reportedUnconfirmed {
		t.Fatalf("launch boundary lost custody: %#v action=%d", pending, action)
	}
	completion := launchCompletion{kind: completedReleased, at: by}
	pending, action = advancePending(pending, pendingEvent{
		generation: 1, kind: launchCompleted, at: completion.at, completion: &completion,
	})
	if action != adoptAndForceDrain || pending.state != adoptedOwned {
		t.Fatalf("same-time completion rewrote or escaped custody: %#v action=%d", pending, action)
	}

	notReleased, _ := advancePending(beginPending(2, by), pendingEvent{generation: 2, kind: launchBoundary, at: by})
	closed := launchCompletion{kind: completedNotReleased, at: by}
	notReleased, action = advancePending(notReleased, pendingEvent{
		generation: 2, kind: launchCompleted, at: closed.at, completion: &closed,
	})
	if action != closeProspective || notReleased.state != closedNotReleased {
		t.Fatalf("late closure rewrote or retained custody: %#v action=%d", notReleased, action)
	}
}

func TestLaunchBoundaryRevokesReleaseOfAControllableUnreleasedIdentity(t *testing.T) {
	by := at(5 * time.Second)
	pending, action := advancePending(beginPending(1, by), pendingEvent{
		generation: 1,
		kind:       nativeIdentityAcquired,
		at:         by.Add(-time.Nanosecond),
	})
	if action != continueLaunchEstablishment || !pending.nativeHeld {
		t.Fatalf("native identity was not retained: %#v action=%d", pending, action)
	}
	pending, action = advancePending(pending, pendingEvent{generation: 1, kind: launchBoundary, at: by})
	if action != reportUnconfirmedAndCloseUnreleased || pending.state != reportedUnconfirmed {
		t.Fatalf("boundary permitted later target release: %#v action=%d", pending, action)
	}
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
	if action != closeUnreleasedIdentity || !late.nativeHeld {
		t.Fatalf("late controllable identity was not closed before release: %#v action=%d", late, action)
	}

	emergency, _ := advancePending(beginPending(4, by), pendingEvent{
		generation: 4, kind: nativeIdentityAcquired, at: by.Add(-2 * time.Second),
	})
	emergency, action = advancePending(emergency, pendingEvent{
		generation: 4, kind: launchReleaseRevoked, at: by.Add(-time.Second),
	})
	if action != reportUnconfirmedAndCloseUnreleased || !emergency.releaseDenied {
		t.Fatalf("emergency closure did not revoke target release: %#v action=%d", emergency, action)
	}
	closed := launchCompletion{kind: completedNotReleased, at: by.Add(-time.Nanosecond)}
	emergency, action = advancePending(emergency, pendingEvent{
		generation: 4, kind: launchCompleted, at: closed.at, completion: &closed,
	})
	if action != closeProspective || emergency.state != closedNotReleased {
		t.Fatalf("emergency could not close before LaunchBy: %#v action=%d", emergency, action)
	}

	withoutIdentity, _ := advancePending(beginPending(5, by), pendingEvent{
		generation: 5, kind: launchReleaseRevoked, at: by.Add(-2 * time.Second),
	})
	withoutIdentity, action = advancePending(withoutIdentity, pendingEvent{
		generation: 5, kind: nativeIdentityAcquired, at: by.Add(-time.Second),
	})
	if action != closeUnreleasedIdentity || !withoutIdentity.releaseDenied {
		t.Fatalf("post-emergency identity was not closed: %#v action=%d", withoutIdentity, action)
	}

	published := launchCompletion{kind: completedNotReleased, at: by.Add(-3 * time.Second)}
	preclosed, action := advancePending(beginPending(6, by), pendingEvent{
		generation: 6, kind: launchReleaseRevoked, at: by.Add(-2 * time.Second), completion: &published,
	})
	if action != returnNotReleased || preclosed.state != closedNotReleased {
		t.Fatalf("emergency ignored its completion snapshot: %#v action=%d", preclosed, action)
	}
	releasedSnapshot := launchCompletion{kind: completedReleased, at: by.Add(-3 * time.Second)}
	preowned, action := advancePending(beginPending(8, by), pendingEvent{
		generation: 8, kind: launchReleaseRevoked, at: by.Add(-2 * time.Second), completion: &releasedSnapshot,
	})
	if action != returnOwnedAndForceDrain || preowned.state != adoptedOwned {
		t.Fatalf("emergency failed to return and drain its owned snapshot: %#v action=%d", preowned, action)
	}

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
	if action != returnNotReleased || pending.state != closedNotReleased {
		t.Fatalf("delayed notification hid published completion: %#v action=%d", pending, action)
	}

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
	if got, ok := chooseIntent(spec, 2, t0, at(2*time.Second), []terminalFact{stale}); ok {
		t.Fatalf("reused public ID accepted old generation: %#v", got)
	}
	fresh := stale
	fresh.generation = 2
	if got, ok := chooseIntent(spec, 2, t0, at(2*time.Second), []terminalFact{fresh}); !ok || got.kind != factExit {
		t.Fatalf("fresh generation was rejected: %#v", got)
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
			if !ok || got.kind != test.want {
				t.Fatalf("want %d, got %#v", test.want, got)
			}
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
			if !ok || got.kind != test.want || !got.stop.DrainBy.Equal(at(21*time.Second)) {
				t.Fatalf("tie lost cause or cleanup constraint: %#v", got)
			}
		})
	}
}

func TestSameInstantFactsAreCanonicalOrRejected(t *testing.T) {
	tie := at(time.Second)
	fuse, _ := chooseIntent(automaticSpec(), 1, t0, tie, []terminalFact{
		{generation: 1, kind: factFuse, at: tie, rootLive: true, live: 70},
		{generation: 1, kind: factFuse, at: tie, rootLive: true, live: 90},
	})
	if fuse.live != 90 {
		t.Fatalf("same-time fuse depends on delivery order: %#v", fuse)
	}
	stop, _ := chooseIntent(automaticSpec(), 1, t0, tie, []terminalFact{
		{generation: 1, kind: factStop, at: tie, stop: StopRequest{At: tie, DrainBy: at(9 * time.Second)}},
		{generation: 1, kind: factStop, at: tie, stop: StopRequest{At: tie, DrainBy: at(8 * time.Second)}},
	})
	if !stop.stop.DrainBy.Equal(at(8 * time.Second)) {
		t.Fatalf("same-time stop depends on delivery order: %#v", stop)
	}
	t.Run("duplicate exit", func(t *testing.T) {
		defer expectPanic(t)
		chooseIntent(automaticSpec(), 1, t0, tie, []terminalFact{
			{generation: 1, kind: factExit, at: tie},
			{generation: 1, kind: factExit, at: tie},
		})
	})
}

func TestFuseSampleRequiresMatchingGenerationAndLiveRoot(t *testing.T) {
	for _, fact := range []terminalFact{
		{generation: 2, kind: factFuse, at: at(time.Second), rootLive: true, live: 100},
		{generation: 1, kind: factFuse, at: at(time.Second), rootLive: false, live: 100},
		{generation: 1, kind: factFuse, at: at(time.Second), rootLive: true, live: 64},
	} {
		if got, ok := chooseIntent(automaticSpec(), 1, t0, at(2*time.Second), []terminalFact{fact}); ok {
			t.Fatalf("invalid fuse selected: %#v", got)
		}
	}
	if got, ok := chooseIntent(serialSpec(), 1, t0, at(2*time.Second), []terminalFact{{
		generation: 1, kind: factFuse, at: at(time.Second), rootLive: true, live: 100,
	}}); ok {
		t.Fatalf("serial attempt selected fuse: %#v", got)
	}
}

func TestDeadlineIsInclusiveAndAnObservableExitWinsTheTie(t *testing.T) {
	if _, ok := chooseIntent(automaticSpec(), 1, t0, at(20*time.Second-time.Nanosecond), nil); ok {
		t.Fatal("deadline fired early")
	}
	got, _ := chooseIntent(automaticSpec(), 1, t0, at(20*time.Second), nil)
	if got.kind != factDeadline {
		t.Fatalf("deadline missed equality: %#v", got)
	}
	got, _ = chooseIntent(automaticSpec(), 1, t0, at(20*time.Second), []terminalFact{{
		generation: 1, kind: factExit, at: at(20 * time.Second), exit: ExitStatus{Code: 1},
	}})
	if got.kind != factExit {
		t.Fatalf("observable exit lost tie: %#v", got)
	}
}

func TestNativeFactUsesItsPostOperationInstant(t *testing.T) {
	lateFuse := terminalFact{
		generation: 1, kind: factFuse, at: at(21 * time.Second), rootLive: true, live: 100,
	}
	got, _ := chooseIntent(automaticSpec(), 1, t0, at(21*time.Second), []terminalFact{lateFuse})
	if got.kind != factDeadline {
		t.Fatalf("late census inherited an earlier instant: %#v", got)
	}
}

func TestAutomaticDeadlineNeverFabricatesAndRetainsARealRunningSample(t *testing.T) {
	m, _ := begin(automaticSpec(), 1, t0, 4*time.Millisecond)
	facts := []terminalFact{
		{generation: 1, kind: factFuse, at: at(time.Second), rootLive: true, live: 3},
		{generation: 1, kind: factFuse, at: at(21 * time.Second), rootLive: true, live: 200},
	}
	m = observeRunning(m, at(21*time.Second), at(25*time.Second), facts)
	if m.intent.kind != factDeadline || m.intent.peak != 3 {
		t.Fatalf("post-intent sample changed timeout evidence: %#v", m)
	}
	original := *m.intent
	m = observeRunning(m, at(30*time.Second), at(35*time.Second), []terminalFact{{
		generation: 1, kind: factExit, at: at(22 * time.Second),
	}})
	if !reflect.DeepEqual(*m.intent, original) {
		t.Fatalf("late exit rewrote intent: before=%#v after=%#v", original, *m.intent)
	}

	noSample, _ := begin(automaticSpec(), 1, t0, 0)
	noSample = observeRunning(noSample, at(20*time.Second), at(25*time.Second), nil)
	terminal := finishAlreadyDrained(t, noSample, "").(Tripped)
	if terminal.Trip.(AutomaticDeadlineTrip).Peak.Present {
		t.Fatalf("missing census became a measured zero: %#v", terminal)
	}
}

func TestTimeoutForcesFirstAndCarriesOneAbsoluteDeadline(t *testing.T) {
	m := deadlineMachine(t, Automatic)
	m, first := advanceDrain(m, nil)
	if first.kind != forceDomain || !first.by.Equal(m.drainBy) {
		t.Fatalf("first action=%#v", first)
	}
	m, second := advanceDrain(m, &drainEvent{action: m.awaiting, kind: forceCompleted, at: at(21 * time.Second)})
	if second.kind != observeDomain || !second.by.Equal(first.by) {
		t.Fatalf("drain reset its deadline: first=%#v second=%#v", first, second)
	}
}

func TestNaturalExitObservesBeforeForcingAndSkipsForceWhenEmpty(t *testing.T) {
	m := exitMachine(t)
	m, first := advanceDrain(m, nil)
	if first.kind != observeDomain || m.forced {
		t.Fatalf("clean exit forced before census: action=%#v state=%#v", first, m)
	}
	m, next := advanceDrain(m, &drainEvent{action: m.awaiting, kind: domainObserved, at: at(2 * time.Second), empty: true})
	if next.kind != captureOutput || !m.drained || m.forced {
		t.Fatalf("empty exit domain was forced: action=%#v state=%#v", next, m)
	}
}

func TestNaturalExitWithResidualForcesEvenWhenObservationReachesDeadline(t *testing.T) {
	m := exitMachine(t)
	m.drainBy = at(2 * time.Second)
	m, _ = advanceDrain(m, nil)
	m, force := advanceDrain(m, &drainEvent{action: m.awaiting, kind: domainObserved, at: m.drainBy, empty: false})
	if force.kind != forceDomain || !m.forced {
		t.Fatalf("expired residual escaped without force: action=%#v state=%#v", force, m)
	}
	m, capture := advanceDrain(m, &drainEvent{action: m.awaiting, kind: forceCompleted, at: at(3 * time.Second)})
	if capture.kind != captureOutput || !m.unconfirmed {
		t.Fatalf("forced expired residual did not remain unconfirmed: action=%#v state=%#v", capture, m)
	}
}

func TestEmergencyClampCannotBeLostDuringTheFirstAction(t *testing.T) {
	m := deadlineMachine(t, Automatic)
	clamp := at(22 * time.Second)
	m = shortenDrain(m, clamp)
	original := *m.intent
	m, first := advanceDrain(m, nil)
	if first.kind != forceDomain || !first.by.Equal(clamp) || !reflect.DeepEqual(*m.intent, original) {
		t.Fatalf("clamp rewrote intent or missed first action: %#v %#v", m, first)
	}
}

func TestExpiryAtEqualityCapturesOutputThenDeliversUnconfirmedWithoutRelease(t *testing.T) {
	m := deadlineMachine(t, Automatic)
	m, _ = advanceDrain(m, nil)
	m, _ = advanceDrain(m, &drainEvent{action: m.awaiting, kind: forceCompleted, at: at(21 * time.Second)})
	m, capture := advanceDrain(m, &drainEvent{
		action: m.awaiting,
		kind:   domainObserved, at: m.drainBy, empty: true,
	})
	if capture.kind != captureOutput || !m.unconfirmed || m.drained {
		t.Fatalf("deadline manufactured timely drainage: action=%#v state=%#v", capture, m)
	}
	_, next, terminal := acceptOutput(m, outputAt(m, "partial mutant output", 40, errors.New("partial read")))
	unconfirmed, ok := terminal.(DrainUnconfirmed)
	if !ok || next.kind != deliverTerminal || unconfirmed.Output.Bytes != "partial mutant output" ||
		unconfirmed.Output.CompleteThroughCutoff || unconfirmed.Output.Final ||
		unconfirmed.Failures.Output == "" || unconfirmed.BoundFired != CommandDeadlineFired {
		t.Fatalf("unconfirmed path lost output or attempted release: action=%#v terminal=%#v", next, terminal)
	}
}

func TestDrainedPathCapturesOutputBeforeReleaseAndThenDelivers(t *testing.T) {
	m := deadlineMachine(t, Automatic)
	m, _ = advanceDrain(m, nil)
	m, _ = advanceDrain(m, &drainEvent{action: m.awaiting, kind: forceCompleted, at: at(21 * time.Second)})
	m, capture := advanceDrain(m, &drainEvent{action: m.awaiting, kind: domainObserved, at: at(22 * time.Second), empty: true})
	if capture.kind != captureOutput {
		t.Fatalf("want capture, got %#v", capture)
	}
	m, release, terminal := acceptOutput(m, outputAt(m, "captured", 8, nil))
	if release.kind != releaseDomain || terminal != nil {
		t.Fatalf("output skipped release phase: action=%#v terminal=%#v", release, terminal)
	}
	_, deliver, terminal := acceptRelease(m, m.awaiting, nil)
	if deliver.kind != deliverTerminal || terminal == nil {
		t.Fatalf("release did not deliver: action=%#v terminal=%#v", deliver, terminal)
	}
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
			if reflect.TypeOf(got) != reflect.TypeOf(test.want) {
				t.Fatalf("want %T, got %T (%#v)", test.want, got, got)
			}
		})
	}
}

func TestTripCountExposureIsVariantSpecific(t *testing.T) {
	runaway := finishDrained(t, automaticSpec(), []terminalFact{{
		generation: 1, kind: factFuse, at: at(time.Second), rootLive: true, live: 70,
	}}, "").(Tripped)
	if runaway.Trip.(FuseTrip).Live != 70 {
		t.Fatalf("runaway=%#v", runaway)
	}
	autoTimeout := finishDrained(t, automaticSpec(), []terminalFact{{
		generation: 1, kind: factFuse, at: at(time.Second), rootLive: true, live: 7,
	}}, "").(Tripped)
	if peak := autoTimeout.Trip.(AutomaticDeadlineTrip).Peak; !peak.Present || peak.Value != 7 {
		t.Fatalf("timeout=%#v", autoTimeout)
	}
	if _, ok := finishDrained(t, serialSpec(), nil, "").(Tripped).Trip.(SerialDeadlineTrip); !ok {
		t.Fatal("serial timeout grew count evidence")
	}
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
	if !ok || infra.Cause != TerminationControlFailed || !errors.Is(infra.Err, controlErr) {
		t.Fatalf("want termination infrastructure, got %#v", terminal)
	}
	if infra.BoundFired != CommandDeadlineFired {
		t.Fatalf("infrastructure overlay lost the fired command bound: %#v", infra)
	}

	m = deadlineMachine(t, Automatic)
	m, _ = advanceDrain(m, nil)
	m, _ = advanceDrain(m, &drainEvent{action: m.awaiting, kind: forceCompleted, at: m.drainBy, controlErr: controlErr})
	_, _, terminal = acceptOutput(m, outputAt(m, "", 0, nil))
	unconfirmed, ok := terminal.(DrainUnconfirmed)
	if !ok {
		t.Fatalf("control failure manufactured emptiness: %#v", terminal)
	}
	if unconfirmed.BoundFired != CommandDeadlineFired {
		t.Fatalf("unconfirmed overlay lost the fired command bound: %#v", unconfirmed)
	}
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
	if unconfirmed.Failures.Termination == "" || unconfirmed.Failures.DrainCensus == "" ||
		unconfirmed.Failures.Output == "" {
		t.Fatalf("useful diagnoses were lost: %#v", unconfirmed.Failures)
	}
}

func TestSimultaneousWaitAndCensusFailuresAreStableAndRetained(t *testing.T) {
	facts := []terminalFact{
		{generation: 1, kind: factSupervisionFailure, at: at(time.Second), cause: CensusFailed, err: errors.New("running census")},
		{generation: 1, kind: factSupervisionFailure, at: at(time.Second), cause: WaitFailed, err: errors.New("root wait")},
	}
	terminal := finishDrained(t, automaticSpec(), facts, "").(Infrastructure)
	if terminal.Cause != WaitFailed || terminal.Failures.Wait == "" || terminal.Failures.RunningCensus == "" {
		t.Fatalf("simultaneous supervision failures were unstable or lost: %#v", terminal)
	}

	m, err := begin(automaticSpec(), 1, t0, 0)
	if err != nil {
		t.Fatal(err)
	}
	m = observeRunning(m, at(time.Second), at(5*time.Second), facts[1:])
	m, _ = advanceDrain(m, nil)
	m, _ = advanceDrain(m, &drainEvent{action: m.awaiting, kind: forceCompleted, at: m.drainBy})
	_, _, got := acceptOutput(m, outputAt(m, "", 0, nil))
	unconfirmed := got.(DrainUnconfirmed)
	if unconfirmed.Failures.Wait == "" {
		t.Fatalf("unconfirmed overlay lost initiating wait failure: %#v", unconfirmed)
	}
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
	if terminal.Output.Bytes != "panic: test timed out" || !terminal.Output.Final ||
		terminal.CommandDuration != time.Second {
		t.Fatalf("output/duration changed after delivery: %#v", terminal)
	}
	if terminal.Exit.Passed() {
		t.Fatal("inner timeout-looking exit was classified by #61")
	}
}

func TestOutputSnapshotExcludesLaterAppendsAndRetainsAShortPrefix(t *testing.T) {
	m := drainedBeforeOutput(t)
	source := []byte("before-after")
	m, _, _ = acceptOutput(m, outputAt(m, string(source[:6]), 10, errors.New("short read")))
	source[0] = 'X'
	if m.output.Bytes != "before" || m.output.Cutoff != 10 ||
		m.output.CompleteThroughCutoff || !m.output.Final {
		t.Fatalf("output prefix/cutoff changed: %#v", m.output)
	}
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
			if got := terminal.Cause; got != test.want {
				t.Fatalf("want %d, got %d", test.want, got)
			}
			if test.name == "output" && terminal.Output.Bytes != "partial" {
				t.Fatalf("partial output was discarded: %#v", terminal)
			}
		})
	}
}

func TestStopAtSetsIntentAndTiedStopConstrainsCleanup(t *testing.T) {
	m, _ := begin(automaticSpec(), 1, t0, 0)
	request := StopRequest{At: at(3 * time.Second), DrainBy: at(8 * time.Second)}
	m = observeRunning(m, at(7*time.Second), at(20*time.Second), []terminalFact{{
		generation: 1, kind: factStop, at: request.At, stop: request,
	}})
	if m.intent.kind != factStop || !m.drainBy.Equal(request.DrainBy) {
		t.Fatalf("stop instants conflated: %#v", m)
	}

	deadlineTie, _ := begin(serialSpec(), 2, t0, 0)
	tied := StopRequest{At: at(20 * time.Second), DrainBy: at(21 * time.Second)}
	deadlineTie = observeRunning(deadlineTie, tied.At, at(25*time.Second), []terminalFact{{
		generation: 2, kind: factStop, at: tied.At, stop: tied,
	}})
	if deadlineTie.intent.kind != factDeadline || !deadlineTie.drainBy.Equal(tied.DrainBy) {
		t.Fatalf("deadline tie discarded stop cleanup bound: %#v", deadlineTie)
	}
}

func TestEmergencyEpochUsesOneRequestForOwnedProspectiveAndLateAdoptedObligations(t *testing.T) {
	request := EmergencyRequest{At: at(30 * time.Second), DrainBy: at(40 * time.Second)}
	initial := []obligation{
		{ID: "pending", generation: 2, Kind: ProspectiveUnresolved},
		{ID: "owned", generation: 1, Kind: OwnedUndrained},
	}
	ledger, dispatches := beginEmergency(request, initial)
	if len(dispatches) != 2 || dispatches[0].obligation.generation != 1 {
		t.Fatalf("emergency did not close/dispatch: %#v %#v", ledger, dispatches)
	}
	for _, dispatch := range dispatches {
		if !reflect.DeepEqual(dispatch.request, request) {
			t.Fatalf("emergency multiplied/reset epoch: %#v", dispatch)
		}
	}
	ledger, late := ledger.adoptLate(
		obligation{ID: "pending", generation: 2, Kind: OwnedUndrained},
		at(35*time.Second),
	)
	if !reflect.DeepEqual(late.request, request) {
		t.Fatalf("late start escaped emergency epoch: %#v", late)
	}
	ledger = ledger.resolve(1, OwnedUndrained, at(36*time.Second))
	if _, result, ready := ledger.settle(at(39 * time.Second)); ready || result != nil {
		t.Fatalf("emergency settled before its bound with a residual: %#v", result)
	}
	ledger, result, ready := ledger.settle(request.DrainBy)
	settlement, ok := result.(SweepUnconfirmed)
	if !ready || !ok {
		t.Fatalf("emergency did not settle at equality: ready=%v result=%#v", ready, result)
	}
	first := settlement.Residuals()
	first[0].ID = "mutated"
	second := settlement.Residuals()
	if len(second) != 1 || second[0].ID != "pending" || second[0].Kind != OwnedUndrained {
		t.Fatalf("residual settlement unstable/mutable: %#v", second)
	}
	ledger = ledger.resolve(2, OwnedUndrained, at(41*time.Second))
	_, stable, ready := ledger.settle(at(41 * time.Second))
	if !ready || !reflect.DeepEqual(stable, settlement) {
		t.Fatalf("post-settlement custody rewrote result: before=%#v after=%#v", settlement, stable)
	}
}

func TestEmergencyCanCloseALateProvenNotReleasedObligation(t *testing.T) {
	request := EmergencyRequest{At: at(30 * time.Second), DrainBy: at(40 * time.Second)}
	ledger, _ := beginEmergency(request, []obligation{{
		ID: "pending", generation: 2, Kind: ProspectiveUnresolved,
	}})
	ledger = ledger.resolve(2, ProspectiveUnresolved, at(35*time.Second))
	_, result, ready := ledger.settle(at(35 * time.Second))
	if _, ok := result.(SweepDrained); !ready || !ok {
		t.Fatalf("proven not-released obligation stayed residual: %#v", result)
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

	before := newLedger().resolve(1, OwnedUndrained, request.DrainBy.Add(-time.Nanosecond))
	_, result, ready := before.settle(request.DrainBy.Add(-time.Nanosecond))
	if _, ok := result.(SweepDrained); !ready || !ok {
		t.Fatalf("timely resolution did not drain: ready=%v result=%#v", ready, result)
	}

	for _, when := range []time.Time{request.DrainBy, request.DrainBy.Add(time.Nanosecond)} {
		late := newLedger().resolve(1, OwnedUndrained, when)
		_, result, ready = late.settle(when)
		if _, ok := result.(SweepUnconfirmed); !ready || !ok {
			t.Fatalf("late resolution rewrote inclusive expiry at %s: %#v", when, result)
		}
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
	if !ready || len(residual) != 1 || residual[0].Kind != ProspectiveUnresolved {
		t.Fatalf("at-boundary adoption rewrote the stable snapshot: %#v", residual)
	}
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
	if got := darwinTerminationScript(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Darwin drain can destroy its census handle: want=%v got=%v", want, got)
	}
}

func TestDarwinRememberedPIDMustStillNameTheCapturedProcess(t *testing.T) {
	captured := processIdentity{pid: 42, birthToken: 100}
	if !sameProcess(captured, processIdentity{pid: 42, birthToken: 100}) {
		t.Fatal("same process identity was rejected")
	}
	if sameProcess(captured, processIdentity{pid: 42, birthToken: 101}) {
		t.Fatal("reused pid was accepted as captured process")
	}
}

func TestTwoPolicyClassesProduceThreeFreshAbsoluteDeadlines(t *testing.T) {
	resolver := policyResolver{launchProgress: 7 * time.Second, drainEpoch: 11 * time.Second}
	got := resolver.resolve(at(time.Second), at(20*time.Second), at(40*time.Second))
	want := resolvedDeadlines{
		LaunchBy:         at(8 * time.Second),
		LocalDrainBy:     at(31 * time.Second),
		EmergencyDrainBy: at(51 * time.Second),
	}
	if !reflect.DeepEqual(got, want) || got.LocalDrainBy.Equal(got.EmergencyDrainBy) {
		t.Fatalf("policy classes/epochs conflated: want=%#v got=%#v", want, got)
	}
	t.Run("unbounded", func(t *testing.T) {
		defer expectPanic(t)
		policyResolver{drainEpoch: time.Second}.resolve(t0, t0, t0)
	})
}

func TestFusePolicyIsPrivateNominalAndFixed(t *testing.T) {
	if fuseCeiling != 64 || nominalFuseCadence != 50*time.Millisecond {
		t.Fatalf("ceiling=%d cadence=%s", fuseCeiling, nominalFuseCadence)
	}
}

func deadlineMachine(t *testing.T, profile Profile) machine {
	t.Helper()
	spec := automaticSpec()
	spec.Profile = profile
	m, err := begin(spec, 1, t0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var facts []terminalFact
	if profile == Automatic {
		facts = []terminalFact{{generation: 1, kind: factFuse, at: at(time.Second), rootLive: true, live: 7}}
	}

	return observeRunning(m, at(20*time.Second), at(25*time.Second), facts)
}

func exitMachine(t *testing.T) machine {
	t.Helper()
	m, err := begin(automaticSpec(), 1, t0, 0)
	if err != nil {
		t.Fatal(err)
	}

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
	if next.kind != captureOutput {
		t.Fatalf("want output capture, got %#v", next)
	}

	return m
}

func finishDrained(t *testing.T, spec Spec, facts []terminalFact, output string) Terminal {
	t.Helper()
	m, err := begin(spec, 1, t0, 4*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Profile == Automatic && len(facts) == 0 {
		facts = []terminalFact{{generation: 1, kind: factFuse, at: at(time.Second), rootLive: true, live: 0}}
	}
	m = observeRunning(m, at(20*time.Second), at(25*time.Second), facts)
	m, next := advanceDrain(m, nil)
	if next.kind == forceDomain {
		m, next = advanceDrain(m, &drainEvent{action: m.awaiting, kind: forceCompleted, at: m.intent.at.Add(time.Millisecond)})
	}
	if next.kind != observeDomain {
		t.Fatalf("want domain observation, got %#v", next)
	}
	m, next = advanceDrain(m, &drainEvent{action: m.awaiting, kind: domainObserved, at: m.intent.at.Add(2 * time.Millisecond), empty: true})
	if next.kind != captureOutput {
		t.Fatalf("want output capture, got %#v", next)
	}
	m, next, terminal := acceptOutput(m, outputAt(m, output, uint64(len(output)), nil))
	if next.kind != releaseDomain || terminal != nil {
		t.Fatalf("want release, got %#v terminal=%#v", next, terminal)
	}
	_, next, terminal = acceptRelease(m, m.awaiting, nil)
	if next.kind != deliverTerminal || terminal == nil {
		t.Fatalf("want delivery, got %#v terminal=%#v", next, terminal)
	}

	return terminal
}

func finishAlreadyDrained(t *testing.T, m machine, output string) Terminal {
	t.Helper()
	m.drainStarted, m.drained = true, true
	m, _ = issue(m, captureOutput)
	m, next, terminal := acceptOutput(m, outputAt(m, output, uint64(len(output)), nil))
	if next.kind != releaseDomain || terminal != nil {
		t.Fatalf("want release, got %#v terminal=%#v", next, terminal)
	}
	_, _, terminal = acceptRelease(m, m.awaiting, nil)

	return terminal
}

func expectPanic(t *testing.T) {
	t.Helper()
	if recover() == nil {
		t.Fatal("expected invariant panic")
	}
}

func outputAt(m machine, prefix string, cutoff uint64, err error) outputObservation {
	return outputObservation{action: m.awaiting, cutoff: cutoff, prefix: prefix, err: err}
}
