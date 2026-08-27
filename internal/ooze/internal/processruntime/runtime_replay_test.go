package processruntime_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplayRegistersIndependentCampaignsAndRejectsRecursion(t *testing.T) {
	replay := processruntime.NewReplay(2)
	replay, first := replay.Apply(processruntime.RegisterCampaignCut(11))
	replay, second := replay.Apply(processruntime.RegisterCampaignCut(22))
	before := replay.Projection()
	replay, recursive := replay.Apply(processruntime.RegisterCampaignCut(11))

	assert.Equal(t, processruntime.CampaignRegistered, first.Registration().Decision())
	assert.Equal(t, processruntime.CampaignRegistered, second.Registration().Decision())
	assert.Equal(t, processruntime.CampaignRejectedRecursive, recursive.Registration().Decision())
	assert.NotEqual(t, first.Registration().Campaign().ID(), second.Registration().Campaign().ID())
	assert.Equal(t, before, replay.Projection())
}

func TestReplayGrantsAdmissionsInStableOrder(t *testing.T) {
	tests := map[string]struct {
		classes []processruntime.AdmissionClass
	}{
		"shared capacity": {
			classes: []processruntime.AdmissionClass{
				processruntime.SharedAdmission, processruntime.SharedAdmission, processruntime.SharedAdmission,
			},
		},
		"exclusive barrier": {
			classes: []processruntime.AdmissionClass{
				processruntime.SharedAdmission, processruntime.SharedAdmission, processruntime.ExclusiveAdmission,
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			replay := processruntime.NewReplay(2)
			campaigns := make([]processruntime.Campaign, len(test.classes))
			for index := range campaigns {
				var registered processruntime.ReplayResult
				replay, registered = replay.Apply(processruntime.RegisterCampaignCut(processruntime.Lineage(index + 1)))
				campaigns[index] = registered.Registration().Campaign()
			}

			admissions := make([]processruntime.Admission, len(test.classes))
			for index, class := range test.classes {
				admissions[index] = processruntime.Admission{
					Campaign: campaigns[index], Attempt: string(rune('a' + index)), Class: class,
				}
				var requested processruntime.ReplayResult
				replay, requested = replay.Apply(processruntime.RequestAdmissionCut(admissions[index]))
				require.Equal(t, processruntime.AdmissionAccepted, requested.Admission().Decision())
			}

			assert.EqualValues(t, 3, replay.Projection().AdmissionCount())
		})
	}
}

func TestReplayBoundsCampaignDemandAndExclusiveOwnership(t *testing.T) {
	replay := processruntime.NewReplay(2)
	replay, registered := replay.Apply(processruntime.RegisterCampaignCut(11))
	campaign := registered.Registration().Campaign()

	t.Run("shared demand", func(t *testing.T) {
		local := replay
		for _, attempt := range []string{"a1", "a2"} {
			local, _ = local.Apply(processruntime.RequestAdmissionCut(processruntime.Admission{
				Campaign: campaign, Attempt: attempt, Class: processruntime.SharedAdmission,
			}))
		}
		before := local.Projection()
		local, rejected := local.Apply(processruntime.RequestAdmissionCut(processruntime.Admission{
			Campaign: campaign, Attempt: "a3", Class: processruntime.SharedAdmission,
		}))
		assert.Equal(t, processruntime.AdmissionRejectedSharedLimit, rejected.Admission().Decision())
		assert.Equal(t, before, local.Projection())
	})

	t.Run("exclusive ownership", func(t *testing.T) {
		local, _ := replay.Apply(processruntime.RequestAdmissionCut(processruntime.Admission{
			Campaign: campaign, Attempt: "x1", Class: processruntime.ExclusiveAdmission,
		}))
		before := local.Projection()
		local, rejected := local.Apply(processruntime.RequestAdmissionCut(processruntime.Admission{
			Campaign: campaign, Attempt: "x2", Class: processruntime.ExclusiveAdmission,
		}))
		assert.Equal(t, processruntime.AdmissionRejectedExclusiveOutstanding, rejected.Admission().Decision())
		assert.Equal(t, before, local.Projection())
	})
}

func TestRuntimeInstallsStartBeforeLaunchAndCorrelatesTerminalEvidence(t *testing.T) {
	runtime := processruntime.New(1)
	campaign := runtime.RegisterCampaign(11).Campaign()
	await := runtime.RequestAdmission(processruntime.Admission{
		Campaign: campaign, Attempt: "a1", Class: processruntime.SharedAdmission,
	})
	grant, received := await.Receive()
	require.True(t, received)
	cell := processruntime.NewStartCell()
	start := runtime.CommitStart(grant, cell)

	observed := start.Launch(func(generation processruntime.Generation) processruntime.Observation {
		assert.Equal(t, generation, cell.InstalledGeneration())
		assert.True(t, runtime.Projection().Prospective(generation))
		return processruntime.NotReleased(false)
	})
	receipt := runtime.Observe(start.Generation(), observed)

	assert.True(t, receipt.SettlementAcknowledged())
	assert.Empty(t, runtime.Residual())
	assert.Panics(t, func() {
		runtime.Observe(start.Generation()+1, processruntime.Settled(processruntime.AutomaticProfile, 0))
	})
}
