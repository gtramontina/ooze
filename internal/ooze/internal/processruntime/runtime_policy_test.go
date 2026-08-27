package processruntime_test

import (
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplayAttributesOverlappedDeadlineThroughExclusiveConfirmation(t *testing.T) {
	replay := processruntime.NewReplay(2)
	replay, campaignResult := replay.Apply(processruntime.RegisterCampaignCut(11))
	campaign := campaignResult.Registration().Campaign()
	replay, peerResult := replay.Apply(processruntime.RegisterCampaignCut(22))
	peer := peerResult.Registration().Campaign()

	first := processruntime.Admission{
		Campaign: campaign, Attempt: "first", Class: processruntime.SharedAdmission,
		Profile: processruntime.AutomaticProfile, Deadline: 31 * time.Second,
	}
	second := processruntime.Admission{
		Campaign: peer, Attempt: "second", Class: processruntime.SharedAdmission,
		Profile: processruntime.AutomaticProfile, Deadline: 31 * time.Second,
	}
	replay, firstAdmission := replay.Apply(processruntime.RequestAdmissionCut(first))
	replay, secondAdmission := replay.Apply(processruntime.RequestAdmissionCut(second))
	replay, firstStart := replay.Apply(processruntime.CommitStartCut(firstAdmission.Admission().Deliveries()[0]))
	replay, secondStart := replay.Apply(processruntime.CommitStartCut(secondAdmission.Admission().Deliveries()[0]))
	firstGeneration := firstStart.Start().Generation()
	secondGeneration := secondStart.Start().Generation()
	replay, _ = replay.Apply(processruntime.ObserveAttemptCut(firstGeneration, processruntime.Owned()))
	replay, _ = replay.Apply(processruntime.ObserveAttemptCut(secondGeneration, processruntime.Owned()))
	replay, provisional := replay.Apply(processruntime.ObserveAttemptCut(
		firstGeneration, processruntime.Tripped(false, processruntime.AutomaticProfile, 31*time.Second),
	))

	t.Run("gate closes atomically", func(t *testing.T) {
		assert.True(t, provisional.Receipt().ConfirmationProvisional())
		replayAfterRequest, rejected := replay.Apply(processruntime.RequestAdmissionCut(processruntime.Admission{
			Campaign: campaign, Attempt: "late", Class: processruntime.SharedAdmission,
		}))
		assert.Equal(t, processruntime.AdmissionRejectedGateClosed, rejected.Admission().Decision())
		assert.Equal(t, replay.Projection(), replayAfterRequest.Projection())
	})

	replay, bound := replay.Apply(processruntime.BindConfirmationBarrierCut(processruntime.Barrier{
		Campaign: campaign, Attempt: "confirmation", Profile: processruntime.AutomaticProfile,
		Deadline: 31 * time.Second,
	}))
	assert.Equal(t, processruntime.BarrierBound, bound.Barrier().Decision())
	assert.Empty(t, bound.Barrier().Deliveries())
	replay, settledPeer := replay.Apply(processruntime.ObserveAttemptCut(
		secondGeneration, processruntime.Settled(processruntime.AutomaticProfile, 31*time.Second),
	))
	assert.True(t, settledPeer.Receipt().SettlementAcknowledged())
	require.Len(t, settledPeer.Receipt().Deliveries(), 1)
	confirmation := settledPeer.Receipt().Deliveries()[0]
	replay, confirmationStart := replay.Apply(processruntime.CommitStartCut(confirmation))
	replay, _ = replay.Apply(processruntime.ObserveAttemptCut(confirmationStart.Start().Generation(), processruntime.Owned()))
	replay, confirmationResult := replay.Apply(processruntime.ObserveAttemptCut(
		confirmationStart.Start().Generation(), processruntime.Settled(confirmation.Profile, confirmation.Deadline),
	))

	t.Run("confirmation authorizes pressure then queue completion", func(t *testing.T) {
		assert.True(t, confirmationResult.Receipt().PressureTransitioned())
		assert.True(t, replay.Projection().SingleAdmission())
		var completed processruntime.ReplayResult
		replay, completed = replay.Apply(processruntime.CompleteConfirmationQueueCut(campaign))
		assert.Equal(t, processruntime.ConfirmationQueueCompleted, completed.Queue().Decision())
	})
}

func TestReplayPreservesExactResidualCustodyThroughFatalClosure(t *testing.T) {
	replay := processruntime.NewReplay(2)
	replay, registered := replay.Apply(processruntime.RegisterCampaignCut(11))
	campaign := registered.Registration().Campaign()
	admissions := []processruntime.Admission{
		{Campaign: campaign, Attempt: "owned", Class: processruntime.SharedAdmission},
		{Campaign: campaign, Attempt: "prospective", Class: processruntime.SharedAdmission},
	}
	generations := make([]processruntime.Generation, len(admissions))
	for index, admission := range admissions {
		var admitted processruntime.ReplayResult
		replay, admitted = replay.Apply(processruntime.RequestAdmissionCut(admission))
		var started processruntime.ReplayResult
		replay, started = replay.Apply(processruntime.CommitStartCut(admitted.Admission().Deliveries()[0]))
		generations[index] = started.Start().Generation()
	}
	replay, _ = replay.Apply(processruntime.ObserveAttemptCut(generations[0], processruntime.Owned()))
	replay, closed := replay.Apply(processruntime.CloseCut("fatal test"))

	t.Run("closure reports owned and prospective custody", func(t *testing.T) {
		residual := closed.Closure().Residual()
		require.Len(t, residual, 2)
		assert.False(t, residual[0].Prospective())
		assert.True(t, residual[1].Prospective())
		assert.True(t, replay.Projection().Closing())
	})

	replay, settled := replay.Apply(processruntime.SettleEmergencyCut([]processruntime.Resolution{
		processruntime.TransferCustody(generations[1]), processruntime.ConfirmedDrained(generations[0]),
	}))

	t.Run("settlement retains only transferred custody", func(t *testing.T) {
		assert.Equal(t, []processruntime.Generation{generations[1], generations[0]}, settled.Settlement().Acknowledged())
		residual := replay.Residual()
		require.Len(t, residual, 1)
		assert.Equal(t, generations[1], residual[0].Generation())
		assert.True(t, residual[0].Transferred())
		assert.True(t, replay.Projection().Unconfirmed())
	})
}

func TestReplayRejectsTerminalCommitmentWithOutstandingCustody(t *testing.T) {
	replay := processruntime.NewReplay(1)
	replay, registered := replay.Apply(processruntime.RegisterCampaignCut(11))
	campaign := registered.Registration().Campaign()
	admission := processruntime.Admission{Campaign: campaign, Attempt: "a", Class: processruntime.SharedAdmission}
	replay, _ = replay.Apply(processruntime.RequestAdmissionCut(admission))
	before := replay.Projection()
	replay, rejected := replay.Apply(processruntime.CommitTerminalCut(campaign))

	assert.Equal(t, processruntime.TerminalRejectedOutstanding, rejected.Terminal().Decision())
	assert.Equal(t, before, replay.Projection())
}

func TestRuntimeMalformedEvidenceClosesWithoutReplacingTheViolation(t *testing.T) {
	runtime := processruntime.New(1)
	campaign := runtime.RegisterCampaign(11).Campaign()
	await := runtime.RequestAdmission(processruntime.Admission{
		Campaign: campaign, Attempt: "a", Class: processruntime.SharedAdmission,
	})
	grant, received := await.Receive()
	require.True(t, received)
	start := runtime.CommitStart(grant, processruntime.NewStartCell())
	runtime.Observe(start.Generation(), start.Launch(func(processruntime.Generation) processruntime.Observation {
		return processruntime.Owned()
	}))

	assert.Panics(t, func() {
		runtime.Observe(start.Generation()+1, processruntime.Settled(processruntime.AutomaticProfile, 0))
	})
	assert.True(t, runtime.Projection().Closing())
}
