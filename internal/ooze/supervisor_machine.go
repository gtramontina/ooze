package ooze

import "time"

type supervisorMachine struct {
	state supervisorState
}

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
	generation attemptGeneration
	attempt    attemptIdentity
	at         supervisionInstant
}

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
		event:   supervisionEventFromFact(accepted),
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

func (machine *supervisorMachine) EmergencyActive() bool {
	return machine != nil && machine.state.emergency.active
}

func (machine *supervisorMachine) PendingEmergencyAction() supervisorPendingAction {
	if machine == nil {
		return supervisorPendingAction{}
	}

	return machine.state.emergency.pendingAction
}

func (machine *supervisorMachine) Attempt(generation attemptGeneration) (supervisionAttemptState, bool) {
	if machine == nil {
		return supervisionAttemptState{}, false
	}
	projection := machine.Projection()
	for _, attempt := range projection.attempts {
		if attempt.generation == generation {
			return attempt, true
		}
	}

	return supervisionAttemptState{}, false
}

func (machine *supervisorMachine) PrepareEmergency(at, drainBy time.Time) (supervisionFact, bool) {
	state := machine.state
	for _, attempt := range state.attempts {
		if attempt.lastEventAt.After(at) {
			at = attempt.lastEventAt
		}
	}
	for _, attempt := range state.attempts {
		if attempt.phase == supervisorLaunchEstablishing && at.After(attempt.launchBy) {
			return supervisionFact{}, false
		}
	}
	snapshots := make([]supervisorEmergencySnapshot, 0, len(state.attempts))
	for _, attempt := range state.attempts {
		if attempt.phase == supervisorLaunchClosedNotReleased {
			continue
		}
		snapshot := supervisorEmergencySnapshot{generation: attempt.generation}
		if attempt.phase == supervisorRunning || attempt.phase == supervisorIntentLatched {
			snapshot.running = &supervisorRunningBundle{
				generation: attempt.generation, waitAction: attempt.waitAction, sampleAction: attempt.sampleAction,
			}
			if attempt.phase == supervisorRunning && !at.Before(attempt.deadlineAt) {
				snapshot.running.exitRecheck = supervisorExitRecheck{performed: true, at: attempt.deadlineAt}
				snapshot.running.drainBy = attempt.deadlineAt.Add(5 * time.Second)
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

func cloneSupervisorEvent(event supervisorEvent) supervisorEvent {
	if event.completion != nil {
		completion := *event.completion
		event.completion = &completion
	}
	if event.running != nil {
		running := *event.running
		running.facts = append([]supervisorRunningFact(nil), running.facts...)
		event.running = &running
	}
	if event.drain != nil {
		drain := *event.drain
		event.drain = &drain
	}
	if event.output != nil {
		output := *event.output
		event.output = &output
	}
	if event.seal != nil {
		seal := *event.seal
		event.seal = &seal
	}
	if event.release != nil {
		release := *event.release
		event.release = &release
	}
	if event.runtime != nil {
		runtime := *event.runtime
		event.runtime = &runtime
	}
	if event.emergencySettlement != nil {
		settlement := *event.emergencySettlement
		settlement.acknowledged = append([]attemptGeneration(nil), settlement.acknowledged...)
		settlement.residuals = append([]supervisorEmergencyResolution(nil), settlement.residuals...)
		event.emergencySettlement = &settlement
	}
	if event.emergencySnapshots != nil {
		event.emergencySnapshots = append([]supervisorEmergencySnapshot(nil), event.emergencySnapshots...)
		for index := range event.emergencySnapshots {
			snapshot := &event.emergencySnapshots[index]
			if snapshot.completion != nil {
				completion := *snapshot.completion
				snapshot.completion = &completion
			}
			if snapshot.running != nil {
				running := *snapshot.running
				running.facts = append([]supervisorRunningFact(nil), running.facts...)
				snapshot.running = &running
			}
		}
	}

	return event
}

func cloneSupervisorActions(actions []supervisorAction) []supervisorAction {
	cloned := append([]supervisorAction(nil), actions...)
	for index := range cloned {
		cloned[index].resolutions = append([]supervisorEmergencyResolution(nil), cloned[index].resolutions...)
		cloned[index].residuals = append([]supervisorEmergencyResidual(nil), cloned[index].residuals...)
	}

	return cloned
}

func cloneSupervisionFact(fact supervisionFact) supervisionFact {
	return supervisionFactFromEvent(fact.production())
}

func cloneSupervisionEffects(effects []supervisionEffect) []supervisionEffect {
	return supervisionEffectsFromActions(supervisorActionsFromEffects(effects))
}

func cloneSupervisionProjection(projection supervisionProjection) supervisionProjection {
	projection.attempts = append([]supervisionAttemptState(nil), projection.attempts...)

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

func supervisionEventFromFact(fact supervisionFact) supervisorDomainEvent {
	kind := supervisionEventKind(0)
	switch fact.kind {
	case supervisorProspectiveRegistered:
		kind = supervisionAttemptRegistered
	case supervisorLaunchCompleted:
		kind = supervisionLaunchResolved
	case supervisorLaunchBoundary:
		kind = supervisionLaunchBoundaryReached
	case supervisorEmergencyStarted:
		kind = supervisionEmergencyStartedEvent
	case supervisorRunningObserved:
		kind = supervisionRunningEvidenceAccepted
	case supervisorDrainCompleted:
		kind = supervisionDrainEvidenceAccepted
	case supervisorOutputCompleted:
		kind = supervisionOutputAccepted
	case supervisorStopAdmissionSealed:
		kind = supervisionStopAdmissionClosed
	case supervisorReleaseCompleted:
		kind = supervisionDomainReleased
	case supervisorRuntimeCompleted:
		kind = supervisionRuntimeReceiptAccepted
	case supervisorEmergencySettlementCompleted:
		kind = supervisionEmergencySettlementAccepted
	}

	return supervisorDomainEvent{kind: kind, generation: fact.generation, attempt: fact.attempt, at: fact.at}
}
