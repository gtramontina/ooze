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
)

type supervisorEventKind uint8

const (
	supervisorProspectiveRegistered supervisorEventKind = iota + 1
	supervisorLaunchCompleted
	supervisorLaunchBoundary
	supervisorEmergencyStarted
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
)

type supervisorLaunchCompletion struct {
	generation attemptGeneration
	action     supervisorActionToken
	at         time.Time
	kind       supervisorLaunchCompletionKind
	failure    LaunchFailure
}

type supervisorEmergencySnapshot struct {
	generation attemptGeneration
	completion *supervisorLaunchCompletion
}

type supervisorEmergencyEpoch struct {
	active  bool
	at      time.Time
	drainBy time.Time
}

type supervisorAttemptState struct {
	generation       attemptGeneration
	attempt          attemptIdentity
	launchBy         time.Time
	lastEventAt      time.Time
	revokedAt        time.Time
	emergencyAt      time.Time
	emergencyDrainBy time.Time
	launchAction     supervisorActionToken
	phase            supervisorAttemptPhase
	releaseRevoked   bool
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
}

type supervisorAction struct {
	kind          supervisorActionKind
	generation    attemptGeneration
	token         supervisorActionToken
	at            time.Time
	drainBy       time.Time
	launchKind    supervisorLaunchCompletionKind
	launchFailure LaunchFailure
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
		!event.drainBy.IsZero() || event.completion != nil || len(event.emergencySnapshots) != 0 ||
		state.attemptIndex(event.generation) >= 0 {
		invariant(supervisorReducerOperation, "prospective registration is incomplete or duplicated")
	}
	state.attempts = append(state.attempts, supervisorAttemptState{
		generation:  event.generation,
		attempt:     event.attempt,
		launchBy:    event.launchBy,
		lastEventAt: event.at,
		phase:       supervisorLaunchEstablishing,
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
		!event.launchBy.IsZero() || !event.drainBy.IsZero() ||
		len(event.emergencySnapshots) != 0 || event.at.Before(attempt.lastEventAt) {
		invariant(supervisorReducerOperation, "launch completion event is malformed or moved backward")
	}

	switch attempt.phase {
	case supervisorLaunchEstablishing:
		if !event.at.Before(attempt.launchBy) {
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
		if !attempt.emergencyAt.IsZero() {
			actionAt = attempt.emergencyAt
		}

		return state.completeLaunch(index, completion, true, false, actionAt, attempt.emergencyDrainBy)
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
		len(event.emergencySnapshots) != 0 || event.at.Before(attempt.lastEventAt) {
		invariant(supervisorReducerOperation, "launch boundary is invalid or duplicated")
	}
	if event.completion != nil {
		completion := requireLaunchCompletion(attempt, event.completion)
		if completion.at.After(event.at) || completion.at.Before(attempt.lastEventAt) {
			invariant(supervisorReducerOperation, "boundary snapshot is outside its serialized interval")
		}
		state.attempts[index].lastEventAt = event.at

		return state.completeLaunch(index, completion, false, false, event.at, time.Time{})
	}

	return state.revokeProspective(index, event.at, time.Time{})
}

func reduceLaunchEmergency(
	state supervisorState,
	event supervisorEvent,
) (supervisorState, []supervisorAction) {
	if state.emergency.active || event.generation != 0 || !event.attemptIsZero() ||
		event.at.IsZero() || !event.launchBy.IsZero() || !event.drainBy.After(event.at) ||
		event.completion != nil {
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
		state.attempts[index].emergencyAt = event.at
		state.attempts[index].emergencyDrainBy = event.drainBy

		switch attempt.phase {
		case supervisorLaunchEstablishing:
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
			if snapshot.completion != nil {
				invariant(supervisorReducerOperation, "owned emergency snapshot contains launch completion")
			}
			actions = append(actions, state.newAction(
				supervisorForceOwned, index, event.at, event.drainBy, nil,
			))
		default:
			invariant(supervisorReducerOperation, "emergency encountered an invalid attempt phase")
		}
	}
	if snapshotIndex != len(event.emergencySnapshots) {
		invariant(supervisorReducerOperation, "emergency snapshot set contains an unknown or closed attempt")
	}

	return state, actions
}

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
		state.attempts[index].phase = supervisorLaunchOwned
		kind := supervisorPublishOwned
		if late {
			kind = supervisorAdoptOwned
			force = true
		}
		actions := []supervisorAction{state.newAction(kind, index, at, drainBy, &completion)}
		if force {
			actions = append(actions, state.newAction(supervisorForceOwned, index, at, drainBy, &completion))
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
	if !drainBy.IsZero() {
		state.attempts[index].emergencyAt = at
		state.attempts[index].emergencyDrainBy = drainBy
	}
	state.attempts[index].lastEventAt = at
	actions := []supervisorAction{
		state.newAction(supervisorRevokeLaunchRelease, index, at, drainBy, nil),
		state.newAction(supervisorPublishLaunchUnconfirmed, index, at, drainBy, nil),
	}

	return state, actions
}

func requireLaunchCompletion(
	attempt supervisorAttemptState,
	completion *supervisorLaunchCompletion,
) supervisorLaunchCompletion {
	if completion == nil || completion.generation != attempt.generation ||
		completion.action != attempt.launchAction || completion.at.IsZero() {
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
	if kind < supervisorLaunchNative || kind > supervisorForceOwned ||
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
