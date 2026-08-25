//nolint:exhaustruct,lll // Pure reducer traces deliberately omit shell-only fields.
package ooze

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const confirmationAttempt = "confirm-a"

func settledConfirmation(grant admissionGrant) attemptSettled {
	return attemptSettled{profile: grant.profile, deadline: grant.deadline}
}

func automaticDeadlineTrip() attemptTripped {
	return attemptTripped{kind: deadlineTrip, profile: AutomaticProfile, deadline: 31 * time.Second}
}

//nolint:cyclop // This is one ordered trace; splitting its checks would hide the event sequence.
func TestProcessRuntimeOverlapDeadlineAtomicallyClosesGateAndInstallsBarrier(t *testing.T) {
	runtime := newProcessRuntime(3)
	runtime, campaignA := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, campaignB := runtime.registerCampaign(campaignProvenance{lineage: 22})
	runtime, campaignC := runtime.registerCampaign(campaignProvenance{lineage: 33})
	runtime, admittedA1 := runtime.requestAdmission(admissionRequest{
		campaign: campaignA.token, attempt: "a1", class: sharedAdmission,
	})
	runtime, admittedA2 := runtime.requestAdmission(admissionRequest{
		campaign: campaignA.token, attempt: "a2", class: sharedAdmission,
	})
	runtime, admittedB1 := runtime.requestAdmission(admissionRequest{
		campaign: campaignB.token, attempt: "b1", class: sharedAdmission,
	})

	runtime, startedA := runtime.startCommitted(admittedA1.deliveries[0])
	runtime, startedB := runtime.startCommitted(admittedB1.deliveries[0])
	runtime, _ = runtime.observeAttempt(startedA.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(startedB.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(startedA.generation, automaticDeadlineTrip())

	campaignAt := runtime.campaignIndex(campaignA.token)
	assert.False(t, campaignAt < 0, "campaign gate/deadline state=%#v", runtime)
	assert.False(t, runtime.campaigns[campaignAt].primaryGateOpen, "campaign gate/deadline state=%#v", runtime)
	barrierAt := runtime.unboundBarrierIndex(campaignA.token)
	assert.False(t, barrierAt < 0, "overlap deadline did not install a barrier: %#v", runtime)

	runtime, rejectedStart := runtime.startCommitted(admittedA2.deliveries[0])
	returnedAt := runtime.admissionIndex(admittedA2.request)
	assert.Equal(t, startCommittedRejectedGrant, rejectedStart.decision, "post-closure start result/state=%#v/%#v", rejectedStart, runtime)
	assert.False(t, returnedAt < 0, "post-closure start result/state=%#v/%#v", rejectedStart, runtime)
	assert.Equal(t, dispositionReturnedAfterGate, runtime.admissions[returnedAt].disposition, "post-closure start result/state=%#v/%#v", rejectedStart, runtime)
	runtime, returned := runtime.acknowledgeGrantReturn(admittedA2.deliveries[0])
	assert.Equal(t, admissionReturnedAfterGateClosure, returned.decision, "late gate-closed grant return=%#v", returned)
	runtime, gateRejected := runtime.requestAdmission(admissionRequest{
		campaign: campaignA.token,
		attempt:  "a3",
		class:    sharedAdmission,
	})
	assert.Equal(t, admissionRejectedGateClosed, gateRejected.decision, "post-closure shared admission=%#v", gateRejected)
	runtime, later := runtime.requestAdmission(admissionRequest{
		campaign: campaignC.token,
		attempt:  "c1",
		class:    sharedAdmission,
	})
	assert.EqualValues(t, 0, len(later.deliveries), "later shared request passed unbound barrier: %#v", later)

	runtime, bound := runtime.sealAndBindConfirmationBarrier(barrierBinding{
		campaign: campaignA.token,
		attempt:  confirmationAttempt,
		profile:  AutomaticProfile, deadline: 31 * time.Second,
	})
	assert.Equal(t, barrierBound, bound.decision, "barrier bind=%#v", bound)
	assert.EqualValues(t, 0, len(bound.deliveries), "barrier bind=%#v", bound)
	runtime, settledB := runtime.observeAttempt(startedB.generation, attemptSettled{})
	require.Len(t, settledB.deliveries, 1, "barrier did not grant at global zero: %#v", settledB)
	assert.Equal(t, confirmationBarrierAdmission, settledB.deliveries[0].class, "barrier did not grant at global zero: %#v", settledB)
	runtime, confirmation := runtime.startCommitted(settledB.deliveries[0])
	runtime, _ = runtime.observeAttempt(confirmation.generation, launchOwned{})
	runtime, confirmationSettled := runtime.observeAttempt(
		confirmation.generation,
		settledConfirmation(settledB.deliveries[0]),
	)
	assert.Equal(t, []admissionGrant{later.request}, confirmationSettled.deliveries, "post-confirmation FIFO deliveries=%#v", confirmationSettled.deliveries)
	_, completed := runtime.completeConfirmationQueue(campaignA.token)
	assert.EqualValues(t, 0, len(completed.deliveries), "queue completion duplicated FIFO deliveries=%#v", completed.deliveries)
}

func TestProcessRuntimeBarrierCannotBindBeforeCampaignClosureSetSettles(t *testing.T) {
	runtime := newProcessRuntime(3)
	runtime, campaignA := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, campaignB := runtime.registerCampaign(campaignProvenance{lineage: 22})
	runtime, admittedA1 := runtime.requestAdmission(admissionRequest{
		campaign: campaignA.token, attempt: "a1", class: sharedAdmission,
	})
	runtime, admittedA2 := runtime.requestAdmission(admissionRequest{
		campaign: campaignA.token, attempt: "a2", class: sharedAdmission,
	})
	runtime, admittedB1 := runtime.requestAdmission(admissionRequest{
		campaign: campaignB.token, attempt: "b1", class: sharedAdmission,
	})
	runtime, startedA1 := runtime.startCommitted(admittedA1.deliveries[0])
	runtime, startedA2 := runtime.startCommitted(admittedA2.deliveries[0])
	runtime, startedB := runtime.startCommitted(admittedB1.deliveries[0])
	runtime, _ = runtime.observeAttempt(startedA1.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(startedA2.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(startedB.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(startedA1.generation, automaticDeadlineTrip())

	unchanged := runtime
	runtime, premature := runtime.sealAndBindConfirmationBarrier(barrierBinding{
		campaign: campaignA.token,
		attempt:  confirmationAttempt,
		profile:  AutomaticProfile, deadline: 31 * time.Second,
	})
	assert.Equal(t, barrierRejectedClosureOutstanding, premature.decision, "premature bind result/state=%#v/%#v", premature, runtime)
	assert.Equal(t, unchanged, runtime, "premature bind result/state=%#v/%#v", premature, runtime)
	runtime, _ = runtime.observeAttempt(startedA2.generation, attemptSettled{})
	_, bound := runtime.sealAndBindConfirmationBarrier(barrierBinding{
		campaign: campaignA.token,
		attempt:  confirmationAttempt,
		profile:  AutomaticProfile, deadline: 31 * time.Second,
	})
	assert.Equal(t, barrierBound, bound.decision, "barrier was not bound after closure set settled: %#v", bound)
}

func TestProcessRuntimeBarrierRejectsConfirmationFactsDifferentFromProvisionalPrimary(t *testing.T) {
	runtime := newProcessRuntime(2)
	runtime, campaignA := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, campaignB := runtime.registerCampaign(campaignProvenance{lineage: 22})
	runtime, admittedA := runtime.requestAdmission(admissionRequest{
		campaign: campaignA.token, attempt: "a", class: sharedAdmission,
	})
	runtime, admittedB := runtime.requestAdmission(admissionRequest{
		campaign: campaignB.token, attempt: "b", class: sharedAdmission,
	})
	runtime, startedA := runtime.startCommitted(admittedA.deliveries[0])
	runtime, startedB := runtime.startCommitted(admittedB.deliveries[0])
	runtime, _ = runtime.observeAttempt(startedA.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(startedB.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(startedA.generation, attemptTripped{
		kind: deadlineTrip, profile: AutomaticProfile, deadline: 31 * time.Second,
	})
	runtime, _ = runtime.observeAttempt(startedB.generation, attemptSettled{})
	unchanged := runtime
	runtime, result := runtime.sealAndBindConfirmationBarrier(barrierBinding{
		campaign: campaignA.token, attempt: confirmationAttempt,
		profile: AutomaticProfile, deadline: 30 * time.Second,
	})
	assert.Equal(t, barrierRejectedExecutionMismatch, result.decision, "mismatched confirmation facts result/state=%#v/%#v", result, runtime)
	assert.Equal(t, unchanged, runtime, "mismatched confirmation facts result/state=%#v/%#v", result, runtime)
}

func TestProcessRuntimeHardPressureTransitionsOnceWithoutRevocation(t *testing.T) {
	runtime := newProcessRuntime(3)
	runtime, campaign := runtime.registerCampaign(campaignProvenance{lineage: 11})
	starts := make([]startCommittedResult, 0, 3)
	for _, attempt := range []attemptIdentity{"a1", "a2", "a3"} {
		var admitted admissionResult
		runtime, admitted = runtime.requestAdmission(admissionRequest{
			campaign: campaign.token,
			attempt:  attempt,
			class:    sharedAdmission,
		})
		var started startCommittedResult
		runtime, started = runtime.startCommitted(admitted.deliveries[0])
		starts = append(starts, started)
	}
	runtime, _ = runtime.observeAttempt(starts[0].generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(starts[1].generation, launchOwned{})
	runtime, exhausted := runtime.observeAttempt(starts[2].generation, launchNotReleased{
		reason: launchResourceExhausted,
	})
	assert.Equal(t, singleAdmission, runtime.mode, "pressure transition/state=%#v/%#v", exhausted, runtime)
	assert.EqualValues(t, 0, len(exhausted.deliveries), "pressure transition/state=%#v/%#v", exhausted, runtime)
	runtime, queued := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "a4",
		class:    sharedAdmission,
	})
	assert.EqualValues(t, 0, len(queued.deliveries), "single admission granted while overcommitted: %#v", queued)
	runtime, firstSettlement := runtime.observeAttempt(starts[0].generation, attemptSettled{})
	assert.EqualValues(t, 0, len(firstSettlement.deliveries), "single admission granted at one live obligation: %#v", firstSettlement)
	_, secondSettlement := runtime.observeAttempt(starts[1].generation, attemptSettled{})
	assert.Equal(t, []admissionGrant{queued.request}, secondSettlement.deliveries, "single admission did not grant at zero: %#v", secondSettlement)
}

func TestProcessRuntimeOverlapIgnoresUncommittedWorkAndRemainsLatched(t *testing.T) {
	runtime := newProcessRuntime(2)
	runtime, campaignA := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, campaignB := runtime.registerCampaign(campaignProvenance{lineage: 22})
	runtime, campaignC := runtime.registerCampaign(campaignProvenance{lineage: 33})
	runtime, admittedA := runtime.requestAdmission(admissionRequest{
		campaign: campaignA.token, attempt: "a", class: sharedAdmission,
	})
	runtime, admittedB := runtime.requestAdmission(admissionRequest{
		campaign: campaignB.token, attempt: "b", class: sharedAdmission,
	})
	runtime, startedA := runtime.startCommitted(admittedA.deliveries[0])
	assert.False(t, runtime.admissions[runtime.admissionIndexByGeneration(startedA.generation)].overlapped, "granted-but-uncommitted work counted as overlap")
	runtime, cancelled := runtime.cancelAdmission(admittedB.request)
	runtime, admittedC := runtime.requestAdmission(admissionRequest{
		campaign: campaignC.token, attempt: "c", class: sharedAdmission,
	})
	assert.EqualValues(t, 0, len(cancelled.deliveries), "unexpected cancellation/request deliveries: %#v/%#v", cancelled, admittedC)
	require.Len(t, admittedC.deliveries, 1, "unexpected cancellation/request deliveries: %#v/%#v", cancelled, admittedC)
	runtime, startedC := runtime.startCommitted(admittedC.deliveries[0])
	runtime, _ = runtime.observeAttempt(startedC.generation, launchNotReleased{reason: launchFailed})
	indexA := runtime.admissionIndexByGeneration(startedA.generation)
	assert.False(t, indexA < 0, "overlap was not retained after peer settlement: %#v", runtime)
	assert.True(t, runtime.admissions[indexA].overlapped, "overlap was not retained after peer settlement: %#v", runtime)
	runtime, _ = runtime.observeAttempt(startedA.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(startedA.generation, automaticDeadlineTrip())
	assert.False(t, runtime.unboundBarrierIndex(campaignA.token) < 0, "latched overlap did not attribute deadline: %#v", runtime)
}

func TestProcessRuntimeConfirmationSettlementAuthorizesSingleAdmission(t *testing.T) {
	runtime := runtimeAtBoundConfirmation(t)
	barrierAt := -1
	for index, admission := range runtime.admissions {
		if admission.grant.class == confirmationBarrierAdmission && admission.stage == admissionGranted {
			barrierAt = index
		}
	}
	assert.False(t, barrierAt < 0, "confirmation setup=%#v", runtime)
	assert.Equal(t, fullAutomatic, runtime.mode, "confirmation setup=%#v", runtime)
	grant := runtime.admissions[barrierAt].grant
	runtime, started := runtime.startCommitted(grant)
	runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
	runtime, result := runtime.observeAttempt(started.generation, settledConfirmation(grant))
	assert.Equal(t, singleAdmission, runtime.mode, "confirmation did not authorize pressure transition: %#v", runtime)
	assert.True(t, result.pressureTransitioned, "confirmation transition reopened gate early: %#v/%#v", result, runtime)
	assert.False(t, runtime.campaigns[runtime.campaignIndex(grant.campaign)].primaryGateOpen, "confirmation transition reopened gate early: %#v/%#v", result, runtime)
	runtime, completed := runtime.completeConfirmationQueue(grant.campaign)
	assert.Equal(t, confirmationQueueCompleted, completed.decision, "confirmation queue completion/state = %#v/%#v", completed, runtime)
	assert.True(t, runtime.campaigns[runtime.campaignIndex(grant.campaign)].primaryGateOpen, "confirmation queue completion/state = %#v/%#v", completed, runtime)
}

//nolint:cyclop // This trace distinguishes repeated continuation from the final queue-drained cut.
func TestProcessRuntimeConfirmationPressureDoesNotReopenGateBeforeQueueDrains(t *testing.T) {
	runtime := runtimeAtBoundConfirmation(t)
	firstAt := runtime.grantedConfirmationIndex()
	firstGrant := runtime.admissions[firstAt].grant
	runtime, first := runtime.startCommitted(firstGrant)
	runtime, _ = runtime.observeAttempt(first.generation, launchOwned{})
	runtime, firstResult := runtime.observeAttempt(first.generation, settledConfirmation(firstGrant))
	campaignAt := runtime.campaignIndex(firstGrant.campaign)
	assert.True(t, firstResult.pressureTransitioned, "continuing confirmation pressure/gate=%#v/%#v", firstResult, runtime)
	assert.Equal(t, singleAdmission, runtime.mode, "continuing confirmation pressure/gate=%#v/%#v", firstResult, runtime)
	assert.False(t, runtime.campaigns[campaignAt].primaryGateOpen, "continuing confirmation pressure/gate=%#v/%#v", firstResult, runtime)
	runtime, requested := runtime.requestAdmission(admissionRequest{
		campaign: firstGrant.campaign, attempt: "next-confirmation", class: confirmationAdmission, profile: AutomaticProfile, deadline: 31 * time.Second,
	})
	assert.Equal(t, admissionAccepted, requested.decision, "next confirmation admission=%#v", requested)
	require.Len(t, requested.deliveries, 1, "next confirmation admission=%#v", requested)
	runtime, second := runtime.startCommitted(requested.deliveries[0])
	runtime, _ = runtime.observeAttempt(second.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(second.generation, automaticDeadlineTrip())
	assert.False(t, runtime.campaigns[campaignAt].primaryGateOpen, "repeated continuing confirmation reopened gate: %#v", runtime)
	runtime, requested = runtime.requestAdmission(admissionRequest{
		campaign: firstGrant.campaign, attempt: "last-confirmation", class: confirmationAdmission, profile: AutomaticProfile, deadline: 31 * time.Second,
	})
	assert.Equal(t, admissionAccepted, requested.decision, "last confirmation admission=%#v", requested)
	require.Len(t, requested.deliveries, 1, "last confirmation admission=%#v", requested)
	runtime, last := runtime.startCommitted(requested.deliveries[0])
	runtime, _ = runtime.observeAttempt(last.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(last.generation, automaticDeadlineTrip())
	runtime, completed := runtime.completeConfirmationQueue(firstGrant.campaign)
	assert.Equal(t, confirmationQueueCompleted, completed.decision, "drained confirmation queue completion=%#v", completed)
	assert.True(t, runtime.campaigns[campaignAt].primaryGateOpen, "drained confirmation queue did not reopen gate: %#v", runtime)
	for _, admission := range runtime.admissions {
		assert.False(t, admission.grant.campaign == firstGrant.campaign && admission.grant.class.exclusive(), "gate reopened with outstanding confirmation: %#v", runtime)
	}
}

func TestProcessRuntimeRejectsConfirmationBeforePrimaryGateCloses(t *testing.T) {
	runtime := newProcessRuntime(1)
	runtime, campaign := runtime.registerCampaign(campaignProvenance{lineage: 11})
	unchanged := runtime
	runtime, result := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "early-confirmation", class: confirmationAdmission, profile: AutomaticProfile, deadline: 31 * time.Second,
	})
	assert.Equal(t, admissionRejectedGateOpen, result.decision, "early confirmation result/state=%#v/%#v", result, runtime)
	assert.Equal(t, unchanged, runtime, "early confirmation result/state=%#v/%#v", result, runtime)
}

//nolint:cyclop // This trace checks the ordered closure outputs and residual snapshot together.
func TestProcessRuntimeFatalClosurePreservesStableCorrelatedResidualCustody(t *testing.T) {
	runtime := newProcessRuntime(3)
	runtime, campaign := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, waitingCampaign := runtime.registerCampaign(campaignProvenance{lineage: 22})
	requests := make([]admissionResult, 0, 4)
	for _, attempt := range []attemptIdentity{"owned", "prospective", "granted", "waiting"} {
		requestCampaign := campaign.token
		if attempt == "waiting" {
			requestCampaign = waitingCampaign.token
		}
		var requested admissionResult
		runtime, requested = runtime.requestAdmission(admissionRequest{
			campaign: requestCampaign,
			attempt:  attempt,
			class:    sharedAdmission,
		})
		requests = append(requests, requested)
	}
	runtime, owned := runtime.startCommitted(requests[0].deliveries[0])
	runtime, prospective := runtime.startCommitted(requests[1].deliveries[0])
	runtime, _ = runtime.observeAttempt(owned.generation, launchOwned{})

	runtime, closed := runtime.closeRuntime(runtimeFatalCause("fatal test"))
	assert.Equal(t, runtimeFatalClosing, runtime.lifecycle, "closure result/state=%#v/%#v", closed, runtime)
	assert.Equal(t, []admissionRequestToken{requests[2].request}, closed.compensatedGrants, "closure uncommitted outputs=%#v", closed)
	assert.Equal(t, []admissionRequestToken{requests[3].request}, closed.cancelledWaiting, "closure uncommitted outputs=%#v", closed)
	wantResidual := []residualCustody{
		{generation: owned.generation, attempt: "owned", stage: admissionOwned, transferred: false},
		{generation: prospective.generation, attempt: "prospective", stage: admissionProspective, transferred: false},
	}
	{
		got := runtime.residualCustody()
		assert.Equal(t, wantResidual, got, "residual=%#v, want %#v", got, wantResidual)
	}
	runtime, rejected := runtime.startCommitted(requests[2].deliveries[0])
	assert.Equal(t, startCommittedRejectedClosed, rejected.decision, "known grant start after close=%#v", rejected)
	runtime, returned := runtime.acknowledgeGrantReturn(requests[2].deliveries[0])
	assert.Equal(t, admissionReturnedAfterClosure, returned.decision, "late known grant return=%#v", returned)

	runtime, swept := runtime.settleEmergency(emergencySweep{
		resolutions: []emergencyResolution{
			{generation: prospective.generation, disposition: emergencyCustodyTransferred},
			{generation: owned.generation, disposition: emergencyConfirmedDrained},
		},
	})
	assert.Equal(t, []attemptGeneration{prospective.generation, owned.generation}, swept.acknowledged, "sweep acknowledgements=%#v", swept)
	wantResidual = []residualCustody{{
		generation:  prospective.generation,
		attempt:     "prospective",
		stage:       admissionProspective,
		transferred: true,
	}}
	assert.Equal(t, runtimeClosedUnconfirmed, runtime.lifecycle, "post-sweep state=%#v residual=%#v", runtime, runtime.residualCustody())
	assert.Equal(t, wantResidual, runtime.residualCustody(), "post-sweep state=%#v residual=%#v", runtime, runtime.residualCustody())
}

func TestProcessRuntimeFatalClosingDefersEachValidOwnedTerminal(t *testing.T) {
	for _, test := range []struct {
		name        string
		observation attemptObservation
	}{
		{name: "settled", observation: attemptSettled{}},
		{name: "fuse trip", observation: attemptTripped{kind: fuseTrip}},
		{name: "deadline trip", observation: automaticDeadlineTrip()},
		{name: "stopped", observation: attemptStopped{}},
		{name: "infrastructure", observation: attemptInfrastructure{cause: "wait failed"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime, generation := fatalClosingRuntimeWithOwnedAttempt(t)
			next, result := runtime.observeAttempt(generation, test.observation)
			assertDeferredOwnedTerminal(t, next, generation, result)
		})
	}
}

func TestProcessRuntimeElectsOneResidualOwnerAndForceAbortsItsPeer(t *testing.T) {
	runtime := newProcessRuntime(2)
	runtime, firstCampaign := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, secondCampaign := runtime.registerCampaign(campaignProvenance{lineage: 12})
	runtime, first := runtime.requestAdmission(admissionRequest{
		campaign: firstCampaign.token, attempt: "first", class: sharedAdmission,
	})
	runtime, second := runtime.requestAdmission(admissionRequest{
		campaign: secondCampaign.token, attempt: "second", class: sharedAdmission,
	})
	runtime, firstStart := runtime.startCommitted(first.deliveries[0])
	runtime, secondStart := runtime.startCommitted(second.deliveries[0])
	runtime, _ = runtime.observeAttempt(firstStart.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(secondStart.generation, launchOwned{})
	runtime, closure := runtime.observeAttempt(firstStart.generation, drainUnconfirmed{})
	runtime, settlement := runtime.settleEmergency(emergencySweep{resolutions: []emergencyResolution{
		{generation: firstStart.generation, disposition: emergencyCustodyTransferred},
		{generation: secondStart.generation, disposition: emergencyCustodyTransferred},
	}})

	assert.Equal(t, firstCampaign.token, settlement.owner, "settlement owner = %#v, want first residual campaign %#v", settlement.owner, firstCampaign.token)
	runtime, peer := runtime.authorizeForcedAbort(secondCampaign.token, closure.fatalEpoch)
	assert.Equal(t, terminalForcedAborted, peer.decision, "peer terminal = %#v, want forced abort", peer)
	_, owner := runtime.authorizeForcedAbort(firstCampaign.token, closure.fatalEpoch)
	assert.Equal(t, terminalRejectedClosed, owner.decision, "owner terminal = %#v, want retained cleanup ownership", owner)
}

func TestProcessRuntimeFatalClosingTerminalDeferralRejectsMalformedAndDuplicateObservations(t *testing.T) {
	for _, test := range []struct {
		name        string
		observation attemptObservation
	}{
		{name: "trip kind", observation: attemptTripped{}},
		{name: "infrastructure cause", observation: attemptInfrastructure{}},
	} {
		t.Run("malformed "+test.name, func(t *testing.T) {
			runtime, generation := fatalClosingRuntimeWithOwnedAttempt(t)
			unchanged := runtime
			assertInvariantViolation(t, func() { runtime.observeAttempt(generation, test.observation) })
			assert.Equal(t, unchanged, runtime, "malformed terminal changed state: %#v", runtime)
		})
	}

	for _, test := range []struct {
		name        string
		observation attemptObservation
	}{
		{name: "same", observation: attemptStopped{}},
		{name: "different", observation: attemptInfrastructure{cause: "later failure"}},
	} {
		t.Run(test.name+" duplicate", func(t *testing.T) {
			runtime, generation := fatalClosingRuntimeWithOwnedAttempt(t)
			runtime, _ = runtime.observeAttempt(generation, attemptStopped{})
			unchanged := runtime
			assertInvariantViolation(t, func() { runtime.observeAttempt(generation, test.observation) })
			assert.Equal(t, unchanged, runtime, "duplicate terminal changed state: %#v", runtime)
		})
	}
}

func TestProcessRuntimeTerminalDeferralRejectsInvalidCustodyStates(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T) (processRuntime, attemptGeneration)
	}{
		{
			name: "prospective",
			setup: func(t *testing.T) (processRuntime, attemptGeneration) {
				t.Helper()
				runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, false)
				runtime, _ = runtime.closeRuntime(runtimeFatalCause("fatal test"))

				return runtime, generation
			},
		},
		{
			name: "fatal seeded",
			setup: func(t *testing.T) (processRuntime, attemptGeneration) {
				t.Helper()
				runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, false)
				runtime, _ = runtime.observeAttempt(generation, launchUnconfirmed{})
				runtime, _ = runtime.observeAttempt(generation, launchOwned{})
				index := runtime.admissionIndexByGeneration(generation)
				assert.False(t, index < 0, "late adopted fatal custody setup = %#v", runtime)
				assert.Equal(t, runtimeFatalClosing, runtime.lifecycle, "late adopted fatal custody setup = %#v", runtime)
				assert.Equal(t, admissionOwned, runtime.admissions[index].stage, "late adopted fatal custody setup = %#v", runtime)
				assert.Equal(t, dispositionFatalSeeded, runtime.admissions[index].disposition, "late adopted fatal custody setup = %#v", runtime)

				return runtime, generation
			},
		},
		{
			name: "transferred",
			setup: func(t *testing.T) (processRuntime, attemptGeneration) {
				t.Helper()
				runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, true)
				runtime, _ = runtime.observeAttempt(generation, drainUnconfirmed{})

				return runtime, generation
			},
		},
		{name: "settled", setup: fatalClosingRuntimeWithSettledCustody},
		{
			name: "closed drained",
			setup: func(t *testing.T) (processRuntime, attemptGeneration) {
				t.Helper()
				runtime, generation := fatalClosingRuntimeWithOwnedAttempt(t)
				runtime, _ = runtime.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
					generation: generation, disposition: emergencyConfirmedDrained,
				}}})

				return runtime, generation
			},
		},
		{
			name: "closed unconfirmed",
			setup: func(t *testing.T) (processRuntime, attemptGeneration) {
				t.Helper()
				runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, true)
				runtime, _ = runtime.observeAttempt(generation, drainUnconfirmed{})
				runtime, _ = runtime.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
					generation: generation, disposition: emergencyCustodyTransferred,
				}}})

				return runtime, generation
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, generation := test.setup(t)
			unchanged := runtime
			assertInvariantViolation(t, func() { runtime.observeAttempt(generation, attemptSettled{}) })
			assert.Equal(t, unchanged, runtime, "invalid custody terminal changed state: %#v", runtime)
		})
	}
}

func TestProcessRuntimeEmergencyAloneAcknowledgesDeferredTerminalAsDrained(t *testing.T) {
	t.Run("confirmed drained", func(t *testing.T) {
		runtime, generation := fatalClosingRuntimeWithOwnedAttempt(t)
		runtime, deferred := runtime.observeAttempt(generation, attemptSettled{})
		runtime, settled := runtime.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
			generation: generation, disposition: emergencyConfirmedDrained,
		}}})
		assert.False(t, deferred.settlementAcknowledged, "deferred/emergency settlement = %#v/%#v/%#v", deferred, settled, runtime)
		assert.True(t, deferred.runtimeClosureInProgress, "deferred/emergency settlement = %#v/%#v/%#v", deferred, settled, runtime)
		assert.Equal(t, []attemptGeneration{generation}, settled.acknowledged, "deferred/emergency settlement = %#v/%#v/%#v", deferred, settled, runtime)
		assert.EqualValues(t, 0, len(settled.residual), "deferred/emergency settlement = %#v/%#v/%#v", deferred, settled, runtime)
		assert.Equal(t, runtimeClosedDrained, runtime.lifecycle, "deferred/emergency settlement = %#v/%#v/%#v", deferred, settled, runtime)
	})

	t.Run("custody transfer", func(t *testing.T) {
		runtime, generation := fatalClosingRuntimeWithOwnedAttempt(t)
		runtime, _ = runtime.observeAttempt(generation, attemptSettled{})
		unchanged := runtime
		assertInvariantViolation(t, func() {
			runtime.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
				generation: generation, disposition: emergencyCustodyTransferred,
			}}})
		})
		assert.Equal(t, unchanged, runtime, "deferred transfer changed state: %#v", runtime)
	})
}

func TestProcessRuntimeDeferredTerminalWaitsForKnownGrantReturnBeforeFinalClosure(t *testing.T) {
	runtime := newProcessRuntime(2)
	runtime, campaign := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, owned := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "owned", class: sharedAdmission,
	})
	runtime, granted := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "return pending", class: sharedAdmission,
	})
	runtime, started := runtime.startCommitted(owned.deliveries[0])
	runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
	runtime, closed := runtime.closeRuntime(runtimeFatalCause("fatal test"))
	assert.Equal(t, []admissionRequestToken{granted.request}, closed.compensatedGrants, "closure compensation=%#v", closed)
	runtime, deferred := runtime.observeAttempt(started.generation, attemptSettled{})
	assertDeferredOwnedTerminal(t, runtime, started.generation, deferred)

	runtime, settled := runtime.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
		generation: started.generation, disposition: emergencyConfirmedDrained,
	}}})
	assert.Equal(t, runtimeFatalSettledClosing, runtime.lifecycle, "settlement finalized before grant return: %#v/%#v", runtime, settled)
	assert.Equal(t, []attemptGeneration{started.generation}, settled.acknowledged, "settlement finalized before grant return: %#v/%#v", runtime, settled)
	assert.EqualValues(t, 0, len(settled.residual), "settlement finalized before grant return: %#v/%#v", runtime, settled)

	runtime, returned := runtime.acknowledgeGrantReturn(granted.deliveries[0])
	assert.Equal(t, admissionReturnedAfterClosure, returned.decision, "grant return did not finalize drained closure: %#v/%#v", returned, runtime)
	assert.Equal(t, runtimeClosedDrained, runtime.lifecycle, "grant return did not finalize drained closure: %#v/%#v", returned, runtime)
	unchanged := runtime
	assertInvariantViolation(t, func() { runtime.acknowledgeGrantReturn(granted.deliveries[0]) })
	assert.Equal(t, unchanged, runtime, "duplicate return changed closed runtime: %#v", runtime)
}

func assertDeferredOwnedTerminal(
	t *testing.T,
	runtime processRuntime,
	generation attemptGeneration,
	result observationResult,
) {
	t.Helper()
	index := runtime.admissionIndexByGeneration(generation)
	require.False(t, index < 0, "deferred terminal state = %#v", runtime)
	assert.Equal(t, dispositionTerminalDeferred, runtime.admissions[index].disposition, "deferred terminal state = %#v", runtime)
	want := observationResult{generation: generation, runtimeClosureInProgress: true}
	assert.Equal(t, want, result, "deferred terminal receipt = %#v, want %#v", result, want)
}

func fatalClosingRuntimeWithOwnedAttempt(t *testing.T) (processRuntime, attemptGeneration) {
	t.Helper()
	runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, true)
	runtime, _ = runtime.closeRuntime(runtimeFatalCause("fatal test"))
	assert.Equal(t, runtimeFatalClosing, runtime.lifecycle, "owned setup did not enter fatal closing: %#v", runtime)

	return runtime, generation
}

func fatalClosingRuntimeWithSettledCustody(t *testing.T) (processRuntime, attemptGeneration) {
	t.Helper()
	runtime := newProcessRuntime(2)
	runtime, campaign := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, owned := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "owned", class: sharedAdmission,
	})
	runtime, blocker := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "return blocker", class: sharedAdmission,
	})
	runtime, started := runtime.startCommitted(owned.deliveries[0])
	runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(started.generation, drainUnconfirmed{})
	runtime, _ = runtime.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
		generation: started.generation, disposition: emergencyCustodyTransferred,
	}}})
	index := runtime.admissionIndexByGeneration(started.generation)
	assert.Equal(t, runtimeFatalSettledClosing, runtime.lifecycle, "settled custody setup = %#v/%#v", runtime, blocker)
	assert.EqualValues(t, 1, len(blocker.deliveries), "settled custody setup = %#v/%#v", runtime, blocker)
	assert.False(t, index < 0, "settled custody setup = %#v/%#v", runtime, blocker)
	assert.Equal(t, admissionOwned, runtime.admissions[index].stage, "settled custody setup = %#v/%#v", runtime, blocker)
	assert.Equal(t, dispositionCustodySettled, runtime.admissions[index].disposition, "settled custody setup = %#v/%#v", runtime, blocker)

	return runtime, started.generation
}

func TestProcessRuntimeDrainUnconfirmedNeverLooksLikeRelease(t *testing.T) {
	runtime := newProcessRuntime(1)
	runtime, campaign := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, first := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "a", class: sharedAdmission,
	})
	runtime, started := runtime.startCommitted(first.deliveries[0])
	runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
	runtime, waiting := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "b", class: sharedAdmission,
	})
	runtime, result := runtime.observeAttempt(started.generation, drainUnconfirmed{})

	assert.True(t, result.runtimeClosureInProgress, "drain-unconfirmed result/state=%#v/%#v", result, runtime)
	assert.EqualValues(t, 0, len(result.deliveries), "drain-unconfirmed result/state=%#v/%#v", result, runtime)
	assert.False(t, runtime.admissionIndex(waiting.request) >= 0, "drain-unconfirmed result/state=%#v/%#v", result, runtime)
	want := []residualCustody{{
		generation: started.generation, attempt: "a", stage: admissionOwned, transferred: true,
	}}
	assert.Equal(t, want, runtime.residualCustody(), "drain-unconfirmed residual=%#v, want %#v", runtime.residualCustody(), want)
}

func TestProcessRuntimeCancellationCannotRewriteCommittedCustody(t *testing.T) {
	stages := []struct {
		name    string
		advance func(processRuntime, attemptGeneration) processRuntime
	}{
		{name: "prospective", advance: func(runtime processRuntime, _ attemptGeneration) processRuntime { return runtime }},
		{name: "owned", advance: func(runtime processRuntime, generation attemptGeneration) processRuntime {
			runtime, _ = runtime.observeAttempt(generation, launchOwned{})

			return runtime
		}},
		{name: "custody transferred", advance: func(runtime processRuntime, generation attemptGeneration) processRuntime {
			runtime, _ = runtime.observeAttempt(generation, launchOwned{})
			runtime, _ = runtime.observeAttempt(generation, drainUnconfirmed{})

			return runtime
		}},
	}
	for _, stage := range stages {
		t.Run(stage.name, func(t *testing.T) {
			runtime := newProcessRuntime(1)
			runtime, campaign := runtime.registerCampaign(campaignProvenance{lineage: 11})
			runtime, requested := runtime.requestAdmission(admissionRequest{
				campaign: campaign.token, attempt: "a", class: sharedAdmission,
			})
			runtime, started := runtime.startCommitted(requested.deliveries[0])
			runtime = stage.advance(runtime, started.generation)
			unchanged := runtime
			runtime, cancelled := runtime.cancelAdmission(requested.request)
			assert.Equal(t, admissionRejectedAlreadyCommitted, cancelled.decision, "cancel committed result/state=%#v/%#v", cancelled, runtime)
			assert.Equal(t, unchanged, runtime, "cancel committed result/state=%#v/%#v", cancelled, runtime)
		})
	}
}

func TestProcessRuntimeCancellationCannotRewriteClosedUnconfirmed(t *testing.T) {
	runtime := newProcessRuntime(1)
	runtime, campaign := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, requested := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "a", class: sharedAdmission,
	})
	runtime, started := runtime.startCommitted(requested.deliveries[0])
	runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(started.generation, drainUnconfirmed{})
	runtime, _ = runtime.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
		generation: started.generation, disposition: emergencyCustodyTransferred,
	}}})
	unchanged := runtime
	runtime, cancelled := runtime.cancelAdmission(requested.request)
	assert.Equal(t, admissionRejectedAlreadyCommitted, cancelled.decision, "closed-unconfirmed cancellation result/state=%#v/%#v", cancelled, runtime)
	assert.Equal(t, unchanged, runtime, "closed-unconfirmed cancellation result/state=%#v/%#v", cancelled, runtime)
}

func TestProcessRuntimeTerminalCommitRequiresNoOutstandingCustody(t *testing.T) {
	runtime := newProcessRuntime(1)
	runtime, campaign := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, admitted := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "a", class: sharedAdmission,
	})
	unchanged := runtime
	runtime, rejected := runtime.commitTerminal(campaign.token)
	assert.Equal(t, terminalRejectedOutstanding, rejected.decision, "premature terminal=%#v/%#v", rejected, runtime)
	assert.Equal(t, unchanged, runtime, "premature terminal=%#v/%#v", rejected, runtime)
	runtime, _ = runtime.cancelAdmission(admitted.request)
	runtime, accepted := runtime.commitTerminal(campaign.token)
	assert.Equal(t, terminalCommitted, accepted.decision, "terminal commit=%#v/%#v", accepted, runtime)
	assert.False(t, runtime.hasCampaign(campaign.token), "terminal commit=%#v/%#v", accepted, runtime)
}

func TestProcessRuntimeTerminalCommitRetiresEmptyConfirmationClosure(t *testing.T) {
	runtime := newProcessRuntime(2)
	runtime, campaign := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, peerCampaign := runtime.registerCampaign(campaignProvenance{lineage: 22})
	runtime, admitted := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "candidate", class: sharedAdmission,
	})
	runtime, peerAdmitted := runtime.requestAdmission(admissionRequest{
		campaign: peerCampaign.token, attempt: "peer", class: sharedAdmission,
	})
	runtime, started := runtime.startCommitted(admitted.deliveries[0])
	runtime, peerStarted := runtime.startCommitted(peerAdmitted.deliveries[0])
	runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(peerStarted.generation, launchOwned{})
	runtime, provisional := runtime.observeAttempt(started.generation, automaticDeadlineTrip())
	runtime, _ = runtime.observeAttempt(peerStarted.generation, attemptSettled{})
	assert.True(t, provisional.confirmationProvisional, "empty confirmation closure setup=%#v/%#v", provisional, runtime)
	require.Len(t, runtime.admissions, 1, "empty confirmation closure setup=%#v/%#v", provisional, runtime)
	assert.Equal(t, confirmationBarrierAdmission, runtime.admissions[0].grant.class, "empty confirmation closure setup=%#v/%#v", provisional, runtime)
	assert.EqualValues(t, "", runtime.admissions[0].grant.attempt, "empty confirmation closure setup=%#v/%#v", provisional, runtime)

	runtime, committed := runtime.commitTerminal(campaign.token)
	assert.Equal(t, terminalCommitted, committed.decision, "terminal after empty confirmation closure=%#v/%#v", committed, runtime)
	assert.False(t, runtime.hasCampaign(campaign.token), "terminal after empty confirmation closure=%#v/%#v", committed, runtime)
	assert.False(t, slices.ContainsFunc(runtime.admissions, func(admission admittedAttempt) bool {
		return admission.grant.campaign == campaign.token
	}), "terminal after empty confirmation closure=%#v/%#v", committed, runtime)
}

func TestProcessRuntimeTerminalCommitCannotRetireBoundConfirmation(t *testing.T) {
	runtime := runtimeAtBoundConfirmation(t)
	campaign := runtime.admissions[runtime.grantedConfirmationIndex()].grant.campaign
	unchanged := runtime

	runtime, committed := runtime.commitTerminal(campaign)
	assert.Equal(t, terminalRejectedOutstanding, committed.decision, "terminal with bound confirmation=%#v/%#v", committed, runtime)
	assert.Equal(t, unchanged, runtime, "terminal with bound confirmation=%#v/%#v", committed, runtime)
}

func TestProcessRuntimePressureAndOverlapExclusions(t *testing.T) {
	t.Run("a lone hard exhaustion qualifies", func(t *testing.T) {
		runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, false)
		runtime, _ = runtime.observeAttempt(generation, launchNotReleased{reason: launchResourceExhausted})
		assert.Equal(t, singleAdmission, runtime.mode, "hard exhaustion mode=%v", runtime.mode)
	})

	t.Run("launch failure does not qualify", func(t *testing.T) {
		runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, false)
		runtime, _ = runtime.observeAttempt(generation, launchNotReleased{reason: launchFailed})
		assert.Equal(t, fullAutomatic, runtime.mode, "launch failure mode=%v", runtime.mode)
	})

	t.Run("baseline exclusive settlement does not qualify", func(t *testing.T) {
		runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, exclusiveAdmission, true)
		runtime, _ = runtime.observeAttempt(generation, attemptSettled{})
		assert.Equal(t, fullAutomatic, runtime.mode, "baseline exclusive mode=%v", runtime.mode)
	})
}

func TestProcessRuntimeTripAndTerminalExclusions(t *testing.T) {
	t.Run("granted peer does not attribute deadline overlap", func(t *testing.T) {
		runtime := newProcessRuntime(2)
		runtime, campaignA := runtime.registerCampaign(campaignProvenance{lineage: 11})
		runtime, campaignB := runtime.registerCampaign(campaignProvenance{lineage: 22})
		runtime, requestA := runtime.requestAdmission(admissionRequest{
			campaign: campaignA.token,
			attempt:  "a",
			class:    sharedAdmission,
		})
		runtime, _ = runtime.requestAdmission(admissionRequest{
			campaign: campaignB.token,
			attempt:  "b",
			class:    sharedAdmission,
		})
		runtime, started := runtime.startCommitted(requestA.deliveries[0])
		runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
		runtime, _ = runtime.observeAttempt(started.generation, automaticDeadlineTrip())
		campaignAt := runtime.campaignIndex(campaignA.token)
		assert.True(t, runtime.campaigns[campaignAt].primaryGateOpen, "unattributed deadline state=%#v", runtime)
		assert.False(t, runtime.unboundBarrierIndex(campaignA.token) >= 0, "unattributed deadline state=%#v", runtime)
	})

	t.Run("confirmation deadline does not qualify", func(t *testing.T) {
		runtime := runtimeAtBoundConfirmation(t)
		barrierAt := runtime.grantedConfirmationIndex()
		grant := runtime.admissions[barrierAt].grant
		runtime, started := runtime.startCommitted(grant)
		runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
		runtime, _ = runtime.observeAttempt(started.generation, automaticDeadlineTrip())
		assert.Equal(t, fullAutomatic, runtime.mode, "confirmation deadline mode=%v", runtime.mode)
	})

	t.Run("fuse and infrastructure release without pressure", func(t *testing.T) {
		for _, observation := range []attemptObservation{
			attemptTripped{kind: fuseTrip},
			attemptInfrastructure{cause: "confirmed infrastructure"},
			attemptStopped{},
		} {
			runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, true)
			runtime, result := runtime.observeAttempt(generation, observation)
			assert.Equal(t, fullAutomatic, runtime.mode, "observation %T result/state=%#v/%#v", observation, result, runtime)
			assert.True(t, result.settlementAcknowledged, "observation %T result/state=%#v/%#v", observation, result, runtime)
		}
	})
}

func TestProcessRuntimeInvariantBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		action func()
	}{
		{name: "non-positive capacity", action: func() { newProcessRuntime(0) }},
		{name: "zero lineage", action: func() {
			runtime := newProcessRuntime(1)
			runtime.registerCampaign(campaignProvenance{lineage: 0})
		}},
		{name: "wrong settlement generation", action: func() {
			runtime, _ := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, true)
			runtime.observeAttempt(999, attemptSettled{})
		}},
		{name: "partial emergency sweep", action: func() {
			runtime, _ := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, true)
			runtime, _ = runtime.closeRuntime(runtimeFatalCause("fatal test"))
			runtime.settleEmergency(emergencySweep{resolutions: nil})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertInvariantViolation(t, test.action) })
	}
}

func TestProcessRuntimeOverlapTripCancelsSameCampaignPrimariesBeforeBarrier(t *testing.T) {
	runtime := newProcessRuntime(3)
	runtime, campaignA := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, campaignB := runtime.registerCampaign(campaignProvenance{lineage: 22})
	runtime, campaignC := runtime.registerCampaign(campaignProvenance{lineage: 33})
	runtime, a1 := runtime.requestAdmission(admissionRequest{campaign: campaignA.token, attempt: "a1", class: sharedAdmission})
	runtime, b1 := runtime.requestAdmission(admissionRequest{campaign: campaignB.token, attempt: "b1", class: sharedAdmission})
	runtime, requestA2 := runtime.requestAdmission(admissionRequest{campaign: campaignA.token, attempt: "a2", class: sharedAdmission})
	runtime, requestA3 := runtime.requestAdmission(admissionRequest{campaign: campaignA.token, attempt: "a3", class: sharedAdmission})
	runtime, requestC1 := runtime.requestAdmission(admissionRequest{campaign: campaignC.token, attempt: "c1", class: sharedAdmission})
	runtime, startedA := runtime.startCommitted(a1.deliveries[0])
	runtime, startedB := runtime.startCommitted(b1.deliveries[0])
	runtime, _ = runtime.observeAttempt(startedA.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(startedB.generation, launchOwned{})
	runtime, tripped := runtime.observeAttempt(startedA.generation, automaticDeadlineTrip())

	assert.Equal(t, []admissionRequestToken{requestA2.request}, tripped.compensatedGrants, "trip cancellation outputs=%#v", tripped)
	assert.Equal(t, []admissionRequestToken{requestA3.request}, tripped.cancelledWaiting, "trip cancellation outputs=%#v", tripped)
	assert.Equal(t, []admissionGrant{requestC1.request}, tripped.deliveries, "pre-barrier FIFO delivery=%#v", tripped.deliveries)
	assert.False(t, runtime.unboundBarrierIndex(campaignA.token) < runtime.admissionIndex(requestC1.request), "barrier overtook an earlier other-campaign request: %#v", runtime)
	barrierAt := runtime.unboundBarrierIndex(campaignA.token)
	assert.False(t, barrierAt < 0, "unbound barrier acquired a delivery action: %#v", runtime)
	assert.Nil(t, runtime.admissions[barrierAt].grant.delivery, "unbound barrier acquired a delivery action: %#v", runtime)
	_, returned := runtime.acknowledgeGrantReturn(requestA2.deliveries[0])
	assert.Equal(t, admissionReturnedAfterGateClosure, returned.decision, "compensated grant return=%#v", returned)
}

func TestProcessRuntimeFatalClosureDoesNotCompensateGateReturnedGrantTwice(t *testing.T) {
	runtime := newProcessRuntime(3)
	runtime, campaignA := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, campaignB := runtime.registerCampaign(campaignProvenance{lineage: 22})
	runtime, a1 := runtime.requestAdmission(admissionRequest{campaign: campaignA.token, attempt: "a1", class: sharedAdmission})
	runtime, requestA2 := runtime.requestAdmission(admissionRequest{campaign: campaignA.token, attempt: "a2", class: sharedAdmission})
	runtime, b1 := runtime.requestAdmission(admissionRequest{campaign: campaignB.token, attempt: "b1", class: sharedAdmission})
	runtime, startedA := runtime.startCommitted(a1.deliveries[0])
	runtime, startedB := runtime.startCommitted(b1.deliveries[0])
	runtime, _ = runtime.observeAttempt(startedA.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(startedB.generation, launchOwned{})
	runtime, tripped := runtime.observeAttempt(startedA.generation, automaticDeadlineTrip())
	assert.Equal(t, []admissionRequestToken{requestA2.request}, tripped.compensatedGrants, "gate compensation=%#v", tripped.compensatedGrants)
	runtime, closed := runtime.closeRuntime(runtimeFatalCause("fatal after gate closure"))
	assert.EqualValues(t, 0, len(closed.compensatedGrants), "fatal closure repeated gate compensation: %#v", closed.compensatedGrants)
	returnedAt := runtime.admissionIndex(requestA2.request)
	assert.False(t, returnedAt < 0, "fatal closure rewrote gate attribution: %#v", runtime)
	assert.Equal(t, dispositionReturnedAfterGate, runtime.admissions[returnedAt].disposition, "fatal closure rewrote gate attribution: %#v", runtime)
	runtime, returned := runtime.acknowledgeGrantReturn(requestA2.deliveries[0])
	assert.Equal(t, admissionReturnedAfterGateClosure, returned.decision, "gate return acknowledgement=%#v/%#v", returned, runtime)
	assert.Equal(t, runtimeFatalClosing, runtime.lifecycle, "gate return acknowledgement=%#v/%#v", returned, runtime)
	resolutions := make([]emergencyResolution, len(runtime.residualCustody()))
	for index, residual := range runtime.residualCustody() {
		resolutions[index] = emergencyResolution{
			generation: residual.generation, disposition: emergencyCustodyTransferred,
		}
	}
	runtime, _ = runtime.settleEmergency(emergencySweep{resolutions: resolutions})
	assert.Equal(t, runtimeClosedUnconfirmed, runtime.lifecycle, "gate return did not permit final settlement: %#v", runtime)
}

//nolint:cyclop,gocognit,nestif // Both legal return/sweep orders must assert the same final cut in one corpus.
func TestProcessRuntimeFinalClosureWaitsForCompensatedGrantReturn(t *testing.T) {
	for _, returnFirst := range []bool{true, false} {
		name := "emergency before return"
		if returnFirst {
			name = "return before emergency"
		}
		t.Run(name, func(t *testing.T) {
			runtime, generation, granted := runtimeAwaitingGrantReturn(t)
			unchanged := runtime
			runtime, lateCancel := runtime.cancelAdmission(granted.request)
			assert.Equal(t, admissionRejectedAlreadyCommitted, lateCancel.decision, "late cancellation consumed return authority: %#v/%#v", lateCancel, runtime)
			assert.Equal(t, unchanged, runtime, "late cancellation consumed return authority: %#v/%#v", lateCancel, runtime)
			wrong := granted.deliveries[0]
			wrong.attempt += "-wrong"
			assertInvariantViolation(t, func() { runtime.acknowledgeGrantReturn(wrong) })
			assert.Equal(t, unchanged, runtime, "wrong return acknowledgement changed state: %#v", runtime)
			sweep := emergencySweep{resolutions: []emergencyResolution{{
				generation: generation, disposition: emergencyCustodyTransferred,
			}}}
			var returned admissionResult
			if returnFirst {
				runtime, returned = runtime.acknowledgeGrantReturn(granted.deliveries[0])
				assert.Equal(t, runtimeFatalClosing, runtime.lifecycle, "return finalized before emergency settlement: %#v", runtime)
				unchanged := runtime
				var lateCancel admissionResult
				runtime, lateCancel = runtime.cancelAdmission(granted.request)
				assert.Equal(t, admissionRejectedClosed, lateCancel.decision, "late cancellation changed fatal-closing state: %#v/%#v", lateCancel, runtime)
				assert.Equal(t, unchanged, runtime, "late cancellation changed fatal-closing state: %#v/%#v", lateCancel, runtime)
				assertInvariantViolation(t, func() { runtime.acknowledgeGrantReturn(granted.deliveries[0]) })
				assert.Equal(t, unchanged, runtime, "duplicate return changed fatal-closing state: %#v", runtime)
				runtime, _ = runtime.settleEmergency(sweep)
			} else {
				runtime, _ = runtime.settleEmergency(sweep)
				assert.Equal(t, runtimeFatalSettledClosing, runtime.lifecycle, "emergency finalized before grant return: %#v", runtime)
				unchanged := runtime
				assertInvariantViolation(t, func() { runtime.settleEmergency(sweep) })
				assert.Equal(t, unchanged, runtime, "duplicate emergency settlement changed state: %#v", runtime)
				runtime, returned = runtime.acknowledgeGrantReturn(granted.deliveries[0])
			}
			want := []residualCustody{{
				generation: generation, attempt: "owned", stage: admissionOwned, transferred: true,
			}}
			assert.Equal(t, admissionReturnedAfterClosure, returned.decision, "return/final closure=%#v/%#v", returned, runtime)
			assert.Equal(t, runtimeClosedUnconfirmed, runtime.lifecycle, "return/final closure=%#v/%#v", returned, runtime)
			assert.Equal(t, want, runtime.residualCustody(), "return/final closure=%#v/%#v", returned, runtime)
			unchanged = runtime
			runtime, lateCancel = runtime.cancelAdmission(granted.request)
			assert.Equal(t, admissionRejectedClosed, lateCancel.decision, "late cancellation changed final state: %#v/%#v", lateCancel, runtime)
			assert.Equal(t, unchanged, runtime, "late cancellation changed final state: %#v/%#v", lateCancel, runtime)
			assertInvariantViolation(t, func() { runtime.acknowledgeGrantReturn(granted.deliveries[0]) })
			assert.Equal(t, unchanged, runtime, "closed state changed after duplicate return: %#v", runtime)
		})
	}
}

func runtimeAwaitingGrantReturn(t *testing.T) (processRuntime, attemptGeneration, admissionResult) {
	t.Helper()
	runtime := newProcessRuntime(2)
	runtime, campaignA := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, campaignB := runtime.registerCampaign(campaignProvenance{lineage: 22})
	runtime, obligation := runtime.requestAdmission(admissionRequest{
		campaign: campaignA.token, attempt: "owned", class: sharedAdmission,
	})
	runtime, granted := runtime.requestAdmission(admissionRequest{
		campaign: campaignB.token, attempt: "granted", class: sharedAdmission,
	})
	runtime, started := runtime.startCommitted(obligation.deliveries[0])
	runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
	runtime, closed := runtime.closeRuntime(runtimeFatalCause("fatal test"))
	assert.Equal(t, []admissionRequestToken{granted.request}, closed.compensatedGrants, "fatal compensation=%#v", closed.compensatedGrants)

	return runtime, started.generation, granted
}

func TestProcessRuntimeLaterOverlapTripJoinsExistingBarrier(t *testing.T) {
	runtime := newProcessRuntime(3)
	runtime, campaignA := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, campaignB := runtime.registerCampaign(campaignProvenance{lineage: 22})
	runtime, a1 := runtime.requestAdmission(admissionRequest{campaign: campaignA.token, attempt: "a1", class: sharedAdmission})
	runtime, a2 := runtime.requestAdmission(admissionRequest{campaign: campaignA.token, attempt: "a2", class: sharedAdmission})
	runtime, b1 := runtime.requestAdmission(admissionRequest{campaign: campaignB.token, attempt: "b1", class: sharedAdmission})
	runtime, startedA1 := runtime.startCommitted(a1.deliveries[0])
	runtime, startedA2 := runtime.startCommitted(a2.deliveries[0])
	runtime, startedB := runtime.startCommitted(b1.deliveries[0])
	for _, generation := range []attemptGeneration{startedA1.generation, startedA2.generation, startedB.generation} {
		runtime, _ = runtime.observeAttempt(generation, launchOwned{})
	}
	runtime, first := runtime.observeAttempt(startedA1.generation, automaticDeadlineTrip())
	runtime, second := runtime.observeAttempt(startedA2.generation, automaticDeadlineTrip())
	barriers := 0
	for _, admission := range runtime.admissions {
		if admission.grant.class == confirmationBarrierAdmission {
			barriers++
		}
	}
	assert.True(t, first.confirmationProvisional, "provisional results/barriers=%#v/%#v/%d", first, second, barriers)
	assert.True(t, second.confirmationProvisional, "provisional results/barriers=%#v/%#v/%d", first, second, barriers)
	assert.EqualValues(t, 1, barriers, "provisional results/barriers=%#v/%#v/%d", first, second, barriers)
	assert.EqualValues(t, 0, len(second.cancelledWaiting), "provisional results/barriers=%#v/%#v/%d", first, second, barriers)
	assert.EqualValues(t, 0, len(second.compensatedGrants), "provisional results/barriers=%#v/%#v/%d", first, second, barriers)
}

func TestProcessRuntimeConfirmationOutcomeControlsPressureAndReopensGate(t *testing.T) {
	t.Run("rejected", func(t *testing.T) {
		runtime := runtimeAtBoundConfirmation(t)
		barrierAt := runtime.grantedConfirmationIndex()
		grant := runtime.admissions[barrierAt].grant
		runtime, started := runtime.startCommitted(grant)
		runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
		runtime, result := runtime.observeAttempt(started.generation, automaticDeadlineTrip())
		runtime, completed := runtime.completeConfirmationQueue(grant.campaign)
		campaignAt := runtime.campaignIndex(grant.campaign)
		assert.Equal(t, confirmationQueueCompleted, completed.decision, "rejected confirmation result/completion/state=%#v/%#v/%#v", result, completed, runtime)
		assert.Equal(t, fullAutomatic, runtime.mode, "rejected confirmation result/completion/state=%#v/%#v/%#v", result, completed, runtime)
		assert.False(t, result.pressureTransitioned, "rejected confirmation result/completion/state=%#v/%#v/%#v", result, completed, runtime)
		assert.True(t, runtime.campaigns[campaignAt].primaryGateOpen, "rejected confirmation result/completion/state=%#v/%#v/%#v", result, completed, runtime)
	})

	t.Run("already single is idempotent", func(t *testing.T) {
		runtime := runtimeAtBoundConfirmation(t)
		runtime.mode = singleAdmission
		barrierAt := runtime.grantedConfirmationIndex()
		grant := runtime.admissions[barrierAt].grant
		runtime, started := runtime.startCommitted(grant)
		runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
		_, result := runtime.observeAttempt(started.generation, settledConfirmation(grant))
		assert.False(t, result.pressureTransitioned, "duplicate pressure transition=%#v", result)
	})
}

func TestProcessRuntimeDerivesConfirmationPressureFromOrdinarySettlement(t *testing.T) {
	runtime := runtimeAtBoundConfirmation(t)
	barrierAt := runtime.grantedConfirmationIndex()
	grant := runtime.admissions[barrierAt].grant
	runtime, started := runtime.startCommitted(grant)
	runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
	runtime, result := runtime.observeAttempt(started.generation, settledConfirmation(grant))
	campaignAt := runtime.campaignIndex(grant.campaign)
	assert.Equal(t, singleAdmission, runtime.mode, "ordinary confirmation settlement result/state = %#v/%#v", result, runtime)
	assert.True(t, result.pressureTransitioned, "ordinary confirmation settlement result/state = %#v/%#v", result, runtime)
	assert.False(t, runtime.campaigns[campaignAt].primaryGateOpen, "ordinary confirmation settlement result/state = %#v/%#v", result, runtime)
}

func TestProcessRuntimeRejectsPressureWhenConfirmationFactsDifferFromGrant(t *testing.T) {
	runtime := runtimeAtBoundConfirmation(t)
	barrierAt := runtime.grantedConfirmationIndex()
	runtime.admissions[barrierAt].grant.profile = AutomaticProfile
	runtime.admissions[barrierAt].grant.deadline = 31 * time.Second
	grant := runtime.admissions[barrierAt].grant
	runtime, started := runtime.startCommitted(grant)
	runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
	runtime, result := runtime.observeAttempt(started.generation, attemptSettled{
		profile: AutomaticProfile, deadline: 30 * time.Second,
	})
	assert.Equal(t, fullAutomatic, runtime.mode, "mismatched confirmation changed pressure = %#v/%#v", result, runtime)
	assert.False(t, result.pressureTransitioned, "mismatched confirmation changed pressure = %#v/%#v", result, runtime)
}

func TestProcessRuntimeReopensPrimaryGateOnlyAfterConfirmationQueueCompletion(t *testing.T) {
	runtime := runtimeAtBoundConfirmation(t)
	barrierAt := runtime.grantedConfirmationIndex()
	grant := runtime.admissions[barrierAt].grant
	runtime, started := runtime.startCommitted(grant)
	runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(started.generation, automaticDeadlineTrip())
	runtime, rejected := runtime.requestAdmission(admissionRequest{
		campaign: grant.campaign, attempt: "primary-before-completion", class: sharedAdmission,
	})
	assert.Equal(t, admissionRejectedGateClosed, rejected.decision, "primary admission before queue completion = %#v", rejected)

	runtime, completed := runtime.completeConfirmationQueue(grant.campaign)
	assert.Equal(t, confirmationQueueCompleted, completed.decision, "confirmation queue completion = %#v", completed)
	runtime, admitted := runtime.requestAdmission(admissionRequest{
		campaign: grant.campaign, attempt: "primary-after-completion", class: sharedAdmission,
	})
	assert.Equal(t, admissionAccepted, admitted.decision, "primary admission after queue completion = %#v", admitted)
	assert.EqualValues(t, 1, len(admitted.deliveries), "primary admission after queue completion = %#v", admitted)
}

func TestProcessRuntimeClosedEmergencyStateCannotBeRewritten(t *testing.T) {
	runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, true)
	runtime, _ = runtime.closeRuntime(runtimeFatalCause("fatal test"))
	runtime, _ = runtime.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
		generation: generation, disposition: emergencyCustodyTransferred,
	}}})
	assert.Equal(t, runtimeClosedUnconfirmed, runtime.lifecycle, "first sweep lifecycle=%v", runtime.lifecycle)
	assertInvariantViolation(t, func() {
		runtime.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
			generation: generation, disposition: emergencyConfirmedDrained,
		}}})
	})
}

func TestProcessRuntimeDrainUnconfirmedRemainsFatalClosingUntilSweep(t *testing.T) {
	runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, true)
	runtime, _ = runtime.observeAttempt(generation, drainUnconfirmed{})
	assert.Equal(t, runtimeFatalClosing, runtime.lifecycle, "drain-unconfirmed finalized before sweep: %#v", runtime)
	runtime, _ = runtime.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
		generation: generation, disposition: emergencyCustodyTransferred,
	}}})
	assert.Equal(t, runtimeClosedUnconfirmed, runtime.lifecycle, "transferred sweep lifecycle=%v", runtime.lifecycle)
}

func TestProcessRuntimePrimaryGateAppliesToSharedAndSerialButNotConfirmation(t *testing.T) {
	runtime := runtimeAtBoundConfirmation(t)
	campaign := runtime.admissions[runtime.grantedConfirmationIndex()].grant.campaign
	for _, class := range []admissionClass{sharedAdmission, serialPrimaryAdmission} {
		_, result := runtime.requestAdmission(admissionRequest{campaign: campaign, attempt: "blocked", class: class})
		assert.Equal(t, admissionRejectedGateClosed, result.decision, "class %v gate result=%#v", class, result)
	}
	_, result := runtime.requestAdmission(admissionRequest{
		campaign: campaign, attempt: "follow-on", class: confirmationAdmission,
		profile: AutomaticProfile, deadline: 31 * time.Second,
	})
	assert.Equal(t, admissionRejectedExclusiveOutstanding, result.decision, "confirmation cardinality result=%#v", result)
}

func TestProcessRuntimeAttemptIdentityCannotBeReusedWithAnotherReturnAddress(t *testing.T) {
	runtime := newProcessRuntime(1)
	runtime, campaign := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, _ = runtime.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "same", class: sharedAdmission, delivery: make(chan admissionGrant, 1),
	})
	_, duplicate := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "same", class: sharedAdmission, delivery: make(chan admissionGrant, 1),
	})
	assert.Equal(t, admissionRejectedDuplicate, duplicate.decision, "duplicate identity decision=%#v", duplicate)
}

func TestProcessRuntimeFatalCausesRetainEveryIngressInOrder(t *testing.T) {
	runtime := newProcessRuntime(1)
	runtime, _ = runtime.closeRuntime(runtimeFatalCause("first"))
	runtime, _ = runtime.closeRuntime(runtimeFatalCause("second"))
	runtime, _ = runtime.closeRuntime(runtimeFatalCause("first"))
	assert.Equal(t, []runtimeFatalCause{"first", "second", "first"}, runtime.fatalCauses, "joined causes=%#v", runtime.fatalCauses)
}

func TestProcessRuntimeLaterAttemptFatalSeedsRetainGenerationProvenance(t *testing.T) {
	for _, owned := range []bool{false, true} {
		runtime := newProcessRuntime(2)
		runtime, campaign := runtime.registerCampaign(campaignProvenance{lineage: 11})
		generations := make([]attemptGeneration, 0, 2)
		for _, attempt := range []attemptIdentity{"a", "b"} {
			var requested admissionResult
			runtime, requested = runtime.requestAdmission(admissionRequest{
				campaign: campaign.token, attempt: attempt, class: sharedAdmission,
			})
			var started startCommittedResult
			runtime, started = runtime.startCommitted(requested.deliveries[0])
			generations = append(generations, started.generation)
			if owned {
				runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
			}
		}
		kind := "launch unconfirmed"
		for _, generation := range generations {
			observation := attemptObservation(launchUnconfirmed{})
			if owned {
				kind = "drain unconfirmed"
				observation = drainUnconfirmed{}
			}
			runtime, _ = runtime.observeAttempt(generation, observation)
			unchanged := runtime
			assertInvariantViolation(t, func() { runtime.observeAttempt(generation, observation) })
			assert.Equal(t, unchanged, runtime, "owned=%t duplicate seed changed state: %#v", owned, runtime)
		}
		want := []runtimeFatalCause{
			runtimeFatalCause(fmt.Sprintf("%s generation=%d", kind, generations[0])),
			runtimeFatalCause(fmt.Sprintf("%s generation=%d", kind, generations[1])),
		}
		assert.Equal(t, want, runtime.fatalCauses, "owned=%t fatal causes=%#v, want %#v", owned, runtime.fatalCauses, want)
	}
}

func TestProcessRuntimeLateOwnedLaunchCanJoinDrainSeed(t *testing.T) {
	runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, false)
	runtime, _ = runtime.observeAttempt(generation, launchUnconfirmed{})
	runtime, _ = runtime.observeAttempt(generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(generation, drainUnconfirmed{})
	wantCauses := []runtimeFatalCause{
		runtimeFatalCause(fmt.Sprintf("launch unconfirmed generation=%d", generation)),
		runtimeFatalCause(fmt.Sprintf("drain unconfirmed generation=%d", generation)),
	}
	wantResidual := []residualCustody{{
		generation: generation, attempt: "attempt", stage: admissionOwned, transferred: true,
	}}
	assert.Equal(t, wantCauses, runtime.fatalCauses, "late-owned drain state=%#v, want causes/residual %#v/%#v", runtime, wantCauses, wantResidual)
	assert.Equal(t, wantResidual, runtime.residualCustody(), "late-owned drain state=%#v, want causes/residual %#v/%#v", runtime, wantCauses, wantResidual)
}

func TestProcessRuntimeNoReleaseCannotDeleteSettledEmergencyCustody(t *testing.T) {
	runtime := newProcessRuntime(2)
	runtime, campaignA := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, campaignB := runtime.registerCampaign(campaignProvenance{lineage: 22})
	runtime, prospective := runtime.requestAdmission(admissionRequest{
		campaign: campaignA.token, attempt: "prospective", class: sharedAdmission,
	})
	runtime, pending := runtime.requestAdmission(admissionRequest{
		campaign: campaignB.token, attempt: "pending return", class: sharedAdmission,
	})
	runtime, started := runtime.startCommitted(prospective.deliveries[0])
	runtime, _ = runtime.closeRuntime(runtimeFatalCause("fatal test"))
	runtime, _ = runtime.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
		generation: started.generation, disposition: emergencyCustodyTransferred,
	}}})
	assert.Equal(t, runtimeFatalSettledClosing, runtime.lifecycle, "pending return did not hold fatal closing: %#v", runtime)
	unchanged := runtime
	assertInvariantViolation(t, func() {
		runtime.observeAttempt(started.generation, launchNotReleased{reason: launchFailed})
	})
	assert.Equal(t, unchanged, runtime, "late no-release deleted transferred custody: %#v", runtime)
	runtime, returned := runtime.acknowledgeGrantReturn(pending.deliveries[0])
	want := []residualCustody{{
		generation: started.generation, attempt: "prospective", stage: admissionProspective, transferred: true,
	}}
	assert.Equal(t, admissionReturnedAfterClosure, returned.decision, "return/final custody=%#v/%#v, want %#v", returned, runtime, want)
	assert.Equal(t, runtimeClosedUnconfirmed, runtime.lifecycle, "return/final custody=%#v/%#v, want %#v", returned, runtime, want)
	assert.Equal(t, want, runtime.residualCustody(), "return/final custody=%#v/%#v, want %#v", returned, runtime, want)
}

func TestProcessRuntimeLateProvenNoReleaseWaitsForExactEmptySettlement(t *testing.T) {
	runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, false)
	runtime, _ = runtime.observeAttempt(generation, launchUnconfirmed{})
	runtime, noRelease := runtime.observeAttempt(generation, launchNotReleased{reason: launchFailed})
	wantNoRelease := observationResult{
		generation: generation, settlementAcknowledged: true, runtimeClosureInProgress: true,
	}
	assert.Equal(t, wantNoRelease, noRelease, "late no-release settlement/state=%#v/%#v", noRelease, runtime)
	assert.Equal(t, runtimeFatalClosing, runtime.lifecycle, "late no-release settlement/state=%#v/%#v", noRelease, runtime)
	assert.False(t, runtime.admissionIndexByGeneration(generation) >= 0, "late no-release settlement/state=%#v/%#v", noRelease, runtime)
	assert.EqualValues(t, 0, len(runtime.admissions), "late no-release settlement/state=%#v/%#v", noRelease, runtime)
	assert.EqualValues(t, 0, len(runtime.residualCustody()), "late no-release settlement/state=%#v/%#v", noRelease, runtime)
	runtime, settled := runtime.settleEmergency(emergencySweep{})
	assert.Equal(t, runtimeClosedDrained, runtime.lifecycle, "empty emergency settlement/state=%#v/%#v", settled, runtime)
	assert.EqualValues(t, 0, len(settled.acknowledged), "empty emergency settlement/state=%#v/%#v", settled, runtime)
	assert.EqualValues(t, 0, len(settled.residual), "empty emergency settlement/state=%#v/%#v", settled, runtime)
	unchanged := runtime.clone()
	assertInvariantViolation(t, func() { runtime.settleEmergency(emergencySweep{}) })
	assert.Equal(t, unchanged, runtime, "duplicate closed empty settlement changed state: %#v", runtime)
}

func TestProcessRuntimeEmptySettlementLinearizesAgainstKnownGrantReturn(t *testing.T) {
	for _, returnFirst := range []bool{true, false} {
		name := "settlement before return"
		if returnFirst {
			name = "return before settlement"
		}
		t.Run(name, func(t *testing.T) {
			runtime, granted := fatalProspectiveAwaitingGrantReturn(t)
			var returned admissionResult
			var settled emergencySettlement
			if returnFirst {
				runtime, returned = runtime.acknowledgeGrantReturn(granted.deliveries[0])
				assert.Equal(t, runtimeFatalClosing, runtime.lifecycle, "grant return finalized unsettled empty epoch: %#v", runtime)
				runtime, settled = runtime.settleEmergency(emergencySweep{})
			} else {
				beforeSettlement := runtime
				runtime, settled = runtime.settleEmergency(emergencySweep{})
				wantSettled := beforeSettlement.clone()
				wantSettled.lifecycle = runtimeFatalSettledClosing
				assert.Equal(t, wantSettled, runtime, "empty settlement state = %#v, want %#v", runtime, wantSettled)
				beforeRepeatedFatal := runtime.clone()
				laterCause := runtimeFatalCause("later fatal ingress")
				var repeatedFatal runtimeClosure
				runtime, repeatedFatal = runtime.closeRuntime(laterCause)
				wantRepeatedFatal := beforeRepeatedFatal.clone()
				wantRepeatedFatal.fatalCauses = append(wantRepeatedFatal.fatalCauses, laterCause)
				assert.Equal(t, wantRepeatedFatal, runtime, "repeated fatal reset settled closing: state=%#v closure=%#v want=%#v", runtime, repeatedFatal, wantRepeatedFatal)
				assert.Equal(t, runtimeFatalSettledClosing, runtime.lifecycle, "repeated fatal reset settled closing: state=%#v closure=%#v want=%#v", runtime, repeatedFatal, wantRepeatedFatal)
				assert.Equal(t, beforeRepeatedFatal.admissions, runtime.admissions, "repeated fatal reset settled closing: state=%#v closure=%#v want=%#v", runtime, repeatedFatal, wantRepeatedFatal)
				assert.EqualValues(t, 0, len(runtime.residualCustody()), "repeated fatal reset settled closing: state=%#v closure=%#v want=%#v", runtime, repeatedFatal, wantRepeatedFatal)
				assert.EqualValues(t, 0, len(repeatedFatal.residual), "repeated fatal reset settled closing: state=%#v closure=%#v want=%#v", runtime, repeatedFatal, wantRepeatedFatal)
				assert.EqualValues(t, 0, len(repeatedFatal.cancelledWaiting), "repeated fatal reset settled closing: state=%#v closure=%#v want=%#v", runtime, repeatedFatal, wantRepeatedFatal)
				assert.EqualValues(t, 0, len(repeatedFatal.compensatedGrants), "repeated fatal reset settled closing: state=%#v closure=%#v want=%#v", runtime, repeatedFatal, wantRepeatedFatal)
				unchanged := runtime
				assertInvariantViolation(t, func() {
					runtime.settleEmergency(emergencySweep{})
				})
				assert.Equal(t, unchanged, runtime, "duplicate empty settlement changed state: %#v", runtime)
				runtime, returned = runtime.acknowledgeGrantReturn(granted.deliveries[0])
			}
			assert.Equal(t, admissionReturnedAfterClosure, returned.decision, "empty settlement/return final state=%#v/%#v/%#v", settled, returned, runtime)
			assert.Equal(t, runtimeClosedDrained, runtime.lifecycle, "empty settlement/return final state=%#v/%#v/%#v", settled, returned, runtime)
			assert.EqualValues(t, 0, len(runtime.residualCustody()), "empty settlement/return final state=%#v/%#v/%#v", settled, returned, runtime)
			assert.EqualValues(t, 0, len(settled.acknowledged), "empty settlement/return final state=%#v/%#v/%#v", settled, returned, runtime)
			assert.EqualValues(t, 0, len(settled.residual), "empty settlement/return final state=%#v/%#v/%#v", settled, returned, runtime)
		})
	}
}

func fatalProspectiveAwaitingGrantReturn(
	t *testing.T,
) (processRuntime, admissionResult) {
	t.Helper()
	runtime := newProcessRuntime(2)
	runtime, campaignA := runtime.registerCampaign(campaignProvenance{lineage: 31})
	runtime, campaignB := runtime.registerCampaign(campaignProvenance{lineage: 32})
	runtime, prospective := runtime.requestAdmission(admissionRequest{
		campaign: campaignA.token, attempt: "prospective", class: sharedAdmission,
	})
	runtime, granted := runtime.requestAdmission(admissionRequest{
		campaign: campaignB.token, attempt: "return pending", class: sharedAdmission,
	})
	runtime, started := runtime.startCommitted(prospective.deliveries[0])
	runtime, closed := runtime.closeRuntime(runtimeFatalCause("fatal empty epoch"))
	assert.Equal(t, []admissionRequestToken{granted.request}, closed.compensatedGrants, "empty epoch compensation=%#v", closed)
	runtime, noRelease := runtime.observeAttempt(
		started.generation,
		launchNotReleased{reason: launchFailed},
	)
	wantNoRelease := observationResult{
		generation: started.generation, settlementAcknowledged: true,
		runtimeClosureInProgress: true,
	}
	assert.Equal(t, wantNoRelease, noRelease, "empty epoch no-release state=%#v/%#v", noRelease, runtime)
	assert.Equal(t, runtimeFatalClosing, runtime.lifecycle, "empty epoch no-release state=%#v/%#v", noRelease, runtime)
	assert.False(t, runtime.admissionIndexByGeneration(started.generation) >= 0, "empty epoch no-release state=%#v/%#v", noRelease, runtime)
	require.Len(t, runtime.admissions, 1, "empty epoch no-release state=%#v/%#v", noRelease, runtime)
	assert.Equal(t, granted.deliveries[0], runtime.admissions[0].grant, "empty epoch no-release state=%#v/%#v", noRelease, runtime)
	assert.Equal(t, admissionGranted, runtime.admissions[0].stage, "empty epoch no-release state=%#v/%#v", noRelease, runtime)
	assert.Equal(t, dispositionReturnedAfterClosure, runtime.admissions[0].disposition, "empty epoch no-release state=%#v/%#v", noRelease, runtime)
	assert.EqualValues(t, 0, len(runtime.residualCustody()), "empty epoch no-release state=%#v/%#v", noRelease, runtime)

	return runtime, granted
}

func (r processRuntime) grantedConfirmationIndex() int {
	for index, admission := range r.admissions {
		if admission.grant.class == confirmationBarrierAdmission && admission.stage == admissionGranted {
			return index
		}
	}

	return -1
}

func runtimeWithOwnedOrProspectiveAttempt(
	t *testing.T,
	class admissionClass,
	owned bool,
) (processRuntime, attemptGeneration) {
	t.Helper()
	runtime := newProcessRuntime(1)
	runtime, campaign := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, requested := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "attempt",
		class:    class,
	})
	runtime, started := runtime.startCommitted(requested.deliveries[0])
	if owned {
		runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
	}

	return runtime, started.generation
}

func runtimeAtBoundConfirmation(t *testing.T) processRuntime {
	t.Helper()
	runtime := newProcessRuntime(2)
	runtime, campaignA := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, campaignB := runtime.registerCampaign(campaignProvenance{lineage: 22})
	runtime, admittedA := runtime.requestAdmission(admissionRequest{
		campaign: campaignA.token, attempt: "a", class: sharedAdmission,
	})
	runtime, admittedB := runtime.requestAdmission(admissionRequest{
		campaign: campaignB.token, attempt: "b", class: sharedAdmission,
	})
	runtime, startedA := runtime.startCommitted(admittedA.deliveries[0])
	runtime, startedB := runtime.startCommitted(admittedB.deliveries[0])
	runtime, _ = runtime.observeAttempt(startedA.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(startedB.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(startedA.generation, automaticDeadlineTrip())
	runtime, _ = runtime.observeAttempt(startedB.generation, attemptSettled{})
	runtime, bound := runtime.sealAndBindConfirmationBarrier(barrierBinding{
		campaign: campaignA.token,
		attempt:  confirmationAttempt,
		profile:  AutomaticProfile, deadline: 31 * time.Second,
	})
	assert.Equal(t, barrierBound, bound.decision, "bound confirmation setup=%#v/%#v", bound, runtime)
	assert.EqualValues(t, 1, len(bound.deliveries), "bound confirmation setup=%#v/%#v", bound, runtime)

	return runtime
}
