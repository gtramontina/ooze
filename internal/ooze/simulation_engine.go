package ooze

import (
	"fmt"
	"slices"
	"time"
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
	source             simulationCausalSource
	effect             campaignEffect
	action             supervisorAction
	variant            uint8
	attemptKind        campaignAttemptKind
	mutant             mutantIdentity
	delivery           campaignEventPayload
	supervisorDelivery *supervisorEvent
}

type simulationEngine struct {
	definition   simulationDefinition
	campaign     campaignState
	runtime      processRuntime
	supervisor   supervisorState
	trace        simulationTrace
	pending      []simulationEngineMove
	registration campaignRegistration
	launches     map[attemptGeneration]campaignEffect
	receipts     map[attemptGeneration]observationResult
	emergency    emergencySettlement
	attempts     int
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
		trace:    simulationTrace{definition: definition},
		launches: make(map[attemptGeneration]campaignEffect),
		receipts: make(map[attemptGeneration]observationResult),
	}
	engine.enqueueEffects(effects)
	recoverySteps := 0
	recoveryBound := 32 * (1 + 2*max(1, len(definition.catalogue)))
	seenRecovery := make(map[string]struct{})
	for engine.campaign.outcome == nil && engine.campaign.failure == nil {
		moves := engine.enabledMoves()
		if len(moves) == 0 {
			return SimulationResult{trace: engine.trace, failure: simulationLivenessFailure{
				kind: simulationLivenessNoMove, live: engine.liveSources(),
			}}
		}
		selected, recovery := simulationSelectEngineMove(&engine.trace, choices, moves)
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
		if !engine.consume(move) {
			return SimulationResult{trace: engine.trace, failure: fmt.Errorf("selected simulation source is absent")}
		}
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
	moves []simulationEngineMove,
) (int, bool) {
	limit := len(moves)
	recovery := choices == nil
	if focused, ok := choices.(interface {
		chooseMove([]simulationEngineMove) int
	}); ok {
		selected := focused.chooseMove(slices.Clone(moves))
		if selected < 0 || selected >= limit {
			panic("focused simulation choice is outside the enabled set")
		}
		trace.choices = append(trace.choices, simulationChoiceRecord{
			limit: limit, selected: selected,
		})

		return selected, false
	}
	if exhaustion, ok := choices.(interface{ exhausted() bool }); ok {
		recovery = exhaustion.exhausted()
	}
	selected := 0
	if limit > 1 && choices != nil && !recovery {
		selected = choices.choose(limit)
	}
	trace.choices = append(trace.choices, simulationChoiceRecord{
		limit: limit, selected: selected, recovery: recovery,
	})

	return selected, recovery
}

func (cursor *simulationChoiceCursor) exhausted() bool {
	return cursor.at >= len(cursor.values)
}

func (engine *simulationEngine) apply(move simulationEngineMove) error {
	if move.source.kind == simulationOwnerDeliverySource {
		if move.supervisorDelivery != nil {
			return engine.applySupervisor(move.source, *move.supervisorDelivery)
		}
		return engine.applyCampaign(move.source, move.delivery)
	}
	if move.source.kind == simulationSupervisorActionSource {
		return engine.applySupervisorAction(move)
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
	case campaignEffectMaterializeWorkspace:
		engine.attempts++
		return engine.applyCampaign(move.source, workspaceMaterializedEvent{
			attempt:   move.effect.attempt,
			workspace: fmt.Sprintf("workspace-%d", engine.attempts), snapshot: move.effect.snapshot,
		})
	case campaignEffectRequestAdmission:
		request := runtimeAdmissionRequest(move.effect.request)
		var result admissionResult
		engine.runtime, result = engine.runtime.requestAdmission(request)
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeOperation:    simulationRequestAdmission,
			runtimeAdmission:    simulationTraceAdmission(request),
			runtimeState:        simulationTraceRuntimeState(engine.runtime),
			runtimeAdmissionOut: simulationTraceAdmissionResult(result),
		})
		if result.decision != admissionAccepted {
			engine.enqueueDelivery(sequence, admissionRejectedEvent{
				attempt: move.effect.attempt, result: campaignAdmissionEvidence(result),
				cause: "simulation admission rejected",
			})
			break
		}
		for _, grant := range result.deliveries {
			engine.enqueueDelivery(sequence, admissionGrantedEvent{
				attempt: grant.attempt, grant: campaignAdmissionFact(grant),
			})
		}
	case campaignEffectRequestStartCommitment:
		grant := runtimeAdmissionRequest(move.effect.grant)
		var result startCommittedResult
		engine.runtime, result = engine.runtime.startCommitted(grant)
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeOperation: simulationStartCommitted,
			runtimeGrant:     simulationTraceAdmission(grant),
			runtimeState:     simulationTraceRuntimeState(engine.runtime), runtimeStart: result,
		})
		engine.enqueueDelivery(sequence, startCommittedEvent{
			attempt: move.effect.attempt, grant: move.effect.grant, result: campaignStartEvidence(result),
		})
	case campaignEffectBindConfirmationBarrier:
		binding := runtimeBarrierBinding(move.effect.binding)
		var result barrierResult
		engine.runtime, result = engine.runtime.sealAndBindConfirmationBarrier(binding)
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeOperation:  simulationBindConfirmationBarrier,
			runtimeBarrier:    simulationTraceBarrierBinding(binding),
			runtimeState:      simulationTraceRuntimeState(engine.runtime),
			runtimeBarrierOut: simulationTraceBarrierResult(result),
		})
		engine.enqueueDelivery(sequence, confirmationBarrierBoundEvent{
			attempt: move.effect.attempt, result: campaignBarrierEvidence(result),
		})
	case campaignEffectLaunchAttempt:
		move.effect.mutant = simulationCampaignAttemptMutant(engine.campaign, move.effect.attempt)
		engine.launches[move.effect.generation] = move.effect
		registeredAt := time.Unix(int64(1_000+engine.attempts*100), 0)
		return engine.applySupervisor(move.source, supervisorEvent{
			kind: supervisorProspectiveRegistered, generation: move.effect.generation,
			attempt: move.effect.attempt, at: registeredAt, launchBy: registeredAt.Add(time.Second),
			profile: move.effect.spec.Profile, commandDeadline: move.effect.spec.Deadline,
		})
	case campaignEffectReleaseWorkspace:
		return engine.applyCampaign(move.source, resourceSettledEvent{
			kind: campaignResourceWorkspace, identity: move.effect.workspace,
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

func (engine *simulationEngine) applySupervisorAction(move simulationEngineMove) error {
	action := move.action
	switch action.kind {
	case supervisorLaunchNative:
		attempt := simulationSupervisorAttempt(engine.supervisor, action.generation)
		completedAt := attempt.launchBy.Add(-time.Nanosecond)
		kind := supervisorLaunchCompleted
		if move.variant == 1 {
			completedAt = attempt.launchBy
			kind = supervisorLaunchBoundary
		}
		completion := supervisorLaunchCompletion{
			generation: action.generation, action: action.token, at: completedAt,
			kind: supervisorLaunchReleased,
		}
		return engine.applySupervisor(move.source, supervisorEvent{
			kind: kind, generation: action.generation,
			at: completedAt, completion: &completion,
		})
	case supervisorPublishOwned:
		launch := engine.launches[action.generation]
		var result observationResult
		engine.runtime, result = engine.runtime.observeAttempt(action.generation, launchOwned{})
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeOperation: simulationObserveAttempt, runtimeGeneration: action.generation,
			runtimeObservation:    simulationTraceObservation(launchOwned{}),
			runtimeState:          simulationTraceRuntimeState(engine.runtime),
			runtimeObservationOut: simulationTraceObservationResult(result),
		})
		engine.enqueueDelivery(sequence, attemptLaunchEvent{
			attempt: launch.attempt, generation: launch.generation,
			result: campaignLaunchObservation{kind: campaignLaunchOwned}, receipt: campaignReceipt(result),
		})
	case supervisorWaitRoot, supervisorSampleRunning:
		return engine.applyHealthyRunning(move)
	case supervisorForceOwned:
		at := action.at.Add(time.Nanosecond)
		completion := supervisorDrainCompletion{
			generation: action.generation,
			action:     supervisorPendingAction{kind: action.kind, token: action.token},
			at:         at, kind: supervisorDrainForceCompleted,
		}
		return engine.applySupervisor(move.source, supervisorEvent{
			kind: supervisorDrainCompleted, generation: action.generation, at: at, drain: &completion,
		})
	case supervisorObserveEmptiness:
		at := action.drainBy.Add(-time.Nanosecond)
		kind := supervisorDrainObservedEmpty
		if move.variant == 1 {
			at = action.drainBy
			kind = supervisorDrainObservedResidual
		}
		completion := supervisorDrainCompletion{
			generation: action.generation,
			action:     supervisorPendingAction{kind: action.kind, token: action.token},
			at:         at, kind: kind,
		}
		return engine.applySupervisor(move.source, supervisorEvent{
			kind: supervisorDrainCompleted, generation: action.generation, at: at, drain: &completion,
		})
	case supervisorCaptureOutput:
		completion := supervisorOutputCompletion{
			generation: action.generation,
			action:     supervisorPendingAction{kind: action.kind, token: action.token}, at: action.at, ref: 1,
		}
		return engine.applySupervisor(move.source, supervisorEvent{
			kind: supervisorOutputCompleted, generation: action.generation, at: action.at, output: &completion,
		})
	case supervisorSealStopAdmission:
		completion := supervisorStopSealCompletion{
			generation: action.generation,
			action:     supervisorPendingAction{kind: action.kind, token: action.token}, at: action.at,
		}
		return engine.applySupervisor(move.source, supervisorEvent{
			kind: supervisorStopAdmissionSealed, generation: action.generation, at: action.at, seal: &completion,
		})
	case supervisorReleaseDomain:
		completion := supervisorReleaseCompletion{
			generation: action.generation,
			action:     supervisorPendingAction{kind: action.kind, token: action.token}, at: action.at,
		}
		return engine.applySupervisor(move.source, supervisorEvent{
			kind: supervisorReleaseCompleted, generation: action.generation, at: action.at, release: &completion,
		})
	case supervisorTransferResidualCustody:
		var receipt observationResult
		engine.runtime, receipt = engine.runtime.observeAttempt(action.generation, drainUnconfirmed{})
		engine.receipts[action.generation] = receipt
		closure := runtimeClosure{
			epoch: receipt.fatalEpoch, cancelledWaiting: receipt.cancelledWaiting,
			compensatedGrants: receipt.compensatedGrants, residual: engine.runtime.residualCustody(),
		}
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeOperation: simulationObserveAttempt, runtimeGeneration: action.generation,
			runtimeObservation:    simulationTraceObservation(drainUnconfirmed{}),
			runtimeState:          simulationTraceRuntimeState(engine.runtime),
			runtimeObservationOut: simulationTraceObservationResult(receipt),
			runtimeClosure:        simulationTraceRuntimeClosure(closure),
		})
		engine.enqueueDelivery(sequence, runtimeEmergencyStartedEvent{closure: campaignClosure(closure)})
		engine.enqueueSupervisorDelivery(sequence, supervisorEvent{
			kind: supervisorRuntimeCompleted, generation: action.generation,
			runtime: &supervisorRuntimeCompletion{
				generation: action.generation,
				action:     supervisorPendingAction{kind: action.kind, token: action.token},
				kind:       supervisorRuntimeClosurePending,
			},
		})
		emergencyAt := action.at.Add(time.Nanosecond)
		engine.enqueueSupervisorDelivery(sequence, supervisorEvent{
			kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: emergencyAt.Add(5 * time.Second),
			emergencySnapshots: simulationEmergencySnapshots(engine.supervisor),
		})
	case supervisorSettleEmergency:
		resolutions, acknowledged, residuals := normalizeSupervisorEmergencyResolutions(action.resolutions)
		var settlement emergencySettlement
		engine.runtime, settlement = engine.runtime.settleEmergency(emergencySweep{resolutions: resolutions})
		validateSupervisorRuntimeSettlement(settlement, acknowledged, residuals)
		engine.emergency = settlement
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeOperation: simulationSettleEmergency,
			runtimeSweep: simulationTraceEmergencySweep(emergencySweep{resolutions: resolutions}),
			runtimeState: simulationTraceRuntimeState(engine.runtime),
			runtimeEmergencyOut: simulationTraceEmergencySettlement(settlement),
		})
		engine.enqueueSupervisorDelivery(sequence, supervisorEvent{
			kind: supervisorEmergencySettlementCompleted,
			emergencySettlement: &supervisorEmergencySettlementCompletion{
				action: engine.supervisor.emergency.pendingAction,
				acknowledged: acknowledged, residuals: residuals,
			},
		})
	case supervisorDeliverEmergencySettlement:
		return engine.applyCampaign(move.source, runtimeEmergencySettledEvent{
			epoch: engine.emergency.epoch, settlement: campaignSettlement(engine.emergency),
		})
	case supervisorSettleRuntime:
		observation := terminalObservation(action.terminal)
		var receipt observationResult
		engine.runtime, receipt = engine.runtime.observeAttempt(action.generation, observation)
		engine.receipts[action.generation] = receipt
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeOperation: simulationObserveAttempt, runtimeGeneration: action.generation,
			runtimeObservation:    simulationTraceObservation(observation),
			runtimeState:          simulationTraceRuntimeState(engine.runtime),
			runtimeObservationOut: simulationTraceObservationResult(receipt),
		})
		engine.enqueueAdmissionDeliveries(sequence, receipt.deliveries)
		kind := supervisorRuntimeAcknowledged
		if receipt.confirmationProvisional {
			kind = supervisorRuntimeProvisionalDeadline
		}
		completion := supervisorRuntimeCompletion{
			generation: action.generation,
			action:     supervisorPendingAction{kind: action.kind, token: action.token}, kind: kind,
		}
		engine.enqueueSupervisorDelivery(sequence, supervisorEvent{
			kind: supervisorRuntimeCompleted, generation: action.generation, runtime: &completion,
		})
	case supervisorDeliverTerminal:
		launch := engine.launches[action.generation]
		receipt := engine.receipts[action.generation]
		terminal := publicTerminal(action.terminal, func(supervisorOutputRef) string { return "" }, nil, action.runtimeKind)
		event := attemptTerminalEvent{
			attempt: launch.attempt, generation: action.generation,
			terminal: terminal, receipt: campaignReceipt(receipt),
		}
		if launch.attemptKind == campaignAttemptBaseline {
			event.resolvedMutationDeadline = resolveBaselineMutationDeadline(
				terminalExecutionData(terminal).CommandDuration, engine.definition.campaign.peers,
			)
		}
		if launch.completesConfirmationQueue {
			var completed confirmationQueueResult
			engine.runtime, completed = engine.runtime.completeConfirmationQueue(engine.registration.token)
			sequence := engine.append(simulationRecord{
				authority: simulationRuntimeAuthority, source: move.source,
				runtimeOperation: simulationCompleteConfirmationQueue,
				runtimeCampaign:  engine.registration.token,
				runtimeState:     simulationTraceRuntimeState(engine.runtime),
				runtimeQueueOut:  simulationTraceConfirmationQueueResult(completed),
			})
			event.receipt.confirmationQueueDrained = completed.decision == confirmationQueueCompleted
			engine.enqueueAdmissionDeliveries(sequence, completed.deliveries)
			engine.enqueueDelivery(sequence, event)

			return nil
		}
		return engine.applyCampaign(move.source, event)
	default:
		return fmt.Errorf("simulation engine supervisor action %v is not implemented", action.kind)
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

func (engine *simulationEngine) applySupervisor(
	source simulationCausalSource,
	event supervisorEvent,
) error {
	state, actions := reduceSupervisor(engine.supervisor, event)
	engine.supervisor = state
	record := simulationRecord{
		authority: simulationSupervisorAuthority, source: source,
		supervisorEvent:   simulationTraceSupervisorEvent(event),
		supervisorState:   simulationTraceSupervisorState(state),
		supervisorActions: simulationTraceSupervisorActions(actions),
	}
	engine.append(record)
	engine.enqueueActions(actions)

	return nil
}

func (engine *simulationEngine) applyHealthyRunning(move simulationEngineMove) error {
	attempt := simulationSupervisorAttempt(engine.supervisor, move.action.generation)
	var wait, sample supervisorAction
	for index := 0; index < len(engine.pending); {
		candidate := engine.pending[index]
		if candidate.source.kind != simulationSupervisorActionSource ||
			candidate.action.generation != move.action.generation ||
			(candidate.action.kind != supervisorWaitRoot && candidate.action.kind != supervisorSampleRunning) {
			index++
			continue
		}
		if candidate.action.kind == supervisorWaitRoot {
			wait = candidate.action
		} else {
			sample = candidate.action
		}
		engine.pending = slices.Delete(engine.pending, index, index+1)
	}
	if move.action.kind == supervisorWaitRoot {
		wait = move.action
	} else {
		sample = move.action
	}
	if wait.token == 0 {
		return fmt.Errorf("simulation healthy running move has no wait action")
	}
	observedAt := attempt.startedAt.Add(time.Second)
	drainBy := observedAt.Add(5 * time.Second)
	bundle := &supervisorRunningBundle{
		generation: move.action.generation, sampleAction: sample.token, waitAction: wait.token,
	}
	switch move.variant {
	case 0, 1:
		exitCode := 0
		if move.variant == 1 {
			exitCode = 1
		}
		bundle.facts = []supervisorRunningFact{{
			generation: move.action.generation, action: wait.token,
			kind: supervisorRunningRootExited, at: observedAt, exitCode: exitCode,
		}}
	case 2:
		observedAt = attempt.deadlineAt
		drainBy = observedAt.Add(5 * time.Second)
		bundle.exitRecheck = supervisorExitRecheck{performed: true, at: observedAt}
	case 3:
		if sample.token == 0 {
			return fmt.Errorf("simulation fuse move has no running sample action")
		}
		bundle.facts = []supervisorRunningFact{{
			generation: move.action.generation, action: sample.token,
			kind: supervisorRunningFuseObserved, at: observedAt, rootLive: true, live: supervisorFuseCeiling + 1,
		}}
	default:
		return fmt.Errorf("simulation running variant %d is invalid", move.variant)
	}
	return engine.applySupervisor(move.source, supervisorEvent{
		kind: supervisorRunningObserved, generation: move.action.generation, at: observedAt, drainBy: drainBy,
		running: bundle,
	})
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

func (engine *simulationEngine) enqueueActions(actions []supervisorAction) {
	for _, enabled := range simulationEnabledMoves(nil, actions, engine.definition.catalogue) {
		engine.pending = append(engine.pending, simulationEngineMove{
			source: simulationCausalSource{
				kind: simulationSupervisorActionSource, identity: uint64(enabled.action.token),
			},
			action: enabled.action,
		})
	}
}

func (engine *simulationEngine) enqueueDelivery(sequence uint64, payload campaignEventPayload) {
	engine.pending = append(engine.pending, simulationEngineMove{
		source:   simulationCausalSource{kind: simulationOwnerDeliverySource, identity: sequence},
		delivery: payload,
	})
}

func (engine *simulationEngine) enqueueAdmissionDeliveries(
	sequence uint64,
	deliveries []admissionGrant,
) {
	for _, grant := range deliveries {
		engine.enqueueDelivery(sequence, admissionGrantedEvent{
			attempt: grant.attempt, grant: campaignAdmissionFact(grant),
		})
	}
}

func (engine *simulationEngine) enqueueSupervisorDelivery(sequence uint64, event supervisorEvent) {
	engine.pending = append(engine.pending, simulationEngineMove{
		source:             simulationCausalSource{kind: simulationOwnerDeliverySource, identity: sequence},
		supervisorDelivery: &event,
	})
}

func (engine simulationEngine) enabledMoves() []simulationEngineMove {
	moves := make([]simulationEngineMove, 0, len(engine.pending)+2)
	for _, move := range engine.pending {
		if move.source.kind == simulationSupervisorActionSource {
			launch := engine.launches[move.action.generation]
			move.attemptKind, move.mutant = launch.attemptKind, launch.mutant
		}
		if move.source.kind == simulationSupervisorActionSource &&
			move.action.kind == supervisorSampleRunning {
			continue
		}
		moves = append(moves, move)
		if move.source.kind != simulationSupervisorActionSource {
			continue
		}
		switch move.action.kind {
		case supervisorLaunchNative, supervisorWaitRoot, supervisorObserveEmptiness:
			alternative := move
			alternative.variant = 1
			moves = append(moves, alternative)
			if move.action.kind == supervisorWaitRoot {
				deadline := move
				deadline.variant = 2
				moves = append(moves, deadline)
				attempt := simulationSupervisorAttempt(engine.supervisor, move.action.generation)
				if attempt.profile == AutomaticProfile {
					fuse := move
					fuse.variant = 3
					moves = append(moves, fuse)
				}
			}
		}
	}

	return moves
}

func (engine *simulationEngine) consume(selected simulationEngineMove) bool {
	for index, pending := range engine.pending {
		if pending.source != selected.source || pending.effect.id != selected.effect.id ||
			pending.action.token != selected.action.token {
			continue
		}
		engine.pending = slices.Delete(engine.pending, index, index+1)

		return true
	}

	return false
}

func (engine simulationEngine) liveSources() []simulationCausalSource {
	sources := make([]simulationCausalSource, len(engine.pending))
	for index, move := range engine.pending {
		sources[index] = move.source
	}
	return sources
}

func simulationSupervisorAttempt(state supervisorState, generation attemptGeneration) supervisorAttemptState {
	for _, attempt := range state.attempts {
		if attempt.generation == generation {
			return attempt
		}
	}
	panic("simulation supervisor attempt is absent")
}

func simulationCampaignAttemptMutant(state campaignState, identity attemptIdentity) mutantIdentity {
	for _, attempt := range state.attempts {
		if attempt.identity == identity {
			return attempt.mutant
		}
	}
	panic("simulation campaign attempt is absent")
}

func simulationEmergencySnapshots(state supervisorState) []supervisorEmergencySnapshot {
	snapshots := make([]supervisorEmergencySnapshot, 0, len(state.attempts))
	for _, attempt := range state.attempts {
		if attempt.phase != supervisorLaunchClosedNotReleased {
			snapshots = append(snapshots, supervisorEmergencySnapshot{generation: attempt.generation})
		}
	}

	return snapshots
}
