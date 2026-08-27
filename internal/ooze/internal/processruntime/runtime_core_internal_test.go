package processruntime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessRuntimeRegistersConcurrentCampaignsAndRejectsRecursiveEntry(t *testing.T) {
	runtime := newProcessRuntime(2)

	var first campaignRegistration
	runtime, first = runtime.registerCampaign(campaignProvenance{lineage: 11})
	var second campaignRegistration
	runtime, second = runtime.registerCampaign(campaignProvenance{lineage: 22})
	var recursive campaignRegistration
	runtime, recursive = runtime.registerCampaign(campaignProvenance{lineage: 11})

	assert.Equal(t, campaignRegistered, first.decision, "independent campaigns were not registered: first=%#v second=%#v", first, second)
	assert.Equal(t, campaignRegistered, second.decision, "independent campaigns were not registered: first=%#v second=%#v", first, second)
	assert.Equal(t, campaignRejectedRecursive, recursive.decision, "recursive registration decision=%v, want %v", recursive.decision, campaignRejectedRecursive)
	assert.NotEqual(t, second.token.id, first.token.id, "campaign identity/state mismatch: first=%#v second=%#v state=%#v", first, second, runtime)
	assert.EqualValues(t, 2, len(runtime.campaigns), "campaign identity/state mismatch: first=%#v second=%#v state=%#v", first, second, runtime)
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

	assert.Equal(t, admissionAccepted, waiting.decision, "third request must wait: %#v", waiting)
	assert.EqualValues(t, 0, len(waiting.deliveries), "third request must wait: %#v", waiting)

	runtime, cancelled := runtime.cancelAdmission(first.request)
	trace = append(trace, cancelled.deliveries...)

	want := []admissionGrant{
		first.request,
		second.request,
		waiting.request,
	}
	assert.Equal(t, want, trace, "grant trace=%#v, want %#v", trace, want)
	assert.Equal(t, admissionCancelledGranted, cancelled.decision, "cancel result/state mismatch: result=%#v state=%#v", cancelled, runtime)
	assert.EqualValues(t, 2, len(runtime.admissions), "cancel result/state mismatch: result=%#v state=%#v", cancelled, runtime)
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
	assert.Equal(t, []admissionGrant{requestB1.request}, result.deliveries, "first release deliveries=%#v, want b1", result.deliveries)
	runtime, result = runtime.cancelAdmission(requestA2.request)
	trace = append(trace, result.deliveries...)
	assert.EqualValues(t, 0, len(result.deliveries), "exclusive must wait for b1, got deliveries %#v", result.deliveries)
	runtime, result = runtime.cancelAdmission(requestB1.request)
	trace = append(trace, result.deliveries...)
	assert.Equal(t, []admissionGrant{exclusive.request}, result.deliveries, "exclusive grant=%#v", result.deliveries)
	_, result = runtime.cancelAdmission(exclusive.request)
	trace = append(trace, result.deliveries...)
	assert.Equal(t, []admissionGrant{later.request}, result.deliveries, "post-exclusive grant=%#v", result.deliveries)

	want := []admissionGrant{
		requestA1.request,
		requestA2.request,
		requestB1.request,
		exclusive.request,
		later.request,
	}
	assert.Equal(t, want, trace, "grant trace=%#v, want %#v", trace, want)
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

	assert.Equal(t, admissionRejectedExclusiveOutstanding, second.decision, "second exclusive result/state=%#v/%#v, want rejection and unchanged state", second, runtime)
	assert.Equal(t, unchanged, runtime, "second exclusive result/state=%#v/%#v, want rejection and unchanged state", second, runtime)
	runtime, _ = runtime.cancelAdmission(first.request)
	_, second = runtime.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "serial-2",
		class:    exclusiveAdmission,
	})
	assert.Equal(t, admissionAccepted, second.decision, "exclusive was not accepted after return: %#v", second)
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
	assert.Equal(t, admissionAccepted, first.decision, "granted/waiting shared demand=%#v/%#v", first, second)
	assert.EqualValues(t, 1, len(first.deliveries), "granted/waiting shared demand=%#v/%#v", first, second)
	assert.Equal(t, admissionAccepted, second.decision, "granted/waiting shared demand=%#v/%#v", first, second)
	assert.EqualValues(t, 0, len(second.deliveries), "granted/waiting shared demand=%#v/%#v", first, second)
	unchanged := runtime
	runtime, rejected := runtime.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "a3", class: sharedAdmission,
	})
	assert.Equal(t, admissionRejectedSharedLimit, rejected.decision, "request beyond P result/state=%#v/%#v", rejected, runtime)
	assert.Equal(t, unchanged, runtime, "request beyond P result/state=%#v/%#v", rejected, runtime)
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
		assert.Equal(t, installed, generation, "launch observed uninstalled generation: installed=%d state=%#v", installed, snapshot)
		assert.NotEqual(t, 0, installed, "launch observed uninstalled generation: installed=%d state=%#v", installed, snapshot)
		if assert.False(t, index < 0, "launch observed uninstalled generation: installed=%d state=%#v", installed, snapshot) {
			assert.Equal(t, admissionProspective, snapshot.admissions[index].stage, "launch observed uninstalled generation: installed=%d state=%#v", installed, snapshot)
		}
		reentrant = shell.registerCampaign(campaignProvenance{lineage: 22})

		return launchNotReleased{reason: launchFailed}
	})
	settled := shell.observeAttempt(prepared.result.generation, observed)
	result := prepared.result
	result.settlementAcknowledged = settled.settlementAcknowledged

	assert.Equal(t, startCommittedAccepted, result.decision, "start result=%#v", result)
	assert.NotEqual(t, 0, result.generation, "start result=%#v", result)
	assert.True(t, result.settlementAcknowledged, "start result=%#v", result)
	assert.Equal(t, result.generation, installed, "generation/calls/reentry/state=%d/%d/%#v/%#v", installed, launchCalls, reentrant, shell.snapshot())
	assert.EqualValues(t, 1, launchCalls, "generation/calls/reentry/state=%d/%d/%#v/%#v", installed, launchCalls, reentrant, shell.snapshot())
	assert.Equal(t, campaignRegistered, reentrant.decision, "generation/calls/reentry/state=%d/%d/%#v/%#v", installed, launchCalls, reentrant, shell.snapshot())
	assert.EqualValues(t, 0, len(shell.snapshot().admissions), "generation/calls/reentry/state=%d/%d/%#v/%#v", installed, launchCalls, reentrant, shell.snapshot())
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
	assert.Equal(t, runtimeFatalClosing, snapshot.lifecycle, "stale generation did not close with retained custody: %#v", snapshot)
	assert.Equal(t, []residualCustody{{
		generation: started.generation, attempt: grant.attempt, stage: admissionOwned,
	}}, snapshot.residualCustody(), "stale generation did not close with retained custody: %#v", snapshot)
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
		{
			_, ok := recover().(runtimeInvariantViolation)
			require.True(t, ok, "action did not panic with runtimeInvariantViolation")
		}
	}()
	action()
}
