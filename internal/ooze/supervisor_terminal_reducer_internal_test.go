package ooze

import (
	"reflect"
	"testing"
	"time"
)

func TestSupervisorReducerTerminalReleaseStoresEvidenceBeforeRuntimeSettlement(t *testing.T) {
	for _, test := range []struct {
		name              string
		controlDiagnostic supervisorDiagnosticRef
		outputDiagnostic  supervisorDiagnosticRef
		releaseDiagnostic supervisorDiagnosticRef
		wantKind          supervisorTerminalKind
	}{
		{
			name:     "drain census failure becomes infrastructure",
			wantKind: supervisorTerminalInfrastructureRunning,
		},
		{
			name: "release failure becomes infrastructure", releaseDiagnostic: 404,
			wantKind: supervisorTerminalInfrastructureRelease,
		},
		{
			name:             "output failure precedes release failure",
			outputDiagnostic: 505, releaseDiagnostic: 404,
			wantKind: supervisorTerminalInfrastructureOutput,
		},
		{
			name:              "control failure precedes output and release failures",
			controlDiagnostic: 606, outputDiagnostic: 505, releaseDiagnostic: 404,
			wantKind: supervisorTerminalInfrastructureControl,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newTerminalReleaseReducerFixture(
				t, test.controlDiagnostic, test.outputDiagnostic,
			)
			before := cloneSupervisorState(fixture.state)
			attempt := supervisorAttemptByGeneration(t, fixture.state, fixture.generation)
			if attempt.phase != supervisorReleasingDomain ||
				attempt.pendingAction != (supervisorPendingAction{
					kind: fixture.release.kind, token: fixture.release.token,
				}) {
				t.Fatalf("release fixture = %#v action=%#v", attempt, fixture.release)
			}

			at := fixture.release.at.Add(time.Nanosecond)
			completion := supervisorReleaseCompletion{
				generation: fixture.generation,
				action: supervisorPendingAction{
					kind: fixture.release.kind, token: fixture.release.token,
				},
				at: at, diagnostic: test.releaseDiagnostic,
			}
			next, actions := reduceSupervisor(fixture.state, supervisorEvent{
				kind:       supervisorReleaseCompleted,
				generation: fixture.generation,
				at:         at,
				release:    &completion,
			})

			prefixLength := uint64(6)
			completeThroughCutoff := true
			if test.outputDiagnostic != 0 {
				prefixLength = 4
				completeThroughCutoff = false
			}
			wantEvidence := supervisorTerminalEvidence{
				kind:            test.wantKind,
				commandDeadline: attempt.commandDeadline,
				launchDuration:  attempt.startedAt.Sub(attempt.registeredAt),
				commandDuration: attempt.intent.duration,
				firedBound:      supervisorNoCommandBound,
				exitCode:        23,
				exitSignal:      9,
				count:           supervisorObservedCount{},
				output: supervisorOutputEvidence{
					ref: 81, cutoff: 6, prefixLength: prefixLength,
					completeThroughCutoff: completeThroughCutoff, final: true,
					diagnostic: test.outputDiagnostic,
				},
				diagnostics: supervisorTerminalDiagnostics{
					wait: 101, running: 202, drain: 303,
					control: test.controlDiagnostic, release: test.releaseDiagnostic,
				},
			}
			wantToken := fixture.state.nextAction + 1
			wantAction := supervisorAction{
				kind:       supervisorSettleRuntime,
				generation: fixture.generation,
				token:      wantToken,
				at:         at,
				terminal:   wantEvidence,
			}
			wantState := cloneSupervisorState(fixture.state)
			wantState.nextAction = wantToken
			wantAttempt := &wantState.attempts[0]
			wantAttempt.phase = supervisorSettlingRuntime
			wantAttempt.lastEventAt = at
			wantAttempt.pendingAction = supervisorPendingAction{
				kind: supervisorSettleRuntime, token: wantToken,
			}
			wantAttempt.releaseDiagnostic = test.releaseDiagnostic
			wantAttempt.terminal = wantEvidence

			if !reflect.DeepEqual(actions, []supervisorAction{wantAction}) {
				t.Fatalf("release actions = %#v, want %#v", actions, []supervisorAction{wantAction})
			}
			if !reflect.DeepEqual(next, wantState) {
				t.Fatalf("release next state = %#v, want %#v", next, wantState)
			}
			if !reflect.DeepEqual(fixture.state, before) {
				t.Fatalf("release transition mutated its input: before=%#v after=%#v", before, fixture.state)
			}
		})
	}
}

func TestSupervisorReducerTerminalReleaseEmergencyPreservesRuntimeSettlementInFlight(t *testing.T) {
	fixture := newTerminalReleaseReducerFixture(t, 0, 0)
	releaseAt := fixture.release.at.Add(time.Nanosecond)
	completion := supervisorReleaseCompletion{
		generation: fixture.generation,
		action: supervisorPendingAction{
			kind: fixture.release.kind, token: fixture.release.token,
		},
		at: releaseAt,
	}
	settling, settleActions := reduceSupervisor(fixture.state, supervisorEvent{
		kind: supervisorReleaseCompleted, generation: fixture.generation,
		at: releaseAt, release: &completion,
	})
	assertSupervisorActions(t, settleActions, supervisorSettleRuntime)
	settlingAttempt := supervisorAttemptByGeneration(t, settling, fixture.generation)
	if settlingAttempt.phase != supervisorSettlingRuntime ||
		settlingAttempt.pendingAction != (supervisorPendingAction{
			kind: supervisorSettleRuntime, token: settleActions[0].token,
		}) || settlingAttempt.releaseDiagnostic != 0 {
		t.Fatalf("runtime settlement fixture = %#v actions=%#v", settlingAttempt, settleActions)
	}

	emergencyAt := releaseAt.Add(time.Nanosecond)
	emergencyDrainBy := settlingAttempt.drain.effectiveDrainBy.Add(time.Second)
	before := cloneSupervisorState(settling)
	next, actions := reduceSupervisor(settling, supervisorEvent{
		kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: emergencyDrainBy,
		emergencySnapshots: []supervisorEmergencySnapshot{{generation: fixture.generation}},
	})
	if len(actions) != 0 {
		t.Fatalf("emergency competed with runtime settlement: %#v", actions)
	}
	want := cloneSupervisorState(settling)
	want.emergency = supervisorEmergencyEpoch{active: true, at: emergencyAt, drainBy: emergencyDrainBy}
	want.attempts[0].lastEventAt = emergencyAt
	if !reflect.DeepEqual(next, want) {
		t.Fatalf("runtime-settlement emergency state = %#v, want %#v", next, want)
	}
	if !reflect.DeepEqual(settling, before) {
		t.Fatalf("runtime-settlement emergency mutated input: before=%#v after=%#v", before, settling)
	}
}

func TestSupervisorReducerTerminalReleaseRejectsMalformedCompletionByteStable(t *testing.T) {
	fixture := newTerminalReleaseReducerFixture(t, 0, 0)
	validAt := fixture.release.at.Add(time.Nanosecond)

	for _, test := range []struct {
		name   string
		mutate func(*supervisorState, *supervisorEvent, *supervisorReleaseCompletion)
	}{
		{
			name: "inner generation mismatch",
			mutate: func(_ *supervisorState, _ *supervisorEvent, completion *supervisorReleaseCompletion) {
				completion.generation++
			},
		},
		{
			name: "outer generation mismatch",
			mutate: func(_ *supervisorState, event *supervisorEvent, _ *supervisorReleaseCompletion) {
				event.generation++
			},
		},
		{
			name: "wrong action kind",
			mutate: func(_ *supervisorState, _ *supervisorEvent, completion *supervisorReleaseCompletion) {
				completion.action.kind = supervisorCaptureOutput
			},
		},
		{
			name: "wrong token",
			mutate: func(_ *supervisorState, _ *supervisorEvent, completion *supervisorReleaseCompletion) {
				completion.action.token++
			},
		},
		{
			name: "event and completion instants differ",
			mutate: func(_ *supervisorState, event *supervisorEvent, _ *supervisorReleaseCompletion) {
				event.at = event.at.Add(time.Nanosecond)
			},
		},
		{
			name: "completion moves backward",
			mutate: func(_ *supervisorState, event *supervisorEvent, completion *supervisorReleaseCompletion) {
				completion.at = fixture.release.at.Add(-time.Nanosecond)
				event.at = completion.at
			},
		},
		{
			name: "wrong phase",
			mutate: func(state *supervisorState, _ *supervisorEvent, _ *supervisorReleaseCompletion) {
				state.attempts[0].phase = supervisorSealingStopAdmission
			},
		},
		{
			name: "output is not final",
			mutate: func(state *supervisorState, _ *supervisorEvent, _ *supervisorReleaseCompletion) {
				state.attempts[0].output.final = false
			},
		},
		{
			name: "drain is not proven empty",
			mutate: func(state *supervisorState, _ *supervisorEvent, _ *supervisorReleaseCompletion) {
				state.attempts[0].drain.decision = supervisorDrainUnconfirmed
			},
		},
		{
			name: "root exit follows resolved deadline",
			mutate: func(state *supervisorState, _ *supervisorEvent, _ *supervisorReleaseCompletion) {
				attempt := &state.attempts[0]
				attempt.intent.at = attempt.deadlineAt.Add(time.Nanosecond)
				attempt.intent.duration = attempt.intent.at.Sub(attempt.startedAt)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := cloneSupervisorState(fixture.state)
			completion := supervisorReleaseCompletion{
				generation: fixture.generation,
				action: supervisorPendingAction{
					kind: fixture.release.kind, token: fixture.release.token,
				},
				at: validAt,
			}
			event := supervisorEvent{
				kind:       supervisorReleaseCompleted,
				generation: fixture.generation,
				at:         validAt,
				release:    &completion,
			}
			test.mutate(&state, &event, &completion)
			before := cloneSupervisorState(state)
			assertSupervisorInvariant(t, func() {
				reduceSupervisor(state, event)
			})
			if !reflect.DeepEqual(state, before) {
				t.Fatalf("rejected release completion mutated input: before=%#v after=%#v", before, state)
			}
		})
	}

	t.Run("root exit at resolved deadline remains valid", func(t *testing.T) {
		state := cloneSupervisorState(fixture.state)
		state.attempts[0].intent.at = state.attempts[0].deadlineAt
		state.attempts[0].intent.duration = state.attempts[0].commandDeadline
		completion := supervisorReleaseCompletion{
			generation: fixture.generation,
			action: supervisorPendingAction{
				kind: fixture.release.kind, token: fixture.release.token,
			},
			at: validAt,
		}
		next, actions := reduceSupervisorMustAccept(t, state, supervisorEvent{
			kind: supervisorReleaseCompleted, generation: fixture.generation,
			at: validAt, release: &completion,
		})
		assertSupervisorActions(t, actions, supervisorSettleRuntime)
		attempt := supervisorAttemptByGeneration(t, next, fixture.generation)
		if attempt.terminal.commandDuration != attempt.commandDeadline {
			t.Fatalf(
				"deadline-equal root exit duration = %s, want %s",
				attempt.terminal.commandDuration,
				attempt.commandDeadline,
			)
		}
	})

	completion := supervisorReleaseCompletion{
		generation: fixture.generation,
		action: supervisorPendingAction{
			kind: fixture.release.kind, token: fixture.release.token,
		},
		at: validAt,
	}
	event := supervisorEvent{
		kind:       supervisorReleaseCompleted,
		generation: fixture.generation,
		at:         validAt,
		release:    &completion,
	}
	settling, _ := reduceSupervisor(fixture.state, event)
	before := cloneSupervisorState(settling)
	assertSupervisorInvariant(t, func() {
		reduceSupervisor(settling, event)
	})
	if !reflect.DeepEqual(settling, before) {
		t.Fatalf("duplicate release completion mutated input: before=%#v after=%#v", before, settling)
	}
}

func TestSupervisorReducerTerminalReleaseLateAdoptionAwaitsEmergencySettlement(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		assertLateAdoptedRelease(t, 0)
	})
	t.Run("release diagnostic", func(t *testing.T) {
		fixture, released, fixtureAttempt := assertLateAdoptedRelease(t, 707)
		assertLateAdoptedEmergencyContinuation(t, fixture, released, fixtureAttempt)
	})
}

func assertLateAdoptedRelease(
	t *testing.T,
	diagnostic supervisorDiagnosticRef,
) (terminalReleaseReducerFixture, supervisorState, supervisorAttemptState) {
	t.Helper()
	fixture := newLateAdoptedReleaseReducerFixture(t)
	fixtureAttempt := supervisorAttemptByGeneration(t, fixture.state, fixture.generation)
	if fixtureAttempt.intent != (supervisorRunningIntent{}) ||
		fixtureAttempt.pendingAction != (supervisorPendingAction{
			kind: fixture.release.kind, token: fixture.release.token,
		}) {
		t.Fatalf("late-adopted release fixture manufactured intent or lost action: %#v", fixtureAttempt)
	}

	at := fixture.release.at.Add(time.Nanosecond)
	completion := supervisorReleaseCompletion{
		generation: fixture.generation,
		action: supervisorPendingAction{
			kind: fixture.release.kind, token: fixture.release.token,
		},
		at: at, diagnostic: diagnostic,
	}
	beforeRelease := cloneSupervisorState(fixture.state)
	next, actions := reduceSupervisor(fixture.state, supervisorEvent{
		kind:       supervisorReleaseCompleted,
		generation: fixture.generation,
		at:         at,
		release:    &completion,
	})
	if len(actions) != 0 {
		t.Fatalf("late-adopted sweep emitted ordinary settlement or delivery: %#v", actions)
	}
	wantReleased := cloneSupervisorState(fixture.state)
	wantReleased.attempts[0].phase = supervisorAwaitingEmergencySettlement
	wantReleased.attempts[0].lastEventAt = at
	wantReleased.attempts[0].pendingAction = supervisorPendingAction{}
	wantReleased.attempts[0].releaseDiagnostic = diagnostic
	if !reflect.DeepEqual(next, wantReleased) {
		t.Fatalf("late-adopted release state = %#v, want %#v", next, wantReleased)
	}
	if !reflect.DeepEqual(fixture.state, beforeRelease) {
		t.Fatalf("late-adopted release mutated its input: before=%#v after=%#v", beforeRelease, fixture.state)
	}

	return fixture, next, fixtureAttempt
}

func assertLateAdoptedEmergencyContinuation(
	t *testing.T,
	fixture terminalReleaseReducerFixture,
	released supervisorState,
	fixtureAttempt supervisorAttemptState,
) {
	t.Helper()
	emergencyAt := supervisorAttemptByGeneration(t, released, fixture.generation).lastEventAt.Add(time.Nanosecond)
	emergencyDrainBy := fixtureAttempt.drain.effectiveDrainBy.Add(time.Second)
	beforeEmergency := cloneSupervisorState(released)
	afterEmergency, emergencyActions := reduceSupervisor(released, supervisorEvent{
		kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: emergencyDrainBy,
		emergencySnapshots: []supervisorEmergencySnapshot{{generation: fixture.generation}},
	})
	wantEmergency := cloneSupervisorState(released)
	wantEmergency.emergency = supervisorEmergencyEpoch{
		active: true, at: emergencyAt, drainBy: emergencyDrainBy,
	}
	wantEmergency.attempts[0].lastEventAt = emergencyAt
	wantEmergency.nextAction++
	settle := supervisorAction{
		kind: supervisorSettleEmergency, token: wantEmergency.nextAction,
		resolutions: []supervisorEmergencyResolution{{
			generation: fixture.generation,
			kind:       supervisorEmergencyConfirmedDrained,
		}},
	}
	wantEmergency.emergency.pendingAction = supervisorPendingAction{
		kind: settle.kind, token: settle.token,
	}
	if !reflect.DeepEqual(emergencyActions, []supervisorAction{settle}) {
		t.Fatalf("settled late-adopted sweep actions = %#v, want %#v", emergencyActions, []supervisorAction{settle})
	}
	if !reflect.DeepEqual(afterEmergency, wantEmergency) {
		t.Fatalf("post-release emergency state = %#v, want %#v", afterEmergency, wantEmergency)
	}
	if !reflect.DeepEqual(released, beforeEmergency) {
		t.Fatalf("post-release emergency mutated its input: before=%#v after=%#v", beforeEmergency, released)
	}
	assertLateAdoptedEmergencyState(t, afterEmergency, fixture, fixtureAttempt)
}

func assertLateAdoptedEmergencyState(
	t *testing.T,
	afterEmergency supervisorState,
	fixture terminalReleaseReducerFixture,
	fixtureAttempt supervisorAttemptState,
) {
	t.Helper()
	after := supervisorAttemptByGeneration(t, afterEmergency, fixture.generation)
	if after.phase != supervisorAwaitingEmergencySettlement ||
		after.pendingAction != (supervisorPendingAction{}) ||
		after.terminal != (supervisorTerminalEvidence{}) ||
		after.intent != (supervisorRunningIntent{}) || after.releaseDiagnostic != 707 ||
		!after.drain.effectiveDrainBy.Equal(fixtureAttempt.drain.effectiveDrainBy) ||
		afterEmergency.nextAction != fixture.state.nextAction+1 {
		t.Fatalf("post-release emergency restarted or lengthened completed drainage: %#v", after)
	}
}

type terminalReleaseReducerFixture struct {
	state      supervisorState
	generation attemptGeneration
	release    supervisorAction
}

func newTerminalReleaseReducerFixture(
	t *testing.T,
	controlDiagnostic supervisorDiagnosticRef,
	outputDiagnostic supervisorDiagnosticRef,
) terminalReleaseReducerFixture {
	t.Helper()
	running := newRunningReducerFixture(t, AutomaticProfile)
	intentAt := running.startedAt.Add(time.Second)
	facts := []supervisorRunningFact{
		{
			generation: running.generation, action: running.waitAction,
			kind: supervisorRunningRootExited, at: intentAt,
			exitCode: 23, exitSignal: 9,
		},
		{
			generation: running.generation, action: running.waitAction,
			kind: supervisorRunningObservationFailed, at: intentAt,
			source: supervisorObservationWait, diagnostic: 101,
		},
		{
			generation: running.generation, action: running.sampleAction,
			kind: supervisorRunningObservationFailed, at: intentAt,
			source: supervisorObservationRunning, diagnostic: 202,
		},
	}
	state, actions := running.reduceBundle(t, intentAt, facts, supervisorExitRecheck{})
	assertSupervisorActions(t, actions, supervisorObserveEmptiness)
	drain := drainReducerFixture{generation: running.generation}

	state, actions = drain.complete(
		t, state, actions[0], supervisorDrainObservationFailed,
		intentAt.Add(time.Nanosecond), 303,
	)
	assertSupervisorActions(t, actions, supervisorForceOwned)
	state, actions = drain.complete(
		t, state, actions[0], supervisorDrainForceCompleted,
		intentAt.Add(2*time.Nanosecond), controlDiagnostic,
	)
	assertSupervisorActions(t, actions, supervisorObserveEmptiness)
	state, actions = drain.complete(
		t, state, actions[0], supervisorDrainObservedEmpty,
		intentAt.Add(3*time.Nanosecond), 0,
	)
	assertSupervisorActions(t, actions, supervisorCaptureOutput)

	prefixLength := uint64(6)
	if outputDiagnostic != 0 {
		prefixLength = 4
	}
	state, actions = completeReducerOutput(
		t, state, running.generation, actions[0], intentAt.Add(4*time.Nanosecond),
		81, 6, prefixLength, outputDiagnostic,
	)
	assertSupervisorActions(t, actions, supervisorSealStopAdmission)
	state, actions = completeReducerStopSeal(
		t, state, running.generation, actions[0], intentAt.Add(5*time.Nanosecond),
	)
	assertSupervisorActions(t, actions, supervisorReleaseDomain)

	return terminalReleaseReducerFixture{
		state: state, generation: running.generation, release: actions[0],
	}
}

func newLateAdoptedReleaseReducerFixture(t *testing.T) terminalReleaseReducerFixture {
	t.Helper()
	const generation = attemptGeneration(97)
	launchBy := time.Unix(2_000, 0)
	state, launch := registeredReducerLaunch(t, generation, "attempt-late-terminal", launchBy)
	state, actions := reduceSupervisor(state, supervisorEvent{
		kind: supervisorLaunchBoundary, generation: generation, at: launchBy,
	})
	assertSupervisorActions(t, actions, supervisorRevokeLaunchRelease, supervisorPublishLaunchUnconfirmed)

	releasedAt := launchBy.Add(time.Nanosecond)
	drainBy := releasedAt.Add(5 * time.Second)
	completion := supervisorLaunchCompletion{
		generation: generation, action: launch.token, at: releasedAt,
		kind: supervisorLaunchReleased,
	}
	state, actions = reduceSupervisor(state, supervisorEvent{
		kind: supervisorLaunchCompleted, generation: generation, at: releasedAt,
		drainBy: drainBy, completion: &completion,
	})
	assertSupervisorActions(t, actions, supervisorAdoptOwned, supervisorForceOwned)
	drain := drainReducerFixture{generation: generation}
	state, actions = drain.complete(
		t, state, actions[1], supervisorDrainForceCompleted, releasedAt.Add(time.Nanosecond), 0,
	)
	assertSupervisorActions(t, actions, supervisorObserveEmptiness)
	state, actions = drain.complete(
		t, state, actions[0], supervisorDrainObservedEmpty, releasedAt.Add(2*time.Nanosecond), 0,
	)
	assertSupervisorActions(t, actions, supervisorCaptureOutput)
	state, actions = completeReducerOutput(
		t, state, generation, actions[0], releasedAt.Add(3*time.Nanosecond),
		91, 3, 3, 0,
	)
	assertSupervisorActions(t, actions, supervisorSealStopAdmission)
	state, actions = completeReducerStopSeal(
		t, state, generation, actions[0], releasedAt.Add(4*time.Nanosecond),
	)
	assertSupervisorActions(t, actions, supervisorReleaseDomain)

	return terminalReleaseReducerFixture{state: state, generation: generation, release: actions[0]}
}

func TestSupervisorReducerTerminalReleaseDataRemainsCapabilityFree(t *testing.T) {
	diagnosticsType := reflect.TypeOf(supervisorTerminalDiagnostics{})
	wantDiagnosticFields := []string{"wait", "running", "drain", "control", "release"}
	if diagnosticsType.NumField() != len(wantDiagnosticFields) {
		t.Fatalf("terminal diagnostics fields = %d, want exactly %d", diagnosticsType.NumField(), len(wantDiagnosticFields))
	}
	for index, want := range wantDiagnosticFields {
		if got := diagnosticsType.Field(index).Name; got != want {
			t.Fatalf("terminal diagnostics field %d = %q, want %q", index, got, want)
		}
	}

	for _, dataType := range []reflect.Type{
		reflect.TypeOf(supervisorReleaseCompletion{}),
		reflect.TypeOf(supervisorTerminalEvidence{}),
		reflect.TypeOf(supervisorTerminalDiagnostics{}),
		reflect.TypeOf(supervisorState{}),
		reflect.TypeOf(supervisorEvent{}),
		reflect.TypeOf(supervisorAction{}),
	} {
		t.Run(dataType.Name(), func(t *testing.T) {
			assertReducerDataOnly(t, dataType, make(map[reflect.Type]bool), dataType.Name())
		})
	}
}
