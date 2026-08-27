package ooze

import (
	"testing"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/stretchr/testify/require"
)

type pendingStartCell = processruntime.StartCell
type installedStart = processruntime.PreparedStart
type processRuntimeShell = processruntime.Runtime

func newProcessRuntimeShell(capacity int) *processruntime.Runtime {
	return processruntime.New(capacity)
}

func newProcessRuntimeShellWithObserver(capacity int, observer processruntime.Observer) *processruntime.Runtime {
	return processruntime.NewObserved(capacity, observer)
}

func registerCampaignForTest(runtime *processruntime.Runtime, provenance campaignProvenance) campaignRegistration {
	return campaignRegistrationEvidence(runtime.RegisterCampaign(provenance.lineage))
}

func requestAdmissionForTest(runtime *processruntime.Runtime, request admissionRequest) admissionAwait {
	await := runtime.RequestAdmission(processRuntimeAdmission(campaignAdmissionValue(request)))
	delivery := make(chan admissionGrant, 1)
	go func() {
		grant, received := await.Receive()
		if received {
			value := runtimeAdmissionValue(grant.Admission())
			value.grant = grant
			delivery <- value
		}
		close(delivery)
	}()
	return admissionAwait{
		decision: await.Decision(), request: runtimeAdmissionValue(await.Request()),
		delivery: delivery, fatal: fatalEpochID(runtime.FatalEpoch()),
	}
}

type startInstallation struct {
	grant admissionGrant
	cell  *pendingStartCell
}

type preparedStart struct {
	result startCommittedResult
	start  installedStart
}

func startCommittedForTest(runtime *processruntime.Runtime, grant admissionGrant, installation startInstallation) preparedStart {
	prepared := runtime.CommitStart(grant.grant, installation.cell)
	return preparedStart{
		result: startCommittedResult{decision: prepared.Decision(), generation: prepared.Generation()},
		start:  prepared,
	}
}

func launchForTest(start installedStart, launch func(attemptGeneration) attemptObservation) attemptObservation {
	return runtimeObservation(start.Launch(func(generation processruntime.Generation) processruntime.Observation {
		return processRuntimeObservation(launch(attemptGeneration(generation)))
	}))
}

func observeAttemptForTest(runtime *processruntime.Runtime, generation attemptGeneration, observation attemptObservation) observationResult {
	return runtimeReceipt(runtime.Observe(generation, processRuntimeObservation(observation)))
}

func closeRuntimeForTest(runtime *processruntime.Runtime, cause runtimeFatalCause) runtimeClosure {
	return runtimeClosureValue(runtime.Close(string(cause)))
}

func settleEmergencyForTest(runtime *processruntime.Runtime, sweep emergencySweep) emergencySettlement {
	return runtimeEmergencySettlement(runtime.SettleEmergency(processRuntimeResolutions(sweep)))
}

func startOwned(runtime *processruntime.Runtime, grant admissionGrant) startCommittedResult {
	cell := processruntime.NewStartCell()
	prepared := startCommittedForTest(runtime, grant, startInstallation{grant: grant, cell: cell})
	if prepared.result.decision == startCommittedAccepted {
		observeAttemptForTest(runtime, prepared.result.generation, launchOwned{})
	}

	return prepared.result
}

func assertInvariantViolation(t *testing.T, action func()) {
	t.Helper()
	defer func() {
		switch recover().(type) {
		case runtimeInvariantViolation, processruntime.Violation:
		default:
			require.FailNow(t, "action did not panic with an invariant violation")
		}
	}()
	action()
}

func runtimeAdmissionByGeneration(image processruntime.Image, generation attemptGeneration) (admissionAuthority, bool) {
	admission, found := image.Admission(generation)
	return runtimeAdmissionValue(admission), found
}

type testProcessRuntime struct{ processRuntime }

func newProcessRuntime(capacity int) testProcessRuntime {
	return testProcessRuntime{processRuntime: processruntime.NewState(capacity)}
}

func campaignTokenForTest(lineage campaignLineage) campaignToken {
	_, registration := newProcessRuntime(1).registerCampaign(campaignProvenance{lineage: lineage})
	return registration.token
}

func (runtime testProcessRuntime) registerCampaign(provenance campaignProvenance) (testProcessRuntime, campaignRegistration) {
	next, result := runtime.RegisterCampaign(provenance.lineage)
	return testProcessRuntime{processRuntime: next}, campaignRegistrationEvidence(result)
}

func (runtime testProcessRuntime) requestAdmission(request admissionRequest) (testProcessRuntime, admissionResult) {
	next, result := runtime.RequestAdmission(processRuntimeAdmission(campaignAdmissionValue(request)))
	return testProcessRuntime{processRuntime: next}, runtimeAdmissionResult(result)
}

func (runtime testProcessRuntime) cancelAdmission(request admissionRequest) (testProcessRuntime, admissionResult) {
	next, result := runtime.CancelAdmission(processRuntimeAdmission(campaignAdmissionValue(request)))
	return testProcessRuntime{processRuntime: next}, runtimeAdmissionResult(result)
}

func (runtime testProcessRuntime) acknowledgeGrantReturn(grant admissionGrant) (testProcessRuntime, admissionResult) {
	next, result := runtime.ReturnGrant(processRuntimeAdmission(campaignAdmissionValue(grant)))
	return testProcessRuntime{processRuntime: next}, runtimeAdmissionResult(result)
}

func (runtime testProcessRuntime) startCommitted(grant admissionGrant) (testProcessRuntime, startCommittedResult) {
	next, result := runtime.CommitStart(processRuntimeAdmission(campaignAdmissionValue(grant)))
	return testProcessRuntime{processRuntime: next}, runtimeStartResult(result)
}

func (runtime testProcessRuntime) observeAttempt(generation attemptGeneration, observation attemptObservation) (testProcessRuntime, observationResult) {
	next, result := runtime.Observe(generation, processRuntimeObservation(observation))
	return testProcessRuntime{processRuntime: next}, runtimeReceipt(result)
}

func (runtime testProcessRuntime) commitTerminal(campaign campaignToken) (testProcessRuntime, terminalResult) {
	next, result := runtime.CommitTerminal(campaign)
	return testProcessRuntime{processRuntime: next}, terminalResult{
		decision: result.Decision(), epoch: fatalEpochID(result.Epoch()),
	}
}

func (runtime testProcessRuntime) authorizeForcedAbort(campaign campaignToken, epoch fatalEpochID) (testProcessRuntime, terminalResult) {
	next, result := runtime.AuthorizeForcedAbort(campaign, uint64(epoch))
	return testProcessRuntime{processRuntime: next}, terminalResult{
		decision: result.Decision(), epoch: fatalEpochID(result.Epoch()),
	}
}

func (runtime testProcessRuntime) sealAndBindConfirmationBarrier(binding barrierBinding) (testProcessRuntime, barrierResult) {
	next, result := runtime.BindConfirmationBarrier(processruntime.Barrier{
		Campaign: binding.campaign, Attempt: string(binding.attempt), Profile: binding.profile, Deadline: binding.deadline,
	})
	return testProcessRuntime{processRuntime: next}, runtimeBarrierResult(result)
}

func (runtime testProcessRuntime) completeConfirmationQueue(campaign campaignToken) (testProcessRuntime, confirmationQueueResult) {
	next, result := runtime.CompleteConfirmationQueue(campaign)
	return testProcessRuntime{processRuntime: next}, runtimeQueueResult(result)
}

func (runtime testProcessRuntime) closeRuntime(cause runtimeFatalCause) (testProcessRuntime, runtimeClosure) {
	next, result := runtime.Close(string(cause))
	return testProcessRuntime{processRuntime: next}, runtimeClosureValue(result)
}

func (runtime testProcessRuntime) settleEmergency(sweep emergencySweep) (testProcessRuntime, emergencySettlement) {
	next, result := runtime.SettleEmergency(processRuntimeResolutions(sweep))
	return testProcessRuntime{processRuntime: next}, runtimeEmergencySettlement(result)
}

func (runtime testProcessRuntime) residualCustody() []residualCustody {
	return runtimeResiduals(runtime.Residual())
}
