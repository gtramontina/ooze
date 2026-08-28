package ooze

import (
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
	"slices"
	"sort"
	"sync"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
)

type simulationRecorder struct {
	gate         sync.RWMutex
	mutex        sync.Mutex
	next         supervision.OwnerCutSequence
	records      []simulationRecord
	barriers     []simulationQuiescentBarrier
	actionMutex  sync.Mutex
	actions      map[supervision.ActionToken]simulationInFlightAction
	actionWake   chan struct{}
	causalMutex  sync.Mutex
	activeEffect campaignEffect
	runtimeCuts  []simulationRecordedRuntimeCut
	runtimeError error
	runtimeState processruntime.Replay
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

func (recorder *simulationRecorder) executeEffect(effect campaignEffect) func() {
	if recorder == nil {
		return func() {}
	}
	recorder.causalMutex.Lock()
	if recorder.activeEffect.id != 0 {
		recorder.causalMutex.Unlock()
		panic("simulation recorder campaign effects overlap")
	}
	recorder.activeEffect = effect
	recorder.causalMutex.Unlock()

	return func() {
		recorder.causalMutex.Lock()
		if recorder.activeEffect.id != effect.id {
			recorder.causalMutex.Unlock()
			panic("simulation recorder campaign effect completion is stale")
		}
		recorder.activeEffect = campaignEffect{}
		recorder.causalMutex.Unlock()
	}
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

func (recorder *simulationRecorder) recordCampaign(
	reservation simulationReservation,
	event campaignEvent,
	previous campaignState,
	state campaignState,
	effects []campaignEffect,
) {
	if recorder == nil {
		return
	}
	var source simulationCausalSource
	switch payload := event.payload.(type) {
	case attemptTerminalEvent:
		source = recorder.recordSupervisorDelivery(supervision.DeliverTerminalEffect, payload.generation)
	case runtimeEmergencySettledEvent:
		source = recorder.recordSupervisorDelivery(supervision.DeliverEmergencySettlementEffect, 0)
	default:
		source = recorder.campaignSource(payload)
	}
	projectedState := simulationProjectCampaign(state)
	recorder.append(simulationRecord{
		sequence: reservation.sequence, authority: reservation.authority, source: source,
		campaignEvent:   simulationTraceCampaignEvent(simulationProjectCampaignEvent(event, previous)),
		campaignState:   simulationTraceCampaignState(projectedState),
		campaignEffects: simulationProjectCampaignEffects(effects, state),
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
	recorder.causalMutex.Lock()
	effect := recorder.activeEffect
	recorder.causalMutex.Unlock()
	if effect.id != 0 {
		return simulationCausalSource{kind: simulationCampaignEffectSource, identity: uint64(effect.id)}
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

func (recorder *simulationRecorder) campaignSource(payload campaignEventPayload) simulationCausalSource {
	if source := recorder.takeRuntimeCut(func(record simulationRecord) bool {
		return simulationRuntimeCutEnablesCampaign(record, payload)
	}); source.kind != 0 {
		return source
	}
	recorder.causalMutex.Lock()
	effect := recorder.activeEffect
	recorder.causalMutex.Unlock()
	if effect.id != 0 && simulationEffectEnablesExternalFact(effect, payload) {
		return simulationCausalSource{kind: simulationCampaignEffectSource, identity: uint64(effect.id)}
	}

	return simulationCausalSource{}
}

func simulationRuntimeCutEnablesCampaign(record simulationRecord, payload campaignEventPayload) bool {
	result := record.runtimeCut.Result()
	switch event := payload.(type) {
	case campaignRegisteredEvent:
		return record.runtimeCut.Operation() == processruntime.RegisterCampaignOperation &&
			campaignRegistrationEvidence(result.Registration()) == event.registration
	case admissionGrantedEvent:
		return slices.ContainsFunc(simulationRuntimeDeliveries(record), func(delivery simulationAdmission) bool {
			return campaignAdmissionValue(delivery.production()) == event.grant
		})
	case admissionCancelledEvent:
		cancelled := runtimeAdmissionResult(result.Admission())
		return record.runtimeCut.Operation() == processruntime.CancelAdmissionOperation &&
			campaignAdmissionValue(cancelled.request) == event.request &&
			cancelled.decision == event.result.decision &&
			campaignAdmissionValue(cancelled.request) == event.result.request &&
			cancelled.fatalEpoch == event.result.fatalEpoch
	case admissionRejectedEvent:
		rejected := runtimeAdmissionResult(result.Admission())
		return record.runtimeCut.Operation() == processruntime.RequestAdmissionOperation &&
			rejected.decision == event.result.decision && campaignAdmissionValue(rejected.request) == event.result.request
	case startCommittedEvent:
		return record.runtimeCut.Operation() == processruntime.CommitStartOperation &&
			runtimeStartResult(result.Start()) == startCommittedResult(event.result)
	case attemptLaunchEvent:
		return record.runtimeCut.Matches(processruntime.ObserveAttemptCut(event.generation, processruntime.Owned()))
	case confirmationBarrierBoundEvent:
		bound := runtimeBarrierResult(result.Barrier())
		return record.runtimeCut.Operation() == processruntime.BindConfirmationBarrierOperation &&
			bound.decision == event.result.decision && campaignAdmissionValue(bound.request) == event.result.request &&
			slices.EqualFunc(bound.deliveries, event.result.deliveries,
				func(left admissionAuthority, right campaignAdmission) bool {
					return campaignAdmissionValue(left) == right
				})
	case grantReturnAcknowledgedEvent:
		return record.runtimeCut.Operation() == processruntime.ReturnGrantOperation &&
			result.Admission().Decision() == event.result.decision
	case terminalCommittedEvent:
		return record.runtimeCut.Operation() == processruntime.CommitTerminalOperation &&
			terminalResult{decision: result.Terminal().Decision()} == terminalResult(event.result)
	default:
		return false
	}
}

func simulationRuntimeDeliveries(record simulationRecord) []simulationAdmission {
	result := record.runtimeCut.Result()
	var deliveries []processruntime.Admission
	switch record.runtimeCut.Operation() {
	case processruntime.RequestAdmissionOperation, processruntime.CancelAdmissionOperation, processruntime.ReturnGrantOperation:
		deliveries = result.Admission().Deliveries()
	case processruntime.BindConfirmationBarrierOperation:
		deliveries = result.Barrier().Deliveries()
	case processruntime.CompleteConfirmationQueueOperation:
		deliveries = result.Queue().Deliveries()
	case processruntime.ObserveAttemptOperation:
		deliveries = result.Receipt().Deliveries()
	}
	return simulationTraceAdmissions(runtimeAdmissions(deliveries))
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
	recorder.causalMutex.Lock()
	effect := recorder.activeEffect
	recorder.causalMutex.Unlock()
	if fact.Kind() == supervision.ProspectiveRegisteredFact && effect.kind == campaignEffectLaunchAttempt {
		return simulationCausalSource{kind: simulationCampaignEffectSource, identity: uint64(effect.id)}
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
	runner *managedCampaignRunner,
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
	campaignState := simulationProjectCampaign(runner.state)
	definition := campaignState.definition
	definition.baselineDeadline = 0
	definition.command = slices.Clone(definition.command)
	definition.env = slices.Clone(definition.env)

	afterSequence := uint64(0)
	if len(records) != 0 {
		afterSequence = records[len(records)-1].sequence
	}
	barrier := simulationQuiescentBarrier{
		afterSequence: afterSequence,
		campaign:      simulationTraceCampaignState(campaignState),
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
				catalogue: slices.Clone(campaignState.catalogue),
			},
			records: records, barriers: barriers,
		}, simulationWorld{
			campaign: campaignState, runtime: runtimeState,
			supervisor: supervisorState, machine: supervisionMachine,
		}
}

func simulationProjectCampaign(state campaignState) campaignState {
	state = state.clone()
	logicalSnapshot := simulationLogicalSnapshot(state.definition.identity)
	if state.snapshot != "" {
		state.snapshot = logicalSnapshot
	}
	for index := range state.attempts {
		if state.attempts[index].workspace != "" {
			state.attempts[index].workspace = simulationLogicalWorkspace(state.attempts[index].identity)
		}
	}
	for index := range state.obligations {
		switch state.obligations[index].kind {
		case campaignResourceSnapshot:
			if state.snapshot != "" {
				state.obligations[index].identity = string(logicalSnapshot)
			}
		case campaignResourceWorkspace:
			attemptAt := state.attemptIndex(state.obligations[index].attempt)
			if attemptAt >= 0 && state.attempts[attemptAt].workspace != "" {
				state.obligations[index].identity = simulationLogicalWorkspace(state.obligations[index].attempt)
			}
		}
	}
	for index := range state.artifactResidue {
		state.artifactResidue[index] = "artifact-residue"
	}

	return state
}

func simulationProjectCampaignEvent(event campaignEvent, state campaignState) campaignEvent {
	logicalSnapshot := simulationLogicalSnapshot(state.definition.identity)
	switch payload := event.payload.(type) {
	case snapshotEstablishedEvent:
		payload.snapshot = logicalSnapshot
		event.payload = payload
	case catalogueDiscoveredEvent:
		payload.snapshot = logicalSnapshot
		payload.mutants = slices.Clone(payload.mutants)
		event.payload = payload
	case workspaceMaterializedEvent:
		payload.snapshot = logicalSnapshot
		payload.workspace = simulationLogicalWorkspace(payload.attempt)
		event.payload = payload
	case workspaceMaterializationFailedEvent:
		payload.artifactResidue = slices.Clone(payload.artifactResidue)
		for index := range payload.artifactResidue {
			payload.artifactResidue[index] = "artifact-residue"
		}
		event.payload = payload
	case resourceSettledEvent:
		payload.identity = simulationLogicalResource(payload.kind, payload.identity, "", state)
		event.payload = payload
	case resourceSettlementFailedEvent:
		payload.identity = simulationLogicalResource(payload.kind, payload.identity, "", state)
		event.payload = payload
	}

	return event
}

func simulationProjectCampaignEffects(effects []campaignEffect, state campaignState) []campaignEffect {
	projected := slices.Clone(effects)
	for index := range projected {
		effect := &projected[index]
		if effect.snapshot != "" {
			effect.snapshot = simulationLogicalSnapshot(state.definition.identity)
		}
		if effect.workspace != "" {
			effect.workspace = simulationLogicalWorkspace(effect.attempt)
		}
		if effect.spec.Dir != "" {
			effect.spec.Dir = simulationLogicalWorkspace(effect.attempt)
		}
		effect.spec.Command = slices.Clone(effect.spec.Command)
		effect.spec.Env = slices.Clone(effect.spec.Env)
	}

	return projected
}

func simulationLogicalResource(
	kind campaignResourceKind,
	identity string,
	attempt attemptIdentity,
	state campaignState,
) string {
	switch kind {
	case campaignResourceSnapshot:
		return string(simulationLogicalSnapshot(state.definition.identity))
	case campaignResourceWorkspace:
		if attempt != "" {
			return simulationLogicalWorkspace(attempt)
		}
		for _, candidate := range state.attempts {
			if candidate.workspace == identity {
				return simulationLogicalWorkspace(candidate.identity)
			}
		}
		return "workspace:settled"
	default:
		return identity
	}
}

func simulationLogicalSnapshot(campaign campaignIdentity) snapshotIdentity {
	return snapshotIdentity("snapshot:" + string(campaign))
}

func simulationLogicalWorkspace(attempt attemptIdentity) string {
	return "workspace:" + string(attempt)
}
