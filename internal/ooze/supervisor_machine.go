package ooze

type supervisorMachine struct {
	state supervisorState
}

type supervisorTransition struct {
	event   supervisorDomainEvent
	effects []supervisorAction
}

type supervisorDomainEvent struct {
	fact supervisorEvent
}

func newSupervisorMachine() *supervisorMachine {
	return &supervisorMachine{}
}

func newSupervisorMachineFrom(state supervisorState) *supervisorMachine {
	return &supervisorMachine{state: cloneSupervisorState(state)}
}

func (machine *supervisorMachine) Apply(fact supervisorEvent) supervisorTransition {
	fact = cloneSupervisorEvent(fact)
	next, effects := reduceSupervisor(machine.state, fact)
	machine.state = next

	return supervisorTransition{
		event:   supervisorDomainEvent{fact: fact},
		effects: cloneSupervisorActions(effects),
	}
}

func (machine *supervisorMachine) snapshot() supervisorState {
	return cloneSupervisorState(machine.state)
}

func (transition supervisorTransition) Event() supervisorDomainEvent {
	return supervisorDomainEvent{fact: cloneSupervisorEvent(transition.event.fact)}
}

func (event supervisorDomainEvent) Fact() supervisorEvent {
	return cloneSupervisorEvent(event.fact)
}

func (transition supervisorTransition) Effects() []supervisorAction {
	return cloneSupervisorActions(transition.effects)
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
