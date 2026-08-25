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
	id                         campaignEffectID
	kind                       campaignEffectKind
	snapshot                   snapshotIdentity
	workspace                  string
	attempt                    attemptIdentity
	mutant                     mutantIdentity
	request                    campaignAdmission
	grant                      campaignAdmission
	generation                 attemptGeneration
	binding                    campaignBarrierBinding
	terminal                   campaignTerminalCandidate
	fatalEpoch                 fatalEpochID
	attemptKind                campaignAttemptKind
	completesConfirmationQueue bool
	spec                       Spec
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
	pendingGrantReturns      []campaignAdmission
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
	request    campaignAdmission
	grant      campaignAdmission
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

// Campaign facts deliberately copy runtime authority into a reducer-owned
// vocabulary. Channels, broker custody, and settlement objects never enter the
// pure campaign state machine.
type campaignAdmission struct {
	campaign campaignToken
	attempt  attemptIdentity
	class    admissionClass
	profile  Profile
	deadline time.Duration
}

type campaignAdmissionResult struct {
	decision   admissionDecision
	request    campaignAdmission
	fatalEpoch fatalEpochID
}

type campaignStartResult struct {
	decision                                         startCommittedDecision
	generation                                       attemptGeneration
	settlementAcknowledged, runtimeClosureInProgress bool
}

type campaignRuntimeReceipt struct {
	generation                                      attemptGeneration
	cancelledWaiting, compensatedGrants             []campaignAdmission
	settlementAcknowledged, confirmationProvisional bool
	pressureTransitioned, runtimeClosureInProgress  bool
	confirmationObserved, confirmationQueueDrained  bool
	fatalEpoch                                      fatalEpochID
}

type campaignBarrierBinding struct {
	campaign campaignToken
	attempt  attemptIdentity
	profile  Profile
	deadline time.Duration
}

type campaignBarrierResult struct {
	decision   barrierDecision
	request    campaignAdmission
	deliveries []campaignAdmission
}

type campaignTerminalResult struct {
	decision terminalDecision
	epoch    fatalEpochID
}

type campaignRuntimeClosure struct {
	epoch                               fatalEpochID
	cancelledWaiting, compensatedGrants []campaignAdmission
	residual                            []campaignResidualCustody
}

type campaignResidualCustody struct {
	generation  attemptGeneration
	stage       admissionStage
	transferred bool
}

type campaignEmergencySettlement struct {
	epoch        fatalEpochID
	owner        campaignToken
	acknowledged []attemptGeneration
	residual     []campaignResidualCustody
}

type resourceSettledEvent struct {
	kind     campaignResourceKind
	identity string
}
type resourceSettlementFailedEvent struct {
	kind     campaignResourceKind
	identity string
	cause    string
}
type terminalCommittedEvent struct{ result campaignTerminalResult }
type workspaceMaterializedEvent struct {
	attempt   attemptIdentity
	workspace string
	snapshot  snapshotIdentity
}
type workspaceMaterializationFailedEvent struct {
	attempt attemptIdentity
	cause   string
}
type admissionGrantedEvent struct {
	attempt attemptIdentity
	grant   campaignAdmission
}
type admissionCancelledEvent struct {
	attempt attemptIdentity
	request campaignAdmission
	result  campaignAdmissionResult
}
type admissionRejectedEvent struct {
	attempt attemptIdentity
	result  campaignAdmissionResult
	cause   string
}
type startCommittedEvent struct {
	attempt attemptIdentity
	grant   campaignAdmission
	result  campaignStartResult
}
type attemptLaunchEvent struct {
	attempt    attemptIdentity
	generation attemptGeneration
	result     campaignLaunchObservation
	receipt    campaignRuntimeReceipt
}
type campaignLaunchKind uint8

const (
	campaignLaunchOwned campaignLaunchKind = iota + 1
	campaignLaunchNotReleased
	campaignLaunchUnconfirmed
)

type campaignLaunchObservation struct {
	kind     campaignLaunchKind
	failure  LaunchFailure
	residual Residual
}
type attemptTerminalEvent struct {
	attempt                  attemptIdentity
	generation               attemptGeneration
	terminal                 Terminal
	receipt                  campaignRuntimeReceipt
	resolvedMutationDeadline mutationDeadlineResolution
}
type mutationDeadlineResolution struct{ duration time.Duration }
type confirmationBarrierBoundEvent struct {
	attempt attemptIdentity
	result  campaignBarrierResult
}
type grantReturnAcknowledgedEvent struct {
	grant  campaignAdmission
	result campaignAdmissionResult
}
type runtimeEmergencySettledEvent struct {
	epoch      fatalEpochID
	settlement campaignEmergencySettlement
}
type runtimeEmergencyStartedEvent struct{ closure campaignRuntimeClosure }

func (campaignRegisteredEvent) campaignEventPayload()             {}
func (snapshotEstablishedEvent) campaignEventPayload()            {}
func (catalogueDiscoveredEvent) campaignEventPayload()            {}
func (campaignPreparationFailedEvent) campaignEventPayload()      {}
func (resourceSettledEvent) campaignEventPayload()                {}
func (resourceSettlementFailedEvent) campaignEventPayload()       {}
func (terminalCommittedEvent) campaignEventPayload()              {}
func (workspaceMaterializedEvent) campaignEventPayload()          {}
func (workspaceMaterializationFailedEvent) campaignEventPayload() {}
func (admissionGrantedEvent) campaignEventPayload()               {}
func (admissionCancelledEvent) campaignEventPayload()             {}
func (admissionRejectedEvent) campaignEventPayload()              {}
func (startCommittedEvent) campaignEventPayload()                 {}
func (attemptLaunchEvent) campaignEventPayload()                  {}
func (attemptTerminalEvent) campaignEventPayload()                {}
func (confirmationBarrierBoundEvent) campaignEventPayload()       {}
func (grantReturnAcknowledgedEvent) campaignEventPayload()        {}
func (runtimeEmergencySettledEvent) campaignEventPayload()        {}
func (runtimeEmergencyStartedEvent) campaignEventPayload()        {}

func (campaignRegisteredEvent) campaignEventName() string  { return "campaign registered" }
func (snapshotEstablishedEvent) campaignEventName() string { return "snapshot established" }
func (catalogueDiscoveredEvent) campaignEventName() string { return "catalogue discovered" }
func (campaignPreparationFailedEvent) campaignEventName() string {
	return "campaign preparation failed"
}
func (resourceSettledEvent) campaignEventName() string { return "resource settled" }
func (resourceSettlementFailedEvent) campaignEventName() string {
	return "resource settlement failed"
}
func (terminalCommittedEvent) campaignEventName() string     { return "terminal committed" }
func (workspaceMaterializedEvent) campaignEventName() string { return "workspace materialized" }
func (workspaceMaterializationFailedEvent) campaignEventName() string {
	return "workspace materialization failed"
}
func (admissionGrantedEvent) campaignEventName() string   { return "admission granted" }
func (admissionCancelledEvent) campaignEventName() string { return "admission cancelled" }
func (admissionRejectedEvent) campaignEventName() string  { return "admission rejected" }
func (startCommittedEvent) campaignEventName() string     { return "start committed" }
func (attemptLaunchEvent) campaignEventName() string      { return "attempt launched" }
func (attemptTerminalEvent) campaignEventName() string    { return "attempt terminal" }
func (confirmationBarrierBoundEvent) campaignEventName() string {
	return "confirmation barrier bound"
}
func (grantReturnAcknowledgedEvent) campaignEventName() string { return "grant return acknowledged" }
func (runtimeEmergencySettledEvent) campaignEventName() string { return "runtime emergency settled" }
func (runtimeEmergencyStartedEvent) campaignEventName() string { return "runtime emergency started" }

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
	head campaignResidualCustody
	tail []campaignResidualCustody
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
	case resourceSettlementFailedEvent:
		return state.onResourceSettlementFailed(observed)
	case terminalCommittedEvent:
		return state.onTerminalCommitted(observed)
	case workspaceMaterializedEvent:
		return state.onWorkspaceMaterialized(observed)
	case workspaceMaterializationFailedEvent:
		return state.onWorkspaceMaterializationFailed(observed)
	case admissionGrantedEvent:
		return state.onAdmissionGranted(observed)
	case admissionCancelledEvent:
		return state.onAdmissionCancelled(observed)
	case admissionRejectedEvent:
		return state.onAdmissionRejected(observed)
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
	case runtimeEmergencyStartedEvent:
		return state.onRuntimeEmergencyStarted(observed)
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

		return state.proposeTerminal()
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
		return state.proposeTerminal()
	}

	return state, nil
}

func (state campaignState) onResourceSettlementFailed(
	event resourceSettlementFailedEvent,
) (campaignState, []campaignEffect) {
	index := state.obligationIndex(event.kind, event.identity)
	if index < 0 || event.kind == campaignResourceRegistration || event.cause == "" {
		campaignInvariant("settle resource", "resource failure is invalid")
	}
	state.obligations = slices.Delete(state.obligations, index, index+1)
	state.phase = campaignDraining
	state.drain = campaignDrainIntent{kind: campaignDrainAbort, cause: event.cause}
	if event.kind == campaignResourceWorkspace {
		attemptAt := slices.IndexFunc(state.attempts, func(attempt campaignAttempt) bool {
			return attempt.workspace == event.identity && attempt.stage == campaignAttemptSettled
		})
		if attemptAt < 0 {
			campaignInvariant("settle resource", "failed workspace has no settled attempt")
		}
		state.attempts = slices.Delete(state.attempts, attemptAt, attemptAt+1)
		if len(state.attempts) == 0 {
			return state.releaseSnapshot(campaignTerminalAborted)
		}

		return state.emitAll(state.abortOutstandingAttempts(""))
	}
	if event.kind == campaignResourceSnapshot && len(state.obligations) == 1 &&
		state.obligations[0].kind == campaignResourceRegistration {
		state.candidate = campaignTerminalCandidate{kind: campaignTerminalAborted}

		return state.proposeTerminal()
	}

	return state, nil
}

func (state campaignState) onTerminalCommitted(event terminalCommittedEvent) (campaignState, []campaignEffect) {
	if event.result.decision == terminalRejectedClosed {
		if state.candidate.kind == 0 || event.result.epoch == 0 || len(state.obligations) != 1 ||
			state.obligations[0].kind != campaignResourceRegistration {
			campaignInvariant("commit terminal", "fatal terminal rejection is invalid")
		}
		state.phase = campaignDraining
		state.drain = campaignDrainIntent{
			kind: campaignDrainRuntimeEmergency, cause: "fatal closure won terminal commitment",
			epoch: event.result.epoch,
		}
		state.candidate = campaignTerminalCandidate{}

		return state, nil
	}
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
	obligationAt := state.obligationIndex(campaignResourceWorkspace, string(event.attempt))
	state.obligations[obligationAt].identity = event.workspace
	if state.drain.kind == campaignDrainAbort || state.drain.kind == campaignDrainRuntimeEmergency {
		state.attempts[attemptAt].stage = campaignAttemptSettled
		mutantAt := state.mutantIndex(state.attempts[attemptAt].mutant)
		if mutantAt >= 0 {
			state.mutants[mutantAt].primaryStarted = false
		}

		return state.emit(campaignEffect{
			kind: campaignEffectReleaseWorkspace, attempt: event.attempt, workspace: event.workspace,
		})
	}
	state.attempts[attemptAt].stage = campaignAttemptAdmissionWaiting
	class := sharedAdmission
	if state.attempts[attemptAt].kind == campaignAttemptBaseline {
		class = exclusiveAdmission
	} else if state.attempts[attemptAt].kind == campaignAttemptConfirmation {
		class = confirmationAdmission
	} else if state.definition.profile == SerialProfile {
		class = serialPrimaryAdmission
	}
	deadline := state.mutationDeadline
	if state.attempts[attemptAt].kind == campaignAttemptBaseline {
		deadline = state.definition.baselineDeadline
	}
	if deadline <= 0 {
		campaignInvariant("materialize workspace", "attempt deadline is unresolved")
	}
	if state.attempts[attemptAt].kind == campaignAttemptConfirmation && !state.confirmationBarrierBound {
		state.obligations = append(state.obligations, campaignObligation{
			kind: campaignResourceAdmission, identity: string(event.attempt), attempt: event.attempt,
		})
		binding := campaignBarrierBinding{
			campaign: state.runtimeToken, attempt: event.attempt,
			profile: state.definition.profile, deadline: deadline,
		}

		return state.emit(campaignEffect{
			kind: campaignEffectBindConfirmationBarrier, attempt: event.attempt,
			mutant: state.attempts[attemptAt].mutant, binding: binding,
		})
	}
	request := campaignAdmission{
		campaign: state.runtimeToken, attempt: event.attempt, class: class,
		profile: state.definition.profile, deadline: deadline,
	}
	state.attempts[attemptAt].request = request
	state.obligations = append(state.obligations, campaignObligation{
		kind: campaignResourceAdmission, identity: string(event.attempt), attempt: event.attempt,
	})

	return state.emit(campaignEffect{kind: campaignEffectRequestAdmission, attempt: event.attempt, request: request})
}

func (state campaignState) onWorkspaceMaterializationFailed(
	event workspaceMaterializationFailedEvent,
) (campaignState, []campaignEffect) {
	attemptAt := state.attemptIndex(event.attempt)
	if attemptAt < 0 || state.attempts[attemptAt].stage != campaignAttemptMaterializing || event.cause == "" {
		campaignInvariant("materialize workspace", "workspace failure is invalid")
	}
	state.removeAttemptObligation(campaignResourceWorkspace, event.attempt, 0)
	mutantAt := state.mutantIndex(state.attempts[attemptAt].mutant)
	if mutantAt >= 0 {
		state.mutants[mutantAt].primaryStarted = false
	}
	state.attempts = slices.Delete(state.attempts, attemptAt, attemptAt+1)
	state.phase = campaignDraining
	state.drain = campaignDrainIntent{kind: campaignDrainAbort, cause: event.cause}
	effects := state.abortOutstandingAttempts("")
	if len(state.attempts) == 0 {
		return state.releaseSnapshot(campaignTerminalAborted)
	}

	return state.emitAll(effects)
}

func (state campaignState) onAdmissionGranted(event admissionGrantedEvent) (campaignState, []campaignEffect) {
	attemptAt := state.attemptIndex(event.attempt)
	if attemptAt < 0 || state.attempts[attemptAt].stage != campaignAttemptAdmissionWaiting ||
		!sameAdmissionRequest(event.grant, state.attempts[attemptAt].request) {
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

func (state campaignState) onAdmissionCancelled(
	event admissionCancelledEvent,
) (campaignState, []campaignEffect) {
	attemptAt := state.attemptIndex(event.attempt)
	if attemptAt < 0 || !sameAdmissionRequest(event.request, state.attempts[attemptAt].request) ||
		!sameAdmissionRequest(event.result.request, event.request) {
		campaignInvariant("cancel admission", "cancellation is stale or wrong")
	}
	if event.result.decision == admissionRejectedAlreadyCommitted {
		if state.attempts[attemptAt].stage != campaignAttemptGranted {
			campaignInvariant("cancel admission", "commitment race has no granted attempt")
		}

		return state, nil
	}
	stage := state.attempts[attemptAt].stage
	validWaiting := stage == campaignAttemptAdmissionWaiting &&
		(event.result.decision == admissionCancelledWaiting || event.result.decision == admissionCancelledGranted)
	validGranted := stage == campaignAttemptGranted && event.result.decision == admissionCancelledGranted
	if !validWaiting && !validGranted {
		campaignInvariant("cancel admission", "cancellation result is invalid")
	}
	state.removeAttemptObligation(campaignResourceAdmission, event.attempt, 0)
	if stage == campaignAttemptGranted {
		state.removeAttemptObligation(campaignResourcePendingStart, event.attempt, 0)
	}
	state.attempts[attemptAt].stage = campaignAttemptSettled
	mutantAt := state.mutantIndex(state.attempts[attemptAt].mutant)
	if mutantAt >= 0 {
		state.mutants[mutantAt].primaryStarted = false
	}

	return state.emit(campaignEffect{
		kind: campaignEffectReleaseWorkspace, attempt: event.attempt,
		workspace: state.attempts[attemptAt].workspace,
	})
}

func (state campaignState) onAdmissionRejected(
	event admissionRejectedEvent,
) (campaignState, []campaignEffect) {
	attemptAt := state.attemptIndex(event.attempt)
	if attemptAt < 0 || state.attempts[attemptAt].stage != campaignAttemptAdmissionWaiting ||
		!sameAdmissionRequest(event.result.request, state.attempts[attemptAt].request) || event.cause == "" ||
		event.result.decision == admissionAccepted {
		campaignInvariant("reject admission", "admission rejection is invalid")
	}
	if event.result.decision != admissionRejectedClosed && event.result.decision != admissionRejectedGateClosed {
		campaignInvariant("reject admission", "admission rejection decision is invalid")
	}
	state.removeAttemptObligation(campaignResourceAdmission, event.attempt, 0)
	state.attempts[attemptAt].stage = campaignAttemptSettled
	mutantAt := state.mutantIndex(state.attempts[attemptAt].mutant)
	if mutantAt >= 0 {
		state.mutants[mutantAt].primaryStarted = false
	}
	if event.result.decision == admissionRejectedClosed {
		if event.result.fatalEpoch == 0 {
			campaignInvariant("reject admission", "closed rejection lacks fatal epoch")
		}
		state.phase = campaignDraining
		state.drain = campaignDrainIntent{
			kind: campaignDrainRuntimeEmergency, cause: event.cause, epoch: event.result.fatalEpoch,
		}
	} else if state.drain.kind != campaignDrainConfirm {
		campaignInvariant("reject admission", "gate rejection arrived outside confirmation closure")
	}
	effects := state.abortOutstandingAttempts(event.attempt)
	effects = append(effects, campaignEffect{
		kind: campaignEffectReleaseWorkspace, attempt: event.attempt,
		workspace: state.attempts[attemptAt].workspace,
	})

	return state.emitAll(effects)
}

func (state campaignState) acceptGrant(attemptAt int, grant campaignAdmission) (campaignState, []campaignEffect) {
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
	spec := Spec{
		Attempt: string(event.attempt), Command: slices.Clone(state.definition.command),
		Dir: state.attempts[attemptAt].workspace, Env: slices.Clone(state.definition.env),
		Profile: state.definition.profile, Deadline: deadline,
	}

	return state.emit(campaignEffect{
		kind: campaignEffectLaunchAttempt, attempt: event.attempt, generation: event.result.generation,
		snapshot: state.snapshot, workspace: state.attempts[attemptAt].workspace,
		attemptKind: state.attempts[attemptAt].kind,
		completesConfirmationQueue: state.attempts[attemptAt].kind == campaignAttemptConfirmation &&
			len(state.drain.provisionals) == 1,
		spec: spec,
	})
}

func (state campaignState) onAttemptLaunch(event attemptLaunchEvent) (campaignState, []campaignEffect) {
	attemptAt := state.attemptIndex(event.attempt)
	if attemptAt < 0 || state.attempts[attemptAt].stage != campaignAttemptProspective || event.generation == 0 ||
		event.generation != state.attempts[attemptAt].generation || event.receipt.generation != event.generation {
		campaignInvariant("observe launch", "launch observation is invalid")
	}
	switch event.result.kind {
	case campaignLaunchOwned:
		if event.result.failure != 0 || event.result.residual != 0 {
			campaignInvariant("observe launch", "owned launch carries unrelated evidence")
		}
		state.attempts[attemptAt].stage = campaignAttemptOwned
		if state.drain.kind == campaignDrainAbort {
			return state.emit(campaignEffect{
				kind: campaignEffectStopAttempt, attempt: event.attempt, generation: event.generation,
			})
		}

		return state, nil
	case campaignLaunchNotReleased:
		if (event.result.failure != LaunchFailed && event.result.failure != LaunchResourceExhausted) ||
			event.result.residual != 0 ||
			!event.receipt.settlementAcknowledged || event.receipt.runtimeClosureInProgress {
			campaignInvariant("observe launch", "proven no-release observation is invalid")
		}
		state.attempts[attemptAt].stage = campaignAttemptSettled
		state.removeAttemptObligation(campaignResourceAdmission, event.attempt, event.generation)
		state.removeAttemptObligation(campaignResourceExecutionDomain, event.attempt, event.generation)
		state.phase = campaignDraining
		state.drain = campaignDrainIntent{kind: campaignDrainAbort, cause: "attempt was not released"}
		effects := state.abortOutstandingAttempts(event.attempt)
		effects = append(effects, campaignEffect{
			kind: campaignEffectReleaseWorkspace, attempt: event.attempt,
			workspace: state.attempts[attemptAt].workspace,
		})

		return state.emitAll(effects)
	case campaignLaunchUnconfirmed:
		if event.result.residual != ProspectiveUnresolved || event.result.failure != 0 ||
			!event.receipt.runtimeClosureInProgress ||
			event.receipt.fatalEpoch == 0 {
			campaignInvariant("observe launch", "unconfirmed launch observation is invalid")
		}
		state.phase = campaignDraining
		state.drain = campaignDrainIntent{
			kind: campaignDrainRuntimeEmergency, cause: "prospective launch unresolved",
			epoch: event.receipt.fatalEpoch,
		}
		return state.applyRuntimeCompensations(event.receipt)
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
		if terminalDeadline(event.terminal) != state.attemptDeadline(state.attempts[attemptAt]) {
			campaignInvariant("observe drain unconfirmed", "attempt deadline is not campaign-authorized")
		}
		state.phase = campaignDraining
		state.drain = campaignDrainIntent{
			kind: campaignDrainRuntimeEmergency, cause: "execution-domain drainage unconfirmed",
			epoch: event.receipt.fatalEpoch,
		}
		return state.applyRuntimeCompensations(event.receipt)
	}
	if attemptAt < 0 || state.attempts[attemptAt].stage != campaignAttemptOwned ||
		event.generation != state.attempts[attemptAt].generation || event.terminal == nil ||
		event.receipt.generation != event.generation || !event.receipt.settlementAcknowledged ||
		event.receipt.runtimeClosureInProgress {
		campaignInvariant("observe terminal", "attempt terminal is invalid")
	}
	attempt := &state.attempts[attemptAt]
	if terminalDeadline(event.terminal) != state.attemptDeadline(*attempt) {
		campaignInvariant("observe terminal", "attempt deadline is not campaign-authorized")
	}
	var transitionEffects []campaignEffect
	switch attempt.kind {
	case campaignAttemptBaseline:
		settled, passed := event.terminal.(Settled)
		passed = passed && settled.Exit.Passed() && settled.Deadline == state.definition.baselineDeadline &&
			settled.CommandDuration > 0
		if passed && event.resolvedMutationDeadline.duration <= 0 {
			campaignInvariant("observe terminal", "recorded baseline mutation deadline is not positive")
		}
		if passed {
			state.mutationDeadline = event.resolvedMutationDeadline.duration
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
				transitionEffects = state.abortOutstandingAttempts(event.attempt)
			}
		case Infrastructure:
			if state.drain.kind != campaignDrainAbort {
				state.phase = campaignDraining
				state.drain = campaignDrainIntent{kind: campaignDrainAbort, cause: "primary infrastructure uncertainty"}
				transitionEffects = state.abortOutstandingAttempts(event.attempt)
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
		_, settledConfirmation := event.terminal.(Settled)
		_, trippedConfirmation := event.terminal.(Tripped)
		if (settledConfirmation || trippedConfirmation) &&
			(!event.receipt.confirmationObserved ||
				event.receipt.confirmationQueueDrained != (len(state.drain.provisionals) == 1)) {
			campaignInvariant("observe confirmation terminal", "confirmation runtime authority is missing")
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

func (state campaignState) attemptDeadline(attempt campaignAttempt) time.Duration {
	if attempt.kind == campaignAttemptBaseline {
		return state.definition.baselineDeadline
	}

	return state.mutationDeadline
}

func terminalDeadline(terminal Terminal) time.Duration {
	switch observed := terminal.(type) {
	case Settled:
		return observed.Deadline
	case Tripped:
		return observed.Deadline
	case Stopped:
		return observed.Deadline
	case Infrastructure:
		return observed.Deadline
	case DrainUnconfirmed:
		return observed.Deadline
	default:
		return 0
	}
}

func (state campaignState) abortOutstandingAttempts(except attemptIdentity) []campaignEffect {
	effects := make([]campaignEffect, 0, len(state.attempts))
	for _, attempt := range state.attempts {
		if attempt.identity == except {
			continue
		}
		switch attempt.stage {
		case campaignAttemptAdmissionWaiting, campaignAttemptGranted:
			effects = append(effects, campaignEffect{
				kind: campaignEffectCancelAdmission, attempt: attempt.identity, request: attempt.request,
			})
		case campaignAttemptOwned:
			effects = append(effects, campaignEffect{
				kind: campaignEffectStopAttempt, attempt: attempt.identity, generation: attempt.generation,
			})
		}
	}

	return effects
}

func (state campaignState) onRuntimeEmergencySettled(
	event runtimeEmergencySettledEvent,
) (campaignState, []campaignEffect) {
	if state.drain.kind != campaignDrainRuntimeEmergency || event.epoch == 0 ||
		event.epoch != state.drain.epoch || event.settlement.epoch != event.epoch ||
		(len(event.settlement.residual) == 0) != (event.settlement.owner.id == 0) {
		campaignInvariant("settle runtime emergency", "emergency settlement is stale or wrong")
	}
	if len(event.settlement.residual) == 0 || event.settlement.owner != state.runtimeToken {
		effects := make([]campaignEffect, 0, len(event.settlement.acknowledged))
		for _, generation := range event.settlement.acknowledged {
			attemptAt := slices.IndexFunc(state.attempts, func(attempt campaignAttempt) bool {
				return attempt.generation == generation &&
					(attempt.stage == campaignAttemptProspective || attempt.stage == campaignAttemptOwned)
			})
			if attemptAt < 0 {
				continue
			}
			state.removeAttemptObligation(campaignResourceAdmission, state.attempts[attemptAt].identity, generation)
			state.removeAttemptObligation(campaignResourceExecutionDomain, state.attempts[attemptAt].identity, generation)
			state.attempts[attemptAt].stage = campaignAttemptSettled
			effects = append(effects, campaignEffect{
				kind: campaignEffectReleaseWorkspace, attempt: state.attempts[attemptAt].identity,
				workspace: state.attempts[attemptAt].workspace,
			})
		}
		if len(effects) == 0 && len(state.obligations) == 1 &&
			state.obligations[0].kind == campaignResourceRegistration {
			state.candidate = campaignTerminalCandidate{kind: campaignTerminalAborted}

			return state.proposeTerminal()
		}

		return state.emitAll(effects)
	}
	residual := slices.Clone(event.settlement.residual)
	state.failure = cleanupUnconfirmedFault{residual: nonEmptyResidualCustody{
		head: residual[0], tail: slices.Clone(residual[1:]),
	}}

	return state, nil
}

func (state campaignState) proposeTerminal() (campaignState, []campaignEffect) {
	effect := campaignEffect{kind: campaignEffectProposeTerminal, terminal: state.candidate}
	if state.drain.kind == campaignDrainRuntimeEmergency {
		effect.fatalEpoch = state.drain.epoch
	}

	return state.emit(effect)
}

func (state campaignState) runtimeEmergencySettlementRequest() (fatalEpochID, bool) {
	return state.drain.epoch, state.drain.kind == campaignDrainRuntimeEmergency
}

func (state campaignState) onRuntimeEmergencyStarted(
	event runtimeEmergencyStartedEvent,
) (campaignState, []campaignEffect) {
	if event.closure.epoch == 0 ||
		(state.drain.kind == campaignDrainRuntimeEmergency && state.drain.epoch != event.closure.epoch) {
		campaignInvariant("start runtime emergency", "runtime closure is stale or wrong")
	}
	state.phase = campaignDraining
	state.drain = campaignDrainIntent{
		kind: campaignDrainRuntimeEmergency, cause: "process runtime emergency", epoch: event.closure.epoch,
	}
	state.candidate = campaignTerminalCandidate{}

	return state.applyRuntimeCompensations(campaignRuntimeReceipt{
		cancelledWaiting:  event.closure.cancelledWaiting,
		compensatedGrants: event.closure.compensatedGrants,
	})
}

func (state campaignState) applyRuntimeCompensations(
	receipt campaignRuntimeReceipt,
) (campaignState, []campaignEffect) {
	effects := make([]campaignEffect, 0, len(receipt.cancelledWaiting)+len(receipt.compensatedGrants))
	for _, request := range receipt.cancelledWaiting {
		attemptAt := state.attemptIndex(request.attempt)
		if attemptAt < 0 || state.attempts[attemptAt].stage != campaignAttemptAdmissionWaiting ||
			!sameAdmissionRequest(state.attempts[attemptAt].request, request) {
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

func sameAdmissionRequest(left, right campaignAdmission) bool {
	return left.campaign == right.campaign && left.attempt == right.attempt && left.class == right.class &&
		left.profile == right.profile && left.deadline == right.deadline
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
	case resourceSettlementFailedEvent:
		summary += " resource=" + strconv.Itoa(int(observed.kind)) + ":" + observed.identity +
			" cause=" + observed.cause
	case terminalCommittedEvent:
		summary += " decision=" + strconv.Itoa(int(observed.result.decision)) + " epoch=" +
			strconv.FormatUint(uint64(observed.result.epoch), 10)
	case workspaceMaterializedEvent:
		summary += " attempt=" + string(observed.attempt) + " snapshot=" + string(observed.snapshot)
	case workspaceMaterializationFailedEvent:
		summary += " attempt=" + string(observed.attempt) + " cause=" + observed.cause
	case admissionGrantedEvent:
		summary += " attempt=" + string(observed.attempt)
	case admissionCancelledEvent:
		summary += " attempt=" + string(observed.attempt) + " decision=" +
			strconv.Itoa(int(observed.result.decision))
	case admissionRejectedEvent:
		summary += " attempt=" + string(observed.attempt) + " decision=" +
			strconv.Itoa(int(observed.result.decision)) + " cause=" + observed.cause
	case startCommittedEvent:
		summary += " attempt=" + string(observed.attempt) + " generation=" +
			strconv.FormatUint(uint64(observed.result.generation), 10)
	case attemptLaunchEvent:
		summary += " attempt=" + string(observed.attempt) + " generation=" +
			strconv.FormatUint(uint64(observed.generation), 10) + " receipt=" +
			campaignReceiptSummary(observed.receipt)
	case attemptTerminalEvent:
		summary += " attempt=" + string(observed.attempt) + " generation=" +
			strconv.FormatUint(uint64(observed.generation), 10) + " resolved-deadline=" +
			observed.resolvedMutationDeadline.duration.String() + " terminal=" + campaignTerminalSummary(observed.terminal) +
			" receipt=" + campaignReceiptSummary(observed.receipt)
	case confirmationBarrierBoundEvent:
		summary += " attempt=" + string(observed.attempt)
	case grantReturnAcknowledgedEvent:
		summary += " attempt=" + string(observed.grant.attempt)
	case runtimeEmergencySettledEvent:
		summary += " epoch=" + strconv.FormatUint(uint64(observed.epoch), 10) +
			" residual=" + strconv.Itoa(len(observed.settlement.residual))
	case runtimeEmergencyStartedEvent:
		summary += " epoch=" + strconv.FormatUint(uint64(observed.closure.epoch), 10) +
			" residual=" + strconv.Itoa(len(observed.closure.residual))
	}

	return summary
}

func campaignTerminalSummary(terminal Terminal) string {
	switch observed := terminal.(type) {
	case Settled:
		return "settled/code=" + strconv.Itoa(observed.Exit.Code) + "/signal=" +
			strconv.Itoa(observed.Exit.Signal) + "/deadline=" + observed.Deadline.String()
	case Tripped:
		trip := "unknown"
		switch evidence := observed.Trip.(type) {
		case FuseTrip:
			trip = "fuse/live=" + strconv.Itoa(evidence.Live)
		case AutomaticDeadlineTrip:
			trip = "automatic-deadline/peak=" + strconv.Itoa(evidence.Peak.Value) +
				"/present=" + strconv.FormatBool(evidence.Peak.Present)
		case SerialDeadlineTrip:
			trip = "serial-deadline"
		}

		return "tripped/" + trip + "/deadline=" + observed.Deadline.String()
	case Stopped:
		return "stopped/deadline=" + observed.Deadline.String()
	case Infrastructure:
		return "infrastructure/cause=" + strconv.Itoa(int(observed.Cause)) +
			"/deadline=" + observed.Deadline.String()
	case DrainUnconfirmed:
		return "drain-unconfirmed/residual=" + strconv.Itoa(int(observed.Residual)) +
			"/deadline=" + observed.Deadline.String()
	default:
		return "nil-or-unknown"
	}
}

func campaignReceiptSummary(receipt campaignRuntimeReceipt) string {
	summary := "generation=" + strconv.FormatUint(uint64(receipt.generation), 10) +
		"/settled=" + strconv.FormatBool(receipt.settlementAcknowledged) +
		"/provisional=" + strconv.FormatBool(receipt.confirmationProvisional) +
		"/pressure=" + strconv.FormatBool(receipt.pressureTransitioned) +
		"/confirmation-observed=" + strconv.FormatBool(receipt.confirmationObserved) +
		"/confirmation-queue-drained=" + strconv.FormatBool(receipt.confirmationQueueDrained) +
		"/closing=" + strconv.FormatBool(receipt.runtimeClosureInProgress) +
		"/epoch=" + strconv.FormatUint(uint64(receipt.fatalEpoch), 10)
	appendRequests := func(label string, requests []campaignAdmission) {
		summary += "/" + label + "="
		for index, request := range requests {
			if index != 0 {
				summary += ","
			}
			summary += string(request.attempt) + ":" + strconv.Itoa(int(request.class))
		}
	}
	appendRequests("cancelled", receipt.cancelledWaiting)
	appendRequests("compensated", receipt.compensatedGrants)

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

func resolveBaselineMutationDeadline(baseline time.Duration, peers int) mutationDeadlineResolution {
	return mutationDeadlineResolution{duration: resolveMutationDeadline(baseline, peers)}
}

func recordedMutationDeadline(deadline time.Duration) mutationDeadlineResolution {
	if deadline <= 0 {
		campaignInvariant("record mutation deadline", "recorded deadline must be positive")
	}

	return mutationDeadlineResolution{duration: deadline}
}
