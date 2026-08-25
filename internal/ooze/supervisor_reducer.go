package ooze

import (
	"slices"
	"time"
)

const supervisorReducerOperation = "reduce supervisor"

type supervisorActionToken uint64

type supervisorLaunchCompletionKind uint8

const (
	supervisorLaunchProvenNotReleased supervisorLaunchCompletionKind = iota + 1
	supervisorLaunchReleased
	supervisorLaunchReleaseUnconfirmed
)

type supervisorAttemptPhase uint8

const (
	supervisorLaunchEstablishing supervisorAttemptPhase = iota + 1
	supervisorLaunchReportedUnconfirmed
	supervisorLaunchClosedNotReleased
	supervisorLaunchOwned
	supervisorRunning
	supervisorIntentLatched
	supervisorEmergencyDraining
	supervisorCapturingOutput
	supervisorSealingStopAdmission
	supervisorReleasingDomain
	supervisorTransferringResidualCustody
	supervisorSettlingRuntime
	supervisorAwaitingEmergencySettlement
)

type supervisorEventKind uint8

const (
	supervisorProspectiveRegistered supervisorEventKind = iota + 1
	supervisorLaunchCompleted
	supervisorLaunchBoundary
	supervisorEmergencyStarted
	supervisorRunningObserved
	supervisorDrainCompleted
	supervisorOutputCompleted
	supervisorStopAdmissionSealed
	supervisorReleaseCompleted
	supervisorRuntimeCompleted
	supervisorEmergencySettlementCompleted
)

type supervisorActionKind uint8

const (
	supervisorLaunchNative supervisorActionKind = iota + 1
	supervisorPublishNotReleased
	supervisorPublishOwned
	supervisorRevokeLaunchRelease
	supervisorPublishLaunchUnconfirmed
	supervisorCloseProspective
	supervisorAdoptOwned
	supervisorForceOwned
	supervisorWaitRoot
	supervisorSampleRunning
	supervisorObserveEmptiness
	supervisorCaptureOutput
	supervisorSealStopAdmission
	supervisorReleaseDomain
	supervisorTransferResidualCustody
	supervisorSettleRuntime
	supervisorDeliverTerminal
	supervisorSettleEmergency
	supervisorDeliverEmergencySettlement
)

type supervisorEmergencyResolutionKind uint8

const (
	supervisorEmergencyConfirmedDrained supervisorEmergencyResolutionKind = iota + 1
	supervisorEmergencyResidualOwned
)

type supervisorResidualKind uint8

const supervisorResidualOwned supervisorResidualKind = iota + 1

type supervisorRuntimeReceiptKind uint8

const (
	supervisorRuntimeAcknowledged supervisorRuntimeReceiptKind = iota + 1
	supervisorRuntimeProvisionalDeadline
	supervisorRuntimeClosurePending
)

type supervisorTerminalKind uint8

const (
	supervisorTerminalSettled supervisorTerminalKind = iota + 1
	supervisorTerminalFuseTrip
	supervisorTerminalAutomaticDeadlineTrip
	supervisorTerminalSerialDeadlineTrip
	supervisorTerminalStopped
	supervisorTerminalInfrastructureWait
	supervisorTerminalInfrastructureRunning
	supervisorTerminalInfrastructureRelease
	supervisorTerminalInfrastructureOutput
	supervisorTerminalInfrastructureControl
	supervisorTerminalDrainUnconfirmed
)

type supervisorFiredBound uint8

const (
	supervisorNoCommandBound supervisorFiredBound = iota
	supervisorCommandDeadlineFired
)

type supervisorDrainCompletionKind uint8

const (
	supervisorDrainForceCompleted supervisorDrainCompletionKind = iota + 1
	supervisorDrainObservedEmpty
	supervisorDrainObservedResidual
	supervisorDrainObservationFailed
)

type supervisorRunningFactKind uint8

const (
	supervisorRunningFuseObserved supervisorRunningFactKind = iota + 1
	supervisorRunningRootExited
	supervisorRunningObservationFailed
	supervisorRunningStopRequested
)

type supervisorRunningIntentKind uint8

const (
	supervisorIntentFuse supervisorRunningIntentKind = iota + 1
	supervisorIntentRootExit
	supervisorIntentObservationFailure
	supervisorIntentDeadline
	supervisorIntentStop
	supervisorIntentRuntimeEmergency
)

type supervisorObservationSource uint8

const (
	supervisorObservationWait supervisorObservationSource = iota + 1
	supervisorObservationRunning
)

type supervisorDiagnosticRef uint64

type supervisorOutputRef uint64

type supervisorObservationDiagnostics struct {
	wait    supervisorDiagnosticRef
	running supervisorDiagnosticRef
}

type supervisorObservedCount struct {
	present bool
	value   int
}

type supervisorPendingAction struct {
	kind  supervisorActionKind
	token supervisorActionToken
}

type supervisorDrainState struct {
	effectiveDrainBy      time.Time
	forced                bool
	decision              supervisorDrainDecision
	waitDiagnostic        supervisorDiagnosticRef
	controlDiagnostic     supervisorDiagnosticRef
	observationDiagnostic supervisorDiagnosticRef
}

type supervisorDrainDecision uint8

const (
	supervisorDrainProvenEmpty supervisorDrainDecision = iota + 1
	supervisorDrainUnconfirmed
)

type supervisorExitRecheck struct {
	performed bool
	observed  bool
	at        time.Time
	code      int
	signal    int
	action    supervisorActionToken
}

type supervisorRunningFact struct {
	generation   attemptGeneration
	action       supervisorActionToken
	kind         supervisorRunningFactKind
	at           time.Time
	stop         StopRequest
	rootLive     bool
	live         uint64
	liveNegative bool
	exitCode     int
	exitSignal   int
	source       supervisorObservationSource
	diagnostic   supervisorDiagnosticRef
}

type supervisorRunningBundle struct {
	generation   attemptGeneration
	sampleAction supervisorActionToken
	waitAction   supervisorActionToken
	facts        []supervisorRunningFact
	exitRecheck  supervisorExitRecheck
	drainBy      time.Time
}

type supervisorRunningIntent struct {
	latched           bool
	kind              supervisorRunningIntentKind
	at                time.Time
	drainBy           time.Time
	duration          time.Duration
	count             supervisorObservedCount
	stop              StopRequest
	exitCode          int
	exitSignal        int
	observationSource supervisorObservationSource
	diagnostics       supervisorObservationDiagnostics
}

type supervisorLaunchCompletion struct {
	generation attemptGeneration
	action     supervisorActionToken
	at         time.Time
	kind       supervisorLaunchCompletionKind
	failure    LaunchFailure
	diagnostic supervisorDiagnosticRef
}

type supervisorDrainCompletion struct {
	generation     attemptGeneration
	action         supervisorPendingAction
	at             time.Time
	kind           supervisorDrainCompletionKind
	waitDiagnostic supervisorDiagnosticRef
	diagnostic     supervisorDiagnosticRef
}

type supervisorOutputCompletion struct {
	generation     attemptGeneration
	action         supervisorPendingAction
	at             time.Time
	ref            supervisorOutputRef
	cutoff         uint64
	prefixLength   uint64
	waitDiagnostic supervisorDiagnosticRef
	diagnostic     supervisorDiagnosticRef
}

type supervisorStopSealCompletion struct {
	generation attemptGeneration
	action     supervisorPendingAction
	at         time.Time
}

type supervisorReleaseCompletion struct {
	generation attemptGeneration
	action     supervisorPendingAction
	at         time.Time
	diagnostic supervisorDiagnosticRef
}

type supervisorRuntimeCompletion struct {
	generation attemptGeneration
	action     supervisorPendingAction
	kind       supervisorRuntimeReceiptKind
}

type supervisorEmergencyResolution struct {
	generation attemptGeneration
	kind       supervisorEmergencyResolutionKind
}

type supervisorEmergencyResidual struct {
	generation attemptGeneration
	attempt    attemptIdentity
	kind       supervisorResidualKind
}

type supervisorEmergencySettlementCompletion struct {
	action       supervisorPendingAction
	acknowledged []attemptGeneration
	residuals    []supervisorEmergencyResolution
}

type supervisorOutputEvidence struct {
	ref                   supervisorOutputRef
	cutoff                uint64
	prefixLength          uint64
	completeThroughCutoff bool
	final                 bool
	diagnostic            supervisorDiagnosticRef
}

type supervisorTerminalDiagnostics struct {
	wait    supervisorDiagnosticRef
	running supervisorDiagnosticRef
	drain   supervisorDiagnosticRef
	control supervisorDiagnosticRef
	release supervisorDiagnosticRef
}

type supervisorTerminalEvidence struct {
	kind            supervisorTerminalKind
	commandDeadline time.Duration
	launchDuration  time.Duration
	commandDuration time.Duration
	firedBound      supervisorFiredBound
	exitCode        int
	exitSignal      int
	count           supervisorObservedCount
	output          supervisorOutputEvidence
	diagnostics     supervisorTerminalDiagnostics
}

type supervisorEmergencySnapshot struct {
	generation attemptGeneration
	completion *supervisorLaunchCompletion
	running    *supervisorRunningBundle
}

type supervisorEmergencyEpoch struct {
	active        bool
	at            time.Time
	drainBy       time.Time
	pendingAction supervisorPendingAction
}

type supervisorAttemptState struct {
	generation        attemptGeneration
	attempt           attemptIdentity
	profile           Profile
	commandDeadline   time.Duration
	registeredAt      time.Time
	launchBy          time.Time
	lastEventAt       time.Time
	revokedAt         time.Time
	startedAt         time.Time
	deadlineAt        time.Time
	launchAction      supervisorActionToken
	waitAction        supervisorActionToken
	sampleAction      supervisorActionToken
	pendingAction     supervisorPendingAction
	phase             supervisorAttemptPhase
	releaseRevoked    bool
	releaseDiagnostic supervisorDiagnosticRef
	runningPeak       supervisorObservedCount
	intent            supervisorRunningIntent
	drain             supervisorDrainState
	output            supervisorOutputEvidence
	terminal          supervisorTerminalEvidence
}

type supervisorState struct {
	nextAction supervisorActionToken
	attempts   []supervisorAttemptState
	emergency  supervisorEmergencyEpoch
}

type supervisorEvent struct {
	kind                supervisorEventKind
	generation          attemptGeneration
	attempt             attemptIdentity
	at                  time.Time
	launchBy            time.Time
	drainBy             time.Time
	completion          *supervisorLaunchCompletion
	emergencySnapshots  []supervisorEmergencySnapshot
	profile             Profile
	commandDeadline     time.Duration
	running             *supervisorRunningBundle
	drain               *supervisorDrainCompletion
	output              *supervisorOutputCompletion
	seal                *supervisorStopSealCompletion
	release             *supervisorReleaseCompletion
	runtime             *supervisorRuntimeCompletion
	emergencySettlement *supervisorEmergencySettlementCompletion
}

type supervisorAction struct {
	kind             supervisorActionKind
	generation       attemptGeneration
	token            supervisorActionToken
	at               time.Time
	drainBy          time.Time
	launchKind       supervisorLaunchCompletionKind
	launchFailure    LaunchFailure
	launchDiagnostic supervisorDiagnosticRef
	launchDuration   time.Duration
	intent           supervisorRunningIntent
	terminal         supervisorTerminalEvidence
	runtimeKind      supervisorRuntimeReceiptKind
	resolutions      []supervisorEmergencyResolution
	residuals        []supervisorEmergencyResidual
}

//nolint:cyclop // One sealed deterministic event dispatch intentionally enumerates every supervisor event.
func reduceSupervisor(state supervisorState, event supervisorEvent) (supervisorState, []supervisorAction) {
	if event.kind != supervisorRuntimeCompleted && event.runtime != nil {
		invariant(supervisorReducerOperation, "non-runtime event carries a runtime completion")
	}
	if event.kind != supervisorEmergencySettlementCompleted && event.emergencySettlement != nil {
		invariant(supervisorReducerOperation, "non-settlement event carries an emergency settlement completion")
	}
	if state.emergency.pendingAction != (supervisorPendingAction{}) &&
		event.kind != supervisorEmergencySettlementCompleted {
		invariant(supervisorReducerOperation, "emergency settlement freezes noncompletion events")
	}
	next := cloneSupervisorState(state)
	if event.kind == supervisorEmergencySettlementCompleted {
		return reduceEmergencySettlementCompletion(next, event)
	}
	var actions []supervisorAction
	switch event.kind {
	case supervisorProspectiveRegistered:
		next, actions = reduceProspectiveRegistration(next, event)
	case supervisorLaunchCompleted:
		next, actions = reduceLaunchCompletion(next, event)
	case supervisorLaunchBoundary:
		next, actions = reduceLaunchBoundary(next, event)
	case supervisorEmergencyStarted:
		next, actions = reduceLaunchEmergency(next, event)
	case supervisorRunningObserved:
		next, actions = reduceRunningBundle(next, event)
	case supervisorDrainCompleted:
		next, actions = reduceDrainCompletion(next, event)
	case supervisorOutputCompleted:
		next, actions = reduceOutputCompletion(next, event)
	case supervisorStopAdmissionSealed:
		next, actions = reduceStopSealCompletion(next, event)
	case supervisorReleaseCompleted:
		next, actions = reduceReleaseCompletion(next, event)
	case supervisorRuntimeCompleted:
		next, actions = reduceRuntimeCompletion(next, event)
	default:
		invariant(supervisorReducerOperation, "event kind is invalid")

		return supervisorState{}, nil
	}

	return appendEmergencySettlementIfReady(next, actions)
}

func cloneSupervisorState(state supervisorState) supervisorState {
	state.attempts = append([]supervisorAttemptState(nil), state.attempts...)

	return state
}

func appendEmergencySettlementIfReady(
	state supervisorState,
	actions []supervisorAction,
) (supervisorState, []supervisorAction) {
	if !state.emergency.active || state.emergency.pendingAction != (supervisorPendingAction{}) ||
		!state.emergencySettlementInventoryReady() {
		return state, actions
	}
	action := state.newGlobalAction(supervisorSettleEmergency)
	action.resolutions = state.emergencySettlementResolutions()
	state.emergency.pendingAction = supervisorPendingAction{kind: action.kind, token: action.token}

	return state, append(actions, action)
}

func (state supervisorState) emergencySettlementInventoryReady() bool {
	for _, attempt := range state.attempts {
		if attempt.phase != supervisorLaunchClosedNotReleased &&
			attempt.phase != supervisorAwaitingEmergencySettlement {
			return false
		}
	}

	return true
}

func (state supervisorState) emergencySettlementResolutions() []supervisorEmergencyResolution {
	var resolutions []supervisorEmergencyResolution
	for _, attempt := range state.attempts {
		if attempt.phase == supervisorLaunchClosedNotReleased {
			continue
		}
		validateEmergencySettlementCustody(attempt, state.emergency)
		kind := supervisorEmergencyConfirmedDrained
		if attempt.drain.decision == supervisorDrainUnconfirmed {
			kind = supervisorEmergencyResidualOwned
		}
		resolutions = append(resolutions, supervisorEmergencyResolution{
			generation: attempt.generation,
			kind:       kind,
		})
	}

	return resolutions
}

func reduceEmergencySettlementCompletion(
	state supervisorState,
	event supervisorEvent,
) (supervisorState, []supervisorAction) {
	completion := requireEmergencySettlementCompletion(state, event)
	resolutions := state.emergencySettlementResolutions()
	validateEmergencySettlementCompletionInventory(completion, resolutions)

	actions := make([]supervisorAction, 0, len(state.attempts)+1)
	var kept []supervisorAttemptState
	var residuals []supervisorEmergencyResidual
	for index, attempt := range state.attempts {
		if attempt.phase == supervisorLaunchClosedNotReleased {
			kept = append(kept, attempt)

			continue
		}
		if attempt.drain.decision == supervisorDrainProvenEmpty &&
			attempt.terminal != (supervisorTerminalEvidence{}) {
			action := state.newAction(supervisorDeliverTerminal, index, time.Time{}, time.Time{}, nil)
			action.terminal = attempt.terminal
			action.runtimeKind = supervisorRuntimeClosurePending
			actions = append(actions, action)
		}
		if attempt.drain.decision == supervisorDrainUnconfirmed {
			residuals = append(residuals, supervisorEmergencyResidual{
				generation: attempt.generation,
				attempt:    attempt.attempt,
				kind:       supervisorResidualOwned,
			})
		}
	}
	delivery := state.newGlobalAction(supervisorDeliverEmergencySettlement)
	delivery.residuals = residuals
	actions = append(actions, delivery)
	state.attempts = kept
	state.emergency.pendingAction = supervisorPendingAction{}

	return state, actions
}

func requireEmergencySettlementCompletion(
	state supervisorState,
	event supervisorEvent,
) supervisorEmergencySettlementCompletion {
	if event.emergencySettlement == nil || !state.emergency.active ||
		state.emergency.pendingAction.kind != supervisorSettleEmergency ||
		state.emergency.pendingAction.token == 0 || !state.emergencySettlementInventoryReady() {
		invariant(supervisorReducerOperation, "emergency settlement completion is outside pending custody")
	}
	validateEmergencySettlementEventShape(event)
	completion := *event.emergencySettlement
	if completion.action != state.emergency.pendingAction {
		invariant(supervisorReducerOperation, "emergency settlement completion correlation is stale or wrong")
	}

	return completion
}

func validateEmergencySettlementEventShape(event supervisorEvent) {
	if emergencySettlementEventHasEnvelopeData(event) ||
		emergencySettlementEventHasLaunchData(event) ||
		emergencySettlementEventHasCustodyData(event) {
		invariant(supervisorReducerOperation, "emergency settlement completion event shape is invalid")
	}
}

func emergencySettlementEventHasEnvelopeData(event supervisorEvent) bool {
	return event.kind != supervisorEmergencySettlementCompleted || event.generation != 0 ||
		!event.attemptIsZero() || !event.at.IsZero() || !event.launchBy.IsZero() || !event.drainBy.IsZero()
}

func emergencySettlementEventHasLaunchData(event supervisorEvent) bool {
	return event.completion != nil || len(event.emergencySnapshots) != 0 ||
		event.profile != 0 || event.commandDeadline != 0 || event.running != nil
}

func emergencySettlementEventHasCustodyData(event supervisorEvent) bool {
	return event.drain != nil || event.output != nil || event.seal != nil ||
		event.release != nil || event.runtime != nil
}

func validateEmergencySettlementCompletionInventory(
	completion supervisorEmergencySettlementCompletion,
	resolutions []supervisorEmergencyResolution,
) {
	acknowledged := make([]attemptGeneration, len(resolutions))
	var residuals []supervisorEmergencyResolution
	for index, resolution := range resolutions {
		acknowledged[index] = resolution.generation
		if resolution.kind == supervisorEmergencyResidualOwned {
			residuals = append(residuals, resolution)
		}
	}
	if !slices.Equal(completion.acknowledged, acknowledged) ||
		!slices.Equal(completion.residuals, residuals) {
		invariant(supervisorReducerOperation, "emergency settlement completion inventory is stale or reordered")
	}
}

func reduceProspectiveRegistration(
	state supervisorState,
	event supervisorEvent,
) (supervisorState, []supervisorAction) {
	if event.generation == 0 || event.attempt == "" || event.at.IsZero() ||
		state.emergency.active ||
		event.launchBy.IsZero() || !event.launchBy.After(event.at) ||
		!event.drainBy.IsZero() || event.completion != nil || event.drain != nil ||
		event.output != nil || event.seal != nil || event.release != nil ||
		len(event.emergencySnapshots) != 0 ||
		event.running != nil || (event.profile != AutomaticProfile && event.profile != SerialProfile) ||
		event.commandDeadline <= 0 || state.attemptIndex(event.generation) >= 0 {
		invariant(supervisorReducerOperation, "prospective registration is incomplete or duplicated")
	}
	attempt := supervisorAttemptState{
		generation:      event.generation,
		attempt:         event.attempt,
		profile:         event.profile,
		commandDeadline: event.commandDeadline,
		registeredAt:    event.at,
		launchBy:        event.launchBy,
		lastEventAt:     event.at,
		phase:           supervisorLaunchEstablishing,
	}
	index := len(state.attempts)
	for candidate := range state.attempts {
		if state.attempts[candidate].generation > event.generation {
			index = candidate

			break
		}
	}
	state.attempts = slices.Insert(state.attempts, index, attempt)
	action := state.newAction(supervisorLaunchNative, index, event.at, time.Time{}, nil)
	state.attempts[index].launchAction = action.token

	return state, []supervisorAction{action}
}

func reduceLaunchCompletion(
	state supervisorState,
	event supervisorEvent,
) (supervisorState, []supervisorAction) {
	index := state.requireAttempt(event.generation)
	attempt := state.attempts[index]
	completion := requireLaunchCompletion(attempt, event.completion)
	if event.at.IsZero() || !event.at.Equal(completion.at) || !event.attemptIsZero() ||
		!event.launchBy.IsZero() || event.drain != nil || event.output != nil || event.seal != nil ||
		event.release != nil ||
		len(event.emergencySnapshots) != 0 || event.profile != 0 || event.commandDeadline != 0 ||
		event.running != nil || event.at.Before(attempt.lastEventAt) {
		invariant(supervisorReducerOperation, "launch completion event is malformed or moved backward")
	}

	switch attempt.phase {
	case supervisorLaunchEstablishing:
		releaseUnknown := completion.kind == supervisorLaunchReleaseUnconfirmed
		if !event.at.Before(attempt.launchBy) ||
			(releaseUnknown && !event.drainBy.After(event.at)) ||
			(!releaseUnknown && !event.drainBy.IsZero()) {
			invariant(supervisorReducerOperation, "at-bound or late completion bypassed boundary snapshot")
		}
		state.attempts[index].lastEventAt = event.at

		return state.completeLaunch(index, completion, event.at, event.drainBy)
	case supervisorLaunchReportedUnconfirmed:
		if event.at.Before(attempt.revokedAt) {
			invariant(supervisorReducerOperation, "late completion predates release revocation")
		}
		state.attempts[index].lastEventAt = event.at
		actionAt := event.at
		drainBy := event.drainBy
		if state.emergency.active {
			if !event.drainBy.IsZero() {
				invariant(supervisorReducerOperation, "late emergency launch completion supplied a second drain bound")
			}
			actionAt = state.emergency.at
			drainBy = state.emergency.drainBy
		} else if completion.kind == supervisorLaunchReleased ||
			completion.kind == supervisorLaunchReleaseUnconfirmed {
			if !event.drainBy.After(event.at) {
				invariant(supervisorReducerOperation, "late release lacks a positive local drain bound")
			}
		} else if !event.drainBy.IsZero() {
			invariant(supervisorReducerOperation, "not-released launch completion supplied a drain bound")
		}

		return state.completeLaunch(index, completion, actionAt, drainBy)
	default:
		invariant(supervisorReducerOperation, "launch completion was duplicated after closure")

		return supervisorState{}, nil
	}
}

func reduceLaunchBoundary(
	state supervisorState,
	event supervisorEvent,
) (supervisorState, []supervisorAction) {
	index := state.requireAttempt(event.generation)
	attempt := state.attempts[index]
	if attempt.phase != supervisorLaunchEstablishing || !event.at.Equal(attempt.launchBy) ||
		!event.attemptIsZero() || !event.launchBy.IsZero() ||
		len(event.emergencySnapshots) != 0 || event.profile != 0 || event.commandDeadline != 0 ||
		event.running != nil || event.drain != nil || event.output != nil || event.seal != nil ||
		event.release != nil ||
		event.at.Before(attempt.lastEventAt) {
		invariant(supervisorReducerOperation, "launch boundary is invalid or duplicated")
	}
	if event.completion != nil {
		completion := requireLaunchCompletion(attempt, event.completion)
		releaseUnknown := completion.kind == supervisorLaunchReleaseUnconfirmed
		if (releaseUnknown && !event.drainBy.After(completion.at)) ||
			(!releaseUnknown && !event.drainBy.IsZero()) {
			invariant(supervisorReducerOperation, "boundary completion carries an invalid drain bound")
		}
		if completion.at.After(event.at) || completion.at.Before(attempt.lastEventAt) {
			invariant(supervisorReducerOperation, "boundary snapshot is outside its serialized interval")
		}
		state.attempts[index].lastEventAt = event.at

		return state.completeLaunch(index, completion, completion.at, event.drainBy)
	}
	if !event.drainBy.IsZero() {
		invariant(supervisorReducerOperation, "empty launch boundary carries a drain bound")
	}

	return state.revokeProspective(index, event.at, time.Time{})
}

func reduceLaunchEmergency(
	state supervisorState,
	event supervisorEvent,
) (supervisorState, []supervisorAction) {
	if state.emergency.active || event.generation != 0 || !event.attemptIsZero() ||
		event.at.IsZero() || !event.launchBy.IsZero() || !event.drainBy.After(event.at) ||
		event.completion != nil || event.profile != 0 || event.commandDeadline != 0 ||
		event.running != nil || event.drain != nil || event.output != nil || event.seal != nil ||
		event.release != nil {
		invariant(supervisorReducerOperation, "emergency epoch is invalid, duplicated, or conflicting")
	}
	state.emergency = supervisorEmergencyEpoch{active: true, at: event.at, drainBy: event.drainBy}
	actions := make([]supervisorAction, 0, len(event.emergencySnapshots)*2)
	snapshotIndex := 0
	for index := range state.attempts {
		attempt := state.attempts[index]
		if attempt.phase == supervisorLaunchClosedNotReleased {
			continue
		}
		if snapshotIndex >= len(event.emergencySnapshots) ||
			event.emergencySnapshots[snapshotIndex].generation != attempt.generation {
			invariant(supervisorReducerOperation, "emergency snapshot set is incomplete or out of order")
		}
		snapshot := event.emergencySnapshots[snapshotIndex]
		snapshotIndex++
		switch attempt.phase {
		case supervisorLaunchEstablishing:
			if event.at.After(attempt.launchBy) || event.at.Before(attempt.lastEventAt) {
				invariant(supervisorReducerOperation, "emergency launch snapshot is outside its interval")
			}
			if snapshot.completion == nil {
				if snapshot.running != nil {
					invariant(supervisorReducerOperation, "unresolved emergency launch snapshot contains running facts")
				}
				var emitted []supervisorAction
				state, emitted = state.revokeProspective(index, event.at, event.drainBy)
				actions = append(actions, emitted...)

				continue
			}
			completion := requireLaunchCompletion(attempt, snapshot.completion)
			if completion.at.After(event.at) || completion.at.Before(attempt.lastEventAt) {
				invariant(supervisorReducerOperation, "emergency completion is outside its serialized interval")
			}
			if (completion.kind == supervisorLaunchReleased) != (snapshot.running != nil) {
				invariant(supervisorReducerOperation, "emergency launch completion lacks its matching running snapshot")
			}
			state.attempts[index].lastEventAt = event.at
			var emitted []supervisorAction
			if completion.kind == supervisorLaunchReleased {
				state, emitted = state.completeEmergencyReleasedLaunch(
					index, completion, event.at, event.drainBy, *snapshot.running,
				)
			} else {
				state, emitted = state.completeLaunch(index, completion, event.at, event.drainBy)
			}
			actions = append(actions, emitted...)
		case supervisorLaunchReportedUnconfirmed:
			if snapshot.running != nil {
				invariant(supervisorReducerOperation, "prospective emergency snapshot contains running facts")
			}
			if event.at.Before(attempt.lastEventAt) {
				invariant(supervisorReducerOperation, "prospective emergency snapshot moved backward")
			}
			state.attempts[index].lastEventAt = event.at
			if snapshot.completion == nil {
				continue
			}
			completion := requireLaunchCompletion(attempt, snapshot.completion)
			if completion.at.After(event.at) || completion.at.Before(attempt.revokedAt) {
				invariant(supervisorReducerOperation, "emergency late completion is outside its interval")
			}
			var emitted []supervisorAction
			state, emitted = state.completeLaunch(index, completion, event.at, event.drainBy)
			actions = append(actions, emitted...)
		case supervisorLaunchOwned:
			if snapshot.completion != nil || snapshot.running != nil || event.at.Before(attempt.lastEventAt) {
				invariant(supervisorReducerOperation, "owned emergency snapshot contains launch completion")
			}
			state.attempts[index].phase = supervisorEmergencyDraining
			if attempt.pendingAction.token == 0 {
				action := state.newAction(supervisorForceOwned, index, event.at, event.drainBy, nil)
				state.attempts[index].pendingAction = supervisorPendingAction{kind: action.kind, token: action.token}
				state.attempts[index].drain = supervisorDrainState{
					effectiveDrainBy: event.drainBy,
					forced:           true,
				}
				actions = append(actions, action)
			} else {
				state.clampDrain(index, event.drainBy)
			}
			state.attempts[index].lastEventAt = event.at
		case supervisorRunning:
			if snapshot.completion != nil || snapshot.running == nil || event.at.Before(attempt.lastEventAt) {
				invariant(supervisorReducerOperation, "running emergency snapshot is incomplete")
			}
			var effectiveDrainBy time.Time
			state, effectiveDrainBy = reduceEmergencyRunningSnapshot(
				state, index, event.at, event.drainBy, *snapshot.running,
			)
			action := state.newAction(supervisorForceOwned, index, event.at, event.drainBy, nil)
			action.intent = state.attempts[index].intent
			state.attempts[index].pendingAction = supervisorPendingAction{kind: action.kind, token: action.token}
			state.attempts[index].drain = supervisorDrainState{
				effectiveDrainBy: effectiveDrainBy,
				forced:           true,
			}
			action.drainBy = state.attempts[index].drain.effectiveDrainBy
			actions = append(actions, action)
		case supervisorIntentLatched:
			if snapshot.completion != nil || snapshot.running == nil {
				invariant(supervisorReducerOperation, "latched emergency snapshot is incomplete")
			}
			validateRunningBundleCorrelation(attempt, snapshot.running)
			validateSealedRunningBundle(attempt, event.at, *snapshot.running)
			state.attempts[index].phase = supervisorEmergencyDraining
			if attempt.pendingAction.token == 0 || attempt.pendingAction.kind == 0 {
				invariant(supervisorReducerOperation, "latched intent has no correlated drain action")
			}
			state.clampDrain(index, event.drainBy)
			state.attempts[index].lastEventAt = event.at
		case supervisorCapturingOutput:
			if snapshot.completion != nil || snapshot.running != nil || event.at.Before(attempt.lastEventAt) ||
				attempt.pendingAction.kind != supervisorCaptureOutput || attempt.pendingAction.token == 0 ||
				(attempt.drain.decision != supervisorDrainProvenEmpty &&
					attempt.drain.decision != supervisorDrainUnconfirmed) {
				invariant(supervisorReducerOperation, "capture emergency snapshot or pending action is invalid")
			}
			state.clampDrain(index, event.drainBy)
			state.attempts[index].lastEventAt = event.at
		case supervisorSealingStopAdmission, supervisorReleasingDomain:
			wantPending := supervisorSealStopAdmission
			if attempt.phase == supervisorReleasingDomain {
				wantPending = supervisorReleaseDomain
			}
			branchOwnsDecision := attempt.phase == supervisorSealingStopAdmission ||
				(attempt.phase == supervisorReleasingDomain &&
					attempt.drain.decision == supervisorDrainProvenEmpty)
			if snapshot.completion != nil || snapshot.running != nil || event.at.Before(attempt.lastEventAt) ||
				attempt.pendingAction.kind != wantPending || attempt.pendingAction.token == 0 ||
				attempt.output.ref == 0 || !branchOwnsDecision ||
				attempt.output.final != (attempt.drain.decision == supervisorDrainProvenEmpty) ||
				(attempt.drain.decision != supervisorDrainProvenEmpty &&
					attempt.drain.decision != supervisorDrainUnconfirmed) {
				invariant(supervisorReducerOperation, "output pipeline emergency snapshot is invalid")
			}
			state.clampDrain(index, event.drainBy)
			state.attempts[index].lastEventAt = event.at
		case supervisorTransferringResidualCustody:
			if snapshot.completion != nil || snapshot.running != nil || event.at.Before(attempt.lastEventAt) {
				invariant(supervisorReducerOperation, "residual-transfer emergency snapshot is invalid")
			}
			validateRuntimeTransferCustody(attempt, state.emergency)
			state.clampDrain(index, event.drainBy)
			state.attempts[index].lastEventAt = event.at
		case supervisorAwaitingEmergencySettlement:
			validateAwaitingEmergencySettlement(attempt, snapshot, event.at, state.emergency)
			state.attempts[index].lastEventAt = event.at
		case supervisorSettlingRuntime:
			if snapshot.completion != nil || snapshot.running != nil ||
				event.at.Before(attempt.lastEventAt) ||
				attempt.pendingAction.kind != supervisorSettleRuntime ||
				attempt.pendingAction.token == 0 {
				invariant(supervisorReducerOperation, "runtime-settlement emergency snapshot is invalid")
			}
			validateNormalizedTerminalCustody(attempt, state.emergency)
			state.attempts[index].lastEventAt = event.at
		default:
			invariant(supervisorReducerOperation, "emergency encountered an invalid attempt phase")
		}
	}
	if snapshotIndex != len(event.emergencySnapshots) {
		invariant(supervisorReducerOperation, "emergency snapshot set contains an unknown or closed attempt")
	}

	return state, actions
}

func validateAwaitingEmergencySettlement(
	attempt supervisorAttemptState,
	snapshot supervisorEmergencySnapshot,
	at time.Time,
	emergency supervisorEmergencyEpoch,
) {
	if snapshot.completion != nil || snapshot.running != nil || at.Before(attempt.lastEventAt) {
		invariant(supervisorReducerOperation, "awaiting-settlement emergency snapshot is invalid")
	}
	validateEmergencySettlementCustody(attempt, emergency)
}

func validateEmergencySettlementCustody(
	attempt supervisorAttemptState,
	emergency supervisorEmergencyEpoch,
) {
	if attempt.phase != supervisorAwaitingEmergencySettlement ||
		attempt.pendingAction != (supervisorPendingAction{}) {
		invariant(supervisorReducerOperation, "emergency settlement attempt custody is invalid")
	}
	switch attempt.drain.decision {
	case supervisorDrainProvenEmpty:
		if attempt.terminal == (supervisorTerminalEvidence{}) {
			validateLateAdoptedProvenEmptyCustody(attempt)
		} else {
			validateNormalizedTerminalCustody(attempt, emergency)
		}
	case supervisorDrainUnconfirmed:
		validateUnconfirmedResidualCustody(attempt, emergency)
	default:
		invariant(supervisorReducerOperation, "awaiting-settlement drain decision is invalid")
	}
}

func validateLateAdoptedProvenEmptyCustody(attempt supervisorAttemptState) {
	validateLateAdoptedSettlementIdentity(attempt)
	if attempt.drain.decision != supervisorDrainProvenEmpty {
		invariant(supervisorReducerOperation, "settled late-adoption emergency snapshot is invalid")
	}
	validateOutputCustody(attempt.output, true)
}

func validateNormalizedTerminalCustody(
	attempt supervisorAttemptState,
	emergency supervisorEmergencyEpoch,
) {
	if !attempt.intent.latched || attempt.releaseRevoked || attempt.startedAt.IsZero() ||
		attempt.deadlineAt.IsZero() || attempt.drain.decision != supervisorDrainProvenEmpty {
		invariant(supervisorReducerOperation, "normalized terminal custody is invalid")
	}
	validateOutputCustody(attempt.output, true)
	if !attempt.drain.forced &&
		(attempt.drain.waitDiagnostic != 0 || attempt.drain.controlDiagnostic != 0 ||
			attempt.drain.observationDiagnostic != 0) {
		invariant(supervisorReducerOperation, "normalized terminal drain diagnostics are invalid")
	}
	validateTerminalReleaseProvenance(attempt, emergency)
	if attempt.terminal != normalizeTerminalEvidence(attempt) {
		invariant(supervisorReducerOperation, "normalized terminal evidence is invalid")
	}
}

type supervisorIntentCandidate struct {
	kind              supervisorRunningIntentKind
	at                time.Time
	count             supervisorObservedCount
	stop              StopRequest
	exitCode          int
	exitSignal        int
	observationSource supervisorObservationSource
	diagnostic        supervisorDiagnosticRef
}

func reduceRunningBundle(
	state supervisorState,
	event supervisorEvent,
) (supervisorState, []supervisorAction) {
	index := state.requireAttempt(event.generation)
	attempt := state.attempts[index]
	if event.running == nil || event.at.IsZero() || event.drainBy.IsZero() ||
		!event.attemptIsZero() || !event.launchBy.IsZero() || event.completion != nil ||
		event.drain != nil || event.output != nil || event.seal != nil || event.release != nil ||
		len(event.emergencySnapshots) != 0 || event.profile != 0 || event.commandDeadline != 0 ||
		!event.running.drainBy.IsZero() {
		invariant(supervisorReducerOperation, "running bundle shape or action correlation is invalid")
	}
	if event.at.Before(attempt.startedAt) {
		invariant(supervisorReducerOperation, "running bundle is outside the active running interval")
	}
	if event.at.Before(attempt.lastEventAt) {
		event.at = attempt.lastEventAt
	}
	validateRunningBundleCorrelation(attempt, event.running)
	if attempt.phase == supervisorIntentLatched || attempt.phase == supervisorEmergencyDraining {
		validateSealedRunningBundle(attempt, event.at, *event.running)

		return state, nil
	}
	if attempt.phase != supervisorRunning {
		invariant(supervisorReducerOperation, "running bundle is outside the active running interval")
	}
	state, selected := reduceRunningSnapshot(state, index, event.at, event.drainBy, *event.running)
	if !selected {
		return state, nil
	}
	intent := state.attempts[index].intent
	actionKind := supervisorForceOwned
	if intent.kind == supervisorIntentRootExit {
		actionKind = supervisorObserveEmptiness
	}
	action := state.newAction(actionKind, index, intent.at, intent.drainBy, nil)
	action.intent = intent
	state.attempts[index].pendingAction = supervisorPendingAction{kind: action.kind, token: action.token}
	state.attempts[index].drain = supervisorDrainState{
		effectiveDrainBy: action.drainBy,
		forced:           action.kind == supervisorForceOwned,
	}

	return state, []supervisorAction{action}
}

func reduceDrainCompletion(
	state supervisorState,
	event supervisorEvent,
) (supervisorState, []supervisorAction) {
	index := state.requireAttempt(event.generation)
	attempt := state.attempts[index]
	completion := event.drain
	if completion == nil || completion.generation != attempt.generation ||
		completion.action != attempt.pendingAction || completion.action.token == 0 ||
		completion.at.IsZero() || !event.at.Equal(completion.at) || event.at.Before(attempt.lastEventAt) ||
		!event.attemptIsZero() || !event.launchBy.IsZero() || !event.drainBy.IsZero() ||
		event.completion != nil || event.output != nil || event.seal != nil || event.release != nil ||
		len(event.emergencySnapshots) != 0 || event.profile != 0 ||
		event.commandDeadline != 0 || event.running != nil {
		invariant(supervisorReducerOperation, "drain completion correlation or shape is invalid")
	}
	if attempt.phase != supervisorIntentLatched && attempt.phase != supervisorEmergencyDraining &&
		attempt.phase != supervisorLaunchOwned {
		invariant(supervisorReducerOperation, "drain completion is outside owned drainage")
	}
	if attempt.drain.effectiveDrainBy.IsZero() || attempt.drain.decision != 0 {
		invariant(supervisorReducerOperation, "drain completion follows a settled drain")
	}

	switch completion.action.kind {
	case supervisorForceOwned:
		if completion.kind != supervisorDrainForceCompleted || !attempt.drain.forced {
			invariant(supervisorReducerOperation, "force completion does not match the pending action")
		}
	case supervisorObserveEmptiness:
		switch completion.kind {
		case supervisorDrainObservedEmpty, supervisorDrainObservedResidual:
			if completion.diagnostic != 0 {
				invariant(supervisorReducerOperation, "authoritative observation carries a diagnostic")
			}
		case supervisorDrainObservationFailed:
			if completion.diagnostic == 0 {
				invariant(supervisorReducerOperation, "failed observation lacks a diagnostic")
			}
		default:
			invariant(supervisorReducerOperation, "observation completion does not match the pending action")
		}
	default:
		invariant(supervisorReducerOperation, "pending native action is not a drain completion")
	}
	if completion.waitDiagnostic != 0 && completion.waitDiagnostic == completion.diagnostic {
		invariant(supervisorReducerOperation, "drain completion aliases independent diagnostics")
	}

	state.attempts[index].pendingAction = supervisorPendingAction{}
	state.attempts[index].lastEventAt = completion.at
	reconcileDrainWaitDiagnostic(&state.attempts[index], completion.waitDiagnostic)
	if completion.kind == supervisorDrainForceCompleted {
		if completion.diagnostic != 0 {
			state.attempts[index].drain.controlDiagnostic = completion.diagnostic
		}
		if !completion.at.Before(state.attempts[index].drain.effectiveDrainBy) {
			return state.captureDrain(index, completion.at, false)
		}

		return state.issueDrainAction(index, supervisorObserveEmptiness, completion.at)
	}

	if completion.kind == supervisorDrainObservationFailed {
		state.attempts[index].drain.observationDiagnostic = completion.diagnostic
	}
	if !state.attempts[index].drain.forced && completion.kind != supervisorDrainObservedEmpty {
		state.attempts[index].drain.forced = true

		return state.issueDrainAction(index, supervisorForceOwned, completion.at)
	}
	if !completion.at.Before(state.attempts[index].drain.effectiveDrainBy) {
		return state.captureDrain(index, completion.at, false)
	}
	if completion.kind == supervisorDrainObservedEmpty {
		return state.captureDrain(index, completion.at, true)
	}

	return state.issueDrainAction(index, supervisorObserveEmptiness, completion.at)
}

func reduceOutputCompletion(
	state supervisorState,
	event supervisorEvent,
) (supervisorState, []supervisorAction) {
	index := state.requireAttempt(event.generation)
	attempt := state.attempts[index]
	completion := event.output
	if completion == nil || completion.generation != attempt.generation ||
		completion.action != attempt.pendingAction ||
		completion.action.kind != supervisorCaptureOutput || completion.action.token == 0 ||
		completion.at.IsZero() || !event.at.Equal(completion.at) || event.at.Before(attempt.lastEventAt) ||
		completion.ref == 0 || completion.prefixLength > completion.cutoff ||
		(completion.diagnostic == 0 && completion.prefixLength != completion.cutoff) ||
		!event.attemptIsZero() || !event.launchBy.IsZero() || !event.drainBy.IsZero() ||
		event.completion != nil || event.drain != nil || event.seal != nil || event.release != nil ||
		len(event.emergencySnapshots) != 0 || event.profile != 0 ||
		event.commandDeadline != 0 || event.running != nil {
		invariant(supervisorReducerOperation, "output completion correlation, evidence, or shape is invalid")
	}
	if attempt.phase != supervisorCapturingOutput ||
		(attempt.drain.decision != supervisorDrainProvenEmpty &&
			attempt.drain.decision != supervisorDrainUnconfirmed) ||
		attempt.output != (supervisorOutputEvidence{}) {
		invariant(supervisorReducerOperation, "output completion is outside immutable capture")
	}
	if completion.waitDiagnostic != 0 && completion.waitDiagnostic == completion.diagnostic {
		invariant(supervisorReducerOperation, "output completion aliases independent diagnostics")
	}

	state.attempts[index].pendingAction = supervisorPendingAction{}
	state.attempts[index].lastEventAt = completion.at
	reconcileDrainWaitDiagnostic(&state.attempts[index], completion.waitDiagnostic)
	state.attempts[index].output = supervisorOutputEvidence{
		ref:                   completion.ref,
		cutoff:                completion.cutoff,
		prefixLength:          completion.prefixLength,
		completeThroughCutoff: completion.diagnostic == 0,
		final:                 attempt.drain.decision == supervisorDrainProvenEmpty,
		diagnostic:            completion.diagnostic,
	}
	state.attempts[index].phase = supervisorSealingStopAdmission
	action := state.newAction(
		supervisorSealStopAdmission,
		index,
		completion.at,
		state.attempts[index].drain.effectiveDrainBy,
		nil,
	)
	state.attempts[index].pendingAction = supervisorPendingAction{kind: action.kind, token: action.token}

	return state, []supervisorAction{action}
}

func reconcileDrainWaitDiagnostic(
	attempt *supervisorAttemptState,
	diagnostic supervisorDiagnosticRef,
) {
	if diagnostic == 0 || attempt.intent.diagnostics.wait != 0 || attempt.drain.waitDiagnostic != 0 {
		return
	}
	attempt.drain.waitDiagnostic = diagnostic
}

func reduceStopSealCompletion(
	state supervisorState,
	event supervisorEvent,
) (supervisorState, []supervisorAction) {
	index := state.requireAttempt(event.generation)
	attempt := state.attempts[index]
	completion := event.seal
	if completion == nil || completion.generation != attempt.generation ||
		completion.action != attempt.pendingAction ||
		completion.action.kind != supervisorSealStopAdmission || completion.action.token == 0 ||
		completion.at.IsZero() || !event.at.Equal(completion.at) || event.at.Before(attempt.lastEventAt) ||
		!event.attemptIsZero() || !event.launchBy.IsZero() || !event.drainBy.IsZero() ||
		event.completion != nil || event.drain != nil || event.output != nil || event.release != nil ||
		len(event.emergencySnapshots) != 0 || event.profile != 0 ||
		event.commandDeadline != 0 || event.running != nil {
		invariant(supervisorReducerOperation, "stop-admission seal completion correlation or shape is invalid")
	}
	if attempt.phase != supervisorSealingStopAdmission || attempt.output.ref == 0 ||
		attempt.output.final != (attempt.drain.decision == supervisorDrainProvenEmpty) ||
		(attempt.drain.decision != supervisorDrainProvenEmpty &&
			attempt.drain.decision != supervisorDrainUnconfirmed) {
		invariant(supervisorReducerOperation, "stop-admission seal completion is outside output custody")
	}

	state.attempts[index].pendingAction = supervisorPendingAction{}
	state.attempts[index].lastEventAt = completion.at
	actionKind := supervisorTransferResidualCustody
	state.attempts[index].phase = supervisorTransferringResidualCustody
	if attempt.drain.decision == supervisorDrainProvenEmpty {
		actionKind = supervisorReleaseDomain
		state.attempts[index].phase = supervisorReleasingDomain
	}
	action := state.newAction(
		actionKind,
		index,
		completion.at,
		state.attempts[index].drain.effectiveDrainBy,
		nil,
	)
	state.attempts[index].pendingAction = supervisorPendingAction{kind: action.kind, token: action.token}

	return state, []supervisorAction{action}
}

func reduceReleaseCompletion(
	state supervisorState,
	event supervisorEvent,
) (supervisorState, []supervisorAction) {
	index := state.requireAttempt(event.generation)
	attempt := state.attempts[index]
	completion := requireReleaseCompletion(attempt, event)
	validateProvenEmptyReleaseCustody(attempt)

	state.attempts[index].pendingAction = supervisorPendingAction{}
	state.attempts[index].lastEventAt = completion.at
	state.attempts[index].releaseDiagnostic = completion.diagnostic
	if !attempt.intent.latched {
		return state.completeLateAdoptedRelease(index)
	}

	validateTerminalReleaseProvenance(attempt, state.emergency)
	evidence := normalizeTerminalEvidence(state.attempts[index])

	return state.beginRuntimeSettlement(index, completion.at, evidence)
}

func reduceRuntimeCompletion(
	state supervisorState,
	event supervisorEvent,
) (supervisorState, []supervisorAction) {
	index := state.requireAttempt(event.generation)
	attempt := state.attempts[index]
	completion := requireRuntimeCompletion(attempt, event)
	switch attempt.pendingAction.kind {
	case supervisorSettleRuntime:
		validateRuntimeSettlementCustody(attempt)
		validateNormalizedTerminalCustody(attempt, state.emergency)

		return reduceRuntimeSettlementCompletion(state, index, attempt, completion)
	case supervisorTransferResidualCustody:
		validateRuntimeTransferCustody(attempt, state.emergency)

		return reduceRuntimeTransferCompletion(state, index, attempt, completion)
	default:
		invariant(supervisorReducerOperation, "runtime completion is outside runtime custody")

		return supervisorState{}, nil
	}
}

func reduceRuntimeSettlementCompletion(
	state supervisorState,
	index int,
	attempt supervisorAttemptState,
	completion supervisorRuntimeCompletion,
) (supervisorState, []supervisorAction) {
	switch completion.kind {
	case supervisorRuntimeAcknowledged, supervisorRuntimeProvisionalDeadline:
		if completion.kind == supervisorRuntimeProvisionalDeadline &&
			attempt.terminal.kind != supervisorTerminalAutomaticDeadlineTrip {
			invariant(supervisorReducerOperation, "provisional runtime receipt lacks automatic deadline evidence")
		}
		action := state.newAction(supervisorDeliverTerminal, index, time.Time{}, time.Time{}, nil)
		action.terminal = attempt.terminal
		action.runtimeKind = completion.kind
		copy(state.attempts[index:], state.attempts[index+1:])
		state.attempts[len(state.attempts)-1] = supervisorAttemptState{}
		state.attempts = state.attempts[:len(state.attempts)-1]

		return state, []supervisorAction{action}
	case supervisorRuntimeClosurePending:
		state.attempts[index].pendingAction = supervisorPendingAction{}
		state.attempts[index].phase = supervisorAwaitingEmergencySettlement

		return state, nil
	default:
		invariant(supervisorReducerOperation, "runtime receipt kind is invalid")

		return supervisorState{}, nil
	}
}

func reduceRuntimeTransferCompletion(
	state supervisorState,
	index int,
	attempt supervisorAttemptState,
	completion supervisorRuntimeCompletion,
) (supervisorState, []supervisorAction) {
	if completion.kind != supervisorRuntimeClosurePending {
		invariant(supervisorReducerOperation, "residual custody accepts only closure-pending runtime receipt")
	}
	state.attempts[index].pendingAction = supervisorPendingAction{}
	state.attempts[index].phase = supervisorAwaitingEmergencySettlement
	if !attempt.intent.latched {
		return state, nil
	}
	evidence := normalizeDrainUnconfirmedTerminalEvidence(attempt)
	action := state.newAction(supervisorDeliverTerminal, index, time.Time{}, time.Time{}, nil)
	action.terminal = evidence
	action.runtimeKind = supervisorRuntimeClosurePending

	return state, []supervisorAction{action}
}

func requireRuntimeCompletion(
	attempt supervisorAttemptState,
	event supervisorEvent,
) supervisorRuntimeCompletion {
	if event.runtime == nil {
		invariant(supervisorReducerOperation, "runtime completion correlation or shape is invalid")
	}
	completion := *event.runtime
	validateRuntimeCompletionCorrelation(attempt, event, completion)
	validateRuntimeCompletionEventShape(event)

	return completion
}

func validateRuntimeTransferCustody(
	attempt supervisorAttemptState,
	emergency supervisorEmergencyEpoch,
) {
	if attempt.phase != supervisorTransferringResidualCustody ||
		attempt.pendingAction.kind != supervisorTransferResidualCustody ||
		attempt.pendingAction.token == 0 {
		invariant(supervisorReducerOperation, "runtime completion is outside residual-transfer custody")
	}
	validateUnconfirmedResidualCustody(attempt, emergency)
}

func validateUnconfirmedResidualCustody(
	attempt supervisorAttemptState,
	emergency supervisorEmergencyEpoch,
) {
	validateUnconfirmedResidualEvidence(attempt)
	validateUnconfirmedDrainProvenance(attempt)
	validateUnconfirmedResidualOwner(attempt, emergency)
}

func validateUnconfirmedResidualEvidence(attempt supervisorAttemptState) {
	if attempt.drain.decision != supervisorDrainUnconfirmed ||
		attempt.drain.effectiveDrainBy.IsZero() ||
		attempt.drain.effectiveDrainBy.After(attempt.lastEventAt) ||
		attempt.releaseDiagnostic != 0 || attempt.terminal != (supervisorTerminalEvidence{}) {
		invariant(supervisorReducerOperation, "unconfirmed residual custody is invalid")
	}
	validateOutputCustody(attempt.output, false)
}

func validateUnconfirmedDrainProvenance(attempt supervisorAttemptState) {
	if !attempt.drain.forced &&
		(attempt.intent.kind != supervisorIntentRootExit ||
			attempt.drain.waitDiagnostic != 0 || attempt.drain.controlDiagnostic != 0 ||
			attempt.drain.observationDiagnostic != 0) {
		invariant(supervisorReducerOperation, "unforced residual custody lacks root-exit provenance")
	}
}

func validateUnconfirmedResidualOwner(
	attempt supervisorAttemptState,
	emergency supervisorEmergencyEpoch,
) {
	if attempt.intent.latched {
		if attempt.releaseRevoked || attempt.startedAt.IsZero() || attempt.deadlineAt.IsZero() {
			invariant(supervisorReducerOperation, "caller-owned residual custody is invalid")
		}
		validateTerminalReleaseProvenance(attempt, emergency)

		return
	}
	validateLateAdoptedSettlementIdentity(attempt)
}

func validateRuntimeSettlementCustody(attempt supervisorAttemptState) {
	if attempt.phase != supervisorSettlingRuntime || attempt.terminal.kind == 0 ||
		attempt.pendingAction.kind != supervisorSettleRuntime || attempt.pendingAction.token == 0 {
		invariant(supervisorReducerOperation, "runtime completion is outside settlement custody")
	}
}

func validateRuntimeCompletionCorrelation(
	attempt supervisorAttemptState,
	event supervisorEvent,
	completion supervisorRuntimeCompletion,
) {
	wantCompletion := supervisorRuntimeCompletion{
		generation: attempt.generation,
		action:     attempt.pendingAction,
		kind:       completion.kind,
	}
	if completion != wantCompletion || event.generation != attempt.generation {
		invariant(supervisorReducerOperation, "runtime completion correlation or shape is invalid")
	}
}

func validateRuntimeCompletionEventShape(event supervisorEvent) {
	type runtimeEventShape struct {
		kind            supervisorEventKind
		generation      attemptGeneration
		attempt         attemptIdentity
		at              time.Time
		launchBy        time.Time
		drainBy         time.Time
		completion      *supervisorLaunchCompletion
		profile         Profile
		commandDeadline time.Duration
		running         *supervisorRunningBundle
		drain           *supervisorDrainCompletion
		output          *supervisorOutputCompletion
		seal            *supervisorStopSealCompletion
		release         *supervisorReleaseCompletion
	}
	shape := runtimeEventShape{
		kind: event.kind, generation: event.generation, attempt: event.attempt,
		at: event.at, launchBy: event.launchBy, drainBy: event.drainBy,
		completion: event.completion, profile: event.profile,
		commandDeadline: event.commandDeadline, running: event.running,
		drain: event.drain, output: event.output, seal: event.seal, release: event.release,
	}
	want := runtimeEventShape{kind: supervisorRuntimeCompleted, generation: event.generation}
	if shape != want || len(event.emergencySnapshots) != 0 {
		invariant(supervisorReducerOperation, "runtime completion event shape is invalid")
	}
}

func requireReleaseCompletion(
	attempt supervisorAttemptState,
	event supervisorEvent,
) supervisorReleaseCompletion {
	if event.release == nil {
		invariant(supervisorReducerOperation, "release completion correlation or shape is invalid")
	}
	completion := *event.release
	wantCompletion := supervisorReleaseCompletion{
		generation: attempt.generation,
		action:     attempt.pendingAction,
		at:         event.at,
		diagnostic: completion.diagnostic,
	}
	if completion != wantCompletion || completion.action.kind != supervisorReleaseDomain ||
		completion.action.token == 0 || completion.at.IsZero() || event.at.Before(attempt.lastEventAt) {
		invariant(supervisorReducerOperation, "release completion correlation or shape is invalid")
	}

	type releaseEventShape struct {
		kind            supervisorEventKind
		generation      attemptGeneration
		attempt         attemptIdentity
		at              time.Time
		launchBy        time.Time
		drainBy         time.Time
		completion      *supervisorLaunchCompletion
		profile         Profile
		commandDeadline time.Duration
		running         *supervisorRunningBundle
		drain           *supervisorDrainCompletion
		output          *supervisorOutputCompletion
		seal            *supervisorStopSealCompletion
	}
	shape := releaseEventShape{
		kind: event.kind, generation: event.generation, attempt: event.attempt,
		at: event.at, launchBy: event.launchBy, drainBy: event.drainBy,
		completion: event.completion, profile: event.profile,
		commandDeadline: event.commandDeadline, running: event.running,
		drain: event.drain, output: event.output, seal: event.seal,
	}
	wantShape := releaseEventShape{
		kind: supervisorReleaseCompleted, generation: attempt.generation, at: completion.at,
	}
	if shape != wantShape || len(event.emergencySnapshots) != 0 {
		invariant(supervisorReducerOperation, "release completion correlation or shape is invalid")
	}

	return completion
}

func validateProvenEmptyReleaseCustody(attempt supervisorAttemptState) {
	if attempt.phase != supervisorReleasingDomain ||
		attempt.drain.decision != supervisorDrainProvenEmpty ||
		attempt.terminal != (supervisorTerminalEvidence{}) {
		invariant(supervisorReducerOperation, "release completion is outside proven-empty output custody")
	}
	validateOutputCustody(attempt.output, true)
	if !attempt.drain.forced &&
		(attempt.drain.waitDiagnostic != 0 || attempt.drain.controlDiagnostic != 0 ||
			attempt.drain.observationDiagnostic != 0) {
		invariant(supervisorReducerOperation, "unforced drain carries impossible diagnostics")
	}
}

func validateOutputCustody(output supervisorOutputEvidence, final bool) {
	if output.ref == 0 || output.final != final || output.prefixLength > output.cutoff ||
		output.completeThroughCutoff != (output.diagnostic == 0) ||
		(output.diagnostic == 0 && output.prefixLength != output.cutoff) {
		invariant(supervisorReducerOperation, "output evidence is outside immutable custody")
	}
}

func (state supervisorState) completeLateAdoptedRelease(
	index int,
) (supervisorState, []supervisorAction) {
	attempt := state.attempts[index]
	validateLateAdoptedSettlementIdentity(attempt)
	state.attempts[index].phase = supervisorAwaitingEmergencySettlement

	return state, nil
}

func validateLateAdoptedSettlementIdentity(attempt supervisorAttemptState) {
	if attempt.intent != (supervisorRunningIntent{}) || !attempt.releaseRevoked ||
		!attempt.startedAt.IsZero() || !attempt.deadlineAt.IsZero() {
		invariant(supervisorReducerOperation, "late adoption cannot construct ordinary terminal evidence")
	}
}

func validateTerminalReleaseProvenance(
	attempt supervisorAttemptState,
	emergency supervisorEmergencyEpoch,
) {
	validateTerminalReleaseTiming(attempt)
	wantIntent := canonicalTerminalReleaseIntent(attempt, emergency)
	if attempt.intent != wantIntent {
		invariant(supervisorReducerOperation, "release completion terminal intent shape is invalid")
	}
}

func validateTerminalReleaseTiming(attempt supervisorAttemptState) {
	validateTerminalRegistrationTiming(attempt)
	validateTerminalIntentTiming(attempt)
}

func validateTerminalRegistrationTiming(attempt supervisorAttemptState) {
	if attempt.registeredAt.IsZero() || attempt.startedAt.IsZero() ||
		attempt.startedAt.Before(attempt.registeredAt) || attempt.commandDeadline <= 0 ||
		!attempt.deadlineAt.Equal(attempt.startedAt.Add(attempt.commandDeadline)) ||
		(attempt.profile != AutomaticProfile && attempt.profile != SerialProfile) {
		invariant(supervisorReducerOperation, "release completion lacks immutable terminal provenance")
	}
}

func validateTerminalIntentTiming(attempt supervisorAttemptState) {
	if !attempt.intent.latched || attempt.intent.at.Before(attempt.startedAt) ||
		attempt.intent.at.After(attempt.deadlineAt) ||
		attempt.intent.duration != attempt.intent.at.Sub(attempt.startedAt) ||
		!attempt.intent.drainBy.After(attempt.intent.at) {
		invariant(supervisorReducerOperation, "release completion lacks immutable terminal intent timing")
	}
}

func canonicalTerminalReleaseIntent(
	attempt supervisorAttemptState,
	emergency supervisorEmergencyEpoch,
) supervisorRunningIntent {
	validateTerminalProfileDiagnostics(attempt)
	want := supervisorRunningIntent{
		latched: true, kind: attempt.intent.kind, at: attempt.intent.at,
		drainBy: attempt.intent.drainBy, duration: attempt.intent.duration,
		diagnostics: attempt.intent.diagnostics,
	}
	switch attempt.intent.kind {
	case supervisorIntentRootExit:
		want.exitCode = attempt.intent.exitCode
		want.exitSignal = attempt.intent.exitSignal
	case supervisorIntentFuse:
		validateFuseTerminalProvenance(attempt)
		want.count = attempt.intent.count
	case supervisorIntentDeadline:
		validateDeadlineTerminalProvenance(attempt)
		want.count = attempt.intent.count
	case supervisorIntentStop:
		validateStopTerminalProvenance(attempt)
		want.stop = attempt.intent.stop
	case supervisorIntentRuntimeEmergency:
		validateRuntimeEmergencyTerminalProvenance(attempt, emergency)
	case supervisorIntentObservationFailure:
		validateObservationFailureTerminalProvenance(attempt)
		want.observationSource = attempt.intent.observationSource
	default:
		invariant(supervisorReducerOperation, "release completion terminal intent kind is invalid")
	}

	return want
}

func validateTerminalProfileDiagnostics(attempt supervisorAttemptState) {
	if attempt.profile == SerialProfile && attempt.intent.diagnostics.running != 0 {
		invariant(supervisorReducerOperation, "release completion profile diagnostics are invalid")
	}
}

func validateFuseTerminalProvenance(attempt supervisorAttemptState) {
	if attempt.profile != AutomaticProfile || !attempt.intent.count.present ||
		attempt.intent.count.value <= int(supervisorFuseCeiling) {
		invariant(supervisorReducerOperation, "release completion fuse provenance is invalid")
	}
}

func validateDeadlineTerminalProvenance(attempt supervisorAttemptState) {
	if !attempt.intent.at.Equal(attempt.deadlineAt) ||
		attempt.intent.diagnostics != (supervisorObservationDiagnostics{}) {
		invariant(supervisorReducerOperation, "release completion deadline provenance is invalid")
	}
	switch attempt.profile {
	case AutomaticProfile:
		validateAutomaticDeadlineCount(attempt.intent.count, attempt.runningPeak)
	case SerialProfile:
		validateSerialDeadlineCount(attempt.intent.count, attempt.runningPeak)
	default:
		invariant(supervisorReducerOperation, "release completion deadline profile is invalid")
	}
}

func validateAutomaticDeadlineCount(count supervisorObservedCount, peak supervisorObservedCount) {
	if count != peak || !canonicalAutomaticDeadlineCount(count) {
		invariant(supervisorReducerOperation, "release completion automatic deadline count is invalid")
	}
}

func canonicalAutomaticDeadlineCount(count supervisorObservedCount) bool {
	if !count.present {
		return count.value == 0
	}

	return count.value > 0 && count.value <= int(supervisorFuseCeiling)
}

func validateSerialDeadlineCount(count supervisorObservedCount, peak supervisorObservedCount) {
	if count != (supervisorObservedCount{}) || peak != (supervisorObservedCount{}) {
		invariant(supervisorReducerOperation, "release completion serial deadline count is invalid")
	}
}

func validateStopTerminalProvenance(attempt supervisorAttemptState) {
	if attempt.intent.stop.validate() != nil || !attempt.intent.stop.At.Equal(attempt.intent.at) ||
		!attempt.intent.stop.DrainBy.Equal(attempt.intent.drainBy) ||
		!attempt.intent.at.Before(attempt.deadlineAt) ||
		attempt.intent.diagnostics != (supervisorObservationDiagnostics{}) {
		invariant(supervisorReducerOperation, "release completion stop provenance is invalid")
	}
}

func validateRuntimeEmergencyTerminalProvenance(
	attempt supervisorAttemptState,
	emergency supervisorEmergencyEpoch,
) {
	if !emergency.active || !attempt.intent.at.Equal(emergency.at) ||
		!attempt.intent.drainBy.Equal(emergency.drainBy) ||
		!attempt.intent.at.Before(attempt.deadlineAt) ||
		attempt.intent.diagnostics != (supervisorObservationDiagnostics{}) {
		invariant(supervisorReducerOperation, "release completion runtime emergency provenance is invalid")
	}
}

func validateObservationFailureTerminalProvenance(attempt supervisorAttemptState) {
	diagnostics := attempt.intent.diagnostics
	switch attempt.intent.observationSource {
	case supervisorObservationWait:
		if diagnostics.wait == 0 {
			invariant(supervisorReducerOperation, "release completion wait failure provenance is invalid")
		}
	case supervisorObservationRunning:
		if attempt.profile != AutomaticProfile || diagnostics.running == 0 || diagnostics.wait != 0 {
			invariant(supervisorReducerOperation, "release completion running failure provenance is invalid")
		}
	default:
		invariant(supervisorReducerOperation, "release completion observation source is invalid")
	}
}

func normalizeTerminalEvidence(
	attempt supervisorAttemptState,
) supervisorTerminalEvidence {
	terminalKind, firedBound, count, exitCode, exitSignal := normalizeTerminalIntent(attempt)
	switch {
	case attempt.drain.controlDiagnostic != 0:
		terminalKind = supervisorTerminalInfrastructureControl
	case attempt.output.diagnostic != 0:
		terminalKind = supervisorTerminalInfrastructureOutput
	case attempt.releaseDiagnostic != 0:
		terminalKind = supervisorTerminalInfrastructureRelease
	case attempt.drain.waitDiagnostic != 0:
		terminalKind = supervisorTerminalInfrastructureWait
	case attempt.drain.observationDiagnostic != 0:
		terminalKind = supervisorTerminalInfrastructureRunning
	}

	return supervisorTerminalEvidence{
		kind:            terminalKind,
		commandDeadline: attempt.commandDeadline,
		launchDuration:  attempt.startedAt.Sub(attempt.registeredAt),
		commandDuration: attempt.intent.duration,
		firedBound:      firedBound,
		exitCode:        exitCode,
		exitSignal:      exitSignal,
		count:           count,
		output:          attempt.output,
		diagnostics: supervisorTerminalDiagnostics{
			wait:    terminalWaitDiagnostic(attempt),
			running: attempt.intent.diagnostics.running,
			drain:   attempt.drain.observationDiagnostic,
			control: attempt.drain.controlDiagnostic,
			release: attempt.releaseDiagnostic,
		},
	}
}

func terminalWaitDiagnostic(attempt supervisorAttemptState) supervisorDiagnosticRef {
	if attempt.drain.waitDiagnostic != 0 {
		if attempt.intent.diagnostics.wait != 0 {
			invariant(supervisorReducerOperation, "terminal wait diagnostic is duplicated")
		}

		return attempt.drain.waitDiagnostic
	}

	return attempt.intent.diagnostics.wait
}

func normalizeDrainUnconfirmedTerminalEvidence(
	attempt supervisorAttemptState,
) supervisorTerminalEvidence {
	evidence := normalizeTerminalEvidence(attempt)
	evidence.kind = supervisorTerminalDrainUnconfirmed
	evidence.exitCode = 0
	evidence.exitSignal = 0
	evidence.count = supervisorObservedCount{}

	return evidence
}

func normalizeTerminalIntent(
	attempt supervisorAttemptState,
) (supervisorTerminalKind, supervisorFiredBound, supervisorObservedCount, int, int) {
	switch attempt.intent.kind {
	case supervisorIntentRootExit:
		return supervisorTerminalSettled, supervisorNoCommandBound, supervisorObservedCount{},
			attempt.intent.exitCode, attempt.intent.exitSignal
	case supervisorIntentFuse:
		return supervisorTerminalFuseTrip, supervisorNoCommandBound, attempt.intent.count, 0, 0
	case supervisorIntentDeadline:
		kind := supervisorTerminalAutomaticDeadlineTrip
		if attempt.profile == SerialProfile {
			kind = supervisorTerminalSerialDeadlineTrip
		}

		return kind, supervisorCommandDeadlineFired, attempt.intent.count, 0, 0
	case supervisorIntentStop, supervisorIntentRuntimeEmergency:
		return supervisorTerminalStopped, supervisorNoCommandBound, supervisorObservedCount{}, 0, 0
	case supervisorIntentObservationFailure:
		kind := supervisorTerminalInfrastructureWait
		if attempt.intent.observationSource == supervisorObservationRunning {
			kind = supervisorTerminalInfrastructureRunning
		}

		return kind, supervisorNoCommandBound, supervisorObservedCount{}, 0, 0
	default:
		invariant(supervisorReducerOperation, "terminal normalization intent kind is invalid")

		return 0, 0, supervisorObservedCount{}, 0, 0
	}
}

func (state supervisorState) beginRuntimeSettlement(
	index int,
	at time.Time,
	evidence supervisorTerminalEvidence,
) (supervisorState, []supervisorAction) {
	state.attempts[index].terminal = evidence
	state.attempts[index].phase = supervisorSettlingRuntime
	action := state.newAction(supervisorSettleRuntime, index, at, time.Time{}, nil)
	action.terminal = evidence
	state.attempts[index].pendingAction = supervisorPendingAction{kind: action.kind, token: action.token}

	return state, []supervisorAction{action}
}

func (state supervisorState) issueDrainAction(
	index int,
	kind supervisorActionKind,
	at time.Time,
) (supervisorState, []supervisorAction) {
	if state.attempts[index].pendingAction != (supervisorPendingAction{}) ||
		(kind != supervisorForceOwned && kind != supervisorObserveEmptiness) {
		invariant(supervisorReducerOperation, "drain action overlaps its predecessor or has an invalid kind")
	}
	action := state.newAction(kind, index, at, state.attempts[index].drain.effectiveDrainBy, nil)
	state.attempts[index].pendingAction = supervisorPendingAction{kind: action.kind, token: action.token}

	return state, []supervisorAction{action}
}

func (state supervisorState) captureDrain(
	index int,
	at time.Time,
	provenEmpty bool,
) (supervisorState, []supervisorAction) {
	if state.attempts[index].pendingAction != (supervisorPendingAction{}) ||
		state.attempts[index].drain.decision != 0 {
		invariant(supervisorReducerOperation, "drain capture overlaps or duplicates settlement")
	}
	state.attempts[index].drain.decision = supervisorDrainUnconfirmed
	if provenEmpty {
		state.attempts[index].drain.decision = supervisorDrainProvenEmpty
	}
	state.attempts[index].phase = supervisorCapturingOutput
	action := state.newAction(
		supervisorCaptureOutput,
		index,
		at,
		state.attempts[index].drain.effectiveDrainBy,
		nil,
	)
	state.attempts[index].pendingAction = supervisorPendingAction{kind: action.kind, token: action.token}

	return state, []supervisorAction{action}
}

func (state *supervisorState) clampDrain(index int, drainBy time.Time) {
	if drainBy.IsZero() || state.attempts[index].drain.effectiveDrainBy.IsZero() {
		invariant(supervisorReducerOperation, "drain clamp lacks an active absolute bound")
	}
	if drainBy.Before(state.attempts[index].drain.effectiveDrainBy) {
		state.attempts[index].drain.effectiveDrainBy = drainBy
	}
}

func earlierTime(left time.Time, right time.Time) time.Time {
	if left.IsZero() || right.IsZero() {
		invariant(supervisorReducerOperation, "cannot intersect a zero drain bound")
	}
	if right.Before(left) {
		return right
	}

	return left
}

func validateSealedRunningBundle(
	attempt supervisorAttemptState,
	through time.Time,
	bundle supervisorRunningBundle,
) {
	if through.Before(attempt.startedAt) || through.Before(attempt.lastEventAt) {
		invariant(supervisorReducerOperation, "sealed running bundle is outside the owned interval")
	}
	candidates := make([]supervisorIntentCandidate, 0, len(bundle.facts))
	for _, fact := range bundle.facts {
		candidate, accepted := validateRunningFact(attempt, through, fact)
		if accepted {
			candidates = append(candidates, candidate)
		}
	}
	validateSameInstantRunningCandidates(candidates)
	if exitRecheckZero(bundle.exitRecheck) {
		return
	}
	recheck := bundle.exitRecheck
	if !recheck.performed || recheck.at.Before(attempt.startedAt) || recheck.at.After(through) ||
		recheck.action != 0 || (!recheck.observed && (recheck.code != 0 || recheck.signal != 0)) {
		invariant(supervisorReducerOperation, "sealed running bundle carries an invalid exit recheck")
	}
}

func reduceRunningSnapshot(
	state supervisorState,
	index int,
	through time.Time,
	drainBy time.Time,
	bundle supervisorRunningBundle,
) (supervisorState, bool) {
	attempt := state.attempts[index]
	validateRunningBundleCorrelation(attempt, &bundle)
	if through.Before(attempt.startedAt) {
		invariant(supervisorReducerOperation, "running snapshot interval is invalid")
	}

	candidates := make([]supervisorIntentCandidate, 0, len(bundle.facts)+2)
	for _, fact := range bundle.facts {
		candidate, accepted := validateRunningFact(attempt, through, fact)
		if accepted {
			candidates = append(candidates, candidate)
		}
	}
	deadlineReached := !through.Before(attempt.deadlineAt)
	if deadlineReached {
		recheck := bundle.exitRecheck
		if !recheck.performed || recheck.action != 0 ||
			(recheck.observed && (recheck.at.Before(attempt.startedAt) ||
				recheck.at.After(attempt.deadlineAt))) ||
			(!recheck.observed && !recheck.at.Equal(attempt.deadlineAt)) {
			invariant(supervisorReducerOperation, "deadline boundary lacks its explicit exit recheck")
		}
		if !recheck.observed && (recheck.code != 0 || recheck.signal != 0) {
			invariant(supervisorReducerOperation, "unobserved exit recheck carries exit status")
		}
		if recheck.observed {
			candidate := supervisorIntentCandidate{
				kind: supervisorIntentRootExit, at: recheck.at,
				exitCode: recheck.code, exitSignal: recheck.signal,
			}
			corroborated := false
			for _, existing := range candidates {
				if existing.kind != supervisorIntentRootExit || !existing.at.Equal(candidate.at) {
					continue
				}
				if existing.exitCode != candidate.exitCode || existing.exitSignal != candidate.exitSignal {
					invariant(supervisorReducerOperation, "deadline recheck contradicts root completion")
				}
				corroborated = true
			}
			if !corroborated {
				candidates = append(candidates, candidate)
			}
		}
		candidates = append(candidates, supervisorIntentCandidate{
			kind: supervisorIntentDeadline, at: attempt.deadlineAt,
		})
	} else if !exitRecheckZero(bundle.exitRecheck) {
		invariant(supervisorReducerOperation, "exit recheck was supplied before the command deadline")
	}

	validateSameInstantRunningCandidates(candidates)
	state.attempts[index].lastEventAt = through
	selected, ok := chooseRunningIntent(candidates)
	peak := runningPeakThrough(attempt.runningPeak, bundle.facts, through)
	if !ok {
		state.attempts[index].runningPeak = peak

		return state, false
	}
	if drainBy.IsZero() {
		invariant(supervisorReducerOperation, "selected running intent lacks a local drain bound")
	}
	if selected.kind != supervisorIntentStop && !drainBy.After(selected.at) {
		invariant(supervisorReducerOperation, "local drain bound does not follow the selected intent")
	}
	peak = runningPeakThrough(attempt.runningPeak, bundle.facts, selected.at)
	diagnostics := diagnosticsAtSelectedInstant(candidates, selected.at)
	intent := supervisorRunningIntent{
		latched:           true,
		kind:              selected.kind,
		at:                selected.at,
		drainBy:           drainBy,
		duration:          selected.at.Sub(attempt.startedAt),
		stop:              selected.stop,
		exitCode:          selected.exitCode,
		exitSignal:        selected.exitSignal,
		observationSource: selected.observationSource,
		diagnostics:       diagnostics,
	}
	if selected.kind == supervisorIntentFuse {
		intent.count = selected.count
	}
	if selected.kind == supervisorIntentDeadline && attempt.profile == AutomaticProfile {
		intent.count = peak
	}
	if selected.kind == supervisorIntentStop {
		intent.drainBy = selected.stop.DrainBy
	}
	state.attempts[index].runningPeak = peak
	state.attempts[index].intent = intent
	state.attempts[index].phase = supervisorIntentLatched

	return state, true
}

func reduceEmergencyRunningSnapshot(
	state supervisorState,
	index int,
	at time.Time,
	drainBy time.Time,
	bundle supervisorRunningBundle,
) (supervisorState, time.Time) {
	var selected bool
	state, selected = reduceRunningSnapshot(state, index, at, bundle.drainBy, bundle)
	effectiveDrainBy := drainBy
	if selected {
		effectiveDrainBy = earlierTime(state.attempts[index].intent.drainBy, drainBy)
	} else {
		attempt := state.attempts[index]
		if attempt.intent != (supervisorRunningIntent{}) {
			invariant(supervisorReducerOperation, "runtime emergency intent is duplicated")
		}
		state.attempts[index].intent = runtimeEmergencyIntent(attempt, at, drainBy)
	}
	state.attempts[index].phase = supervisorEmergencyDraining

	return state, effectiveDrainBy
}

func runtimeEmergencyIntent(
	attempt supervisorAttemptState,
	at time.Time,
	drainBy time.Time,
) supervisorRunningIntent {
	if attempt.startedAt.IsZero() || at.Before(attempt.startedAt) || !drainBy.After(at) {
		invariant(supervisorReducerOperation, "runtime emergency intent chronology or bound is invalid")
	}

	return supervisorRunningIntent{
		latched:  true,
		kind:     supervisorIntentRuntimeEmergency,
		at:       at,
		drainBy:  drainBy,
		duration: at.Sub(attempt.startedAt),
	}
}

func validateRunningBundleCorrelation(attempt supervisorAttemptState, bundle *supervisorRunningBundle) {
	if bundle == nil || bundle.generation != attempt.generation ||
		bundle.waitAction != attempt.waitAction || bundle.sampleAction != attempt.sampleAction {
		invariant(supervisorReducerOperation, "running bundle action correlation is stale or wrong")
	}
}

func validateRunningFact(
	attempt supervisorAttemptState,
	through time.Time,
	fact supervisorRunningFact,
) (supervisorIntentCandidate, bool) {
	if fact.generation != attempt.generation || fact.at.Before(attempt.startedAt) || fact.at.After(through) {
		invariant(supervisorReducerOperation, "running fact generation or logical instant is invalid")
	}
	switch fact.kind {
	case supervisorRunningFuseObserved:
		if attempt.profile != AutomaticProfile || attempt.sampleAction == 0 ||
			fact.action != attempt.sampleAction ||
			!fact.rootLive || fact.live == 0 || fact.liveNegative || fact.live > maximumSupervisorCount() ||
			!fact.stop.At.IsZero() || !fact.stop.DrainBy.IsZero() || fact.exitCode != 0 ||
			fact.exitSignal != 0 || fact.source != 0 || fact.diagnostic != 0 {
			invariant(supervisorReducerOperation, "running count evidence is invalid or overflowed")
		}
		if fact.live <= supervisorFuseCeiling {
			return supervisorIntentCandidate{}, false
		}

		return supervisorIntentCandidate{
			kind: supervisorIntentFuse, at: fact.at,
			count: supervisorObservedCount{present: true, value: int(fact.live)},
		}, true
	case supervisorRunningRootExited:
		if attempt.waitAction == 0 || fact.action != attempt.waitAction ||
			fact.rootLive || fact.live != 0 || fact.liveNegative ||
			!fact.stop.At.IsZero() || !fact.stop.DrainBy.IsZero() ||
			fact.source != 0 || fact.diagnostic != 0 {
			invariant(supervisorReducerOperation, "root exit fact correlation is invalid")
		}

		return supervisorIntentCandidate{
			kind: supervisorIntentRootExit, at: fact.at,
			exitCode: fact.exitCode, exitSignal: fact.exitSignal,
		}, true
	case supervisorRunningObservationFailed:
		var observationAction supervisorActionToken
		switch fact.source {
		case supervisorObservationWait:
			observationAction = attempt.waitAction
		case supervisorObservationRunning:
			if attempt.profile != AutomaticProfile {
				invariant(supervisorReducerOperation, "serial attempt received a running observation failure")
			}
			observationAction = attempt.sampleAction
		default:
			invariant(supervisorReducerOperation, "running observation failure source is invalid")
		}
		if fact.action != observationAction || observationAction == 0 || fact.diagnostic == 0 ||
			fact.rootLive || fact.live != 0 || fact.liveNegative || fact.exitCode != 0 ||
			fact.exitSignal != 0 || !fact.stop.At.IsZero() || !fact.stop.DrainBy.IsZero() {
			invariant(supervisorReducerOperation, "running observation failure correlation is invalid")
		}

		return supervisorIntentCandidate{
			kind: supervisorIntentObservationFailure, at: fact.at,
			observationSource: fact.source, diagnostic: fact.diagnostic,
		}, true
	case supervisorRunningStopRequested:
		if fact.action != 0 || fact.rootLive || fact.live != 0 || fact.liveNegative || fact.exitCode != 0 ||
			fact.exitSignal != 0 || fact.source != 0 || fact.diagnostic != 0 ||
			fact.stop.validate() != nil || !fact.stop.At.Equal(fact.at) {
			invariant(supervisorReducerOperation, "stop fact is invalid")
		}

		return supervisorIntentCandidate{kind: supervisorIntentStop, at: fact.at, stop: fact.stop}, true
	default:
		invariant(supervisorReducerOperation, "running fact kind is invalid")

		return supervisorIntentCandidate{}, false
	}
}

func chooseRunningIntent(candidates []supervisorIntentCandidate) (supervisorIntentCandidate, bool) {
	var selected supervisorIntentCandidate
	found := false
	for _, candidate := range candidates {
		if !found || candidate.at.Before(selected.at) ||
			(candidate.at.Equal(selected.at) && runningIntentPriority(candidate.kind) < runningIntentPriority(selected.kind)) ||
			(candidate.at.Equal(selected.at) && candidate.kind == supervisorIntentFuse &&
				selected.kind == supervisorIntentFuse && candidate.count.value > selected.count.value) ||
			(candidate.at.Equal(selected.at) && candidate.kind == supervisorIntentStop &&
				selected.kind == supervisorIntentStop && candidate.stop.DrainBy.Before(selected.stop.DrainBy)) ||
			(candidate.at.Equal(selected.at) && candidate.kind == supervisorIntentObservationFailure &&
				selected.kind == supervisorIntentObservationFailure &&
				candidate.observationSource == supervisorObservationWait &&
				selected.observationSource == supervisorObservationRunning) {
			selected = candidate
			found = true
		}
	}

	return selected, found
}

func validateSameInstantRunningCandidates(candidates []supervisorIntentCandidate) {
	for left := range candidates {
		for right := left + 1; right < len(candidates); right++ {
			if !candidates[left].at.Equal(candidates[right].at) ||
				candidates[left].kind != candidates[right].kind {
				continue
			}
			switch candidates[left].kind {
			case supervisorIntentRootExit:
				invariant(supervisorReducerOperation, "duplicate root exit at one logical instant")
			case supervisorIntentObservationFailure:
				if candidates[left].observationSource == candidates[right].observationSource {
					invariant(supervisorReducerOperation, "duplicate supervision failure source at one logical instant")
				}
			}
		}
	}
}

func diagnosticsAtSelectedInstant(
	candidates []supervisorIntentCandidate,
	at time.Time,
) supervisorObservationDiagnostics {
	var diagnostics supervisorObservationDiagnostics
	for _, candidate := range candidates {
		if candidate.kind != supervisorIntentObservationFailure || !candidate.at.Equal(at) {
			continue
		}
		switch candidate.observationSource {
		case supervisorObservationWait:
			diagnostics.wait = candidate.diagnostic
		case supervisorObservationRunning:
			diagnostics.running = candidate.diagnostic
		default:
			invariant(supervisorReducerOperation, "supervision diagnostic source is invalid")
		}
	}

	return diagnostics
}

func runningIntentPriority(kind supervisorRunningIntentKind) uint8 {
	switch kind {
	case supervisorIntentFuse:
		return 1
	case supervisorIntentRootExit:
		return 2
	case supervisorIntentObservationFailure:
		return 3
	case supervisorIntentDeadline:
		return 4
	case supervisorIntentStop:
		return 5
	default:
		invariant(supervisorReducerOperation, "running intent kind is invalid")

		return 0
	}
}

func runningPeakThrough(
	peak supervisorObservedCount,
	facts []supervisorRunningFact,
	through time.Time,
) supervisorObservedCount {
	for _, fact := range facts {
		if fact.kind != supervisorRunningFuseObserved || fact.at.After(through) ||
			fact.live > supervisorFuseCeiling {
			continue
		}
		value := int(fact.live)
		if !peak.present || value > peak.value {
			peak = supervisorObservedCount{present: true, value: value}
		}
	}

	return peak
}

func exitRecheckZero(recheck supervisorExitRecheck) bool {
	return !recheck.performed && !recheck.observed && recheck.at.IsZero() &&
		recheck.code == 0 && recheck.signal == 0 && recheck.action == 0
}

const supervisorFuseCeiling uint64 = 64

func maximumSupervisorCount() uint64 { return uint64(^uint(0) >> 1) }

func (state supervisorState) completeLaunch(
	index int,
	completion supervisorLaunchCompletion,
	at time.Time,
	drainBy time.Time,
) (supervisorState, []supervisorAction) {
	priorPhase := state.attempts[index].phase
	late := priorPhase == supervisorLaunchReportedUnconfirmed
	if priorPhase != supervisorLaunchEstablishing && !late {
		invariant(supervisorReducerOperation, "launch completion phase is invalid")
	}
	switch completion.kind {
	case supervisorLaunchProvenNotReleased:
		state.attempts[index].phase = supervisorLaunchClosedNotReleased
		kind := supervisorPublishNotReleased
		if late {
			kind = supervisorCloseProspective
		}
		action := state.newAction(kind, index, at, drainBy, &completion)

		return state, []supervisorAction{action}
	case supervisorLaunchReleased:
		if late {
			if drainBy.IsZero() {
				invariant(supervisorReducerOperation, "forced launch ownership lacks a drain bound")
			}
			state.attempts[index].phase = supervisorLaunchOwned

			return state.forceLaunchOwnership(index, at, drainBy, completion)
		}

		state.attempts[index].phase = supervisorRunning
		state.attempts[index].startedAt = completion.at
		state.attempts[index].deadlineAt = completion.at.Add(state.attempts[index].commandDeadline)
		actions := []supervisorAction{
			state.newAction(supervisorPublishOwned, index, completion.at, time.Time{}, &completion),
			state.newAction(supervisorWaitRoot, index, completion.at, time.Time{}, nil),
		}
		state.attempts[index].waitAction = actions[1].token
		if state.attempts[index].profile == AutomaticProfile {
			actions = append(actions, state.newAction(supervisorSampleRunning, index, completion.at, time.Time{}, nil))
			state.attempts[index].sampleAction = actions[2].token
		}

		return state, actions
	case supervisorLaunchReleaseUnconfirmed:
		if !drainBy.After(at) {
			invariant(supervisorReducerOperation, "release-unknown launch lacks a positive drain bound")
		}
		state.attempts[index].phase = supervisorLaunchOwned
		state.attempts[index].releaseRevoked = true
		var publish supervisorAction
		if !late {
			publish = state.newAction(supervisorPublishLaunchUnconfirmed, index, at, drainBy, nil)
			publish.launchDuration = completion.at.Sub(state.attempts[index].registeredAt)
		}
		released := completion
		released.kind = supervisorLaunchReleased
		released.diagnostic = 0
		var actions []supervisorAction
		state, actions = state.forceLaunchOwnership(index, at, drainBy, released)
		if late {
			return state, actions
		}

		return state, append([]supervisorAction{publish}, actions...)
	default:
		invariant(supervisorReducerOperation, "launch completion kind is invalid")

		return supervisorState{}, nil
	}
}

func (state supervisorState) completeEmergencyReleasedLaunch(
	index int,
	completion supervisorLaunchCompletion,
	at time.Time,
	emergencyDrainBy time.Time,
	snapshot supervisorRunningBundle,
) (supervisorState, []supervisorAction) {
	attempt := state.attempts[index]
	state.attempts[index].startedAt = completion.at
	state.attempts[index].deadlineAt = completion.at.Add(attempt.commandDeadline)
	intent, effectiveDrainBy := selectEmergencyReleasedIntent(
		state.attempts[index], at, emergencyDrainBy, snapshot,
	)
	state.attempts[index].intent = intent
	state.attempts[index].phase = supervisorEmergencyDraining

	return state.forceLaunchOwnership(index, at, effectiveDrainBy, completion)
}

func selectEmergencyReleasedIntent(
	attempt supervisorAttemptState,
	at time.Time,
	emergencyDrainBy time.Time,
	snapshot supervisorRunningBundle,
) (supervisorRunningIntent, time.Time) {
	recheck := snapshot.exitRecheck
	if snapshot.generation != attempt.generation || snapshot.waitAction != 0 ||
		snapshot.sampleAction != 0 || len(snapshot.facts) != 0 || !recheck.performed ||
		recheck.action == 0 || recheck.action != attempt.launchAction ||
		attempt.startedAt.IsZero() || !attempt.deadlineAt.Equal(attempt.startedAt.Add(attempt.commandDeadline)) ||
		!attempt.deadlineAt.After(attempt.startedAt) {
		invariant(supervisorReducerOperation, "emergency released snapshot shape or deadline is invalid")
	}
	if recheck.observed {
		if recheck.at.Before(attempt.startedAt) || recheck.at.After(at) {
			invariant(supervisorReducerOperation, "observed emergency root completion is outside its interval")
		}
	} else if !recheck.at.Equal(at) || recheck.code != 0 || recheck.signal != 0 {
		invariant(supervisorReducerOperation, "unobserved emergency root snapshot does not prove absence through the cut")
	}

	intent := runtimeEmergencyIntent(attempt, at, emergencyDrainBy)
	if recheck.observed && !recheck.at.After(attempt.deadlineAt) {
		intent.kind = supervisorIntentRootExit
		intent.at = recheck.at
		intent.drainBy = snapshot.drainBy
		intent.duration = recheck.at.Sub(attempt.startedAt)
		intent.exitCode = recheck.code
		intent.exitSignal = recheck.signal
	} else if !at.Before(attempt.deadlineAt) {
		intent.kind = supervisorIntentDeadline
		intent.at = attempt.deadlineAt
		intent.drainBy = snapshot.drainBy
		intent.duration = attempt.commandDeadline
	}
	if intent.kind == supervisorIntentRuntimeEmergency {
		if !snapshot.drainBy.IsZero() {
			invariant(supervisorReducerOperation, "runtime emergency fallback supplied a local drain bound")
		}

		return intent, emergencyDrainBy
	}
	if !snapshot.drainBy.After(intent.at) {
		invariant(supervisorReducerOperation, "emergency root or deadline intent lacks a positive local drain bound")
	}

	return intent, earlierTime(snapshot.drainBy, emergencyDrainBy)
}

func (state supervisorState) forceLaunchOwnership(
	index int,
	at time.Time,
	drainBy time.Time,
	completion supervisorLaunchCompletion,
) (supervisorState, []supervisorAction) {
	attempt := state.attempts[index]
	if completion.kind != supervisorLaunchReleased || completion.failure != 0 || drainBy.IsZero() {
		invariant(supervisorReducerOperation, "forced launch ownership lacks released completion or bound")
	}
	publishKind := supervisorPublishOwned
	intent := attempt.intent
	switch attempt.phase {
	case supervisorLaunchOwned:
		publishKind = supervisorAdoptOwned
		if intent != (supervisorRunningIntent{}) {
			invariant(supervisorReducerOperation, "late adopted launch carries a caller terminal intent")
		}
	case supervisorEmergencyDraining:
		if !intent.latched {
			invariant(supervisorReducerOperation, "caller-owned emergency launch lacks a terminal intent")
		}
	default:
		invariant(supervisorReducerOperation, "forced launch ownership phase is invalid")
	}
	publish := state.newAction(publishKind, index, at, drainBy, &completion)
	force := state.newAction(supervisorForceOwned, index, at, drainBy, &completion)
	force.intent = intent
	state.attempts[index].pendingAction = supervisorPendingAction{
		kind: force.kind, token: force.token,
	}
	state.attempts[index].drain = supervisorDrainState{
		effectiveDrainBy: drainBy,
		forced:           true,
	}

	return state, []supervisorAction{publish, force}
}

func (state supervisorState) revokeProspective(
	index int,
	at time.Time,
	drainBy time.Time,
) (supervisorState, []supervisorAction) {
	state.attempts[index].phase = supervisorLaunchReportedUnconfirmed
	state.attempts[index].releaseRevoked = true
	state.attempts[index].revokedAt = at
	state.attempts[index].lastEventAt = at
	actions := []supervisorAction{
		state.newAction(supervisorRevokeLaunchRelease, index, at, drainBy, nil),
		state.newAction(supervisorPublishLaunchUnconfirmed, index, at, drainBy, nil),
	}
	actions[1].launchDuration = at.Sub(state.attempts[index].registeredAt)

	return state, actions
}

func requireLaunchCompletion(
	attempt supervisorAttemptState,
	completion *supervisorLaunchCompletion,
) supervisorLaunchCompletion {
	if completion == nil || completion.generation != attempt.generation ||
		completion.action != attempt.launchAction || completion.at.IsZero() ||
		completion.at.Before(attempt.registeredAt) {
		invariant(supervisorReducerOperation, "launch completion correlation is stale or wrong")
	}
	switch completion.kind {
	case supervisorLaunchProvenNotReleased:
		if completion.failure != LaunchFailed && completion.failure != LaunchResourceExhausted {
			invariant(supervisorReducerOperation, "not-released completion has invalid failure classification")
		}
	case supervisorLaunchReleased:
		if completion.failure != 0 || completion.diagnostic != 0 {
			invariant(supervisorReducerOperation, "released completion carries a launch failure")
		}
	case supervisorLaunchReleaseUnconfirmed:
		if completion.failure != 0 || completion.diagnostic == 0 {
			invariant(supervisorReducerOperation, "release-unknown completion lacks its diagnostic")
		}
	default:
		invariant(supervisorReducerOperation, "launch completion kind is invalid")
	}

	return *completion
}

func (state *supervisorState) newAction(
	kind supervisorActionKind,
	index int,
	at time.Time,
	drainBy time.Time,
	completion *supervisorLaunchCompletion,
) supervisorAction {
	if kind < supervisorLaunchNative || kind > supervisorDeliverTerminal ||
		index < 0 || index >= len(state.attempts) || state.nextAction == ^supervisorActionToken(0) {
		invariant(supervisorReducerOperation, "action allocation is invalid or exhausted")
	}
	state.nextAction++
	action := supervisorAction{
		kind:       kind,
		generation: state.attempts[index].generation,
		token:      state.nextAction,
		at:         at,
		drainBy:    drainBy,
	}
	if completion != nil {
		action.launchKind = completion.kind
		action.launchFailure = completion.failure
		action.launchDiagnostic = completion.diagnostic
		action.launchDuration = completion.at.Sub(state.attempts[index].registeredAt)
	}

	return action
}

func (state *supervisorState) newGlobalAction(kind supervisorActionKind) supervisorAction {
	if (kind != supervisorSettleEmergency && kind != supervisorDeliverEmergencySettlement) ||
		state.nextAction == ^supervisorActionToken(0) {
		invariant(supervisorReducerOperation, "global action allocation is invalid or exhausted")
	}
	state.nextAction++

	return supervisorAction{kind: kind, token: state.nextAction}
}

func (state supervisorState) attemptIndex(generation attemptGeneration) int {
	for index := range state.attempts {
		if state.attempts[index].generation == generation {
			return index
		}
	}

	return -1
}

func (state supervisorState) requireAttempt(generation attemptGeneration) int {
	if generation == 0 {
		invariant(supervisorReducerOperation, "event generation is zero")
	}
	index := state.attemptIndex(generation)
	if index < 0 {
		invariant(supervisorReducerOperation, "event generation is stale or unknown")
	}

	return index
}

func (event supervisorEvent) attemptIsZero() bool { return event.attempt == "" }
