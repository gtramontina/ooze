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
				complete = runner.conformance.BeginEffect(Effect{value: effect})
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
	reservation := uint64(0)
	if runner.conformance != nil {
		leave := runner.conformance.Enter()
		defer leave()
		reservation = runner.conformance.Reserve()
	}
	fact := Fact{payload: payload, label: runner.eventMutationLabel(payload)}
	machine, transition := (Machine{state: runner.state}).Apply(fact)
	runner.state = machine.state
	runner.nextEvent = transition.event.value.id
	effects := transition.effects
	if runner.conformance != nil {
		runner.conformance.Publish(
			reservation, transition.Event(), transition.Projection(), transition.Effects(),
		)
	}
	if runner.observe != nil {
		runner.observe(transition.Event())
	}

	return effects
}

func (runner *managedCampaignRunner) eventMutationLabel(payload campaignEventPayload) string {
	attempt := Fact{payload: payload}.Attempt()
	attemptAt := runner.state.attemptIndex(attemptIdentity(attempt))
	if attemptAt < 0 {
		return ""
	}
	mutant := runner.state.attempts[attemptAt].mutant
	if mutant != "" {
		return runner.mutationLabel(mutant)
	}
	return ""
}

func (runner *managedCampaignRunner) mutationLabel(identity mutantIdentity) string {
	mutation := runner.mutations[identity]
	if mutation == nil {
		panic("managed progress mutation is missing")
	}

	return mutation.Label()
}
