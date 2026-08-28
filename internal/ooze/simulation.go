package ooze

import (
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
)

const (
	observeOperation         = "observe attempt"
	settleEmergencyOperation = "settle emergency"
)

type simulationDuration int64

func simulationTraceDuration(value time.Duration) simulationDuration {
	return simulationDuration(value)
}

func (duration simulationDuration) production() time.Duration {
	return time.Duration(duration)
}

type simulationAuthority uint8

const (
	simulationCampaignAuthority simulationAuthority = iota + 1
	simulationRuntimeAuthority
	supervisionAuthority
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
	supervisor    supervisionProjection
}

type simulationRecord struct {
	sequence  uint64
	authority simulationAuthority
	source    simulationCausalSource

	campaignEvent   simulationCampaignEvent
	campaignState   simulationCampaignState
	campaignEffects []campaignEffect

	runtimeCut        processruntime.RecordedCut
	runtimeCorruption *processruntime.CorruptedCut
	runtimeState      simulationRuntimeState

	supervisorEvent       supervisionFact
	supervisorDomainEvent supervisorDomainEvent
	supervisorState       supervisionProjection
	supervisorActions     []supervisionEffect
}

type simulationWorld struct {
	campaign     campaignState
	runtime      simulationRuntimeState
	runtimeState processruntime.Replay
	supervisor   supervisionProjection
	machine      *supervisorMachine
}

type SimulationResult struct {
	trace   simulationTrace
	world   simulationWorld
	key     FailureKey
	failure error
}

type simulationMalformedFact struct {
	authority  simulationAuthority
	campaign   simulationCampaignEvent
	runtimeCut processruntime.Cut
	supervisor supervisionFact
}

type simulationFailureKind uint8

const (
	simulationInvariantFailureKind simulationFailureKind = iota + 1
	simulationLivenessFailureKind
	simulationReplayFailureKind
)

type FailureKey struct {
	property   string
	kind       simulationFailureKind
	authority  simulationAuthority
	divergence simulationReplayDivergence
	legality   simulationReplayLegality
	operation  string
	reason     string
	liveness   simulationLivenessKind
	identities []string
}

type simulationReplayDivergence uint8

type simulationReplayLegality uint8

const (
	simulationReplaySequenceFailure simulationReplayLegality = iota + 1
	simulationReplayEnablednessFailure
	simulationReplayCausalityFailure
	simulationReplayOperationFailure
	simulationReplayQuiescenceFailure
	simulationReplayCommutationFailure
)

const (
	simulationCampaignStateDivergence simulationReplayDivergence = iota + 1
	simulationCampaignEffectsDivergence
	simulationRuntimeStateDivergence
	simulationRegistrationDivergence
	simulationAdmissionDivergence
	simulationBarrierDivergence
	simulationConfirmationQueueDivergence
	simulationStartDivergence
	simulationObservationDivergence
	simulationEmergencyDivergence
	simulationTerminalDivergence
	simulationRuntimeClosureDivergence
	supervisionDivergence
)

type ViolationResult struct {
	world     simulationWorld
	invariant runtimeInvariantViolation
	key       FailureKey
	failure   error
}

func Explore(definition simulationDefinition, choices simulationChoiceSource) SimulationResult {
	if values, ok := choices.(simulationChoiceBytes); ok {
		choices = &simulationChoiceCursor{values: slices.Clone(values)}
	}
	definition.catalogue = append([]mutantIdentity(nil), definition.catalogue...)
	return simulationExploreEngine(definition, choices)
}

func simulationOnlySupervisorAction(
	actions []supervisionEffect,
	kind supervisorActionKind,
) supervisionEffect {
	var matched supervisionEffect
	count := 0
	for _, action := range actions {
		if action.kind != kind {
			continue
		}
		matched = action
		count++
	}
	if count != 1 {
		panic(fmt.Sprintf("simulation supervisor actions=%#v, want one %v", actions, kind))
	}

	return matched
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

func ReplayLegal(trace simulationTrace) SimulationResult {
	return simulationReplayLegal(trace, true)
}

func simulationReplayLegal(trace simulationTrace, verifyCommutation bool) (result SimulationResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = SimulationResult{trace: trace, failure: fmt.Errorf("replay invariant: %v", recovered)}
		}
	}()

	campaign, effects := beginCampaign(trace.definition.campaign)
	runtime := processruntime.NewReplay(trace.definition.capacity)
	supervisorMachine := newSupervisorMachine()
	var delivered campaignEventPayload
	pendingDeliveries := make(map[simulationCausalSource][]campaignEventPayload)
	activeLaunches := make(map[attemptGeneration]campaignEffect)
	terminalReceipts := make(map[attemptGeneration]observationResult)
	actionKinds := make(map[supervisorActionToken]supervisorActionKind)
	barrierAt := 0
	for index, record := range trace.records {
		if record.sequence != uint64(index+1) {
			return simulationReplayFailure(
				trace, simulationReplaySequenceFailure, "record %d has sequence %d", index, record.sequence,
			)
		}
		switch record.authority {
		case simulationRuntimeAuthority:
			delivered = nil
			switch record.runtimeCut.Operation() {
			case processruntime.RegisterCampaignOperation:
				registrationEffect, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return effect.kind == campaignEffectRegister
				})
				if !ok {
					return simulationReplayFailure(
						trace, simulationReplayEnablednessFailure,
						"registration is not enabled at record %d", index,
					)
				}
				if !record.runtimeCut.Matches(processruntime.RegisterCampaignCut(trace.definition.campaign.lineage)) {
					return simulationReplayFailure(trace, simulationReplayCausalityFailure,
						"registration input diverged at record %d", index)
				}
				_ = registrationEffect
				effects = remaining
				var matches bool
				runtime, matches = simulationReplayRuntimeCut(runtime, record)
				if !matches {
					return simulationReplayDivergenceFailure(trace, simulationRegistrationDivergence,
						"registration diverged at record %d", index)
				}
				registered := record.runtimeCut.Result().Registration()
				registration := campaignRegistrationEvidence(registered)
				delivered = campaignRegisteredEvent{registration: registration}
			case processruntime.RequestAdmissionOperation:
				requestEffect, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return effect.kind == campaignEffectRequestAdmission && record.runtimeCut.Matches(
						processruntime.RequestAdmissionCut(processRuntimeAdmission(effect.request)),
					)
				})
				if !ok {
					return simulationReplayFailure(
						trace, simulationReplayEnablednessFailure,
						"admission request is not enabled at record %d", index,
					)
				}
				effects = remaining
				var matches bool
				runtime, matches = simulationReplayRuntimeCut(runtime, record)
				if !matches {
					return simulationReplayDivergenceFailure(trace, simulationAdmissionDivergence,
						"admission decision diverged at record %d", index)
				}
				processed := record.runtimeCut.Result().Admission()
				admission := runtimeAdmissionResult(processed)
				if admission.decision != processruntime.AdmissionAccepted {
					delivered = admissionRejectedEvent{
						attempt: requestEffect.attempt, result: campaignAdmissionEvidence(admission),
						cause: "simulation admission rejected",
					}
					break
				}
				source := simulationCausalSource{
					kind: simulationOwnerDeliverySource, identity: record.sequence,
				}
				for _, grant := range admission.deliveries {
					pendingDeliveries[source] = append(pendingDeliveries[source], admissionGrantedEvent{
						attempt: grant.attempt, grant: campaignAdmissionValue(grant),
					})
				}
			case processruntime.CancelAdmissionOperation:
				cancelEffect, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return effect.kind == campaignEffectCancelAdmission && record.runtimeCut.Matches(
						processruntime.CancelAdmissionCut(processRuntimeAdmission(effect.request)),
					)
				})
				if !ok {
					return simulationReplayFailure(
						trace, simulationReplayEnablednessFailure,
						"admission cancellation is not enabled at record %d", index,
					)
				}
				effects = remaining
				var matches bool
				runtime, matches = simulationReplayRuntimeCut(runtime, record)
				if !matches {
					return simulationReplayDivergenceFailure(trace, simulationAdmissionDivergence,
						"admission cancellation diverged at record %d", index)
				}
				processed := record.runtimeCut.Result().Admission()
				cancelled := runtimeAdmissionResult(processed)
				delivered = admissionCancelledEvent{
					attempt: cancelEffect.attempt, request: cancelEffect.request,
					result: campaignAdmissionEvidence(cancelled),
				}
			case processruntime.ReturnGrantOperation:
				returnEffect, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return effect.kind == campaignEffectReturnAdmission && record.runtimeCut.Matches(
						processruntime.ReturnGrantCut(processRuntimeAdmission(effect.grant)),
					)
				})
				if !ok {
					return simulationReplayFailure(
						trace, simulationReplayEnablednessFailure,
						"grant return is not enabled at record %d", index,
					)
				}
				effects = remaining
				var matches bool
				runtime, matches = simulationReplayRuntimeCut(runtime, record)
				if !matches {
					return simulationReplayDivergenceFailure(trace, simulationAdmissionDivergence,
						"grant return diverged at record %d", index)
				}
				processed := record.runtimeCut.Result().Admission()
				returned := runtimeAdmissionResult(processed)
				delivered = grantReturnAcknowledgedEvent{
					grant: returnEffect.grant, result: campaignAdmissionEvidence(returned),
				}
			case processruntime.BindConfirmationBarrierOperation:
				bindingEffect, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					binding := runtimeBarrierBinding(effect.binding)
					return effect.kind == campaignEffectBindConfirmationBarrier && record.runtimeCut.Matches(
						processruntime.BindConfirmationBarrierCut(processruntime.Barrier{
							Campaign: binding.campaign, Attempt: string(binding.attempt),
							Profile: binding.profile, Deadline: binding.deadline,
						}),
					)
				})
				if !ok {
					return simulationReplayFailure(
						trace, simulationReplayEnablednessFailure,
						"confirmation barrier is not enabled at record %d", index,
					)
				}
				effects = remaining
				var matches bool
				runtime, matches = simulationReplayRuntimeCut(runtime, record)
				if !matches {
					return simulationReplayDivergenceFailure(trace, simulationBarrierDivergence,
						"confirmation barrier diverged at record %d", index)
				}
				processed := record.runtimeCut.Result().Barrier()
				bound := runtimeBarrierResult(processed)
				delivered = confirmationBarrierBoundEvent{
					attempt: bindingEffect.attempt, result: campaignBarrierEvidence(bound),
				}
			case processruntime.CompleteConfirmationQueueOperation:
				if !record.runtimeCut.Matches(processruntime.CompleteConfirmationQueueCut(campaign.runtimeToken)) {
					return simulationReplayFailure(trace, simulationReplayCausalityFailure,
						"confirmation queue input diverged at record %d", index)
				}
				var matches bool
				runtime, matches = simulationReplayRuntimeCut(runtime, record)
				if !matches {
					return simulationReplayDivergenceFailure(trace, simulationConfirmationQueueDivergence,
						"confirmation queue completion diverged at record %d", index)
				}
				processed := record.runtimeCut.Result().Queue()
				completed := runtimeQueueResult(processed)
				candidates := pendingDeliveries[record.source]
				terminalAt := slices.IndexFunc(candidates, func(candidate campaignEventPayload) bool {
					_, ok := candidate.(attemptTerminalEvent)

					return ok
				})
				if terminalAt < 0 {
					return simulationReplayFailure(
						trace, simulationReplayCausalityFailure,
						"confirmation queue has no causal terminal at record %d", index,
					)
				}
				terminal := candidates[terminalAt].(attemptTerminalEvent)
				terminal.receipt.confirmationQueueDrained = completed.decision == processruntime.ConfirmationQueueCompleted
				pendingDeliveries[record.source] = slices.Delete(candidates, terminalAt, terminalAt+1)
				delivered = terminal
			case processruntime.CommitStartOperation:
				startEffect, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return effect.kind == campaignEffectRequestStartCommitment && record.runtimeCut.Matches(
						processruntime.CommitStartCut(processRuntimeAdmission(effect.grant)),
					)
				})
				if !ok {
					return simulationReplayFailure(
						trace, simulationReplayEnablednessFailure,
						"start commitment is not enabled at record %d", index,
					)
				}
				effects = remaining
				var matches bool
				runtime, matches = simulationReplayRuntimeCut(runtime, record)
				if !matches {
					return simulationReplayDivergenceFailure(trace, simulationStartDivergence,
						"start commitment diverged at record %d", index)
				}
				processed := record.runtimeCut.Result().Start()
				started := runtimeStartResult(processed)
				delivered = startCommittedEvent{
					attempt: startEffect.attempt, grant: startEffect.grant, result: campaignStartEvidence(started),
				}
			case processruntime.ObserveAttemptOperation:
				generation, observed, ok := record.runtimeCut.Observation()
				if !ok {
					return simulationReplayFailure(trace, simulationReplayOperationFailure,
						"attempt observation input is unavailable at record %d", index)
				}
				var matches bool
				runtime, matches = simulationReplayRuntimeCut(runtime, record)
				if !matches {
					return simulationReplayDivergenceFailure(trace, simulationObservationDivergence,
						"attempt observation diverged at record %d", index)
				}
				processed := record.runtimeCut.Result().Receipt()
				observation := runtimeReceipt(processed)
				actionKind := supervisorActionKind(0)
				if record.source.kind == supervisionActionSource {
					actionKind = actionKinds[supervisorActionToken(record.source.identity)]
				}
				switch observed.Kind() {
				case processruntime.LaunchOwned:
					if actionKind != supervisorPublishOwned {
						break
					}
					activeLaunch, found := activeLaunches[generation]
					if !found {
						return simulationReplayFailure(
							trace, simulationReplayCausalityFailure,
							"owned observation has no causal launch at record %d", index,
						)
					}
					delivered = attemptLaunchEvent{
						attempt: activeLaunch.attempt, generation: activeLaunch.generation,
						result:  campaignLaunchObservation{kind: campaignLaunchOwned},
						receipt: campaignReceiptValue(observation),
					}
				case processruntime.LaunchNotReleased:
					if actionKind != supervisorPublishNotReleased {
						break
					}
					activeLaunch, found := activeLaunches[generation]
					if !found {
						return simulationReplayFailure(
							trace, simulationReplayCausalityFailure,
							"not-released observation has no causal launch at record %d", index,
						)
					}
					failure := LaunchFailed
					if observed.ResourceExhausted() {
						failure = LaunchResourceExhausted
					}
					delivered = attemptLaunchEvent{
						attempt: activeLaunch.attempt, generation: activeLaunch.generation,
						result: campaignLaunchObservation{
							kind: campaignLaunchNotReleased, failure: failure,
						},
						receipt: campaignReceiptValue(observation),
					}
				case processruntime.LaunchUnconfirmedKind:
					if actionKind != supervisorPublishLaunchUnconfirmed {
						break
					}
					activeLaunch, found := activeLaunches[generation]
					if !found {
						return simulationReplayFailure(
							trace, simulationReplayCausalityFailure,
							"unconfirmed observation has no causal launch at record %d", index,
						)
					}
					delivered = attemptLaunchEvent{
						attempt: activeLaunch.attempt, generation: activeLaunch.generation,
						result: campaignLaunchObservation{
							kind: campaignLaunchUnconfirmed, residual: ProspectiveUnresolved,
						},
						receipt: campaignReceiptValue(observation),
					}
				case processruntime.DrainUnconfirmedKind:
					terminalReceipts[generation] = observation
				default:
					terminalReceipts[generation] = observation
				}
			case processruntime.CommitTerminalOperation:
				_, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return effect.kind == campaignEffectProposeTerminal
				})
				if !ok {
					return simulationReplayFailure(
						trace, simulationReplayEnablednessFailure,
						"terminal commitment is not enabled at record %d", index,
					)
				}
				effects = remaining
				if !record.runtimeCut.Matches(processruntime.CommitTerminalCut(campaign.runtimeToken)) {
					return simulationReplayFailure(trace, simulationReplayCausalityFailure,
						"terminal input diverged at record %d", index)
				}
				var matches bool
				runtime, matches = simulationReplayRuntimeCut(runtime, record)
				if !matches {
					return simulationReplayDivergenceFailure(trace, simulationTerminalDivergence,
						"terminal commitment diverged at record %d", index)
				}
				processed := record.runtimeCut.Result().Terminal()
				terminal := terminalResult{decision: processed.Decision()}
				delivered = terminalCommittedEvent{result: campaignTerminalEvidence(terminal)}
			case processruntime.SettleEmergencyOperation:
				var matches bool
				runtime, matches = simulationReplayRuntimeCut(runtime, record)
				if !matches {
					return simulationReplayDivergenceFailure(trace, simulationEmergencyDivergence,
						"emergency settlement diverged at record %d", index)
				}
				processed := record.runtimeCut.Result().Settlement()
				settlement := runtimeEmergencySettlement(processed)
				delivered = runtimeEmergencySettledEvent{
					epoch: settlement.epoch, settlement: campaignSettlementValue(settlement),
				}
			case processruntime.AuthorizeForcedAbortOperation:
				_, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return effect.kind == campaignEffectProposeTerminal && record.runtimeCut.Matches(
						processruntime.AuthorizeForcedAbortCut(campaign.runtimeToken, uint64(effect.fatalEpoch)),
					)
				})
				if !ok {
					return simulationReplayFailure(
						trace, simulationReplayEnablednessFailure,
						"forced abort is not enabled at record %d", index,
					)
				}
				effects = remaining
				var matches bool
				runtime, matches = simulationReplayRuntimeCut(runtime, record)
				if !matches {
					return simulationReplayDivergenceFailure(trace, simulationTerminalDivergence,
						"forced abort diverged at record %d", index)
				}
				processed := record.runtimeCut.Result().Terminal()
				terminal := terminalResult{decision: processed.Decision()}
				delivered = terminalCommittedEvent{result: campaignTerminalEvidence(terminal)}
			case processruntime.CloseOperation:
				var matches bool
				runtime, matches = simulationReplayRuntimeCut(runtime, record)
				if !matches {
					return simulationReplayDivergenceFailure(trace, simulationRuntimeClosureDivergence,
						"runtime closure diverged at record %d", index)
				}
				processed := record.runtimeCut.Result().Closure()
				closure := runtimeClosureValue(processed)
				delivered = runtimeEmergencyStartedEvent{closure: campaignClosureValue(closure)}
			default:
				return simulationReplayFailure(
					trace, simulationReplayOperationFailure,
					"runtime operation is invalid at record %d", index,
				)
			}
			if delivered != nil {
				source := simulationCausalSource{
					kind: simulationOwnerDeliverySource, identity: record.sequence,
				}
				pendingDeliveries[source] = append(pendingDeliveries[source], delivered)
			}
			if !reflect.DeepEqual(simulationTraceRuntimeState(runtime), record.runtimeState) {
				return simulationReplayDivergenceFailure(
					trace, simulationRuntimeStateDivergence, "runtime state diverged at record %d", index,
				)
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
					trace, simulationReplayCausalityFailure,
					"causal campaign fact diverged at record %d: got=%#v want=%#v",
					index, delivered, payload,
				)
			}
			if delivered == nil {
				_, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return simulationEffectEnablesExternalFact(effect, payload)
				})
				if !ok {
					return simulationReplayFailure(
						trace, simulationReplayEnablednessFailure,
						"external campaign fact is not enabled at record %d (%s): source=%#v effects=%v deliveries=%v",
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
				return simulationReplayDivergenceFailure(
					trace, simulationCampaignStateDivergence,
					"campaign state diverged at record %d (%s)", index, payload.campaignEventName(),
				)
			}
			if !slices.EqualFunc(emitted, record.campaignEffects, func(left, right campaignEffect) bool {
				return reflect.DeepEqual(left, right)
			}) {
				return simulationReplayDivergenceFailure(
					trace, simulationCampaignEffectsDivergence,
					"campaign effects diverged at record %d (%s): got=%v want=%v",
					index, payload.campaignEventName(), simulationEffectKinds(emitted),
					simulationEffectKinds(record.campaignEffects),
				)
			}
			effects = append(effects, emitted...)
		case supervisionAuthority:
			fact := record.supervisorEvent
			if registration, registered := fact.Registration(); registered {
				launchEffect, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return effect.kind == campaignEffectLaunchAttempt &&
						supervisionRegistrationMatches(effect, registration)
				})
				if !ok {
					return simulationReplayFailure(
						trace, simulationReplayEnablednessFailure,
						"supervisor launch is not enabled at record %d", index,
					)
				}
				activeLaunches[registration.generation] = launchEffect
				effects = remaining
			}
			if generation, stopped := fact.StopGeneration(); stopped {
				_, remaining, ok := simulationTakeEffect(effects, func(effect campaignEffect) bool {
					return effect.kind == campaignEffectStopAttempt && effect.generation == generation
				})
				if !ok {
					return simulationReplayFailure(
						trace, simulationReplayEnablednessFailure,
						"supervisor stop is not enabled at record %d", index,
					)
				}
				effects = remaining
			}
			var transition supervisorTransition
			supervisorMachine, transition = supervisorMachine.Apply(fact)
			supervisionEffects := transition.Effects()
			for _, effect := range supervisionEffects {
				actionKinds[effect.token] = effect.kind
			}
			if transition.Event() != record.supervisorDomainEvent ||
				!supervisorMachine.Projection().Equal(record.supervisorState) ||
				!reflect.DeepEqual(transition.Effects(), record.supervisorActions) {
				return simulationReplayDivergenceFailure(
					trace, supervisionDivergence, "supervisor transition diverged at record %d", index,
				)
			}
			for _, effect := range supervisionEffects {
				if effect.kind != supervisorDeliverTerminal {
					continue
				}
				activeLaunch, found := activeLaunches[effect.generation]
				if !found {
					return simulationReplayFailure(
						trace, simulationReplayCausalityFailure,
						"runtime completion has no causal launch at record %d", index,
					)
				}
				terminal := publicTerminalFromEffect(effect, func(supervisorOutputRef) string { return "" }, nil)
				receipt, found := terminalReceipts[activeLaunch.generation]
				if !found {
					return simulationReplayFailure(
						trace, simulationReplayCausalityFailure,
						"terminal completion has no runtime receipt at record %d", index,
					)
				}
				delete(terminalReceipts, activeLaunch.generation)
				terminalEvent := attemptTerminalEvent{
					attempt: activeLaunch.attempt, generation: activeLaunch.generation,
					terminal: terminal, receipt: campaignReceiptValue(receipt),
				}
				pendingDeliveries[simulationCausalSource{
					kind: supervisionActionSource, identity: uint64(effect.token),
				}] = append(pendingDeliveries[simulationCausalSource{
					kind: supervisionActionSource, identity: uint64(effect.token),
				}], terminalEvent)
				delivered = terminalEvent
			}
			if record.supervisorEvent.kind == supervisorEmergencySettlementCompleted {
				deliverAt := slices.IndexFunc(supervisionEffects, func(effect supervisionEffect) bool {
					return effect.kind == supervisorDeliverEmergencySettlement
				})
				candidates := pendingDeliveries[record.source]
				settlementAt := slices.IndexFunc(candidates, func(candidate campaignEventPayload) bool {
					_, ok := candidate.(runtimeEmergencySettledEvent)

					return ok
				})
				if deliverAt < 0 || settlementAt < 0 {
					return simulationReplayFailure(
						trace, simulationReplayCausalityFailure,
						"emergency settlement has no causal delivery at record %d", index,
					)
				}
				settlement := candidates[settlementAt]
				pendingDeliveries[record.source] = slices.Delete(candidates, settlementAt, settlementAt+1)
				source := simulationCausalSource{
					kind: supervisionActionSource, identity: uint64(supervisionEffects[deliverAt].token),
				}
				pendingDeliveries[source] = append(pendingDeliveries[source], settlement)
			}
		default:
			return simulationReplayFailure(
				trace, simulationReplayOperationFailure, "authority is invalid at record %d", index,
			)
		}
		for barrierAt < len(trace.barriers) && trace.barriers[barrierAt].afterSequence == record.sequence {
			barrier := trace.barriers[barrierAt]
			if !reflect.DeepEqual(simulationTraceCampaignState(campaign), barrier.campaign) ||
				!reflect.DeepEqual(simulationTraceRuntimeState(runtime), barrier.runtime) ||
				!reflect.DeepEqual(supervisorMachine.Projection(), barrier.supervisor) {
				return simulationReplayFailure(
					trace, simulationReplayQuiescenceFailure,
					"quiescent world diverged after sequence %d", record.sequence,
				)
			}
			barrierAt++
		}
	}
	if barrierAt != len(trace.barriers) {
		return simulationReplayFailure(
			trace, simulationReplayQuiescenceFailure,
			"quiescent barrier %d has no accepted prefix", barrierAt,
		)
	}
	if verifyCommutation && len(trace.barriers) != 0 {
		for leftAt := 0; leftAt+1 < len(trace.records); leftAt++ {
			if !simulationRecordsAreIndependent(trace.records[leftAt], trace.records[leftAt+1]) {
				continue
			}
			if err := simulationVerifyAdjacentCommutation(trace, leftAt); err != nil {
				return simulationReplayFailure(
					trace, simulationReplayCommutationFailure,
					"independent owner cuts diverged at record %d: %v", leftAt, err,
				)
			}
		}
	}

	return SimulationResult{
		trace: trace,
		world: simulationWorld{
			campaign: campaign, runtime: simulationTraceRuntimeState(runtime), runtimeState: runtime,
			supervisor: supervisorMachine.Projection(), machine: supervisorMachine,
		},
	}
}

func simulationVerifyAdjacentCommutation(trace simulationTrace, leftAt int) error {
	if leftAt < 0 || leftAt+1 >= len(trace.records) {
		return fmt.Errorf("commutation pair is outside the trace")
	}
	left, right := trace.records[leftAt], trace.records[leftAt+1]
	if !simulationRecordsAreIndependent(left, right) {
		return fmt.Errorf("commutation pair is causally related")
	}
	prefix := simulationCloneTrace(trace)
	prefix.records = slices.Clone(prefix.records[:leftAt])
	prefix.barriers = slices.DeleteFunc(prefix.barriers, func(barrier simulationQuiescentBarrier) bool {
		return barrier.afterSequence > uint64(leftAt)
	})
	initial := simulationReplayLegal(prefix, false)
	if initial.failure != nil {
		return fmt.Errorf("commutation prefix: %w", initial.failure)
	}
	apply := func(first, second simulationRecord) (simulationWorld, error) {
		world, err := simulationApplyRecordedOwnerCut(initial.world, first)
		if err != nil {
			return simulationWorld{}, err
		}

		return simulationApplyRecordedOwnerCut(world, second)
	}
	forward, err := apply(left, right)
	if err != nil {
		return fmt.Errorf("forward commutation: %w", err)
	}
	reversed, err := apply(right, left)
	if err != nil {
		return fmt.Errorf("reversed commutation: %w", err)
	}
	if !reflect.DeepEqual(forward, reversed) {
		return fmt.Errorf("independent owner cuts changed the composed world")
	}

	return nil
}

func simulationRecordsAreIndependent(left, right simulationRecord) bool {
	return left.authority != right.authority &&
		!simulationRecordDependsOn(right, left) && !simulationRecordDependsOn(left, right)
}

func simulationRecordDependsOn(child, parent simulationRecord) bool {
	switch child.source.kind {
	case simulationOwnerDeliverySource:
		return child.source.identity == parent.sequence
	case simulationCampaignEffectSource:
		return slices.ContainsFunc(parent.campaignEffects, func(effect campaignEffect) bool {
			return uint64(effect.id) == child.source.identity
		})
	case supervisionActionSource:
		return slices.ContainsFunc(parent.supervisorActions, func(action supervisionEffect) bool {
			return uint64(action.token) == child.source.identity
		})
	default:
		return false
	}
}

func simulationApplyRecordedOwnerCut(world simulationWorld, record simulationRecord) (simulationWorld, error) {
	switch record.authority {
	case simulationCampaignAuthority:
		state, effects := advanceCampaign(world.campaign, record.campaignEvent.production())
		if !reflect.DeepEqual(simulationTraceCampaignState(state), record.campaignState) ||
			!reflect.DeepEqual(effects, record.campaignEffects) {
			return simulationWorld{}, fmt.Errorf("campaign owner cut diverged")
		}
		world.campaign = state
	case simulationRuntimeAuthority:
		state, err := simulationApplyRecordedRuntimeCut(world.runtimeState, record)
		if err != nil {
			return simulationWorld{}, err
		}
		world.runtimeState = state
		world.runtime = simulationTraceRuntimeState(state)
	case supervisionAuthority:
		machine := world.machine.Fork()
		machine, transition := machine.Apply(record.supervisorEvent)
		if transition.Event() != record.supervisorDomainEvent ||
			!machine.Projection().Equal(record.supervisorState) ||
			!reflect.DeepEqual(transition.Effects(), record.supervisorActions) {
			return simulationWorld{}, fmt.Errorf("supervisor owner cut diverged")
		}
		world.supervisor = machine.Projection()
		world.machine = machine
	default:
		return simulationWorld{}, fmt.Errorf("commutation authority is invalid")
	}

	return world, nil
}

func simulationApplyRecordedRuntimeCut(state processruntime.Replay, record simulationRecord) (processruntime.Replay, error) {
	return simulationApplyRuntimeCut(state, record, true)
}

func simulationApplyRuntimeCut(state processruntime.Replay, record simulationRecord, compareState bool) (processruntime.Replay, error) {
	if record.runtimeCut.Operation() == 0 {
		return processruntime.Replay{}, fmt.Errorf("runtime commutation operation is invalid")
	}
	state, matches := simulationReplayRuntimeCut(state, record)
	if !matches {
		return processruntime.Replay{}, fmt.Errorf("runtime owner cut diverged")
	}
	if compareState && !reflect.DeepEqual(simulationTraceRuntimeState(state), record.runtimeState) {
		return processruntime.Replay{}, fmt.Errorf("runtime owner state diverged")
	}

	return state, nil
}

func simulationReplayRuntimeCut(state processruntime.Replay, record simulationRecord) (processruntime.Replay, bool) {
	if record.runtimeCorruption != nil {
		return state.ApplyCorrupted(*record.runtimeCorruption)
	}
	return state.ApplyRecorded(record.runtimeCut)
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
		if _, ok := malformed.runtimeCut.Malformed(); !ok {
			return ViolationResult{failure: fmt.Errorf("malformed runtime operation is not implemented")}
		}
	case supervisionAuthority:
	default:
		return ViolationResult{failure: fmt.Errorf("malformed fact authority is not implemented")}
	}

	runtime := legal.world.runtimeState
	campaign := legal.world.campaign
	defer func() {
		recovered := recover()
		violation, ok := recovered.(runtimeInvariantViolation)
		if runtimeViolation, runtimeFailed := recovered.(processruntime.Violation); runtimeFailed {
			violation = runtimeInvariantViolation{operation: runtimeViolation.Operation(), reason: runtimeViolation.Reason()}
			ok = true
		}
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
				campaign: campaign, runtime: simulationTraceRuntimeState(runtime), runtimeState: runtime,
				supervisor: legal.world.supervisor,
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
		violation, _ := malformed.runtimeCut.Malformed()
		simulationAdvanceRuntimeGuarded(&runtime, "runtime violation replay", func(state processruntime.Replay) processruntime.Replay {
			return state.ApplyMalformed(violation)
		})
	case supervisionAuthority:
		simulationAdvanceSupervisorGuarded(&runtime, legal.world.machine, malformed.supervisor)
	}

	return ViolationResult{failure: fmt.Errorf("malformed fact was accepted")}
}

func simulationAdvanceRuntimeGuarded(
	runtime *processruntime.Replay,
	operation string,
	advance func(processruntime.Replay) processruntime.Replay,
) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		violation, ok := recovered.(runtimeInvariantViolation)
		if runtimeViolation, runtimeFailed := recovered.(processruntime.Violation); runtimeFailed {
			violation = runtimeInvariantViolation{operation: runtimeViolation.Operation(), reason: runtimeViolation.Reason()}
			ok = true
		}
		if !ok {
			violation = runtimeInvariantViolation{operation: operation, reason: "unexpected panic"}
		}
		simulationSettleInvariantCleanup(runtime, violation)
		panic(violation)
	}()

	*runtime = advance(*runtime)
}

func simulationAdvanceSupervisorGuarded(
	runtime *processruntime.Replay,
	machine *supervisorMachine,
	fact supervisionFact,
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
		simulationSettleInvariantCleanup(runtime, violation)
		panic(violation)
	}()

	_, _ = machine.Apply(fact)
}

func simulationSettleInvariantCleanup(runtime *processruntime.Replay, violation runtimeInvariantViolation) {
	defer func() {
		_ = recover()
	}()
	next, applied := runtime.Apply(processruntime.CloseCut(violation.reason))
	*runtime = next
	closure := runtimeClosureValue(applied.Closure())
	next, _ = runtime.Apply(processruntime.SettleEmergencyCut(processRuntimeResolutions(simulationEmergencySweep(*runtime, closure))))
	*runtime = next
}

func simulationEmergencySweep(runtime processruntime.Replay, closure runtimeClosure) emergencySweep {
	resolutions := make([]emergencyResolution, len(closure.residual))
	for index, residual := range closure.residual {
		disposition := emergencyCustodyTransferred
		if !residual.transferred && residual.stage == admissionOwned && !runtime.Accepts(processruntime.ObserveAttemptCut(
			residual.generation, processruntime.DrainUnconfirmed(),
		)) {
			disposition = emergencyConfirmedDrained
		}
		resolutions[index] = emergencyResolution{
			generation: residual.generation, disposition: disposition,
		}
	}

	return emergencySweep{resolutions: resolutions}
}

func Shrink(trace simulationTrace, key FailureKey) simulationTrace {
	shrunk := simulationCloneTrace(trace)
	for {
		before := simulationTraceShrinkMeasure(shrunk)
		candidate := simulationShrinkOnce(shrunk, key)
		after := simulationTraceShrinkMeasure(candidate)
		if !simulationShrinkMeasureLess(after, before) {
			return candidate
		}
		shrunk = candidate
	}
}

func simulationShrinkOnce(trace simulationTrace, key FailureKey) simulationTrace {
	if key.kind == simulationLivenessFailureKind {
		return simulationShrinkLivenessWith(trace, key, Explore)
	}
	if trace.malformed == nil && key.kind != simulationReplayFailureKind {
		panic("simulation shrink requires a reproducible failure")
	}
	shrunk := simulationCloneTrace(trace)
	for width := len(shrunk.records); width > 0; {
		accepted := false
		for start := 0; start+width <= len(shrunk.records); start++ {
			candidate := simulationCloneTrace(shrunk)
			candidate.records = slices.Delete(candidate.records, start, start+width)
			simulationRenumberRecords(candidate.records)
			if !simulationPreservesFailure(candidate, key) || !simulationTraceShrinks(candidate, shrunk) {
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
	shrunk = simulationShrinkDefinitionAndChoices(shrunk, simulationShrinkPreservers{
		definition: func(trace simulationTrace, definition simulationDefinition) (simulationTrace, bool) {
			candidate, ok := simulationExploreShrinkCandidate(trace, definition)

			return candidate, ok && simulationPreservesFailure(candidate, key)
		},
		choices: func(
			trace simulationTrace, definition simulationDefinition, choices []simulationChoiceRecord,
		) (simulationTrace, bool) {
			candidate, ok := simulationExploreShrinkCandidateWithChoices(
				trace, definition, &simulationShrinkChoiceSource{choices: slices.Clone(choices)},
			)

			return candidate, ok && simulationPreservesFailure(candidate, key)
		},
	})
	if key.kind == simulationReplayFailureKind {
		first := ReplayLegal(shrunk)
		second := ReplayLegal(shrunk)
		if first.failure == nil || !reflect.DeepEqual(first.key, key) || !reflect.DeepEqual(first, second) {
			panic("simulation shrink did not retain a deterministic failure")
		}
	} else {
		first := ReplayViolation(shrunk, *shrunk.malformed)
		second := ReplayViolation(shrunk, *shrunk.malformed)
		if first.failure != nil || !reflect.DeepEqual(first.key, key) || !reflect.DeepEqual(first, second) {
			panic("simulation shrink did not retain a deterministic failure")
		}
	}

	return shrunk
}

type simulationExploreEvaluator func(simulationDefinition, simulationChoiceSource) SimulationResult

type simulationShrinkPreservers struct {
	definition func(simulationTrace, simulationDefinition) (simulationTrace, bool)
	choices    func(simulationTrace, simulationDefinition, []simulationChoiceRecord) (simulationTrace, bool)
}

func simulationShrinkLivenessWith(
	trace simulationTrace,
	key FailureKey,
	evaluate simulationExploreEvaluator,
) simulationTrace {
	shrunk := simulationCloneTrace(trace)
	for {
		before := simulationTraceShrinkMeasure(shrunk)
		candidate := simulationShrinkLivenessOnceWith(shrunk, key, evaluate)
		after := simulationTraceShrinkMeasure(candidate)
		if !simulationShrinkMeasureLess(after, before) {
			return candidate
		}
		shrunk = candidate
	}
}

func simulationShrinkLivenessOnceWith(
	trace simulationTrace,
	key FailureKey,
	evaluate simulationExploreEvaluator,
) simulationTrace {
	shrunk := simulationCloneTrace(trace)
	preserves := func(definition simulationDefinition, choices []simulationChoiceRecord) (simulationTrace, bool) {
		result := evaluate(definition, &simulationShrinkChoiceSource{choices: slices.Clone(choices)})

		return result.trace, result.failure != nil && reflect.DeepEqual(result.key, key)
	}
	for width := len(shrunk.choices); width > 0; {
		accepted := false
		for start := 0; start+width <= len(shrunk.choices); start++ {
			choices := slices.Delete(slices.Clone(shrunk.choices), start, start+width)
			candidate, ok := preserves(shrunk.definition, choices)
			if !ok || !simulationTraceShrinks(candidate, shrunk) {
				continue
			}
			shrunk = candidate
			accepted = true
			break
		}
		if accepted {
			width = min(width, len(shrunk.choices))
			continue
		}
		width--
	}
	shrunk = simulationShrinkDefinitionAndChoices(shrunk, simulationShrinkPreservers{
		definition: func(trace simulationTrace, definition simulationDefinition) (simulationTrace, bool) {
			return preserves(definition, trace.choices)
		},
		choices: func(
			_ simulationTrace, definition simulationDefinition, choices []simulationChoiceRecord,
		) (simulationTrace, bool) {
			return preserves(definition, choices)
		},
	})
	first := evaluate(shrunk.definition, &simulationShrinkChoiceSource{choices: slices.Clone(shrunk.choices)})
	second := evaluate(shrunk.definition, &simulationShrinkChoiceSource{choices: slices.Clone(shrunk.choices)})
	if first.failure == nil || !reflect.DeepEqual(first.key, key) || !reflect.DeepEqual(first, second) {
		panic("simulation shrink did not retain a deterministic liveness failure")
	}

	return shrunk
}

func simulationShrinkDefinitionAndChoices(
	shrunk simulationTrace,
	preservers simulationShrinkPreservers,
) simulationTrace {
	for index := 0; index < len(shrunk.definition.catalogue); {
		definition := shrunk.definition
		definition.catalogue = slices.Delete(slices.Clone(definition.catalogue), index, index+1)
		candidate, ok := preservers.definition(shrunk, definition)
		if !ok || !simulationTraceShrinks(candidate, shrunk) {
			index++
			continue
		}
		shrunk = candidate
	}
	for parallelism := 1; parallelism < shrunk.definition.capacity; parallelism++ {
		definition := shrunk.definition
		definition.capacity = parallelism
		definition.campaign.peers = parallelism
		candidate, ok := preservers.definition(shrunk, definition)
		if !ok || !simulationTraceShrinks(candidate, shrunk) {
			continue
		}
		shrunk = candidate
		break
	}
	for choiceAt := 0; choiceAt < len(shrunk.choices); choiceAt++ {
		choice := shrunk.choices[choiceAt]
		if choice.recovery || choice.selected == 0 {
			continue
		}
		for selected := 0; selected < choice.selected; selected++ {
			choices := slices.Clone(shrunk.choices[:choiceAt+1])
			choices[choiceAt].selected = selected
			candidate, ok := preservers.choices(shrunk, shrunk.definition, choices)
			if !ok || !simulationTraceShrinks(candidate, shrunk) {
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
	if candidate, ok := preservers.choices(shrunk, canonical, shrunk.choices); ok &&
		simulationTraceShrinks(candidate, shrunk) {
		shrunk = candidate
	}

	return shrunk
}

func simulationTraceShrinkMeasure(trace simulationTrace) [4]int {
	return [4]int{
		len(trace.definition.catalogue) + trace.definition.capacity + trace.definition.campaign.peers,
		len(trace.records), simulationTracePayloadRank(trace), simulationTraceBoundaryDistance(trace),
	}
}

func simulationShrinkMeasureLess(left, right [4]int) bool {
	for index := range left {
		if left[index] != right[index] {
			return left[index] < right[index]
		}
	}

	return false
}

func simulationTraceShrinks(candidate, current simulationTrace) bool {
	return simulationShrinkMeasureLess(
		simulationTraceShrinkMeasure(candidate), simulationTraceShrinkMeasure(current),
	)
}

func simulationTracePayloadRank(trace simulationTrace) int {
	rank := simulationIdentityPayloadRank(trace.definition) + len(trace.choices)
	for _, record := range trace.records {
		rank += simulationRecordPayloadRank(record)
	}

	return rank
}

func simulationIdentityPayloadRank(definition simulationDefinition) int {
	rank := 0
	if definition.campaign.identity != "campaign-1" || definition.campaign.lineage != 1 {
		rank++
	}
	for index, mutant := range definition.catalogue {
		if mutant != mutantIdentity(fmt.Sprintf("mutant-%d", index+1)) {
			rank++
		}
	}

	return rank
}

func simulationRecordPayloadRank(record simulationRecord) int {
	rank := 1 + len(record.campaignEffects) + len(record.supervisorActions)
	switch record.authority {
	case simulationCampaignAuthority:
		rank += len(record.campaignEvent.catalogueDiscovered.mutants)
		rank += len(record.campaignEvent.workspaceMaterializationFailed.artifactResidue)
	case simulationRuntimeAuthority:
		rank += record.runtimeCut.Complexity()
	case supervisionAuthority:
		event := record.supervisorEvent
		rank += len(event.emergencySnapshots)
		for _, present := range []bool{
			event.completion != nil, event.running != nil, event.drain != nil, event.output != nil,
			event.seal != nil, event.release != nil, event.runtime != nil,
			event.emergencySettlement != nil,
		} {
			if present {
				rank++
			}
		}
		if event.running != nil {
			rank += len(event.running.facts)
		}
	}

	return rank
}

func simulationTraceBoundaryDistance(trace simulationTrace) int {
	distance := 0
	var origins []supervisionEffect
	for _, record := range trace.records {
		origins = append(origins, record.supervisorActions...)
		if record.authority != supervisionAuthority {
			continue
		}
		distance += record.supervisorState.BoundaryDistance(record.supervisorEvent, origins)
	}

	return distance
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
	replayFailure := ReplayLegal(trace)
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
		if trace.malformed == nil {
			candidate.records[candidateCut] = simulationRetainRecordedFailure(
				candidate.records[candidateCut], cut, replayFailure.key.divergence,
			)
		}
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

func simulationRetainRecordedFailure(
	candidate, failing simulationRecord,
	divergence simulationReplayDivergence,
) simulationRecord {
	switch candidate.authority {
	case simulationCampaignAuthority:
		if divergence == simulationCampaignStateDivergence {
			candidate.campaignState = failing.campaignState
		}
		if divergence == simulationCampaignEffectsDivergence {
			candidate.campaignEffects = slices.Clone(failing.campaignEffects)
		}
	case simulationRuntimeAuthority:
		switch divergence {
		case simulationRuntimeStateDivergence:
			candidate.runtimeState = failing.runtimeState
		default:
			if corrupted, ok := candidate.runtimeCut.ExpectResultFrom(failing.runtimeCut); ok {
				candidate.runtimeCorruption = &corrupted
			}
		}
	case supervisionAuthority:
		if divergence == supervisionDivergence {
			candidate.supervisorState = failing.supervisorState
			candidate.supervisorActions = slices.Clone(failing.supervisorActions)
		}
	}

	return candidate
}

func simulationRepairMalformedCut(
	malformed simulationMalformedFact,
	before, after simulationRecord,
) simulationMalformedFact {
	if malformed.authority != supervisionAuthority ||
		before.authority != supervisionAuthority || after.authority != supervisionAuthority {
		return malformed
	}
	if malformed.supervisor.generation == before.supervisorEvent.generation {
		malformed.supervisor.generation = after.supervisorEvent.generation
	}
	if malformed.supervisor.kind == before.supervisorEvent.kind {
		malformed.supervisor.kind = after.supervisorEvent.kind
	}
	if malformed.supervisor.attempt == before.supervisorEvent.attempt {
		malformed.supervisor.attempt = after.supervisorEvent.attempt
	}
	if malformed.supervisor.at == before.supervisorEvent.at {
		malformed.supervisor.at = after.supervisorEvent.at
	}
	if malformed.supervisor.launchBy == before.supervisorEvent.launchBy {
		malformed.supervisor.launchBy = after.supervisorEvent.launchBy
	}
	if malformed.supervisor.drainBy == before.supervisorEvent.drainBy {
		malformed.supervisor.drainBy = after.supervisorEvent.drainBy
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
		return left.runtimeCut.Operation() == right.runtimeCut.Operation()
	case supervisionAuthority:
		leftKind, rightKind := left.supervisorEvent.kind, right.supervisorEvent.kind
		if (leftKind == supervisorLaunchCompleted || leftKind == supervisorLaunchBoundary) &&
			(rightKind == supervisorLaunchCompleted || rightKind == supervisorLaunchBoundary) {
			return true
		}

		return leftKind == rightKind
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
	trace.barriers = slices.Clone(trace.barriers)
	trace.choices = slices.Clone(trace.choices)
	if trace.malformed != nil {
		malformed := *trace.malformed
		trace.malformed = &malformed
	}

	return trace
}

func simulationPreservesFailure(trace simulationTrace, key FailureKey) bool {
	if key.kind == simulationReplayFailureKind {
		result := ReplayLegal(trace)

		return result.failure != nil && reflect.DeepEqual(result.key, key)
	}
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
		property: "ReplayViolation", kind: simulationInvariantFailureKind,
		authority: authority, operation: violation.operation, reason: violation.reason,
		identities: identities,
	}
}

func supervisionRegistrationMatches(effect campaignEffect, registration supervisionRegistration) bool {
	return registration.generation == effect.generation && registration.attempt == effect.attempt &&
		registration.profile == effect.spec.Profile && registration.commandDeadline == effect.spec.Deadline
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

func simulationReplayFailure(
	trace simulationTrace,
	legality simulationReplayLegality,
	format string,
	arguments ...any,
) SimulationResult {
	authority := simulationAuthority(0)
	if len(arguments) != 0 {
		if index, ok := arguments[0].(int); ok && index >= 0 && index < len(trace.records) {
			authority = trace.records[index].authority
		}
	}

	return SimulationResult{
		trace: trace,
		key: FailureKey{
			property: "ReplayLegal", kind: simulationReplayFailureKind,
			authority: authority, legality: legality,
		},
		failure: fmt.Errorf(format, arguments...),
	}
}

func simulationReplayDivergenceFailure(
	trace simulationTrace,
	divergence simulationReplayDivergence,
	format string,
	arguments ...any,
) SimulationResult {
	result := simulationReplayFailure(trace, 0, format, arguments...)
	result.key.divergence = divergence
	result.key.operation = ""

	return result
}
