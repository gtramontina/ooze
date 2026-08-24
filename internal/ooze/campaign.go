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
	mutant     mutantIdentity
	deadline   time.Duration
	profile    Profile
	request    admissionRequest
	grant      admissionGrant
	generation attemptGeneration
	binding    barrierBinding
	terminal   campaignTerminalCandidate
}

type campaignTraceRecord struct {
	id          campaignEventID
	kind        string
	phase       campaignPhase
	payload     string
	identities  []string
	obligations []string
}

type campaignState struct {
	definition               campaignDefinition
	phase                    campaignPhase
	runtimeToken             campaignToken
	snapshot                 snapshotIdentity
	catalogue                []mutantIdentity
	catalogueKnown           bool
	mutants                  []campaignMutant
	attempts                 []campaignAttempt
	obligations              []campaignObligation
	trace                    []campaignTraceRecord
	outcome                  campaignOutcome
	failure                  campaignFailure
	candidate                campaignTerminalCandidate
	nextEffect               campaignEffectID
	commands                 int
	nextAttempt              uint64
	mutationDeadline         time.Duration
	drain                    campaignDrainIntent
	confirmationBarrierBound bool
	pendingGrantReturns      []admissionGrant
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
	epoch        fatalEpochID
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
	campaignAttemptReturningGrant
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
	identity            mutantIdentity
	result              mutantResultKind
	primaryStarted      bool
	confirmationStarted bool
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
type campaignPreparationStage uint8

const (
	campaignPreparingSnapshot campaignPreparationStage = iota + 1
	campaignPreparingCatalogue
)

type campaignPreparationFailedEvent struct {
	stage campaignPreparationStage
	cause string
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
type confirmationBarrierBoundEvent struct {
	attempt attemptIdentity
	result  barrierResult
}
type grantReturnAcknowledgedEvent struct {
	grant  admissionGrant
	result admissionResult
}
type runtimeEmergencySettledEvent struct {
	epoch      fatalEpochID
	settlement emergencySettlement
}

func (campaignRegisteredEvent) campaignEventPayload()        {}
func (snapshotEstablishedEvent) campaignEventPayload()       {}
func (catalogueDiscoveredEvent) campaignEventPayload()       {}
func (campaignPreparationFailedEvent) campaignEventPayload() {}
func (resourceSettledEvent) campaignEventPayload()           {}
func (terminalCommittedEvent) campaignEventPayload()         {}
func (workspaceMaterializedEvent) campaignEventPayload()     {}
func (admissionGrantedEvent) campaignEventPayload()          {}
func (startCommittedEvent) campaignEventPayload()            {}
func (attemptLaunchEvent) campaignEventPayload()             {}
func (attemptTerminalEvent) campaignEventPayload()           {}
func (confirmationBarrierBoundEvent) campaignEventPayload()  {}
func (grantReturnAcknowledgedEvent) campaignEventPayload()   {}
func (runtimeEmergencySettledEvent) campaignEventPayload()   {}

func (campaignRegisteredEvent) campaignEventName() string  { return "campaign registered" }
func (snapshotEstablishedEvent) campaignEventName() string { return "snapshot established" }
func (catalogueDiscoveredEvent) campaignEventName() string { return "catalogue discovered" }
func (campaignPreparationFailedEvent) campaignEventName() string {
	return "campaign preparation failed"
}
func (resourceSettledEvent) campaignEventName() string       { return "resource settled" }
func (terminalCommittedEvent) campaignEventName() string     { return "terminal committed" }
func (workspaceMaterializedEvent) campaignEventName() string { return "workspace materialized" }
func (admissionGrantedEvent) campaignEventName() string      { return "admission granted" }
func (startCommittedEvent) campaignEventName() string        { return "start committed" }
func (attemptLaunchEvent) campaignEventName() string         { return "attempt launched" }
func (attemptTerminalEvent) campaignEventName() string       { return "attempt terminal" }
func (confirmationBarrierBoundEvent) campaignEventName() string {
	return "confirmation barrier bound"
}
func (grantReturnAcknowledgedEvent) campaignEventName() string { return "grant return acknowledged" }
func (runtimeEmergencySettledEvent) campaignEventName() string { return "runtime emergency settled" }

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

type campaignFailure interface{ campaignFailure() }

type nonEmptyResidualCustody struct {
	head residualCustody
	tail []residualCustody
}

type cleanupUnconfirmedFault struct{ residual nonEmptyResidualCustody }

func (noMutantsOutcome) campaignOutcome()        {}
func (completedOutcome) campaignOutcome()        {}
func (abortedOutcome) campaignOutcome()          {}
func (cleanupUnconfirmedFault) campaignFailure() {}

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
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		violation, ok := recovered.(runtimeInvariantViolation)
		if !ok {
			panic(recovered)
		}
		if violation.rejectedEvent == "" {
			violation.phase = uint8(state.phase)
			violation.rejectedEvent = campaignEventSummary(event)
			violation.stableIdentities = state.stableIdentitySnapshot(event)
			violation.obligationSnapshot = state.obligationSnapshot()
			start := max(0, len(state.trace)-16)
			violation.traceTail = make([]string, 0, len(state.trace)-start)
			for _, record := range state.trace[start:] {
				violation.traceTail = append(violation.traceTail, record.payload)
			}
		}
		panic(violation)
	}()
	if state.outcome != nil {
		campaignInvariant("advance", "terminal campaign accepts no event")
	}
	if state.failure != nil {
		campaignInvariant("advance", "failed campaign accepts no event")
	}
	if event.payload == nil || event.id == 0 || event.id != campaignEventID(len(state.trace)+1) {
		campaignInvariant("advance", "event identity is invalid")
	}
	state.trace = append(state.trace, campaignTraceRecord{
		id: event.id, kind: event.payload.campaignEventName(), phase: state.phase,
		payload: campaignEventSummary(event), identities: state.stableIdentitySnapshot(event),
		obligations: state.obligationSnapshot(),
	})

	switch observed := event.payload.(type) {
	case campaignRegisteredEvent:
		return state.onRegistered(observed)
	case snapshotEstablishedEvent:
		return state.onSnapshotEstablished(observed)
	case catalogueDiscoveredEvent:
		return state.onCatalogueDiscovered(observed)
	case campaignPreparationFailedEvent:
		return state.onPreparationFailed(observed)
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
	case confirmationBarrierBoundEvent:
		return state.onConfirmationBarrierBound(observed)
	case grantReturnAcknowledgedEvent:
		return state.onGrantReturnAcknowledged(observed)
	case runtimeEmergencySettledEvent:
		return state.onRuntimeEmergencySettled(observed)
	default:
		campaignInvariant("advance", "event kind is unknown")
	}

	return campaignState{}, nil
}

func advanceCampaignGuarded(
	runtime *processRuntime,
	state campaignState,
	event campaignEvent,
	emergencyDrain func(runtimeClosure) emergencySweep,
) (next campaignState, effects []campaignEffect) {
	if runtime == nil || emergencyDrain == nil {
		campaignInvariant("guard", "runtime guard is invalid")
	}
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		violation, ok := recovered.(runtimeInvariantViolation)
		if !ok {
			violation = runtimeInvariantViolation{operation: "campaign advance", reason: "unexpected panic"}
		}
		var closure runtimeClosure
		*runtime, closure = runtime.closeRuntime(runtimeFatalCause(violation.reason))
		sweep := emergencyDrain(closure)
		*runtime, _ = runtime.settleEmergency(sweep)
		panic(violation)
	}()

	return advanceCampaign(state, event)
}

func (state campaignState) onRegistered(event campaignRegisteredEvent) (campaignState, []campaignEffect) {
	if state.phase != campaignPreparing || state.runtimeToken.id != 0 {
		campaignInvariant("register", "registration is invalid")
	}
	if event.registration.decision == campaignRejectedRecursive || event.registration.decision == campaignRejectedClosed {
		if event.registration.token.id != 0 || len(state.obligations) != 1 ||
			state.obligations[0].kind != campaignResourceRegistration {
			campaignInvariant("register", "registration rejection is invalid")
		}
		state.obligations = nil
		state.drain = campaignDrainIntent{kind: campaignDrainAbort, cause: "campaign registration rejected"}
		state.outcome = abortedOutcome{cause: state.drain.cause}

		return state, nil
	}
	if event.registration.decision != campaignRegistered || event.registration.token.id == 0 ||
		event.registration.token.lineage != state.definition.lineage {
		campaignInvariant("register", "registration is invalid")
	}
	state.runtimeToken = event.registration.token
	state.obligations = append(state.obligations, campaignObligation{
		kind: campaignResourceSnapshot, identity: string(state.definition.identity),
	})

	return state.emit(campaignEffect{kind: campaignEffectEstablishSnapshot})
}

func (state campaignState) onPreparationFailed(
	event campaignPreparationFailedEvent,
) (campaignState, []campaignEffect) {
	if state.phase != campaignPreparing || state.runtimeToken.id == 0 || event.cause == "" {
		campaignInvariant("prepare", "preparation failure is invalid")
	}
	state.phase = campaignDraining
	state.drain = campaignDrainIntent{kind: campaignDrainAbort, cause: event.cause}
	switch event.stage {
	case campaignPreparingSnapshot:
		if state.snapshot != "" {
			campaignInvariant("prepare", "snapshot failure arrived after establishment")
		}
		index := state.obligationIndex(campaignResourceSnapshot, string(state.definition.identity))
		if index < 0 {
			campaignInvariant("prepare", "snapshot obligation is missing")
		}
		state.obligations = slices.Delete(state.obligations, index, index+1)
		state.candidate = campaignTerminalCandidate{kind: campaignTerminalAborted}

		return state.emit(campaignEffect{kind: campaignEffectProposeTerminal, terminal: state.candidate})
	case campaignPreparingCatalogue:
		if state.snapshot == "" || state.catalogueKnown {
			campaignInvariant("prepare", "catalogue failure is stale")
		}

		return state.releaseSnapshot(campaignTerminalAborted)
	default:
		campaignInvariant("prepare", "preparation stage is invalid")
	}

	return campaignState{}, nil
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
			if state.drain.kind == campaignDrainAbort || state.drain.kind == campaignDrainRuntimeEmergency {
				return state.releaseSnapshot(campaignTerminalAborted)
			}
			state.phase = campaignRunning

			return state.materializePrimaryBatch()
		}
		if kind == campaignAttemptConfirmation {
			if len(state.drain.provisionals) != 0 {
				return state.materializeConfirmation()
			}
			if state.allMutantsResolved() && len(state.attempts) == 0 {
				state.phase = campaignDraining
				state.drain = campaignDrainIntent{kind: campaignDrainComplete}

				return state.releaseSnapshot(campaignTerminalCompleted)
			}
			state.phase = campaignRunning

			return state.materializePrimaryBatch()
		}
		if state.drain.kind == campaignDrainAbort || state.drain.kind == campaignDrainRuntimeEmergency {
			if len(state.attempts) == 0 {
				return state.releaseSnapshot(campaignTerminalAborted)
			}

			return state, nil
		}
		if state.allMutantsResolved() && len(state.attempts) == 0 {
			state.phase = campaignDraining
			state.drain = campaignDrainIntent{kind: campaignDrainComplete}

			return state.releaseSnapshot(campaignTerminalCompleted)
		}
		if state.drain.kind == campaignDrainConfirm && len(state.attempts) == 0 {
			return state.materializeConfirmation()
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
	expected := terminalCommitted
	if state.drain.kind == campaignDrainRuntimeEmergency {
		expected = terminalForcedAborted
	}
	if state.candidate.kind == 0 || event.result.decision != expected || len(state.obligations) != 1 ||
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
	} else if state.attempts[attemptAt].kind == campaignAttemptConfirmation {
		class = confirmationAdmission
	} else if state.definition.profile == SerialProfile {
		class = serialPrimaryAdmission
	}
	if state.attempts[attemptAt].kind == campaignAttemptConfirmation && !state.confirmationBarrierBound {
		state.obligations = append(state.obligations, campaignObligation{
			kind: campaignResourceAdmission, identity: string(event.attempt), attempt: event.attempt,
		})
		binding := barrierBinding{campaign: state.runtimeToken, attempt: event.attempt}

		return state.emit(campaignEffect{
			kind: campaignEffectBindConfirmationBarrier, attempt: event.attempt,
			mutant: state.attempts[attemptAt].mutant, binding: binding,
		})
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
	if slices.Contains(state.pendingGrantReturns, event.grant) {
		state.attempts[attemptAt].grant = event.grant
		state.attempts[attemptAt].stage = campaignAttemptReturningGrant

		return state.emit(campaignEffect{
			kind: campaignEffectReturnAdmission, attempt: event.attempt, grant: event.grant,
		})
	}

	return state.acceptGrant(attemptAt, event.grant)
}

func (state campaignState) acceptGrant(attemptAt int, grant admissionGrant) (campaignState, []campaignEffect) {
	state.attempts[attemptAt].stage = campaignAttemptGranted
	state.attempts[attemptAt].grant = grant
	state.obligations = append(state.obligations, campaignObligation{
		kind: campaignResourcePendingStart, identity: string(grant.attempt), attempt: grant.attempt,
	})

	return state.emit(campaignEffect{
		kind: campaignEffectRequestStartCommitment, attempt: grant.attempt, grant: grant,
	})
}

func (state campaignState) onConfirmationBarrierBound(
	event confirmationBarrierBoundEvent,
) (campaignState, []campaignEffect) {
	attemptAt := state.attemptIndex(event.attempt)
	if attemptAt < 0 || state.confirmationBarrierBound || state.attempts[attemptAt].kind != campaignAttemptConfirmation ||
		state.attempts[attemptAt].stage != campaignAttemptAdmissionWaiting || event.result.decision != barrierBound ||
		event.result.request.attempt != event.attempt || len(event.result.deliveries) != 1 ||
		event.result.deliveries[0] != event.result.request {
		campaignInvariant("bind confirmation barrier", "barrier binding is invalid")
	}
	state.confirmationBarrierBound = true
	state.attempts[attemptAt].request = event.result.request

	return state.acceptGrant(attemptAt, event.result.deliveries[0])
}

func (state campaignState) onGrantReturnAcknowledged(
	event grantReturnAcknowledgedEvent,
) (campaignState, []campaignEffect) {
	attemptAt := state.attemptIndex(event.grant.attempt)
	returnAt := slices.Index(state.pendingGrantReturns, event.grant)
	if attemptAt < 0 || returnAt < 0 || state.attempts[attemptAt].stage != campaignAttemptReturningGrant ||
		state.attempts[attemptAt].grant != event.grant ||
		(event.result.decision != admissionReturnedAfterGateClosure &&
			event.result.decision != admissionReturnedAfterClosure) {
		campaignInvariant("acknowledge grant return", "grant return acknowledgement is invalid")
	}
	state.pendingGrantReturns = slices.Delete(state.pendingGrantReturns, returnAt, returnAt+1)
	state.removeAttemptObligation(campaignResourceAdmission, event.grant.attempt, 0)
	state.attempts[attemptAt].stage = campaignAttemptSettled
	mutantAt := state.mutantIndex(state.attempts[attemptAt].mutant)
	if mutantAt >= 0 {
		state.mutants[mutantAt].primaryStarted = false
	}

	return state.emit(campaignEffect{
		kind: campaignEffectReleaseWorkspace, attempt: event.grant.attempt,
		workspace: state.attempts[attemptAt].workspace,
	})
}

func (state campaignState) onStartCommitted(event startCommittedEvent) (campaignState, []campaignEffect) {
	attemptAt := state.attemptIndex(event.attempt)
	if attemptAt < 0 || event.grant != state.attempts[attemptAt].grant {
		campaignInvariant("start committed", "commitment is stale or unauthorized")
	}
	if event.result.decision == startCommittedRejectedGrant || event.result.decision == startCommittedRejectedGate ||
		event.result.decision == startCommittedRejectedClosed {
		if state.attempts[attemptAt].stage != campaignAttemptReturningGrant || event.result.generation != 0 ||
			!slices.Contains(state.pendingGrantReturns, event.grant) {
			campaignInvariant("start committed", "rejected commitment lacks grant compensation")
		}

		return state, nil
	}
	if state.attempts[attemptAt].stage != campaignAttemptGranted ||
		event.result.decision != startCommittedAccepted || event.result.generation == 0 {
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
	if attemptAt < 0 || state.attempts[attemptAt].stage != campaignAttemptProspective || event.generation == 0 ||
		event.generation != state.attempts[attemptAt].generation || event.receipt.generation != event.generation {
		campaignInvariant("observe launch", "launch observation is invalid")
	}
	switch result := event.result.(type) {
	case Owned:
		if result.Attempt == nil && event.receipt.runtimeClosureInProgress {
			campaignInvariant("observe launch", "owned launch is closing")
		}
		state.attempts[attemptAt].stage = campaignAttemptOwned
		if state.drain.kind == campaignDrainAbort {
			return state.emit(campaignEffect{
				kind: campaignEffectStopAttempt, attempt: event.attempt, generation: event.generation,
			})
		}

		return state, nil
	case NotReleased:
		if (result.Kind != LaunchFailed && result.Kind != LaunchResourceExhausted) ||
			!event.receipt.settlementAcknowledged || event.receipt.runtimeClosureInProgress {
			campaignInvariant("observe launch", "proven no-release observation is invalid")
		}
		state.attempts[attemptAt].stage = campaignAttemptSettled
		state.removeAttemptObligation(campaignResourceAdmission, event.attempt, event.generation)
		state.removeAttemptObligation(campaignResourceExecutionDomain, event.attempt, event.generation)
		state.phase = campaignDraining
		state.drain = campaignDrainIntent{kind: campaignDrainAbort, cause: "attempt was not released"}
		effects := state.stopCommittedPeers(event.attempt)
		effects = append(effects, campaignEffect{
			kind: campaignEffectReleaseWorkspace, attempt: event.attempt,
			workspace: state.attempts[attemptAt].workspace,
		})

		return state.emitAll(effects)
	case LaunchUnconfirmed:
		if result.Residual != ProspectiveUnresolved || !event.receipt.runtimeClosureInProgress ||
			event.receipt.fatalEpoch == 0 {
			campaignInvariant("observe launch", "unconfirmed launch observation is invalid")
		}
		state.phase = campaignDraining
		state.drain = campaignDrainIntent{
			kind: campaignDrainRuntimeEmergency, cause: "prospective launch unresolved",
			epoch: event.receipt.fatalEpoch,
		}

		return state, nil
	default:
		campaignInvariant("observe launch", "launch result kind is invalid")
	}

	return campaignState{}, nil
}

func (state campaignState) onAttemptTerminal(event attemptTerminalEvent) (campaignState, []campaignEffect) {
	attemptAt := state.attemptIndex(event.attempt)
	_, drainFailed := event.terminal.(DrainUnconfirmed)
	if drainFailed {
		if attemptAt < 0 || state.attempts[attemptAt].stage != campaignAttemptOwned ||
			event.generation != state.attempts[attemptAt].generation || !event.receipt.runtimeClosureInProgress ||
			event.receipt.fatalEpoch == 0 || event.receipt.settlementAcknowledged {
			campaignInvariant("observe drain unconfirmed", "fatal seed is invalid")
		}
		state.phase = campaignDraining
		state.drain = campaignDrainIntent{
			kind: campaignDrainRuntimeEmergency, cause: "execution-domain drainage unconfirmed",
			epoch: event.receipt.fatalEpoch,
		}

		return state, nil
	}
	if attemptAt < 0 || state.attempts[attemptAt].stage != campaignAttemptOwned ||
		event.generation != state.attempts[attemptAt].generation || event.terminal == nil ||
		event.receipt.generation != event.generation || !event.receipt.settlementAcknowledged ||
		event.receipt.runtimeClosureInProgress {
		campaignInvariant("observe terminal", "attempt terminal is invalid")
	}
	attempt := &state.attempts[attemptAt]
	var transitionEffects []campaignEffect
	switch attempt.kind {
	case campaignAttemptBaseline:
		settled, passed := event.terminal.(Settled)
		passed = passed && settled.Exit.Passed() && settled.Deadline == state.definition.baselineDeadline &&
			settled.CommandDuration > 0
		if passed && event.resolvedMutationDeadline != resolveMutationDeadline(
			settled.CommandDuration, state.definition.peers,
		) {
			campaignInvariant("observe terminal", "baseline mutation deadline resolution is invalid")
		}
		if passed {
			state.mutationDeadline = event.resolvedMutationDeadline
		} else {
			state.phase = campaignDraining
			state.drain = campaignDrainIntent{kind: campaignDrainAbort, cause: "baseline did not pass"}
		}
	case campaignAttemptPrimary:
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
					state.phase = campaignDraining
					state.drain.kind = campaignDrainConfirm
					state.drain.provisionals = state.insertProvisional(attempt.mutant)
				} else {
					state.mutants[mutantAt].result = mutantTimedOut
				}
			default:
				campaignInvariant("observe primary terminal", "trip kind is invalid")
			}
		case Stopped:
			if state.drain.kind != campaignDrainAbort {
				state.phase = campaignDraining
				state.drain = campaignDrainIntent{kind: campaignDrainAbort, cause: "primary stopped"}
				transitionEffects = state.stopCommittedPeers(event.attempt)
			}
		case Infrastructure:
			if state.drain.kind != campaignDrainAbort {
				state.phase = campaignDraining
				state.drain = campaignDrainIntent{kind: campaignDrainAbort, cause: "primary infrastructure uncertainty"}
				transitionEffects = state.stopCommittedPeers(event.attempt)
			}
		default:
			campaignInvariant("observe primary terminal", "primary terminal kind is invalid")
		}
	case campaignAttemptConfirmation:
		mutantAt := state.mutantIndex(attempt.mutant)
		if mutantAt < 0 || state.mutants[mutantAt].result != 0 || len(state.drain.provisionals) == 0 ||
			state.drain.provisionals[0] != attempt.mutant {
			campaignInvariant("observe confirmation terminal", "confirmation mutant is invalid")
		}
		resolvedConfirmation := true
		switch terminal := event.terminal.(type) {
		case Settled:
			if terminal.Exit.Passed() {
				state.mutants[mutantAt].result = mutantSurvived
			} else {
				state.mutants[mutantAt].result = mutantKilled
			}
		case Tripped:
			switch terminal.Trip.(type) {
			case AutomaticDeadlineTrip, SerialDeadlineTrip:
				state.mutants[mutantAt].result = mutantTimedOut
			default:
				campaignInvariant("observe confirmation terminal", "confirmation trip is invalid")
			}
		case Infrastructure:
			resolvedConfirmation = false
			state.phase = campaignDraining
			state.drain = campaignDrainIntent{kind: campaignDrainAbort, cause: "confirmation infrastructure uncertainty"}
			state.drain.provisionals = nil
		default:
			campaignInvariant("observe confirmation terminal", "confirmation terminal is invalid")
		}
		if resolvedConfirmation {
			state.drain.provisionals = slices.Clone(state.drain.provisionals[1:])
		}
	default:
		campaignInvariant("observe terminal", "attempt kind is invalid")
	}
	attempt.stage = campaignAttemptSettled
	state.removeAttemptObligation(campaignResourceAdmission, event.attempt, event.generation)
	state.removeAttemptObligation(campaignResourceExecutionDomain, event.attempt, event.generation)
	var effects []campaignEffect
	state, effects = state.applyRuntimeCompensations(event.receipt)
	effects = append(transitionEffects, effects...)
	effects = append(effects, campaignEffect{
		kind: campaignEffectReleaseWorkspace, attempt: event.attempt, workspace: attempt.workspace,
	})

	return state.emitAll(effects)
}

func (state campaignState) stopCommittedPeers(except attemptIdentity) []campaignEffect {
	effects := make([]campaignEffect, 0, len(state.attempts))
	for _, attempt := range state.attempts {
		if attempt.identity == except || attempt.stage != campaignAttemptOwned {
			continue
		}
		effects = append(effects, campaignEffect{
			kind: campaignEffectStopAttempt, attempt: attempt.identity, generation: attempt.generation,
		})
	}

	return effects
}

func (state campaignState) onRuntimeEmergencySettled(
	event runtimeEmergencySettledEvent,
) (campaignState, []campaignEffect) {
	if state.drain.kind != campaignDrainRuntimeEmergency || event.epoch == 0 ||
		event.epoch != state.drain.epoch || event.settlement.epoch != event.epoch {
		campaignInvariant("settle runtime emergency", "emergency settlement is stale or wrong")
	}
	if len(event.settlement.residual) == 0 {
		for _, generation := range event.settlement.acknowledged {
			attemptAt := slices.IndexFunc(state.attempts, func(attempt campaignAttempt) bool {
				return attempt.generation == generation &&
					(attempt.stage == campaignAttemptProspective || attempt.stage == campaignAttemptOwned)
			})
			if attemptAt < 0 {
				campaignInvariant("settle runtime emergency", "acknowledged generation is unknown")
			}
			state.removeAttemptObligation(campaignResourceAdmission, state.attempts[attemptAt].identity, generation)
			state.removeAttemptObligation(campaignResourceExecutionDomain, state.attempts[attemptAt].identity, generation)
			state.attempts[attemptAt].stage = campaignAttemptSettled
		}
		effects := make([]campaignEffect, 0, len(event.settlement.acknowledged))
		for _, attempt := range state.attempts {
			if attempt.stage == campaignAttemptSettled {
				effects = append(effects, campaignEffect{
					kind: campaignEffectReleaseWorkspace, attempt: attempt.identity, workspace: attempt.workspace,
				})
			}
		}

		return state.emitAll(effects)
	}
	residual := slices.Clone(event.settlement.residual)
	state.failure = cleanupUnconfirmedFault{residual: nonEmptyResidualCustody{
		head: residual[0], tail: slices.Clone(residual[1:]),
	}}

	return state, nil
}

func (state campaignState) applyRuntimeCompensations(
	receipt observationResult,
) (campaignState, []campaignEffect) {
	effects := make([]campaignEffect, 0, len(receipt.cancelledWaiting)+len(receipt.compensatedGrants))
	for _, request := range receipt.cancelledWaiting {
		attemptAt := state.attemptIndex(request.attempt)
		if attemptAt < 0 || state.attempts[attemptAt].stage != campaignAttemptAdmissionWaiting ||
			state.attempts[attemptAt].request != request {
			campaignInvariant("compensate cancellation", "cancelled admission is stale or wrong")
		}
		state.attempts[attemptAt].stage = campaignAttemptSettled
		state.removeAttemptObligation(campaignResourceAdmission, request.attempt, 0)
		mutantAt := state.mutantIndex(state.attempts[attemptAt].mutant)
		if mutantAt >= 0 {
			state.mutants[mutantAt].primaryStarted = false
		}
		effects = append(effects, campaignEffect{
			kind: campaignEffectReleaseWorkspace, attempt: request.attempt,
			workspace: state.attempts[attemptAt].workspace,
		})
	}
	for _, grant := range receipt.compensatedGrants {
		if slices.Contains(state.pendingGrantReturns, grant) {
			campaignInvariant("compensate grant", "grant return is duplicated")
		}
		attemptAt := state.attemptIndex(grant.attempt)
		if attemptAt < 0 || (state.attempts[attemptAt].stage != campaignAttemptAdmissionWaiting &&
			state.attempts[attemptAt].stage != campaignAttemptGranted) {
			campaignInvariant("compensate grant", "grant return is stale or wrong")
		}
		state.pendingGrantReturns = append(state.pendingGrantReturns, grant)
		if state.attempts[attemptAt].stage == campaignAttemptGranted {
			state.attempts[attemptAt].stage = campaignAttemptReturningGrant
			state.removeAttemptObligation(campaignResourcePendingStart, grant.attempt, 0)
			effects = append(effects, campaignEffect{
				kind: campaignEffectReturnAdmission, attempt: grant.attempt, grant: grant,
			})
		}
	}

	return state, effects
}

func (state campaignState) insertProvisional(mutant mutantIdentity) []mutantIdentity {
	if slices.Contains(state.drain.provisionals, mutant) {
		campaignInvariant("queue provisional", "provisional mutant is duplicated")
	}
	queue := append(slices.Clone(state.drain.provisionals), mutant)
	slices.SortFunc(queue, func(left, right mutantIdentity) int {
		return state.mutantIndex(left) - state.mutantIndex(right)
	})

	return queue
}

func (state campaignState) materializeConfirmation() (campaignState, []campaignEffect) {
	if len(state.drain.provisionals) == 0 {
		campaignInvariant("materialize confirmation", "provisional queue is empty")
	}
	mutant := state.drain.provisionals[0]
	mutantAt := state.mutantIndex(mutant)
	if mutantAt < 0 || state.mutants[mutantAt].confirmationStarted {
		campaignInvariant("materialize confirmation", "confirmation was already started")
	}
	state.mutants[mutantAt].confirmationStarted = true
	state.phase = campaignConfirming
	var effect campaignEffect
	state, effect = state.materializeAttempt(campaignAttemptConfirmation, mutant)

	return state, []campaignEffect{effect}
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
		snapshot: state.snapshot, attempt: attempt, mutant: mutant,
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

func (state campaignState) emitAll(effects []campaignEffect) (campaignState, []campaignEffect) {
	for index := range effects {
		state.nextEffect++
		effects[index].id = state.nextEffect
	}

	return state, effects
}

func (state campaignState) obligationIndex(kind campaignResourceKind, identity string) int {
	return slices.IndexFunc(state.obligations, func(obligation campaignObligation) bool {
		return obligation.kind == kind && obligation.identity == identity
	})
}

func campaignEventSummary(event campaignEvent) string {
	if event.payload == nil {
		return "event=" + strconv.FormatUint(uint64(event.id), 10) + " kind=nil"
	}
	summary := "event=" + strconv.FormatUint(uint64(event.id), 10) + " kind=" + event.payload.campaignEventName()
	switch observed := event.payload.(type) {
	case snapshotEstablishedEvent:
		summary += " snapshot=" + string(observed.snapshot)
	case catalogueDiscoveredEvent:
		summary += " snapshot=" + string(observed.snapshot) + " mutants=" + strconv.Itoa(len(observed.mutants))
	case campaignPreparationFailedEvent:
		summary += " stage=" + strconv.Itoa(int(observed.stage)) + " cause=" + observed.cause
	case resourceSettledEvent:
		summary += " resource=" + strconv.Itoa(int(observed.kind)) + ":" + observed.identity
	case workspaceMaterializedEvent:
		summary += " attempt=" + string(observed.attempt) + " snapshot=" + string(observed.snapshot)
	case admissionGrantedEvent:
		summary += " attempt=" + string(observed.attempt)
	case startCommittedEvent:
		summary += " attempt=" + string(observed.attempt) + " generation=" +
			strconv.FormatUint(uint64(observed.result.generation), 10)
	case attemptLaunchEvent:
		summary += " attempt=" + string(observed.attempt) + " generation=" +
			strconv.FormatUint(uint64(observed.generation), 10)
	case attemptTerminalEvent:
		summary += " attempt=" + string(observed.attempt) + " generation=" +
			strconv.FormatUint(uint64(observed.generation), 10) + " resolved-deadline=" +
			observed.resolvedMutationDeadline.String()
	case confirmationBarrierBoundEvent:
		summary += " attempt=" + string(observed.attempt)
	case grantReturnAcknowledgedEvent:
		summary += " attempt=" + string(observed.grant.attempt)
	case runtimeEmergencySettledEvent:
		summary += " epoch=" + strconv.FormatUint(uint64(observed.epoch), 10) +
			" residual=" + strconv.Itoa(len(observed.settlement.residual))
	}

	return summary
}

func (state campaignState) stableIdentitySnapshot(event campaignEvent) []string {
	identities := []string{"campaign=" + string(state.definition.identity)}
	if state.snapshot != "" {
		identities = append(identities, "snapshot="+string(state.snapshot))
	}
	if state.runtimeToken.id != 0 {
		identities = append(identities, "campaign-token="+strconv.FormatUint(uint64(state.runtimeToken.id), 10))
	}
	for _, attempt := range state.attempts {
		identity := "attempt=" + string(attempt.identity)
		if attempt.generation != 0 {
			identity += "/generation=" + strconv.FormatUint(uint64(attempt.generation), 10)
		}
		identities = append(identities, identity)
	}
	if state.drain.epoch != 0 {
		identities = append(identities, "epoch="+strconv.FormatUint(uint64(state.drain.epoch), 10))
	}
	_ = event

	return identities
}

func (state campaignState) obligationSnapshot() []string {
	obligations := make([]string, len(state.obligations))
	for index, obligation := range state.obligations {
		obligations[index] = strconv.Itoa(int(obligation.kind)) + ":" + obligation.identity + ":" +
			string(obligation.attempt) + ":" + strconv.FormatUint(uint64(obligation.generation), 10)
	}

	return obligations
}

func (state campaignState) clone() campaignState {
	state.definition.command = slices.Clone(state.definition.command)
	state.definition.env = slices.Clone(state.definition.env)
	state.catalogue = slices.Clone(state.catalogue)
	state.mutants = slices.Clone(state.mutants)
	state.attempts = slices.Clone(state.attempts)
	state.drain.provisionals = slices.Clone(state.drain.provisionals)
	state.pendingGrantReturns = slices.Clone(state.pendingGrantReturns)
	state.obligations = slices.Clone(state.obligations)
	state.trace = slices.Clone(state.trace)
	for index := range state.trace {
		state.trace[index].identities = slices.Clone(state.trace[index].identities)
		state.trace[index].obligations = slices.Clone(state.trace[index].obligations)
	}

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
