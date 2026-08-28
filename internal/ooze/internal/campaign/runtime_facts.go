package campaign

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
}

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

type barrierResult struct {
	decision   processruntime.BarrierDecision
	request    admissionRequestToken
	deliveries []admissionGrant
}

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

type runtimeClosure struct {
	epoch                               fatalEpochID
	cancelledWaiting, compensatedGrants []admissionAuthority
	residual                            []residualCustody
}

type emergencySettlement struct {
	epoch        fatalEpochID
	owner        campaignToken
	acknowledged []attemptGeneration
	residual     []residualCustody
}

type Violation struct {
	operation, reason                               string
	phase                                           uint8
	rejectedEvent                                   string
	stableIdentities, obligationSnapshot, traceTail []string
}

type runtimeInvariantViolation = Violation

// NewViolation records invariant evidence originating in another campaign-owned adapter.
func NewViolation(operation, reason string) Violation {
	return Violation{operation: operation, reason: reason}
}

// Operation returns the rejected campaign operation.
func (violation Violation) Operation() string { return violation.operation }

// Reason returns the invariant reason.
func (violation Violation) Reason() string { return violation.reason }

// Phase returns the campaign phase at rejection.
func (violation Violation) Phase() uint8 { return violation.phase }

// PhaseName returns the campaign phase at rejection.
func (violation Violation) PhaseName() string {
	switch campaignPhase(violation.phase) {
	case campaignPreparing:
		return "Preparing"
	case campaignBaselining:
		return "Baselining"
	case campaignRunning:
		return "Running"
	case campaignDraining:
		return "Draining"
	case campaignConfirming:
		return "Confirming"
	default:
		return ""
	}
}

// RejectedEvent returns the rejected fact name.
func (violation Violation) RejectedEvent() string { return violation.rejectedEvent }

// StableIdentities returns detached stable campaign identities.
func (violation Violation) StableIdentities() []string {
	return append([]string(nil), violation.stableIdentities...)
}

// Obligations returns detached campaign obligations at rejection.
func (violation Violation) Obligations() []string {
	return append([]string(nil), violation.obligationSnapshot...)
}

// TraceTail returns detached campaign trace evidence.
func (violation Violation) TraceTail() []string {
	return append([]string(nil), violation.traceTail...)
}

func invariant(operation, reason string) {
	panic(runtimeInvariantViolation{operation: operation, reason: reason})
}
