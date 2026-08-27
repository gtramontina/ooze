package ooze

import (
	"fmt"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
)

type simulationRuntimeObserver struct {
	recorder *simulationRecorder
	state    processruntime.Replay
	mismatch error
}

func newSimulationRuntimeObserver(recorder *simulationRecorder, capacity int) *simulationRuntimeObserver {
	return &simulationRuntimeObserver{recorder: recorder, state: processruntime.NewReplay(capacity)}
}

func (observer *simulationRuntimeObserver) Observe(event processruntime.Event) {
	leave := observer.recorder.enter()
	defer leave()
	reservation := observer.recorder.reserve(simulationRuntimeAuthority)
	record := simulationRuntimeEventRecord(event)
	state, matches := observer.state.ApplyEvent(event)
	if !matches {
		observer.mismatch = fmt.Errorf("process runtime event diverged")
		return
	}
	observer.state = state
	observer.recorder.recordRuntime(reservation, record, state)

}

func simulationRuntimeEventRecord(event processruntime.Event) simulationRecord {
	record := simulationRecord{}
	switch event := event.(type) {
	case processruntime.CampaignRegistrationProcessed:
		record.runtimeOperation = processruntime.RegisterCampaignOperation
		record.runtimeProvenance = campaignProvenance{lineage: event.Lineage()}
		record.runtimeRegistration = campaignRegistrationEvidence(event.Registration())
	case processruntime.AdmissionRequestProcessed:
		record.runtimeOperation = processruntime.RequestAdmissionOperation
		record.runtimeAdmission = simulationTraceAdmission(runtimeAdmissionValue(event.Admission()))
		record.runtimeAdmissionOut = simulationTraceAdmissionResult(runtimeAdmissionResult(event.Result()))
	case processruntime.AdmissionCancellationProcessed:
		record.runtimeOperation = processruntime.CancelAdmissionOperation
		record.runtimeAdmissionToken = simulationTraceAdmission(runtimeAdmissionValue(event.Admission()))
		record.runtimeAdmissionOut = simulationTraceAdmissionResult(runtimeAdmissionResult(event.Result()))
	case processruntime.GrantReturnProcessed:
		record.runtimeOperation = processruntime.ReturnGrantOperation
		record.runtimeGrant = simulationTraceAdmission(runtimeAdmissionValue(event.Admission()))
		record.runtimeAdmissionOut = simulationTraceAdmissionResult(runtimeAdmissionResult(event.Result()))
	case processruntime.ConfirmationBarrierProcessed:
		record.runtimeOperation = processruntime.BindConfirmationBarrierOperation
		record.runtimeBarrier = simulationTraceBarrierBinding(runtimeBarrier(event.Barrier()))
		record.runtimeBarrierOut = simulationTraceBarrierResult(runtimeBarrierResult(event.Result()))
	case processruntime.ConfirmationQueueProcessed:
		record.runtimeOperation = processruntime.CompleteConfirmationQueueOperation
		record.runtimeCampaign = event.Campaign()
		record.runtimeQueueOut = simulationTraceConfirmationQueueResult(runtimeQueueResult(event.Result()))
	case processruntime.StartCommitmentProcessed:
		record.runtimeOperation = processruntime.CommitStartOperation
		record.runtimeGrant = simulationTraceAdmission(runtimeAdmissionValue(event.Grant()))
		record.runtimeStart = runtimeStartResult(event.Result())
	case processruntime.AttemptObservationProcessed:
		record.runtimeOperation = processruntime.ObserveAttemptOperation
		record.runtimeGeneration = event.Generation()
		record.runtimeObservation = simulationTraceObservation(runtimeObservation(event.Observation()))
		record.runtimeObservationOut = simulationTraceObservationResult(runtimeReceipt(event.Result()))
	case processruntime.EmergencySettlementProcessed:
		record.runtimeOperation = processruntime.SettleEmergencyOperation
		record.runtimeSweep = simulationTraceEmergencySweep(runtimeSweep(event.Resolutions()))
		record.runtimeEmergencyOut = simulationTraceEmergencySettlement(runtimeEmergencySettlement(event.Result()))
	case processruntime.TerminalCommitmentProcessed:
		record.runtimeOperation = processruntime.CommitTerminalOperation
		record.runtimeCampaign = event.Campaign()
		record.runtimeTerminal = terminalResult{decision: event.Result().Decision()}
	case processruntime.ForcedAbortProcessed:
		record.runtimeOperation = processruntime.AuthorizeForcedAbortOperation
		record.runtimeCampaign = event.Campaign()
		record.runtimeFatalEpoch = fatalEpochID(event.Epoch())
		record.runtimeTerminal = terminalResult{decision: event.Result().Decision()}
	case processruntime.RuntimeClosureProcessed:
		record.runtimeOperation = processruntime.CloseOperation
		record.runtimeFatalCause = runtimeFatalCause(event.Cause())
		record.runtimeClosure = simulationTraceRuntimeClosure(runtimeClosureValue(event.Result()))
	default:
		invariant("record runtime event", "runtime event variant is unknown")
	}
	return record
}

func runtimeAdmissionResult(result processruntime.AdmissionResult) admissionResult {
	return admissionResult{
		decision: result.Decision(), request: runtimeAdmissionValue(result.Request()),
		deliveries: runtimeAdmissions(result.Deliveries()), fatalEpoch: fatalEpochID(result.FatalEpoch()),
	}
}

func runtimeAdmissions(values []processruntime.Admission) []admissionAuthority {
	result := make([]admissionAuthority, len(values))
	for index, value := range values {
		result[index] = runtimeAdmissionValue(value)
	}
	return result
}

func runtimeBarrier(value processruntime.Barrier) barrierBinding {
	return barrierBinding{
		campaign: value.Campaign, attempt: attemptIdentity(value.Attempt),
		profile: value.Profile, deadline: value.Deadline,
	}
}

func runtimeBarrierResult(result processruntime.BarrierResult) barrierResult {
	return barrierResult{
		decision: result.Decision(), request: runtimeAdmissionValue(result.Request()),
		deliveries: runtimeAdmissions(result.Deliveries()),
	}
}

func runtimeQueueResult(result processruntime.QueueResult) confirmationQueueResult {
	return confirmationQueueResult{decision: result.Decision(), deliveries: runtimeAdmissions(result.Deliveries())}
}

func runtimeStartResult(result processruntime.StartResult) startCommittedResult {
	return startCommittedResult{
		decision: result.Decision(), generation: result.Generation(),
		settlementAcknowledged:   result.SettlementAcknowledged(),
		runtimeClosureInProgress: result.RuntimeClosureInProgress(),
	}
}

func runtimeObservation(value processruntime.Observation) attemptObservation {
	switch value.Kind() {
	case processruntime.LaunchOwned:
		return launchOwned{}
	case processruntime.LaunchNotReleased:
		reason := launchFailed
		if value.ResourceExhausted() {
			reason = launchResourceExhausted
		}
		return launchNotReleased{reason: reason}
	case processruntime.AttemptSettled:
		return attemptSettled{profile: value.Profile(), deadline: value.Deadline()}
	case processruntime.AttemptTripped:
		kind := deadlineTrip
		if value.FuseTrip() {
			kind = fuseTrip
		}
		return attemptTripped{kind: kind, profile: value.Profile(), deadline: value.Deadline()}
	case processruntime.LaunchUnconfirmedKind:
		return launchUnconfirmed{}
	case processruntime.DrainUnconfirmedKind:
		return drainUnconfirmed{}
	case processruntime.AttemptStopped:
		return attemptStopped{}
	case processruntime.AttemptInfrastructure:
		return attemptInfrastructure{cause: value.Cause()}
	default:
		return nil
	}
}

func runtimeReceipt(receipt processruntime.Receipt) observationResult {
	return observationResult{
		generation: receipt.Generation(), deliveries: runtimeAdmissions(receipt.Deliveries()),
		cancelledWaiting:         runtimeAdmissions(receipt.CancelledWaiting()),
		compensatedGrants:        runtimeAdmissions(receipt.CompensatedGrants()),
		settlementAcknowledged:   receipt.SettlementAcknowledged(),
		confirmationProvisional:  receipt.ConfirmationProvisional(),
		pressureTransitioned:     receipt.PressureTransitioned(),
		runtimeClosureInProgress: receipt.RuntimeClosureInProgress(),
		confirmationObserved:     receipt.ConfirmationObserved(),
		confirmationQueueDrained: receipt.ConfirmationQueueDrained(), fatalEpoch: fatalEpochID(receipt.FatalEpoch()),
	}
}

func runtimeSweep(values []processruntime.Resolution) emergencySweep {
	result := emergencySweep{resolutions: make([]emergencyResolution, len(values))}
	for index, value := range values {
		disposition := emergencyConfirmedDrained
		if value.Transferred() {
			disposition = emergencyCustodyTransferred
		}
		result.resolutions[index] = emergencyResolution{generation: value.Generation(), disposition: disposition}
	}
	return result
}

func runtimeClosureValue(value processruntime.Closure) runtimeClosure {
	return runtimeClosure{
		epoch: fatalEpochID(value.Epoch()), cancelledWaiting: runtimeAdmissions(value.CancelledWaiting()),
		compensatedGrants: runtimeAdmissions(value.CompensatedGrants()), residual: runtimeResiduals(value.Residual()),
	}
}
