package ooze

type processRuntimeEvent interface{ processRuntimeEvent() }

type runtimeCampaignRegistrationProcessed struct {
	provenance campaignProvenance
	result     campaignRegistration
}

type runtimeAdmissionRequestProcessed struct {
	request admissionRequest
	result  admissionResult
}

type runtimeAdmissionCancellationProcessed struct {
	request admissionRequestToken
	result  admissionResult
}

type runtimeGrantReturnProcessed struct {
	grant  admissionGrant
	result admissionResult
}

type runtimeConfirmationBarrierProcessed struct {
	barrier barrierBinding
	result  barrierResult
}

type runtimeConfirmationQueueProcessed struct {
	campaign campaignToken
	result   confirmationQueueResult
}

type runtimeStartCommitmentProcessed struct {
	grant  admissionGrant
	result startCommittedResult
}

type runtimeAttemptObservationProcessed struct {
	generation  attemptGeneration
	observation attemptObservation
	result      observationResult
}

type runtimeEmergencySettlementProcessed struct {
	sweep  emergencySweep
	result emergencySettlement
}

type runtimeTerminalCommitmentProcessed struct {
	campaign campaignToken
	result   terminalResult
}

type runtimeForcedAbortProcessed struct {
	campaign campaignToken
	epoch    fatalEpochID
	result   terminalResult
}

type runtimeClosureProcessed struct {
	cause  runtimeFatalCause
	result runtimeClosure
}

func (runtimeCampaignRegistrationProcessed) processRuntimeEvent()  {}
func (runtimeAdmissionRequestProcessed) processRuntimeEvent()      {}
func (runtimeAdmissionCancellationProcessed) processRuntimeEvent() {}
func (runtimeGrantReturnProcessed) processRuntimeEvent()           {}
func (runtimeConfirmationBarrierProcessed) processRuntimeEvent()   {}
func (runtimeConfirmationQueueProcessed) processRuntimeEvent()     {}
func (runtimeStartCommitmentProcessed) processRuntimeEvent()       {}
func (runtimeAttemptObservationProcessed) processRuntimeEvent()    {}
func (runtimeEmergencySettlementProcessed) processRuntimeEvent()   {}
func (runtimeTerminalCommitmentProcessed) processRuntimeEvent()    {}
func (runtimeForcedAbortProcessed) processRuntimeEvent()           {}
func (runtimeClosureProcessed) processRuntimeEvent()               {}

type processRuntimeObserver interface {
	Observe(processRuntimeEvent) error
}

type processRuntimeObserverFunc func(processRuntimeEvent) error

func (observe processRuntimeObserverFunc) Observe(event processRuntimeEvent) error {
	return observe(event)
}

func runtimeEventAdmission(authority admissionAuthority) admissionAuthority {
	authority.delivery = nil
	return authority
}

func runtimeEventAdmissions[Values ~[]admissionAuthority](values Values) []admissionAuthority {
	result := make([]admissionAuthority, len(values))
	for index, value := range values {
		result[index] = runtimeEventAdmission(value)
	}
	return result
}

func runtimeEventAdmissionResult(result admissionResult) admissionResult {
	result.request = runtimeEventAdmission(result.request)
	result.deliveries = runtimeEventAdmissions(result.deliveries)
	return result
}

func runtimeEventBarrierResult(result barrierResult) barrierResult {
	result.request = runtimeEventAdmission(result.request)
	result.deliveries = runtimeEventAdmissions(result.deliveries)
	return result
}

func runtimeEventObservationResult(result observationResult) observationResult {
	result.deliveries = runtimeEventAdmissions(result.deliveries)
	result.cancelledWaiting = runtimeEventAdmissions(result.cancelledWaiting)
	result.compensatedGrants = runtimeEventAdmissions(result.compensatedGrants)
	return result
}

func runtimeEventClosure(result runtimeClosure) runtimeClosure {
	result.cancelledWaiting = runtimeEventAdmissions(result.cancelledWaiting)
	result.compensatedGrants = runtimeEventAdmissions(result.compensatedGrants)
	result.residual = append([]residualCustody(nil), result.residual...)
	return result
}

func runtimeEventEmergency(result emergencySettlement) emergencySettlement {
	result.acknowledged = append([]attemptGeneration(nil), result.acknowledged...)
	result.residual = append([]residualCustody(nil), result.residual...)
	return result
}
