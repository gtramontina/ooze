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
	fatalEpochID      uint64
)

type campaignToken struct {
	id      uint64
	lineage campaignLineage
}

func (token campaignToken) ID() uint64 { return token.id }

func (token campaignToken) Lineage() campaignLineage { return token.lineage }

func campaignTokenValue(value processruntime.Campaign) campaignToken {
	return campaignToken{id: value.ID(), lineage: value.Lineage()}
}

type Profile = processruntime.Profile

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

func NewViolation(operation, reason string) Violation {
	return Violation{operation: operation, reason: reason}
}

func (violation Violation) Operation() string { return violation.operation }

func (violation Violation) Reason() string { return violation.reason }

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

func (violation Violation) RejectedEvent() string { return violation.rejectedEvent }

func (violation Violation) StableIdentities() []string {
	return append([]string(nil), violation.stableIdentities...)
}

func (violation Violation) Obligations() []string {
	return append([]string(nil), violation.obligationSnapshot...)
}

func (violation Violation) TraceTail() []string {
	return append([]string(nil), violation.traceTail...)
}

func invariant(operation, reason string) {
	panic(runtimeInvariantViolation{operation: operation, reason: reason})
}
