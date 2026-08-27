package ooze

import (
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
)

const (
	observeOperation         = "observe attempt"
	settleEmergencyOperation = "settle emergency"
)

type (
	campaignLineage   = processruntime.Lineage
	attemptIdentity   string
	attemptGeneration = processruntime.Generation
	fatalEpochID      uint64
	runtimeFatalCause string
	campaignToken     = processruntime.Campaign
)

type processRuntime = processruntime.State

type processRuntimeOperation uint8

const (
	processRuntimeRegisterCampaign processRuntimeOperation = iota + 1
	processRuntimeRequestAdmission
	processRuntimeCancelAdmission
	processRuntimeAcknowledgeGrantReturn
	processRuntimeBindConfirmationBarrier
	processRuntimeCompleteConfirmationQueue
	processRuntimeStartCommitted
	processRuntimeObserveAttempt
	processRuntimeSettleEmergency
	processRuntimeCommitTerminal
	processRuntimeAuthorizeForcedAbort
	processRuntimeClose
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

type registeredCampaign struct {
	token           campaignToken
	primaryGateOpen bool
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

type admissionClass = processruntime.AdmissionClass

const (
	sharedAdmission                             = processruntime.SharedAdmission
	exclusiveAdmission                          = processruntime.ExclusiveAdmission
	serialPrimaryAdmission                      = processruntime.SerialPrimaryAdmission
	confirmationAdmission                       = processruntime.ConfirmationAdmission
	confirmationBarrierAdmission admissionClass = 5
)

type campaignProvenance struct{ lineage campaignLineage }

type campaignDecision = processruntime.CampaignDecision

const (
	campaignRegistered        = processruntime.CampaignRegistered
	campaignRejectedRecursive = processruntime.CampaignRejectedRecursive
	campaignRejectedClosed    = processruntime.CampaignRejectedClosed
)

type campaignRegistration struct {
	decision campaignDecision
	token    campaignToken
}

type admissionAuthority struct {
	campaign campaignToken
	attempt  attemptIdentity
	class    admissionClass
	profile  Profile
	deadline time.Duration
	grant    processruntime.Grant
}

type admissionRequest = admissionAuthority
type admissionRequestToken = admissionAuthority
type admissionGrant = admissionAuthority

type admissionResult struct {
	decision   admissionDecision
	request    admissionRequestToken
	deliveries []admissionGrant
	fatalEpoch fatalEpochID
}

type admissionAwait struct {
	decision admissionDecision
	request  admissionRequestToken
	delivery <-chan admissionGrant
	fatal    fatalEpochID
}

type startCommittedResult struct {
	decision                                         startCommittedDecision
	generation                                       attemptGeneration
	settlementAcknowledged, runtimeClosureInProgress bool
}

type terminalResult struct {
	decision terminalDecision
	epoch    fatalEpochID
}

type barrierBinding struct {
	campaign campaignToken
	attempt  attemptIdentity
	profile  Profile
	deadline time.Duration
}

type barrierResult struct {
	decision   barrierDecision
	request    admissionRequestToken
	deliveries []admissionGrant
}

type barrierAwait struct {
	decision barrierDecision
	request  admissionRequestToken
	delivery <-chan admissionGrant
}

type confirmationQueueResult struct {
	decision   confirmationQueueDecision
	deliveries []admissionGrant
}

type admissionDecision = processruntime.AdmissionDecision

const (
	admissionAccepted                     = processruntime.AdmissionAccepted
	admissionRejectedClosed               = processruntime.AdmissionRejectedClosed
	admissionRejectedUnknownCampaign      = processruntime.AdmissionRejectedUnknownCampaign
	admissionRejectedGateClosed           = processruntime.AdmissionRejectedGateClosed
	admissionRejectedGateOpen             = processruntime.AdmissionRejectedGateOpen
	admissionRejectedDuplicate            = processruntime.AdmissionRejectedDuplicate
	admissionRejectedExclusiveOutstanding = processruntime.AdmissionRejectedExclusiveOutstanding
	admissionRejectedSharedLimit          = processruntime.AdmissionRejectedSharedLimit
	admissionRejectedAlreadyCommitted     = processruntime.AdmissionRejectedAlreadyCommitted
	admissionCancelledWaiting             = processruntime.AdmissionCancelledWaiting
	admissionCancelledGranted             = processruntime.AdmissionCancelledGranted
	admissionReturnedAfterClosure         = processruntime.AdmissionReturnedAfterClosure
	admissionReturnedAfterGateClosure     = processruntime.AdmissionReturnedAfterGateClosure
)

type startCommittedDecision = processruntime.StartDecision

const (
	startCommittedAccepted       = processruntime.StartAccepted
	startCommittedRejectedGrant  = processruntime.StartRejectedGrant
	startCommittedRejectedGate   = processruntime.StartRejectedGate
	startCommittedRejectedClosed = processruntime.StartRejectedClosed
)

type terminalDecision = processruntime.TerminalDecision

const (
	terminalCommitted           = processruntime.TerminalCommitted
	terminalForcedAborted       = processruntime.TerminalForcedAborted
	terminalRejectedUnknown     = processruntime.TerminalRejectedUnknown
	terminalRejectedOutstanding = processruntime.TerminalRejectedOutstanding
	terminalRejectedClosed      = processruntime.TerminalRejectedClosed
)

type barrierDecision = processruntime.BarrierDecision

const (
	barrierBound                      = processruntime.BarrierBound
	barrierRejectedMissing            = processruntime.BarrierRejectedMissing
	barrierRejectedClosureOutstanding = processruntime.BarrierRejectedClosureOutstanding
	barrierRejectedExecutionMismatch  = processruntime.BarrierRejectedExecutionMismatch
)

type confirmationQueueDecision = processruntime.QueueDecision

const (
	confirmationQueueCompleted           = processruntime.ConfirmationQueueCompleted
	confirmationQueueRejectedMissing     = processruntime.ConfirmationQueueRejectedMissing
	confirmationQueueRejectedOutstanding = processruntime.ConfirmationQueueRejectedOutstanding
)

type launchNotReleasedReason uint8

const (
	launchFailed launchNotReleasedReason = iota + 1
	launchResourceExhausted
)

type attemptTripKind uint8

const (
	deadlineTrip attemptTripKind = iota + 1
	fuseTrip
)

type attemptObservation interface{ attemptObservation() }

type (
	launchOwned       struct{}
	launchNotReleased struct{ reason launchNotReleasedReason }
	attemptSettled    struct {
		profile  Profile
		deadline time.Duration
	}
	attemptTripped struct {
		kind     attemptTripKind
		profile  Profile
		deadline time.Duration
	}
	launchUnconfirmed     struct{}
	drainUnconfirmed      struct{}
	attemptStopped        struct{}
	attemptInfrastructure struct{ cause string }
)

func (launchOwned) attemptObservation()           {}
func (launchNotReleased) attemptObservation()     {}
func (attemptSettled) attemptObservation()        {}
func (attemptTripped) attemptObservation()        {}
func (launchUnconfirmed) attemptObservation()     {}
func (drainUnconfirmed) attemptObservation()      {}
func (attemptStopped) attemptObservation()        {}
func (attemptInfrastructure) attemptObservation() {}

type admissionStage uint8

const (
	admissionWaiting admissionStage = iota + 1
	admissionGranted
	admissionProspective
	admissionOwned
)

type residualCustody struct {
	generation  attemptGeneration
	attempt     attemptIdentity
	stage       admissionStage
	transferred bool
}

type observationResult struct {
	generation                                      attemptGeneration
	deliveries                                      []admissionAuthority
	cancelledWaiting, compensatedGrants             []admissionAuthority
	settlementAcknowledged, confirmationProvisional bool
	pressureTransitioned, runtimeClosureInProgress  bool
	confirmationObserved, confirmationQueueDrained  bool
	fatalEpoch                                      fatalEpochID
}

type runtimeClosure struct {
	epoch                               fatalEpochID
	cancelledWaiting, compensatedGrants []admissionAuthority
	residual                            []residualCustody
}

type emergencyResolution struct {
	generation  attemptGeneration
	disposition emergencyDisposition
}

type emergencyDisposition uint8

const (
	emergencyConfirmedDrained emergencyDisposition = iota + 1
	emergencyCustodyTransferred
)

type emergencySweep struct{ resolutions []emergencyResolution }

type emergencySettlement struct {
	epoch        fatalEpochID
	owner        campaignToken
	acknowledged []attemptGeneration
	residual     []residualCustody
}

type runtimeInvariantViolation struct {
	operation, reason                               string
	phase                                           uint8
	rejectedEvent                                   string
	stableIdentities, obligationSnapshot, traceTail []string
}

func invariant(operation, reason string) {
	panic(runtimeInvariantViolation{operation: operation, reason: reason})
}
