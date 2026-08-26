package ooze

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupervisorEmergencyExecutorNormalizesRuntimeCustodyIntoCanonicalGenerationOrder(t *testing.T) {
	action := supervisorAction{
		kind:  supervisorSettleEmergency,
		token: 41,
		resolutions: []supervisorEmergencyResolution{
			{generation: 801, kind: supervisorEmergencyResidualOwned},
			{generation: 851, kind: supervisorEmergencyConfirmedDrained},
			{generation: 901, kind: supervisorEmergencyResidualOwned},
		},
	}
	state := supervisorState{
		nextAction: action.token,
		emergency: supervisorEmergencyEpoch{
			active: true,
			pendingAction: supervisorPendingAction{
				kind: action.kind, token: action.token,
			},
		},
	}
	originalAction := cloneSupervisorAction(action)
	raw := emergencySettlement{
		acknowledged: []attemptGeneration{801, 851, 901},
		residual: []residualCustody{
			{generation: 901, stage: admissionOwned, transferred: true},
			{generation: 801, stage: admissionOwned, transferred: true},
		},
	}
	originalRaw := cloneEmergencySettlement(raw)
	wantSweep := emergencySweep{resolutions: []emergencyResolution{
		{generation: 801, disposition: emergencyCustodyTransferred},
		{generation: 851, disposition: emergencyConfirmedDrained},
		{generation: 901, disposition: emergencyCustodyTransferred},
	}}
	settleCalls := 0
	executor := supervisorEmergencyExecutor{
		settleEmergency: func(got emergencySweep) emergencySettlement {
			settleCalls++
			assert.Equal(t, wantSweep, got, "runtime sweep = %#v, want %#v", got, wantSweep)

			return raw
		},
	}

	event := executor.execute(state, action)
	wantEvent := &supervisorEvent{
		kind: supervisorEmergencySettlementCompleted,
		emergencySettlement: &supervisorEmergencySettlementCompletion{
			action:       state.emergency.pendingAction,
			acknowledged: []attemptGeneration{801, 851, 901},
			residuals: []supervisorEmergencyResolution{
				{generation: 801, kind: supervisorEmergencyResidualOwned},
				{generation: 901, kind: supervisorEmergencyResidualOwned},
			},
		},
	}
	assert.EqualValues(t, 1, settleCalls, "settle calls/event = %d/%#v, want 1/%#v", settleCalls, event, wantEvent)
	assert.Equal(t, wantEvent, event, "settle calls/event = %d/%#v, want 1/%#v", settleCalls, event, wantEvent)
	assert.Equal(t, originalAction, action, "executor mutated input: action=%#v raw=%#v", action, raw)
	assert.Equal(t, originalRaw, raw, "executor mutated input: action=%#v raw=%#v", action, raw)
}

func TestSupervisorEmergencyExecutorDeliversExactCanonicalResidualsOnce(t *testing.T) {
	action := supervisorAction{
		kind:  supervisorDeliverEmergencySettlement,
		token: 42,
		residuals: []supervisorEmergencyResidual{
			{generation: 801, attempt: "attempt-801", kind: supervisorResidualOwned},
			{generation: 901, attempt: "attempt-901", kind: supervisorResidualOwned},
		},
	}
	state := supervisorState{
		nextAction: action.token,
		emergency:  supervisorEmergencyEpoch{active: true},
	}
	original := cloneSupervisorAction(action)
	deliveries := 0
	var delivered []supervisorEmergencyResidual
	executor := supervisorEmergencyExecutor{
		deliverEmergencySettlement: func(residuals []supervisorEmergencyResidual) {
			deliveries++
			delivered = append([]supervisorEmergencyResidual(nil), residuals...)
			residuals[0] = supervisorEmergencyResidual{}
		},
	}

	event := executor.execute(state, action)
	assert.Nil(t, event, "delivery event/count/residuals = %#v/%d/%#v", event, deliveries, delivered)
	assert.EqualValues(t, 1, deliveries, "delivery event/count/residuals = %#v/%d/%#v", event, deliveries, delivered)
	assert.Equal(t, action.residuals, delivered, "delivery event/count/residuals = %#v/%d/%#v", event, deliveries, delivered)
	assert.Equal(t, original, action, "delivery mutated action: %#v, want %#v", action, original)
}

func TestSupervisorEmergencyExecutorRejectsStaleSettlementActionBeforeRuntime(t *testing.T) {
	action := supervisorAction{kind: supervisorSettleEmergency, token: 41}
	state := supervisorState{
		nextAction: 42,
		emergency: supervisorEmergencyEpoch{
			active:        true,
			pendingAction: supervisorPendingAction{kind: action.kind, token: action.token},
		},
	}
	settleCalls := 0
	executor := supervisorEmergencyExecutor{
		settleEmergency: func(emergencySweep) emergencySettlement {
			settleCalls++

			return emergencySettlement{}
		},
	}

	assertInvariantViolation(t, func() { executor.execute(state, action) })
	assert.EqualValues(t, 0, settleCalls, "stale action reached runtime %d times", settleCalls)
}

func TestSupervisorEmergencyExecutorRejectsMismatchedRuntimeSettlementFacts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*emergencySettlement)
	}{
		{
			name: "acknowledgements reordered",
			mutate: func(settled *emergencySettlement) {
				settled.acknowledged = []attemptGeneration{901, 801}
			},
		},
		{
			name: "acknowledgement missing",
			mutate: func(settled *emergencySettlement) {
				settled.acknowledged = []attemptGeneration{801}
			},
		},
		{
			name: "residual generation zero",
			mutate: func(settled *emergencySettlement) {
				settled.residual[0].generation = 0
			},
		},
		{
			name: "residual generation unknown",
			mutate: func(settled *emergencySettlement) {
				settled.residual[0].generation = 902
			},
		},
		{
			name: "confirmed generation returned as residual",
			mutate: func(settled *emergencySettlement) {
				settled.residual[0].generation = 801
			},
		},
		{
			name: "residual not transferred",
			mutate: func(settled *emergencySettlement) {
				settled.residual[0].transferred = false
			},
		},
		{
			name: "residual waiting",
			mutate: func(settled *emergencySettlement) {
				settled.residual[0].stage = admissionWaiting
			},
		},
		{
			name: "residual granted",
			mutate: func(settled *emergencySettlement) {
				settled.residual[0].stage = admissionGranted
			},
		},
		{
			name: "residual prospective",
			mutate: func(settled *emergencySettlement) {
				settled.residual[0].stage = admissionProspective
			},
		},
		{
			name: "residual duplicated",
			mutate: func(settled *emergencySettlement) {
				settled.residual = append(settled.residual, settled.residual[0])
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			action := supervisorAction{
				kind:  supervisorSettleEmergency,
				token: 41,
				resolutions: []supervisorEmergencyResolution{
					{generation: 801, kind: supervisorEmergencyConfirmedDrained},
					{generation: 901, kind: supervisorEmergencyResidualOwned},
				},
			}
			state := supervisorState{
				nextAction: action.token,
				emergency: supervisorEmergencyEpoch{
					active:        true,
					pendingAction: supervisorPendingAction{kind: action.kind, token: action.token},
				},
			}
			settled := emergencySettlement{
				acknowledged: []attemptGeneration{801, 901},
				residual: []residualCustody{{
					generation: 901, stage: admissionOwned, transferred: true,
				}},
			}
			test.mutate(&settled)
			executor := supervisorEmergencyExecutor{
				settleEmergency: func(emergencySweep) emergencySettlement { return settled },
			}

			assertInvariantViolation(t, func() { executor.execute(state, action) })
		})
	}
}

func TestSupervisorEmergencyExecutorRejectsCrossKindPayloadBeforeEffects(t *testing.T) {
	settle := supervisorAction{
		kind:      supervisorSettleEmergency,
		token:     41,
		residuals: []supervisorEmergencyResidual{{generation: 801, attempt: "attempt-801", kind: supervisorResidualOwned}},
	}
	settling := supervisorState{
		nextAction: settle.token,
		emergency: supervisorEmergencyEpoch{
			active:        true,
			pendingAction: supervisorPendingAction{kind: settle.kind, token: settle.token},
		},
	}
	deliver := supervisorAction{
		kind:        supervisorDeliverEmergencySettlement,
		token:       42,
		resolutions: []supervisorEmergencyResolution{{generation: 801, kind: supervisorEmergencyConfirmedDrained}},
	}
	delivering := supervisorState{nextAction: deliver.token, emergency: supervisorEmergencyEpoch{active: true}}
	settleCalls, deliveryCalls := 0, 0
	executor := supervisorEmergencyExecutor{
		settleEmergency: func(emergencySweep) emergencySettlement {
			settleCalls++

			return emergencySettlement{}
		},
		deliverEmergencySettlement: func([]supervisorEmergencyResidual) { deliveryCalls++ },
	}

	assertInvariantViolation(t, func() { executor.execute(settling, settle) })
	assertInvariantViolation(t, func() { executor.execute(delivering, deliver) })
	assert.EqualValues(t, 0, settleCalls, "cross-kind payload reached effects: settle=%d delivery=%d", settleCalls, deliveryCalls)
	assert.EqualValues(t, 0, deliveryCalls, "cross-kind payload reached effects: settle=%d delivery=%d", settleCalls, deliveryCalls)
}

func TestSupervisorEmergencyExecutorSettlesProcessRuntimeOnceAcrossAdmissionOrder(t *testing.T) {
	shell := newProcessRuntimeShell(2)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 67})
	admittedHigh := shell.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "admitted-first", class: sharedAdmission,
	})
	admittedLow := shell.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "admitted-second", class: sharedAdmission,
	})
	low := startOwned(shell, <-admittedLow.delivery)
	high := startOwned(shell, <-admittedHigh.delivery)
	assert.False(t, high.generation <= low.generation, "start generations = high %d low %d", high.generation, low.generation)
	shell.observeAttempt(high.generation, drainUnconfirmed{})
	shell.observeAttempt(low.generation, drainUnconfirmed{})
	action := supervisorAction{
		kind:  supervisorSettleEmergency,
		token: 41,
		resolutions: []supervisorEmergencyResolution{
			{generation: low.generation, kind: supervisorEmergencyResidualOwned},
			{generation: high.generation, kind: supervisorEmergencyResidualOwned},
		},
	}
	state := supervisorState{
		nextAction: action.token,
		emergency: supervisorEmergencyEpoch{
			active:        true,
			pendingAction: supervisorPendingAction{kind: action.kind, token: action.token},
		},
	}
	executor := supervisorEmergencyExecutor{settleEmergency: shell.settleEmergency}

	event := executor.execute(state, action)
	wantResiduals := []supervisorEmergencyResolution{
		{generation: low.generation, kind: supervisorEmergencyResidualOwned},
		{generation: high.generation, kind: supervisorEmergencyResidualOwned},
	}
	require.NotNil(t, event, "normalized runtime completion = %#v", event)
	require.NotNil(t, event.emergencySettlement, "normalized runtime completion = %#v", event)
	assert.Equal(t, wantResiduals, event.emergencySettlement.residuals, "normalized runtime completion = %#v", event)
	runtime := shell.snapshot()
	rawResidual := runtime.residualCustody()
	assert.Equal(t, runtimeClosedUnconfirmed, runtime.lifecycle, "settled runtime/raw admission order = %#v/%#v", runtime, rawResidual)
	require.Len(t, rawResidual, 2, "settled runtime/raw admission order = %#v/%#v", runtime, rawResidual)
	assert.Equal(t, high.generation, rawResidual[0].generation, "settled runtime/raw admission order = %#v/%#v", runtime, rawResidual)
	assert.Equal(t, low.generation, rawResidual[1].generation, "settled runtime/raw admission order = %#v/%#v", runtime, rawResidual)
	assertInvariantViolation(t, func() { executor.execute(state, action) })
}

func TestSupervisorEmergencyExecutorRejectsWrongActionTokensBeforeEffects(t *testing.T) {
	tests := []struct {
		name   string
		state  supervisorState
		action supervisorAction
	}{
		{
			name: "settlement token differs from pending",
			state: supervisorState{
				nextAction: 41,
				emergency: supervisorEmergencyEpoch{
					active:        true,
					pendingAction: supervisorPendingAction{kind: supervisorSettleEmergency, token: 41},
				},
			},
			action: supervisorAction{kind: supervisorSettleEmergency, token: 42},
		},
		{
			name: "delivery token differs from reducer sequence",
			state: supervisorState{
				nextAction: 42,
				emergency:  supervisorEmergencyEpoch{active: true},
			},
			action: supervisorAction{kind: supervisorDeliverEmergencySettlement, token: 41},
		},
		{
			name: "delivery still has global pending custody",
			state: supervisorState{
				nextAction: 42,
				emergency: supervisorEmergencyEpoch{
					active:        true,
					pendingAction: supervisorPendingAction{kind: supervisorSettleEmergency, token: 41},
				},
			},
			action: supervisorAction{kind: supervisorDeliverEmergencySettlement, token: 42},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settleCalls, deliveryCalls := 0, 0
			executor := supervisorEmergencyExecutor{
				settleEmergency: func(emergencySweep) emergencySettlement {
					settleCalls++

					return emergencySettlement{}
				},
				deliverEmergencySettlement: func([]supervisorEmergencyResidual) { deliveryCalls++ },
			}

			assertInvariantViolation(t, func() { executor.execute(test.state, test.action) })
			assert.EqualValues(t, 0, settleCalls, "wrong token reached effects: settle=%d delivery=%d", settleCalls, deliveryCalls)
			assert.EqualValues(t, 0, deliveryCalls, "wrong token reached effects: settle=%d delivery=%d", settleCalls, deliveryCalls)
		})
	}
}

func TestSupervisorEmergencyExecutorPreservesExactEmptyRuntimeSettlement(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	shell.closeRuntime(runtimeFatalCause("empty supervisor epoch"))
	action := supervisorAction{kind: supervisorSettleEmergency, token: 41}
	state := supervisorState{
		nextAction: action.token,
		emergency: supervisorEmergencyEpoch{
			active:        true,
			pendingAction: supervisorPendingAction{kind: action.kind, token: action.token},
		},
	}
	executor := supervisorEmergencyExecutor{settleEmergency: shell.settleEmergency}

	event := executor.execute(state, action)
	want := &supervisorEvent{
		kind: supervisorEmergencySettlementCompleted,
		emergencySettlement: &supervisorEmergencySettlementCompletion{
			action: state.emergency.pendingAction,
		},
	}
	assert.Equal(t, want, event, "empty executor event/runtime = %#v/%#v", event, shell.snapshot())
	assert.Equal(t, runtimeClosedDrained, shell.snapshot().lifecycle, "empty executor event/runtime = %#v/%#v", event, shell.snapshot())
	assertInvariantViolation(t, func() { executor.execute(state, action) })
}

func cloneSupervisorAction(action supervisorAction) supervisorAction {
	action.resolutions = append([]supervisorEmergencyResolution(nil), action.resolutions...)
	action.residuals = append([]supervisorEmergencyResidual(nil), action.residuals...)

	return action
}

func cloneEmergencySettlement(settlement emergencySettlement) emergencySettlement {
	settlement.acknowledged = append([]attemptGeneration(nil), settlement.acknowledged...)
	settlement.residual = append([]residualCustody(nil), settlement.residual...)

	return settlement
}
