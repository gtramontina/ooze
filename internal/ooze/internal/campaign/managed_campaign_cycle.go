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
			complete := func() {}
			if runner.conformance != nil {
				complete = runner.conformance.Execute(Effect{value: effect})
			}
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
	leave := func() {}
	reservation := uint64(0)
	if runner.conformance != nil {
		leave = runner.conformance.Enter()
		defer leave()
		reservation = runner.conformance.Reserve()
	}
	previous := runner.state
	machine, transition := (Machine{state: runner.state}).Apply(Fact{payload: payload})
	runner.state = machine.state
	runner.nextEvent = transition.event.value.id
	effects := transition.effects
	if runner.conformance != nil {
		runner.conformance.Publish(
			reservation, transition.Event(), transition.Projection(), transition.Effects(),
		)
	}
	runner.publishProgress(payload, previous, runner.state)

	return effects
}
