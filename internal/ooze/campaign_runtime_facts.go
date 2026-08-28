package ooze

import "github.com/gtramontina/ooze/internal/ooze/internal/processruntime"

func campaignAdmissionFact(authority processruntime.Admission) campaignAdmission {
	return campaignAdmission{
		campaign: authority.Campaign, attempt: attemptIdentity(authority.Attempt), class: authority.Class,
		profile: authority.Profile, deadline: authority.Deadline,
	}
}

func campaignAdmissions(authorities []processruntime.Admission) []campaignAdmission {
	facts := make([]campaignAdmission, len(authorities))
	for index, authority := range authorities {
		facts[index] = campaignAdmissionFact(authority)
	}

	return facts
}

func campaignAdmissionEvidence(result admissionResult) campaignAdmissionResult {
	return campaignAdmissionResult{
		decision: result.decision, request: campaignAdmissionValue(result.request), fatalEpoch: result.fatalEpoch,
	}
}

func campaignAdmissionValue(authority admissionAuthority) campaignAdmission {
	return campaignAdmission(authority)
}

func campaignRegistrationEvidence(result processruntime.Registration) campaignRegistration {
	return campaignRegistration{decision: result.Decision(), token: result.Campaign()}
}

func campaignStartEvidence(result startCommittedResult) campaignStartResult {
	return campaignStartResult(result)
}

func campaignReceipt(result processruntime.Receipt) campaignRuntimeReceipt {
	return campaignRuntimeReceipt{
		generation:               result.Generation(),
		cancelledWaiting:         campaignAdmissions(result.CancelledWaiting()),
		compensatedGrants:        campaignAdmissions(result.CompensatedGrants()),
		settlementAcknowledged:   result.SettlementAcknowledged(),
		confirmationProvisional:  result.ConfirmationProvisional(),
		pressureTransitioned:     result.PressureTransitioned(),
		runtimeClosureInProgress: result.RuntimeClosureInProgress(),
		confirmationObserved:     result.ConfirmationObserved(),
		confirmationQueueDrained: result.ConfirmationQueueDrained(),
		fatalEpoch:               fatalEpochID(result.FatalEpoch()),
	}
}

func campaignReceiptValue(result observationResult) campaignRuntimeReceipt {
	return campaignRuntimeReceipt{
		generation: result.generation, cancelledWaiting: campaignAdmissionValues(result.cancelledWaiting),
		compensatedGrants:        campaignAdmissionValues(result.compensatedGrants),
		settlementAcknowledged:   result.settlementAcknowledged,
		confirmationProvisional:  result.confirmationProvisional,
		pressureTransitioned:     result.pressureTransitioned,
		runtimeClosureInProgress: result.runtimeClosureInProgress,
		confirmationObserved:     result.confirmationObserved,
		confirmationQueueDrained: result.confirmationQueueDrained, fatalEpoch: result.fatalEpoch,
	}
}

func campaignAdmissionValues(values []admissionAuthority) []campaignAdmission {
	result := make([]campaignAdmission, len(values))
	for index, value := range values {
		result[index] = campaignAdmissionValue(value)
	}
	return result
}

func campaignResiduals(residual []processruntime.Residual) []campaignResidualCustody {
	facts := make([]campaignResidualCustody, len(residual))
	for index, custody := range residual {
		stage := admissionOwned
		if custody.Prospective() {
			stage = admissionProspective
		}
		facts[index] = campaignResidualCustody{
			generation: custody.Generation(), attempt: attemptIdentity(custody.Attempt()),
			stage: stage, transferred: custody.Transferred(),
		}
	}
	return facts
}

func campaignSettlement(result processruntime.EmergencySettlement) campaignEmergencySettlement {

	return campaignEmergencySettlement{
		epoch: fatalEpochID(result.Epoch()), owner: result.Owner(),
		acknowledged: result.Acknowledged(),
		residual:     campaignResiduals(result.Residual()),
	}
}

func campaignSettlementValue(result emergencySettlement) campaignEmergencySettlement {
	return campaignEmergencySettlement{
		epoch: result.epoch, owner: result.owner, acknowledged: append([]attemptGeneration(nil), result.acknowledged...),
		residual: campaignResidualValues(result.residual),
	}
}

func campaignResidualValues(values []residualCustody) []campaignResidualCustody {
	result := make([]campaignResidualCustody, len(values))
	for index, value := range values {
		result[index] = campaignResidualCustody(value)
	}
	return result
}

func campaignClosureValue(result runtimeClosure) campaignRuntimeClosure {
	return campaignRuntimeClosure{
		epoch: result.epoch, cancelledWaiting: campaignAdmissionValues(result.cancelledWaiting),
		compensatedGrants: campaignAdmissionValues(result.compensatedGrants), residual: campaignResidualValues(result.residual),
	}
}

func campaignTerminalEvidence(result terminalResult) campaignTerminalResult {
	return campaignTerminalResult(result)
}

func campaignBarrierEvidence(result barrierResult) campaignBarrierResult {
	deliveries := make([]campaignAdmission, len(result.deliveries))
	for index, grant := range result.deliveries {
		deliveries[index] = campaignAdmissionValue(grant)
	}

	return campaignBarrierResult{
		decision: result.decision, request: campaignAdmissionValue(result.request), deliveries: deliveries,
	}
}

func runtimeAdmissionRequest(fact campaignAdmission) admissionRequest {
	return admissionAuthority(fact)
}

func processRuntimeAdmission(fact campaignAdmission) processruntime.Admission {
	return processruntime.Admission{
		Campaign: fact.campaign, Attempt: string(fact.attempt), Class: fact.class,
		Profile: fact.profile, Deadline: fact.deadline,
	}
}

func runtimeAdmissionValue(value processruntime.Admission) admissionAuthority {
	return admissionAuthority{
		campaign: value.Campaign, attempt: attemptIdentity(value.Attempt), class: value.Class,
		profile: value.Profile, deadline: value.Deadline,
	}
}

func processRuntimeObservation(observation attemptObservation) processruntime.Observation {
	switch observation := observation.(type) {
	case launchOwned:
		return processruntime.Owned()
	case launchNotReleased:
		return processruntime.NotReleased(observation.reason == launchResourceExhausted)
	case attemptSettled:
		return processruntime.Settled(observation.profile, observation.deadline)
	case attemptTripped:
		return processruntime.Tripped(observation.kind == fuseTrip, observation.profile, observation.deadline)
	case launchUnconfirmed:
		return processruntime.LaunchUnconfirmed()
	case drainUnconfirmed:
		return processruntime.DrainUnconfirmed()
	case attemptStopped:
		return processruntime.Stopped()
	case attemptInfrastructure:
		return processruntime.Infrastructure(observation.cause)
	default:
		return processruntime.Observation{}
	}
}

func processRuntimeResolutions(sweep emergencySweep) []processruntime.Resolution {
	result := make([]processruntime.Resolution, len(sweep.resolutions))
	for index, resolution := range sweep.resolutions {
		result[index] = processruntime.ConfirmedDrained(resolution.generation)
		if resolution.disposition == emergencyCustodyTransferred {
			result[index] = processruntime.TransferCustody(resolution.generation)
		}
	}
	return result
}

func runtimeEmergencySettlement(settlement processruntime.EmergencySettlement) emergencySettlement {
	return emergencySettlement{
		epoch: fatalEpochID(settlement.Epoch()), owner: settlement.Owner(), acknowledged: settlement.Acknowledged(),
		residual: runtimeResiduals(settlement.Residual()),
	}
}

func runtimeResiduals(values []processruntime.Residual) []residualCustody {
	result := make([]residualCustody, len(values))
	for index, value := range values {
		stage := admissionOwned
		if value.Prospective() {
			stage = admissionProspective
		}
		result[index] = residualCustody{
			generation: value.Generation(), attempt: attemptIdentity(value.Attempt()),
			stage: stage, transferred: value.Transferred(),
		}
	}
	return result
}

func runtimeBarrierBinding(fact campaignBarrierBinding) barrierBinding {
	return barrierBinding(fact)
}
