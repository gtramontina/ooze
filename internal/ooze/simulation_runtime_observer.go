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

func runtimeTerminalResult(result processruntime.TerminalResult) terminalResult {
	return terminalResult{decision: result.Decision(), epoch: fatalEpochID(result.Epoch())}
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

func runtimeClosureValue(value processruntime.Closure) runtimeClosure {
	return runtimeClosure{
		epoch: fatalEpochID(value.Epoch()), cancelledWaiting: runtimeAdmissions(value.CancelledWaiting()),
		compensatedGrants: runtimeAdmissions(value.CompensatedGrants()), residual: runtimeResiduals(value.Residual()),
	}
}
