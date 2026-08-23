package ooze

import "time"

const supervisorReducerOperation = "reduce supervisor"

type supervisorActionToken uint64

type supervisorLaunchCompletionKind uint8

const (
	supervisorLaunchProvenNotReleased supervisorLaunchCompletionKind = iota + 1
	supervisorLaunchReleased
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
}

type supervisorDrainCompletion struct {
	generation attemptGeneration
	action     supervisorPendingAction
	at         time.Time
	kind       supervisorDrainCompletionKind
	diagnostic supervisorDiagnosticRef
}

type supervisorOutputCompletion struct {
	generation   attemptGeneration
	action       supervisorPendingAction
	at           time.Time
	ref          supervisorOutputRef
	cutoff       uint64
	prefixLength uint64
	diagnostic   supervisorDiagnosticRef
}

type supervisorStopSealCompletion struct {
	generation attemptGeneration
	action     supervisorPendingAction
	at         time.Time
}

type supervisorOutputEvidence struct {
	ref                   supervisorOutputRef
	cutoff                uint64
	prefixLength          uint64
	completeThroughCutoff bool
	final                 bool
	diagnostic            supervisorDiagnosticRef
}

type supervisorEmergencySnapshot struct {
	generation attemptGeneration
	completion *supervisorLaunchCompletion
	running    *supervisorRunningBundle
}

type supervisorEmergencyEpoch struct {
	active  bool
	at      time.Time
	drainBy time.Time
}

type supervisorAttemptState struct {
	generation      attemptGeneration
	attempt         attemptIdentity
	profile         Profile
	commandDeadline time.Duration
	registeredAt    time.Time
	launchBy        time.Time
	lastEventAt     time.Time
	revokedAt       time.Time
	startedAt       time.Time
	deadlineAt      time.Time
	launchAction    supervisorActionToken
	waitAction      supervisorActionToken
	sampleAction    supervisorActionToken
	pendingDrain    supervisorPendingAction
	phase           supervisorAttemptPhase
	releaseRevoked  bool
	runningPeak     supervisorObservedCount
	intent          supervisorRunningIntent
	drain           supervisorDrainState
	output          supervisorOutputEvidence
}

type supervisorState struct {
	nextAction supervisorActionToken
	attempts   []supervisorAttemptState
	emergency  supervisorEmergencyEpoch
}

type supervisorEvent struct {
	kind               supervisorEventKind
	generation         attemptGeneration
	attempt            attemptIdentity
	at                 time.Time
	launchBy           time.Time
	drainBy            time.Time
	completion         *supervisorLaunchCompletion
	emergencySnapshots []supervisorEmergencySnapshot
	profile            Profile
	commandDeadline    time.Duration
	running            *supervisorRunningBundle
	drain              *supervisorDrainCompletion
	output             *supervisorOutputCompletion
	seal               *supervisorStopSealCompletion
}

type supervisorAction struct {
	kind           supervisorActionKind
	generation     attemptGeneration
	token          supervisorActionToken
	at             time.Time
	drainBy        time.Time
	launchKind     supervisorLaunchCompletionKind
	launchFailure  LaunchFailure
	launchDuration time.Duration
	intent         supervisorRunningIntent
}

func reduceSupervisor(state supervisorState, event supervisorEvent) (supervisorState, []supervisorAction) {
	next := cloneSupervisorState(state)
	switch event.kind {
	case supervisorProspectiveRegistered:
		return reduceProspectiveRegistration(next, event)
	case supervisorLaunchCompleted:
		return reduceLaunchCompletion(next, event)
	case supervisorLaunchBoundary:
		return reduceLaunchBoundary(next, event)
	case supervisorEmergencyStarted:
		return reduceLaunchEmergency(next, event)
	case supervisorRunningObserved:
		return reduceRunningBundle(next, event)
	case supervisorDrainCompleted:
		return reduceDrainCompletion(next, event)
	case supervisorOutputCompleted:
		return reduceOutputCompletion(next, event)
	case supervisorStopAdmissionSealed:
		return reduceStopSealCompletion(next, event)
	default:
		invariant(supervisorReducerOperation, "event kind is invalid")

		return supervisorState{}, nil
	}
}

func cloneSupervisorState(state supervisorState) supervisorState {
	state.attempts = append([]supervisorAttemptState(nil), state.attempts...)

	return state
}

func reduceProspectiveRegistration(
	state supervisorState,
	event supervisorEvent,
) (supervisorState, []supervisorAction) {
	if event.generation == 0 || event.attempt == "" || event.at.IsZero() ||
		event.launchBy.IsZero() || !event.launchBy.After(event.at) ||
		!event.drainBy.IsZero() || event.completion != nil || event.drain != nil ||
		event.output != nil || event.seal != nil || len(event.emergencySnapshots) != 0 ||
		event.running != nil || (event.profile != AutomaticProfile && event.profile != SerialProfile) ||
		event.commandDeadline <= 0 || state.attemptIndex(event.generation) >= 0 {
		invariant(supervisorReducerOperation, "prospective registration is incomplete or duplicated")
	}
	state.attempts = append(state.attempts, supervisorAttemptState{
		generation:      event.generation,
		attempt:         event.attempt,
		profile:         event.profile,
		commandDeadline: event.commandDeadline,
		registeredAt:    event.at,
		launchBy:        event.launchBy,
		lastEventAt:     event.at,
		phase:           supervisorLaunchEstablishing,
	})
	index := len(state.attempts) - 1
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
		len(event.emergencySnapshots) != 0 || event.profile != 0 || event.commandDeadline != 0 ||
		event.running != nil || event.at.Before(attempt.lastEventAt) {
		invariant(supervisorReducerOperation, "launch completion event is malformed or moved backward")
	}

	switch attempt.phase {
	case supervisorLaunchEstablishing:
		if !event.at.Before(attempt.launchBy) || !event.drainBy.IsZero() {
			invariant(supervisorReducerOperation, "at-bound or late completion bypassed boundary snapshot")
		}
		state.attempts[index].lastEventAt = event.at

		return state.completeLaunch(index, completion, false, false, event.at, time.Time{})
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
		} else if completion.kind == supervisorLaunchReleased {
			if !event.drainBy.After(event.at) {
				invariant(supervisorReducerOperation, "late release lacks a positive local drain bound")
			}
		} else if !event.drainBy.IsZero() {
			invariant(supervisorReducerOperation, "not-released launch completion supplied a drain bound")
		}

		return state.completeLaunch(index, completion, true, false, actionAt, drainBy)
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
		!event.attemptIsZero() || !event.launchBy.IsZero() || !event.drainBy.IsZero() ||
		len(event.emergencySnapshots) != 0 || event.profile != 0 || event.commandDeadline != 0 ||
		event.running != nil || event.drain != nil || event.output != nil || event.seal != nil ||
		event.at.Before(attempt.lastEventAt) {
		invariant(supervisorReducerOperation, "launch boundary is invalid or duplicated")
	}
	if event.completion != nil {
		completion := requireLaunchCompletion(attempt, event.completion)
		if completion.at.After(event.at) || completion.at.Before(attempt.lastEventAt) {
			invariant(supervisorReducerOperation, "boundary snapshot is outside its serialized interval")
		}
		state.attempts[index].lastEventAt = event.at

		return state.completeLaunch(index, completion, false, false, completion.at, time.Time{})
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
		event.running != nil || event.drain != nil || event.output != nil || event.seal != nil {
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
			if snapshot.running != nil {
				invariant(supervisorReducerOperation, "establishing emergency snapshot contains running facts")
			}
			if event.at.After(attempt.launchBy) || event.at.Before(attempt.lastEventAt) {
				invariant(supervisorReducerOperation, "emergency launch snapshot is outside its interval")
			}
			if snapshot.completion == nil {
				var emitted []supervisorAction
				state, emitted = state.revokeProspective(index, event.at, event.drainBy)
				actions = append(actions, emitted...)

				continue
			}
			completion := requireLaunchCompletion(attempt, snapshot.completion)
			if completion.at.After(event.at) || completion.at.Before(attempt.lastEventAt) {
				invariant(supervisorReducerOperation, "emergency completion is outside its serialized interval")
			}
			state.attempts[index].lastEventAt = event.at
			var emitted []supervisorAction
			state, emitted = state.completeLaunch(index, completion, false, true, event.at, event.drainBy)
			actions = append(actions, emitted...)
		case supervisorLaunchReportedUnconfirmed:
			if snapshot.running != nil {
				invariant(supervisorReducerOperation, "prospective emergency snapshot contains running facts")
			}
			if snapshot.completion == nil {
				continue
			}
			completion := requireLaunchCompletion(attempt, snapshot.completion)
			if completion.at.After(event.at) || completion.at.Before(attempt.revokedAt) {
				invariant(supervisorReducerOperation, "emergency late completion is outside its interval")
			}
			var emitted []supervisorAction
			state, emitted = state.completeLaunch(index, completion, true, false, event.at, event.drainBy)
			actions = append(actions, emitted...)
		case supervisorLaunchOwned:
			if snapshot.completion != nil || snapshot.running != nil {
				invariant(supervisorReducerOperation, "owned emergency snapshot contains launch completion")
			}
			state.attempts[index].phase = supervisorEmergencyDraining
			if attempt.pendingDrain.token == 0 {
				action := state.newAction(supervisorForceOwned, index, event.at, event.drainBy, nil)
				state.attempts[index].pendingDrain = supervisorPendingAction{kind: action.kind, token: action.token}
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
			if snapshot.completion != nil || snapshot.running == nil || snapshot.running.drainBy.IsZero() {
				invariant(supervisorReducerOperation, "running emergency snapshot is incomplete")
			}
			var selected bool
			state, selected = reduceRunningSnapshot(
				state, index, event.at, snapshot.running.drainBy, *snapshot.running,
			)
			state.attempts[index].phase = supervisorEmergencyDraining
			action := state.newAction(supervisorForceOwned, index, event.at, event.drainBy, nil)
			if selected {
				action.intent = state.attempts[index].intent
			}
			state.attempts[index].pendingDrain = supervisorPendingAction{kind: action.kind, token: action.token}
			state.attempts[index].drain = supervisorDrainState{
				effectiveDrainBy: earlierTime(snapshot.running.drainBy, event.drainBy),
				forced:           true,
			}
			action.drainBy = state.attempts[index].drain.effectiveDrainBy
			actions = append(actions, action)
		case supervisorIntentLatched:
			if snapshot.completion != nil || snapshot.running == nil {
				invariant(supervisorReducerOperation, "latched emergency snapshot is incomplete")
			}
			validateRunningBundleCorrelation(attempt, snapshot.running)
			state.attempts[index].phase = supervisorEmergencyDraining
			if attempt.pendingDrain.token == 0 || attempt.pendingDrain.kind == 0 {
				invariant(supervisorReducerOperation, "latched intent has no correlated drain action")
			}
			state.clampDrain(index, event.drainBy)
			state.attempts[index].lastEventAt = event.at
		case supervisorCapturingOutput:
			if snapshot.completion != nil || snapshot.running != nil || event.at.Before(attempt.lastEventAt) ||
				attempt.pendingDrain.kind != supervisorCaptureOutput || attempt.pendingDrain.token == 0 ||
				(attempt.drain.decision != supervisorDrainProvenEmpty &&
					attempt.drain.decision != supervisorDrainUnconfirmed) {
				invariant(supervisorReducerOperation, "capture emergency snapshot or pending action is invalid")
			}
			state.clampDrain(index, event.drainBy)
			state.attempts[index].lastEventAt = event.at
		case supervisorSealingStopAdmission, supervisorReleasingDomain,
			supervisorTransferringResidualCustody:
			wantPending := supervisorSealStopAdmission
			if attempt.phase == supervisorReleasingDomain {
				wantPending = supervisorReleaseDomain
			} else if attempt.phase == supervisorTransferringResidualCustody {
				wantPending = supervisorTransferResidualCustody
			}
			branchOwnsDecision := attempt.phase == supervisorSealingStopAdmission ||
				(attempt.phase == supervisorReleasingDomain &&
					attempt.drain.decision == supervisorDrainProvenEmpty) ||
				(attempt.phase == supervisorTransferringResidualCustody &&
					attempt.drain.decision == supervisorDrainUnconfirmed)
			if snapshot.completion != nil || snapshot.running != nil || event.at.Before(attempt.lastEventAt) ||
				attempt.pendingDrain.kind != wantPending || attempt.pendingDrain.token == 0 ||
				attempt.output.ref == 0 || !branchOwnsDecision ||
				attempt.output.final != (attempt.drain.decision == supervisorDrainProvenEmpty) ||
				(attempt.drain.decision != supervisorDrainProvenEmpty &&
					attempt.drain.decision != supervisorDrainUnconfirmed) {
				invariant(supervisorReducerOperation, "output pipeline emergency snapshot is invalid")
			}
			state.clampDrain(index, event.drainBy)
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
		event.drain != nil || event.output != nil || event.seal != nil ||
		len(event.emergencySnapshots) != 0 || event.profile != 0 || event.commandDeadline != 0 ||
		!event.running.drainBy.IsZero() {
		invariant(supervisorReducerOperation, "running bundle shape or action correlation is invalid")
	}
	validateRunningBundleCorrelation(attempt, event.running)
	if attempt.phase == supervisorIntentLatched || attempt.phase == supervisorEmergencyDraining {
		validateSealedRunningBundle(attempt, event.at, *event.running)

		return state, nil
	}
	if attempt.phase != supervisorRunning || event.at.Before(attempt.startedAt) ||
		event.at.Before(attempt.lastEventAt) {
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
	state.attempts[index].pendingDrain = supervisorPendingAction{kind: action.kind, token: action.token}
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
		completion.action != attempt.pendingDrain || completion.action.token == 0 ||
		completion.at.IsZero() || !event.at.Equal(completion.at) || event.at.Before(attempt.lastEventAt) ||
		!event.attemptIsZero() || !event.launchBy.IsZero() || !event.drainBy.IsZero() ||
		event.completion != nil || event.output != nil || event.seal != nil ||
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

	state.attempts[index].pendingDrain = supervisorPendingAction{}
	state.attempts[index].lastEventAt = completion.at
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
		completion.action != attempt.pendingDrain ||
		completion.action.kind != supervisorCaptureOutput || completion.action.token == 0 ||
		completion.at.IsZero() || !event.at.Equal(completion.at) || event.at.Before(attempt.lastEventAt) ||
		completion.ref == 0 || completion.prefixLength > completion.cutoff ||
		(completion.diagnostic == 0 && completion.prefixLength != completion.cutoff) ||
		!event.attemptIsZero() || !event.launchBy.IsZero() || !event.drainBy.IsZero() ||
		event.completion != nil || event.drain != nil || event.seal != nil ||
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

	state.attempts[index].pendingDrain = supervisorPendingAction{}
	state.attempts[index].lastEventAt = completion.at
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
	state.attempts[index].pendingDrain = supervisorPendingAction{kind: action.kind, token: action.token}

	return state, []supervisorAction{action}
}

func reduceStopSealCompletion(
	state supervisorState,
	event supervisorEvent,
) (supervisorState, []supervisorAction) {
	index := state.requireAttempt(event.generation)
	attempt := state.attempts[index]
	completion := event.seal
	if completion == nil || completion.generation != attempt.generation ||
		completion.action != attempt.pendingDrain ||
		completion.action.kind != supervisorSealStopAdmission || completion.action.token == 0 ||
		completion.at.IsZero() || !event.at.Equal(completion.at) || event.at.Before(attempt.lastEventAt) ||
		!event.attemptIsZero() || !event.launchBy.IsZero() || !event.drainBy.IsZero() ||
		event.completion != nil || event.drain != nil || event.output != nil ||
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

	state.attempts[index].pendingDrain = supervisorPendingAction{}
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
	state.attempts[index].pendingDrain = supervisorPendingAction{kind: action.kind, token: action.token}

	return state, []supervisorAction{action}
}

func (state supervisorState) issueDrainAction(
	index int,
	kind supervisorActionKind,
	at time.Time,
) (supervisorState, []supervisorAction) {
	if state.attempts[index].pendingDrain != (supervisorPendingAction{}) ||
		(kind != supervisorForceOwned && kind != supervisorObserveEmptiness) {
		invariant(supervisorReducerOperation, "drain action overlaps its predecessor or has an invalid kind")
	}
	action := state.newAction(kind, index, at, state.attempts[index].drain.effectiveDrainBy, nil)
	state.attempts[index].pendingDrain = supervisorPendingAction{kind: action.kind, token: action.token}

	return state, []supervisorAction{action}
}

func (state supervisorState) captureDrain(
	index int,
	at time.Time,
	provenEmpty bool,
) (supervisorState, []supervisorAction) {
	if state.attempts[index].pendingDrain != (supervisorPendingAction{}) ||
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
	state.attempts[index].pendingDrain = supervisorPendingAction{kind: action.kind, token: action.token}

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
		(!recheck.observed && (recheck.code != 0 || recheck.signal != 0)) {
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
	if through.Before(attempt.startedAt) || drainBy.IsZero() {
		invariant(supervisorReducerOperation, "running snapshot interval or local drain bound is invalid")
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
		if !recheck.performed || !recheck.at.Equal(attempt.deadlineAt) {
			invariant(supervisorReducerOperation, "deadline boundary lacks its explicit exit recheck")
		}
		if !recheck.observed && (recheck.code != 0 || recheck.signal != 0) {
			invariant(supervisorReducerOperation, "unobserved exit recheck carries exit status")
		}
		if recheck.observed {
			candidates = append(candidates, supervisorIntentCandidate{
				kind: supervisorIntentRootExit, at: attempt.deadlineAt,
				exitCode: recheck.code, exitSignal: recheck.signal,
			})
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
		if attempt.profile != AutomaticProfile || fact.action != attempt.sampleAction ||
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
		if fact.action != attempt.waitAction || fact.rootLive || fact.live != 0 || fact.liveNegative ||
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
		recheck.code == 0 && recheck.signal == 0
}

const supervisorFuseCeiling uint64 = 64

func maximumSupervisorCount() uint64 { return uint64(^uint(0) >> 1) }

func (state supervisorState) completeLaunch(
	index int,
	completion supervisorLaunchCompletion,
	late bool,
	force bool,
	at time.Time,
	drainBy time.Time,
) (supervisorState, []supervisorAction) {
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
		kind := supervisorPublishOwned
		if late {
			kind = supervisorAdoptOwned
			force = true
		}
		if force {
			if drainBy.IsZero() {
				invariant(supervisorReducerOperation, "forced launch ownership lacks a drain bound")
			}
			state.attempts[index].phase = supervisorLaunchOwned
			actions := []supervisorAction{state.newAction(kind, index, at, drainBy, &completion)}
			actions = append(actions, state.newAction(supervisorForceOwned, index, at, drainBy, &completion))
			state.attempts[index].pendingDrain = supervisorPendingAction{
				kind: actions[1].kind, token: actions[1].token,
			}
			state.attempts[index].drain = supervisorDrainState{
				effectiveDrainBy: drainBy,
				forced:           true,
			}

			return state, actions
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
	default:
		invariant(supervisorReducerOperation, "launch completion kind is invalid")

		return supervisorState{}, nil
	}
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
		if completion.failure != 0 {
			invariant(supervisorReducerOperation, "released completion carries a launch failure")
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
	if kind < supervisorLaunchNative || kind > supervisorTransferResidualCustody ||
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
		action.launchDuration = completion.at.Sub(state.attempts[index].registeredAt)
	}

	return action
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
