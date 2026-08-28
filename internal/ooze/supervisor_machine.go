package ooze

import (
	"reflect"
	"slices"
	"sync/atomic"
	"time"
)

type supervisorMachine struct {
	state supervisorState
}

type supervisionOwnerCutReservation uint64

type supervisionOwnerCutSequence struct {
	atomic.Uint64
}

type supervisionOwnerCut struct {
	reservation supervisionOwnerCutReservation
	fact        supervisionFact
	event       supervisorDomainEvent
	projection  supervisionProjection
	effects     []supervisionEffect
}

type supervisionRegistration struct {
	generation      attemptGeneration
	attempt         attemptIdentity
	profile         Profile
	commandDeadline time.Duration
}

func (fact supervisionFact) Registration() (supervisionRegistration, bool) {
	if fact.kind != supervisorProspectiveRegistered {
		return supervisionRegistration{}, false
	}

	return supervisionRegistration{
		generation: fact.generation, attempt: fact.attempt, profile: fact.profile,
		commandDeadline: fact.commandDeadline.production(),
	}, true
}

func (fact supervisionFact) StopGeneration() (attemptGeneration, bool) {
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

func (fact supervisionFact) CausalEffect() (supervisorActionToken, bool) {
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

type supervisionOwnerCutObserver interface {
	Enter() func()
	Publish(supervisionOwnerCutReservation, supervisionFact, supervisorDomainEvent, supervisionProjection, []supervisionEffect)
	Complete(supervisionEffect)
}

type supervisionNoopObserver struct{}

func (supervisionNoopObserver) Enter() func() {
	return func() {}
}

func (supervisionNoopObserver) Publish(
	supervisionOwnerCutReservation,
	supervisionFact,
	supervisorDomainEvent,
	supervisionProjection,
	[]supervisionEffect,
) {
}

func (supervisionNoopObserver) Complete(supervisionEffect) {}

type supervisorTransition struct {
	event   supervisorDomainEvent
	effects []supervisionEffect
	state   supervisionProjection
}

type supervisionEventKind uint8

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

type supervisorDomainEvent struct {
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

type supervisionLaunchOutcome uint8

const (
	supervisionLaunchReleasedBeforeBoundary supervisionLaunchOutcome = iota
	supervisionLaunchReleasedAtBoundary
	supervisionLaunchReleasedAfterBoundary
	supervisionLaunchProvenNotReleased
)

type supervisionRunningOutcome uint8

const (
	supervisionRunningPassed supervisionRunningOutcome = iota
	supervisionRunningFailed
	supervisionRunningAtDeadline
	supervisionRunningFuse
	supervisionRunningAfterDeadline
)

type supervisionStopDisposition uint8

const (
	supervisionStopAbsent supervisionStopDisposition = iota
	supervisionStopNotReady
	supervisionStopReady
	supervisionStopResolved
)

type supervisionCompletionPosition uint8

const (
	supervisionCompletionBeforeBoundary supervisionCompletionPosition = iota
	supervisionCompletionAtBoundary
	supervisionCompletionAfterBoundary
)

func newSupervisorMachine() *supervisorMachine {
	return &supervisorMachine{}
}

func newSupervisorMachineFrom(state supervisorState) *supervisorMachine {
	return &supervisorMachine{state: cloneSupervisorState(state)}
}

func (machine *supervisorMachine) Apply(fact supervisionFact) (*supervisorMachine, supervisorTransition) {
	accepted := cloneSupervisionFact(fact)
	next, actions := reduceSupervisor(machine.state, accepted.production())
	projection := supervisionProjectionFromState(next)

	return newSupervisorMachineFrom(next), supervisorTransition{
		event:   supervisionEventFromTransition(accepted, next),
		effects: supervisionEffectsFromActions(actions),
		state:   projection,
	}
}

func (machine *supervisorMachine) snapshot() supervisorState {
	return cloneSupervisorState(machine.state)
}

func (machine *supervisorMachine) Projection() supervisionProjection {
	if machine == nil {
		return supervisionProjectionFromState(supervisorState{})
	}

	return supervisionProjectionFromState(machine.state)
}

func (projection supervisionProjection) Equal(other supervisionProjection) bool {
	return reflect.DeepEqual(projection, other)
}

func (projection supervisionProjection) Quiescent() bool {
	return len(projection.value.attempts) == 0 && !projection.value.emergency.active
}

func (projection supervisionProjection) BoundaryDistance(
	fact supervisionFact,
	origins []supervisionEffect,
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

func (machine *supervisorMachine) Fork() *supervisorMachine {
	if machine == nil {
		return newSupervisorMachine()
	}

	return newSupervisorMachineFrom(machine.state)
}

func (machine *supervisorMachine) LaunchFacts(
	effect supervisionEffect,
	outcome supervisionLaunchOutcome,
) ([]supervisionFact, bool) {
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
	var facts []supervisionFact
	var drainBy time.Time
	switch outcome {
	case supervisionLaunchReleasedBeforeBoundary:
	case supervisionLaunchReleasedAtBoundary:
		completedAt = launchBy
		eventKind = supervisorLaunchBoundary
	case supervisionLaunchReleasedAfterBoundary:
		facts = append(facts, supervisionFactFromEvent(supervisorEvent{
			kind: supervisorLaunchBoundary, generation: effect.generation, at: launchBy,
		}))
		completedAt = launchBy.Add(time.Nanosecond)
		drainBy = completedAt.Add(5 * time.Second)
	case supervisionLaunchProvenNotReleased:
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

func (machine *supervisorMachine) RunningFact(
	wait supervisionEffect,
	sample supervisionEffect,
	outcome supervisionRunningOutcome,
) (supervisionFact, bool) {
	if machine == nil || wait.kind != supervisorWaitRoot {
		return supervisionFact{}, false
	}
	index := machine.state.attemptIndex(wait.generation)
	if index < 0 {
		return supervisionFact{}, false
	}
	attempt := machine.state.attempts[index]
	if attempt.waitAction != wait.token ||
		(sample.kind != 0 && (sample.kind != supervisorSampleRunning || sample.generation != wait.generation ||
			attempt.sampleAction != sample.token)) {
		return supervisionFact{}, false
	}
	observedAt := attempt.startedAt.Add(time.Second)
	drainBy := observedAt.Add(5 * time.Second)
	bundle := &supervisorRunningBundle{
		generation: wait.generation, sampleAction: sample.token, waitAction: wait.token,
	}
	switch outcome {
	case supervisionRunningPassed, supervisionRunningFailed:
		exitCode := 0
		if outcome == supervisionRunningFailed {
			exitCode = 1
		}
		bundle.facts = []supervisorRunningFact{{
			generation: wait.generation, action: wait.token,
			kind: supervisorRunningRootExited, at: observedAt, exitCode: exitCode,
		}}
	case supervisionRunningAtDeadline:
		observedAt = attempt.deadlineAt
		drainBy = observedAt.Add(5 * time.Second)
		bundle.exitRecheck = supervisorExitRecheck{performed: true, at: observedAt}
	case supervisionRunningFuse:
		if attempt.profile != AutomaticProfile || sample.kind != supervisorSampleRunning {
			return supervisionFact{}, false
		}
		bundle.facts = []supervisorRunningFact{{
			generation: wait.generation, action: sample.token,
			kind: supervisorRunningFuseObserved, at: observedAt, rootLive: true, live: supervisorFuseCeiling + 1,
		}}
	case supervisionRunningAfterDeadline:
		observedAt = attempt.deadlineAt.Add(time.Nanosecond)
		drainBy = attempt.deadlineAt.Add(5 * time.Second)
		bundle.exitRecheck = supervisorExitRecheck{performed: true, at: attempt.deadlineAt}
		bundle.facts = []supervisorRunningFact{{
			generation: wait.generation, action: wait.token,
			kind: supervisorRunningRootExited, at: observedAt,
		}}
	default:
		return supervisionFact{}, false
	}

	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorRunningObserved, generation: wait.generation, at: observedAt,
		drainBy: drainBy, running: bundle,
	}), true
}

func (machine *supervisorMachine) StopFact(generation attemptGeneration) (
	supervisionFact,
	supervisionStopDisposition,
) {
	if machine == nil {
		return supervisionFact{}, supervisionStopAbsent
	}
	index := machine.state.attemptIndex(generation)
	if index < 0 {
		return supervisionFact{}, supervisionStopAbsent
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
		}), supervisionStopReady
	case supervisorReleasingDomain, supervisorTransferringResidualCustody,
		supervisorSettlingRuntime, supervisorAwaitingEmergencySettlement:
		return supervisionFact{}, supervisionStopResolved
	default:
		return supervisionFact{}, supervisionStopNotReady
	}
}

func (machine *supervisorMachine) CompletionFact(
	effect supervisionEffect,
	position supervisionCompletionPosition,
) (supervisionFact, bool) {
	if machine == nil {
		return supervisionFact{}, false
	}
	index := machine.state.attemptIndex(effect.generation)
	if index < 0 {
		return supervisionFact{}, false
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
		if position == supervisionCompletionAtBoundary || position == supervisionCompletionAfterBoundary {
			at = drainBy
			kind = supervisorDrainObservedResidual
		}
		if position == supervisionCompletionAfterBoundary {
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
		return supervisionFact{}, false
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

func (machine *supervisorMachine) DeterministicEmergencyEvidence(at time.Time) []supervisionEmergencyEvidence {
	if machine == nil {
		return nil
	}
	evidence := make([]supervisionEmergencyEvidence, 0, len(machine.state.attempts))
	for _, attempt := range machine.state.attempts {
		item := supervisionEmergencyEvidence{generation: attempt.generation}
		if (attempt.phase == supervisorRunning && !at.Before(attempt.deadlineAt)) ||
			attempt.phase == supervisorLaunchEstablishing {
			item.root = supervisionEmergencyRootEvidence{checked: true, at: at}
		}
		evidence = append(evidence, item)
	}

	return evidence
}

func (machine *supervisorMachine) AcceptsEmergencyRequest() bool {
	return machine != nil && !machine.state.emergency.active
}

func (machine *supervisorMachine) EmergencySettlementFact(
	effect supervisionEffect,
	acknowledged []attemptGeneration,
	residuals []supervisorEmergencyResolution,
) (supervisionFact, bool) {
	if machine == nil || effect.kind != supervisorSettleEmergency ||
		machine.state.emergency.pendingAction.kind != supervisorSettleEmergency ||
		machine.state.emergency.pendingAction.token != effect.token {
		return supervisionFact{}, false
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

func (machine *supervisorMachine) EmergencyRequest(at, drainBy time.Time) supervisionFact {
	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorEmergencyStarted, at: at, drainBy: drainBy,
	})
}

func (machine *supervisorMachine) RuntimeReceiptFact(
	effect supervisionEffect,
	kind supervisorRuntimeReceiptKind,
) (supervisionFact, bool) {
	if machine == nil {
		return supervisionFact{}, false
	}
	index := machine.state.attemptIndex(effect.generation)
	if index < 0 {
		return supervisionFact{}, false
	}
	attempt := machine.state.attempts[index]
	if attempt.pendingAction.kind != effect.kind || attempt.pendingAction.token != effect.token {
		return supervisionFact{}, false
	}
	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorRuntimeCompleted, generation: effect.generation,
		runtime: &supervisorRuntimeCompletion{
			generation: effect.generation, action: attempt.pendingAction, kind: kind,
		},
	}), true
}

func (machine *supervisorMachine) PrepareEmergency(
	at, drainBy time.Time,
	drainEpoch time.Duration,
	evidence []supervisionEmergencyEvidence,
) (supervisionFact, bool) {
	if machine == nil || machine.state.emergency.active || drainEpoch <= 0 {
		return supervisionFact{}, false
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
			return supervisionFact{}, false
		}
		if _, found := byGeneration[item.generation]; found {
			return supervisionFact{}, false
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
			return supervisionFact{}, false
		}
		snapshot := supervisorEmergencySnapshot{generation: attempt.generation, completion: item.completion}
		if attempt.phase == supervisorLaunchEstablishing && item.completion != nil &&
			item.completion.kind == supervisorLaunchReleased {
			if !item.root.checked {
				return supervisionFact{}, false
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
					return supervisionFact{}, false
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

func (transition supervisorTransition) Event() supervisorDomainEvent {
	return transition.event
}

func (transition supervisorTransition) Effects() []supervisionEffect {
	return cloneSupervisionEffects(transition.effects)
}

func (transition supervisorTransition) Projection() supervisionProjection {
	return cloneSupervisionProjection(transition.state)
}

func (transition supervisorTransition) actions() []supervisorAction {
	actions := make([]supervisorAction, len(transition.effects))
	for index, effect := range transition.effects {
		actions[index] = effect.production()
	}

	return actions
}

func cloneSupervisionFact(fact supervisionFact) supervisionFact {
	return supervisionFactFromEvent(fact.production())
}

func cloneSupervisionEffects(effects []supervisionEffect) []supervisionEffect {
	return supervisionEffectsFromActions(supervisorActionsFromEffects(effects))
}

func cloneSupervisionProjection(projection supervisionProjection) supervisionProjection {
	projection.value.attempts = append([]supervisionAttemptState(nil), projection.value.attempts...)

	return projection
}

func supervisionProspectiveRegistration(
	generation attemptGeneration,
	attempt attemptIdentity,
	registeredAt time.Time,
	launchBy time.Time,
	profile Profile,
	commandDeadline time.Duration,
) supervisionFact {
	return supervisionFactFromEvent(supervisorEvent{
		kind: supervisorProspectiveRegistered, generation: generation, attempt: attempt,
		at: registeredAt, launchBy: launchBy, profile: profile, commandDeadline: commandDeadline,
	})
}

func supervisionEventFromTransition(fact supervisionFact, state supervisorState) supervisorDomainEvent {
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

	return supervisorDomainEvent{
		kind: kind, outcome: outcome, generation: fact.generation, attempt: attempt, at: fact.at,
	}
}
