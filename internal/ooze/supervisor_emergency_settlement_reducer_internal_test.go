package ooze

import (
	"reflect"
	"testing"
	"time"
)

const (
	emergencyDeferredGeneration attemptGeneration = 201 + iota
	emergencySentinelGeneration
	emergencyCallerResidualGeneration
	emergencyLateDrainedGeneration
	emergencyLateResidualGeneration
	emergencyLateNoReleaseGeneration
	emergencyMixedLateNoReleaseGeneration
)

type emergencySettlementLedger struct {
	state                  supervisorState
	deferredTerminal       supervisorTerminalEvidence
	callerResidualTerminal supervisorTerminalEvidence
	settle                 supervisorAction
	transfer               supervisorAction
	lateRelease            supervisorAction
	lateTransfer           supervisorAction
}

func TestSupervisorReducerEmergencySettlementBatchesMixedLedgerAfterLastReceipt(t *testing.T) {
	ledger := newEmergencySettlementLedger(t, "transfer")
	active, emergencyEvent := beginEmergencySettlement(t, ledger.state, false)
	if active.emergency.pendingAction != (supervisorPendingAction{}) {
		t.Fatalf("not-ready emergency installed settlement action: %#v", active.emergency)
	}

	input := cloneSupervisorState(active)
	completion := supervisorRuntimeCompletion{
		generation: emergencyCallerResidualGeneration,
		action: supervisorPendingAction{
			kind: ledger.transfer.kind, token: ledger.transfer.token,
		},
		kind: supervisorRuntimeClosurePending,
	}
	next, actions := reduceSupervisorMustAccept(t, active, supervisorEvent{
		kind: supervisorRuntimeCompleted, generation: emergencyCallerResidualGeneration,
		runtime: &completion,
	})
	wantResolutions := mixedEmergencySettlementResolutions()
	wantDeliveryToken := input.nextAction + 1
	wantSettlementToken := wantDeliveryToken + 1
	wantActions := []supervisorAction{
		{
			kind: supervisorDeliverTerminal, generation: emergencyCallerResidualGeneration,
			token: wantDeliveryToken, terminal: ledger.callerResidualTerminal,
			runtimeKind: supervisorRuntimeClosurePending,
		},
		{
			kind: supervisorSettleEmergency, token: wantSettlementToken,
			resolutions: wantResolutions,
		},
	}
	wantState := cloneSupervisorState(input)
	wantState.nextAction = wantSettlementToken
	wantState.emergency.pendingAction = supervisorPendingAction{
		kind: supervisorSettleEmergency, token: wantSettlementToken,
	}
	target := &wantState.attempts[runtimeReceiptAttemptIndex(
		t, wantState, emergencyCallerResidualGeneration,
	)]
	target.phase = supervisorAwaitingEmergencySettlement
	target.pendingAction = supervisorPendingAction{}
	if !reflect.DeepEqual(actions, wantActions) {
		t.Fatalf("last local receipt actions = %#v, want %#v", actions, wantActions)
	}
	if !reflect.DeepEqual(next, wantState) {
		t.Fatalf("last local receipt state = %#v, want %#v", next, wantState)
	}
	if !reflect.DeepEqual(active, input) {
		t.Fatalf("last local receipt mutated input: before=%#v after=%#v", input, active)
	}
	assertEmergencySettlementSentinel(t, next)
	if emergencyEvent.generation != 0 {
		t.Fatalf("emergency setup manufactured generation: %#v", emergencyEvent)
	}
}

func TestSupervisorReducerEmergencySettlementTriggersAtEveryReadyTransition(t *testing.T) {
	t.Run("all ready then emergency starts", func(t *testing.T) {
		ledger := newEmergencySettlementLedger(t, "")
		if ledger.state.emergency.active || ledger.state.emergency.pendingAction != (supervisorPendingAction{}) {
			t.Fatalf("inactive ready inventory settled early: %#v", ledger.state.emergency)
		}
		beginEmergencySettlement(t, ledger.state, true)
	})

	for _, hold := range []string{"settle", "transfer", "late transfer"} {
		t.Run("emergency then last "+hold+" receipt", func(t *testing.T) {
			assertEmergencySettlementLastReceiptTrigger(t, hold)
		})
	}

	t.Run("acknowledged receipt removal", func(t *testing.T) {
		ledger := newEmergencySettlementLedger(t, "settle")
		active, _ := beginEmergencySettlement(t, ledger.state, false)
		completion := supervisorRuntimeCompletion{
			generation: emergencyDeferredGeneration,
			action: supervisorPendingAction{
				kind: ledger.settle.kind, token: ledger.settle.token,
			},
			kind: supervisorRuntimeAcknowledged,
		}
		input := cloneSupervisorState(active)
		next, actions := reduceSupervisorMustAccept(t, active, supervisorEvent{
			kind: supervisorRuntimeCompleted, generation: emergencyDeferredGeneration,
			runtime: &completion,
		})
		assertEmergencySettlementTail(t, input, next, actions)
		if next.attemptIndex(emergencyDeferredGeneration) >= 0 {
			t.Fatalf("acknowledged attempt remained in settlement inventory: %#v", next.attempts)
		}
	})

	t.Run("late release completes", func(t *testing.T) {
		ledger := newEmergencySettlementLedger(t, "late release")
		active, _ := beginEmergencySettlement(t, ledger.state, false)
		at := active.emergency.at.Add(time.Nanosecond)
		completion := supervisorReleaseCompletion{
			generation: emergencyLateDrainedGeneration,
			action: supervisorPendingAction{
				kind: ledger.lateRelease.kind, token: ledger.lateRelease.token,
			},
			at: at,
		}
		input := cloneSupervisorState(active)
		next, actions := reduceSupervisorMustAccept(t, active, supervisorEvent{
			kind: supervisorReleaseCompleted, generation: emergencyLateDrainedGeneration,
			at: at, release: &completion,
		})
		assertEmergencySettlementTail(t, input, next, actions)
	})

	t.Run("active empty inventory", func(t *testing.T) {
		beginEmergencySettlement(t, supervisorState{}, true)
	})
}

func assertEmergencySettlementLastReceiptTrigger(t *testing.T, hold string) {
	t.Helper()
	ledger := newEmergencySettlementLedger(t, hold)
	active, _ := beginEmergencySettlement(t, ledger.state, false)
	pending := ledger.settle
	generation := emergencyDeferredGeneration
	switch hold {
	case "transfer":
		pending = ledger.transfer
		generation = emergencyCallerResidualGeneration
	case "late transfer":
		pending = ledger.lateTransfer
		generation = emergencyLateResidualGeneration
	}
	completion := supervisorRuntimeCompletion{
		generation: generation,
		action:     supervisorPendingAction{kind: pending.kind, token: pending.token},
		kind:       supervisorRuntimeClosurePending,
	}
	input := cloneSupervisorState(active)
	next, actions := reduceSupervisorMustAccept(t, active, supervisorEvent{
		kind: supervisorRuntimeCompleted, generation: generation, runtime: &completion,
	})
	assertEmergencySettlementTail(t, input, next, actions)
	if hold == "late transfer" {
		assertLateTransferSettlement(t, input, active, next, actions, generation)
	}
}

func assertLateTransferSettlement(
	t *testing.T,
	input supervisorState,
	active supervisorState,
	next supervisorState,
	actions []supervisorAction,
	generation attemptGeneration,
) {
	t.Helper()
	wantToken := input.nextAction + 1
	wantActions := []supervisorAction{{
		kind: supervisorSettleEmergency, token: wantToken,
		resolutions: mixedEmergencySettlementResolutions(),
	}}
	want := cloneSupervisorState(input)
	want.nextAction = wantToken
	want.emergency.pendingAction = supervisorPendingAction{
		kind: supervisorSettleEmergency, token: wantToken,
	}
	target := &want.attempts[runtimeReceiptAttemptIndex(t, want, generation)]
	target.phase = supervisorAwaitingEmergencySettlement
	target.pendingAction = supervisorPendingAction{}
	if !reflect.DeepEqual(actions, wantActions) || !reflect.DeepEqual(next, want) {
		t.Fatalf("late transfer settlement = %#v actions=%#v, want %#v/%#v", next, actions, want, wantActions)
	}
	if !reflect.DeepEqual(active, input) {
		t.Fatalf("late transfer settlement mutated input: before=%#v after=%#v", input, active)
	}
}

func TestSupervisorReducerEmergencySettlementCompletionDeliversDeferredTerminalsOnce(t *testing.T) {
	ledger := newEmergencySettlementLedger(t, "")
	pending, _ := beginEmergencySettlement(t, ledger.state, true)
	input := cloneSupervisorState(pending)
	completion, event := emergencySettlementCompletionEvent(pending)
	next, actions := reduceSupervisorMustAccept(t, pending, event)
	wantToken := input.nextAction + 1
	wantActions := []supervisorAction{
		{
			kind: supervisorDeliverTerminal, generation: emergencyDeferredGeneration,
			token: wantToken, terminal: ledger.deferredTerminal,
			runtimeKind: supervisorRuntimeClosurePending,
		},
		{
			kind: supervisorDeliverEmergencySettlement, token: wantToken + 1,
			residuals: mixedEmergencySettlementDeliveryResiduals(),
		},
	}
	wantState := cloneSupervisorState(input)
	wantState.nextAction = wantToken + 1
	wantState.emergency.pendingAction = supervisorPendingAction{}
	wantState.attempts = []supervisorAttemptState{
		input.attempts[runtimeReceiptAttemptIndex(t, input, emergencySentinelGeneration)],
	}
	if !reflect.DeepEqual(actions, wantActions) {
		t.Fatalf("emergency completion delivery = %#v, want %#v", actions, wantActions)
	}
	if !reflect.DeepEqual(next, wantState) {
		t.Fatalf("emergency completion state = %#v, want %#v", next, wantState)
	}
	if !reflect.DeepEqual(pending, input) {
		t.Fatalf("emergency completion mutated input: before=%#v after=%#v", input, pending)
	}
	if !reflect.DeepEqual(completion.acknowledged, emergencySettlementGenerations(input)) {
		t.Fatalf("completion acknowledgement order = %#v", completion.acknowledged)
	}
	if !reflect.DeepEqual(completion.residuals, emergencySettlementResidualResolutions(input)) {
		t.Fatalf("completion residual order = %#v", completion.residuals)
	}
	assertEmergencySettlementSentinel(t, next)
	assertEmergencySettlementInvariantByteStable(t, next, event)
}

func TestSupervisorReducerEmergencySettlementCanonicalGenerationOrder(t *testing.T) {
	const (
		lowGeneration  attemptGeneration = 801
		highGeneration attemptGeneration = 901
	)
	state := supervisorState{}
	state, highTransfer := appendEmergencyLatePipeline(t, state, highGeneration, false)
	state = completeEmergencyRuntimeReceipt(
		t, state, highGeneration, highTransfer, supervisorRuntimeClosurePending,
	)
	state, lowTransfer := appendEmergencyLatePipeline(t, state, lowGeneration, false)
	state = completeEmergencyRuntimeReceipt(
		t, state, lowGeneration, lowTransfer, supervisorRuntimeClosurePending,
	)

	wantReady := cloneSupervisorState(state)
	wantReady.attempts = []supervisorAttemptState{
		supervisorAttemptByGeneration(t, state, lowGeneration),
		supervisorAttemptByGeneration(t, state, highGeneration),
	}
	if !reflect.DeepEqual(state, wantReady) {
		t.Fatalf("reverse registration ledger = %#v, want canonical %#v", state, wantReady)
	}

	emergencyAt := emergencySettlementCut(state)
	emergencyEvent := supervisorEvent{
		kind:    supervisorEmergencyStarted,
		at:      emergencyAt,
		drainBy: emergencyAt.Add(10 * time.Second),
		emergencySnapshots: []supervisorEmergencySnapshot{
			{generation: lowGeneration},
			{generation: highGeneration},
		},
	}
	emergencyInput := cloneSupervisorState(state)
	pending, actions := reduceSupervisorMustAccept(t, state, emergencyEvent)
	wantResolutions := []supervisorEmergencyResolution{
		{generation: lowGeneration, kind: supervisorEmergencyResidualOwned},
		{generation: highGeneration, kind: supervisorEmergencyResidualOwned},
	}
	wantPending := cloneSupervisorState(emergencyInput)
	wantPending.nextAction++
	wantPending.emergency = supervisorEmergencyEpoch{
		active: true, at: emergencyAt, drainBy: emergencyEvent.drainBy,
		pendingAction: supervisorPendingAction{
			kind: supervisorSettleEmergency, token: wantPending.nextAction,
		},
	}
	for index := range wantPending.attempts {
		wantPending.attempts[index].lastEventAt = emergencyAt
	}
	wantSettlementActions := []supervisorAction{{
		kind: supervisorSettleEmergency, token: wantPending.nextAction,
		resolutions: wantResolutions,
	}}
	if !reflect.DeepEqual(pending, wantPending) || !reflect.DeepEqual(actions, wantSettlementActions) {
		t.Fatalf("canonical settlement = %#v actions=%#v, want %#v/%#v",
			pending, actions, wantPending, wantSettlementActions)
	}
	if !reflect.DeepEqual(state, emergencyInput) {
		t.Fatalf("canonical emergency mutated input: before=%#v after=%#v", emergencyInput, state)
	}

	completion := supervisorEmergencySettlementCompletion{
		action:       pending.emergency.pendingAction,
		acknowledged: []attemptGeneration{lowGeneration, highGeneration},
		residuals:    wantResolutions,
	}
	completionEvent := supervisorEvent{
		kind:                supervisorEmergencySettlementCompleted,
		emergencySettlement: &completion,
	}
	completionInput := cloneSupervisorState(pending)
	completed, delivery := reduceSupervisorMustAccept(t, pending, completionEvent)
	wantCompleted := cloneSupervisorState(completionInput)
	wantCompleted.nextAction++
	wantCompleted.attempts = nil
	wantCompleted.emergency.pendingAction = supervisorPendingAction{}
	wantDelivery := []supervisorAction{{
		kind: supervisorDeliverEmergencySettlement, token: wantCompleted.nextAction,
		residuals: []supervisorEmergencyResidual{
			{generation: lowGeneration, attempt: "emergency-late", kind: supervisorResidualOwned},
			{generation: highGeneration, attempt: "emergency-late", kind: supervisorResidualOwned},
		},
	}}
	if !reflect.DeepEqual(completed, wantCompleted) || !reflect.DeepEqual(delivery, wantDelivery) {
		t.Fatalf("canonical completion = %#v delivery=%#v, want %#v/%#v",
			completed, delivery, wantCompleted, wantDelivery)
	}
	if !reflect.DeepEqual(pending, completionInput) {
		t.Fatalf("canonical completion mutated input: before=%#v after=%#v", completionInput, pending)
	}
}

func TestSupervisorReducerEmergencySettlementEmptyCompletionOccursOnce(t *testing.T) {
	pending, _ := beginEmergencySettlement(t, supervisorState{}, true)
	completion, event := emergencySettlementCompletionEvent(pending)
	input := cloneSupervisorState(pending)
	next, actions := reduceSupervisorMustAccept(t, pending, event)
	want := cloneSupervisorState(input)
	want.nextAction++
	want.emergency.pendingAction = supervisorPendingAction{}
	wantActions := []supervisorAction{{
		kind: supervisorDeliverEmergencySettlement, token: want.nextAction,
	}}
	if !reflect.DeepEqual(actions, wantActions) || !reflect.DeepEqual(next, want) ||
		len(completion.acknowledged) != 0 || len(completion.residuals) != 0 {
		t.Fatalf("empty emergency completion = %#v actions=%#v, want %#v", next, actions, want)
	}
	if !reflect.DeepEqual(pending, input) {
		t.Fatalf("empty emergency completion mutated input: before=%#v after=%#v", input, pending)
	}
	assertEmergencySettlementInvariantByteStable(t, next, event)
}

func TestSupervisorReducerEmergencySettlementStartsAfterLateProvenNoRelease(t *testing.T) {
	launchBy := time.Unix(80_000, 0)
	state, launch := appendReducerLaunchWithFacts(
		t, supervisorState{}, emergencyLateNoReleaseGeneration,
		"emergency-late-no-release", AutomaticProfile, 20*time.Second, launchBy,
	)
	state, boundaryActions := reduceSupervisorMustAccept(t, state, supervisorEvent{
		kind: supervisorLaunchBoundary, generation: emergencyLateNoReleaseGeneration,
		at: launchBy,
	})
	assertSupervisorActions(
		t, boundaryActions, supervisorRevokeLaunchRelease, supervisorPublishLaunchUnconfirmed,
	)
	active, _ := beginEmergencySettlement(t, state, false)
	completion := supervisorLaunchCompletion{
		generation: emergencyLateNoReleaseGeneration,
		action:     launch.token,
		at:         active.emergency.at.Add(time.Nanosecond),
		kind:       supervisorLaunchProvenNotReleased,
		failure:    LaunchFailed,
	}
	input := cloneSupervisorState(active)
	next, actions := reduceSupervisorMustAccept(t, active, supervisorEvent{
		kind: supervisorLaunchCompleted, generation: emergencyLateNoReleaseGeneration,
		at: completion.at, completion: &completion,
	})
	wantToken := input.nextAction + 1
	wantState := cloneSupervisorState(input)
	wantState.nextAction = wantToken + 1
	wantState.emergency.pendingAction = supervisorPendingAction{
		kind: supervisorSettleEmergency, token: wantToken + 1,
	}
	wantAttempt := &wantState.attempts[runtimeReceiptAttemptIndex(
		t, wantState, emergencyLateNoReleaseGeneration,
	)]
	wantAttempt.phase = supervisorLaunchClosedNotReleased
	wantAttempt.lastEventAt = completion.at
	wantActions := []supervisorAction{
		{
			kind: supervisorCloseProspective, generation: emergencyLateNoReleaseGeneration,
			token: wantToken, at: input.emergency.at, drainBy: input.emergency.drainBy,
			launchKind: supervisorLaunchProvenNotReleased, launchFailure: LaunchFailed,
			launchDuration: completion.at.Sub(wantAttempt.registeredAt),
		},
		{kind: supervisorSettleEmergency, token: wantToken + 1},
	}
	if !reflect.DeepEqual(actions, wantActions) {
		t.Fatalf("late no-release settlement actions = %#v, want %#v", actions, wantActions)
	}
	if !reflect.DeepEqual(next, wantState) {
		t.Fatalf("late no-release settlement state = %#v, want %#v", next, wantState)
	}
	if !reflect.DeepEqual(active, input) {
		t.Fatalf("late no-release settlement mutated input: before=%#v after=%#v", input, active)
	}
}

func TestSupervisorReducerEmergencySettlementStartsAfterMixedLateProvenNoRelease(t *testing.T) {
	ledger := newEmergencySettlementLedger(t, "")
	launchBy := emergencySettlementCut(ledger.state).Add(time.Second)
	state, launch := appendReducerLaunchWithFacts(
		t, ledger.state, emergencyMixedLateNoReleaseGeneration,
		"emergency-mixed-late-no-release", AutomaticProfile, 20*time.Second, launchBy,
	)
	state, boundaryActions := reduceSupervisorMustAccept(t, state, supervisorEvent{
		kind: supervisorLaunchBoundary, generation: emergencyMixedLateNoReleaseGeneration,
		at: launchBy,
	})
	assertSupervisorActions(
		t, boundaryActions, supervisorRevokeLaunchRelease, supervisorPublishLaunchUnconfirmed,
	)
	active, _ := beginEmergencySettlement(t, state, false)
	if active.emergency.pendingAction != (supervisorPendingAction{}) {
		t.Fatalf("unresolved mixed prospective settled early: %#v", active.emergency)
	}
	completion := supervisorLaunchCompletion{
		generation: emergencyMixedLateNoReleaseGeneration,
		action:     launch.token,
		at:         active.emergency.at.Add(time.Nanosecond),
		kind:       supervisorLaunchProvenNotReleased,
		failure:    LaunchFailed,
	}
	input := cloneSupervisorState(active)
	next, actions := reduceSupervisorMustAccept(t, active, supervisorEvent{
		kind: supervisorLaunchCompleted, generation: emergencyMixedLateNoReleaseGeneration,
		at: completion.at, completion: &completion,
	})
	wantCloseToken := input.nextAction + 1
	wantSettlementToken := wantCloseToken + 1
	wantActions := []supervisorAction{
		{
			kind: supervisorCloseProspective, generation: emergencyMixedLateNoReleaseGeneration,
			token: wantCloseToken, at: input.emergency.at, drainBy: input.emergency.drainBy,
			launchKind: supervisorLaunchProvenNotReleased, launchFailure: LaunchFailed,
			launchDuration: completion.at.Sub(
				supervisorAttemptByGeneration(t, input, emergencyMixedLateNoReleaseGeneration).registeredAt,
			),
		},
		{
			kind: supervisorSettleEmergency, token: wantSettlementToken,
			resolutions: mixedEmergencySettlementResolutions(),
		},
	}
	want := cloneSupervisorState(input)
	want.nextAction = wantSettlementToken
	want.emergency.pendingAction = supervisorPendingAction{
		kind: supervisorSettleEmergency, token: wantSettlementToken,
	}
	target := &want.attempts[runtimeReceiptAttemptIndex(
		t, want, emergencyMixedLateNoReleaseGeneration,
	)]
	target.phase = supervisorLaunchClosedNotReleased
	target.lastEventAt = completion.at
	if !reflect.DeepEqual(actions, wantActions) || !reflect.DeepEqual(next, want) {
		t.Fatalf("mixed late no-release settlement = %#v actions=%#v, want %#v/%#v", next, actions, want, wantActions)
	}
	if !reflect.DeepEqual(active, input) {
		t.Fatalf("mixed late no-release mutated input: before=%#v after=%#v", input, active)
	}
}

func TestSupervisorReducerEmergencySettlementPayloadRejectsOtherEventByteStable(t *testing.T) {
	launchBy := time.Unix(90_000, 0)
	state, launch := appendReducerLaunchWithFacts(
		t, supervisorState{}, 999, "emergency-payload-union",
		AutomaticProfile, 20*time.Second, launchBy,
	)
	completion := supervisorLaunchCompletion{
		generation: 999, action: launch.token, at: launchBy.Add(-time.Nanosecond),
		kind: supervisorLaunchProvenNotReleased, failure: LaunchFailed,
	}
	assertEmergencySettlementInvariantByteStable(t, state, supervisorEvent{
		kind: supervisorLaunchCompleted, generation: 999, at: completion.at,
		completion:          &completion,
		emergencySettlement: &supervisorEmergencySettlementCompletion{},
	})
}

func TestSupervisorReducerEmergencySettlementRejectsMalformedCompletionByteStable(t *testing.T) {
	for _, test := range emergencySettlementMalformedSpecs() {
		t.Run(test.name, func(t *testing.T) {
			ledger := newEmergencySettlementLedger(t, "")
			pending, _ := beginEmergencySettlement(t, ledger.state, true)
			completion, event := emergencySettlementCompletionEvent(pending)
			test.mutate(&event, &completion)
			if event.emergencySettlement != nil {
				event.emergencySettlement = &completion
			}
			assertEmergencySettlementInvariantByteStable(t, pending, event)
		})
	}
}

func TestSupervisorReducerEmergencySettlementRejectsEveryOtherEventWhilePending(t *testing.T) {
	ledger := newEmergencySettlementLedger(t, "")
	pending, _ := beginEmergencySettlement(t, ledger.state, true)
	for kind := supervisorProspectiveRegistered; kind < supervisorEmergencySettlementCompleted; kind++ {
		t.Run(emergencySettlementEventName(kind), func(t *testing.T) {
			assertEmergencySettlementInvariantByteStable(t, pending, supervisorEvent{kind: kind})
		})
	}
}

func TestSupervisorReducerEmergencySettlementRejectsRegistrationAfterCompletion(t *testing.T) {
	pending, _ := beginEmergencySettlement(t, supervisorState{}, true)
	_, event := emergencySettlementCompletionEvent(pending)
	settled, _ := reduceSupervisorMustAccept(t, pending, event)
	registeredAt := time.Unix(50_000, 0)
	assertEmergencySettlementInvariantByteStable(t, settled, supervisorEvent{
		kind: supervisorProspectiveRegistered, generation: 999,
		attempt: "post-emergency", at: registeredAt, launchBy: registeredAt.Add(time.Second),
		profile: AutomaticProfile, commandDeadline: time.Second,
	})
}

func TestSupervisorReducerEmergencySettlementDataRemainsCapabilityFree(t *testing.T) {
	for _, dataType := range []reflect.Type{
		reflect.TypeOf(supervisorEmergencyResolution{}),
		reflect.TypeOf(supervisorEmergencyResidual{}),
		reflect.TypeOf(supervisorEmergencySettlementCompletion{}),
		reflect.TypeOf(supervisorEmergencyEpoch{}),
		reflect.TypeOf(supervisorState{}),
		reflect.TypeOf(supervisorEvent{}),
		reflect.TypeOf(supervisorAction{}),
	} {
		t.Run(dataType.Name(), func(t *testing.T) {
			assertReducerDataOnly(t, dataType, make(map[reflect.Type]bool), dataType.Name())
		})
	}
}

func newEmergencySettlementLedger(t *testing.T, hold string) emergencySettlementLedger {
	t.Helper()
	var ledger emergencySettlementLedger
	ledger.state, ledger.settle, ledger.deferredTerminal = appendEmergencyDeferredCaller(
		t, ledger.state, emergencyDeferredGeneration,
	)
	if hold != "settle" {
		ledger.state = completeEmergencyRuntimeReceipt(
			t, ledger.state, emergencyDeferredGeneration, ledger.settle,
			supervisorRuntimeClosurePending,
		)
	}
	ledger.state = appendEmergencyClosedSentinel(t, ledger.state, emergencySentinelGeneration)
	ledger.state, ledger.transfer, ledger.callerResidualTerminal = appendEmergencyCallerResidual(
		t, ledger.state, emergencyCallerResidualGeneration,
	)
	if hold != "transfer" {
		ledger.state = completeEmergencyRuntimeReceipt(
			t, ledger.state, emergencyCallerResidualGeneration, ledger.transfer,
			supervisorRuntimeClosurePending,
		)
	}
	ledger.state, ledger.lateRelease = appendEmergencyLatePipeline(
		t, ledger.state, emergencyLateDrainedGeneration, true,
	)
	if hold != "late release" {
		ledger.state = completeEmergencyLateRelease(
			t, ledger.state, emergencyLateDrainedGeneration, ledger.lateRelease,
		)
	}
	ledger.state, ledger.lateTransfer = appendEmergencyLatePipeline(
		t, ledger.state, emergencyLateResidualGeneration, false,
	)
	if hold != "late transfer" {
		ledger.state = completeEmergencyRuntimeReceipt(
			t, ledger.state, emergencyLateResidualGeneration, ledger.lateTransfer,
			supervisorRuntimeClosurePending,
		)
	}

	return ledger
}

func appendEmergencyRunning(
	t *testing.T,
	state supervisorState,
	generation attemptGeneration,
) runningReducerFixture {
	t.Helper()
	launchBy := time.Unix(10_000+int64(generation)*100, 0)
	state, launch := appendReducerLaunchWithFacts(
		t, state, generation, attemptIdentity("emergency-attempt"),
		AutomaticProfile, 20*time.Second, launchBy,
	)
	completion := supervisorLaunchCompletion{
		generation: generation, action: launch.token,
		at: launchBy.Add(-time.Nanosecond), kind: supervisorLaunchReleased,
	}
	state, actions := reduceSupervisorMustAccept(t, state, supervisorEvent{
		kind: supervisorLaunchCompleted, generation: generation,
		at: completion.at, completion: &completion,
	})
	assertSupervisorActions(t, actions, supervisorPublishOwned, supervisorWaitRoot, supervisorSampleRunning)

	return runningReducerFixture{
		state: state, generation: generation, startedAt: completion.at,
		deadlineAt: completion.at.Add(20 * time.Second),
		drainBy:    completion.at.Add(30 * time.Second),
		waitAction: actions[1].token, sampleAction: actions[2].token,
	}
}

func appendEmergencyDeferredCaller(
	t *testing.T,
	state supervisorState,
	generation attemptGeneration,
) (supervisorState, supervisorAction, supervisorTerminalEvidence) {
	t.Helper()
	running := appendEmergencyRunning(t, state, generation)
	intentAt := running.startedAt.Add(time.Second)
	fact := supervisorRunningFact{
		generation: generation, action: running.waitAction,
		kind: supervisorRunningRootExited, at: intentAt, exitCode: 31,
	}
	state, actions := running.reduceBundle(t, intentAt, []supervisorRunningFact{fact}, supervisorExitRecheck{})
	assertSupervisorActions(t, actions, supervisorObserveEmptiness)
	drain := drainReducerFixture{generation: generation}
	state, actions = drain.complete(
		t, state, actions[0], supervisorDrainObservedEmpty, intentAt.Add(time.Nanosecond), 0,
	)
	assertSupervisorActions(t, actions, supervisorCaptureOutput)
	state, release := completeEmergencyOutputPipeline(t, state, generation, actions[0], true)
	at := release.at.Add(time.Nanosecond)
	completion := supervisorReleaseCompletion{
		generation: generation,
		action:     supervisorPendingAction{kind: release.kind, token: release.token},
		at:         at,
	}
	state, actions = reduceSupervisorMustAccept(t, state, supervisorEvent{
		kind: supervisorReleaseCompleted, generation: generation, at: at, release: &completion,
	})
	assertSupervisorActions(t, actions, supervisorSettleRuntime)
	terminal := supervisorAttemptByGeneration(t, state, generation).terminal

	return state, actions[0], terminal
}

func appendEmergencyCallerResidual(
	t *testing.T,
	state supervisorState,
	generation attemptGeneration,
) (supervisorState, supervisorAction, supervisorTerminalEvidence) {
	t.Helper()
	running := appendEmergencyRunning(t, state, generation)
	intentAt := running.startedAt.Add(time.Second)
	fact := supervisorRunningFact{
		generation: generation, action: running.waitAction,
		kind: supervisorRunningRootExited, at: intentAt, exitCode: 41, exitSignal: 9,
	}
	state, actions := running.reduceBundle(t, intentAt, []supervisorRunningFact{fact}, supervisorExitRecheck{})
	assertSupervisorActions(t, actions, supervisorObserveEmptiness)
	drain := drainReducerFixture{generation: generation}
	state, actions = drain.complete(
		t, state, actions[0], supervisorDrainObservedEmpty, running.drainBy, 0,
	)
	assertSupervisorActions(t, actions, supervisorCaptureOutput)
	state, transfer := completeEmergencyOutputPipeline(t, state, generation, actions[0], false)
	wantTerminal := supervisorTerminalEvidence{
		kind:            supervisorTerminalDrainUnconfirmed,
		commandDeadline: 20 * time.Second,
		launchDuration:  time.Second - time.Nanosecond,
		commandDuration: time.Second,
		firedBound:      supervisorNoCommandBound,
		output: supervisorOutputEvidence{
			ref: supervisorOutputRef(generation), cutoff: 4, prefixLength: 4,
			completeThroughCutoff: true,
		},
	}

	return state, transfer, wantTerminal
}

func appendEmergencyClosedSentinel(
	t *testing.T,
	state supervisorState,
	generation attemptGeneration,
) supervisorState {
	t.Helper()
	launchBy := time.Unix(10_000+int64(generation)*100, 0)
	state, launch := appendReducerLaunchWithFacts(
		t, state, generation, "emergency-sentinel", AutomaticProfile, 20*time.Second, launchBy,
	)
	completion := supervisorLaunchCompletion{
		generation: generation, action: launch.token,
		at:   launchBy.Add(-time.Nanosecond),
		kind: supervisorLaunchProvenNotReleased, failure: LaunchFailed,
	}
	state, actions := reduceSupervisorMustAccept(t, state, supervisorEvent{
		kind: supervisorLaunchCompleted, generation: generation,
		at: completion.at, completion: &completion,
	})
	assertSupervisorActions(t, actions, supervisorPublishNotReleased)

	return state
}

func appendEmergencyLatePipeline(
	t *testing.T,
	state supervisorState,
	generation attemptGeneration,
	provenEmpty bool,
) (supervisorState, supervisorAction) {
	t.Helper()
	launchBy := time.Unix(10_000+int64(generation)*100, 0)
	state, launch := appendReducerLaunchWithFacts(
		t, state, generation, attemptIdentity("emergency-late"),
		AutomaticProfile, 20*time.Second, launchBy,
	)
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
		kind: supervisorLaunchCompleted, generation: generation,
		at: releasedAt, drainBy: drainBy, completion: &completion,
	})
	assertSupervisorActions(t, actions, supervisorAdoptOwned, supervisorForceOwned)
	drain := drainReducerFixture{generation: generation}
	forceAt := drainBy
	if provenEmpty {
		forceAt = releasedAt.Add(time.Nanosecond)
	}
	state, actions = drain.complete(
		t, state, actions[1], supervisorDrainForceCompleted, forceAt, 0,
	)
	if provenEmpty {
		assertSupervisorActions(t, actions, supervisorObserveEmptiness)
		state, actions = drain.complete(
			t, state, actions[0], supervisorDrainObservedEmpty, forceAt.Add(time.Nanosecond), 0,
		)
	}
	assertSupervisorActions(t, actions, supervisorCaptureOutput)

	return completeEmergencyOutputPipeline(t, state, generation, actions[0], provenEmpty)
}

func completeEmergencyOutputPipeline(
	t *testing.T,
	state supervisorState,
	generation attemptGeneration,
	capture supervisorAction,
	provenEmpty bool,
) (supervisorState, supervisorAction) {
	t.Helper()
	state, actions := completeReducerOutput(
		t, state, generation, capture, capture.at.Add(time.Nanosecond),
		supervisorOutputRef(generation), 4, 4, 0,
	)
	assertSupervisorActions(t, actions, supervisorSealStopAdmission)
	state, actions = completeReducerStopSeal(
		t, state, generation, actions[0], actions[0].at.Add(time.Nanosecond),
	)
	want := supervisorTransferResidualCustody
	if provenEmpty {
		want = supervisorReleaseDomain
	}
	assertSupervisorActions(t, actions, want)

	return state, actions[0]
}

func completeEmergencyRuntimeReceipt(
	t *testing.T,
	state supervisorState,
	generation attemptGeneration,
	pending supervisorAction,
	kind supervisorRuntimeReceiptKind,
) supervisorState {
	t.Helper()
	callerOwned := supervisorAttemptByGeneration(t, state, generation).intent.latched
	completion := supervisorRuntimeCompletion{
		generation: generation,
		action:     supervisorPendingAction{kind: pending.kind, token: pending.token},
		kind:       kind,
	}
	state, actions := reduceSupervisorMustAccept(t, state, supervisorEvent{
		kind: supervisorRuntimeCompleted, generation: generation, runtime: &completion,
	})
	if pending.kind == supervisorTransferResidualCustody && callerOwned {
		assertSupervisorActions(t, actions, supervisorDeliverTerminal)
	} else if len(actions) != 0 {
		t.Fatalf("deferred runtime receipt emitted action: %#v", actions)
	}

	return state
}

func completeEmergencyLateRelease(
	t *testing.T,
	state supervisorState,
	generation attemptGeneration,
	release supervisorAction,
) supervisorState {
	t.Helper()
	at := release.at.Add(time.Nanosecond)
	completion := supervisorReleaseCompletion{
		generation: generation,
		action:     supervisorPendingAction{kind: release.kind, token: release.token},
		at:         at,
	}
	state, actions := reduceSupervisorMustAccept(t, state, supervisorEvent{
		kind: supervisorReleaseCompleted, generation: generation, at: at, release: &completion,
	})
	if len(actions) != 0 {
		t.Fatalf("late release emitted ordinary terminal action: %#v", actions)
	}

	return state
}

func beginEmergencySettlement(
	t *testing.T,
	state supervisorState,
	wantSettlement bool,
) (supervisorState, supervisorEvent) {
	t.Helper()
	at := emergencySettlementCut(state)
	event := supervisorEvent{
		kind: supervisorEmergencyStarted, at: at, drainBy: at.Add(10 * time.Second),
		emergencySnapshots: emergencySettlementSnapshots(state),
	}
	input := cloneSupervisorState(state)
	next, actions := reduceSupervisorMustAccept(t, state, event)
	want := cloneSupervisorState(input)
	want.emergency = supervisorEmergencyEpoch{active: true, at: event.at, drainBy: event.drainBy}
	for index := range want.attempts {
		if want.attempts[index].phase != supervisorLaunchClosedNotReleased {
			want.attempts[index].lastEventAt = event.at
		}
	}
	if wantSettlement {
		want.nextAction++
		want.emergency.pendingAction = supervisorPendingAction{
			kind: supervisorSettleEmergency, token: want.nextAction,
		}
		wantActions := []supervisorAction{{
			kind: supervisorSettleEmergency, token: want.nextAction,
			resolutions: emergencySettlementResolutions(input),
		}}
		if !reflect.DeepEqual(actions, wantActions) {
			t.Fatalf("emergency settlement actions = %#v, want %#v", actions, wantActions)
		}
	} else if len(actions) != 0 {
		t.Fatalf("not-ready emergency emitted settlement: %#v", actions)
	}
	if !reflect.DeepEqual(next, want) {
		t.Fatalf("emergency start state = %#v, want %#v", next, want)
	}
	if !reflect.DeepEqual(state, input) {
		t.Fatalf("emergency start mutated input: before=%#v after=%#v", input, state)
	}

	return next, event
}

func emergencySettlementCut(state supervisorState) time.Time {
	at := time.Unix(60_000, 0)
	for _, attempt := range state.attempts {
		if !attempt.lastEventAt.Before(at) {
			at = attempt.lastEventAt.Add(time.Nanosecond)
		}
	}

	return at
}

func emergencySettlementSnapshots(state supervisorState) []supervisorEmergencySnapshot {
	var snapshots []supervisorEmergencySnapshot
	for _, attempt := range state.attempts {
		if attempt.phase != supervisorLaunchClosedNotReleased {
			snapshots = append(snapshots, supervisorEmergencySnapshot{generation: attempt.generation})
		}
	}

	return snapshots
}

func emergencySettlementResolutions(state supervisorState) []supervisorEmergencyResolution {
	var resolutions []supervisorEmergencyResolution
	for _, attempt := range state.attempts {
		if attempt.phase == supervisorLaunchClosedNotReleased {
			continue
		}
		kind := supervisorEmergencyConfirmedDrained
		if attempt.drain.decision == supervisorDrainUnconfirmed {
			kind = supervisorEmergencyResidualOwned
		}
		resolutions = append(resolutions, supervisorEmergencyResolution{
			generation: attempt.generation, kind: kind,
		})
	}

	return resolutions
}

func mixedEmergencySettlementResolutions() []supervisorEmergencyResolution {
	return []supervisorEmergencyResolution{
		{generation: emergencyDeferredGeneration, kind: supervisorEmergencyConfirmedDrained},
		{generation: emergencyCallerResidualGeneration, kind: supervisorEmergencyResidualOwned},
		{generation: emergencyLateDrainedGeneration, kind: supervisorEmergencyConfirmedDrained},
		{generation: emergencyLateResidualGeneration, kind: supervisorEmergencyResidualOwned},
	}
}

func mixedEmergencySettlementDeliveryResiduals() []supervisorEmergencyResidual {
	return []supervisorEmergencyResidual{
		{
			generation: emergencyCallerResidualGeneration,
			attempt:    "emergency-attempt",
			kind:       supervisorResidualOwned,
		},
		{
			generation: emergencyLateResidualGeneration,
			attempt:    "emergency-late",
			kind:       supervisorResidualOwned,
		},
	}
}

func emergencySettlementResidualResolutions(
	state supervisorState,
) []supervisorEmergencyResolution {
	var residuals []supervisorEmergencyResolution
	for _, resolution := range emergencySettlementResolutions(state) {
		if resolution.kind != supervisorEmergencyConfirmedDrained {
			residuals = append(residuals, resolution)
		}
	}

	return residuals
}

func emergencySettlementGenerations(state supervisorState) []attemptGeneration {
	resolutions := emergencySettlementResolutions(state)
	generations := make([]attemptGeneration, len(resolutions))
	for index := range resolutions {
		generations[index] = resolutions[index].generation
	}

	return generations
}

func emergencySettlementCompletionEvent(
	state supervisorState,
) (supervisorEmergencySettlementCompletion, supervisorEvent) {
	completion := supervisorEmergencySettlementCompletion{
		action:       state.emergency.pendingAction,
		acknowledged: emergencySettlementGenerations(state),
		residuals:    emergencySettlementResidualResolutions(state),
	}
	event := supervisorEvent{
		kind:                supervisorEmergencySettlementCompleted,
		emergencySettlement: &completion,
	}

	return completion, event
}

func assertEmergencySettlementTail(
	t *testing.T,
	input supervisorState,
	next supervisorState,
	actions []supervisorAction,
) {
	t.Helper()
	if len(actions) == 0 || actions[len(actions)-1].kind != supervisorSettleEmergency {
		t.Fatalf("ready transition omitted emergency settlement tail: %#v", actions)
	}
	settle := actions[len(actions)-1]
	if settle.generation != 0 || !settle.at.IsZero() || !settle.drainBy.IsZero() ||
		!reflect.DeepEqual(settle.resolutions, emergencySettlementResolutions(next)) ||
		next.emergency.pendingAction != (supervisorPendingAction{
			kind: supervisorSettleEmergency, token: settle.token,
		}) {
		t.Fatalf("emergency settlement tail = %#v state=%#v", settle, next)
	}
	if !reflect.DeepEqual(input.emergency, supervisorEmergencyEpoch{
		active: true, at: input.emergency.at, drainBy: input.emergency.drainBy,
	}) {
		t.Fatalf("trigger input lacked active emergency: %#v", input.emergency)
	}
}

type emergencySettlementMalformedSpec struct {
	name   string
	mutate func(*supervisorEvent, *supervisorEmergencySettlementCompletion)
}

func emergencySettlementMalformedSpecs() []emergencySettlementMalformedSpec {
	return []emergencySettlementMalformedSpec{
		{name: "nil completion", mutate: func(event *supervisorEvent, _ *supervisorEmergencySettlementCompletion) {
			event.emergencySettlement = nil
		}},
		{name: "wrong action kind", mutate: func(_ *supervisorEvent, completion *supervisorEmergencySettlementCompletion) {
			completion.action.kind = supervisorSettleRuntime
		}},
		{name: "wrong action token", mutate: func(_ *supervisorEvent, completion *supervisorEmergencySettlementCompletion) {
			completion.action.token++
		}},
		{name: "nonzero generation", mutate: func(event *supervisorEvent, _ *supervisorEmergencySettlementCompletion) {
			event.generation = emergencyDeferredGeneration
		}},
		{name: "nonzero instant", mutate: func(event *supervisorEvent, _ *supervisorEmergencySettlementCompletion) {
			event.at = time.Unix(70_000, 0)
		}},
		{name: "unrelated profile", mutate: func(event *supervisorEvent, _ *supervisorEmergencySettlementCompletion) {
			event.profile = AutomaticProfile
		}},
		{
			name: "missing acknowledgement",
			mutate: func(_ *supervisorEvent, completion *supervisorEmergencySettlementCompletion) {
				completion.acknowledged = completion.acknowledged[:len(completion.acknowledged)-1]
			},
		},
		{
			name: "extra acknowledgement",
			mutate: func(_ *supervisorEvent, completion *supervisorEmergencySettlementCompletion) {
				completion.acknowledged = append(completion.acknowledged, 999)
			},
		},
		{
			name: "duplicate acknowledgement",
			mutate: func(_ *supervisorEvent, completion *supervisorEmergencySettlementCompletion) {
				completion.acknowledged[1] = completion.acknowledged[0]
			},
		},
		{
			name: "reordered acknowledgement",
			mutate: func(_ *supervisorEvent, completion *supervisorEmergencySettlementCompletion) {
				completion.acknowledged[0], completion.acknowledged[1] =
					completion.acknowledged[1], completion.acknowledged[0]
			},
		},
		{
			name: "closed sentinel acknowledged",
			mutate: func(_ *supervisorEvent, completion *supervisorEmergencySettlementCompletion) {
				completion.acknowledged[0] = emergencySentinelGeneration
			},
		},
		{
			name: "missing residual",
			mutate: func(_ *supervisorEvent, completion *supervisorEmergencySettlementCompletion) {
				completion.residuals = completion.residuals[:len(completion.residuals)-1]
			},
		},
		{
			name: "extra residual",
			mutate: func(_ *supervisorEvent, completion *supervisorEmergencySettlementCompletion) {
				completion.residuals = append(completion.residuals, supervisorEmergencyResolution{
					generation: emergencyDeferredGeneration,
					kind:       supervisorEmergencyResidualOwned,
				})
			},
		},
		{
			name: "reordered residual",
			mutate: func(_ *supervisorEvent, completion *supervisorEmergencySettlementCompletion) {
				completion.residuals[0], completion.residuals[1] =
					completion.residuals[1], completion.residuals[0]
			},
		},
		{
			name: "wrong residual kind",
			mutate: func(_ *supervisorEvent, completion *supervisorEmergencySettlementCompletion) {
				completion.residuals[0].kind = supervisorEmergencyConfirmedDrained
			},
		},
	}
}

func assertEmergencySettlementInvariantByteStable(
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
		t.Fatalf("rejected emergency settlement event emitted action: %#v", actions)
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatalf("rejected emergency settlement event mutated input: before=%#v after=%#v", before, state)
	}
}

func assertEmergencySettlementSentinel(t *testing.T, state supervisorState) {
	t.Helper()
	sentinel := supervisorAttemptByGeneration(t, state, emergencySentinelGeneration)
	if sentinel.phase != supervisorLaunchClosedNotReleased {
		t.Fatalf("emergency settlement rewrote closed sentinel: %#v", sentinel)
	}
}

func emergencySettlementEventName(kind supervisorEventKind) string {
	return "event-" + string(rune('0'+kind))
}
