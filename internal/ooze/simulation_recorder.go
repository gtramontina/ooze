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
	recorder.append(simulationRecord{
		sequence: recorder.next.Add(1), authority: simulationCampaignAuthority,
		campaignEvent: event, campaignState: state.clone(), campaignEffects: slices.Clone(effects),
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
	campaignState := runner.state.clone()

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
