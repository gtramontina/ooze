package processruntime

import (
	"fmt"
	"slices"
)

// AdmissionClass identifies the process-runtime admission policy for an attempt.
type AdmissionClass uint8

// Process-runtime admission classes.
const (
	SharedAdmission AdmissionClass = iota + 1
	ExclusiveAdmission
	SerialPrimaryAdmission
	ConfirmationAdmission
	ConfirmationBarrierAdmission
)

// RegistrationDecision identifies the result of campaign registration.
type RegistrationDecision uint8

// Campaign registration decisions.
const (
	RegistrationAccepted RegistrationDecision = iota + 1
	RegistrationRejectedRecursive
	RegistrationRejectedClosed
)

// AdmissionDecision identifies the result of an admission operation.
type AdmissionDecision uint8

// Admission decisions.
const (
	AdmissionAccepted AdmissionDecision = iota + 1
	AdmissionRejectedClosed
	AdmissionRejectedUnknownCampaign
	AdmissionRejectedGateClosed
	AdmissionRejectedGateOpen
	AdmissionRejectedDuplicate
	AdmissionRejectedExclusiveOutstanding
	AdmissionRejectedSharedLimit
	AdmissionRejectedAlreadyCommitted
	AdmissionCancellationProcessedWaiting
	AdmissionCancellationProcessedGranted
	AdmissionReturnedAfterClosure
	AdmissionReturnedAfterGateClosure
)

// BarrierDecision identifies the result of binding a confirmation barrier.
type BarrierDecision uint8

// Confirmation barrier decisions.
const (
	BarrierBound BarrierDecision = iota + 1
	BarrierRejectedMissing
	BarrierRejectedClosureOutstanding
	BarrierRejectedExecutionMismatch
)

// QueueDecision identifies the result of completing a confirmation queue.
type QueueDecision uint8

// Confirmation queue decisions.
const (
	ConfirmationQueueCompleted QueueDecision = iota + 1
	ConfirmationQueueRejectedMissing
	ConfirmationQueueRejectedOutstanding
)

// StartDecision identifies the result of committing an attempt start.
type StartDecision uint8

// Start commitment decisions.
const (
	StartAccepted StartDecision = iota + 1
	StartRejectedGrant
	StartRejectedGate
	StartRejectedClosed
)

// TerminalDecision identifies the result of campaign terminal commitment.
type TerminalDecision uint8

// Terminal commitment decisions.
const (
	TerminalAccepted TerminalDecision = iota + 1
	TerminalForcedAbort
	TerminalRejectedUnknown
	TerminalRejectedOutstanding
	TerminalRejectedClosed
)

// LaunchFailure identifies a proven pre-release launch failure.
type LaunchFailure uint8

// Proven launch failures.
const (
	LaunchFailed LaunchFailure = iota + 1
	LaunchResourceExhausted
)

// TripKind identifies attempt deadline or fuse evidence.
type TripKind uint8

// Attempt trip kinds.
const (
	DeadlineTrip TripKind = iota + 1
	FuseTrip
)

// EmergencyDisposition identifies the custody established for one generation.
type EmergencyDisposition uint8

// Emergency custody dispositions.
const (
	EmergencyConfirmedDrained EmergencyDisposition = iota + 1
	EmergencyCustodyTransferred
)

// AdmissionStage identifies the last established custody stage.
type AdmissionStage uint8

// Admission custody stages.
const (
	AdmissionWaiting AdmissionStage = iota + 1
	AdmissionGranted
	AdmissionProspective
	AdmissionOwned
)

// Campaign identifies one registered process-runtime campaign.
type Campaign struct {
	ID      uint64
	Lineage uint64
}

// Admission identifies one request or grant without its delivery capability.
type Admission struct {
	Campaign Campaign
	Attempt  string
	Class    AdmissionClass
	Profile  Profile
	Deadline int64
}

// Observation contains immutable attempt evidence accepted by the process runtime.
type Observation struct {
	Kind     ObservationKind
	Reason   LaunchFailure
	Profile  Profile
	Deadline int64
	Trip     TripKind
	Cause    string
}

// ObservationKind identifies one attempt-evidence variant.
type ObservationKind uint8

// Attempt observation kinds.
const (
	LaunchOwned ObservationKind = iota + 1
	LaunchNotReleased
	AttemptSettled
	AttemptTripped
	LaunchUnconfirmed
	DrainUnconfirmed
	AttemptStopped
	AttemptInfrastructure
)

// Resolution records emergency custody for one generation.
type Resolution struct {
	Generation  uint64
	Disposition EmergencyDisposition
}

// Residual identifies custody left after an emergency operation.
type Residual struct {
	Generation  uint64
	Attempt     string
	Stage       AdmissionStage
	Transferred bool
}

// Registration is the result of campaign registration.
type Registration struct {
	Decision RegistrationDecision
	Campaign Campaign
}

// AdmissionResult is the result of an admission operation.
type AdmissionResult struct {
	Decision   AdmissionDecision
	Request    Admission
	Deliveries []Admission
	FatalEpoch uint64
}

// BarrierResult is the result of binding a confirmation barrier.
type BarrierResult struct {
	Decision   BarrierDecision
	Request    Admission
	Deliveries []Admission
}

// QueueResult is the result of completing a confirmation queue.
type QueueResult struct {
	Decision   QueueDecision
	Deliveries []Admission
}

// StartResult is the result of committing an attempt start.
type StartResult struct {
	Decision                 StartDecision
	Generation               uint64
	SettlementAcknowledged   bool
	RuntimeClosureInProgress bool
}

// ObservationResult is the response to accepted attempt evidence.
type ObservationResult struct {
	Generation               uint64
	Deliveries               []Admission
	CancelledWaiting         []Admission
	CompensatedGrants        []Admission
	SettlementAcknowledged   bool
	ConfirmationProvisional  bool
	PressureTransitioned     bool
	RuntimeClosureInProgress bool
	ConfirmationObserved     bool
	ConfirmationQueueDrained bool
	FatalEpoch               uint64
}

// EmergencyResult is the exact runtime-wide emergency settlement.
type EmergencyResult struct {
	Epoch        uint64
	Owner        Campaign
	Acknowledged []uint64
	Residual     []Residual
}

// TerminalResult is the result of terminal campaign commitment.
type TerminalResult struct {
	Decision TerminalDecision
	Epoch    uint64
}

// ClosureResult is the custody established when the process runtime closes.
type ClosureResult struct {
	Epoch             uint64
	CancelledWaiting  []Admission
	CompensatedGrants []Admission
	Residual          []Residual
}

// Event is an immutable accepted process-runtime command and result.
type Event interface {
	Variant() any
	processRuntimeEvent()
}

type acceptedEvent struct{ variant any }

func (acceptedEvent) processRuntimeEvent() {}
func (event acceptedEvent) Variant() any   { return event.variant }

// CampaignRegistrationProcessed reports accepted campaign registration.
type CampaignRegistrationProcessed struct {
	lineage      uint64
	registration Registration
}

// NewCampaignRegistrationProcessed validates and creates a campaign registration event.
func NewCampaignRegistrationProcessed(lineage uint64, registration Registration) (Event, error) {
	if !registration.Decision.valid() {
		return nil, fmt.Errorf("invalid registration decision %d", registration.Decision)
	}
	return acceptedEvent{variant: CampaignRegistrationProcessed{lineage: lineage, registration: registration}}, nil
}

// Lineage returns the requested campaign lineage.
func (event CampaignRegistrationProcessed) Lineage() uint64 { return event.lineage }

// Registration returns the accepted registration result.
func (event CampaignRegistrationProcessed) Registration() Registration { return event.registration }

// AdmissionRequestProcessed reports an accepted admission request.
type AdmissionRequestProcessed struct {
	admission Admission
	result    AdmissionResult
}

// NewAdmissionRequestProcessed validates and creates an admission request event.
func NewAdmissionRequestProcessed(admission Admission, result AdmissionResult) (Event, error) {
	if err := validateAdmissionEvent(admission, result); err != nil {
		return nil, err
	}
	if result.Decision > AdmissionRejectedAlreadyCommitted {
		return nil, fmt.Errorf("invalid admission-request decision %d", result.Decision)
	}
	return acceptedEvent{variant: AdmissionRequestProcessed{admission: admission, result: cloneAdmissionResult(result)}}, nil
}

// Admission returns the request without its delivery capability.
func (event AdmissionRequestProcessed) Admission() Admission { return event.admission }

// Result returns an independent copy of the admission result.
func (event AdmissionRequestProcessed) Result() AdmissionResult {
	return cloneAdmissionResult(event.result)
}

// AdmissionCancellationProcessed reports an accepted admission cancellation.
type AdmissionCancellationProcessed struct{ AdmissionRequestProcessed }

// NewAdmissionCancellationProcessed validates and creates an admission cancellation event.
func NewAdmissionCancellationProcessed(admission Admission, result AdmissionResult) (Event, error) {
	if err := validateAdmissionEvent(admission, result); err != nil {
		return nil, err
	}
	if result.Decision != AdmissionRejectedClosed && result.Decision != AdmissionRejectedDuplicate &&
		result.Decision != AdmissionRejectedAlreadyCommitted && result.Decision != AdmissionCancellationProcessedWaiting &&
		result.Decision != AdmissionCancellationProcessedGranted {
		return nil, fmt.Errorf("invalid admission-cancellation decision %d", result.Decision)
	}
	return acceptedEvent{variant: AdmissionCancellationProcessed{AdmissionRequestProcessed: AdmissionRequestProcessed{
		admission: admission, result: cloneAdmissionResult(result),
	}}}, nil
}

// GrantReturnProcessed reports an accepted grant-return acknowledgement.
type GrantReturnProcessed struct{ AdmissionRequestProcessed }

// NewGrantReturnProcessed validates and creates a grant-return event.
func NewGrantReturnProcessed(admission Admission, result AdmissionResult) (Event, error) {
	if err := validateAdmissionEvent(admission, result); err != nil {
		return nil, err
	}
	if result.Decision != AdmissionReturnedAfterClosure && result.Decision != AdmissionReturnedAfterGateClosure {
		return nil, fmt.Errorf("invalid grant-return decision %d", result.Decision)
	}
	return acceptedEvent{variant: GrantReturnProcessed{AdmissionRequestProcessed: AdmissionRequestProcessed{
		admission: admission, result: cloneAdmissionResult(result),
	}}}, nil
}

// ConfirmationBarrierProcessed reports an accepted confirmation-barrier binding.
type ConfirmationBarrierProcessed struct {
	barrier Admission
	result  BarrierResult
}

// NewConfirmationBarrierProcessed validates and creates a barrier-binding event.
func NewConfirmationBarrierProcessed(barrier Admission, result BarrierResult) (Event, error) {
	if !barrier.valid() || !result.Decision.valid() || !admissionsValid(result.Deliveries) {
		return nil, fmt.Errorf("invalid confirmation barrier event")
	}
	result.Deliveries = slices.Clone(result.Deliveries)
	return acceptedEvent{variant: ConfirmationBarrierProcessed{barrier: barrier, result: result}}, nil
}

// Barrier returns the bound barrier without its delivery capability.
func (event ConfirmationBarrierProcessed) Barrier() Admission { return event.barrier }

// Result returns an independent copy of the barrier result.
func (event ConfirmationBarrierProcessed) Result() BarrierResult {
	event.result.Deliveries = slices.Clone(event.result.Deliveries)
	return event.result
}

// ConfirmationQueueProcessed reports accepted confirmation-queue completion.
type ConfirmationQueueProcessed struct {
	campaign Campaign
	result   QueueResult
}

// NewConfirmationQueueProcessed validates and creates a queue-completion event.
func NewConfirmationQueueProcessed(campaign Campaign, result QueueResult) (Event, error) {
	if !result.Decision.valid() || !admissionsValid(result.Deliveries) {
		return nil, fmt.Errorf("invalid confirmation queue event")
	}
	result.Deliveries = slices.Clone(result.Deliveries)
	return acceptedEvent{variant: ConfirmationQueueProcessed{campaign: campaign, result: result}}, nil
}

// Campaign returns the campaign whose confirmation queue completed.
func (event ConfirmationQueueProcessed) Campaign() Campaign { return event.campaign }

// Result returns an independent copy of the queue result.
func (event ConfirmationQueueProcessed) Result() QueueResult {
	event.result.Deliveries = slices.Clone(event.result.Deliveries)
	return event.result
}

// StartCommitmentProcessed reports an accepted attempt start commitment.
type StartCommitmentProcessed struct {
	grant  Admission
	result StartResult
}

// NewStartCommitmentProcessed validates and creates a start-commitment event.
func NewStartCommitmentProcessed(grant Admission, result StartResult) (Event, error) {
	if !grant.valid() || !result.Decision.valid() {
		return nil, fmt.Errorf("invalid start commitment event")
	}
	return acceptedEvent{variant: StartCommitmentProcessed{grant: grant, result: result}}, nil
}

// Grant returns the committed grant without its delivery capability.
func (event StartCommitmentProcessed) Grant() Admission { return event.grant }

// Result returns the start-commitment result.
func (event StartCommitmentProcessed) Result() StartResult { return event.result }

// AttemptObservationProcessed reports accepted evidence for one attempt generation.
type AttemptObservationProcessed struct {
	generation  uint64
	observation Observation
	result      ObservationResult
}

// NewAttemptObservationProcessed validates and creates an attempt-observation event.
func NewAttemptObservationProcessed(generation uint64, observation Observation, result ObservationResult) (Event, error) {
	if !observation.valid() || !admissionsValid(result.Deliveries) ||
		!admissionsValid(result.CancelledWaiting) || !admissionsValid(result.CompensatedGrants) {
		return nil, fmt.Errorf("invalid attempt observation event")
	}
	return acceptedEvent{variant: AttemptObservationProcessed{
		generation: generation, observation: observation, result: cloneObservationResult(result),
	}}, nil
}

// Generation returns the observed attempt generation.
func (event AttemptObservationProcessed) Generation() uint64 { return event.generation }

// Observation returns the accepted attempt evidence.
func (event AttemptObservationProcessed) Observation() Observation { return event.observation }

// Result returns an independent copy of the observation result.
func (event AttemptObservationProcessed) Result() ObservationResult {
	return cloneObservationResult(event.result)
}

// EmergencySettlementProcessed reports accepted runtime-wide emergency settlement.
type EmergencySettlementProcessed struct {
	resolutions []Resolution
	result      EmergencyResult
}

// NewEmergencySettlementProcessed validates and creates an emergency-settlement event.
func NewEmergencySettlementProcessed(resolutions []Resolution, result EmergencyResult) (Event, error) {
	for _, resolution := range resolutions {
		if !resolution.Disposition.valid() {
			return nil, fmt.Errorf("invalid emergency disposition %d", resolution.Disposition)
		}
	}
	for _, residual := range result.Residual {
		if !residual.Stage.valid() {
			return nil, fmt.Errorf("invalid residual stage %d", residual.Stage)
		}
	}
	return acceptedEvent{variant: EmergencySettlementProcessed{
		resolutions: slices.Clone(resolutions), result: cloneEmergencyResult(result),
	}}, nil
}

// Resolutions returns an independent copy of the submitted resolutions.
func (event EmergencySettlementProcessed) Resolutions() []Resolution {
	return slices.Clone(event.resolutions)
}

// Result returns an independent copy of the emergency result.
func (event EmergencySettlementProcessed) Result() EmergencyResult {
	return cloneEmergencyResult(event.result)
}

// TerminalCommitmentProcessed reports accepted campaign terminal commitment.
type TerminalCommitmentProcessed struct {
	campaign Campaign
	result   TerminalResult
}

// NewTerminalCommitmentProcessed validates and creates a terminal-commitment event.
func NewTerminalCommitmentProcessed(campaign Campaign, result TerminalResult) (Event, error) {
	if !result.Decision.valid() {
		return nil, fmt.Errorf("invalid terminal decision %d", result.Decision)
	}
	return acceptedEvent{variant: TerminalCommitmentProcessed{campaign: campaign, result: result}}, nil
}

// Campaign returns the campaign requesting terminal commitment.
func (event TerminalCommitmentProcessed) Campaign() Campaign { return event.campaign }

// Result returns the terminal-commitment result.
func (event TerminalCommitmentProcessed) Result() TerminalResult { return event.result }

// ForcedAbortProcessed reports accepted forced-abort authorization.
type ForcedAbortProcessed struct {
	campaign Campaign
	epoch    uint64
	result   TerminalResult
}

// NewForcedAbortProcessed validates and creates a forced-abort event.
func NewForcedAbortProcessed(campaign Campaign, epoch uint64, result TerminalResult) (Event, error) {
	if !result.Decision.valid() {
		return nil, fmt.Errorf("invalid terminal decision %d", result.Decision)
	}
	return acceptedEvent{variant: ForcedAbortProcessed{campaign: campaign, epoch: epoch, result: result}}, nil
}

// Campaign returns the campaign requesting forced abort.
func (event ForcedAbortProcessed) Campaign() Campaign { return event.campaign }

// Epoch returns the fatal epoch authorizing forced abort.
func (event ForcedAbortProcessed) Epoch() uint64 { return event.epoch }

// Result returns the terminal result.
func (event ForcedAbortProcessed) Result() TerminalResult { return event.result }

// RuntimeClosureProcessed reports accepted process-runtime closure.
type RuntimeClosureProcessed struct {
	cause  string
	result ClosureResult
}

// NewRuntimeClosureProcessed validates and creates a runtime-closure event.
func NewRuntimeClosureProcessed(cause string, result ClosureResult) (Event, error) {
	for _, residual := range result.Residual {
		if !residual.Stage.valid() {
			return nil, fmt.Errorf("invalid residual stage %d", residual.Stage)
		}
	}
	if !admissionsValid(result.CancelledWaiting) || !admissionsValid(result.CompensatedGrants) {
		return nil, fmt.Errorf("invalid runtime closure event")
	}
	return acceptedEvent{variant: RuntimeClosureProcessed{cause: cause, result: cloneClosureResult(result)}}, nil
}

// Cause returns the fatal cause that closed the runtime.
func (event RuntimeClosureProcessed) Cause() string { return event.cause }

// Result returns an independent copy of the closure result.
func (event RuntimeClosureProcessed) Result() ClosureResult { return cloneClosureResult(event.result) }

// Observer receives accepted process-runtime events.
type Observer interface{ Observe(Event) error }

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(Event) error

// Observe receives one accepted process-runtime event.
func (observe ObserverFunc) Observe(event Event) error { return observe(event) }

func validateAdmissionEvent(admission Admission, result AdmissionResult) error {
	if !admission.valid() || !result.Decision.valid() || result.Request != admission ||
		!admissionsValid(result.Deliveries) {
		return fmt.Errorf("invalid admission event")
	}
	return nil
}

func (admission Admission) valid() bool {
	return admission.Class >= SharedAdmission && admission.Class <= ConfirmationBarrierAdmission &&
		admission.Profile <= SerialProfile
}

func admissionsValid(admissions []Admission) bool {
	for _, admission := range admissions {
		if !admission.valid() {
			return false
		}
	}
	return true
}

func (observation Observation) valid() bool {
	switch observation.Kind {
	case LaunchOwned, LaunchUnconfirmed, DrainUnconfirmed, AttemptStopped, AttemptInfrastructure:
		return true
	case LaunchNotReleased:
		return observation.Reason == LaunchFailed || observation.Reason == LaunchResourceExhausted
	case AttemptSettled:
		return observation.Profile <= SerialProfile
	case AttemptTripped:
		return observation.Profile <= SerialProfile &&
			(observation.Trip == DeadlineTrip || observation.Trip == FuseTrip)
	default:
		return false
	}
}

func (decision RegistrationDecision) valid() bool {
	return decision >= RegistrationAccepted && decision <= RegistrationRejectedClosed
}
func (decision AdmissionDecision) valid() bool {
	return decision >= AdmissionAccepted && decision <= AdmissionReturnedAfterGateClosure
}
func (decision BarrierDecision) valid() bool {
	return decision >= BarrierBound && decision <= BarrierRejectedExecutionMismatch
}
func (decision QueueDecision) valid() bool {
	return decision >= ConfirmationQueueCompleted && decision <= ConfirmationQueueRejectedOutstanding
}
func (decision StartDecision) valid() bool {
	return decision >= StartAccepted && decision <= StartRejectedClosed
}
func (decision TerminalDecision) valid() bool {
	return decision >= TerminalAccepted && decision <= TerminalRejectedClosed
}
func (disposition EmergencyDisposition) valid() bool {
	return disposition == EmergencyConfirmedDrained || disposition == EmergencyCustodyTransferred
}
func (stage AdmissionStage) valid() bool { return stage >= AdmissionWaiting && stage <= AdmissionOwned }

func cloneAdmissionResult(result AdmissionResult) AdmissionResult {
	result.Deliveries = slices.Clone(result.Deliveries)
	return result
}

func cloneObservationResult(result ObservationResult) ObservationResult {
	result.Deliveries = slices.Clone(result.Deliveries)
	result.CancelledWaiting = slices.Clone(result.CancelledWaiting)
	result.CompensatedGrants = slices.Clone(result.CompensatedGrants)
	return result
}

func cloneEmergencyResult(result EmergencyResult) EmergencyResult {
	result.Acknowledged = slices.Clone(result.Acknowledged)
	result.Residual = slices.Clone(result.Residual)
	return result
}

func cloneClosureResult(result ClosureResult) ClosureResult {
	result.CancelledWaiting = slices.Clone(result.CancelledWaiting)
	result.CompensatedGrants = slices.Clone(result.CompensatedGrants)
	result.Residual = slices.Clone(result.Residual)
	return result
}
