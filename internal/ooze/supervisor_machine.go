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

type supervisorDomainEvent struct {
	fact supervisionFact
}

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
		event:   supervisorDomainEvent{fact: accepted},
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
	return supervisorDomainEvent{fact: cloneSupervisionFact(transition.event.fact)}
}

func (event supervisorDomainEvent) Fact() supervisionFact {
	return cloneSupervisionFact(event.fact)
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
