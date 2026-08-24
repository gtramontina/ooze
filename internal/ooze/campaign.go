package ooze

import (
	"slices"
	"strconv"
	"time"
)

const baselineBootstrapDeadline = 10 * time.Minute

type (
	campaignIdentity string
	snapshotIdentity string
	mutantIdentity   string
	campaignEventID  uint64
	campaignEffectID uint64
)

type campaignDefinition struct {
	identity         campaignIdentity
	lineage          campaignLineage
	command          []string
	env              []string
	profile          Profile
	peers            int
	baselineDeadline time.Duration
}

type campaignPhase uint8

const (
	campaignPreparing campaignPhase = iota + 1
	campaignBaselining
	campaignRunning
	campaignDraining
	campaignConfirming
)

type campaignResourceKind uint8

const (
	campaignResourceRegistration campaignResourceKind = iota + 1
	campaignResourceSnapshot
	campaignResourceWorkspace
	campaignResourceAdmission
	campaignResourcePendingStart
	campaignResourceExecutionDomain
)

type campaignObligation struct {
	kind       campaignResourceKind
	identity   string
	attempt    attemptIdentity
	generation attemptGeneration
}

type campaignEffectKind uint8

const (
	campaignEffectRegister campaignEffectKind = iota + 1
	campaignEffectEstablishSnapshot
	campaignEffectDiscoverCatalogue
	campaignEffectReleaseSnapshot
	campaignEffectMaterializeWorkspace
	campaignEffectRequestAdmission
	campaignEffectRequestStartCommitment
	campaignEffectLaunchAttempt
	campaignEffectCancelAdmission
	campaignEffectReturnAdmission
	campaignEffectStopAttempt
	campaignEffectReleaseWorkspace
	campaignEffectBindConfirmationBarrier
	campaignEffectProposeTerminal
)

type campaignEffect struct {
	id         campaignEffectID
	kind       campaignEffectKind
	snapshot   snapshotIdentity
	workspace  string
	attempt    attemptIdentity
	deadline   time.Duration
	profile    Profile
	request    admissionRequest
	grant      admissionGrant
	generation attemptGeneration
	terminal   campaignTerminalCandidate
}

type campaignTraceRecord struct {
	id   campaignEventID
	kind string
}

type campaignState struct {
	definition       campaignDefinition
	phase            campaignPhase
	runtimeToken     campaignToken
	snapshot         snapshotIdentity
	catalogue        []mutantIdentity
	catalogueKnown   bool
	mutants          []campaignMutant
	attempts         []campaignAttempt
	obligations      []campaignObligation
	trace            []campaignTraceRecord
	outcome          campaignOutcome
	candidate        campaignTerminalCandidate
	nextEffect       campaignEffectID
	commands         int
	nextAttempt      uint64
	mutationDeadline time.Duration
	drain            campaignDrainIntent
}

type campaignDrainIntentKind uint8

const (
	campaignDrainConfirm campaignDrainIntentKind = iota + 1
	campaignDrainComplete
	campaignDrainAbort
	campaignDrainRuntimeEmergency
)

type campaignDrainIntent struct {
	kind         campaignDrainIntentKind
	provisionals []mutantIdentity
	cause        string
	epoch        uint64
}

type campaignAttemptKind uint8

const (
	campaignAttemptBaseline campaignAttemptKind = iota + 1
	campaignAttemptPrimary
	campaignAttemptConfirmation
)

type campaignAttemptStage uint8

const (
	campaignAttemptMaterializing campaignAttemptStage = iota + 1
	campaignAttemptAdmissionWaiting
	campaignAttemptGranted
	campaignAttemptProspective
	campaignAttemptOwned
	campaignAttemptSettled
)

type campaignAttempt struct {
	identity   attemptIdentity
	kind       campaignAttemptKind
	mutant     mutantIdentity
	stage      campaignAttemptStage
	workspace  string
	request    admissionRequest
	grant      admissionGrant
	generation attemptGeneration
}

type campaignMutant struct {
	identity       mutantIdentity
	result         mutantResultKind
	primaryStarted bool
}

type campaignEventPayload interface {
	campaignEventPayload()
	campaignEventName() string
}

type campaignEvent struct {
	id      campaignEventID
	payload campaignEventPayload
}

type campaignRegisteredEvent struct{ registration campaignRegistration }
type snapshotEstablishedEvent struct{ snapshot snapshotIdentity }
type catalogueDiscoveredEvent struct {
	snapshot snapshotIdentity
	mutants  []mutantIdentity
}
type resourceSettledEvent struct {
	kind     campaignResourceKind
	identity string
}
type terminalCommittedEvent struct{ result terminalResult }
type workspaceMaterializedEvent struct {
	attempt   attemptIdentity
	workspace string
	snapshot  snapshotIdentity
}
type admissionGrantedEvent struct {
	attempt attemptIdentity
	grant   admissionGrant
}
type startCommittedEvent struct {
	attempt attemptIdentity
	grant   admissionGrant
	result  startCommittedResult
}
type attemptLaunchEvent struct {
	attempt    attemptIdentity
	generation attemptGeneration
	result     LaunchResult
	receipt    observationResult
}
type attemptTerminalEvent struct {
	attempt                  attemptIdentity
	generation               attemptGeneration
	terminal                 Terminal
	receipt                  observationResult
	resolvedMutationDeadline time.Duration
}

func (campaignRegisteredEvent) campaignEventPayload()    {}
func (snapshotEstablishedEvent) campaignEventPayload()   {}
func (catalogueDiscoveredEvent) campaignEventPayload()   {}
func (resourceSettledEvent) campaignEventPayload()       {}
func (terminalCommittedEvent) campaignEventPayload()     {}
func (workspaceMaterializedEvent) campaignEventPayload() {}
func (admissionGrantedEvent) campaignEventPayload()      {}
func (startCommittedEvent) campaignEventPayload()        {}
func (attemptLaunchEvent) campaignEventPayload()         {}
func (attemptTerminalEvent) campaignEventPayload()       {}

func (campaignRegisteredEvent) campaignEventName() string    { return "campaign registered" }
func (snapshotEstablishedEvent) campaignEventName() string   { return "snapshot established" }
func (catalogueDiscoveredEvent) campaignEventName() string   { return "catalogue discovered" }
func (resourceSettledEvent) campaignEventName() string       { return "resource settled" }
func (terminalCommittedEvent) campaignEventName() string     { return "terminal committed" }
func (workspaceMaterializedEvent) campaignEventName() string { return "workspace materialized" }
func (admissionGrantedEvent) campaignEventName() string      { return "admission granted" }
func (startCommittedEvent) campaignEventName() string        { return "start committed" }
func (attemptLaunchEvent) campaignEventName() string         { return "attempt launched" }
func (attemptTerminalEvent) campaignEventName() string       { return "attempt terminal" }

type campaignTerminalKind uint8

const (
	campaignTerminalNoMutants campaignTerminalKind = iota + 1
	campaignTerminalCompleted
	campaignTerminalAborted
)

type campaignTerminalCandidate struct{ kind campaignTerminalKind }

type campaignOutcome interface{ campaignOutcome() }
type noMutantsOutcome struct{}
type completedOutcome struct{ mutants []mutantResult }
type abortedOutcome struct{ cause string }

func (noMutantsOutcome) campaignOutcome() {}
func (completedOutcome) campaignOutcome() {}
func (abortedOutcome) campaignOutcome()   {}

type mutantResult struct {
	mutant mutantIdentity
	kind   mutantResultKind
}

type mutantResultKind uint8

const (
	mutantSurvived mutantResultKind = iota + 1
	mutantKilled
	mutantTimedOut
	mutantRunaway
)

func beginCampaign(definition campaignDefinition) (campaignState, []campaignEffect) {
	if definition.identity == "" || definition.lineage == 0 || len(definition.command) == 0 ||
		definition.command[0] == "" || definition.peers <= 0 ||
		(definition.profile != AutomaticProfile && definition.profile != SerialProfile) {
		campaignInvariant("begin", "definition is invalid")
	}
	if definition.baselineDeadline != 0 {
		campaignInvariant("begin", "baseline bootstrap deadline is campaign policy")
	}
	definition.command = slices.Clone(definition.command)
	definition.env = slices.Clone(definition.env)
	definition.baselineDeadline = baselineBootstrapDeadline
	state := campaignState{
		definition: definition,
		phase:      campaignPreparing,
		obligations: []campaignObligation{{
			kind: campaignResourceRegistration, identity: string(definition.identity),
		}},
	}

	return state.emit(campaignEffect{kind: campaignEffectRegister})
}

func advanceCampaign(state campaignState, event campaignEvent) (campaignState, []campaignEffect) {
	state = state.clone()
	if state.outcome != nil {
		campaignInvariant("advance", "terminal campaign accepts no event")
	}
	if event.payload == nil || event.id == 0 || event.id != campaignEventID(len(state.trace)+1) {
		campaignInvariant("advance", "event identity is invalid")
	}
	state.trace = append(state.trace, campaignTraceRecord{id: event.id, kind: event.payload.campaignEventName()})

	switch observed := event.payload.(type) {
	case campaignRegisteredEvent:
		return state.onRegistered(observed)
	case snapshotEstablishedEvent:
		return state.onSnapshotEstablished(observed)
	case catalogueDiscoveredEvent:
		return state.onCatalogueDiscovered(observed)
	case resourceSettledEvent:
		return state.onResourceSettled(observed)
	case terminalCommittedEvent:
		return state.onTerminalCommitted(observed)
	case workspaceMaterializedEvent:
		return state.onWorkspaceMaterialized(observed)
	case admissionGrantedEvent:
		return state.onAdmissionGranted(observed)
	case startCommittedEvent:
		return state.onStartCommitted(observed)
	case attemptLaunchEvent:
		return state.onAttemptLaunch(observed)
	case attemptTerminalEvent:
		return state.onAttemptTerminal(observed)
	default:
		campaignInvariant("advance", "event kind is unknown")
	}

	return campaignState{}, nil
}

func (state campaignState) onRegistered(event campaignRegisteredEvent) (campaignState, []campaignEffect) {
	if state.phase != campaignPreparing || state.runtimeToken.id != 0 ||
		event.registration.decision != campaignRegistered || event.registration.token.id == 0 ||
		event.registration.token.lineage != state.definition.lineage {
		campaignInvariant("register", "registration is invalid")
	}
	state.runtimeToken = event.registration.token
	state.obligations = append(state.obligations, campaignObligation{
		kind: campaignResourceSnapshot, identity: string(state.definition.identity),
	})

	return state.emit(campaignEffect{kind: campaignEffectEstablishSnapshot})
}

func (state campaignState) onSnapshotEstablished(event snapshotEstablishedEvent) (campaignState, []campaignEffect) {
	if state.phase != campaignPreparing || state.runtimeToken.id == 0 || state.snapshot != "" || event.snapshot == "" {
		campaignInvariant("establish snapshot", "snapshot observation is invalid")
	}
	state.snapshot = event.snapshot
	index := state.obligationIndex(campaignResourceSnapshot, string(state.definition.identity))
	if index < 0 {
		campaignInvariant("establish snapshot", "snapshot obligation is missing")
	}
	state.obligations[index].identity = string(event.snapshot)

	return state.emit(campaignEffect{kind: campaignEffectDiscoverCatalogue, snapshot: state.snapshot})
}

func (state campaignState) onCatalogueDiscovered(event catalogueDiscoveredEvent) (campaignState, []campaignEffect) {
	if state.phase != campaignPreparing || state.snapshot == "" || event.snapshot != state.snapshot || state.catalogueKnown {
		campaignInvariant("discover catalogue", "catalogue observation is invalid")
	}
	seen := make(map[mutantIdentity]struct{}, len(event.mutants))
	for _, mutant := range event.mutants {
		if mutant == "" {
			campaignInvariant("discover catalogue", "mutant identity is zero")
		}
		if _, duplicate := seen[mutant]; duplicate {
			campaignInvariant("discover catalogue", "mutant identity is duplicated")
		}
		seen[mutant] = struct{}{}
	}
	state.catalogue = slices.Clone(event.mutants)
	state.catalogueKnown = true
	state.mutants = make([]campaignMutant, len(state.catalogue))
	for index, mutant := range state.catalogue {
		state.mutants[index].identity = mutant
	}
	if len(state.catalogue) != 0 {
		state.phase = campaignBaselining
		var effect campaignEffect
		state, effect = state.materializeAttempt(campaignAttemptBaseline, "")

		return state, []campaignEffect{effect}
	}
	state.candidate = campaignTerminalCandidate{kind: campaignTerminalNoMutants}

	return state.emit(campaignEffect{kind: campaignEffectReleaseSnapshot, snapshot: state.snapshot})
}

func (state campaignState) onResourceSettled(event resourceSettledEvent) (campaignState, []campaignEffect) {
	index := state.obligationIndex(event.kind, event.identity)
	if index < 0 || event.kind == campaignResourceRegistration {
		campaignInvariant("settle resource", "resource obligation is unknown")
	}
	state.obligations = slices.Delete(state.obligations, index, index+1)
	if event.kind == campaignResourceWorkspace {
		attemptAt := slices.IndexFunc(state.attempts, func(attempt campaignAttempt) bool {
			return attempt.workspace == event.identity && attempt.stage == campaignAttemptSettled
		})
		if attemptAt < 0 {
			campaignInvariant("settle resource", "workspace has no settled attempt")
		}
		kind := state.attempts[attemptAt].kind
		state.attempts = slices.Delete(state.attempts, attemptAt, attemptAt+1)
		if kind == campaignAttemptBaseline {
			if state.drain.kind == campaignDrainAbort {
				return state.releaseSnapshot(campaignTerminalAborted)
			}
			state.phase = campaignRunning

			return state.materializePrimaryBatch()
		}
		if state.allMutantsResolved() && len(state.attempts) == 0 {
			state.phase = campaignDraining
			state.drain = campaignDrainIntent{kind: campaignDrainComplete}

			return state.releaseSnapshot(campaignTerminalCompleted)
		}

		return state.materializePrimaryBatch()
	}
	if state.candidate.kind != 0 && event.kind == campaignResourceSnapshot &&
		len(state.obligations) == 1 && state.obligations[0].kind == campaignResourceRegistration {
		return state.emit(campaignEffect{kind: campaignEffectProposeTerminal, terminal: state.candidate})
	}

	return state, nil
}

func (state campaignState) onTerminalCommitted(event terminalCommittedEvent) (campaignState, []campaignEffect) {
	if state.candidate.kind == 0 || event.result.decision != terminalCommitted || len(state.obligations) != 1 ||
		state.obligations[0].kind != campaignResourceRegistration {
		campaignInvariant("commit terminal", "terminal commitment is invalid")
	}
	state.obligations = nil
	switch state.candidate.kind {
	case campaignTerminalNoMutants:
		state.outcome = noMutantsOutcome{}
	case campaignTerminalCompleted:
		results := make([]mutantResult, len(state.mutants))
		for index, mutant := range state.mutants {
			if mutant.result == 0 {
				campaignInvariant("commit terminal", "completed mutant is unattributed")
			}
			results[index] = mutantResult{mutant: mutant.identity, kind: mutant.result}
		}
		state.outcome = completedOutcome{mutants: results}
	case campaignTerminalAborted:
		state.outcome = abortedOutcome{cause: state.drain.cause}
	default:
		campaignInvariant("commit terminal", "terminal candidate is invalid")
	}

	return state, nil
}

func (state campaignState) onWorkspaceMaterialized(event workspaceMaterializedEvent) (campaignState, []campaignEffect) {
	attemptAt := state.attemptIndex(event.attempt)
	if attemptAt < 0 || event.workspace == "" || event.snapshot != state.snapshot ||
		state.attempts[attemptAt].stage != campaignAttemptMaterializing {
		campaignInvariant("materialize workspace", "workspace observation is invalid")
	}
	if state.obligationIndex(campaignResourceWorkspace, string(event.attempt)) < 0 {
		campaignInvariant("materialize workspace", "workspace obligation is missing")
	}
	state.attempts[attemptAt].workspace = event.workspace
	state.attempts[attemptAt].stage = campaignAttemptAdmissionWaiting
	obligationAt := state.obligationIndex(campaignResourceWorkspace, string(event.attempt))
	state.obligations[obligationAt].identity = event.workspace
	class := sharedAdmission
	if state.attempts[attemptAt].kind == campaignAttemptBaseline {
		class = exclusiveAdmission
	} else if state.definition.profile == SerialProfile {
		class = serialPrimaryAdmission
	} else if state.attempts[attemptAt].kind == campaignAttemptConfirmation {
		class = confirmationAdmission
	}
	request := admissionRequest{campaign: state.runtimeToken, attempt: event.attempt, class: class}
	state.attempts[attemptAt].request = request
	state.obligations = append(state.obligations, campaignObligation{
		kind: campaignResourceAdmission, identity: string(event.attempt), attempt: event.attempt,
	})

	return state.emit(campaignEffect{kind: campaignEffectRequestAdmission, attempt: event.attempt, request: request})
}

func (state campaignState) onAdmissionGranted(event admissionGrantedEvent) (campaignState, []campaignEffect) {
	attemptAt := state.attemptIndex(event.attempt)
	if attemptAt < 0 || state.attempts[attemptAt].stage != campaignAttemptAdmissionWaiting ||
		event.grant != state.attempts[attemptAt].request {
		campaignInvariant("grant admission", "grant is stale or wrong")
	}
	state.attempts[attemptAt].stage = campaignAttemptGranted
	state.attempts[attemptAt].grant = event.grant
	state.obligations = append(state.obligations, campaignObligation{
		kind: campaignResourcePendingStart, identity: string(event.attempt), attempt: event.attempt,
	})

	return state.emit(campaignEffect{
		kind: campaignEffectRequestStartCommitment, attempt: event.attempt, grant: event.grant,
	})
}

func (state campaignState) onStartCommitted(event startCommittedEvent) (campaignState, []campaignEffect) {
	attemptAt := state.attemptIndex(event.attempt)
	if attemptAt < 0 || state.attempts[attemptAt].stage != campaignAttemptGranted ||
		event.grant != state.attempts[attemptAt].grant || event.result.decision != startCommittedAccepted ||
		event.result.generation == 0 {
		campaignInvariant("start committed", "commitment is stale or unauthorized")
	}
	pendingAt := state.obligationIndex(campaignResourcePendingStart, string(event.attempt))
	if pendingAt < 0 {
		campaignInvariant("start committed", "pending-start obligation is missing")
	}
	state.obligations = slices.Delete(state.obligations, pendingAt, pendingAt+1)
	state.obligations = append(state.obligations, campaignObligation{
		kind: campaignResourceExecutionDomain, identity: string(event.attempt),
		attempt: event.attempt, generation: event.result.generation,
	})
	state.attempts[attemptAt].stage = campaignAttemptProspective
	state.attempts[attemptAt].generation = event.result.generation
	deadline := state.mutationDeadline
	if state.attempts[attemptAt].kind == campaignAttemptBaseline {
		deadline = state.definition.baselineDeadline
	}
	if deadline <= 0 {
		campaignInvariant("start committed", "attempt deadline is unresolved")
	}
	state.commands++

	return state.emit(campaignEffect{
		kind: campaignEffectLaunchAttempt, attempt: event.attempt, generation: event.result.generation,
		snapshot: state.snapshot, workspace: state.attempts[attemptAt].workspace,
		deadline: deadline, profile: state.definition.profile,
	})
}

func (state campaignState) onAttemptLaunch(event attemptLaunchEvent) (campaignState, []campaignEffect) {
	attemptAt := state.attemptIndex(event.attempt)
	_, owned := event.result.(Owned)
	if attemptAt < 0 || state.attempts[attemptAt].stage != campaignAttemptProspective || !owned ||
		event.generation == 0 || event.generation != state.attempts[attemptAt].generation ||
		event.receipt.generation != event.generation || event.receipt.runtimeClosureInProgress {
		campaignInvariant("observe launch", "owned launch is invalid")
	}
	state.attempts[attemptAt].stage = campaignAttemptOwned

	return state, nil
}

func (state campaignState) onAttemptTerminal(event attemptTerminalEvent) (campaignState, []campaignEffect) {
	attemptAt := state.attemptIndex(event.attempt)
	if attemptAt < 0 || state.attempts[attemptAt].stage != campaignAttemptOwned ||
		event.generation != state.attempts[attemptAt].generation || event.terminal == nil ||
		event.receipt.generation != event.generation || !event.receipt.settlementAcknowledged ||
		event.receipt.runtimeClosureInProgress {
		campaignInvariant("observe terminal", "attempt terminal is invalid")
	}
	attempt := &state.attempts[attemptAt]
	if attempt.kind == campaignAttemptBaseline {
		settled, passed := event.terminal.(Settled)
		passed = passed && settled.Exit.Passed() && settled.Deadline == state.definition.baselineDeadline &&
			settled.CommandDuration > 0 && event.resolvedMutationDeadline > 0
		if passed {
			state.mutationDeadline = event.resolvedMutationDeadline
		} else {
			state.phase = campaignDraining
			state.drain = campaignDrainIntent{kind: campaignDrainAbort, cause: "baseline did not pass"}
		}
	} else if attempt.kind == campaignAttemptPrimary {
		mutantAt := state.mutantIndex(attempt.mutant)
		if mutantAt < 0 || state.mutants[mutantAt].result != 0 {
			campaignInvariant("observe primary terminal", "primary mutant is stale or already attributed")
		}
		switch terminal := event.terminal.(type) {
		case Settled:
			if terminal.Exit.Passed() {
				state.mutants[mutantAt].result = mutantSurvived
			} else {
				state.mutants[mutantAt].result = mutantKilled
			}
		case Tripped:
			switch terminal.Trip.(type) {
			case FuseTrip:
				state.mutants[mutantAt].result = mutantRunaway
			case AutomaticDeadlineTrip, SerialDeadlineTrip:
				if event.receipt.confirmationProvisional {
					campaignInvariant("observe primary terminal", "confirmation transition is not installed")
				}
				state.mutants[mutantAt].result = mutantTimedOut
			default:
				campaignInvariant("observe primary terminal", "trip kind is invalid")
			}
		case Stopped, Infrastructure:
			state.phase = campaignDraining
			state.drain = campaignDrainIntent{kind: campaignDrainAbort, cause: "primary infrastructure uncertainty"}
		default:
			campaignInvariant("observe primary terminal", "primary terminal kind is invalid")
		}
	} else {
		campaignInvariant("observe terminal", "confirmation transition is not installed")
	}
	attempt.stage = campaignAttemptSettled
	state.removeAttemptObligation(campaignResourceAdmission, event.attempt, event.generation)
	state.removeAttemptObligation(campaignResourceExecutionDomain, event.attempt, event.generation)

	return state.emit(campaignEffect{
		kind: campaignEffectReleaseWorkspace, attempt: event.attempt, workspace: attempt.workspace,
	})
}

func (state campaignState) releaseSnapshot(candidate campaignTerminalKind) (campaignState, []campaignEffect) {
	if state.snapshot == "" || state.obligationIndex(campaignResourceSnapshot, string(state.snapshot)) < 0 ||
		state.candidate.kind != 0 {
		campaignInvariant("release snapshot", "snapshot release is invalid")
	}
	state.candidate = campaignTerminalCandidate{kind: candidate}

	return state.emit(campaignEffect{kind: campaignEffectReleaseSnapshot, snapshot: state.snapshot})
}

func (state campaignState) materializeAttempt(kind campaignAttemptKind, mutant mutantIdentity) (campaignState, campaignEffect) {
	state.nextAttempt++
	attempt := attemptIdentity(string(state.definition.identity) + ":" + strconv.FormatUint(state.nextAttempt, 10))
	state.attempts = append(state.attempts, campaignAttempt{
		identity: attempt, kind: kind, mutant: mutant, stage: campaignAttemptMaterializing,
	})
	state.obligations = append(state.obligations, campaignObligation{
		kind: campaignResourceWorkspace, identity: string(attempt), attempt: attempt,
	})
	state.nextEffect++

	return state, campaignEffect{
		id: state.nextEffect, kind: campaignEffectMaterializeWorkspace,
		snapshot: state.snapshot, attempt: attempt,
	}
}

func (state campaignState) materializePrimaryBatch() (campaignState, []campaignEffect) {
	limit := state.definition.peers
	if state.definition.profile == SerialProfile {
		limit = 1
	}
	active := 0
	for _, attempt := range state.attempts {
		if attempt.kind == campaignAttemptPrimary && attempt.stage != campaignAttemptSettled {
			active++
		}
	}
	available := limit - active
	if available <= 0 {
		return state, nil
	}
	effects := make([]campaignEffect, 0, available)
	for index := range state.mutants {
		if len(effects) == available {
			break
		}
		if state.mutants[index].primaryStarted || state.mutants[index].result != 0 {
			continue
		}
		state.mutants[index].primaryStarted = true
		var effect campaignEffect
		state, effect = state.materializeAttempt(campaignAttemptPrimary, state.mutants[index].identity)
		effects = append(effects, effect)
	}

	return state, effects
}

func (state campaignState) mutantIndex(identity mutantIdentity) int {
	return slices.IndexFunc(state.mutants, func(mutant campaignMutant) bool { return mutant.identity == identity })
}

func (state campaignState) allMutantsResolved() bool {
	return len(state.mutants) != 0 && !slices.ContainsFunc(state.mutants, func(mutant campaignMutant) bool {
		return mutant.result == 0
	})
}

func (state campaignState) attemptIndex(identity attemptIdentity) int {
	return slices.IndexFunc(state.attempts, func(attempt campaignAttempt) bool { return attempt.identity == identity })
}

func (state *campaignState) removeAttemptObligation(
	kind campaignResourceKind,
	attempt attemptIdentity,
	generation attemptGeneration,
) {
	index := slices.IndexFunc(state.obligations, func(obligation campaignObligation) bool {
		return obligation.kind == kind && obligation.attempt == attempt &&
			(generation == 0 || obligation.generation == 0 || obligation.generation == generation)
	})
	if index < 0 {
		campaignInvariant("settle attempt", "attempt obligation is missing")
	}
	state.obligations = slices.Delete(state.obligations, index, index+1)
}

func (state campaignState) emit(effect campaignEffect) (campaignState, []campaignEffect) {
	state.nextEffect++
	effect.id = state.nextEffect

	return state, []campaignEffect{effect}
}

func (state campaignState) obligationIndex(kind campaignResourceKind, identity string) int {
	return slices.IndexFunc(state.obligations, func(obligation campaignObligation) bool {
		return obligation.kind == kind && obligation.identity == identity
	})
}

func (state campaignState) clone() campaignState {
	state.definition.command = slices.Clone(state.definition.command)
	state.definition.env = slices.Clone(state.definition.env)
	state.catalogue = slices.Clone(state.catalogue)
	state.mutants = slices.Clone(state.mutants)
	state.attempts = slices.Clone(state.attempts)
	state.drain.provisionals = slices.Clone(state.drain.provisionals)
	state.obligations = slices.Clone(state.obligations)
	state.trace = slices.Clone(state.trace)

	return state
}

func (state campaignState) commandCount() int { return state.commands }

func campaignInvariant(operation, reason string) {
	panic(runtimeInvariantViolation{operation: "campaign " + operation, reason: reason})
}

func resolveMutationDeadline(baseline time.Duration, peers int) time.Duration {
	if baseline <= 0 || peers <= 0 {
		campaignInvariant("resolve mutation deadline", "inputs must be positive")
	}
	factorHalves := int64(10 + 3*(peers-1))
	resolved := baseline * time.Duration(factorHalves) / 2
	if resolved < 20*time.Second {
		return 20 * time.Second
	}

	return resolved
}
