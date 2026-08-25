package ooze

import (
	"fmt"
	"reflect"
	"slices"
	"time"
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
	simulationRequestAdmission
	simulationCancelAdmission
	simulationAcknowledgeGrantReturn
	simulationBindConfirmationBarrier
	simulationCompleteConfirmationQueue
	simulationStartCommitted
	simulationObserveAttempt
	simulationSettleEmergency
	simulationCommitTerminal
	simulationAuthorizeForcedAbort
	simulationCloseRuntime
)

const simulationChooseBaselineFailure byte = 1

type simulationDefinition struct {
	campaign  campaignDefinition
	capacity  int
	catalogue []mutantIdentity
}

type simulationChoiceBytes []byte

type simulationChoiceSource interface {
	choose(limit int) int
}

type simulationChoiceCursor struct {
	values simulationChoiceBytes
	at     int
}

func (source simulationChoiceBytes) choose(limit int) int {
	if limit <= 0 {
		panic("simulation choice limit must be positive")
	}
	if len(source) == 0 {
		return 0
	}

	return int(source[0]) % limit
}

func (source *simulationChoiceCursor) choose(limit int) int {
	if limit <= 0 {
		panic("simulation choice limit must be positive")
	}
	if source == nil || source.at >= len(source.values) {
		return 0
	}
	choice := int(source.values[source.at]) % limit
	source.at++

	return choice
}

type simulationTrace struct {
	definition simulationDefinition
	records    []simulationRecord
	malformed  *simulationMalformedFact
}

type simulationRecord struct {
	sequence  uint64
	authority simulationAuthority

	campaignEvent   simulationCampaignEvent
	campaignState   simulationCampaignState
	campaignEffects []campaignEffect

	runtimeOperation      simulationRuntimeOperation
	runtimeOperationName  string
	runtimeProvenance     campaignProvenance
	runtimeCampaign       campaignToken
	runtimeAdmission      simulationAdmission
	runtimeAdmissionToken simulationAdmission
	runtimeGrant          simulationAdmission
	runtimeBarrier        simulationBarrierBinding
	runtimeSweep          simulationEmergencySweepRecord
	runtimeFatalCause     runtimeFatalCause
	runtimeFatalEpoch     fatalEpochID
	runtimeGeneration     attemptGeneration
	runtimeObservation    simulationRuntimeObservation
	runtimeState          simulationRuntimeState
	runtimeRegistration   campaignRegistration
	runtimeAdmissionOut   simulationAdmissionResult
	runtimeBarrierOut     simulationBarrierResult
	runtimeQueueOut       simulationConfirmationQueueResult
	runtimeStart          startCommittedResult
	runtimeObservationOut simulationObservationResult
	runtimeEmergencyOut   simulationEmergencySettlement
	runtimeTerminal       terminalResult
	runtimeClosure        simulationRuntimeClosure

	supervisorEvent   simulationSupervisorEvent
	supervisorState   simulationSupervisorState
	supervisorActions []simulationSupervisorActionRecord
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

type simulationMalformedFact struct {
	authority        simulationAuthority
	campaign         simulationCampaignEvent
	runtimeOperation simulationRuntimeOperation
	runtimeAdmission simulationAdmission
	supervisor       simulationSupervisorEvent
}

// FailureKey is the alpha-normalized semantic identity retained while shrinking.
type FailureKey struct {
	authority  simulationAuthority
	operation  string
	reason     string
	identities []string
}

// ViolationResult retains the original invariant and the world after guarded cleanup.
type ViolationResult struct {
	world     simulationWorld
	invariant runtimeInvariantViolation
	key       FailureKey
	failure   error
}

// Explore expands choices only through facts enabled by the production owners.
func Explore(definition simulationDefinition, choices simulationChoiceSource) SimulationResult {
	if values, ok := choices.(simulationChoiceBytes); ok {
		choices = &simulationChoiceCursor{values: slices.Clone(values)}
	}
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
		runtimeState:      simulationTraceRuntimeState(runtime), runtimeRegistration: registration,
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
		choice := 0
		if choices != nil {
			choice = choices.choose(3)
		}
		baselineExit := 0
		primaryExit := 0
		if choice == 1 {
			baselineExit = 1
		}
		if choice == 2 {
			primaryExit = 1
		}

		return simulationExplorePendingMoves(
			definition, trace, state, effects, runtime, registration, supervisorState{},
			baselineExit, primaryExit, choice == 1, 1, choices,
		)
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
		runtimeState: simulationTraceRuntimeState(runtime), runtimeTerminal: terminal,
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

type simulationPendingAttempt struct {
	launch      campaignEffect
	wait        supervisorAction
	sample      supervisorAction
	completedAt time.Time
}

func simulationStartPendingAttempt(
	campaign campaignState,
	runtime processRuntime,
	supervisor supervisorState,
	trace simulationTrace,
	materialize campaignEffect,
	attemptOrdinal int,
	launchAtBoundary bool,
) (campaignState, processRuntime, supervisorState, simulationTrace, simulationPendingAttempt) {
	payload := campaignEventPayload(workspaceMaterializedEvent{
		attempt:   materialize.attempt,
		workspace: fmt.Sprintf("workspace-%d", attemptOrdinal), snapshot: materialize.snapshot,
	})
	campaign, effects := simulationAdvanceCampaign(campaign, payload)
	trace.records = append(trace.records, simulationCampaignRecord(trace, campaign, effects, payload))
	requestEffect := simulationOnlyEffect(effects, campaignEffectRequestAdmission)
	request := runtimeAdmissionRequest(requestEffect.request)
	runtime, admission := runtime.requestAdmission(request)
	trace.records = append(trace.records, simulationRecord{
		sequence: uint64(len(trace.records) + 1), authority: simulationRuntimeAuthority,
		runtimeOperation:    simulationRequestAdmission,
		runtimeAdmission:    simulationTraceAdmission(request),
		runtimeState:        simulationTraceRuntimeState(runtime),
		runtimeAdmissionOut: simulationTraceAdmissionResult(admission),
	})
	payload = admissionGrantedEvent{
		attempt: requestEffect.attempt, grant: campaignAdmissionFact(admission.deliveries[0]),
	}
	campaign, effects = simulationAdvanceCampaign(campaign, payload)
	trace.records = append(trace.records, simulationCampaignRecord(trace, campaign, effects, payload))
	startEffect := simulationOnlyEffect(effects, campaignEffectRequestStartCommitment)
	grant := admission.deliveries[0]
	runtime, started := runtime.startCommitted(grant)
	trace.records = append(trace.records, simulationRecord{
		sequence: uint64(len(trace.records) + 1), authority: simulationRuntimeAuthority,
		runtimeOperation: simulationStartCommitted, runtimeGrant: simulationTraceAdmission(grant),
		runtimeState: simulationTraceRuntimeState(runtime), runtimeStart: started,
	})
	payload = startCommittedEvent{
		attempt: startEffect.attempt, grant: startEffect.grant, result: campaignStartEvidence(started),
	}
	campaign, effects = simulationAdvanceCampaign(campaign, payload)
	trace.records = append(trace.records, simulationCampaignRecord(trace, campaign, effects, payload))
	launchEffect := simulationOnlyEffect(effects, campaignEffectLaunchAttempt)
	registeredAt := time.Unix(int64(1_000+attemptOrdinal*100), 0)
	launchBy := registeredAt.Add(time.Second)
	var actions []supervisorAction
	supervisor, actions = simulationRecordSupervisor(&trace, supervisor, supervisorEvent{
		kind: supervisorProspectiveRegistered, generation: launchEffect.generation,
		attempt: launchEffect.attempt, at: registeredAt, launchBy: launchBy,
		profile: launchEffect.spec.Profile, commandDeadline: launchEffect.spec.Deadline,
	})
	launch := simulationOnlySupervisorAction(actions, supervisorLaunchNative)
	completedAt := launchBy.Add(-time.Nanosecond)
	launchEventKind := supervisorLaunchCompleted
	if launchAtBoundary {
		completedAt = launchBy
		launchEventKind = supervisorLaunchBoundary
	}
	completion := supervisorLaunchCompletion{
		generation: launchEffect.generation, action: launch.token, at: completedAt,
		kind: supervisorLaunchReleased,
	}
	supervisor, actions = simulationRecordSupervisor(&trace, supervisor, supervisorEvent{
		kind: launchEventKind, generation: launchEffect.generation,
		at: completedAt, completion: &completion,
	})
	wait := simulationSupervisorAction(actions, supervisorWaitRoot)
	sample := supervisorAction{}
	if launchEffect.spec.Profile == AutomaticProfile {
		sample = simulationSupervisorAction(actions, supervisorSampleRunning)
	}
	runtime, launchReceipt := runtime.observeAttempt(launchEffect.generation, launchOwned{})
	trace.records = append(trace.records, simulationRecord{
		sequence: uint64(len(trace.records) + 1), authority: simulationRuntimeAuthority,
		runtimeOperation: simulationObserveAttempt, runtimeGeneration: launchEffect.generation,
		runtimeObservation:    simulationTraceObservation(launchOwned{}),
		runtimeState:          simulationTraceRuntimeState(runtime),
		runtimeObservationOut: simulationTraceObservationResult(launchReceipt),
	})
	payload = attemptLaunchEvent{
		attempt: launchEffect.attempt, generation: launchEffect.generation,
		result: campaignLaunchObservation{kind: campaignLaunchOwned}, receipt: campaignReceipt(launchReceipt),
	}
	campaign, effects = simulationAdvanceCampaign(campaign, payload)
	trace.records = append(trace.records, simulationCampaignRecord(trace, campaign, effects, payload))
	if len(effects) != 0 {
		panic("owned launch emitted campaign effects")
	}

	return campaign, runtime, supervisor, trace, simulationPendingAttempt{
		launch: launchEffect, wait: wait, sample: sample, completedAt: completedAt,
	}
}

func simulationSettlePendingAttempt(
	definition simulationDefinition,
	campaign campaignState,
	runtime processRuntime,
	supervisor supervisorState,
	trace simulationTrace,
	attempt simulationPendingAttempt,
	exitCode int,
) (campaignState, processRuntime, supervisorState, simulationTrace, []campaignEffect) {
	rootAt := attempt.completedAt.Add(time.Second)
	drainBy := rootAt.Add(5 * time.Second)
	var actions []supervisorAction
	supervisor, actions = simulationRecordSupervisor(&trace, supervisor, supervisorEvent{
		kind: supervisorRunningObserved, generation: attempt.launch.generation, at: rootAt, drainBy: drainBy,
		running: &supervisorRunningBundle{
			generation:   attempt.launch.generation,
			sampleAction: attempt.sample.token, waitAction: attempt.wait.token,
			facts: []supervisorRunningFact{{
				generation: attempt.launch.generation, action: attempt.wait.token,
				kind: supervisorRunningRootExited, at: rootAt, exitCode: exitCode,
			}},
		},
	})
	drain := simulationOnlySupervisorAction(actions, supervisorObserveEmptiness)
	drainAt := rootAt.Add(time.Nanosecond)
	drainCompletion := supervisorDrainCompletion{
		generation: attempt.launch.generation,
		action:     supervisorPendingAction{kind: drain.kind, token: drain.token},
		at:         drainAt, kind: supervisorDrainObservedEmpty,
	}
	supervisor, actions = simulationRecordSupervisor(&trace, supervisor, supervisorEvent{
		kind: supervisorDrainCompleted, generation: attempt.launch.generation,
		at: drainAt, drain: &drainCompletion,
	})
	capture := simulationOnlySupervisorAction(actions, supervisorCaptureOutput)
	outputAt := drainAt.Add(time.Nanosecond)
	outputCompletion := supervisorOutputCompletion{
		generation: attempt.launch.generation,
		action:     supervisorPendingAction{kind: capture.kind, token: capture.token},
		at:         outputAt, ref: 1,
	}
	supervisor, actions = simulationRecordSupervisor(&trace, supervisor, supervisorEvent{
		kind: supervisorOutputCompleted, generation: attempt.launch.generation,
		at: outputAt, output: &outputCompletion,
	})
	seal := simulationOnlySupervisorAction(actions, supervisorSealStopAdmission)
	sealAt := outputAt.Add(time.Nanosecond)
	sealCompletion := supervisorStopSealCompletion{
		generation: attempt.launch.generation,
		action:     supervisorPendingAction{kind: seal.kind, token: seal.token}, at: sealAt,
	}
	supervisor, actions = simulationRecordSupervisor(&trace, supervisor, supervisorEvent{
		kind: supervisorStopAdmissionSealed, generation: attempt.launch.generation,
		at: sealAt, seal: &sealCompletion,
	})
	release := simulationOnlySupervisorAction(actions, supervisorReleaseDomain)
	releaseAt := sealAt.Add(time.Nanosecond)
	releaseCompletion := supervisorReleaseCompletion{
		generation: attempt.launch.generation,
		action:     supervisorPendingAction{kind: release.kind, token: release.token}, at: releaseAt,
	}
	supervisor, actions = simulationRecordSupervisor(&trace, supervisor, supervisorEvent{
		kind: supervisorReleaseCompleted, generation: attempt.launch.generation,
		at: releaseAt, release: &releaseCompletion,
	})
	settle := simulationOnlySupervisorAction(actions, supervisorSettleRuntime)
	observation := terminalObservation(settle.terminal)
	runtime, terminalReceipt := runtime.observeAttempt(attempt.launch.generation, observation)
	trace.records = append(trace.records, simulationRecord{
		sequence: uint64(len(trace.records) + 1), authority: simulationRuntimeAuthority,
		runtimeOperation: simulationObserveAttempt, runtimeGeneration: attempt.launch.generation,
		runtimeObservation:    simulationTraceObservation(observation),
		runtimeState:          simulationTraceRuntimeState(runtime),
		runtimeObservationOut: simulationTraceObservationResult(terminalReceipt),
	})
	runtimeCompletion := supervisorRuntimeCompletion{
		generation: attempt.launch.generation,
		action:     supervisorPendingAction{kind: settle.kind, token: settle.token},
		kind:       supervisorRuntimeAcknowledged,
	}
	supervisor, actions = simulationRecordSupervisor(&trace, supervisor, supervisorEvent{
		kind: supervisorRuntimeCompleted, generation: attempt.launch.generation,
		runtime: &runtimeCompletion,
	})
	deliver := simulationOnlySupervisorAction(actions, supervisorDeliverTerminal)
	terminal := publicTerminal(deliver.terminal, func(supervisorOutputRef) string { return "" }, nil, deliver.runtimeKind)
	terminalEvent := attemptTerminalEvent{
		attempt: attempt.launch.attempt, generation: attempt.launch.generation,
		terminal: terminal, receipt: campaignReceipt(terminalReceipt),
	}
	if attempt.launch.attemptKind == campaignAttemptBaseline {
		terminalEvent.resolvedMutationDeadline = resolveBaselineMutationDeadline(
			terminalExecutionData(terminal).CommandDuration, definition.campaign.peers,
		)
	}
	payload := campaignEventPayload(terminalEvent)
	campaign, effects := simulationAdvanceCampaign(campaign, payload)
	trace.records = append(trace.records, simulationCampaignRecord(trace, campaign, effects, payload))
	workspaceRelease := simulationOnlyEffect(effects, campaignEffectReleaseWorkspace)
	payload = resourceSettledEvent{kind: campaignResourceWorkspace, identity: workspaceRelease.workspace}
	campaign, effects = simulationAdvanceCampaign(campaign, payload)
	trace.records = append(trace.records, simulationCampaignRecord(trace, campaign, effects, payload))

	return campaign, runtime, supervisor, trace, effects
}

func simulationExplorePendingMoves(
	definition simulationDefinition,
	trace simulationTrace,
	campaign campaignState,
	effects []campaignEffect,
	runtime processRuntime,
	registration campaignRegistration,
	supervisor supervisorState,
	baselineExit int,
	primaryExit int,
	launchAtBoundary bool,
	attemptOrdinal int,
	choices simulationChoiceSource,
) SimulationResult {
	for len(effects) != 0 && effects[0].kind == campaignEffectMaterializeWorkspace {
		pending := make([]simulationPendingAttempt, 0, len(effects))
		materializations := slices.Clone(effects)
		for len(materializations) != 0 {
			selected := simulationChooseMove(choices, len(materializations))
			materialize := materializations[selected]
			materializations = slices.Delete(materializations, selected, selected+1)
			if materialize.kind != campaignEffectMaterializeWorkspace {
				return SimulationResult{trace: trace, failure: fmt.Errorf("attempt wave contains effect %v", materialize.kind)}
			}
			var attempt simulationPendingAttempt
			campaign, runtime, supervisor, trace, attempt = simulationStartPendingAttempt(
				campaign, runtime, supervisor, trace, materialize, attemptOrdinal+len(pending), launchAtBoundary,
			)
			pending = append(pending, attempt)
		}
		effects = nil
		started := len(pending)
		for len(pending) != 0 {
			selected := simulationChooseMove(choices, len(pending))
			attempt := pending[selected]
			pending = slices.Delete(pending, selected, selected+1)
			exitCode := primaryExit
			if attempt.launch.attemptKind == campaignAttemptBaseline {
				exitCode = baselineExit
			}
			var next []campaignEffect
			campaign, runtime, supervisor, trace, next = simulationSettlePendingAttempt(
				definition, campaign, runtime, supervisor, trace, attempt, exitCode,
			)
			if len(pending) != 0 && len(next) != 0 {
				return SimulationResult{trace: trace, failure: fmt.Errorf("attempt wave emitted effects before its peers settled")}
			}
			effects = append(effects, next...)
		}
		attemptOrdinal += started
	}
	snapshotRelease := simulationOnlyEffect(effects, campaignEffectReleaseSnapshot)
	payload := campaignEventPayload(resourceSettledEvent{
		kind: campaignResourceSnapshot, identity: string(snapshotRelease.snapshot),
	})
	campaign, effects = simulationAdvanceCampaign(campaign, payload)
	trace.records = append(trace.records, simulationCampaignRecord(trace, campaign, effects, payload))
	simulationRequireOnlyEffect(effects, campaignEffectProposeTerminal)
	runtime, committed := runtime.commitTerminal(registration.token)
	trace.records = append(trace.records, simulationRecord{
		sequence: uint64(len(trace.records) + 1), authority: simulationRuntimeAuthority,
		runtimeOperation: simulationCommitTerminal, runtimeCampaign: registration.token,
		runtimeState: simulationTraceRuntimeState(runtime), runtimeTerminal: committed,
	})
	payload = terminalCommittedEvent{result: campaignTerminalEvidence(committed)}
	campaign, effects = simulationAdvanceCampaign(campaign, payload)
	trace.records = append(trace.records, simulationCampaignRecord(trace, campaign, effects, payload))

	return SimulationResult{
		trace: trace,
		world: simulationWorld{
			campaign: campaign, runtime: runtime, supervisor: simulationProjectSupervisorState(supervisor),
		},
	}
}

func simulationChooseMove(choices simulationChoiceSource, limit int) int {
	if choices == nil || limit == 1 {
		return 0
	}

	return choices.choose(limit)
}

func simulationRequireOnlyEffect(effects []campaignEffect, kind campaignEffectKind) {
	if len(effects) != 1 || effects[0].kind != kind {
		panic(fmt.Sprintf("simulation effect=%#v, want one %v", effects, kind))
	}
}

func simulationOnlyEffect(effects []campaignEffect, kind campaignEffectKind) campaignEffect {
	simulationRequireOnlyEffect(effects, kind)

	return effects[0]
}

func simulationRecordSupervisor(
	trace *simulationTrace,
	state supervisorState,
	event supervisorEvent,
) (supervisorState, []supervisorAction) {
	next, actions := reduceSupervisor(state, event)
	trace.records = append(trace.records, simulationRecord{
		sequence: uint64(len(trace.records) + 1), authority: simulationSupervisorAuthority,
		supervisorEvent:   simulationTraceSupervisorEvent(event),
		supervisorState:   simulationTraceSupervisorState(next),
		supervisorActions: simulationTraceSupervisorActions(actions),
	})

	return next, actions
}

func simulationOnlySupervisorAction(
	actions []supervisorAction,
	kind supervisorActionKind,
) supervisorAction {
	if len(actions) != 1 || actions[0].kind != kind {
		panic(fmt.Sprintf("simulation supervisor actions=%#v, want one %v", actions, kind))
	}

	return actions[0]
}

func simulationSupervisorAction(
	actions []supervisorAction,
	kind supervisorActionKind,
) supervisorAction {
	for _, action := range actions {
		if action.kind == kind {
			return action
		}
	}
	panic(fmt.Sprintf("simulation supervisor actions=%#v, want %v", actions, kind))
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
		campaignEvent: simulationTraceCampaignEvent(campaignEvent{
			id: campaignEventID(len(state.trace)), payload: payload,
		}), campaignState: simulationTraceCampaignState(state),
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
	supervisor := supervisorState{}
	var delivered campaignEventPayload
	activeLaunches := make(map[attemptGeneration]campaignEffect)
	terminalReceipts := make(map[attemptGeneration]observationResult)
	for index, record := range trace.records {
		if record.sequence != uint64(index+1) {
			return simulationReplayFailure(trace, "record %d has sequence %d", index, record.sequence)
		}
		switch record.authority {
		case simulationRuntimeAuthority:
			switch record.runtimeOperation {
			case simulationRegisterCampaign:
				registrationEffect, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return effect.kind == campaignEffectRegister
				})
				if !ok {
					return simulationReplayFailure(trace, "registration is not enabled at record %d", index)
				}
				_ = registrationEffect
				effects = remaining
				var registration campaignRegistration
				runtime, registration = runtime.registerCampaign(record.runtimeProvenance)
				if !reflect.DeepEqual(registration, record.runtimeRegistration) {
					return simulationReplayFailure(trace, "registration diverged at record %d", index)
				}
				delivered = campaignRegisteredEvent{registration: registration}
			case simulationRequestAdmission:
				requestEffect, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return effect.kind == campaignEffectRequestAdmission && reflect.DeepEqual(
						simulationTraceAdmission(runtimeAdmissionRequest(effect.request)), record.runtimeAdmission,
					)
				})
				if !ok {
					return simulationReplayFailure(trace, "admission request is not enabled at record %d", index)
				}
				effects = remaining
				var admission admissionResult
				runtime, admission = runtime.requestAdmission(record.runtimeAdmission.production())
				if !reflect.DeepEqual(simulationTraceAdmissionResult(admission), record.runtimeAdmissionOut) ||
					len(admission.deliveries) != 1 {
					return simulationReplayFailure(trace, "admission decision diverged at record %d", index)
				}
				delivered = admissionGrantedEvent{
					attempt: requestEffect.attempt, grant: campaignAdmissionFact(admission.deliveries[0]),
				}
			case simulationStartCommitted:
				startEffect, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return effect.kind == campaignEffectRequestStartCommitment && reflect.DeepEqual(
						record.runtimeGrant, simulationTraceAdmission(runtimeAdmissionRequest(effect.grant)),
					)
				})
				if !ok {
					return simulationReplayFailure(trace, "start commitment is not enabled at record %d", index)
				}
				effects = remaining
				var started startCommittedResult
				runtime, started = runtime.startCommitted(record.runtimeGrant.production())
				if !reflect.DeepEqual(started, record.runtimeStart) {
					return simulationReplayFailure(trace, "start commitment diverged at record %d", index)
				}
				delivered = startCommittedEvent{
					attempt: startEffect.attempt, grant: startEffect.grant, result: campaignStartEvidence(started),
				}
			case simulationObserveAttempt:
				var observation observationResult
				runtime, observation = runtime.observeAttempt(
					record.runtimeGeneration, record.runtimeObservation.production(),
				)
				if !reflect.DeepEqual(simulationTraceObservationResult(observation), record.runtimeObservationOut) {
					return simulationReplayFailure(trace, "attempt observation diverged at record %d", index)
				}
				switch record.runtimeObservation.kind {
				case simulationLaunchOwnedObservation:
					activeLaunch, found := activeLaunches[record.runtimeGeneration]
					if !found {
						return simulationReplayFailure(trace, "owned observation has no causal launch at record %d", index)
					}
					delivered = attemptLaunchEvent{
						attempt: activeLaunch.attempt, generation: activeLaunch.generation,
						result:  campaignLaunchObservation{kind: campaignLaunchOwned},
						receipt: campaignReceipt(observation),
					}
				default:
					terminalReceipts[record.runtimeGeneration] = observation
				}
			case simulationCommitTerminal:
				_, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return effect.kind == campaignEffectProposeTerminal
				})
				if !ok {
					return simulationReplayFailure(trace, "terminal commitment is not enabled at record %d", index)
				}
				effects = remaining
				var terminal terminalResult
				runtime, terminal = runtime.commitTerminal(record.runtimeCampaign)
				if !reflect.DeepEqual(terminal, record.runtimeTerminal) {
					return simulationReplayFailure(trace, "terminal commitment diverged at record %d", index)
				}
				delivered = terminalCommittedEvent{result: campaignTerminalEvidence(terminal)}
			default:
				return simulationReplayFailure(trace, "runtime operation is invalid at record %d", index)
			}
			if !reflect.DeepEqual(simulationTraceRuntimeState(runtime), record.runtimeState) {
				return simulationReplayFailure(trace, "runtime state diverged at record %d", index)
			}
		case simulationCampaignAuthority:
			payload := record.campaignEvent.production().payload
			if payload == nil {
				payload = delivered
			}
			if delivered != nil {
				delivered = simulationCausalCampaignPayload(payload, delivered)
			}
			if delivered != nil && !reflect.DeepEqual(payload, delivered) {
				return simulationReplayFailure(trace, "causal campaign fact diverged at record %d", index)
			}
			if delivered == nil {
				_, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return simulationEffectEnablesExternalFact(effect, payload)
				})
				if !ok {
					return simulationReplayFailure(
						trace, "external campaign fact is not enabled at record %d (%s): effects=%v",
						index, payload.campaignEventName(), simulationEffectKinds(effects),
					)
				}
				effects = remaining
			}
			var emitted []campaignEffect
			campaign, emitted = advanceCampaign(campaign, campaignEvent{
				id: campaignEventID(len(campaign.trace) + 1), payload: payload,
			})
			delivered = nil
			if !reflect.DeepEqual(simulationTraceCampaignState(campaign), record.campaignState) {
				return simulationReplayFailure(
					trace, "campaign state diverged at record %d (%s)", index, payload.campaignEventName(),
				)
			}
			if !slices.EqualFunc(emitted, record.campaignEffects, func(left, right campaignEffect) bool {
				return reflect.DeepEqual(left, right)
			}) {
				return simulationReplayFailure(
					trace, "campaign effects diverged at record %d (%s): got=%v want=%v",
					index, payload.campaignEventName(), simulationEffectKinds(emitted),
					simulationEffectKinds(record.campaignEffects),
				)
			}
			effects = append(effects, emitted...)
		case simulationSupervisorAuthority:
			event := record.supervisorEvent.production()
			if record.supervisorEvent.kind == supervisorProspectiveRegistered {
				launchEffect, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return effect.kind == campaignEffectLaunchAttempt &&
						simulationSupervisorRegistrationMatches(effect, event)
				})
				if !ok {
					return simulationReplayFailure(trace, "supervisor launch is not enabled at record %d", index)
				}
				activeLaunches[event.generation] = launchEffect
				effects = remaining
			}
			var actions []supervisorAction
			supervisor, actions = reduceSupervisor(supervisor, event)
			if !reflect.DeepEqual(simulationTraceSupervisorState(supervisor), record.supervisorState) ||
				!reflect.DeepEqual(simulationTraceSupervisorActions(actions), record.supervisorActions) {
				return simulationReplayFailure(trace, "supervisor transition diverged at record %d", index)
			}
			if record.supervisorEvent.kind == supervisorRuntimeCompleted {
				activeLaunch, found := activeLaunches[event.generation]
				if !found {
					return simulationReplayFailure(trace, "runtime completion has no causal launch at record %d", index)
				}
				deliver := simulationOnlySupervisorAction(actions, supervisorDeliverTerminal)
				terminal := publicTerminal(
					deliver.terminal, func(supervisorOutputRef) string { return "" }, nil, deliver.runtimeKind,
				)
				receipt, found := terminalReceipts[activeLaunch.generation]
				if !found {
					return simulationReplayFailure(trace, "terminal completion has no runtime receipt at record %d", index)
				}
				delete(terminalReceipts, activeLaunch.generation)
				terminalEvent := attemptTerminalEvent{
					attempt: activeLaunch.attempt, generation: activeLaunch.generation,
					terminal: terminal, receipt: campaignReceipt(receipt),
				}
				delivered = terminalEvent
			}
		default:
			return simulationReplayFailure(trace, "authority is invalid at record %d", index)
		}
	}

	return SimulationResult{
		trace: trace,
		world: simulationWorld{
			campaign: campaign, runtime: runtime, supervisor: simulationProjectSupervisorState(supervisor),
		},
	}
}

func simulationCausalCampaignPayload(recorded, derived campaignEventPayload) campaignEventPayload {
	recordedTerminal, recordedIsTerminal := recorded.(attemptTerminalEvent)
	derivedTerminal, derivedIsTerminal := derived.(attemptTerminalEvent)
	if recordedIsTerminal && derivedIsTerminal {
		derivedTerminal.resolvedMutationDeadline = recordedTerminal.resolvedMutationDeadline

		return derivedTerminal
	}

	return derived
}

// ReplayViolation applies one malformed fact after a legal prefix and captures the guard's re-panic.
func ReplayViolation(prefix simulationTrace, malformed simulationMalformedFact) (result ViolationResult) {
	legal := ReplayLegal(prefix)
	if legal.failure != nil {
		return ViolationResult{failure: fmt.Errorf("legal prefix: %w", legal.failure)}
	}
	switch malformed.authority {
	case simulationCampaignAuthority:
		if malformed.campaign.kind == 0 {
			return ViolationResult{failure: fmt.Errorf("malformed campaign fact is absent")}
		}
	case simulationRuntimeAuthority:
		if malformed.runtimeOperation != simulationRequestAdmission {
			return ViolationResult{failure: fmt.Errorf("malformed runtime operation is not implemented")}
		}
	case simulationSupervisorAuthority:
	default:
		return ViolationResult{failure: fmt.Errorf("malformed fact authority is not implemented")}
	}

	runtime := legal.world.runtime
	campaign := legal.world.campaign
	defer func() {
		recovered := recover()
		violation, ok := recovered.(runtimeInvariantViolation)
		if !ok {
			if recovered == nil {
				result = ViolationResult{failure: fmt.Errorf("malformed fact was accepted")}
			} else {
				result = ViolationResult{failure: fmt.Errorf("unexpected violation panic: %v", recovered)}
			}

			return
		}
		result = ViolationResult{
			world: simulationWorld{
				campaign: campaign, runtime: runtime, supervisor: legal.world.supervisor,
			},
			invariant: violation,
			key:       simulationFailureKey(malformed.authority, violation),
		}
	}()

	switch malformed.authority {
	case simulationCampaignAuthority:
		malformedEvent := malformed.campaign.production()
		_, _ = advanceCampaignGuarded(&runtime, campaign, campaignEvent{
			id: campaignEventID(len(campaign.trace) + 1), payload: malformedEvent.payload,
		}, simulationEmergencySweep)
	case simulationRuntimeAuthority:
		simulationAdvanceRuntimeGuarded(&runtime, malformed.runtimeAdmission.production())
	case simulationSupervisorAuthority:
		simulationAdvanceSupervisorGuarded(&runtime, legal.world.supervisor, malformed.supervisor.production())
	}

	return ViolationResult{failure: fmt.Errorf("malformed fact was accepted")}
}

func simulationAdvanceRuntimeGuarded(runtime *processRuntime, request admissionRequest) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		violation, ok := recovered.(runtimeInvariantViolation)
		if !ok {
			violation = runtimeInvariantViolation{operation: "request admission", reason: "unexpected panic"}
		}
		var closure runtimeClosure
		*runtime, closure = runtime.closeRuntime(runtimeFatalCause(violation.reason))
		*runtime, _ = runtime.settleEmergency(simulationEmergencySweep(closure))
		panic(violation)
	}()

	*runtime, _ = runtime.requestAdmission(request)
}

func simulationAdvanceSupervisorGuarded(
	runtime *processRuntime,
	state supervisorState,
	event supervisorEvent,
) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		violation, ok := recovered.(runtimeInvariantViolation)
		if !ok {
			violation = runtimeInvariantViolation{
				operation: supervisorReducerOperation, reason: "unexpected panic",
			}
		}
		var closure runtimeClosure
		*runtime, closure = runtime.closeRuntime(runtimeFatalCause(violation.reason))
		*runtime, _ = runtime.settleEmergency(simulationEmergencySweep(closure))
		panic(violation)
	}()

	_, _ = reduceSupervisor(state, event)
}

func simulationEmergencySweep(closure runtimeClosure) emergencySweep {
	resolutions := make([]emergencyResolution, len(closure.residual))
	for index, residual := range closure.residual {
		resolutions[index] = emergencyResolution{
			generation: residual.generation, disposition: emergencyConfirmedDrained,
		}
	}

	return emergencySweep{resolutions: resolutions}
}

// Shrink removes semantic records and definition members while retaining one typed failure.
func Shrink(trace simulationTrace, key FailureKey) simulationTrace {
	if trace.malformed == nil {
		panic("simulation shrink requires one malformed fact")
	}
	shrunk := simulationCloneTrace(trace)
	for width := len(shrunk.records); width > 0; {
		accepted := false
		for start := 0; start+width <= len(shrunk.records); start++ {
			candidate := simulationCloneTrace(shrunk)
			candidate.records = slices.Delete(candidate.records, start, start+width)
			simulationRenumberRecords(candidate.records)
			if !simulationPreservesFailure(candidate, key) {
				continue
			}
			shrunk = candidate
			accepted = true
			break
		}
		if accepted {
			width = min(width, len(shrunk.records))
			continue
		}
		width--
	}
	for index := 0; index < len(shrunk.definition.catalogue); {
		candidate := simulationCloneTrace(shrunk)
		candidate.definition.catalogue = slices.Delete(candidate.definition.catalogue, index, index+1)
		if simulationPreservesFailure(candidate, key) {
			shrunk = candidate
			continue
		}
		index++
	}
	for capacity := 1; capacity < shrunk.definition.capacity; capacity++ {
		candidate := simulationCloneTrace(shrunk)
		candidate.definition.capacity = capacity
		if simulationPreservesFailure(candidate, key) {
			shrunk = candidate
			break
		}
	}
	for peers := 1; peers < shrunk.definition.campaign.peers; peers++ {
		candidate := simulationCloneTrace(shrunk)
		candidate.definition.campaign.peers = peers
		if simulationPreservesFailure(candidate, key) {
			shrunk = candidate
			break
		}
	}
	first := ReplayViolation(shrunk, *shrunk.malformed)
	second := ReplayViolation(shrunk, *shrunk.malformed)
	if first.failure != nil || !reflect.DeepEqual(first.key, key) || !reflect.DeepEqual(first, second) {
		panic("simulation shrink did not retain a deterministic failure")
	}

	return shrunk
}

func simulationRenumberRecords(records []simulationRecord) {
	for index := range records {
		records[index].sequence = uint64(index + 1)
	}
}

func simulationCloneTrace(trace simulationTrace) simulationTrace {
	trace.definition.catalogue = slices.Clone(trace.definition.catalogue)
	trace.definition.campaign.command = slices.Clone(trace.definition.campaign.command)
	trace.definition.campaign.env = slices.Clone(trace.definition.campaign.env)
	trace.records = slices.Clone(trace.records)
	if trace.malformed != nil {
		malformed := *trace.malformed
		trace.malformed = &malformed
	}

	return trace
}

func simulationPreservesFailure(trace simulationTrace, key FailureKey) bool {
	result := ReplayViolation(trace, *trace.malformed)

	return result.failure == nil && reflect.DeepEqual(result.key, key)
}

func simulationFailureKey(authority simulationAuthority, violation runtimeInvariantViolation) FailureKey {
	relevant := violation.stableIdentities
	if authority == simulationCampaignAuthority && len(relevant) != 0 {
		relevant = relevant[:1]
	}
	identities := make([]string, len(relevant))
	seen := make(map[string]int, len(relevant))
	for index, identity := range relevant {
		ordinal, ok := seen[identity]
		if !ok {
			ordinal = len(seen) + 1
			seen[identity] = ordinal
		}
		role := identity
		for at, character := range identity {
			if character == '=' {
				role = identity[:at+1]
				break
			}
		}
		identities[index] = fmt.Sprintf("%s#%d", role, ordinal)
	}

	return FailureKey{
		authority: authority, operation: violation.operation, reason: violation.reason,
		identities: identities,
	}
}

func simulationSupervisorRegistrationMatches(effect campaignEffect, event supervisorEvent) bool {
	return event.generation == effect.generation && event.attempt == effect.attempt &&
		event.profile == effect.spec.Profile && event.commandDeadline == effect.spec.Deadline
}

func simulationEffectEnablesExternalFact(effect campaignEffect, payload campaignEventPayload) bool {
	switch fact := payload.(type) {
	case snapshotEstablishedEvent:
		return effect.kind == campaignEffectEstablishSnapshot
	case catalogueDiscoveredEvent:
		return effect.kind == campaignEffectDiscoverCatalogue
	case resourceSettledEvent:
		switch fact.kind {
		case campaignResourceSnapshot:
			return effect.kind == campaignEffectReleaseSnapshot && string(effect.snapshot) == fact.identity
		case campaignResourceWorkspace:
			return effect.kind == campaignEffectReleaseWorkspace && effect.workspace == fact.identity
		default:
			return false
		}
	case workspaceMaterializedEvent:
		materialized := payload.(workspaceMaterializedEvent)

		return effect.kind == campaignEffectMaterializeWorkspace && effect.attempt == materialized.attempt
	default:
		return false
	}
}

func simulationTakeEffect(
	effects []campaignEffect,
	match func(campaignEffect) bool,
) (campaignEffect, []campaignEffect, bool) {
	for index, effect := range effects {
		if !match(effect) {
			continue
		}
		remaining := slices.Clone(effects)
		remaining = slices.Delete(remaining, index, index+1)

		return effect, remaining, true
	}

	return campaignEffect{}, effects, false
}

func simulationEffectKinds(effects []campaignEffect) []campaignEffectKind {
	kinds := make([]campaignEffectKind, len(effects))
	for index, effect := range effects {
		kinds[index] = effect.kind
	}

	return kinds
}

func simulationReplayFailure(trace simulationTrace, format string, arguments ...any) SimulationResult {
	return SimulationResult{trace: trace, failure: fmt.Errorf(format, arguments...)}
}
