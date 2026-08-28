package ooze

import (
	"slices"
	"sort"
	"sync"

	campaignmodule "github.com/gtramontina/ooze/internal/ooze/internal/campaign"
	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
)

type simulationRecorder struct {
	gate          sync.RWMutex
	mutex         sync.Mutex
	next          supervision.OwnerCutSequence
	records       []simulationRecord
	barriers      []simulationQuiescentBarrier
	actionMutex   sync.Mutex
	actions       map[supervision.ActionToken]simulationInFlightAction
	actionWake    chan struct{}
	causalMutex   sync.Mutex
	runtimeCuts   []simulationRecordedRuntimeCut
	runtimeError  error
	runtimeState  processruntime.Replay
	campaignState campaignmodule.Projection
}

func (recorder *simulationRecorder) recordRuntimeError(err error) {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	if recorder.runtimeError == nil {
		recorder.runtimeError = err
	}
}

func (recorder *simulationRecorder) beginRuntime(state processruntime.Replay) {
	recorder.mutex.Lock()
	recorder.runtimeState = state
	recorder.mutex.Unlock()
}

type simulationRecordedRuntimeCut struct {
	sequence uint64
	record   simulationRecord
}

type simulationInFlightAction struct {
	kind       supervision.EffectKind
	generation attemptGeneration
}

type simulationReservation struct {
	sequence  uint64
	authority simulationAuthority
}

func newSimulationRecorder() *simulationRecorder {
	return &simulationRecorder{
		actions:    make(map[supervision.ActionToken]simulationInFlightAction),
		actionWake: make(chan struct{}, 1),
	}
}

func (recorder *simulationRecorder) enter() func() {
	if recorder == nil {
		return func() {}
	}
	recorder.gate.RLock()

	return recorder.gate.RUnlock
}

func (recorder *simulationRecorder) Enter() func() {
	return recorder.enter()
}

func (recorder *simulationRecorder) Publish(
	reservation supervision.OwnerCutReservation,
	fact supervision.Fact,
	event supervision.Event,
	projection supervision.Projection,
	effects []supervision.Effect,
) {
	recorder.recordSupervisor(
		simulationReservation{sequence: uint64(reservation), authority: supervisionAuthority},
		fact, event, projection, effects,
	)
}

func (recorder *simulationRecorder) Complete(effect supervision.Effect) {
	recorder.recordSupervisorEffect(effect)
}

func (recorder *simulationRecorder) reserve(authority simulationAuthority) simulationReservation {
	if recorder == nil {
		return simulationReservation{}
	}

	return simulationReservation{sequence: recorder.next.Add(1), authority: authority}
}

func (recorder *simulationRecorder) recordRuntime(
	reservation simulationReservation,
	record simulationRecord,
	state processruntime.Replay,
) {
	if recorder == nil {
		return
	}
	record.sequence = reservation.sequence
	record.authority = reservation.authority
	record.source = recorder.runtimeSource(record)
	record.runtimeState = simulationTraceRuntimeState(state)
	recorder.mutex.Lock()
	recorder.runtimeState = state
	recorder.mutex.Unlock()
	recorder.append(record)
	recorder.causalMutex.Lock()
	recorder.runtimeCuts = append(recorder.runtimeCuts, simulationRecordedRuntimeCut{
		sequence: record.sequence, record: record,
	})
	recorder.causalMutex.Unlock()
}

type simulationCampaignRecorder struct{ recorder *simulationRecorder }

func (recorder simulationCampaignRecorder) enter() func() { return recorder.recorder.enter() }

func (recorder simulationCampaignRecorder) reserve() uint64 {
	return recorder.recorder.reserveCampaign()
}

func (recorder simulationCampaignRecorder) publish(
	reservation uint64,
	event campaignmodule.Event,
	projection campaignmodule.Projection,
	effects []campaignmodule.Effect,
) {
	recorder.recorder.recordCampaign(reservation, event, projection, effects)
}

func (recorder *simulationRecorder) reserveCampaign() uint64 {
	return recorder.reserve(simulationCampaignAuthority).sequence
}

func (recorder *simulationRecorder) recordCampaign(
	reservation uint64,
	event campaignmodule.Event,
	projection campaignmodule.Projection,
	effects []campaignmodule.Effect,
) {
	fact := event.Fact()
	var source simulationCausalSource
	if kind, generation, delivered := fact.SupervisorDelivery(); delivered {
		source = recorder.recordSupervisorDelivery(kind, generation)
	} else {
		source = recorder.campaignSource(fact)
	}
	canonical := projection.Canonical()
	projectedEffects := make([]campaignmodule.Effect, len(effects))
	for index, effect := range effects {
		projectedEffects[index] = effect.Canonical(projection)
	}
	recorder.campaignState = canonical
	recorder.append(simulationRecord{
		sequence: uint64(reservation), authority: simulationCampaignAuthority, source: source,
		campaignEvent: event.Canonical(), campaignState: canonical, campaignEffects: projectedEffects,
	})
}

func (recorder *simulationRecorder) recordSupervisor(
	reservation simulationReservation,
	fact supervision.Fact,
	domainEvent supervision.Event,
	state supervision.Projection,
	effects []supervision.Effect,
) {
	if recorder == nil {
		return
	}
	recorder.recordSupervisorEffects(effects)
	source := recorder.supervisorSource(fact)
	recorder.append(simulationRecord{
		sequence: reservation.sequence, authority: reservation.authority, source: source,
		supervisorEvent:       fact,
		supervisorDomainEvent: domainEvent,
		supervisorState:       state,
		supervisorActions:     effects,
	})
}

func (recorder *simulationRecorder) runtimeSource(record simulationRecord) simulationCausalSource {
	if source := recorder.runtimeActionSource(record); source.kind != 0 {
		return source
	}
	if record.runtimeCut.Operation() == processruntime.RegisterCampaignOperation {
		return simulationCausalSource{kind: simulationCampaignEffectSource, identity: 1}
	}

	return simulationCausalSource{}
}

func (recorder *simulationRecorder) runtimeActionSource(record simulationRecord) simulationCausalSource {
	recorder.actionMutex.Lock()
	defer recorder.actionMutex.Unlock()
	var matched supervision.ActionToken
	for token, action := range recorder.actions {
		matches := false
		switch record.runtimeCut.Operation() {
		case processruntime.ObserveAttemptOperation:
			matches = action.generation == record.runtimeCut.Result().Receipt().Generation() &&
				(action.kind == supervision.PublishOwnedEffect || action.kind == supervision.CloseProspectiveEffect ||
					action.kind == supervision.SettleRuntimeEffect ||
					action.kind == supervision.TransferResidualCustodyEffect)
		case processruntime.CompleteConfirmationQueueOperation:
			matches = action.kind == supervision.DeliverTerminalEffect
		case processruntime.SettleEmergencyOperation:
			matches = action.kind == supervision.SettleEmergencyEffect
		}
		if !matches {
			continue
		}
		if matched != 0 {
			panic("simulation recorder runtime action source is ambiguous")
		}
		matched = token
	}
	if matched == 0 {
		return simulationCausalSource{}
	}

	return simulationCausalSource{kind: supervisionActionSource, identity: uint64(matched)}
}

func (recorder *simulationRecorder) campaignSource(payload campaignmodule.Fact) simulationCausalSource {
	if source := recorder.takeRuntimeCut(func(record simulationRecord) bool {
		return simulationRuntimeCutEnablesCampaign(record, payload)
	}); source.kind != 0 {
		return source
	}
	return simulationCausalSource{}
}

func simulationRuntimeCutEnablesCampaign(record simulationRecord, payload campaignmodule.Fact) bool {
	return payload.MatchesRuntimeCut(record.runtimeCut)
}

func (recorder *simulationRecorder) supervisorSource(fact supervision.Fact) simulationCausalSource {
	if generation, token, correlated := fact.RuntimeCorrelation(); correlated {
		return recorder.takeRuntimeCut(func(record simulationRecord) bool {
			return record.runtimeCut.Operation() == processruntime.ObserveAttemptOperation &&
				record.runtimeCut.Result().Receipt().Generation() == generation &&
				record.source.kind == supervisionActionSource &&
				record.source.identity == uint64(token)
		})
	}
	if token, found := fact.CausalEffect(); found {
		return simulationCausalSource{kind: supervisionActionSource, identity: uint64(token)}
	}
	return simulationCausalSource{}
}

func (recorder *simulationRecorder) takeRuntimeCut(
	match func(simulationRecord) bool,
) simulationCausalSource {
	recorder.causalMutex.Lock()
	defer recorder.causalMutex.Unlock()
	for index, cut := range recorder.runtimeCuts {
		if !match(cut.record) {
			continue
		}
		recorder.runtimeCuts = slices.Delete(recorder.runtimeCuts, index, index+1)

		return simulationCausalSource{kind: simulationOwnerDeliverySource, identity: cut.sequence}
	}

	return simulationCausalSource{}
}

func (recorder *simulationRecorder) append(record simulationRecord) {
	recorder.mutex.Lock()
	recorder.records = append(recorder.records, record)
	recorder.mutex.Unlock()
}

func (recorder *simulationRecorder) recordSupervisorEffects(effects []supervision.Effect) {
	if recorder == nil {
		return
	}
	recorder.actionMutex.Lock()
	defer recorder.actionMutex.Unlock()
	for _, effect := range effects {
		_, found := recorder.actions[effect.Token()]
		if effect.Token() == 0 || found {
			panic("simulation recorder action is zero or duplicated")
		}
		recorder.actions[effect.Token()] = simulationInFlightAction{
			kind: effect.Kind(), generation: effect.Generation(),
		}
	}
}

func (recorder *simulationRecorder) recordSupervisorCompletion(kind supervision.EffectKind, token supervision.ActionToken) {
	if recorder == nil {
		return
	}
	recorder.actionMutex.Lock()
	pending, found := recorder.actions[token]
	if !found || pending.kind != kind {
		recorder.actionMutex.Unlock()
		panic("simulation recorder action completion is stale or wrong")
	}
	delete(recorder.actions, token)
	recorder.actionMutex.Unlock()
	select {
	case recorder.actionWake <- struct{}{}:
	default:
	}
}

func (recorder *simulationRecorder) recordSupervisorEffect(effect supervision.Effect) {
	recorder.recordSupervisorCompletion(effect.Kind(), effect.Token())
}

func (recorder *simulationRecorder) recordSupervisorDelivery(
	kind supervision.EffectKind,
	generation attemptGeneration,
) simulationCausalSource {
	if recorder == nil {
		return simulationCausalSource{}
	}
	recorder.actionMutex.Lock()
	var matched supervision.ActionToken
	for token, action := range recorder.actions {
		if action.kind != kind || action.generation != generation {
			continue
		}
		if matched != 0 {
			recorder.actionMutex.Unlock()
			panic("simulation recorder delivery action is ambiguous")
		}
		matched = token
	}
	if matched == 0 {
		recorder.actionMutex.Unlock()
		panic("simulation recorder delivery action is absent")
	}
	delete(recorder.actions, matched)
	recorder.actionMutex.Unlock()
	select {
	case recorder.actionWake <- struct{}{}:
	default:
	}

	return simulationCausalSource{kind: supervisionActionSource, identity: uint64(matched)}
}

func (recorder *simulationRecorder) quiescent(
	runtime *processruntime.Runtime,
	machine *supervision.Machine,
) (simulationTrace, simulationWorld) {
	for {
		recorder.gate.Lock()
		recorder.actionMutex.Lock()
		pending := len(recorder.actions)
		recorder.actionMutex.Unlock()
		if pending == 0 {
			break
		}
		recorder.gate.Unlock()
		<-recorder.actionWake
	}
	defer recorder.gate.Unlock()

	recorder.mutex.Lock()
	if recorder.runtimeError != nil {
		err := recorder.runtimeError
		recorder.mutex.Unlock()
		panic(err)
	}
	records := slices.Clone(recorder.records)
	recorder.mutex.Unlock()
	sort.Slice(records, func(left, right int) bool {
		return records[left].sequence < records[right].sequence
	})

	runtimeState := simulationTraceRuntimeState(recorder.runtimeState)
	supervisionMachine := machine.Fork()
	supervisorState := supervisionMachine.Projection()
	campaignState := recorder.campaignState
	definition := campaignState.Definition()

	afterSequence := uint64(0)
	if len(records) != 0 {
		afterSequence = records[len(records)-1].sequence
	}
	barrier := simulationQuiescentBarrier{
		afterSequence: afterSequence,
		campaign:      campaignState,
		runtime:       runtimeState,
		supervisor:    supervisorState,
	}
	recorder.mutex.Lock()
	recorder.barriers = append(recorder.barriers, barrier)
	barriers := slices.Clone(recorder.barriers)
	recorder.mutex.Unlock()

	return simulationTrace{
			definition: simulationDefinition{
				campaign: definition, capacity: runtimeState.Capacity(),
				catalogue: campaignState.Catalogue(),
			},
			records: records, barriers: barriers,
		}, simulationWorld{
			campaign: campaignState.Fork(), runtime: runtimeState,
			supervisor: supervisorState, machine: supervisionMachine,
		}
}
