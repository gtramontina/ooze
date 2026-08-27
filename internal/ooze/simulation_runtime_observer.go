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

func (observer *simulationRuntimeObserver) Observe(event processruntime.Event) (err error) {
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

func simulationRuntimeEventRecord(event processruntime.Event) simulationRecord {
	record := simulationRecord{}
	switch event := event.Variant().(type) {
	case processruntime.CampaignRegistrationProcessed:
		result := event.Registration()
		record.runtimeOperation = processRuntimeRegisterCampaign
		record.runtimeProvenance = campaignProvenance{lineage: campaignLineage(event.Lineage())}
		record.runtimeRegistration = campaignRegistration{
			decision: campaignDecision(result.Decision),
			token:    simulationRuntimeCampaign(result.Campaign),
		}
	case processruntime.AdmissionRequestProcessed:
		record.runtimeOperation = processRuntimeRequestAdmission
		record.runtimeAdmission = simulationRuntimeAdmission(event.Admission())
		record.runtimeAdmissionOut = simulationRuntimeAdmissionResult(event.Result())
	case processruntime.AdmissionCancellationProcessed:
		record.runtimeOperation = processRuntimeCancelAdmission
		record.runtimeAdmissionToken = simulationRuntimeAdmission(event.Admission())
		record.runtimeAdmissionOut = simulationRuntimeAdmissionResult(event.Result())
	case processruntime.GrantReturnProcessed:
		record.runtimeOperation = processRuntimeAcknowledgeGrantReturn
		record.runtimeGrant = simulationRuntimeAdmission(event.Admission())
		record.runtimeAdmissionOut = simulationRuntimeAdmissionResult(event.Result())
	case processruntime.ConfirmationBarrierProcessed:
		barrier := event.Barrier()
		record.runtimeOperation = processRuntimeBindConfirmationBarrier
		record.runtimeBarrier = simulationBarrierBinding{
			campaign: simulationRuntimeCampaign(barrier.Campaign),
			attempt:  attemptIdentity(barrier.Attempt), profile: Profile(barrier.Profile),
			deadline: simulationDuration(barrier.Deadline),
		}
		record.runtimeBarrierOut = simulationRuntimeBarrierResult(event.Result())
	case processruntime.ConfirmationQueueProcessed:
		result := event.Result()
		record.runtimeOperation = processRuntimeCompleteConfirmationQueue
		record.runtimeCampaign = simulationRuntimeCampaign(event.Campaign())
		record.runtimeQueueOut = simulationConfirmationQueueResult{
			decision: confirmationQueueDecision(result.Decision), deliveries: simulationRuntimeAdmissions(result.Deliveries),
		}
	case processruntime.StartCommitmentProcessed:
		result := event.Result()
		record.runtimeOperation = processRuntimeStartCommitted
		record.runtimeGrant = simulationRuntimeAdmission(event.Grant())
		record.runtimeStart = startCommittedResult{
			decision: startCommittedDecision(result.Decision), generation: attemptGeneration(result.Generation),
			settlementAcknowledged: result.SettlementAcknowledged, runtimeClosureInProgress: result.RuntimeClosureInProgress,
		}
	case processruntime.AttemptObservationProcessed:
		observation := event.Observation()
		record.runtimeOperation = processRuntimeObserveAttempt
		record.runtimeGeneration = attemptGeneration(event.Generation())
		record.runtimeObservation = simulationRuntimeObservation{
			kind: simulationRuntimeObservationKind(observation.Kind), reason: launchNotReleasedReason(observation.Reason),
			profile: Profile(observation.Profile), deadline: simulationDuration(observation.Deadline),
			trip: attemptTripKind(observation.Trip), cause: observation.Cause,
		}
		record.runtimeObservationOut = simulationRuntimeObservationResult(event.Result())
	case processruntime.EmergencySettlementProcessed:
		resolutions := make([]emergencyResolution, len(event.Resolutions()))
		for index, resolution := range event.Resolutions() {
			resolutions[index] = emergencyResolution{
				generation:  attemptGeneration(resolution.Generation),
				disposition: emergencyDisposition(resolution.Disposition),
			}
		}
		record.runtimeOperation = processRuntimeSettleEmergency
		record.runtimeSweep = simulationEmergencySweepRecord{resolutions: resolutions}
		record.runtimeEmergencyOut = simulationRuntimeEmergencyResult(event.Result())
	case processruntime.TerminalCommitmentProcessed:
		result := event.Result()
		record.runtimeOperation = processRuntimeCommitTerminal
		record.runtimeCampaign = simulationRuntimeCampaign(event.Campaign())
		record.runtimeTerminal = terminalResult{decision: terminalDecision(result.Decision), epoch: fatalEpochID(result.Epoch)}
	case processruntime.ForcedAbortProcessed:
		result := event.Result()
		record.runtimeOperation = processRuntimeAuthorizeForcedAbort
		record.runtimeCampaign = simulationRuntimeCampaign(event.Campaign())
		record.runtimeFatalEpoch = fatalEpochID(event.Epoch())
		record.runtimeTerminal = terminalResult{decision: terminalDecision(result.Decision), epoch: fatalEpochID(result.Epoch)}
	case processruntime.RuntimeClosureProcessed:
		record.runtimeOperation = processRuntimeClose
		record.runtimeFatalCause = runtimeFatalCause(event.Cause())
		record.runtimeClosure = simulationRuntimeClosureResult(event.Result())
	default:
		invariant("record runtime event", "runtime event variant is unknown")
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
