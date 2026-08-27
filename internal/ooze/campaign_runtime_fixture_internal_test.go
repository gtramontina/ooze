package ooze

import (
	"testing"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/stretchr/testify/require"
)

const (
	sharedAdmission        = processruntime.SharedAdmission
	exclusiveAdmission     = processruntime.ExclusiveAdmission
	serialPrimaryAdmission = processruntime.SerialPrimaryAdmission
)

type campaignProvenance struct{ lineage campaignLineage }

type admissionAwait struct {
	decision processruntime.AdmissionDecision
	request  admissionRequestToken
	delivery <-chan admissionGrant
	fatal    fatalEpochID
}

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
	if prepared.result.decision == processruntime.StartAccepted {
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

func runtimeAdmissionByGeneration(image processruntime.Projection, generation attemptGeneration) (admissionAuthority, bool) {
	admission, found := image.Admission(generation)
	return runtimeAdmissionValue(admission), found
}

type campaignRuntimeFixture struct{ processruntime.Replay }

func newCampaignRuntimeFixture(capacity int) campaignRuntimeFixture {
	return campaignRuntimeFixture{processruntime.NewReplay(capacity)}
}

func campaignTokenForTest(lineage campaignLineage) campaignToken {
	_, registration := newCampaignRuntimeFixture(1).registerCampaign(campaignProvenance{lineage: lineage})
	return registration.token
}

func (runtime campaignRuntimeFixture) registerCampaign(provenance campaignProvenance) (campaignRuntimeFixture, campaignRegistration) {
	next, result := runtime.Apply(processruntime.RegisterCampaignCut(provenance.lineage))
	return campaignRuntimeFixture{next}, campaignRegistrationEvidence(result.Registration())
}

func (runtime campaignRuntimeFixture) requestAdmission(request admissionRequest) (campaignRuntimeFixture, admissionResult) {
	next, result := runtime.Apply(processruntime.RequestAdmissionCut(processRuntimeAdmission(campaignAdmissionValue(request))))
	return campaignRuntimeFixture{next}, runtimeAdmissionResult(result.Admission())
}

func (runtime campaignRuntimeFixture) cancelAdmission(request admissionRequest) (campaignRuntimeFixture, admissionResult) {
	next, result := runtime.Apply(processruntime.CancelAdmissionCut(processRuntimeAdmission(campaignAdmissionValue(request))))
	return campaignRuntimeFixture{next}, runtimeAdmissionResult(result.Admission())
}

func (runtime campaignRuntimeFixture) acknowledgeGrantReturn(grant admissionGrant) (campaignRuntimeFixture, admissionResult) {
	next, result := runtime.Apply(processruntime.ReturnGrantCut(processRuntimeAdmission(campaignAdmissionValue(grant))))
	return campaignRuntimeFixture{next}, runtimeAdmissionResult(result.Admission())
}

func (runtime campaignRuntimeFixture) startCommitted(grant admissionGrant) (campaignRuntimeFixture, startCommittedResult) {
	next, result := runtime.Apply(processruntime.CommitStartCut(processRuntimeAdmission(campaignAdmissionValue(grant))))
	return campaignRuntimeFixture{next}, runtimeStartResult(result.Start())
}

func (runtime campaignRuntimeFixture) observeAttempt(generation attemptGeneration, observation attemptObservation) (campaignRuntimeFixture, observationResult) {
	next, result := runtime.Apply(processruntime.ObserveAttemptCut(generation, processRuntimeObservation(observation)))
	return campaignRuntimeFixture{next}, runtimeReceipt(result.Receipt())
}

func (runtime campaignRuntimeFixture) commitTerminal(campaign campaignToken) (campaignRuntimeFixture, terminalResult) {
	next, result := runtime.Apply(processruntime.CommitTerminalCut(campaign))
	return campaignRuntimeFixture{next}, terminalResult{
		decision: result.Terminal().Decision(), epoch: fatalEpochID(result.Terminal().Epoch()),
	}
}

func (runtime campaignRuntimeFixture) authorizeForcedAbort(campaign campaignToken, epoch fatalEpochID) (campaignRuntimeFixture, terminalResult) {
	next, result := runtime.Apply(processruntime.AuthorizeForcedAbortCut(campaign, uint64(epoch)))
	return campaignRuntimeFixture{next}, terminalResult{
		decision: result.Terminal().Decision(), epoch: fatalEpochID(result.Terminal().Epoch()),
	}
}

func (runtime campaignRuntimeFixture) sealAndBindConfirmationBarrier(binding barrierBinding) (campaignRuntimeFixture, barrierResult) {
	next, result := runtime.Apply(processruntime.BindConfirmationBarrierCut(processruntime.Barrier{
		Campaign: binding.campaign, Attempt: string(binding.attempt), Profile: binding.profile, Deadline: binding.deadline,
	}))
	return campaignRuntimeFixture{next}, runtimeBarrierResult(result.Barrier())
}

func (runtime campaignRuntimeFixture) completeConfirmationQueue(campaign campaignToken) (campaignRuntimeFixture, confirmationQueueResult) {
	next, result := runtime.Apply(processruntime.CompleteConfirmationQueueCut(campaign))
	return campaignRuntimeFixture{next}, runtimeQueueResult(result.Queue())
}

func (runtime campaignRuntimeFixture) closeRuntime(cause runtimeFatalCause) (campaignRuntimeFixture, runtimeClosure) {
	next, result := runtime.Apply(processruntime.CloseCut(string(cause)))
	return campaignRuntimeFixture{next}, runtimeClosureValue(result.Closure())
}

func (runtime campaignRuntimeFixture) settleEmergency(sweep emergencySweep) (campaignRuntimeFixture, emergencySettlement) {
	next, result := runtime.Apply(processruntime.SettleEmergencyCut(processRuntimeResolutions(sweep)))
	return campaignRuntimeFixture{next}, runtimeEmergencySettlement(result.Settlement())
}
