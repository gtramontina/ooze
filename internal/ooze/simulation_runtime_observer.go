package ooze

import (
	"fmt"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
)

type simulationRuntimeObserver struct {
	recorder *simulationRecorder
	state    processRuntime
}

func newSimulationRuntimeObserver(recorder *simulationRecorder, capacity int) *simulationRuntimeObserver {
	return &simulationRuntimeObserver{recorder: recorder, state: newProcessRuntime(capacity)}
}

func (observer *simulationRuntimeObserver) Begin() func(processruntime.Event) {
	leave := observer.recorder.enter()
	reservation := observer.recorder.reserve(simulationRuntimeAuthority)
	return func(event processruntime.Event) {
		record := simulationRuntimeEventRecord(event)
		state, err := simulationApplyRuntimeCut(observer.state, record, false)
		if err != nil {
			leave()
			panic(fmt.Sprintf("process runtime event diverged: %v", err))
		}
		observer.state = state
		observer.recorder.recordRuntime(reservation, record, state)
		leave()
	}
}

func simulationRuntimeEventRecord(event processruntime.Event) simulationRecord {
	command, result := event.Command(), event.Result()
	record := simulationRecord{runtimeOperation: processRuntimeOperation(event.Kind())}
	switch event.Kind() {
	case processruntime.RegisterCampaign:
		record.runtimeProvenance = campaignProvenance{lineage: campaignLineage(command.Lineage)}
		record.runtimeRegistration = campaignRegistration{
			decision: campaignDecision(result.Registration.Decision),
			token:    simulationRuntimeCampaign(result.Registration.Campaign),
		}
	case processruntime.RequestAdmission:
		record.runtimeAdmission = simulationRuntimeAdmission(command.Admission)
		record.runtimeAdmissionOut = simulationRuntimeAdmissionResult(result.Admission)
	case processruntime.CancelAdmission:
		record.runtimeAdmissionToken = simulationRuntimeAdmission(command.Admission)
		record.runtimeAdmissionOut = simulationRuntimeAdmissionResult(result.Admission)
	case processruntime.AcknowledgeGrantReturn:
		record.runtimeGrant = simulationRuntimeAdmission(command.Admission)
		record.runtimeAdmissionOut = simulationRuntimeAdmissionResult(result.Admission)
	case processruntime.BindConfirmationBarrier:
		record.runtimeBarrier = simulationBarrierBinding{
			campaign: simulationRuntimeCampaign(command.Barrier.Campaign),
			attempt:  attemptIdentity(command.Barrier.Attempt), profile: Profile(command.Barrier.Profile),
			deadline: simulationDuration(command.Barrier.Deadline),
		}
		record.runtimeBarrierOut = simulationRuntimeBarrierResult(result.Barrier)
	case processruntime.CompleteConfirmationQueue:
		record.runtimeCampaign = simulationRuntimeCampaign(command.Campaign)
		record.runtimeQueueOut = simulationConfirmationQueueResult{
			decision:   confirmationQueueDecision(result.Queue.Decision),
			deliveries: simulationRuntimeAdmissions(result.Queue.Deliveries),
		}
	case processruntime.StartCommitted:
		record.runtimeGrant = simulationRuntimeAdmission(command.Admission)
		record.runtimeStart = startCommittedResult{
			decision:                 startCommittedDecision(result.Start.Decision),
			generation:               attemptGeneration(result.Start.Generation),
			settlementAcknowledged:   result.Start.SettlementAcknowledged,
			runtimeClosureInProgress: result.Start.RuntimeClosureInProgress,
		}
	case processruntime.ObserveAttempt:
		record.runtimeGeneration = attemptGeneration(command.Generation)
		record.runtimeObservation = simulationRuntimeObservation{
			kind:    simulationRuntimeObservationKind(command.Observation.Kind),
			reason:  launchNotReleasedReason(command.Observation.Reason),
			profile: Profile(command.Observation.Profile), deadline: simulationDuration(command.Observation.Deadline),
			trip: attemptTripKind(command.Observation.Trip), cause: command.Observation.Cause,
		}
		record.runtimeObservationOut = simulationRuntimeObservationResult(result.Observation)
	case processruntime.SettleEmergency:
		resolutions := make([]emergencyResolution, len(command.Resolutions))
		for index, resolution := range command.Resolutions {
			resolutions[index] = emergencyResolution{
				generation:  attemptGeneration(resolution.Generation),
				disposition: emergencyDisposition(resolution.Disposition),
			}
		}
		record.runtimeSweep = simulationEmergencySweepRecord{resolutions: resolutions}
		record.runtimeEmergencyOut = simulationRuntimeEmergencyResult(result.Emergency)
	case processruntime.CommitTerminal:
		record.runtimeCampaign = simulationRuntimeCampaign(command.Campaign)
		record.runtimeTerminal = terminalResult{decision: terminalDecision(result.Terminal.Decision), epoch: fatalEpochID(result.Terminal.Epoch)}
	case processruntime.AuthorizeForcedAbort:
		record.runtimeCampaign = simulationRuntimeCampaign(command.Campaign)
		record.runtimeFatalEpoch = fatalEpochID(command.FatalEpoch)
		record.runtimeTerminal = terminalResult{decision: terminalDecision(result.Terminal.Decision), epoch: fatalEpochID(result.Terminal.Epoch)}
	case processruntime.Close:
		record.runtimeFatalCause = runtimeFatalCause(command.FatalCause)
		record.runtimeClosure = simulationRuntimeClosureResult(result.Closure)
	}
	return record
}

func simulationRuntimeCampaign(campaign processruntime.Campaign) campaignToken {
	return campaignToken{id: campaignID(campaign.ID), lineage: campaignLineage(campaign.Lineage)}
}

func simulationRuntimeAdmission(admission processruntime.Admission) simulationAdmission {
	return simulationAdmission{
		campaign: simulationRuntimeCampaign(admission.Campaign), attempt: attemptIdentity(admission.Attempt),
		class: admissionClass(admission.Class), profile: Profile(admission.Profile),
		deadline: simulationDuration(admission.Deadline),
	}
}

func simulationRuntimeAdmissions(admissions []processruntime.Admission) []simulationAdmission {
	result := make([]simulationAdmission, len(admissions))
	for index, admission := range admissions {
		result[index] = simulationRuntimeAdmission(admission)
	}
	return result
}

func simulationRuntimeAdmissionResult(result processruntime.AdmissionResult) simulationAdmissionResult {
	return simulationAdmissionResult{
		decision: admissionDecision(result.Decision), request: simulationRuntimeAdmission(result.Request),
		deliveries: simulationRuntimeAdmissions(result.Deliveries), fatalEpoch: fatalEpochID(result.FatalEpoch),
	}
}

func simulationRuntimeBarrierResult(result processruntime.BarrierResult) simulationBarrierResult {
	return simulationBarrierResult{
		decision: barrierDecision(result.Decision), request: simulationRuntimeAdmission(result.Request),
		deliveries: simulationRuntimeAdmissions(result.Deliveries),
	}
}

func simulationRuntimeObservationResult(result processruntime.ObservationResult) simulationObservationResult {
	return simulationObservationResult{
		generation: attemptGeneration(result.Generation), deliveries: simulationRuntimeAdmissions(result.Deliveries),
		cancelledWaiting: simulationRuntimeAdmissions(result.CancelledWaiting), compensatedGrants: simulationRuntimeAdmissions(result.CompensatedGrants),
		settlementAcknowledged: result.SettlementAcknowledged, confirmationProvisional: result.ConfirmationProvisional,
		pressureTransitioned: result.PressureTransitioned, runtimeClosureInProgress: result.RuntimeClosureInProgress,
		confirmationObserved: result.ConfirmationObserved, confirmationQueueDrained: result.ConfirmationQueueDrained,
		fatalEpoch: fatalEpochID(result.FatalEpoch),
	}
}

func simulationRuntimeResiduals(residuals []processruntime.Residual) []residualCustody {
	result := make([]residualCustody, len(residuals))
	for index, residual := range residuals {
		result[index] = residualCustody{
			generation: attemptGeneration(residual.Generation), attempt: attemptIdentity(residual.Attempt),
			stage: admissionStage(residual.Stage), transferred: residual.Transferred,
		}
	}
	return result
}

func simulationRuntimeEmergencyResult(result processruntime.EmergencyResult) simulationEmergencySettlement {
	acknowledged := make([]attemptGeneration, len(result.Acknowledged))
	for index, generation := range result.Acknowledged {
		acknowledged[index] = attemptGeneration(generation)
	}
	return simulationEmergencySettlement{
		epoch: fatalEpochID(result.Epoch), owner: simulationRuntimeCampaign(result.Owner),
		acknowledged: acknowledged, residual: simulationRuntimeResiduals(result.Residual),
	}
}

func simulationRuntimeClosureResult(result processruntime.ClosureResult) simulationRuntimeClosure {
	return simulationRuntimeClosure{
		epoch: fatalEpochID(result.Epoch), cancelledWaiting: simulationRuntimeAdmissions(result.CancelledWaiting),
		compensatedGrants: simulationRuntimeAdmissions(result.CompensatedGrants),
		residual:          simulationRuntimeResiduals(result.Residual),
	}
}
