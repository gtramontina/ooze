package processruntime

// Event is an immutable accepted process-runtime transition.
type Event interface{ processRuntimeEvent() }

type processRuntimeEvent = Event

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

// CampaignRegistrationProcessed reports accepted campaign registration.
type CampaignRegistrationProcessed = runtimeCampaignRegistrationProcessed

// AdmissionRequestProcessed reports an accepted admission request.
type AdmissionRequestProcessed = runtimeAdmissionRequestProcessed

// AdmissionCancellationProcessed reports accepted admission cancellation.
type AdmissionCancellationProcessed = runtimeAdmissionCancellationProcessed

// GrantReturnProcessed reports an accepted compensated-grant return.
type GrantReturnProcessed = runtimeGrantReturnProcessed

// ConfirmationBarrierProcessed reports accepted confirmation-barrier binding.
type ConfirmationBarrierProcessed = runtimeConfirmationBarrierProcessed

// ConfirmationQueueProcessed reports accepted confirmation-queue completion.
type ConfirmationQueueProcessed = runtimeConfirmationQueueProcessed

// StartCommitmentProcessed reports accepted start commitment.
type StartCommitmentProcessed = runtimeStartCommitmentProcessed

// AttemptObservationProcessed reports accepted attempt evidence.
type AttemptObservationProcessed = runtimeAttemptObservationProcessed

// EmergencySettlementProcessed reports accepted emergency settlement.
type EmergencySettlementProcessed = runtimeEmergencySettlementProcessed

// TerminalCommitmentProcessed reports accepted terminal commitment.
type TerminalCommitmentProcessed = runtimeTerminalCommitmentProcessed

// ForcedAbortProcessed reports accepted fatal-epoch terminal authorization.
type ForcedAbortProcessed = runtimeForcedAbortProcessed

// RuntimeClosureProcessed reports accepted runtime closure.
type RuntimeClosureProcessed = runtimeClosureProcessed

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

// Lineage returns the requested registration lineage.
func (event runtimeCampaignRegistrationProcessed) Lineage() Lineage {
	return Lineage(event.provenance.lineage)
}

// Registration returns the accepted registration result.
func (event runtimeCampaignRegistrationProcessed) Registration() Registration {
	return Registration{value: event.result}
}

// Admission returns the submitted admission fact.
func (event runtimeAdmissionRequestProcessed) Admission() Admission {
	return admissionValue(event.request)
}

// Result returns the admission result.
func (event runtimeAdmissionRequestProcessed) Result() AdmissionResult {
	return AdmissionResult{value: event.result}
}

// Admission returns the cancelled admission fact.
func (event runtimeAdmissionCancellationProcessed) Admission() Admission {
	return admissionValue(event.request)
}

// Result returns the cancellation result.
func (event runtimeAdmissionCancellationProcessed) Result() AdmissionResult {
	return AdmissionResult{value: event.result}
}

// Admission returns the returned grant fact.
func (event runtimeGrantReturnProcessed) Admission() Admission { return admissionValue(event.grant) }

// Result returns the grant-return result.
func (event runtimeGrantReturnProcessed) Result() AdmissionResult {
	return AdmissionResult{value: event.result}
}

// Barrier returns the submitted confirmation binding.
func (event runtimeConfirmationBarrierProcessed) Barrier() Barrier {
	return Barrier{
		Campaign: Campaign{token: event.barrier.campaign}, Attempt: string(event.barrier.attempt),
		Profile: event.barrier.profile, Deadline: event.barrier.deadline,
	}
}

// Result returns the confirmation barrier result.
func (event runtimeConfirmationBarrierProcessed) Result() BarrierResult {
	return BarrierResult{value: event.result}
}

// Campaign returns the completed confirmation campaign.
func (event runtimeConfirmationQueueProcessed) Campaign() Campaign {
	return Campaign{token: event.campaign}
}

// Result returns the confirmation queue result.
func (event runtimeConfirmationQueueProcessed) Result() QueueResult {
	return QueueResult{value: event.result}
}

// Grant returns the committed admission fact.
func (event runtimeStartCommitmentProcessed) Grant() Admission { return admissionValue(event.grant) }

// Result returns the start commitment result.
func (event runtimeStartCommitmentProcessed) Result() StartResult {
	return StartResult{value: event.result}
}

// Generation returns the observed generation.
func (event runtimeAttemptObservationProcessed) Generation() Generation {
	return Generation(event.generation)
}

// Observation returns immutable accepted attempt evidence.
func (event runtimeAttemptObservationProcessed) Observation() Observation {
	return Observation{value: event.observation}
}

// Result returns the attempt observation receipt.
func (event runtimeAttemptObservationProcessed) Result() Receipt { return Receipt{value: event.result} }

// Resolutions returns submitted emergency custody facts.
func (event runtimeEmergencySettlementProcessed) Resolutions() []Resolution {
	result := make([]Resolution, len(event.sweep.resolutions))
	for index, resolution := range event.sweep.resolutions {
		result[index] = Resolution{
			generation:  Generation(resolution.generation),
			transferred: resolution.disposition == emergencyCustodyTransferred,
		}
	}
	return result
}

// Result returns exact emergency settlement.
func (event runtimeEmergencySettlementProcessed) Result() EmergencySettlement {
	return EmergencySettlement{value: event.result}
}

// Campaign returns the terminal campaign.
func (event runtimeTerminalCommitmentProcessed) Campaign() Campaign {
	return Campaign{token: event.campaign}
}

// Result returns the terminal commitment result.
func (event runtimeTerminalCommitmentProcessed) Result() TerminalResult {
	return TerminalResult{value: event.result}
}

// Campaign returns the forced-abort campaign.
func (event runtimeForcedAbortProcessed) Campaign() Campaign { return Campaign{token: event.campaign} }

// Epoch returns the requested fatal epoch.
func (event runtimeForcedAbortProcessed) Epoch() uint64 { return uint64(event.epoch) }

// Result returns the forced-abort result.
func (event runtimeForcedAbortProcessed) Result() TerminalResult {
	return TerminalResult{value: event.result}
}

// Cause returns the runtime closure cause.
func (event runtimeClosureProcessed) Cause() string { return string(event.cause) }

// Result returns runtime closure custody.
func (event runtimeClosureProcessed) Result() Closure { return Closure{value: event.result} }

// Observer receives immutable accepted process-runtime events.
type Observer interface{ Observe(Event) error }

type processRuntimeObserver = Observer

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(Event) error

// Observe receives one accepted process-runtime event.
func (observe ObserverFunc) Observe(event Event) error { return observe(event) }

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
