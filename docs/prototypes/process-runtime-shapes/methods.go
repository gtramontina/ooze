package runtimeshapes

type methodsShape struct{}

func (methodsShape) apply(current state, command call) (state, reply) {
	core := methodsCore{state: current}

	var result reply
	switch command.name {
	case "register":
		core, result = core.registerCampaign(command.input.(registerInput))
	case "request":
		core, result = core.requestAdmission(command.input.(requestInput))
	case "cancel":
		core, result = core.cancelAdmission(command.input.(cancelInput))
	case "start":
		core, result = core.startCommitted(command.input.(startInput))
	case "observe":
		core, result = core.observeAttempt(command.input.(observationInput))
	case "bind":
		core, result = core.sealAndBindBarrier(command.input.(bindInput))
	case "terminal":
		core, result = core.commitTerminal(command.input.(terminalInput))
	case "close":
		core, result = core.closeRuntime(command.input.(closeInput))
	case "settle":
		core, result = core.settleEmergency(command.input.(settleInput))
	default:
		panic("unknown methods-shape call")
	}

	return core.state, result
}

type methodsCore struct{ state state }

func (c methodsCore) registerCampaign(input registerInput) (methodsCore, reply) {
	if c.state.closed {
		return c, reply{closed: true}
	}
	for _, registered := range c.state.campaigns {
		if input.lineage != 0 && registered.lineage == input.lineage {
			return c, reply{recursive: true}
		}
	}
	c.state.nextCampaign++
	lineage := input.lineage
	if lineage == 0 {
		lineage = uint64(c.state.nextCampaign)
	}
	c.state.campaigns = append(c.state.campaigns, campaign{
		id: c.state.nextCampaign, lineage: lineage, gateOpen: true,
	})

	return c, reply{accepted: true, campaign: c.state.nextCampaign}
}

func (c methodsCore) requestAdmission(input requestInput) (methodsCore, reply) {
	index := campaignIndex(c.state, input.campaign)
	if c.state.closed || index < 0 || input.attempt == "" ||
		(input.class != shared && input.class != exclusive) ||
		(input.class == shared && !c.state.campaigns[index].gateOpen) {
		return c, reply{closed: c.state.closed}
	}
	for _, existing := range c.state.entries {
		if existing.campaign == input.campaign && existing.attempt == input.attempt {
			return c, reply{}
		}
		if input.class == exclusive && existing.campaign == input.campaign && existing.class != shared {
			return c, reply{}
		}
	}
	c.state.entries = append(c.state.entries, entry{
		campaign: input.campaign, attempt: input.attempt, class: input.class, stage: waiting, bound: true,
	})
	var deliveries []grant
	c.state, deliveries = grantAvailable(c.state)

	return c, reply{accepted: true, deliveries: deliveries}
}

func (c methodsCore) cancelAdmission(input cancelInput) (methodsCore, reply) {
	index := entryIndexByGrant(c.state, input.grant)
	if c.state.closed || index < 0 ||
		(c.state.entries[index].stage != waiting && c.state.entries[index].stage != granted) {
		return c, reply{closed: c.state.closed}
	}
	c.state.entries = removeEntry(c.state.entries, index)
	var deliveries []grant
	c.state, deliveries = grantAvailable(c.state)

	return c, reply{accepted: true, deliveries: deliveries}
}

func (c methodsCore) startCommitted(input startInput) (methodsCore, reply) {
	index := entryIndexByGrant(c.state, input.grant)
	if c.state.closed || index < 0 || c.state.entries[index].stage != granted {
		return c, reply{closed: c.state.closed}
	}
	registered := campaignIndex(c.state, input.grant.campaign)
	if registered < 0 || (c.state.entries[index].class == shared && !c.state.campaigns[registered].gateOpen) {
		return c, reply{}
	}
	c.state.nextGeneration++
	c.state.entries[index].stage = prospective
	c.state.entries[index].generation = c.state.nextGeneration
	for other := range c.state.entries {
		if other == index ||
			(c.state.entries[other].stage != prospective && c.state.entries[other].stage != owned) {
			continue
		}
		c.state.entries[index].overlapped = true
		c.state.entries[other].overlapped = true
	}

	return c, reply{accepted: true, generation: c.state.nextGeneration}
}

func (c methodsCore) observeAttempt(input observationInput) (methodsCore, reply) {
	index := entryIndexByGeneration(c.state, input.generation)
	if index < 0 {
		return c, reply{closed: c.state.closed}
	}
	current := c.state.entries[index]
	switch input.kind {
	case launchOwned:
		if current.stage != prospective {
			return c, reply{closed: c.state.closed}
		}
		c.state.entries[index].stage = owned

		return c, reply{accepted: true}
	case launchFailed, launchResourceExhausted:
		if current.stage != prospective {
			return c, reply{closed: c.state.closed}
		}
		if input.kind == launchResourceExhausted && current.class == shared {
			c.state.single = true
		}
		c.state.entries = removeEntry(c.state.entries, index)
	case drainUnconfirmed:
		if current.stage != prospective && current.stage != owned {
			return c, reply{closed: c.state.closed}
		}
		c.state.closed = true
		c.state = retainCommitted(c.state)

		return c, reply{accepted: true, closed: true}
	case settled, deadline, fuse, infrastructure:
		if current.stage != owned {
			return c, reply{closed: c.state.closed}
		}
		if input.kind == deadline && current.overlapped && current.class == shared {
			campaignAt := campaignIndex(c.state, current.campaign)
			c.state.campaigns[campaignAt].gateOpen = false
			c.state.entries = append(c.state.entries, entry{
				campaign: current.campaign, class: barrier, stage: waiting,
			})
		}
		if current.class != shared && input.confirmationDrained {
			campaignAt := campaignIndex(c.state, current.campaign)
			c.state.campaigns[campaignAt].gateOpen = true
			if input.kind == settled {
				c.state.single = true
			}
		}
		c.state.entries = removeEntry(c.state.entries, index)
	default:
		return c, reply{closed: c.state.closed}
	}
	if !c.state.closed {
		var deliveries []grant
		c.state, deliveries = grantAvailable(c.state)

		return c, reply{accepted: true, deliveries: deliveries}
	}

	return c, reply{accepted: true, closed: true}
}

func (c methodsCore) sealAndBindBarrier(input bindInput) (methodsCore, reply) {
	if c.state.closed || campaignIndex(c.state, input.campaign) < 0 || input.attempt == "" {
		return c, reply{closed: c.state.closed}
	}
	barrierAt := -1
	for index, existing := range c.state.entries {
		if existing.campaign != input.campaign {
			continue
		}
		if existing.stage == prospective || existing.stage == owned {
			return c, reply{}
		}
		if existing.class == barrier && !existing.bound {
			barrierAt = index
		}
	}
	if barrierAt < 0 {
		return c, reply{}
	}
	c.state.entries[barrierAt].attempt = input.attempt
	c.state.entries[barrierAt].bound = true
	var deliveries []grant
	c.state, deliveries = grantAvailable(c.state)

	return c, reply{accepted: true, deliveries: deliveries}
}

func (c methodsCore) commitTerminal(input terminalInput) (methodsCore, reply) {
	if c.state.closed {
		return c, reply{closed: true}
	}
	registered := campaignIndex(c.state, input.campaign)
	if registered < 0 {
		return c, reply{}
	}
	for _, existing := range c.state.entries {
		if existing.campaign == input.campaign {
			return c, reply{}
		}
	}
	c.state.campaigns = append(c.state.campaigns[:registered], c.state.campaigns[registered+1:]...)

	return c, reply{accepted: true}
}

func (c methodsCore) closeRuntime(closeInput) (methodsCore, reply) {
	if c.state.closed {
		return c, reply{accepted: true, closed: true}
	}
	c.state.closed = true
	c.state = retainCommitted(c.state)

	return c, reply{accepted: true, closed: true}
}

func (c methodsCore) settleEmergency(input settleInput) (methodsCore, reply) {
	index := entryIndexByGeneration(c.state, input.generation)
	if !c.state.closed || index < 0 ||
		(c.state.entries[index].stage != prospective && c.state.entries[index].stage != owned) {
		return c, reply{closed: c.state.closed}
	}
	c.state.entries = removeEntry(c.state.entries, index)

	return c, reply{accepted: true, closed: true}
}

func campaignIndex(current state, id campaignID) int {
	for index, registered := range current.campaigns {
		if registered.id == id {
			return index
		}
	}

	return -1
}

func entryIndexByGrant(current state, token grant) int {
	for index, existing := range current.entries {
		if existing.campaign == token.campaign && existing.attempt == token.attempt {
			return index
		}
	}

	return -1
}

func entryIndexByGeneration(current state, gen generation) int {
	for index, existing := range current.entries {
		if existing.generation == gen && gen != 0 {
			return index
		}
	}

	return -1
}

func removeEntry(entries []entry, index int) []entry {
	result := append([]entry(nil), entries[:index]...)

	return append(result, entries[index+1:]...)
}

func retainCommitted(current state) state {
	retained := make([]entry, 0, len(current.entries))
	for _, existing := range current.entries {
		if existing.stage == prospective || existing.stage == owned {
			retained = append(retained, existing)
		}
	}
	current.entries = retained

	return current
}

func grantAvailable(current state) (state, []grant) {
	active, activeExclusive := 0, false
	for _, existing := range current.entries {
		if existing.stage == granted || existing.stage == prospective || existing.stage == owned {
			active++
			activeExclusive = activeExclusive || existing.class != shared
		}
	}
	if activeExclusive {
		return current, nil
	}
	limit := current.capacity
	if current.single {
		limit = 1
	}
	deliveries := make([]grant, 0, limit)
	for index := range current.entries {
		candidate := &current.entries[index]
		if candidate.stage != waiting {
			continue
		}
		if candidate.class != shared {
			if !candidate.bound || active != 0 {
				return current, deliveries
			}
			candidate.stage = granted

			return current, append(deliveries, grant{candidate.campaign, candidate.attempt})
		}
		if active >= limit {
			return current, deliveries
		}
		candidate.stage = granted
		active++
		deliveries = append(deliveries, grant{candidate.campaign, candidate.attempt})
	}

	return current, deliveries
}
