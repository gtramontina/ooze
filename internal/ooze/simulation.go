package ooze

import (
	"fmt"
	"reflect"
)

type simulationAuthority uint8

const (
	simulationCampaignAuthority simulationAuthority = iota + 1
	simulationRuntimeAuthority
	simulationSupervisorAuthority
)

type simulationRuntimeOperation uint8

const (
	simulationRegisterCampaign simulationRuntimeOperation = iota + 1
	simulationCommitTerminal
)

type simulationDefinition struct {
	campaign  campaignDefinition
	capacity  int
	catalogue []mutantIdentity
}

type simulationChoiceBytes []byte

type simulationTrace struct {
	definition simulationDefinition
	records    []simulationRecord
}

type simulationRecord struct {
	sequence  uint64
	authority simulationAuthority

	campaignEvent   campaignEvent
	campaignState   campaignState
	campaignEffects []campaignEffect

	runtimeOperation    simulationRuntimeOperation
	runtimeProvenance   campaignProvenance
	runtimeCampaign     campaignToken
	runtimeState        processRuntime
	runtimeRegistration campaignRegistration
	runtimeTerminal     terminalResult
}

type simulationWorld struct {
	campaign   campaignState
	runtime    processRuntime
	supervisor supervisorState
}

// SimulationResult contains the canonical trace and its replayed production world.
type SimulationResult struct {
	trace   simulationTrace
	world   simulationWorld
	failure error
}

// Explore expands choices only through facts enabled by the production owners.
func Explore(definition simulationDefinition, _ simulationChoiceBytes) SimulationResult {
	definition.catalogue = append([]mutantIdentity(nil), definition.catalogue...)
	state, effects := beginCampaign(definition.campaign)
	runtime := newProcessRuntime(definition.capacity)
	trace := simulationTrace{definition: definition}

	simulationRequireOnlyEffect(effects, campaignEffectRegister)
	var registration campaignRegistration
	runtime, registration = runtime.registerCampaign(campaignProvenance{lineage: definition.campaign.lineage})
	trace.records = append(trace.records, simulationRecord{
		sequence: 1, authority: simulationRuntimeAuthority,
		runtimeOperation:  simulationRegisterCampaign,
		runtimeProvenance: campaignProvenance{lineage: definition.campaign.lineage},
		runtimeState:      runtime, runtimeRegistration: registration,
	})

	payload := campaignEventPayload(campaignRegisteredEvent{registration: registration})
	state, effects = simulationAdvanceCampaign(state, payload)
	trace.records = append(trace.records, simulationCampaignRecord(trace, state, effects, payload))
	simulationRequireOnlyEffect(effects, campaignEffectEstablishSnapshot)
	payload = snapshotEstablishedEvent{snapshot: "snapshot-1"}
	state, effects = simulationAdvanceCampaign(state, payload)
	trace.records = append(trace.records, simulationCampaignRecord(trace, state, effects, payload))
	simulationRequireOnlyEffect(effects, campaignEffectDiscoverCatalogue)
	payload = catalogueDiscoveredEvent{
		snapshot: "snapshot-1", mutants: append([]mutantIdentity(nil), definition.catalogue...),
	}
	state, effects = simulationAdvanceCampaign(state, payload)
	trace.records = append(trace.records, simulationCampaignRecord(trace, state, effects, payload))
	if len(definition.catalogue) != 0 {
		return SimulationResult{trace: trace, failure: fmt.Errorf("non-empty simulation catalogue is not implemented")}
	}
	simulationRequireOnlyEffect(effects, campaignEffectReleaseSnapshot)
	payload = resourceSettledEvent{
		kind: campaignResourceSnapshot, identity: "snapshot-1",
	}
	state, effects = simulationAdvanceCampaign(state, payload)
	trace.records = append(trace.records, simulationCampaignRecord(trace, state, effects, payload))

	simulationRequireOnlyEffect(effects, campaignEffectProposeTerminal)
	var terminal terminalResult
	runtime, terminal = runtime.commitTerminal(registration.token)
	trace.records = append(trace.records, simulationRecord{
		sequence: uint64(len(trace.records) + 1), authority: simulationRuntimeAuthority,
		runtimeOperation: simulationCommitTerminal, runtimeCampaign: registration.token,
		runtimeState: runtime, runtimeTerminal: terminal,
	})
	payload = terminalCommittedEvent{
		result: campaignTerminalEvidence(terminal),
	}
	state, effects = simulationAdvanceCampaign(state, payload)
	trace.records = append(trace.records, simulationCampaignRecord(trace, state, effects, payload))

	return SimulationResult{
		trace: trace,
		world: simulationWorld{campaign: state, runtime: runtime, supervisor: supervisorState{}},
	}
}

func simulationRequireOnlyEffect(effects []campaignEffect, kind campaignEffectKind) {
	if len(effects) != 1 || effects[0].kind != kind {
		panic(fmt.Sprintf("simulation effect=%#v, want one %v", effects, kind))
	}
}

func simulationAdvanceCampaign(
	state campaignState,
	payload campaignEventPayload,
) (campaignState, []campaignEffect) {
	return advanceCampaign(state, campaignEvent{
		id:      campaignEventID(len(state.trace) + 1),
		payload: payload,
	})
}

func simulationCampaignRecord(
	trace simulationTrace,
	state campaignState,
	effects []campaignEffect,
	payload campaignEventPayload,
) simulationRecord {
	return simulationRecord{
		sequence: uint64(len(trace.records) + 1), authority: simulationCampaignAuthority,
		campaignEvent: campaignEvent{id: campaignEventID(len(state.trace)), payload: payload}, campaignState: state,
		campaignEffects: append([]campaignEffect(nil), effects...),
	}
}

// ReplayLegal replays a typed legal trace through fresh production owner states.
func ReplayLegal(trace simulationTrace) (result SimulationResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = SimulationResult{trace: trace, failure: fmt.Errorf("replay invariant: %v", recovered)}
		}
	}()

	campaign, effects := beginCampaign(trace.definition.campaign)
	runtime := newProcessRuntime(trace.definition.capacity)
	var delivered campaignEventPayload
	for index, record := range trace.records {
		if record.sequence != uint64(index+1) {
			return simulationReplayFailure(trace, "record %d has sequence %d", index, record.sequence)
		}
		switch record.authority {
		case simulationRuntimeAuthority:
			if len(effects) != 1 {
				return simulationReplayFailure(trace, "runtime record %d has %d enabled campaign effects", index, len(effects))
			}
			switch record.runtimeOperation {
			case simulationRegisterCampaign:
				if effects[0].kind != campaignEffectRegister {
					return simulationReplayFailure(trace, "registration is not enabled at record %d", index)
				}
				var registration campaignRegistration
				runtime, registration = runtime.registerCampaign(record.runtimeProvenance)
				if !reflect.DeepEqual(registration, record.runtimeRegistration) {
					return simulationReplayFailure(trace, "registration diverged at record %d", index)
				}
				delivered = campaignRegisteredEvent{registration: registration}
			case simulationCommitTerminal:
				if effects[0].kind != campaignEffectProposeTerminal {
					return simulationReplayFailure(trace, "terminal commitment is not enabled at record %d", index)
				}
				var terminal terminalResult
				runtime, terminal = runtime.commitTerminal(record.runtimeCampaign)
				if !reflect.DeepEqual(terminal, record.runtimeTerminal) {
					return simulationReplayFailure(trace, "terminal commitment diverged at record %d", index)
				}
				delivered = terminalCommittedEvent{result: campaignTerminalEvidence(terminal)}
			default:
				return simulationReplayFailure(trace, "runtime operation is invalid at record %d", index)
			}
			effects = nil
			if !reflect.DeepEqual(runtime, record.runtimeState) {
				return simulationReplayFailure(trace, "runtime state diverged at record %d", index)
			}
		case simulationCampaignAuthority:
			payload := record.campaignEvent.payload
			if payload == nil {
				payload = delivered
			}
			if delivered != nil && !reflect.DeepEqual(payload, delivered) {
				return simulationReplayFailure(trace, "causal campaign fact diverged at record %d", index)
			}
			if delivered == nil && !simulationExternalFactEnabled(effects, payload) {
				return simulationReplayFailure(trace, "external campaign fact is not enabled at record %d", index)
			}
			campaign, effects = advanceCampaign(campaign, campaignEvent{
				id: campaignEventID(len(campaign.trace) + 1), payload: payload,
			})
			delivered = nil
			if !reflect.DeepEqual(campaign, record.campaignState) ||
				!reflect.DeepEqual(effects, record.campaignEffects) {
				return simulationReplayFailure(trace, "campaign transition diverged at record %d", index)
			}
		default:
			return simulationReplayFailure(trace, "authority is invalid at record %d", index)
		}
	}

	return SimulationResult{
		trace: trace,
		world: simulationWorld{campaign: campaign, runtime: runtime, supervisor: supervisorState{}},
	}
}

func simulationExternalFactEnabled(effects []campaignEffect, payload campaignEventPayload) bool {
	if len(effects) != 1 {
		return false
	}
	switch payload.(type) {
	case snapshotEstablishedEvent:
		return effects[0].kind == campaignEffectEstablishSnapshot
	case catalogueDiscoveredEvent:
		return effects[0].kind == campaignEffectDiscoverCatalogue
	case resourceSettledEvent:
		return effects[0].kind == campaignEffectReleaseSnapshot
	default:
		return false
	}
}

func simulationReplayFailure(trace simulationTrace, format string, arguments ...any) SimulationResult {
	return SimulationResult{trace: trace, failure: fmt.Errorf(format, arguments...)}
}
