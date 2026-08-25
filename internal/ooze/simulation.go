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
	simulationStartCommitted
	simulationObserveAttempt
	simulationCommitTerminal
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

func (source simulationChoiceBytes) choose(limit int) int {
	if limit <= 0 {
		panic("simulation choice limit must be positive")
	}
	if len(source) == 0 {
		return 0
	}

	return int(source[0]) % limit
}

type simulationTrace struct {
	definition simulationDefinition
	records    []simulationRecord
	malformed  *simulationMalformedFact
}

type simulationRecord struct {
	sequence  uint64
	authority simulationAuthority

	campaignEvent   campaignEvent
	campaignState   campaignState
	campaignEffects []campaignEffect

	runtimeOperation      simulationRuntimeOperation
	runtimeOperationName  string
	runtimeProvenance     campaignProvenance
	runtimeCampaign       campaignToken
	runtimeAdmission      admissionRequest
	runtimeGrant          admissionGrant
	runtimeGeneration     attemptGeneration
	runtimeObservation    attemptObservation
	runtimeState          processRuntime
	runtimeRegistration   campaignRegistration
	runtimeAdmissionOut   admissionResult
	runtimeStart          startCommittedResult
	runtimeObservationOut observationResult
	runtimeTerminal       terminalResult

	supervisorEvent   supervisorEvent
	supervisorState   supervisorState
	supervisorActions []supervisorAction
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
	authority simulationAuthority
	campaign  campaignEventPayload
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

		return simulationExploreAttempt(
			definition, trace, state, effects, runtime, registration, supervisorState{},
			baselineExit, primaryExit, choice == 1, 1,
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

func simulationExploreAttempt(
	definition simulationDefinition,
	trace simulationTrace,
	campaign campaignState,
	effects []campaignEffect,
	runtime processRuntime,
	registration campaignRegistration,
	supervisor supervisorState,
	exitCode int,
	primaryExit int,
	launchAtBoundary bool,
	attemptOrdinal int,
) SimulationResult {
	materialize := simulationOnlyEffect(effects, campaignEffectMaterializeWorkspace)
	payload := campaignEventPayload(workspaceMaterializedEvent{
		attempt:   materialize.attempt,
		workspace: fmt.Sprintf("workspace-%d", attemptOrdinal), snapshot: materialize.snapshot,
	})
	campaign, effects = simulationAdvanceCampaign(campaign, payload)
	trace.records = append(trace.records, simulationCampaignRecord(trace, campaign, effects, payload))

	requestEffect := simulationOnlyEffect(effects, campaignEffectRequestAdmission)
	request := runtimeAdmissionRequest(requestEffect.request)
	var admission admissionResult
	runtime, admission = runtime.requestAdmission(request)
	trace.records = append(trace.records, simulationRecord{
		sequence: uint64(len(trace.records) + 1), authority: simulationRuntimeAuthority,
		runtimeOperation: simulationRequestAdmission, runtimeAdmission: request,
		runtimeState: runtime, runtimeAdmissionOut: admission,
	})
	payload = admissionGrantedEvent{
		attempt: requestEffect.attempt, grant: campaignAdmissionFact(admission.deliveries[0]),
	}
	campaign, effects = simulationAdvanceCampaign(campaign, payload)
	trace.records = append(trace.records, simulationCampaignRecord(trace, campaign, effects, payload))

	startEffect := simulationOnlyEffect(effects, campaignEffectRequestStartCommitment)
	grant := admission.deliveries[0]
	var started startCommittedResult
	runtime, started = runtime.startCommitted(grant)
	trace.records = append(trace.records, simulationRecord{
		sequence: uint64(len(trace.records) + 1), authority: simulationRuntimeAuthority,
		runtimeOperation: simulationStartCommitted, runtimeGrant: grant,
		runtimeState: runtime, runtimeStart: started,
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

	var launchReceipt observationResult
	runtime, launchReceipt = runtime.observeAttempt(launchEffect.generation, launchOwned{})
	trace.records = append(trace.records, simulationRecord{
		sequence: uint64(len(trace.records) + 1), authority: simulationRuntimeAuthority,
		runtimeOperation: simulationObserveAttempt, runtimeGeneration: launchEffect.generation,
		runtimeObservation: launchOwned{}, runtimeState: runtime, runtimeObservationOut: launchReceipt,
	})
	payload = attemptLaunchEvent{
		attempt: launchEffect.attempt, generation: launchEffect.generation,
		result: campaignLaunchObservation{kind: campaignLaunchOwned}, receipt: campaignReceipt(launchReceipt),
	}
	campaign, effects = simulationAdvanceCampaign(campaign, payload)
	trace.records = append(trace.records, simulationCampaignRecord(trace, campaign, effects, payload))
	if len(effects) != 0 {
		return SimulationResult{trace: trace, failure: fmt.Errorf("owned launch emitted campaign effects")}
	}

	rootAt := completedAt.Add(time.Second)
	drainBy := rootAt.Add(5 * time.Second)
	supervisor, actions = simulationRecordSupervisor(&trace, supervisor, supervisorEvent{
		kind: supervisorRunningObserved, generation: launchEffect.generation, at: rootAt, drainBy: drainBy,
		running: &supervisorRunningBundle{
			generation: launchEffect.generation, sampleAction: sample.token, waitAction: wait.token,
			facts: []supervisorRunningFact{{
				generation: launchEffect.generation, action: wait.token,
				kind: supervisorRunningRootExited, at: rootAt, exitCode: exitCode,
			}},
		},
	})
	drain := simulationOnlySupervisorAction(actions, supervisorObserveEmptiness)
	drainAt := rootAt.Add(time.Nanosecond)
	drainCompletion := supervisorDrainCompletion{
		generation: launchEffect.generation,
		action:     supervisorPendingAction{kind: drain.kind, token: drain.token},
		at:         drainAt, kind: supervisorDrainObservedEmpty,
	}
	supervisor, actions = simulationRecordSupervisor(&trace, supervisor, supervisorEvent{
		kind: supervisorDrainCompleted, generation: launchEffect.generation,
		at: drainAt, drain: &drainCompletion,
	})
	capture := simulationOnlySupervisorAction(actions, supervisorCaptureOutput)
	outputAt := drainAt.Add(time.Nanosecond)
	outputCompletion := supervisorOutputCompletion{
		generation: launchEffect.generation,
		action:     supervisorPendingAction{kind: capture.kind, token: capture.token},
		at:         outputAt, ref: 1,
	}
	supervisor, actions = simulationRecordSupervisor(&trace, supervisor, supervisorEvent{
		kind: supervisorOutputCompleted, generation: launchEffect.generation,
		at: outputAt, output: &outputCompletion,
	})
	seal := simulationOnlySupervisorAction(actions, supervisorSealStopAdmission)
	sealAt := outputAt.Add(time.Nanosecond)
	sealCompletion := supervisorStopSealCompletion{
		generation: launchEffect.generation,
		action:     supervisorPendingAction{kind: seal.kind, token: seal.token}, at: sealAt,
	}
	supervisor, actions = simulationRecordSupervisor(&trace, supervisor, supervisorEvent{
		kind: supervisorStopAdmissionSealed, generation: launchEffect.generation,
		at: sealAt, seal: &sealCompletion,
	})
	release := simulationOnlySupervisorAction(actions, supervisorReleaseDomain)
	releaseAt := sealAt.Add(time.Nanosecond)
	releaseCompletion := supervisorReleaseCompletion{
		generation: launchEffect.generation,
		action:     supervisorPendingAction{kind: release.kind, token: release.token}, at: releaseAt,
	}
	supervisor, actions = simulationRecordSupervisor(&trace, supervisor, supervisorEvent{
		kind: supervisorReleaseCompleted, generation: launchEffect.generation,
		at: releaseAt, release: &releaseCompletion,
	})
	settle := simulationOnlySupervisorAction(actions, supervisorSettleRuntime)

	observation := terminalObservation(settle.terminal)
	var terminalReceipt observationResult
	runtime, terminalReceipt = runtime.observeAttempt(launchEffect.generation, observation)
	trace.records = append(trace.records, simulationRecord{
		sequence: uint64(len(trace.records) + 1), authority: simulationRuntimeAuthority,
		runtimeOperation: simulationObserveAttempt, runtimeGeneration: launchEffect.generation,
		runtimeObservation: observation, runtimeState: runtime, runtimeObservationOut: terminalReceipt,
	})
	runtimeCompletion := supervisorRuntimeCompletion{
		generation: launchEffect.generation,
		action:     supervisorPendingAction{kind: settle.kind, token: settle.token},
		kind:       supervisorRuntimeAcknowledged,
	}
	supervisor, actions = simulationRecordSupervisor(&trace, supervisor, supervisorEvent{
		kind: supervisorRuntimeCompleted, generation: launchEffect.generation,
		runtime: &runtimeCompletion,
	})
	deliver := simulationOnlySupervisorAction(actions, supervisorDeliverTerminal)
	terminal := publicTerminal(deliver.terminal, func(supervisorOutputRef) string { return "" }, nil, deliver.runtimeKind)
	terminalEvent := attemptTerminalEvent{
		attempt: launchEffect.attempt, generation: launchEffect.generation,
		terminal: terminal, receipt: campaignReceipt(terminalReceipt),
	}
	if launchEffect.attemptKind == campaignAttemptBaseline {
		terminalEvent.resolvedMutationDeadline = resolveBaselineMutationDeadline(
			terminalExecutionData(terminal).CommandDuration, definition.campaign.peers,
		)
	}
	payload = terminalEvent
	campaign, effects = simulationAdvanceCampaign(campaign, payload)
	trace.records = append(trace.records, simulationCampaignRecord(trace, campaign, effects, payload))

	workspaceRelease := simulationOnlyEffect(effects, campaignEffectReleaseWorkspace)
	payload = resourceSettledEvent{kind: campaignResourceWorkspace, identity: workspaceRelease.workspace}
	campaign, effects = simulationAdvanceCampaign(campaign, payload)
	trace.records = append(trace.records, simulationCampaignRecord(trace, campaign, effects, payload))
	if len(effects) == 1 && effects[0].kind == campaignEffectMaterializeWorkspace {
		return simulationExploreAttempt(
			definition, trace, campaign, effects, runtime, registration, supervisor,
			primaryExit, primaryExit, launchAtBoundary, attemptOrdinal+1,
		)
	}
	snapshotRelease := simulationOnlyEffect(effects, campaignEffectReleaseSnapshot)
	payload = resourceSettledEvent{kind: campaignResourceSnapshot, identity: string(snapshotRelease.snapshot)}
	campaign, effects = simulationAdvanceCampaign(campaign, payload)
	trace.records = append(trace.records, simulationCampaignRecord(trace, campaign, effects, payload))

	simulationRequireOnlyEffect(effects, campaignEffectProposeTerminal)
	var committed terminalResult
	runtime, committed = runtime.commitTerminal(registration.token)
	trace.records = append(trace.records, simulationRecord{
		sequence: uint64(len(trace.records) + 1), authority: simulationRuntimeAuthority,
		runtimeOperation: simulationCommitTerminal, runtimeCampaign: registration.token,
		runtimeState: runtime, runtimeTerminal: committed,
	})
	payload = terminalCommittedEvent{result: campaignTerminalEvidence(committed)}
	campaign, effects = simulationAdvanceCampaign(campaign, payload)
	trace.records = append(trace.records, simulationCampaignRecord(trace, campaign, effects, payload))

	return SimulationResult{
		trace: trace,
		world: simulationWorld{campaign: campaign, runtime: runtime, supervisor: supervisor},
	}
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
		supervisorEvent: event, supervisorState: next,
		supervisorActions: append([]supervisorAction(nil), actions...),
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
	supervisor := supervisorState{}
	var delivered campaignEventPayload
	var activeLaunch campaignEffect
	var terminalReceipt observationResult
	for index, record := range trace.records {
		if record.sequence != uint64(index+1) {
			return simulationReplayFailure(trace, "record %d has sequence %d", index, record.sequence)
		}
		switch record.authority {
		case simulationRuntimeAuthority:
			switch record.runtimeOperation {
			case simulationRegisterCampaign:
				if len(effects) != 1 || effects[0].kind != campaignEffectRegister {
					return simulationReplayFailure(trace, "registration is not enabled at record %d", index)
				}
				var registration campaignRegistration
				runtime, registration = runtime.registerCampaign(record.runtimeProvenance)
				if !reflect.DeepEqual(registration, record.runtimeRegistration) {
					return simulationReplayFailure(trace, "registration diverged at record %d", index)
				}
				delivered = campaignRegisteredEvent{registration: registration}
				effects = nil
			case simulationRequestAdmission:
				if len(effects) != 1 || effects[0].kind != campaignEffectRequestAdmission ||
					!reflect.DeepEqual(runtimeAdmissionRequest(effects[0].request), record.runtimeAdmission) {
					return simulationReplayFailure(trace, "admission request is not enabled at record %d", index)
				}
				var admission admissionResult
				runtime, admission = runtime.requestAdmission(record.runtimeAdmission)
				if !reflect.DeepEqual(admission, record.runtimeAdmissionOut) || len(admission.deliveries) != 1 {
					return simulationReplayFailure(trace, "admission decision diverged at record %d", index)
				}
				delivered = admissionGrantedEvent{
					attempt: effects[0].attempt, grant: campaignAdmissionFact(admission.deliveries[0]),
				}
				effects = nil
			case simulationStartCommitted:
				if len(effects) != 1 || effects[0].kind != campaignEffectRequestStartCommitment ||
					!reflect.DeepEqual(record.runtimeGrant, runtimeAdmissionRequest(effects[0].grant)) {
					return simulationReplayFailure(trace, "start commitment is not enabled at record %d", index)
				}
				var started startCommittedResult
				runtime, started = runtime.startCommitted(record.runtimeGrant)
				if !reflect.DeepEqual(started, record.runtimeStart) {
					return simulationReplayFailure(trace, "start commitment diverged at record %d", index)
				}
				delivered = startCommittedEvent{
					attempt: effects[0].attempt, grant: effects[0].grant, result: campaignStartEvidence(started),
				}
				effects = nil
			case simulationObserveAttempt:
				var observation observationResult
				runtime, observation = runtime.observeAttempt(record.runtimeGeneration, record.runtimeObservation)
				if !reflect.DeepEqual(observation, record.runtimeObservationOut) {
					return simulationReplayFailure(trace, "attempt observation diverged at record %d", index)
				}
				switch record.runtimeObservation.(type) {
				case launchOwned:
					delivered = attemptLaunchEvent{
						attempt: activeLaunch.attempt, generation: activeLaunch.generation,
						result:  campaignLaunchObservation{kind: campaignLaunchOwned},
						receipt: campaignReceipt(observation),
					}
				default:
					terminalReceipt = observation
				}
			case simulationCommitTerminal:
				if len(effects) != 1 || effects[0].kind != campaignEffectProposeTerminal {
					return simulationReplayFailure(trace, "terminal commitment is not enabled at record %d", index)
				}
				var terminal terminalResult
				runtime, terminal = runtime.commitTerminal(record.runtimeCampaign)
				if !reflect.DeepEqual(terminal, record.runtimeTerminal) {
					return simulationReplayFailure(trace, "terminal commitment diverged at record %d", index)
				}
				delivered = terminalCommittedEvent{result: campaignTerminalEvidence(terminal)}
				effects = nil
			default:
				return simulationReplayFailure(trace, "runtime operation is invalid at record %d", index)
			}
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
		case simulationSupervisorAuthority:
			if record.supervisorEvent.kind == supervisorProspectiveRegistered {
				if len(effects) != 1 || effects[0].kind != campaignEffectLaunchAttempt ||
					!simulationSupervisorRegistrationMatches(effects[0], record.supervisorEvent) {
					return simulationReplayFailure(trace, "supervisor launch is not enabled at record %d", index)
				}
				activeLaunch = effects[0]
				effects = nil
			}
			var actions []supervisorAction
			supervisor, actions = reduceSupervisor(supervisor, record.supervisorEvent)
			if !reflect.DeepEqual(supervisor, record.supervisorState) ||
				!reflect.DeepEqual(actions, record.supervisorActions) {
				return simulationReplayFailure(trace, "supervisor transition diverged at record %d", index)
			}
			if record.supervisorEvent.kind == supervisorRuntimeCompleted {
				deliver := simulationOnlySupervisorAction(actions, supervisorDeliverTerminal)
				terminal := publicTerminal(
					deliver.terminal, func(supervisorOutputRef) string { return "" }, nil, deliver.runtimeKind,
				)
				terminalEvent := attemptTerminalEvent{
					attempt: activeLaunch.attempt, generation: activeLaunch.generation,
					terminal: terminal, receipt: campaignReceipt(terminalReceipt),
				}
				if activeLaunch.attemptKind == campaignAttemptBaseline {
					terminalEvent.resolvedMutationDeadline = resolveBaselineMutationDeadline(
						terminalExecutionData(terminal).CommandDuration, trace.definition.campaign.peers,
					)
				}
				delivered = terminalEvent
			}
		default:
			return simulationReplayFailure(trace, "authority is invalid at record %d", index)
		}
	}

	return SimulationResult{
		trace: trace,
		world: simulationWorld{campaign: campaign, runtime: runtime, supervisor: supervisor},
	}
}

// ReplayViolation applies one malformed fact after a legal prefix and captures the guard's re-panic.
func ReplayViolation(prefix simulationTrace, malformed simulationMalformedFact) (result ViolationResult) {
	legal := ReplayLegal(prefix)
	if legal.failure != nil {
		return ViolationResult{failure: fmt.Errorf("legal prefix: %w", legal.failure)}
	}
	if malformed.authority != simulationCampaignAuthority || malformed.campaign == nil {
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

	_, _ = advanceCampaignGuarded(&runtime, campaign, campaignEvent{
		id: campaignEventID(len(campaign.trace) + 1), payload: malformed.campaign,
	}, func(closure runtimeClosure) emergencySweep {
		resolutions := make([]emergencyResolution, len(closure.residual))
		for index, residual := range closure.residual {
			resolutions[index] = emergencyResolution{
				generation: residual.generation, disposition: emergencyConfirmedDrained,
			}
		}

		return emergencySweep{resolutions: resolutions}
	})

	return ViolationResult{failure: fmt.Errorf("malformed fact was accepted")}
}

// Shrink removes semantic records and definition members while retaining one typed failure.
func Shrink(trace simulationTrace, key FailureKey) simulationTrace {
	if trace.malformed == nil {
		panic("simulation shrink requires one malformed fact")
	}
	shrunk := simulationCloneTrace(trace)
	for len(shrunk.records) != 0 {
		candidate := simulationCloneTrace(shrunk)
		candidate.records = candidate.records[:len(candidate.records)-1]
		if !simulationPreservesFailure(candidate, key) {
			break
		}
		shrunk = candidate
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
	first := ReplayViolation(shrunk, *shrunk.malformed)
	second := ReplayViolation(shrunk, *shrunk.malformed)
	if first.failure != nil || !reflect.DeepEqual(first.key, key) || !reflect.DeepEqual(first, second) {
		panic("simulation shrink did not retain a deterministic failure")
	}

	return shrunk
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
		return effects[0].kind == campaignEffectReleaseSnapshot ||
			effects[0].kind == campaignEffectReleaseWorkspace
	case workspaceMaterializedEvent:
		return effects[0].kind == campaignEffectMaterializeWorkspace
	default:
		return false
	}
}

func simulationReplayFailure(trace simulationTrace, format string, arguments ...any) SimulationResult {
	return SimulationResult{trace: trace, failure: fmt.Errorf(format, arguments...)}
}
