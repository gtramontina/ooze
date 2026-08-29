package processruntime

import "reflect"

type Operation uint8

const (
	RegisterCampaignOperation Operation = iota + 1
	RequestAdmissionOperation
	CancelAdmissionOperation
	ReturnGrantOperation
	BindConfirmationBarrierOperation
	CompleteConfirmationQueueOperation
	CommitStartOperation
	ObserveAttemptOperation
	SettleEmergencyOperation
	CommitTerminalOperation
	AuthorizeForcedAbortOperation
	CloseOperation
)

type Cut struct {
	operation   Operation
	lineage     Lineage
	admission   Admission
	barrier     Barrier
	campaign    Campaign
	generation  Generation
	observation Observation
	resolutions []Resolution
	epoch       uint64
	cause       string
}

func (cut Cut) Operation() Operation { return cut.operation }

func (cut Cut) Malformed() (MalformedCut, bool) {
	if cut.operation != RequestAdmissionOperation && cut.operation != ReturnGrantOperation &&
		cut.operation != ObserveAttemptOperation && cut.operation != SettleEmergencyOperation &&
		cut.operation != CloseOperation {
		return MalformedCut{}, false
	}
	return MalformedCut{cut: cut}, true
}

type MalformedCut struct{ cut Cut }

func RegisterCampaignCut(lineage Lineage) Cut {
	return Cut{operation: RegisterCampaignOperation, lineage: lineage}
}

func RequestAdmissionCut(admission Admission) Cut {
	return Cut{operation: RequestAdmissionOperation, admission: admission}
}

func CancelAdmissionCut(admission Admission) Cut {
	return Cut{operation: CancelAdmissionOperation, admission: admission}
}

func ReturnGrantCut(admission Admission) Cut {
	return Cut{operation: ReturnGrantOperation, admission: admission}
}

func BindConfirmationBarrierCut(barrier Barrier) Cut {
	return Cut{operation: BindConfirmationBarrierOperation, barrier: barrier}
}

func CompleteConfirmationQueueCut(campaign Campaign) Cut {
	return Cut{operation: CompleteConfirmationQueueOperation, campaign: campaign}
}

func CommitStartCut(admission Admission) Cut {
	return Cut{operation: CommitStartOperation, admission: admission}
}

func ObserveAttemptCut(generation Generation, observation Observation) Cut {
	return Cut{operation: ObserveAttemptOperation, generation: generation, observation: observation}
}

func SettleEmergencyCut(resolutions []Resolution) Cut {
	return Cut{operation: SettleEmergencyOperation, resolutions: append([]Resolution(nil), resolutions...)}
}

func CommitTerminalCut(campaign Campaign) Cut {
	return Cut{operation: CommitTerminalOperation, campaign: campaign}
}

func AuthorizeForcedAbortCut(campaign Campaign, epoch uint64) Cut {
	return Cut{operation: AuthorizeForcedAbortOperation, campaign: campaign, epoch: epoch}
}

func CloseCut(cause string) Cut { return Cut{operation: CloseOperation, cause: cause} }

type ReplayResult struct {
	registration Registration
	admission    AdmissionResult
	barrier      BarrierResult
	queue        QueueResult
	start        StartResult
	receipt      Receipt
	terminal     TerminalResult
	closure      Closure
	settlement   EmergencySettlement
	recorded     RecordedCut
}

func (result ReplayResult) RecordedCut() RecordedCut { return result.recorded }

type RecordedCut struct {
	cut    Cut
	result recordedResult
}

type recordedResult struct {
	registrationDecision                             CampaignDecision
	campaign                                         Campaign
	admissionDecision                                AdmissionDecision
	request                                          Admission
	deliveries                                       []Admission
	fatalEpoch                                       uint64
	barrierDecision                                  BarrierDecision
	queueDecision                                    QueueDecision
	startDecision                                    StartDecision
	generation                                       Generation
	settlementAcknowledged, runtimeClosureInProgress bool
	confirmationProvisional, pressureTransitioned    bool
	confirmationObserved, confirmationQueueDrained   bool
	cancelledWaiting, compensatedGrants              []Admission
	terminalDecision                                 TerminalDecision
	epoch                                            uint64
	closureResidual, settlementResidual              []Residual
	owner                                            Campaign
	acknowledged                                     []Generation
}

func (recorded RecordedCut) Operation() Operation {
	return recorded.cut.operation
}

func (recorded RecordedCut) Matches(cut Cut) bool {
	return reflect.DeepEqual(recorded.cut, cut)
}

func (recorded RecordedCut) Result() ReplayResult {
	return thawRecordedResult(recorded.cut.operation, recorded.result)
}

func (recorded RecordedCut) Observation() (Generation, Observation, bool) {
	if recorded.cut.operation != ObserveAttemptOperation {
		return 0, Observation{}, false
	}
	return recorded.cut.generation, recorded.cut.observation, true
}

func (recorded RecordedCut) ExpectResultFrom(other RecordedCut) (CorruptedCut, bool) {
	if recorded.Operation() == 0 || recorded.Operation() != other.Operation() {
		return CorruptedCut{}, false
	}
	return CorruptedCut{cut: recorded.cut, result: other.result}, true
}

type CorruptedCut struct {
	cut    Cut
	result recordedResult
}

func (recorded RecordedCut) Complexity() int {
	return len(recorded.cut.resolutions) + len(recorded.result.deliveries) +
		len(recorded.result.cancelledWaiting) + len(recorded.result.compensatedGrants) +
		len(recorded.result.acknowledged) + len(recorded.result.closureResidual) +
		len(recorded.result.settlementResidual)
}

func (result ReplayResult) Registration() Registration { return result.registration }

func (result ReplayResult) Admission() AdmissionResult { return result.admission }

func (result ReplayResult) Barrier() BarrierResult { return result.barrier }

func (result ReplayResult) Queue() QueueResult { return result.queue }

func (result ReplayResult) Start() StartResult { return result.start }

func (result ReplayResult) Receipt() Receipt { return result.receipt }

func (result ReplayResult) Terminal() TerminalResult { return result.terminal }

func (result ReplayResult) Closure() Closure { return result.closure }

func (result ReplayResult) Settlement() EmergencySettlement { return result.settlement }

type Replay struct{ state processRuntime }

func NewReplay(capacity int) Replay { return Replay{state: newProcessRuntime(capacity)} }

func (replay Replay) Apply(cut Cut) (Replay, ReplayResult) {
	var result ReplayResult
	switch cut.operation {
	case RegisterCampaignOperation:
		var value campaignRegistration
		replay.state, value = replay.state.registerCampaign(campaignProvenance{lineage: campaignLineage(cut.lineage)})
		result.registration = Registration{value: value}
	case RequestAdmissionOperation:
		var value admissionResult
		replay.state, value = replay.state.requestAdmission(admissionAuthorityValue(cut.admission))
		result.admission = AdmissionResult{value: value}
	case CancelAdmissionOperation:
		var value admissionResult
		replay.state, value = replay.state.cancelAdmission(admissionAuthorityValue(cut.admission))
		result.admission = AdmissionResult{value: value}
	case ReturnGrantOperation:
		var value admissionResult
		replay.state, value = replay.state.acknowledgeGrantReturn(admissionAuthorityValue(cut.admission))
		result.admission = AdmissionResult{value: value}
	case BindConfirmationBarrierOperation:
		var value barrierResult
		replay.state, value = replay.state.sealAndBindConfirmationBarrier(barrierBinding{
			campaign: cut.barrier.Campaign.token, attempt: attemptIdentity(cut.barrier.Attempt),
			profile: cut.barrier.Profile, deadline: cut.barrier.Deadline,
		})
		result.barrier = BarrierResult{value: value}
	case CompleteConfirmationQueueOperation:
		var value confirmationQueueResult
		replay.state, value = replay.state.completeConfirmationQueue(cut.campaign.token)
		result.queue = QueueResult{value: value}
	case CommitStartOperation:
		var value startCommittedResult
		replay.state, value = replay.state.startCommitted(admissionAuthorityValue(cut.admission))
		result.start = StartResult{value: value}
	case ObserveAttemptOperation:
		var value observationResult
		replay.state, value = replay.state.observeAttempt(attemptGeneration(cut.generation), cut.observation.value)
		result.receipt = Receipt{value: value}
	case SettleEmergencyOperation:
		values := make([]emergencyResolution, len(cut.resolutions))
		for index, resolution := range cut.resolutions {
			disposition := emergencyConfirmedDrained
			if resolution.Transferred() {
				disposition = emergencyCustodyTransferred
			}
			values[index] = emergencyResolution{
				generation: attemptGeneration(resolution.Generation()), disposition: disposition,
			}
		}
		var value emergencySettlement
		replay.state, value = replay.state.settleEmergency(emergencySweep{resolutions: values})
		result.settlement = EmergencySettlement{value: value}
	case CommitTerminalOperation:
		var value terminalResult
		replay.state, value = replay.state.commitTerminal(cut.campaign.token)
		result.terminal = TerminalResult{value: value}
	case AuthorizeForcedAbortOperation:
		var value terminalResult
		replay.state, value = replay.state.authorizeForcedAbort(cut.campaign.token, fatalEpochID(cut.epoch))
		result.terminal = TerminalResult{value: value}
	case CloseOperation:
		var value runtimeClosure
		replay.state, value = replay.state.closeRuntime(runtimeFatalCause(cut.cause))
		result.closure = Closure{value: value}
	default:
		panic(Violation{operation: "replay", reason: "unknown operation"})
	}
	result.recorded = recordedEvent(cut, result)
	return replay, result
}

func (replay Replay) ApplyRecorded(recorded RecordedCut) (Replay, bool) {
	next, result := replay.Apply(recorded.cut)
	return next, reflect.DeepEqual(freezeReplayResult(recorded.cut.operation, result), recorded.result)
}

func (replay Replay) ApplyCorrupted(corrupted CorruptedCut) (Replay, bool) {
	next, result := replay.Apply(corrupted.cut)
	return next, reflect.DeepEqual(freezeReplayResult(corrupted.cut.operation, result), corrupted.result)
}

func (replay Replay) ApplyMalformed(malformed MalformedCut) Replay {
	next, _ := replay.Apply(malformed.cut)
	return next
}

func (replay Replay) Accepts(cut Cut) (accepted bool) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if _, invalid := recovered.(Violation); invalid {
			accepted = false
			return
		}
		panic(recovered)
	}()
	replay.Apply(cut)
	return true
}

func recordedEvent(cut Cut, result ReplayResult) RecordedCut {
	return RecordedCut{cut: cut, result: freezeReplayResult(cut.operation, result)}
}

func freezeReplayResult(operation Operation, result ReplayResult) recordedResult {
	frozen := recordedResult{}
	switch operation {
	case RegisterCampaignOperation:
		frozen.registrationDecision = result.Registration().Decision()
		frozen.campaign = result.Registration().Campaign()
	case RequestAdmissionOperation, CancelAdmissionOperation, ReturnGrantOperation:
		frozen.admissionDecision = result.Admission().Decision()
		frozen.request = result.Admission().Request()
		frozen.deliveries = result.Admission().Deliveries()
		frozen.fatalEpoch = result.Admission().FatalEpoch()
	case BindConfirmationBarrierOperation:
		frozen.barrierDecision = result.Barrier().Decision()
		frozen.request = result.Barrier().Request()
		frozen.deliveries = result.Barrier().Deliveries()
	case CompleteConfirmationQueueOperation:
		frozen.queueDecision = result.Queue().Decision()
		frozen.deliveries = result.Queue().Deliveries()
	case CommitStartOperation:
		frozen.startDecision = result.Start().Decision()
		frozen.generation = result.Start().Generation()
		frozen.settlementAcknowledged = result.Start().SettlementAcknowledged()
		frozen.runtimeClosureInProgress = result.Start().RuntimeClosureInProgress()
	case ObserveAttemptOperation:
		receipt := result.Receipt()
		frozen.generation = receipt.Generation()
		frozen.deliveries = receipt.Deliveries()
		frozen.cancelledWaiting = receipt.CancelledWaiting()
		frozen.compensatedGrants = receipt.CompensatedGrants()
		frozen.settlementAcknowledged = receipt.SettlementAcknowledged()
		frozen.runtimeClosureInProgress = receipt.RuntimeClosureInProgress()
		frozen.confirmationProvisional = receipt.ConfirmationProvisional()
		frozen.pressureTransitioned = receipt.PressureTransitioned()
		frozen.confirmationObserved = receipt.ConfirmationObserved()
		frozen.confirmationQueueDrained = receipt.ConfirmationQueueDrained()
		frozen.fatalEpoch = receipt.FatalEpoch()
	case SettleEmergencyOperation:
		settlement := result.Settlement()
		frozen.epoch = settlement.Epoch()
		frozen.owner = settlement.Owner()
		frozen.acknowledged = settlement.Acknowledged()
		frozen.settlementResidual = settlement.Residual()
	case CommitTerminalOperation, AuthorizeForcedAbortOperation:
		frozen.terminalDecision = result.Terminal().Decision()
		frozen.epoch = result.Terminal().Epoch()
	case CloseOperation:
		closure := result.Closure()
		frozen.epoch = closure.Epoch()
		frozen.cancelledWaiting = closure.CancelledWaiting()
		frozen.compensatedGrants = closure.CompensatedGrants()
		frozen.closureResidual = closure.Residual()
	}
	return frozen
}

func thawRecordedResult(operation Operation, frozen recordedResult) ReplayResult {
	result := ReplayResult{}
	switch operation {
	case RegisterCampaignOperation:
		result.registration = Registration{value: campaignRegistration{
			decision: campaignDecision(frozen.registrationDecision), token: frozen.campaign.token,
		}}
	case RequestAdmissionOperation, CancelAdmissionOperation, ReturnGrantOperation:
		result.admission = AdmissionResult{value: admissionResult{
			decision: admissionDecision(frozen.admissionDecision), request: admissionAuthorityValue(frozen.request),
			deliveries: admissionAuthorities(frozen.deliveries), fatalEpoch: fatalEpochID(frozen.fatalEpoch),
		}}
	case BindConfirmationBarrierOperation:
		result.barrier = BarrierResult{value: barrierResult{
			decision: barrierDecision(frozen.barrierDecision), request: admissionAuthorityValue(frozen.request),
			deliveries: admissionAuthorities(frozen.deliveries),
		}}
	case CompleteConfirmationQueueOperation:
		result.queue = QueueResult{value: confirmationQueueResult{
			decision:   confirmationQueueDecision(frozen.queueDecision),
			deliveries: admissionAuthorities(frozen.deliveries),
		}}
	case CommitStartOperation:
		result.start = StartResult{value: startCommittedResult{
			decision: startCommittedDecision(frozen.startDecision), generation: attemptGeneration(frozen.generation),
			settlementAcknowledged:   frozen.settlementAcknowledged,
			runtimeClosureInProgress: frozen.runtimeClosureInProgress,
		}}
	case ObserveAttemptOperation:
		result.receipt = Receipt{value: observationResult{
			generation: attemptGeneration(frozen.generation), deliveries: admissionAuthorities(frozen.deliveries),
			cancelledWaiting:         admissionAuthorities(frozen.cancelledWaiting),
			compensatedGrants:        admissionAuthorities(frozen.compensatedGrants),
			settlementAcknowledged:   frozen.settlementAcknowledged,
			runtimeClosureInProgress: frozen.runtimeClosureInProgress,
			confirmationProvisional:  frozen.confirmationProvisional,
			pressureTransitioned:     frozen.pressureTransitioned,
			confirmationObserved:     frozen.confirmationObserved,
			confirmationQueueDrained: frozen.confirmationQueueDrained,
			fatalEpoch:               fatalEpochID(frozen.fatalEpoch),
		}}
	case SettleEmergencyOperation:
		result.settlement = EmergencySettlement{value: emergencySettlement{
			epoch: fatalEpochID(frozen.epoch), owner: frozen.owner.token,
			acknowledged: attemptGenerations(frozen.acknowledged), residual: residualCustodies(frozen.settlementResidual),
		}}
	case CommitTerminalOperation, AuthorizeForcedAbortOperation:
		result.terminal = TerminalResult{value: terminalResult{
			decision: terminalDecision(frozen.terminalDecision), epoch: fatalEpochID(frozen.epoch),
		}}
	case CloseOperation:
		result.closure = Closure{value: runtimeClosure{
			epoch: fatalEpochID(frozen.epoch), cancelledWaiting: admissionAuthorities(frozen.cancelledWaiting),
			compensatedGrants: admissionAuthorities(frozen.compensatedGrants),
			residual:          residualCustodies(frozen.closureResidual),
		}}
	}
	return result
}

func admissionAuthorities(values []Admission) []admissionAuthority {
	result := make([]admissionAuthority, len(values))
	for index, value := range values {
		result[index] = admissionAuthorityValue(value)
	}
	return result
}

func attemptGenerations(values []Generation) []attemptGeneration {
	result := make([]attemptGeneration, len(values))
	for index, value := range values {
		result[index] = attemptGeneration(value)
	}
	return result
}

func residualCustodies(values []Residual) []residualCustody {
	result := make([]residualCustody, len(values))
	for index, value := range values {
		stage := admissionOwned
		if value.prospective {
			stage = admissionProspective
		}
		result[index] = residualCustody{
			generation: attemptGeneration(value.generation), attempt: attemptIdentity(value.attempt),
			stage: stage, transferred: value.transferred,
		}
	}
	return result
}

func (replay Replay) Projection() Projection { return projectState(replay.state) }
