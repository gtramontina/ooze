package processruntime

import "slices"

type EventKind uint8

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

type Campaign struct {
	ID      uint64
	Lineage uint64
}

type Admission struct {
	Campaign Campaign
	Attempt  string
	Class    uint8
	Profile  Profile
	Deadline int64
}

type Observation struct {
	Kind     ObservationKind
	Reason   uint8
	Profile  Profile
	Deadline int64
	Trip     uint8
	Cause    string
}

type ObservationKind uint8

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

type Resolution struct {
	Generation  uint64
	Disposition uint8
}

type Residual struct {
	Generation  uint64
	Attempt     string
	Stage       uint8
	Transferred bool
}

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

type Registration struct {
	Decision uint8
	Campaign Campaign
}

type AdmissionResult struct {
	Decision   uint8
	Request    Admission
	Deliveries []Admission
	FatalEpoch uint64
}

type BarrierResult struct {
	Decision   uint8
	Request    Admission
	Deliveries []Admission
}

type QueueResult struct {
	Decision   uint8
	Deliveries []Admission
}

type StartResult struct {
	Decision                 uint8
	Generation               uint64
	SettlementAcknowledged   bool
	RuntimeClosureInProgress bool
}

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

type EmergencyResult struct {
	Epoch        uint64
	Owner        Campaign
	Acknowledged []uint64
	Residual     []Residual
}

type TerminalResult struct {
	Decision uint8
	Epoch    uint64
}

type ClosureResult struct {
	Epoch             uint64
	CancelledWaiting  []Admission
	CompensatedGrants []Admission
	Residual          []Residual
}

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

type Event struct {
	kind    EventKind
	command Command
	result  Result
}

func Accepted(kind EventKind, command Command, result Result) Event {
	return Event{kind: kind, command: cloneCommand(command), result: cloneResult(result)}
}

func (event Event) Kind() EventKind { return event.kind }

func (event Event) Command() Command { return cloneCommand(event.command) }

func (event Event) Result() Result { return cloneResult(event.result) }

type Observer interface {
	Begin() func(Event)
}

type ObserverFunc func() func(Event)

func (observe ObserverFunc) Begin() func(Event) { return observe() }

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
