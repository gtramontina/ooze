package ooze

import (
	"slices"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
)

type simulationRecorder struct {
	gate         sync.RWMutex
	mutex        sync.Mutex
	next         atomic.Uint64
	records      []simulationRecord
	barriers     []simulationQuiescentBarrier
	actionMutex  sync.Mutex
	actions      map[supervisorActionToken]simulationInFlightAction
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
	kind       supervisorActionKind
	generation attemptGeneration
}

type simulationReservation struct {
	sequence  uint64
	authority simulationAuthority
}

func newSimulationRecorder() *simulationRecorder {
	return &simulationRecorder{
		actions:    make(map[supervisorActionToken]simulationInFlightAction),
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

func (recorder *simulationRecorder) Reserve() supervisionOwnerCutReservation {
	return supervisionOwnerCutReservation(recorder.reserve(supervisionAuthority).sequence)
}

func (recorder *simulationRecorder) Publish(
	reservation supervisionOwnerCutReservation,
	fact supervisionFact,
	projection supervisionProjection,
	effects []supervisionEffect,
) {
	recorder.recordSupervisor(
		simulationReservation{sequence: uint64(reservation), authority: supervisionAuthority},
		fact.production(), projection, supervisorActionsFromEffects(effects),
	)
}

func (recorder *simulationRecorder) Complete(effect supervisionEffect) {
	recorder.recordSupervisorAction(effect.production())
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
		source = recorder.recordSupervisorDelivery(supervisorDeliverTerminal, payload.generation)
	case runtimeEmergencySettledEvent:
		source = recorder.recordSupervisorDelivery(supervisorDeliverEmergencySettlement, 0)
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
	event supervisorEvent,
	state supervisionProjection,
	actions []supervisorAction,
) {
	if recorder == nil {
		return
	}
	recorder.recordSupervisorActions(actions)
	source := recorder.supervisorSource(event)
	recorder.append(simulationRecord{
		sequence: reservation.sequence, authority: reservation.authority, source: source,
		supervisorEvent:   supervisionFactFromEvent(event),
		supervisorState:   state,
		supervisorActions: supervisionEffectsFromActions(actions),
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
	var matched supervisorActionToken
	for token, action := range recorder.actions {
		matches := false
		switch record.runtimeCut.Operation() {
		case processruntime.ObserveAttemptOperation:
			matches = action.generation == record.runtimeCut.Result().Receipt().Generation() &&
				(action.kind == supervisorPublishOwned || action.kind == supervisorCloseProspective ||
					action.kind == supervisorSettleRuntime ||
					action.kind == supervisorTransferResidualCustody)
		case processruntime.CompleteConfirmationQueueOperation:
			matches = action.kind == supervisorDeliverTerminal
		case processruntime.SettleEmergencyOperation:
			matches = action.kind == supervisorSettleEmergency
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

func (recorder *simulationRecorder) supervisorSource(event supervisorEvent) simulationCausalSource {
	if token := supervisionFactAction(event); token != 0 {
		return simulationCausalSource{kind: supervisionActionSource, identity: uint64(token)}
	}
	if event.kind == supervisorRuntimeCompleted {
		action := event.runtime.action
		return recorder.takeRuntimeCut(func(record simulationRecord) bool {
			return record.runtimeCut.Operation() == processruntime.ObserveAttemptOperation &&
				record.runtimeCut.Result().Receipt().Generation() == event.generation &&
				record.source.kind == supervisionActionSource &&
				record.source.identity == uint64(action.token)
		})
	}
	recorder.causalMutex.Lock()
	effect := recorder.activeEffect
	recorder.causalMutex.Unlock()
	if event.kind == supervisorProspectiveRegistered && effect.kind == campaignEffectLaunchAttempt {
		return simulationCausalSource{kind: simulationCampaignEffectSource, identity: uint64(effect.id)}
	}

	return simulationCausalSource{}
}

func supervisionFactAction(event supervisorEvent) supervisorActionToken {
	switch event.kind {
	case supervisorLaunchCompleted, supervisorLaunchBoundary:
		if event.completion != nil {
			return event.completion.action
		}
	case supervisorRunningObserved:
		if event.running != nil {
			if event.running.waitAction != 0 {
				return event.running.waitAction
			}
			return event.running.sampleAction
		}
	case supervisorDrainCompleted:
		if event.drain != nil {
			return event.drain.action.token
		}
	case supervisorOutputCompleted:
		if event.output != nil {
			return event.output.action.token
		}
	case supervisorStopAdmissionSealed:
		if event.seal != nil {
			return event.seal.action.token
		}
	case supervisorReleaseCompleted:
		if event.release != nil {
			return event.release.action.token
		}
	case supervisorEmergencySettlementCompleted:
		if event.emergencySettlement != nil {
			return event.emergencySettlement.action.token
		}
	}

	return 0
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

func (recorder *simulationRecorder) recordSupervisorActions(actions []supervisorAction) {
	if recorder == nil {
		return
	}
	recorder.actionMutex.Lock()
	defer recorder.actionMutex.Unlock()
	for _, action := range actions {
		_, found := recorder.actions[action.token]
		if action.token == 0 || found {
			panic("simulation recorder action is zero or duplicated")
		}
		recorder.actions[action.token] = simulationInFlightAction{
			kind: action.kind, generation: action.generation,
		}
	}
}

func (recorder *simulationRecorder) recordSupervisorCompletion(action supervisorPendingAction) {
	if recorder == nil {
		return
	}
	recorder.actionMutex.Lock()
	pending, found := recorder.actions[action.token]
	if !found || pending.kind != action.kind {
		recorder.actionMutex.Unlock()
		panic("simulation recorder action completion is stale or wrong")
	}
	delete(recorder.actions, action.token)
	recorder.actionMutex.Unlock()
	select {
	case recorder.actionWake <- struct{}{}:
	default:
	}
}

func (recorder *simulationRecorder) recordSupervisorAction(action supervisorAction) {
	recorder.recordSupervisorCompletion(supervisorPendingAction{kind: action.kind, token: action.token})
}

func (recorder *simulationRecorder) recordSupervisorDelivery(
	kind supervisorActionKind,
	generation attemptGeneration,
) simulationCausalSource {
	if recorder == nil {
		return simulationCausalSource{}
	}
	recorder.actionMutex.Lock()
	var matched supervisorActionToken
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
	driver *supervisorDriver,
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
	driver.mutex.Lock()
	supervisorState := driver.machine.Projection()
	supervisorMachine := driver.machine.Fork()
	driver.mutex.Unlock()
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
			supervisor: supervisorState, machine: supervisorMachine,
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
