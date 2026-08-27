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

func campaignTokenForTest(lineage campaignLineage) campaignToken {
	_, result := processruntime.NewReplay(1).Apply(processruntime.RegisterCampaignCut(lineage))
	return result.Registration().Campaign()
}
