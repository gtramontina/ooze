//nolint:cyclop,exhaustruct // Ordered traces deliberately omit shell-only fields.
package ooze

import (
	"reflect"
	"testing"
)

func TestProcessRuntimeRegistersConcurrentCampaignsAndRejectsRecursiveEntry(t *testing.T) {
	runtime := newProcessRuntime(2)

	var first campaignRegistration
	runtime, first = runtime.registerCampaign(campaignProvenance{lineage: 11})
	var second campaignRegistration
	runtime, second = runtime.registerCampaign(campaignProvenance{lineage: 22})
	var recursive campaignRegistration
	runtime, recursive = runtime.registerCampaign(campaignProvenance{lineage: 11})

	if first.decision != campaignRegistered || second.decision != campaignRegistered {
		t.Fatalf("independent campaigns were not registered: first=%#v second=%#v", first, second)
	}
	if recursive.decision != campaignRejectedRecursive {
		t.Fatalf("recursive registration decision=%v, want %v", recursive.decision, campaignRejectedRecursive)
	}
	if first.token.id == second.token.id || len(runtime.campaigns) != 2 {
		t.Fatalf("campaign identity/state mismatch: first=%#v second=%#v state=%#v", first, second, runtime)
	}
}

func TestProcessRuntimeGrantsSharedAdmissionsInStableFIFOOrder(t *testing.T) {
	runtime := newProcessRuntime(2)
	runtime, campaignA := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, campaignB := runtime.registerCampaign(campaignProvenance{lineage: 22})

	trace := make([]admissionGrant, 0, 3)
	runtime, first := runtime.requestAdmission(admissionRequest{
		campaign: campaignA.token,
		attempt:  attemptIdentity("a1"),
		class:    sharedAdmission,
	})
	trace = append(trace, first.deliveries...)
	runtime, second := runtime.requestAdmission(admissionRequest{
		campaign: campaignA.token,
		attempt:  attemptIdentity("a2"),
		class:    sharedAdmission,
	})
	trace = append(trace, second.deliveries...)
	runtime, waiting := runtime.requestAdmission(admissionRequest{
		campaign: campaignB.token,
		attempt:  attemptIdentity("b1"),
		class:    sharedAdmission,
	})
	trace = append(trace, waiting.deliveries...)

	if waiting.decision != admissionAccepted || len(waiting.deliveries) != 0 {
		t.Fatalf("third request must wait: %#v", waiting)
	}

	runtime, cancelled := runtime.cancelAdmission(first.request)
	trace = append(trace, cancelled.deliveries...)

	want := []admissionGrant{
		first.request,
		second.request,
		waiting.request,
	}
	if !reflect.DeepEqual(trace, want) {
		t.Fatalf("grant trace=%#v, want %#v", trace, want)
	}
	if cancelled.decision != admissionCancelledGranted || len(runtime.admissions) != 2 {
		t.Fatalf("cancel result/state mismatch: result=%#v state=%#v", cancelled, runtime)
	}
}

func TestProcessRuntimeQueuesExclusiveAdmissionAsFIFOBarrier(t *testing.T) {
	runtime := newProcessRuntime(2)
	runtime, campaignA := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, campaignB := runtime.registerCampaign(campaignProvenance{lineage: 22})
	runtime, campaignC := runtime.registerCampaign(campaignProvenance{lineage: 33})
	runtime, campaignD := runtime.registerCampaign(campaignProvenance{lineage: 44})

	trace := make([]admissionGrant, 0, 5)
	runtime, requestA1 := requestForTest(runtime, campaignA.token, "a1", sharedAdmission, &trace)
	runtime, requestA2 := requestForTest(runtime, campaignA.token, "a2", sharedAdmission, &trace)
	runtime, requestB1 := requestForTest(runtime, campaignB.token, "b1", sharedAdmission, &trace)
	runtime, exclusive := requestForTest(runtime, campaignC.token, "x", exclusiveAdmission, &trace)
	runtime, later := requestForTest(runtime, campaignD.token, "d1", sharedAdmission, &trace)

	runtime, result := runtime.cancelAdmission(requestA1.request)
	trace = append(trace, result.deliveries...)
	if !reflect.DeepEqual(result.deliveries, []admissionGrant{requestB1.request}) {
		t.Fatalf("first release deliveries=%#v, want b1", result.deliveries)
	}
	runtime, result = runtime.cancelAdmission(requestA2.request)
	trace = append(trace, result.deliveries...)
	if len(result.deliveries) != 0 {
		t.Fatalf("exclusive must wait for b1, got deliveries %#v", result.deliveries)
	}
	runtime, result = runtime.cancelAdmission(requestB1.request)
	trace = append(trace, result.deliveries...)
	if !reflect.DeepEqual(result.deliveries, []admissionGrant{exclusive.request}) {
		t.Fatalf("exclusive grant=%#v", result.deliveries)
	}
	_, result = runtime.cancelAdmission(exclusive.request)
	trace = append(trace, result.deliveries...)
	if !reflect.DeepEqual(result.deliveries, []admissionGrant{later.request}) {
		t.Fatalf("post-exclusive grant=%#v", result.deliveries)
	}

	want := []admissionGrant{
		requestA1.request,
		requestA2.request,
		requestB1.request,
		exclusive.request,
		later.request,
	}
	if !reflect.DeepEqual(trace, want) {
		t.Fatalf("grant trace=%#v, want %#v", trace, want)
	}
}

func TestProcessRuntimeAllowsOneOutstandingExclusivePerCampaign(t *testing.T) {
	runtime := newProcessRuntime(2)
	runtime, campaign := runtime.registerCampaign(campaignProvenance{lineage: 11})

	runtime, first := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "serial-1",
		class:    exclusiveAdmission,
	})
	unchanged := runtime
	runtime, second := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "serial-2",
		class:    exclusiveAdmission,
	})

	if second.decision != admissionRejectedExclusiveOutstanding || !reflect.DeepEqual(runtime, unchanged) {
		t.Fatalf("second exclusive result/state=%#v/%#v, want rejection and unchanged state", second, runtime)
	}
	runtime, _ = runtime.cancelAdmission(first.request)
	_, second = runtime.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "serial-2",
		class:    exclusiveAdmission,
	})
	if second.decision != admissionAccepted {
		t.Fatalf("exclusive was not accepted after return: %#v", second)
	}
}

func TestProcessRuntimeBoundsSharedDemandPerCampaignAtCapacity(t *testing.T) {
	runtime := newProcessRuntime(2)
	runtime, campaign := runtime.registerCampaign(campaignProvenance{lineage: 11})
	runtime, peer := runtime.registerCampaign(campaignProvenance{lineage: 22})
	runtime, first := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "a1", class: sharedAdmission,
	})
	runtime, _ = runtime.requestAdmission(admissionRequest{
		campaign: peer.token, attempt: "b1", class: sharedAdmission,
	})
	runtime, second := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "a2", class: sharedAdmission,
	})
	if first.decision != admissionAccepted || len(first.deliveries) != 1 ||
		second.decision != admissionAccepted || len(second.deliveries) != 0 {
		t.Fatalf("granted/waiting shared demand=%#v/%#v", first, second)
	}
	unchanged := runtime
	runtime, rejected := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "a3", class: sharedAdmission,
	})
	if rejected.decision != admissionRejectedSharedLimit || !reflect.DeepEqual(runtime, unchanged) {
		t.Fatalf("request beyond P result/state=%#v/%#v", rejected, runtime)
	}
}

func TestProcessRuntimeShellStartCommittedInstallsBeforeReentrantLaunchAndSettlesNoRelease(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 11})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "a1",
		class:    sharedAdmission,
	})
	grant := <-requested.delivery

	var installed attemptGeneration
	var reentrant campaignRegistration
	launchCalls := 0
	cell := pendingStartCell{}
	prepared := shell.startCommitted(grant, startInstallation{grant: grant, cell: &cell})
	observed := prepared.start.launch(func(generation attemptGeneration) attemptObservation {
		launchCalls++
		installed = cell.installedGeneration()
		snapshot := shell.snapshot()
		index := snapshot.admissionIndexByGeneration(installed)
		if generation != installed || installed == 0 || index < 0 ||
			snapshot.admissions[index].stage != admissionProspective {
			t.Fatalf("launch observed uninstalled generation: installed=%d state=%#v", installed, snapshot)
		}
		reentrant = shell.registerCampaign(campaignProvenance{lineage: 22})

		return launchNotReleased{reason: launchFailed}
	})
	settled := shell.observeAttempt(prepared.result.generation, observed)
	result := prepared.result
	result.settlementAcknowledged = settled.settlementAcknowledged

	if result.decision != startCommittedAccepted || result.generation == 0 || !result.settlementAcknowledged {
		t.Fatalf("start result=%#v", result)
	}
	if installed != result.generation || launchCalls != 1 || reentrant.decision != campaignRegistered ||
		len(shell.snapshot().admissions) != 0 {
		t.Fatalf("generation/calls/reentry/state=%d/%d/%#v/%#v", installed, launchCalls, reentrant, shell.snapshot())
	}
}

func TestProcessRuntimeShellRejectedOrClosedStartInvokesNothing(t *testing.T) {
	tests := []struct {
		name  string
		close bool
		grant func(admissionGrant) admissionGrant
	}{
		{
			name:  "wrong grant",
			close: false,
			grant: func(grant admissionGrant) admissionGrant {
				grant.attempt = "wrong"

				return grant
			},
		},
		{
			name:  "fatal closure",
			close: true,
			grant: func(grant admissionGrant) admissionGrant { return grant },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			shell := newProcessRuntimeShell(1)
			campaign := shell.registerCampaign(campaignProvenance{lineage: 11})
			requested := shell.requestAdmission(admissionRequest{
				campaign: campaign.token,
				attempt:  "a1",
				class:    sharedAdmission,
			})
			grant := <-requested.delivery
			if test.close {
				shell.closeRuntime(runtimeFatalCause("test closure"))
			}

			launchCalls := 0
			cell := pendingStartCell{}
			native := func(_ attemptGeneration) attemptObservation {
				launchCalls++

				return launchNotReleased{reason: launchFailed}
			}
			attemptedGrant := test.grant(grant)
			prepared := shell.startCommitted(attemptedGrant, startInstallation{grant: attemptedGrant, cell: &cell})

			if prepared.result.decision == startCommittedAccepted || cell.installedGeneration() != 0 || launchCalls != 0 {
				t.Fatalf("rejected start result/cell/calls=%#v/%d/%d", prepared.result,
					cell.installedGeneration(), launchCalls)
			}
			_ = native
		})
	}
}

func TestProcessRuntimeShellTerminalSettlementIsGenerationCorrelated(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 11})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "a1",
		class:    sharedAdmission,
	})
	grant := <-requested.delivery
	started := startOwned(shell, grant)

	assertInvariantViolation(t, func() {
		shell.observeAttempt(started.generation+1, attemptSettled{})
	})
	snapshot := shell.snapshot()
	if snapshot.lifecycle != runtimeFatalClosing || !reflect.DeepEqual(snapshot.residualCustody(), []residualCustody{{
		generation: started.generation, stage: admissionOwned,
	}}) {
		t.Fatalf("stale generation did not close with retained custody: %#v", snapshot)
	}
}

func requestForTest(
	runtime processRuntime,
	campaign campaignToken,
	attempt attemptIdentity,
	class admissionClass,
	trace *[]admissionGrant,
) (processRuntime, admissionResult) {
	runtime, result := runtime.requestAdmission(admissionRequest{
		campaign: campaign,
		attempt:  attempt,
		class:    class,
	})
	*trace = append(*trace, result.deliveries...)

	return runtime, result
}

func (r processRuntime) hasCampaign(token campaignToken) bool { return r.campaignIndex(token) >= 0 }

func assertInvariantViolation(t *testing.T, action func()) {
	t.Helper()
	defer func() {
		if _, ok := recover().(runtimeInvariantViolation); !ok {
			t.Fatal("action did not panic with runtimeInvariantViolation")
		}
	}()
	action()
}
