package simulation

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
	return result
}

func runtimeQueueResult(result processruntime.QueueResult) confirmationQueueResult {
	return result
}

func runtimeStartResult(result processruntime.StartResult) startCommittedResult {
	return result
}

func runtimeClosureValue(value processruntime.Closure) runtimeClosure {
	return value
}
