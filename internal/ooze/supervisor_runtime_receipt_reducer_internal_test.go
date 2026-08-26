package ooze

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type runtimeReceiptReducerFixture struct {
	state              supervisorState
	generation         attemptGeneration
	sentinelGeneration attemptGeneration
	settle             supervisorAction
	terminal           supervisorTerminalEvidence
}

type runtimeReceiptPositiveSpec struct {
	name                   string
	terminalSpec           string
	receipt                supervisorRuntimeReceiptKind
	withEmergency          bool
	emergencyAfterDeferred bool
	deliver                bool
}

func TestSupervisorReducerRuntimeReceiptTransitionsExactlyOnce(t *testing.T) {
	for _, test := range []runtimeReceiptPositiveSpec{
		{
			name:         "acknowledged root removes and delivers",
			terminalSpec: "nonzero root exit stays settled for issue 75",
			receipt:      supervisorRuntimeAcknowledged,
			deliver:      true,
		},
		{
			name:          "acknowledged root after emergency still removes and delivers",
			terminalSpec:  "nonzero root exit stays settled for issue 75",
			receipt:       supervisorRuntimeAcknowledged,
			withEmergency: true,
			deliver:       true,
		},
		{
			name:         "acknowledged automatic deadline is not provisional",
			terminalSpec: "automatic deadline retains peak",
			receipt:      supervisorRuntimeAcknowledged,
			deliver:      true,
		},
		{
			name:         "provisional automatic deadline removes and delivers",
			terminalSpec: "automatic deadline retains peak",
			receipt:      supervisorRuntimeProvisionalDeadline,
			deliver:      true,
		},
		{
			name:                   "closure pending retains terminal before emergency",
			terminalSpec:           "nonzero root exit stays settled for issue 75",
			receipt:                supervisorRuntimeClosurePending,
			emergencyAfterDeferred: true,
		},
		{
			name:          "closure pending retains terminal after emergency",
			terminalSpec:  "nonzero root exit stays settled for issue 75",
			receipt:       supervisorRuntimeClosurePending,
			withEmergency: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertRuntimeReceiptTransition(t, test)
		})
	}
}

func TestSupervisorReducerRuntimeReceiptRejectsMalformedSettlingEmergencyByteStable(t *testing.T) {
	fixture := newRuntimeReceiptReducerFixture(
		t, "nonzero root exit stays settled for issue 75",
	)
	targetIndex := runtimeReceiptAttemptIndex(t, fixture.state, fixture.generation)
	fixture.state.attempts[targetIndex].terminal.output.ref++
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
	assert.EqualValues(t, 0, len(actions), "malformed settling emergency emitted action: %#v", actions)
	assert.Equal(t, before, fixture.state, "malformed settling emergency mutated input: before=%#v after=%#v", before, fixture.state)
}

func assertRuntimeReceiptTransition(t *testing.T, test runtimeReceiptPositiveSpec) {
	t.Helper()
	fixture := newRuntimeReceiptReducerFixture(t, test.terminalSpec)
	if test.withEmergency {
		fixture.state = interposeRuntimeReceiptEmergency(t, fixture)
	}
	input := cloneSupervisorState(fixture.state)
	completion := runtimeReceiptCompletion(fixture, test.receipt)
	next, actions := reduceSupervisorMustAccept(t, fixture.state, supervisorEvent{
		kind: supervisorRuntimeCompleted, generation: fixture.generation,
		runtime: &completion,
	})
	if test.deliver {
		assertDeliveredRuntimeReceipt(t, fixture, input, test.receipt, next, actions)
	} else {
		assertDeferredRuntimeReceipt(t, fixture, input, next, actions)
		if test.emergencyAfterDeferred {
			assertEmergencyAfterDeferredRuntimeReceipt(t, fixture, next)
		}
	}
	assert.Equal(t, input, fixture.state, "runtime receipt mutated input: before=%#v after=%#v", input, fixture.state)
}

func assertDeliveredRuntimeReceipt(
	t *testing.T,
	fixture runtimeReceiptReducerFixture,
	input supervisorState,
	receipt supervisorRuntimeReceiptKind,
	next supervisorState,
	actions []supervisorAction,
) {
	t.Helper()
	wantToken := input.nextAction + 1
	wantActions := []supervisorAction{{
		kind: supervisorDeliverTerminal, generation: fixture.generation,
		token: wantToken, terminal: fixture.terminal, runtimeKind: receipt,
	}}
	wantState := cloneSupervisorState(input)
	wantState.nextAction = wantToken
	wantState = removeRuntimeReceiptAttempt(t, wantState, fixture.generation)
	if input.emergency.active {
		wantState.nextAction++
		settle := supervisorAction{
			kind: supervisorSettleEmergency, token: wantState.nextAction,
		}
		wantActions = append(wantActions, settle)
		wantState.emergency.pendingAction = supervisorPendingAction{
			kind: settle.kind, token: settle.token,
		}
	}
	assert.Equal(t, wantActions, actions, "runtime delivery actions = %#v, want %#v", actions, wantActions)
	assert.Equal(t, wantState, next, "runtime delivery state = %#v, want %#v", next, wantState)
	assertRuntimeReceiptSentinelPreserved(t, input, next, fixture.sentinelGeneration)
}

func assertDeferredRuntimeReceipt(
	t *testing.T,
	fixture runtimeReceiptReducerFixture,
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
	if input.emergency.active {
		wantState.nextAction++
		settle := supervisorAction{
			kind: supervisorSettleEmergency, token: wantState.nextAction,
			resolutions: []supervisorEmergencyResolution{{
				generation: fixture.generation,
				kind:       supervisorEmergencyConfirmedDrained,
			}},
		}
		wantActions = []supervisorAction{settle}
		wantState.emergency.pendingAction = supervisorPendingAction{
			kind: settle.kind, token: settle.token,
		}
	}
	assert.Equal(t, wantActions, actions, "closure-pending receipt actions = %#v, want %#v", actions, wantActions)
	assert.Equal(t, wantState, next, "closure-pending state = %#v, want %#v", next, wantState)
	assert.Equal(t, fixture.terminal, wantAttempt.terminal, "closure-pending terminal = %#v, want %#v", wantAttempt.terminal, fixture.terminal)
	assertRuntimeReceiptSentinelPreserved(t, input, next, fixture.sentinelGeneration)
}

func assertEmergencyAfterDeferredRuntimeReceipt(
	t *testing.T,
	fixture runtimeReceiptReducerFixture,
	deferred supervisorState,
) {
	t.Helper()
	targetIndex := runtimeReceiptAttemptIndex(t, deferred, fixture.generation)
	target := deferred.attempts[targetIndex]
	emergencyAt := target.lastEventAt.Add(time.Nanosecond)
	emergencyDrainBy := target.drain.effectiveDrainBy.Add(time.Second)
	input := cloneSupervisorState(deferred)
	next, actions := reduceSupervisorMustAccept(t, deferred, supervisorEvent{
		kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: emergencyDrainBy,
		emergencySnapshots: []supervisorEmergencySnapshot{{generation: fixture.generation}},
	})
	want := cloneSupervisorState(input)
	want.emergency = supervisorEmergencyEpoch{
		active: true, at: emergencyAt, drainBy: emergencyDrainBy,
	}
	want.attempts[targetIndex].lastEventAt = emergencyAt
	want.nextAction++
	settle := supervisorAction{
		kind: supervisorSettleEmergency, token: want.nextAction,
		resolutions: []supervisorEmergencyResolution{{
			generation: fixture.generation,
			kind:       supervisorEmergencyConfirmedDrained,
		}},
	}
	want.emergency.pendingAction = supervisorPendingAction{
		kind: settle.kind, token: settle.token,
	}
	assert.Equal(t, []supervisorAction{settle}, actions, "post-receipt emergency actions = %#v, want %#v", actions, []supervisorAction{settle})
	assert.Equal(t, want, next, "post-receipt emergency state = %#v, want %#v", next, want)
	after := next.attempts[targetIndex]
	assert.Equal(t, supervisorAwaitingEmergencySettlement, after.phase, "post-receipt emergency target = %#v", after)
	assert.Equal(t, (supervisorPendingAction{}), after.pendingAction, "post-receipt emergency target = %#v", after)
	assert.Equal(t, fixture.terminal, after.terminal, "post-receipt emergency target = %#v", after)
	assertRuntimeReceiptSentinelPreserved(t, input, next, fixture.sentinelGeneration)
	assert.Equal(t, input, deferred, "post-receipt emergency mutated input: before=%#v after=%#v", input, deferred)
}

func newRuntimeReceiptReducerFixture(
	t *testing.T,
	terminalSpec string,
) runtimeReceiptReducerFixture {
	t.Helper()
	spec := terminalNormalizationSpecNamed(t, terminalSpec)
	releasing, release := buildTerminalNormalizationRelease(t, spec)
	releaseAt := release.at.Add(time.Nanosecond)
	releaseCompletion := supervisorReleaseCompletion{
		generation: release.generation,
		action: supervisorPendingAction{
			kind: release.kind, token: release.token,
		},
		at: releaseAt,
	}
	settling, actions := reduceSupervisorMustAccept(t, releasing, supervisorEvent{
		kind: supervisorReleaseCompleted, generation: release.generation,
		at: releaseAt, release: &releaseCompletion,
	})
	assertSupervisorActions(t, actions, supervisorSettleRuntime)
	attempt := supervisorAttemptByGeneration(t, settling, release.generation)
	assert.Equal(t, supervisorSettlingRuntime, attempt.phase, "runtime receipt fixture = %#v actions=%#v", attempt, actions)
	assert.NotEqual(t, 0, attempt.terminal.kind, "runtime receipt fixture = %#v actions=%#v", attempt, actions)
	assert.Equal(t, (supervisorPendingAction{
		kind: supervisorSettleRuntime, token: actions[0].token,
	}), attempt.pendingAction, "runtime receipt fixture = %#v actions=%#v", attempt, actions)

	settling, sentinelGeneration := appendRuntimeReceiptSentinel(t, settling, attempt.lastEventAt)

	return runtimeReceiptReducerFixture{
		state: settling, generation: release.generation,
		sentinelGeneration: sentinelGeneration,
		settle:             actions[0], terminal: attempt.terminal,
	}
}

func appendRuntimeReceiptSentinel(
	t *testing.T,
	state supervisorState,
	after time.Time,
) (supervisorState, attemptGeneration) {
	t.Helper()
	const generation = attemptGeneration(1 << 60)
	launchBy := after.Add(2 * time.Second)
	registered, launch := appendReducerLaunchWithFacts(
		t, state, generation, "runtime-receipt-sentinel",
		AutomaticProfile, 20*time.Second, launchBy,
	)
	completionAt := launchBy.Add(-500 * time.Millisecond)
	completion := supervisorLaunchCompletion{
		generation: generation, action: launch.token, at: completionAt,
		kind: supervisorLaunchProvenNotReleased, failure: LaunchFailed,
	}
	input := cloneSupervisorState(registered)
	closed, actions := reduceSupervisorMustAccept(t, registered, supervisorEvent{
		kind: supervisorLaunchCompleted, generation: generation,
		at: completionAt, completion: &completion,
	})
	wantToken := registered.nextAction + 1
	wantAction := supervisorAction{
		kind: supervisorPublishNotReleased, generation: generation,
		token: wantToken, at: completionAt,
		launchKind:    supervisorLaunchProvenNotReleased,
		launchFailure: LaunchFailed, launchDuration: 500 * time.Millisecond,
	}
	wantState := cloneSupervisorState(registered)
	wantState.nextAction = wantToken
	sentinelIndex := runtimeReceiptAttemptIndex(t, wantState, generation)
	wantState.attempts[sentinelIndex].lastEventAt = completionAt
	wantState.attempts[sentinelIndex].phase = supervisorLaunchClosedNotReleased
	assert.Equal(t, []supervisorAction{wantAction}, actions, "sentinel close actions = %#v, want %#v", actions, []supervisorAction{wantAction})
	assert.Equal(t, wantState, closed, "sentinel close state = %#v, want %#v", closed, wantState)
	assert.Equal(t, input, registered, "sentinel close mutated input: before=%#v after=%#v", input, registered)

	return closed, generation
}

func interposeRuntimeReceiptEmergency(
	t *testing.T,
	fixture runtimeReceiptReducerFixture,
) supervisorState {
	t.Helper()
	attempt := supervisorAttemptByGeneration(t, fixture.state, fixture.generation)
	emergencyAt := attempt.lastEventAt.Add(time.Nanosecond)
	emergencyDrainBy := attempt.drain.effectiveDrainBy.Add(time.Second)
	input := cloneSupervisorState(fixture.state)
	next, actions := reduceSupervisorMustAccept(t, fixture.state, supervisorEvent{
		kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: emergencyDrainBy,
		emergencySnapshots: []supervisorEmergencySnapshot{{generation: fixture.generation}},
	})
	assert.EqualValues(t, 0, len(actions), "runtime-receipt emergency emitted action: %#v", actions)
	want := cloneSupervisorState(input)
	want.emergency = supervisorEmergencyEpoch{
		active: true, at: emergencyAt, drainBy: emergencyDrainBy,
	}
	targetIndex := runtimeReceiptAttemptIndex(t, want, fixture.generation)
	want.attempts[targetIndex].lastEventAt = emergencyAt
	assert.Equal(t, want, next, "runtime-receipt emergency state = %#v, want %#v", next, want)
	assert.Equal(t, input, fixture.state, "runtime-receipt emergency mutated input: before=%#v after=%#v", input, fixture.state)

	return next
}

func runtimeReceiptAttemptIndex(
	t *testing.T,
	state supervisorState,
	generation attemptGeneration,
) int {
	t.Helper()
	for index, attempt := range state.attempts {
		if attempt.generation == generation {
			return index
		}
	}
	require.FailNowf(t, "runtime receipt generation absent from state", "generation %d, state %#v", generation, state)

	return -1
}

func removeRuntimeReceiptAttempt(
	t *testing.T,
	state supervisorState,
	generation attemptGeneration,
) supervisorState {
	t.Helper()
	index := runtimeReceiptAttemptIndex(t, state, generation)
	copy(state.attempts[index:], state.attempts[index+1:])
	state.attempts = state.attempts[:len(state.attempts)-1]

	return state
}

func assertRuntimeReceiptSentinelPreserved(
	t *testing.T,
	before supervisorState,
	after supervisorState,
	generation attemptGeneration,
) {
	t.Helper()
	want := before.attempts[runtimeReceiptAttemptIndex(t, before, generation)]
	got := after.attempts[runtimeReceiptAttemptIndex(t, after, generation)]
	assert.Equal(t, want, got, "runtime receipt rewrote sentinel: got=%#v want=%#v", got, want)
}

func runtimeReceiptCompletion(
	fixture runtimeReceiptReducerFixture,
	kind supervisorRuntimeReceiptKind,
) supervisorRuntimeCompletion {
	return supervisorRuntimeCompletion{
		generation: fixture.generation,
		action: supervisorPendingAction{
			kind: fixture.settle.kind, token: fixture.settle.token,
		},
		kind: kind,
	}
}

type runtimeReceiptMalformedSpec struct {
	name         string
	terminalSpec string
	receipt      supervisorRuntimeReceiptKind
	mutate       func(*supervisorState, *supervisorEvent, *supervisorRuntimeCompletion)
}

func TestSupervisorReducerRuntimeReceiptRejectsMalformedEvidenceByteStable(t *testing.T) {
	for _, test := range runtimeReceiptMalformedSpecs() {
		t.Run(test.name, func(t *testing.T) {
			assertMalformedRuntimeReceiptRejected(t, test)
		})
	}
}

func runtimeReceiptMalformedSpecs() []runtimeReceiptMalformedSpec {
	root := "nonzero root exit stays settled for issue 75"
	deadline := "automatic deadline retains peak"

	return []runtimeReceiptMalformedSpec{
		{
			name: "nil payload", terminalSpec: root, receipt: supervisorRuntimeAcknowledged,
			mutate: func(_ *supervisorState, event *supervisorEvent, _ *supervisorRuntimeCompletion) {
				event.runtime = nil
			},
		},
		{
			name: "outer generation mismatch", terminalSpec: root,
			receipt: supervisorRuntimeAcknowledged,
			mutate: func(_ *supervisorState, event *supervisorEvent, _ *supervisorRuntimeCompletion) {
				event.generation++
			},
		},
		{
			name: "inner generation mismatch", terminalSpec: root,
			receipt: supervisorRuntimeAcknowledged,
			mutate: func(_ *supervisorState, _ *supervisorEvent, completion *supervisorRuntimeCompletion) {
				completion.generation++
			},
		},
		{
			name: "stale generation", terminalSpec: root, receipt: supervisorRuntimeAcknowledged,
			mutate: func(_ *supervisorState, event *supervisorEvent, completion *supervisorRuntimeCompletion) {
				event.generation++
				completion.generation = event.generation
			},
		},
		{
			name: "zero action", terminalSpec: root, receipt: supervisorRuntimeAcknowledged,
			mutate: func(_ *supervisorState, _ *supervisorEvent, completion *supervisorRuntimeCompletion) {
				completion.action = supervisorPendingAction{}
			},
		},
		{
			name: "wrong action kind", terminalSpec: root, receipt: supervisorRuntimeAcknowledged,
			mutate: func(_ *supervisorState, _ *supervisorEvent, completion *supervisorRuntimeCompletion) {
				completion.action.kind = supervisorReleaseDomain
			},
		},
		{
			name: "wrong action token", terminalSpec: root, receipt: supervisorRuntimeAcknowledged,
			mutate: func(_ *supervisorState, _ *supervisorEvent, completion *supervisorRuntimeCompletion) {
				completion.action.token++
			},
		},
		{
			name: "invalid receipt kind", terminalSpec: root, receipt: 99,
		},
		{
			name:         "stored terminal kind contradicts immutable root evidence",
			terminalSpec: root, receipt: supervisorRuntimeAcknowledged,
			mutate: func(state *supervisorState, event *supervisorEvent, _ *supervisorRuntimeCompletion) {
				for index := range state.attempts {
					if state.attempts[index].generation == event.generation {
						state.attempts[index].terminal.kind = supervisorTerminalStopped

						return
					}
				}
			},
		},
		{
			name: "provisional wrong action", terminalSpec: deadline,
			receipt: supervisorRuntimeProvisionalDeadline,
			mutate: func(_ *supervisorState, _ *supervisorEvent, completion *supervisorRuntimeCompletion) {
				completion.action.kind = supervisorReleaseDomain
			},
		},
		{
			name: "provisional non-deadline terminal", terminalSpec: root,
			receipt: supervisorRuntimeProvisionalDeadline,
		},
		{
			name: "event carries instant", terminalSpec: root, receipt: supervisorRuntimeAcknowledged,
			mutate: func(_ *supervisorState, event *supervisorEvent, _ *supervisorRuntimeCompletion) {
				event.at = time.Unix(9_999, 0)
			},
		},
	}
}

func assertMalformedRuntimeReceiptRejected(
	t *testing.T,
	test runtimeReceiptMalformedSpec,
) {
	t.Helper()
	fixture := newRuntimeReceiptReducerFixture(t, test.terminalSpec)
	completion := runtimeReceiptCompletion(fixture, test.receipt)
	event := supervisorEvent{
		kind: supervisorRuntimeCompleted, generation: fixture.generation,
		runtime: &completion,
	}
	if test.mutate != nil {
		test.mutate(&fixture.state, &event, &completion)
	}
	before := cloneSupervisorState(fixture.state)
	assertSupervisorInvariant(t, func() {
		reduceSupervisor(fixture.state, event)
	})
	assert.Equal(t, before, fixture.state, "rejected runtime receipt mutated input: before=%#v after=%#v", before, fixture.state)
}

func TestSupervisorReducerRuntimeReceiptRejectsUnrelatedEventPayload(t *testing.T) {
	for _, test := range runtimeReceiptUnrelatedPayloads() {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRuntimeReceiptReducerFixture(
				t, "nonzero root exit stays settled for issue 75",
			)
			completion := runtimeReceiptCompletion(fixture, supervisorRuntimeAcknowledged)
			event := supervisorEvent{
				kind: supervisorRuntimeCompleted, generation: fixture.generation,
				runtime: &completion,
			}
			test.mutate(&event)
			before := cloneSupervisorState(fixture.state)
			assertSupervisorInvariant(t, func() {
				reduceSupervisor(fixture.state, event)
			})
			assert.Equal(t, before, fixture.state, "unrelated payload mutated input: before=%#v after=%#v", before, fixture.state)
		})
	}
}

type runtimeReceiptPayloadSpec struct {
	name   string
	mutate func(*supervisorEvent)
}

func runtimeReceiptUnrelatedPayloads() []runtimeReceiptPayloadSpec {
	instant := time.Unix(8_000, 0)

	return []runtimeReceiptPayloadSpec{
		{name: "attempt", mutate: func(event *supervisorEvent) { event.attempt = "unrelated" }},
		{name: "launch deadline", mutate: func(event *supervisorEvent) { event.launchBy = instant }},
		{name: "drain deadline", mutate: func(event *supervisorEvent) { event.drainBy = instant }},
		{
			name:   "launch completion",
			mutate: func(event *supervisorEvent) { event.completion = &supervisorLaunchCompletion{} },
		},
		{
			name: "emergency snapshots",
			mutate: func(event *supervisorEvent) {
				event.emergencySnapshots = []supervisorEmergencySnapshot{{generation: event.generation}}
			},
		},
		{name: "profile", mutate: func(event *supervisorEvent) { event.profile = AutomaticProfile }},
		{
			name:   "command deadline",
			mutate: func(event *supervisorEvent) { event.commandDeadline = time.Second },
		},
		{
			name:   "running bundle",
			mutate: func(event *supervisorEvent) { event.running = &supervisorRunningBundle{} },
		},
		{
			name:   "drain completion",
			mutate: func(event *supervisorEvent) { event.drain = &supervisorDrainCompletion{} },
		},
		{
			name:   "output completion",
			mutate: func(event *supervisorEvent) { event.output = &supervisorOutputCompletion{} },
		},
		{
			name:   "stop seal completion",
			mutate: func(event *supervisorEvent) { event.seal = &supervisorStopSealCompletion{} },
		},
		{
			name:   "release completion",
			mutate: func(event *supervisorEvent) { event.release = &supervisorReleaseCompletion{} },
		},
	}
}

func TestSupervisorReducerRuntimeReceiptRejectsDuplicateAndContradictoryReceipt(t *testing.T) {
	t.Run("duplicate after acknowledged removal", func(t *testing.T) {
		fixture := newRuntimeReceiptReducerFixture(
			t, "nonzero root exit stays settled for issue 75",
		)
		completion := runtimeReceiptCompletion(fixture, supervisorRuntimeAcknowledged)
		event := supervisorEvent{
			kind: supervisorRuntimeCompleted, generation: fixture.generation,
			runtime: &completion,
		}
		settled, actions := reduceSupervisorMustAccept(t, fixture.state, event)
		assertSupervisorActions(t, actions, supervisorDeliverTerminal)
		assertRuntimeReceiptInvariantByteStable(t, settled, event)
	})

	t.Run("duplicate and contradictory after deferred", func(t *testing.T) {
		fixture := newRuntimeReceiptReducerFixture(
			t, "nonzero root exit stays settled for issue 75",
		)
		completion := runtimeReceiptCompletion(fixture, supervisorRuntimeClosurePending)
		event := supervisorEvent{
			kind: supervisorRuntimeCompleted, generation: fixture.generation,
			runtime: &completion,
		}
		deferred, actions := reduceSupervisorMustAccept(t, fixture.state, event)
		assert.EqualValues(t, 0, len(actions), "deferred receipt actions = %#v", actions)
		assertRuntimeReceiptInvariantByteStable(t, deferred, event)
		contradictory := completion
		contradictory.kind = supervisorRuntimeAcknowledged
		contradictoryEvent := event
		contradictoryEvent.runtime = &contradictory
		assertRuntimeReceiptInvariantByteStable(t, deferred, contradictoryEvent)
	})
}

func assertRuntimeReceiptInvariantByteStable(
	t *testing.T,
	state supervisorState,
	event supervisorEvent,
) {
	t.Helper()
	before := cloneSupervisorState(state)
	assertSupervisorInvariant(t, func() {
		reduceSupervisor(state, event)
	})
	assert.Equal(t, before, state, "duplicate runtime receipt mutated input: before=%#v after=%#v", before, state)
}

func TestSupervisorReducerRuntimeReceiptDataRemainsCapabilityFree(t *testing.T) {
	for _, dataType := range []reflect.Type{
		reflect.TypeOf(supervisorRuntimeCompletion{}),
		reflect.TypeOf(supervisorState{}),
		reflect.TypeOf(supervisorEvent{}),
		reflect.TypeOf(supervisorAction{}),
	} {
		t.Run(dataType.Name(), func(t *testing.T) {
			assertReducerDataOnly(t, dataType, make(map[reflect.Type]bool), dataType.Name())
		})
	}
}
