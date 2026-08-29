package processruntime

import "time"

type Lineage uint64

type Generation uint64

type Campaign struct{ token campaignToken }

func (campaign Campaign) ID() uint64 { return uint64(campaign.token.id) }

func (campaign Campaign) Lineage() Lineage { return Lineage(campaign.token.lineage) }

type CampaignDecision uint8

const (
	CampaignRegistered CampaignDecision = iota + 1
	CampaignRejectedRecursive
	CampaignRejectedClosed
)

type Registration struct{ value campaignRegistration }

func (registration Registration) Decision() CampaignDecision {
	return CampaignDecision(registration.value.decision)
}

func (registration Registration) Campaign() Campaign {
	return Campaign{token: registration.value.token}
}

type AdmissionClass uint8

const (
	SharedAdmission AdmissionClass = iota + 1
	ExclusiveAdmission
	SerialPrimaryAdmission
	ConfirmationAdmission
)

type Admission struct {
	Campaign Campaign
	Attempt  string
	Class    AdmissionClass
	Profile  Profile
	Deadline time.Duration
}

type AdmissionDecision uint8

const (
	AdmissionAccepted AdmissionDecision = iota + 1
	AdmissionRejectedClosed
	AdmissionRejectedUnknownCampaign
	AdmissionRejectedGateClosed
	AdmissionRejectedGateOpen
	AdmissionRejectedDuplicate
	AdmissionRejectedExclusiveOutstanding
	AdmissionRejectedSharedLimit
	AdmissionRejectedAlreadyCommitted
	AdmissionCancelledWaiting
	AdmissionCancelledGranted
	AdmissionReturnedAfterClosure
	AdmissionReturnedAfterGateClosure
)

type Grant struct{ authority admissionGrant }

func (grant Grant) Admission() Admission { return admissionValue(grant.authority) }

type Await struct{ value admissionAwait }

type AdmissionResult struct{ value admissionResult }

func (result AdmissionResult) Decision() AdmissionDecision {
	return AdmissionDecision(result.value.decision)
}

func (result AdmissionResult) Request() Admission { return admissionValue(result.value.request) }

func (result AdmissionResult) Deliveries() []Admission {
	return admissionValues(result.value.deliveries)
}

func (result AdmissionResult) FatalEpoch() uint64 { return uint64(result.value.fatalEpoch) }

func (await Await) Decision() AdmissionDecision { return AdmissionDecision(await.value.decision) }

func (await Await) Receive() (Grant, bool) {
	grant, received := <-await.value.delivery
	return Grant{authority: grant}, received
}

func (await Await) Request() Admission { return admissionValue(await.value.request) }

type StartDecision uint8

const (
	StartAccepted StartDecision = iota + 1
	StartRejectedGrant
	StartRejectedGate
	StartRejectedClosed
)

type StartCell struct{ value *pendingStartCell }

func NewStartCell() *StartCell { return &StartCell{value: &pendingStartCell{}} }

func (cell *StartCell) InstalledGeneration() Generation {
	if cell == nil || cell.value == nil {
		return 0
	}
	return Generation(cell.value.installedGeneration())
}

type PreparedStart struct {
	value preparedStart
	cell  *StartCell
}

type StartResult struct{ value startCommittedResult }

func (result StartResult) Decision() StartDecision { return StartDecision(result.value.decision) }

func (result StartResult) Generation() Generation { return Generation(result.value.generation) }

func (result StartResult) SettlementAcknowledged() bool { return result.value.settlementAcknowledged }

func (result StartResult) RuntimeClosureInProgress() bool {
	return result.value.runtimeClosureInProgress
}

func (start PreparedStart) Decision() StartDecision {
	return StartDecision(start.value.result.decision)
}

func (start PreparedStart) Generation() Generation { return Generation(start.value.result.generation) }

func (start PreparedStart) Cell() *StartCell {
	return start.cell
}

type Observation struct{ value attemptObservation }

type ObservationKind uint8

const (
	LaunchOwned ObservationKind = iota + 1
	LaunchNotReleased
	AttemptSettled
	AttemptTripped
	LaunchUnconfirmedKind
	DrainUnconfirmedKind
	AttemptStopped
	AttemptInfrastructure
)

func (observation Observation) Kind() ObservationKind {
	switch observation.value.(type) {
	case launchOwned:
		return LaunchOwned
	case launchNotReleased:
		return LaunchNotReleased
	case attemptSettled:
		return AttemptSettled
	case attemptTripped:
		return AttemptTripped
	case launchUnconfirmed:
		return LaunchUnconfirmedKind
	case drainUnconfirmed:
		return DrainUnconfirmedKind
	case attemptStopped:
		return AttemptStopped
	case attemptInfrastructure:
		return AttemptInfrastructure
	default:
		return 0
	}
}

func (observation Observation) Profile() Profile {
	switch observation := observation.value.(type) {
	case attemptSettled:
		return observation.profile
	case attemptTripped:
		return observation.profile
	default:
		return 0
	}
}

func (observation Observation) Deadline() time.Duration {
	switch observation := observation.value.(type) {
	case attemptSettled:
		return observation.deadline
	case attemptTripped:
		return observation.deadline
	default:
		return 0
	}
}

func (observation Observation) FuseTrip() bool {
	observed, ok := observation.value.(attemptTripped)
	return ok && observed.kind == fuseTrip
}

func (observation Observation) ResourceExhausted() bool {
	observed, ok := observation.value.(launchNotReleased)
	return ok && observed.reason == launchResourceExhausted
}

func (observation Observation) Cause() string {
	observed, _ := observation.value.(attemptInfrastructure)
	return observed.cause
}

func Owned() Observation { return Observation{value: launchOwned{}} }

func Settled(profile Profile, deadline time.Duration) Observation {
	return Observation{value: attemptSettled{profile: profile, deadline: deadline}}
}

func NotReleased(resourceExhausted bool) Observation {
	reason := launchFailed
	if resourceExhausted {
		reason = launchResourceExhausted
	}
	return Observation{value: launchNotReleased{reason: reason}}
}

func Tripped(fuse bool, profile Profile, deadline time.Duration) Observation {
	kind := deadlineTrip
	if fuse {
		kind = fuseTrip
	}
	return Observation{value: attemptTripped{kind: kind, profile: profile, deadline: deadline}}
}

func LaunchUnconfirmed() Observation { return Observation{value: launchUnconfirmed{}} }

func DrainUnconfirmed() Observation { return Observation{value: drainUnconfirmed{}} }

func Stopped() Observation { return Observation{value: attemptStopped{}} }

func Infrastructure(cause string) Observation {
	return Observation{value: attemptInfrastructure{cause: cause}}
}

type Receipt struct{ value observationResult }

func (receipt Receipt) SettlementAcknowledged() bool { return receipt.value.settlementAcknowledged }

func (receipt Receipt) Generation() Generation { return Generation(receipt.value.generation) }

func (receipt Receipt) RuntimeClosureInProgress() bool { return receipt.value.runtimeClosureInProgress }

func (receipt Receipt) ConfirmationProvisional() bool { return receipt.value.confirmationProvisional }

func (receipt Receipt) PressureTransitioned() bool { return receipt.value.pressureTransitioned }

func (receipt Receipt) ConfirmationObserved() bool { return receipt.value.confirmationObserved }

func (receipt Receipt) ConfirmationQueueDrained() bool { return receipt.value.confirmationQueueDrained }

func (receipt Receipt) FatalEpoch() uint64 { return uint64(receipt.value.fatalEpoch) }

func (receipt Receipt) CancelledWaiting() []Admission {
	return admissionValues(receipt.value.cancelledWaiting)
}

func (receipt Receipt) CompensatedGrants() []Admission {
	return admissionValues(receipt.value.compensatedGrants)
}

func (receipt Receipt) Deliveries() []Admission { return admissionValues(receipt.value.deliveries) }

func (start PreparedStart) Launch(launch func(Generation) Observation) Observation {
	observed := start.value.start.launch(func(generation attemptGeneration) attemptObservation {
		return launch(Generation(generation)).value
	})
	return Observation{value: observed}
}

func (start PreparedStart) Observe(observation Observation) Receipt {
	return Receipt{value: start.value.start.shell.observeAttempt(start.value.start.generation, observation.value)}
}

type Barrier struct {
	Campaign Campaign
	Attempt  string
	Profile  Profile
	Deadline time.Duration
}

type BarrierDecision uint8

const (
	BarrierBound BarrierDecision = iota + 1
	BarrierRejectedMissing
	BarrierRejectedClosureOutstanding
	BarrierRejectedExecutionMismatch
)

type BarrierAwait struct{ value barrierAwait }

func (await BarrierAwait) Decision() BarrierDecision { return BarrierDecision(await.value.decision) }

func (await BarrierAwait) Request() Admission { return admissionValue(await.value.request) }

func (await BarrierAwait) Receive() (Grant, bool) {
	grant, received := <-await.value.delivery
	return Grant{authority: grant}, received
}

type QueueDecision uint8

const (
	ConfirmationQueueCompleted QueueDecision = iota + 1
	ConfirmationQueueRejectedMissing
	ConfirmationQueueRejectedOutstanding
)

type QueueResult struct{ value confirmationQueueResult }

func (result QueueResult) Decision() QueueDecision { return QueueDecision(result.value.decision) }

func (result QueueResult) Deliveries() []Admission { return admissionValues(result.value.deliveries) }

type Resolution struct {
	generation  Generation
	transferred bool
}

func ConfirmedDrained(generation Generation) Resolution { return Resolution{generation: generation} }

func TransferCustody(generation Generation) Resolution {
	return Resolution{generation: generation, transferred: true}
}

func (resolution Resolution) Generation() Generation { return resolution.generation }

func (resolution Resolution) Transferred() bool { return resolution.transferred }

type Residual struct {
	generation  Generation
	attempt     string
	prospective bool
	transferred bool
}

func (residual Residual) Generation() Generation { return residual.generation }

func (residual Residual) Attempt() string { return residual.attempt }

func (residual Residual) Prospective() bool { return residual.prospective }

func (residual Residual) Transferred() bool { return residual.transferred }

type Closure struct{ value runtimeClosure }

func (closure Closure) Epoch() uint64 { return uint64(closure.value.epoch) }

func (closure Closure) CancelledWaiting() []Admission {
	return admissionValues(closure.value.cancelledWaiting)
}

func (closure Closure) CompensatedGrants() []Admission {
	return admissionValues(closure.value.compensatedGrants)
}

func (closure Closure) Residual() []Residual { return residualValues(closure.value.residual) }

type EmergencySettlement struct{ value emergencySettlement }

func (settlement EmergencySettlement) Epoch() uint64 { return uint64(settlement.value.epoch) }

func (settlement EmergencySettlement) Owner() Campaign {
	return Campaign{token: settlement.value.owner}
}

func (settlement EmergencySettlement) Acknowledged() []Generation {
	result := make([]Generation, len(settlement.value.acknowledged))
	for index, generation := range settlement.value.acknowledged {
		result[index] = Generation(generation)
	}
	return result
}

func (settlement EmergencySettlement) Residual() []Residual {
	return residualValues(settlement.value.residual)
}

type TerminalDecision uint8

const (
	TerminalCommitted TerminalDecision = iota + 1
	TerminalForcedAborted
	TerminalRejectedUnknown
	TerminalRejectedOutstanding
	TerminalRejectedClosed
)

type TerminalResult struct{ value terminalResult }

func (result TerminalResult) Decision() TerminalDecision {
	return TerminalDecision(result.value.decision)
}

func (result TerminalResult) Epoch() uint64 { return uint64(result.value.epoch) }

type Runtime struct{ shell *processRuntimeShell }

type BarrierResult struct{ value barrierResult }

func (result BarrierResult) Decision() BarrierDecision { return BarrierDecision(result.value.decision) }

func (result BarrierResult) Request() Admission { return admissionValue(result.value.request) }

func (result BarrierResult) Deliveries() []Admission { return admissionValues(result.value.deliveries) }

func New(capacity int) *Runtime { return &Runtime{shell: newProcessRuntimeShell(capacity)} }

func NewObserved(capacity int, observer Observer) *Runtime {
	return &Runtime{shell: newProcessRuntimeShellWithObserver(capacity, observer)}
}

func (runtime *Runtime) RegisterCampaign(lineage Lineage) Registration {
	return Registration{value: runtime.shell.registerCampaign(campaignProvenance{lineage: campaignLineage(lineage)})}
}

func (runtime *Runtime) RequestAdmission(request Admission) Await {
	return Await{value: runtime.shell.requestAdmission(admissionRequest{
		campaign: request.Campaign.token,
		attempt:  attemptIdentity(request.Attempt),
		class:    admissionClass(request.Class),
		profile:  request.Profile,
		deadline: request.Deadline,
	})}
}

func (runtime *Runtime) CommitStart(grant Grant, cell *StartCell) PreparedStart {
	if cell == nil {
		return PreparedStart{value: runtime.shell.startCommitted(grant.authority, startInstallation{})}
	}
	return PreparedStart{value: runtime.shell.startCommitted(grant.authority, startInstallation{
		grant: grant.authority,
		cell:  cell.value,
	}), cell: cell}
}

func (runtime *Runtime) Observe(generation Generation, observation Observation) Receipt {
	return Receipt{value: runtime.shell.observeAttempt(attemptGeneration(generation), observation.value)}
}

func (runtime *Runtime) FatalEpoch() uint64 { return uint64(runtime.shell.fatalEpoch()) }

func (runtime *Runtime) EmergencySettlementRequired() bool {
	return runtime.shell.emergencySettlementRequired()
}

func (runtime *Runtime) CancelAdmission(request Admission) AdmissionDecision {
	return AdmissionDecision(runtime.shell.cancelAdmission(admissionAuthorityValue(request)).decision)
}

func (runtime *Runtime) ReturnGrant(grant Grant) AdmissionDecision {
	return AdmissionDecision(runtime.shell.acknowledgeGrantReturn(grant.authority).decision)
}

func (runtime *Runtime) BindConfirmationBarrier(binding Barrier) BarrierAwait {
	return BarrierAwait{value: runtime.shell.sealAndBindConfirmationBarrier(barrierBinding{
		campaign: binding.Campaign.token, attempt: attemptIdentity(binding.Attempt),
		profile: binding.Profile, deadline: binding.Deadline,
	})}
}

func (runtime *Runtime) CompleteConfirmationQueue(campaign Campaign) QueueResult {
	return QueueResult{value: runtime.shell.completeConfirmationQueue(campaign.token)}
}

func (runtime *Runtime) Close(cause string) Closure {
	return Closure{value: runtime.shell.closeRuntime(runtimeFatalCause(cause))}
}

func (runtime *Runtime) SettleEmergency(resolutions []Resolution) EmergencySettlement {
	values := make([]emergencyResolution, len(resolutions))
	for index, resolution := range resolutions {
		disposition := emergencyConfirmedDrained
		if resolution.Transferred() {
			disposition = emergencyCustodyTransferred
		}
		values[index] = emergencyResolution{
			generation: attemptGeneration(resolution.Generation()), disposition: disposition,
		}
	}
	return EmergencySettlement{value: runtime.shell.settleEmergency(emergencySweep{resolutions: values})}
}

func (runtime *Runtime) AuthorizeForcedAbort(campaign Campaign, epoch uint64) TerminalResult {
	return TerminalResult{value: runtime.shell.authorizeForcedAbort(campaign.token, fatalEpochID(epoch))}
}

func (runtime *Runtime) CommitTerminal(campaign Campaign) TerminalResult {
	return TerminalResult{value: runtime.shell.commitTerminal(campaign.token)}
}

func admissionValue(authority admissionAuthority) Admission {
	return Admission{
		Campaign: Campaign{token: authority.campaign}, Attempt: string(authority.attempt),
		Class: AdmissionClass(authority.class), Profile: authority.profile, Deadline: authority.deadline,
	}
}

func admissionValues[Values ~[]admissionAuthority](values Values) []Admission {
	result := make([]Admission, len(values))
	for index, value := range values {
		result[index] = admissionValue(value)
	}
	return result
}

func admissionAuthorityValue(value Admission) admissionAuthority {
	return admissionAuthority{
		campaign: value.Campaign.token, attempt: attemptIdentity(value.Attempt), class: admissionClass(value.Class),
		profile: value.Profile, deadline: value.Deadline,
	}
}

func residualValues(values []residualCustody) []Residual {
	result := make([]Residual, len(values))
	for index, value := range values {
		result[index] = Residual{
			generation: Generation(value.generation), attempt: string(value.attempt),
			prospective: value.stage == admissionProspective, transferred: value.transferred,
		}
	}
	return result
}
