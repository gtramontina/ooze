package processruntime

import "reflect"

// Operation identifies one process-runtime transition in a deterministic trace.
type Operation uint8

const (
	// RegisterCampaignOperation registers a campaign lineage.
	RegisterCampaignOperation Operation = iota + 1
	// RequestAdmissionOperation requests attempt admission.
	RequestAdmissionOperation
	// CancelAdmissionOperation cancels an admission request.
	CancelAdmissionOperation
	// ReturnGrantOperation acknowledges a compensated grant.
	ReturnGrantOperation
	// BindConfirmationBarrierOperation binds an exclusive confirmation barrier.
	BindConfirmationBarrierOperation
	// CompleteConfirmationQueueOperation completes a confirmation queue.
	CompleteConfirmationQueueOperation
	// CommitStartOperation commits runtime ownership before launch.
	CommitStartOperation
	// ObserveAttemptOperation submits attempt evidence.
	ObserveAttemptOperation
	// SettleEmergencyOperation settles exact emergency custody.
	SettleEmergencyOperation
	// CommitTerminalOperation commits ordinary campaign termination.
	CommitTerminalOperation
	// AuthorizeForcedAbortOperation authorizes fatal campaign termination.
	AuthorizeForcedAbortOperation
	// CloseOperation starts or joins fatal runtime closure.
	CloseOperation
)

// Cut is one inert process-runtime trace input.
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

// Operation returns the transition kind.
func (cut Cut) Operation() Operation { return cut.operation }

// RegisterCampaignCut creates a campaign-registration input.
func RegisterCampaignCut(lineage Lineage) Cut {
	return Cut{operation: RegisterCampaignOperation, lineage: lineage}
}

// RequestAdmissionCut creates an admission-request input.
func RequestAdmissionCut(admission Admission) Cut {
	return Cut{operation: RequestAdmissionOperation, admission: admission}
}

// CancelAdmissionCut creates an admission-cancellation input.
func CancelAdmissionCut(admission Admission) Cut {
	return Cut{operation: CancelAdmissionOperation, admission: admission}
}

// ReturnGrantCut creates a grant-return input.
func ReturnGrantCut(admission Admission) Cut {
	return Cut{operation: ReturnGrantOperation, admission: admission}
}

// BindConfirmationBarrierCut creates a barrier-binding input.
func BindConfirmationBarrierCut(barrier Barrier) Cut {
	return Cut{operation: BindConfirmationBarrierOperation, barrier: barrier}
}

// CompleteConfirmationQueueCut creates a confirmation-queue completion input.
func CompleteConfirmationQueueCut(campaign Campaign) Cut {
	return Cut{operation: CompleteConfirmationQueueOperation, campaign: campaign}
}

// CommitStartCut creates a start-commitment input.
func CommitStartCut(admission Admission) Cut {
	return Cut{operation: CommitStartOperation, admission: admission}
}

// ObserveAttemptCut creates an attempt-evidence input.
func ObserveAttemptCut(generation Generation, observation Observation) Cut {
	return Cut{operation: ObserveAttemptOperation, generation: generation, observation: observation}
}

// SettleEmergencyCut creates an emergency-settlement input.
func SettleEmergencyCut(resolutions []Resolution) Cut {
	return Cut{operation: SettleEmergencyOperation, resolutions: append([]Resolution(nil), resolutions...)}
}

// CommitTerminalCut creates an ordinary terminal-commitment input.
func CommitTerminalCut(campaign Campaign) Cut {
	return Cut{operation: CommitTerminalOperation, campaign: campaign}
}

// AuthorizeForcedAbortCut creates a fatal terminal-authorization input.
func AuthorizeForcedAbortCut(campaign Campaign, epoch uint64) Cut {
	return Cut{operation: AuthorizeForcedAbortOperation, campaign: campaign, epoch: epoch}
}

// CloseCut creates a fatal-closure input.
func CloseCut(cause string) Cut { return Cut{operation: CloseOperation, cause: cause} }

// ReplayResult is the owner-authored result of one replayed cut.
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
}

// Registration returns the campaign-registration result.
func (result ReplayResult) Registration() Registration { return result.registration }

// Admission returns the admission result.
func (result ReplayResult) Admission() AdmissionResult { return result.admission }

// Barrier returns the barrier-binding result.
func (result ReplayResult) Barrier() BarrierResult { return result.barrier }

// Queue returns the confirmation-queue result.
func (result ReplayResult) Queue() QueueResult { return result.queue }

// Start returns the start-commitment result.
func (result ReplayResult) Start() StartResult { return result.start }

// Receipt returns the attempt-evidence receipt.
func (result ReplayResult) Receipt() Receipt { return result.receipt }

// Terminal returns the terminal-commitment result.
func (result ReplayResult) Terminal() TerminalResult { return result.terminal }

// Closure returns the fatal-closure result.
func (result ReplayResult) Closure() Closure { return result.closure }

// Settlement returns the emergency-settlement result.
func (result ReplayResult) Settlement() EmergencySettlement { return result.settlement }

// Replay owns deterministic process-runtime transition replay and queries.
type Replay struct{ state processRuntime }

// NewReplay creates an empty deterministic process-runtime replay.
func NewReplay(capacity int) Replay { return Replay{state: newProcessRuntime(capacity)} }

// Apply reduces one owner-authored cut.
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
	return replay, result
}

// Accepts reports whether the production reducer accepts a proposed cut.
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

// ApplyEvent replays one accepted production event and verifies its result.
func (replay Replay) ApplyEvent(event Event) (Replay, bool) {
	cut, expected := replayEvent(event)
	next, result := replay.Apply(cut)
	result = canonicalReplayResult(cut.operation, result)
	return next, reflect.DeepEqual(result, expected)
}

func canonicalReplayResult(operation Operation, result ReplayResult) ReplayResult {
	switch operation {
	case RequestAdmissionOperation, CancelAdmissionOperation, ReturnGrantOperation:
		result.admission.value = runtimeEventAdmissionResult(result.admission.value)
	case BindConfirmationBarrierOperation:
		result.barrier.value = runtimeEventBarrierResult(result.barrier.value)
	case CompleteConfirmationQueueOperation:
		result.queue.value.deliveries = runtimeEventAdmissions(result.queue.value.deliveries)
	case ObserveAttemptOperation:
		result.receipt.value = runtimeEventObservationResult(result.receipt.value)
	case CloseOperation:
		result.closure.value = runtimeEventClosure(result.closure.value)
	case SettleEmergencyOperation:
		result.settlement.value = runtimeEventEmergency(result.settlement.value)
	}
	return result
}

func replayEvent(event Event) (Cut, ReplayResult) {
	switch event := event.(type) {
	case runtimeCampaignRegistrationProcessed:
		return RegisterCampaignCut(Lineage(event.provenance.lineage)), ReplayResult{
			registration: Registration{value: event.result},
		}
	case runtimeAdmissionRequestProcessed:
		return RequestAdmissionCut(admissionValue(event.request)), ReplayResult{
			admission: AdmissionResult{value: event.result},
		}
	case runtimeAdmissionCancellationProcessed:
		return CancelAdmissionCut(admissionValue(event.request)), ReplayResult{
			admission: AdmissionResult{value: event.result},
		}
	case runtimeGrantReturnProcessed:
		return ReturnGrantCut(admissionValue(event.grant)), ReplayResult{
			admission: AdmissionResult{value: event.result},
		}
	case runtimeConfirmationBarrierProcessed:
		return BindConfirmationBarrierCut(event.Barrier()), ReplayResult{
			barrier: BarrierResult{value: event.result},
		}
	case runtimeConfirmationQueueProcessed:
		return CompleteConfirmationQueueCut(Campaign{token: event.campaign}), ReplayResult{
			queue: QueueResult{value: event.result},
		}
	case runtimeStartCommitmentProcessed:
		return CommitStartCut(admissionValue(event.grant)), ReplayResult{
			start: StartResult{value: event.result},
		}
	case runtimeAttemptObservationProcessed:
		return ObserveAttemptCut(Generation(event.generation), Observation{value: event.observation}), ReplayResult{
			receipt: Receipt{value: event.result},
		}
	case runtimeEmergencySettlementProcessed:
		return SettleEmergencyCut(event.Resolutions()), ReplayResult{
			settlement: EmergencySettlement{value: event.result},
		}
	case runtimeTerminalCommitmentProcessed:
		return CommitTerminalCut(Campaign{token: event.campaign}), ReplayResult{
			terminal: TerminalResult{value: event.result},
		}
	case runtimeForcedAbortProcessed:
		return AuthorizeForcedAbortCut(Campaign{token: event.campaign}, uint64(event.epoch)), ReplayResult{
			terminal: TerminalResult{value: event.result},
		}
	case runtimeClosureProcessed:
		return CloseCut(string(event.cause)), ReplayResult{closure: Closure{value: event.result}}
	default:
		panic(Violation{operation: "replay event", reason: "unknown event"})
	}
}

// Projection returns a capability-free immutable domain projection.
func (replay Replay) Projection() Projection { return projectState(replay.state) }
