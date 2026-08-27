package ooze

import (
	"fmt"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
)

type simulationRuntimeObserver struct {
	recorder *simulationRecorder
	state    processruntime.Replay
}

func newSimulationRuntimeObserver(recorder *simulationRecorder, capacity int) *simulationRuntimeObserver {
	state := processruntime.NewReplay(capacity)
	recorder.beginRuntime(state)
	return &simulationRuntimeObserver{recorder: recorder, state: state}
}

func (observer *simulationRuntimeObserver) Observe(event processruntime.RecordedCut) {
	leave := observer.recorder.enter()
	defer leave()
	reservation := observer.recorder.reserve(simulationRuntimeAuthority)
	record := simulationRecord{runtimeCut: event}
	state, matches := observer.state.ApplyRecorded(event)
	if !matches {
		observer.recorder.recordRuntimeError(fmt.Errorf("process runtime event diverged"))
		return
	}
	observer.state = state
	observer.recorder.recordRuntime(reservation, record, state)
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
