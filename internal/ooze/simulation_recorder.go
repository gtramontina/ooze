package ooze

import (
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

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
	state processRuntime,
) {
	if recorder == nil {
		return
	}
	record.sequence = reservation.sequence
	record.authority = reservation.authority
	record.source = recorder.runtimeSource(record)
	record.runtimeState = simulationTraceRuntimeState(state)
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
	state supervisorState,
	actions []supervisorAction,
) {
	if recorder == nil {
		return
	}
	recorder.recordSupervisorActions(actions)
	source := recorder.supervisorSource(event)
	recorder.append(simulationRecord{
		sequence: reservation.sequence, authority: reservation.authority, source: source,
		supervisorEvent:   simulationTraceSupervisorEvent(event),
		supervisorState:   simulationTraceSupervisorState(state),
		supervisorActions: simulationTraceSupervisorActions(actions),
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
	if record.runtimeOperation == simulationRegisterCampaign {
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
		switch record.runtimeOperation {
		case simulationObserveAttempt:
			matches = action.generation == record.runtimeGeneration &&
				(action.kind == supervisorPublishOwned || action.kind == supervisorCloseProspective ||
					action.kind == supervisorSettleRuntime ||
					action.kind == supervisorTransferResidualCustody)
		case simulationCompleteConfirmationQueue:
			matches = action.kind == supervisorDeliverTerminal
		case simulationSettleEmergency:
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

	return simulationCausalSource{kind: simulationSupervisorActionSource, identity: uint64(matched)}
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
	switch event := payload.(type) {
	case campaignRegisteredEvent:
		return record.runtimeOperation == simulationRegisterCampaign &&
			record.runtimeRegistration == event.registration
	case admissionGrantedEvent:
		return slices.ContainsFunc(simulationRuntimeDeliveries(record), func(delivery simulationAdmission) bool {
			return campaignAdmissionValue(delivery.production()) == event.grant
		})
	case admissionCancelledEvent:
		return record.runtimeOperation == simulationCancelAdmission &&
			campaignAdmissionValue(record.runtimeAdmissionToken.production()) == event.request &&
			record.runtimeAdmissionOut.decision == event.result.decision &&
			campaignAdmissionValue(record.runtimeAdmissionOut.request.production()) == event.result.request &&
			record.runtimeAdmissionOut.fatalEpoch == event.result.fatalEpoch
	case admissionRejectedEvent:
		return record.runtimeOperation == simulationRequestAdmission &&
			record.runtimeAdmissionOut.decision == event.result.decision &&
			campaignAdmissionValue(record.runtimeAdmissionOut.request.production()) == event.result.request
	case startCommittedEvent:
		return record.runtimeOperation == simulationStartCommitted && record.runtimeStart == startCommittedResult(event.result)
	case attemptLaunchEvent:
		return record.runtimeOperation == simulationObserveAttempt &&
			record.runtimeObservation.kind == simulationLaunchOwnedObservation &&
			record.runtimeGeneration == event.generation
	case confirmationBarrierBoundEvent:
		return record.runtimeOperation == simulationBindConfirmationBarrier &&
			record.runtimeBarrierOut.decision == event.result.decision &&
			campaignAdmissionValue(record.runtimeBarrierOut.request.production()) == event.result.request &&
			slices.EqualFunc(record.runtimeBarrierOut.deliveries, event.result.deliveries,
				func(left simulationAdmission, right campaignAdmission) bool {
					return campaignAdmissionValue(left.production()) == right
				})
	case grantReturnAcknowledgedEvent:
		return record.runtimeOperation == simulationAcknowledgeGrantReturn &&
			record.runtimeAdmissionOut.decision == event.result.decision
	case terminalCommittedEvent:
		return record.runtimeOperation == simulationCommitTerminal &&
			record.runtimeTerminal == terminalResult(event.result)
	default:
		return false
	}
}

func simulationRuntimeDeliveries(record simulationRecord) []simulationAdmission {
	switch record.runtimeOperation {
	case simulationRequestAdmission, simulationCancelAdmission, simulationAcknowledgeGrantReturn:
		return record.runtimeAdmissionOut.deliveries
	case simulationBindConfirmationBarrier:
		return record.runtimeBarrierOut.deliveries
	case simulationCompleteConfirmationQueue:
		return record.runtimeQueueOut.deliveries
	case simulationObserveAttempt:
		return record.runtimeObservationOut.deliveries
	default:
		return nil
	}
}

func (recorder *simulationRecorder) supervisorSource(event supervisorEvent) simulationCausalSource {
	if token := simulationSupervisorEventAction(event); token != 0 {
		return simulationCausalSource{kind: simulationSupervisorActionSource, identity: uint64(token)}
	}
	if event.kind == supervisorRuntimeCompleted {
		action := event.runtime.action
		return recorder.takeRuntimeCut(func(record simulationRecord) bool {
			return record.runtimeOperation == simulationObserveAttempt && record.runtimeGeneration == event.generation &&
				record.source.kind == simulationSupervisorActionSource &&
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

func simulationSupervisorEventAction(event supervisorEvent) supervisorActionToken {
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

	return simulationCausalSource{kind: simulationSupervisorActionSource, identity: uint64(matched)}
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
	records := slices.Clone(recorder.records)
	recorder.mutex.Unlock()
	sort.Slice(records, func(left, right int) bool {
		return records[left].sequence < records[right].sequence
	})

	runtimeState := runtime.Image()
	driver.mutex.Lock()
	supervisorState := simulationProjectSupervisorState(driver.state)
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
		supervisor:    simulationTraceSupervisorState(supervisorState),
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
			campaign: campaignState, runtime: runtimeState, supervisor: supervisorState,
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

func simulationProjectSupervisorState(state supervisorState) supervisorState {
	state = cloneSupervisorState(state)
	state.emergency.at = simulationCanonicalTime(state.emergency.at)
	state.emergency.drainBy = simulationCanonicalTime(state.emergency.drainBy)
	for index := range state.attempts {
		attempt := &state.attempts[index]
		attempt.registeredAt = simulationCanonicalTime(attempt.registeredAt)
		attempt.launchBy = simulationCanonicalTime(attempt.launchBy)
		attempt.lastEventAt = simulationCanonicalTime(attempt.lastEventAt)
		attempt.revokedAt = simulationCanonicalTime(attempt.revokedAt)
		attempt.startedAt = simulationCanonicalTime(attempt.startedAt)
		attempt.deadlineAt = simulationCanonicalTime(attempt.deadlineAt)
		attempt.intent.at = simulationCanonicalTime(attempt.intent.at)
		attempt.intent.drainBy = simulationCanonicalTime(attempt.intent.drainBy)
		attempt.intent.stop = simulationProjectStop(attempt.intent.stop)
		attempt.drain.effectiveDrainBy = simulationCanonicalTime(attempt.drain.effectiveDrainBy)
	}

	return state
}

func simulationProjectStop(stop StopRequest) StopRequest {
	stop.At = simulationCanonicalTime(stop.At)
	stop.DrainBy = simulationCanonicalTime(stop.DrainBy)

	return stop
}

func simulationCanonicalTime(instant time.Time) time.Time {
	if instant.IsZero() {
		return time.Time{}
	}

	return time.Unix(0, instant.UnixNano()).UTC()
}
