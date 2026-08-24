package ooze

import (
	"reflect"
	"testing"
	"time"
)

type terminalNormalizationSpec struct {
	name               string
	profile            Profile
	intent             supervisorRunningIntentKind
	rootAtDeadline     bool
	exitCode           int
	exitSignal         int
	waitDiagnostic     supervisorDiagnosticRef
	runningDiagnostic  supervisorDiagnosticRef
	releaseDiagnostic  supervisorDiagnosticRef
	emergencyAfterStop bool
	wantKind           supervisorTerminalKind
	wantBound          supervisorFiredBound
	wantCount          supervisorObservedCount
}

func TestSupervisorReducerTerminalNormalization(t *testing.T) {
	for _, spec := range terminalNormalizationSpecs() {
		t.Run(spec.name, func(t *testing.T) {
			releasing, release := buildTerminalNormalizationRelease(t, spec)
			input := cloneSupervisorState(releasing)
			before := supervisorAttemptByGeneration(t, releasing, release.generation)
			completionAt := release.at.Add(time.Nanosecond)
			completion := supervisorReleaseCompletion{
				generation: release.generation,
				action: supervisorPendingAction{
					kind: release.kind, token: release.token,
				},
				at: completionAt, diagnostic: spec.releaseDiagnostic,
			}
			next, actions := reduceSupervisorMustAccept(t, releasing, supervisorEvent{
				kind: supervisorReleaseCompleted, generation: release.generation,
				at: completionAt, release: &completion,
			})
			assertSupervisorActions(t, actions, supervisorSettleRuntime)

			want := supervisorTerminalEvidence{
				kind:            spec.wantKind,
				commandDeadline: before.commandDeadline,
				launchDuration:  before.startedAt.Sub(before.registeredAt),
				commandDuration: before.intent.duration,
				firedBound:      spec.wantBound,
				exitCode:        spec.exitCode,
				exitSignal:      spec.exitSignal,
				count:           spec.wantCount,
				output: supervisorOutputEvidence{
					ref: 131, cutoff: 5, prefixLength: 5,
					completeThroughCutoff: true, final: true,
				},
				diagnostics: supervisorTerminalDiagnostics{
					wait: spec.waitDiagnostic, running: spec.runningDiagnostic,
					release: spec.releaseDiagnostic,
				},
			}
			wantToken := releasing.nextAction + 1
			wantAction := supervisorAction{
				kind: supervisorSettleRuntime, generation: release.generation,
				token: wantToken, at: completionAt, terminal: want,
			}
			wantState := cloneSupervisorState(releasing)
			wantState.nextAction = wantToken
			wantAttempt := &wantState.attempts[0]
			wantAttempt.lastEventAt = completionAt
			wantAttempt.pendingAction = supervisorPendingAction{
				kind: supervisorSettleRuntime, token: wantToken,
			}
			wantAttempt.phase = supervisorSettlingRuntime
			wantAttempt.releaseDiagnostic = spec.releaseDiagnostic
			wantAttempt.terminal = want
			if !reflect.DeepEqual(actions, []supervisorAction{wantAction}) {
				t.Fatalf("normalized action = %#v, want %#v", actions, []supervisorAction{wantAction})
			}
			if !reflect.DeepEqual(next, wantState) {
				t.Fatalf("normalized state = %#v, want %#v", next, wantState)
			}
			if !reflect.DeepEqual(releasing, input) {
				t.Fatalf("terminal normalization mutated input: before=%#v after=%#v", input, releasing)
			}
		})
	}
}

func TestSupervisorReducerDrainCensusNormalizationPrecedence(t *testing.T) {
	for _, test := range []struct {
		name    string
		attempt supervisorAttemptState
		want    supervisorTerminalKind
	}{
		{
			name: "drain census promotes confirmed terminal",
			attempt: supervisorAttemptState{
				intent: supervisorRunningIntent{kind: supervisorIntentRootExit},
				drain:  supervisorDrainState{observationDiagnostic: 303},
			},
			want: supervisorTerminalInfrastructureRunning,
		},
		{
			name: "late wait precedes drain census",
			attempt: supervisorAttemptState{
				intent: supervisorRunningIntent{kind: supervisorIntentRootExit},
				drain: supervisorDrainState{
					waitDiagnostic: 101, observationDiagnostic: 303,
				},
			},
			want: supervisorTerminalInfrastructureWait,
		},
		{
			name: "release precedes wait and drain census",
			attempt: supervisorAttemptState{
				intent: supervisorRunningIntent{kind: supervisorIntentRootExit},
				drain: supervisorDrainState{
					waitDiagnostic: 101, observationDiagnostic: 303,
				},
				releaseDiagnostic: 404,
			},
			want: supervisorTerminalInfrastructureRelease,
		},
		{
			name: "output precedes release wait and drain census",
			attempt: supervisorAttemptState{
				intent: supervisorRunningIntent{kind: supervisorIntentRootExit},
				drain: supervisorDrainState{
					waitDiagnostic: 101, observationDiagnostic: 303,
				},
				output:            supervisorOutputEvidence{diagnostic: 505},
				releaseDiagnostic: 404,
			},
			want: supervisorTerminalInfrastructureOutput,
		},
		{
			name: "control precedes every later diagnostic",
			attempt: supervisorAttemptState{
				intent: supervisorRunningIntent{kind: supervisorIntentRootExit},
				drain: supervisorDrainState{
					waitDiagnostic: 101, observationDiagnostic: 303, controlDiagnostic: 606,
				},
				output:            supervisorOutputEvidence{diagnostic: 505},
				releaseDiagnostic: 404,
			},
			want: supervisorTerminalInfrastructureControl,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			evidence := normalizeTerminalEvidence(test.attempt)
			if evidence.kind != test.want ||
				evidence.diagnostics.wait != test.attempt.drain.waitDiagnostic ||
				evidence.diagnostics.drain != test.attempt.drain.observationDiagnostic ||
				evidence.diagnostics.control != test.attempt.drain.controlDiagnostic ||
				evidence.output.diagnostic != test.attempt.output.diagnostic ||
				evidence.diagnostics.release != test.attempt.releaseDiagnostic {
				t.Fatalf("normalized evidence = %#v, want kind %d with every diagnostic", evidence, test.want)
			}
		})
	}

	attempt := supervisorAttemptState{
		intent: supervisorRunningIntent{kind: supervisorIntentRootExit},
		drain: supervisorDrainState{
			waitDiagnostic: 101, observationDiagnostic: 303, controlDiagnostic: 606,
		},
		output:            supervisorOutputEvidence{diagnostic: 505},
		releaseDiagnostic: 404,
	}
	evidence := normalizeDrainUnconfirmedTerminalEvidence(attempt)
	if evidence.kind != supervisorTerminalDrainUnconfirmed || evidence.diagnostics.wait != 101 ||
		evidence.diagnostics.drain != 303 || evidence.diagnostics.control != 606 ||
		evidence.output.diagnostic != 505 || evidence.diagnostics.release != 404 {
		t.Fatalf("unconfirmed evidence = %#v, want unconfirmed with every diagnostic", evidence)
	}
}

func terminalNormalizationSpecs() []terminalNormalizationSpec {
	return []terminalNormalizationSpec{
		{
			name: "nonzero root exit stays settled for issue 75", profile: AutomaticProfile,
			intent: supervisorIntentRootExit, exitCode: 23, exitSignal: 9,
			wantKind: supervisorTerminalSettled, wantBound: supervisorNoCommandBound,
		},
		{
			name: "serial root exit remains settled", profile: SerialProfile,
			intent: supervisorIntentRootExit, exitCode: 17, exitSignal: 6,
			wantKind: supervisorTerminalSettled, wantBound: supervisorNoCommandBound,
		},
		{
			name: "fuse carries exact crossing count", profile: AutomaticProfile,
			intent:   supervisorIntentFuse,
			wantKind: supervisorTerminalFuseTrip, wantBound: supervisorNoCommandBound,
			wantCount: supervisorObservedCount{present: true, value: 65},
		},
		{
			name: "automatic deadline retains peak", profile: AutomaticProfile,
			intent:    supervisorIntentDeadline,
			wantKind:  supervisorTerminalAutomaticDeadlineTrip,
			wantBound: supervisorCommandDeadlineFired,
			wantCount: supervisorObservedCount{present: true, value: 7},
		},
		{
			name: "automatic deadline keeps absent peak explicit", profile: AutomaticProfile,
			intent:    supervisorIntentDeadline,
			wantKind:  supervisorTerminalAutomaticDeadlineTrip,
			wantBound: supervisorCommandDeadlineFired,
		},
		{
			name: "serial deadline has no count", profile: SerialProfile,
			intent:    supervisorIntentDeadline,
			wantKind:  supervisorTerminalSerialDeadlineTrip,
			wantBound: supervisorCommandDeadlineFired,
		},
		{
			name: "accepted stop request remains caller stop", profile: AutomaticProfile,
			intent:   supervisorIntentStop,
			wantKind: supervisorTerminalStopped, wantBound: supervisorNoCommandBound,
		},
		{
			name: "caller stop survives later emergency clamp", profile: AutomaticProfile,
			intent: supervisorIntentStop, emergencyAfterStop: true,
			wantKind: supervisorTerminalStopped, wantBound: supervisorNoCommandBound,
		},
		{
			name: "runtime emergency remains distinct stop", profile: AutomaticProfile,
			intent:    supervisorIntentRuntimeEmergency,
			wantKind:  supervisorTerminalStopped,
			wantBound: supervisorNoCommandBound,
		},
		{
			name: "release failure overlays fuse without losing count", profile: AutomaticProfile,
			intent: supervisorIntentFuse, releaseDiagnostic: 404,
			wantKind:  supervisorTerminalInfrastructureRelease,
			wantBound: supervisorNoCommandBound,
			wantCount: supervisorObservedCount{present: true, value: 65},
		},
		{
			name: "equal-time wait failure wins and retains both diagnostics", profile: AutomaticProfile,
			intent:         supervisorIntentObservationFailure,
			waitDiagnostic: 101, runningDiagnostic: 202,
			wantKind: supervisorTerminalInfrastructureWait, wantBound: supervisorNoCommandBound,
		},
		{
			name: "running observation failure is distinct infrastructure", profile: AutomaticProfile,
			intent: supervisorIntentObservationFailure, runningDiagnostic: 202,
			wantKind: supervisorTerminalInfrastructureRunning, wantBound: supervisorNoCommandBound,
		},
		{
			name: "serial wait observation failure remains wait infrastructure", profile: SerialProfile,
			intent: supervisorIntentObservationFailure, waitDiagnostic: 101,
			wantKind: supervisorTerminalInfrastructureWait, wantBound: supervisorNoCommandBound,
		},
		{
			name: "root exit at deadline equality remains settled", profile: AutomaticProfile,
			intent: supervisorIntentRootExit, rootAtDeadline: true, exitCode: 29, exitSignal: 15,
			wantKind: supervisorTerminalSettled, wantBound: supervisorNoCommandBound,
		},
	}
}

func buildTerminalNormalizationRelease(
	t *testing.T,
	spec terminalNormalizationSpec,
) (supervisorState, supervisorAction) {
	t.Helper()
	running := newRunningReducerFixture(t, spec.profile)
	state, first := selectTerminalNormalizationIntent(t, running, spec)
	drain := drainReducerFixture{generation: running.generation}

	observe := first
	if first.kind == supervisorForceOwned {
		var actions []supervisorAction
		completionAt := first.at.Add(time.Nanosecond)
		lastEventAt := supervisorAttemptByGeneration(t, state, running.generation).lastEventAt
		if !completionAt.After(lastEventAt) {
			completionAt = lastEventAt.Add(time.Nanosecond)
		}
		state, actions = drain.complete(
			t, state, first, supervisorDrainForceCompleted, completionAt, 0,
		)
		assertSupervisorActions(t, actions, supervisorObserveEmptiness)
		observe = actions[0]
	} else {
		assertSupervisorActions(t, []supervisorAction{first}, supervisorObserveEmptiness)
	}
	emptyAt := observe.at.Add(time.Nanosecond)
	if !emptyAt.Before(observe.drainBy) {
		t.Fatalf("normalization fixture cannot prove emptiness before bound: action=%#v", observe)
	}
	state, actions := drain.complete(
		t, state, observe, supervisorDrainObservedEmpty, emptyAt, 0,
	)
	assertSupervisorActions(t, actions, supervisorCaptureOutput)
	state, actions = completeReducerOutput(
		t, state, running.generation, actions[0], emptyAt.Add(time.Nanosecond),
		131, 5, 5, 0,
	)
	assertSupervisorActions(t, actions, supervisorSealStopAdmission)
	state, actions = completeReducerStopSeal(
		t, state, running.generation, actions[0], actions[0].at.Add(time.Nanosecond),
	)
	assertSupervisorActions(t, actions, supervisorReleaseDomain)

	return state, actions[0]
}

func selectTerminalNormalizationIntent(
	t *testing.T,
	running runningReducerFixture,
	spec terminalNormalizationSpec,
) (supervisorState, supervisorAction) {
	t.Helper()
	switch spec.intent {
	case supervisorIntentRootExit:
		return selectNormalizationRootExit(t, running, spec)
	case supervisorIntentFuse:
		return selectNormalizationFuse(t, running)
	case supervisorIntentDeadline:
		return selectNormalizationDeadline(t, running, spec.wantCount)
	case supervisorIntentStop:
		state, action := selectNormalizationStop(t, running)
		if spec.emergencyAfterStop {
			state = clampNormalizationStopWithEmergency(t, running, state, action)
		}

		return state, action
	case supervisorIntentRuntimeEmergency:
		return selectNormalizationRuntimeEmergency(t, running)
	case supervisorIntentObservationFailure:
		return selectNormalizationObservationFailure(t, running, spec)
	default:
		t.Fatalf("unsupported terminal normalization intent %d", spec.intent)

		return supervisorState{}, supervisorAction{}
	}
}

func selectNormalizationRootExit(
	t *testing.T,
	running runningReducerFixture,
	spec terminalNormalizationSpec,
) (supervisorState, supervisorAction) {
	t.Helper()
	if spec.rootAtDeadline {
		state, actions := running.reduceBundle(t, running.deadlineAt, nil, supervisorExitRecheck{
			performed: true, observed: true, at: running.deadlineAt,
			code: spec.exitCode, signal: spec.exitSignal,
		})
		assertSupervisorActions(t, actions, supervisorObserveEmptiness)

		return state, actions[0]
	}
	at := running.startedAt.Add(time.Second)
	fact := supervisorRunningFact{
		generation: running.generation, action: running.waitAction,
		kind: supervisorRunningRootExited, at: at,
		exitCode: spec.exitCode, exitSignal: spec.exitSignal,
	}
	state, actions := running.reduceBundle(t, at, []supervisorRunningFact{fact}, supervisorExitRecheck{})
	assertSupervisorActions(t, actions, supervisorObserveEmptiness)

	return state, actions[0]
}

func selectNormalizationFuse(
	t *testing.T,
	running runningReducerFixture,
) (supervisorState, supervisorAction) {
	t.Helper()
	at := running.startedAt.Add(time.Second)
	fact := supervisorRunningFact{
		generation: running.generation, action: running.sampleAction,
		kind: supervisorRunningFuseObserved, at: at, rootLive: true, live: 65,
	}
	state, actions := running.reduceBundle(t, at, []supervisorRunningFact{fact}, supervisorExitRecheck{})
	assertSupervisorActions(t, actions, supervisorForceOwned)

	return state, actions[0]
}

func selectNormalizationDeadline(
	t *testing.T,
	running runningReducerFixture,
	wantCount supervisorObservedCount,
) (supervisorState, supervisorAction) {
	t.Helper()
	if wantCount.present {
		at := running.startedAt.Add(time.Second)
		fact := supervisorRunningFact{
			generation: running.generation, action: running.sampleAction,
			kind: supervisorRunningFuseObserved, at: at, rootLive: true,
			live: uint64(wantCount.value),
		}
		state, actions := running.reduceBundle(t, at, []supervisorRunningFact{fact}, supervisorExitRecheck{})
		if len(actions) != 0 {
			t.Fatalf("pre-deadline peak selected terminal intent: %#v", actions)
		}
		running.state = state
	}
	state, actions := running.reduceBundle(t, running.deadlineAt, nil, supervisorExitRecheck{
		performed: true, at: running.deadlineAt,
	})
	assertSupervisorActions(t, actions, supervisorForceOwned)

	return state, actions[0]
}

func selectNormalizationStop(
	t *testing.T,
	running runningReducerFixture,
) (supervisorState, supervisorAction) {
	t.Helper()
	at := running.startedAt.Add(2 * time.Second)
	fact := supervisorRunningFact{
		generation: running.generation, kind: supervisorRunningStopRequested, at: at,
		stop: StopRequest{At: at, DrainBy: running.startedAt.Add(15 * time.Second)},
	}
	state, actions := running.reduceBundle(t, at, []supervisorRunningFact{fact}, supervisorExitRecheck{})
	assertSupervisorActions(t, actions, supervisorForceOwned)

	return state, actions[0]
}

func clampNormalizationStopWithEmergency(
	t *testing.T,
	running runningReducerFixture,
	state supervisorState,
	force supervisorAction,
) supervisorState {
	t.Helper()
	before := supervisorAttemptByGeneration(t, state, running.generation)
	emergencyAt := force.at.Add(time.Nanosecond)
	emergencyDrainBy := before.drain.effectiveDrainBy.Add(-time.Second)
	input := cloneSupervisorState(state)
	next, actions := reduceSupervisorMustAccept(t, state, supervisorEvent{
		kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: emergencyDrainBy,
		emergencySnapshots: []supervisorEmergencySnapshot{{
			generation: running.generation,
			running: &supervisorRunningBundle{
				generation: running.generation, sampleAction: running.sampleAction,
				waitAction: running.waitAction,
			},
		}},
	})
	if len(actions) != 0 {
		t.Fatalf("emergency clamp emitted competing action: %#v", actions)
	}
	after := supervisorAttemptByGeneration(t, next, running.generation)
	if after.phase != supervisorEmergencyDraining ||
		!reflect.DeepEqual(after.intent, before.intent) ||
		after.pendingAction != before.pendingAction ||
		!after.drain.effectiveDrainBy.Equal(emergencyDrainBy) {
		t.Fatalf("stop emergency clamp = %#v, want original intent/pending and bound %v", after, emergencyDrainBy)
	}
	if !reflect.DeepEqual(state, input) {
		t.Fatalf("stop emergency clamp mutated input: before=%#v after=%#v", input, state)
	}

	return next
}

func selectNormalizationRuntimeEmergency(
	t *testing.T,
	running runningReducerFixture,
) (supervisorState, supervisorAction) {
	t.Helper()
	at := running.startedAt.Add(5 * time.Second)
	drainBy := running.startedAt.Add(15 * time.Second)
	state, actions := reduceSupervisor(running.state, supervisorEvent{
		kind: supervisorEmergencyStarted, at: at, drainBy: drainBy,
		emergencySnapshots: []supervisorEmergencySnapshot{{
			generation: running.generation,
			running: &supervisorRunningBundle{
				generation: running.generation, sampleAction: running.sampleAction,
				waitAction: running.waitAction,
			},
		}},
	})
	assertSupervisorActions(t, actions, supervisorForceOwned)

	return state, actions[0]
}

func selectNormalizationObservationFailure(
	t *testing.T,
	running runningReducerFixture,
	spec terminalNormalizationSpec,
) (supervisorState, supervisorAction) {
	t.Helper()
	at := running.startedAt.Add(time.Second)
	facts := make([]supervisorRunningFact, 0, 2)
	if spec.runningDiagnostic != 0 {
		facts = append(facts, supervisorRunningFact{
			generation: running.generation, action: running.sampleAction,
			kind: supervisorRunningObservationFailed, at: at,
			source: supervisorObservationRunning, diagnostic: spec.runningDiagnostic,
		})
	}
	if spec.waitDiagnostic != 0 {
		facts = append(facts, supervisorRunningFact{
			generation: running.generation, action: running.waitAction,
			kind: supervisorRunningObservationFailed, at: at,
			source: supervisorObservationWait, diagnostic: spec.waitDiagnostic,
		})
	}
	state, actions := running.reduceBundle(t, at, facts, supervisorExitRecheck{})
	assertSupervisorActions(t, actions, supervisorForceOwned)

	return state, actions[0]
}

type terminalNormalizationMalformedSpec struct {
	name                   string
	specName               string
	clearCount             bool
	count                  supervisorObservedCount
	matchRunningPeak       bool
	absentCountValue       int
	exitCode               int
	runningDiagnostic      supervisorDiagnosticRef
	controlDiagnostic      supervisorDiagnosticRef
	drainDiagnostic        supervisorDiagnosticRef
	deadlineBeforeBoundary bool
	clearEmergencyEpoch    bool
	mismatchEmergencyEpoch bool
	mismatchEmergencyBound bool
}

func TestSupervisorReducerTerminalNormalizationRejectsMalformedEvidence(t *testing.T) {
	for _, test := range terminalNormalizationMalformedSpecs() {
		t.Run(test.name, func(t *testing.T) {
			assertMalformedTerminalNormalizationRejected(t, test)
		})
	}
}

func terminalNormalizationMalformedSpecs() []terminalNormalizationMalformedSpec {
	return []terminalNormalizationMalformedSpec{
		{name: "fuse requires count", specName: "fuse carries exact crossing count", clearCount: true},
		{
			name:     "fuse count must cross the ceiling",
			specName: "fuse carries exact crossing count",
			count:    supervisorObservedCount{present: true, value: 64},
		},
		{
			name:     "automatic deadline peak cannot cross the ceiling",
			specName: "automatic deadline retains peak",
			count:    supervisorObservedCount{present: true, value: 65}, matchRunningPeak: true,
		},
		{
			name:             "automatic deadline absent peak has zero value",
			specName:         "automatic deadline keeps absent peak explicit",
			absentCountValue: 7,
		},
		{
			name:     "root exit rejects count",
			specName: "nonzero root exit stays settled for issue 75",
			count:    supervisorObservedCount{present: true, value: 1},
		},
		{
			name:     "serial deadline rejects count",
			specName: "serial deadline has no count",
			count:    supervisorObservedCount{present: true, value: 7},
		},
		{
			name:     "stop rejects count",
			specName: "accepted stop request remains caller stop",
			count:    supervisorObservedCount{present: true, value: 1},
		},
		{
			name:     "runtime emergency rejects count",
			specName: "runtime emergency remains distinct stop",
			count:    supervisorObservedCount{present: true, value: 1},
		},
		{
			name:     "wait failure rejects count",
			specName: "equal-time wait failure wins and retains both diagnostics",
			count:    supervisorObservedCount{present: true, value: 1},
		},
		{name: "fuse rejects exit status", specName: "fuse carries exact crossing count", exitCode: 1},
		{name: "automatic deadline rejects exit status", specName: "automatic deadline retains peak", exitCode: 1},
		{name: "serial deadline rejects exit status", specName: "serial deadline has no count", exitCode: 1},
		{name: "stop rejects exit status", specName: "accepted stop request remains caller stop", exitCode: 1},
		{name: "runtime emergency rejects exit status", specName: "runtime emergency remains distinct stop", exitCode: 1},
		{
			name:     "running failure rejects exit status",
			specName: "running observation failure is distinct infrastructure",
			exitCode: 1,
		},
		{
			name:              "serial wait failure rejects running diagnostic",
			specName:          "serial wait observation failure remains wait infrastructure",
			runningDiagnostic: 202,
		},
		{
			name:              "serial root exit rejects running diagnostic",
			specName:          "serial root exit remains settled",
			runningDiagnostic: 202,
		},
		{
			name:              "direct empty rejects control diagnostic",
			specName:          "nonzero root exit stays settled for issue 75",
			controlDiagnostic: 606,
		},
		{
			name:            "direct empty rejects prior observation diagnostic",
			specName:        "nonzero root exit stays settled for issue 75",
			drainDiagnostic: 303,
		},
		{
			name:                   "automatic deadline rejects intent before deadline",
			specName:               "automatic deadline retains peak",
			deadlineBeforeBoundary: true,
		},
		{
			name:                "runtime emergency requires emergency epoch",
			specName:            "runtime emergency remains distinct stop",
			clearEmergencyEpoch: true,
		},
		{
			name:                   "runtime emergency rejects mismatched emergency epoch",
			specName:               "runtime emergency remains distinct stop",
			mismatchEmergencyEpoch: true,
		},
		{
			name:                   "runtime emergency rejects mismatched emergency bound",
			specName:               "runtime emergency remains distinct stop",
			mismatchEmergencyBound: true,
		},
	}
}

func assertMalformedTerminalNormalizationRejected(
	t *testing.T,
	test terminalNormalizationMalformedSpec,
) {
	t.Helper()
	spec := terminalNormalizationSpecNamed(t, test.specName)
	state, release := buildTerminalNormalizationRelease(t, spec)
	mutateMalformedTerminalCount(&state, test)
	mutateMalformedTerminalProvenance(&state, test)
	completionAt := release.at.Add(time.Nanosecond)
	completion := supervisorReleaseCompletion{
		generation: release.generation,
		action:     supervisorPendingAction{kind: release.kind, token: release.token},
		at:         completionAt,
	}
	before := cloneSupervisorState(state)
	assertSupervisorInvariant(t, func() {
		reduceSupervisor(state, supervisorEvent{
			kind: supervisorReleaseCompleted, generation: release.generation,
			at: completionAt, release: &completion,
		})
	})
	if !reflect.DeepEqual(state, before) {
		t.Fatalf("malformed normalization mutated input: before=%#v after=%#v", before, state)
	}
}

func mutateMalformedTerminalCount(
	state *supervisorState,
	test terminalNormalizationMalformedSpec,
) {
	if test.clearCount {
		state.attempts[0].intent.count = supervisorObservedCount{}
	}
	if test.count.present {
		state.attempts[0].intent.count = test.count
		if test.matchRunningPeak {
			state.attempts[0].runningPeak = test.count
		}
	}
	if test.absentCountValue != 0 {
		count := supervisorObservedCount{value: test.absentCountValue}
		state.attempts[0].intent.count = count
		state.attempts[0].runningPeak = count
	}
}

func mutateMalformedTerminalProvenance(
	state *supervisorState,
	test terminalNormalizationMalformedSpec,
) {
	if test.exitCode != 0 {
		state.attempts[0].intent.exitCode = test.exitCode
	}
	if test.runningDiagnostic != 0 {
		state.attempts[0].intent.diagnostics.running = test.runningDiagnostic
	}
	if test.controlDiagnostic != 0 {
		state.attempts[0].drain.controlDiagnostic = test.controlDiagnostic
	}
	if test.drainDiagnostic != 0 {
		state.attempts[0].drain.observationDiagnostic = test.drainDiagnostic
	}
	if test.deadlineBeforeBoundary {
		state.attempts[0].intent.at = state.attempts[0].deadlineAt.Add(-time.Nanosecond)
		state.attempts[0].intent.duration = state.attempts[0].intent.at.Sub(state.attempts[0].startedAt)
	}
	if test.clearEmergencyEpoch {
		state.emergency = supervisorEmergencyEpoch{}
	}
	if test.mismatchEmergencyEpoch {
		state.emergency.at = state.emergency.at.Add(time.Nanosecond)
	}
	if test.mismatchEmergencyBound {
		state.emergency.drainBy = state.emergency.drainBy.Add(time.Nanosecond)
	}
}

func terminalNormalizationSpecNamed(t *testing.T, name string) terminalNormalizationSpec {
	t.Helper()
	for _, spec := range terminalNormalizationSpecs() {
		if spec.name == name {
			return spec
		}
	}
	t.Fatalf("terminal normalization spec %q not found", name)

	return terminalNormalizationSpec{}
}
