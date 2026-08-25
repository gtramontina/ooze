package ooze

import (
	"fmt"
	"reflect"
	"slices"
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

type simulationShrinkChoiceSource struct {
	choices []simulationChoiceRecord
	at      int
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
	barriers   []simulationQuiescentBarrier
	choices    []simulationChoiceRecord
	malformed  *simulationMalformedFact
}

type simulationChoiceRecord struct {
	limit    int
	selected int
	recovery bool
}

type simulationQuiescentBarrier struct {
	afterSequence uint64
	campaign      simulationCampaignState
	runtime       simulationRuntimeState
	supervisor    simulationSupervisorState
}

type simulationRecord struct {
	sequence  uint64
	authority simulationAuthority
	source    simulationCausalSource

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
	return simulationExploreEngine(definition, choices)
}

func simulationOnlySupervisorAction(
	actions []supervisorAction,
	kind supervisorActionKind,
) supervisorAction {
	moves := simulationEnabledMoves(nil, actions, nil)
	if len(moves) != 1 || moves[0].action.kind != kind {
		panic(fmt.Sprintf("simulation supervisor actions=%#v, want one %v", actions, kind))
	}

	return moves[0].action
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
	pendingDeliveries := make(map[simulationCausalSource][]campaignEventPayload)
	activeLaunches := make(map[attemptGeneration]campaignEffect)
	terminalReceipts := make(map[attemptGeneration]observationResult)
	barrierAt := 0
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
			case simulationCancelAdmission:
				cancelEffect, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return effect.kind == campaignEffectCancelAdmission && reflect.DeepEqual(
						record.runtimeAdmissionToken, simulationTraceAdmission(runtimeAdmissionRequest(effect.request)),
					)
				})
				if !ok {
					return simulationReplayFailure(trace, "admission cancellation is not enabled at record %d", index)
				}
				effects = remaining
				var cancelled admissionResult
				runtime, cancelled = runtime.cancelAdmission(record.runtimeAdmissionToken.production())
				if !reflect.DeepEqual(simulationTraceAdmissionResult(cancelled), record.runtimeAdmissionOut) {
					return simulationReplayFailure(trace, "admission cancellation diverged at record %d", index)
				}
				delivered = admissionCancelledEvent{
					attempt: cancelEffect.attempt, request: cancelEffect.request,
					result: campaignAdmissionEvidence(cancelled),
				}
			case simulationAcknowledgeGrantReturn:
				returnEffect, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return effect.kind == campaignEffectReturnAdmission && reflect.DeepEqual(
						record.runtimeGrant, simulationTraceAdmission(runtimeAdmissionRequest(effect.grant)),
					)
				})
				if !ok {
					return simulationReplayFailure(trace, "grant return is not enabled at record %d", index)
				}
				effects = remaining
				var returned admissionResult
				runtime, returned = runtime.acknowledgeGrantReturn(record.runtimeGrant.production())
				if !reflect.DeepEqual(simulationTraceAdmissionResult(returned), record.runtimeAdmissionOut) {
					return simulationReplayFailure(trace, "grant return diverged at record %d", index)
				}
				delivered = grantReturnAcknowledgedEvent{
					grant: returnEffect.grant, result: campaignAdmissionEvidence(returned),
				}
			case simulationBindConfirmationBarrier:
				bindingEffect, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return effect.kind == campaignEffectBindConfirmationBarrier && reflect.DeepEqual(
						record.runtimeBarrier, simulationTraceBarrierBinding(runtimeBarrierBinding(effect.binding)),
					)
				})
				if !ok {
					return simulationReplayFailure(trace, "confirmation barrier is not enabled at record %d", index)
				}
				effects = remaining
				var bound barrierResult
				runtime, bound = runtime.sealAndBindConfirmationBarrier(record.runtimeBarrier.production())
				if !reflect.DeepEqual(simulationTraceBarrierResult(bound), record.runtimeBarrierOut) {
					return simulationReplayFailure(trace, "confirmation barrier diverged at record %d", index)
				}
				delivered = confirmationBarrierBoundEvent{
					attempt: bindingEffect.attempt, result: campaignBarrierEvidence(bound),
				}
			case simulationCompleteConfirmationQueue:
				var completed confirmationQueueResult
				runtime, completed = runtime.completeConfirmationQueue(record.runtimeCampaign)
				if !reflect.DeepEqual(simulationTraceConfirmationQueueResult(completed), record.runtimeQueueOut) {
					return simulationReplayFailure(trace, "confirmation queue completion diverged at record %d", index)
				}
				candidates := pendingDeliveries[record.source]
				terminalAt := slices.IndexFunc(candidates, func(candidate campaignEventPayload) bool {
					_, ok := candidate.(attemptTerminalEvent)

					return ok
				})
				if terminalAt < 0 {
					return simulationReplayFailure(trace, "confirmation queue has no causal terminal at record %d", index)
				}
				terminal := candidates[terminalAt].(attemptTerminalEvent)
				terminal.receipt.confirmationQueueDrained = completed.decision == confirmationQueueCompleted
				pendingDeliveries[record.source] = slices.Delete(candidates, terminalAt, terminalAt+1)
				delivered = terminal
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
			case simulationSettleEmergency:
				var settlement emergencySettlement
				runtime, settlement = runtime.settleEmergency(record.runtimeSweep.production())
				if !reflect.DeepEqual(simulationTraceEmergencySettlement(settlement), record.runtimeEmergencyOut) {
					return simulationReplayFailure(trace, "emergency settlement diverged at record %d", index)
				}
				delivered = runtimeEmergencySettledEvent{
					epoch: settlement.epoch, settlement: campaignSettlement(settlement),
				}
			case simulationAuthorizeForcedAbort:
				_, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return effect.kind == campaignEffectProposeTerminal && effect.fatalEpoch == record.runtimeFatalEpoch
				})
				if !ok {
					return simulationReplayFailure(trace, "forced abort is not enabled at record %d", index)
				}
				effects = remaining
				var terminal terminalResult
				runtime, terminal = runtime.authorizeForcedAbort(record.runtimeCampaign, record.runtimeFatalEpoch)
				if !reflect.DeepEqual(terminal, record.runtimeTerminal) {
					return simulationReplayFailure(trace, "forced abort diverged at record %d", index)
				}
				delivered = terminalCommittedEvent{result: campaignTerminalEvidence(terminal)}
			case simulationCloseRuntime:
				var closure runtimeClosure
				runtime, closure = runtime.closeRuntime(record.runtimeFatalCause)
				if !reflect.DeepEqual(simulationTraceRuntimeClosure(closure), record.runtimeClosure) {
					return simulationReplayFailure(trace, "runtime closure diverged at record %d", index)
				}
				delivered = runtimeEmergencyStartedEvent{closure: campaignClosure(closure)}
			default:
				return simulationReplayFailure(trace, "runtime operation is invalid at record %d", index)
			}
			if delivered != nil {
				source := simulationCausalSource{
					kind: simulationOwnerDeliverySource, identity: record.sequence,
				}
				pendingDeliveries[source] = append(pendingDeliveries[source], delivered)
			}
			if !reflect.DeepEqual(simulationTraceRuntimeState(runtime), record.runtimeState) {
				return simulationReplayFailure(trace, "runtime state diverged at record %d", index)
			}
		case simulationCampaignAuthority:
			payload := record.campaignEvent.production().payload
			if record.source.kind != 0 {
				candidates := pendingDeliveries[record.source]
				delivered = nil
				for candidateAt, candidate := range candidates {
					candidate = simulationCausalCampaignPayload(payload, candidate)
					if !reflect.DeepEqual(payload, candidate) {
						continue
					}
					delivered = candidate
					candidates = slices.Delete(candidates, candidateAt, candidateAt+1)
					pendingDeliveries[record.source] = candidates
					break
				}
			}
			if payload == nil {
				payload = delivered
			}
			if delivered != nil {
				delivered = simulationCausalCampaignPayload(payload, delivered)
			}
			if delivered != nil && !reflect.DeepEqual(payload, delivered) {
				return simulationReplayFailure(
					trace, "causal campaign fact diverged at record %d: got=%#v want=%#v",
					index, delivered, payload,
				)
			}
			if delivered == nil {
				_, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return simulationEffectEnablesExternalFact(effect, payload)
				})
				if !ok {
					return simulationReplayFailure(
						trace, "external campaign fact is not enabled at record %d (%s): source=%#v effects=%v deliveries=%v",
						index, payload.campaignEventName(), record.source,
						simulationEffectKinds(effects), pendingDeliveries,
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
				pendingDeliveries[simulationCausalSource{
					kind: simulationSupervisorActionSource, identity: uint64(deliver.token),
				}] = append(pendingDeliveries[simulationCausalSource{
					kind: simulationSupervisorActionSource, identity: uint64(deliver.token),
				}], terminalEvent)
				delivered = terminalEvent
			}
		default:
			return simulationReplayFailure(trace, "authority is invalid at record %d", index)
		}
		for barrierAt < len(trace.barriers) && trace.barriers[barrierAt].afterSequence == record.sequence {
			barrier := trace.barriers[barrierAt]
			if !reflect.DeepEqual(simulationTraceCampaignState(campaign), barrier.campaign) ||
				!reflect.DeepEqual(simulationTraceRuntimeState(runtime), barrier.runtime) ||
				!reflect.DeepEqual(simulationTraceSupervisorState(supervisor), barrier.supervisor) {
				return simulationReplayFailure(trace, "quiescent world diverged after sequence %d", record.sequence)
			}
			barrierAt++
		}
	}
	if barrierAt != len(trace.barriers) {
		return simulationReplayFailure(trace, "quiescent barrier %d has no accepted prefix", barrierAt)
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
		}, func(closure runtimeClosure) emergencySweep {
			return simulationEmergencySweep(runtime, closure)
		})
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
		*runtime, _ = runtime.settleEmergency(simulationEmergencySweep(*runtime, closure))
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
		*runtime, _ = runtime.settleEmergency(simulationEmergencySweep(*runtime, closure))
		panic(violation)
	}()

	_, _ = reduceSupervisor(state, event)
}

func simulationEmergencySweep(runtime processRuntime, closure runtimeClosure) emergencySweep {
	resolutions := make([]emergencyResolution, len(closure.residual))
	for index, residual := range closure.residual {
		disposition := emergencyCustodyTransferred
		admissionAt := runtime.admissionIndexByGeneration(residual.generation)
		if admissionAt >= 0 && runtime.admissions[admissionAt].disposition == dispositionTerminalDeferred {
			disposition = emergencyConfirmedDrained
		}
		resolutions[index] = emergencyResolution{
			generation: residual.generation, disposition: disposition,
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
		definition := shrunk.definition
		definition.catalogue = slices.Delete(slices.Clone(definition.catalogue), index, index+1)
		candidate, ok := simulationExploreShrinkCandidate(shrunk, definition)
		if !ok {
			index++
			continue
		}
		if simulationPreservesFailure(candidate, key) {
			shrunk = candidate
			continue
		}
		index++
	}
	for parallelism := 1; parallelism < shrunk.definition.capacity; parallelism++ {
		definition := shrunk.definition
		definition.capacity = parallelism
		definition.campaign.peers = parallelism
		candidate, ok := simulationExploreShrinkCandidate(shrunk, definition)
		if !ok {
			continue
		}
		if simulationPreservesFailure(candidate, key) {
			shrunk = candidate
			break
		}
	}
	for choiceAt := 0; choiceAt < len(shrunk.choices); choiceAt++ {
		choice := shrunk.choices[choiceAt]
		if choice.recovery || choice.selected == 0 {
			continue
		}
		for selected := 0; selected < choice.selected; selected++ {
			choices := slices.Clone(shrunk.choices)
			choices[choiceAt].selected = selected
			candidate, ok := simulationExploreShrinkCandidateWithChoices(
				shrunk, shrunk.definition, &simulationShrinkChoiceSource{choices: choices},
			)
			if !ok || !simulationPreservesFailure(candidate, key) {
				continue
			}
			shrunk = candidate
			break
		}
	}
	canonical := shrunk.definition
	canonical.campaign.identity = "campaign-1"
	canonical.campaign.lineage = 1
	canonical.catalogue = make([]mutantIdentity, len(shrunk.definition.catalogue))
	for index := range canonical.catalogue {
		canonical.catalogue[index] = mutantIdentity(fmt.Sprintf("mutant-%d", index+1))
	}
	if candidate, ok := simulationExploreShrinkCandidateWithChoices(
		shrunk, canonical, &simulationShrinkChoiceSource{choices: slices.Clone(shrunk.choices)},
	); ok && simulationPreservesFailure(candidate, key) {
		shrunk = candidate
	}
	first := ReplayViolation(shrunk, *shrunk.malformed)
	second := ReplayViolation(shrunk, *shrunk.malformed)
	if first.failure != nil || !reflect.DeepEqual(first.key, key) || !reflect.DeepEqual(first, second) {
		panic("simulation shrink did not retain a deterministic failure")
	}

	return shrunk
}

func simulationExploreShrinkCandidate(
	trace simulationTrace,
	definition simulationDefinition,
) (simulationTrace, bool) {
	return simulationExploreShrinkCandidateWithChoices(trace, definition, nil)
}

func simulationExploreShrinkCandidateWithChoices(
	trace simulationTrace,
	definition simulationDefinition,
	choices simulationChoiceSource,
) (simulationTrace, bool) {
	explored := Explore(definition, choices)
	if explored.failure != nil {
		return simulationTrace{}, false
	}
	candidate := explored.trace
	if len(trace.records) == 0 {
		candidate.records = nil
	} else {
		cut := trace.records[len(trace.records)-1]
		ordinal := 0
		for _, record := range trace.records {
			if simulationSameRecordKind(record, cut) {
				ordinal++
			}
		}
		candidateCut := -1
		for index, record := range candidate.records {
			if !simulationSameRecordKind(record, cut) {
				continue
			}
			ordinal--
			if ordinal == 0 {
				candidateCut = index
				break
			}
		}
		if candidateCut < 0 {
			return simulationTrace{}, false
		}
		candidate.records = slices.Clone(candidate.records[:candidateCut+1])
	}
	if trace.malformed != nil {
		malformed := *trace.malformed
		if len(trace.records) != 0 && len(candidate.records) != 0 {
			malformed = simulationRepairMalformedCut(
				malformed, trace.records[len(trace.records)-1], candidate.records[len(candidate.records)-1],
			)
		}
		candidate.malformed = &malformed
	}

	return candidate, true
}

func simulationRepairMalformedCut(
	malformed simulationMalformedFact,
	before, after simulationRecord,
) simulationMalformedFact {
	if malformed.authority != simulationSupervisorAuthority ||
		before.authority != simulationSupervisorAuthority || after.authority != simulationSupervisorAuthority {
		return malformed
	}
	if malformed.supervisor.generation == before.supervisorEvent.generation {
		malformed.supervisor.generation = after.supervisorEvent.generation
	}
	if malformed.supervisor.attempt == before.supervisorEvent.attempt {
		malformed.supervisor.attempt = after.supervisorEvent.attempt
	}
	completion := malformed.supervisor.completion
	beforeCompletion := before.supervisorEvent.completion
	afterCompletion := after.supervisorEvent.completion
	if completion == nil || beforeCompletion == nil || afterCompletion == nil {
		return malformed
	}
	copy := *completion
	if copy.generation == beforeCompletion.generation {
		copy.generation = afterCompletion.generation
	}
	if copy.action == beforeCompletion.action {
		copy.action = afterCompletion.action
	}
	if copy.at == beforeCompletion.at {
		copy.at = afterCompletion.at
	}
	malformed.supervisor.completion = &copy

	return malformed
}

func (source *simulationShrinkChoiceSource) choose(limit int) int {
	if source.at >= len(source.choices) {
		return 0
	}
	selected := source.choices[source.at].selected
	source.at++

	return selected % limit
}

func (source *simulationShrinkChoiceSource) exhausted() bool {
	return source.at >= len(source.choices) || source.choices[source.at].recovery
}

func simulationSameRecordKind(left, right simulationRecord) bool {
	if left.authority != right.authority {
		return false
	}
	switch left.authority {
	case simulationCampaignAuthority:
		return left.campaignEvent.kind == right.campaignEvent.kind
	case simulationRuntimeAuthority:
		return left.runtimeOperation == right.runtimeOperation
	case simulationSupervisorAuthority:
		return left.supervisorEvent.kind == right.supervisorEvent.kind
	default:
		return false
	}
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
