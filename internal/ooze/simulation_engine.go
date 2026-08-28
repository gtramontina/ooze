package ooze

import (
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
)

type simulationCausalSourceKind uint8

const (
	simulationCampaignEffectSource simulationCausalSourceKind = iota + 1
	supervisionActionSource
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
	variant            simulationMoveVariant
	attemptKind        campaignAttemptKind
	mutant             mutantIdentity
	delivery           campaignEventPayload
	supervisorDelivery *supervisorEvent
}

type simulationMoveVariant struct {
	launch  simulationLaunchVariant
	running simulationRunningVariant
	drain   simulationDrainVariant
}

type simulationLaunchVariant uint8

const (
	simulationLaunchAtBoundary simulationLaunchVariant = iota + 1
	simulationLaunchAfterBoundary
	simulationLaunchProvenNotReleased
)

type simulationRunningVariant uint8

const (
	simulationRunningFailed simulationRunningVariant = iota + 1
	simulationRunningAtDeadline
	simulationRunningFuse
	simulationRunningAfterDeadline
)

type simulationDrainVariant uint8

const (
	simulationDrainAtBoundary simulationDrainVariant = iota + 1
	simulationDrainAfterBoundary
)

type simulationEngine struct {
	definition   simulationDefinition
	campaign     campaignState
	runtime      processruntime.Replay
	supervisor   supervisorState
	machine      *supervisorMachine
	trace        simulationTrace
	pending      []simulationEngineMove
	registration campaignRegistration
	launches     map[attemptGeneration]campaignEffect
	receipts     map[attemptGeneration]observationResult
	emergency    emergencySettlement
	runtimeCut   processruntime.RecordedCut
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
		definition: definition, campaign: campaign, runtime: processruntime.NewReplay(definition.capacity),
		machine:  newSupervisorMachine(),
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
			campaign: engine.campaign, runtime: simulationTraceRuntimeState(engine.runtime), runtimeState: engine.runtime,
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
			campaign: engine.campaign, runtime: simulationTraceRuntimeState(engine.runtime), runtimeState: engine.runtime,
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
	if move.source.kind == supervisionActionSource {
		return engine.applySupervisorAction(move)
	}
	if move.source.kind != simulationCampaignEffectSource || move.effect.id == 0 ||
		uint64(move.effect.id) != move.source.identity {
		return fmt.Errorf("simulation move source=%v effect=%d/%d is invalid",
			move.source, move.effect.id, move.effect.kind)
	}
	switch move.effect.kind {
	case campaignEffectRegister:
		registered := engine.applyRuntime(processruntime.RegisterCampaignCut(engine.definition.campaign.lineage)).Registration()
		registration := campaignRegistrationEvidence(registered)
		engine.registration = registration
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
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
		processed := engine.applyRuntime(processruntime.RequestAdmissionCut(processRuntimeAdmission(campaignAdmissionValue(request)))).Admission()
		result := runtimeAdmissionResult(processed)
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		if result.decision != processruntime.AdmissionAccepted {
			engine.enqueueDelivery(sequence, admissionRejectedEvent{
				attempt: move.effect.attempt, result: campaignAdmissionEvidence(result),
				cause: "simulation admission rejected",
			})
			break
		}
		for _, grant := range result.deliveries {
			engine.enqueueDelivery(sequence, admissionGrantedEvent{
				attempt: grant.attempt, grant: campaignAdmissionValue(grant),
			})
		}
	case campaignEffectCancelAdmission:
		request := runtimeAdmissionRequest(move.effect.request)
		processed := engine.applyRuntime(processruntime.CancelAdmissionCut(processRuntimeAdmission(campaignAdmissionValue(request)))).Admission()
		result := runtimeAdmissionResult(processed)
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		engine.enqueueDelivery(sequence, admissionCancelledEvent{
			attempt: move.effect.attempt, request: move.effect.request,
			result: campaignAdmissionEvidence(result),
		})
	case campaignEffectRequestStartCommitment:
		grant := runtimeAdmissionRequest(move.effect.grant)
		processed := engine.applyRuntime(processruntime.CommitStartCut(processRuntimeAdmission(campaignAdmissionValue(grant)))).Start()
		result := runtimeStartResult(processed)
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		engine.enqueueDelivery(sequence, startCommittedEvent{
			attempt: move.effect.attempt, grant: move.effect.grant, result: campaignStartEvidence(result),
		})
	case campaignEffectReturnAdmission:
		grant := runtimeAdmissionRequest(move.effect.grant)
		cut := processruntime.ReturnGrantCut(processRuntimeAdmission(campaignAdmissionValue(grant)))
		if !engine.runtime.Accepts(cut) {
			return fmt.Errorf("simulation grant return %v has no returnable runtime authority", move.effect.grant)
		}
		processed := engine.applyRuntime(cut).Admission()
		result := runtimeAdmissionResult(processed)
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		engine.enqueueDelivery(sequence, grantReturnAcknowledgedEvent{
			grant: move.effect.grant, result: campaignAdmissionEvidence(result),
		})
	case campaignEffectBindConfirmationBarrier:
		binding := runtimeBarrierBinding(move.effect.binding)
		processed := engine.applyRuntime(processruntime.BindConfirmationBarrierCut(processruntime.Barrier{
			Campaign: binding.campaign, Attempt: string(binding.attempt), Profile: binding.profile, Deadline: binding.deadline,
		})).Barrier()
		result := runtimeBarrierResult(processed)
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
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
		attempt, found := supervisionAttemptIfPresent(engine.supervisor, move.effect.generation)
		if !found {
			if _, registered := engine.launches[move.effect.generation]; registered {
				return nil
			}

			return fmt.Errorf("simulation stop effect has no registered generation %d", move.effect.generation)
		}
		if supervisionStopResolved(attempt.phase) {
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
		if move.effect.fatalEpoch == 0 {
			processed := engine.applyRuntime(processruntime.CommitTerminalCut(engine.registration.token)).Terminal()
			terminal = terminalResult{decision: processed.Decision()}
		} else {
			processed := engine.applyRuntime(processruntime.AuthorizeForcedAbortCut(
				engine.registration.token, uint64(move.effect.fatalEpoch),
			)).Terminal()
			terminal = terminalResult{decision: processed.Decision()}
		}
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		engine.enqueueDelivery(sequence, terminalCommittedEvent{result: campaignTerminalEvidence(terminal)})
	default:
		return fmt.Errorf("simulation engine effect %v is not implemented", move.effect.kind)
	}

	return nil
}

func (engine *simulationEngine) applyRuntime(cut processruntime.Cut) processruntime.ReplayResult {
	next, result := engine.runtime.Apply(cut)
	engine.runtime = next
	engine.runtimeCut = result.RecordedCut()
	return result
}

func (engine *simulationEngine) applySupervisorAction(move simulationEngineMove) error {
	action := move.action
	switch action.kind {
	case supervisorLaunchNative:
		attempt := supervisionAttempt(engine.supervisor, action.generation)
		completedAt := attempt.launchBy.Add(-time.Nanosecond)
		var drainBy time.Time
		kind := supervisorLaunchCompleted
		completionKind := supervisorLaunchReleased
		var failure LaunchFailure
		if move.variant.launch == simulationLaunchAtBoundary {
			completedAt = attempt.launchBy
			kind = supervisorLaunchBoundary
		}
		if move.variant.launch == simulationLaunchAfterBoundary {
			if err := engine.applySupervisor(move.source, supervisorEvent{
				kind: supervisorLaunchBoundary, generation: action.generation, at: attempt.launchBy,
			}); err != nil {
				return err
			}
			completedAt = attempt.launchBy.Add(time.Nanosecond)
			drainBy = completedAt.Add(5 * time.Second)
		}
		if move.variant.launch == simulationLaunchProvenNotReleased {
			completionKind = supervisorLaunchProvenNotReleased
			failure = LaunchFailed
		}
		completion := supervisorLaunchCompletion{
			generation: action.generation, action: action.token, at: completedAt,
			kind: completionKind, failure: failure,
		}
		return engine.applySupervisor(move.source, supervisorEvent{
			kind: kind, generation: action.generation,
			at: completedAt, drainBy: drainBy, completion: &completion,
		})
	case supervisorPublishOwned:
		launch := engine.launches[action.generation]
		processed := engine.applyRuntime(processruntime.ObserveAttemptCut(action.generation, processruntime.Owned())).Receipt()
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		engine.enqueueDelivery(sequence, attemptLaunchEvent{
			attempt: launch.attempt, generation: launch.generation,
			result: campaignLaunchObservation{kind: campaignLaunchOwned}, receipt: campaignReceipt(processed),
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
		processed := engine.applyRuntime(processruntime.ObserveAttemptCut(action.generation, processRuntimeObservation(observation))).Receipt()
		result := runtimeReceipt(processed)
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		engine.enqueueDelivery(sequence, attemptLaunchEvent{
			attempt: launch.attempt, generation: launch.generation,
			result: launchResult, receipt: campaignReceipt(processed),
		})
		if action.kind == supervisorPublishLaunchUnconfirmed && result.runtimeClosureInProgress &&
			!engine.supervisor.emergency.active && !engine.hasPendingSupervisorEmergency() {
			emergencyAt := action.at.Add(time.Nanosecond)
			engine.enqueueSupervisorDelivery(sequence, supervisorEvent{
				kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: emergencyAt.Add(5 * time.Second),
			})
		}
	case supervisorCloseProspective:
		observation := launchObservationFromAction(action)
		processed := engine.applyRuntime(processruntime.ObserveAttemptCut(action.generation, processRuntimeObservation(observation))).Receipt()
		result := runtimeReceipt(processed)
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		engine.enqueueAdmissionDeliveries(sequence, result.deliveries)
		completion := supervisorRuntimeCompletion{
			generation: action.generation,
			action:     supervisorPendingAction{kind: action.kind, token: action.token},
			kind:       normalizedSupervisorRuntimeReceipt(processed),
		}
		engine.enqueueSupervisorDelivery(sequence, supervisorEvent{
			kind: supervisorRuntimeCompleted, generation: action.generation, runtime: &completion,
		})
	case supervisorAdoptOwned:
		observation := attemptObservation(launchOwned{})
		engine.applyRuntime(processruntime.ObserveAttemptCut(action.generation, processRuntimeObservation(observation)))
		engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
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
		if move.variant.drain == simulationDrainAtBoundary ||
			move.variant.drain == simulationDrainAfterBoundary {
			at = action.drainBy
			kind = supervisorDrainObservedResidual
		}
		if move.variant.drain == simulationDrainAfterBoundary {
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
		wasOpen := engine.runtime.Projection().Open()
		processed := engine.applyRuntime(processruntime.ObserveAttemptCut(action.generation, processruntime.DrainUnconfirmed())).Receipt()
		receipt := runtimeReceipt(processed)
		engine.receipts[action.generation] = receipt
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
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
		if runtimeResiduals := engine.runtime.Projection().Residual(); len(resolutions) != len(runtimeResiduals) {
			return fmt.Errorf(
				"simulation emergency action %d resolves generations %v with runtime residuals %v",
				action.token, acknowledged, runtimeResiduals,
			)
		}
		processed := engine.applyRuntime(processruntime.SettleEmergencyCut(processRuntimeResolutions(emergencySweep{resolutions: resolutions}))).Settlement()
		settlement := runtimeEmergencySettlement(processed)
		validateSupervisorRuntimeSettlement(settlement, acknowledged, residuals)
		engine.emergency = settlement
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
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
			epoch: engine.emergency.epoch, settlement: campaignSettlementValue(engine.emergency),
		})
	case supervisorSettleRuntime:
		observation := terminalObservation(action.terminal)
		processed := engine.applyRuntime(processruntime.ObserveAttemptCut(action.generation, processRuntimeObservation(observation))).Receipt()
		receipt := runtimeReceipt(processed)
		engine.receipts[action.generation] = receipt
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		engine.enqueueAdmissionDeliveries(sequence, receipt.deliveries)
		completion := supervisorRuntimeCompletion{
			generation: action.generation,
			action:     supervisorPendingAction{kind: action.kind, token: action.token},
			kind:       normalizedSupervisorRuntimeReceipt(processed),
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
			terminal: terminal, receipt: campaignReceiptValue(receipt),
		}
		if launch.attemptKind == campaignAttemptBaseline {
			event.resolvedMutationDeadline = resolveBaselineMutationDeadline(
				terminalExecutionData(terminal).CommandDuration, engine.definition.campaign.peers,
			)
		}
		if launch.completesConfirmationQueue {
			processed := engine.applyRuntime(processruntime.CompleteConfirmationQueueCut(engine.registration.token)).Queue()
			completed := runtimeQueueResult(processed)
			sequence := engine.append(simulationRecord{
				authority: simulationRuntimeAuthority, source: move.source,
				runtimeState: simulationTraceRuntimeState(engine.runtime),
			})
			event.receipt.confirmationQueueDrained = completed.decision == processruntime.ConfirmationQueueCompleted
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

func (engine *simulationEngine) applyCampaign(
	source simulationCausalSource,
	payload campaignEventPayload,
) (failure error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			failure = fmt.Errorf("simulation campaign payload %T/%#v failed: %v", payload, payload, recovered)
		}
	}()
	state, effects := simulationAdvanceCampaign(engine.campaign, payload)
	engine.campaign = state
	engine.retireSupersededAdmissionWork()
	if _, settled := payload.(runtimeEmergencySettledEvent); settled {
		engine.retireCampaignTerminals()
	}
	record := simulationCampaignRecord(engine.trace, state, effects, payload)
	record.source = source
	engine.trace.records = append(engine.trace.records, record)
	engine.enqueueEffects(effects)

	return nil
}

func (engine *simulationEngine) retireSupersededAdmissionWork() {
	for index := 0; index < len(engine.pending); {
		move := engine.pending[index]
		attempt, stages := move.effect.attempt, []campaignAttemptStage(nil)
		switch event := move.delivery.(type) {
		case admissionGrantedEvent:
			attempt = event.attempt
			stages = []campaignAttemptStage{campaignAttemptAdmissionWaiting}
		case admissionCancelledEvent:
			attempt = event.attempt
			stages = []campaignAttemptStage{campaignAttemptAdmissionWaiting, campaignAttemptGranted}
		case startCommittedEvent:
			attempt = event.attempt
			stages = []campaignAttemptStage{campaignAttemptGranted, campaignAttemptReturningGrant}
		}
		switch move.effect.kind {
		case campaignEffectCancelAdmission:
			stages = []campaignAttemptStage{campaignAttemptAdmissionWaiting, campaignAttemptGranted}
		case campaignEffectRequestStartCommitment:
			stages = []campaignAttemptStage{campaignAttemptGranted, campaignAttemptReturningGrant}
		}
		if len(stages) == 0 {
			index++
			continue
		}
		attemptAt := engine.campaign.attemptIndex(attempt)
		if attemptAt >= 0 && slices.Contains(stages, engine.campaign.attempts[attemptAt].stage) {
			index++
			continue
		}
		engine.pending = slices.Delete(engine.pending, index, index+1)
	}
}

func (engine *simulationEngine) retireCampaignTerminals() {
	for index := 0; index < len(engine.pending); {
		move := engine.pending[index]
		if move.source.kind != supervisionActionSource ||
			move.action.kind != supervisorDeliverTerminal {
			index++
			continue
		}
		receipt, observed := engine.receipts[move.action.generation]
		if observed && !receipt.runtimeClosureInProgress {
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
		fact, ready := engine.machine.PrepareEmergency(event.at, event.at.Add(5*time.Second))
		if !ready {
			return fmt.Errorf("simulation emergency fact is not enabled")
		}
		event = fact.production()
	}
	if engine.machine == nil {
		engine.machine = newSupervisorMachineFrom(engine.supervisor)
	}
	var transition supervisorTransition
	engine.machine, transition = engine.machine.Apply(supervisionFactFromEvent(event))
	accepted := transition.Event().Fact()
	actions := transition.actions()
	engine.supervisor = engine.machine.snapshot()
	record := simulationRecord{
		authority: supervisionAuthority, source: source,
		supervisorEvent:   accepted,
		supervisorState:   engine.machine.Projection(),
		supervisorActions: transition.Effects(),
	}
	engine.append(record)
	engine.enqueueActions(actions)

	return nil
}

func (engine *simulationEngine) applyHealthyRunning(move simulationEngineMove) error {
	attempt := supervisionAttempt(engine.supervisor, move.action.generation)
	var wait, sample supervisorAction
	for index := 0; index < len(engine.pending); {
		candidate := engine.pending[index]
		if candidate.source.kind != supervisionActionSource ||
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
	switch move.variant.running {
	case 0, simulationRunningFailed:
		exitCode := 0
		if move.variant.running == simulationRunningFailed {
			exitCode = 1
		}
		bundle.facts = []supervisorRunningFact{{
			generation: move.action.generation, action: wait.token,
			kind: supervisorRunningRootExited, at: observedAt, exitCode: exitCode,
		}}
	case simulationRunningAtDeadline:
		observedAt = attempt.deadlineAt
		drainBy = observedAt.Add(5 * time.Second)
		bundle.exitRecheck = supervisorExitRecheck{performed: true, at: observedAt}
	case simulationRunningFuse:
		if sample.token == 0 {
			return fmt.Errorf("simulation fuse move has no running sample action")
		}
		bundle.facts = []supervisorRunningFact{{
			generation: move.action.generation, action: sample.token,
			kind: supervisorRunningFuseObserved, at: observedAt, rootLive: true, live: supervisorFuseCeiling + 1,
		}}
	case simulationRunningAfterDeadline:
		observedAt = attempt.deadlineAt.Add(time.Nanosecond)
		drainBy = attempt.deadlineAt.Add(5 * time.Second)
		bundle.exitRecheck = supervisorExitRecheck{performed: true, at: attempt.deadlineAt}
		bundle.facts = []supervisorRunningFact{{
			generation: move.action.generation, action: wait.token,
			kind: supervisorRunningRootExited, at: observedAt,
		}}
	default:
		return fmt.Errorf("simulation running variant %d is invalid", move.variant.running)
	}
	return engine.applySupervisor(move.source, supervisorEvent{
		kind: supervisorRunningObserved, generation: move.action.generation, at: observedAt, drainBy: drainBy,
		running: bundle,
	})
}

func (engine *simulationEngine) append(record simulationRecord) uint64 {
	if record.authority == simulationRuntimeAuthority {
		record.runtimeCut = engine.runtimeCut
		engine.runtimeCut = processruntime.RecordedCut{}
	}
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
				kind: supervisionActionSource, identity: uint64(enabled.action.token),
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
			attempt: grant.attempt, grant: campaignAdmissionValue(grant),
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
		if move.source.kind == simulationCampaignEffectSource && move.effect.attempt != "" &&
			move.effect.id != engine.firstCampaignEffect(move.effect.attempt) {
			continue
		}
		if move.source.kind == supervisionActionSource &&
			move.action.token != engine.firstSupervisorAction(move.action.generation) {
			continue
		}
		if move.source.kind == simulationOwnerDeliverySource && move.delivery != nil &&
			engine.hasPendingCampaignEffect() && !engine.deliveryPrecedesPendingAttemptEffect(move.delivery) {
			continue
		}
		if terminal, ok := move.delivery.(attemptTerminalEvent); ok &&
			terminal.receipt.runtimeClosureInProgress && engine.campaignEmergencyRequested() {
			continue
		}
		if launch, ok := move.delivery.(attemptLaunchEvent); ok &&
			launch.result.kind == campaignLaunchNotReleased && launch.receipt.runtimeClosureInProgress &&
			!engine.campaignEmergencyRequested() {
			continue
		}
		if move.source.kind == simulationOwnerDeliverySource && move.supervisorDelivery != nil &&
			move.supervisorDelivery.kind == supervisorEmergencyStarted &&
			(!engine.campaignEmergencyRequested() ||
				!engine.supervisorEmergencyReady(move.supervisorDelivery.at) ||
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
		if move.source.kind == supervisionActionSource &&
			move.action.kind == supervisorDeliverTerminal &&
			engine.hasPendingLaunchDelivery(move.action.generation) {
			continue
		}
		if move.source.kind == supervisionActionSource &&
			move.action.kind == supervisorTransferResidualCustody &&
			!engine.runtime.Accepts(processruntime.ObserveAttemptCut(
				move.action.generation, processruntime.DrainUnconfirmed(),
			)) {
			continue
		}
		if move.source.kind == supervisionActionSource &&
			move.action.kind == supervisorSettleRuntime &&
			!engine.runtime.Accepts(processruntime.ObserveAttemptCut(
				move.action.generation, processruntime.Settled(processruntime.AutomaticProfile, 0),
			)) {
			continue
		}
		if move.source.kind == supervisionActionSource {
			launch := engine.launches[move.action.generation]
			move.attemptKind, move.mutant = launch.attemptKind, launch.mutant
		}
		if move.source.kind == supervisionActionSource &&
			move.action.kind == supervisorSampleRunning {
			continue
		}
		moves = append(moves, move)
		if move.source.kind != supervisionActionSource {
			continue
		}
		switch move.action.kind {
		case supervisorLaunchNative, supervisorObserveEmptiness:
			alternative := move
			if move.action.kind == supervisorLaunchNative {
				alternative.variant.launch = simulationLaunchAtBoundary
			} else {
				alternative.variant.drain = simulationDrainAtBoundary
			}
			moves = append(moves, alternative)
			after := move
			if move.action.kind == supervisorLaunchNative {
				after.variant.launch = simulationLaunchAfterBoundary
			} else {
				after.variant.drain = simulationDrainAfterBoundary
			}
			moves = append(moves, after)
			if move.action.kind == supervisorLaunchNative {
				notReleased := move
				notReleased.variant.launch = simulationLaunchProvenNotReleased
				moves = append(moves, notReleased)
			}
		case supervisorWaitRoot:
			for _, variant := range []simulationRunningVariant{
				simulationRunningFailed, simulationRunningAtDeadline, simulationRunningAfterDeadline,
			} {
				alternative := move
				alternative.variant.running = variant
				moves = append(moves, alternative)
			}
			attempt := supervisionAttempt(engine.supervisor, move.action.generation)
			if attempt.profile == AutomaticProfile {
				fuse := move
				fuse.variant.running = simulationRunningFuse
				moves = append(moves, fuse)
			}
		}
	}

	return moves
}

func (engine simulationEngine) supervisorEmergencyReady(at time.Time) bool {
	machine := engine.machine
	if machine == nil {
		machine = newSupervisorMachineFrom(engine.supervisor)
	}
	_, ready := machine.PrepareEmergency(at, at.Add(5*time.Second))

	return ready
}

func (engine simulationEngine) supervisorAcceptsStop(generation attemptGeneration) bool {
	for _, attempt := range engine.supervisor.attempts {
		if attempt.generation != generation {
			continue
		}

		return attempt.phase == supervisorRunning || attempt.phase == supervisorIntentLatched ||
			attempt.phase == supervisorEmergencyDraining || supervisionStopResolved(attempt.phase)
	}

	_, registered := engine.launches[generation]

	return registered
}

func supervisionStopResolved(phase supervisorAttemptPhase) bool {
	switch phase {
	case supervisorReleasingDomain, supervisorTransferringResidualCustody,
		supervisorSettlingRuntime, supervisorAwaitingEmergencySettlement:
		return true
	default:
		return false
	}
}

func (engine simulationEngine) firstSupervisorAction(generation attemptGeneration) supervisorActionToken {
	var first supervisorActionToken
	for _, move := range engine.pending {
		if move.source.kind != supervisionActionSource || move.action.generation != generation ||
			(first != 0 && move.action.token >= first) {
			continue
		}
		first = move.action.token
	}

	return first
}

func (engine simulationEngine) firstCampaignEffect(attempt attemptIdentity) campaignEffectID {
	var first campaignEffectID
	for _, move := range engine.pending {
		if move.source.kind != simulationCampaignEffectSource || move.effect.attempt != attempt ||
			(first != 0 && move.effect.id >= first) {
			continue
		}
		first = move.effect.id
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
		if record.sequence == move.source.identity && record.source.kind == supervisionActionSource {
			return supervisorActionToken(record.source.identity)
		}
	}

	return 0
}

func simulationMutatesRuntimeCustody(kind supervisorActionKind) bool {
	return kind == supervisorTransferResidualCustody || kind == supervisorSettleRuntime ||
		kind == supervisorSettleEmergency
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
	case supervisionActionSource:
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

func supervisionAttempt(state supervisorState, generation attemptGeneration) supervisorAttemptState {
	attempt, found := supervisionAttemptIfPresent(state, generation)
	if found {
		return attempt
	}
	panic("simulation supervisor attempt is absent")
}

func supervisionAttemptIfPresent(
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
	attempt := supervisionAttempt(state, generation)
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

func (engine simulationEngine) emergencyCampaignCutReady() bool {
	for _, move := range engine.pending {
		if move.source.kind == simulationCampaignEffectSource &&
			move.effect.kind != campaignEffectProposeTerminal {
			return false
		}
		if move.source.kind == supervisionActionSource &&
			(move.action.kind == supervisorPublishNotReleased || move.action.kind == supervisorCloseProspective) {
			return false
		}
		if committed, ok := move.delivery.(startCommittedEvent); ok &&
			committed.result.decision == processruntime.StartAccepted {
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

func (engine simulationEngine) deliveryPrecedesPendingAttemptEffect(payload campaignEventPayload) bool {
	attempt := simulationCampaignDeliveryAttempt(payload)
	if attempt == "" {
		return false
	}

	return slices.ContainsFunc(engine.pending, func(move simulationEngineMove) bool {
		return move.source.kind == simulationCampaignEffectSource && move.effect.attempt == attempt
	})
}

func simulationCampaignDeliveryAttempt(payload campaignEventPayload) attemptIdentity {
	switch event := payload.(type) {
	case workspaceMaterializedEvent:
		return event.attempt
	case workspaceMaterializationFailedEvent:
		return event.attempt
	case admissionGrantedEvent:
		return event.attempt
	case admissionCancelledEvent:
		return event.attempt
	case admissionRejectedEvent:
		return event.attempt
	case startCommittedEvent:
		return event.attempt
	case attemptLaunchEvent:
		return event.attempt
	case attemptTerminalEvent:
		return event.attempt
	case confirmationBarrierBoundEvent:
		return event.attempt
	case grantReturnAcknowledgedEvent:
		return event.grant.attempt
	default:
		return ""
	}
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
