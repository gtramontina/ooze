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
	AdmissionCancelledWaiting
	AdmissionCancelledGranted
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
type Event interface{ processRuntimeEvent() }

// CampaignRegistered reports accepted campaign registration.
type CampaignRegistered struct {
	lineage      uint64
	registration Registration
}

// NewCampaignRegistered validates and creates a campaign registration event.
func NewCampaignRegistered(lineage uint64, registration Registration) (CampaignRegistered, error) {
	if !registration.Decision.valid() {
		return CampaignRegistered{}, fmt.Errorf("invalid registration decision %d", registration.Decision)
	}
	return CampaignRegistered{lineage: lineage, registration: registration}, nil
}

func (CampaignRegistered) processRuntimeEvent() {}

// Lineage returns the requested campaign lineage.
func (event CampaignRegistered) Lineage() uint64 { return event.lineage }

// Registration returns the accepted registration result.
func (event CampaignRegistered) Registration() Registration { return event.registration }

// AdmissionRequested reports an accepted admission request.
type AdmissionRequested struct {
	admission Admission
	result    AdmissionResult
}

// NewAdmissionRequested validates and creates an admission request event.
func NewAdmissionRequested(admission Admission, result AdmissionResult) (AdmissionRequested, error) {
	if err := validateAdmissionEvent(admission, result); err != nil {
		return AdmissionRequested{}, err
	}
	return AdmissionRequested{admission: admission, result: cloneAdmissionResult(result)}, nil
}

func (AdmissionRequested) processRuntimeEvent() {}

// Admission returns the request without its delivery capability.
func (event AdmissionRequested) Admission() Admission { return event.admission }

// Result returns an independent copy of the admission result.
func (event AdmissionRequested) Result() AdmissionResult { return cloneAdmissionResult(event.result) }

// AdmissionCancelled reports an accepted admission cancellation.
type AdmissionCancelled struct{ AdmissionRequested }

// NewAdmissionCancelled validates and creates an admission cancellation event.
func NewAdmissionCancelled(admission Admission, result AdmissionResult) (AdmissionCancelled, error) {
	event, err := NewAdmissionRequested(admission, result)
	return AdmissionCancelled{AdmissionRequested: event}, err
}

func (AdmissionCancelled) processRuntimeEvent() {}

// GrantReturnAcknowledged reports an accepted grant-return acknowledgement.
type GrantReturnAcknowledged struct{ AdmissionRequested }

// NewGrantReturnAcknowledged validates and creates a grant-return event.
func NewGrantReturnAcknowledged(admission Admission, result AdmissionResult) (GrantReturnAcknowledged, error) {
	event, err := NewAdmissionRequested(admission, result)
	return GrantReturnAcknowledged{AdmissionRequested: event}, err
}

func (GrantReturnAcknowledged) processRuntimeEvent() {}

// ConfirmationBarrierBound reports an accepted confirmation-barrier binding.
type ConfirmationBarrierBound struct {
	barrier Admission
	result  BarrierResult
}

// NewConfirmationBarrierBound validates and creates a barrier-binding event.
func NewConfirmationBarrierBound(barrier Admission, result BarrierResult) (ConfirmationBarrierBound, error) {
	if !barrier.valid() || !result.Decision.valid() || !admissionsValid(result.Deliveries) {
		return ConfirmationBarrierBound{}, fmt.Errorf("invalid confirmation barrier event")
	}
	result.Deliveries = slices.Clone(result.Deliveries)
	return ConfirmationBarrierBound{barrier: barrier, result: result}, nil
}

func (ConfirmationBarrierBound) processRuntimeEvent() {}

// Barrier returns the bound barrier without its delivery capability.
func (event ConfirmationBarrierBound) Barrier() Admission { return event.barrier }

// Result returns an independent copy of the barrier result.
func (event ConfirmationBarrierBound) Result() BarrierResult {
	event.result.Deliveries = slices.Clone(event.result.Deliveries)
	return event.result
}

// ConfirmationQueueFinished reports accepted confirmation-queue completion.
type ConfirmationQueueFinished struct {
	campaign Campaign
	result   QueueResult
}

// NewConfirmationQueueFinished validates and creates a queue-completion event.
func NewConfirmationQueueFinished(campaign Campaign, result QueueResult) (ConfirmationQueueFinished, error) {
	if !result.Decision.valid() || !admissionsValid(result.Deliveries) {
		return ConfirmationQueueFinished{}, fmt.Errorf("invalid confirmation queue event")
	}
	result.Deliveries = slices.Clone(result.Deliveries)
	return ConfirmationQueueFinished{campaign: campaign, result: result}, nil
}

func (ConfirmationQueueFinished) processRuntimeEvent() {}

// Campaign returns the campaign whose confirmation queue completed.
func (event ConfirmationQueueFinished) Campaign() Campaign { return event.campaign }

// Result returns an independent copy of the queue result.
func (event ConfirmationQueueFinished) Result() QueueResult {
	event.result.Deliveries = slices.Clone(event.result.Deliveries)
	return event.result
}

// AttemptStartCommitted reports an accepted attempt start commitment.
type AttemptStartCommitted struct {
	grant  Admission
	result StartResult
}

// NewAttemptStartCommitted validates and creates a start-commitment event.
func NewAttemptStartCommitted(grant Admission, result StartResult) (AttemptStartCommitted, error) {
	if !grant.valid() || !result.Decision.valid() {
		return AttemptStartCommitted{}, fmt.Errorf("invalid start commitment event")
	}
	return AttemptStartCommitted{grant: grant, result: result}, nil
}

func (AttemptStartCommitted) processRuntimeEvent() {}

// Grant returns the committed grant without its delivery capability.
func (event AttemptStartCommitted) Grant() Admission { return event.grant }

// Result returns the start-commitment result.
func (event AttemptStartCommitted) Result() StartResult { return event.result }

// AttemptObserved reports accepted evidence for one attempt generation.
type AttemptObserved struct {
	generation  uint64
	observation Observation
	result      ObservationResult
}

// NewAttemptObserved validates and creates an attempt-observation event.
func NewAttemptObserved(generation uint64, observation Observation, result ObservationResult) (AttemptObserved, error) {
	if !observation.valid() || !admissionsValid(result.Deliveries) ||
		!admissionsValid(result.CancelledWaiting) || !admissionsValid(result.CompensatedGrants) {
		return AttemptObserved{}, fmt.Errorf("invalid attempt observation event")
	}
	return AttemptObserved{generation: generation, observation: observation, result: cloneObservationResult(result)}, nil
}

func (AttemptObserved) processRuntimeEvent() {}

// Generation returns the observed attempt generation.
func (event AttemptObserved) Generation() uint64 { return event.generation }

// Observation returns the accepted attempt evidence.
func (event AttemptObserved) Observation() Observation { return event.observation }

// Result returns an independent copy of the observation result.
func (event AttemptObserved) Result() ObservationResult { return cloneObservationResult(event.result) }

// EmergencySettled reports accepted runtime-wide emergency settlement.
type EmergencySettled struct {
	resolutions []Resolution
	result      EmergencyResult
}

// NewEmergencySettled validates and creates an emergency-settlement event.
func NewEmergencySettled(resolutions []Resolution, result EmergencyResult) (EmergencySettled, error) {
	for _, resolution := range resolutions {
		if !resolution.Disposition.valid() {
			return EmergencySettled{}, fmt.Errorf("invalid emergency disposition %d", resolution.Disposition)
		}
	}
	for _, residual := range result.Residual {
		if !residual.Stage.valid() {
			return EmergencySettled{}, fmt.Errorf("invalid residual stage %d", residual.Stage)
		}
	}
	return EmergencySettled{resolutions: slices.Clone(resolutions), result: cloneEmergencyResult(result)}, nil
}

func (EmergencySettled) processRuntimeEvent() {}

// Resolutions returns an independent copy of the submitted resolutions.
func (event EmergencySettled) Resolutions() []Resolution { return slices.Clone(event.resolutions) }

// Result returns an independent copy of the emergency result.
func (event EmergencySettled) Result() EmergencyResult { return cloneEmergencyResult(event.result) }

// TerminalCommitted reports accepted campaign terminal commitment.
type TerminalCommitted struct {
	campaign Campaign
	result   TerminalResult
}

// NewTerminalCommitted validates and creates a terminal-commitment event.
func NewTerminalCommitted(campaign Campaign, result TerminalResult) (TerminalCommitted, error) {
	if !result.Decision.valid() {
		return TerminalCommitted{}, fmt.Errorf("invalid terminal decision %d", result.Decision)
	}
	return TerminalCommitted{campaign: campaign, result: result}, nil
}

func (TerminalCommitted) processRuntimeEvent() {}

// Campaign returns the campaign requesting terminal commitment.
func (event TerminalCommitted) Campaign() Campaign { return event.campaign }

// Result returns the terminal-commitment result.
func (event TerminalCommitted) Result() TerminalResult { return event.result }

// ForcedAbortAuthorized reports accepted forced-abort authorization.
type ForcedAbortAuthorized struct {
	campaign Campaign
	epoch    uint64
	result   TerminalResult
}

// NewForcedAbortAuthorized validates and creates a forced-abort event.
func NewForcedAbortAuthorized(campaign Campaign, epoch uint64, result TerminalResult) (ForcedAbortAuthorized, error) {
	if !result.Decision.valid() {
		return ForcedAbortAuthorized{}, fmt.Errorf("invalid terminal decision %d", result.Decision)
	}
	return ForcedAbortAuthorized{campaign: campaign, epoch: epoch, result: result}, nil
}

func (ForcedAbortAuthorized) processRuntimeEvent() {}

// Campaign returns the campaign requesting forced abort.
func (event ForcedAbortAuthorized) Campaign() Campaign { return event.campaign }

// Epoch returns the fatal epoch authorizing forced abort.
func (event ForcedAbortAuthorized) Epoch() uint64 { return event.epoch }

// Result returns the terminal result.
func (event ForcedAbortAuthorized) Result() TerminalResult { return event.result }

// RuntimeClosed reports accepted process-runtime closure.
type RuntimeClosed struct {
	cause  string
	result ClosureResult
}

// NewRuntimeClosed validates and creates a runtime-closure event.
func NewRuntimeClosed(cause string, result ClosureResult) (RuntimeClosed, error) {
	for _, residual := range result.Residual {
		if !residual.Stage.valid() {
			return RuntimeClosed{}, fmt.Errorf("invalid residual stage %d", residual.Stage)
		}
	}
	if !admissionsValid(result.CancelledWaiting) || !admissionsValid(result.CompensatedGrants) {
		return RuntimeClosed{}, fmt.Errorf("invalid runtime closure event")
	}
	return RuntimeClosed{cause: cause, result: cloneClosureResult(result)}, nil
}

func (RuntimeClosed) processRuntimeEvent() {}

// Cause returns the fatal cause that closed the runtime.
func (event RuntimeClosed) Cause() string { return event.cause }

// Result returns an independent copy of the closure result.
func (event RuntimeClosed) Result() ClosureResult { return cloneClosureResult(event.result) }

// Observer reserves an accepted owner cut before receiving its event.
type Observer interface{ Begin() func(Event) error }

// ObserverFunc adapts a reservation function to Observer.
type ObserverFunc func() func(Event) error

// Begin reserves an accepted owner cut and returns its event recipient.
func (observe ObserverFunc) Begin() func(Event) error { return observe() }

func validateAdmissionEvent(admission Admission, result AdmissionResult) error {
	if !admission.valid() || !result.Decision.valid() || !admissionsValid(result.Deliveries) {
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
