package ooze

import (
	"reflect"
	"testing"
	"time"
)

func TestSupervisorReducerLaunchCompletionBeforeAndAtBoundary(t *testing.T) {
	launchBy := time.Unix(100, 0)
	for _, test := range []struct {
		name           string
		completionKind supervisorLaunchCompletionKind
		failure        LaunchFailure
		completion     time.Time
		boundary       bool
		wantActions    []supervisorActionKind
		wantPhase      supervisorAttemptPhase
	}{
		{
			name: "not released before", completionKind: supervisorLaunchProvenNotReleased,
			failure:     LaunchFailed,
			completion:  launchBy.Add(-time.Nanosecond),
			wantActions: []supervisorActionKind{supervisorPublishNotReleased},
			wantPhase:   supervisorLaunchClosedNotReleased,
		},
		{
			name: "owned before", completionKind: supervisorLaunchReleased,
			completion: launchBy.Add(-time.Nanosecond),
			wantActions: []supervisorActionKind{
				supervisorPublishOwned, supervisorWaitRoot, supervisorSampleRunning,
			},
			wantPhase: supervisorRunning,
		},
		{
			name: "not released at equality", completionKind: supervisorLaunchProvenNotReleased,
			failure:    LaunchFailed,
			completion: launchBy, boundary: true,
			wantActions: []supervisorActionKind{supervisorPublishNotReleased},
			wantPhase:   supervisorLaunchClosedNotReleased,
		},
		{
			name: "owned at equality", completionKind: supervisorLaunchReleased,
			completion: launchBy, boundary: true,
			wantActions: []supervisorActionKind{
				supervisorPublishOwned, supervisorWaitRoot, supervisorSampleRunning,
			},
			wantPhase: supervisorRunning,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, launch := registeredReducerLaunch(t, 11, "attempt-a", launchBy)
			completion := supervisorLaunchCompletion{
				generation: 11,
				action:     launch.token,
				at:         test.completion,
				kind:       test.completionKind,
				failure:    test.failure,
			}
			event := supervisorEvent{
				kind:       supervisorLaunchCompleted,
				generation: 11,
				at:         test.completion,
				completion: &completion,
			}
			if test.boundary {
				event.kind = supervisorLaunchBoundary
				event.at = launchBy
			}

			next, actions := reduceSupervisor(state, event)
			assertSupervisorActions(t, actions, test.wantActions...)
			if actions[0].generation != 11 || actions[0].token <= launch.token ||
				actions[0].launchKind != test.completionKind || actions[0].launchFailure != test.failure {
				t.Fatalf("classified launch action = %#v, launch token %d", actions[0], launch.token)
			}
			attempt := supervisorAttemptByGeneration(t, next, 11)
			if attempt.phase != test.wantPhase {
				t.Fatalf("launch phase = %d, want %d", attempt.phase, test.wantPhase)
			}
			if test.completionKind == supervisorLaunchReleased &&
				(!attempt.startedAt.Equal(test.completion) ||
					!attempt.deadlineAt.Equal(test.completion.Add(20*time.Second)) ||
					attempt.waitAction != actions[1].token || attempt.sampleAction != actions[2].token) {
				t.Fatalf("released completion did not atomically enter running: %#v actions=%#v", attempt, actions)
			}
		})
	}

	t.Run("published before but notification delayed until boundary", func(t *testing.T) {
		state, launch := registeredReducerLaunch(t, 12, "attempt-b", launchBy)
		completion := supervisorLaunchCompletion{
			generation: 12,
			action:     launch.token,
			at:         launchBy.Add(-time.Nanosecond),
			kind:       supervisorLaunchProvenNotReleased,
			failure:    LaunchFailed,
		}
		next, actions := reduceSupervisor(state, supervisorEvent{
			kind: supervisorLaunchBoundary, generation: 12, at: launchBy, completion: &completion,
		})
		assertSupervisorActions(t, actions, supervisorPublishNotReleased)
		if supervisorAttemptByGeneration(t, next, 12).phase != supervisorLaunchClosedNotReleased {
			t.Fatalf("delayed notification lost the serialized boundary snapshot: %#v", next)
		}
	})
}

func TestSupervisorReducerLaunchNilBoundaryRevokesAndLateCompletionRetainsCustody(t *testing.T) {
	launchBy := time.Unix(200, 0)
	for _, test := range []struct {
		name           string
		generation     attemptGeneration
		completionKind supervisorLaunchCompletionKind
		failure        LaunchFailure
		wantActions    []supervisorActionKind
		wantPhase      supervisorAttemptPhase
	}{
		{
			name: "late proven no release closes prospective custody", generation: 21,
			completionKind: supervisorLaunchProvenNotReleased, failure: LaunchFailed,
			wantActions: []supervisorActionKind{supervisorCloseProspective},
			wantPhase:   supervisorLaunchClosedNotReleased,
		},
		{
			name: "late unavoidable release is adopted and forced", generation: 22,
			completionKind: supervisorLaunchReleased,
			wantActions:    []supervisorActionKind{supervisorAdoptOwned, supervisorForceOwned},
			wantPhase:      supervisorLaunchOwned,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, launch := registeredReducerLaunch(t, test.generation, "attempt-late", launchBy)
			unconfirmed, boundaryActions := reduceSupervisor(state, supervisorEvent{
				kind: supervisorLaunchBoundary, generation: test.generation, at: launchBy,
			})
			original := supervisorAttemptByGeneration(t, state, test.generation)
			if original.phase != supervisorLaunchEstablishing || original.releaseRevoked {
				t.Fatalf("reducer mutated its input state: %#v", original)
			}
			assertSupervisorActions(t, boundaryActions,
				supervisorRevokeLaunchRelease, supervisorPublishLaunchUnconfirmed)
			attempt := supervisorAttemptByGeneration(t, unconfirmed, test.generation)
			if attempt.phase != supervisorLaunchReportedUnconfirmed || !attempt.releaseRevoked {
				t.Fatalf("nil boundary did not latch custody/revocation: %#v", attempt)
			}
			if boundaryActions[0].token <= launch.token ||
				boundaryActions[1].token <= boundaryActions[0].token {
				t.Fatalf("nil boundary actions = %#v", boundaryActions)
			}

			completion := supervisorLaunchCompletion{
				generation: test.generation,
				action:     launch.token,
				at:         launchBy.Add(time.Nanosecond),
				kind:       test.completionKind,
				failure:    test.failure,
			}
			lateEvent := supervisorEvent{
				kind: supervisorLaunchCompleted, generation: test.generation,
				at: completion.at, completion: &completion,
			}
			if test.completionKind == supervisorLaunchReleased {
				lateEvent.drainBy = launchBy.Add(time.Second)
			}
			next, lateActions := reduceSupervisor(unconfirmed, lateEvent)
			assertSupervisorActions(t, lateActions, test.wantActions...)
			if lateActions[0].token <= boundaryActions[len(boundaryActions)-1].token {
				t.Fatalf("late action token did not advance: boundary=%#v late=%#v", boundaryActions, lateActions)
			}
			if supervisorAttemptByGeneration(t, next, test.generation).phase != test.wantPhase {
				t.Fatalf("late completion phase = %#v", next)
			}
		})
	}
}

func TestSupervisorReducerLaunchEmergencyUsesSerializedCompletionOrNil(t *testing.T) {
	launchBy := time.Unix(300, 0)
	emergencyAt := launchBy.Add(-time.Second)
	for _, test := range []struct {
		name           string
		generation     attemptGeneration
		hasCompletion  bool
		completionKind supervisorLaunchCompletionKind
		failure        LaunchFailure
		wantActions    []supervisorActionKind
		wantPhase      supervisorAttemptPhase
	}{
		{
			name: "nil snapshot revokes and reports unconfirmed", generation: 31,
			wantActions: []supervisorActionKind{
				supervisorRevokeLaunchRelease, supervisorPublishLaunchUnconfirmed,
			},
			wantPhase: supervisorLaunchReportedUnconfirmed,
		},
		{
			name: "proven no release snapshot closes normally", generation: 32,
			hasCompletion: true, completionKind: supervisorLaunchProvenNotReleased,
			failure: LaunchFailed,
			wantActions: []supervisorActionKind{
				supervisorPublishNotReleased, supervisorSettleEmergency,
			},
			wantPhase: supervisorLaunchClosedNotReleased,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, launch := registeredReducerLaunch(t, test.generation, "attempt-emergency", launchBy)
			var snapshot *supervisorLaunchCompletion
			if test.hasCompletion {
				snapshot = &supervisorLaunchCompletion{
					generation: test.generation,
					action:     launch.token,
					at:         emergencyAt,
					kind:       test.completionKind,
					failure:    test.failure,
				}
			}
			snapshots := []supervisorEmergencySnapshot{{
				generation: test.generation,
				completion: snapshot,
			}}
			next, actions := reduceSupervisor(state, supervisorEvent{
				kind: supervisorEmergencyStarted, at: emergencyAt,
				drainBy: launchBy.Add(time.Second), emergencySnapshots: snapshots,
			})
			assertSupervisorActions(t, actions, test.wantActions...)
			if test.hasCompletion &&
				(actions[0].launchKind != test.completionKind || actions[0].launchFailure != test.failure) {
				t.Fatalf("emergency normalized completion = %#v", actions[0])
			}
			assertStrictlyIncreasingActionTokens(t, append([]supervisorAction{launch}, actions...))
			attempt := supervisorAttemptByGeneration(t, next, test.generation)
			if attempt.phase != test.wantPhase {
				t.Fatalf("emergency launch phase = %d, want %d", attempt.phase, test.wantPhase)
			}
		})
	}
}

func TestSupervisorReducerLaunchEmergencyReleasedSnapshotSelectsOwnedIntentThroughCut(t *testing.T) {
	launchBy := time.Unix(325, 0)
	emergencyAt := launchBy.Add(-100 * time.Millisecond)
	emergencyDrainBy := emergencyAt.Add(5 * time.Second)
	releasedAt := emergencyAt.Add(-500 * time.Millisecond)
	for _, test := range []struct {
		name            string
		commandDeadline time.Duration
		rootOffset      time.Duration
		exitObserved    bool
		exitCode        int
		wantKind        supervisorRunningIntentKind
	}{
		{
			name: "runtime emergency fallback", commandDeadline: 20 * time.Second,
			wantKind: supervisorIntentRuntimeEmergency,
		},
		{
			name: "root completion before deadline beats fallback", commandDeadline: 20 * time.Second,
			rootOffset:   100 * time.Millisecond,
			exitObserved: true, exitCode: 23, wantKind: supervisorIntentRootExit,
		},
		{
			name: "root completion at deadline wins equality", commandDeadline: 200 * time.Millisecond,
			rootOffset:   200 * time.Millisecond,
			exitObserved: true, exitCode: 29, wantKind: supervisorIntentRootExit,
		},
		{
			name: "root completion after deadline loses to deadline", commandDeadline: 200 * time.Millisecond,
			rootOffset:   400 * time.Millisecond,
			exitObserved: true, exitCode: 31, wantKind: supervisorIntentDeadline,
		},
		{
			name: "unobserved at deadline selects deadline", commandDeadline: emergencyAt.Sub(releasedAt),
			wantKind: supervisorIntentDeadline,
		},
		{
			name: "unobserved after deadline selects deadline", commandDeadline: 200 * time.Millisecond,
			wantKind: supervisorIntentDeadline,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			const generation = attemptGeneration(34)
			state, launch := appendReducerLaunchWithFacts(
				t, supervisorState{}, generation, "attempt-emergency-owned",
				AutomaticProfile, test.commandDeadline, launchBy,
			)
			completion := supervisorLaunchCompletion{
				generation: generation, action: launch.token,
				at: releasedAt, kind: supervisorLaunchReleased,
			}
			running := &supervisorRunningBundle{generation: generation}
			if test.wantKind != supervisorIntentRuntimeEmergency {
				running.drainBy = emergencyAt.Add(10 * time.Second)
			}
			running.exitRecheck = supervisorExitRecheck{
				performed: true, observed: test.exitObserved,
				at: emergencyAt, action: launch.token,
			}
			if test.exitObserved {
				running.exitRecheck.at = releasedAt.Add(test.rootOffset)
				running.exitRecheck.code = test.exitCode
			}
			next, actions := reduceSupervisorMustAccept(t, state, supervisorEvent{
				kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: emergencyDrainBy,
				emergencySnapshots: []supervisorEmergencySnapshot{{
					generation: generation, completion: &completion, running: running,
				}},
			})
			assertSupervisorActions(t, actions, supervisorPublishOwned, supervisorForceOwned)
			attempt := supervisorAttemptByGeneration(t, next, generation)
			wantIntent := supervisorRunningIntent{
				latched: true, kind: test.wantKind,
				at: emergencyAt, drainBy: emergencyDrainBy,
				duration: emergencyAt.Sub(releasedAt),
			}
			if test.wantKind == supervisorIntentRootExit {
				wantIntent.at = releasedAt.Add(test.rootOffset)
				wantIntent.drainBy = running.drainBy
				wantIntent.duration = test.rootOffset
				wantIntent.exitCode = test.exitCode
			} else if test.wantKind == supervisorIntentDeadline {
				wantIntent.at = releasedAt.Add(test.commandDeadline)
				wantIntent.drainBy = running.drainBy
				wantIntent.duration = test.commandDeadline
			}
			if attempt.phase != supervisorEmergencyDraining || !attempt.startedAt.Equal(releasedAt) ||
				!attempt.deadlineAt.Equal(releasedAt.Add(test.commandDeadline)) ||
				!reflect.DeepEqual(attempt.intent, wantIntent) ||
				!reflect.DeepEqual(actions[1].intent, wantIntent) ||
				attempt.pendingAction != (supervisorPendingAction{kind: actions[1].kind, token: actions[1].token}) ||
				!attempt.drain.effectiveDrainBy.Equal(emergencyDrainBy) ||
				!actions[1].drainBy.Equal(emergencyDrainBy) {
				t.Fatalf("serialized emergency ownership = %#v actions=%#v, want intent=%#v", attempt, actions, wantIntent)
			}
		})
	}
}

func TestSupervisorReducerLaunchEmergencyReleasedSnapshotRequiresDedicatedSelectorShape(t *testing.T) {
	launchBy := time.Unix(340, 0)
	emergencyAt := launchBy.Add(-100 * time.Millisecond)
	releasedAt := emergencyAt.Add(-500 * time.Millisecond)
	factAt := releasedAt.Add(100 * time.Millisecond)
	const generation = attemptGeneration(35)
	reduceMalformed := func(
		t *testing.T,
		mutate func(*supervisorRunningBundle, supervisorActionToken, supervisorActionToken),
	) {
		t.Helper()
		state, staleLaunch := appendReducerLaunchWithFacts(
			t, supervisorState{}, generation-1, "attempt-emergency-stale-token",
			AutomaticProfile, 20*time.Second, launchBy,
		)
		staleCompletion := supervisorLaunchCompletion{
			generation: generation - 1, action: staleLaunch.token,
			at: releasedAt, kind: supervisorLaunchProvenNotReleased, failure: LaunchFailed,
		}
		state, _ = reduceSupervisor(state, supervisorEvent{
			kind: supervisorLaunchCompleted, generation: generation - 1,
			at: staleCompletion.at, completion: &staleCompletion,
		})
		state, launch := appendReducerLaunchWithFacts(
			t, state, generation, "attempt-emergency-unowned-fact",
			AutomaticProfile, 20*time.Second, launchBy,
		)
		completion := supervisorLaunchCompletion{
			generation: generation, action: launch.token,
			at: releasedAt, kind: supervisorLaunchReleased,
		}
		running := supervisorRunningBundle{
			generation: generation,
			exitRecheck: supervisorExitRecheck{
				performed: true, at: emergencyAt, action: launch.token,
			},
		}
		mutate(&running, launch.token, staleLaunch.token)
		assertSupervisorInvariant(t, func() {
			reduceSupervisor(state, supervisorEvent{
				kind: supervisorEmergencyStarted, at: emergencyAt,
				drainBy: emergencyAt.Add(5 * time.Second),
				emergencySnapshots: []supervisorEmergencySnapshot{{
					generation: generation, completion: &completion, running: &running,
				}},
			})
		})
	}

	t.Run("missing root completion snapshot", func(t *testing.T) {
		reduceMalformed(t, func(running *supervisorRunningBundle, _, _ supervisorActionToken) {
			running.exitRecheck = supervisorExitRecheck{}
		})
	})
	t.Run("unobserved snapshot is not stamped at emergency cut", func(t *testing.T) {
		reduceMalformed(t, func(running *supervisorRunningBundle, _, _ supervisorActionToken) {
			running.exitRecheck.at = emergencyAt.Add(-time.Nanosecond)
		})
	})
	t.Run("unobserved snapshot carries status", func(t *testing.T) {
		reduceMalformed(t, func(running *supervisorRunningBundle, _, _ supervisorActionToken) {
			running.exitRecheck.code = 1
		})
	})
	t.Run("observed root completion predates release", func(t *testing.T) {
		reduceMalformed(t, func(running *supervisorRunningBundle, exact, _ supervisorActionToken) {
			running.exitRecheck = supervisorExitRecheck{
				performed: true, observed: true, at: releasedAt.Add(-time.Nanosecond),
				code: 53, action: exact,
			}
			running.drainBy = emergencyAt.Add(10 * time.Second)
		})
	})
	t.Run("observed root completion follows emergency cut", func(t *testing.T) {
		reduceMalformed(t, func(running *supervisorRunningBundle, exact, _ supervisorActionToken) {
			running.exitRecheck = supervisorExitRecheck{
				performed: true, observed: true, at: emergencyAt.Add(time.Nanosecond),
				code: 59, action: exact,
			}
			running.drainBy = emergencyAt.Add(10 * time.Second)
		})
	})
	t.Run("zero launch action token", func(t *testing.T) {
		reduceMalformed(t, func(running *supervisorRunningBundle, _, _ supervisorActionToken) {
			running.exitRecheck.action = 0
		})
	})
	t.Run("wrong launch action token", func(t *testing.T) {
		reduceMalformed(t, func(running *supervisorRunningBundle, exact, _ supervisorActionToken) {
			running.exitRecheck.action = exact + 100
		})
	})
	t.Run("stale cross-attempt launch action token", func(t *testing.T) {
		reduceMalformed(t, func(running *supervisorRunningBundle, _, stale supervisorActionToken) {
			running.exitRecheck.action = stale
		})
	})
	t.Run("nonzero wait action", func(t *testing.T) {
		reduceMalformed(t, func(running *supervisorRunningBundle, _, _ supervisorActionToken) {
			running.waitAction = 41
		})
	})
	t.Run("nonzero sample action", func(t *testing.T) {
		reduceMalformed(t, func(running *supervisorRunningBundle, _, _ supervisorActionToken) {
			running.sampleAction = 43
		})
	})
	t.Run("fallback local drain bound", func(t *testing.T) {
		reduceMalformed(t, func(running *supervisorRunningBundle, _, _ supervisorActionToken) {
			running.drainBy = emergencyAt.Add(10 * time.Second)
		})
	})
	t.Run("selected root lacks local drain bound", func(t *testing.T) {
		reduceMalformed(t, func(running *supervisorRunningBundle, exact, _ supervisorActionToken) {
			running.exitRecheck = supervisorExitRecheck{
				performed: true, observed: true, at: factAt, code: 47, action: exact,
			}
		})
	})

	for _, test := range []struct {
		name string
		fact supervisorRunningFact
	}{
		{
			name: "zero-token fuse",
			fact: supervisorRunningFact{
				kind: supervisorRunningFuseObserved, at: factAt, rootLive: true, live: 65,
			},
		},
		{
			name: "zero-token root exit",
			fact: supervisorRunningFact{kind: supervisorRunningRootExited, at: factAt, exitCode: 31},
		},
		{
			name: "pre-ownership stop",
			fact: supervisorRunningFact{
				kind: supervisorRunningStopRequested, at: factAt,
				stop: StopRequest{At: factAt, DrainBy: emergencyAt.Add(10 * time.Second)},
			},
		},
		{
			name: "zero-token observation failure",
			fact: supervisorRunningFact{
				kind: supervisorRunningObservationFailed, at: factAt,
				source: supervisorObservationRunning, diagnostic: 37,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fact := test.fact
			fact.generation = generation
			reduceMalformed(t, func(running *supervisorRunningBundle, _, _ supervisorActionToken) {
				running.facts = []supervisorRunningFact{fact}
			})
		})
	}
}

func TestSupervisorReducerLaunchEmergencyPreOwnedCutDoesNotAuthorizeRunningFacts(t *testing.T) {
	launchBy := time.Unix(345, 0)
	emergencyAt := launchBy.Add(-100 * time.Millisecond)
	releasedAt := emergencyAt.Add(-500 * time.Millisecond)
	const generation = attemptGeneration(36)
	state, launch := appendReducerLaunchWithFacts(
		t, supervisorState{}, generation, "attempt-emergency-no-running-actions",
		AutomaticProfile, 20*time.Second, launchBy,
	)
	completion := supervisorLaunchCompletion{
		generation: generation, action: launch.token,
		at: releasedAt, kind: supervisorLaunchReleased,
	}
	sealed, _ := reduceSupervisor(state, supervisorEvent{
		kind: supervisorEmergencyStarted, at: emergencyAt,
		drainBy: emergencyAt.Add(5 * time.Second),
		emergencySnapshots: []supervisorEmergencySnapshot{{
			generation: generation, completion: &completion,
			running: &supervisorRunningBundle{
				generation: generation,
				exitRecheck: supervisorExitRecheck{
					performed: true, at: emergencyAt, action: launch.token,
				},
			},
		}},
	})
	attempt := supervisorAttemptByGeneration(t, sealed, generation)
	if attempt.waitAction != 0 || attempt.sampleAction != 0 {
		t.Fatalf("pre-Owned emergency invented running actions: %#v", attempt)
	}

	for _, test := range []struct {
		name string
		fact supervisorRunningFact
	}{
		{
			name: "zero-token fuse",
			fact: supervisorRunningFact{
				generation: generation, kind: supervisorRunningFuseObserved,
				at: emergencyAt.Add(time.Nanosecond), rootLive: true, live: 65,
			},
		},
		{
			name: "zero-token root exit",
			fact: supervisorRunningFact{
				generation: generation, kind: supervisorRunningRootExited,
				at: emergencyAt.Add(time.Nanosecond), exitCode: 61,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertSupervisorInvariant(t, func() {
				reduceSupervisor(sealed, supervisorEvent{
					kind: supervisorRunningObserved, generation: generation,
					at: test.fact.at, drainBy: emergencyAt.Add(5 * time.Second),
					running: &supervisorRunningBundle{
						generation: generation, facts: []supervisorRunningFact{test.fact},
					},
				})
			})
		})
	}
}

func TestSupervisorReducerLaunchUnconfirmedEmergencyEstablishesMonotonicFloor(t *testing.T) {
	launchBy := time.Unix(348, 0)
	emergencyAt := launchBy.Add(time.Second)
	drainBy := emergencyAt.Add(5 * time.Second)
	completionAt := launchBy.Add(500 * time.Millisecond)
	const generation = attemptGeneration(37)
	newUnconfirmed := func(t *testing.T) (supervisorState, supervisorAction) {
		t.Helper()
		state, launch := appendReducerLaunch(
			t, supervisorState{}, generation, "attempt-unconfirmed-emergency-floor", launchBy,
		)
		state, _ = reduceSupervisor(state, supervisorEvent{
			kind: supervisorLaunchBoundary, generation: generation, at: launchBy,
		})

		return state, launch
	}

	t.Run("nil completion floors a later launch completion", func(t *testing.T) {
		state, launch := newUnconfirmed(t)
		floored, _ := reduceSupervisor(state, supervisorEvent{
			kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: drainBy,
			emergencySnapshots: []supervisorEmergencySnapshot{{generation: generation}},
		})
		attempt := supervisorAttemptByGeneration(t, floored, generation)
		if !attempt.lastEventAt.Equal(emergencyAt) {
			t.Errorf("nil-completion emergency floor = %s, want %s", attempt.lastEventAt, emergencyAt)
		}
		completion := supervisorLaunchCompletion{
			generation: generation, action: launch.token,
			at: completionAt, kind: supervisorLaunchReleased,
		}
		assertSupervisorInvariant(t, func() {
			reduceSupervisor(floored, supervisorEvent{
				kind: supervisorLaunchCompleted, generation: generation,
				at: completion.at, completion: &completion,
			})
		})
	})

	t.Run("released completion floors its force completion", func(t *testing.T) {
		state, launch := newUnconfirmed(t)
		completion := supervisorLaunchCompletion{
			generation: generation, action: launch.token,
			at: completionAt, kind: supervisorLaunchReleased,
		}
		floored, actions := reduceSupervisor(state, supervisorEvent{
			kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: drainBy,
			emergencySnapshots: []supervisorEmergencySnapshot{{
				generation: generation, completion: &completion,
			}},
		})
		assertSupervisorActions(t, actions, supervisorAdoptOwned, supervisorForceOwned)
		attempt := supervisorAttemptByGeneration(t, floored, generation)
		if !attempt.lastEventAt.Equal(emergencyAt) {
			t.Errorf("released-completion emergency floor = %s, want %s", attempt.lastEventAt, emergencyAt)
		}
		forceCompletion := supervisorDrainCompletion{
			generation: generation,
			action: supervisorPendingAction{
				kind: actions[1].kind, token: actions[1].token,
			},
			at: emergencyAt.Add(-time.Nanosecond), kind: supervisorDrainForceCompleted,
		}
		assertSupervisorInvariant(t, func() {
			reduceSupervisor(floored, supervisorEvent{
				kind: supervisorDrainCompleted, generation: generation,
				at: forceCompletion.at, drain: &forceCompletion,
			})
		})
	})
}

func TestSupervisorReducerLaunchGlobalEmergencyOrdersAllLiveObligationsAndPersistsEpoch(t *testing.T) {
	launchBy := time.Unix(350, 0)
	emergencyAt := launchBy.Add(-500 * time.Millisecond)
	drainBy := launchBy.Add(5 * time.Second)
	state := supervisorState{}
	var launchActions [4]supervisorAction
	for index, generation := range []attemptGeneration{51, 52, 53, 54} {
		state, launchActions[index] = appendReducerLaunch(
			t, state, generation, attemptIdentity("attempt-"+string(rune('a'+index))), launchBy,
		)
	}

	ownedCompletion := supervisorLaunchCompletion{
		generation: 53,
		action:     launchActions[2].token,
		at:         emergencyAt.Add(-time.Nanosecond),
		kind:       supervisorLaunchReleased,
	}
	var ownedActions []supervisorAction
	state, ownedActions = reduceSupervisor(state, supervisorEvent{
		kind: supervisorLaunchCompleted, generation: 53,
		at: ownedCompletion.at, completion: &ownedCompletion,
	})
	closedCompletion := supervisorLaunchCompletion{
		generation: 54,
		action:     launchActions[3].token,
		at:         emergencyAt.Add(-time.Nanosecond),
		kind:       supervisorLaunchProvenNotReleased,
		failure:    LaunchFailed,
	}
	state, _ = reduceSupervisor(state, supervisorEvent{
		kind: supervisorLaunchCompleted, generation: 54,
		at: closedCompletion.at, completion: &closedCompletion,
	})
	releasedSnapshot := supervisorLaunchCompletion{
		generation: 52,
		action:     launchActions[1].token,
		at:         emergencyAt,
		kind:       supervisorLaunchReleased,
	}
	event := supervisorEvent{
		kind:    supervisorEmergencyStarted,
		at:      emergencyAt,
		drainBy: drainBy,
		emergencySnapshots: []supervisorEmergencySnapshot{
			{generation: 51},
			{generation: 52, completion: &releasedSnapshot, running: &supervisorRunningBundle{
				generation: 52,
				exitRecheck: supervisorExitRecheck{
					performed: true, at: emergencyAt, action: launchActions[1].token,
				},
			}},
			{generation: 53, running: &supervisorRunningBundle{
				generation: 53, waitAction: ownedActions[1].token, sampleAction: ownedActions[2].token,
				drainBy: drainBy,
			}},
		},
	}

	next, actions := reduceSupervisor(state, event)
	assertSupervisorActions(t, actions,
		supervisorRevokeLaunchRelease,
		supervisorPublishLaunchUnconfirmed,
		supervisorPublishOwned,
		supervisorForceOwned,
		supervisorForceOwned,
	)
	wantGenerations := []attemptGeneration{51, 51, 52, 52, 53}
	for index, action := range actions {
		if action.generation != wantGenerations[index] || !action.at.Equal(emergencyAt) ||
			!action.drainBy.Equal(drainBy) {
			t.Fatalf("emergency action order/request at %d = %#v", index, action)
		}
	}
	if !next.emergency.active || !next.emergency.at.Equal(emergencyAt) ||
		!next.emergency.drainBy.Equal(drainBy) {
		t.Fatalf("emergency epoch = %#v", next.emergency)
	}
	if supervisorAttemptByGeneration(t, next, 54).phase != supervisorLaunchClosedNotReleased {
		t.Fatalf("closed-not-released attempt reentered emergency: %#v", next)
	}

	lateReleased := supervisorLaunchCompletion{
		generation: 51,
		action:     launchActions[0].token,
		at:         emergencyAt.Add(time.Nanosecond),
		kind:       supervisorLaunchReleased,
	}
	adopted, lateActions := reduceSupervisor(next, supervisorEvent{
		kind: supervisorLaunchCompleted, generation: 51,
		at: lateReleased.at, completion: &lateReleased,
	})
	assertSupervisorActions(t, lateActions, supervisorAdoptOwned, supervisorForceOwned)
	for _, action := range lateActions {
		if !action.at.Equal(emergencyAt) || !action.drainBy.Equal(drainBy) {
			t.Fatalf("late adoption lost emergency request: %#v", lateActions)
		}
	}
	adoptedAttempt := supervisorAttemptByGeneration(t, adopted, 51)
	if adoptedAttempt.phase != supervisorLaunchOwned ||
		adoptedAttempt.intent != (supervisorRunningIntent{}) || adoptedAttempt.intent.duration < 0 {
		t.Fatalf("late release was not adopted: %#v", adopted)
	}

	t.Run("duplicate emergency", func(t *testing.T) {
		assertSupervisorInvariant(t, func() { reduceSupervisor(next, event) })
	})
	t.Run("conflicting emergency", func(t *testing.T) {
		conflict := event
		conflict.drainBy = drainBy.Add(time.Second)
		assertSupervisorInvariant(t, func() { reduceSupervisor(next, conflict) })
	})
}

func TestSupervisorReducerLaunchRejectsStaleWrongAndDuplicateCompletionCorrelation(t *testing.T) {
	launchBy := time.Unix(400, 0)
	validCompletion := func(generation attemptGeneration, token supervisorActionToken) supervisorLaunchCompletion {
		return supervisorLaunchCompletion{
			generation: generation,
			action:     token,
			at:         launchBy.Add(-time.Nanosecond),
			kind:       supervisorLaunchProvenNotReleased,
			failure:    LaunchFailed,
		}
	}

	t.Run("stale generation", func(t *testing.T) {
		state, launch := registeredReducerLaunch(t, 41, "attempt-reused", launchBy)
		completion := validCompletion(40, launch.token)
		assertSupervisorInvariant(t, func() {
			reduceSupervisor(state, supervisorEvent{
				kind: supervisorLaunchCompleted, generation: 40,
				at: completion.at, completion: &completion,
			})
		})
	})

	t.Run("wrong action token", func(t *testing.T) {
		state, launch := registeredReducerLaunch(t, 42, "attempt-a", launchBy)
		completion := validCompletion(42, launch.token+1)
		assertSupervisorInvariant(t, func() {
			reduceSupervisor(state, supervisorEvent{
				kind: supervisorLaunchCompleted, generation: 42,
				at: completion.at, completion: &completion,
			})
		})
	})

	t.Run("duplicate completion", func(t *testing.T) {
		state, launch := registeredReducerLaunch(t, 43, "attempt-a", launchBy)
		completion := validCompletion(43, launch.token)
		event := supervisorEvent{
			kind: supervisorLaunchCompleted, generation: 43,
			at: completion.at, completion: &completion,
		}
		closed, _ := reduceSupervisor(state, event)
		assertSupervisorInvariant(t, func() { reduceSupervisor(closed, event) })
	})

	t.Run("equality completion cannot bypass serialized snapshot", func(t *testing.T) {
		state, launch := registeredReducerLaunch(t, 44, "attempt-a", launchBy)
		completion := validCompletion(44, launch.token)
		completion.at = launchBy
		assertSupervisorInvariant(t, func() {
			reduceSupervisor(state, supervisorEvent{
				kind: supervisorLaunchCompleted, generation: 44,
				at: launchBy, completion: &completion,
			})
		})
	})

	t.Run("post-bound completion cannot skip nil boundary snapshot", func(t *testing.T) {
		state, launch := registeredReducerLaunch(t, 45, "attempt-a", launchBy)
		completion := validCompletion(45, launch.token)
		completion.at = launchBy.Add(time.Nanosecond)
		assertSupervisorInvariant(t, func() {
			reduceSupervisor(state, supervisorEvent{
				kind: supervisorLaunchCompleted, generation: 45,
				at: completion.at, completion: &completion,
			})
		})
	})
}

func TestSupervisorReducerLaunchDataTypesContainNoExecutionCapability(t *testing.T) {
	// Stable slices preserve reducer order. The only permitted pointer is a
	// pointer to a data-only optional completion snapshot carried atomically by
	// a boundary event. time.Time is an explicitly approved logical fact type.
	types := []reflect.Type{
		reflect.TypeOf(supervisorState{}),
		reflect.TypeOf(supervisorEvent{}),
		reflect.TypeOf(supervisorLaunchCompletion{}),
		reflect.TypeOf(supervisorAction{}),
	}
	for _, dataType := range types {
		t.Run(dataType.Name(), func(t *testing.T) {
			assertReducerDataOnly(t, dataType, make(map[reflect.Type]bool), dataType.Name())
		})
	}
}

func registeredReducerLaunch(
	t *testing.T,
	generation attemptGeneration,
	attempt attemptIdentity,
	launchBy time.Time,
) (supervisorState, supervisorAction) {
	t.Helper()

	return appendReducerLaunch(t, supervisorState{}, generation, attempt, launchBy)
}

func appendReducerLaunch(
	t *testing.T,
	state supervisorState,
	generation attemptGeneration,
	attempt attemptIdentity,
	launchBy time.Time,
) (supervisorState, supervisorAction) {
	t.Helper()

	return appendReducerLaunchWithFacts(
		t, state, generation, attempt, AutomaticProfile, 20*time.Second, launchBy,
	)
}

func appendReducerLaunchWithFacts(
	t *testing.T,
	state supervisorState,
	generation attemptGeneration,
	attempt attemptIdentity,
	profile Profile,
	commandDeadline time.Duration,
	launchBy time.Time,
) (supervisorState, supervisorAction) {
	t.Helper()
	state, actions := reduceSupervisor(state, supervisorEvent{
		kind:            supervisorProspectiveRegistered,
		generation:      generation,
		attempt:         attempt,
		at:              launchBy.Add(-time.Second),
		launchBy:        launchBy,
		profile:         profile,
		commandDeadline: commandDeadline,
	})
	assertSupervisorActions(t, actions, supervisorLaunchNative)
	if actions[0].generation != generation || actions[0].token == 0 {
		t.Fatalf("native launch action = %#v", actions[0])
	}
	registered := supervisorAttemptByGeneration(t, state, generation)
	if registered.attempt != attempt || registered.launchAction != actions[0].token ||
		registered.phase != supervisorLaunchEstablishing || registered.profile != profile ||
		registered.commandDeadline != commandDeadline {
		t.Fatalf("registered launch = %#v action=%#v", registered, actions[0])
	}

	return state, actions[0]
}

func supervisorAttemptByGeneration(
	t *testing.T,
	state supervisorState,
	generation attemptGeneration,
) supervisorAttemptState {
	t.Helper()
	for _, attempt := range state.attempts {
		if attempt.generation == generation {
			return attempt
		}
	}
	t.Fatalf("generation %d absent from state %#v", generation, state)

	return supervisorAttemptState{}
}

func assertSupervisorActions(t *testing.T, actions []supervisorAction, want ...supervisorActionKind) {
	t.Helper()
	got := make([]supervisorActionKind, len(actions))
	for index, action := range actions {
		got[index] = action.kind
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %v, want %v (%#v)", got, want, actions)
	}
	assertStrictlyIncreasingActionTokens(t, actions)
}

func assertStrictlyIncreasingActionTokens(t *testing.T, actions []supervisorAction) {
	t.Helper()
	var previous supervisorActionToken
	for _, action := range actions {
		if action.token == 0 || action.token <= previous {
			t.Fatalf("action tokens are not strictly increasing: %#v", actions)
		}
		previous = action.token
	}
}

func assertSupervisorInvariant(t *testing.T, action func()) {
	t.Helper()
	defer func() {
		if _, ok := recover().(runtimeInvariantViolation); !ok {
			t.Fatalf("reducer did not raise runtimeInvariantViolation")
		}
	}()
	action()
}

func reduceSupervisorMustAccept(
	t *testing.T,
	state supervisorState,
	event supervisorEvent,
) (next supervisorState, actions []supervisorAction) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("reducer unexpectedly rejected valid evidence: %v", recovered)
		}
	}()

	return reduceSupervisor(state, event)
}

func assertReducerDataOnly(t *testing.T, dataType reflect.Type, visiting map[reflect.Type]bool, path string) {
	t.Helper()
	if dataType == reflect.TypeOf(time.Time{}) || visiting[dataType] {
		return
	}
	visiting[dataType] = true
	defer delete(visiting, dataType)

	switch dataType.Kind() {
	case reflect.Func, reflect.Chan, reflect.Interface, reflect.Map, reflect.UnsafePointer, reflect.Uintptr:
		t.Fatalf("reducer data contains execution capability at %s: %s", path, dataType)
	case reflect.Pointer, reflect.Slice, reflect.Array:
		assertReducerDataOnly(t, dataType.Elem(), visiting, path+" -> "+dataType.String())
	case reflect.Struct:
		for index := range dataType.NumField() {
			field := dataType.Field(index)
			assertReducerDataOnly(t, field.Type, visiting, path+"."+field.Name)
		}
	}
}
