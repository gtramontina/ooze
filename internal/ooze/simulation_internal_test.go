package ooze

import (
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	campaignmodule "github.com/gtramontina/ooze/internal/ooze/internal/campaign"
	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type simulationFocusedChoiceSource func([]simulationEngineMove) int

type mutantResultKind = campaignmodule.ManagedMutationOutcome
type campaignAttemptEvidenceKind = campaignmodule.AttemptKind
type simulationTestFactKind string

const (
	campaignAttemptBaseline                            = campaignmodule.BaselineAttempt
	campaignAttemptPrimary                             = campaignmodule.PrimaryAttempt
	campaignAttemptConfirmation                        = campaignmodule.ConfirmationAttempt
	campaignFactAttemptLaunched simulationTestFactKind = "attempt launched"
	campaignFactAttemptTerminal simulationTestFactKind = "attempt terminal"
	campaignFactStartCommitted  simulationTestFactKind = "start committed"

	mutantSurvived = campaignmodule.ManagedSurvived
	mutantKilled   = campaignmodule.ManagedKilled
	mutantTimedOut = campaignmodule.ManagedTimedOut
	mutantRunaway  = campaignmodule.ManagedRunaway

	campaignEvidenceSettled  = campaignmodule.AttemptSettled
	campaignEvidenceDeadline = campaignmodule.AttemptDeadline
)

func (simulationFocusedChoiceSource) choose(int) int { return 0 }

func (source simulationFocusedChoiceSource) chooseMove(moves []simulationEngineMove) int {
	return source(moves)
}

func simulationMaterializesWorkspace(effect campaignmodule.Effect) bool {
	request, ok := effect.ArtifactRequest()
	if !ok {
		return false
	}
	_, _, ok = request.Workspace()
	return ok
}

func simulationLaunchesAttempt(effect campaignmodule.Effect) bool {
	request, ok := effect.SupervisionRequest()
	if !ok {
		return false
	}
	_, ok = request.Prospective(time.Time{}, time.Time{})
	return ok
}

func simulationStopsAttempt(effect campaignmodule.Effect) bool {
	request, ok := effect.SupervisionRequest()
	if !ok {
		return false
	}
	_, ok = request.StopGeneration()
	return ok
}

func simulationTestFact(fact campaignmodule.Fact) simulationTestFactKind {
	switch {
	case fact.IsAttemptLaunched():
		return campaignFactAttemptLaunched
	case fact.IsAttemptTerminal():
		return campaignFactAttemptTerminal
	case fact.IsStartCommitted():
		return campaignFactStartCommitted
	default:
		return ""
	}
}

func TestSimulationExploresAndReplaysEmptyCatalogueThroughProductionOwners(t *testing.T) {
	definition := simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-a",
			Lineage:  11,
			Command:  []string{"go", "test", "./..."},
			Profile:  AutomaticProfile,
			Peers:    2,
		},
		capacity: 2,
	}

	explored := Explore(definition, simulationChoiceBytes{0, 1, 2})
	assert.Nil(t, explored.failure, "exploration failure=%v", explored.failure)
	assert.Equal(t, campaignmodule.NoMutantsOutcome, explored.world.campaign.Outcome().Kind())
	{
		got, want := simulationAuthorities(explored.trace), []simulationAuthority{
			simulationRuntimeAuthority,
			simulationCampaignAuthority,
			simulationCampaignAuthority,
			simulationCampaignAuthority,
			simulationCampaignAuthority,
			simulationRuntimeAuthority,
			simulationCampaignAuthority,
		}
		assert.Equal(t, want, got, "trace authorities=%v, want %v", got, want)
	}
	for index, record := range explored.trace.records {
		{
			got, want := record.sequence, uint64(index+1)
			assert.Equal(t, want, got, "record %d sequence=%d, want %d", index, got, want)
		}
	}

	replayed := ReplayLegal(explored.trace)
	assert.Nil(t, replayed.failure, "replay failure=%v", replayed.failure)
	assert.Equal(t, explored.world, replayed.world, "replayed world diverged:\n got: %#v\nwant: %#v", replayed.world, explored.world)
}

func TestSimulationComposesSupervisedBaselineFailureAndTerminalRecovery(t *testing.T) {
	definition := simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-supervised",
			Lineage:  21,
			Command:  []string{"go", "test", "./..."},
			Profile:  AutomaticProfile,
			Peers:    2,
		},
		capacity:  2,
		catalogue: []mutantIdentity{"mutant-a"},
	}

	explored := Explore(definition, simulationChoiceBytes{0, 2})
	assert.Nil(t, explored.failure, "exploration failure=%v", explored.failure)
	{
		ok := explored.world.campaign.Outcome().Kind() == campaignmodule.AbortedOutcome
		require.True(t, ok, "explored outcome=%#v, want aborted baseline", explored.world.campaign.Outcome())
	}
	var supervisorKinds []supervision.FactKind
	for _, record := range explored.trace.records {
		if record.authority == supervisionAuthority {
			supervisorKinds = append(supervisorKinds, record.supervisorEvent.Kind())
		}
	}
	wantSupervisorKinds := []supervision.FactKind{
		supervision.ProspectiveRegisteredFact, supervision.LaunchCompletedFact, supervision.RunningObservedFact,
		supervision.DrainCompletedFact, supervision.DrainCompletedFact, supervision.OutputCompletedFact,
		supervision.StopAdmissionSealedFact, supervision.ReleaseCompletedFact, supervision.RuntimeCompletedFact,
	}
	assert.Equal(t, wantSupervisorKinds, supervisorKinds, "supervisor lifecycle=%v, want %v", supervisorKinds, wantSupervisorKinds)
	assert.True(t, explored.world.supervisor.Quiescent(), "terminal world is not quiescent: %#v", explored.world)
	assert.EqualValues(t, 0, explored.world.runtime.CampaignCount(), "terminal world is not quiescent: %#v", explored.world)

	replayed := ReplayLegal(explored.trace)
	assert.Nil(t, replayed.failure, "replay failure=%v", replayed.failure)
	assert.Equal(t, explored.world, replayed.world, "replayed world diverged:\n got: %#v\nwant: %#v", replayed.world, explored.world)
}

func TestSimulationChoiceSourceSelectsCanonicalLegalLaunchBoundaryFacts(t *testing.T) {
	definition := simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-boundary", Lineage: 22, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}
	for _, test := range []struct {
		name    string
		choice  byte
		kind    supervision.FactKind
		equalAt bool
	}{
		{name: "completion before", choice: 0, kind: supervision.LaunchCompletedFact},
		{name: "completion at equality", choice: 1, kind: supervision.LaunchBoundaryFact, equalAt: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			explored := Explore(definition, simulationChoiceBytes{test.choice})
			var launch supervision.Fact
			var launchBy time.Time
			for _, record := range explored.trace.records {
				if record.authority == supervisionAuthority &&
					record.supervisorEvent.Kind() == supervision.ProspectiveRegisteredFact {
					launchBy, _, _ = record.supervisorEvent.LaunchEvidence()
				}
				if record.authority == supervisionAuthority &&
					(record.supervisorEvent.Kind() == supervision.LaunchCompletedFact ||
						record.supervisorEvent.Kind() == supervision.LaunchBoundaryFact) {
					launch = record.supervisorEvent
					break
				}
			}
			require.Equal(t, test.kind, launch.Kind(), "selected launch fact=%#v, want kind %v", launch, test.kind)
			{
				_, completionAt, present := launch.LaunchEvidence()
				require.True(t, present)
				got := completionAt.Equal(launchBy)
				assert.Equal(t, test.equalAt, got, "completion/boundary equality=%v, want %v", got, test.equalAt)
			}
			replayed := ReplayLegal(explored.trace)
			assert.Nil(t, replayed.failure, "boundary replay diverged: %#v", replayed)
			assert.Equal(t, explored.world, replayed.world, "boundary replay diverged: %#v", replayed)
		})
	}
}

func TestSimulationChoiceSourceSelectsCanonicalAfterBoundaryFacts(t *testing.T) {
	tests := []struct {
		name    string
		action  supervision.EffectKind
		variant simulationMoveVariant
		outcome func(simulationWorld) bool
		observe func(simulationTrace) bool
	}{
		{
			name: "launch after", action: supervision.LaunchNativeEffect,
			variant: simulationMoveVariant{launch: simulationLaunchAfterBoundary},
			outcome: func(world simulationWorld) bool {
				aborted := world.campaign.Outcome().Kind() == campaignmodule.AbortedOutcome

				return aborted && !world.campaign.Failed()
			},
			observe: func(trace simulationTrace) bool {
				var launchBy time.Time
				for _, record := range trace.records {
					if record.authority != supervisionAuthority {
						continue
					}
					if record.supervisorEvent.Kind() == supervision.ProspectiveRegisteredFact {
						launchBy, _, _ = record.supervisorEvent.LaunchEvidence()
					}
					if record.supervisorEvent.Kind() == supervision.LaunchCompletedFact &&
						record.supervisorEvent.OccurredAt().After(launchBy) {
						return true
					}
				}

				return false
			},
		},
		{
			name: "deadline after", action: supervision.WaitRootEffect,
			variant: simulationMoveVariant{running: simulationRunningAfterDeadline},
			outcome: func(world simulationWorld) bool {
				completed, ok := world.campaign.Mutations(), world.campaign.Outcome().Kind() == campaignmodule.CompletedOutcome

				return ok && !world.campaign.Failed() && len(completed) == 1 &&
					completed[0].Result() == mutantTimedOut
			},
			observe: func(trace simulationTrace) bool {
				for _, record := range trace.records {
					if record.authority != supervisionAuthority ||
						record.supervisorEvent.Kind() != supervision.RunningObservedFact {
						continue
					}
					if record.supervisorEvent.HasRootExitAfterRecheck() {
						return true
					}
				}

				return false
			},
		},
		{
			name: "drain after", action: supervision.ObserveEmptinessEffect,
			variant: simulationMoveVariant{drain: simulationDrainAfterBoundary},
			outcome: func(world simulationWorld) bool {
				failed := world.campaign.CleanupUnconfirmed()

				return failed && world.runtime.Unconfirmed()
			},
			observe: func(trace simulationTrace) bool {
				drainBy := make(map[supervision.ActionToken]time.Time)
				for _, record := range trace.records {
					for _, action := range record.supervisorActions {
						if action.Kind() == supervision.ObserveEmptinessEffect {
							drainBy[action.Token()] = action.DrainBy()
						}
					}
					if record.authority == supervisionAuthority &&
						record.supervisorEvent.Kind() == supervision.DrainCompletedFact &&
						record.supervisorEvent.OccurredAt().After(
							drainBy[supervision.ActionToken(record.source.identity)],
						) {
						return true
					}
				}

				return false
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selected := false
			choices := simulationFocusedChoiceSource(func(moves []simulationEngineMove) int {
				for index, move := range moves {
					if move.action.Kind() == test.action && move.attemptKind == campaignAttemptPrimary &&
						move.variant == test.variant {
						selected = true

						return index
					}
				}
				for index, move := range moves {
					if move.action.Kind() != supervision.WaitRootEffect {
						return index
					}
				}

				return 0
			})
			explored := Explore(simulationDefinition{
				campaign: campaignmodule.Definition{
					Identity: "campaign-after-boundary", Lineage: 221, Command: []string{"test"},
					Profile: AutomaticProfile, Peers: 1,
				},
				capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
			}, choices)
			assert.Nil(t, explored.failure, "after-boundary selected=%v outcome=%T campaign-failure=%T simulation-failure=%v", selected, explored.world.campaign.Outcome(), explored.world.campaign.Failed(), explored.failure)
			assert.True(t, selected, "after-boundary selected=%v outcome=%T campaign-failure=%T simulation-failure=%v", selected, explored.world.campaign.Outcome(), explored.world.campaign.Failed(), explored.failure)
			assert.True(t, test.outcome(explored.world), "after-boundary selected=%v outcome=%T campaign-failure=%T simulation-failure=%v", selected, explored.world.campaign.Outcome(), explored.world.campaign.Failed(), explored.failure)
			assert.True(t, test.observe(explored.trace), "after-boundary selected=%v outcome=%T campaign-failure=%T simulation-failure=%v", selected, explored.world.campaign.Outcome(), explored.world.campaign.Failed(), explored.failure)
			if test.action == supervision.LaunchNativeEffect {
				order := make(map[supervision.EffectKind]int)
				ordinal := 0
				for _, record := range explored.trace.records {
					for _, action := range record.supervisorActions {
						ordinal++
						order[action.Kind()] = ordinal
					}
				}
				assert.False(t, order[supervision.RevokeLaunchReleaseEffect] >= order[supervision.PublishLaunchUnconfirmedEffect], "late closure/adoption action order=%v", order)
				assert.False(t, order[supervision.PublishLaunchUnconfirmedEffect] >= order[supervision.AdoptOwnedEffect], "late closure/adoption action order=%v", order)
				assert.False(t, order[supervision.AdoptOwnedEffect] >= order[supervision.ForceOwnedEffect], "late closure/adoption action order=%v", order)
			}
			{
				replayed := ReplayLegal(explored.trace)
				assert.Nil(t, replayed.failure, "after-boundary replay=%#v", replayed)
				assert.Equal(t, explored.world, replayed.world, "after-boundary replay=%#v", replayed)
			}
		})
	}
}

func TestSimulationChoiceSourceCompletesPrimaryOutcomes(t *testing.T) {
	definition := simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-primary", Lineage: 23, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}
	for _, test := range []struct {
		name    string
		variant simulationRunningVariant
		want    mutantResultKind
	}{
		{name: "survived", want: mutantSurvived},
		{name: "killed", variant: simulationRunningFailed, want: mutantKilled},
		{name: "deadline", variant: simulationRunningAtDeadline, want: mutantTimedOut},
		{name: "fuse", variant: simulationRunningFuse, want: mutantRunaway},
	} {
		t.Run(test.name, func(t *testing.T) {
			choices := simulationFocusedChoiceSource(func(moves []simulationEngineMove) int {
				for index, move := range moves {
					if move.action.Kind() == supervision.WaitRootEffect && move.attemptKind == campaignAttemptPrimary &&
						move.variant.running == test.variant {
						return index
					}
				}
				for index, move := range moves {
					if move.action.Kind() != supervision.WaitRootEffect {
						return index
					}
				}

				return 0
			})
			explored := Explore(definition, choices)
			completed, ok := explored.world.campaign.Mutations(), explored.world.campaign.Outcome().Kind() == campaignmodule.CompletedOutcome
			require.Nil(t, explored.failure, "completed outcome=%#v failure=%v, want %v; choices=%#v", explored.world.campaign.Outcome(), explored.failure, test.want, explored.trace.choices)
			assert.True(t, ok, "completed outcome=%#v failure=%v, want %v; choices=%#v", explored.world.campaign.Outcome(), explored.failure, test.want, explored.trace.choices)
			require.Len(t, completed, 1, "completed outcome=%#v failure=%v, want %v; choices=%#v", explored.world.campaign.Outcome(), explored.failure, test.want, explored.trace.choices)
			assert.Equal(t, test.want, completed[0].Result(), "completed outcome=%#v failure=%v, want %v; choices=%#v", explored.world.campaign.Outcome(), explored.failure, test.want, explored.trace.choices)
			assert.EqualValues(t, 2, explored.world.campaign.CommandCount(), "terminal commands/obligations=%d/%#v", explored.world.campaign.CommandCount(), explored.world.campaign.Projection().Obligations())
			assert.EqualValues(t, 0, len(explored.world.campaign.Projection().Obligations()), "terminal commands/obligations=%d/%#v", explored.world.campaign.CommandCount(), explored.world.campaign.Projection().Obligations())
			replayed := ReplayLegal(explored.trace)
			assert.Nil(t, replayed.failure, "outcome replay diverged: %#v", replayed)
			assert.Equal(t, explored.world, replayed.world, "outcome replay diverged: %#v", replayed)
		})
	}
}

func TestSimulationExploresEveryCatalogueMemberInStableOrder(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-catalogue", Lineage: 24, Command: []string{"test"},
			Profile: SerialProfile, Peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, simulationChoiceBytes{0})
	completed, ok := explored.world.campaign.Mutations(), explored.world.campaign.Outcome().Kind() == campaignmodule.CompletedOutcome
	require.Nil(t, explored.failure, "catalogue exploration=%#v failure=%v", explored.world.campaign.Outcome(), explored.failure)
	assert.True(t, ok, "catalogue exploration=%#v failure=%v", explored.world.campaign.Outcome(), explored.failure)
	got := make([]mutantIdentity, len(completed))
	for index, mutant := range completed {
		got[index] = mutant.Identity()
	}
	{
		want := []mutantIdentity{"mutant-a", "mutant-b"}
		assert.Equal(t, want, got, "completed catalogue order=%v, want %v", got, want)
	}
	assert.EqualValues(t, 3, explored.world.campaign.CommandCount(), "command count=%d, want baseline plus two primaries", explored.world.campaign.CommandCount())
}

func TestSimulationExploresOneMutantWithSparePeerCapacity(t *testing.T) {
	result := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-spare-capacity", Lineage: 43, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 4,
		},
		capacity: 4, catalogue: []mutantIdentity{"mutant-a"},
	}, nil)
	completed, ok := result.world.campaign.Mutations(), result.world.campaign.Outcome().Kind() == campaignmodule.CompletedOutcome
	assert.Nil(t, result.failure, "spare-capacity exploration outcome=%#v failure=%v", result.world.campaign.Outcome(), result.failure)
	assert.True(t, ok, "spare-capacity exploration outcome=%#v failure=%v", result.world.campaign.Outcome(), result.failure)
	assert.EqualValues(t, 1, len(completed), "spare-capacity exploration outcome=%#v failure=%v", result.world.campaign.Outcome(), result.failure)
}

func TestSimulationExploresPeerPrimaryOverlapFromEmittedEffectWave(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-overlap", Lineage: 25, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 2,
		},
		capacity: 2, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, simulationChoiceBytes{0})
	assert.Nil(t, explored.failure, "overlap exploration failed: %v", explored.failure)
	found := false
	for _, record := range explored.trace.records {
		if record.authority != simulationRuntimeAuthority || record.runtimeState.AdmissionCount() != 2 {
			continue
		}
		if record.runtimeState.HasOverlappedPair() {
			found = true
			break
		}
	}
	assert.True(t, found, "two emitted primary effects never reached overlapping start commitments")
	replayed := ReplayLegal(explored.trace)
	assert.Nil(t, replayed.failure, "overlap replay failed: %v", replayed.failure)
	assert.Equal(t, explored.world, replayed.world, "overlap replay world diverged:\n got=%#v\nwant=%#v", replayed.world, explored.world)
}

func TestSimulationFocusedLateGrantDeliveryRemainsLegal(t *testing.T) {
	delayed := false
	choices := simulationFocusedChoiceSource(func(moves []simulationEngineMove) int {
		grantAt := -1
		for index, move := range moves {
			if move.delivery.IsAdmissionGranted() {
				grantAt = index
				break
			}
		}
		if grantAt >= 0 {
			for index, move := range moves {
				if index == grantAt || move.action.Kind() == supervision.WaitRootEffect {
					continue
				}
				delayed = true

				return index
			}

			return grantAt
		}
		for index, move := range moves {
			if move.action.Kind() != supervision.WaitRootEffect {
				return index
			}
		}

		return 0
	})
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-late-grant", Lineage: 2511, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 2,
		},
		capacity: 2, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, choices)
	assert.Nil(t, explored.failure, "late-grant exploration failure=%v delayed=%v", explored.failure, delayed)
	assert.True(t, delayed, "late-grant exploration failure=%v delayed=%v", explored.failure, delayed)
	{
		replayed := ReplayLegal(explored.trace)
		assert.Nil(t, replayed.failure, "late-grant replay=%#v", replayed)
		assert.Equal(t, explored.world, replayed.world, "late-grant replay=%#v", replayed)
	}
}

func TestSimulationFocusedRepeatedIntrinsicDeadlinesDoNotChangeAdmission(t *testing.T) {
	choices := simulationFocusedChoiceSource(func(moves []simulationEngineMove) int {
		for index, move := range moves {
			if move.action.Kind() == supervision.WaitRootEffect && move.attemptKind == campaignAttemptPrimary &&
				move.variant.running == simulationRunningAtDeadline {
				return index
			}
		}
		for index, move := range moves {
			if move.action.Kind() != supervision.WaitRootEffect {
				return index
			}
		}

		return 0
	})
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-repeated-intrinsic-deadline", Lineage: 2512, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, choices)
	completed, ok := explored.world.campaign.Mutations(), explored.world.campaign.Outcome().Kind() == campaignmodule.CompletedOutcome
	require.Nil(t, explored.failure, "repeated-deadline exploration=%#v failure=%v", explored.world.campaign.Outcome(), explored.failure)
	assert.True(t, ok, "repeated-deadline exploration=%#v failure=%v", explored.world.campaign.Outcome(), explored.failure)
	assert.EqualValues(t, 2, len(completed), "repeated-deadline exploration=%#v failure=%v", explored.world.campaign.Outcome(), explored.failure)
	for _, mutant := range completed {
		assert.Equal(t, mutantTimedOut, mutant.Result(), "repeated-deadline mutant=%#v, want direct deadline without confirmation", mutant)
		assert.Equal(t, campaignEvidenceDeadline, mutant.Primary(), "repeated-deadline mutant=%#v, want direct deadline without confirmation", mutant)
		assert.Equal(t, campaignAttemptEvidenceKind(0), mutant.Confirmation(), "repeated-deadline mutant=%#v, want direct deadline without confirmation", mutant)
	}
	assert.False(t, explored.world.runtime.SingleAdmission(), "repeated-deadline admission mode/fallback=%v/%v", explored.world.runtime.SingleAdmission(), explored.world.campaign.SingleAdmissionFallback())
	assert.False(t, explored.world.campaign.SingleAdmissionFallback(), "repeated-deadline admission mode/fallback=%v/%v", explored.world.runtime.SingleAdmission(), explored.world.campaign.SingleAdmissionFallback())
	{
		replayed := ReplayLegal(explored.trace)
		assert.Nil(t, replayed.failure, "repeated-deadline replay=%#v", replayed)
		assert.Equal(t, explored.world, replayed.world, "repeated-deadline replay=%#v", replayed)
	}
}

func TestSimulationExploresOverlapConfirmationAndPressureFallback(t *testing.T) {
	timedOut := false
	choices := simulationFocusedChoiceSource(func(moves []simulationEngineMove) int {
		if !timedOut {
			peerReady := false
			for _, move := range moves {
				peerReady = peerReady || move.action.Kind() == supervision.WaitRootEffect &&
					move.attemptKind == campaignAttemptPrimary && move.mutant == "mutant-b"
			}
			if peerReady {
				for index, move := range moves {
					if move.action.Kind() == supervision.WaitRootEffect &&
						move.variant.running == simulationRunningAtDeadline &&
						move.attemptKind == campaignAttemptPrimary && move.mutant == "mutant-a" {
						timedOut = true

						return index
					}
				}
			}
		}
		for index, move := range moves {
			if move.action.Kind() != supervision.WaitRootEffect {
				return index
			}
		}

		return 0
	})
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-confirmation", Lineage: 252, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 2,
		},
		capacity: 2, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, choices)
	completed, ok := explored.world.campaign.Mutations(), explored.world.campaign.Outcome().Kind() == campaignmodule.CompletedOutcome
	require.Nil(t, explored.failure, "confirmation exploration=%#v failure=%v", explored.world.campaign.Outcome(), explored.failure)
	assert.True(t, ok, "confirmation exploration=%#v failure=%v", explored.world.campaign.Outcome(), explored.failure)
	assert.True(t, timedOut, "confirmation exploration=%#v failure=%v", explored.world.campaign.Outcome(), explored.failure)
	require.Len(t, completed, 2, "confirmation exploration=%#v failure=%v", explored.world.campaign.Outcome(), explored.failure)
	assert.Equal(t, mutantSurvived, completed[0].Result(), "confirmation outcome=%#v commands=%d", completed, explored.world.campaign.CommandCount())
	assert.Equal(t, campaignEvidenceDeadline, completed[0].Primary(), "confirmation outcome=%#v commands=%d", completed, explored.world.campaign.CommandCount())
	assert.Equal(t, campaignEvidenceSettled, completed[0].Confirmation(), "confirmation outcome=%#v commands=%d", completed, explored.world.campaign.CommandCount())
	assert.True(t, explored.world.campaign.SingleAdmissionFallback(), "confirmation outcome=%#v commands=%d", completed, explored.world.campaign.CommandCount())
	assert.EqualValues(t, 4, explored.world.campaign.CommandCount(), "confirmation outcome=%#v commands=%d", completed, explored.world.campaign.CommandCount())
	replayed := ReplayLegal(explored.trace)
	assert.Nil(t, replayed.failure, "confirmation replay failure=%v", replayed.failure)
	assert.Equal(t, explored.world, replayed.world, "confirmation replay world diverged")
}

func TestSimulationFocusedMultipleProvisionalsBindFIFOConfirmationBarriers(t *testing.T) {
	catalogue := []mutantIdentity{"mutant-a", "mutant-b", "mutant-c"}
	timedOut := make(map[mutantIdentity]bool, len(catalogue))
	choices := simulationFocusedChoiceSource(func(moves []simulationEngineMove) int {
		ready := 0
		for _, move := range moves {
			if move.action.Kind() == supervision.WaitRootEffect && move.attemptKind == campaignAttemptPrimary {
				ready++
			}
		}
		if ready >= 2 || len(timedOut) == len(catalogue)-1 {
			for _, mutant := range catalogue {
				if timedOut[mutant] {
					continue
				}
				for index, move := range moves {
					if move.action.Kind() == supervision.WaitRootEffect && move.attemptKind == campaignAttemptPrimary &&
						move.mutant == mutant &&
						move.variant.running == simulationRunningAtDeadline {
						timedOut[mutant] = true

						return index
					}
				}
			}
		}
		for index, move := range moves {
			if move.action.Kind() != supervision.WaitRootEffect {
				return index
			}
		}

		return 0
	})
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-multiple-provisionals", Lineage: 2521, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 3,
		},
		capacity: 3, catalogue: catalogue,
	}, choices)
	completed, ok := explored.world.campaign.Mutations(), explored.world.campaign.Outcome().Kind() == campaignmodule.CompletedOutcome
	require.Nil(t, explored.failure, "multiple-provisional exploration=%#v timed=%v failure=%v", explored.world.campaign.Outcome(), timedOut, explored.failure)
	assert.True(t, ok, "multiple-provisional exploration=%#v timed=%v failure=%v", explored.world.campaign.Outcome(), timedOut, explored.failure)
	assert.Equal(t, len(catalogue), len(timedOut), "multiple-provisional exploration=%#v timed=%v failure=%v", explored.world.campaign.Outcome(), timedOut, explored.failure)
	assert.Equal(t, len(catalogue), len(completed), "multiple-provisional exploration=%#v timed=%v failure=%v", explored.world.campaign.Outcome(), timedOut, explored.failure)
	for index, mutant := range completed {
		assert.Equal(t, catalogue[index], mutant.Identity(), "multiple-provisional mutant[%d]=%#v", index, mutant)
		assert.Equal(t, campaignEvidenceDeadline, mutant.Primary(), "multiple-provisional mutant[%d]=%#v", index, mutant)
		assert.Equal(t, campaignEvidenceSettled, mutant.Confirmation(), "multiple-provisional mutant[%d]=%#v", index, mutant)
	}
	barrierCount := 0
	var confirmations []mutantIdentity
	for _, record := range explored.trace.records {
		if record.authority == simulationRuntimeAuthority &&
			record.runtimeCut.Operation() == processruntime.BindConfirmationBarrierOperation {
			barrierCount++
		}
		for _, effect := range record.campaignEffects {
			if simulationLaunchesAttempt(effect) && effect.AttemptRole() == campaignAttemptConfirmation {
				confirmations = append(confirmations, effect.Mutant())
			}
		}
	}
	assert.Equal(t, 1, barrierCount, "confirmation barrier count=%d, confirmations=%v", barrierCount, confirmations)
	assert.Equal(t, catalogue, confirmations, "confirmation FIFO order=%v, want %v", confirmations, catalogue)
	{
		replayed := ReplayLegal(explored.trace)
		assert.Nil(t, replayed.failure, "multiple-provisional replay=%#v", replayed)
		assert.Equal(t, explored.world, replayed.world, "multiple-provisional replay=%#v", replayed)
	}
}

func TestSimulationFocusedReleaseCutPrecedesStopOfOwnedPeer(t *testing.T) {
	selected := false
	choices := simulationFocusedChoiceSource(func(moves []simulationEngineMove) int {
		ownedPeerReady := slices.ContainsFunc(moves, func(move simulationEngineMove) bool {
			return move.action.Kind() == supervision.WaitRootEffect && move.mutant == "mutant-a"
		})
		if ownedPeerReady {
			for index, move := range moves {
				if move.action.Kind() == supervision.LaunchNativeEffect && move.mutant == "mutant-b" &&
					move.variant.launch == simulationLaunchProvenNotReleased {
					selected = true

					return index
				}
			}
		}
		for index, move := range moves {
			if move.action.Kind() == supervision.LaunchNativeEffect && move.mutant == "mutant-b" {
				continue
			}
			if move.action.Kind() != supervision.WaitRootEffect {
				return index
			}
		}

		return 0
	})
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-release-stop", Lineage: 2522, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 2,
		},
		capacity: 2, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, choices)
	assert.Nil(t, explored.failure, "release/stop exploration selected=%v failure=%v", selected, explored.failure)
	assert.True(t, selected, "release/stop exploration selected=%v failure=%v", selected, explored.failure)
	publicationAt, stopEffectAt, stopFactAt := -1, -1, -1
	for index, record := range explored.trace.records {
		for _, action := range record.supervisorActions {
			if action.Kind() == supervision.PublishNotReleasedEffect {
				publicationAt = index
			}
		}
		for _, effect := range record.campaignEffects {
			if simulationStopsAttempt(effect) {
				stopEffectAt = index
			}
		}
		if record.supervisorEvent.HasStopRequest() {
			stopFactAt = index
		}
	}
	assert.False(t, publicationAt < 0, "release publication/stop effect/stop fact order=%d/%d/%d", publicationAt, stopEffectAt, stopFactAt)
	assert.False(t, stopEffectAt <= publicationAt, "release publication/stop effect/stop fact order=%d/%d/%d", publicationAt, stopEffectAt, stopFactAt)
	assert.False(t, stopFactAt <= stopEffectAt, "release publication/stop effect/stop fact order=%d/%d/%d", publicationAt, stopEffectAt, stopFactAt)
	{
		replayed := ReplayLegal(explored.trace)
		assert.Nil(t, replayed.failure, "release/stop replay=%#v", replayed)
		assert.Equal(t, explored.world, replayed.world, "release/stop replay=%#v", replayed)
	}
}

func TestSimulationFocusedUnconfirmedCustodyOrdersProspectiveBeforeOwned(t *testing.T) {
	selected := false
	choices := simulationFocusedChoiceSource(func(moves []simulationEngineMove) int {
		ownedPeerReady := slices.ContainsFunc(moves, func(move simulationEngineMove) bool {
			return move.action.Kind() == supervision.WaitRootEffect && move.mutant == "mutant-b"
		})
		if ownedPeerReady {
			for index, move := range moves {
				if move.action.Kind() == supervision.LaunchNativeEffect && move.mutant == "mutant-a" &&
					move.variant.launch == simulationLaunchAfterBoundary {
					selected = true

					return index
				}
			}
		}
		for index, move := range moves {
			if move.action.Kind() == supervision.LaunchNativeEffect && move.mutant == "mutant-a" {
				continue
			}
			if move.action.Kind() != supervision.WaitRootEffect {
				return index
			}
		}

		return 0
	})
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-custody-order", Lineage: 2523, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 2,
		},
		capacity: 2, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, choices)
	assert.Nil(t, explored.failure, "custody-order exploration selected=%v failure=%v", selected, explored.failure)
	assert.True(t, selected, "custody-order exploration selected=%v failure=%v", selected, explored.failure)
	var prospective []bool
	for _, record := range explored.trace.records {
		_, observation, observed := record.runtimeCut.Observation()
		if record.authority != simulationRuntimeAuthority || !observed ||
			observation.Kind() != processruntime.LaunchUnconfirmedKind {
			continue
		}
		for _, residual := range record.runtimeState.Residual() {
			prospective = append(prospective, residual.Prospective())
		}
		break
	}
	assert.Equal(t, []bool{true, false}, prospective, "unconfirmed prospective/owned custody order=%v", prospective)
	{
		replayed := ReplayLegal(explored.trace)
		assert.Nil(t, replayed.failure, "custody-order replay=%#v", replayed)
		assert.Equal(t, explored.world, replayed.world, "custody-order replay=%#v", replayed)
	}
}

func TestSimulationFocusedStartClosureTerminalFatalAndGlobalDrainExpiry(t *testing.T) {
	expired := false
	choices := simulationFocusedChoiceSource(func(moves []simulationEngineMove) int {
		for index, move := range moves {
			if move.action.Kind() == supervision.ObserveEmptinessEffect &&
				move.variant.drain == simulationDrainAtBoundary &&
				move.attemptKind == campaignAttemptPrimary {
				expired = true

				return index
			}
		}
		for index, move := range moves {
			if move.action.Kind() != supervision.WaitRootEffect {
				return index
			}
		}

		return 0
	})
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-drain-expiry", Lineage: 253, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}, choices)
	if explored.failure != nil || !expired {
		var last simulationRecord
		for _, record := range explored.trace.records {
			if record.authority == simulationCampaignAuthority {
				last = record
			}
		}
		require.FailNowf(t, "drain-expiry exploration failed", "failure=%v expired=%v phase=%v event=%v obligations=%d campaign-failure=%v", explored.failure, expired, last.campaignState.PhaseName(), last.campaignEvent.Fact().Name(), len(last.campaignState.Obligations()), last.campaignState.Failed())
	}
	{
		ok := explored.world.campaign.CleanupUnconfirmed()
		require.True(t, ok, "drain-expiry world=%#v", explored.world)
		assert.True(t, explored.world.runtime.Unconfirmed(), "drain-expiry world=%#v", explored.world)
	}
	startAt, closedAt, forcedAbortAt := -1, -1, -1
	for index, record := range explored.trace.records {
		if record.authority != simulationRuntimeAuthority {
			continue
		}
		if record.runtimeCut.Operation() == processruntime.CommitStartOperation &&
			record.runtimeCut.Result().Start().Decision() == processruntime.StartAccepted {
			startAt = index
		}
		if closedAt < 0 && !record.runtimeState.Open() {
			closedAt = index
		}
		if record.runtimeCut.Operation() == processruntime.AuthorizeForcedAbortOperation {
			forcedAbortAt = index
		}
		assert.False(t, closedAt >= 0 && index > closedAt && record.runtimeCut.Operation() == processruntime.CommitTerminalOperation, "normal terminal commitment followed fatal closure at record %d", index)
	}
	assert.False(t, startAt < 0, "start/closure/forced-abort order=%d/%d/%d", startAt, closedAt, forcedAbortAt)
	assert.False(t, closedAt <= startAt, "start/closure/forced-abort order=%d/%d/%d", startAt, closedAt, forcedAbortAt)
	assert.False(t, forcedAbortAt >= 0, "start/closure/forced-abort order=%d/%d/%d", startAt, closedAt, forcedAbortAt)
	replayed := ReplayLegal(explored.trace)
	assert.Nil(t, replayed.failure, "drain-expiry replay failure=%v", replayed.failure)
	assert.Equal(t, explored.world, replayed.world, "drain-expiry replay world diverged")

	var repeatedEmergency supervision.Fact
	var settledAt int
	for index, record := range explored.trace.records {
		if record.authority != supervisionAuthority {
			continue
		}
		if record.supervisorEvent.Kind() == supervision.EmergencyStartedFact {
			repeatedEmergency = record.supervisorEvent
		}
		if record.supervisorEvent.Kind() == supervision.EmergencySettlementCompletedFact {
			settledAt = index + 1
		}
	}
	assert.NotEqual(t, 0, repeatedEmergency.Kind(), "fatal trace lacks emergency start/settlement: start=%v settlement=%d", repeatedEmergency.Kind(), settledAt)
	assert.NotEqual(t, 0, settledAt, "fatal trace lacks emergency start/settlement: start=%v settlement=%d", repeatedEmergency.Kind(), settledAt)

	malformed := simulationMalformedFact{
		authority:  supervisionAuthority,
		supervisor: repeatedEmergency,
	}
	firstViolation := ReplayViolation(explored.trace, malformed)
	secondViolation := ReplayViolation(explored.trace, malformed)
	assert.Nil(t, firstViolation.failure, "repeated closure/invariant dominance first=%#v second=%#v", firstViolation, secondViolation)
	assert.Equal(t, "reduce supervisor", firstViolation.invariant.Operation(), "repeated closure/invariant dominance first=%#v second=%#v", firstViolation, secondViolation)
	assert.EqualValues(t, "emergency epoch is invalid, duplicated, or conflicting", firstViolation.invariant.Reason(), "repeated closure/invariant dominance first=%#v second=%#v", firstViolation, secondViolation)
	assert.Equal(t, secondViolation, firstViolation, "repeated closure/invariant dominance first=%#v second=%#v", firstViolation, secondViolation)
	assert.Equal(t, explored.world.campaign.Outcome(), firstViolation.world.campaign.Outcome(), "repeated closure/invariant dominance first=%#v second=%#v", firstViolation, secondViolation)
	assert.Equal(t, explored.world.campaign.Failed(), firstViolation.world.campaign.Failed(), "repeated closure/invariant dominance first=%#v second=%#v", firstViolation, secondViolation)
}

func TestSimulationChoiceSourceSelectsEnabledPeerSettlementOrder(t *testing.T) {
	definition := simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-choice-order", Lineage: 251, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 2,
		},
		capacity: 2, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}
	explorePreferred := func(preferred mutantIdentity) SimulationResult {
		return Explore(definition, simulationFocusedChoiceSource(func(moves []simulationEngineMove) int {
			for index, move := range moves {
				if simulationMaterializesWorkspace(move.effect) && move.effect.Mutant() == preferred {
					return index
				}
			}

			return 0
		}))
	}
	first := explorePreferred("mutant-a")
	second := explorePreferred("mutant-b")
	assert.Nil(t, first.failure, "choice exploration failures=%v/%v", first.failure, second.failure)
	assert.Nil(t, second.failure, "choice exploration failures=%v/%v", first.failure, second.failure)
	terminalOrder := func(trace simulationTrace) []attemptIdentity {
		var attempts []attemptIdentity
		for _, record := range trace.records {
			if record.authority != simulationCampaignAuthority {
				continue
			}
			fact := record.campaignEvent.Fact()
			if fact.IsAttemptTerminal() {
				attempts = append(attempts, fact.Attempt())
			}
		}

		return attempts[1:]
	}
	firstOrder := terminalOrder(first.trace)
	secondOrder := terminalOrder(second.trace)
	assert.NotEqual(t, secondOrder, firstOrder, "distinct choice streams selected the same primary order: %v; first=%#v second=%#v", firstOrder, first.trace.choices, second.trace.choices)
	for _, explored := range []SimulationResult{first, second} {
		{
			replayed := ReplayLegal(explored.trace)
			assert.Nil(t, replayed.failure, "choice-selected trace did not replay: %v", replayed.failure)
		}
	}
}

func TestSimulationReplayRequiresExactReleasedResource(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-resource", Lineage: 26, Command: []string{"test"},
			Profile: SerialProfile, Peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}, simulationChoiceBytes{0})
	require.Nil(t, explored.failure, "resource exploration failed: %v", explored.failure)
	trace := explored.trace
	trace.records = slices.Clone(trace.records)
	for index, record := range trace.records {
		if record.authority != simulationCampaignAuthority {
			continue
		}
		settled := record.campaignEvent.Fact()
		if !settled.IsResourceSettled() || settled.ResourceKind() != campaignmodule.WorkspaceResource {
			continue
		}
		trace.records[index].campaignEvent = record.campaignEvent.WithFact(settled.WithResourceIdentity("workspace-not-released"))
		break
	}

	replayed := ReplayLegal(trace)
	require.NotNil(t, replayed.failure, "wrong resource replay failure=%v, want exact resource rejection", replayed.failure)
	assert.Contains(t, replayed.failure.Error(), "external campaign fact is not enabled", "wrong resource replay failure=%v, want exact resource rejection", replayed.failure)
}

func TestSimulationViolationReplayCleansRuntimeAndRetainsTypedInvariant(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-violation", Lineage: 31, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 1,
		},
		capacity: 1,
	}, nil)
	prefix := simulationTrace{
		definition: explored.trace.definition,
		records:    append([]simulationRecord(nil), explored.trace.records[:2]...),
	}
	malformed := simulationMalformedFact{
		authority: simulationCampaignAuthority,
		campaign:  campaignmodule.SnapshotEstablished(""),
	}

	first := ReplayViolation(prefix, malformed)
	second := ReplayViolation(prefix, malformed)
	assert.Nil(t, first.failure, "violation replay failures=%v/%v", first.failure, second.failure)
	assert.Nil(t, second.failure, "violation replay failures=%v/%v", first.failure, second.failure)
	assert.Equal(t, second, first, "violation replay is nondeterministic:\nfirst=%#v\nsecond=%#v", first, second)
	assert.EqualValues(t, "campaign establish snapshot", first.invariant.Operation(), "retained invariant=%#v", first.invariant)
	assert.EqualValues(t, "snapshot observation is invalid", first.invariant.Reason(), "retained invariant=%#v", first.invariant)
	assert.Equal(t, simulationCampaignAuthority, first.key.authority, "failure key=%#v, invariant=%#v", first.key, first.invariant)
	assert.Equal(t, first.invariant.Operation(), first.key.operation, "failure key=%#v, invariant=%#v", first.key, first.invariant)
	assert.Equal(t, first.invariant.Reason(), first.key.reason, "failure key=%#v, invariant=%#v", first.key, first.invariant)
	assert.True(t, first.world.runtime.Drained(), "runtime cleanup=%#v", first.world.runtime)
	assert.NotEqual(t, 0, first.world.runtime.FatalEpoch(), "runtime cleanup=%#v", first.world.runtime)
	assert.EqualValues(t, 1, first.world.runtime.FatalCauseCount(), "runtime cleanup=%#v", first.world.runtime)
}

func TestSimulationViolationReplayRejectsWrongSupervisorActionAndCleansCustody(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-supervisor-violation", Lineage: 32, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}, simulationChoiceBytes{simulationChooseBaselineFailure})
	prefixLength := 0
	var registered supervision.Fact
	for index, record := range explored.trace.records {
		if record.authority == supervisionAuthority &&
			record.supervisorEvent.Kind() == supervision.ProspectiveRegisteredFact {
			prefixLength = index + 1
			registered = record.supervisorEvent
			break
		}
	}
	prefix := simulationTrace{
		definition: explored.trace.definition,
		records:    append([]simulationRecord(nil), explored.trace.records[:prefixLength]...),
	}
	launchBy, _, _ := registered.LaunchEvidence()
	completedAt := launchBy.Add(-time.Nanosecond)
	malformed := simulationMalformedFact{
		authority:  supervisionAuthority,
		supervisor: registered.LaunchCompletionWithAction(999, completedAt),
	}

	first := ReplayViolation(prefix, malformed)
	second := ReplayViolation(prefix, malformed)
	assert.Nil(t, first.failure, "supervisor violation replay diverged: first=%#v second=%#v", first, second)
	assert.Equal(t, second, first, "supervisor violation replay diverged: first=%#v second=%#v", first, second)
	assert.Equal(t, supervisionAuthority, first.key.authority, "supervisor invariant/key=%#v/%#v", first.invariant, first.key)
	assert.Equal(t, "reduce supervisor", first.invariant.Operation(), "supervisor invariant/key=%#v/%#v", first.invariant, first.key)
	residual := first.world.runtime.Residual()
	assert.True(t, first.world.runtime.Unconfirmed(), "supervisor violation cleanup did not transfer exact custody: %#v", first.world.runtime)
	require.Len(t, residual, 1, "supervisor violation cleanup did not transfer exact custody: %#v", first.world.runtime)
	assert.Equal(t, registered.Generation(), residual[0].Generation(), "supervisor violation cleanup did not transfer exact custody: %#v", first.world.runtime)
	assert.True(t, residual[0].Transferred(), "supervisor violation cleanup did not transfer exact custody: %#v", first.world.runtime)
}

func TestSimulationViolationReplayCoversNamedSupervisorCorruptions(t *testing.T) {
	choices := simulationFocusedChoiceSource(func(moves []simulationEngineMove) int {
		for index, move := range moves {
			if move.action.Kind() == supervision.LaunchNativeEffect &&
				move.variant.launch == simulationLaunchAtBoundary {
				return index
			}
		}
		for index, move := range moves {
			if move.action.Kind() != supervision.WaitRootEffect {
				return index
			}
		}

		return 0
	})
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-supervisor-corruption-families", Lineage: 321, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}, choices)
	assert.Nil(t, explored.failure, "supervisor corruption exploration failure=%v", explored.failure)
	registeredAt, boundaryAt := -1, -1
	var registered simulationRecord
	for index, record := range explored.trace.records {
		if record.authority != supervisionAuthority {
			continue
		}
		if registeredAt < 0 && record.supervisorEvent.Kind() == supervision.ProspectiveRegisteredFact {
			registeredAt, registered = index, record
		}
		if registeredAt >= 0 && record.supervisorEvent.Kind() == supervision.LaunchBoundaryFact &&
			record.supervisorEvent.Generation() == registered.supervisorEvent.Generation() {
			boundaryAt = index
			break
		}
	}
	assert.False(t, registeredAt < 0, "supervisor corruption cuts registration/boundary=%d/%d", registeredAt, boundaryAt)
	assert.False(t, boundaryAt < 0, "supervisor corruption cuts registration/boundary=%d/%d", registeredAt, boundaryAt)
	launchAction := registered.supervisorActions[0].Token()
	registrationPrefix := simulationTrace{
		definition: explored.trace.definition,
		records:    slices.Clone(explored.trace.records[:registeredAt+1]),
	}
	boundaryPrefix := simulationTrace{
		definition: explored.trace.definition,
		records:    slices.Clone(explored.trace.records[:boundaryAt+1]),
	}
	registeredEvent := registered.supervisorEvent
	launchBy, _, _ := registeredEvent.LaunchEvidence()
	completionAt := launchBy.Add(-time.Nanosecond)
	tests := []struct {
		name      string
		prefix    simulationTrace
		malformed supervision.Fact
		reason    string
	}{
		{
			name:   "duplicate registration",
			prefix: registrationPrefix, malformed: registeredEvent,
			reason: "prospective registration is incomplete or duplicated",
		},
		{
			name:      "wrong event kind",
			prefix:    registrationPrefix,
			malformed: registeredEvent.MalformedWithoutKind(completionAt),
			reason:    "event kind is invalid",
		},
		{
			name:      "contradictory released completion",
			prefix:    registrationPrefix,
			malformed: registeredEvent.MalformedContradictoryRelease(launchAction, completionAt),
			reason:    "released completion carries a launch failure",
		},
		{
			name:      "output completion in launch phase",
			prefix:    registrationPrefix,
			malformed: registeredEvent.MalformedOutputCompletion(launchAction, completionAt),
			reason:    "output completion correlation, evidence, or shape is invalid",
		},
		{
			name:      "late release after boundary revocation",
			prefix:    boundaryPrefix,
			malformed: registeredEvent.LaunchCompletionWithAction(launchAction, launchBy.Add(time.Nanosecond)),
			reason:    "launch completion was duplicated after closure",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			malformed := simulationMalformedFact{
				authority:  supervisionAuthority,
				supervisor: test.malformed,
			}
			first := ReplayViolation(test.prefix, malformed)
			second := ReplayViolation(test.prefix, malformed)
			assert.Nil(t, first.failure, "supervisor corruption replay diverged: first=%#v second=%#v", first, second)
			assert.Equal(t, second, first, "supervisor corruption replay diverged: first=%#v second=%#v", first, second)
			assert.Equal(t, "reduce supervisor", first.invariant.Operation(), "supervisor corruption invariant=%#v", first.invariant)
			assert.Equal(t, test.reason, first.invariant.Reason(), "supervisor corruption invariant=%#v", first.invariant)
		})
	}
}

func TestSimulationViolationReplayRejectsMalformedRuntimeAdmissionAndCleansCustody(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-runtime-violation", Lineage: 33, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 1,
		},
		capacity: 1,
	}, nil)
	prefix := simulationTrace{
		definition: explored.trace.definition,
		records:    slices.Clone(explored.trace.records[:1]),
	}
	malformed := simulationMalformedFact{
		authority:  simulationRuntimeAuthority,
		runtimeCut: processruntime.RequestAdmissionCut(processruntime.Admission{}),
	}

	first := ReplayViolation(prefix, malformed)
	second := ReplayViolation(prefix, malformed)
	assert.Nil(t, first.failure, "runtime violation replay diverged: first=%#v second=%#v", first, second)
	assert.Equal(t, second, first, "runtime violation replay diverged: first=%#v second=%#v", first, second)
	assert.Equal(t, simulationRuntimeAuthority, first.key.authority, "runtime invariant/key=%#v/%#v", first.invariant, first.key)
	assert.EqualValues(t, "request admission", first.invariant.Operation(), "runtime invariant/key=%#v/%#v", first.invariant, first.key)
	assert.EqualValues(t, "invalid request", first.invariant.Reason(), "runtime invariant/key=%#v/%#v", first.invariant, first.key)
	assert.True(t, first.world.runtime.Drained(), "runtime violation cleanup retained custody: %#v", first.world.runtime)
	assert.EqualValues(t, 0, first.world.runtime.AdmissionCount(), "runtime violation cleanup retained custody: %#v", first.world.runtime)
}

func TestSimulationViolationReplayRejectsStaleGrantReturnAndCleansRuntime(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-runtime-return-violation", Lineage: 34, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 1,
		},
		capacity: 1,
	}, nil)
	prefix := simulationTrace{
		definition: explored.trace.definition,
		records:    slices.Clone(explored.trace.records[:2]),
	}
	malformed := simulationMalformedFact{
		authority:  simulationRuntimeAuthority,
		runtimeCut: processruntime.ReturnGrantCut(processruntime.Admission{}),
	}

	first := ReplayViolation(prefix, malformed)
	second := ReplayViolation(prefix, malformed)
	assert.Nil(t, first.failure, "stale return replay diverged: first=%#v second=%#v", first, second)
	assert.Equal(t, second, first, "stale return replay diverged: first=%#v second=%#v", first, second)
	assert.Equal(t, simulationRuntimeAuthority, first.key.authority, "stale return invariant/key=%#v/%#v", first.invariant, first.key)
	assert.EqualValues(t, "acknowledge grant return", first.invariant.Operation(), "stale return invariant/key=%#v/%#v", first.invariant, first.key)
	assert.EqualValues(t, "grant return authority is stale or wrong", first.invariant.Reason(), "stale return invariant/key=%#v/%#v", first.invariant, first.key)
	assert.True(t, first.world.runtime.Drained(), "stale return cleanup=%#v", first.world.runtime)
	assert.EqualValues(t, 0, first.world.runtime.AdmissionCount(), "stale return cleanup=%#v", first.world.runtime)
}

func TestSimulationViolationReplayCoversRuntimeObservationEmergencyAndClosureFamilies(t *testing.T) {
	prefix := simulationTrace{definition: simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-runtime-families", Lineage: 35, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 1,
		},
		capacity: 1,
	}}
	tests := []struct {
		name      string
		malformed simulationMalformedFact
		operation string
		reason    string
	}{
		{
			name: "unknown generation observation",
			malformed: simulationMalformedFact{
				authority:  simulationRuntimeAuthority,
				runtimeCut: processruntime.ObserveAttemptCut(99, processruntime.Owned()),
			},
			operation: observeOperation, reason: "generation is not live",
		},
		{
			name: "emergency settlement while open",
			malformed: simulationMalformedFact{
				authority:  simulationRuntimeAuthority,
				runtimeCut: processruntime.SettleEmergencyCut(nil),
			},
			operation: settleEmergencyOperation, reason: "resolution cardinality is invalid",
		},
		{
			name: "empty fatal cause",
			malformed: simulationMalformedFact{
				authority:  simulationRuntimeAuthority,
				runtimeCut: processruntime.CloseCut(""),
			},
			operation: "close runtime", reason: "fatal cause is empty",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first := ReplayViolation(prefix, test.malformed)
			second := ReplayViolation(prefix, test.malformed)
			assert.Nil(t, first.failure, "runtime violation replay diverged: first=%#v second=%#v", first, second)
			assert.Equal(t, second, first, "runtime violation replay diverged: first=%#v second=%#v", first, second)
			assert.Equal(t, simulationRuntimeAuthority, first.key.authority, "runtime invariant/key=%#v/%#v", first.invariant, first.key)
			assert.Equal(t, test.operation, first.invariant.Operation(), "runtime invariant/key=%#v/%#v", first.invariant, first.key)
			assert.Equal(t, test.reason, first.invariant.Reason(), "runtime invariant/key=%#v/%#v", first.invariant, first.key)
			assert.True(t, first.world.runtime.Drained(), "runtime violation cleanup=%#v", first.world.runtime)
			assert.EqualValues(t, 0, first.world.runtime.AdmissionCount(), "runtime violation cleanup=%#v", first.world.runtime)
		})
	}
}

func TestSimulationShrinkRemovesLegalRecordsAndDefinitionMembersToFixpoint(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-shrink", Lineage: 41, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 4,
		},
		capacity:  4,
		catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, simulationChoiceBytes{simulationChooseBaselineFailure})
	malformed := simulationMalformedFact{
		authority: simulationCampaignAuthority,
		campaign:  campaignmodule.SnapshotEstablished(""),
	}
	counterexample := simulationTrace{
		definition: explored.trace.definition,
		records:    append([]simulationRecord(nil), explored.trace.records[:4]...),
		malformed:  &malformed,
	}
	key := ReplayViolation(counterexample, malformed).key

	shrunk := Shrink(counterexample, key)
	assert.False(t, len(shrunk.records) >= len(counterexample.records), "record count was not reduced: got=%d input=%d", len(shrunk.records), len(counterexample.records))
	assert.EqualValues(t, 0, len(shrunk.definition.catalogue), "shrunk catalogue=%v, want no unrelated members", shrunk.definition.catalogue)
	assert.EqualValues(t, 1, shrunk.definition.capacity, "shrunk capacity/peers=%d/%d, want accepted lower bounds 1/1", shrunk.definition.capacity, shrunk.definition.campaign.Peers)
	assert.EqualValues(t, 1, shrunk.definition.campaign.Peers, "shrunk capacity/peers=%d/%d, want accepted lower bounds 1/1", shrunk.definition.capacity, shrunk.definition.campaign.Peers)
	require.NotNil(t, shrunk.malformed, "shrink removed the one intended corruption")
	first := ReplayViolation(shrunk, *shrunk.malformed)
	second := ReplayViolation(shrunk, *shrunk.malformed)
	assert.Nil(t, first.failure, "shrunk replay did not retain stable failure:\nkey=%#v\nfirst=%#v\nsecond=%#v", key, first, second)
	assert.Equal(t, key, first.key, "shrunk replay did not retain stable failure:\nkey=%#v\nfirst=%#v\nsecond=%#v", key, first, second)
	assert.Equal(t, second, first, "shrunk replay did not retain stable failure:\nkey=%#v\nfirst=%#v\nsecond=%#v", key, first, second)
}

func TestSimulationShrinkRemovesPositiveTraceSuffixAndRetainsReplayFailure(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-positive-shrink", Lineage: 46, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 3,
		},
		capacity: 3, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, nil)
	assert.Nil(t, explored.failure, "positive shrink exploration failure=%v", explored.failure)
	counterexample := simulationCloneTrace(explored.trace)
	counterexample.records[0].runtimeState = processruntime.NewReplay(counterexample.records[0].runtimeState.Capacity() + 1).Projection()
	replayed := ReplayLegal(counterexample)
	assert.NotNil(t, replayed.failure, "positive replay failure/key=%v/%#v", replayed.failure, replayed.key)
	assert.NotEqual(t, FailureKey{}, replayed.key, "positive replay failure/key=%v/%#v", replayed.failure, replayed.key)

	shrunk := Shrink(counterexample, replayed.key)
	assert.False(t, len(shrunk.records) >= len(counterexample.records), "positive record count was not reduced: got=%d input=%d", len(shrunk.records), len(counterexample.records))
	assert.EqualValues(t, 1, shrunk.definition.capacity, "positive shrunk definition=%#v", shrunk.definition)
	assert.EqualValues(t, 1, shrunk.definition.campaign.Peers, "positive shrunk definition=%#v", shrunk.definition)
	assert.EqualValues(t, 0, len(shrunk.definition.catalogue), "positive shrunk definition=%#v", shrunk.definition)
	assert.EqualValues(t, "campaign-1", shrunk.definition.campaign.Identity, "positive shrunk definition=%#v", shrunk.definition)
	assert.EqualValues(t, 1, shrunk.definition.campaign.Lineage, "positive shrunk definition=%#v", shrunk.definition)
	first := ReplayLegal(shrunk)
	second := ReplayLegal(shrunk)
	assert.NotNil(t, first.failure, "positive shrunk replay did not retain stable failure:\nkey=%#v\nfirst=%#v\nsecond=%#v", replayed.key, first, second)
	assert.Equal(t, replayed.key, first.key, "positive shrunk replay did not retain stable failure:\nkey=%#v\nfirst=%#v\nsecond=%#v", replayed.key, first, second)
	assert.Equal(t, second, first, "positive shrunk replay did not retain stable failure:\nkey=%#v\nfirst=%#v\nsecond=%#v", replayed.key, first, second)
}

func TestSimulationShrinkMovesPositiveReplayTowardNamedBoundary(t *testing.T) {
	choices := simulationFocusedChoiceSource(func(moves []simulationEngineMove) int {
		for index, move := range moves {
			if move.action.Kind() == supervision.LaunchNativeEffect && move.attemptKind == campaignAttemptPrimary &&
				move.variant.launch == simulationLaunchAfterBoundary {
				return index
			}
		}
		for index, move := range moves {
			if move.action.Kind() != supervision.WaitRootEffect {
				return index
			}
		}

		return 0
	})
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-positive-boundary", Lineage: 47, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}, choices)
	assert.Nil(t, explored.failure, "positive boundary exploration failure=%v", explored.failure)
	counterexample := simulationCloneTrace(explored.trace)
	cut := -1
	for index, record := range counterexample.records {
		if record.authority == supervisionAuthority &&
			record.supervisorEvent.Kind() == supervision.LaunchCompletedFact {
			cut = index
			break
		}
	}
	require.False(t, cut < 0, "positive boundary trace has no equality cut")
	counterexample.records = slices.Clone(counterexample.records[:cut+1])
	counterexample.records[cut].supervisorActions = append(
		counterexample.records[cut].supervisorActions,
		supervision.MalformedEffect(supervision.LaunchNativeEffect),
	)
	replayed := ReplayLegal(counterexample)
	assert.NotNil(t, replayed.failure, "positive boundary replay=%#v", replayed)
	assert.Equal(t, simulationReplayFailureKind, replayed.key.kind, "positive boundary replay=%#v", replayed)
	originalMeasure := simulationTraceShrinkMeasure(counterexample)

	shrunk := Shrink(counterexample, replayed.key)
	{
		measure := simulationTraceShrinkMeasure(shrunk)
		assert.True(t, simulationShrinkMeasureLess(measure, originalMeasure), "positive boundary measure=%v, want less than %v", measure, originalMeasure)
	}
	first := ReplayLegal(shrunk)
	second := ReplayLegal(shrunk)
	assert.NotNil(t, first.failure, "positive boundary shrunk replay diverged: first=%#v second=%#v", first, second)
	assert.Equal(t, replayed.key, first.key, "positive boundary shrunk replay diverged: first=%#v second=%#v", first, second)
	assert.Equal(t, second, first, "positive boundary shrunk replay diverged: first=%#v second=%#v", first, second)
}

func TestSimulationShrinkMeasureUsesPayloadAndNamedBoundaryFacts(t *testing.T) {
	deadline := time.Unix(10, 0)
	near := simulationTrace{
		records: []simulationRecord{{
			authority:       supervisionAuthority,
			supervisorEvent: supervision.RunningBoundaryFact(deadline, time.Unix(10, 1)),
		}},
		choices: []simulationChoiceRecord{{selected: 9}},
	}
	far := simulationCloneTrace(near)
	far.records[0].supervisorEvent = far.records[0].supervisorEvent.WithRootExitAt(time.Unix(10, 9))
	far.choices[0].selected = 0
	assert.True(t, simulationShrinkMeasureLess(simulationTraceShrinkMeasure(near), simulationTraceShrinkMeasure(far)), "near/far shrink measures=%v/%v", simulationTraceShrinkMeasure(near), simulationTraceShrinkMeasure(far))

	simple := simulationCloneTrace(near)
	simple.records[0].supervisorEvent = simple.records[0].supervisorEvent.WithoutRunningEvidence()
	assert.True(t, simulationShrinkMeasureLess(simulationTraceShrinkMeasure(simple), simulationTraceShrinkMeasure(near)), "simple/rich payload measures=%v/%v", simulationTraceShrinkMeasure(simple), simulationTraceShrinkMeasure(near))

	uncanonical := simulationTrace{definition: simulationDefinition{
		campaign: campaignmodule.Definition{Identity: "a", Lineage: 1, Peers: 1}, capacity: 1,
	}}
	canonical := simulationCloneTrace(uncanonical)
	canonical.definition.campaign.Identity = "campaign-1"
	assert.True(t, simulationShrinkMeasureLess(
		simulationTraceShrinkMeasure(canonical), simulationTraceShrinkMeasure(uncanonical),
	), "canonical/short identity measures=%v/%v", simulationTraceShrinkMeasure(canonical), simulationTraceShrinkMeasure(uncanonical))
}

func TestSimulationShrinkRetainsTypedReplayDivergenceIndependentOfDiagnostic(t *testing.T) {
	candidate := simulationRecord{authority: simulationRuntimeAuthority}
	failing := simulationRecord{
		authority:    simulationRuntimeAuthority,
		runtimeState: processruntime.NewReplay(3).Projection(),
	}
	firstKey := simulationReplayDivergenceFailure(
		simulationTrace{}, simulationRuntimeStateDivergence, "rewritten diagnostic",
	).key
	secondKey := simulationReplayDivergenceFailure(
		simulationTrace{}, simulationRuntimeStateDivergence, "another diagnostic: %d", 3,
	).key
	assert.Equal(t, secondKey, firstKey, "typed replay keys depend on diagnostics: %#v/%#v", firstKey, secondKey)
	retained := simulationRetainRecordedFailure(candidate, failing, firstKey.divergence)
	assert.EqualValues(t, 3, retained.runtimeState.Capacity(), "typed replay divergence retained state=%#v", retained.runtimeState)
}

func TestSimulationShrinkRemovesCatalogueMembersWithTheirCausalRecords(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-shrink-causal", Lineage: 42, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 2,
		},
		capacity: 2, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, nil)
	assert.Nil(t, explored.failure, "causal shrink exploration failure=%v", explored.failure)
	malformed := simulationMalformedFact{
		authority: simulationCampaignAuthority,
		campaign:  campaignmodule.SnapshotEstablished(""),
	}
	counterexample := simulationCloneTrace(explored.trace)
	counterexample.malformed = &malformed
	key := ReplayViolation(counterexample, malformed).key

	shrunk := Shrink(counterexample, key)
	assert.EqualValues(t, 0, len(shrunk.definition.catalogue), "causal shrink catalogue=%v, want no unrelated mutants", shrunk.definition.catalogue)
	assert.False(t, len(shrunk.records) >= len(counterexample.records), "causal shrink records=%d, want fewer than %d", len(shrunk.records), len(counterexample.records))
	assert.EqualValues(t, 1, shrunk.definition.capacity, "causal shrink capacity/peers=%d/%d, want 1/1", shrunk.definition.capacity, shrunk.definition.campaign.Peers)
	assert.EqualValues(t, 1, shrunk.definition.campaign.Peers, "causal shrink capacity/peers=%d/%d, want 1/1", shrunk.definition.capacity, shrunk.definition.campaign.Peers)
	replayed := ReplayViolation(shrunk, *shrunk.malformed)
	assert.Nil(t, replayed.failure, "causal shrink replay=%#v, want key %#v", replayed, key)
	assert.Equal(t, key, replayed.key, "causal shrink replay=%#v, want key %#v", replayed, key)
}

func TestSimulationReplayLegalityKeyIsIndependentOfDiagnostic(t *testing.T) {
	trace := simulationTrace{records: []simulationRecord{{authority: simulationRuntimeAuthority}}}
	first := simulationReplayFailure(
		trace, simulationReplayEnablednessFailure, "registration is not enabled at record %d", 0,
	).key
	second := simulationReplayFailure(
		trace, simulationReplayEnablednessFailure, "rewritten enabledness diagnostic for record %d", 0,
	).key
	assert.Equal(t, second, first, "replay legality key depends on diagnostics: %#v/%#v", first, second)
	assert.EqualValues(t, "", first.operation, "replay legality key depends on diagnostics: %#v/%#v", first, second)
	different := simulationReplayFailure(
		trace, simulationReplayCausalityFailure, "rewritten causal diagnostic for record %d", 0,
	).key
	assert.NotEqual(t, different, first, "distinct replay legality categories share key %#v", first)
}

func TestSimulationShrinkMovesChoicesTowardNamedBoundaries(t *testing.T) {
	choices := simulationFocusedChoiceSource(func(moves []simulationEngineMove) int {
		for index, move := range moves {
			if move.action.Kind() == supervision.LaunchNativeEffect && move.attemptKind == campaignAttemptPrimary &&
				move.variant.launch == simulationLaunchAfterBoundary {
				return index
			}
		}
		for index, move := range moves {
			if move.action.Kind() != supervision.WaitRootEffect {
				return index
			}
		}

		return 0
	})
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-shrink-boundary", Lineage: 45, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}, choices)
	assert.Nil(t, explored.failure, "boundary shrink exploration failure=%v", explored.failure)
	prefixLength := 0
	for index, record := range explored.trace.records {
		if record.authority != supervisionAuthority ||
			record.supervisorEvent.Kind() != supervision.LaunchCompletedFact {
			continue
		}
		prefixLength = index + 1
		break
	}
	assert.NotEqual(t, 0, prefixLength, "boundary shrink trace has no launch completion")
	malformed := simulationMalformedFact{
		authority: simulationCampaignAuthority,
		campaign:  campaignmodule.SnapshotEstablished(""),
	}
	counterexample := simulationCloneTrace(explored.trace)
	counterexample.records = slices.Clone(counterexample.records[:prefixLength])
	counterexample.malformed = &malformed
	key := ReplayViolation(counterexample, malformed).key
	originalMeasure := simulationTraceShrinkMeasure(counterexample)

	shrunk := Shrink(counterexample, key)
	{
		measure := simulationTraceShrinkMeasure(shrunk)
		assert.True(t, simulationShrinkMeasureLess(measure, originalMeasure), "boundary measure=%v, want less than %v", measure, originalMeasure)
	}
	assert.EqualValues(t, "campaign-1", shrunk.definition.campaign.Identity, "canonical shrink definition=%#v", shrunk.definition)
	assert.EqualValues(t, 1, shrunk.definition.campaign.Lineage, "canonical shrink definition=%#v", shrunk.definition)
	assert.EqualValues(t, 0, len(shrunk.definition.catalogue), "canonical shrink definition=%#v", shrunk.definition)
	replayed := ReplayViolation(shrunk, *shrunk.malformed)
	assert.Nil(t, replayed.failure, "boundary shrink replay=%#v, want key %#v", replayed, key)
	assert.Equal(t, key, replayed.key, "boundary shrink replay=%#v, want key %#v", replayed, key)
}

func TestSimulationLivenessFailureKeyIgnoresRawOwnerIdentities(t *testing.T) {
	first := simulationLivenessResult(simulationEngine{pending: []simulationEngineMove{
		{source: simulationCausalSource{kind: simulationCampaignEffectSource, identity: 71}},
		{source: simulationCausalSource{kind: supervisionActionSource, identity: 93}},
	}}, simulationLivenessNoMove)
	second := simulationLivenessResult(simulationEngine{pending: []simulationEngineMove{
		{source: simulationCausalSource{kind: simulationCampaignEffectSource, identity: 4}},
		{source: simulationCausalSource{kind: supervisionActionSource, identity: 5}},
	}}, simulationLivenessNoMove)

	assert.NotNil(t, first.failure, "liveness failures/keys diverged: first=%#v second=%#v", first, second)
	assert.NotNil(t, second.failure, "liveness failures/keys diverged: first=%#v second=%#v", first, second)
	assert.Equal(t, second.key, first.key, "liveness failures/keys diverged: first=%#v second=%#v", first, second)
	assert.Equal(t, simulationLivenessFailureKind, first.key.kind, "liveness failure key=%#v", first.key)
	assert.Equal(t, simulationLivenessNoMove, first.key.liveness, "liveness failure key=%#v", first.key)
}

func TestSimulationLivenessShrinkUsesSameFailureEvaluatorToFixpoint(t *testing.T) {
	key := FailureKey{
		property: "Explore", kind: simulationLivenessFailureKind,
		liveness: simulationLivenessRepeatedWorld, identities: []string{"owner-source-1#1"},
	}
	trace := simulationTrace{
		definition: simulationDefinition{
			campaign: campaignmodule.Definition{
				Identity: "campaign-liveness-shrink", Lineage: 49, Command: []string{"test"},
				Profile: AutomaticProfile, Peers: 3,
			},
			capacity: 3, catalogue: []mutantIdentity{"unrelated", "required"},
		},
		choices: []simulationChoiceRecord{{limit: 4, selected: 3}},
	}
	evaluate := func(definition simulationDefinition, choices simulationChoiceSource) SimulationResult {
		candidate := simulationCloneTrace(trace)
		candidate.definition = definition
		if source, ok := choices.(*simulationShrinkChoiceSource); ok {
			candidate.choices = slices.Clone(source.choices)
		}
		if len(definition.catalogue) < len(trace.definition.catalogue) &&
			definition.capacity == trace.definition.capacity {
			return SimulationResult{trace: candidate}
		}

		return SimulationResult{
			trace: candidate, key: key,
			failure: simulationLivenessFailure{kind: simulationLivenessRepeatedWorld},
		}
	}

	shrunk := simulationShrinkLivenessWith(trace, key, evaluate)
	assert.EqualValues(t, 1, shrunk.definition.capacity, "liveness shrunk definition=%#v", shrunk.definition)
	assert.EqualValues(t, 1, shrunk.definition.campaign.Peers, "liveness shrunk definition=%#v", shrunk.definition)
	assert.EqualValues(t, 0, len(shrunk.definition.catalogue), "liveness shrunk definition=%#v", shrunk.definition)
	assert.EqualValues(t, "campaign-1", shrunk.definition.campaign.Identity, "liveness shrunk definition=%#v", shrunk.definition)
	assert.EqualValues(t, 1, shrunk.definition.campaign.Lineage, "liveness shrunk definition=%#v", shrunk.definition)
	assert.EqualValues(t, 0, len(shrunk.choices), "liveness shrunk choices=%#v", shrunk.choices)
}

func TestSimulationRecorderLinearizesProductionOwnerCutsAndQuiescentProjection(t *testing.T) {
	recorder := newSimulationRecorder()
	shell := processruntime.NewObserved(1, newSimulationRuntimeObserver(recorder, 1))
	definition := campaignmodule.Definition{
		Identity: "campaign-conformance", Lineage: 51, Command: []string{"test"},
		Profile: AutomaticProfile, Peers: 1,
	}
	campaign, _ := campaignmodule.NewMachine(definition)
	observer := simulationCampaignRecorder{recorder: recorder}
	machine := supervision.NewMachine()

	registration := shell.RegisterCampaign(definition.Lineage)
	_, campaignTransition := applyRecordedCampaign(observer, campaign, campaignmodule.Registered(registration))
	fact := supervision.ProspectiveRegistration(
		1, "attempt-a", time.Unix(100, 0), time.Unix(101, 0), AutomaticProfile, time.Second,
	)
	machine, transition := machine.Apply(fact)
	recorder.Publish(supervision.OwnerCutReservation(recorder.next.Add(1)), fact,
		transition.Event(), transition.Projection(), transition.Effects())
	for _, effect := range transition.Effects() {
		recorder.recordSupervisorEffect(effect)
	}

	trace, projection := recorder.quiescent(shell, machine)
	{
		got, want := simulationAuthorities(trace), []simulationAuthority{
			simulationRuntimeAuthority, simulationCampaignAuthority, supervisionAuthority,
		}
		assert.Equal(t, want, got, "production authority order=%v, want %v", got, want)
	}
	for index, record := range trace.records {
		assert.Equal(t, uint64(index+1), record.sequence, "production sequence at %d=%d", index, record.sequence)
	}

	wantRuntime := processruntime.NewReplay(1)
	wantRuntime, registrationResult := wantRuntime.Apply(processruntime.RegisterCampaignCut(definition.Lineage))
	wantCampaign, _ := campaignmodule.NewMachine(definition)
	var wantCampaignTransition campaignmodule.Transition
	wantCampaign, wantCampaignTransition = wantCampaign.Apply(campaignmodule.Registered(registrationResult.Registration()))
	wantCampaign = wantCampaign.Projection().Canonical().Fork()
	wantMachine, wantSupervisorTransition := supervision.NewMachine().Apply(fact)
	wantProjection := simulationWorld{
		campaign: wantCampaign, runtime: simulationTraceRuntimeState(wantRuntime),
		supervisor: wantMachine.Projection(), machine: wantMachine,
	}
	assert.Equal(t, wantProjection, projection, "production projection diverged:\n got=%#v\nwant=%#v", projection, wantProjection)
	assert.True(t, campaignTransition.Projection().Canonical().Equal(trace.records[1].campaignState))
	wantCampaignEffects := wantCampaignTransition.Effects()
	for index := range wantCampaignEffects {
		wantCampaignEffects[index] = wantCampaignEffects[index].Canonical(wantCampaignTransition.Projection())
	}
	assert.True(t, slices.EqualFunc(wantCampaignEffects, trace.records[1].campaignEffects,
		func(left, right campaignmodule.Effect) bool { return left.Equal(right) }))
	assert.Equal(t, wantSupervisorTransition.Effects(), trace.records[2].supervisorActions, "recorded ordered outputs diverged: %#v", trace.records)
}

func TestSimulationRecorderRejectsRuntimeDivergenceAtQuiescence(t *testing.T) {
	recorder := newSimulationRecorder()
	observer := newSimulationRuntimeObserver(recorder, 1)
	runtime := processruntime.NewObserved(2, observer)
	firstRegistration := runtime.RegisterCampaign(71)
	secondRegistration := runtime.RegisterCampaign(72)
	runtime.RequestAdmission(processruntime.Admission{
		Campaign: firstRegistration.Campaign(), Attempt: "first", Class: processruntime.SharedAdmission,
	})
	runtime.RequestAdmission(processruntime.Admission{
		Campaign: secondRegistration.Campaign(), Attempt: "second", Class: processruntime.SharedAdmission,
	})
	campaign, _ := campaignmodule.NewMachine(campaignmodule.Definition{
		Identity: "campaign-conformance", Lineage: 71, Command: []string{"test"},
		Profile: AutomaticProfile, Peers: 2,
	})
	_, _ = applyRecordedCampaign(simulationCampaignRecorder{recorder: recorder}, campaign,
		campaignmodule.Registered(firstRegistration))
	machine := supervision.NewMachine()

	assert.PanicsWithError(t, "process runtime event diverged", func() {
		recorder.quiescent(runtime, machine)
	})
}

func TestSimulationReplayChecksIndependentOwnerCutsAtQuiescence(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-commutation", Lineage: 510, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}, nil)
	assert.Nil(t, explored.failure, "commutation exploration failure=%v", explored.failure)
	trace := simulationCloneTrace(explored.trace)
	trace.barriers = []simulationQuiescentBarrier{{
		afterSequence: trace.records[len(trace.records)-1].sequence,
		campaign:      explored.world.campaign.Projection(),
		runtime:       explored.world.runtime,
		supervisor:    explored.world.supervisor,
	}}
	independent, causal := 0, 0
	for index := 0; index+1 < len(trace.records); index++ {
		left, right := trace.records[index], trace.records[index+1]
		if simulationRecordsAreIndependent(left, right) {
			independent++
		} else if left.authority != right.authority {
			causal++
		}
	}
	assert.NotEqual(t, 0, independent, "commutation trace pairs independent/causal=%d/%d", independent, causal)
	assert.NotEqual(t, 0, causal, "commutation trace pairs independent/causal=%d/%d", independent, causal)
	{
		replayed := ReplayLegal(trace)
		assert.Nil(t, replayed.failure, "quiescent commutation replay failure=%v", replayed.failure)
	}
}

func TestSimulationRecorderCorrelatesQueuedGrantWithItsRuntimeCut(t *testing.T) {
	recorder := newSimulationRecorder()
	shell := processruntime.NewObserved(1, newSimulationRuntimeObserver(recorder, 1))
	activeCampaign := shell.RegisterCampaign(512)
	waitingCampaign := shell.RegisterCampaign(513)
	active := shell.RequestAdmission(processruntime.Admission{
		Campaign: activeCampaign.Campaign(), Attempt: "active", Class: processruntime.SharedAdmission,
	})
	waiting := shell.RequestAdmission(processruntime.Admission{
		Campaign: waitingCampaign.Campaign(), Attempt: "waiting", Class: processruntime.SharedAdmission,
	})

	shell.CancelAdmission(active.Request())
	grant, _ := waiting.Receive()
	event := campaignmodule.AdmissionDelivered(grant.Admission())
	recorder.mutex.Lock()
	cancelSequence := recorder.records[len(recorder.records)-1].sequence
	recorder.mutex.Unlock()
	{
		source := recorder.campaignSource(event)
		assert.Equal(t, simulationOwnerDeliverySource, source.kind, "queued grant source=%#v, want cancellation cut %d", source, cancelSequence)
		assert.Equal(t, cancelSequence, source.identity, "queued grant source=%#v, want cancellation cut %d", source, cancelSequence)
	}
}

func TestSimulationRecorderCorrelatesRuntimeReceiptWithItsActionCut(t *testing.T) {
	findEffect := func(effects []supervision.Effect, kind supervision.EffectKind) supervision.Effect {
		t.Helper()
		for _, effect := range effects {
			if effect.Kind() == kind {
				return effect
			}
		}
		require.Failf(t, "effect not found", "kind %d in %v", kind, effects)

		return supervision.Effect{}
	}
	recorder := newSimulationRecorder()
	runtime := processruntime.NewReplay(1)
	var applied processruntime.ReplayResult
	runtime, applied = runtime.Apply(processruntime.RegisterCampaignCut(1))
	campaign := applied.Registration().Campaign()
	runtime, applied = runtime.Apply(processruntime.RequestAdmissionCut(processruntime.Admission{
		Campaign: campaign, Attempt: "attempt", Class: processruntime.SharedAdmission,
	}))
	grant := applied.Admission().Deliveries()[0]
	runtime, applied = runtime.Apply(processruntime.CommitStartCut(grant))
	generation := applied.Start().Generation()
	registeredAt := time.Unix(100, 0)
	machine, transition := supervision.NewMachine().Apply(supervision.ProspectiveRegistration(
		generation, "attempt", registeredAt, registeredAt.Add(time.Second),
		processruntime.AutomaticProfile, time.Minute,
	))
	launch := findEffect(transition.Effects(), supervision.LaunchNativeEffect)
	launchFacts, ok := machine.LaunchFacts(launch, supervision.LaunchReleasedBeforeBoundary)
	require.True(t, ok)
	require.Len(t, launchFacts, 1)
	machine, transition = machine.Apply(launchFacts[0])
	publish := findEffect(transition.Effects(), supervision.PublishOwnedEffect)
	wait := findEffect(transition.Effects(), supervision.WaitRootEffect)
	sample := findEffect(transition.Effects(), supervision.SampleRunningEffect)
	recorder.recordSupervisorEffects([]supervision.Effect{publish})
	launchReservation := recorder.reserve(simulationRuntimeAuthority)
	runtime, applied = runtime.Apply(processruntime.ObserveAttemptCut(generation, processruntime.Owned()))
	recorder.recordRuntime(launchReservation, simulationRecord{runtimeCut: applied.RecordedCut()}, runtime)
	recorder.recordSupervisorEffect(publish)

	running, ok := machine.RunningFact(wait, sample, supervision.RunningPassed)
	require.True(t, ok)
	machine, transition = machine.Apply(running)
	effect := findEffect(transition.Effects(), supervision.ObserveEmptinessEffect)
	for effect.Kind() != supervision.SettleRuntimeEffect {
		completed, found := machine.CompletionFact(effect, supervision.CompletionBeforeBoundary)
		require.True(t, found)
		machine, transition = machine.Apply(completed)
		effects := transition.Effects()
		require.Len(t, effects, 1)
		effect = effects[0]
	}
	settle := effect
	recorder.recordSupervisorEffects([]supervision.Effect{settle})
	terminalReservation := recorder.reserve(simulationRuntimeAuthority)
	runtime, applied = runtime.Apply(processruntime.ObserveAttemptCut(generation, processruntime.Settled(processruntime.AutomaticProfile, 0)))
	recorder.recordRuntime(terminalReservation, simulationRecord{runtimeCut: applied.RecordedCut()}, runtime)
	receipt, ok := machine.RuntimeReceiptFactFor(settle, applied.Receipt())
	require.True(t, ok)
	terminalSource := recorder.supervisorSource(receipt)
	assert.Equal(t, simulationOwnerDeliverySource, terminalSource.kind, "runtime receipt source=%#v", terminalSource)
	assert.Equal(t, terminalReservation.sequence, terminalSource.identity, "runtime receipt source=%#v", terminalSource)

	launchFact := simulationFactForGeneration(t, Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "launch-source", Lineage: 91, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}, nil).trace, campaignFactAttemptLaunched, generation)
	launchSource := recorder.campaignSource(launchFact)
	assert.Equal(t, simulationOwnerDeliverySource, launchSource.kind, "launch source=%#v", launchSource)
	assert.Equal(t, launchReservation.sequence, launchSource.identity, "launch source=%#v", launchSource)
}

func TestSimulationRecorderQuiescenceWaitsForInFlightActionCut(t *testing.T) {
	recorder := newSimulationRecorder()
	shell := processruntime.NewObserved(1, newSimulationRuntimeObserver(recorder, 1))
	registration := shell.RegisterCampaign(511)
	campaign, _ := campaignmodule.NewMachine(campaignmodule.Definition{
		Identity: "campaign-action-barrier", Lineage: 511, Command: []string{"test"},
		Profile: AutomaticProfile, Peers: 1,
	})
	_, _ = applyRecordedCampaign(simulationCampaignRecorder{recorder: recorder}, campaign,
		campaignmodule.Registered(registration))
	machine := supervision.NewMachine()
	action := supervision.MalformedEffect(supervision.LaunchNativeEffect)
	recorder.actions[71] = simulationInFlightAction{kind: supervision.LaunchNativeEffect, generation: 9}

	started := make(chan struct{})
	completed := make(chan struct{})
	go func() {
		close(started)
		recorder.quiescent(shell, machine)
		close(completed)
	}()
	<-started
	select {
	case <-completed:
		require.FailNow(t, "quiescence returned with a supervisor action still in flight")
	case <-time.After(20 * time.Millisecond):
	}

	recorder.recordSupervisorCompletion(action.Kind(), 71)
	select {
	case <-completed:
	case <-time.After(time.Second):
		require.FailNow(t, "quiescence did not resume after the matching action cut")
	}
}

func TestSimulationRecorderReplaysAnEmptyProductionCampaign(t *testing.T) {
	recorder := newSimulationRecorder()
	shell := processruntime.NewObserved(1, newSimulationRuntimeObserver(recorder, 1))
	definition := campaignmodule.Definition{
		Identity: "campaign-recorded-replay", Lineage: 52, Command: []string{"test"},
		Profile: AutomaticProfile, Peers: 1,
	}
	campaign, _ := campaignmodule.NewMachine(definition)
	observer := simulationCampaignRecorder{recorder: recorder}
	machine := supervision.NewMachine()
	cut := func() { _, _ = recorder.quiescent(shell, machine) }

	registration := shell.RegisterCampaign(definition.Lineage)
	campaign, _ = applyRecordedCampaign(observer, campaign, campaignmodule.Registered(registration))
	cut()
	campaign, _ = applyRecordedCampaign(observer, campaign, campaignmodule.SnapshotEstablished("private-snapshot"))
	cut()
	campaign, _ = applyRecordedCampaign(observer, campaign, campaignmodule.CatalogueDiscovered("private-snapshot", nil))
	cut()
	campaign, _ = applyRecordedCampaign(observer, campaign,
		campaignmodule.ResourceSettled(campaignmodule.SnapshotResource, "private-snapshot"))
	cut()
	processedTerminal := shell.CommitTerminal(registration.Campaign())
	cut()
	_, _ = applyRecordedCampaign(observer, campaign, campaignmodule.TerminalCommitted(processedTerminal.Decision()))

	trace, production := recorder.quiescent(shell, machine)
	assert.EqualValues(t, 6, len(trace.barriers), "retained prefix barriers=%d, want 6", len(trace.barriers))
	replayed := ReplayLegal(trace)
	require.Nil(t, replayed.failure, "recorded production trace did not replay: %v", replayed.failure)
	replayed.world.runtimeState = processruntime.Replay{}
	assert.Equal(t, production, replayed.world, "recorded production replay diverged:\n got=%#v\nwant=%#v", replayed.world, production)
	corrupted := trace
	corrupted.barriers = slices.Clone(trace.barriers)
	corrupted.barriers[0].runtime = processruntime.NewReplay(corrupted.barriers[0].runtime.Capacity() + 1).Projection()
	{
		failure := ReplayLegal(corrupted).failure
		require.NotNil(t, failure, "corrupted barrier replay failure=%v", failure)
		assert.Contains(t, failure.Error(), "quiescent world diverged", "corrupted barrier replay failure=%v", failure)
	}
}

func TestSimulationRecorderReplaysTheProductionCampaignExecutor(t *testing.T) {
	recorder := newSimulationRecorder()
	shell := processruntime.NewObserved(1, newSimulationRuntimeObserver(recorder, 1))
	executor := campaignmodule.NewConformingExecutorWithSystem(
		shell, simulationUnusedAttemptSystem{}, simulationCampaignRecorder{recorder: recorder},
	)
	result := executor.Execute(campaignmodule.Configuration{
		Identity: "campaign-recorded-executor", Lineage: 53,
		Repository:   campaignRepository{Repository: &simulationEmptyRepository{}},
		TemporaryDir: &simulationTemporaryDirectory{}, Command: []string{"test"},
		Profile: processruntime.AutomaticProfile, Peers: 1,
	})
	require.Equal(t, campaignmodule.ManagedNoMutants, result.Outcome)

	trace, production := recorder.quiescent(shell, supervision.NewMachine())
	replayed := ReplayLegal(trace)
	require.NoError(t, replayed.failure)
	replayed.world.runtimeState = processruntime.Replay{}
	assert.Equal(t, production, replayed.world)
	assert.Equal(t, []simulationAuthority{
		simulationRuntimeAuthority,
		simulationCampaignAuthority,
		simulationCampaignAuthority,
		simulationCampaignAuthority,
		simulationCampaignAuthority,
		simulationRuntimeAuthority,
		simulationCampaignAuthority,
	}, simulationAuthorities(trace))
}

type simulationEmptyRepository struct{}

func (*simulationEmptyRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile { return nil }

func (*simulationEmptyRepository) MaterializeTemporaryRepository(path string) TemporaryRepository {
	return &simulationEmptyTemporaryRepository{path: path}
}

type simulationEmptyTemporaryRepository struct{ path string }

func (repository *simulationEmptyTemporaryRepository) Root() string { return repository.path }
func (*simulationEmptyTemporaryRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	return nil
}
func (*simulationEmptyTemporaryRepository) MaterializeTemporaryRepository(path string) TemporaryRepository {
	return &simulationEmptyTemporaryRepository{path: path}
}
func (*simulationEmptyTemporaryRepository) Overwrite(string, []byte) {}
func (*simulationEmptyTemporaryRepository) Remove()                  {}

type simulationTemporaryDirectory struct{ next int }

func (directory *simulationTemporaryDirectory) New() string {
	directory.next++
	return fmt.Sprintf("workspace-%d", directory.next)
}

type simulationUnusedAttemptSystem struct{}

func (simulationUnusedAttemptSystem) ReserveLaunch(*processruntime.StartCell, supervision.Spec) {
	panic("unexpected launch")
}
func (simulationUnusedAttemptSystem) DiscardLaunch(*processruntime.StartCell) {
	panic("unexpected launch")
}
func (simulationUnusedAttemptSystem) Launch(
	processruntime.PreparedStart,
	supervision.Spec,
) supervision.ObservedLaunch {
	panic("unexpected launch")
}
func (simulationUnusedAttemptSystem) Wait(
	supervision.Generation,
	*supervision.OwnedAttempt,
) supervision.ObservedTerminal {
	panic("unexpected wait")
}
func (simulationUnusedAttemptSystem) Stop(*supervision.OwnedAttempt) { panic("unexpected stop") }
func (simulationUnusedAttemptSystem) EmergencyDrain(
	supervision.EmergencyRequest,
) (supervision.SweepResult, processruntime.EmergencySettlement) {
	panic("unexpected emergency")
}

func TestSimulationReplaysNonEmptyManagedCampaignAtQuiescence(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-recorded-managed", Lineage: 521, Command: []string{"test"},
			Profile: SerialProfile, Peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}, nil)
	require.Nil(t, explored.failure)
	path := simulationForbiddenValuePath(reflect.ValueOf(explored.trace), "trace")
	assert.Empty(t, path)
	for index, record := range explored.trace.records {
		assert.NotZero(t, record.source.kind, "record %d", index)
		assert.NotZero(t, record.source.identity, "record %d", index)
	}
	replayed := ReplayLegal(explored.trace)
	require.Nil(t, replayed.failure)
	assert.Equal(t, explored.world, replayed.world)
}

func TestSimulationRecorderSealsCatalogueFactsAgainstCallerMutation(t *testing.T) {
	recorder := newSimulationRecorder()
	definition := campaignmodule.Definition{
		Identity: "campaign-immutable-record", Lineage: 53, Command: []string{"test"},
		Profile: AutomaticProfile, Peers: 1,
	}
	campaign, _ := campaignmodule.NewMachine(definition)
	observer := simulationCampaignRecorder{recorder: recorder}
	shell := processruntime.NewObserved(1, newSimulationRuntimeObserver(recorder, 1))
	machine := supervision.NewMachine()
	registration := shell.RegisterCampaign(definition.Lineage)
	campaign, _ = applyRecordedCampaign(observer, campaign, campaignmodule.Registered(registration))
	campaign, _ = applyRecordedCampaign(observer, campaign, campaignmodule.SnapshotEstablished("private-snapshot"))
	mutants := []mutantIdentity{"mutant-a", "mutant-b"}
	mutantNames := []string{"mutant-a", "mutant-b"}
	_, _ = applyRecordedCampaign(observer, campaign,
		campaignmodule.CatalogueDiscovered("private-snapshot", mutantNames))

	trace, _ := recorder.quiescent(shell, machine)
	mutants[0] = "caller-rewrite"
	mutantNames[0] = "caller-rewrite"
	assert.Equal(t, []string{"mutant-a", "mutant-b"},
		trace.records[len(trace.records)-1].campaignState.Catalogue())
}

func TestSimulationTraceStoresRuntimeOwnedRecordedCuts(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-runtime-cuts", Lineage: 91, Command: []string{"test"},
			Profile: SerialProfile, Peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}, simulationChoiceBytes{})
	require.NoError(t, explored.failure)

	for _, record := range explored.trace.records {
		if record.authority == simulationRuntimeAuthority {
			assert.NotZero(t, record.runtimeCut.Operation())
		}
	}
}

func TestSimulationRecorderProjectsRuntimeCustodyWithoutDeliveryCapabilities(t *testing.T) {
	recorder := newSimulationRecorder()
	shell := processruntime.NewObserved(1, newSimulationRuntimeObserver(recorder, 1))
	registration := shell.RegisterCampaign(71)
	await := shell.RequestAdmission(processruntime.Admission{
		Campaign: registration.Campaign(), Attempt: "attempt-a", Class: processruntime.SharedAdmission,
		Profile: AutomaticProfile,
	})
	assert.Equal(t, processruntime.AdmissionAccepted, await.Decision(), "admission decision=%v", await.Decision())
	campaign, _ := campaignmodule.NewMachine(campaignmodule.Definition{
		Identity: "campaign-projection", Lineage: 71, Command: []string{"test"},
		Profile: AutomaticProfile, Peers: 1,
	})
	_, _ = applyRecordedCampaign(simulationCampaignRecorder{recorder: recorder}, campaign,
		campaignmodule.Registered(registration))
	machine := supervision.NewMachine()

	trace, _ := recorder.quiescent(shell, machine)
	{
		path := simulationForbiddenValuePath(reflect.ValueOf(trace), "trace")
		assert.EqualValues(t, "", path, "runtime trace leaked a delivery capability at %s", path)
	}
}

func TestSimulationCausalTerminalConsumesRecordedResolvedDeadline(t *testing.T) {
	traceForPeers := func(peers int) simulationTrace {
		result := Explore(simulationDefinition{
			campaign: campaignmodule.Definition{
				Identity: "campaign-deadline", Lineage: processruntime.Lineage(peers),
				Command: []string{"test"}, Profile: AutomaticProfile, Peers: peers,
			},
			capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
		}, nil)
		require.NoError(t, result.failure)
		return result.trace
	}
	recorded := simulationFact(t, traceForPeers(30), campaignFactAttemptTerminal)
	derived := simulationFact(t, traceForPeers(1), campaignFactAttemptTerminal)
	merged := simulationCausalCampaignPayload(recorded, derived)
	recordedDeadline, recordedOK := recorded.ResolvedMutationDeadline()
	derivedDeadline, derivedOK := derived.ResolvedMutationDeadline()
	mergedDeadline, mergedOK := merged.ResolvedMutationDeadline()
	require.True(t, recordedOK)
	require.True(t, derivedOK)
	require.True(t, mergedOK)
	assert.NotEqual(t, recordedDeadline, derivedDeadline)
	assert.Equal(t, recordedDeadline, mergedDeadline)
}

func TestSimulationEnabledMovesAreCanonicalReducerOwnedWork(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-enabled-order", Lineage: 1901, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 2,
		},
		capacity: 2, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, nil)
	require.NoError(t, explored.failure)
	var materializeA, materializeB, launch campaignmodule.Effect
	for _, record := range explored.trace.records {
		for _, effect := range record.campaignEffects {
			switch {
			case simulationMaterializesWorkspace(effect) && effect.Mutant() == "mutant-a":
				materializeA = effect
			case simulationMaterializesWorkspace(effect) && effect.Mutant() == "mutant-b":
				materializeB = effect
			case simulationLaunchesAttempt(effect) && launch.IsZero():
				launch = effect
			}
		}
	}
	require.False(t, materializeA.IsZero())
	require.False(t, materializeB.IsZero())
	require.False(t, launch.IsZero())
	effects := []campaignmodule.Effect{launch, materializeB, materializeA}
	actions := []supervision.Effect{
		supervision.CorrelatedMalformedEffect(supervision.WaitRootEffect, 3, 8),
		supervision.CorrelatedMalformedEffect(supervision.SampleRunningEffect, 3, 7),
	}

	got := simulationEnabledMoves(effects, actions, []mutantIdentity{"mutant-a", "mutant-b"})
	want := []simulationEnabledMove{
		{authority: simulationCampaignAuthority, effect: materializeA},
		{authority: simulationCampaignAuthority, effect: materializeB},
		{authority: supervisionAuthority, effect: launch},
		{authority: supervisionAuthority, action: actions[1]},
		{authority: supervisionAuthority, action: actions[0]},
	}
	assert.Equal(t, want, got, "enabled moves=%#v, want %#v", got, want)
}

func TestSimulationChoiceRecordsMarkCanonicalRecovery(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-recovery", Lineage: 91, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 2,
		},
		capacity: 2, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, simulationChoiceBytes{2})
	assert.Nil(t, explored.failure, "recovery exploration failed: %v", explored.failure)
	seenExploration, seenRecovery := false, false
	for _, choice := range explored.trace.choices {
		if !choice.recovery {
			assert.False(t, seenRecovery, "exploration resumed after recovery: %#v", explored.trace.choices)
			seenExploration = true
			continue
		}
		seenRecovery = true
		assert.EqualValues(t, 0, choice.selected, "non-canonical recovery choice=%#v", choice)
	}
	assert.True(t, seenExploration, "choice records=%#v, want exploration followed by recovery", explored.trace.choices)
	assert.True(t, seenRecovery, "choice records=%#v, want exploration followed by recovery", explored.trace.choices)
}

func TestSimulationEmptyCampaignRecordsOnlyEnabledCausalMoves(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-causal-empty", Lineage: 92, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 1,
		},
		capacity: 1,
	}, simulationChoiceBytes{})
	assert.Nil(t, explored.failure, "empty exploration failed: %v", explored.failure)
	for index, record := range explored.trace.records {
		assert.NotEqual(t, 0, record.source.kind, "record %d has no reducer-emitted causal source: %#v", index, record.source)
		assert.NotEqual(t, 0, record.source.identity, "record %d has no reducer-emitted causal source: %#v", index, record.source)
	}
}

func TestSimulationNonEmptyCampaignRecordsOnlyEnabledCausalMoves(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-causal-nonempty", Lineage: 93, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 2,
		},
		capacity: 2, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, simulationChoiceBytes{})
	assert.Nil(t, explored.failure, "non-empty exploration failed: %v", explored.failure)
	for index, record := range explored.trace.records {
		assert.NotEqual(t, 0, record.source.kind, "record %d has no reducer-emitted causal source: %#v", index, record.source)
		assert.NotEqual(t, 0, record.source.identity, "record %d has no reducer-emitted causal source: %#v", index, record.source)
	}
}

func TestSimulationRecorderProjectsFilesystemPathsToLogicalIdentities(t *testing.T) {
	recorder := newSimulationRecorder()
	shell := processruntime.NewObserved(1, newSimulationRuntimeObserver(recorder, 1))
	definition := campaignmodule.Definition{
		Identity: "campaign-paths", Lineage: 81, Command: []string{"test"},
		Profile: AutomaticProfile, Peers: 1,
	}
	campaign, _ := campaignmodule.NewMachine(definition)
	observer := simulationCampaignRecorder{recorder: recorder}
	machine := supervision.NewMachine()
	registration := shell.RegisterCampaign(definition.Lineage)
	campaign, _ = applyRecordedCampaign(observer, campaign, campaignmodule.Registered(registration))
	_, _ = applyRecordedCampaign(observer, campaign,
		campaignmodule.SnapshotEstablished("/private/repository/snapshot-937"))

	trace, projection := recorder.quiescent(shell, machine)
	projected := fmt.Sprintf("%#v %#v", trace, projection)
	assert.NotContains(t, projected, "/private/repository", "simulation projection leaked a filesystem path: %s", projected)
	assert.True(t, trace.records[len(trace.records)-1].campaignEvent.Fact().Equal(
		campaignmodule.SnapshotEstablished("snapshot:campaign-paths")))
}

func applyRecordedCampaign(
	observer simulationCampaignRecorder,
	machine campaignmodule.Machine,
	fact campaignmodule.Fact,
) (campaignmodule.Machine, campaignmodule.Transition) {
	leave := observer.Enter()
	defer leave()
	reservation := observer.Reserve()
	next, transition := machine.Apply(fact)
	observer.Publish(reservation, transition.Event(), transition.Projection(), transition.Effects())
	return next, transition
}

func simulationFactForGeneration(
	t *testing.T,
	trace simulationTrace,
	kind simulationTestFactKind,
	generation processruntime.Generation,
) campaignmodule.Fact {
	t.Helper()
	for _, record := range trace.records {
		fact := record.campaignEvent.Fact()
		if simulationTestFact(fact) == kind && fact.Generation() == generation {
			return fact
		}
	}
	require.FailNowf(t, "campaign fact not found", "kind=%s generation=%d", kind, generation)
	return campaignmodule.Fact{}
}

func simulationFact(t *testing.T, trace simulationTrace, kind simulationTestFactKind) campaignmodule.Fact {
	t.Helper()
	for _, record := range trace.records {
		fact := record.campaignEvent.Fact()
		if simulationTestFact(fact) == kind {
			return fact
		}
	}
	require.FailNowf(t, "campaign fact not found", "kind=%s", kind)
	return campaignmodule.Fact{}
}

func simulationEffect(
	t *testing.T,
	trace simulationTrace,
	matches func(campaignmodule.Effect) bool,
	name string,
) campaignmodule.Effect {
	t.Helper()
	for _, record := range trace.records {
		for _, effect := range record.campaignEffects {
			if matches(effect) {
				return effect
			}
		}
	}
	require.FailNowf(t, "campaign effect not found", "effect=%s", name)
	return campaignmodule.Effect{}
}

func simulationEffectForGeneration(
	t *testing.T,
	trace simulationTrace,
	matches func(campaignmodule.Effect) bool,
	name string,
	generation processruntime.Generation,
) campaignmodule.Effect {
	t.Helper()
	for _, record := range trace.records {
		for _, effect := range record.campaignEffects {
			if matches(effect) && effect.Generation() == generation {
				return effect
			}
		}
	}
	require.FailNowf(t, "campaign effect not found", "effect=%s generation=%d", name, generation)
	return campaignmodule.Effect{}
}

func simulationFatalTrace(t *testing.T) simulationTrace {
	t.Helper()
	expired := false
	result := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-fatal-fixture", Lineage: 2179, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}, simulationFocusedChoiceSource(func(moves []simulationEngineMove) int {
		for index, move := range moves {
			if move.action.Kind() == supervision.ObserveEmptinessEffect &&
				move.variant.drain == simulationDrainAtBoundary &&
				move.attemptKind == campaignAttemptPrimary {
				expired = true
				return index
			}
		}
		for index, move := range moves {
			if move.action.Kind() != supervision.WaitRootEffect {
				return index
			}
		}
		return 0
	}))
	require.NoError(t, result.failure)
	require.True(t, expired)
	return result.trace
}

func simulationStopTrace(t *testing.T) simulationTrace {
	t.Helper()
	selected := false
	result := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-stop-fixture", Lineage: 2180, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 2,
		},
		capacity: 2, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, simulationFocusedChoiceSource(func(moves []simulationEngineMove) int {
		ownedPeerReady := slices.ContainsFunc(moves, func(move simulationEngineMove) bool {
			return move.action.Kind() == supervision.WaitRootEffect && move.mutant == "mutant-a"
		})
		if ownedPeerReady {
			for index, move := range moves {
				if move.action.Kind() == supervision.LaunchNativeEffect && move.mutant == "mutant-b" &&
					move.variant.launch == simulationLaunchProvenNotReleased {
					selected = true
					return index
				}
			}
		}
		for index, move := range moves {
			if move.action.Kind() == supervision.LaunchNativeEffect && move.mutant == "mutant-b" {
				continue
			}
			if move.action.Kind() != supervision.WaitRootEffect {
				return index
			}
		}
		return 0
	}))
	require.NoError(t, result.failure)
	require.True(t, selected)
	return result.trace
}

func simulationEmergencyCampaign(t *testing.T, trace simulationTrace) campaignmodule.Machine {
	t.Helper()
	var kinds []string
	for index, record := range trace.records {
		if record.authority == simulationCampaignAuthority {
			kinds = append(kinds, record.campaignEvent.Fact().Name())
		}
		if !record.campaignEvent.Fact().CompletesEmergencySettlement() {
			continue
		}
		prefix := simulationTrace{
			definition: trace.definition,
			records:    slices.Clone(trace.records[:index]),
		}
		replayed := ReplayLegal(prefix)
		require.NoError(t, replayed.failure)
		require.True(t, replayed.world.campaign.EmergencyRequested())
		return replayed.world.campaign
	}
	require.FailNowf(t, "campaign emergency fact not found", "kinds=%v", kinds)
	return campaignmodule.Machine{}
}

func TestSimulationRecorderCanonicalizesSupervisorInstants(t *testing.T) {
	recorder := newSimulationRecorder()
	machine := supervision.NewMachine()
	sourceLocation := time.FixedZone("private-host-zone", 9*60*60)
	registeredAt := time.Date(2026, 8, 26, 1, 2, 3, 4, sourceLocation)
	fact := supervision.ProspectiveRegistration(
		1, "attempt-a", registeredAt, registeredAt.Add(time.Second), AutomaticProfile, time.Second,
	)
	machine, transition := machine.Apply(fact)
	recorder.recordSupervisor(
		recorder.reserve(supervisionAuthority), fact, transition.Event(), machine.Projection(), transition.Effects(),
	)

	recorder.mutex.Lock()
	record := recorder.records[0]
	recorder.mutex.Unlock()
	{
		got := record.supervisorEvent.OccurredAt()
		assert.True(t, got.Equal(registeredAt), "canonical instant changed: got=%s want=%s", got, registeredAt)
		assert.Equal(t, time.UTC, got.Location(), "canonical instant changed: got=%s want=%s", got, registeredAt)
	}
}

func TestSimulationTraceCarriesNoProductionCapabilities(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "campaign-integer-time", Lineage: 82, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}, simulationChoiceBytes{0})
	assert.Nil(t, explored.failure, "exploration failed: %v", explored.failure)
	{
		path := simulationForbiddenValuePath(reflect.ValueOf(explored.trace), "trace")
		assert.EqualValues(t, "", path, "canonical trace retains a production capability at %s", path)
	}
}

func simulationForbiddenValuePath(value reflect.Value, path string) string {
	if !value.IsValid() {
		return ""
	}
	if value.Type() == reflect.TypeOf(time.Time{}) {
		return path
	}
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return path
	case reflect.Interface, reflect.Pointer:
		if !value.IsNil() {
			return simulationForbiddenValuePath(value.Elem(), path)
		}
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			if found := simulationForbiddenValuePath(
				value.Field(index), path+"."+value.Type().Field(index).Name,
			); found != "" {
				return found
			}
		}
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			if found := simulationForbiddenValuePath(
				value.Index(index), fmt.Sprintf("%s[%d]", path, index),
			); found != "" {
				return found
			}
		}
	}

	return ""
}

func TestSimulationFuzzInputDrivesSustainedLegalChoices(t *testing.T) {
	definition, choices := simulationFuzzInput([]byte{2, 2, 4, 5, 6, 7})
	assert.EqualValues(t, 3, definition.capacity, "fuzz definition/choices=%#v/%v", definition, choices)
	assert.EqualValues(t, 3, definition.campaign.Peers, "fuzz definition/choices=%#v/%v", definition, choices)
	assert.EqualValues(t, 3, len(definition.catalogue), "fuzz definition/choices=%#v/%v", definition, choices)
	assert.EqualValues(t, 4, len(choices), "fuzz definition/choices=%#v/%v", definition, choices)
	explored := Explore(definition, choices)
	assert.Nil(t, explored.failure, "sustained fuzz exploration failed: %v", explored.failure)
	nonRecovery := 0
	for _, choice := range explored.trace.choices {
		if !choice.recovery {
			nonRecovery++
		}
	}
	assert.False(t, nonRecovery < len(choices), "exploration consumed %d choices, want at least %d: %#v", nonRecovery, len(choices), explored.trace.choices)
}

func TestSimulationEngineConsumesTheSelectedSameCutDelivery(t *testing.T) {
	firstEvent := supervision.CorrelatedMalformedFact(supervision.RuntimeCompletedFact, 1)
	secondEvent := supervision.NewMachine().EmergencyRequest(time.Unix(1, 0), time.Unix(2, 0))
	first := simulationEngineMove{
		source:             simulationCausalSource{kind: simulationOwnerDeliverySource, identity: 19},
		supervisorDelivery: &firstEvent,
	}
	second := simulationEngineMove{
		source:             simulationCausalSource{kind: simulationOwnerDeliverySource, identity: 19},
		supervisorDelivery: &secondEvent,
	}
	engine := simulationEngine{pending: []simulationEngineMove{first, second}}

	assert.True(t, engine.consume(second), "pending delivery after consuming second=%#v, want first", engine.pending)
	require.Len(t, engine.pending, 1, "pending delivery after consuming second=%#v, want first", engine.pending)
	assert.Equal(t, first.supervisorDelivery, engine.pending[0].supervisorDelivery, "pending delivery after consuming second=%#v, want first", engine.pending)
}

func TestSimulationSelectsOneTypedActionFromACompoundOwnerCut(t *testing.T) {
	want := supervision.CorrelatedMalformedEffect(supervision.DeliverTerminalEffect, 0, 10)
	actions := []supervision.Effect{
		want,
		supervision.CorrelatedMalformedEffect(supervision.SettleEmergencyEffect, 0, 11),
	}
	{
		got := simulationOnlySupervisorAction(actions, supervision.DeliverTerminalEffect)
		assert.Equal(t, want, got, "selected action=%#v, want %#v", got, want)
	}
}

func TestSimulationTerminalWaitsForItsCampaignLaunchDelivery(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignmodule.Definition{
			Identity: "terminal-launch-order", Lineage: 2199, Command: []string{"test"},
			Profile: AutomaticProfile, Peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}, nil)
	require.NoError(t, explored.failure)
	launch := simulationFact(t, explored.trace, campaignFactAttemptLaunched)
	generation := launch.Generation()
	engine := simulationEngine{pending: []simulationEngineMove{
		{
			source:   simulationCausalSource{kind: simulationOwnerDeliverySource, identity: 9},
			delivery: launch,
		},
		{
			source: simulationCausalSource{kind: supervisionActionSource, identity: 10},
			action: supervision.CorrelatedMalformedEffect(supervision.DeliverTerminalEffect, generation, 10),
		},
	}}

	moves := engine.enabledMoves()
	require.Len(t, moves, 1, "enabled moves=%#v, want only the causal launch delivery", moves)
	assert.Equal(t, launch, moves[0].delivery, "enabled moves=%#v, want only the causal launch delivery", moves)
}

func TestSimulationDisablesResidualTransferAfterRuntimeCustodyMoves(t *testing.T) {
	runtime, generation := simulationOwnedRuntime(t)
	runtime, _ = runtime.Apply(processruntime.ObserveAttemptCut(generation, processruntime.DrainUnconfirmed()))
	engine := simulationEngine{
		runtime: runtime,
		pending: []simulationEngineMove{{
			source: simulationCausalSource{kind: supervisionActionSource, identity: 9},
			action: supervision.CorrelatedMalformedEffect(supervision.TransferResidualCustodyEffect, 2, 9),
		}},
	}

	{
		moves := engine.enabledMoves()
		assert.EqualValues(t, 0, len(moves), "enabled stale residual transfer=%#v", moves)
	}
}

func simulationOwnedRuntime(t *testing.T) (processruntime.Replay, attemptGeneration) {
	t.Helper()
	runtime := processruntime.NewReplay(1)
	runtime, registered := runtime.Apply(processruntime.RegisterCampaignCut(91))
	runtime, admission := runtime.Apply(processruntime.RequestAdmissionCut(processruntime.Admission{
		Campaign: registered.Registration().Campaign(), Attempt: "attempt-a", Class: processruntime.SharedAdmission,
	}))
	admitted := runtimeAdmissionResult(admission.Admission())
	require.Len(t, admitted.Deliveries(), 1)
	runtime, start := runtime.Apply(processruntime.CommitStartCut(admitted.Deliveries()[0]))
	started := runtimeStartResult(start.Start())
	runtime, _ = runtime.Apply(processruntime.ObserveAttemptCut(started.Generation(), processruntime.Owned()))
	return runtime, started.Generation()
}

func TestSimulationOrdersRuntimeCustodyActionsByOwnerToken(t *testing.T) {
	runtime, generation := simulationOwnedRuntime(t)
	engine := simulationEngine{
		runtime: runtime,
		pending: []simulationEngineMove{
			{
				source: simulationCausalSource{kind: supervisionActionSource, identity: 10},
				action: supervision.CorrelatedMalformedEffect(supervision.SettleRuntimeEffect, generation, 10),
			},
			{
				source: simulationCausalSource{kind: supervisionActionSource, identity: 11},
				action: supervision.CorrelatedMalformedEffect(supervision.SettleEmergencyEffect, 0, 11),
			},
		},
	}

	moves := engine.enabledMoves()
	require.Len(t, moves, 1, "enabled runtime custody actions=%#v, want token 10 only", moves)
	assert.EqualValues(t, 10, moves[0].action.Token(), "enabled runtime custody actions=%#v, want token 10 only", moves)
}

func TestSimulationEmergencySettlementRetainsLateTerminalNeededForCampaignCleanup(t *testing.T) {
	definition, choices := simulationFuzzInput([]byte("22000000110000010AX12"))
	result := Explore(definition, choices)
	require.NoErrorf(t, result.failure, "phase=%s obligations=%v failed=%t",
		result.world.campaign.Projection().PhaseName(), result.world.campaign.Projection().Obligations(),
		result.world.campaign.Failed())
}

func TestSimulationOrdersRuntimeCompletionBeforeLaterCustodyAction(t *testing.T) {
	completion := supervision.CorrelatedMalformedFact(supervision.RuntimeCompletedFact, 2)
	engine := simulationEngine{
		trace: simulationTrace{records: []simulationRecord{{
			sequence: 19,
			source:   simulationCausalSource{kind: supervisionActionSource, identity: 9},
		}}},
		pending: []simulationEngineMove{
			{
				source:             simulationCausalSource{kind: simulationOwnerDeliverySource, identity: 19},
				supervisorDelivery: &completion,
			},
			{
				source: simulationCausalSource{kind: supervisionActionSource, identity: 11},
				action: supervision.CorrelatedMalformedEffect(supervision.SettleEmergencyEffect, 0, 11),
			},
		},
	}

	moves := engine.enabledMoves()
	require.Len(t, moves, 1, "enabled runtime custody moves=%#v, want completion cut 19 only", moves)
	assert.EqualValues(t, 19, moves[0].source.identity, "enabled runtime custody moves=%#v, want completion cut 19 only", moves)
}

func TestSimulationEmergencySettlementWaitsForCampaignRequest(t *testing.T) {
	engine := simulationEngine{pending: []simulationEngineMove{{
		source: simulationCausalSource{kind: supervisionActionSource, identity: 12},
		action: supervision.CorrelatedMalformedEffect(supervision.DeliverEmergencySettlementEffect, 0, 12),
	}}}

	{
		moves := engine.enabledMoves()
		assert.EqualValues(t, 0, len(moves), "enabled unrequested emergency settlement=%#v", moves)
	}
}

func TestSimulationEmergencySettlementWaitsForPublishedCampaignIngress(t *testing.T) {
	trace := simulationFatalTrace(t)
	launch := simulationFact(t, trace, campaignFactAttemptLaunched)
	engine := simulationEngine{
		campaign: simulationEmergencyCampaign(t, trace),
		pending: []simulationEngineMove{
			{
				source:   simulationCausalSource{kind: simulationOwnerDeliverySource, identity: 19},
				delivery: launch,
			},
			{
				source: simulationCausalSource{kind: supervisionActionSource, identity: 20},
				action: supervision.CorrelatedMalformedEffect(supervision.DeliverEmergencySettlementEffect, 0, 20),
			},
		},
	}

	moves := engine.enabledMoves()
	require.Len(t, moves, 1, "enabled emergency settlement moves=%#v, want published campaign ingress", moves)
	assert.Equal(t, launch, moves[0].delivery, "enabled emergency settlement moves=%#v, want published campaign ingress", moves)
}

func TestSimulationEmergencyCutWaitsForCommittedStartDelivery(t *testing.T) {
	trace := simulationFatalTrace(t)
	emergency := supervision.NewMachine().EmergencyRequest(time.Unix(1, 0), time.Unix(2, 0))
	start := simulationFact(t, trace, campaignFactStartCommitted)
	engine := simulationEngine{
		campaign: simulationEmergencyCampaign(t, trace),
		pending: []simulationEngineMove{
			{
				source:             simulationCausalSource{kind: simulationOwnerDeliverySource, identity: 20},
				supervisorDelivery: &emergency,
			},
			{
				source:   simulationCausalSource{kind: simulationOwnerDeliverySource, identity: 21},
				delivery: start,
			},
		},
	}

	moves := engine.enabledMoves()
	require.Len(t, moves, 1, "enabled emergency cut moves=%#v, want committed start delivery only", moves)
	assert.Equal(t, start, moves[0].delivery, "enabled emergency cut moves=%#v, want committed start delivery only", moves)
}

func TestSimulationStopWaitsForSupervisorAttemptOwnership(t *testing.T) {
	trace := simulationStopTrace(t)
	stopEffect := simulationEffect(t, trace, simulationStopsAttempt, "stop attempt")
	launchEffect := simulationEffectForGeneration(
		t, trace, simulationLaunchesAttempt, "launch attempt", stopEffect.Generation(),
	)
	generation := launchEffect.Generation()
	launch := simulationEngineMove{
		source: simulationCausalSource{kind: simulationCampaignEffectSource, identity: uint64(launchEffect.ID())},
		effect: launchEffect,
	}
	stop := simulationEngineMove{
		source: simulationCausalSource{kind: simulationCampaignEffectSource, identity: uint64(stopEffect.ID())},
		effect: stopEffect,
	}
	machine, transition := supervision.NewMachine().Apply(supervision.ProspectiveRegistration(
		generation, "attempt", time.Unix(1, 0), time.Unix(2, 0), AutomaticProfile, time.Second,
	))
	launchAction := simulationEngineMove{
		source: simulationCausalSource{kind: supervisionActionSource, identity: 21},
		action: transition.Effects()[0],
	}
	tests := []struct {
		name   string
		engine simulationEngine
	}{
		{"campaign_launch_pending", simulationEngine{pending: []simulationEngineMove{launch, stop}}},
		{
			"supervisor_launch_action_pending",
			simulationEngine{
				machine: machine,
				pending: []simulationEngineMove{launchAction, stop},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			moves := test.engine.enabledMoves()
			assert.NotEqual(t, 0, len(moves), "attempt ownership made no progress")
			for _, move := range moves {
				assert.False(t, simulationStopsAttempt(move.effect), "enabled stop before supervisor ownership: %#v", moves)
			}
		})
	}
	t.Run("ownership_acquired", func(t *testing.T) {
		completed := simulationEngine{
			launches: map[attemptGeneration]campaignmodule.Effect{generation: launch.effect},
			pending:  []simulationEngineMove{stop},
		}
		moves := completed.enabledMoves()
		require.EqualValues(t, 1, len(moves), "completed generation retained disabled stop: %#v", moves)
		assert.True(t, simulationStopsAttempt(moves[0].effect), "completed generation retained disabled stop: %#v", moves)
		assert.True(t, completed.consume(moves[0]), "completed generation stop was not pending")
		err := completed.apply(moves[0])
		assert.NoError(t, err, "completed generation stop=%v, want no-op", err)
	})
}

func TestSimulationOrdersSupervisorActionsWithinOneGeneration(t *testing.T) {
	engine := simulationEngine{pending: []simulationEngineMove{
		{
			source: simulationCausalSource{kind: supervisionActionSource, identity: 26},
			action: supervision.CorrelatedMalformedEffect(supervision.SealStopAdmissionEffect, 4, 26),
		},
		{
			source: simulationCausalSource{kind: supervisionActionSource, identity: 27},
			action: supervision.CorrelatedMalformedEffect(supervision.ReleaseDomainEffect, 4, 27),
		},
	}}

	moves := engine.enabledMoves()
	require.Len(t, moves, 1, "enabled same-generation actions=%#v, want token 26 only", moves)
	assert.EqualValues(t, 26, moves[0].action.Token(), "enabled same-generation actions=%#v, want token 26 only", moves)
}

func simulationFuzzInput(source []byte) (simulationDefinition, simulationChoiceBytes) {
	capacity := 1
	definition := simulationDefinition{campaign: campaignmodule.Definition{
		Identity: "campaign-fuzz", Lineage: 61, Command: []string{"test"},
		Profile: AutomaticProfile, Peers: capacity,
	}, capacity: capacity}
	if len(source) == 0 {
		return definition, nil
	}
	mutants := 1 + int(source[0]%3)
	definition.catalogue = make([]mutantIdentity, mutants)
	for index := range definition.catalogue {
		definition.catalogue[index] = mutantIdentity(fmt.Sprintf("mutant-%d", index+1))
	}
	if len(source) > 1 {
		capacity = 1 + int(source[1]%3)
		definition.capacity = capacity
		definition.campaign.Peers = capacity
	}
	if len(source) <= 2 {
		return definition, nil
	}

	return definition, slices.Clone(simulationChoiceBytes(source[2:]))
}

func FuzzSimulationLegalReplayAndViolationRemainDeterministic(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{1, 7, 9})
	f.Add([]byte{2})
	f.Fuzz(func(t *testing.T, source []byte) {
		definition, choices := simulationFuzzInput(source)
		explored := Explore(definition, choices)
		require.Nil(t, explored.failure, "legal exploration failed: %v; runtime=%#v; actions=%v", explored.failure, explored.world.runtime, simulationRecordedActionSummary(explored.trace))
		replayed := ReplayLegal(explored.trace)
		require.Nil(t, replayed.failure, "legal replay failed: %v", replayed.failure)
		assert.Equal(t, replayed.world, explored.world, "legal replay world diverged:\nexplored=%#v\nreplayed=%#v", explored.world, replayed.world)
		prefix := simulationTrace{
			definition: explored.trace.definition,
			records:    append([]simulationRecord(nil), explored.trace.records[:2]...),
		}
		malformed := simulationMalformedFact{
			authority: simulationCampaignAuthority,
			campaign:  campaignmodule.SnapshotEstablished(""),
		}
		if len(source) != 0 {
			switch source[0] % 3 {
			case 1:
				malformed = simulationMalformedFact{
					authority:  simulationRuntimeAuthority,
					runtimeCut: processruntime.RequestAdmissionCut(processruntime.Admission{}),
				}
			case 2:
				malformed = simulationMalformedFact{
					authority:  supervisionAuthority,
					supervisor: supervision.CorrelatedMalformedFact(supervision.ProspectiveRegisteredFact, 0),
				}
			}
		}
		first := ReplayViolation(prefix, malformed)
		second := ReplayViolation(prefix, malformed)
		require.Nil(t, first.failure, "violation replay diverged: first=%#v second=%#v", first, second)
		assert.Equal(t, second, first, "violation replay diverged: first=%#v second=%#v", first, second)
	})
}

func TestSimulationQueuesOnlyOnePendingEmergencyEpoch(t *testing.T) {
	definition, choices := simulationFuzzInput([]byte("X12002"))

	result := Explore(definition, choices)

	assert.Nil(t, result.failure)
}

func simulationRecordedActionSummary(trace simulationTrace) []string {
	var summary []string
	for _, record := range trace.records {
		for _, action := range record.supervisorActions {
			summary = append(summary, fmt.Sprintf(
				"record=%d kind=%d generation=%d token=%d",
				record.sequence, action.Kind(), action.Generation(), action.Token(),
			))
		}
	}

	return summary
}

func simulationAuthorities(trace simulationTrace) []simulationAuthority {
	authorities := make([]simulationAuthority, 0, len(trace.records))
	for _, record := range trace.records {
		authorities = append(authorities, record.authority)
	}

	return authorities
}
