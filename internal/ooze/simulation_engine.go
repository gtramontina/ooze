package ooze

import (
	"fmt"
	"reflect"
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
	kind        simulationLivenessKind
	live        []simulationCausalSource
	diagnostics []string
}

func (failure simulationLivenessFailure) Error() string {
	return fmt.Sprintf("simulation liveness failure kind=%d live=%v pending=%v",
		failure.kind, failure.live, failure.diagnostics)
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
	for (engine.campaign.outcome == nil && engine.campaign.failure == nil) || len(engine.pending) != 0 {
		moves := engine.enabledMoves()
		if len(moves) == 0 {
			return simulationLivenessResult(engine, simulationLivenessNoMove)
		}
		selected, recovery := simulationSelectEngineMove(&engine.trace, choices, moves)
		if recovery {
			recoverySteps++
			if recoverySteps > recoveryBound {
				return simulationLivenessResult(engine, simulationLivenessRecoveryBound)
			}
			cut := fmt.Sprintf("%#v|%#v|%#v", simulationTraceCampaignState(engine.campaign),
				simulationTraceRuntimeState(engine.runtime), engine.pending)
			if _, found := seenRecovery[cut]; found {
				return simulationLivenessResult(engine, simulationLivenessRepeatedWorld)
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
		return simulationLivenessResult(engine, simulationLivenessNoMove)
	}

	return SimulationResult{
		trace: engine.trace,
		world: simulationWorld{
			campaign: engine.campaign, runtime: engine.runtime,
			supervisor: simulationProjectSupervisorState(engine.supervisor),
		},
	}
}

func simulationLivenessResult(engine simulationEngine, kind simulationLivenessKind) SimulationResult {
	live := engine.liveSources()
	ordinals := make(map[simulationCausalSourceKind]int)
	identities := make([]string, len(live))
	for index, source := range live {
		ordinals[source.kind]++
		identities[index] = fmt.Sprintf("owner-source-%d#%d", source.kind, ordinals[source.kind])
	}
	diagnostics := make([]string, len(engine.pending))
	for index, move := range engine.pending {
		supervisorKind := supervisorEventKind(0)
		if move.supervisorDelivery != nil {
			supervisorKind = move.supervisorDelivery.kind
		}
		diagnostics[index] = fmt.Sprintf("source=%v effect=%d action=%d campaign=%T supervisor=%d",
			move.source, move.effect.kind, move.action.kind, move.delivery, supervisorKind)
	}
	failure := simulationLivenessFailure{kind: kind, live: live, diagnostics: diagnostics}

	return SimulationResult{
		trace: engine.trace,
		world: simulationWorld{
			campaign: engine.campaign, runtime: engine.runtime,
			supervisor: simulationProjectSupervisorState(engine.supervisor),
		},
		key: FailureKey{
			property: "Explore", kind: simulationLivenessFailureKind,
			liveness: kind, identities: identities,
		},
		failure: failure,
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
	if move.delivery != nil {
		return engine.applyCampaign(move.source, move.delivery)
	}
	if move.source.kind == simulationOwnerDeliverySource {
		if move.supervisorDelivery != nil {
			return engine.applySupervisor(move.source, *move.supervisorDelivery)
		}
		return fmt.Errorf("simulation owner delivery is absent")
	}
	if move.source.kind == simulationSupervisorActionSource {
		return engine.applySupervisorAction(move)
	}
	if move.source.kind != simulationCampaignEffectSource || move.effect.id == 0 ||
		uint64(move.effect.id) != move.source.identity {
		return fmt.Errorf("simulation move source=%v effect=%d/%d is invalid",
			move.source, move.effect.id, move.effect.kind)
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
	case campaignEffectReturnAdmission:
		grant := runtimeAdmissionRequest(move.effect.grant)
		var result admissionResult
		engine.runtime, result = engine.runtime.acknowledgeGrantReturn(grant)
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeOperation:    simulationAcknowledgeGrantReturn,
			runtimeGrant:        simulationTraceAdmission(grant),
			runtimeState:        simulationTraceRuntimeState(engine.runtime),
			runtimeAdmissionOut: simulationTraceAdmissionResult(result),
		})
		engine.enqueueDelivery(sequence, grantReturnAcknowledgedEvent{
			grant: move.effect.grant, result: campaignAdmissionEvidence(result),
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
	case campaignEffectStopAttempt:
		attempt, found := simulationSupervisorAttemptIfPresent(engine.supervisor, move.effect.generation)
		if !found {
			if _, registered := engine.launches[move.effect.generation]; registered {
				return nil
			}

			return fmt.Errorf("simulation stop effect has no registered generation %d", move.effect.generation)
		}
		if attempt.phase >= supervisorReleasingDomain {
			return nil
		}
		if attempt.phase != supervisorRunning && attempt.phase != supervisorIntentLatched &&
			attempt.phase != supervisorEmergencyDraining {
			return fmt.Errorf("simulation stop effect reached phase %d before stop admission sealed", attempt.phase)
		}
		at := attempt.lastEventAt.Add(time.Nanosecond)
		drainBy := at.Add(5 * time.Second)
		return engine.applySupervisor(move.source, supervisorEvent{
			kind: supervisorRunningObserved, generation: move.effect.generation, at: at, drainBy: drainBy,
			running: &supervisorRunningBundle{
				generation: move.effect.generation,
				waitAction: attempt.waitAction, sampleAction: attempt.sampleAction,
				facts: []supervisorRunningFact{{
					generation: move.effect.generation, kind: supervisorRunningStopRequested,
					at: at, stop: StopRequest{At: at, DrainBy: drainBy},
				}},
			},
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
		operation := simulationCommitTerminal
		if move.effect.fatalEpoch == 0 {
			engine.runtime, terminal = engine.runtime.commitTerminal(engine.registration.token)
		} else {
			operation = simulationAuthorizeForcedAbort
			engine.runtime, terminal = engine.runtime.authorizeForcedAbort(
				engine.registration.token, move.effect.fatalEpoch,
			)
		}
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeOperation: operation, runtimeCampaign: engine.registration.token,
			runtimeFatalEpoch: move.effect.fatalEpoch,
			runtimeState:      simulationTraceRuntimeState(engine.runtime), runtimeTerminal: terminal,
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
		var drainBy time.Time
		kind := supervisorLaunchCompleted
		if move.variant == 1 {
			completedAt = attempt.launchBy
			kind = supervisorLaunchBoundary
		}
		if move.variant == 2 {
			if err := engine.applySupervisor(move.source, supervisorEvent{
				kind: supervisorLaunchBoundary, generation: action.generation, at: attempt.launchBy,
			}); err != nil {
				return err
			}
			completedAt = attempt.launchBy.Add(time.Nanosecond)
			drainBy = completedAt.Add(5 * time.Second)
		}
		completion := supervisorLaunchCompletion{
			generation: action.generation, action: action.token, at: completedAt,
			kind: supervisorLaunchReleased,
		}
		return engine.applySupervisor(move.source, supervisorEvent{
			kind: kind, generation: action.generation,
			at: completedAt, drainBy: drainBy, completion: &completion,
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
	case supervisorPublishNotReleased, supervisorPublishLaunchUnconfirmed:
		launch := engine.launches[action.generation]
		observation := attemptObservation(launchUnconfirmed{})
		launchResult := campaignLaunchObservation{
			kind: campaignLaunchUnconfirmed, residual: ProspectiveUnresolved,
		}
		if action.kind == supervisorPublishNotReleased {
			observation = launchObservationFromAction(action)
			launchResult = campaignLaunchObservation{
				kind: campaignLaunchNotReleased, failure: action.launchFailure,
			}
		}
		var result observationResult
		engine.runtime, result = engine.runtime.observeAttempt(action.generation, observation)
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeOperation: simulationObserveAttempt, runtimeGeneration: action.generation,
			runtimeObservation:    simulationTraceObservation(observation),
			runtimeState:          simulationTraceRuntimeState(engine.runtime),
			runtimeObservationOut: simulationTraceObservationResult(result),
		})
		engine.enqueueDelivery(sequence, attemptLaunchEvent{
			attempt: launch.attempt, generation: launch.generation,
			result: launchResult, receipt: campaignReceipt(result),
		})
		if action.kind == supervisorPublishLaunchUnconfirmed && result.runtimeClosureInProgress &&
			!engine.supervisor.emergency.active && !engine.hasPendingSupervisorEmergency() {
			emergencyAt := action.at.Add(time.Nanosecond)
			engine.enqueueSupervisorDelivery(sequence, supervisorEvent{
				kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: emergencyAt.Add(5 * time.Second),
			})
		}
	case supervisorCloseProspective, supervisorAdoptOwned:
		observation := attemptObservation(launchOwned{})
		if action.kind == supervisorCloseProspective {
			observation = launchObservationFromAction(action)
		}
		var result observationResult
		engine.runtime, result = engine.runtime.observeAttempt(action.generation, observation)
		engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeOperation: simulationObserveAttempt, runtimeGeneration: action.generation,
			runtimeObservation:    simulationTraceObservation(observation),
			runtimeState:          simulationTraceRuntimeState(engine.runtime),
			runtimeObservationOut: simulationTraceObservationResult(result),
		})
	case supervisorRevokeLaunchRelease:
		return nil
	case supervisorWaitRoot, supervisorSampleRunning:
		return engine.applyHealthyRunning(move)
	case supervisorForceOwned:
		at := simulationCompletionAt(engine.supervisor, action.generation, action.at.Add(time.Nanosecond))
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
		if move.variant == 1 || move.variant == 2 {
			at = action.drainBy
			kind = supervisorDrainObservedResidual
		}
		if move.variant == 2 {
			at = action.drainBy.Add(time.Nanosecond)
		}
		at = simulationCompletionAt(engine.supervisor, action.generation, at)
		completion := supervisorDrainCompletion{
			generation: action.generation,
			action:     supervisorPendingAction{kind: action.kind, token: action.token},
			at:         at, kind: kind,
		}
		return engine.applySupervisor(move.source, supervisorEvent{
			kind: supervisorDrainCompleted, generation: action.generation, at: at, drain: &completion,
		})
	case supervisorCaptureOutput:
		at := simulationCompletionAt(engine.supervisor, action.generation, action.at)
		completion := supervisorOutputCompletion{
			generation: action.generation, action: supervisorPendingAction{kind: action.kind, token: action.token},
			at: at, ref: 1,
		}
		return engine.applySupervisor(move.source, supervisorEvent{
			kind: supervisorOutputCompleted, generation: action.generation, at: at, output: &completion,
		})
	case supervisorSealStopAdmission:
		at := simulationCompletionAt(engine.supervisor, action.generation, action.at)
		completion := supervisorStopSealCompletion{
			generation: action.generation, action: supervisorPendingAction{kind: action.kind, token: action.token},
			at: at,
		}
		return engine.applySupervisor(move.source, supervisorEvent{
			kind: supervisorStopAdmissionSealed, generation: action.generation, at: at, seal: &completion,
		})
	case supervisorReleaseDomain:
		at := simulationCompletionAt(engine.supervisor, action.generation, action.at)
		completion := supervisorReleaseCompletion{
			generation: action.generation, action: supervisorPendingAction{kind: action.kind, token: action.token},
			at: at,
		}
		return engine.applySupervisor(move.source, supervisorEvent{
			kind: supervisorReleaseCompleted, generation: action.generation, at: at, release: &completion,
		})
	case supervisorTransferResidualCustody:
		wasOpen := engine.runtime.open()
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
		engine.enqueueSupervisorDelivery(sequence, supervisorEvent{
			kind: supervisorRuntimeCompleted, generation: action.generation,
			runtime: &supervisorRuntimeCompletion{
				generation: action.generation,
				action:     supervisorPendingAction{kind: action.kind, token: action.token},
				kind:       supervisorRuntimeClosurePending,
			},
		})
		if wasOpen {
			emergencyAt := action.at.Add(time.Nanosecond)
			engine.enqueueSupervisorDelivery(sequence, supervisorEvent{
				kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: emergencyAt.Add(5 * time.Second),
			})
		}
	case supervisorSettleEmergency:
		resolutions, acknowledged, residuals := normalizeSupervisorEmergencyResolutions(action.resolutions)
		if runtimeResiduals := engine.runtime.residualCustody(); len(resolutions) != len(runtimeResiduals) {
			return fmt.Errorf(
				"simulation emergency action %d resolves generations %v with runtime residuals %v",
				action.token, acknowledged, runtimeResiduals,
			)
		}
		var settlement emergencySettlement
		engine.runtime, settlement = engine.runtime.settleEmergency(emergencySweep{resolutions: resolutions})
		validateSupervisorRuntimeSettlement(settlement, acknowledged, residuals)
		engine.emergency = settlement
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeOperation:    simulationSettleEmergency,
			runtimeSweep:        simulationTraceEmergencySweep(emergencySweep{resolutions: resolutions}),
			runtimeState:        simulationTraceRuntimeState(engine.runtime),
			runtimeEmergencyOut: simulationTraceEmergencySettlement(settlement),
		})
		engine.enqueueSupervisorDelivery(sequence, supervisorEvent{
			kind: supervisorEmergencySettlementCompleted,
			emergencySettlement: &supervisorEmergencySettlementCompletion{
				action:       engine.supervisor.emergency.pendingAction,
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
		kind := supervisorRuntimeClosurePending
		if receipt.settlementAcknowledged {
			kind = supervisorRuntimeAcknowledged
			if receipt.confirmationProvisional {
				kind = supervisorRuntimeProvisionalDeadline
			}
		} else if !receipt.runtimeClosureInProgress {
			return fmt.Errorf("simulation runtime returned no terminal disposition")
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
		engine.pending = append(engine.pending, simulationEngineMove{
			source: move.source, action: action, delivery: event,
		})

		return nil
	default:
		return fmt.Errorf("simulation engine supervisor action %v is not implemented", action.kind)
	}

	return nil
}

func (engine *simulationEngine) applyCampaign(source simulationCausalSource, payload campaignEventPayload) error {
	state, effects := simulationAdvanceCampaign(engine.campaign, payload)
	engine.campaign = state
	if _, settled := payload.(runtimeEmergencySettledEvent); settled {
		engine.retireCampaignTerminals()
	}
	record := simulationCampaignRecord(engine.trace, state, effects, payload)
	record.source = source
	engine.trace.records = append(engine.trace.records, record)
	engine.enqueueEffects(effects)

	return nil
}

func (engine *simulationEngine) retireCampaignTerminals() {
	for index := 0; index < len(engine.pending); {
		move := engine.pending[index]
		if move.source.kind != simulationSupervisorActionSource ||
			move.action.kind != supervisorDeliverTerminal {
			index++
			continue
		}
		engine.pending = slices.Delete(engine.pending, index, index+1)
	}
}

func (engine *simulationEngine) applySupervisor(
	source simulationCausalSource,
	event supervisorEvent,
) error {
	if event.kind == supervisorEmergencyStarted && source.kind == simulationOwnerDeliverySource {
		event.at = simulationEmergencyAt(engine.supervisor, event.at)
		event.drainBy = event.at.Add(5 * time.Second)
		event.emergencySnapshots = simulationEmergencySnapshots(engine.supervisor, event.at)
	}
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
	case 4:
		observedAt = attempt.deadlineAt.Add(time.Nanosecond)
		drainBy = attempt.deadlineAt.Add(5 * time.Second)
		bundle.exitRecheck = supervisorExitRecheck{performed: true, at: attempt.deadlineAt}
		bundle.facts = []supervisorRunningFact{{
			generation: move.action.generation, action: wait.token,
			kind: supervisorRunningRootExited, at: observedAt,
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
	firstRuntimeCustodyAction := engine.firstRuntimeCustodyAction()
	for _, move := range engine.pending {
		if move.effect.kind == campaignEffectStopAttempt && !engine.supervisorAcceptsStop(move.effect.generation) {
			continue
		}
		if move.source.kind == simulationSupervisorActionSource &&
			move.action.token != engine.firstSupervisorAction(move.action.generation) {
			continue
		}
		if move.source.kind == simulationOwnerDeliverySource && move.delivery != nil &&
			engine.hasPendingCampaignEffect() {
			continue
		}
		if move.delivery != nil && simulationAttemptTerminalDelivery(move.delivery) &&
			engine.campaignEmergencyRequested() {
			continue
		}
		if move.source.kind == simulationOwnerDeliverySource && move.supervisorDelivery != nil &&
			move.supervisorDelivery.kind == supervisorEmergencyStarted &&
			(!engine.campaignEmergencyRequested() ||
				!simulationEmergencyCutReady(engine.supervisor, move.supervisorDelivery.at) ||
				!engine.emergencyCampaignCutReady()) {
			continue
		}
		if token := engine.runtimeCustodyToken(move); token != 0 && token != firstRuntimeCustodyAction {
			continue
		}
		if move.action.kind == supervisorDeliverEmergencySettlement {
			if _, requested := engine.campaign.runtimeEmergencySettlementRequest(); !requested ||
				engine.hasPendingEmergencyCampaignIngress() {
				continue
			}
		}
		if move.source.kind == simulationSupervisorActionSource &&
			move.action.kind == supervisorDeliverTerminal &&
			engine.hasPendingLaunchDelivery(move.action.generation) {
			continue
		}
		if move.source.kind == simulationSupervisorActionSource &&
			move.action.kind == supervisorTransferResidualCustody &&
			!simulationRuntimeOwnsResidualTransfer(engine.runtime, move.action.generation) {
			continue
		}
		if move.source.kind == simulationSupervisorActionSource &&
			move.action.kind == supervisorSettleRuntime &&
			!simulationRuntimeOwnsTerminalSettlement(engine.runtime, move.action.generation) {
			continue
		}
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
		case supervisorLaunchNative, supervisorObserveEmptiness:
			alternative := move
			alternative.variant = 1
			moves = append(moves, alternative)
			after := move
			after.variant = 2
			moves = append(moves, after)
		case supervisorWaitRoot:
			for _, variant := range []uint8{1, 2, 4} {
				alternative := move
				alternative.variant = variant
				moves = append(moves, alternative)
			}
			attempt := simulationSupervisorAttempt(engine.supervisor, move.action.generation)
			if attempt.profile == AutomaticProfile {
				fuse := move
				fuse.variant = 3
				moves = append(moves, fuse)
			}
		}
	}

	return moves
}

func (engine simulationEngine) supervisorAcceptsStop(generation attemptGeneration) bool {
	for _, attempt := range engine.supervisor.attempts {
		if attempt.generation != generation {
			continue
		}

		return attempt.phase == supervisorRunning || attempt.phase == supervisorIntentLatched ||
			attempt.phase == supervisorEmergencyDraining || attempt.phase >= supervisorReleasingDomain
	}

	_, registered := engine.launches[generation]

	return registered
}

func (engine simulationEngine) firstSupervisorAction(generation attemptGeneration) supervisorActionToken {
	var first supervisorActionToken
	for _, move := range engine.pending {
		if move.source.kind != simulationSupervisorActionSource || move.action.generation != generation ||
			(first != 0 && move.action.token >= first) {
			continue
		}
		first = move.action.token
	}

	return first
}

func (engine simulationEngine) firstRuntimeCustodyAction() supervisorActionToken {
	var first supervisorActionToken
	for _, move := range engine.pending {
		token := engine.runtimeCustodyToken(move)
		if token == 0 || (first != 0 && token >= first) {
			continue
		}
		first = token
	}

	return first
}

func (engine simulationEngine) runtimeCustodyToken(move simulationEngineMove) supervisorActionToken {
	if simulationMutatesRuntimeCustody(move.action.kind) {
		return move.action.token
	}
	if move.source.kind != simulationOwnerDeliverySource || move.supervisorDelivery == nil ||
		(move.supervisorDelivery.kind != supervisorRuntimeCompleted &&
			move.supervisorDelivery.kind != supervisorEmergencySettlementCompleted) {
		return 0
	}
	for _, record := range engine.trace.records {
		if record.sequence == move.source.identity && record.source.kind == simulationSupervisorActionSource {
			return supervisorActionToken(record.source.identity)
		}
	}

	return 0
}

func simulationMutatesRuntimeCustody(kind supervisorActionKind) bool {
	return kind == supervisorTransferResidualCustody || kind == supervisorSettleRuntime ||
		kind == supervisorSettleEmergency
}

func simulationRuntimeOwnsResidualTransfer(runtime processRuntime, generation attemptGeneration) bool {
	index := runtime.admissionIndexByGeneration(generation)
	if index < 0 {
		return false
	}
	admission := runtime.admissions[index]

	return admission.stage == admissionOwned && runtime.lifecycle <= runtimeFatalClosing &&
		(admission.disposition == dispositionNone || admission.disposition == dispositionFatalSeeded)
}

func simulationRuntimeOwnsTerminalSettlement(runtime processRuntime, generation attemptGeneration) bool {
	index := runtime.admissionIndexByGeneration(generation)
	if index < 0 {
		return false
	}
	admission := runtime.admissions[index]

	return admission.stage == admissionOwned && admission.disposition == dispositionNone &&
		(runtime.lifecycle == runtimeOpen || runtime.lifecycle == runtimeFatalClosing)
}

func (engine simulationEngine) hasPendingLaunchDelivery(generation attemptGeneration) bool {
	return slices.ContainsFunc(engine.pending, func(move simulationEngineMove) bool {
		launch, ok := move.delivery.(attemptLaunchEvent)

		return ok && launch.generation == generation
	})
}

func (engine simulationEngine) hasPendingSupervisorEmergency() bool {
	return slices.ContainsFunc(engine.pending, func(move simulationEngineMove) bool {
		return move.supervisorDelivery != nil && move.supervisorDelivery.kind == supervisorEmergencyStarted
	})
}

func (engine *simulationEngine) consume(selected simulationEngineMove) bool {
	for index, pending := range engine.pending {
		if !simulationSamePendingMove(pending, selected) {
			continue
		}
		engine.pending = slices.Delete(engine.pending, index, index+1)

		return true
	}

	return false
}

func simulationSamePendingMove(pending, selected simulationEngineMove) bool {
	if pending.source != selected.source {
		return false
	}
	switch selected.source.kind {
	case simulationCampaignEffectSource:
		return pending.effect.id == selected.effect.id
	case simulationSupervisorActionSource:
		return pending.action.token == selected.action.token
	case simulationOwnerDeliverySource:
		return reflect.DeepEqual(pending.delivery, selected.delivery) &&
			reflect.DeepEqual(pending.supervisorDelivery, selected.supervisorDelivery)
	default:
		return false
	}
}

func (engine simulationEngine) liveSources() []simulationCausalSource {
	sources := make([]simulationCausalSource, len(engine.pending))
	for index, move := range engine.pending {
		sources[index] = move.source
	}
	return sources
}

func simulationSupervisorAttempt(state supervisorState, generation attemptGeneration) supervisorAttemptState {
	attempt, found := simulationSupervisorAttemptIfPresent(state, generation)
	if found {
		return attempt
	}
	panic("simulation supervisor attempt is absent")
}

func simulationSupervisorAttemptIfPresent(
	state supervisorState,
	generation attemptGeneration,
) (supervisorAttemptState, bool) {
	for _, attempt := range state.attempts {
		if attempt.generation == generation {
			return attempt, true
		}
	}

	return supervisorAttemptState{}, false
}

func simulationCompletionAt(
	state supervisorState,
	generation attemptGeneration,
	at time.Time,
) time.Time {
	attempt := simulationSupervisorAttempt(state, generation)
	if attempt.lastEventAt.After(at) {
		return attempt.lastEventAt
	}

	return at
}

func simulationCampaignAttemptMutant(state campaignState, identity attemptIdentity) mutantIdentity {
	for _, attempt := range state.attempts {
		if attempt.identity == identity {
			return attempt.mutant
		}
	}
	panic("simulation campaign attempt is absent")
}

func simulationEmergencyAt(state supervisorState, at time.Time) time.Time {
	for _, attempt := range state.attempts {
		if attempt.lastEventAt.After(at) {
			at = attempt.lastEventAt
		}
	}

	return at
}

func simulationEmergencyCutReady(state supervisorState, at time.Time) bool {
	at = simulationEmergencyAt(state, at)
	for _, attempt := range state.attempts {
		if attempt.phase == supervisorLaunchEstablishing && at.After(attempt.launchBy) {
			return false
		}
	}

	return true
}

func (engine simulationEngine) emergencyCampaignCutReady() bool {
	for _, move := range engine.pending {
		if move.source.kind == simulationCampaignEffectSource &&
			move.effect.kind != campaignEffectProposeTerminal {
			return false
		}
		if committed, ok := move.delivery.(startCommittedEvent); ok &&
			committed.result.decision == startCommittedAccepted {
			return false
		}
	}

	return true
}

func (engine simulationEngine) hasPendingCampaignEffect() bool {
	return slices.ContainsFunc(engine.pending, func(move simulationEngineMove) bool {
		return move.source.kind == simulationCampaignEffectSource
	})
}

func (engine simulationEngine) hasPendingEmergencyCampaignIngress() bool {
	return slices.ContainsFunc(engine.pending, func(move simulationEngineMove) bool {
		return move.delivery != nil && !simulationAttemptTerminalDelivery(move.delivery)
	})
}

func simulationAttemptTerminalDelivery(payload campaignEventPayload) bool {
	_, terminal := payload.(attemptTerminalEvent)

	return terminal
}

func (engine simulationEngine) campaignEmergencyRequested() bool {
	_, requested := engine.campaign.runtimeEmergencySettlementRequest()

	return requested
}

func simulationEmergencySnapshots(state supervisorState, at time.Time) []supervisorEmergencySnapshot {
	snapshots := make([]supervisorEmergencySnapshot, 0, len(state.attempts))
	for _, attempt := range state.attempts {
		if attempt.phase == supervisorLaunchClosedNotReleased {
			continue
		}
		snapshot := supervisorEmergencySnapshot{generation: attempt.generation}
		if attempt.phase == supervisorRunning || attempt.phase == supervisorIntentLatched {
			snapshot.running = &supervisorRunningBundle{
				generation: attempt.generation, waitAction: attempt.waitAction, sampleAction: attempt.sampleAction,
			}
			if attempt.phase == supervisorRunning && !at.Before(attempt.deadlineAt) {
				snapshot.running.exitRecheck = supervisorExitRecheck{
					performed: true, at: attempt.deadlineAt,
				}
				snapshot.running.drainBy = attempt.deadlineAt.Add(5 * time.Second)
			}
		}
		snapshots = append(snapshots, snapshot)
	}

	return snapshots
}
