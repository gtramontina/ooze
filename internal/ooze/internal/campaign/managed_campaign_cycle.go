package campaign

import "slices"

func (runner *managedCampaignRunner) run(request managedCampaignRequest) managedCampaignResult {
	request.env = managedExecutionEnvironment(request.env, request.profile, request.peers)
	runner.terminals = make(chan managedTerminalObservation, request.peers+1)
	definition := campaignDefinition{
		identity: request.identity, lineage: request.lineage,
		command: request.command, env: request.env, profile: request.profile, peers: request.peers,
	}
	var effects []campaignEffect
	runner.state, effects = beginCampaign(definition)
	for len(effects) != 0 || runner.pending != 0 || runner.needsEmergencySettlement() {
		if runner.needsEmergencySettlement() && (len(effects) == 0 || proposesTerminal(effects)) {
			effects = runner.settleEmergency()
		}
		var next []campaignEffect
		for _, effect := range effects {
			complete := beginRecordedEffect(runner.recorder, effect)
			next = append(next, runner.execute(effect, request)...)
			complete()
		}
		effects = next
		if len(effects) == 0 && runner.pending != 0 {
			terminal := <-runner.terminals
			runner.pending--
			effects = runner.settle(terminal, request)
		}
	}

	return managedCampaignResult{
		outcome: runner.state.outcome, failure: runner.state.failure, mutations: runner.mutations,
	}
}

func proposesTerminal(effects []campaignEffect) bool {
	return slices.ContainsFunc(effects, func(effect campaignEffect) bool {
		return effect.kind == campaignEffectProposeTerminal
	})
}

func (runner *managedCampaignRunner) advance(payload campaignEventPayload) []campaignEffect {
	leaveRecorder := enterRecorder(runner.recorder)
	defer leaveRecorder()
	reservation := reserveRecorder(runner.recorder)
	previous := runner.state
	machine, transition := (Machine{state: runner.state}).Apply(Fact{payload: payload})
	runner.state = machine.state
	runner.nextEvent = transition.event.value.id
	effects := transition.effects
	publishRecorder(runner.recorder, reservation, transition)
	runner.publishProgress(payload, previous, runner.state)

	return effects
}
