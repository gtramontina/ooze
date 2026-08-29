package campaign

import "github.com/gtramontina/ooze/internal/ooze/internal/processruntime"

func runtimeAdmissions(values []processruntime.Admission) []admissionAuthority {
	result := make([]admissionAuthority, len(values))
	for index, value := range values {
		result[index] = runtimeAdmissionValue(value)
	}
	return result
}

func runtimeAdmissionResult(result processruntime.AdmissionResult) admissionResult {
	return admissionResult{
		decision: result.Decision(), request: runtimeAdmissionValue(result.Request()),
		deliveries: runtimeAdmissions(result.Deliveries()), fatalEpoch: fatalEpochID(result.FatalEpoch()),
	}
}

func runtimeBarrierResult(result processruntime.BarrierResult) barrierResult {
	return barrierResult{
		decision: result.Decision(), request: runtimeAdmissionValue(result.Request()),
		deliveries: runtimeAdmissions(result.Deliveries()),
	}
}

func runtimeStartResult(result processruntime.StartResult) startCommittedResult {
	return startCommittedResult{
		decision: result.Decision(), generation: result.Generation(),
		settlementAcknowledged:   result.SettlementAcknowledged(),
		runtimeClosureInProgress: result.RuntimeClosureInProgress(),
	}
}

func runtimeClosureValue(value processruntime.Closure) runtimeClosure {
	return runtimeClosure{
		epoch: fatalEpochID(value.Epoch()), cancelledWaiting: runtimeAdmissions(value.CancelledWaiting()),
		compensatedGrants: runtimeAdmissions(value.CompensatedGrants()), residual: runtimeResiduals(value.Residual()),
	}
}

func campaignAdmissionFact(authority processruntime.Admission) campaignAdmission {
	return campaignAdmission{
		campaign: campaignTokenValue(authority.Campaign), attempt: attemptIdentity(authority.Attempt), class: authority.Class,
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
	return campaignRegistration{decision: result.Decision(), token: campaignTokenValue(result.Campaign())}
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
		epoch: fatalEpochID(result.Epoch()), owner: campaignTokenValue(result.Owner()),
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

func campaignBarrierEvidence(result barrierResult) campaignBarrierResult {
	deliveries := make([]campaignAdmission, len(result.deliveries))
	for index, grant := range result.deliveries {
		deliveries[index] = campaignAdmissionValue(grant)
	}

	return campaignBarrierResult{
		decision: result.decision, request: campaignAdmissionValue(result.request), deliveries: deliveries,
	}
}

func processRuntimeAdmission(
	fact campaignAdmission,
	authority processruntime.Campaign,
) (processruntime.Admission, bool) {
	if fact.campaign != campaignTokenValue(authority) {
		return processruntime.Admission{}, false
	}
	return processruntime.Admission{
		Campaign: authority, Attempt: string(fact.attempt), Class: fact.class,
		Profile: fact.profile, Deadline: fact.deadline,
	}, true
}

func runtimeAdmissionValue(value processruntime.Admission) admissionAuthority {
	return admissionAuthority{
		campaign: campaignTokenValue(value.Campaign), attempt: attemptIdentity(value.Attempt), class: value.Class,
		profile: value.Profile, deadline: value.Deadline,
	}
}

func runtimeEmergencySettlement(settlement processruntime.EmergencySettlement) emergencySettlement {
	return emergencySettlement{
		epoch: fatalEpochID(settlement.Epoch()), owner: campaignTokenValue(settlement.Owner()),
		acknowledged: settlement.Acknowledged(),
		residual:     runtimeResiduals(settlement.Residual()),
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
