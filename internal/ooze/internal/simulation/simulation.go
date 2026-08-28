package simulation

import (
	"fmt"
	"reflect"
	"slices"
	"time"

	campaignmodule "github.com/gtramontina/ooze/internal/ooze/internal/campaign"
	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
)

const (
	observeOperation         = "observe attempt"
	settleEmergencyOperation = "settle emergency"
)

type attemptIdentity = supervision.Identity
type attemptGeneration = processruntime.Generation
type mutantIdentity = string
type runtimeInvariantViolation = campaignmodule.Violation
type campaignRegistration = processruntime.Registration
type observationResult = processruntime.Receipt
type emergencySettlement = processruntime.EmergencySettlement
type admissionGrant = processruntime.Admission
type admissionResult = processruntime.AdmissionResult
type confirmationQueueResult = processruntime.QueueResult
type startCommittedResult = processruntime.StartResult
type runtimeClosure = processruntime.Closure
type emergencySweep = []processruntime.Resolution

type simulationAuthority uint8

const (
	simulationCampaignAuthority simulationAuthority = iota + 1
	simulationRuntimeAuthority
	supervisionAuthority
)

const simulationChooseBaselineFailure byte = 1

type simulationDefinition struct {
	campaign  campaignmodule.Definition
	capacity  int
	catalogue []string
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
	campaign      campaignmodule.Projection
	runtime       simulationRuntimeState
	supervisor    supervision.Projection
}

type simulationRecord struct {
	sequence  uint64
	authority simulationAuthority
	source    simulationCausalSource

	campaignEvent   campaignmodule.Event
	campaignState   campaignmodule.Projection
	campaignEffects []campaignmodule.Effect

	runtimeCut        processruntime.RecordedCut
	runtimeCorruption *processruntime.CorruptedCut
	runtimeState      simulationRuntimeState

	supervisorEvent       supervision.Fact
	supervisorDomainEvent supervision.Event
	supervisorState       supervision.Projection
	supervisorActions     []supervision.Effect
}

type simulationWorld struct {
	campaign     campaignmodule.Machine
	runtime      simulationRuntimeState
	runtimeState processruntime.Replay
	supervisor   supervision.Projection
	machine      *supervision.Machine
}

type SimulationResult struct {
	trace   simulationTrace
	world   simulationWorld
	key     FailureKey
	failure error
}

type simulationMalformedFact struct {
	authority  simulationAuthority
	campaign   campaignmodule.Fact
	runtimeCut processruntime.Cut
	supervisor supervision.Fact
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
	trace     simulationTrace
	world     simulationWorld
	invariant runtimeInvariantViolation
	key       FailureKey
	failure   error
}

func explore(definition simulationDefinition, choices simulationChoiceSource) SimulationResult {
	if values, ok := choices.(simulationChoiceBytes); ok {
		choices = &simulationChoiceCursor{values: slices.Clone(values)}
	}
	definition.catalogue = append([]string(nil), definition.catalogue...)
	return simulationExploreEngine(definition, choices)
}

func simulationOnlySupervisorAction(
	actions []supervision.Effect,
	kind supervision.EffectKind,
) supervision.Effect {
	var matched supervision.Effect
	count := 0
	for _, action := range actions {
		if action.Kind() != kind {
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

func equalSupervisionEffects(left, right []supervision.Effect) bool {
	return slices.EqualFunc(left, right, func(left, right supervision.Effect) bool {
		return left.Equal(right)
	})
}

func simulationAdvanceCampaign(
	machine campaignmodule.Machine,
	fact campaignmodule.Fact,
) (campaignmodule.Machine, campaignmodule.Transition) {
	return machine.Apply(fact)
}

func simulationCampaignRecord(
	trace simulationTrace,
	machine campaignmodule.Machine,
	transition campaignmodule.Transition,
) simulationRecord {
	effects := transition.Effects()
	for index, effect := range effects {
		effects[index] = effect.Canonical(machine.Projection())
	}
	return simulationRecord{
		sequence: uint64(len(trace.records) + 1), authority: simulationCampaignAuthority,
		campaignEvent: transition.Event().Canonical(), campaignState: machine.Projection().Canonical(),
		campaignEffects: effects,
	}
}

func replayLegal(trace simulationTrace) SimulationResult {
	return simulationReplayLegal(trace, true)
}

func simulationReplayLegal(trace simulationTrace, verifyCommutation bool) (result SimulationResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = SimulationResult{trace: trace, failure: fmt.Errorf("replay invariant: %v", recovered)}
		}
	}()

	campaign, initial := campaignmodule.NewMachine(trace.definition.campaign)
	effects := initial.Effects()
	runtime := processruntime.NewReplay(trace.definition.capacity)
	supervisorMachine := supervision.NewMachine()
	var delivered campaignmodule.Fact
	pendingDeliveries := make(map[simulationCausalSource][]campaignmodule.Fact)
	activeLaunches := make(map[attemptGeneration]campaignmodule.Effect)
	terminalReceipts := make(map[attemptGeneration]observationResult)
	actionKinds := make(map[supervision.ActionToken]supervision.EffectKind)
	var runtimeCampaign processruntime.Campaign
	var runtimeBinding campaignmodule.RuntimeBinding
	barrierAt := 0
	for index, record := range trace.records {
		if record.sequence != uint64(index+1) {
			return simulationReplayFailure(
				trace, simulationReplaySequenceFailure, "record %d has sequence %d", index, record.sequence,
			)
		}
		switch record.authority {
		case simulationRuntimeAuthority:
			delivered = campaignmodule.Fact{}
			request, remaining, campaignRequest := simulationTakeRuntimeRequest(
				effects, runtimeBinding, trace.definition.campaign, record.runtimeCut,
			)
			if campaignRequest {
				effects = remaining
				var matches bool
				runtime, matches = simulationReplayRuntimeCut(runtime, record)
				if !matches {
					return simulationReplayDivergenceFailure(trace, simulationRuntimeStateDivergence,
						"campaign runtime request diverged at record %d", index)
				}
				processed := record.runtimeCut.Result()
				if record.runtimeCut.Operation() == processruntime.RegisterCampaignOperation {
					registered := processed.Registration()
					runtimeCampaign = registered.Campaign()
					runtimeBinding = campaignmodule.BindRuntime(registered)
				}
				source := simulationCausalSource{kind: simulationOwnerDeliverySource, identity: record.sequence}
				pendingDeliveries[source] = append(pendingDeliveries[source], request.Complete(record.runtimeCut)...)
			} else {
				switch record.runtimeCut.Operation() {
				case processruntime.CompleteConfirmationQueueOperation:
					if !record.runtimeCut.Matches(processruntime.CompleteConfirmationQueueCut(runtimeCampaign)) {
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
					terminalAt := slices.IndexFunc(candidates, func(candidate campaignmodule.Fact) bool {
						return candidate.IsAttemptTerminal()
					})
					if terminalAt < 0 {
						return simulationReplayFailure(
							trace, simulationReplayCausalityFailure,
							"confirmation queue has no causal terminal at record %d", index,
						)
					}
					terminal := candidates[terminalAt].WithConfirmationQueueCompleted(completed)
					pendingDeliveries[record.source] = slices.Delete(candidates, terminalAt, terminalAt+1)
					delivered = terminal
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
					observation := record.runtimeCut.Result().Receipt()
					actionKind := supervision.EffectKind(0)
					if record.source.kind == supervisionActionSource {
						actionKind = actionKinds[supervision.ActionToken(record.source.identity)]
					}
					switch observed.Kind() {
					case processruntime.LaunchOwned:
						if actionKind != supervision.PublishOwnedEffect {
							break
						}
						activeLaunch, found := activeLaunches[generation]
						if !found {
							return simulationReplayFailure(
								trace, simulationReplayCausalityFailure,
								"owned observation has no causal launch at record %d", index,
							)
						}
						delivered = campaignmodule.AttemptLaunched(activeLaunch, supervision.Owned{}, observation)
					case processruntime.LaunchNotReleased:
						if actionKind != supervision.PublishNotReleasedEffect {
							break
						}
						activeLaunch, found := activeLaunches[generation]
						if !found {
							return simulationReplayFailure(
								trace, simulationReplayCausalityFailure,
								"not-released observation has no causal launch at record %d", index,
							)
						}
						failure := supervision.LaunchFailed
						if observed.ResourceExhausted() {
							failure = supervision.LaunchResourceExhausted
						}
						delivered = campaignmodule.AttemptLaunched(activeLaunch, supervision.NotReleased{Kind: failure}, observation)
					case processruntime.LaunchUnconfirmedKind:
						if actionKind != supervision.PublishLaunchUnconfirmedEffect {
							break
						}
						activeLaunch, found := activeLaunches[generation]
						if !found {
							return simulationReplayFailure(
								trace, simulationReplayCausalityFailure,
								"unconfirmed observation has no causal launch at record %d", index,
							)
						}
						delivered = campaignmodule.AttemptLaunched(activeLaunch, supervision.LaunchUnconfirmed{Residual: supervision.ProspectiveUnresolved}, observation)
					case processruntime.DrainUnconfirmedKind:
						terminalReceipts[generation] = observation
					default:
						terminalReceipts[generation] = observation
					}
				case processruntime.SettleEmergencyOperation:
					var matches bool
					runtime, matches = simulationReplayRuntimeCut(runtime, record)
					if !matches {
						return simulationReplayDivergenceFailure(trace, simulationEmergencyDivergence,
							"emergency settlement diverged at record %d", index)
					}
					processed := record.runtimeCut.Result().Settlement()
					delivered = campaignmodule.RuntimeEmergencySettled(processed)
				case processruntime.CloseOperation:
					var matches bool
					runtime, matches = simulationReplayRuntimeCut(runtime, record)
					if !matches {
						return simulationReplayDivergenceFailure(trace, simulationRuntimeClosureDivergence,
							"runtime closure diverged at record %d", index)
					}
					processed := record.runtimeCut.Result().Closure()
					delivered = campaignmodule.RuntimeEmergencyStarted(processed)
				default:
					return simulationReplayFailure(
						trace, simulationReplayOperationFailure,
						"runtime operation is invalid at record %d", index,
					)
				}
			}
			if !delivered.IsZero() {
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
			payload := record.campaignEvent.Fact()
			if record.source.kind != 0 {
				candidates := pendingDeliveries[record.source]
				delivered = campaignmodule.Fact{}
				for candidateAt, candidate := range candidates {
					candidate = simulationCausalCampaignPayload(payload, candidate)
					if !payload.Equal(candidate.Canonical()) {
						continue
					}
					delivered = candidate
					candidates = slices.Delete(candidates, candidateAt, candidateAt+1)
					pendingDeliveries[record.source] = candidates
					break
				}
			}
			if payload.IsZero() {
				payload = delivered
			}
			if !delivered.IsZero() {
				delivered = simulationCausalCampaignPayload(payload, delivered)
			}
			if !delivered.IsZero() && !payload.Equal(delivered.Canonical()) {
				return simulationReplayFailure(
					trace, simulationReplayCausalityFailure,
					"causal campaign fact diverged at record %d: got=%#v want=%#v",
					index, delivered, payload,
				)
			}
			if !delivered.IsZero() {
				payload = delivered
			}
			if delivered.IsZero() {
				_, remaining, ok := simulationTakeEffect(effects, func(effect campaignmodule.Effect) bool {
					return simulationEffectEnablesExternalFact(effect, payload)
				})
				if !ok {
					return simulationReplayFailure(
						trace, simulationReplayEnablednessFailure,
						"external campaign fact is not enabled at record %d (%s): source=%#v effects=%v deliveries=%v",
						index, payload.Name(), record.source,
						simulationEffectSummary(effects), pendingDeliveries,
					)
				}
				effects = remaining
			}
			var transition campaignmodule.Transition
			campaign, transition = simulationAdvanceCampaign(campaign, payload)
			emitted := transition.Effects()
			delivered = campaignmodule.Fact{}
			if !campaign.Projection().Canonical().Equal(record.campaignState) {
				return simulationReplayDivergenceFailure(
					trace, simulationCampaignStateDivergence,
					"campaign state diverged at record %d (%s)", index, payload.Name(),
				)
			}
			projected := make([]campaignmodule.Effect, len(emitted))
			for index, effect := range emitted {
				projected[index] = effect.Canonical(campaign.Projection())
			}
			if !slices.EqualFunc(projected, record.campaignEffects, func(left, right campaignmodule.Effect) bool {
				return left.Equal(right)
			}) {
				return simulationReplayDivergenceFailure(
					trace, simulationCampaignEffectsDivergence,
					"campaign effects diverged at record %d (%s): got=%v want=%v",
					index, payload.Name(), simulationEffectSummary(emitted),
					simulationEffectSummary(record.campaignEffects),
				)
			}
			effects = append(effects, emitted...)
		case supervisionAuthority:
			fact := record.supervisorEvent
			if registration, registered := fact.Registration(); registered {
				launchEffect, remaining, ok := simulationTakeEffect(effects, func(effect campaignmodule.Effect) bool {
					request, supervisionEffect := effect.SupervisionRequest()
					_, launches := request.Prospective(time.Time{}, time.Time{})
					return supervisionEffect && launches && supervisionRegistrationMatches(effect, registration)
				})
				if !ok {
					return simulationReplayFailure(
						trace, simulationReplayEnablednessFailure,
						"supervisor launch is not enabled at record %d", index,
					)
				}
				activeLaunches[registration.Generation()] = launchEffect
				effects = remaining
			}
			if generation, stopped := fact.StopGeneration(); stopped {
				_, remaining, ok := simulationTakeEffect(effects, func(effect campaignmodule.Effect) bool {
					request, supervisionEffect := effect.SupervisionRequest()
					stoppedGeneration, stops := request.StopGeneration()
					return supervisionEffect && stops && stoppedGeneration == generation
				})
				if !ok {
					return simulationReplayFailure(
						trace, simulationReplayEnablednessFailure,
						"supervisor stop is not enabled at record %d", index,
					)
				}
				effects = remaining
			}
			var transition supervision.Transition
			supervisorMachine, transition = supervisorMachine.Apply(fact)
			supervisionEffects := transition.Effects()
			for _, effect := range supervisionEffects {
				actionKinds[effect.Token()] = effect.Kind()
			}
			if !transition.Event().Equal(record.supervisorDomainEvent) ||
				!supervisorMachine.Projection().Equal(record.supervisorState) ||
				!equalSupervisionEffects(transition.Effects(), record.supervisorActions) {
				return simulationReplayDivergenceFailure(
					trace, supervisionDivergence, "supervisor transition diverged at record %d", index,
				)
			}
			for _, effect := range supervisionEffects {
				if effect.Kind() != supervision.DeliverTerminalEffect {
					continue
				}
				activeLaunch, found := activeLaunches[effect.Generation()]
				if !found {
					return simulationReplayFailure(
						trace, simulationReplayCausalityFailure,
						"runtime completion has no causal launch at record %d", index,
					)
				}
				terminal, ok := effect.Terminal("")
				if !ok {
					return simulationReplayFailure(trace, simulationReplayCausalityFailure,
						"terminal effect is invalid at record %d", index)
				}
				receipt, found := terminalReceipts[activeLaunch.Generation()]
				if !found {
					return simulationReplayFailure(
						trace, simulationReplayCausalityFailure,
						"terminal completion has no runtime receipt at record %d", index,
					)
				}
				delete(terminalReceipts, activeLaunch.Generation())
				terminalEvent := campaignmodule.AttemptTerminal(activeLaunch, terminal, receipt, 0)
				pendingDeliveries[simulationCausalSource{
					kind: supervisionActionSource, identity: uint64(effect.Token()),
				}] = append(pendingDeliveries[simulationCausalSource{
					kind: supervisionActionSource, identity: uint64(effect.Token()),
				}], terminalEvent)
				delivered = terminalEvent
			}
			if record.supervisorEvent.Kind() == supervision.EmergencySettlementCompletedFact {
				deliverAt := slices.IndexFunc(supervisionEffects, func(effect supervision.Effect) bool {
					return effect.Kind() == supervision.DeliverEmergencySettlementEffect
				})
				candidates := pendingDeliveries[record.source]
				settlementAt := slices.IndexFunc(candidates, func(candidate campaignmodule.Fact) bool {
					return candidate.CompletesEmergencySettlement()
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
					kind: supervisionActionSource, identity: uint64(supervisionEffects[deliverAt].Token()),
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
			if !campaign.Projection().Canonical().Equal(barrier.campaign) ||
				!reflect.DeepEqual(simulationTraceRuntimeState(runtime), barrier.runtime) ||
				!supervisorMachine.Projection().Equal(barrier.supervisor) {
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
			campaign: campaign.Projection().Canonical().Fork(),
			runtime:  simulationTraceRuntimeState(runtime), runtimeState: runtime,
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
		return slices.ContainsFunc(parent.campaignEffects, func(effect campaignmodule.Effect) bool {
			return uint64(effect.ID()) == child.source.identity
		})
	case supervisionActionSource:
		return slices.ContainsFunc(parent.supervisorActions, func(action supervision.Effect) bool {
			return uint64(action.Token()) == child.source.identity
		})
	default:
		return false
	}
}

func simulationApplyRecordedOwnerCut(world simulationWorld, record simulationRecord) (simulationWorld, error) {
	switch record.authority {
	case simulationCampaignAuthority:
		state, transition := world.campaign.Apply(record.campaignEvent.Fact())
		effects := transition.Effects()
		projected := make([]campaignmodule.Effect, len(effects))
		for index, effect := range effects {
			projected[index] = effect.Canonical(state.Projection())
		}
		if !state.Projection().Canonical().Equal(record.campaignState) ||
			!slices.EqualFunc(projected, record.campaignEffects, func(left, right campaignmodule.Effect) bool {
				return left.Equal(right)
			}) {
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
		if !transition.Event().Equal(record.supervisorDomainEvent) ||
			!machine.Projection().Equal(record.supervisorState) ||
			!equalSupervisionEffects(transition.Effects(), record.supervisorActions) {
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

func simulationCausalCampaignPayload(recorded, derived campaignmodule.Fact) campaignmodule.Fact {
	return derived.WithRecordedEvidence(recorded)
}

func replayViolation(prefix simulationTrace, malformed simulationMalformedFact) (result ViolationResult) {
	legal := replayLegal(prefix)
	if legal.failure != nil {
		return ViolationResult{failure: fmt.Errorf("legal prefix: %w", legal.failure)}
	}
	switch malformed.authority {
	case simulationCampaignAuthority:
		if malformed.campaign.IsZero() {
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
			violation = campaignmodule.NewViolation(runtimeViolation.Operation(), runtimeViolation.Reason())
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
		simulationAdvanceCampaignGuarded(&runtime, campaign, malformed.campaign)
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
			violation = campaignmodule.NewViolation(runtimeViolation.Operation(), runtimeViolation.Reason())
			ok = true
		}
		if !ok {
			violation = campaignmodule.NewViolation(operation, "unexpected panic")
		}
		simulationSettleInvariantCleanup(runtime, violation)
		panic(violation)
	}()

	*runtime = advance(*runtime)
}

func simulationAdvanceCampaignGuarded(
	runtime *processruntime.Replay,
	machine campaignmodule.Machine,
	fact campaignmodule.Fact,
) {
	defer func() {
		if recovered := recover(); recovered != nil {
			violation, ok := recovered.(campaignmodule.Violation)
			if !ok {
				panic(recovered)
			}
			simulationSettleInvariantCleanup(runtime, violation)
			panic(violation)
		}
	}()
	_, _ = machine.Apply(fact)
}

func simulationAdvanceSupervisorGuarded(
	runtime *processruntime.Replay,
	machine *supervision.Machine,
	fact supervision.Fact,
) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		violation, ok := recovered.(runtimeInvariantViolation)
		if supervisorViolation, failed := recovered.(supervision.Violation); failed {
			violation = campaignmodule.NewViolation(supervisorViolation.Operation(), supervisorViolation.Reason())
			ok = true
		}
		if !ok {
			violation = campaignmodule.NewViolation("reduce supervisor", "unexpected panic")
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
	next, applied := runtime.Apply(processruntime.CloseCut(violation.Reason()))
	*runtime = next
	closure := runtimeClosureValue(applied.Closure())
	next, _ = runtime.Apply(processruntime.SettleEmergencyCut(simulationEmergencySweep(*runtime, closure)))
	*runtime = next
}

func simulationEmergencySweep(runtime processruntime.Replay, closure runtimeClosure) emergencySweep {
	residuals := closure.Residual()
	resolutions := make([]processruntime.Resolution, len(residuals))
	for index, residual := range residuals {
		resolutions[index] = processruntime.TransferCustody(residual.Generation())
		if !residual.Transferred() && !residual.Prospective() && !runtime.Accepts(processruntime.ObserveAttemptCut(
			residual.Generation(), processruntime.DrainUnconfirmed(),
		)) {
			resolutions[index] = processruntime.ConfirmedDrained(residual.Generation())
		}
	}
	return resolutions
}

func shrink(trace simulationTrace, key FailureKey) simulationTrace {
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
		return simulationShrinkLivenessWith(trace, key, explore)
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
		first := replayLegal(shrunk)
		second := replayLegal(shrunk)
		if first.failure == nil || !reflect.DeepEqual(first.key, key) || !reflect.DeepEqual(first, second) {
			panic("simulation shrink did not retain a deterministic failure")
		}
	} else {
		first := replayViolation(shrunk, *shrunk.malformed)
		second := replayViolation(shrunk, *shrunk.malformed)
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
		definition.campaign.Peers = parallelism
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
	canonical.campaign.Identity = "campaign-1"
	canonical.campaign.Lineage = 1
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
		len(trace.definition.catalogue) + trace.definition.capacity + trace.definition.campaign.Peers,
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
	if definition.campaign.Identity != "campaign-1" || definition.campaign.Lineage != 1 {
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
		rank += record.campaignEvent.Fact().Complexity()
	case simulationRuntimeAuthority:
		rank += record.runtimeCut.Complexity()
	case supervisionAuthority:
		rank += record.supervisorEvent.Complexity()
	}

	return rank
}

func simulationTraceBoundaryDistance(trace simulationTrace) int {
	distance := 0
	var origins []supervision.Effect
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
	explored := explore(definition, choices)
	if explored.failure != nil {
		return simulationTrace{}, false
	}
	candidate := explored.trace
	replayFailure := replayLegal(trace)
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
	malformed.supervisor = malformed.supervisor.RewriteCorrelated(
		before.supervisorEvent, after.supervisorEvent,
	)

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
		return left.campaignEvent.Fact().SameKind(right.campaignEvent.Fact())
	case simulationRuntimeAuthority:
		return left.runtimeCut.Operation() == right.runtimeCut.Operation()
	case supervisionAuthority:
		leftKind, rightKind := left.supervisorEvent.Kind(), right.supervisorEvent.Kind()
		if (leftKind == supervision.LaunchCompletedFact || leftKind == supervision.LaunchBoundaryFact) &&
			(rightKind == supervision.LaunchCompletedFact || rightKind == supervision.LaunchBoundaryFact) {
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
	trace.definition.campaign.Command = slices.Clone(trace.definition.campaign.Command)
	trace.definition.campaign.Env = slices.Clone(trace.definition.campaign.Env)
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
		result := replayLegal(trace)

		return result.failure != nil && reflect.DeepEqual(result.key, key)
	}
	result := replayViolation(trace, *trace.malformed)

	return result.failure == nil && reflect.DeepEqual(result.key, key)
}

func simulationFailureKey(authority simulationAuthority, violation runtimeInvariantViolation) FailureKey {
	relevant := violation.StableIdentities()
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
		authority: authority, operation: violation.Operation(), reason: violation.Reason(),
		identities: identities,
	}
}

func supervisionRegistrationMatches(effect campaignmodule.Effect, registration supervision.Registration) bool {
	return registration.Generation() == effect.Generation() && registration.Attempt() == effect.Attempt() &&
		registration.Profile() == effect.Spec().Profile && registration.CommandDeadline() == effect.Spec().Deadline
}

func simulationEffectEnablesExternalFact(effect campaignmodule.Effect, payload campaignmodule.Fact) bool {
	return effect.Enables(payload)
}

func simulationTakeEffect(
	effects []campaignmodule.Effect,
	match func(campaignmodule.Effect) bool,
) (campaignmodule.Effect, []campaignmodule.Effect, bool) {
	for index, effect := range effects {
		if !match(effect) {
			continue
		}
		remaining := slices.Clone(effects)
		remaining = slices.Delete(remaining, index, index+1)

		return effect, remaining, true
	}

	return campaignmodule.Effect{}, effects, false
}

func simulationTakeRuntimeRequest(
	effects []campaignmodule.Effect,
	binding campaignmodule.RuntimeBinding,
	definition campaignmodule.Definition,
	cut processruntime.RecordedCut,
) (campaignmodule.RuntimeRequest, []campaignmodule.Effect, bool) {
	for index, effect := range effects {
		if effect.Owner() != campaignmodule.RuntimeOwner {
			continue
		}
		request, ok := binding.RuntimeRequest(effect, definition)
		if !ok || !request.Matches(cut) {
			continue
		}
		return request, slices.Delete(slices.Clone(effects), index, index+1), true
	}
	return campaignmodule.RuntimeRequest{}, effects, false
}

func simulationEffectSummary(effects []campaignmodule.Effect) []string {
	summary := make([]string, len(effects))
	for index, effect := range effects {
		summary[index] = fmt.Sprintf("owner=%d effect=%d", effect.Owner(), effect.ID())
	}

	return summary
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
