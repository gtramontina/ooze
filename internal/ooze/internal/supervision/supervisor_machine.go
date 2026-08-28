package supervision

import (
	"reflect"
	"slices"
	"sync/atomic"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
)

// Machine applies canonical facts through the production supervision reducer.
type Machine struct {
	state supervisorState
}

// OwnerCutReservation orders one accepted owner transition.
type OwnerCutReservation uint64

// OwnerCutSequence allocates process-wide owner transition order.
type OwnerCutSequence struct {
	atomic.Uint64
}

type ownerCut struct {
	reservation OwnerCutReservation
	fact        Fact
	event       Event
	projection  Projection
	effects     []Effect
}

// Registration is the immutable prospective-attempt registration fact.
type Registration struct {
	generation      attemptGeneration
	attempt         attemptIdentity
	profile         Profile
	commandDeadline time.Duration
}

// Generation returns the registered execution-domain generation.
func (registration Registration) Generation() Generation { return registration.generation }

// Attempt returns the registered attempt identity.
func (registration Registration) Attempt() Identity { return registration.attempt }

// Profile returns the registered execution profile.
func (registration Registration) Profile() Profile { return registration.profile }

// CommandDeadline returns the resolved command deadline.
func (registration Registration) CommandDeadline() time.Duration { return registration.commandDeadline }

// Registration returns prospective registration evidence when present.
func (fact Fact) Registration() (Registration, bool) {
	if fact.kind != supervisorProspectiveRegistered {
		return Registration{}, false
	}

	return Registration{
		generation: fact.generation, attempt: fact.attempt, profile: fact.profile,
		commandDeadline: fact.commandDeadline.production(),
	}, true
}

// StopGeneration returns the generation named by a stop request.
func (fact Fact) StopGeneration() (attemptGeneration, bool) {
	if fact.kind != supervisorRunningObserved || fact.running == nil {
		return 0, false
	}
	for _, running := range fact.running.facts {
		if running.kind == supervisorRunningStopRequested {
			return fact.generation, true
		}
	}

	return 0, false
}

// CausalEffect returns the effect token completed by this fact.
func (fact Fact) CausalEffect() (supervisorActionToken, bool) {
	var token supervisorActionToken
	switch fact.kind {
	case supervisorLaunchCompleted, supervisorLaunchBoundary:
		if fact.completion != nil {
			token = fact.completion.action
		}
	case supervisorRunningObserved:
		if fact.running != nil {
			token = fact.running.waitAction
			if token == 0 {
				token = fact.running.sampleAction
			}
		}
	case supervisorDrainCompleted:
		if fact.drain != nil {
			token = fact.drain.action.token
		}
	case supervisorOutputCompleted:
		if fact.output != nil {
			token = fact.output.action.token
		}
	case supervisorStopAdmissionSealed:
		if fact.seal != nil {
			token = fact.seal.action.token
		}
	case supervisorReleaseCompleted:
		if fact.release != nil {
			token = fact.release.action.token
		}
	case supervisorRuntimeCompleted:
		if fact.runtime != nil {
			token = fact.runtime.action.token
		}
	case supervisorEmergencySettlementCompleted:
		if fact.emergencySettlement != nil {
			token = fact.emergencySettlement.action.token
		}
	}

	return token, token != 0
}

// OccurredAt returns the fact's normalized instant.
func (fact Fact) OccurredAt() time.Time {
	return fact.at.production()
}

// Kind returns the normalized fact kind.
func (fact Fact) Kind() FactKind { return fact.kind }

// Generation returns the execution-domain generation named by the fact.
func (fact Fact) Generation() Generation { return fact.generation }

// LaunchEvidence returns the launch boundary and completion instant when present.
func (fact Fact) LaunchEvidence() (time.Time, time.Time, bool) {
	if fact.completion == nil {
		return fact.launchBy.production(), time.Time{}, false
	}
	return fact.launchBy.production(), fact.completion.at.production(), true
}

// HasRootExitAfterRecheck reports whether accepted root-exit evidence follows its deadline recheck.
func (fact Fact) HasRootExitAfterRecheck() bool {
	if fact.running == nil || !fact.running.exitRecheck.performed {
		return false
	}
	for _, running := range fact.running.facts {
		if running.kind == supervisorRunningRootExited && running.at.production().After(fact.running.exitRecheck.at.production()) {
			return true
		}
	}
	return false
}

// HasStopRequest reports whether the fact carries a running stop request.
func (fact Fact) HasStopRequest() bool {
	if fact.running == nil {
		return false
	}
	return slices.ContainsFunc(fact.running.facts, func(running supervisionRunningFact) bool {
		return running.kind == supervisorRunningStopRequested
	})
}

// LaunchCompletionWithAction returns a released completion correlated with the supplied action.
func (fact Fact) LaunchCompletionWithAction(action ActionToken, at time.Time) Fact {
	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorLaunchCompleted, generation: fact.generation, at: at,
		completion: &supervisorLaunchCompletion{
			generation: fact.generation, action: action, at: at, kind: supervisorLaunchReleased,
		},
	})
}

// MalformedWithoutKind returns a fact with its kind removed for violation replay.
func (fact Fact) MalformedWithoutKind(at time.Time) Fact {
	return supervisionFactFromEvent(supervisorEvent{generation: fact.generation, at: at})
}

// MalformedContradictoryRelease returns released evidence carrying an impossible launch failure.
func (fact Fact) MalformedContradictoryRelease(action ActionToken, at time.Time) Fact {
	value := fact.LaunchCompletionWithAction(action, at)
	value.completion.failure = LaunchFailed
	return value
}

// MalformedOutputCompletion returns output evidence while the attempt remains in launch.
func (fact Fact) MalformedOutputCompletion(action ActionToken, at time.Time) Fact {
	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorOutputCompleted, generation: fact.generation, at: at,
		output: &supervisorOutputCompletion{
			generation: fact.generation,
			action:     supervisorPendingAction{kind: supervisorCaptureOutput, token: action},
			at:         at, ref: 1,
		},
	})
}

// MalformedEffect returns an incomplete effect of the requested kind for replay divergence tests.
func MalformedEffect(kind EffectKind) Effect { return Effect{kind: kind} }

// CorrelatedMalformedEffect creates an intentionally malformed correlated effect.
func CorrelatedMalformedEffect(kind EffectKind, generation Generation, token ActionToken) Effect {
	return Effect{kind: kind, generation: generation, token: token}
}

// CorrelatedMalformedFact creates an intentionally malformed correlated fact.
func CorrelatedMalformedFact(kind FactKind, generation Generation) Fact {
	return Fact{kind: kind, generation: generation}
}

// RunningBoundaryFact returns root-exit evidence relative to a completed deadline recheck.
func RunningBoundaryFact(recheckAt, rootExitAt time.Time) Fact {
	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorRunningObserved,
		running: &supervisorRunningBundle{
			exitRecheck: supervisorExitRecheck{performed: true, at: recheckAt},
			facts:       []supervisorRunningFact{{kind: supervisorRunningRootExited, at: rootExitAt}},
		},
	})
}

// WithRootExitAt moves the first root-exit observation to the supplied instant.
func (fact Fact) WithRootExitAt(at time.Time) Fact {
	rewritten := cloneSupervisionFact(fact)
	if rewritten.running != nil {
		for index := range rewritten.running.facts {
			if rewritten.running.facts[index].kind == supervisorRunningRootExited {
				rewritten.running.facts[index].at = supervisionInstantFromTime(at)
				break
			}
		}
	}
	return rewritten
}

// WithoutRunningEvidence removes all running observations while preserving the fact envelope.
func (fact Fact) WithoutRunningEvidence() Fact {
	rewritten := cloneSupervisionFact(fact)
	if rewritten.running != nil {
		rewritten.running.facts = nil
	}
	return rewritten
}

// RuntimeCorrelation returns the generation and effect token acknowledged by a runtime receipt fact.
func (fact Fact) RuntimeCorrelation() (Generation, ActionToken, bool) {
	if fact.kind != supervisorRuntimeCompleted || fact.runtime == nil {
		return 0, 0, false
	}
	return fact.generation, fact.runtime.action.token, true
}

// Equal reports whether two facts carry identical normalized evidence.
func (fact Fact) Equal(other Fact) bool { return reflect.DeepEqual(fact, other) }

// Complexity returns the fact's semantic payload size for deterministic shrinking.
func (fact Fact) Complexity() int {
	rank := len(fact.emergencySnapshots)
	for _, present := range []bool{
		fact.completion != nil, fact.running != nil, fact.drain != nil, fact.output != nil,
		fact.seal != nil, fact.release != nil, fact.runtime != nil, fact.emergencySettlement != nil,
	} {
		if present {
			rank++
		}
	}
	if fact.running != nil {
		rank += len(fact.running.facts)
	}
	return rank
}

// RewriteCorrelated rewrites identities shared with before to their values in after.
func (fact Fact) RewriteCorrelated(before, after Fact) Fact {
	rewritten := cloneSupervisionFact(fact)
	if rewritten.generation == before.generation {
		rewritten.generation = after.generation
	}
	if rewritten.kind == before.kind {
		rewritten.kind = after.kind
	}
	if rewritten.attempt == before.attempt {
		rewritten.attempt = after.attempt
	}
	if rewritten.at == before.at {
		rewritten.at = after.at
	}
	if rewritten.launchBy == before.launchBy {
		rewritten.launchBy = after.launchBy
	}
	if rewritten.drainBy == before.drainBy {
		rewritten.drainBy = after.drainBy
	}
	if rewritten.completion == nil || before.completion == nil || after.completion == nil {
		return rewritten
	}
	if rewritten.completion.generation == before.completion.generation {
		rewritten.completion.generation = after.completion.generation
	}
	if rewritten.completion.action == before.completion.action {
		rewritten.completion.action = after.completion.action
	}
	if rewritten.completion.at == before.completion.at {
		rewritten.completion.at = after.completion.at
	}
	return rewritten
}

// OwnerCutObserver receives accepted supervision transitions and effect completion.
type OwnerCutObserver interface {
	Enter() func()
	Publish(OwnerCutReservation, Fact, Event, Projection, []Effect)
	Complete(Effect)
}

type noopObserver struct{}

func (noopObserver) Enter() func() {
	return func() {}
}

func (noopObserver) Publish(
	OwnerCutReservation,
	Fact,
	Event,
	Projection,
	[]Effect,
) {
}

func (noopObserver) Complete(Effect) {}

// Transition contains one accepted event, its effects, and its projection.
type Transition struct {
	event   Event
	effects []Effect
	state   Projection
}

// EventKind identifies one published supervision event.
type EventKind uint8

type supervisionEventKind = EventKind

const (
	supervisionAttemptRegistered supervisionEventKind = iota + 1
	supervisionLaunchResolved
	supervisionLaunchBoundaryReached
	supervisionEmergencyStartedEvent
	supervisionRunningEvidenceAccepted
	supervisionDrainEvidenceAccepted
	supervisionOutputAccepted
	supervisionStopAdmissionClosed
	supervisionDomainReleased
	supervisionRuntimeReceiptAccepted
	supervisionEmergencySettlementAccepted
)

const (
	// AttemptRegisteredEvent records prospective launch registration.
	AttemptRegisteredEvent EventKind = supervisionAttemptRegistered
	// LaunchResolvedEvent records launch-boundary evidence.
	LaunchResolvedEvent EventKind = supervisionLaunchResolved
	// LaunchBoundaryReachedEvent records an unresolved launch deadline.
	LaunchBoundaryReachedEvent EventKind = supervisionLaunchBoundaryReached
	// EmergencyStartedEvent records a global emergency cut.
	EmergencyStartedEvent EventKind = supervisionEmergencyStartedEvent
	// RunningEvidenceAcceptedEvent records accepted running evidence.
	RunningEvidenceAcceptedEvent EventKind = supervisionRunningEvidenceAccepted
	// DrainEvidenceAcceptedEvent records accepted drainage evidence.
	DrainEvidenceAcceptedEvent EventKind = supervisionDrainEvidenceAccepted
	// OutputAcceptedEvent records immutable output capture.
	OutputAcceptedEvent EventKind = supervisionOutputAccepted
	// StopAdmissionClosedEvent records closed stop admission.
	StopAdmissionClosedEvent EventKind = supervisionStopAdmissionClosed
	// DomainReleasedEvent records native-domain release.
	DomainReleasedEvent EventKind = supervisionDomainReleased
	// RuntimeReceiptAcceptedEvent records accepted runtime custody.
	RuntimeReceiptAcceptedEvent EventKind = supervisionRuntimeReceiptAccepted
	// EmergencySettlementAcceptedEvent records accepted global settlement.
	EmergencySettlementAcceptedEvent EventKind = supervisionEmergencySettlementAccepted
)

// Event is the published domain event for one accepted fact.
type Event struct {
	kind       supervisionEventKind
	outcome    supervisionEventOutcome
	generation attemptGeneration
	attempt    attemptIdentity
	at         supervisionInstant
}

type supervisionEventOutcome uint8

const (
	supervisionNoEventOutcome supervisionEventOutcome = iota
	supervisionLaunchReleasedEvent
	supervisionLaunchNotReleasedEvent
	supervisionLaunchUnconfirmedEvent
	supervisionRunningRootExitEvent
	supervisionRunningFuseEvent
	supervisionRunningDeadlineEvent
	supervisionRunningStopEvent
	supervisionRunningEmergencyEvent
	supervisionRunningFailureEvent
	supervisionDrainForcedEvent
	supervisionDrainEmptyEvent
	supervisionDrainResidualEvent
	supervisionRuntimeAcknowledgedEvent
	supervisionRuntimeProvisionalEvent
	supervisionRuntimeClosurePendingEvent
)

// LaunchOutcome selects canonical launch-boundary evidence.
type LaunchOutcome uint8

const (
	// LaunchReleasedBeforeBoundary places release before LaunchBy.
	LaunchReleasedBeforeBoundary LaunchOutcome = iota
	// LaunchReleasedAtBoundary places release exactly at LaunchBy.
	LaunchReleasedAtBoundary
	// LaunchReleasedAfterBoundary places release after LaunchBy.
	LaunchReleasedAfterBoundary
	// LaunchProvenNotReleased proves release did not occur.
	LaunchProvenNotReleased
)

// RunningOutcome selects canonical running evidence.
type RunningOutcome uint8

const (
	// RunningPassed reports successful root exit.
	RunningPassed RunningOutcome = iota
	// RunningFailed reports unsuccessful root exit.
	RunningFailed
	// RunningAtDeadline reports evidence exactly at the command deadline.
	RunningAtDeadline
	// RunningFuse reports a descendant fuse crossing.
	RunningFuse
	// RunningAfterDeadline reports evidence after the command deadline.
	RunningAfterDeadline
)

// StopDisposition describes whether a stop fact can be accepted.
type StopDisposition uint8

const (
	// StopAbsent reports an unknown generation.
	StopAbsent StopDisposition = iota
	// StopNotReady reports a known generation outside stop admission.
	StopNotReady
	// StopReady reports a generation that accepts a stop fact.
	StopReady
	// StopResolved reports a generation whose stop intent is already fixed.
	StopResolved
)

// CompletionPosition places completion evidence around its bound.
type CompletionPosition uint8

const (
	// CompletionBeforeBoundary places completion before its bound.
	CompletionBeforeBoundary CompletionPosition = iota
	// CompletionAtBoundary places completion exactly at its bound.
	CompletionAtBoundary
	// CompletionAfterBoundary places completion after its bound.
	CompletionAfterBoundary
)

// NewMachine creates an empty supervision machine.
func NewMachine() *Machine {
	return &Machine{}
}

func newMachineFrom(state supervisorState) *Machine {
	return &Machine{state: cloneSupervisorState(state)}
}

// Apply reduces one canonical fact without mutating the receiver.
func (machine *Machine) Apply(fact Fact) (*Machine, Transition) {
	accepted := cloneSupervisionFact(fact)
	next, actions := reduceSupervisor(machine.state, accepted.production())
	projection := supervisionProjectionFromState(next)

	return newMachineFrom(next), Transition{
		event:   supervisionEventFromTransition(accepted, next),
		effects: supervisionEffectsFromActions(actions),
		state:   projection,
	}
}

func (machine *Machine) snapshot() supervisorState {
	return cloneSupervisorState(machine.state)
}

// Projection returns the machine's opaque comparable projection.
func (machine *Machine) Projection() Projection {
	if machine == nil {
		return supervisionProjectionFromState(supervisorState{})
	}

	return supervisionProjectionFromState(machine.state)
}

func (machine *Machine) monitorDeadline(generation attemptGeneration) (time.Time, bool) {
	if machine == nil {
		return time.Time{}, false
	}
	index := machine.state.attemptIndex(generation)
	if index < 0 || machine.state.attempts[index].deadlineAt.IsZero() {
		return time.Time{}, false
	}

	return machine.state.attempts[index].deadlineAt, true
}

func (machine *Machine) acceptsRunningObservation(generation attemptGeneration) bool {
	if machine == nil {
		return false
	}
	index := machine.state.attemptIndex(generation)
	if index < 0 {
		return false
	}
	switch machine.state.attempts[index].phase {
	case supervisorRunning, supervisorIntentLatched, supervisorEmergencyDraining:
		return true
	default:
		return false
	}
}

func (machine *Machine) emergencyEvidenceGenerations() []attemptGeneration {
	if machine == nil {
		return nil
	}
	generations := make([]attemptGeneration, 0, len(machine.state.attempts))
	for _, attempt := range machine.state.attempts {
		if attempt.phase != supervisorLaunchClosedNotReleased {
			generations = append(generations, attempt.generation)
		}
	}

	return generations
}

// Equal reports whether two projections describe the same state.
func (projection Projection) Equal(other Projection) bool {
	return reflect.DeepEqual(projection, other)
}

// Quiescent reports whether the projection has no pending effect.
func (projection Projection) Quiescent() bool {
	return len(projection.value.attempts) == 0 && !projection.value.emergency.active
}

// BoundaryDistance measures fact and effect distance from recorded boundaries.
func (projection Projection) BoundaryDistance(
	fact Fact,
	origins []Effect,
) int {
	index := slices.IndexFunc(projection.value.attempts, func(attempt supervisionAttemptState) bool {
		return attempt.generation == fact.generation
	})
	var boundary supervisionInstant
	switch fact.kind {
	case supervisorLaunchCompleted, supervisorLaunchBoundary:
		if index >= 0 {
			boundary = projection.value.attempts[index].launchBy
		}
	case supervisorRunningObserved:
		if fact.running != nil && fact.running.exitRecheck.performed {
			boundary = fact.running.exitRecheck.at
		} else if index >= 0 {
			boundary = projection.value.attempts[index].deadlineAt
		}
	case supervisorDrainCompleted:
		if fact.drain != nil {
			for _, origin := range origins {
				if origin.token == fact.drain.action.token {
					boundary = origin.drainBy
					break
				}
			}
		}
	}
	if fact.kind == supervisorRunningObserved && fact.running != nil {
		distance := 0
		for _, running := range fact.running.facts {
			distance += supervisionInstantDistance(running.at, boundary)
		}

		return distance
	}

	return supervisionInstantDistance(fact.at, boundary)
}

func supervisionInstantDistance(left, right supervisionInstant) int {
	if !left.set || !right.set {
		return 0
	}
	distance := left.production().Sub(right.production())
	if distance == time.Duration(-1<<63) {
		return int(^uint(0) >> 1)
	}
	if distance < 0 {
		distance = -distance
	}
	maximum := int(^uint(0) >> 1)
	if uint64(distance) > uint64(maximum) {
		return maximum
	}

	return int(distance)
}

// Fork returns an independent machine at the same state.
func (machine *Machine) Fork() *Machine {
	if machine == nil {
		return NewMachine()
	}

	return newMachineFrom(machine.state)
}

// LaunchFacts returns canonical evidence for one launch effect.
func (machine *Machine) LaunchFacts(
	effect Effect,
	outcome LaunchOutcome,
) ([]Fact, bool) {
	if machine == nil || effect.kind != supervisorLaunchNative {
		return nil, false
	}
	index := machine.state.attemptIndex(effect.generation)
	if index < 0 {
		return nil, false
	}
	attempt := machine.state.attempts[index]
	if attempt.launchAction != effect.token || attempt.phase != supervisorLaunchEstablishing {
		return nil, false
	}
	launchBy := attempt.launchBy
	completedAt := launchBy.Add(-time.Nanosecond)
	completionKind := supervisorLaunchReleased
	eventKind := supervisorLaunchCompleted
	var failure LaunchFailure
	var facts []Fact
	var drainBy time.Time
	switch outcome {
	case LaunchReleasedBeforeBoundary:
	case LaunchReleasedAtBoundary:
		completedAt = launchBy
		eventKind = supervisorLaunchBoundary
	case LaunchReleasedAfterBoundary:
		facts = append(facts, supervisionFactFromEvent(supervisorEvent{
			kind: supervisorLaunchBoundary, generation: effect.generation, at: launchBy,
		}))
		completedAt = launchBy.Add(time.Nanosecond)
		drainBy = completedAt.Add(5 * time.Second)
	case LaunchProvenNotReleased:
		completionKind = supervisorLaunchProvenNotReleased
		failure = LaunchFailed
	default:
		return nil, false
	}
	completion := supervisorLaunchCompletion{
		generation: effect.generation, action: effect.token, at: completedAt,
		kind: completionKind, failure: failure,
	}
	facts = append(facts, supervisionFactFromEvent(supervisorEvent{
		kind: eventKind, generation: effect.generation, at: completedAt,
		drainBy: drainBy, completion: &completion,
	}))

	return facts, true
}

// LaunchReleasedFact returns release evidence correlated with this launch effect.
func (effect Effect) LaunchReleasedFact(at time.Time) (Fact, bool) {
	if effect.kind != supervisorLaunchNative || at.IsZero() {
		return Fact{}, false
	}
	completion := supervisorLaunchCompletion{
		generation: effect.generation, action: effect.token, at: at, kind: supervisorLaunchReleased,
	}

	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorLaunchCompleted, generation: effect.generation, at: at, completion: &completion,
	}), true
}

// LaunchNotReleasedFact returns correlated proof that this launch did not cross release.
func (effect Effect) LaunchNotReleasedFact(
	at time.Time,
	failure LaunchFailure,
	diagnostic uint64,
) (Fact, bool) {
	if effect.kind != supervisorLaunchNative || at.IsZero() ||
		(failure != LaunchFailed && failure != LaunchResourceExhausted) {
		return Fact{}, false
	}
	completion := supervisorLaunchCompletion{
		generation: effect.generation, action: effect.token, at: at,
		kind: supervisorLaunchProvenNotReleased, failure: failure,
		diagnostic: supervisorDiagnosticRef(diagnostic),
	}

	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorLaunchCompleted, generation: effect.generation, at: at, completion: &completion,
	}), true
}

// RunningFact returns canonical evidence for correlated monitor effects.
func (machine *Machine) RunningFact(
	wait Effect,
	sample Effect,
	outcome RunningOutcome,
) (Fact, bool) {
	if machine == nil || wait.kind != supervisorWaitRoot {
		return Fact{}, false
	}
	index := machine.state.attemptIndex(wait.generation)
	if index < 0 {
		return Fact{}, false
	}
	attempt := machine.state.attempts[index]
	if attempt.waitAction != wait.token ||
		(sample.kind != 0 && (sample.kind != supervisorSampleRunning || sample.generation != wait.generation ||
			attempt.sampleAction != sample.token)) {
		return Fact{}, false
	}
	observedAt := attempt.startedAt.Add(time.Second)
	drainBy := observedAt.Add(5 * time.Second)
	bundle := &supervisorRunningBundle{
		generation: wait.generation, sampleAction: sample.token, waitAction: wait.token,
	}
	switch outcome {
	case RunningPassed, RunningFailed:
		exitCode := 0
		if outcome == RunningFailed {
			exitCode = 1
		}
		bundle.facts = []supervisorRunningFact{{
			generation: wait.generation, action: wait.token,
			kind: supervisorRunningRootExited, at: observedAt, exitCode: exitCode,
		}}
	case RunningAtDeadline:
		observedAt = attempt.deadlineAt
		drainBy = observedAt.Add(5 * time.Second)
		bundle.exitRecheck = supervisorExitRecheck{performed: true, at: observedAt}
	case RunningFuse:
		if attempt.profile != AutomaticProfile || sample.kind != supervisorSampleRunning {
			return Fact{}, false
		}
		bundle.facts = []supervisorRunningFact{{
			generation: wait.generation, action: sample.token,
			kind: supervisorRunningFuseObserved, at: observedAt, rootLive: true, live: supervisorFuseCeiling + 1,
		}}
	case RunningAfterDeadline:
		observedAt = attempt.deadlineAt.Add(time.Nanosecond)
		drainBy = attempt.deadlineAt.Add(5 * time.Second)
		bundle.exitRecheck = supervisorExitRecheck{performed: true, at: attempt.deadlineAt}
		bundle.facts = []supervisorRunningFact{{
			generation: wait.generation, action: wait.token,
			kind: supervisorRunningRootExited, at: observedAt,
		}}
	default:
		return Fact{}, false
	}

	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorRunningObserved, generation: wait.generation, at: observedAt,
		drainBy: drainBy, running: bundle,
	}), true
}

// RootExitFact returns root-exit evidence correlated with this wait effect.
func (effect Effect) RootExitFact(
	at time.Time,
	status ExitStatus,
) (Fact, bool) {
	if effect.kind != supervisorWaitRoot || at.IsZero() {
		return Fact{}, false
	}

	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorRunningObserved, generation: effect.generation, at: at,
		drainBy: at.Add(5 * time.Second),
		running: &supervisorRunningBundle{
			generation: effect.generation, waitAction: effect.token,
			facts: []supervisorRunningFact{{
				generation: effect.generation, action: effect.token,
				kind: supervisorRunningRootExited, at: at,
				exitCode: status.Code, exitSignal: status.Signal,
			}},
		},
	}), true
}

// WaitFailureFact returns failed root-wait evidence correlated with this effect.
func (effect Effect) WaitFailureFact(at time.Time, diagnostic uint64) (Fact, bool) {
	if effect.kind != supervisorWaitRoot || at.IsZero() || diagnostic == 0 {
		return Fact{}, false
	}

	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorRunningObserved, generation: effect.generation, at: at,
		drainBy: at.Add(5 * time.Second),
		running: &supervisorRunningBundle{
			generation: effect.generation, waitAction: effect.token,
			facts: []supervisorRunningFact{{
				generation: effect.generation, action: effect.token,
				kind: supervisorRunningObservationFailed, at: at,
				source: supervisorObservationWait, diagnostic: supervisorDiagnosticRef(diagnostic),
			}},
		},
	}), true
}

// SystemCompletionFact returns successful native completion evidence for this effect.
func (effect Effect) SystemCompletionFact(at time.Time) (Fact, bool) {
	if at.IsZero() {
		return Fact{}, false
	}
	pending := supervisorPendingAction{kind: effect.kind, token: effect.token}
	switch effect.kind {
	case supervisorForceOwned:
		return supervisionFactFromEvent(supervisorEvent{
			kind: supervisorDrainCompleted, generation: effect.generation, at: at,
			drain: &supervisorDrainCompletion{
				generation: effect.generation, action: pending, at: at, kind: supervisorDrainForceCompleted,
			},
		}), true
	case supervisorObserveEmptiness:
		return supervisionFactFromEvent(supervisorEvent{
			kind: supervisorDrainCompleted, generation: effect.generation, at: at,
			drain: &supervisorDrainCompletion{
				generation: effect.generation, action: pending, at: at, kind: supervisorDrainObservedEmpty,
			},
		}), true
	case supervisorCaptureOutput:
		return supervisionFactFromEvent(supervisorEvent{
			kind: supervisorOutputCompleted, generation: effect.generation, at: at,
			output: &supervisorOutputCompletion{
				generation: effect.generation, action: pending, at: at, ref: 1,
			},
		}), true
	case supervisorReleaseDomain:
		return supervisionFactFromEvent(supervisorEvent{
			kind: supervisorReleaseCompleted, generation: effect.generation, at: at,
			release: &supervisorReleaseCompletion{
				generation: effect.generation, action: pending, at: at,
			},
		}), true
	default:
		return Fact{}, false
	}
}

// DrainResidualFact returns authoritative residual evidence for this census effect.
func (effect Effect) DrainResidualFact(at time.Time) (Fact, bool) {
	if effect.kind != supervisorObserveEmptiness || at.IsZero() {
		return Fact{}, false
	}
	pending := supervisorPendingAction{kind: effect.kind, token: effect.token}

	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorDrainCompleted, generation: effect.generation, at: at,
		drain: &supervisorDrainCompletion{
			generation: effect.generation, action: pending, at: at, kind: supervisorDrainObservedResidual,
		},
	}), true
}

// DrainFailureFact returns native force or census failure evidence for this effect.
func (effect Effect) DrainFailureFact(
	at time.Time,
	waitDiagnostic uint64,
	diagnostic uint64,
) (Fact, bool) {
	if at.IsZero() || diagnostic == 0 || waitDiagnostic == diagnostic {
		return Fact{}, false
	}
	kind := supervisorDrainForceCompleted
	switch effect.kind {
	case supervisorForceOwned:
	case supervisorObserveEmptiness:
		kind = supervisorDrainObservationFailed
	default:
		return Fact{}, false
	}
	pending := supervisorPendingAction{kind: effect.kind, token: effect.token}

	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorDrainCompleted, generation: effect.generation, at: at,
		drain: &supervisorDrainCompletion{
			generation: effect.generation, action: pending, at: at, kind: kind,
			waitDiagnostic: supervisorDiagnosticRef(waitDiagnostic),
			diagnostic:     supervisorDiagnosticRef(diagnostic),
		},
	}), true
}

// OutputFailureFact returns partial output and its independent failure evidence.
func (effect Effect) OutputFailureFact(
	at time.Time,
	ref uint64,
	cutoff uint64,
	prefixLength uint64,
	waitDiagnostic uint64,
	diagnostic uint64,
) (Fact, bool) {
	if effect.kind != supervisorCaptureOutput || at.IsZero() || ref == 0 || diagnostic == 0 ||
		prefixLength > cutoff || prefixLength == cutoff || waitDiagnostic == diagnostic {
		return Fact{}, false
	}
	pending := supervisorPendingAction{kind: effect.kind, token: effect.token}

	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorOutputCompleted, generation: effect.generation, at: at,
		output: &supervisorOutputCompletion{
			generation: effect.generation, action: pending, at: at,
			ref: supervisorOutputRef(ref), cutoff: cutoff, prefixLength: prefixLength,
			waitDiagnostic: supervisorDiagnosticRef(waitDiagnostic),
			diagnostic:     supervisorDiagnosticRef(diagnostic),
		},
	}), true
}

// ReleaseFailureFact returns domain-release failure evidence for this effect.
func (effect Effect) ReleaseFailureFact(at time.Time, diagnostic uint64) (Fact, bool) {
	if effect.kind != supervisorReleaseDomain || at.IsZero() || diagnostic == 0 {
		return Fact{}, false
	}
	pending := supervisorPendingAction{kind: effect.kind, token: effect.token}

	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorReleaseCompleted, generation: effect.generation, at: at,
		release: &supervisorReleaseCompletion{
			generation: effect.generation, action: pending, at: at,
			diagnostic: supervisorDiagnosticRef(diagnostic),
		},
	}), true
}

// StopFact returns the canonical stop request allowed by current state.
func (machine *Machine) StopFact(generation attemptGeneration) (
	Fact,
	StopDisposition,
) {
	if machine == nil {
		return Fact{}, StopAbsent
	}
	index := machine.state.attemptIndex(generation)
	if index < 0 {
		return Fact{}, StopAbsent
	}
	attempt := machine.state.attempts[index]
	switch attempt.phase {
	case supervisorRunning, supervisorIntentLatched, supervisorEmergencyDraining:
		at := attempt.lastEventAt.Add(time.Nanosecond)
		drainBy := at.Add(5 * time.Second)
		return supervisionFactFromEvent(supervisorEvent{
			kind: supervisorRunningObserved, generation: generation, at: at, drainBy: drainBy,
			running: &supervisorRunningBundle{
				generation: generation, waitAction: attempt.waitAction, sampleAction: attempt.sampleAction,
				facts: []supervisorRunningFact{{
					generation: generation, kind: supervisorRunningStopRequested,
					at: at, stop: StopRequest{At: at, DrainBy: drainBy},
				}},
			},
		}), StopReady
	case supervisorReleasingDomain, supervisorTransferringResidualCustody,
		supervisorSettlingRuntime, supervisorAwaitingEmergencySettlement:
		return Fact{}, StopResolved
	default:
		return Fact{}, StopNotReady
	}
}

// CompletionFact returns canonical completion evidence for one effect.
func (machine *Machine) CompletionFact(
	effect Effect,
	position CompletionPosition,
) (Fact, bool) {
	if machine == nil {
		return Fact{}, false
	}
	index := machine.state.attemptIndex(effect.generation)
	if index < 0 {
		return Fact{}, false
	}
	attempt := machine.state.attempts[index]
	pending := supervisorPendingAction{kind: effect.kind, token: effect.token}
	normalize := func(at time.Time) time.Time {
		if attempt.lastEventAt.After(at) {
			return attempt.lastEventAt
		}

		return at
	}
	switch effect.kind {
	case supervisorForceOwned:
		at := normalize(effect.at.production().Add(time.Nanosecond))
		return supervisionFactFromEvent(supervisorEvent{
			kind: supervisorDrainCompleted, generation: effect.generation, at: at,
			drain: &supervisorDrainCompletion{
				generation: effect.generation, action: pending, at: at, kind: supervisorDrainForceCompleted,
			},
		}), true
	case supervisorObserveEmptiness:
		drainBy := effect.drainBy.production()
		at := drainBy.Add(-time.Nanosecond)
		kind := supervisorDrainObservedEmpty
		if position == CompletionAtBoundary || position == CompletionAfterBoundary {
			at = drainBy
			kind = supervisorDrainObservedResidual
		}
		if position == CompletionAfterBoundary {
			at = drainBy.Add(time.Nanosecond)
		}
		at = normalize(at)
		return supervisionFactFromEvent(supervisorEvent{
			kind: supervisorDrainCompleted, generation: effect.generation, at: at,
			drain: &supervisorDrainCompletion{
				generation: effect.generation, action: pending, at: at, kind: kind,
			},
		}), true
	case supervisorCaptureOutput:
		at := normalize(effect.at.production())
		return supervisionFactFromEvent(supervisorEvent{
			kind: supervisorOutputCompleted, generation: effect.generation, at: at,
			output: &supervisorOutputCompletion{
				generation: effect.generation, action: pending, at: at, ref: 1,
			},
		}), true
	case supervisorSealStopAdmission:
		at := normalize(effect.at.production())
		return supervisionFactFromEvent(supervisorEvent{
			kind: supervisorStopAdmissionSealed, generation: effect.generation, at: at,
			seal: &supervisorStopSealCompletion{generation: effect.generation, action: pending, at: at},
		}), true
	case supervisorReleaseDomain:
		at := normalize(effect.at.production())
		return supervisionFactFromEvent(supervisorEvent{
			kind: supervisorReleaseCompleted, generation: effect.generation, at: at,
			release: &supervisorReleaseCompletion{generation: effect.generation, action: pending, at: at},
		}), true
	default:
		return Fact{}, false
	}
}

type supervisionEmergencyRootEvidence struct {
	checked    bool
	observed   bool
	at         time.Time
	exitCode   int
	exitSignal int
}

type supervisionEmergencyEvidence struct {
	generation attemptGeneration
	completion *supervisorLaunchCompletion
	root       supervisionEmergencyRootEvidence
}

type supervisionEmergencyRootRequest struct {
	generation attemptGeneration
	required   bool
}

type supervisionEmergencyPlan struct {
	at         time.Time
	drainBy    time.Time
	drainEpoch time.Duration
	projection Projection
	evidence   []supervisionEmergencyEvidence
	roots      []supervisionEmergencyRootRequest
	returning  []attemptGeneration
}

func (machine *Machine) planEmergency(
	at, drainBy time.Time,
	drainEpoch time.Duration,
	launchEvidence []supervisionEmergencyEvidence,
) (supervisionEmergencyPlan, bool) {
	if machine == nil || !machine.AcceptsEmergencyRequest() || drainEpoch <= 0 {
		return supervisionEmergencyPlan{}, false
	}
	completionByGeneration := make(map[attemptGeneration]supervisionEmergencyEvidence, len(launchEvidence))
	for _, evidence := range launchEvidence {
		if evidence.generation == 0 || machine.state.attemptIndex(evidence.generation) < 0 ||
			completionByGeneration[evidence.generation].generation != 0 {
			return supervisionEmergencyPlan{}, false
		}
		completionByGeneration[evidence.generation] = evidence
	}
	plan := supervisionEmergencyPlan{
		at: at, drainBy: drainBy, drainEpoch: drainEpoch, projection: machine.Projection(),
		evidence: make([]supervisionEmergencyEvidence, 0, len(machine.state.attempts)),
	}
	for _, attempt := range machine.state.attempts {
		if attempt.phase == supervisorLaunchClosedNotReleased {
			continue
		}
		evidence := supervisionEmergencyEvidence{}
		switch attempt.phase {
		case supervisorLaunchEstablishing, supervisorLaunchReportedUnconfirmed:
			evidence = completionByGeneration[attempt.generation]
		}
		evidence.generation = attempt.generation
		plan.evidence = append(plan.evidence, evidence)
		switch attempt.phase {
		case supervisorLaunchEstablishing:
			plan.returning = append(plan.returning, attempt.generation)
			if evidence.completion != nil && evidence.completion.kind == supervisorLaunchReleased {
				plan.roots = append(plan.roots, supervisionEmergencyRootRequest{
					generation: attempt.generation, required: true,
				})
			}
		case supervisorRunning:
			plan.roots = append(plan.roots, supervisionEmergencyRootRequest{
				generation: attempt.generation, required: !at.Before(attempt.deadlineAt),
			})
		}
	}

	return plan, true
}

func (plan supervisionEmergencyPlan) rootRequests() []supervisionEmergencyRootRequest {
	return append([]supervisionEmergencyRootRequest(nil), plan.roots...)
}

func (plan supervisionEmergencyPlan) returningLaunches() []attemptGeneration {
	return append([]attemptGeneration(nil), plan.returning...)
}

func (plan supervisionEmergencyPlan) deterministicRootEvidence() []supervisionEmergencyEvidence {
	evidence := make([]supervisionEmergencyEvidence, len(plan.roots))
	for index, request := range plan.roots {
		evidence[index] = supervisionEmergencyEvidence{
			generation: request.generation,
			root:       supervisionEmergencyRootEvidence{checked: true, at: plan.at},
		}
	}

	return evidence
}

func (machine *Machine) prepareEmergencyPlan(
	plan supervisionEmergencyPlan,
	rootEvidence []supervisionEmergencyEvidence,
) (Fact, bool) {
	if machine == nil || !machine.Projection().Equal(plan.projection) {
		return Fact{}, false
	}
	evidence := append([]supervisionEmergencyEvidence(nil), plan.evidence...)
	for _, root := range rootEvidence {
		index := slices.IndexFunc(evidence, func(item supervisionEmergencyEvidence) bool {
			return item.generation == root.generation
		})
		if index < 0 || !root.root.checked {
			return Fact{}, false
		}
		evidence[index].root = root.root
	}
	for _, request := range plan.roots {
		index := slices.IndexFunc(evidence, func(item supervisionEmergencyEvidence) bool {
			return item.generation == request.generation
		})
		if request.required && (index < 0 || !evidence[index].root.checked) {
			return Fact{}, false
		}
	}

	return machine.prepareEmergency(plan.at, plan.drainBy, plan.drainEpoch, evidence)
}

// AcceptsEmergencyRequest reports whether a first emergency may start.
func (machine *Machine) AcceptsEmergencyRequest() bool {
	return machine != nil && !machine.state.emergency.active
}

// DeterministicEmergencyFact returns an emergency fact with deterministic root evidence.
func (machine *Machine) DeterministicEmergencyFact(
	at time.Time,
	drainEpoch time.Duration,
) (Fact, bool) {
	if machine == nil || drainEpoch <= 0 {
		return Fact{}, false
	}
	for _, attempt := range machine.state.attempts {
		if attempt.lastEventAt.After(at) {
			at = attempt.lastEventAt
		}
	}
	plan, ready := machine.planEmergency(at, at.Add(drainEpoch), drainEpoch, nil)
	if !ready {
		return Fact{}, false
	}

	return machine.prepareEmergencyPlan(plan, plan.deterministicRootEvidence())
}

func (machine *Machine) emergencySettlementFact(
	effect Effect,
	acknowledged []attemptGeneration,
	residuals []supervisorEmergencyResolution,
) (Fact, bool) {
	if machine == nil || effect.kind != supervisorSettleEmergency ||
		machine.state.emergency.pendingAction.kind != supervisorSettleEmergency ||
		machine.state.emergency.pendingAction.token != effect.token {
		return Fact{}, false
	}
	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorEmergencySettlementCompleted,
		emergencySettlement: &supervisorEmergencySettlementCompletion{
			action:       machine.state.emergency.pendingAction,
			acknowledged: append([]attemptGeneration(nil), acknowledged...),
			residuals:    append([]supervisorEmergencyResolution(nil), residuals...),
		},
	}), true
}

func (machine *Machine) emergencySettlementRequest(
	effect Effect,
) ([]supervisorEmergencyResolution, bool) {
	if machine == nil || effect.kind != supervisorSettleEmergency || effect.token == 0 ||
		len(effect.residuals) != 0 || !machine.state.emergency.active ||
		machine.state.nextAction != effect.token ||
		machine.state.emergency.pendingAction != (supervisorPendingAction{
			kind: effect.kind, token: effect.token,
		}) {
		return nil, false
	}

	return slices.Clone(effect.resolutions), true
}

func (machine *Machine) emergencyDelivery(
	effect Effect,
) ([]supervisorEmergencyResidual, bool) {
	if machine == nil || effect.kind != supervisorDeliverEmergencySettlement || effect.token == 0 ||
		len(effect.resolutions) != 0 || !machine.state.emergency.active ||
		machine.state.emergency.pendingAction != (supervisorPendingAction{}) ||
		machine.state.nextAction != effect.token {
		return nil, false
	}

	return slices.Clone(effect.residuals), true
}

// EmergencyRequest returns a canonical global emergency fact.
func (machine *Machine) EmergencyRequest(at, drainBy time.Time) Fact {
	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorEmergencyStarted, at: at, drainBy: drainBy,
	})
}

func (machine *Machine) runtimeReceiptFact(
	effect Effect,
	kind supervisorRuntimeReceiptKind,
) (Fact, bool) {
	if machine == nil {
		return Fact{}, false
	}
	index := machine.state.attemptIndex(effect.generation)
	if index < 0 {
		return Fact{}, false
	}
	attempt := machine.state.attempts[index]
	if attempt.pendingAction.kind != effect.kind || attempt.pendingAction.token != effect.token {
		return Fact{}, false
	}
	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorRuntimeCompleted, generation: effect.generation,
		runtime: &supervisorRuntimeCompletion{
			generation: effect.generation, action: attempt.pendingAction, kind: kind,
		},
	}), true
}

func (machine *Machine) prepareEmergency(
	at, drainBy time.Time,
	drainEpoch time.Duration,
	evidence []supervisionEmergencyEvidence,
) (Fact, bool) {
	if machine == nil || machine.state.emergency.active || drainEpoch <= 0 {
		return Fact{}, false
	}
	state := machine.state
	for _, attempt := range state.attempts {
		if attempt.lastEventAt.After(at) {
			at = attempt.lastEventAt
		}
	}
	byGeneration := make(map[attemptGeneration]supervisionEmergencyEvidence, len(evidence))
	for _, item := range evidence {
		if item.generation == 0 || state.attemptIndex(item.generation) < 0 {
			return Fact{}, false
		}
		if _, found := byGeneration[item.generation]; found {
			return Fact{}, false
		}
		byGeneration[item.generation] = item
	}
	snapshots := make([]supervisorEmergencySnapshot, 0, len(state.attempts))
	for _, attempt := range state.attempts {
		if attempt.phase == supervisorLaunchClosedNotReleased {
			continue
		}
		item := byGeneration[attempt.generation]
		if attempt.phase == supervisorLaunchEstablishing && at.After(attempt.launchBy) && item.completion == nil {
			return Fact{}, false
		}
		snapshot := supervisorEmergencySnapshot{generation: attempt.generation, completion: item.completion}
		if attempt.phase == supervisorLaunchEstablishing && item.completion != nil &&
			item.completion.kind == supervisorLaunchReleased {
			if !item.root.checked {
				return Fact{}, false
			}
			recheckAt := at
			if item.root.observed {
				recheckAt = item.root.at
			}
			snapshot.running = &supervisorRunningBundle{
				generation: attempt.generation,
				exitRecheck: supervisorExitRecheck{
					performed: true, observed: item.root.observed, at: recheckAt,
					action: attempt.launchAction, code: item.root.exitCode, signal: item.root.exitSignal,
				},
			}
			deadlineAt := item.completion.at.Add(attempt.commandDeadline)
			selectedAt := time.Time{}
			if item.root.observed && !item.root.at.After(deadlineAt) {
				selectedAt = item.root.at
			} else if !at.Before(deadlineAt) {
				selectedAt = deadlineAt
			}
			if !selectedAt.IsZero() {
				snapshot.running.drainBy = selectedAt.Add(drainEpoch)
			}
		}
		if attempt.phase == supervisorRunning || attempt.phase == supervisorIntentLatched {
			snapshot.running = &supervisorRunningBundle{
				generation: attempt.generation, waitAction: attempt.waitAction, sampleAction: attempt.sampleAction,
			}
			if item.root.checked && item.root.observed && !item.root.at.After(at) {
				snapshot.running.facts = append(snapshot.running.facts, supervisorRunningFact{
					generation: attempt.generation, action: attempt.waitAction,
					kind: supervisorRunningRootExited, at: item.root.at,
					exitCode: item.root.exitCode, exitSignal: item.root.exitSignal,
				})
			}
			if attempt.phase == supervisorRunning && !at.Before(attempt.deadlineAt) {
				if !item.root.checked {
					return Fact{}, false
				}
				snapshot.running.exitRecheck = supervisorExitRecheck{performed: true, at: attempt.deadlineAt}
				if item.root.observed && !item.root.at.After(attempt.deadlineAt) {
					snapshot.running.exitRecheck.observed = true
					snapshot.running.exitRecheck.code = item.root.exitCode
					snapshot.running.exitRecheck.signal = item.root.exitSignal
				}
				snapshot.running.drainBy = attempt.deadlineAt.Add(drainEpoch)
			}
		}
		snapshots = append(snapshots, snapshot)
	}

	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorEmergencyStarted, at: at, drainBy: drainBy, emergencySnapshots: snapshots,
	}), true
}

// Event returns the accepted domain event.
func (transition Transition) Event() Event {
	return transition.event
}

// Kind returns the published event kind.
func (event Event) Kind() EventKind { return event.kind }

// Generation returns the event's execution-domain generation.
func (event Event) Generation() Generation { return event.generation }

// Attempt returns the event's attempt identity.
func (event Event) Attempt() Identity { return event.attempt }

// OccurredAt returns the event's normalized instant.
func (event Event) OccurredAt() time.Time { return event.at.production() }

// Equal reports whether two published events are identical.
func (event Event) Equal(other Event) bool { return event == other }

// Effects returns detached effects in reducer order.
func (transition Transition) Effects() []Effect {
	return cloneSupervisionEffects(transition.effects)
}

// Projection returns the state after the accepted fact.
func (transition Transition) Projection() Projection {
	return cloneSupervisionProjection(transition.state)
}

// OccurredAt returns the effect's normalized instant.
func (effect Effect) OccurredAt() time.Time {
	return effect.at.production()
}

// Kind returns the normalized effect kind.
func (effect Effect) Kind() EffectKind { return effect.kind }

// Generation returns the execution-domain generation named by the effect.
func (effect Effect) Generation() Generation { return effect.generation }

// Token returns the effect's correlation token.
func (effect Effect) Token() ActionToken { return effect.token }

// DrainBy returns the effect's absolute drainage bound.
func (effect Effect) DrainBy() time.Time { return effect.drainBy.production() }

// Equal reports whether two effects are identical.
func (effect Effect) Equal(other Effect) bool { return reflect.DeepEqual(effect, other) }

// Terminal resolves a terminal-delivery effect with the supplied captured output.
func (effect Effect) Terminal(output string) (Terminal, bool) {
	if effect.kind != supervisorDeliverTerminal {
		return nil, false
	}
	return publicTerminalFromEffect(effect, func(supervisorOutputRef) string { return output }, nil), true
}

// LaunchObservation returns the process-runtime observation carried by a launch publication effect.
func (effect Effect) LaunchObservation() (processruntime.Observation, LaunchFailure, bool) {
	switch effect.kind {
	case supervisorPublishNotReleased, supervisorCloseProspective:
		return launchObservationFromEffect(effect), effect.launchFailure, true
	case supervisorPublishLaunchUnconfirmed:
		return processruntime.LaunchUnconfirmed(), 0, true
	case supervisorPublishOwned, supervisorAdoptOwned:
		return processruntime.Owned(), 0, true
	default:
		return processruntime.Observation{}, 0, false
	}
}

// TerminalObservation returns the process-runtime observation carried by terminal settlement.
func (effect Effect) TerminalObservation() (processruntime.Observation, bool) {
	if effect.kind != supervisorSettleRuntime {
		return processruntime.Observation{}, false
	}
	return terminalObservationFromEffect(effect), true
}

// runtimeReceiptFact converts a process-runtime receipt into the correlated supervision fact.
func (machine *Machine) RuntimeReceiptFactFor(effect Effect, receipt processruntime.Receipt) (Fact, bool) {
	return machine.runtimeReceiptFact(effect, normalizedSupervisorRuntimeReceipt(receipt))
}

// EmergencyResolutions returns the process-runtime resolutions requested by an emergency effect.
func (effect Effect) EmergencyResolutions() ([]processruntime.Resolution, bool) {
	if effect.kind != supervisorSettleEmergency {
		return nil, false
	}
	resolutions, _, _ := normalizeSupervisorEmergencyResolutions(effect.resolutions)
	return processRuntimeResolutions(emergencySweep{resolutions: resolutions}), true
}

// EmergencySettlementFactFor validates a runtime settlement and returns its correlated supervision fact.
func (machine *Machine) EmergencySettlementFactFor(
	effect Effect,
	settlement processruntime.EmergencySettlement,
) (Fact, bool) {
	resolutions, acknowledged, residuals := normalizeSupervisorEmergencyResolutions(effect.resolutions)
	if len(resolutions) == 0 && len(effect.resolutions) != 0 {
		return Fact{}, false
	}
	validateSupervisorRuntimeSettlement(runtimeEmergencySettlement(settlement), acknowledged, residuals)
	return machine.emergencySettlementFact(effect, acknowledged, residuals)
}

func (effect Effect) launchCompletion() (supervisionLaunchCompletion, bool) {
	switch effect.kind {
	case supervisorPublishNotReleased, supervisorCloseProspective:
		return supervisionLaunchCompletion{
			generation: effect.generation,
			kind:       effect.launchKind,
			failure:    effect.launchFailure,
		}, true
	default:
		return supervisionLaunchCompletion{}, false
	}
}

func (effect Effect) terminalEvidence() (
	supervisionTerminalEvidence,
	supervisorRuntimeReceiptKind,
	bool,
) {
	switch effect.kind {
	case supervisorSettleRuntime, supervisorDeliverTerminal:
		return effect.terminal, effect.runtimeKind, true
	default:
		return supervisionTerminalEvidence{}, 0, false
	}
}

func (transition Transition) actions() []supervisorAction {
	actions := make([]supervisorAction, len(transition.effects))
	for index, effect := range transition.effects {
		actions[index] = effect.production()
	}

	return actions
}

func cloneSupervisionFact(fact Fact) Fact {
	return supervisionFactFromEvent(fact.production())
}

func cloneSupervisionEffects(effects []Effect) []Effect {
	return supervisionEffectsFromActions(supervisorActionsFromEffects(effects))
}

func cloneSupervisionProjection(projection Projection) Projection {
	projection.value.attempts = append([]supervisionAttemptState(nil), projection.value.attempts...)

	return projection
}

func ProspectiveRegistration(
	generation attemptGeneration,
	attempt attemptIdentity,
	registeredAt time.Time,
	launchBy time.Time,
	profile Profile,
	commandDeadline time.Duration,
) Fact {
	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorProspectiveRegistered, generation: generation, attempt: attempt,
		at: registeredAt, launchBy: launchBy, profile: profile, commandDeadline: commandDeadline,
	})
}

func supervisionEventFromTransition(fact Fact, state supervisorState) Event {
	kind := supervisionEventKind(0)
	outcome := supervisionNoEventOutcome
	switch fact.kind {
	case supervisorProspectiveRegistered:
		kind = supervisionAttemptRegistered
	case supervisorLaunchCompleted:
		kind = supervisionLaunchResolved
		if fact.completion != nil {
			switch fact.completion.kind {
			case supervisorLaunchReleased:
				outcome = supervisionLaunchReleasedEvent
			case supervisorLaunchProvenNotReleased:
				outcome = supervisionLaunchNotReleasedEvent
			case supervisorLaunchReleaseUnconfirmed:
				outcome = supervisionLaunchUnconfirmedEvent
			}
		}
	case supervisorLaunchBoundary:
		kind = supervisionLaunchBoundaryReached
		if fact.completion != nil {
			switch fact.completion.kind {
			case supervisorLaunchReleased:
				outcome = supervisionLaunchReleasedEvent
			case supervisorLaunchProvenNotReleased:
				outcome = supervisionLaunchNotReleasedEvent
			case supervisorLaunchReleaseUnconfirmed:
				outcome = supervisionLaunchUnconfirmedEvent
			}
		}
	case supervisorEmergencyStarted:
		kind = supervisionEmergencyStartedEvent
	case supervisorRunningObserved:
		kind = supervisionRunningEvidenceAccepted
		index := state.attemptIndex(fact.generation)
		if index >= 0 {
			switch state.attempts[index].intent.kind {
			case supervisorIntentRootExit:
				outcome = supervisionRunningRootExitEvent
			case supervisorIntentFuse:
				outcome = supervisionRunningFuseEvent
			case supervisorIntentDeadline:
				outcome = supervisionRunningDeadlineEvent
			case supervisorIntentStop:
				outcome = supervisionRunningStopEvent
			case supervisorIntentRuntimeEmergency:
				outcome = supervisionRunningEmergencyEvent
			case supervisorIntentObservationFailure:
				outcome = supervisionRunningFailureEvent
			}
		}
	case supervisorDrainCompleted:
		kind = supervisionDrainEvidenceAccepted
		if fact.drain != nil {
			switch fact.drain.kind {
			case supervisorDrainForceCompleted:
				outcome = supervisionDrainForcedEvent
			case supervisorDrainObservedEmpty:
				outcome = supervisionDrainEmptyEvent
			case supervisorDrainObservedResidual:
				outcome = supervisionDrainResidualEvent
			}
		}
	case supervisorOutputCompleted:
		kind = supervisionOutputAccepted
	case supervisorStopAdmissionSealed:
		kind = supervisionStopAdmissionClosed
	case supervisorReleaseCompleted:
		kind = supervisionDomainReleased
	case supervisorRuntimeCompleted:
		kind = supervisionRuntimeReceiptAccepted
		if fact.runtime != nil {
			switch fact.runtime.kind {
			case supervisorRuntimeAcknowledged:
				outcome = supervisionRuntimeAcknowledgedEvent
			case supervisorRuntimeProvisionalDeadline:
				outcome = supervisionRuntimeProvisionalEvent
			case supervisorRuntimeClosurePending:
				outcome = supervisionRuntimeClosurePendingEvent
			}
		}
	case supervisorEmergencySettlementCompleted:
		kind = supervisionEmergencySettlementAccepted
	}

	attempt := fact.attempt
	if attempt == "" {
		index := state.attemptIndex(fact.generation)
		if index >= 0 {
			attempt = state.attempts[index].attempt
		}
	}

	return Event{
		kind: kind, outcome: outcome, generation: fact.generation, attempt: attempt, at: fact.at,
	}
}
