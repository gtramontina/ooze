package ooze

import (
	"slices"
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
	id       campaignEffectID
	kind     campaignEffectKind
	snapshot snapshotIdentity
	attempt  attemptIdentity
	deadline time.Duration
	profile  Profile
	terminal campaignTerminalCandidate
}

type campaignTraceRecord struct {
	id   campaignEventID
	kind string
}

type campaignState struct {
	definition   campaignDefinition
	phase        campaignPhase
	runtimeToken campaignToken
	snapshot     snapshotIdentity
	catalogue    []mutantIdentity
	obligations  []campaignObligation
	trace        []campaignTraceRecord
	outcome      campaignOutcome
	candidate    campaignTerminalCandidate
	nextEffect   campaignEffectID
	commands     int
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

func (campaignRegisteredEvent) campaignEventPayload()  {}
func (snapshotEstablishedEvent) campaignEventPayload() {}
func (catalogueDiscoveredEvent) campaignEventPayload() {}
func (resourceSettledEvent) campaignEventPayload()     {}
func (terminalCommittedEvent) campaignEventPayload()   {}

func (campaignRegisteredEvent) campaignEventName() string  { return "campaign registered" }
func (snapshotEstablishedEvent) campaignEventName() string { return "snapshot established" }
func (catalogueDiscoveredEvent) campaignEventName() string { return "catalogue discovered" }
func (resourceSettledEvent) campaignEventName() string     { return "resource settled" }
func (terminalCommittedEvent) campaignEventName() string   { return "terminal committed" }

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
	if state.phase != campaignPreparing || state.snapshot == "" || event.snapshot != state.snapshot || state.catalogue != nil {
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
	if len(state.catalogue) != 0 {
		campaignInvariant("discover catalogue", "non-empty catalogue transition is not installed")
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
	if state.candidate.kind == campaignTerminalNoMutants && event.kind == campaignResourceSnapshot &&
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
		state.outcome = completedOutcome{}
	case campaignTerminalAborted:
		state.outcome = abortedOutcome{}
	default:
		campaignInvariant("commit terminal", "terminal candidate is invalid")
	}

	return state, nil
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
	state.obligations = slices.Clone(state.obligations)
	state.trace = slices.Clone(state.trace)

	return state
}

func (state campaignState) commandCount() int { return state.commands }

func campaignInvariant(operation, reason string) {
	panic(runtimeInvariantViolation{operation: "campaign " + operation, reason: reason})
}
