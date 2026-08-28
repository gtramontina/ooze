package ooze

import (
	"fmt"
	"reflect"
	"slices"
	"time"

	campaignmodule "github.com/gtramontina/ooze/internal/ooze/internal/campaign"
	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
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
	effect             campaignmodule.Effect
	action             supervision.Effect
	variant            simulationMoveVariant
	attemptKind        campaignmodule.AttemptRole
	mutant             mutantIdentity
	delivery           campaignmodule.Fact
	supervisorDelivery *supervision.Fact
}

type simulationMoveVariant struct {
	launch  simulationLaunchVariant
	running simulationRunningVariant
	drain   simulationDrainVariant
}

type simulationLaunchVariant = supervision.LaunchOutcome

const (
	simulationLaunchAtBoundary        = supervision.LaunchReleasedAtBoundary
	simulationLaunchAfterBoundary     = supervision.LaunchReleasedAfterBoundary
	simulationLaunchProvenNotReleased = supervision.LaunchProvenNotReleased
)

type simulationRunningVariant = supervision.RunningOutcome

const (
	simulationRunningFailed        = supervision.RunningFailed
	simulationRunningAtDeadline    = supervision.RunningAtDeadline
	simulationRunningFuse          = supervision.RunningFuse
	simulationRunningAfterDeadline = supervision.RunningAfterDeadline
)

type simulationDrainVariant = supervision.CompletionPosition

const (
	simulationDrainAtBoundary    = supervision.CompletionAtBoundary
	simulationDrainAfterBoundary = supervision.CompletionAfterBoundary
)

type simulationEngine struct {
	definition   simulationDefinition
	campaign     campaignmodule.Machine
	runtime      processruntime.Replay
	machine      *supervision.Machine
	trace        simulationTrace
	pending      []simulationEngineMove
	registration campaignRegistration
	launches     map[attemptGeneration]campaignmodule.Effect
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
	campaign, initial := campaignmodule.NewMachine(definition.campaign)
	engine := simulationEngine{
		definition: definition, campaign: campaign, runtime: processruntime.NewReplay(definition.capacity),
		machine:  supervision.NewMachine(),
		trace:    simulationTrace{definition: definition},
		launches: make(map[attemptGeneration]campaignmodule.Effect),
		receipts: make(map[attemptGeneration]observationResult),
	}
	engine.enqueueEffects(initial.Effects())
	recoverySteps := 0
	recoveryBound := 32 * (1 + 2*max(1, len(definition.catalogue)))
	seenRecovery := make(map[string]struct{})
	for !engine.campaign.Projection().Settled() || len(engine.pending) != 0 {
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
			cut := fmt.Sprintf("%#v|%#v|%#v", engine.campaign.Projection(),
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
			campaign: engine.campaign.Projection().Canonical().Fork(),
			runtime:  simulationTraceRuntimeState(engine.runtime), runtimeState: engine.runtime,
			supervisor: engine.machine.Projection(), machine: engine.machine,
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
		supervisorKind := supervision.FactKind(0)
		if move.supervisorDelivery != nil {
			supervisorKind = move.supervisorDelivery.Kind()
		}
		diagnostics[index] = fmt.Sprintf("source=%v effect=%d action=%d campaign=%T supervisor=%d",
			move.source, move.effect.Kind(), move.action.Kind(), move.delivery, supervisorKind)
	}
	failure := simulationLivenessFailure{kind: kind, live: live, diagnostics: diagnostics}

	return SimulationResult{
		trace: engine.trace,
		world: simulationWorld{
			campaign: engine.campaign.Projection().Canonical().Fork(),
			runtime:  simulationTraceRuntimeState(engine.runtime), runtimeState: engine.runtime,
			supervisor: engine.machine.Projection(), machine: engine.machine,
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
	if !move.delivery.IsZero() {
		return engine.applyCampaign(move.source, move.delivery)
	}
	if move.source.kind == simulationOwnerDeliverySource {
		if move.supervisorDelivery != nil {
			return engine.applySupervisorFact(move.source, *move.supervisorDelivery)
		}
		return fmt.Errorf("simulation owner delivery is absent")
	}
	if move.source.kind == supervisionActionSource {
		return engine.applySupervisorAction(move)
	}
	if move.source.kind != simulationCampaignEffectSource || move.effect.ID() == 0 ||
		uint64(move.effect.ID()) != move.source.identity {
		return fmt.Errorf("simulation move source=%v effect=%d/%d is invalid",
			move.source, move.effect.ID(), move.effect.Kind())
	}
	switch move.effect.Kind() {
	case campaignEffectRegister:
		cut, _ := move.effect.RuntimeCut(engine.definition.campaign, engine.campaign.Campaign())
		registered := engine.applyRuntime(cut).Registration()
		engine.registration = registered
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		engine.enqueueDelivery(sequence, campaignmodule.Registered(registered))
	case campaignEffectEstablishSnapshot:
		return engine.applyCampaign(move.source, campaignmodule.SnapshotEstablished("snapshot-1"))
	case campaignEffectDiscoverCatalogue:
		return engine.applyCampaign(move.source, campaignmodule.CatalogueDiscovered(
			move.effect.Snapshot(), slices.Clone(engine.definition.catalogue),
		))
	case campaignEffectMaterializeWorkspace:
		engine.attempts++
		return engine.applyCampaign(move.source, campaignmodule.WorkspaceMaterialized(
			move.effect, fmt.Sprintf("workspace-%d", engine.attempts),
		))
	case campaignEffectRequestAdmission:
		cut, _ := move.effect.RuntimeCut(engine.definition.campaign, engine.campaign.Campaign())
		processed := engine.applyRuntime(cut).Admission()
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		if processed.Decision() != processruntime.AdmissionAccepted {
			engine.enqueueDelivery(sequence, campaignmodule.AdmissionRejected(
				move.effect, processed, "simulation admission rejected",
			))
			break
		}
		for _, grant := range processed.Deliveries() {
			engine.enqueueDelivery(sequence, campaignmodule.AdmissionGranted(move.effect, grant))
		}
	case campaignEffectCancelAdmission:
		cut, _ := move.effect.RuntimeCut(engine.definition.campaign, engine.campaign.Campaign())
		processed := engine.applyRuntime(cut).Admission()
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		engine.enqueueDelivery(sequence, campaignmodule.AdmissionCancelled(move.effect, processed))
	case campaignEffectRequestStartCommitment:
		cut, _ := move.effect.RuntimeCut(engine.definition.campaign, engine.campaign.Campaign())
		processed := engine.applyRuntime(cut).Start()
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		engine.enqueueDelivery(sequence, campaignmodule.StartCommitted(move.effect, processed))
	case campaignEffectReturnAdmission:
		cut, _ := move.effect.RuntimeCut(engine.definition.campaign, engine.campaign.Campaign())
		if !engine.runtime.Accepts(cut) {
			return fmt.Errorf("simulation grant return has no returnable runtime authority")
		}
		processed := engine.applyRuntime(cut).Admission()
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		engine.enqueueDelivery(sequence, campaignmodule.GrantReturnAcknowledged(move.effect, processed))
	case campaignEffectBindConfirmationBarrier:
		cut, _ := move.effect.RuntimeCut(engine.definition.campaign, engine.campaign.Campaign())
		processed := engine.applyRuntime(cut).Barrier()
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		engine.enqueueDelivery(sequence, campaignmodule.ConfirmationBarrierBound(move.effect, processed))
	case campaignEffectLaunchAttempt:
		engine.launches[move.effect.Generation()] = move.effect
		registeredAt := time.Unix(int64(1_000+engine.attempts*100), 0)
		return engine.applySupervisorFact(move.source, supervision.ProspectiveRegistration(
			move.effect.Generation(), move.effect.Attempt(), registeredAt, registeredAt.Add(time.Second),
			move.effect.Spec().Profile, move.effect.Spec().Deadline,
		))
	case campaignEffectStopAttempt:
		fact, disposition := engine.machine.StopFact(move.effect.Generation())
		switch disposition {
		case supervision.StopAbsent:
			if _, registered := engine.launches[move.effect.Generation()]; registered {
				return nil
			}
			return fmt.Errorf("simulation stop effect has no registered generation %d", move.effect.Generation())
		case supervision.StopResolved:
			return nil
		case supervision.StopNotReady:
			return fmt.Errorf("simulation stop effect reached supervision before stop admission sealed")
		case supervision.StopReady:
			return engine.applySupervisorFact(move.source, fact)
		default:
			return fmt.Errorf("simulation stop disposition %d is invalid", disposition)
		}
	case campaignEffectReleaseWorkspace:
		return engine.applyCampaign(move.source, campaignmodule.ResourceSettled(campaignmodule.WorkspaceResource, move.effect.Workspace()))
	case campaignEffectReleaseSnapshot:
		return engine.applyCampaign(move.source, campaignmodule.ResourceSettled(campaignmodule.SnapshotResource, move.effect.Snapshot()))
	case campaignEffectProposeTerminal:
		cut, _ := move.effect.RuntimeCut(engine.definition.campaign, engine.registration.Campaign())
		processed := engine.applyRuntime(cut).Terminal()
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		engine.enqueueDelivery(sequence, campaignmodule.TerminalCommitted(processed.Decision()))
	default:
		return fmt.Errorf("simulation engine effect %v is not implemented", move.effect.Kind())
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
	switch action.Kind() {
	case supervision.LaunchNativeEffect:
		facts, ready := engine.machine.LaunchFacts(action, move.variant.launch)
		if !ready {
			return fmt.Errorf("simulation launch outcome is not enabled")
		}
		for _, fact := range facts {
			if err := engine.applySupervisorFact(move.source, fact); err != nil {
				return err
			}
		}
		return nil
	case supervision.PublishOwnedEffect:
		launch := engine.launches[action.Generation()]
		processed := engine.applyRuntime(processruntime.ObserveAttemptCut(action.Generation(), processruntime.Owned())).Receipt()
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		engine.enqueueDelivery(sequence, campaignmodule.AttemptLaunched(launch, supervision.Owned{}, processed))
	case supervision.PublishNotReleasedEffect, supervision.PublishLaunchUnconfirmedEffect:
		launch := engine.launches[action.Generation()]
		observation := processruntime.LaunchUnconfirmed()
		var launchResult supervision.LaunchResult = supervision.LaunchUnconfirmed{Residual: supervision.ProspectiveUnresolved}
		if action.Kind() == supervision.PublishNotReleasedEffect {
			observed, failure, ready := action.LaunchObservation()
			if !ready {
				return fmt.Errorf("simulation launch observation is not enabled")
			}
			observation = observed
			launchResult = supervision.NotReleased{Kind: failure}
		}
		processed := engine.applyRuntime(processruntime.ObserveAttemptCut(action.Generation(), observation)).Receipt()
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		engine.enqueueDelivery(sequence, campaignmodule.AttemptLaunched(launch, launchResult, processed))
		if action.Kind() == supervision.PublishLaunchUnconfirmedEffect && processed.RuntimeClosureInProgress() {
			engine.enqueueEmergency(sequence, action.OccurredAt().Add(time.Nanosecond))
		}
	case supervision.CloseProspectiveEffect:
		observation, _, ready := action.LaunchObservation()
		if !ready {
			return fmt.Errorf("simulation prospective close is not enabled")
		}
		processed := engine.applyRuntime(processruntime.ObserveAttemptCut(action.Generation(), observation)).Receipt()
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		engine.enqueueAdmissionDeliveries(sequence, processed.Deliveries())
		fact, ready := engine.machine.RuntimeReceiptFactFor(action, processed)
		if !ready {
			return fmt.Errorf("simulation runtime receipt is not enabled")
		}
		engine.enqueueSupervisorFact(sequence, fact)
	case supervision.AdoptOwnedEffect:
		engine.applyRuntime(processruntime.ObserveAttemptCut(action.Generation(), processruntime.Owned()))
		engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
	case supervision.RevokeLaunchReleaseEffect:
		return nil
	case supervision.WaitRootEffect, supervision.SampleRunningEffect:
		return engine.applyHealthyRunning(move)
	case supervision.ForceOwnedEffect, supervision.ObserveEmptinessEffect, supervision.CaptureOutputEffect,
		supervision.SealStopAdmissionEffect, supervision.ReleaseDomainEffect:
		fact, ready := engine.machine.CompletionFact(action, move.variant.drain)
		if !ready {
			return fmt.Errorf("simulation completion is not enabled for action %d", action.Kind())
		}

		return engine.applySupervisorFact(move.source, fact)
	case supervision.TransferResidualCustodyEffect:
		wasOpen := engine.runtime.Projection().Open()
		processed := engine.applyRuntime(processruntime.ObserveAttemptCut(action.Generation(), processruntime.DrainUnconfirmed())).Receipt()
		engine.receipts[action.Generation()] = processed
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		fact, ready := engine.machine.RuntimeReceiptFactFor(action, processed)
		if !ready {
			return fmt.Errorf("simulation residual custody receipt is not enabled")
		}
		engine.enqueueSupervisorFact(sequence, fact)
		if wasOpen {
			engine.enqueueEmergency(sequence, action.OccurredAt().Add(time.Nanosecond))
		}
	case supervision.SettleEmergencyEffect:
		resolutions, ready := action.EmergencyResolutions()
		if !ready {
			return fmt.Errorf("simulation emergency settlement is not enabled")
		}
		if runtimeResiduals := engine.runtime.Projection().Residual(); len(resolutions) != len(runtimeResiduals) {
			return fmt.Errorf(
				"simulation emergency action %d resolves %d generations with runtime residuals %v",
				action.Token(), len(resolutions), runtimeResiduals,
			)
		}
		processed := engine.applyRuntime(processruntime.SettleEmergencyCut(resolutions)).Settlement()
		engine.emergency = processed
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		fact, ready := engine.machine.EmergencySettlementFactFor(action, processed)
		if !ready {
			return fmt.Errorf("simulation emergency settlement is not enabled")
		}
		engine.enqueueSupervisorFact(sequence, fact)
	case supervision.DeliverEmergencySettlementEffect:
		return engine.applyCampaign(move.source, campaignmodule.RuntimeEmergencySettled(engine.emergency))
	case supervision.SettleRuntimeEffect:
		observation, ready := action.TerminalObservation()
		if !ready {
			return fmt.Errorf("simulation terminal observation is not enabled")
		}
		processed := engine.applyRuntime(processruntime.ObserveAttemptCut(action.Generation(), observation)).Receipt()
		engine.receipts[action.Generation()] = processed
		sequence := engine.append(simulationRecord{
			authority: simulationRuntimeAuthority, source: move.source,
			runtimeState: simulationTraceRuntimeState(engine.runtime),
		})
		engine.enqueueAdmissionDeliveries(sequence, processed.Deliveries())
		fact, ready := engine.machine.RuntimeReceiptFactFor(action, processed)
		if !ready {
			return fmt.Errorf("simulation terminal receipt is not enabled")
		}
		engine.enqueueSupervisorFact(sequence, fact)
	case supervision.DeliverTerminalEffect:
		launch := engine.launches[action.Generation()]
		receipt := engine.receipts[action.Generation()]
		terminal, ready := action.Terminal("")
		if !ready {
			return fmt.Errorf("simulation terminal delivery is not enabled")
		}
		deadline := time.Duration(0)
		if launch.AttemptRole() == campaignAttemptBaseline {
			deadline = simulationBaselineMutationDeadline(
				simulationTerminalExecutionData(terminal).CommandDuration, engine.definition.campaign.Peers,
				engine.definition.campaign.Profile,
			)
		}
		event := campaignmodule.AttemptTerminal(launch, terminal, receipt, deadline)
		if launch.CompletesConfirmationQueue() {
			processed := engine.applyRuntime(processruntime.CompleteConfirmationQueueCut(engine.registration.Campaign())).Queue()
			completed := runtimeQueueResult(processed)
			sequence := engine.append(simulationRecord{
				authority: simulationRuntimeAuthority, source: move.source,
				runtimeState: simulationTraceRuntimeState(engine.runtime),
			})
			event = event.WithConfirmationQueueCompleted(completed)
			engine.enqueueAdmissionDeliveries(sequence, completed.Deliveries())
			engine.enqueueDelivery(sequence, event)

			return nil
		}
		engine.pending = append(engine.pending, simulationEngineMove{
			source: move.source, action: move.action, delivery: event,
		})

		return nil
	default:
		return fmt.Errorf("simulation engine supervisor action %v is not implemented", action.Kind())
	}

	return nil
}

func (engine *simulationEngine) applyCampaign(
	source simulationCausalSource,
	payload campaignmodule.Fact,
) (failure error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			failure = fmt.Errorf("simulation campaign payload %T/%#v failed: %v", payload, payload, recovered)
		}
	}()
	state, transition := simulationAdvanceCampaign(engine.campaign, payload)
	effects := transition.Effects()
	engine.campaign = state
	engine.retireSupersededAdmissionWork()
	if payload.Kind() == campaignmodule.RuntimeEmergencySettledFact {
		engine.retireCampaignTerminals()
	}
	record := simulationCampaignRecord(engine.trace, state, transition)
	record.source = source
	engine.trace.records = append(engine.trace.records, record)
	engine.enqueueEffects(effects)

	return nil
}

func (engine *simulationEngine) retireSupersededAdmissionWork() {
	for index := 0; index < len(engine.pending); {
		move := engine.pending[index]
		if move.delivery.IsZero() && move.effect.Kind() != campaignEffectCancelAdmission &&
			move.effect.Kind() != campaignEffectRequestStartCommitment {
			index++
			continue
		}
		if (!move.delivery.IsZero() && engine.campaign.Accepts(move.delivery)) ||
			(move.delivery.IsZero() && engine.campaign.EffectPending(move.effect)) {
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
			move.action.Kind() != supervision.DeliverTerminalEffect {
			index++
			continue
		}
		receipt, observed := engine.receipts[move.action.Generation()]
		if observed && !receipt.RuntimeClosureInProgress() {
			index++
			continue
		}
		engine.pending = slices.Delete(engine.pending, index, index+1)
	}
}

func (engine *simulationEngine) applySupervisorFact(
	source simulationCausalSource,
	fact supervision.Fact,
) error {
	if engine.machine == nil {
		engine.machine = supervision.NewMachine()
	}
	if fact.Kind() == supervision.EmergencyStartedFact && source.kind == simulationOwnerDeliverySource {
		at := fact.OccurredAt()
		prepared, ready := engine.machine.DeterministicEmergencyFact(at, 5*time.Second)
		if !ready {
			return fmt.Errorf("simulation emergency fact is not enabled")
		}
		fact = prepared
	}
	var transition supervision.Transition
	engine.machine, transition = engine.machine.Apply(fact)
	accepted := fact
	effects := transition.Effects()
	record := simulationRecord{
		authority: supervisionAuthority, source: source,
		supervisorEvent:       accepted,
		supervisorDomainEvent: transition.Event(),
		supervisorState:       engine.machine.Projection(),
		supervisorActions:     effects,
	}
	engine.append(record)
	engine.enqueueActions(effects)

	return nil
}

func (engine *simulationEngine) applyHealthyRunning(move simulationEngineMove) error {
	var wait, sample supervision.Effect
	for index := 0; index < len(engine.pending); {
		candidate := engine.pending[index]
		if candidate.source.kind != supervisionActionSource ||
			candidate.action.Generation() != move.action.Generation() ||
			(candidate.action.Kind() != supervision.WaitRootEffect && candidate.action.Kind() != supervision.SampleRunningEffect) {
			index++
			continue
		}
		if candidate.action.Kind() == supervision.WaitRootEffect {
			wait = candidate.action
		} else {
			sample = candidate.action
		}
		engine.pending = slices.Delete(engine.pending, index, index+1)
	}
	if move.action.Kind() == supervision.WaitRootEffect {
		wait = move.action
	} else {
		sample = move.action
	}
	if wait.Token() == 0 {
		return fmt.Errorf("simulation healthy running move has no wait action")
	}
	fact, ready := engine.machine.RunningFact(wait, sample, move.variant.running)
	if !ready {
		return fmt.Errorf("simulation running outcome %d is not enabled", move.variant.running)
	}

	return engine.applySupervisorFact(move.source, fact)
}

func simulationBaselineMutationDeadline(duration time.Duration, peers int, profile Profile) time.Duration {
	plan, err := NewMutationAttemptPlan(MutationAttemptPlanInput{
		BaselineDuration: duration, Peers: peers, Profile: profile,
	})
	if err != nil {
		return 0
	}
	return plan.Deadline()
}

func simulationTerminalExecutionData(terminal supervision.Terminal) supervision.ExecutionData {
	switch terminal := terminal.(type) {
	case supervision.Settled:
		return terminal.ExecutionData
	case supervision.Tripped:
		return terminal.ExecutionData
	case supervision.Stopped:
		return terminal.ExecutionData
	case supervision.Infrastructure:
		return terminal.ExecutionData
	case supervision.DrainUnconfirmed:
		return terminal.ExecutionData
	default:
		panic("simulation terminal has no execution evidence")
	}
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

func (engine *simulationEngine) enqueueEffects(effects []campaignmodule.Effect) {
	for _, enabled := range simulationEnabledMoves(effects, nil, engine.definition.catalogue) {
		engine.pending = append(engine.pending, simulationEngineMove{
			source: simulationCausalSource{
				kind: simulationCampaignEffectSource, identity: uint64(enabled.effect.ID()),
			},
			effect: enabled.effect,
		})
	}
}

func (engine *simulationEngine) enqueueActions(effects []supervision.Effect) {
	for _, enabled := range simulationEnabledMoves(nil, effects, engine.definition.catalogue) {
		engine.pending = append(engine.pending, simulationEngineMove{
			source: simulationCausalSource{
				kind: supervisionActionSource, identity: uint64(enabled.action.Token()),
			},
			action: enabled.action,
		})
	}
}

func (engine *simulationEngine) enqueueDelivery(sequence uint64, payload campaignmodule.Fact) {
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
		engine.enqueueDelivery(sequence, campaignmodule.AdmissionDelivered(grant))
	}
}

func (engine *simulationEngine) enqueueSupervisorFact(sequence uint64, fact supervision.Fact) {
	engine.pending = append(engine.pending, simulationEngineMove{
		source:             simulationCausalSource{kind: simulationOwnerDeliverySource, identity: sequence},
		supervisorDelivery: &fact,
	})
}

func (engine *simulationEngine) enqueueEmergency(sequence uint64, at time.Time) {
	if !engine.machine.AcceptsEmergencyRequest() || engine.hasPendingSupervisorEmergency() {
		return
	}
	engine.enqueueSupervisorFact(sequence, engine.machine.EmergencyRequest(at, at.Add(5*time.Second)))
}

func (engine simulationEngine) enabledMoves() []simulationEngineMove {
	moves := make([]simulationEngineMove, 0, len(engine.pending)+2)
	firstRuntimeCustodyAction := engine.firstRuntimeCustodyAction()
	for _, move := range engine.pending {
		if move.effect.Kind() == campaignEffectStopAttempt && !engine.supervisorAcceptsStop(move.effect.Generation()) {
			continue
		}
		if move.source.kind == simulationCampaignEffectSource && move.effect.Attempt() != "" &&
			move.effect.ID() != engine.firstCampaignEffect(move.effect.Attempt()) {
			continue
		}
		if move.source.kind == supervisionActionSource &&
			move.action.Token() != engine.firstSupervisorAction(move.action.Generation()) {
			continue
		}
		if move.source.kind == simulationOwnerDeliverySource && !move.delivery.IsZero() &&
			engine.hasPendingCampaignEffect() && !engine.deliveryPrecedesPendingAttemptEffect(move.delivery) {
			continue
		}
		if move.delivery.Kind() == campaignmodule.AttemptTerminalFact &&
			move.delivery.RuntimeClosureInProgress() && engine.campaignEmergencyRequested() {
			continue
		}
		if move.delivery.ProvenNotReleased() && move.delivery.RuntimeClosureInProgress() &&
			!engine.campaignEmergencyRequested() {
			continue
		}
		if move.source.kind == simulationOwnerDeliverySource && move.supervisorDelivery != nil &&
			move.supervisorDelivery.Kind() == supervision.EmergencyStartedFact &&
			(!engine.campaignEmergencyRequested() ||
				!engine.supervisorEmergencyReady(move.supervisorDelivery.OccurredAt()) ||
				!engine.emergencyCampaignCutReady()) {
			continue
		}
		if token := engine.runtimeCustodyToken(move); token != 0 && token != firstRuntimeCustodyAction {
			continue
		}
		if move.action.Kind() == supervision.DeliverEmergencySettlementEffect {
			if !engine.campaign.EmergencyRequested() ||
				engine.hasPendingEmergencyCampaignIngress() {
				continue
			}
		}
		if move.source.kind == supervisionActionSource &&
			move.action.Kind() == supervision.DeliverTerminalEffect &&
			engine.hasPendingLaunchDelivery(move.action.Generation()) {
			continue
		}
		if move.source.kind == supervisionActionSource &&
			move.action.Kind() == supervision.TransferResidualCustodyEffect &&
			!engine.runtime.Accepts(processruntime.ObserveAttemptCut(
				move.action.Generation(), processruntime.DrainUnconfirmed(),
			)) {
			continue
		}
		if move.source.kind == supervisionActionSource &&
			move.action.Kind() == supervision.SettleRuntimeEffect &&
			!engine.runtime.Accepts(processruntime.ObserveAttemptCut(
				move.action.Generation(), processruntime.Settled(processruntime.AutomaticProfile, 0),
			)) {
			continue
		}
		if move.source.kind == supervisionActionSource {
			launch := engine.launches[move.action.Generation()]
			move.attemptKind, move.mutant = launch.AttemptRole(), launch.Mutant()
		}
		if move.source.kind == supervisionActionSource &&
			move.action.Kind() == supervision.SampleRunningEffect {
			continue
		}
		moves = append(moves, move)
		if move.source.kind != supervisionActionSource {
			continue
		}
		switch move.action.Kind() {
		case supervision.LaunchNativeEffect, supervision.ObserveEmptinessEffect:
			alternative := move
			if move.action.Kind() == supervision.LaunchNativeEffect {
				alternative.variant.launch = simulationLaunchAtBoundary
			} else {
				alternative.variant.drain = simulationDrainAtBoundary
			}
			moves = append(moves, alternative)
			after := move
			if move.action.Kind() == supervision.LaunchNativeEffect {
				after.variant.launch = simulationLaunchAfterBoundary
			} else {
				after.variant.drain = simulationDrainAfterBoundary
			}
			moves = append(moves, after)
			if move.action.Kind() == supervision.LaunchNativeEffect {
				notReleased := move
				notReleased.variant.launch = simulationLaunchProvenNotReleased
				moves = append(moves, notReleased)
			}
		case supervision.WaitRootEffect:
			for _, variant := range []simulationRunningVariant{
				simulationRunningFailed, simulationRunningAtDeadline, simulationRunningAfterDeadline,
			} {
				alternative := move
				alternative.variant.running = variant
				moves = append(moves, alternative)
			}
			if sample, found := engine.pendingSupervisorAction(move.action.Generation(), supervision.SampleRunningEffect); found {
				_, supportsFuse := engine.machine.RunningFact(move.action, sample, supervision.RunningFuse)
				if !supportsFuse {
					continue
				}
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
		machine = supervision.NewMachine()
	}
	_, ready := machine.DeterministicEmergencyFact(at, 5*time.Second)

	return ready
}

func (engine simulationEngine) supervisorAcceptsStop(generation attemptGeneration) bool {
	_, disposition := engine.machine.StopFact(generation)
	if disposition != supervision.StopAbsent {
		return disposition == supervision.StopReady || disposition == supervision.StopResolved
	}

	_, registered := engine.launches[generation]

	return registered
}

func (engine simulationEngine) pendingSupervisorAction(
	generation attemptGeneration,
	kind supervision.EffectKind,
) (supervision.Effect, bool) {
	for _, move := range engine.pending {
		if move.source.kind == supervisionActionSource && move.action.Generation() == generation &&
			move.action.Kind() == kind {
			return move.action, true
		}
	}

	return supervision.Effect{}, false
}

func (engine simulationEngine) firstSupervisorAction(generation attemptGeneration) supervision.ActionToken {
	var first supervision.ActionToken
	for _, move := range engine.pending {
		if move.source.kind != supervisionActionSource || move.action.Generation() != generation ||
			(first != 0 && move.action.Token() >= first) {
			continue
		}
		first = move.action.Token()
	}

	return first
}

func (engine simulationEngine) firstCampaignEffect(attempt attemptIdentity) uint64 {
	var first uint64
	for _, move := range engine.pending {
		if move.source.kind != simulationCampaignEffectSource || move.effect.Attempt() != attempt ||
			(first != 0 && move.effect.ID() >= first) {
			continue
		}
		first = move.effect.ID()
	}

	return first
}

func (engine simulationEngine) firstRuntimeCustodyAction() supervision.ActionToken {
	var first supervision.ActionToken
	for _, move := range engine.pending {
		token := engine.runtimeCustodyToken(move)
		if token == 0 || (first != 0 && token >= first) {
			continue
		}
		first = token
	}

	return first
}

func (engine simulationEngine) runtimeCustodyToken(move simulationEngineMove) supervision.ActionToken {
	if simulationMutatesRuntimeCustody(move.action.Kind()) {
		return move.action.Token()
	}
	if move.source.kind != simulationOwnerDeliverySource || move.supervisorDelivery == nil ||
		(move.supervisorDelivery.Kind() != supervision.RuntimeCompletedFact &&
			move.supervisorDelivery.Kind() != supervision.EmergencySettlementCompletedFact) {
		return 0
	}
	for _, record := range engine.trace.records {
		if record.sequence == move.source.identity && record.source.kind == supervisionActionSource {
			return supervision.ActionToken(record.source.identity)
		}
	}

	return 0
}

func simulationMutatesRuntimeCustody(kind supervision.EffectKind) bool {
	return kind == supervision.TransferResidualCustodyEffect || kind == supervision.SettleRuntimeEffect ||
		kind == supervision.SettleEmergencyEffect
}

func (engine simulationEngine) hasPendingLaunchDelivery(generation attemptGeneration) bool {
	return slices.ContainsFunc(engine.pending, func(move simulationEngineMove) bool {
		return move.delivery.Kind() == campaignmodule.AttemptLaunchedFact && move.delivery.Generation() == generation
	})
}

func (engine simulationEngine) hasPendingSupervisorEmergency() bool {
	return slices.ContainsFunc(engine.pending, func(move simulationEngineMove) bool {
		return move.supervisorDelivery != nil && move.supervisorDelivery.Kind() == supervision.EmergencyStartedFact
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
		return pending.effect.ID() == selected.effect.ID()
	case supervisionActionSource:
		return pending.action.Token() == selected.action.Token()
	case simulationOwnerDeliverySource:
		return pending.delivery.Equal(selected.delivery) &&
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

func (engine simulationEngine) emergencyCampaignCutReady() bool {
	for _, move := range engine.pending {
		if move.source.kind == simulationCampaignEffectSource &&
			move.effect.Kind() != campaignEffectProposeTerminal {
			return false
		}
		if move.source.kind == supervisionActionSource &&
			(move.action.Kind() == supervision.PublishNotReleasedEffect || move.action.Kind() == supervision.CloseProspectiveEffect) {
			return false
		}
		if move.delivery.StartAccepted() {
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

func (engine simulationEngine) deliveryPrecedesPendingAttemptEffect(payload campaignmodule.Fact) bool {
	attempt := simulationCampaignDeliveryAttempt(payload)
	if attempt == "" {
		return false
	}

	return slices.ContainsFunc(engine.pending, func(move simulationEngineMove) bool {
		return move.source.kind == simulationCampaignEffectSource && move.effect.Attempt() == attempt
	})
}

func simulationCampaignDeliveryAttempt(payload campaignmodule.Fact) attemptIdentity {
	return payload.Attempt()
}

func (engine simulationEngine) hasPendingEmergencyCampaignIngress() bool {
	return slices.ContainsFunc(engine.pending, func(move simulationEngineMove) bool {
		return !move.delivery.IsZero() && !simulationAttemptTerminalDelivery(move.delivery)
	})
}

func simulationAttemptTerminalDelivery(payload campaignmodule.Fact) bool {
	return payload.Kind() == campaignmodule.AttemptTerminalFact
}

func (engine simulationEngine) campaignEmergencyRequested() bool {
	return engine.campaign.EmergencyRequested()
}
