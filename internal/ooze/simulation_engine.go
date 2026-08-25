package ooze

import (
	"fmt"
	"slices"
)

type simulationCausalSourceKind uint8

const (
	simulationCampaignEffectSource simulationCausalSourceKind = iota + 1
	simulationSupervisorActionSource
	simulationOwnerDeliverySource
)

type simulationCausalSource struct {
	kind     simulationCausalSourceKind
	identity uint64
}

type simulationEngineMove struct {
	source   simulationCausalSource
	effect   campaignEffect
	delivery campaignEventPayload
}

type simulationEngine struct {
	definition   simulationDefinition
	campaign     campaignState
	runtime      processRuntime
	supervisor   supervisorState
	trace        simulationTrace
	pending      []simulationEngineMove
	registration campaignRegistration
}

type simulationLivenessKind uint8

const (
	simulationLivenessNoMove simulationLivenessKind = iota + 1
	simulationLivenessRepeatedWorld
	simulationLivenessRecoveryBound
)

type simulationLivenessFailure struct {
	kind simulationLivenessKind
	live []simulationCausalSource
}

func (failure simulationLivenessFailure) Error() string {
	return fmt.Sprintf("simulation liveness failure kind=%d live=%v", failure.kind, failure.live)
}

func simulationExploreEngine(
	definition simulationDefinition,
	choices simulationChoiceSource,
) SimulationResult {
	campaign, effects := beginCampaign(definition.campaign)
	engine := simulationEngine{
		definition: definition, campaign: campaign, runtime: newProcessRuntime(definition.capacity),
		trace: simulationTrace{definition: definition},
	}
	engine.enqueueEffects(effects)
	recoverySteps := 0
	recoveryBound := 32 * (1 + 2*max(1, len(definition.catalogue)))
	seenRecovery := make(map[string]struct{})
	for engine.campaign.outcome == nil {
		moves := slices.Clone(engine.pending)
		if len(moves) == 0 {
			return SimulationResult{trace: engine.trace, failure: simulationLivenessFailure{
				kind: simulationLivenessNoMove, live: engine.liveSources(),
			}}
		}
		selected, recovery := simulationSelectEngineMove(&engine.trace, choices, len(moves))
		if recovery {
			recoverySteps++
			if recoverySteps > recoveryBound {
				return SimulationResult{trace: engine.trace, failure: simulationLivenessFailure{
					kind: simulationLivenessRecoveryBound, live: engine.liveSources(),
				}}
			}
			cut := fmt.Sprintf("%#v|%#v|%#v", simulationTraceCampaignState(engine.campaign),
				simulationTraceRuntimeState(engine.runtime), engine.liveSources())
			if _, found := seenRecovery[cut]; found {
				return SimulationResult{trace: engine.trace, failure: simulationLivenessFailure{
					kind: simulationLivenessRepeatedWorld, live: engine.liveSources(),
				}}
			}
			seenRecovery[cut] = struct{}{}
		}
		move := moves[selected]
		engine.pending = slices.Delete(engine.pending, selected, selected+1)
		if failure := engine.apply(move); failure != nil {
			return SimulationResult{trace: engine.trace, failure: failure}
		}
	}
	if len(engine.pending) != 0 {
		return SimulationResult{trace: engine.trace, failure: simulationLivenessFailure{
			kind: simulationLivenessNoMove, live: engine.liveSources(),
		}}
	}

	return SimulationResult{
		trace: engine.trace,
		world: simulationWorld{
			campaign: engine.campaign, runtime: engine.runtime,
			supervisor: simulationProjectSupervisorState(engine.supervisor),
		},
	}
}

func simulationSelectEngineMove(
	trace *simulationTrace,
	choices simulationChoiceSource,
	limit int,
) (int, bool) {
	recovery := choices == nil
	if cursor, ok := choices.(*simulationChoiceCursor); ok {
		recovery = cursor.at >= len(cursor.values)
	}
	selected := 0
	if choices != nil && !recovery {
		selected = choices.choose(limit)
	}
	trace.choices = append(trace.choices, simulationChoiceRecord{
		limit: limit, selected: selected, recovery: recovery,
	})

	return selected, recovery
}

func (engine *simulationEngine) apply(move simulationEngineMove) error {
	if move.source.kind == simulationOwnerDeliverySource {
		return engine.applyCampaign(move.source, move.delivery)
	}
	if move.source.kind != simulationCampaignEffectSource || move.effect.id == 0 ||
		uint64(move.effect.id) != move.source.identity {
		return fmt.Errorf("simulation move source is invalid")
	}
	switch move.effect.kind {
	case campaignEffectRegister:
		var registration campaignRegistration
		engine.runtime, registration = engine.runtime.registerCampaign(
			campaignProvenance{lineage: engine.definition.campaign.lineage},
		)
		engine.registration = registration
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeOperation:  simulationRegisterCampaign,
			runtimeProvenance: campaignProvenance{lineage: engine.definition.campaign.lineage},
			runtimeState:      simulationTraceRuntimeState(engine.runtime), runtimeRegistration: registration,
		})
		engine.enqueueDelivery(sequence, campaignRegisteredEvent{registration: registration})
	case campaignEffectEstablishSnapshot:
		return engine.applyCampaign(move.source, snapshotEstablishedEvent{snapshot: "snapshot-1"})
	case campaignEffectDiscoverCatalogue:
		return engine.applyCampaign(move.source, catalogueDiscoveredEvent{
			snapshot: move.effect.snapshot, mutants: slices.Clone(engine.definition.catalogue),
		})
	case campaignEffectReleaseSnapshot:
		return engine.applyCampaign(move.source, resourceSettledEvent{
			kind: campaignResourceSnapshot, identity: string(move.effect.snapshot),
		})
	case campaignEffectProposeTerminal:
		var terminal terminalResult
		engine.runtime, terminal = engine.runtime.commitTerminal(engine.registration.token)
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeOperation: simulationCommitTerminal, runtimeCampaign: engine.registration.token,
			runtimeState: simulationTraceRuntimeState(engine.runtime), runtimeTerminal: terminal,
		})
		engine.enqueueDelivery(sequence, terminalCommittedEvent{result: campaignTerminalEvidence(terminal)})
	default:
		return fmt.Errorf("simulation engine effect %v is not implemented", move.effect.kind)
	}

	return nil
}

func (engine *simulationEngine) applyCampaign(source simulationCausalSource, payload campaignEventPayload) error {
	state, effects := simulationAdvanceCampaign(engine.campaign, payload)
	engine.campaign = state
	record := simulationCampaignRecord(engine.trace, state, effects, payload)
	record.source = source
	engine.trace.records = append(engine.trace.records, record)
	engine.enqueueEffects(effects)

	return nil
}

func (engine *simulationEngine) append(record simulationRecord) uint64 {
	record.sequence = uint64(len(engine.trace.records) + 1)
	engine.trace.records = append(engine.trace.records, record)

	return record.sequence
}

func (engine *simulationEngine) enqueueEffects(effects []campaignEffect) {
	for _, enabled := range simulationEnabledMoves(effects, nil, engine.definition.catalogue) {
		engine.pending = append(engine.pending, simulationEngineMove{
			source: simulationCausalSource{
				kind: simulationCampaignEffectSource, identity: uint64(enabled.effect.id),
			},
			effect: enabled.effect,
		})
	}
}

func (engine *simulationEngine) enqueueDelivery(sequence uint64, payload campaignEventPayload) {
	engine.pending = append(engine.pending, simulationEngineMove{
		source:   simulationCausalSource{kind: simulationOwnerDeliverySource, identity: sequence},
		delivery: payload,
	})
}

func (engine simulationEngine) liveSources() []simulationCausalSource {
	sources := make([]simulationCausalSource, len(engine.pending))
	for index, move := range engine.pending {
		sources[index] = move.source
	}
	return sources
}
