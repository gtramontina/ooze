package ooze

import (
	"slices"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
)

type simulationAdmission struct {
	campaign campaignToken
	attempt  attemptIdentity
	class    processruntime.AdmissionClass
	profile  Profile
	deadline simulationDuration
}

func simulationTraceAdmission(value admissionAuthority) simulationAdmission {
	return simulationAdmission{
		campaign: value.campaign, attempt: value.attempt, class: value.class,
		profile: value.profile, deadline: simulationTraceDuration(value.deadline),
	}
}

func (admission simulationAdmission) production() admissionAuthority {
	return admissionAuthority{
		campaign: admission.campaign, attempt: admission.attempt, class: admission.class,
		profile: admission.profile, deadline: admission.deadline.production(),
	}
}

type simulationRuntimeObservationKind uint8

const (
	simulationLaunchOwnedObservation simulationRuntimeObservationKind = iota + 1
	simulationLaunchNotReleasedObservation
	simulationAttemptSettledObservation
	simulationAttemptTrippedObservation
	simulationLaunchUnconfirmedObservation
	simulationDrainUnconfirmedObservation
	simulationAttemptStoppedObservation
	simulationAttemptInfrastructureObservation
)

type simulationRuntimeObservation struct {
	kind     simulationRuntimeObservationKind
	reason   launchNotReleasedReason
	profile  Profile
	deadline simulationDuration
	trip     attemptTripKind
	cause    string
}

func simulationTraceObservation(value attemptObservation) simulationRuntimeObservation {
	switch observation := value.(type) {
	case launchOwned:
		return simulationRuntimeObservation{kind: simulationLaunchOwnedObservation}
	case launchNotReleased:
		return simulationRuntimeObservation{kind: simulationLaunchNotReleasedObservation, reason: observation.reason}
	case attemptSettled:
		return simulationRuntimeObservation{
			kind: simulationAttemptSettledObservation, profile: observation.profile,
			deadline: simulationTraceDuration(observation.deadline),
		}
	case attemptTripped:
		return simulationRuntimeObservation{
			kind: simulationAttemptTrippedObservation, trip: observation.kind,
			profile: observation.profile, deadline: simulationTraceDuration(observation.deadline),
		}
	case launchUnconfirmed:
		return simulationRuntimeObservation{kind: simulationLaunchUnconfirmedObservation}
	case drainUnconfirmed:
		return simulationRuntimeObservation{kind: simulationDrainUnconfirmedObservation}
	case attemptStopped:
		return simulationRuntimeObservation{kind: simulationAttemptStoppedObservation}
	case attemptInfrastructure:
		return simulationRuntimeObservation{kind: simulationAttemptInfrastructureObservation, cause: observation.cause}
	default:
		return simulationRuntimeObservation{}
	}
}

func (observation simulationRuntimeObservation) production() attemptObservation {
	switch observation.kind {
	case simulationLaunchOwnedObservation:
		return launchOwned{}
	case simulationLaunchNotReleasedObservation:
		return launchNotReleased{reason: observation.reason}
	case simulationAttemptSettledObservation:
		return attemptSettled{profile: observation.profile, deadline: observation.deadline.production()}
	case simulationAttemptTrippedObservation:
		return attemptTripped{
			kind: observation.trip, profile: observation.profile, deadline: observation.deadline.production(),
		}
	case simulationLaunchUnconfirmedObservation:
		return launchUnconfirmed{}
	case simulationDrainUnconfirmedObservation:
		return drainUnconfirmed{}
	case simulationAttemptStoppedObservation:
		return attemptStopped{}
	case simulationAttemptInfrastructureObservation:
		return attemptInfrastructure{cause: observation.cause}
	default:
		return nil
	}
}

type simulationAdmissionResult struct {
	decision   processruntime.AdmissionDecision
	request    simulationAdmission
	deliveries []simulationAdmission
	fatalEpoch fatalEpochID
}

func simulationTraceAdmissionResult(value admissionResult) simulationAdmissionResult {
	deliveries := make([]simulationAdmission, len(value.deliveries))
	for index, delivery := range value.deliveries {
		deliveries[index] = simulationTraceAdmission(delivery)
	}

	return simulationAdmissionResult{
		decision: value.decision, request: simulationTraceAdmission(value.request),
		deliveries: deliveries, fatalEpoch: value.fatalEpoch,
	}
}

type simulationObservationResult struct {
	generation                                      attemptGeneration
	deliveries                                      []simulationAdmission
	cancelledWaiting, compensatedGrants             []simulationAdmission
	settlementAcknowledged, confirmationProvisional bool
	pressureTransitioned, runtimeClosureInProgress  bool
	confirmationObserved, confirmationQueueDrained  bool
	fatalEpoch                                      fatalEpochID
}

func simulationTraceObservationResult(value observationResult) simulationObservationResult {
	result := simulationObservationResult{
		generation:               value.generation,
		settlementAcknowledged:   value.settlementAcknowledged,
		confirmationProvisional:  value.confirmationProvisional,
		pressureTransitioned:     value.pressureTransitioned,
		runtimeClosureInProgress: value.runtimeClosureInProgress,
		confirmationObserved:     value.confirmationObserved,
		confirmationQueueDrained: value.confirmationQueueDrained,
		fatalEpoch:               value.fatalEpoch,
	}
	result.deliveries = simulationTraceAdmissions(value.deliveries)
	result.cancelledWaiting = simulationTraceAdmissions(value.cancelledWaiting)
	result.compensatedGrants = simulationTraceAdmissions(value.compensatedGrants)

	return result
}

func simulationTraceAdmissions[Values ~[]admissionAuthority](values Values) []simulationAdmission {
	result := make([]simulationAdmission, len(values))
	for index, value := range values {
		result[index] = simulationTraceAdmission(value)
	}

	return result
}

type simulationBarrierBinding struct {
	campaign campaignToken
	attempt  attemptIdentity
	profile  Profile
	deadline simulationDuration
}

func simulationTraceBarrierBinding(value barrierBinding) simulationBarrierBinding {
	return simulationBarrierBinding{
		campaign: value.campaign, attempt: value.attempt, profile: value.profile,
		deadline: simulationTraceDuration(value.deadline),
	}
}

func (binding simulationBarrierBinding) production() barrierBinding {
	return barrierBinding{
		campaign: binding.campaign, attempt: binding.attempt,
		profile: binding.profile, deadline: binding.deadline.production(),
	}
}

type simulationBarrierResult struct {
	decision   processruntime.BarrierDecision
	request    simulationAdmission
	deliveries []simulationAdmission
}

func simulationTraceBarrierResult(value barrierResult) simulationBarrierResult {
	return simulationBarrierResult{
		decision: value.decision, request: simulationTraceAdmission(value.request),
		deliveries: simulationTraceAdmissions(value.deliveries),
	}
}

type simulationConfirmationQueueResult struct {
	decision   processruntime.QueueDecision
	deliveries []simulationAdmission
}

func simulationTraceConfirmationQueueResult(value confirmationQueueResult) simulationConfirmationQueueResult {
	return simulationConfirmationQueueResult{
		decision: value.decision, deliveries: simulationTraceAdmissions(value.deliveries),
	}
}

type simulationRuntimeState = processruntime.Projection

func simulationTraceRuntimeState(value processruntime.Replay) simulationRuntimeState {
	return value.Projection()
}

type simulationEmergencySweepRecord struct{ resolutions []emergencyResolution }

func simulationTraceEmergencySweep(value emergencySweep) simulationEmergencySweepRecord {
	return simulationEmergencySweepRecord{resolutions: slices.Clone(value.resolutions)}
}

func (sweep simulationEmergencySweepRecord) production() emergencySweep {
	return emergencySweep{resolutions: slices.Clone(sweep.resolutions)}
}

type simulationEmergencySettlement struct {
	epoch        fatalEpochID
	owner        campaignToken
	acknowledged []attemptGeneration
	residual     []residualCustody
}

func simulationTraceEmergencySettlement(value emergencySettlement) simulationEmergencySettlement {
	return simulationEmergencySettlement{
		epoch: value.epoch, owner: value.owner,
		acknowledged: slices.Clone(value.acknowledged), residual: slices.Clone(value.residual),
	}
}

type simulationRuntimeClosure struct {
	epoch                               fatalEpochID
	cancelledWaiting, compensatedGrants []simulationAdmission
	residual                            []residualCustody
}

func simulationTraceRuntimeClosure(value runtimeClosure) simulationRuntimeClosure {
	return simulationRuntimeClosure{
		epoch:             value.epoch,
		cancelledWaiting:  simulationTraceAdmissions(value.cancelledWaiting),
		compensatedGrants: simulationTraceAdmissions(value.compensatedGrants),
		residual:          slices.Clone(value.residual),
	}
}
