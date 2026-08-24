//nolint:exhaustruct,lll // Pure reducer traces deliberately omit shell-only fields.
package ooze

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

const confirmationAttempt = "confirm-a"

func settledConfirmation(grant admissionGrant) attemptSettled {
	return attemptSettled{profile: grant.profile, deadline: grant.deadline}
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
	runtime, _ = runtime.observeAttempt(startedA.generation, attemptTripped{kind: deadlineTrip})

	campaignAt := runtime.campaignIndex(campaignA.token)
	if campaignAt < 0 || runtime.campaigns[campaignAt].primaryGateOpen {
		t.Fatalf("campaign gate/deadline state=%#v", runtime)
	}
	barrierAt := runtime.unboundBarrierIndex(campaignA.token)
	if barrierAt < 0 {
		t.Fatalf("overlap deadline did not install a barrier: %#v", runtime)
	}

	runtime, rejectedStart := runtime.startCommitted(admittedA2.deliveries[0])
	returnedAt := runtime.admissionIndex(admittedA2.request)
	if rejectedStart.decision != startCommittedRejectedGrant || returnedAt < 0 ||
		runtime.admissions[returnedAt].disposition != dispositionReturnedAfterGate {
		t.Fatalf("post-closure start result/state=%#v/%#v", rejectedStart, runtime)
	}
	runtime, returned := runtime.acknowledgeGrantReturn(admittedA2.deliveries[0])
	if returned.decision != admissionReturnedAfterGateClosure {
		t.Fatalf("late gate-closed grant return=%#v", returned)
	}
	runtime, gateRejected := runtime.requestAdmission(admissionRequest{
		campaign: campaignA.token,
		attempt:  "a3",
		class:    sharedAdmission,
	})
	if gateRejected.decision != admissionRejectedGateClosed {
		t.Fatalf("post-closure shared admission=%#v", gateRejected)
	}
	runtime, later := runtime.requestAdmission(admissionRequest{
		campaign: campaignC.token,
		attempt:  "c1",
		class:    sharedAdmission,
	})
	if len(later.deliveries) != 0 {
		t.Fatalf("later shared request passed unbound barrier: %#v", later)
	}

	runtime, bound := runtime.sealAndBindConfirmationBarrier(barrierBinding{
		campaign: campaignA.token,
		attempt:  confirmationAttempt,
		profile:  AutomaticProfile, deadline: 31 * time.Second,
	})
	if bound.decision != barrierBound || len(bound.deliveries) != 0 {
		t.Fatalf("barrier bind=%#v", bound)
	}
	runtime, settledB := runtime.observeAttempt(startedB.generation, attemptSettled{})
	if len(settledB.deliveries) != 1 || settledB.deliveries[0].class != confirmationBarrierAdmission {
		t.Fatalf("barrier did not grant at global zero: %#v", settledB)
	}
	runtime, confirmation := runtime.startCommitted(settledB.deliveries[0])
	runtime, _ = runtime.observeAttempt(confirmation.generation, launchOwned{})
	runtime, confirmationSettled := runtime.observeAttempt(
		confirmation.generation,
		settledConfirmation(settledB.deliveries[0]),
	)
	if !reflect.DeepEqual(confirmationSettled.deliveries, []admissionGrant{later.request}) {
		t.Fatalf("post-confirmation FIFO deliveries=%#v", confirmationSettled.deliveries)
	}
	_, completed := runtime.completeConfirmationQueue(campaignA.token)
	if len(completed.deliveries) != 0 {
		t.Fatalf("queue completion duplicated FIFO deliveries=%#v", completed.deliveries)
	}
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
	runtime, _ = runtime.observeAttempt(startedA1.generation, attemptTripped{kind: deadlineTrip})

	unchanged := runtime
	runtime, premature := runtime.sealAndBindConfirmationBarrier(barrierBinding{
		campaign: campaignA.token,
		attempt:  confirmationAttempt,
		profile:  AutomaticProfile, deadline: 31 * time.Second,
	})
	if premature.decision != barrierRejectedClosureOutstanding || !reflect.DeepEqual(runtime, unchanged) {
		t.Fatalf("premature bind result/state=%#v/%#v", premature, runtime)
	}
	runtime, _ = runtime.observeAttempt(startedA2.generation, attemptSettled{})
	_, bound := runtime.sealAndBindConfirmationBarrier(barrierBinding{
		campaign: campaignA.token,
		attempt:  confirmationAttempt,
		profile:  AutomaticProfile, deadline: 31 * time.Second,
	})
	if bound.decision != barrierBound {
		t.Fatalf("barrier was not bound after closure set settled: %#v", bound)
	}
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
	if runtime.mode != singleAdmission || len(exhausted.deliveries) != 0 {
		t.Fatalf("pressure transition/state=%#v/%#v", exhausted, runtime)
	}
	runtime, queued := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "a4",
		class:    sharedAdmission,
	})
	if len(queued.deliveries) != 0 {
		t.Fatalf("single admission granted while overcommitted: %#v", queued)
	}
	runtime, firstSettlement := runtime.observeAttempt(starts[0].generation, attemptSettled{})
	if len(firstSettlement.deliveries) != 0 {
		t.Fatalf("single admission granted at one live obligation: %#v", firstSettlement)
	}
	_, secondSettlement := runtime.observeAttempt(starts[1].generation, attemptSettled{})
	if !reflect.DeepEqual(secondSettlement.deliveries, []admissionGrant{queued.request}) {
		t.Fatalf("single admission did not grant at zero: %#v", secondSettlement)
	}
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
	if runtime.admissions[runtime.admissionIndexByGeneration(startedA.generation)].overlapped {
		t.Fatal("granted-but-uncommitted work counted as overlap")
	}
	runtime, cancelled := runtime.cancelAdmission(admittedB.request)
	runtime, admittedC := runtime.requestAdmission(admissionRequest{
		campaign: campaignC.token, attempt: "c", class: sharedAdmission,
	})
	if len(cancelled.deliveries) != 0 || len(admittedC.deliveries) != 1 {
		t.Fatalf("unexpected cancellation/request deliveries: %#v/%#v", cancelled, admittedC)
	}
	runtime, startedC := runtime.startCommitted(admittedC.deliveries[0])
	runtime, _ = runtime.observeAttempt(startedC.generation, launchNotReleased{reason: launchFailed})
	indexA := runtime.admissionIndexByGeneration(startedA.generation)
	if indexA < 0 || !runtime.admissions[indexA].overlapped {
		t.Fatalf("overlap was not retained after peer settlement: %#v", runtime)
	}
	runtime, _ = runtime.observeAttempt(startedA.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(startedA.generation, attemptTripped{kind: deadlineTrip})
	if runtime.unboundBarrierIndex(campaignA.token) < 0 {
		t.Fatalf("latched overlap did not attribute deadline: %#v", runtime)
	}
}

func TestProcessRuntimeConfirmationSettlementAuthorizesSingleAdmission(t *testing.T) {
	runtime := runtimeAtBoundConfirmation(t)
	barrierAt := -1
	for index, admission := range runtime.admissions {
		if admission.grant.class == confirmationBarrierAdmission && admission.stage == admissionGranted {
			barrierAt = index
		}
	}
	if barrierAt < 0 || runtime.mode != fullAutomatic {
		t.Fatalf("confirmation setup=%#v", runtime)
	}
	grant := runtime.admissions[barrierAt].grant
	runtime, started := runtime.startCommitted(grant)
	runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
	runtime, result := runtime.observeAttempt(started.generation, settledConfirmation(grant))
	if runtime.mode != singleAdmission {
		t.Fatalf("confirmation did not authorize pressure transition: %#v", runtime)
	}
	if !result.pressureTransitioned || runtime.campaigns[runtime.campaignIndex(grant.campaign)].primaryGateOpen {
		t.Fatalf("confirmation transition reopened gate early: %#v/%#v", result, runtime)
	}
	runtime, completed := runtime.completeConfirmationQueue(grant.campaign)
	if completed.decision != confirmationQueueCompleted ||
		!runtime.campaigns[runtime.campaignIndex(grant.campaign)].primaryGateOpen {
		t.Fatalf("confirmation queue completion/state = %#v/%#v", completed, runtime)
	}
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
	if !firstResult.pressureTransitioned || runtime.mode != singleAdmission || runtime.campaigns[campaignAt].primaryGateOpen {
		t.Fatalf("continuing confirmation pressure/gate=%#v/%#v", firstResult, runtime)
	}
	runtime, requested := runtime.requestAdmission(admissionRequest{
		campaign: firstGrant.campaign, attempt: "next-confirmation", class: confirmationAdmission, profile: AutomaticProfile, deadline: 31 * time.Second,
	})
	if requested.decision != admissionAccepted || len(requested.deliveries) != 1 {
		t.Fatalf("next confirmation admission=%#v", requested)
	}
	runtime, second := runtime.startCommitted(requested.deliveries[0])
	runtime, _ = runtime.observeAttempt(second.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(second.generation, attemptTripped{kind: deadlineTrip})
	if runtime.campaigns[campaignAt].primaryGateOpen {
		t.Fatalf("repeated continuing confirmation reopened gate: %#v", runtime)
	}
	runtime, requested = runtime.requestAdmission(admissionRequest{
		campaign: firstGrant.campaign, attempt: "last-confirmation", class: confirmationAdmission, profile: AutomaticProfile, deadline: 31 * time.Second,
	})
	if requested.decision != admissionAccepted || len(requested.deliveries) != 1 {
		t.Fatalf("last confirmation admission=%#v", requested)
	}
	runtime, last := runtime.startCommitted(requested.deliveries[0])
	runtime, _ = runtime.observeAttempt(last.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(last.generation, attemptTripped{kind: deadlineTrip})
	runtime, completed := runtime.completeConfirmationQueue(firstGrant.campaign)
	if completed.decision != confirmationQueueCompleted {
		t.Fatalf("drained confirmation queue completion=%#v", completed)
	}
	if !runtime.campaigns[campaignAt].primaryGateOpen {
		t.Fatalf("drained confirmation queue did not reopen gate: %#v", runtime)
	}
	for _, admission := range runtime.admissions {
		if admission.grant.campaign == firstGrant.campaign && admission.grant.class.exclusive() {
			t.Fatalf("gate reopened with outstanding confirmation: %#v", runtime)
		}
	}
}

func TestProcessRuntimeRejectsConfirmationBeforePrimaryGateCloses(t *testing.T) {
	runtime := newProcessRuntime(1)
	runtime, campaign := runtime.registerCampaign(campaignProvenance{lineage: 11})
	unchanged := runtime
	runtime, result := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "early-confirmation", class: confirmationAdmission, profile: AutomaticProfile, deadline: 31 * time.Second,
	})
	if result.decision != admissionRejectedGateOpen || !reflect.DeepEqual(runtime, unchanged) {
		t.Fatalf("early confirmation result/state=%#v/%#v", result, runtime)
	}
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
	if runtime.lifecycle != runtimeFatalClosing {
		t.Fatalf("closure result/state=%#v/%#v", closed, runtime)
	}
	if !reflect.DeepEqual(closed.compensatedGrants, []admissionRequestToken{requests[2].request}) ||
		!reflect.DeepEqual(closed.cancelledWaiting, []admissionRequestToken{requests[3].request}) {
		t.Fatalf("closure uncommitted outputs=%#v", closed)
	}
	wantResidual := []residualCustody{
		{generation: owned.generation, stage: admissionOwned, transferred: false},
		{generation: prospective.generation, stage: admissionProspective, transferred: false},
	}
	if got := runtime.residualCustody(); !reflect.DeepEqual(got, wantResidual) {
		t.Fatalf("residual=%#v, want %#v", got, wantResidual)
	}
	runtime, rejected := runtime.startCommitted(requests[2].deliveries[0])
	if rejected.decision != startCommittedRejectedClosed {
		t.Fatalf("known grant start after close=%#v", rejected)
	}
	runtime, returned := runtime.acknowledgeGrantReturn(requests[2].deliveries[0])
	if returned.decision != admissionReturnedAfterClosure {
		t.Fatalf("late known grant return=%#v", returned)
	}

	runtime, swept := runtime.settleEmergency(emergencySweep{
		resolutions: []emergencyResolution{
			{generation: prospective.generation, disposition: emergencyCustodyTransferred},
			{generation: owned.generation, disposition: emergencyConfirmedDrained},
		},
	})
	if !reflect.DeepEqual(swept.acknowledged, []attemptGeneration{prospective.generation, owned.generation}) {
		t.Fatalf("sweep acknowledgements=%#v", swept)
	}
	wantResidual = []residualCustody{{
		generation:  prospective.generation,
		stage:       admissionProspective,
		transferred: true,
	}}
	if runtime.lifecycle != runtimeClosedUnconfirmed || !reflect.DeepEqual(runtime.residualCustody(), wantResidual) {
		t.Fatalf("post-sweep state=%#v residual=%#v", runtime, runtime.residualCustody())
	}
}

func TestProcessRuntimeFatalClosingDefersEachValidOwnedTerminal(t *testing.T) {
	for _, test := range []struct {
		name        string
		observation attemptObservation
	}{
		{name: "settled", observation: attemptSettled{}},
		{name: "fuse trip", observation: attemptTripped{kind: fuseTrip}},
		{name: "deadline trip", observation: attemptTripped{kind: deadlineTrip}},
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
			if !reflect.DeepEqual(runtime, unchanged) {
				t.Fatalf("malformed terminal changed state: %#v", runtime)
			}
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
			if !reflect.DeepEqual(runtime, unchanged) {
				t.Fatalf("duplicate terminal changed state: %#v", runtime)
			}
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
				if index < 0 || runtime.lifecycle != runtimeFatalClosing ||
					runtime.admissions[index].stage != admissionOwned ||
					runtime.admissions[index].disposition != dispositionFatalSeeded {
					t.Fatalf("late adopted fatal custody setup = %#v", runtime)
				}

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
			if !reflect.DeepEqual(runtime, unchanged) {
				t.Fatalf("invalid custody terminal changed state: %#v", runtime)
			}
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
		if deferred.settlementAcknowledged || !deferred.runtimeClosureInProgress ||
			!reflect.DeepEqual(settled.acknowledged, []attemptGeneration{generation}) ||
			len(settled.residual) != 0 || runtime.lifecycle != runtimeClosedDrained {
			t.Fatalf("deferred/emergency settlement = %#v/%#v/%#v", deferred, settled, runtime)
		}
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
		if !reflect.DeepEqual(runtime, unchanged) {
			t.Fatalf("deferred transfer changed state: %#v", runtime)
		}
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
	if !reflect.DeepEqual(closed.compensatedGrants, []admissionRequestToken{granted.request}) {
		t.Fatalf("closure compensation=%#v", closed)
	}
	runtime, deferred := runtime.observeAttempt(started.generation, attemptSettled{})
	assertDeferredOwnedTerminal(t, runtime, started.generation, deferred)

	runtime, settled := runtime.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
		generation: started.generation, disposition: emergencyConfirmedDrained,
	}}})
	if runtime.lifecycle != runtimeFatalSettledClosing ||
		!reflect.DeepEqual(settled.acknowledged, []attemptGeneration{started.generation}) ||
		len(settled.residual) != 0 {
		t.Fatalf("settlement finalized before grant return: %#v/%#v", runtime, settled)
	}

	runtime, returned := runtime.acknowledgeGrantReturn(granted.deliveries[0])
	if returned.decision != admissionReturnedAfterClosure || runtime.lifecycle != runtimeClosedDrained {
		t.Fatalf("grant return did not finalize drained closure: %#v/%#v", returned, runtime)
	}
	unchanged := runtime
	assertInvariantViolation(t, func() { runtime.acknowledgeGrantReturn(granted.deliveries[0]) })
	if !reflect.DeepEqual(runtime, unchanged) {
		t.Fatalf("duplicate return changed closed runtime: %#v", runtime)
	}
}

func assertDeferredOwnedTerminal(
	t *testing.T,
	runtime processRuntime,
	generation attemptGeneration,
	result observationResult,
) {
	t.Helper()
	index := runtime.admissionIndexByGeneration(generation)
	if index < 0 || runtime.admissions[index].disposition != dispositionTerminalDeferred {
		t.Fatalf("deferred terminal state = %#v", runtime)
	}
	want := observationResult{generation: generation, runtimeClosureInProgress: true}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("deferred terminal receipt = %#v, want %#v", result, want)
	}
}

func fatalClosingRuntimeWithOwnedAttempt(t *testing.T) (processRuntime, attemptGeneration) {
	t.Helper()
	runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, true)
	runtime, _ = runtime.closeRuntime(runtimeFatalCause("fatal test"))
	if runtime.lifecycle != runtimeFatalClosing {
		t.Fatalf("owned setup did not enter fatal closing: %#v", runtime)
	}

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
	if runtime.lifecycle != runtimeFatalSettledClosing || len(blocker.deliveries) != 1 || index < 0 ||
		runtime.admissions[index].stage != admissionOwned ||
		runtime.admissions[index].disposition != dispositionCustodySettled {
		t.Fatalf("settled custody setup = %#v/%#v", runtime, blocker)
	}

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

	if !result.runtimeClosureInProgress || len(result.deliveries) != 0 || runtime.admissionIndex(waiting.request) >= 0 {
		t.Fatalf("drain-unconfirmed result/state=%#v/%#v", result, runtime)
	}
	want := []residualCustody{{generation: started.generation, stage: admissionOwned, transferred: true}}
	if !reflect.DeepEqual(runtime.residualCustody(), want) {
		t.Fatalf("drain-unconfirmed residual=%#v, want %#v", runtime.residualCustody(), want)
	}
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
			if cancelled.decision != admissionRejectedAlreadyCommitted || !reflect.DeepEqual(runtime, unchanged) {
				t.Fatalf("cancel committed result/state=%#v/%#v", cancelled, runtime)
			}
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
	if cancelled.decision != admissionRejectedAlreadyCommitted || !reflect.DeepEqual(runtime, unchanged) {
		t.Fatalf("closed-unconfirmed cancellation result/state=%#v/%#v", cancelled, runtime)
	}
}

func TestProcessRuntimeTerminalCommitRequiresNoOutstandingCustody(t *testing.T) {
	runtime := newProcessRuntime(1)
	runtime, campaign := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, admitted := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "a", class: sharedAdmission,
	})
	unchanged := runtime
	runtime, rejected := runtime.commitTerminal(campaign.token)
	if rejected.decision != terminalRejectedOutstanding || !reflect.DeepEqual(runtime, unchanged) {
		t.Fatalf("premature terminal=%#v/%#v", rejected, runtime)
	}
	runtime, _ = runtime.cancelAdmission(admitted.request)
	runtime, accepted := runtime.commitTerminal(campaign.token)
	if accepted.decision != terminalCommitted || runtime.hasCampaign(campaign.token) {
		t.Fatalf("terminal commit=%#v/%#v", accepted, runtime)
	}
}

func TestProcessRuntimePressureAndOverlapExclusions(t *testing.T) {
	t.Run("a lone hard exhaustion qualifies", func(t *testing.T) {
		runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, false)
		runtime, _ = runtime.observeAttempt(generation, launchNotReleased{reason: launchResourceExhausted})
		if runtime.mode != singleAdmission {
			t.Fatalf("hard exhaustion mode=%v", runtime.mode)
		}
	})

	t.Run("launch failure does not qualify", func(t *testing.T) {
		runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, false)
		runtime, _ = runtime.observeAttempt(generation, launchNotReleased{reason: launchFailed})
		if runtime.mode != fullAutomatic {
			t.Fatalf("launch failure mode=%v", runtime.mode)
		}
	})

	t.Run("baseline exclusive settlement does not qualify", func(t *testing.T) {
		runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, exclusiveAdmission, true)
		runtime, _ = runtime.observeAttempt(generation, attemptSettled{})
		if runtime.mode != fullAutomatic {
			t.Fatalf("baseline exclusive mode=%v", runtime.mode)
		}
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
		runtime, _ = runtime.observeAttempt(started.generation, attemptTripped{kind: deadlineTrip})
		campaignAt := runtime.campaignIndex(campaignA.token)
		if !runtime.campaigns[campaignAt].primaryGateOpen || runtime.unboundBarrierIndex(campaignA.token) >= 0 {
			t.Fatalf("unattributed deadline state=%#v", runtime)
		}
	})

	t.Run("confirmation deadline does not qualify", func(t *testing.T) {
		runtime := runtimeAtBoundConfirmation(t)
		barrierAt := runtime.grantedConfirmationIndex()
		grant := runtime.admissions[barrierAt].grant
		runtime, started := runtime.startCommitted(grant)
		runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
		runtime, _ = runtime.observeAttempt(started.generation, attemptTripped{kind: deadlineTrip})
		if runtime.mode != fullAutomatic {
			t.Fatalf("confirmation deadline mode=%v", runtime.mode)
		}
	})

	t.Run("fuse and infrastructure release without pressure", func(t *testing.T) {
		for _, observation := range []attemptObservation{
			attemptTripped{kind: fuseTrip},
			attemptInfrastructure{cause: "confirmed infrastructure"},
			attemptStopped{},
		} {
			runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, true)
			runtime, result := runtime.observeAttempt(generation, observation)
			if runtime.mode != fullAutomatic || !result.settlementAcknowledged {
				t.Fatalf("observation %T result/state=%#v/%#v", observation, result, runtime)
			}
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
	runtime, tripped := runtime.observeAttempt(startedA.generation, attemptTripped{kind: deadlineTrip})

	if !reflect.DeepEqual(tripped.compensatedGrants, []admissionRequestToken{requestA2.request}) ||
		!reflect.DeepEqual(tripped.cancelledWaiting, []admissionRequestToken{requestA3.request}) {
		t.Fatalf("trip cancellation outputs=%#v", tripped)
	}
	if !reflect.DeepEqual(tripped.deliveries, []admissionGrant{requestC1.request}) {
		t.Fatalf("pre-barrier FIFO delivery=%#v", tripped.deliveries)
	}
	if runtime.unboundBarrierIndex(campaignA.token) < runtime.admissionIndex(requestC1.request) {
		t.Fatalf("barrier overtook an earlier other-campaign request: %#v", runtime)
	}
	barrierAt := runtime.unboundBarrierIndex(campaignA.token)
	if barrierAt < 0 || runtime.admissions[barrierAt].grant.delivery != nil {
		t.Fatalf("unbound barrier acquired a delivery action: %#v", runtime)
	}
	_, returned := runtime.acknowledgeGrantReturn(requestA2.deliveries[0])
	if returned.decision != admissionReturnedAfterGateClosure {
		t.Fatalf("compensated grant return=%#v", returned)
	}
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
	runtime, tripped := runtime.observeAttempt(startedA.generation, attemptTripped{kind: deadlineTrip})
	if !reflect.DeepEqual(tripped.compensatedGrants, []admissionRequestToken{requestA2.request}) {
		t.Fatalf("gate compensation=%#v", tripped.compensatedGrants)
	}
	runtime, closed := runtime.closeRuntime(runtimeFatalCause("fatal after gate closure"))
	if len(closed.compensatedGrants) != 0 {
		t.Fatalf("fatal closure repeated gate compensation: %#v", closed.compensatedGrants)
	}
	returnedAt := runtime.admissionIndex(requestA2.request)
	if returnedAt < 0 || runtime.admissions[returnedAt].disposition != dispositionReturnedAfterGate {
		t.Fatalf("fatal closure rewrote gate attribution: %#v", runtime)
	}
	runtime, returned := runtime.acknowledgeGrantReturn(requestA2.deliveries[0])
	if returned.decision != admissionReturnedAfterGateClosure || runtime.lifecycle != runtimeFatalClosing {
		t.Fatalf("gate return acknowledgement=%#v/%#v", returned, runtime)
	}
	resolutions := make([]emergencyResolution, len(runtime.residualCustody()))
	for index, residual := range runtime.residualCustody() {
		resolutions[index] = emergencyResolution{
			generation: residual.generation, disposition: emergencyCustodyTransferred,
		}
	}
	runtime, _ = runtime.settleEmergency(emergencySweep{resolutions: resolutions})
	if runtime.lifecycle != runtimeClosedUnconfirmed {
		t.Fatalf("gate return did not permit final settlement: %#v", runtime)
	}
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
			if lateCancel.decision != admissionRejectedAlreadyCommitted || !reflect.DeepEqual(runtime, unchanged) {
				t.Fatalf("late cancellation consumed return authority: %#v/%#v", lateCancel, runtime)
			}
			wrong := granted.deliveries[0]
			wrong.attempt += "-wrong"
			assertInvariantViolation(t, func() { runtime.acknowledgeGrantReturn(wrong) })
			if !reflect.DeepEqual(runtime, unchanged) {
				t.Fatalf("wrong return acknowledgement changed state: %#v", runtime)
			}
			sweep := emergencySweep{resolutions: []emergencyResolution{{
				generation: generation, disposition: emergencyCustodyTransferred,
			}}}
			var returned admissionResult
			if returnFirst {
				runtime, returned = runtime.acknowledgeGrantReturn(granted.deliveries[0])
				if runtime.lifecycle != runtimeFatalClosing {
					t.Fatalf("return finalized before emergency settlement: %#v", runtime)
				}
				unchanged := runtime
				var lateCancel admissionResult
				runtime, lateCancel = runtime.cancelAdmission(granted.request)
				if lateCancel.decision != admissionRejectedClosed || !reflect.DeepEqual(runtime, unchanged) {
					t.Fatalf("late cancellation changed fatal-closing state: %#v/%#v", lateCancel, runtime)
				}
				assertInvariantViolation(t, func() { runtime.acknowledgeGrantReturn(granted.deliveries[0]) })
				if !reflect.DeepEqual(runtime, unchanged) {
					t.Fatalf("duplicate return changed fatal-closing state: %#v", runtime)
				}
				runtime, _ = runtime.settleEmergency(sweep)
			} else {
				runtime, _ = runtime.settleEmergency(sweep)
				if runtime.lifecycle != runtimeFatalSettledClosing {
					t.Fatalf("emergency finalized before grant return: %#v", runtime)
				}
				unchanged := runtime
				assertInvariantViolation(t, func() { runtime.settleEmergency(sweep) })
				if !reflect.DeepEqual(runtime, unchanged) {
					t.Fatalf("duplicate emergency settlement changed state: %#v", runtime)
				}
				runtime, returned = runtime.acknowledgeGrantReturn(granted.deliveries[0])
			}
			want := []residualCustody{{generation: generation, stage: admissionOwned, transferred: true}}
			if returned.decision != admissionReturnedAfterClosure || runtime.lifecycle != runtimeClosedUnconfirmed ||
				!reflect.DeepEqual(runtime.residualCustody(), want) {
				t.Fatalf("return/final closure=%#v/%#v", returned, runtime)
			}
			unchanged = runtime
			runtime, lateCancel = runtime.cancelAdmission(granted.request)
			if lateCancel.decision != admissionRejectedClosed || !reflect.DeepEqual(runtime, unchanged) {
				t.Fatalf("late cancellation changed final state: %#v/%#v", lateCancel, runtime)
			}
			assertInvariantViolation(t, func() { runtime.acknowledgeGrantReturn(granted.deliveries[0]) })
			if !reflect.DeepEqual(runtime, unchanged) {
				t.Fatalf("closed state changed after duplicate return: %#v", runtime)
			}
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
	if !reflect.DeepEqual(closed.compensatedGrants, []admissionRequestToken{granted.request}) {
		t.Fatalf("fatal compensation=%#v", closed.compensatedGrants)
	}

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
	runtime, first := runtime.observeAttempt(startedA1.generation, attemptTripped{kind: deadlineTrip})
	runtime, second := runtime.observeAttempt(startedA2.generation, attemptTripped{kind: deadlineTrip})
	barriers := 0
	for _, admission := range runtime.admissions {
		if admission.grant.class == confirmationBarrierAdmission {
			barriers++
		}
	}
	if !first.confirmationProvisional || !second.confirmationProvisional || barriers != 1 ||
		len(second.cancelledWaiting) != 0 || len(second.compensatedGrants) != 0 {
		t.Fatalf("provisional results/barriers=%#v/%#v/%d", first, second, barriers)
	}
}

func TestProcessRuntimeConfirmationOutcomeControlsPressureAndReopensGate(t *testing.T) {
	t.Run("rejected", func(t *testing.T) {
		runtime := runtimeAtBoundConfirmation(t)
		barrierAt := runtime.grantedConfirmationIndex()
		grant := runtime.admissions[barrierAt].grant
		runtime, started := runtime.startCommitted(grant)
		runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
		runtime, result := runtime.observeAttempt(started.generation, attemptTripped{kind: deadlineTrip})
		runtime, completed := runtime.completeConfirmationQueue(grant.campaign)
		campaignAt := runtime.campaignIndex(grant.campaign)
		if completed.decision != confirmationQueueCompleted || runtime.mode != fullAutomatic ||
			result.pressureTransitioned || !runtime.campaigns[campaignAt].primaryGateOpen {
			t.Fatalf("rejected confirmation result/completion/state=%#v/%#v/%#v", result, completed, runtime)
		}
	})

	t.Run("already single is idempotent", func(t *testing.T) {
		runtime := runtimeAtBoundConfirmation(t)
		runtime.mode = singleAdmission
		barrierAt := runtime.grantedConfirmationIndex()
		grant := runtime.admissions[barrierAt].grant
		runtime, started := runtime.startCommitted(grant)
		runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
		_, result := runtime.observeAttempt(started.generation, settledConfirmation(grant))
		if result.pressureTransitioned {
			t.Fatalf("duplicate pressure transition=%#v", result)
		}
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
	if runtime.mode != singleAdmission || !result.pressureTransitioned ||
		runtime.campaigns[campaignAt].primaryGateOpen {
		t.Fatalf("ordinary confirmation settlement result/state = %#v/%#v", result, runtime)
	}
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
	if runtime.mode != fullAutomatic || result.pressureTransitioned {
		t.Fatalf("mismatched confirmation changed pressure = %#v/%#v", result, runtime)
	}
}

func TestProcessRuntimeReopensPrimaryGateOnlyAfterConfirmationQueueCompletion(t *testing.T) {
	runtime := runtimeAtBoundConfirmation(t)
	barrierAt := runtime.grantedConfirmationIndex()
	grant := runtime.admissions[barrierAt].grant
	runtime, started := runtime.startCommitted(grant)
	runtime, _ = runtime.observeAttempt(started.generation, launchOwned{})
	runtime, _ = runtime.observeAttempt(started.generation, attemptTripped{kind: deadlineTrip})
	runtime, rejected := runtime.requestAdmission(admissionRequest{
		campaign: grant.campaign, attempt: "primary-before-completion", class: sharedAdmission,
	})
	if rejected.decision != admissionRejectedGateClosed {
		t.Fatalf("primary admission before queue completion = %#v", rejected)
	}

	runtime, completed := runtime.completeConfirmationQueue(grant.campaign)
	if completed.decision != confirmationQueueCompleted {
		t.Fatalf("confirmation queue completion = %#v", completed)
	}
	runtime, admitted := runtime.requestAdmission(admissionRequest{
		campaign: grant.campaign, attempt: "primary-after-completion", class: sharedAdmission,
	})
	if admitted.decision != admissionAccepted || len(admitted.deliveries) != 1 {
		t.Fatalf("primary admission after queue completion = %#v", admitted)
	}
}

func TestProcessRuntimeClosedEmergencyStateCannotBeRewritten(t *testing.T) {
	runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, true)
	runtime, _ = runtime.closeRuntime(runtimeFatalCause("fatal test"))
	runtime, _ = runtime.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
		generation: generation, disposition: emergencyCustodyTransferred,
	}}})
	if runtime.lifecycle != runtimeClosedUnconfirmed {
		t.Fatalf("first sweep lifecycle=%v", runtime.lifecycle)
	}
	assertInvariantViolation(t, func() {
		runtime.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
			generation: generation, disposition: emergencyConfirmedDrained,
		}}})
	})
}

func TestProcessRuntimeDrainUnconfirmedRemainsFatalClosingUntilSweep(t *testing.T) {
	runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, true)
	runtime, _ = runtime.observeAttempt(generation, drainUnconfirmed{})
	if runtime.lifecycle != runtimeFatalClosing {
		t.Fatalf("drain-unconfirmed finalized before sweep: %#v", runtime)
	}
	runtime, _ = runtime.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
		generation: generation, disposition: emergencyCustodyTransferred,
	}}})
	if runtime.lifecycle != runtimeClosedUnconfirmed {
		t.Fatalf("transferred sweep lifecycle=%v", runtime.lifecycle)
	}
}

func TestProcessRuntimePrimaryGateAppliesToSharedAndSerialButNotConfirmation(t *testing.T) {
	runtime := runtimeAtBoundConfirmation(t)
	campaign := runtime.admissions[runtime.grantedConfirmationIndex()].grant.campaign
	for _, class := range []admissionClass{sharedAdmission, serialPrimaryAdmission} {
		_, result := runtime.requestAdmission(admissionRequest{campaign: campaign, attempt: "blocked", class: class})
		if result.decision != admissionRejectedGateClosed {
			t.Fatalf("class %v gate result=%#v", class, result)
		}
	}
	_, result := runtime.requestAdmission(admissionRequest{
		campaign: campaign, attempt: "follow-on", class: confirmationAdmission,
		profile: AutomaticProfile, deadline: 31 * time.Second,
	})
	if result.decision != admissionRejectedExclusiveOutstanding {
		t.Fatalf("confirmation cardinality result=%#v", result)
	}
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
	if duplicate.decision != admissionRejectedDuplicate {
		t.Fatalf("duplicate identity decision=%#v", duplicate)
	}
}

func TestProcessRuntimeFatalCausesRetainEveryIngressInOrder(t *testing.T) {
	runtime := newProcessRuntime(1)
	runtime, _ = runtime.closeRuntime(runtimeFatalCause("first"))
	runtime, _ = runtime.closeRuntime(runtimeFatalCause("second"))
	runtime, _ = runtime.closeRuntime(runtimeFatalCause("first"))
	if !reflect.DeepEqual(runtime.fatalCauses, []runtimeFatalCause{"first", "second", "first"}) {
		t.Fatalf("joined causes=%#v", runtime.fatalCauses)
	}
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
			if !reflect.DeepEqual(runtime, unchanged) {
				t.Fatalf("owned=%t duplicate seed changed state: %#v", owned, runtime)
			}
		}
		want := []runtimeFatalCause{
			runtimeFatalCause(fmt.Sprintf("%s generation=%d", kind, generations[0])),
			runtimeFatalCause(fmt.Sprintf("%s generation=%d", kind, generations[1])),
		}
		if !reflect.DeepEqual(runtime.fatalCauses, want) {
			t.Fatalf("owned=%t fatal causes=%#v, want %#v", owned, runtime.fatalCauses, want)
		}
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
	wantResidual := []residualCustody{{generation: generation, stage: admissionOwned, transferred: true}}
	if !reflect.DeepEqual(runtime.fatalCauses, wantCauses) ||
		!reflect.DeepEqual(runtime.residualCustody(), wantResidual) {
		t.Fatalf("late-owned drain state=%#v, want causes/residual %#v/%#v", runtime, wantCauses, wantResidual)
	}
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
	if runtime.lifecycle != runtimeFatalSettledClosing {
		t.Fatalf("pending return did not hold fatal closing: %#v", runtime)
	}
	unchanged := runtime
	assertInvariantViolation(t, func() {
		runtime.observeAttempt(started.generation, launchNotReleased{reason: launchFailed})
	})
	if !reflect.DeepEqual(runtime, unchanged) {
		t.Fatalf("late no-release deleted transferred custody: %#v", runtime)
	}
	runtime, returned := runtime.acknowledgeGrantReturn(pending.deliveries[0])
	want := []residualCustody{{
		generation: started.generation, stage: admissionProspective, transferred: true,
	}}
	if returned.decision != admissionReturnedAfterClosure || runtime.lifecycle != runtimeClosedUnconfirmed ||
		!reflect.DeepEqual(runtime.residualCustody(), want) {
		t.Fatalf("return/final custody=%#v/%#v, want %#v", returned, runtime, want)
	}
}

func TestProcessRuntimeLateProvenNoReleaseWaitsForExactEmptySettlement(t *testing.T) {
	runtime, generation := runtimeWithOwnedOrProspectiveAttempt(t, sharedAdmission, false)
	runtime, _ = runtime.observeAttempt(generation, launchUnconfirmed{})
	runtime, noRelease := runtime.observeAttempt(generation, launchNotReleased{reason: launchFailed})
	wantNoRelease := observationResult{
		generation: generation, settlementAcknowledged: true, runtimeClosureInProgress: true,
	}
	if !reflect.DeepEqual(noRelease, wantNoRelease) || runtime.lifecycle != runtimeFatalClosing ||
		runtime.admissionIndexByGeneration(generation) >= 0 || len(runtime.admissions) != 0 ||
		len(runtime.residualCustody()) != 0 {
		t.Fatalf("late no-release settlement/state=%#v/%#v", noRelease, runtime)
	}
	runtime, settled := runtime.settleEmergency(emergencySweep{})
	if runtime.lifecycle != runtimeClosedDrained ||
		len(settled.acknowledged) != 0 || len(settled.residual) != 0 {
		t.Fatalf("empty emergency settlement/state=%#v/%#v", settled, runtime)
	}
	unchanged := runtime.clone()
	assertInvariantViolation(t, func() { runtime.settleEmergency(emergencySweep{}) })
	if !reflect.DeepEqual(runtime, unchanged) {
		t.Fatalf("duplicate closed empty settlement changed state: %#v", runtime)
	}
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
				if runtime.lifecycle != runtimeFatalClosing {
					t.Fatalf("grant return finalized unsettled empty epoch: %#v", runtime)
				}
				runtime, settled = runtime.settleEmergency(emergencySweep{})
			} else {
				beforeSettlement := runtime
				runtime, settled = runtime.settleEmergency(emergencySweep{})
				wantSettled := beforeSettlement.clone()
				wantSettled.lifecycle = runtimeFatalSettledClosing
				if !reflect.DeepEqual(runtime, wantSettled) {
					t.Fatalf("empty settlement state = %#v, want %#v", runtime, wantSettled)
				}
				beforeRepeatedFatal := runtime.clone()
				laterCause := runtimeFatalCause("later fatal ingress")
				var repeatedFatal runtimeClosure
				runtime, repeatedFatal = runtime.closeRuntime(laterCause)
				wantRepeatedFatal := beforeRepeatedFatal.clone()
				wantRepeatedFatal.fatalCauses = append(wantRepeatedFatal.fatalCauses, laterCause)
				if !reflect.DeepEqual(runtime, wantRepeatedFatal) ||
					runtime.lifecycle != runtimeFatalSettledClosing ||
					!reflect.DeepEqual(runtime.admissions, beforeRepeatedFatal.admissions) ||
					len(runtime.residualCustody()) != 0 || len(repeatedFatal.residual) != 0 ||
					len(repeatedFatal.cancelledWaiting) != 0 || len(repeatedFatal.compensatedGrants) != 0 {
					t.Fatalf(
						"repeated fatal reset settled closing: state=%#v closure=%#v want=%#v",
						runtime, repeatedFatal, wantRepeatedFatal,
					)
				}
				unchanged := runtime
				assertInvariantViolation(t, func() {
					runtime.settleEmergency(emergencySweep{})
				})
				if !reflect.DeepEqual(runtime, unchanged) {
					t.Fatalf("duplicate empty settlement changed state: %#v", runtime)
				}
				runtime, returned = runtime.acknowledgeGrantReturn(granted.deliveries[0])
			}
			if returned.decision != admissionReturnedAfterClosure ||
				runtime.lifecycle != runtimeClosedDrained || len(runtime.residualCustody()) != 0 ||
				len(settled.acknowledged) != 0 || len(settled.residual) != 0 {
				t.Fatalf("empty settlement/return final state=%#v/%#v/%#v", settled, returned, runtime)
			}
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
	if !reflect.DeepEqual(closed.compensatedGrants, []admissionRequestToken{granted.request}) {
		t.Fatalf("empty epoch compensation=%#v", closed)
	}
	runtime, noRelease := runtime.observeAttempt(
		started.generation,
		launchNotReleased{reason: launchFailed},
	)
	wantNoRelease := observationResult{
		generation: started.generation, settlementAcknowledged: true,
		runtimeClosureInProgress: true,
	}
	if !reflect.DeepEqual(noRelease, wantNoRelease) || runtime.lifecycle != runtimeFatalClosing ||
		runtime.admissionIndexByGeneration(started.generation) >= 0 ||
		len(runtime.admissions) != 1 || runtime.admissions[0].grant != granted.deliveries[0] ||
		runtime.admissions[0].stage != admissionGranted ||
		runtime.admissions[0].disposition != dispositionReturnedAfterClosure ||
		len(runtime.residualCustody()) != 0 {
		t.Fatalf("empty epoch no-release state=%#v/%#v", noRelease, runtime)
	}

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
	runtime, _ = runtime.observeAttempt(startedA.generation, attemptTripped{kind: deadlineTrip})
	runtime, _ = runtime.observeAttempt(startedB.generation, attemptSettled{})
	runtime, bound := runtime.sealAndBindConfirmationBarrier(barrierBinding{
		campaign: campaignA.token,
		attempt:  confirmationAttempt,
		profile:  AutomaticProfile, deadline: 31 * time.Second,
	})
	if bound.decision != barrierBound || len(bound.deliveries) != 1 {
		t.Fatalf("bound confirmation setup=%#v/%#v", bound, runtime)
	}

	return runtime
}
