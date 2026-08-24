package ooze

import (
	"reflect"
	"testing"
	"time"
)

func TestSupervisorReducerRunningEarliestFactAndExactTiePriority(t *testing.T) {
	tests := []struct {
		name      string
		facts     []runningFactSpec
		recheck   bool
		want      supervisorRunningIntentKind
		wantCount supervisorObservedCount
	}{
		{
			name: "exit before fuse",
			facts: []runningFactSpec{
				{kind: supervisorRunningFuseObserved, offset: -time.Second, live: 65, rootLive: true},
				{kind: supervisorRunningRootExited, offset: -2 * time.Second},
			},
			recheck: true, want: supervisorIntentRootExit,
		},
		{
			name: "fuse before exit",
			facts: []runningFactSpec{
				{kind: supervisorRunningRootExited, offset: -time.Second},
				{kind: supervisorRunningFuseObserved, offset: -2 * time.Second, live: 65, rootLive: true},
			},
			recheck: true, want: supervisorIntentFuse,
			wantCount: supervisorObservedCount{present: true, value: 65},
		},
		{
			name: "fuse wins full equality",
			facts: []runningFactSpec{
				{kind: supervisorRunningStopRequested},
				{kind: supervisorRunningObservationFailed},
				{kind: supervisorRunningFuseObserved, live: 73, rootLive: true},
			},
			recheck: true, want: supervisorIntentFuse,
			wantCount: supervisorObservedCount{present: true, value: 73},
		},
		{
			name: "exit wins equality without fuse",
			facts: []runningFactSpec{
				{kind: supervisorRunningStopRequested},
				{kind: supervisorRunningObservationFailed},
			},
			recheck: true, want: supervisorIntentRootExit,
		},
		{
			name: "observation failure wins equality without exit",
			facts: []runningFactSpec{
				{kind: supervisorRunningStopRequested},
				{kind: supervisorRunningObservationFailed},
			},
			want: supervisorIntentObservationFailure,
		},
		{
			name:  "inclusive deadline wins equality over stop",
			facts: []runningFactSpec{{kind: supervisorRunningStopRequested}},
			want:  supervisorIntentDeadline,
		},
		{
			name:  "earlier stop participates when delivered in deadline bundle",
			facts: []runningFactSpec{{kind: supervisorRunningStopRequested, offset: -time.Second}},
			want:  supervisorIntentStop,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunningReducerFixture(t, AutomaticProfile)
			facts := fixture.runningFacts(test.facts)
			recheck := supervisorExitRecheck{
				performed: true,
				at:        fixture.deadlineAt,
				observed:  test.recheck,
			}
			next, actions := fixture.reduceBundle(t, fixture.deadlineAt, facts, recheck)
			attempt := supervisorAttemptByGeneration(t, next, fixture.generation)
			if !attempt.intent.latched || attempt.intent.kind != test.want ||
				attempt.intent.count != test.wantCount ||
				attempt.intent.duration != attempt.intent.at.Sub(fixture.startedAt) {
				t.Fatalf("running intent = %#v, want kind=%d count=%#v", attempt.intent, test.want, test.wantCount)
			}
			wantAction := supervisorForceOwned
			if test.want == supervisorIntentRootExit {
				wantAction = supervisorObserveEmptiness
			}
			assertSupervisorActions(t, actions, wantAction)
			if actions[0].generation != fixture.generation ||
				!reflect.DeepEqual(actions[0].intent, attempt.intent) ||
				!actions[0].drainBy.Equal(fixture.drainByFor(attempt.intent)) ||
				actions[0].kind == supervisorSampleRunning || actions[0].kind == supervisorWaitRoot {
				t.Fatalf("intent action = %#v", actions[0])
			}
		})
	}
}

func TestSupervisorReducerRunningDeadlineRechecksExitAndRetainsOnlyValidCountEvidence(t *testing.T) {
	t.Run("boundary exit recheck wins equality", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, AutomaticProfile)
		recheck := supervisorExitRecheck{
			performed: true,
			observed:  true,
			at:        fixture.deadlineAt,
			code:      17,
		}
		next, actions := fixture.reduceBundle(t, fixture.deadlineAt, nil, recheck)
		intent := supervisorAttemptByGeneration(t, next, fixture.generation).intent
		if intent.kind != supervisorIntentRootExit || intent.exitCode != 17 {
			t.Fatalf("deadline exit recheck = %#v", intent)
		}
		assertSupervisorActions(t, actions, supervisorObserveEmptiness)
	})

	t.Run("automatic deadline retains real pre-intent peak", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, AutomaticProfile)
		facts := fixture.runningFacts([]runningFactSpec{
			{kind: supervisorRunningFuseObserved, offset: -10 * time.Second, live: 3, rootLive: true},
			{kind: supervisorRunningFuseObserved, offset: -5 * time.Second, live: 7, rootLive: true},
		})
		next, _ := fixture.reduceBundle(t, fixture.deadlineAt, facts, supervisorExitRecheck{
			performed: true, at: fixture.deadlineAt,
		})
		intent := supervisorAttemptByGeneration(t, next, fixture.generation).intent
		if intent.kind != supervisorIntentDeadline ||
			intent.count != (supervisorObservedCount{present: true, value: 7}) {
			t.Fatalf("automatic deadline peak = %#v", intent)
		}
	})

	t.Run("automatic deadline invents no missing peak", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, AutomaticProfile)
		next, _ := fixture.reduceBundle(t, fixture.deadlineAt, nil, supervisorExitRecheck{
			performed: true, at: fixture.deadlineAt,
		})
		intent := supervisorAttemptByGeneration(t, next, fixture.generation).intent
		if intent.kind != supervisorIntentDeadline || intent.count.present || intent.count.value != 0 {
			t.Fatalf("automatic deadline invented a count: %#v", intent)
		}
	})

	t.Run("serial deadline neither samples fuse nor carries a count", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, SerialProfile)
		if fixture.sampleAction != 0 {
			t.Fatalf("serial attempt received sample action %d", fixture.sampleAction)
		}
		next, actions := fixture.reduceBundle(t, fixture.deadlineAt, nil, supervisorExitRecheck{
			performed: true, at: fixture.deadlineAt,
		})
		intent := supervisorAttemptByGeneration(t, next, fixture.generation).intent
		if intent.kind != supervisorIntentDeadline || intent.count.present || intent.count.value != 0 {
			t.Fatalf("serial deadline count = %#v", intent)
		}
		assertSupervisorActions(t, actions, supervisorForceOwned)
	})

	t.Run("serial rejects fuse observation", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, SerialProfile)
		fact := supervisorRunningFact{
			generation: fixture.generation,
			action:     fixture.waitAction,
			kind:       supervisorRunningFuseObserved,
			at:         fixture.startedAt.Add(time.Second),
			rootLive:   true,
			live:       65,
		}
		assertSupervisorInvariant(t, func() {
			fixture.reduceBundle(t, fixture.startedAt.Add(time.Second), []supervisorRunningFact{fact}, supervisorExitRecheck{})
		})
	})
}

func TestSupervisorReducerRunningIntentIsFinalAndStopsSamplingAndWaitActions(t *testing.T) {
	fixture := newRunningReducerFixture(t, AutomaticProfile)
	facts := fixture.runningFacts([]runningFactSpec{{
		kind: supervisorRunningFuseObserved, offset: -time.Second, live: 65, rootLive: true,
	}})
	latched, actions := fixture.reduceBundle(t, fixture.deadlineAt, facts, supervisorExitRecheck{
		performed: true, at: fixture.deadlineAt,
	})
	assertSupervisorActions(t, actions, supervisorForceOwned)
	before := supervisorAttemptByGeneration(t, latched, fixture.generation).intent

	lateFact := supervisorRunningFact{
		generation: fixture.generation,
		action:     fixture.sampleAction,
		kind:       supervisorRunningFuseObserved,
		at:         fixture.deadlineAt.Add(time.Second),
		rootLive:   true,
		live:       200,
	}
	after, lateActions := reduceSupervisor(latched, supervisorEvent{
		kind: supervisorRunningObserved, generation: fixture.generation,
		at: fixture.deadlineAt.Add(time.Second), drainBy: fixture.drainBy,
		running: &supervisorRunningBundle{
			generation:   fixture.generation,
			sampleAction: fixture.sampleAction,
			waitAction:   fixture.waitAction,
			facts:        []supervisorRunningFact{lateFact},
		},
	})
	if len(lateActions) != 0 {
		t.Fatalf("post-intent facts emitted actions: %#v", lateActions)
	}
	got := supervisorAttemptByGeneration(t, after, fixture.generation).intent
	if !reflect.DeepEqual(got, before) {
		t.Fatalf("post-intent facts rewrote intent: before=%#v after=%#v", before, got)
	}
}

func TestSupervisorReducerRunningSealedStatesStillValidateLateProvenance(t *testing.T) {
	newOrdinarySeal := func(t *testing.T) (runningReducerFixture, supervisorState, time.Time) {
		t.Helper()
		fixture := newRunningReducerFixture(t, AutomaticProfile)
		at := fixture.startedAt.Add(time.Second)
		fact := supervisorRunningFact{
			generation: fixture.generation, action: fixture.sampleAction,
			kind: supervisorRunningFuseObserved, at: at, rootLive: true, live: 65,
		}
		sealed, _ := fixture.reduceBundle(t, at, []supervisorRunningFact{fact}, supervisorExitRecheck{})

		return fixture, sealed, at
	}
	newEmergencySeal := func(t *testing.T) (runningReducerFixture, supervisorState, time.Time) {
		t.Helper()
		fixture := newRunningReducerFixture(t, AutomaticProfile)
		at := fixture.startedAt.Add(time.Second)
		sealed, _ := reduceSupervisor(fixture.state, supervisorEvent{
			kind: supervisorEmergencyStarted, at: at, drainBy: at.Add(time.Second),
			emergencySnapshots: []supervisorEmergencySnapshot{{
				generation: fixture.generation,
				running: &supervisorRunningBundle{
					generation: fixture.generation, sampleAction: fixture.sampleAction,
					waitAction: fixture.waitAction, drainBy: at.Add(2 * time.Second),
				},
			}},
		})

		return fixture, sealed, at
	}

	for _, seal := range []struct {
		name string
		make func(*testing.T) (runningReducerFixture, supervisorState, time.Time)
	}{
		{name: "ordinary intent", make: newOrdinarySeal},
		{name: "emergency", make: newEmergencySeal},
	} {
		t.Run(seal.name, func(t *testing.T) {
			fixture, sealed, sealedAt := seal.make(t)
			validAt := sealedAt.Add(time.Second)
			valid := supervisorRunningFact{
				generation: fixture.generation, action: fixture.sampleAction,
				kind: supervisorRunningFuseObserved, at: validAt, rootLive: true, live: 100,
			}
			unchanged, actions := reduceSupervisor(sealed, supervisorEvent{
				kind: supervisorRunningObserved, generation: fixture.generation,
				at: validAt, drainBy: fixture.drainBy,
				running: &supervisorRunningBundle{
					generation: fixture.generation, sampleAction: fixture.sampleAction,
					waitAction: fixture.waitAction, facts: []supervisorRunningFact{valid},
				},
			})
			if len(actions) != 0 || !reflect.DeepEqual(unchanged, sealed) {
				t.Fatalf("valid sealed notification changed state: before=%#v after=%#v actions=%#v", sealed, unchanged, actions)
			}

			for _, malformed := range []struct {
				name   string
				event  time.Time
				mutate func(*supervisorRunningFact)
			}{
				{
					name: "stale fact generation", event: validAt,
					mutate: func(fact *supervisorRunningFact) { fact.generation-- },
				},
				{
					name: "wrong fact action", event: validAt,
					mutate: func(fact *supervisorRunningFact) { fact.action++ },
				},
				{name: "backward event instant", event: sealedAt.Add(-time.Nanosecond)},
			} {
				t.Run(malformed.name, func(t *testing.T) {
					fact := valid
					if malformed.mutate != nil {
						malformed.mutate(&fact)
					}
					assertSupervisorInvariant(t, func() {
						reduceSupervisor(sealed, supervisorEvent{
							kind: supervisorRunningObserved, generation: fixture.generation,
							at: malformed.event, drainBy: fixture.drainBy,
							running: &supervisorRunningBundle{
								generation: fixture.generation, sampleAction: fixture.sampleAction,
								waitAction: fixture.waitAction, facts: []supervisorRunningFact{fact},
							},
						})
					})
				})
			}
		})
	}
}

func TestSupervisorReducerRunningRejectsWrongGenerationAndActionTokens(t *testing.T) {
	fixture := newRunningReducerFixture(t, AutomaticProfile)
	t.Run("stale fact generation", func(t *testing.T) {
		fact := supervisorRunningFact{
			generation: fixture.generation - 1,
			action:     fixture.sampleAction,
			kind:       supervisorRunningFuseObserved,
			at:         fixture.startedAt.Add(time.Second),
			rootLive:   true,
			live:       65,
		}
		assertSupervisorInvariant(t, func() {
			fixture.reduceBundle(t, fixture.startedAt.Add(time.Second), []supervisorRunningFact{fact}, supervisorExitRecheck{})
		})
	})
	t.Run("wrong sample action", func(t *testing.T) {
		fact := supervisorRunningFact{
			generation: fixture.generation,
			action:     fixture.sampleAction + 1,
			kind:       supervisorRunningFuseObserved,
			at:         fixture.startedAt.Add(time.Second),
			rootLive:   true,
			live:       65,
		}
		assertSupervisorInvariant(t, func() {
			fixture.reduceBundle(t, fixture.startedAt.Add(time.Second), []supervisorRunningFact{fact}, supervisorExitRecheck{})
		})
	})
}

func TestSupervisorReducerRunningRejectsMalformedOwnershipFacts(t *testing.T) {
	launchBy := time.Unix(1_500, 0)
	for _, test := range []struct {
		name     string
		profile  Profile
		deadline time.Duration
	}{
		{name: "profile", deadline: 20 * time.Second},
		{name: "deadline", profile: AutomaticProfile},
	} {
		t.Run("registration "+test.name, func(t *testing.T) {
			assertSupervisorInvariant(t, func() {
				reduceSupervisor(supervisorState{}, supervisorEvent{
					kind: supervisorProspectiveRegistered, generation: 91,
					attempt: "attempt-invalid", at: launchBy.Add(-time.Second), launchBy: launchBy,
					profile: test.profile, commandDeadline: test.deadline,
				})
			})
		})
	}

	t.Run("missing deadline exit recheck", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, AutomaticProfile)
		assertSupervisorInvariant(t, func() {
			fixture.reduceBundle(t, fixture.deadlineAt, nil, supervisorExitRecheck{})
		})
	})

	t.Run("unobserved exit recheck carries status", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, AutomaticProfile)
		assertSupervisorInvariant(t, func() {
			fixture.reduceBundle(t, fixture.deadlineAt, nil, supervisorExitRecheck{
				performed: true, at: fixture.deadlineAt, code: 17,
			})
		})
	})

	t.Run("ordinary exit recheck carries launch action token", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, AutomaticProfile)
		launchAction := supervisorAttemptByGeneration(t, fixture.state, fixture.generation).launchAction
		assertSupervisorInvariant(t, func() {
			fixture.reduceBundle(t, fixture.deadlineAt, nil, supervisorExitRecheck{
				performed: true, at: fixture.deadlineAt, action: launchAction,
			})
		})
	})

	t.Run("fuse fact carries exit status", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, AutomaticProfile)
		fact := supervisorRunningFact{
			generation: fixture.generation,
			action:     fixture.sampleAction,
			kind:       supervisorRunningFuseObserved,
			at:         fixture.startedAt.Add(time.Second),
			rootLive:   true,
			live:       65,
			exitCode:   17,
		}
		assertSupervisorInvariant(t, func() {
			fixture.reduceBundle(
				t, fixture.startedAt.Add(time.Second), []supervisorRunningFact{fact}, supervisorExitRecheck{},
			)
		})
	})

	t.Run("observation failure carries exit status", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, AutomaticProfile)
		fact := supervisorRunningFact{
			generation: fixture.generation,
			action:     fixture.sampleAction,
			kind:       supervisorRunningObservationFailed,
			at:         fixture.startedAt.Add(time.Second),
			source:     supervisorObservationRunning,
			diagnostic: 1,
			exitSignal: 9,
		}
		assertSupervisorInvariant(t, func() {
			fixture.reduceBundle(
				t, fixture.startedAt.Add(time.Second), []supervisorRunningFact{fact}, supervisorExitRecheck{},
			)
		})
	})

	for _, test := range []struct {
		name         string
		rootLive     bool
		live         uint64
		liveNegative bool
	}{
		{name: "root not live", live: 65},
		{name: "zero", rootLive: true},
		{name: "negative", rootLive: true, liveNegative: true},
		{name: "overflow", rootLive: true, live: ^uint64(0)},
	} {
		t.Run("fuse "+test.name, func(t *testing.T) {
			fixture := newRunningReducerFixture(t, AutomaticProfile)
			fact := supervisorRunningFact{
				generation:   fixture.generation,
				action:       fixture.sampleAction,
				kind:         supervisorRunningFuseObserved,
				at:           fixture.startedAt.Add(time.Second),
				rootLive:     test.rootLive,
				live:         test.live,
				liveNegative: test.liveNegative,
			}
			assertSupervisorInvariant(t, func() {
				fixture.reduceBundle(
					t, fixture.startedAt.Add(time.Second), []supervisorRunningFact{fact}, supervisorExitRecheck{},
				)
			})
		})
	}
}

func TestSupervisorReducerRunningDelayedBoundarySnapshotPreservesReleaseIntervalFacts(t *testing.T) {
	const generation = attemptGeneration(81)
	launchBy := time.Unix(2_000, 0)
	state, launch := appendReducerLaunchWithFacts(
		t, supervisorState{}, generation, "attempt-delayed", AutomaticProfile, 20*time.Second, launchBy,
	)
	completionAt := launchBy.Add(-500 * time.Millisecond)
	completion := supervisorLaunchCompletion{
		generation: generation,
		action:     launch.token,
		at:         completionAt,
		kind:       supervisorLaunchReleased,
	}
	state, releasedActions := reduceSupervisor(state, supervisorEvent{
		kind: supervisorLaunchBoundary, generation: generation,
		at: launchBy, completion: &completion,
	})
	assertSupervisorActions(t, releasedActions,
		supervisorPublishOwned, supervisorWaitRoot, supervisorSampleRunning)
	attempt := supervisorAttemptByGeneration(t, state, generation)
	if !attempt.startedAt.Equal(completionAt) ||
		!attempt.deadlineAt.Equal(completionAt.Add(20*time.Second)) {
		t.Fatalf("delayed boundary moved the release cut: %#v", attempt)
	}
	factAt := completionAt.Add(250 * time.Millisecond)
	fact := supervisorRunningFact{
		generation: generation,
		action:     releasedActions[2].token,
		kind:       supervisorRunningFuseObserved,
		at:         factAt,
		rootLive:   true,
		live:       65,
	}
	next, actions := reduceSupervisor(state, supervisorEvent{
		kind: supervisorRunningObserved, generation: generation,
		at: launchBy, drainBy: launchBy.Add(10 * time.Second),
		running: &supervisorRunningBundle{
			generation:   generation,
			sampleAction: releasedActions[2].token,
			waitAction:   releasedActions[1].token,
			facts:        []supervisorRunningFact{fact},
		},
	})
	assertSupervisorActions(t, actions, supervisorForceOwned)
	intent := supervisorAttemptByGeneration(t, next, generation).intent
	if intent.kind != supervisorIntentFuse || !intent.at.Equal(factAt) ||
		intent.count != (supervisorObservedCount{present: true, value: 65}) {
		t.Fatalf("release-interval fact was erased by boundary delivery: %#v", intent)
	}
}

func TestSupervisorReducerRunningObservationFailuresRetainIndependentDiagnostics(t *testing.T) {
	t.Run("root exit wins while retaining both failure diagnostics", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, AutomaticProfile)
		at := fixture.startedAt.Add(time.Second)
		facts := []supervisorRunningFact{
			{
				generation: fixture.generation, action: fixture.sampleAction,
				kind: supervisorRunningObservationFailed, at: at,
				source: supervisorObservationRunning, diagnostic: 202,
			},
			{
				generation: fixture.generation, action: fixture.waitAction,
				kind: supervisorRunningRootExited, at: at, exitCode: 17,
			},
			{
				generation: fixture.generation, action: fixture.waitAction,
				kind: supervisorRunningObservationFailed, at: at,
				source: supervisorObservationWait, diagnostic: 101,
			},
		}
		next, actions := fixture.reduceBundle(t, at, facts, supervisorExitRecheck{})
		assertSupervisorActions(t, actions, supervisorObserveEmptiness)
		intent := supervisorAttemptByGeneration(t, next, fixture.generation).intent
		if intent.kind != supervisorIntentRootExit || intent.exitCode != 17 ||
			intent.diagnostics.wait != 101 || intent.diagnostics.running != 202 {
			t.Fatalf("root-exit tie lost independent diagnostics: %#v", intent)
		}
	})

	t.Run("automatic composes wait and running failures with wait canonical", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, AutomaticProfile)
		at := fixture.startedAt.Add(time.Second)
		facts := []supervisorRunningFact{
			{
				generation: fixture.generation, action: fixture.sampleAction,
				kind: supervisorRunningObservationFailed, at: at,
				source: supervisorObservationRunning, diagnostic: 202,
			},
			{
				generation: fixture.generation, action: fixture.waitAction,
				kind: supervisorRunningObservationFailed, at: at,
				source: supervisorObservationWait, diagnostic: 101,
			},
		}
		next, actions := fixture.reduceBundle(t, at, facts, supervisorExitRecheck{})
		assertSupervisorActions(t, actions, supervisorForceOwned)
		intent := supervisorAttemptByGeneration(t, next, fixture.generation).intent
		if intent.kind != supervisorIntentObservationFailure ||
			intent.observationSource != supervisorObservationWait ||
			intent.diagnostics.wait != 101 || intent.diagnostics.running != 202 {
			t.Fatalf("independent observation diagnostics = %#v", intent)
		}
	})

	t.Run("serial accepts wait failure", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, SerialProfile)
		at := fixture.startedAt.Add(time.Second)
		fact := supervisorRunningFact{
			generation: fixture.generation, action: fixture.waitAction,
			kind: supervisorRunningObservationFailed, at: at,
			source: supervisorObservationWait, diagnostic: 303,
		}
		next, _ := fixture.reduceBundle(t, at, []supervisorRunningFact{fact}, supervisorExitRecheck{})
		intent := supervisorAttemptByGeneration(t, next, fixture.generation).intent
		if intent.observationSource != supervisorObservationWait || intent.diagnostics.wait != 303 {
			t.Fatalf("serial wait failure = %#v", intent)
		}
	})

	t.Run("serial rejects running failure", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, SerialProfile)
		at := fixture.startedAt.Add(time.Second)
		fact := supervisorRunningFact{
			generation: fixture.generation, action: fixture.waitAction,
			kind: supervisorRunningObservationFailed, at: at,
			source: supervisorObservationRunning, diagnostic: 404,
		}
		assertSupervisorInvariant(t, func() {
			fixture.reduceBundle(t, at, []supervisorRunningFact{fact}, supervisorExitRecheck{})
		})
	})

	t.Run("duplicate same-source failure at equality", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, AutomaticProfile)
		at := fixture.startedAt.Add(time.Second)
		facts := []supervisorRunningFact{
			{
				generation: fixture.generation, action: fixture.waitAction,
				kind: supervisorRunningObservationFailed, at: at,
				source: supervisorObservationWait, diagnostic: 1,
			},
			{
				generation: fixture.generation, action: fixture.waitAction,
				kind: supervisorRunningObservationFailed, at: at,
				source: supervisorObservationWait, diagnostic: 2,
			},
		}
		assertSupervisorInvariant(t, func() {
			fixture.reduceBundle(t, at, facts, supervisorExitRecheck{})
		})
	})
}

func TestSupervisorReducerRunningCanonicalSameKindFactsAndLocalDrainBound(t *testing.T) {
	t.Run("equal stops select earliest DrainBy", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, AutomaticProfile)
		at := fixture.startedAt.Add(time.Second)
		earlier := at.Add(time.Second)
		facts := []supervisorRunningFact{
			{
				generation: fixture.generation, kind: supervisorRunningStopRequested, at: at,
				stop: StopRequest{At: at, DrainBy: at.Add(2 * time.Second)},
			},
			{
				generation: fixture.generation, kind: supervisorRunningStopRequested, at: at,
				stop: StopRequest{At: at, DrainBy: earlier},
			},
		}
		next, actions := fixture.reduceBundle(t, at, facts, supervisorExitRecheck{})
		intent := supervisorAttemptByGeneration(t, next, fixture.generation).intent
		if intent.kind != supervisorIntentStop || !intent.stop.DrainBy.Equal(earlier) ||
			!actions[0].drainBy.Equal(earlier) {
			t.Fatalf("equal stop canonicalization = %#v actions=%#v", intent, actions)
		}
	})

	t.Run("duplicate root exits at equality", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, AutomaticProfile)
		at := fixture.startedAt.Add(time.Second)
		fact := supervisorRunningFact{
			generation: fixture.generation, action: fixture.waitAction,
			kind: supervisorRunningRootExited, at: at,
		}
		assertSupervisorInvariant(t, func() {
			fixture.reduceBundle(t, at, []supervisorRunningFact{fact, fact}, supervisorExitRecheck{})
		})
	})

	t.Run("non-stop local DrainBy must follow intent", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, AutomaticProfile)
		at := fixture.startedAt.Add(time.Second)
		fact := supervisorRunningFact{
			generation: fixture.generation, action: fixture.sampleAction,
			kind: supervisorRunningFuseObserved, at: at, rootLive: true, live: 65,
		}
		assertSupervisorInvariant(t, func() {
			reduceSupervisor(fixture.state, supervisorEvent{
				kind: supervisorRunningObserved, generation: fixture.generation,
				at: at, drainBy: at,
				running: &supervisorRunningBundle{
					generation: fixture.generation, sampleAction: fixture.sampleAction,
					waitAction: fixture.waitAction, facts: []supervisorRunningFact{fact},
				},
			})
		})
	})
}

func TestSupervisorReducerRunningEmergencyConsumesSnapshotAndSealsOrdinaryIntent(t *testing.T) {
	t.Run("snapshot preserves earlier exit and emits only emergency force", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, AutomaticProfile)
		emergencyAt := fixture.startedAt.Add(5 * time.Second)
		emergencyDrainBy := emergencyAt.Add(5 * time.Second)
		localDrainBy := emergencyAt.Add(10 * time.Second)
		factAt := emergencyAt.Add(-time.Second)
		fact := supervisorRunningFact{
			generation: fixture.generation, action: fixture.waitAction,
			kind: supervisorRunningRootExited, at: factAt, exitCode: 17,
		}
		next, actions := reduceSupervisor(fixture.state, supervisorEvent{
			kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: emergencyDrainBy,
			emergencySnapshots: []supervisorEmergencySnapshot{{
				generation: fixture.generation,
				running: &supervisorRunningBundle{
					generation: fixture.generation, sampleAction: fixture.sampleAction,
					waitAction: fixture.waitAction, drainBy: localDrainBy,
					facts: []supervisorRunningFact{fact},
				},
			}},
		})
		assertSupervisorActions(t, actions, supervisorForceOwned)
		attempt := supervisorAttemptByGeneration(t, next, fixture.generation)
		if attempt.phase != supervisorEmergencyDraining || attempt.intent.kind != supervisorIntentRootExit ||
			!attempt.intent.at.Equal(factAt) || !attempt.intent.drainBy.Equal(localDrainBy) ||
			attempt.pendingAction != (supervisorPendingAction{kind: actions[0].kind, token: actions[0].token}) ||
			!actions[0].at.Equal(emergencyAt) || !actions[0].drainBy.Equal(emergencyDrainBy) {
			t.Fatalf("emergency running snapshot = %#v actions=%#v", attempt, actions)
		}

		lateFact := supervisorRunningFact{
			generation: fixture.generation, action: fixture.sampleAction,
			kind: supervisorRunningFuseObserved, at: emergencyAt.Add(time.Second),
			rootLive: true, live: 100,
		}
		sealed, lateActions := reduceSupervisor(next, supervisorEvent{
			kind: supervisorRunningObserved, generation: fixture.generation,
			at: lateFact.at, drainBy: localDrainBy,
			running: &supervisorRunningBundle{
				generation: fixture.generation, sampleAction: fixture.sampleAction,
				waitAction: fixture.waitAction, facts: []supervisorRunningFact{lateFact},
			},
		})
		if len(lateActions) != 0 ||
			!reflect.DeepEqual(supervisorAttemptByGeneration(t, sealed, fixture.generation).intent, attempt.intent) {
			t.Fatalf("post-emergency notification escaped seal: state=%#v actions=%#v", sealed, lateActions)
		}
	})

	t.Run("empty snapshot latches private runtime emergency intent", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, AutomaticProfile)
		emergencyAt := fixture.startedAt.Add(5 * time.Second)
		drainBy := emergencyAt.Add(5 * time.Second)
		next, actions := reduceSupervisor(fixture.state, supervisorEvent{
			kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: drainBy,
			emergencySnapshots: []supervisorEmergencySnapshot{{
				generation: fixture.generation,
				running: &supervisorRunningBundle{
					generation: fixture.generation, sampleAction: fixture.sampleAction,
					waitAction: fixture.waitAction,
				},
			}},
		})
		assertSupervisorActions(t, actions, supervisorForceOwned)
		attempt := supervisorAttemptByGeneration(t, next, fixture.generation)
		wantIntent := supervisorRunningIntent{
			latched: true, kind: supervisorIntentRuntimeEmergency,
			at: emergencyAt, drainBy: drainBy,
			duration: emergencyAt.Sub(fixture.startedAt),
		}
		if attempt.phase != supervisorEmergencyDraining || !reflect.DeepEqual(attempt.intent, wantIntent) ||
			attempt.pendingAction != (supervisorPendingAction{kind: actions[0].kind, token: actions[0].token}) ||
			!reflect.DeepEqual(actions[0].intent, wantIntent) ||
			!attempt.drain.effectiveDrainBy.Equal(drainBy) || !actions[0].drainBy.Equal(drainBy) {
			t.Fatalf("runtime emergency fallback = %#v actions=%#v, want intent=%#v", attempt, actions, wantIntent)
		}

		lateFact := supervisorRunningFact{
			generation: fixture.generation, action: fixture.sampleAction,
			kind: supervisorRunningFuseObserved, at: emergencyAt.Add(time.Second),
			rootLive: true, live: 100,
		}
		sealed, lateActions := reduceSupervisor(next, supervisorEvent{
			kind: supervisorRunningObserved, generation: fixture.generation,
			at: lateFact.at, drainBy: drainBy,
			running: &supervisorRunningBundle{
				generation: fixture.generation, sampleAction: fixture.sampleAction,
				waitAction: fixture.waitAction, facts: []supervisorRunningFact{lateFact},
			},
		})
		if len(lateActions) != 0 || !reflect.DeepEqual(sealed, next) {
			t.Fatalf("valid late fact changed runtime-emergency intent: before=%#v after=%#v actions=%#v", next, sealed, lateActions)
		}
	})

	for _, test := range []struct {
		name string
		kind supervisorRunningFactKind
	}{
		{name: "stop", kind: supervisorRunningStopRequested},
		{name: "observation failure", kind: supervisorRunningObservationFailed},
	} {
		t.Run("ordinary equality wins "+test.name, func(t *testing.T) {
			fixture := newRunningReducerFixture(t, AutomaticProfile)
			emergencyAt := fixture.startedAt.Add(5 * time.Second)
			emergencyDrainBy := emergencyAt.Add(5 * time.Second)
			localDrainBy := emergencyAt.Add(10 * time.Second)
			fact := supervisorRunningFact{
				generation: fixture.generation, kind: test.kind, at: emergencyAt,
			}
			want := supervisorIntentStop
			if test.kind == supervisorRunningStopRequested {
				fact.stop = StopRequest{At: emergencyAt, DrainBy: localDrainBy}
			} else {
				fact.action = fixture.sampleAction
				fact.source = supervisorObservationRunning
				fact.diagnostic = 77
				want = supervisorIntentObservationFailure
			}
			next, actions := reduceSupervisor(fixture.state, supervisorEvent{
				kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: emergencyDrainBy,
				emergencySnapshots: []supervisorEmergencySnapshot{{
					generation: fixture.generation,
					running: &supervisorRunningBundle{
						generation: fixture.generation, sampleAction: fixture.sampleAction,
						waitAction: fixture.waitAction, drainBy: localDrainBy,
						facts: []supervisorRunningFact{fact},
					},
				}},
			})
			assertSupervisorActions(t, actions, supervisorForceOwned)
			attempt := supervisorAttemptByGeneration(t, next, fixture.generation)
			if attempt.intent.kind != want || !attempt.intent.at.Equal(emergencyAt) ||
				attempt.intent.kind == supervisorIntentRuntimeEmergency ||
				!reflect.DeepEqual(actions[0].intent, attempt.intent) {
				t.Fatalf("ordinary equality lost to runtime fallback: %#v actions=%#v", attempt, actions)
			}
		})
	}

	t.Run("already latched intent is preserved", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, AutomaticProfile)
		intentAt := fixture.startedAt.Add(time.Second)
		fact := supervisorRunningFact{
			generation: fixture.generation, action: fixture.sampleAction,
			kind: supervisorRunningFuseObserved, at: intentAt, rootLive: true, live: 65,
		}
		latched, intentActions := fixture.reduceBundle(
			t, intentAt, []supervisorRunningFact{fact}, supervisorExitRecheck{},
		)
		beforeAttempt := supervisorAttemptByGeneration(t, latched, fixture.generation)
		before := beforeAttempt.intent
		if beforeAttempt.pendingAction != (supervisorPendingAction{
			kind: intentActions[0].kind, token: intentActions[0].token,
		}) {
			t.Fatalf("latched intent did not retain first drain action: %#v actions=%#v", beforeAttempt, intentActions)
		}
		emergencyAt := intentAt.Add(time.Second)
		drainBy := emergencyAt.Add(time.Second)
		next, actions := reduceSupervisor(latched, supervisorEvent{
			kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: drainBy,
			emergencySnapshots: []supervisorEmergencySnapshot{{
				generation: fixture.generation,
				running: &supervisorRunningBundle{
					generation: fixture.generation, sampleAction: fixture.sampleAction,
					waitAction: fixture.waitAction, drainBy: fixture.drainBy,
				},
			}},
		})
		if len(actions) != 0 {
			t.Fatalf("emergency dispatched competing force: %#v", actions)
		}
		after := supervisorAttemptByGeneration(t, next, fixture.generation)
		if after.phase != supervisorEmergencyDraining || !reflect.DeepEqual(after.intent, before) ||
			after.pendingAction != beforeAttempt.pendingAction ||
			!next.emergency.active || !next.emergency.at.Equal(emergencyAt) ||
			!next.emergency.drainBy.Equal(drainBy) {
			t.Fatalf("emergency rewrote accepted intent: before=%#v after=%#v", before, after)
		}
	})

	t.Run("already latched intent cannot postdate emergency", func(t *testing.T) {
		fixture := newRunningReducerFixture(t, AutomaticProfile)
		intentAt := fixture.startedAt.Add(2 * time.Second)
		fact := supervisorRunningFact{
			generation: fixture.generation, action: fixture.sampleAction,
			kind: supervisorRunningFuseObserved, at: intentAt, rootLive: true, live: 65,
		}
		latched, _ := fixture.reduceBundle(
			t, intentAt, []supervisorRunningFact{fact}, supervisorExitRecheck{},
		)
		emergencyAt := intentAt.Add(-time.Nanosecond)
		assertSupervisorInvariant(t, func() {
			reduceSupervisor(latched, supervisorEvent{
				kind: supervisorEmergencyStarted, at: emergencyAt,
				drainBy: emergencyAt.Add(time.Second),
				emergencySnapshots: []supervisorEmergencySnapshot{{
					generation: fixture.generation,
					running: &supervisorRunningBundle{
						generation: fixture.generation, sampleAction: fixture.sampleAction,
						waitAction: fixture.waitAction, drainBy: fixture.drainBy,
					},
				}},
			})
		})
	})
}

type runningFactSpec struct {
	kind     supervisorRunningFactKind
	offset   time.Duration
	live     uint64
	rootLive bool
}

type runningReducerFixture struct {
	state        supervisorState
	generation   attemptGeneration
	startedAt    time.Time
	deadlineAt   time.Time
	drainBy      time.Time
	sampleAction supervisorActionToken
	waitAction   supervisorActionToken
}

func newRunningReducerFixture(t *testing.T, profile Profile) runningReducerFixture {
	t.Helper()
	const generation = attemptGeneration(71)
	launchBy := time.Unix(900, 0)
	state, launch := appendReducerLaunchWithFacts(
		t, supervisorState{}, generation, "attempt-running", profile, 20*time.Second, launchBy,
	)
	completion := supervisorLaunchCompletion{
		generation: generation,
		action:     launch.token,
		at:         launchBy.Add(-time.Nanosecond),
		kind:       supervisorLaunchReleased,
	}
	state, actions := reduceSupervisor(state, supervisorEvent{
		kind: supervisorLaunchCompleted, generation: generation,
		at: completion.at, completion: &completion,
	})
	want := []supervisorActionKind{supervisorPublishOwned, supervisorWaitRoot, supervisorSampleRunning}
	if profile == SerialProfile {
		want = []supervisorActionKind{supervisorPublishOwned, supervisorWaitRoot}
	}
	assertSupervisorActions(t, actions, want...)
	startedAt := completion.at
	fixture := runningReducerFixture{
		state:      state,
		generation: generation,
		startedAt:  startedAt,
		deadlineAt: startedAt.Add(20 * time.Second),
		drainBy:    startedAt.Add(30 * time.Second),
	}
	for _, action := range actions {
		switch action.kind {
		case supervisorSampleRunning:
			fixture.sampleAction = action.token
		case supervisorWaitRoot:
			fixture.waitAction = action.token
		}
	}

	return fixture
}

func (fixture runningReducerFixture) runningFacts(specs []runningFactSpec) []supervisorRunningFact {
	facts := make([]supervisorRunningFact, len(specs))
	for index, spec := range specs {
		at := fixture.deadlineAt.Add(spec.offset)
		action := fixture.sampleAction
		if spec.kind == supervisorRunningRootExited {
			action = fixture.waitAction
		}
		fact := supervisorRunningFact{
			generation: fixture.generation,
			action:     action,
			kind:       spec.kind,
			at:         at,
			rootLive:   spec.rootLive,
			live:       spec.live,
		}
		if spec.kind == supervisorRunningObservationFailed {
			fact.source = supervisorObservationRunning
			fact.diagnostic = supervisorDiagnosticRef(index + 1)
		}
		if spec.kind == supervisorRunningStopRequested {
			fact.action = 0
			fact.stop = StopRequest{At: at, DrainBy: fixture.drainBy}
		}
		facts[index] = fact
	}

	return facts
}

func (fixture runningReducerFixture) reduceBundle(
	t *testing.T,
	at time.Time,
	facts []supervisorRunningFact,
	recheck supervisorExitRecheck,
) (supervisorState, []supervisorAction) {
	t.Helper()

	return reduceSupervisor(fixture.state, supervisorEvent{
		kind: supervisorRunningObserved, generation: fixture.generation,
		at: at, drainBy: fixture.drainBy,
		running: &supervisorRunningBundle{
			generation:   fixture.generation,
			sampleAction: fixture.sampleAction,
			waitAction:   fixture.waitAction,
			facts:        facts,
			exitRecheck:  recheck,
		},
	})
}

func (fixture runningReducerFixture) drainByFor(intent supervisorRunningIntent) time.Time {
	if intent.kind == supervisorIntentStop {
		return intent.stop.DrainBy
	}

	return fixture.drainBy
}
