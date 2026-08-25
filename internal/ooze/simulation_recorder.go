package ooze

import (
	"slices"
	"sort"
	"sync"
	"sync/atomic"
)

type simulationRecorder struct {
	gate    sync.RWMutex
	mutex   sync.Mutex
	next    atomic.Uint64
	records []simulationRecord
}

func newSimulationRecorder() *simulationRecorder { return &simulationRecorder{} }

func (recorder *simulationRecorder) enter() func() {
	if recorder == nil {
		return func() {}
	}
	recorder.gate.RLock()

	return recorder.gate.RUnlock
}

func (recorder *simulationRecorder) recordRuntime(operation string, state processRuntime) {
	if recorder == nil {
		return
	}
	record := simulationRecord{
		sequence: recorder.next.Add(1), authority: simulationRuntimeAuthority,
		runtimeOperationName: operation, runtimeState: simulationProjectRuntime(state),
	}
	if operation == "register campaign" && len(state.campaigns) != 0 {
		token := state.campaigns[len(state.campaigns)-1].token
		record.runtimeOperation = simulationRegisterCampaign
		record.runtimeProvenance = campaignProvenance{lineage: token.lineage}
		record.runtimeRegistration = campaignRegistration{decision: campaignRegistered, token: token}
	}
	recorder.append(record)
}

func (recorder *simulationRecorder) recordCampaign(
	event campaignEvent,
	state campaignState,
	effects []campaignEffect,
) {
	if recorder == nil {
		return
	}
	projectedState := simulationProjectCampaign(state)
	recorder.append(simulationRecord{
		sequence: recorder.next.Add(1), authority: simulationCampaignAuthority,
		campaignEvent: simulationProjectCampaignEvent(event, state), campaignState: projectedState,
		campaignEffects: simulationProjectCampaignEffects(effects, state),
	})
}

func (recorder *simulationRecorder) recordSupervisor(
	event supervisorEvent,
	state supervisorState,
	actions []supervisorAction,
) {
	if recorder == nil {
		return
	}
	recorder.append(simulationRecord{
		sequence: recorder.next.Add(1), authority: simulationSupervisorAuthority,
		supervisorEvent: event, supervisorState: cloneSupervisorState(state),
		supervisorActions: slices.Clone(actions),
	})
}

func (recorder *simulationRecorder) append(record simulationRecord) {
	recorder.mutex.Lock()
	recorder.records = append(recorder.records, record)
	recorder.mutex.Unlock()
}

func (recorder *simulationRecorder) quiescent(
	runner *managedCampaignRunner,
	runtime *processRuntimeShell,
	driver *supervisorDriver,
) (simulationTrace, simulationWorld) {
	recorder.gate.Lock()
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
	supervisorState := cloneSupervisorState(driver.state)
	driver.mutex.Unlock()
	campaignState := simulationProjectCampaign(runner.state)

	return simulationTrace{
			definition: simulationDefinition{
				campaign: campaignState.definition, capacity: runtimeState.capacity,
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
			state.obligations[index].identity = simulationLogicalWorkspace(state.obligations[index].attempt)
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
		event.payload = payload
	case workspaceMaterializedEvent:
		payload.snapshot = logicalSnapshot
		payload.workspace = simulationLogicalWorkspace(payload.attempt)
		event.payload = payload
	case workspaceMaterializationFailedEvent:
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
