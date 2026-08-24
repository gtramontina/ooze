package ooze

import (
	"reflect"
	"testing"
	"time"
)

type runtimeTransferReceiptFixture struct {
	state        supervisorState
	generation   attemptGeneration
	transfer     supervisorAction
	wantTerminal supervisorTerminalEvidence
	callerOwned  bool
}

func TestSupervisorReducerRuntimeTransferReceiptTransitionsResidualCustody(t *testing.T) {
	for _, test := range []struct {
		name  string
		build func(*testing.T) runtimeTransferReceiptFixture
	}{
		{
			name: "caller forced diagnostics",
			build: func(t *testing.T) runtimeTransferReceiptFixture {
				return newForcedRuntimeTransferReceiptFixture(t, true)
			},
		},
		{
			name:  "caller unforced root empty at drain bound",
			build: newRootRuntimeTransferReceiptFixture,
		},
		{
			name:  "late adopted zero intent",
			build: newLateAdoptedRuntimeTransferReceiptFixture,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := test.build(t)
			input := cloneSupervisorState(fixture.state)
			next, actions := reduceRuntimeTransferReceiptMustAccept(t, fixture)
			assertRuntimeTransferReceiptTransition(t, fixture, input, next, actions)
		})
	}
}

func TestSupervisorReducerRuntimeTransferReceiptCommutesWithEmergency(t *testing.T) {
	for _, test := range []struct {
		name  string
		build func(*testing.T) runtimeTransferReceiptFixture
	}{
		{name: "caller residual", build: newRootRuntimeTransferReceiptFixture},
		{name: "late adopted residual", build: newLateAdoptedRuntimeTransferReceiptFixture},
	} {
		t.Run(test.name, func(t *testing.T) {
			receiptFirst, receiptFirstDelivery := reduceRuntimeTransferReceiptAndEmergency(
				t, test.build(t), true,
			)
			emergencyFirst, emergencyFirstDelivery := reduceRuntimeTransferReceiptAndEmergency(
				t, test.build(t), false,
			)
			if !reflect.DeepEqual(receiptFirst, emergencyFirst) {
				t.Fatalf("runtime transfer receipt did not commute: receipt-first=%#v emergency-first=%#v",
					receiptFirst, emergencyFirst)
			}
			if !reflect.DeepEqual(receiptFirstDelivery, emergencyFirstDelivery) {
				t.Fatalf("runtime transfer delivery changed with ordering: receipt-first=%#v emergency-first=%#v",
					receiptFirstDelivery, emergencyFirstDelivery)
			}
		})
	}
}

func TestSupervisorReducerRuntimeTransferReceiptRejectsMalformedCustodyByteStable(t *testing.T) {
	for _, test := range runtimeTransferReceiptMalformedSpecs() {
		t.Run(test.name, func(t *testing.T) {
			fixture := test.build(t)
			completion, event := runtimeTransferReceiptEvent(fixture)
			completion.kind = test.receipt
			event.runtime = &completion
			if test.mutate != nil {
				test.mutate(&fixture.state, &event, &completion)
			}
			before := cloneSupervisorState(fixture.state)
			var actions []supervisorAction
			assertSupervisorInvariant(t, func() {
				_, actions = reduceSupervisor(fixture.state, event)
			})
			if len(actions) != 0 {
				t.Fatalf("malformed runtime transfer receipt emitted action: %#v", actions)
			}
			if !reflect.DeepEqual(fixture.state, before) {
				t.Fatalf("malformed runtime transfer receipt mutated input: before=%#v after=%#v",
					before, fixture.state)
			}
		})
	}
}

func TestSupervisorReducerRuntimeTransferReceiptRejectsMalformedEmergencyFirstByteStable(t *testing.T) {
	fixture := newLateAdoptedRuntimeTransferReceiptFixture(t)
	targetIndex := runtimeReceiptAttemptIndex(t, fixture.state, fixture.generation)
	fixture.state.attempts[targetIndex].releaseRevoked = false
	target := fixture.state.attempts[targetIndex]
	event := supervisorEvent{
		kind:    supervisorEmergencyStarted,
		at:      target.lastEventAt.Add(time.Nanosecond),
		drainBy: target.drain.effectiveDrainBy.Add(time.Second),
		emergencySnapshots: []supervisorEmergencySnapshot{{
			generation: fixture.generation,
		}},
	}
	before := cloneSupervisorState(fixture.state)
	var actions []supervisorAction
	assertSupervisorInvariant(t, func() {
		_, actions = reduceSupervisor(fixture.state, event)
	})
	if len(actions) != 0 {
		t.Fatalf("malformed emergency-first transfer emitted action: %#v", actions)
	}
	if !reflect.DeepEqual(fixture.state, before) {
		t.Fatalf("malformed emergency-first transfer mutated input: before=%#v after=%#v",
			before, fixture.state)
	}
}

func TestSupervisorReducerRuntimeTransferReceiptRejectsDuplicateByteStable(t *testing.T) {
	fixture := newRootRuntimeTransferReceiptFixture(t)
	settled, actions := reduceRuntimeTransferReceiptMustAccept(t, fixture)
	assertSupervisorActions(t, actions, supervisorDeliverTerminal)
	completion, event := runtimeTransferReceiptEvent(fixture)
	event.runtime = &completion
	assertRuntimeTransferReceiptInvariantByteStable(t, settled, event)
}

func newForcedRuntimeTransferReceiptFixture(
	t *testing.T,
	withDiagnostics bool,
) runtimeTransferReceiptFixture {
	t.Helper()
	running := newRunningReducerFixture(t, AutomaticProfile)
	facts := running.runningFacts([]runningFactSpec{{
		kind: supervisorRunningFuseObserved, offset: -time.Second, live: 7, rootLive: true,
	}})
	state, actions := running.reduceBundle(t, running.deadlineAt, facts, supervisorExitRecheck{
		performed: true, at: running.deadlineAt,
	})
	assertSupervisorActions(t, actions, supervisorForceOwned)
	drain := drainReducerFixture{generation: running.generation}
	controlDiagnostic := supervisorDiagnosticRef(0)
	drainDiagnostic := supervisorDiagnosticRef(0)
	outputDiagnostic := supervisorDiagnosticRef(0)
	if withDiagnostics {
		controlDiagnostic = 303
		drainDiagnostic = 404
		outputDiagnostic = 505
		state, actions = drain.complete(
			t, state, actions[0], supervisorDrainForceCompleted,
			running.deadlineAt.Add(time.Nanosecond), controlDiagnostic,
		)
		assertSupervisorActions(t, actions, supervisorObserveEmptiness)
		state, actions = drain.complete(
			t, state, actions[0], supervisorDrainObservationFailed,
			running.drainBy, drainDiagnostic,
		)
	} else {
		state, actions = drain.complete(
			t, state, actions[0], supervisorDrainForceCompleted, running.drainBy, 0,
		)
	}
	assertSupervisorActions(t, actions, supervisorCaptureOutput)
	prefixLength := uint64(8)
	if outputDiagnostic != 0 {
		prefixLength = 5
	}
	state, transfer := completeRuntimeTransferOutputPipeline(
		t, state, running.generation, actions[0], 301, 8, prefixLength, outputDiagnostic,
	)
	wantTerminal := supervisorTerminalEvidence{
		kind:            supervisorTerminalDrainUnconfirmed,
		profile:         AutomaticProfile,
		commandDeadline: 20 * time.Second,
		launchDuration:  time.Second - time.Nanosecond,
		commandDuration: 20 * time.Second,
		firedBound:      supervisorCommandDeadlineFired,
		output: supervisorOutputEvidence{
			ref: 301, cutoff: 8, prefixLength: prefixLength,
			completeThroughCutoff: outputDiagnostic == 0,
			final:                 false,
			diagnostic:            outputDiagnostic,
		},
		diagnostics: supervisorTerminalDiagnostics{
			drain: drainDiagnostic, control: controlDiagnostic,
		},
	}

	return runtimeTransferReceiptFixture{
		state: state, generation: running.generation, transfer: transfer,
		wantTerminal: wantTerminal, callerOwned: true,
	}
}

func newRootRuntimeTransferReceiptFixture(t *testing.T) runtimeTransferReceiptFixture {
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
		t, state, actions[0], supervisorDrainObservedEmpty, running.drainBy, 0,
	)
	assertSupervisorActions(t, actions, supervisorCaptureOutput)
	state, transfer := completeRuntimeTransferOutputPipeline(
		t, state, running.generation, actions[0], 302, 8, 8, 0,
	)
	wantTerminal := supervisorTerminalEvidence{
		kind:            supervisorTerminalDrainUnconfirmed,
		profile:         AutomaticProfile,
		commandDeadline: 20 * time.Second,
		launchDuration:  time.Second - time.Nanosecond,
		commandDuration: time.Second,
		firedBound:      supervisorNoCommandBound,
		output: supervisorOutputEvidence{
			ref: 302, cutoff: 8, prefixLength: 8,
			completeThroughCutoff: true,
			final:                 false,
		},
		diagnostics: supervisorTerminalDiagnostics{wait: 101, running: 202},
	}

	return runtimeTransferReceiptFixture{
		state: state, generation: running.generation, transfer: transfer,
		wantTerminal: wantTerminal, callerOwned: true,
	}
}

func newLateAdoptedRuntimeTransferReceiptFixture(t *testing.T) runtimeTransferReceiptFixture {
	t.Helper()
	const generation = attemptGeneration(97)
	launchBy := time.Unix(2_000, 0)
	state, launch := registeredReducerLaunch(t, generation, "attempt-late-transfer", launchBy)
	state, actions := reduceSupervisorMustAccept(t, state, supervisorEvent{
		kind: supervisorLaunchBoundary, generation: generation, at: launchBy,
	})
	assertSupervisorActions(t, actions, supervisorRevokeLaunchRelease, supervisorPublishLaunchUnconfirmed)
	releasedAt := launchBy.Add(time.Nanosecond)
	drainBy := releasedAt.Add(5 * time.Second)
	completion := supervisorLaunchCompletion{
		generation: generation, action: launch.token, at: releasedAt,
		kind: supervisorLaunchReleased,
	}
	state, actions = reduceSupervisorMustAccept(t, state, supervisorEvent{
		kind: supervisorLaunchCompleted, generation: generation, at: releasedAt,
		drainBy: drainBy, completion: &completion,
	})
	assertSupervisorActions(t, actions, supervisorAdoptOwned, supervisorForceOwned)
	drain := drainReducerFixture{generation: generation}
	state, actions = drain.complete(
		t, state, actions[1], supervisorDrainForceCompleted, drainBy, 0,
	)
	assertSupervisorActions(t, actions, supervisorCaptureOutput)
	state, transfer := completeRuntimeTransferOutputPipeline(
		t, state, generation, actions[0], 303, 3, 3, 0,
	)

	return runtimeTransferReceiptFixture{
		state: state, generation: generation, transfer: transfer,
	}
}

func completeRuntimeTransferOutputPipeline(
	t *testing.T,
	state supervisorState,
	generation attemptGeneration,
	capture supervisorAction,
	ref supervisorOutputRef,
	cutoff uint64,
	prefixLength uint64,
	diagnostic supervisorDiagnosticRef,
) (supervisorState, supervisorAction) {
	t.Helper()
	outputAt := capture.at.Add(time.Nanosecond)
	state, actions := completeReducerOutput(
		t, state, generation, capture, outputAt,
		ref, cutoff, prefixLength, diagnostic,
	)
	assertSupervisorActions(t, actions, supervisorSealStopAdmission)
	sealAt := actions[0].at.Add(time.Nanosecond)
	state, actions = completeReducerStopSeal(t, state, generation, actions[0], sealAt)
	assertSupervisorActions(t, actions, supervisorTransferResidualCustody)
	attempt := supervisorAttemptByGeneration(t, state, generation)
	if attempt.phase != supervisorTransferringResidualCustody ||
		attempt.pendingAction != (supervisorPendingAction{
			kind: supervisorTransferResidualCustody, token: actions[0].token,
		}) || attempt.drain.decision != supervisorDrainUnconfirmed || attempt.output.final ||
		attempt.terminal != (supervisorTerminalEvidence{}) || attempt.releaseDiagnostic != 0 {
		t.Fatalf("runtime transfer fixture lost residual custody: %#v action=%#v", attempt, actions[0])
	}

	return state, actions[0]
}

func reduceRuntimeTransferReceiptMustAccept(
	t *testing.T,
	fixture runtimeTransferReceiptFixture,
) (supervisorState, []supervisorAction) {
	t.Helper()
	completion, event := runtimeTransferReceiptEvent(fixture)
	event.runtime = &completion

	return reduceSupervisorMustAccept(t, fixture.state, event)
}

func runtimeTransferReceiptEvent(
	fixture runtimeTransferReceiptFixture,
) (supervisorRuntimeCompletion, supervisorEvent) {
	completion := supervisorRuntimeCompletion{
		generation: fixture.generation,
		action: supervisorPendingAction{
			kind: fixture.transfer.kind, token: fixture.transfer.token,
		},
		kind: supervisorRuntimeClosurePending,
	}
	event := supervisorEvent{
		kind: supervisorRuntimeCompleted, generation: fixture.generation,
	}

	return completion, event
}

func assertRuntimeTransferReceiptTransition(
	t *testing.T,
	fixture runtimeTransferReceiptFixture,
	input supervisorState,
	next supervisorState,
	actions []supervisorAction,
) {
	t.Helper()
	wantState := cloneSupervisorState(input)
	targetIndex := runtimeReceiptAttemptIndex(t, wantState, fixture.generation)
	wantAttempt := &wantState.attempts[targetIndex]
	wantAttempt.phase = supervisorAwaitingEmergencySettlement
	wantAttempt.pendingAction = supervisorPendingAction{}
	var wantActions []supervisorAction
	if fixture.callerOwned {
		wantState.nextAction++
		wantActions = []supervisorAction{{
			kind: supervisorDeliverTerminal, generation: fixture.generation,
			token: wantState.nextAction, terminal: fixture.wantTerminal,
			runtimeKind: supervisorRuntimeClosurePending,
		}}
	}
	if input.emergency.active {
		wantState.nextAction++
		settle := supervisorAction{
			kind: supervisorSettleEmergency, token: wantState.nextAction,
			resolutions: []supervisorEmergencyResolution{{
				generation: fixture.generation, kind: supervisorEmergencyResidualOwned,
			}},
		}
		wantActions = append(wantActions, settle)
		wantState.emergency.pendingAction = supervisorPendingAction{
			kind: settle.kind, token: settle.token,
		}
	}
	if !reflect.DeepEqual(actions, wantActions) {
		t.Fatalf("runtime transfer actions = %#v, want %#v", actions, wantActions)
	}
	if !reflect.DeepEqual(next, wantState) {
		t.Fatalf("runtime transfer state = %#v, want %#v", next, wantState)
	}
	got := supervisorAttemptByGeneration(t, next, fixture.generation)
	if got.terminal != (supervisorTerminalEvidence{}) ||
		got.pendingAction != (supervisorPendingAction{}) {
		t.Fatalf("runtime transfer stored terminal or retained action: %#v", got)
	}
	if !reflect.DeepEqual(fixture.state, input) {
		t.Fatalf("runtime transfer receipt mutated input: before=%#v after=%#v", input, fixture.state)
	}
}

func reduceRuntimeTransferReceiptAndEmergency(
	t *testing.T,
	fixture runtimeTransferReceiptFixture,
	receiptFirst bool,
) (supervisorState, []supervisorAction) {
	t.Helper()
	baseAttempt := supervisorAttemptByGeneration(t, fixture.state, fixture.generation)
	emergencyAt := baseAttempt.lastEventAt.Add(time.Nanosecond)
	emergencyDrainBy := baseAttempt.drain.effectiveDrainBy.Add(time.Second)
	state := fixture.state
	var combined []supervisorAction
	if !receiptFirst {
		state, combined = applyRuntimeTransferEmergency(
			t, state, fixture.generation, emergencyAt, emergencyDrainBy,
		)
		fixture.state = state
	}
	input := cloneSupervisorState(fixture.state)
	next, delivery := reduceRuntimeTransferReceiptMustAccept(t, fixture)
	assertRuntimeTransferReceiptTransition(t, fixture, input, next, delivery)
	state = next
	combined = append(combined, delivery...)
	if receiptFirst {
		var emergencyActions []supervisorAction
		state, emergencyActions = applyRuntimeTransferEmergency(
			t, state, fixture.generation, emergencyAt, emergencyDrainBy,
		)
		combined = append(combined, emergencyActions...)
	}
	completion, event := runtimeTransferReceiptEvent(fixture)
	event.runtime = &completion
	assertRuntimeTransferReceiptInvariantByteStable(t, state, event)

	return state, combined
}

func applyRuntimeTransferEmergency(
	t *testing.T,
	state supervisorState,
	generation attemptGeneration,
	at time.Time,
	drainBy time.Time,
) (supervisorState, []supervisorAction) {
	t.Helper()
	input := cloneSupervisorState(state)
	targetIndex := runtimeReceiptAttemptIndex(t, state, generation)
	wantBound := state.attempts[targetIndex].drain.effectiveDrainBy
	next, actions := reduceSupervisorMustAccept(t, state, supervisorEvent{
		kind: supervisorEmergencyStarted, at: at, drainBy: drainBy,
		emergencySnapshots: []supervisorEmergencySnapshot{{generation: generation}},
	})
	want := cloneSupervisorState(input)
	want.emergency = supervisorEmergencyEpoch{active: true, at: at, drainBy: drainBy}
	want.attempts[targetIndex].lastEventAt = at
	wantActions := make([]supervisorAction, 0)
	if state.attempts[targetIndex].phase == supervisorAwaitingEmergencySettlement {
		want.nextAction++
		settle := supervisorAction{
			kind: supervisorSettleEmergency, token: want.nextAction,
			resolutions: []supervisorEmergencyResolution{{
				generation: generation, kind: supervisorEmergencyResidualOwned,
			}},
		}
		wantActions = []supervisorAction{settle}
		want.emergency.pendingAction = supervisorPendingAction{
			kind: settle.kind, token: settle.token,
		}
	}
	if !reflect.DeepEqual(actions, wantActions) {
		t.Fatalf("runtime transfer emergency actions = %#v, want %#v", actions, wantActions)
	}
	if !reflect.DeepEqual(next, want) {
		t.Fatalf("runtime transfer emergency state = %#v, want %#v", next, want)
	}
	after := supervisorAttemptByGeneration(t, next, generation)
	if !after.drain.effectiveDrainBy.Equal(wantBound) ||
		after.phase != state.attempts[targetIndex].phase || after.terminal != (supervisorTerminalEvidence{}) {
		t.Fatalf("runtime transfer emergency restarted or lengthened custody: %#v", after)
	}
	if !reflect.DeepEqual(state, input) {
		t.Fatalf("runtime transfer emergency mutated input: before=%#v after=%#v", input, state)
	}

	return next, actions
}

type runtimeTransferReceiptMalformedSpec struct {
	name    string
	build   func(*testing.T) runtimeTransferReceiptFixture
	receipt supervisorRuntimeReceiptKind
	mutate  func(*supervisorState, *supervisorEvent, *supervisorRuntimeCompletion)
}

func runtimeTransferReceiptMalformedSpecs() []runtimeTransferReceiptMalformedSpec {
	forced := func(t *testing.T) runtimeTransferReceiptFixture {
		return newForcedRuntimeTransferReceiptFixture(t, false)
	}
	root := newRootRuntimeTransferReceiptFixture
	late := newLateAdoptedRuntimeTransferReceiptFixture

	return []runtimeTransferReceiptMalformedSpec{
		{name: "acknowledged", build: forced, receipt: supervisorRuntimeAcknowledged},
		{name: "provisional", build: forced, receipt: supervisorRuntimeProvisionalDeadline},
		{
			name: "wrong generation", build: forced, receipt: supervisorRuntimeClosurePending,
			mutate: func(_ *supervisorState, event *supervisorEvent, completion *supervisorRuntimeCompletion) {
				event.generation++
				completion.generation = event.generation
			},
		},
		{
			name: "wrong action kind", build: forced, receipt: supervisorRuntimeClosurePending,
			mutate: func(_ *supervisorState, _ *supervisorEvent, completion *supervisorRuntimeCompletion) {
				completion.action.kind = supervisorSettleRuntime
			},
		},
		{
			name: "wrong action token", build: forced, receipt: supervisorRuntimeClosurePending,
			mutate: func(_ *supervisorState, _ *supervisorEvent, completion *supervisorRuntimeCompletion) {
				completion.action.token++
			},
		},
		{
			name: "proven empty decision", build: forced, receipt: supervisorRuntimeClosurePending,
			mutate: func(state *supervisorState, event *supervisorEvent, _ *supervisorRuntimeCompletion) {
				state.attempts[runtimeTransferAttemptIndex(state, event.generation)].drain.decision =
					supervisorDrainProvenEmpty
			},
		},
		{
			name: "final output", build: forced, receipt: supervisorRuntimeClosurePending,
			mutate: func(state *supervisorState, event *supervisorEvent, _ *supervisorRuntimeCompletion) {
				state.attempts[runtimeTransferAttemptIndex(state, event.generation)].output.final = true
			},
		},
		{
			name: "prior terminal", build: forced, receipt: supervisorRuntimeClosurePending,
			mutate: func(state *supervisorState, event *supervisorEvent, _ *supervisorRuntimeCompletion) {
				state.attempts[runtimeTransferAttemptIndex(state, event.generation)].terminal.kind =
					supervisorTerminalSettled
			},
		},
		{
			name: "release diagnostic", build: forced, receipt: supervisorRuntimeClosurePending,
			mutate: func(state *supervisorState, event *supervisorEvent, _ *supervisorRuntimeCompletion) {
				state.attempts[runtimeTransferAttemptIndex(state, event.generation)].releaseDiagnostic = 707
			},
		},
		{
			name: "unforced non-root", build: forced, receipt: supervisorRuntimeClosurePending,
			mutate: func(state *supervisorState, event *supervisorEvent, _ *supervisorRuntimeCompletion) {
				state.attempts[runtimeTransferAttemptIndex(state, event.generation)].drain.forced = false
			},
		},
		{
			name: "unforced drain diagnostic", build: root, receipt: supervisorRuntimeClosurePending,
			mutate: func(state *supervisorState, event *supervisorEvent, _ *supervisorRuntimeCompletion) {
				state.attempts[runtimeTransferAttemptIndex(state, event.generation)].drain.observationDiagnostic = 909
			},
		},
		{
			name: "late adoption lacks revoked release", build: late, receipt: supervisorRuntimeClosurePending,
			mutate: func(state *supervisorState, event *supervisorEvent, _ *supervisorRuntimeCompletion) {
				state.attempts[runtimeTransferAttemptIndex(state, event.generation)].releaseRevoked = false
			},
		},
		{
			name: "transfer precedes effective drain bound", build: forced,
			receipt: supervisorRuntimeClosurePending,
			mutate: func(state *supervisorState, event *supervisorEvent, _ *supervisorRuntimeCompletion) {
				attempt := &state.attempts[runtimeTransferAttemptIndex(state, event.generation)]
				attempt.drain.effectiveDrainBy = attempt.lastEventAt.Add(time.Nanosecond)
			},
		},
		{
			name: "diagnostic output claims complete", build: forced,
			receipt: supervisorRuntimeClosurePending,
			mutate: func(state *supervisorState, event *supervisorEvent, _ *supervisorRuntimeCompletion) {
				attempt := &state.attempts[runtimeTransferAttemptIndex(state, event.generation)]
				attempt.output.diagnostic = 808
				attempt.output.completeThroughCutoff = true
			},
		},
	}
}

func runtimeTransferAttemptIndex(state *supervisorState, generation attemptGeneration) int {
	for index := range state.attempts {
		if state.attempts[index].generation == generation {
			return index
		}
	}

	return -1
}

func assertRuntimeTransferReceiptInvariantByteStable(
	t *testing.T,
	state supervisorState,
	event supervisorEvent,
) {
	t.Helper()
	before := cloneSupervisorState(state)
	var actions []supervisorAction
	assertSupervisorInvariant(t, func() {
		_, actions = reduceSupervisor(state, event)
	})
	if len(actions) != 0 {
		t.Fatalf("rejected runtime transfer receipt emitted action: %#v", actions)
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatalf("rejected runtime transfer receipt mutated input: before=%#v after=%#v", before, state)
	}
}
