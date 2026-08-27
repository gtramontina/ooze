package processruntime

import "slices"

// EventKind identifies an accepted process-runtime command.
type EventKind uint8

// Accepted process-runtime commands.
const (
	RegisterCampaign EventKind = iota + 1
	RequestAdmission
	CancelAdmission
	AcknowledgeGrantReturn
	BindConfirmationBarrier
	CompleteConfirmationQueue
	StartCommitted
	ObserveAttempt
	SettleEmergency
	CommitTerminal
	AuthorizeForcedAbort
	Close
)

// Campaign identifies one registered campaign.
type Campaign struct {
	ID      uint64
	Lineage uint64
}

// Admission identifies one admission request or grant.
type Admission struct {
	Campaign Campaign
	Attempt  string
	Class    uint8
	Profile  Profile
	Deadline int64
}

// Observation is terminal or launch evidence for one attempt generation.
type Observation struct {
	Kind     ObservationKind
	Reason   uint8
	Profile  Profile
	Deadline int64
	Trip     uint8
	Cause    string
}

// ObservationKind identifies attempt evidence.
type ObservationKind uint8

// Process-runtime attempt observations.
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
	Disposition uint8
}

// Residual identifies custody left after an emergency operation.
type Residual struct {
	Generation  uint64
	Attempt     string
	Stage       uint8
	Transferred bool
}

// Command contains the fact accepted by one process-runtime transition.
type Command struct {
	Lineage     uint64
	Campaign    Campaign
	Admission   Admission
	Barrier     Admission
	Generation  uint64
	Observation Observation
	Resolutions []Resolution
	FatalCause  string
	FatalEpoch  uint64
}

// Registration is a campaign-registration result.
type Registration struct {
	Decision uint8
	Campaign Campaign
}

// AdmissionResult is an admission command result.
type AdmissionResult struct {
	Decision   uint8
	Request    Admission
	Deliveries []Admission
	FatalEpoch uint64
}

// BarrierResult is a confirmation-barrier result.
type BarrierResult struct {
	Decision   uint8
	Request    Admission
	Deliveries []Admission
}

// QueueResult is a completed-confirmation-queue result.
type QueueResult struct {
	Decision   uint8
	Deliveries []Admission
}

// StartResult is a start-commitment result.
type StartResult struct {
	Decision                 uint8
	Generation               uint64
	SettlementAcknowledged   bool
	RuntimeClosureInProgress bool
}

// ObservationResult is the process-runtime response to attempt evidence.
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

// EmergencyResult is the result of one emergency settlement.
type EmergencyResult struct {
	Epoch        uint64
	Owner        Campaign
	Acknowledged []uint64
	Residual     []Residual
}

// TerminalResult is a campaign terminal-commitment result.
type TerminalResult struct {
	Decision uint8
	Epoch    uint64
}

// ClosureResult is the result of closing the process runtime.
type ClosureResult struct {
	Epoch             uint64
	CancelledWaiting  []Admission
	CompensatedGrants []Admission
	Residual          []Residual
}

// Result contains the response to one accepted command.
type Result struct {
	Registration Registration
	Admission    AdmissionResult
	Barrier      BarrierResult
	Queue        QueueResult
	Start        StartResult
	Observation  ObservationResult
	Emergency    EmergencyResult
	Terminal     TerminalResult
	Closure      ClosureResult
}

// Event is an immutable accepted process-runtime command and result.
type Event struct {
	kind    EventKind
	command Command
	result  Result
}

// Accepted creates an immutable event from an accepted command and result.
func Accepted(kind EventKind, command Command, result Result) Event {
	return Event{kind: kind, command: cloneCommand(command), result: cloneResult(result)}
}

// Kind returns the accepted command kind.
func (event Event) Kind() EventKind { return event.kind }

// Command returns a copy of the accepted command.
func (event Event) Command() Command { return cloneCommand(event.command) }

// Result returns a copy of the command result.
func (event Event) Result() Result { return cloneResult(event.result) }

// Observer reserves an owner cut before receiving its accepted event.
type Observer interface {
	Begin() func(Event) error
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func() func(Event) error

// Begin reserves an owner cut and returns its event recipient.
func (observe ObserverFunc) Begin() func(Event) error { return observe() }

func cloneCommand(command Command) Command {
	command.Resolutions = slices.Clone(command.Resolutions)
	return command
}

func cloneResult(result Result) Result {
	result.Admission.Deliveries = slices.Clone(result.Admission.Deliveries)
	result.Barrier.Deliveries = slices.Clone(result.Barrier.Deliveries)
	result.Queue.Deliveries = slices.Clone(result.Queue.Deliveries)
	result.Observation.Deliveries = slices.Clone(result.Observation.Deliveries)
	result.Observation.CancelledWaiting = slices.Clone(result.Observation.CancelledWaiting)
	result.Observation.CompensatedGrants = slices.Clone(result.Observation.CompensatedGrants)
	result.Emergency.Acknowledged = slices.Clone(result.Emergency.Acknowledged)
	result.Emergency.Residual = slices.Clone(result.Emergency.Residual)
	result.Closure.CancelledWaiting = slices.Clone(result.Closure.CancelledWaiting)
	result.Closure.CompensatedGrants = slices.Clone(result.Closure.CompensatedGrants)
	result.Closure.Residual = slices.Clone(result.Closure.Residual)
	return result
}
