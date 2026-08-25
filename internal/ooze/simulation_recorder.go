package ooze

import (
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type simulationRecorder struct {
	gate        sync.RWMutex
	mutex       sync.Mutex
	next        atomic.Uint64
	records     []simulationRecord
	actionMutex sync.Mutex
	actions     map[supervisorActionToken]simulationInFlightAction
	actionWake  chan struct{}
}

type simulationInFlightAction struct {
	kind       supervisorActionKind
	generation attemptGeneration
}

type simulationReservation struct {
	sequence  uint64
	authority simulationAuthority
}

func newSimulationRecorder() *simulationRecorder {
	return &simulationRecorder{
		actions:    make(map[supervisorActionToken]simulationInFlightAction),
		actionWake: make(chan struct{}, 1),
	}
}

func (recorder *simulationRecorder) enter() func() {
	if recorder == nil {
		return func() {}
	}
	recorder.gate.RLock()

	return recorder.gate.RUnlock
}

func (recorder *simulationRecorder) reserve(authority simulationAuthority) simulationReservation {
	if recorder == nil {
		return simulationReservation{}
	}

	return simulationReservation{sequence: recorder.next.Add(1), authority: authority}
}

func (recorder *simulationRecorder) recordRuntime(
	reservation simulationReservation,
	record simulationRecord,
	state processRuntime,
) {
	if recorder == nil {
		return
	}
	record.sequence = reservation.sequence
	record.authority = reservation.authority
	record.runtimeState = simulationTraceRuntimeState(state)
	recorder.append(record)
}

func (recorder *simulationRecorder) recordCampaign(
	reservation simulationReservation,
	event campaignEvent,
	previous campaignState,
	state campaignState,
	effects []campaignEffect,
) {
	if recorder == nil {
		return
	}
	switch payload := event.payload.(type) {
	case attemptTerminalEvent:
		recorder.recordSupervisorDelivery(supervisorDeliverTerminal, payload.generation)
	case runtimeEmergencySettledEvent:
		recorder.recordSupervisorDelivery(supervisorDeliverEmergencySettlement, 0)
	}
	projectedState := simulationProjectCampaign(state)
	recorder.append(simulationRecord{
		sequence: reservation.sequence, authority: reservation.authority,
		campaignEvent: simulationProjectCampaignEvent(event, previous), campaignState: projectedState,
		campaignEffects: simulationProjectCampaignEffects(effects, state),
	})
}

func (recorder *simulationRecorder) recordSupervisor(
	reservation simulationReservation,
	event supervisorEvent,
	state supervisorState,
	actions []supervisorAction,
) {
	if recorder == nil {
		return
	}
	recorder.recordSupervisorActions(actions)
	recorder.append(simulationRecord{
		sequence: reservation.sequence, authority: reservation.authority,
		supervisorEvent:   simulationTraceSupervisorEvent(event),
		supervisorState:   simulationTraceSupervisorState(state),
		supervisorActions: simulationTraceSupervisorActions(actions),
	})
}

func (recorder *simulationRecorder) append(record simulationRecord) {
	recorder.mutex.Lock()
	recorder.records = append(recorder.records, record)
	recorder.mutex.Unlock()
}

func (recorder *simulationRecorder) recordSupervisorActions(actions []supervisorAction) {
	if recorder == nil {
		return
	}
	recorder.actionMutex.Lock()
	defer recorder.actionMutex.Unlock()
	for _, action := range actions {
		_, found := recorder.actions[action.token]
		if action.token == 0 || found {
			panic("simulation recorder action is zero or duplicated")
		}
		recorder.actions[action.token] = simulationInFlightAction{
			kind: action.kind, generation: action.generation,
		}
	}
}

func (recorder *simulationRecorder) recordSupervisorCompletion(action supervisorPendingAction) {
	if recorder == nil {
		return
	}
	recorder.actionMutex.Lock()
	pending, found := recorder.actions[action.token]
	if !found || pending.kind != action.kind {
		recorder.actionMutex.Unlock()
		panic("simulation recorder action completion is stale or wrong")
	}
	delete(recorder.actions, action.token)
	recorder.actionMutex.Unlock()
	select {
	case recorder.actionWake <- struct{}{}:
	default:
	}
}

func (recorder *simulationRecorder) recordSupervisorAction(action supervisorAction) {
	recorder.recordSupervisorCompletion(supervisorPendingAction{kind: action.kind, token: action.token})
}

func (recorder *simulationRecorder) recordSupervisorDelivery(
	kind supervisorActionKind,
	generation attemptGeneration,
) {
	if recorder == nil {
		return
	}
	recorder.actionMutex.Lock()
	var matched supervisorActionToken
	for token, action := range recorder.actions {
		if action.kind != kind || action.generation != generation {
			continue
		}
		if matched != 0 {
			recorder.actionMutex.Unlock()
			panic("simulation recorder delivery action is ambiguous")
		}
		matched = token
	}
	if matched == 0 {
		recorder.actionMutex.Unlock()
		panic("simulation recorder delivery action is absent")
	}
	delete(recorder.actions, matched)
	recorder.actionMutex.Unlock()
	select {
	case recorder.actionWake <- struct{}{}:
	default:
	}
}

func (recorder *simulationRecorder) quiescent(
	runner *managedCampaignRunner,
	runtime *processRuntimeShell,
	driver *supervisorDriver,
) (simulationTrace, simulationWorld) {
	for {
		recorder.gate.Lock()
		recorder.actionMutex.Lock()
		pending := len(recorder.actions)
		recorder.actionMutex.Unlock()
		if pending == 0 {
			break
		}
		recorder.gate.Unlock()
		<-recorder.actionWake
	}
	defer recorder.gate.Unlock()

	recorder.mutex.Lock()
	records := slices.Clone(recorder.records)
	recorder.mutex.Unlock()
	sort.Slice(records, func(left, right int) bool {
		return records[left].sequence < records[right].sequence
	})

	runtime.mutex.Lock()
	runtimeState := simulationProjectRuntime(runtime.core)
	runtime.mutex.Unlock()
	driver.mutex.Lock()
	supervisorState := simulationProjectSupervisorState(driver.state)
	driver.mutex.Unlock()
	campaignState := simulationProjectCampaign(runner.state)
	definition := campaignState.definition
	definition.baselineDeadline = 0
	definition.command = slices.Clone(definition.command)
	definition.env = slices.Clone(definition.env)

	return simulationTrace{
			definition: simulationDefinition{
				campaign: definition, capacity: runtimeState.capacity,
				catalogue: slices.Clone(campaignState.catalogue),
			},
			records: records,
		}, simulationWorld{
			campaign: campaignState, runtime: runtimeState, supervisor: supervisorState,
		}
}

func simulationProjectRuntime(state processRuntime) processRuntime {
	state = state.clone()
	for index := range state.admissions {
		state.admissions[index].grant.delivery = nil
	}

	return state
}

func simulationProjectCampaign(state campaignState) campaignState {
	state = state.clone()
	logicalSnapshot := simulationLogicalSnapshot(state.definition.identity)
	if state.snapshot != "" {
		state.snapshot = logicalSnapshot
	}
	for index := range state.attempts {
		if state.attempts[index].workspace != "" {
			state.attempts[index].workspace = simulationLogicalWorkspace(state.attempts[index].identity)
		}
	}
	for index := range state.obligations {
		switch state.obligations[index].kind {
		case campaignResourceSnapshot:
			if state.snapshot != "" {
				state.obligations[index].identity = string(logicalSnapshot)
			}
		case campaignResourceWorkspace:
			attemptAt := state.attemptIndex(state.obligations[index].attempt)
			if attemptAt >= 0 && state.attempts[attemptAt].workspace != "" {
				state.obligations[index].identity = simulationLogicalWorkspace(state.obligations[index].attempt)
			}
		}
	}
	for index := range state.artifactResidue {
		state.artifactResidue[index] = "artifact-residue"
	}

	return state
}

func simulationProjectCampaignEvent(event campaignEvent, state campaignState) campaignEvent {
	logicalSnapshot := simulationLogicalSnapshot(state.definition.identity)
	switch payload := event.payload.(type) {
	case snapshotEstablishedEvent:
		payload.snapshot = logicalSnapshot
		event.payload = payload
	case catalogueDiscoveredEvent:
		payload.snapshot = logicalSnapshot
		payload.mutants = slices.Clone(payload.mutants)
		event.payload = payload
	case workspaceMaterializedEvent:
		payload.snapshot = logicalSnapshot
		payload.workspace = simulationLogicalWorkspace(payload.attempt)
		event.payload = payload
	case workspaceMaterializationFailedEvent:
		payload.artifactResidue = slices.Clone(payload.artifactResidue)
		for index := range payload.artifactResidue {
			payload.artifactResidue[index] = "artifact-residue"
		}
		event.payload = payload
	case resourceSettledEvent:
		payload.identity = simulationLogicalResource(payload.kind, payload.identity, "", state)
		event.payload = payload
	case resourceSettlementFailedEvent:
		payload.identity = simulationLogicalResource(payload.kind, payload.identity, "", state)
		event.payload = payload
	}

	return event
}

func simulationProjectCampaignEffects(effects []campaignEffect, state campaignState) []campaignEffect {
	projected := slices.Clone(effects)
	for index := range projected {
		effect := &projected[index]
		if effect.snapshot != "" {
			effect.snapshot = simulationLogicalSnapshot(state.definition.identity)
		}
		if effect.workspace != "" {
			effect.workspace = simulationLogicalWorkspace(effect.attempt)
		}
		if effect.spec.Dir != "" {
			effect.spec.Dir = simulationLogicalWorkspace(effect.attempt)
		}
		effect.spec.Command = slices.Clone(effect.spec.Command)
		effect.spec.Env = slices.Clone(effect.spec.Env)
	}

	return projected
}

func simulationLogicalResource(
	kind campaignResourceKind,
	identity string,
	attempt attemptIdentity,
	state campaignState,
) string {
	switch kind {
	case campaignResourceSnapshot:
		return string(simulationLogicalSnapshot(state.definition.identity))
	case campaignResourceWorkspace:
		if attempt != "" {
			return simulationLogicalWorkspace(attempt)
		}
		for _, candidate := range state.attempts {
			if candidate.workspace == identity {
				return simulationLogicalWorkspace(candidate.identity)
			}
		}
		return "workspace:settled"
	default:
		return identity
	}
}

func simulationLogicalSnapshot(campaign campaignIdentity) snapshotIdentity {
	return snapshotIdentity("snapshot:" + string(campaign))
}

func simulationLogicalWorkspace(attempt attemptIdentity) string {
	return "workspace:" + string(attempt)
}

func simulationProjectSupervisorState(state supervisorState) supervisorState {
	state = cloneSupervisorState(state)
	state.emergency.at = simulationCanonicalTime(state.emergency.at)
	state.emergency.drainBy = simulationCanonicalTime(state.emergency.drainBy)
	for index := range state.attempts {
		attempt := &state.attempts[index]
		attempt.registeredAt = simulationCanonicalTime(attempt.registeredAt)
		attempt.launchBy = simulationCanonicalTime(attempt.launchBy)
		attempt.lastEventAt = simulationCanonicalTime(attempt.lastEventAt)
		attempt.revokedAt = simulationCanonicalTime(attempt.revokedAt)
		attempt.startedAt = simulationCanonicalTime(attempt.startedAt)
		attempt.deadlineAt = simulationCanonicalTime(attempt.deadlineAt)
		attempt.intent.at = simulationCanonicalTime(attempt.intent.at)
		attempt.intent.drainBy = simulationCanonicalTime(attempt.intent.drainBy)
		attempt.intent.stop = simulationProjectStop(attempt.intent.stop)
		attempt.drain.effectiveDrainBy = simulationCanonicalTime(attempt.drain.effectiveDrainBy)
	}

	return state
}

func simulationProjectStop(stop StopRequest) StopRequest {
	stop.At = simulationCanonicalTime(stop.At)
	stop.DrainBy = simulationCanonicalTime(stop.DrainBy)

	return stop
}

func simulationCanonicalTime(instant time.Time) time.Time {
	if instant.IsZero() {
		return time.Time{}
	}

	return time.Unix(0, instant.UnixNano()).UTC()
}
