package ooze

import (
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
)

type (
	campaignLineage   = processruntime.Lineage
	attemptIdentity   = supervision.Identity
	attemptGeneration = processruntime.Generation
	Profile           = processruntime.Profile
	fatalEpochID      uint64
	runtimeFatalCause string
	campaignToken     = processruntime.Campaign
)

const (
	AutomaticProfile = processruntime.AutomaticProfile
	SerialProfile    = processruntime.SerialProfile
)

type campaignRegistration struct {
	decision processruntime.CampaignDecision
	token    campaignToken
}

type admissionAuthority struct {
	campaign campaignToken
	attempt  attemptIdentity
	class    processruntime.AdmissionClass
	profile  Profile
	deadline time.Duration
	grant    processruntime.Grant
}

type admissionRequest = admissionAuthority
type admissionRequestToken = admissionAuthority
type admissionGrant = admissionAuthority

type admissionResult struct {
	decision   processruntime.AdmissionDecision
	request    admissionRequestToken
	deliveries []admissionGrant
	fatalEpoch fatalEpochID
}

type startCommittedResult struct {
	decision                                         processruntime.StartDecision
	generation                                       attemptGeneration
	settlementAcknowledged, runtimeClosureInProgress bool
}

type terminalResult struct {
	decision processruntime.TerminalDecision
	epoch    fatalEpochID
}

type barrierBinding struct {
	campaign campaignToken
	attempt  attemptIdentity
	profile  Profile
	deadline time.Duration
}

type barrierResult struct {
	decision   processruntime.BarrierDecision
	request    admissionRequestToken
	deliveries []admissionGrant
}

type confirmationQueueResult struct {
	decision   processruntime.QueueDecision
	deliveries []admissionGrant
}

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
