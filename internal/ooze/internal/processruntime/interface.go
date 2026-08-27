package processruntime

import (
	"slices"
	"time"
)

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
	CampaignRegistered CampaignDecision = iota + 1
	CampaignRejectedRecursive
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
	SharedAdmission AdmissionClass = iota + 1
	ExclusiveAdmission
	SerialPrimaryAdmission
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
	StartAccepted StartDecision = iota + 1
	StartRejectedGrant
	StartRejectedGate
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
	LaunchOwned ObservationKind = iota + 1
	LaunchNotReleased
	AttemptSettled
	AttemptTripped
	LaunchUnconfirmedKind
	DrainUnconfirmedKind
	AttemptStopped
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
	BarrierBound BarrierDecision = iota + 1
	BarrierRejectedMissing
	BarrierRejectedClosureOutstanding
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
	ConfirmationQueueCompleted QueueDecision = iota + 1
	ConfirmationQueueRejectedMissing
	ConfirmationQueueRejectedOutstanding
)

// QueueResult is the result of completing a confirmation queue.
type QueueResult struct{ value confirmationQueueResult }

// Decision returns the confirmation queue decision.
func (result QueueResult) Decision() QueueDecision { return QueueDecision(result.value.decision) }

// Deliveries returns immutable admission facts granted by queue completion.
func (result QueueResult) Deliveries() []Admission { return admissionValues(result.value.deliveries) }

// Image is an opaque immutable process-runtime state projection.
type Image struct {
	capacity    int
	nextID      uint64
	mode        admissionMode
	lifecycle   runtimeLifecycle
	fatalCauses []runtimeFatalCause
	fatalEpoch  fatalEpochID
	fatalOwner  campaignToken
	campaigns   []registeredCampaign
	admissions  []imageAdmission
}

type imageAdmission struct {
	authority   imageAuthority
	stage       admissionStage
	generation  attemptGeneration
	overlapped  bool
	disposition admissionDisposition
}

type imageAuthority struct {
	campaign campaignToken
	attempt  attemptIdentity
	class    admissionClass
	profile  Profile
	deadline time.Duration
}

// Capacity returns the admission capacity.
func (image Image) Capacity() int { return image.capacity }

// Open reports whether the runtime accepts new work.
func (image Image) Open() bool { return image.lifecycle == runtimeOpen }

// Closing reports whether the fatal epoch still requires settlement.
func (image Image) Closing() bool { return image.lifecycle == runtimeFatalClosing }

// Drained reports proven empty terminal custody.
func (image Image) Drained() bool { return image.lifecycle == runtimeClosedDrained }

// Unconfirmed reports terminal residual custody.
func (image Image) Unconfirmed() bool { return image.lifecycle == runtimeClosedUnconfirmed }

// SingleAdmission reports irreversible single-admission fallback.
func (image Image) SingleAdmission() bool { return image.mode == singleAdmission }

// FatalEpoch returns the current fatal epoch.
func (image Image) FatalEpoch() uint64 { return uint64(image.fatalEpoch) }

// FatalCauseCount returns the retained fatal-cause count.
func (image Image) FatalCauseCount() int { return len(image.fatalCauses) }

// CampaignCount returns the registered campaign count.
func (image Image) CampaignCount() int { return len(image.campaigns) }

// AdmissionCount returns the retained admission count.
func (image Image) AdmissionCount() int { return len(image.admissions) }

// Admission returns the immutable fact for one generation.
func (image Image) Admission(generation Generation) (Admission, bool) {
	index := image.admissionIndex(attemptGeneration(generation))
	if index < 0 {
		return Admission{}, false
	}
	authority := image.admissions[index].authority
	return Admission{
		Campaign: Campaign{token: authority.campaign}, Attempt: string(authority.attempt),
		Class: AdmissionClass(authority.class), Profile: authority.profile, Deadline: authority.deadline,
	}, true
}

// Owned reports runtime ownership for one generation.
func (image Image) Owned(generation Generation) bool {
	index := image.admissionIndex(attemptGeneration(generation))
	return index >= 0 && image.admissions[index].stage == admissionOwned
}

// Prospective reports committed start custody before owned publication.
func (image Image) Prospective(generation Generation) bool {
	index := image.admissionIndex(attemptGeneration(generation))
	return index >= 0 && image.admissions[index].stage == admissionProspective
}

// CustodyTransferred reports local residual-custody transfer for one generation.
func (image Image) CustodyTransferred(generation Generation) bool {
	index := image.admissionIndex(attemptGeneration(generation))
	return index >= 0 && image.admissions[index].disposition == dispositionCustodyTransferred
}

// HasOverlappedPair reports whether at least two retained admissions overlapped.
func (image Image) HasOverlappedPair() bool {
	count := 0
	for _, admission := range image.admissions {
		if admission.overlapped {
			count++
		}
	}
	return count >= 2
}

// ContainsAttempt reports whether an attempt remains admitted.
func (image Image) ContainsAttempt(attempt string) bool {
	for _, admission := range image.admissions {
		if admission.authority.attempt == attemptIdentity(attempt) {
			return true
		}
	}
	return false
}

// Residual returns unresolved execution-domain custody in runtime order.
func (image Image) Residual() []Residual {
	result := make([]Residual, 0, len(image.admissions))
	for _, admission := range image.admissions {
		if admission.stage != admissionProspective && admission.stage != admissionOwned {
			continue
		}
		result = append(result, Residual{
			Generation: Generation(admission.generation), Attempt: string(admission.authority.attempt),
			Stage: uint8(admission.stage), Transferred: admission.disposition == dispositionCustodyTransferred ||
				admission.disposition == dispositionCustodySettled,
		})
	}
	return result
}

func (image Image) admissionIndex(generation attemptGeneration) int {
	for index, admission := range image.admissions {
		if generation != 0 && admission.generation == generation {
			return index
		}
	}
	return -1
}

// Resolution records exact emergency custody for one generation.
type Resolution struct {
	Generation  Generation
	Transferred bool
}

// Residual records unresolved execution-domain custody.
type Residual struct {
	Generation  Generation
	Attempt     string
	Stage       uint8
	Transferred bool
}

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
	TerminalCommitted TerminalDecision = iota + 1
	TerminalForcedAborted
	TerminalRejectedUnknown
	TerminalRejectedOutstanding
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

// State is an opaque immutable process-runtime reducer state.
type State struct{ value processRuntime }

// NewState creates an empty process-runtime reducer state.
func NewState(capacity int) State { return State{value: newProcessRuntime(capacity)} }

// RegisterCampaign applies campaign registration to immutable state.
func (state State) RegisterCampaign(lineage Lineage) (State, Registration) {
	next, result := state.value.registerCampaign(campaignProvenance{lineage: campaignLineage(lineage)})
	return State{value: next}, Registration{value: result}
}

// RequestAdmission applies an admission request to immutable state.
func (state State) RequestAdmission(request Admission) (State, AdmissionResult) {
	next, result := state.value.requestAdmission(admissionAuthorityValue(request))
	return State{value: next}, AdmissionResult{value: result}
}

// BindConfirmationBarrier applies a confirmation barrier binding to immutable state.
func (state State) BindConfirmationBarrier(binding Barrier) (State, BarrierResult) {
	next, result := state.value.sealAndBindConfirmationBarrier(barrierBinding{
		campaign: binding.Campaign.token, attempt: attemptIdentity(binding.Attempt),
		profile: binding.Profile, deadline: binding.Deadline,
	})
	return State{value: next}, BarrierResult{value: result}
}

// BarrierResult is the inert result of confirmation barrier binding.
type BarrierResult struct{ value barrierResult }

// Decision returns the barrier decision.
func (result BarrierResult) Decision() BarrierDecision { return BarrierDecision(result.value.decision) }

// Request returns the bound confirmation admission.
func (result BarrierResult) Request() Admission { return admissionValue(result.value.request) }

// Deliveries returns grants released by the barrier transition.
func (result BarrierResult) Deliveries() []Admission { return admissionValues(result.value.deliveries) }

// CancelAdmission applies admission cancellation to immutable state.
func (state State) CancelAdmission(request Admission) (State, AdmissionResult) {
	next, result := state.value.cancelAdmission(admissionAuthorityValue(request))
	return State{value: next}, AdmissionResult{value: result}
}

// ReturnGrant applies a compensated grant acknowledgement to immutable state.
func (state State) ReturnGrant(request Admission) (State, AdmissionResult) {
	next, result := state.value.acknowledgeGrantReturn(admissionAuthorityValue(request))
	return State{value: next}, AdmissionResult{value: result}
}

// CommitStart applies start commitment to immutable state.
func (state State) CommitStart(grant Admission) (State, StartResult) {
	next, result := state.value.startCommitted(admissionAuthorityValue(grant))
	return State{value: next}, StartResult{value: result}
}

// Observe applies attempt evidence to immutable state.
func (state State) Observe(generation Generation, observation Observation) (State, Receipt) {
	next, result := state.value.observeAttempt(attemptGeneration(generation), observation.value)
	return State{value: next}, Receipt{value: result}
}

// CommitTerminal applies terminal campaign commitment to immutable state.
func (state State) CommitTerminal(campaign Campaign) (State, TerminalResult) {
	next, result := state.value.commitTerminal(campaign.token)
	return State{value: next}, TerminalResult{value: result}
}

// AuthorizeForcedAbort applies fatal-epoch terminal authorization to immutable state.
func (state State) AuthorizeForcedAbort(campaign Campaign, epoch uint64) (State, TerminalResult) {
	next, result := state.value.authorizeForcedAbort(campaign.token, fatalEpochID(epoch))
	return State{value: next}, TerminalResult{value: result}
}

// CompleteConfirmationQueue applies confirmation queue completion to immutable state.
func (state State) CompleteConfirmationQueue(campaign Campaign) (State, QueueResult) {
	next, result := state.value.completeConfirmationQueue(campaign.token)
	return State{value: next}, QueueResult{value: result}
}

// Close applies process-runtime fatal closure to immutable state.
func (state State) Close(cause string) (State, Closure) {
	next, result := state.value.closeRuntime(runtimeFatalCause(cause))
	return State{value: next}, Closure{value: result}
}

// SettleEmergency applies exact emergency custody settlement to immutable state.
func (state State) SettleEmergency(resolutions []Resolution) (State, EmergencySettlement) {
	values := make([]emergencyResolution, len(resolutions))
	for index, resolution := range resolutions {
		disposition := emergencyConfirmedDrained
		if resolution.Transferred {
			disposition = emergencyCustodyTransferred
		}
		values[index] = emergencyResolution{generation: attemptGeneration(resolution.Generation), disposition: disposition}
	}
	next, result := state.value.settleEmergency(emergencySweep{resolutions: values})
	return State{value: next}, EmergencySettlement{value: result}
}

// Image returns an opaque immutable state for deterministic conformance.
func (state State) Image() Image { return imageState(state.value) }

// Open reports whether the state accepts ordinary commands.
func (state State) Open() bool { return state.value.open() }

// Residual returns exact live execution-domain custody.
func (state State) Residual() []Residual { return residualValues(state.value.residualCustody()) }

// CanReturn reports whether an admission holds return authority.
func (state State) CanReturn(admission Admission) bool {
	index := state.value.admissionIndex(admissionAuthorityValue(admission))
	if index < 0 {
		return false
	}
	disposition := state.value.admissions[index].disposition
	return disposition == dispositionReturnedAfterGate || disposition == dispositionReturnedAfterClosure
}

// CanObserveOwnedTerminal reports whether the generation accepts owned terminal evidence.
func (state State) CanObserveOwnedTerminal(generation Generation) bool {
	index := state.value.admissionIndexByGeneration(attemptGeneration(generation))
	if index < 0 {
		return false
	}
	admission := state.value.admissions[index]
	return admission.stage == admissionOwned && state.value.lifecycle <= runtimeFatalClosing &&
		admission.disposition == dispositionNone
}

// CanTransferResidual reports whether the generation accepts unconfirmed-drain custody transfer.
func (state State) CanTransferResidual(generation Generation) bool {
	index := state.value.admissionIndexByGeneration(attemptGeneration(generation))
	if index < 0 {
		return false
	}
	admission := state.value.admissions[index]
	return admission.stage == admissionOwned && state.value.lifecycle <= runtimeFatalClosing &&
		(admission.disposition == dispositionNone || admission.disposition == dispositionFatalSeeded)
}

// CanObserveNotReleased reports whether the generation accepts proven pre-release evidence.
func (state State) CanObserveNotReleased(generation Generation) bool {
	index := state.value.admissionIndexByGeneration(attemptGeneration(generation))
	if index < 0 {
		return false
	}
	admission := state.value.admissions[index]
	return admission.stage == admissionProspective &&
		(admission.disposition == dispositionNone || admission.disposition == dispositionFatalSeeded) &&
		(state.value.lifecycle == runtimeOpen || state.value.lifecycle == runtimeFatalClosing)
}

// TerminalDeferred reports whether fatal closure retained terminal evidence for a generation.
func (state State) TerminalDeferred(generation Generation) bool {
	index := state.value.admissionIndexByGeneration(attemptGeneration(generation))
	return index >= 0 && state.value.admissions[index].disposition == dispositionTerminalDeferred
}

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

// Emergency returns the process-lifetime fatal epoch notification.
func (runtime *Runtime) Emergency() <-chan struct{} { return runtime.shell.runtimeEmergency() }

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
		if resolution.Transferred {
			disposition = emergencyCustodyTransferred
		}
		values[index] = emergencyResolution{
			generation: attemptGeneration(resolution.Generation), disposition: disposition,
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

// Image returns an opaque immutable synchronized runtime projection.
func (runtime *Runtime) Image() Image {
	runtime.shell.mutex.Lock()
	defer runtime.shell.mutex.Unlock()
	return imageState(runtime.shell.core)
}

// Residual returns unresolved execution-domain custody in runtime order.
func (runtime *Runtime) Residual() []Residual {
	runtime.shell.mutex.Lock()
	defer runtime.shell.mutex.Unlock()
	return residualValues(runtime.shell.core.residualCustody())
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
			Generation: Generation(value.generation), Attempt: string(value.attempt), Stage: uint8(value.stage),
			Transferred: value.transferred,
		}
	}
	return result
}

func imageState(state processRuntime) Image {
	image := Image{
		capacity: state.capacity, nextID: state.nextID, mode: state.mode, lifecycle: state.lifecycle,
		fatalCauses: slices.Clone(state.fatalCauses), fatalEpoch: state.fatalEpoch, fatalOwner: state.fatalOwner,
		campaigns: slices.Clone(state.campaigns), admissions: make([]imageAdmission, len(state.admissions)),
	}
	for index, admission := range state.admissions {
		image.admissions[index] = imageAdmission{
			authority: imageAuthority{
				campaign: admission.grant.campaign, attempt: admission.grant.attempt,
				class: admission.grant.class, profile: admission.grant.profile, deadline: admission.grant.deadline,
			},
			stage:      admission.stage,
			generation: admission.generation, overlapped: admission.overlapped, disposition: admission.disposition,
		}
	}
	return image
}
