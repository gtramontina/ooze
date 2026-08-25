package ooze

func campaignAdmissionFact(authority admissionAuthority) campaignAdmission {
	return campaignAdmission{
		campaign: authority.campaign, attempt: authority.attempt, class: authority.class,
		profile: authority.profile, deadline: authority.deadline,
	}
}

func campaignAdmissions(authorities []admissionRequestToken) []campaignAdmission {
	facts := make([]campaignAdmission, len(authorities))
	for index, authority := range authorities {
		facts[index] = campaignAdmissionFact(authority)
	}

	return facts
}

func campaignAdmissionEvidence(result admissionResult) campaignAdmissionResult {
	return campaignAdmissionResult{
		decision: result.decision, request: campaignAdmissionFact(result.request), fatalEpoch: result.fatalEpoch,
	}
}

func campaignStartEvidence(result startCommittedResult) campaignStartResult {
	return campaignStartResult(result)
}

func campaignReceipt(result observationResult) campaignRuntimeReceipt {
	return campaignRuntimeReceipt{
		generation:               result.generation,
		cancelledWaiting:         campaignAdmissions(result.cancelledWaiting),
		compensatedGrants:        campaignAdmissions(result.compensatedGrants),
		settlementAcknowledged:   result.settlementAcknowledged,
		confirmationProvisional:  result.confirmationProvisional,
		pressureTransitioned:     result.pressureTransitioned,
		runtimeClosureInProgress: result.runtimeClosureInProgress,
		confirmationObserved:     result.confirmationObserved,
		confirmationQueueDrained: result.confirmationQueueDrained,
		fatalEpoch:               result.fatalEpoch,
	}
}

func campaignClosure(result runtimeClosure) campaignRuntimeClosure {
	return campaignRuntimeClosure{
		epoch: result.epoch, cancelledWaiting: campaignAdmissions(result.cancelledWaiting),
		compensatedGrants: campaignAdmissions(result.compensatedGrants), residual: campaignResiduals(result.residual),
	}
}

func campaignResiduals(residual []residualCustody) []campaignResidualCustody {
	facts := make([]campaignResidualCustody, len(residual))
	for index, custody := range residual {
		facts[index] = campaignResidualCustody(custody)
	}
	return facts
}

func campaignSettlement(result emergencySettlement) campaignEmergencySettlement {

	return campaignEmergencySettlement{
		epoch: result.epoch, owner: result.owner,
		acknowledged: append([]attemptGeneration(nil), result.acknowledged...),
		residual:     campaignResiduals(result.residual),
	}
}

func campaignTerminalEvidence(result terminalResult) campaignTerminalResult {
	return campaignTerminalResult(result)
}

func campaignBarrierEvidence(result barrierResult) campaignBarrierResult {
	deliveries := make([]campaignAdmission, len(result.deliveries))
	for index, grant := range result.deliveries {
		deliveries[index] = campaignAdmissionFact(grant)
	}

	return campaignBarrierResult{
		decision: result.decision, request: campaignAdmissionFact(result.request), deliveries: deliveries,
	}
}

func runtimeAdmissionRequest(fact campaignAdmission) admissionRequest {
	return admissionAuthority{
		campaign: fact.campaign, attempt: fact.attempt, class: fact.class,
		profile: fact.profile, deadline: fact.deadline,
	}
}

func runtimeBarrierBinding(fact campaignBarrierBinding) barrierBinding {
	return barrierBinding{
		campaign: fact.campaign, attempt: fact.attempt, profile: fact.profile, deadline: fact.deadline,
	}
}
