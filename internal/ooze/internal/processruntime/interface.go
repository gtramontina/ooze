package processruntime

import "time"

// Lineage identifies one campaign invocation across recursive registration attempts.
type Lineage uint64

// Generation identifies one accepted attempt start.
type Generation uint64

// Campaign is an opaque process-runtime campaign authority.
type Campaign struct{ token campaignToken }

// ID returns the process-local campaign identity.
func (campaign Campaign) ID() uint64 { return uint64(campaign.token.id) }

// Lineage returns the registered campaign lineage.
func (campaign Campaign) Lineage() Lineage { return Lineage(campaign.token.lineage) }

// CampaignDecision identifies the result of campaign registration.
type CampaignDecision uint8

const (
	// CampaignRegistered accepts a new campaign lineage.
	CampaignRegistered CampaignDecision = iota + 1
	// CampaignRejectedRecursive rejects recursive registration.
	CampaignRejectedRecursive
	// CampaignRejectedClosed rejects registration after fatal closure.
	CampaignRejectedClosed
)

// Registration is the result of campaign registration.
type Registration struct{ value campaignRegistration }

// Decision returns the campaign registration decision.
func (registration Registration) Decision() CampaignDecision {
	return CampaignDecision(registration.value.decision)
}

// Campaign returns the registered campaign authority.
func (registration Registration) Campaign() Campaign {
	return Campaign{token: registration.value.token}
}

// AdmissionClass selects process-runtime admission policy.
type AdmissionClass uint8

const (
	// SharedAdmission shares detected process capacity.
	SharedAdmission AdmissionClass = iota + 1
	// ExclusiveAdmission requires process-wide exclusivity.
	ExclusiveAdmission
	// SerialPrimaryAdmission is a serial campaign primary.
	SerialPrimaryAdmission
	// ConfirmationAdmission is an exclusive confirmation attempt.
	ConfirmationAdmission
)

// Admission describes one attempt admission request.
type Admission struct {
	Campaign Campaign
	Attempt  string
	Class    AdmissionClass
	Profile  Profile
	Deadline time.Duration
}

// AdmissionDecision identifies the result of an admission request.
type AdmissionDecision uint8

const (
	// AdmissionAccepted retains an admission request.
	AdmissionAccepted AdmissionDecision = iota + 1
	// AdmissionRejectedClosed rejects work after fatal closure.
	AdmissionRejectedClosed
	// AdmissionRejectedUnknownCampaign rejects unknown campaign authority.
	AdmissionRejectedUnknownCampaign
	// AdmissionRejectedGateClosed rejects ordinary work behind a closed campaign gate.
	AdmissionRejectedGateClosed
	// AdmissionRejectedGateOpen rejects confirmation before its gate closes.
	AdmissionRejectedGateOpen
	// AdmissionRejectedDuplicate rejects a repeated attempt identity.
	AdmissionRejectedDuplicate
	// AdmissionRejectedExclusiveOutstanding rejects a second exclusive request.
	AdmissionRejectedExclusiveOutstanding
	// AdmissionRejectedSharedLimit rejects shared demand beyond the campaign bound.
	AdmissionRejectedSharedLimit
	// AdmissionRejectedAlreadyCommitted rejects cancellation after start commitment.
	AdmissionRejectedAlreadyCommitted
	// AdmissionCancelledWaiting cancels a request before grant.
	AdmissionCancelledWaiting
	// AdmissionCancelledGranted cancels a granted request.
	AdmissionCancelledGranted
	// AdmissionReturnedAfterClosure accepts a compensated grant after fatal closure.
	AdmissionReturnedAfterClosure
	// AdmissionReturnedAfterGateClosure accepts a grant after campaign gate closure.
	AdmissionReturnedAfterGateClosure
)

// Grant is an opaque admission capability.
type Grant struct{ authority admissionGrant }

// Admission returns the immutable fact carried by the grant.
func (grant Grant) Admission() Admission { return admissionValue(grant.authority) }

// Await is an opaque admission request capability.
type Await struct{ value admissionAwait }

// AdmissionResult is the inert result of an admission state transition.
type AdmissionResult struct{ value admissionResult }

// Decision returns the admission decision.
func (result AdmissionResult) Decision() AdmissionDecision {
	return AdmissionDecision(result.value.decision)
}

// Request returns the correlated admission request.
func (result AdmissionResult) Request() Admission { return admissionValue(result.value.request) }

// Deliveries returns newly granted admissions.
func (result AdmissionResult) Deliveries() []Admission {
	return admissionValues(result.value.deliveries)
}

// FatalEpoch returns the correlated fatal epoch.
func (result AdmissionResult) FatalEpoch() uint64 { return uint64(result.value.fatalEpoch) }

// Decision returns the admission decision.
func (await Await) Decision() AdmissionDecision { return AdmissionDecision(await.value.decision) }

// Receive waits for the request's admission grant or closure.
func (await Await) Receive() (Grant, bool) {
	grant, received := <-await.value.delivery
	return Grant{authority: grant}, received
}

// Request returns the immutable requested admission.
func (await Await) Request() Admission { return admissionValue(await.value.request) }

// StartDecision identifies the result of committing an attempt start.
type StartDecision uint8

const (
	// StartAccepted commits process-runtime custody.
	StartAccepted StartDecision = iota + 1
	// StartRejectedGrant rejects invalid grant authority.
	StartRejectedGrant
	// StartRejectedGate rejects a grant closed by its campaign gate.
	StartRejectedGate
	// StartRejectedClosed rejects a start after fatal closure.
	StartRejectedClosed
)

// StartCell carries one opaque start-installation capability.
type StartCell struct{ value *pendingStartCell }

// NewStartCell creates an unused start-installation capability.
func NewStartCell() *StartCell { return &StartCell{value: &pendingStartCell{}} }

// InstalledGeneration returns the generation installed in this capability.
func (cell *StartCell) InstalledGeneration() Generation {
	if cell == nil || cell.value == nil {
		return 0
	}
	return Generation(cell.value.installedGeneration())
}

// PreparedStart is the result and launch capability of start commitment.
type PreparedStart struct {
	value preparedStart
	cell  *StartCell
}

// StartResult is the inert result of a start commitment transition.
type StartResult struct{ value startCommittedResult }

// Decision returns the start commitment decision.
func (result StartResult) Decision() StartDecision { return StartDecision(result.value.decision) }

// Generation returns the accepted generation.
func (result StartResult) Generation() Generation { return Generation(result.value.generation) }

// SettlementAcknowledged reports settlement during start rejection.
func (result StartResult) SettlementAcknowledged() bool { return result.value.settlementAcknowledged }

// RuntimeClosureInProgress reports fatal runtime closure during start rejection.
func (result StartResult) RuntimeClosureInProgress() bool {
	return result.value.runtimeClosureInProgress
}

// Decision returns the start commitment decision.
func (start PreparedStart) Decision() StartDecision {
	return StartDecision(start.value.result.decision)
}

// Generation returns the committed attempt generation.
func (start PreparedStart) Generation() Generation { return Generation(start.value.result.generation) }

// Cell returns the opaque installation capability correlated with the start.
func (start PreparedStart) Cell() *StartCell {
	return start.cell
}

// Observation is immutable attempt evidence submitted to the process runtime.
type Observation struct{ value attemptObservation }

// ObservationKind identifies one attempt-evidence variant.
type ObservationKind uint8

const (
	// LaunchOwned reports successful ownership publication.
	LaunchOwned ObservationKind = iota + 1
	// LaunchNotReleased reports proven pre-release failure.
	LaunchNotReleased
	// AttemptSettled reports ordinary terminal settlement.
	AttemptSettled
	// AttemptTripped reports deadline or fuse evidence.
	AttemptTripped
	// LaunchUnconfirmedKind reports unresolved prospective custody.
	LaunchUnconfirmedKind
	// DrainUnconfirmedKind reports unresolved owned custody.
	DrainUnconfirmedKind
	// AttemptStopped reports stop before terminal evidence.
	AttemptStopped
	// AttemptInfrastructure reports infrastructure failure.
	AttemptInfrastructure
)

// Kind returns the attempt-evidence variant.
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

// Profile returns recorded execution-profile evidence.
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

// Deadline returns recorded deadline evidence.
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

// FuseTrip reports whether this is process-fuse evidence.
func (observation Observation) FuseTrip() bool {
	observed, ok := observation.value.(attemptTripped)
	return ok && observed.kind == fuseTrip
}

// ResourceExhausted reports whether no-release evidence is capacity pressure.
func (observation Observation) ResourceExhausted() bool {
	observed, ok := observation.value.(launchNotReleased)
	return ok && observed.reason == launchResourceExhausted
}

// Cause returns infrastructure failure evidence.
func (observation Observation) Cause() string {
	observed, _ := observation.value.(attemptInfrastructure)
	return observed.cause
}

// Owned reports that launch custody became owned.
func Owned() Observation { return Observation{value: launchOwned{}} }

// Settled reports an ordinary owned-attempt settlement.
func Settled(profile Profile, deadline time.Duration) Observation {
	return Observation{value: attemptSettled{profile: profile, deadline: deadline}}
}

// NotReleased reports a proven pre-release launch failure.
func NotReleased(resourceExhausted bool) Observation {
	reason := launchFailed
	if resourceExhausted {
		reason = launchResourceExhausted
	}
	return Observation{value: launchNotReleased{reason: reason}}
}

// Tripped reports attributable deadline or fuse evidence.
func Tripped(fuse bool, profile Profile, deadline time.Duration) Observation {
	kind := deadlineTrip
	if fuse {
		kind = fuseTrip
	}
	return Observation{value: attemptTripped{kind: kind, profile: profile, deadline: deadline}}
}

// LaunchUnconfirmed reports unresolved prospective launch custody.
func LaunchUnconfirmed() Observation { return Observation{value: launchUnconfirmed{}} }

// DrainUnconfirmed reports unresolved owned execution-domain custody.
func DrainUnconfirmed() Observation { return Observation{value: drainUnconfirmed{}} }

// Stopped reports an owned attempt stopped before terminal evidence.
func Stopped() Observation { return Observation{value: attemptStopped{}} }

// Infrastructure reports an owned attempt infrastructure failure.
func Infrastructure(cause string) Observation {
	return Observation{value: attemptInfrastructure{cause: cause}}
}

// Receipt is the result of accepted attempt evidence.
type Receipt struct{ value observationResult }

// SettlementAcknowledged reports whether runtime custody was released.
func (receipt Receipt) SettlementAcknowledged() bool { return receipt.value.settlementAcknowledged }

// Generation returns the correlated attempt generation.
func (receipt Receipt) Generation() Generation { return Generation(receipt.value.generation) }

// RuntimeClosureInProgress reports whether the fatal epoch owns remaining settlement.
func (receipt Receipt) RuntimeClosureInProgress() bool { return receipt.value.runtimeClosureInProgress }

// ConfirmationProvisional reports that a deadline requires exclusive confirmation.
func (receipt Receipt) ConfirmationProvisional() bool { return receipt.value.confirmationProvisional }

// PressureTransitioned reports entry into single-admission automatic mode.
func (receipt Receipt) PressureTransitioned() bool { return receipt.value.pressureTransitioned }

// ConfirmationObserved reports terminal evidence from a confirmation attempt.
func (receipt Receipt) ConfirmationObserved() bool { return receipt.value.confirmationObserved }

// ConfirmationQueueDrained reports authoritative completion of the confirmation queue.
func (receipt Receipt) ConfirmationQueueDrained() bool { return receipt.value.confirmationQueueDrained }

// FatalEpoch returns the correlated fatal epoch, if any.
func (receipt Receipt) FatalEpoch() uint64 { return uint64(receipt.value.fatalEpoch) }

// CancelledWaiting returns admissions cancelled before grant delivery.
func (receipt Receipt) CancelledWaiting() []Admission {
	return admissionValues(receipt.value.cancelledWaiting)
}

// CompensatedGrants returns delivered grants that callers must return.
func (receipt Receipt) CompensatedGrants() []Admission {
	return admissionValues(receipt.value.compensatedGrants)
}

// Deliveries returns admissions granted by this transition.
func (receipt Receipt) Deliveries() []Admission { return admissionValues(receipt.value.deliveries) }

// Launch invokes the dormant launch after the committed start is installed.
func (start PreparedStart) Launch(launch func(Generation) Observation) Observation {
	observed := start.value.start.launch(func(generation attemptGeneration) attemptObservation {
		return launch(Generation(generation)).value
	})
	return Observation{value: observed}
}

// Observe submits evidence through this start's runtime authority.
func (start PreparedStart) Observe(observation Observation) Receipt {
	return Receipt{value: start.value.start.shell.observeAttempt(start.value.start.generation, observation.value)}
}

// Barrier describes one confirmation barrier binding.
type Barrier struct {
	Campaign Campaign
	Attempt  string
	Profile  Profile
	Deadline time.Duration
}

// BarrierDecision identifies a confirmation barrier result.
type BarrierDecision uint8

const (
	// BarrierBound installs the confirmation barrier.
	BarrierBound BarrierDecision = iota + 1
	// BarrierRejectedMissing rejects a missing provisional barrier.
	BarrierRejectedMissing
	// BarrierRejectedClosureOutstanding rejects binding before campaign closure settles.
	BarrierRejectedClosureOutstanding
	// BarrierRejectedExecutionMismatch rejects confirmation facts that differ from the primary.
	BarrierRejectedExecutionMismatch
)

// BarrierAwait carries a confirmation barrier result and delivery capability.
type BarrierAwait struct{ value barrierAwait }

// Decision returns the barrier decision.
func (await BarrierAwait) Decision() BarrierDecision { return BarrierDecision(await.value.decision) }

// Request returns the bound confirmation admission.
func (await BarrierAwait) Request() Admission { return admissionValue(await.value.request) }

// Receive waits for the confirmation grant or closure.
func (await BarrierAwait) Receive() (Grant, bool) {
	grant, received := <-await.value.delivery
	return Grant{authority: grant}, received
}

// QueueDecision identifies confirmation queue completion.
type QueueDecision uint8

const (
	// ConfirmationQueueCompleted completes an empty confirmation queue.
	ConfirmationQueueCompleted QueueDecision = iota + 1
	// ConfirmationQueueRejectedMissing rejects a missing queue.
	ConfirmationQueueRejectedMissing
	// ConfirmationQueueRejectedOutstanding rejects a nonempty queue.
	ConfirmationQueueRejectedOutstanding
)

// QueueResult is the result of completing a confirmation queue.
type QueueResult struct{ value confirmationQueueResult }

// Decision returns the confirmation queue decision.
func (result QueueResult) Decision() QueueDecision { return QueueDecision(result.value.decision) }

// Deliveries returns immutable admission facts granted by queue completion.
func (result QueueResult) Deliveries() []Admission { return admissionValues(result.value.deliveries) }

// Projection is an opaque immutable process-runtime state projection.
// Resolution records exact emergency custody for one generation.
type Resolution struct {
	generation  Generation
	transferred bool
}

// ConfirmedDrained resolves one generation with proven empty custody.
func ConfirmedDrained(generation Generation) Resolution { return Resolution{generation: generation} }

// TransferCustody resolves one generation by transferring residual custody.
func TransferCustody(generation Generation) Resolution {
	return Resolution{generation: generation, transferred: true}
}

// Generation returns the resolved generation.
func (resolution Resolution) Generation() Generation { return resolution.generation }

// Transferred reports whether residual custody was transferred.
func (resolution Resolution) Transferred() bool { return resolution.transferred }

// Residual records unresolved execution-domain custody.
type Residual struct {
	generation  Generation
	attempt     string
	prospective bool
	transferred bool
}

// Generation returns the residual generation.
func (residual Residual) Generation() Generation { return residual.generation }

// Attempt returns the residual attempt identity.
func (residual Residual) Attempt() string { return residual.attempt }

// Prospective reports custody before owned publication.
func (residual Residual) Prospective() bool { return residual.prospective }

// Transferred reports whether residual custody was transferred.
func (residual Residual) Transferred() bool { return residual.transferred }

// Closure is the custody established when the runtime closes.
type Closure struct{ value runtimeClosure }

// Epoch returns the fatal epoch.
func (closure Closure) Epoch() uint64 { return uint64(closure.value.epoch) }

// CancelledWaiting returns admissions cancelled before grant delivery.
func (closure Closure) CancelledWaiting() []Admission {
	return admissionValues(closure.value.cancelledWaiting)
}

// CompensatedGrants returns delivered grants that callers must return.
func (closure Closure) CompensatedGrants() []Admission {
	return admissionValues(closure.value.compensatedGrants)
}

// Residual returns unresolved execution-domain custody.
func (closure Closure) Residual() []Residual { return residualValues(closure.value.residual) }

// EmergencySettlement is exact runtime-wide emergency settlement evidence.
type EmergencySettlement struct{ value emergencySettlement }

// Epoch returns the settled fatal epoch.
func (settlement EmergencySettlement) Epoch() uint64 { return uint64(settlement.value.epoch) }

// Owner returns the campaign owning residual custody.
func (settlement EmergencySettlement) Owner() Campaign {
	return Campaign{token: settlement.value.owner}
}

// Acknowledged returns settled generations in canonical order.
func (settlement EmergencySettlement) Acknowledged() []Generation {
	result := make([]Generation, len(settlement.value.acknowledged))
	for index, generation := range settlement.value.acknowledged {
		result[index] = Generation(generation)
	}
	return result
}

// Residual returns custody remaining after emergency settlement.
func (settlement EmergencySettlement) Residual() []Residual {
	return residualValues(settlement.value.residual)
}

// TerminalDecision identifies the result of campaign terminal commitment.
type TerminalDecision uint8

const (
	// TerminalCommitted retires a settled campaign.
	TerminalCommitted TerminalDecision = iota + 1
	// TerminalForcedAborted retires a campaign under fatal authority.
	TerminalForcedAborted
	// TerminalRejectedUnknown rejects unknown campaign authority.
	TerminalRejectedUnknown
	// TerminalRejectedOutstanding rejects a campaign with outstanding custody.
	TerminalRejectedOutstanding
	// TerminalRejectedClosed rejects ordinary commitment after fatal closure.
	TerminalRejectedClosed
)

// TerminalResult is the result of terminal campaign commitment.
type TerminalResult struct{ value terminalResult }

// Decision returns the terminal commitment decision.
func (result TerminalResult) Decision() TerminalDecision {
	return TerminalDecision(result.value.decision)
}

// Epoch returns the fatal epoch that rejected terminal commitment.
func (result TerminalResult) Epoch() uint64 { return uint64(result.value.epoch) }

// Runtime is the synchronized process-local coordination authority.
type Runtime struct{ shell *processRuntimeShell }

// BarrierResult is the inert result of confirmation barrier binding.
type BarrierResult struct{ value barrierResult }

// Decision returns the barrier decision.
func (result BarrierResult) Decision() BarrierDecision { return BarrierDecision(result.value.decision) }

// Request returns the bound confirmation admission.
func (result BarrierResult) Request() Admission { return admissionValue(result.value.request) }

// Deliveries returns grants released by the barrier transition.
func (result BarrierResult) Deliveries() []Admission { return admissionValues(result.value.deliveries) }

// New creates a process runtime with a fixed detected-capacity bound.
func New(capacity int) *Runtime { return &Runtime{shell: newProcessRuntimeShell(capacity)} }

// NewObserved creates a process runtime that publishes accepted transitions.
func NewObserved(capacity int, observer Observer) *Runtime {
	return &Runtime{shell: newProcessRuntimeShellWithObserver(capacity, observer)}
}

// RegisterCampaign registers one campaign lineage.
func (runtime *Runtime) RegisterCampaign(lineage Lineage) Registration {
	return Registration{value: runtime.shell.registerCampaign(campaignProvenance{lineage: campaignLineage(lineage)})}
}

// RequestAdmission requests process-runtime attempt admission.
func (runtime *Runtime) RequestAdmission(request Admission) Await {
	return Await{value: runtime.shell.requestAdmission(admissionRequest{
		campaign: request.Campaign.token,
		attempt:  attemptIdentity(request.Attempt),
		class:    admissionClass(request.Class),
		profile:  request.Profile,
		deadline: request.Deadline,
	})}
}

// CommitStart accepts runtime ownership before external launch.
func (runtime *Runtime) CommitStart(grant Grant, cell *StartCell) PreparedStart {
	if cell == nil {
		return PreparedStart{value: runtime.shell.startCommitted(grant.authority, startInstallation{})}
	}
	return PreparedStart{value: runtime.shell.startCommitted(grant.authority, startInstallation{
		grant: grant.authority,
		cell:  cell.value,
	}), cell: cell}
}

// Observe submits immutable attempt evidence.
func (runtime *Runtime) Observe(generation Generation, observation Observation) Receipt {
	return Receipt{value: runtime.shell.observeAttempt(attemptGeneration(generation), observation.value)}
}

// FatalEpoch returns the current fatal epoch.
func (runtime *Runtime) FatalEpoch() uint64 { return uint64(runtime.shell.fatalEpoch()) }

// EmergencySettlementRequired reports whether exact settlement is pending.
func (runtime *Runtime) EmergencySettlementRequired() bool {
	return runtime.shell.emergencySettlementRequired()
}

// CancelAdmission cancels a waiting or granted admission.
func (runtime *Runtime) CancelAdmission(request Admission) AdmissionDecision {
	return AdmissionDecision(runtime.shell.cancelAdmission(admissionAuthorityValue(request)).decision)
}

// ReturnGrant acknowledges a compensated grant.
func (runtime *Runtime) ReturnGrant(grant Grant) AdmissionDecision {
	return AdmissionDecision(runtime.shell.acknowledgeGrantReturn(grant.authority).decision)
}

// BindConfirmationBarrier binds the runtime-owned exclusive barrier.
func (runtime *Runtime) BindConfirmationBarrier(binding Barrier) BarrierAwait {
	return BarrierAwait{value: runtime.shell.sealAndBindConfirmationBarrier(barrierBinding{
		campaign: binding.Campaign.token, attempt: attemptIdentity(binding.Attempt),
		profile: binding.Profile, deadline: binding.Deadline,
	})}
}

// CompleteConfirmationQueue reopens ordinary campaign admission after confirmation.
func (runtime *Runtime) CompleteConfirmationQueue(campaign Campaign) QueueResult {
	return QueueResult{value: runtime.shell.completeConfirmationQueue(campaign.token)}
}

// Close starts or joins the process-lifetime fatal epoch.
func (runtime *Runtime) Close(cause string) Closure {
	return Closure{value: runtime.shell.closeRuntime(runtimeFatalCause(cause))}
}

// SettleEmergency settles exact runtime-wide emergency custody.
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

// AuthorizeForcedAbort authorizes terminal commitment for a fatal epoch owner.
func (runtime *Runtime) AuthorizeForcedAbort(campaign Campaign, epoch uint64) TerminalResult {
	return TerminalResult{value: runtime.shell.authorizeForcedAbort(campaign.token, fatalEpochID(epoch))}
}

// CommitTerminal requests terminal campaign commitment.
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
