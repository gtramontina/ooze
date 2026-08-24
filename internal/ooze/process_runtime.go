//nolint:cyclop,exhaustruct,lll // Flat bounded reducers and typed stage results are intentional.
package ooze

import (
	"slices"
	"strconv"
)

const (
	observeOperation         = "observe attempt"
	settleEmergencyOperation = "settle emergency"
)

type (
	campaignID        uint64
	campaignLineage   uint64
	attemptIdentity   string
	attemptGeneration uint64
	runtimeFatalCause string
)

type (
	campaignProvenance struct{ lineage campaignLineage }
	campaignToken      struct {
		id      campaignID
		lineage campaignLineage
	}
)

type campaignDecision uint8

const (
	campaignRegistered campaignDecision = iota + 1
	campaignRejectedRecursive
	campaignRejectedClosed
)

type campaignRegistration struct {
	decision campaignDecision
	token    campaignToken
}

type admissionClass uint8

const (
	sharedAdmission admissionClass = iota + 1
	exclusiveAdmission
	serialPrimaryAdmission
	confirmationAdmission
	confirmationBarrierAdmission
)

func (c admissionClass) primary() bool   { return c == sharedAdmission || c == serialPrimaryAdmission }
func (c admissionClass) exclusive() bool { return c != sharedAdmission }

type admissionStage uint8

const (
	admissionWaiting admissionStage = iota + 1
	admissionGranted
	admissionProspective
	admissionOwned
)

type admissionAuthority struct {
	campaign campaignToken
	attempt  attemptIdentity
	class    admissionClass
	delivery chan admissionAuthority
}

type (
	admissionRequest      = admissionAuthority
	admissionRequestToken = admissionAuthority
	admissionGrant        = admissionAuthority
)

type admissionDecision uint8

const (
	admissionAccepted admissionDecision = iota + 1
	admissionRejectedClosed
	admissionRejectedUnknownCampaign
	admissionRejectedGateClosed
	admissionRejectedGateOpen
	admissionRejectedDuplicate
	admissionRejectedExclusiveOutstanding
	admissionRejectedSharedLimit
	admissionRejectedAlreadyCommitted
	admissionCancelledWaiting
	admissionCancelledGranted
	admissionReturnedAfterClosure
	admissionReturnedAfterGateClosure
)

type admissionResult struct {
	decision   admissionDecision
	request    admissionRequestToken
	deliveries []admissionGrant
}

type registeredCampaign struct {
	token           campaignToken
	primaryGateOpen bool
}

type admittedAttempt struct {
	grant       admissionGrant
	stage       admissionStage
	generation  attemptGeneration
	overlapped  bool
	disposition admissionDisposition
}

func (a admittedAttempt) committed() bool {
	return a.stage == admissionProspective || a.stage == admissionOwned
}

type admissionDisposition uint8

const (
	dispositionNone admissionDisposition = iota
	dispositionReturnedAfterGate
	dispositionReturnedAfterClosure
	dispositionFatalSeeded
	dispositionTerminalDeferred
	dispositionCustodyTransferred
	dispositionCustodySettled
)

type admissionMode uint8

const (
	fullAutomatic admissionMode = iota + 1
	singleAdmission
)

type runtimeLifecycle uint8

const (
	runtimeOpen runtimeLifecycle = iota + 1
	runtimeFatalClosing
	runtimeFatalSettledClosing
	runtimeClosedDrained
	runtimeClosedUnconfirmed
)

type processRuntime struct {
	capacity    int
	nextID      uint64
	mode        admissionMode
	lifecycle   runtimeLifecycle
	fatalCauses []runtimeFatalCause
	campaigns   []registeredCampaign
	admissions  []admittedAttempt
}

type startCommittedDecision uint8

const (
	startCommittedAccepted startCommittedDecision = iota + 1
	startCommittedRejectedGrant
	startCommittedRejectedGate
	startCommittedRejectedClosed
)

type startCommittedResult struct {
	decision                                         startCommittedDecision
	generation                                       attemptGeneration
	settlementAcknowledged, runtimeClosureInProgress bool
}

type launchNotReleasedReason uint8

const (
	launchFailed launchNotReleasedReason = iota + 1
	launchResourceExhausted
)

type attemptObservation interface{ attemptObservation() }

type (
	launchOwned              struct{}
	launchNotReleased        struct{ reason launchNotReleasedReason }
	attemptSettled           struct{}
	launchUnconfirmed        struct{}
	drainUnconfirmed         struct{}
	attemptStopped           struct{}
	attemptInfrastructure    struct{ cause string }
	confirmationContinues    struct{ outcome confirmationOutcome }
	confirmationQueueDrained struct{ outcome confirmationOutcome }
)

type confirmationOutcome uint8

const (
	confirmationRejected confirmationOutcome = iota + 1
	confirmationPressureAccepted
)

type attemptTripKind uint8

const (
	deadlineTrip attemptTripKind = iota + 1
	fuseTrip
)

type attemptTripped struct{ kind attemptTripKind }

func (launchOwned) attemptObservation()              {}
func (launchNotReleased) attemptObservation()        {}
func (attemptSettled) attemptObservation()           {}
func (attemptTripped) attemptObservation()           {}
func (launchUnconfirmed) attemptObservation()        {}
func (drainUnconfirmed) attemptObservation()         {}
func (attemptStopped) attemptObservation()           {}
func (attemptInfrastructure) attemptObservation()    {}
func (confirmationContinues) attemptObservation()    {}
func (confirmationQueueDrained) attemptObservation() {}

type observationResult struct {
	generation                                      attemptGeneration
	deliveries                                      []admissionGrant
	cancelledWaiting, compensatedGrants             []admissionRequestToken
	settlementAcknowledged, confirmationProvisional bool
	pressureTransitioned, runtimeClosureInProgress  bool
}

type runtimeClosure struct {
	cancelledWaiting, compensatedGrants []admissionRequestToken
	residual                            []residualCustody
}

type residualCustody struct {
	generation  attemptGeneration
	stage       admissionStage
	transferred bool
}

type emergencyDisposition uint8

const (
	emergencyConfirmedDrained emergencyDisposition = iota + 1
	emergencyCustodyTransferred
)

type emergencyResolution struct {
	generation  attemptGeneration
	disposition emergencyDisposition
}

type emergencySweep struct{ resolutions []emergencyResolution }

type emergencySettlement struct {
	acknowledged []attemptGeneration
	residual     []residualCustody
}

type terminalDecision uint8

const (
	terminalCommitted terminalDecision = iota + 1
	terminalRejectedUnknown
	terminalRejectedOutstanding
	terminalRejectedClosed
)

type terminalResult struct{ decision terminalDecision }

type barrierBinding struct {
	campaign campaignToken
	attempt  attemptIdentity
	delivery chan admissionGrant
}

type barrierDecision uint8

const (
	barrierBound barrierDecision = iota + 1
	barrierRejectedMissing
	barrierRejectedClosureOutstanding
)

type barrierResult struct {
	decision   barrierDecision
	request    admissionRequestToken
	deliveries []admissionGrant
}

type runtimeInvariantViolation struct{ operation, reason string }

func newProcessRuntime(capacity int) processRuntime {
	if capacity <= 0 {
		invariant("construct", "capacity must be positive")
	}

	return processRuntime{capacity: capacity, mode: fullAutomatic, lifecycle: runtimeOpen}
}

func (r processRuntime) registerCampaign(provenance campaignProvenance) (processRuntime, campaignRegistration) {
	if !r.open() {
		return r, campaignRegistration{decision: campaignRejectedClosed}
	}
	if provenance.lineage == 0 {
		invariant("register campaign", "lineage is zero")
	}
	for _, campaign := range r.campaigns {
		if campaign.token.lineage == provenance.lineage {
			return r, campaignRegistration{decision: campaignRejectedRecursive}
		}
	}
	next := r.clone()
	next.nextID++
	token := campaignToken{id: campaignID(next.nextID), lineage: provenance.lineage}
	next.campaigns = append(next.campaigns, registeredCampaign{token: token, primaryGateOpen: true})

	return next, campaignRegistration{decision: campaignRegistered, token: token}
}

func (r processRuntime) requestAdmission(request admissionRequest) (processRuntime, admissionResult) {
	token := request
	if request.attempt == "" || request.class < sharedAdmission || request.class > confirmationAdmission {
		invariant("request admission", "invalid request")
	}
	if !r.open() {
		return r, admissionResult{decision: admissionRejectedClosed, request: token}
	}
	campaignAt := r.campaignIndex(request.campaign)
	if campaignAt < 0 {
		return r, admissionResult{decision: admissionRejectedUnknownCampaign, request: token}
	}
	if request.class == confirmationAdmission && r.campaigns[campaignAt].primaryGateOpen {
		return r, admissionResult{decision: admissionRejectedGateOpen, request: token}
	}
	if request.class.primary() && !r.campaigns[campaignAt].primaryGateOpen {
		return r, admissionResult{decision: admissionRejectedGateClosed, request: token}
	}
	shared := 0
	for _, admission := range r.admissions {
		if admission.grant.campaign == token.campaign && admission.grant.attempt == token.attempt {
			return r, admissionResult{decision: admissionRejectedDuplicate, request: token}
		}
		if request.class.exclusive() && admission.grant.class.exclusive() && admission.grant.campaign == request.campaign {
			return r, admissionResult{decision: admissionRejectedExclusiveOutstanding, request: token}
		}
		if admission.grant.campaign == request.campaign && admission.grant.class == sharedAdmission {
			shared++
		}
	}
	if request.class == sharedAdmission && shared >= r.capacity {
		return r, admissionResult{decision: admissionRejectedSharedLimit, request: token}
	}
	next := r.clone()
	next.admissions = append(next.admissions, admittedAttempt{
		grant: token, stage: admissionWaiting,
	})
	next, deliveries := next.grantAvailable()

	return next, admissionResult{decision: admissionAccepted, request: token, deliveries: deliveries}
}

func (r processRuntime) cancelAdmission(token admissionRequestToken) (processRuntime, admissionResult) {
	index := r.admissionIndex(token)
	if index >= 0 && r.admissions[index].committed() {
		return r, admissionResult{decision: admissionRejectedAlreadyCommitted, request: token}
	}
	if index >= 0 && r.admissions[index].disposition != dispositionNone {
		return r, admissionResult{decision: admissionRejectedAlreadyCommitted, request: token}
	}
	if !r.open() {
		return r, admissionResult{decision: admissionRejectedClosed, request: token}
	}
	if index < 0 || r.admissions[index].stage > admissionGranted {
		return r, admissionResult{decision: admissionRejectedDuplicate, request: token}
	}
	next := r.clone()
	decision := admissionCancelledWaiting
	if next.admissions[index].stage == admissionGranted {
		decision = admissionCancelledGranted
	}
	next.admissions = slices.Delete(next.admissions, index, index+1)
	next, deliveries := next.grantAvailable()

	return next, admissionResult{decision: decision, request: token, deliveries: deliveries}
}

func (r processRuntime) acknowledgeGrantReturn(grant admissionGrant) (processRuntime, admissionResult) {
	index := r.admissionIndex(grant)
	if index < 0 || (r.admissions[index].disposition != dispositionReturnedAfterGate &&
		r.admissions[index].disposition != dispositionReturnedAfterClosure) {
		invariant("acknowledge grant return", "grant return authority is stale or wrong")
	}
	next := r.clone()
	decision := admissionReturnedAfterGateClosure
	if next.admissions[index].disposition == dispositionReturnedAfterClosure {
		decision = admissionReturnedAfterClosure
	}
	next.admissions = slices.Delete(next.admissions, index, index+1)
	next = next.finalizeFatalClosure()

	return next, admissionResult{decision: decision, request: grant}
}

func (r processRuntime) startCommitted(grant admissionGrant) (processRuntime, startCommittedResult) {
	if !r.open() {
		return r, startCommittedResult{decision: startCommittedRejectedClosed, runtimeClosureInProgress: true}
	}
	index := r.admissionIndex(grant)
	if index < 0 || r.admissions[index].stage != admissionGranted || r.admissions[index].grant != grant ||
		r.admissions[index].disposition != dispositionNone {
		return r, startCommittedResult{decision: startCommittedRejectedGrant}
	}
	campaignAt := r.campaignIndex(grant.campaign)
	if campaignAt < 0 {
		invariant("start committed", "grant campaign disappeared")
	}
	if grant.class.primary() && !r.campaigns[campaignAt].primaryGateOpen {
		return r, startCommittedResult{decision: startCommittedRejectedGate}
	}
	next := r.clone()
	next.nextID++
	next.admissions[index].stage = admissionProspective
	next.admissions[index].generation = attemptGeneration(next.nextID)
	for other := range next.admissions {
		if other != index && next.admissions[other].committed() {
			next.admissions[index].overlapped = true
			next.admissions[other].overlapped = true
		}
	}

	return next, startCommittedResult{decision: startCommittedAccepted, generation: next.admissions[index].generation}
}

func (r processRuntime) observeAttempt(generation attemptGeneration, observation attemptObservation) (processRuntime, observationResult) {
	if observation == nil {
		invariant(observeOperation, "observation is nil")
	}
	index := r.admissionIndexByGeneration(generation)
	if index < 0 {
		invariant(observeOperation, "generation is not live")
	}
	next := r.clone()
	switch observed := observation.(type) {
	case launchOwned:
		return next.observeOwned(generation, index)
	case launchNotReleased:
		return next.observeNotReleased(generation, index, observed)
	case attemptSettled:
		return next.observeOwnedTerminal(generation, index, observed)
	case attemptTripped:
		return next.observeOwnedTerminal(generation, index, observed)
	case attemptStopped:
		return next.observeOwnedTerminal(generation, index, observed)
	case attemptInfrastructure:
		return next.observeOwnedTerminal(generation, index, observed)
	case launchUnconfirmed:
		return next.observeLaunchUnconfirmed(generation, index)
	case drainUnconfirmed:
		return next.observeDrainUnconfirmed(generation, index)
	case confirmationContinues:
		return next.observeConfirmation(generation, index, observed.outcome, false)
	case confirmationQueueDrained:
		return next.observeConfirmation(generation, index, observed.outcome, true)
	default:
		invariant(observeOperation, "unknown observation")
	}

	return processRuntime{}, observationResult{}
}

func (r processRuntime) observeOwnedTerminal(
	generation attemptGeneration,
	index int,
	observation attemptObservation,
) (processRuntime, observationResult) {
	var tripKind attemptTripKind
	switch observed := observation.(type) {
	case attemptSettled, attemptStopped:
	case attemptTripped:
		if observed.kind != deadlineTrip && observed.kind != fuseTrip {
			invariant(observeOperation, "trip kind is invalid")
		}
		tripKind = observed.kind
	case attemptInfrastructure:
		if observed.cause == "" {
			invariant(observeOperation, "infrastructure cause is empty")
		}
	default:
		invariant(observeOperation, "owned terminal observation is invalid")
	}
	admission := r.admissions[index]
	if admission.stage != admissionOwned || admission.disposition != dispositionNone ||
		(r.lifecycle != runtimeOpen && r.lifecycle != runtimeFatalClosing) {
		invariant(observeOperation, "terminal is not a live undecided owned attempt")
	}
	if r.lifecycle == runtimeFatalClosing {
		r.admissions[index].disposition = dispositionTerminalDeferred

		return r, observationResult{generation: generation, runtimeClosureInProgress: true}
	}
	if tripKind != 0 {
		return r.observeTrip(generation, index, attemptTripped{kind: tripKind})
	}

	return r.releaseOwned(generation, index)
}

func (r processRuntime) observeOwned(generation attemptGeneration, index int) (processRuntime, observationResult) {
	if r.admissions[index].stage != admissionProspective || r.lifecycle > runtimeFatalClosing {
		invariant(observeOperation, "owned launch is not prospective")
	}
	r.admissions[index].stage = admissionOwned

	return r, observationResult{generation: generation, runtimeClosureInProgress: !r.open()}
}

func (r processRuntime) observeNotReleased(generation attemptGeneration, index int, observed launchNotReleased) (processRuntime, observationResult) {
	disposition := r.admissions[index].disposition
	if r.admissions[index].stage != admissionProspective || r.lifecycle > runtimeFatalClosing ||
		(disposition != dispositionNone && disposition != dispositionFatalSeeded) {
		invariant(observeOperation, "no-release launch is not prospective")
	}
	if observed.reason != launchFailed && observed.reason != launchResourceExhausted {
		invariant(observeOperation, "no-release reason is invalid")
	}
	pressure := observed.reason == launchResourceExhausted && r.admissions[index].grant.class == sharedAdmission &&
		r.mode == fullAutomatic
	if pressure {
		r.mode = singleAdmission
	}
	r.admissions = slices.Delete(r.admissions, index, index+1)
	if !r.open() {
		r = r.finalizeFatalClosure()
	}
	result := observationResult{generation: generation, settlementAcknowledged: true, runtimeClosureInProgress: !r.open()}
	result.pressureTransitioned = pressure
	if r.open() {
		r, result.deliveries = r.grantAvailable()
	}

	return r, result
}

func (r processRuntime) releaseOwned(generation attemptGeneration, index int) (processRuntime, observationResult) {
	if !r.open() || r.admissions[index].stage != admissionOwned {
		invariant(observeOperation, "terminal is not a live owned attempt")
	}
	r.admissions = slices.Delete(r.admissions, index, index+1)
	r, deliveries := r.grantAvailable()
	result := observationResult{generation: generation, settlementAcknowledged: true}
	result.deliveries = deliveries

	return r, result
}

func (r processRuntime) observeTrip(generation attemptGeneration, index int, observed attemptTripped) (processRuntime, observationResult) {
	if !r.open() || r.admissions[index].stage != admissionOwned {
		invariant(observeOperation, "trip is not a live owned attempt")
	}
	if observed.kind != deadlineTrip && observed.kind != fuseTrip {
		invariant(observeOperation, "trip kind is invalid")
	}
	tripped := r.admissions[index]
	provisional := observed.kind == deadlineTrip && tripped.grant.class.primary() && tripped.overlapped
	result := observationResult{generation: generation, settlementAcknowledged: true}
	if provisional {
		result.confirmationProvisional = true
		if r.unboundBarrierIndex(tripped.grant.campaign) < 0 {
			r, result.cancelledWaiting, result.compensatedGrants = r.installBarrier(tripped.grant.campaign)
		}
		index = r.admissionIndexByGeneration(generation)
	}
	r.admissions = slices.Delete(r.admissions, index, index+1)
	r, result.deliveries = r.grantAvailable()

	return r, result
}

func (r processRuntime) installBarrier(campaign campaignToken) (processRuntime, []admissionRequestToken, []admissionRequestToken) {
	campaignAt := r.campaignIndex(campaign)
	if campaignAt < 0 {
		invariant("install confirmation barrier", "campaign disappeared")
	}
	r.campaigns[campaignAt].primaryGateOpen = false
	cancelled := make([]admissionRequestToken, 0)
	compensated := make([]admissionRequestToken, 0)
	kept := make([]admittedAttempt, 0, len(r.admissions)+1)
	for _, admission := range r.admissions {
		if admission.grant.campaign != campaign || !admission.grant.class.primary() ||
			admission.committed() || admission.disposition != dispositionNone {
			kept = append(kept, admission)

			continue
		}
		if admission.stage == admissionGranted {
			compensated = append(compensated, admission.grant)
			admission.disposition = dispositionReturnedAfterGate
			kept = append(kept, admission)
		} else {
			cancelled = append(cancelled, admission.grant)
		}
	}
	r.admissions = append(kept, admittedAttempt{ //nolint:gocritic // Replaces the filtered source slice.
		grant: admissionAuthority{campaign: campaign, class: confirmationBarrierAdmission},
		stage: admissionWaiting,
	})

	return r, cancelled, compensated
}

func (r processRuntime) observeLaunchUnconfirmed(generation attemptGeneration, index int) (processRuntime, observationResult) {
	if r.admissions[index].stage != admissionProspective || r.admissions[index].disposition != dispositionNone {
		invariant(observeOperation, "unconfirmed launch is not prospective")
	}
	var closure runtimeClosure
	r, closure = r.closeRuntime(attemptFatalCause("launch unconfirmed", generation))
	index = r.admissionIndexByGeneration(generation)
	r.admissions[index].disposition = dispositionFatalSeeded
	result := observationResult{generation: generation, runtimeClosureInProgress: true}
	result.cancelledWaiting = closure.cancelledWaiting
	result.compensatedGrants = closure.compensatedGrants

	return r, result
}

func (r processRuntime) observeDrainUnconfirmed(generation attemptGeneration, index int) (processRuntime, observationResult) {
	if r.admissions[index].stage != admissionOwned || r.lifecycle > runtimeFatalClosing ||
		(r.admissions[index].disposition != dispositionNone &&
			r.admissions[index].disposition != dispositionFatalSeeded) {
		invariant(observeOperation, "unconfirmed drain is not owned")
	}
	var closure runtimeClosure
	r, closure = r.closeRuntime(attemptFatalCause("drain unconfirmed", generation))
	index = r.admissionIndexByGeneration(generation)
	r.admissions[index].disposition = dispositionCustodyTransferred
	result := observationResult{generation: generation, runtimeClosureInProgress: true}
	result.cancelledWaiting = closure.cancelledWaiting
	result.compensatedGrants = closure.compensatedGrants

	return r, result
}

func attemptFatalCause(kind string, generation attemptGeneration) runtimeFatalCause {
	if kind == "" || generation == 0 {
		invariant(observeOperation, "attempt fatal cause is uncorrelated")
	}

	return runtimeFatalCause(kind + " generation=" + strconv.FormatUint(uint64(generation), 10))
}

func (r processRuntime) observeConfirmation(
	generation attemptGeneration,
	index int,
	outcome confirmationOutcome,
	queueDrained bool,
) (processRuntime, observationResult) {
	admission := r.admissions[index]
	validClass := admission.grant.class == confirmationAdmission || admission.grant.class == confirmationBarrierAdmission
	if !r.open() || admission.stage != admissionOwned || !validClass ||
		(outcome != confirmationRejected && outcome != confirmationPressureAccepted) {
		invariant(observeOperation, "confirmation completion is invalid")
	}
	campaignAt := r.campaignIndex(admission.grant.campaign)
	if campaignAt < 0 || r.campaigns[campaignAt].primaryGateOpen {
		invariant(observeOperation, "confirmation campaign is not provisional")
	}
	transitioned := outcome == confirmationPressureAccepted && r.mode == fullAutomatic
	if transitioned {
		r.mode = singleAdmission
	}
	r.admissions = slices.Delete(r.admissions, index, index+1)
	if queueDrained {
		r.campaigns[campaignAt].primaryGateOpen = true
	}
	r, deliveries := r.grantAvailable()
	result := observationResult{generation: generation, settlementAcknowledged: true}
	result.deliveries = deliveries
	result.pressureTransitioned = transitioned

	return r, result
}

func (r processRuntime) sealAndBindConfirmationBarrier(binding barrierBinding) (processRuntime, barrierResult) {
	index := r.unboundBarrierIndex(binding.campaign)
	if !r.open() || binding.attempt == "" || index < 0 {
		return r, barrierResult{decision: barrierRejectedMissing}
	}
	for _, admission := range r.admissions {
		if admission.grant.campaign == binding.campaign && admission.committed() {
			return r, barrierResult{decision: barrierRejectedClosureOutstanding}
		}
	}
	next := r.clone()
	request := admissionAuthority{
		campaign: binding.campaign, attempt: binding.attempt, class: confirmationBarrierAdmission,
		delivery: binding.delivery,
	}
	next.admissions[index].grant = request
	next, deliveries := next.grantAvailable()

	return next, barrierResult{decision: barrierBound, request: request, deliveries: deliveries}
}

func (r processRuntime) closeRuntime(cause runtimeFatalCause) (processRuntime, runtimeClosure) {
	if cause == "" {
		invariant("close runtime", "fatal cause is empty")
	}
	if r.lifecycle == runtimeClosedDrained || r.lifecycle == runtimeClosedUnconfirmed {
		return r, runtimeClosure{residual: r.residualCustody()}
	}
	next := r.clone()
	if next.open() {
		next.lifecycle = runtimeFatalClosing
	}
	next.fatalCauses = append(next.fatalCauses, cause)
	closure := runtimeClosure{}
	if !r.open() {
		closure.residual = next.residualCustody()

		return next, closure
	}
	kept := make([]admittedAttempt, 0, len(next.admissions))
	for _, admission := range next.admissions {
		switch admission.stage {
		case admissionWaiting:
			if admission.grant.class != confirmationBarrierAdmission || admission.grant.attempt != "" {
				closure.cancelledWaiting = append(closure.cancelledWaiting, admission.grant)
			}
		case admissionGranted:
			if admission.disposition == dispositionNone {
				closure.compensatedGrants = append(closure.compensatedGrants, admission.grant)
				admission.disposition = dispositionReturnedAfterClosure
			}
			kept = append(kept, admission)
		case admissionProspective, admissionOwned:
			kept = append(kept, admission)
		}
	}
	next.admissions = kept
	closure.residual = next.residualCustody()

	return next, closure
}

func (r processRuntime) settleEmergency(sweep emergencySweep) (processRuntime, emergencySettlement) {
	if r.lifecycle != runtimeFatalClosing || len(sweep.resolutions) != len(r.residualCustody()) {
		invariant(settleEmergencyOperation, "resolution cardinality is invalid")
	}
	next := r.clone()
	acknowledged := make([]attemptGeneration, 0, len(sweep.resolutions))
	for _, resolution := range sweep.resolutions {
		if slices.Contains(acknowledged, resolution.generation) {
			invariant(settleEmergencyOperation, "generation is duplicated")
		}
		index := next.admissionIndexByGeneration(resolution.generation)
		if index < 0 || (resolution.disposition != emergencyConfirmedDrained &&
			resolution.disposition != emergencyCustodyTransferred) {
			invariant(settleEmergencyOperation, "resolution is invalid")
		}
		if next.admissions[index].disposition == dispositionCustodySettled {
			invariant(settleEmergencyOperation, "generation was already settled")
		}
		if next.admissions[index].disposition == dispositionTerminalDeferred &&
			resolution.disposition != emergencyConfirmedDrained {
			invariant(settleEmergencyOperation, "deferred terminal cannot transfer custody")
		}
		if next.admissions[index].disposition == dispositionCustodyTransferred &&
			resolution.disposition != emergencyCustodyTransferred {
			invariant(settleEmergencyOperation, "transferred custody cannot be drained")
		}
		acknowledged = append(acknowledged, resolution.generation)
		if resolution.disposition == emergencyConfirmedDrained {
			next.admissions = slices.Delete(next.admissions, index, index+1)
		} else {
			next.admissions[index].disposition = dispositionCustodySettled
		}
	}
	next.lifecycle = runtimeFatalSettledClosing
	next = next.finalizeFatalClosure()

	return next, emergencySettlement{acknowledged: acknowledged, residual: next.residualCustody()}
}

func (r processRuntime) finalizeFatalClosure() processRuntime {
	if r.lifecycle != runtimeFatalSettledClosing {
		return r
	}
	for _, admission := range r.admissions {
		if admission.stage == admissionGranted ||
			(admission.committed() && admission.disposition != dispositionCustodySettled) {
			return r
		}
	}
	next := r.clone()
	next.lifecycle = runtimeClosedDrained
	if len(next.residualCustody()) != 0 {
		next.lifecycle = runtimeClosedUnconfirmed
	}

	return next
}

func (r processRuntime) commitTerminal(campaign campaignToken) (processRuntime, terminalResult) {
	if !r.open() {
		return r, terminalResult{decision: terminalRejectedClosed}
	}
	index := r.campaignIndex(campaign)
	if index < 0 {
		return r, terminalResult{decision: terminalRejectedUnknown}
	}
	for _, admission := range r.admissions {
		if admission.grant.campaign == campaign {
			return r, terminalResult{decision: terminalRejectedOutstanding}
		}
	}
	next := r.clone()
	next.campaigns = slices.Delete(next.campaigns, index, index+1)

	return next, terminalResult{decision: terminalCommitted}
}

func (r processRuntime) grantAvailable() (processRuntime, []admissionGrant) {
	active := 0
	exclusive := false
	for _, admission := range r.admissions {
		if admission.stage != admissionWaiting && admission.disposition == dispositionNone {
			active++
			exclusive = exclusive || admission.grant.class.exclusive()
		}
	}
	if exclusive {
		return r, nil
	}
	limit := r.capacity
	if r.mode == singleAdmission {
		limit = 1
	}
	var deliveries []admissionGrant
	for index := range r.admissions {
		admission := &r.admissions[index]
		if admission.stage != admissionWaiting || admission.disposition != dispositionNone {
			continue
		}
		if admission.grant.class.exclusive() {
			if admission.grant.class == confirmationBarrierAdmission && admission.grant.attempt == "" || active != 0 {
				return r, deliveries
			}
			admission.stage = admissionGranted

			return r, append(deliveries, admission.grant)
		}
		if active >= limit {
			return r, deliveries
		}
		admission.stage = admissionGranted
		active++
		deliveries = append(deliveries, admission.grant)
	}

	return r, deliveries
}

func (r processRuntime) clone() processRuntime {
	r.fatalCauses = slices.Clone(r.fatalCauses)
	r.campaigns = slices.Clone(r.campaigns)
	r.admissions = slices.Clone(r.admissions)

	return r
}

func (r processRuntime) open() bool { return r.lifecycle == runtimeOpen }

func (r processRuntime) campaignIndex(token campaignToken) int {
	return slices.IndexFunc(r.campaigns, func(campaign registeredCampaign) bool { return campaign.token == token })
}

func (r processRuntime) admissionIndex(token admissionRequestToken) int {
	return slices.IndexFunc(r.admissions, func(admission admittedAttempt) bool { return admission.grant == token })
}

func (r processRuntime) admissionIndexByGeneration(generation attemptGeneration) int {
	return slices.IndexFunc(r.admissions, func(admission admittedAttempt) bool {
		return generation != 0 && admission.generation == generation
	})
}

func (r processRuntime) unboundBarrierIndex(campaign campaignToken) int {
	return slices.IndexFunc(r.admissions, func(admission admittedAttempt) bool {
		return admission.grant.campaign == campaign && admission.grant.class == confirmationBarrierAdmission &&
			admission.grant.attempt == ""
	})
}

func (r processRuntime) residualCustody() []residualCustody {
	residual := make([]residualCustody, 0, len(r.admissions))
	for _, admission := range r.admissions {
		if admission.committed() {
			residual = append(residual, residualCustody{
				generation: admission.generation, stage: admission.stage,
				transferred: admission.disposition == dispositionCustodyTransferred ||
					admission.disposition == dispositionCustodySettled,
			})
		}
	}

	return residual
}

func invariant(operation, reason string) {
	panic(runtimeInvariantViolation{operation: operation, reason: reason})
}
