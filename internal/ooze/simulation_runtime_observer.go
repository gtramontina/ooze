package ooze

import "fmt"

type simulationRuntimeObserver struct {
	recorder *simulationRecorder
	state    processRuntime
}

func newSimulationRuntimeObserver(recorder *simulationRecorder, capacity int) *simulationRuntimeObserver {
	return &simulationRuntimeObserver{recorder: recorder, state: newProcessRuntime(capacity)}
}

func (observer *simulationRuntimeObserver) Observe(event processRuntimeEvent) (err error) {
	leave := observer.recorder.enter()
	defer leave()
	reservation := observer.recorder.reserve(simulationRuntimeAuthority)
	record := simulationRuntimeEventRecord(event)
	state, err := simulationApplyRuntimeCut(observer.state, record, false)
	if err != nil {
		return fmt.Errorf("process runtime event diverged: %w", err)
	}
	observer.state = state
	observer.recorder.recordRuntime(reservation, record, state)

	return nil
}

func simulationRuntimeEventRecord(event processRuntimeEvent) simulationRecord {
	record := simulationRecord{}
	switch event := event.(type) {
	case runtimeCampaignRegistrationProcessed:
		record.runtimeOperation = processRuntimeRegisterCampaign
		record.runtimeProvenance = event.provenance
		record.runtimeRegistration = event.result
	case runtimeAdmissionRequestProcessed:
		record.runtimeOperation = processRuntimeRequestAdmission
		record.runtimeAdmission = simulationTraceAdmission(event.request)
		record.runtimeAdmissionOut = simulationTraceAdmissionResult(event.result)
	case runtimeAdmissionCancellationProcessed:
		record.runtimeOperation = processRuntimeCancelAdmission
		record.runtimeAdmissionToken = simulationTraceAdmission(event.request)
		record.runtimeAdmissionOut = simulationTraceAdmissionResult(event.result)
	case runtimeGrantReturnProcessed:
		record.runtimeOperation = processRuntimeAcknowledgeGrantReturn
		record.runtimeGrant = simulationTraceAdmission(event.grant)
		record.runtimeAdmissionOut = simulationTraceAdmissionResult(event.result)
	case runtimeConfirmationBarrierProcessed:
		record.runtimeOperation = processRuntimeBindConfirmationBarrier
		record.runtimeBarrier = simulationTraceBarrierBinding(event.barrier)
		record.runtimeBarrierOut = simulationTraceBarrierResult(event.result)
	case runtimeConfirmationQueueProcessed:
		record.runtimeOperation = processRuntimeCompleteConfirmationQueue
		record.runtimeCampaign = event.campaign
		record.runtimeQueueOut = simulationTraceConfirmationQueueResult(event.result)
	case runtimeStartCommitmentProcessed:
		record.runtimeOperation = processRuntimeStartCommitted
		record.runtimeGrant = simulationTraceAdmission(event.grant)
		record.runtimeStart = event.result
	case runtimeAttemptObservationProcessed:
		record.runtimeOperation = processRuntimeObserveAttempt
		record.runtimeGeneration = event.generation
		record.runtimeObservation = simulationTraceObservation(event.observation)
		record.runtimeObservationOut = simulationTraceObservationResult(event.result)
	case runtimeEmergencySettlementProcessed:
		record.runtimeOperation = processRuntimeSettleEmergency
		record.runtimeSweep = simulationTraceEmergencySweep(event.sweep)
		record.runtimeEmergencyOut = simulationTraceEmergencySettlement(event.result)
	case runtimeTerminalCommitmentProcessed:
		record.runtimeOperation = processRuntimeCommitTerminal
		record.runtimeCampaign = event.campaign
		record.runtimeTerminal = event.result
	case runtimeForcedAbortProcessed:
		record.runtimeOperation = processRuntimeAuthorizeForcedAbort
		record.runtimeCampaign = event.campaign
		record.runtimeFatalEpoch = event.epoch
		record.runtimeTerminal = event.result
	case runtimeClosureProcessed:
		record.runtimeOperation = processRuntimeClose
		record.runtimeFatalCause = event.cause
		record.runtimeClosure = simulationTraceRuntimeClosure(event.result)
	default:
		invariant("record runtime event", "runtime event variant is unknown")
	}
	return record
}
