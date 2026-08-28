package ooze

import (
	"testing"

	"github.com/gtramontina/ooze/internal/ooze/internal/campaign"
	"github.com/stretchr/testify/assert"
)

func TestManagedCleanupFailureRetainsOrderedResidualEvidence(t *testing.T) {
	result := presentManagedRelease(campaign.Result{
		Outcome: campaign.ManagedCleanupUnconfirmed,
		Residual: []campaign.ResidualCustody{
			{Attempt: "campaign-1:2", Generation: 7, Transferred: true},
			{Attempt: "campaign-1:3", Generation: 9, Prospective: true},
		},
	})

	assert.Equal(t, ManagedCleanupUnconfirmed, result.Outcome)
	assert.Equal(t, []ManagedResidualCustody{
		{Attempt: "campaign-1:2", Generation: 7, Stage: ManagedResidualOwned, Transferred: true},
		{Attempt: "campaign-1:3", Generation: 9, Stage: ManagedResidualProspective},
	}, result.Residual)
}

func TestManagedAbortCauseMappingIsExhaustive(t *testing.T) {
	tests := []struct {
		name string
		from campaign.AbortCause
		to   ManagedAbortCause
	}{
		{"campaign registration", campaign.AbortCampaignRegistration, ManagedAbortCampaignRegistration},
		{"snapshot materialization", campaign.AbortSnapshotMaterialization, ManagedAbortSnapshotMaterialization},
		{"catalogue discovery", campaign.AbortCatalogueDiscovery, ManagedAbortCatalogueDiscovery},
		{"workspace materialization", campaign.AbortWorkspaceMaterialization, ManagedAbortWorkspaceMaterialization},
		{"admission rejection", campaign.AbortAdmissionRejected, ManagedAbortAdmissionRejected},
		{"fatal epoch", campaign.AbortFatalEpoch, ManagedAbortFatalEpoch},
		{"workspace cleanup", campaign.AbortWorkspaceCleanup, ManagedAbortWorkspaceCleanup},
		{"snapshot cleanup", campaign.AbortSnapshotCleanup, ManagedAbortSnapshotCleanup},
		{"attempt not released", campaign.AbortAttemptNotReleased, ManagedAbortAttemptNotReleased},
		{"prospective launch", campaign.AbortProspectiveLaunch, ManagedAbortProspectiveLaunch},
		{"drainage", campaign.AbortDrainageUnconfirmed, ManagedAbortDrainageUnconfirmed},
		{"baseline failure", campaign.AbortBaselineFailed, ManagedAbortBaselineFailed},
		{"baseline deadline", campaign.AbortBaselineDeadline, ManagedAbortBaselineDeadline},
		{"baseline fuse", campaign.AbortBaselineFuse, ManagedAbortBaselineFuse},
		{"baseline stopped", campaign.AbortBaselineStopped, ManagedAbortBaselineStopped},
		{"baseline infrastructure", campaign.AbortBaselineInfrastructure, ManagedAbortBaselineInfrastructure},
		{"primary stopped", campaign.AbortPrimaryStopped, ManagedAbortPrimaryStopped},
		{"primary infrastructure", campaign.AbortPrimaryInfrastructure, ManagedAbortPrimaryInfrastructure},
		{"confirmation infrastructure", campaign.AbortConfirmationInfrastructure, ManagedAbortConfirmationInfrastructure},
		{"process emergency", campaign.AbortProcessEmergency, ManagedAbortProcessEmergency},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.to, presentManagedAbortCause(test.from))
		})
	}

	assert.Panics(t, func() { presentManagedAbortCause(0) })
}
