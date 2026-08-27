package ooze

import (
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/gtramontina/ooze/viruses"
	"github.com/gtramontina/ooze/viruses/integerincrement"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type simulationFocusedChoiceSource func([]simulationEngineMove) int

func (simulationFocusedChoiceSource) choose(int) int { return 0 }

func (source simulationFocusedChoiceSource) chooseMove(moves []simulationEngineMove) int {
	return source(moves)
}

func TestSimulationStopEligibilityUsesExplicitSupervisorPhases(t *testing.T) {
	for _, test := range []struct {
		name  string
		phase supervisorAttemptPhase
		want  bool
	}{
		{name: "running", phase: supervisorRunning, want: true},
		{name: "intent latched", phase: supervisorIntentLatched, want: true},
		{name: "emergency draining", phase: supervisorEmergencyDraining, want: true},
		{name: "releasing domain", phase: supervisorReleasingDomain, want: true},
		{name: "transferring residual custody", phase: supervisorTransferringResidualCustody, want: true},
		{name: "settling runtime", phase: supervisorSettlingRuntime, want: true},
		{name: "awaiting emergency settlement", phase: supervisorAwaitingEmergencySettlement, want: true},
		{name: "closing prospective", phase: supervisorClosingProspective, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := simulationEngine{supervisor: supervisorState{attempts: []supervisorAttemptState{{
				generation: 1,
				phase:      test.phase,
			}}}}
			assert.Equal(t, test.want, engine.supervisorAcceptsStop(1), "stop eligibility for phase %d", test.phase)
		})
	}
}

func TestSimulationExploresAndReplaysEmptyCatalogueThroughProductionOwners(t *testing.T) {
	definition := simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-a",
			lineage:  11,
			command:  []string{"go", "test", "./..."},
			profile:  AutomaticProfile,
			peers:    2,
		},
		capacity: 2,
	}

	explored := Explore(definition, simulationChoiceBytes{0, 1, 2})
	assert.Nil(t, explored.failure, "exploration failure=%v", explored.failure)
	{
		got, want := explored.world.campaign.outcome, (campaignOutcome)(noMutantsOutcome{})
		assert.Equal(t, want, got, "explored outcome=%#v, want %#v", got, want)
	}
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
		campaign: campaignDefinition{
			identity: "campaign-supervised",
			lineage:  21,
			command:  []string{"go", "test", "./..."},
			profile:  AutomaticProfile,
			peers:    2,
		},
		capacity:  2,
		catalogue: []mutantIdentity{"mutant-a"},
	}

	explored := Explore(definition, simulationChoiceBytes{0, 2})
	assert.Nil(t, explored.failure, "exploration failure=%v", explored.failure)
	{
		_, ok := explored.world.campaign.outcome.(abortedOutcome)
		require.True(t, ok, "explored outcome=%#v, want aborted baseline", explored.world.campaign.outcome)
	}
	var supervisorKinds []supervisorEventKind
	for _, record := range explored.trace.records {
		if record.authority == simulationSupervisorAuthority {
			supervisorKinds = append(supervisorKinds, record.supervisorEvent.kind)
		}
	}
	wantSupervisorKinds := []supervisorEventKind{
		supervisorProspectiveRegistered, supervisorLaunchCompleted, supervisorRunningObserved,
		supervisorDrainCompleted, supervisorDrainCompleted, supervisorOutputCompleted,
		supervisorStopAdmissionSealed, supervisorReleaseCompleted, supervisorRuntimeCompleted,
	}
	assert.Equal(t, wantSupervisorKinds, supervisorKinds, "supervisor lifecycle=%v, want %v", supervisorKinds, wantSupervisorKinds)
	assert.EqualValues(t, 0, len(explored.world.supervisor.attempts), "terminal world is not quiescent: %#v", explored.world)
	assert.EqualValues(t, 0, explored.world.runtime.CampaignCount(), "terminal world is not quiescent: %#v", explored.world)

	replayed := ReplayLegal(explored.trace)
	assert.Nil(t, replayed.failure, "replay failure=%v", replayed.failure)
	assert.Equal(t, explored.world, replayed.world, "replayed world diverged:\n got: %#v\nwant: %#v", replayed.world, explored.world)
}

func TestSimulationChoiceSourceSelectsCanonicalLegalLaunchBoundaryFacts(t *testing.T) {
	definition := simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-boundary", lineage: 22, command: []string{"test"},
			profile: AutomaticProfile, peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}
	for _, test := range []struct {
		name    string
		choice  byte
		kind    supervisorEventKind
		equalAt bool
	}{
		{name: "completion before", choice: 0, kind: supervisorLaunchCompleted},
		{name: "completion at equality", choice: 1, kind: supervisorLaunchBoundary, equalAt: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			explored := Explore(definition, simulationChoiceBytes{test.choice})
			var launch supervisorEvent
			var launchBy time.Time
			for _, record := range explored.trace.records {
				if record.authority == simulationSupervisorAuthority &&
					record.supervisorEvent.kind == supervisorProspectiveRegistered {
					launchBy = record.supervisorEvent.launchBy.production()
				}
				if record.authority == simulationSupervisorAuthority &&
					(record.supervisorEvent.kind == supervisorLaunchCompleted ||
						record.supervisorEvent.kind == supervisorLaunchBoundary) {
					launch = record.supervisorEvent.production()
					break
				}
			}
			require.Equal(t, test.kind, launch.kind, "selected launch fact=%#v, want kind %v", launch, test.kind)
			require.NotNil(t, launch.completion, "selected launch fact=%#v, want kind %v", launch, test.kind)
			{
				got := launch.completion.at.Equal(launchBy)
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
		action  supervisorActionKind
		variant simulationMoveVariant
		outcome func(simulationWorld) bool
		observe func(simulationTrace) bool
	}{
		{
			name: "launch after", action: supervisorLaunchNative,
			variant: simulationMoveVariant{launch: simulationLaunchAfterBoundary},
			outcome: func(world simulationWorld) bool {
				_, aborted := world.campaign.outcome.(abortedOutcome)

				return aborted && world.campaign.failure == nil
			},
			observe: func(trace simulationTrace) bool {
				var launchBy simulationInstant
				for _, record := range trace.records {
					if record.authority != simulationSupervisorAuthority {
						continue
					}
					if record.supervisorEvent.kind == supervisorProspectiveRegistered {
						launchBy = record.supervisorEvent.launchBy
					}
					if record.supervisorEvent.kind == supervisorLaunchCompleted &&
						record.supervisorEvent.at.production().After(launchBy.production()) {
						return true
					}
				}

				return false
			},
		},
		{
			name: "deadline after", action: supervisorWaitRoot,
			variant: simulationMoveVariant{running: simulationRunningAfterDeadline},
			outcome: func(world simulationWorld) bool {
				completed, ok := world.campaign.outcome.(completedOutcome)

				return ok && world.campaign.failure == nil && len(completed.mutants) == 1 &&
					completed.mutants[0].kind == mutantTimedOut
			},
			observe: func(trace simulationTrace) bool {
				for _, record := range trace.records {
					if record.authority != simulationSupervisorAuthority ||
						record.supervisorEvent.kind != supervisorRunningObserved ||
						record.supervisorEvent.running == nil {
						continue
					}
					for _, fact := range record.supervisorEvent.running.facts {
						if fact.kind == supervisorRunningRootExited &&
							fact.at.production().After(record.supervisorEvent.running.exitRecheck.at.production()) {
							return true
						}
					}
				}

				return false
			},
		},
		{
			name: "drain after", action: supervisorObserveEmptiness,
			variant: simulationMoveVariant{drain: simulationDrainAfterBoundary},
			outcome: func(world simulationWorld) bool {
				_, failed := world.campaign.failure.(cleanupUnconfirmedFault)

				return failed && world.runtime.Unconfirmed()
			},
			observe: func(trace simulationTrace) bool {
				drainBy := make(map[supervisorActionToken]simulationInstant)
				for _, record := range trace.records {
					for _, action := range record.supervisorActions {
						if action.kind == supervisorObserveEmptiness {
							drainBy[action.token] = action.drainBy
						}
					}
					if record.authority == simulationSupervisorAuthority &&
						record.supervisorEvent.kind == supervisorDrainCompleted &&
						record.supervisorEvent.at.production().After(
							drainBy[supervisorActionToken(record.source.identity)].production(),
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
					if move.action.kind == test.action && move.attemptKind == campaignAttemptPrimary &&
						move.variant == test.variant {
						selected = true

						return index
					}
				}
				for index, move := range moves {
					if move.action.kind != supervisorWaitRoot {
						return index
					}
				}

				return 0
			})
			explored := Explore(simulationDefinition{
				campaign: campaignDefinition{
					identity: "campaign-after-boundary", lineage: 221, command: []string{"test"},
					profile: AutomaticProfile, peers: 1,
				},
				capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
			}, choices)
			assert.Nil(t, explored.failure, "after-boundary selected=%v outcome=%T campaign-failure=%T simulation-failure=%v", selected, explored.world.campaign.outcome, explored.world.campaign.failure, explored.failure)
			assert.True(t, selected, "after-boundary selected=%v outcome=%T campaign-failure=%T simulation-failure=%v", selected, explored.world.campaign.outcome, explored.world.campaign.failure, explored.failure)
			assert.True(t, test.outcome(explored.world), "after-boundary selected=%v outcome=%T campaign-failure=%T simulation-failure=%v", selected, explored.world.campaign.outcome, explored.world.campaign.failure, explored.failure)
			assert.True(t, test.observe(explored.trace), "after-boundary selected=%v outcome=%T campaign-failure=%T simulation-failure=%v", selected, explored.world.campaign.outcome, explored.world.campaign.failure, explored.failure)
			if test.action == supervisorLaunchNative {
				order := make(map[supervisorActionKind]int)
				ordinal := 0
				for _, record := range explored.trace.records {
					for _, action := range record.supervisorActions {
						ordinal++
						order[action.kind] = ordinal
					}
				}
				assert.False(t, order[supervisorRevokeLaunchRelease] >= order[supervisorPublishLaunchUnconfirmed], "late closure/adoption action order=%v", order)
				assert.False(t, order[supervisorPublishLaunchUnconfirmed] >= order[supervisorAdoptOwned], "late closure/adoption action order=%v", order)
				assert.False(t, order[supervisorAdoptOwned] >= order[supervisorForceOwned], "late closure/adoption action order=%v", order)
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
		campaign: campaignDefinition{
			identity: "campaign-primary", lineage: 23, command: []string{"test"},
			profile: AutomaticProfile, peers: 1,
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
					if move.action.kind == supervisorWaitRoot && move.attemptKind == campaignAttemptPrimary &&
						move.variant.running == test.variant {
						return index
					}
				}
				for index, move := range moves {
					if move.action.kind != supervisorWaitRoot {
						return index
					}
				}

				return 0
			})
			explored := Explore(definition, choices)
			completed, ok := explored.world.campaign.outcome.(completedOutcome)
			require.Nil(t, explored.failure, "completed outcome=%#v failure=%v, want %v; choices=%#v", explored.world.campaign.outcome, explored.failure, test.want, explored.trace.choices)
			assert.True(t, ok, "completed outcome=%#v failure=%v, want %v; choices=%#v", explored.world.campaign.outcome, explored.failure, test.want, explored.trace.choices)
			require.Len(t, completed.mutants, 1, "completed outcome=%#v failure=%v, want %v; choices=%#v", explored.world.campaign.outcome, explored.failure, test.want, explored.trace.choices)
			assert.Equal(t, test.want, completed.mutants[0].kind, "completed outcome=%#v failure=%v, want %v; choices=%#v", explored.world.campaign.outcome, explored.failure, test.want, explored.trace.choices)
			assert.EqualValues(t, 2, explored.world.campaign.commandCount(), "terminal commands/obligations=%d/%#v", explored.world.campaign.commandCount(), explored.world.campaign.obligations)
			assert.EqualValues(t, 0, len(explored.world.campaign.obligations), "terminal commands/obligations=%d/%#v", explored.world.campaign.commandCount(), explored.world.campaign.obligations)
			replayed := ReplayLegal(explored.trace)
			assert.Nil(t, replayed.failure, "outcome replay diverged: %#v", replayed)
			assert.Equal(t, explored.world, replayed.world, "outcome replay diverged: %#v", replayed)
		})
	}
}

func TestSimulationExploresEveryCatalogueMemberInStableOrder(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-catalogue", lineage: 24, command: []string{"test"},
			profile: SerialProfile, peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, simulationChoiceBytes{0})
	completed, ok := explored.world.campaign.outcome.(completedOutcome)
	require.Nil(t, explored.failure, "catalogue exploration=%#v failure=%v", explored.world.campaign.outcome, explored.failure)
	assert.True(t, ok, "catalogue exploration=%#v failure=%v", explored.world.campaign.outcome, explored.failure)
	got := make([]mutantIdentity, len(completed.mutants))
	for index, mutant := range completed.mutants {
		got[index] = mutant.mutant
	}
	{
		want := []mutantIdentity{"mutant-a", "mutant-b"}
		assert.Equal(t, want, got, "completed catalogue order=%v, want %v", got, want)
	}
	assert.EqualValues(t, 3, explored.world.campaign.commandCount(), "command count=%d, want baseline plus two primaries", explored.world.campaign.commandCount())
}

func TestSimulationExploresOneMutantWithSparePeerCapacity(t *testing.T) {
	result := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-spare-capacity", lineage: 43, command: []string{"test"},
			profile: AutomaticProfile, peers: 4,
		},
		capacity: 4, catalogue: []mutantIdentity{"mutant-a"},
	}, nil)
	completed, ok := result.world.campaign.outcome.(completedOutcome)
	assert.Nil(t, result.failure, "spare-capacity exploration outcome=%#v failure=%v", result.world.campaign.outcome, result.failure)
	assert.True(t, ok, "spare-capacity exploration outcome=%#v failure=%v", result.world.campaign.outcome, result.failure)
	assert.EqualValues(t, 1, len(completed.mutants), "spare-capacity exploration outcome=%#v failure=%v", result.world.campaign.outcome, result.failure)
}

func TestSimulationExploresPeerPrimaryOverlapFromEmittedEffectWave(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-overlap", lineage: 25, command: []string{"test"},
			profile: AutomaticProfile, peers: 2,
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
			if _, ok := move.delivery.(admissionGrantedEvent); ok {
				grantAt = index
				break
			}
		}
		if grantAt >= 0 {
			for index, move := range moves {
				if index == grantAt || move.action.kind == supervisorWaitRoot {
					continue
				}
				delayed = true

				return index
			}

			return grantAt
		}
		for index, move := range moves {
			if move.action.kind != supervisorWaitRoot {
				return index
			}
		}

		return 0
	})
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-late-grant", lineage: 2511, command: []string{"test"},
			profile: AutomaticProfile, peers: 2,
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
			if move.action.kind == supervisorWaitRoot && move.attemptKind == campaignAttemptPrimary &&
				move.variant.running == simulationRunningAtDeadline {
				return index
			}
		}
		for index, move := range moves {
			if move.action.kind != supervisorWaitRoot {
				return index
			}
		}

		return 0
	})
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-repeated-intrinsic-deadline", lineage: 2512, command: []string{"test"},
			profile: AutomaticProfile, peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, choices)
	completed, ok := explored.world.campaign.outcome.(completedOutcome)
	require.Nil(t, explored.failure, "repeated-deadline exploration=%#v failure=%v", explored.world.campaign.outcome, explored.failure)
	assert.True(t, ok, "repeated-deadline exploration=%#v failure=%v", explored.world.campaign.outcome, explored.failure)
	assert.EqualValues(t, 2, len(completed.mutants), "repeated-deadline exploration=%#v failure=%v", explored.world.campaign.outcome, explored.failure)
	for _, mutant := range completed.mutants {
		assert.Equal(t, mutantTimedOut, mutant.kind, "repeated-deadline mutant=%#v, want direct deadline without confirmation", mutant)
		assert.Equal(t, campaignEvidenceDeadline, mutant.primary.kind, "repeated-deadline mutant=%#v, want direct deadline without confirmation", mutant)
		assert.Equal(t, campaignAttemptEvidenceKind(0), mutant.confirmation.kind, "repeated-deadline mutant=%#v, want direct deadline without confirmation", mutant)
	}
	assert.False(t, explored.world.runtime.SingleAdmission(), "repeated-deadline admission mode/fallback=%v/%v", explored.world.runtime.SingleAdmission(), completed.singleAdmissionFallback)
	assert.False(t, completed.singleAdmissionFallback, "repeated-deadline admission mode/fallback=%v/%v", explored.world.runtime.SingleAdmission(), completed.singleAdmissionFallback)
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
				peerReady = peerReady || move.action.kind == supervisorWaitRoot &&
					move.attemptKind == campaignAttemptPrimary && move.mutant == "mutant-b"
			}
			if peerReady {
				for index, move := range moves {
					if move.action.kind == supervisorWaitRoot &&
						move.variant.running == simulationRunningAtDeadline &&
						move.attemptKind == campaignAttemptPrimary && move.mutant == "mutant-a" {
						timedOut = true

						return index
					}
				}
			}
		}
		for index, move := range moves {
			if move.action.kind != supervisorWaitRoot {
				return index
			}
		}

		return 0
	})
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-confirmation", lineage: 252, command: []string{"test"},
			profile: AutomaticProfile, peers: 2,
		},
		capacity: 2, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, choices)
	completed, ok := explored.world.campaign.outcome.(completedOutcome)
	require.Nil(t, explored.failure, "confirmation exploration=%#v failure=%v", explored.world.campaign.outcome, explored.failure)
	assert.True(t, ok, "confirmation exploration=%#v failure=%v", explored.world.campaign.outcome, explored.failure)
	assert.True(t, timedOut, "confirmation exploration=%#v failure=%v", explored.world.campaign.outcome, explored.failure)
	require.Len(t, completed.mutants, 2, "confirmation exploration=%#v failure=%v", explored.world.campaign.outcome, explored.failure)
	assert.Equal(t, mutantSurvived, completed.mutants[0].kind, "confirmation outcome=%#v commands=%d", completed, explored.world.campaign.commandCount())
	assert.Equal(t, campaignEvidenceDeadline, completed.mutants[0].primary.kind, "confirmation outcome=%#v commands=%d", completed, explored.world.campaign.commandCount())
	assert.Equal(t, campaignEvidenceSettled, completed.mutants[0].confirmation.kind, "confirmation outcome=%#v commands=%d", completed, explored.world.campaign.commandCount())
	assert.True(t, completed.singleAdmissionFallback, "confirmation outcome=%#v commands=%d", completed, explored.world.campaign.commandCount())
	assert.EqualValues(t, 4, explored.world.campaign.commandCount(), "confirmation outcome=%#v commands=%d", completed, explored.world.campaign.commandCount())
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
			if move.action.kind == supervisorWaitRoot && move.attemptKind == campaignAttemptPrimary {
				ready++
			}
		}
		if ready >= 2 || len(timedOut) == len(catalogue)-1 {
			for _, mutant := range catalogue {
				if timedOut[mutant] {
					continue
				}
				for index, move := range moves {
					if move.action.kind == supervisorWaitRoot && move.attemptKind == campaignAttemptPrimary &&
						move.mutant == mutant &&
						move.variant.running == simulationRunningAtDeadline {
						timedOut[mutant] = true

						return index
					}
				}
			}
		}
		for index, move := range moves {
			if move.action.kind != supervisorWaitRoot {
				return index
			}
		}

		return 0
	})
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-multiple-provisionals", lineage: 2521, command: []string{"test"},
			profile: AutomaticProfile, peers: 3,
		},
		capacity: 3, catalogue: catalogue,
	}, choices)
	completed, ok := explored.world.campaign.outcome.(completedOutcome)
	require.Nil(t, explored.failure, "multiple-provisional exploration=%#v timed=%v failure=%v", explored.world.campaign.outcome, timedOut, explored.failure)
	assert.True(t, ok, "multiple-provisional exploration=%#v timed=%v failure=%v", explored.world.campaign.outcome, timedOut, explored.failure)
	assert.Equal(t, len(catalogue), len(timedOut), "multiple-provisional exploration=%#v timed=%v failure=%v", explored.world.campaign.outcome, timedOut, explored.failure)
	assert.Equal(t, len(catalogue), len(completed.mutants), "multiple-provisional exploration=%#v timed=%v failure=%v", explored.world.campaign.outcome, timedOut, explored.failure)
	for index, mutant := range completed.mutants {
		assert.Equal(t, catalogue[index], mutant.mutant, "multiple-provisional mutant[%d]=%#v", index, mutant)
		assert.Equal(t, campaignEvidenceDeadline, mutant.primary.kind, "multiple-provisional mutant[%d]=%#v", index, mutant)
		assert.Equal(t, campaignEvidenceSettled, mutant.confirmation.kind, "multiple-provisional mutant[%d]=%#v", index, mutant)
	}
	var barriers, confirmations []mutantIdentity
	for _, record := range explored.trace.records {
		for _, effect := range record.campaignEffects {
			if effect.kind == campaignEffectBindConfirmationBarrier {
				barriers = append(barriers, effect.mutant)
			}
			if effect.kind == campaignEffectLaunchAttempt && effect.attemptKind == campaignAttemptConfirmation {
				attemptAt := slices.IndexFunc(record.campaignState.attempts, func(attempt campaignAttempt) bool {
					return attempt.identity == effect.attempt
				})
				require.False(t, attemptAt < 0, "confirmation launch attempt %q is absent", effect.attempt)
				confirmations = append(confirmations, record.campaignState.attempts[attemptAt].mutant)
			}
		}
	}
	assert.Equal(t, catalogue[:1], barriers, "confirmation barrier/FIFO order=%v/%v, want %v/%v", barriers, confirmations, catalogue[:1], catalogue)
	assert.Equal(t, catalogue, confirmations, "confirmation barrier/FIFO order=%v/%v, want %v/%v", barriers, confirmations, catalogue[:1], catalogue)
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
			return move.action.kind == supervisorWaitRoot && move.mutant == "mutant-a"
		})
		if ownedPeerReady {
			for index, move := range moves {
				if move.action.kind == supervisorLaunchNative && move.mutant == "mutant-b" &&
					move.variant.launch == simulationLaunchProvenNotReleased {
					selected = true

					return index
				}
			}
		}
		for index, move := range moves {
			if move.action.kind == supervisorLaunchNative && move.mutant == "mutant-b" {
				continue
			}
			if move.action.kind != supervisorWaitRoot {
				return index
			}
		}

		return 0
	})
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-release-stop", lineage: 2522, command: []string{"test"},
			profile: AutomaticProfile, peers: 2,
		},
		capacity: 2, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, choices)
	assert.Nil(t, explored.failure, "release/stop exploration selected=%v failure=%v", selected, explored.failure)
	assert.True(t, selected, "release/stop exploration selected=%v failure=%v", selected, explored.failure)
	publicationAt, stopEffectAt, stopFactAt := -1, -1, -1
	for index, record := range explored.trace.records {
		for _, action := range record.supervisorActions {
			if action.kind == supervisorPublishNotReleased {
				publicationAt = index
			}
		}
		for _, effect := range record.campaignEffects {
			if effect.kind == campaignEffectStopAttempt {
				stopEffectAt = index
			}
		}
		if record.supervisorEvent.running != nil && slices.ContainsFunc(
			record.supervisorEvent.running.facts,
			func(fact simulationSupervisorRunningFact) bool {
				return fact.kind == supervisorRunningStopRequested
			},
		) {
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
			return move.action.kind == supervisorWaitRoot && move.mutant == "mutant-b"
		})
		if ownedPeerReady {
			for index, move := range moves {
				if move.action.kind == supervisorLaunchNative && move.mutant == "mutant-a" &&
					move.variant.launch == simulationLaunchAfterBoundary {
					selected = true

					return index
				}
			}
		}
		for index, move := range moves {
			if move.action.kind == supervisorLaunchNative && move.mutant == "mutant-a" {
				continue
			}
			if move.action.kind != supervisorWaitRoot {
				return index
			}
		}

		return 0
	})
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-custody-order", lineage: 2523, command: []string{"test"},
			profile: AutomaticProfile, peers: 2,
		},
		capacity: 2, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, choices)
	assert.Nil(t, explored.failure, "custody-order exploration selected=%v failure=%v", selected, explored.failure)
	assert.True(t, selected, "custody-order exploration selected=%v failure=%v", selected, explored.failure)
	var stages []admissionStage
	for _, record := range explored.trace.records {
		_, observation, observed := record.runtimeCut.Observation()
		if record.authority != simulationRuntimeAuthority || !observed ||
			observation.Kind() != processruntime.LaunchUnconfirmedKind {
			continue
		}
		for _, residual := range record.runtimeState.Residual() {
			stage := admissionOwned
			if residual.Prospective() {
				stage = admissionProspective
			}
			stages = append(stages, stage)
		}
		break
	}
	assert.Equal(t, []admissionStage{admissionProspective, admissionOwned}, stages, "unconfirmed prospective/owned custody order=%v", stages)
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
			if move.action.kind == supervisorObserveEmptiness &&
				move.variant.drain == simulationDrainAtBoundary &&
				move.attemptKind == campaignAttemptPrimary {
				expired = true

				return index
			}
		}
		for index, move := range moves {
			if move.action.kind != supervisorWaitRoot {
				return index
			}
		}

		return 0
	})
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-drain-expiry", lineage: 253, command: []string{"test"},
			profile: AutomaticProfile, peers: 1,
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
		require.FailNowf(t, "drain-expiry exploration failed", "failure=%v expired=%v phase=%v event=%v obligations=%d campaign-failure=%T", explored.failure, expired, last.campaignState.phase, last.campaignEvent.kind, len(last.campaignState.obligations), last.campaignState.failure)
	}
	{
		_, ok := explored.world.campaign.failure.(cleanupUnconfirmedFault)
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

	var repeatedEmergency simulationSupervisorEvent
	var settledAt int
	for index, record := range explored.trace.records {
		if record.authority != simulationSupervisorAuthority {
			continue
		}
		if record.supervisorEvent.kind == supervisorEmergencyStarted {
			repeatedEmergency = record.supervisorEvent
		}
		if record.supervisorEvent.kind == supervisorEmergencySettlementCompleted {
			settledAt = index + 1
		}
	}
	assert.NotEqual(t, 0, repeatedEmergency.kind, "fatal trace lacks emergency start/settlement: start=%v settlement=%d", repeatedEmergency.kind, settledAt)
	assert.NotEqual(t, 0, settledAt, "fatal trace lacks emergency start/settlement: start=%v settlement=%d", repeatedEmergency.kind, settledAt)

	malformed := simulationMalformedFact{
		authority:  simulationSupervisorAuthority,
		supervisor: repeatedEmergency,
	}
	firstViolation := ReplayViolation(explored.trace, malformed)
	secondViolation := ReplayViolation(explored.trace, malformed)
	assert.Nil(t, firstViolation.failure, "repeated closure/invariant dominance first=%#v second=%#v", firstViolation, secondViolation)
	assert.Equal(t, supervisorReducerOperation, firstViolation.invariant.operation, "repeated closure/invariant dominance first=%#v second=%#v", firstViolation, secondViolation)
	assert.EqualValues(t, "emergency epoch is invalid, duplicated, or conflicting", firstViolation.invariant.reason, "repeated closure/invariant dominance first=%#v second=%#v", firstViolation, secondViolation)
	assert.Equal(t, secondViolation, firstViolation, "repeated closure/invariant dominance first=%#v second=%#v", firstViolation, secondViolation)
	assert.Equal(t, explored.world.campaign.outcome, firstViolation.world.campaign.outcome, "repeated closure/invariant dominance first=%#v second=%#v", firstViolation, secondViolation)
	assert.Equal(t, explored.world.campaign.failure, firstViolation.world.campaign.failure, "repeated closure/invariant dominance first=%#v second=%#v", firstViolation, secondViolation)
}

func TestSimulationChoiceSourceSelectsEnabledPeerSettlementOrder(t *testing.T) {
	definition := simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-choice-order", lineage: 251, command: []string{"test"},
			profile: AutomaticProfile, peers: 2,
		},
		capacity: 2, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}
	explorePreferred := func(preferred mutantIdentity) SimulationResult {
		return Explore(definition, simulationFocusedChoiceSource(func(moves []simulationEngineMove) int {
			for index, move := range moves {
				if move.effect.kind == campaignEffectMaterializeWorkspace && move.effect.mutant == preferred {
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
			terminal, ok := record.campaignEvent.production().payload.(attemptTerminalEvent)
			if ok {
				attempts = append(attempts, terminal.attempt)
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
		campaign: campaignDefinition{
			identity: "campaign-resource", lineage: 26, command: []string{"test"},
			profile: SerialProfile, peers: 1,
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
		settled, ok := record.campaignEvent.production().payload.(resourceSettledEvent)
		if !ok || settled.kind != campaignResourceWorkspace {
			continue
		}
		settled.identity = "workspace-not-released"
		trace.records[index].campaignEvent = simulationTraceCampaignEvent(campaignEvent{
			id: trace.records[index].campaignEvent.id, payload: settled,
		})
		break
	}

	replayed := ReplayLegal(trace)
	require.NotNil(t, replayed.failure, "wrong resource replay failure=%v, want exact resource rejection", replayed.failure)
	assert.Contains(t, replayed.failure.Error(), "external campaign fact is not enabled", "wrong resource replay failure=%v, want exact resource rejection", replayed.failure)
}

func TestSimulationViolationReplayCleansRuntimeAndRetainsTypedInvariant(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-violation", lineage: 31, command: []string{"test"},
			profile: AutomaticProfile, peers: 1,
		},
		capacity: 1,
	}, nil)
	prefix := simulationTrace{
		definition: explored.trace.definition,
		records:    append([]simulationRecord(nil), explored.trace.records[:2]...),
	}
	malformed := simulationMalformedFact{
		authority: simulationCampaignAuthority,
		campaign: simulationTraceCampaignEvent(campaignEvent{
			id: 1, payload: snapshotEstablishedEvent{},
		}),
	}

	first := ReplayViolation(prefix, malformed)
	second := ReplayViolation(prefix, malformed)
	assert.Nil(t, first.failure, "violation replay failures=%v/%v", first.failure, second.failure)
	assert.Nil(t, second.failure, "violation replay failures=%v/%v", first.failure, second.failure)
	assert.Equal(t, second, first, "violation replay is nondeterministic:\nfirst=%#v\nsecond=%#v", first, second)
	assert.EqualValues(t, "campaign establish snapshot", first.invariant.operation, "retained invariant=%#v", first.invariant)
	assert.EqualValues(t, "snapshot observation is invalid", first.invariant.reason, "retained invariant=%#v", first.invariant)
	assert.Equal(t, simulationCampaignAuthority, first.key.authority, "failure key=%#v, invariant=%#v", first.key, first.invariant)
	assert.Equal(t, first.invariant.operation, first.key.operation, "failure key=%#v, invariant=%#v", first.key, first.invariant)
	assert.Equal(t, first.invariant.reason, first.key.reason, "failure key=%#v, invariant=%#v", first.key, first.invariant)
	assert.True(t, first.world.runtime.Drained(), "runtime cleanup=%#v", first.world.runtime)
	assert.NotEqual(t, 0, first.world.runtime.FatalEpoch(), "runtime cleanup=%#v", first.world.runtime)
	assert.EqualValues(t, 1, first.world.runtime.FatalCauseCount(), "runtime cleanup=%#v", first.world.runtime)
}

func TestSimulationViolationReplayRejectsWrongSupervisorActionAndCleansCustody(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-supervisor-violation", lineage: 32, command: []string{"test"},
			profile: AutomaticProfile, peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}, simulationChoiceBytes{simulationChooseBaselineFailure})
	prefixLength := 0
	var registered supervisorEvent
	for index, record := range explored.trace.records {
		if record.authority == simulationSupervisorAuthority &&
			record.supervisorEvent.kind == supervisorProspectiveRegistered {
			prefixLength = index + 1
			registered = record.supervisorEvent.production()
			break
		}
	}
	prefix := simulationTrace{
		definition: explored.trace.definition,
		records:    append([]simulationRecord(nil), explored.trace.records[:prefixLength]...),
	}
	completedAt := registered.launchBy.Add(-time.Nanosecond)
	malformed := simulationMalformedFact{
		authority: simulationSupervisorAuthority,
		supervisor: simulationTraceSupervisorEvent(supervisorEvent{
			kind: supervisorLaunchCompleted, generation: registered.generation, at: completedAt,
			completion: &supervisorLaunchCompletion{
				generation: registered.generation, action: 999, at: completedAt,
				kind: supervisorLaunchReleased,
			},
		}),
	}

	first := ReplayViolation(prefix, malformed)
	second := ReplayViolation(prefix, malformed)
	assert.Nil(t, first.failure, "supervisor violation replay diverged: first=%#v second=%#v", first, second)
	assert.Equal(t, second, first, "supervisor violation replay diverged: first=%#v second=%#v", first, second)
	assert.Equal(t, simulationSupervisorAuthority, first.key.authority, "supervisor invariant/key=%#v/%#v", first.invariant, first.key)
	assert.Equal(t, supervisorReducerOperation, first.invariant.operation, "supervisor invariant/key=%#v/%#v", first.invariant, first.key)
	residual := first.world.runtime.Residual()
	assert.True(t, first.world.runtime.Unconfirmed(), "supervisor violation cleanup did not transfer exact custody: %#v", first.world.runtime)
	require.Len(t, residual, 1, "supervisor violation cleanup did not transfer exact custody: %#v", first.world.runtime)
	assert.Equal(t, registered.generation, residual[0].Generation(), "supervisor violation cleanup did not transfer exact custody: %#v", first.world.runtime)
	assert.True(t, residual[0].Transferred(), "supervisor violation cleanup did not transfer exact custody: %#v", first.world.runtime)
}

func TestSimulationViolationReplayCoversNamedSupervisorCorruptions(t *testing.T) {
	choices := simulationFocusedChoiceSource(func(moves []simulationEngineMove) int {
		for index, move := range moves {
			if move.action.kind == supervisorLaunchNative &&
				move.variant.launch == simulationLaunchAtBoundary {
				return index
			}
		}
		for index, move := range moves {
			if move.action.kind != supervisorWaitRoot {
				return index
			}
		}

		return 0
	})
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-supervisor-corruption-families", lineage: 321, command: []string{"test"},
			profile: AutomaticProfile, peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}, choices)
	assert.Nil(t, explored.failure, "supervisor corruption exploration failure=%v", explored.failure)
	registeredAt, boundaryAt := -1, -1
	var registered simulationRecord
	for index, record := range explored.trace.records {
		if record.authority != simulationSupervisorAuthority {
			continue
		}
		if registeredAt < 0 && record.supervisorEvent.kind == supervisorProspectiveRegistered {
			registeredAt, registered = index, record
		}
		if registeredAt >= 0 && record.supervisorEvent.kind == supervisorLaunchBoundary &&
			record.supervisorEvent.generation == registered.supervisorEvent.generation {
			boundaryAt = index
			break
		}
	}
	assert.False(t, registeredAt < 0, "supervisor corruption cuts registration/boundary=%d/%d", registeredAt, boundaryAt)
	assert.False(t, boundaryAt < 0, "supervisor corruption cuts registration/boundary=%d/%d", registeredAt, boundaryAt)
	launchAction := registered.supervisorActions[0].token
	registrationPrefix := simulationTrace{
		definition: explored.trace.definition,
		records:    slices.Clone(explored.trace.records[:registeredAt+1]),
	}
	boundaryPrefix := simulationTrace{
		definition: explored.trace.definition,
		records:    slices.Clone(explored.trace.records[:boundaryAt+1]),
	}
	registeredEvent := registered.supervisorEvent.production()
	completionAt := registeredEvent.launchBy.Add(-time.Nanosecond)
	tests := []struct {
		name      string
		prefix    simulationTrace
		malformed supervisorEvent
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
			malformed: supervisorEvent{generation: registeredEvent.generation, at: completionAt},
			reason:    "event kind is invalid",
		},
		{
			name:   "contradictory released completion",
			prefix: registrationPrefix,
			malformed: supervisorEvent{
				kind: supervisorLaunchCompleted, generation: registeredEvent.generation, at: completionAt,
				completion: &supervisorLaunchCompletion{
					generation: registeredEvent.generation, action: launchAction, at: completionAt,
					kind: supervisorLaunchReleased, failure: LaunchFailed,
				},
			},
			reason: "released completion carries a launch failure",
		},
		{
			name:   "output completion in launch phase",
			prefix: registrationPrefix,
			malformed: supervisorEvent{
				kind: supervisorOutputCompleted, generation: registeredEvent.generation, at: completionAt,
				output: &supervisorOutputCompletion{
					generation: registeredEvent.generation,
					action:     supervisorPendingAction{kind: supervisorCaptureOutput, token: launchAction},
					at:         completionAt, ref: 1,
				},
			},
			reason: "output completion correlation, evidence, or shape is invalid",
		},
		{
			name:   "late release after boundary revocation",
			prefix: boundaryPrefix,
			malformed: supervisorEvent{
				kind: supervisorLaunchCompleted, generation: registeredEvent.generation,
				at: registeredEvent.launchBy.Add(time.Nanosecond),
				completion: &supervisorLaunchCompletion{
					generation: registeredEvent.generation, action: launchAction,
					at: registeredEvent.launchBy.Add(time.Nanosecond), kind: supervisorLaunchReleased,
				},
			},
			reason: "launch completion was duplicated after closure",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			malformed := simulationMalformedFact{
				authority:  simulationSupervisorAuthority,
				supervisor: simulationTraceSupervisorEvent(test.malformed),
			}
			first := ReplayViolation(test.prefix, malformed)
			second := ReplayViolation(test.prefix, malformed)
			assert.Nil(t, first.failure, "supervisor corruption replay diverged: first=%#v second=%#v", first, second)
			assert.Equal(t, second, first, "supervisor corruption replay diverged: first=%#v second=%#v", first, second)
			assert.Equal(t, supervisorReducerOperation, first.invariant.operation, "supervisor corruption invariant=%#v", first.invariant)
			assert.Equal(t, test.reason, first.invariant.reason, "supervisor corruption invariant=%#v", first.invariant)
		})
	}
}

func TestSimulationViolationReplayRejectsMalformedRuntimeAdmissionAndCleansCustody(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-runtime-violation", lineage: 33, command: []string{"test"},
			profile: AutomaticProfile, peers: 1,
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
	assert.EqualValues(t, "request admission", first.invariant.operation, "runtime invariant/key=%#v/%#v", first.invariant, first.key)
	assert.EqualValues(t, "invalid request", first.invariant.reason, "runtime invariant/key=%#v/%#v", first.invariant, first.key)
	assert.True(t, first.world.runtime.Drained(), "runtime violation cleanup retained custody: %#v", first.world.runtime)
	assert.EqualValues(t, 0, first.world.runtime.AdmissionCount(), "runtime violation cleanup retained custody: %#v", first.world.runtime)
}

func TestSimulationViolationReplayRejectsStaleGrantReturnAndCleansRuntime(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-runtime-return-violation", lineage: 34, command: []string{"test"},
			profile: AutomaticProfile, peers: 1,
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
	assert.EqualValues(t, "acknowledge grant return", first.invariant.operation, "stale return invariant/key=%#v/%#v", first.invariant, first.key)
	assert.EqualValues(t, "grant return authority is stale or wrong", first.invariant.reason, "stale return invariant/key=%#v/%#v", first.invariant, first.key)
	assert.True(t, first.world.runtime.Drained(), "stale return cleanup=%#v", first.world.runtime)
	assert.EqualValues(t, 0, first.world.runtime.AdmissionCount(), "stale return cleanup=%#v", first.world.runtime)
}

func TestSimulationViolationReplayCoversRuntimeObservationEmergencyAndClosureFamilies(t *testing.T) {
	prefix := simulationTrace{definition: simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-runtime-families", lineage: 35, command: []string{"test"},
			profile: AutomaticProfile, peers: 1,
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
			assert.Equal(t, test.operation, first.invariant.operation, "runtime invariant/key=%#v/%#v", first.invariant, first.key)
			assert.Equal(t, test.reason, first.invariant.reason, "runtime invariant/key=%#v/%#v", first.invariant, first.key)
			assert.True(t, first.world.runtime.Drained(), "runtime violation cleanup=%#v", first.world.runtime)
			assert.EqualValues(t, 0, first.world.runtime.AdmissionCount(), "runtime violation cleanup=%#v", first.world.runtime)
		})
	}
}

func TestSimulationShrinkRemovesLegalRecordsAndDefinitionMembersToFixpoint(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-shrink", lineage: 41, command: []string{"test"},
			profile: AutomaticProfile, peers: 4,
		},
		capacity:  4,
		catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, simulationChoiceBytes{simulationChooseBaselineFailure})
	malformed := simulationMalformedFact{
		authority: simulationCampaignAuthority,
		campaign: simulationTraceCampaignEvent(campaignEvent{
			id: 1, payload: snapshotEstablishedEvent{},
		}),
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
	assert.EqualValues(t, 1, shrunk.definition.capacity, "shrunk capacity/peers=%d/%d, want accepted lower bounds 1/1", shrunk.definition.capacity, shrunk.definition.campaign.peers)
	assert.EqualValues(t, 1, shrunk.definition.campaign.peers, "shrunk capacity/peers=%d/%d, want accepted lower bounds 1/1", shrunk.definition.capacity, shrunk.definition.campaign.peers)
	require.NotNil(t, shrunk.malformed, "shrink removed the one intended corruption")
	first := ReplayViolation(shrunk, *shrunk.malformed)
	second := ReplayViolation(shrunk, *shrunk.malformed)
	assert.Nil(t, first.failure, "shrunk replay did not retain stable failure:\nkey=%#v\nfirst=%#v\nsecond=%#v", key, first, second)
	assert.Equal(t, key, first.key, "shrunk replay did not retain stable failure:\nkey=%#v\nfirst=%#v\nsecond=%#v", key, first, second)
	assert.Equal(t, second, first, "shrunk replay did not retain stable failure:\nkey=%#v\nfirst=%#v\nsecond=%#v", key, first, second)
}

func TestSimulationShrinkRemovesPositiveTraceSuffixAndRetainsReplayFailure(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-positive-shrink", lineage: 46, command: []string{"test"},
			profile: AutomaticProfile, peers: 3,
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
	assert.EqualValues(t, 1, shrunk.definition.campaign.peers, "positive shrunk definition=%#v", shrunk.definition)
	assert.EqualValues(t, 0, len(shrunk.definition.catalogue), "positive shrunk definition=%#v", shrunk.definition)
	assert.EqualValues(t, "campaign-1", shrunk.definition.campaign.identity, "positive shrunk definition=%#v", shrunk.definition)
	assert.EqualValues(t, 1, shrunk.definition.campaign.lineage, "positive shrunk definition=%#v", shrunk.definition)
	first := ReplayLegal(shrunk)
	second := ReplayLegal(shrunk)
	assert.NotNil(t, first.failure, "positive shrunk replay did not retain stable failure:\nkey=%#v\nfirst=%#v\nsecond=%#v", replayed.key, first, second)
	assert.Equal(t, replayed.key, first.key, "positive shrunk replay did not retain stable failure:\nkey=%#v\nfirst=%#v\nsecond=%#v", replayed.key, first, second)
	assert.Equal(t, second, first, "positive shrunk replay did not retain stable failure:\nkey=%#v\nfirst=%#v\nsecond=%#v", replayed.key, first, second)
}

func TestSimulationShrinkMovesPositiveReplayTowardNamedBoundary(t *testing.T) {
	choices := simulationFocusedChoiceSource(func(moves []simulationEngineMove) int {
		for index, move := range moves {
			if move.action.kind == supervisorLaunchNative && move.attemptKind == campaignAttemptPrimary &&
				move.variant.launch == simulationLaunchAfterBoundary {
				return index
			}
		}
		for index, move := range moves {
			if move.action.kind != supervisorWaitRoot {
				return index
			}
		}

		return 0
	})
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-positive-boundary", lineage: 47, command: []string{"test"},
			profile: AutomaticProfile, peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}, choices)
	assert.Nil(t, explored.failure, "positive boundary exploration failure=%v", explored.failure)
	counterexample := simulationCloneTrace(explored.trace)
	cut := -1
	for index, record := range counterexample.records {
		if record.authority == simulationSupervisorAuthority &&
			record.supervisorEvent.kind == supervisorLaunchCompleted {
			cut = index
			break
		}
	}
	require.False(t, cut < 0, "positive boundary trace has no equality cut")
	counterexample.records = slices.Clone(counterexample.records[:cut+1])
	counterexample.records[cut].supervisorState.nextAction++
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
	deadline := simulationTraceInstant(time.Unix(10, 0))
	near := simulationTrace{
		records: []simulationRecord{{
			authority: simulationSupervisorAuthority,
			supervisorEvent: simulationSupervisorEvent{
				kind: supervisorRunningObserved,
				running: &simulationSupervisorRunningBundle{
					exitRecheck: simulationSupervisorExitRecheck{performed: true, at: deadline},
					facts: []simulationSupervisorRunningFact{{
						kind: supervisorRunningRootExited,
						at:   simulationTraceInstant(time.Unix(10, 1)),
					}},
				},
			},
		}},
		choices: []simulationChoiceRecord{{selected: 9}},
	}
	far := simulationCloneTrace(near)
	farRunning := *near.records[0].supervisorEvent.running
	farRunning.facts = slices.Clone(farRunning.facts)
	far.records[0].supervisorEvent.running = &farRunning
	far.records[0].supervisorEvent.running.facts[0].at = simulationTraceInstant(time.Unix(10, 9))
	far.choices[0].selected = 0
	assert.True(t, simulationShrinkMeasureLess(simulationTraceShrinkMeasure(near), simulationTraceShrinkMeasure(far)), "near/far shrink measures=%v/%v", simulationTraceShrinkMeasure(near), simulationTraceShrinkMeasure(far))

	simple := simulationCloneTrace(near)
	simpleRunning := *near.records[0].supervisorEvent.running
	simple.records[0].supervisorEvent.running = &simpleRunning
	simple.records[0].supervisorEvent.running.facts = nil
	assert.True(t, simulationShrinkMeasureLess(simulationTraceShrinkMeasure(simple), simulationTraceShrinkMeasure(near)), "simple/rich payload measures=%v/%v", simulationTraceShrinkMeasure(simple), simulationTraceShrinkMeasure(near))

	uncanonical := simulationTrace{definition: simulationDefinition{
		campaign: campaignDefinition{identity: "a", lineage: 1, peers: 1}, capacity: 1,
	}}
	canonical := simulationCloneTrace(uncanonical)
	canonical.definition.campaign.identity = "campaign-1"
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
		campaign: campaignDefinition{
			identity: "campaign-shrink-causal", lineage: 42, command: []string{"test"},
			profile: AutomaticProfile, peers: 2,
		},
		capacity: 2, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, nil)
	assert.Nil(t, explored.failure, "causal shrink exploration failure=%v", explored.failure)
	malformed := simulationMalformedFact{
		authority: simulationCampaignAuthority,
		campaign: simulationTraceCampaignEvent(campaignEvent{
			id: 1, payload: snapshotEstablishedEvent{},
		}),
	}
	counterexample := simulationCloneTrace(explored.trace)
	counterexample.malformed = &malformed
	key := ReplayViolation(counterexample, malformed).key

	shrunk := Shrink(counterexample, key)
	assert.EqualValues(t, 0, len(shrunk.definition.catalogue), "causal shrink catalogue=%v, want no unrelated mutants", shrunk.definition.catalogue)
	assert.False(t, len(shrunk.records) >= len(counterexample.records), "causal shrink records=%d, want fewer than %d", len(shrunk.records), len(counterexample.records))
	assert.EqualValues(t, 1, shrunk.definition.capacity, "causal shrink capacity/peers=%d/%d, want 1/1", shrunk.definition.capacity, shrunk.definition.campaign.peers)
	assert.EqualValues(t, 1, shrunk.definition.campaign.peers, "causal shrink capacity/peers=%d/%d, want 1/1", shrunk.definition.capacity, shrunk.definition.campaign.peers)
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
			if move.action.kind == supervisorLaunchNative && move.attemptKind == campaignAttemptPrimary &&
				move.variant.launch == simulationLaunchAfterBoundary {
				return index
			}
		}
		for index, move := range moves {
			if move.action.kind != supervisorWaitRoot {
				return index
			}
		}

		return 0
	})
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-shrink-boundary", lineage: 45, command: []string{"test"},
			profile: AutomaticProfile, peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}, choices)
	assert.Nil(t, explored.failure, "boundary shrink exploration failure=%v", explored.failure)
	prefixLength := 0
	for index, record := range explored.trace.records {
		if record.authority != simulationSupervisorAuthority ||
			record.supervisorEvent.kind != supervisorLaunchCompleted {
			continue
		}
		prefixLength = index + 1
		break
	}
	assert.NotEqual(t, 0, prefixLength, "boundary shrink trace has no launch completion")
	malformed := simulationMalformedFact{
		authority: simulationCampaignAuthority,
		campaign: simulationTraceCampaignEvent(campaignEvent{
			id: 1, payload: snapshotEstablishedEvent{},
		}),
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
	assert.EqualValues(t, "campaign-1", shrunk.definition.campaign.identity, "canonical shrink definition=%#v", shrunk.definition)
	assert.EqualValues(t, 1, shrunk.definition.campaign.lineage, "canonical shrink definition=%#v", shrunk.definition)
	assert.EqualValues(t, 0, len(shrunk.definition.catalogue), "canonical shrink definition=%#v", shrunk.definition)
	replayed := ReplayViolation(shrunk, *shrunk.malformed)
	assert.Nil(t, replayed.failure, "boundary shrink replay=%#v, want key %#v", replayed, key)
	assert.Equal(t, key, replayed.key, "boundary shrink replay=%#v, want key %#v", replayed, key)
}

func TestSimulationLivenessFailureKeyIgnoresRawOwnerIdentities(t *testing.T) {
	first := simulationLivenessResult(simulationEngine{pending: []simulationEngineMove{
		{source: simulationCausalSource{kind: simulationCampaignEffectSource, identity: 71}},
		{source: simulationCausalSource{kind: simulationSupervisorActionSource, identity: 93}},
	}}, simulationLivenessNoMove)
	second := simulationLivenessResult(simulationEngine{pending: []simulationEngineMove{
		{source: simulationCausalSource{kind: simulationCampaignEffectSource, identity: 4}},
		{source: simulationCausalSource{kind: simulationSupervisorActionSource, identity: 5}},
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
			campaign: campaignDefinition{
				identity: "campaign-liveness-shrink", lineage: 49, command: []string{"test"},
				profile: AutomaticProfile, peers: 3,
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
	assert.EqualValues(t, 1, shrunk.definition.campaign.peers, "liveness shrunk definition=%#v", shrunk.definition)
	assert.EqualValues(t, 0, len(shrunk.definition.catalogue), "liveness shrunk definition=%#v", shrunk.definition)
	assert.EqualValues(t, "campaign-1", shrunk.definition.campaign.identity, "liveness shrunk definition=%#v", shrunk.definition)
	assert.EqualValues(t, 1, shrunk.definition.campaign.lineage, "liveness shrunk definition=%#v", shrunk.definition)
	assert.EqualValues(t, 0, len(shrunk.choices), "liveness shrunk choices=%#v", shrunk.choices)
}

func TestSimulationRecorderLinearizesProductionOwnerCutsAndQuiescentProjection(t *testing.T) {
	recorder := newSimulationRecorder()
	shell := newProcessRuntimeShellWithObserver(1, newSimulationRuntimeObserver(recorder, 1))
	definition := campaignDefinition{
		identity: "campaign-conformance", lineage: 51, command: []string{"test"},
		profile: AutomaticProfile, peers: 1,
	}
	campaign, _ := beginCampaign(definition)
	runner := &managedCampaignRunner{state: campaign, recorder: recorder}
	driver := &supervisorDriver{recorder: recorder}

	registration := campaignRegistrationEvidence(shell.RegisterCampaign(definition.lineage))
	runner.advance(campaignRegisteredEvent{registration: registration})
	event := supervisorEvent{
		kind: supervisorProspectiveRegistered, generation: 1, attempt: "attempt-a",
		at: time.Unix(100, 0), launchBy: time.Unix(101, 0),
		profile: AutomaticProfile, commandDeadline: time.Second,
	}
	actions := driver.reduce(event)
	for _, action := range actions {
		recorder.recordSupervisorAction(action)
	}

	trace, projection := recorder.quiescent(runner, shell, driver)
	{
		got, want := simulationAuthorities(trace), []simulationAuthority{
			simulationRuntimeAuthority, simulationCampaignAuthority, simulationSupervisorAuthority,
		}
		assert.Equal(t, want, got, "production authority order=%v, want %v", got, want)
	}
	for index, record := range trace.records {
		assert.Equal(t, uint64(index+1), record.sequence, "production sequence at %d=%d", index, record.sequence)
	}

	wantRuntime := processruntime.NewReplay(1)
	wantRuntime, registrationResult := wantRuntime.Apply(processruntime.RegisterCampaignCut(definition.lineage))
	wantRegistration := campaignRegistrationEvidence(registrationResult.Registration())
	wantCampaign, wantEffects := advanceCampaign(campaign, campaignEvent{
		id: 1, payload: campaignRegisteredEvent{registration: wantRegistration},
	})
	wantSupervisor, wantActions := reduceSupervisor(supervisorState{}, event)
	wantProjection := simulationWorld{
		campaign: wantCampaign, runtime: simulationTraceRuntimeState(wantRuntime),
		supervisor: simulationProjectSupervisorState(wantSupervisor),
	}
	assert.Equal(t, wantProjection, projection, "production projection diverged:\n got=%#v\nwant=%#v", projection, wantProjection)
	assert.Equal(t, wantEffects, trace.records[1].campaignEffects, "recorded ordered outputs diverged: %#v", trace.records)
	assert.Equal(t, simulationTraceSupervisorActions(wantActions), trace.records[2].supervisorActions, "recorded ordered outputs diverged: %#v", trace.records)
}

func TestSimulationRecorderRejectsRuntimeDivergenceAtQuiescence(t *testing.T) {
	recorder := newSimulationRecorder()
	observer := newSimulationRuntimeObserver(recorder, 1)
	runtime := newProcessRuntimeShellWithObserver(2, observer)
	first := runtime.RegisterCampaign(71).Campaign()
	second := runtime.RegisterCampaign(72).Campaign()
	runtime.RequestAdmission(processruntime.Admission{
		Campaign: first, Attempt: "first", Class: processruntime.SharedAdmission,
	})
	runtime.RequestAdmission(processruntime.Admission{
		Campaign: second, Attempt: "second", Class: processruntime.SharedAdmission,
	})
	campaign, _ := beginCampaign(campaignDefinition{
		identity: "campaign-conformance", lineage: 71, command: []string{"test"},
		profile: AutomaticProfile, peers: 2,
	})
	runner := &managedCampaignRunner{state: campaign, recorder: recorder}
	driver := &supervisorDriver{recorder: recorder}

	assert.PanicsWithError(t, "process runtime event diverged", func() {
		recorder.quiescent(runner, runtime, driver)
	})
}

func TestSimulationReplayChecksIndependentOwnerCutsAtQuiescence(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-commutation", lineage: 510, command: []string{"test"},
			profile: AutomaticProfile, peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}, nil)
	assert.Nil(t, explored.failure, "commutation exploration failure=%v", explored.failure)
	trace := simulationCloneTrace(explored.trace)
	trace.barriers = []simulationQuiescentBarrier{{
		afterSequence: trace.records[len(trace.records)-1].sequence,
		campaign:      simulationTraceCampaignState(explored.world.campaign),
		runtime:       explored.world.runtime,
		supervisor:    simulationTraceSupervisorState(explored.world.supervisor),
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
	shell := newProcessRuntimeShellWithObserver(1, newSimulationRuntimeObserver(recorder, 1))
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
	event := admissionGrantedEvent{attempt: "waiting", grant: campaignAdmissionFact(grant.Admission())}
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
	publish := supervisorAction{kind: supervisorPublishOwned, token: 11, generation: generation}
	recorder.recordSupervisorActions([]supervisorAction{publish})
	launchReservation := recorder.reserve(simulationRuntimeAuthority)
	runtime, applied = runtime.Apply(processruntime.ObserveAttemptCut(generation, processruntime.Owned()))
	recorder.recordRuntime(launchReservation, simulationRecord{runtimeCut: applied.RecordedCut()}, runtime)
	recorder.recordSupervisorAction(publish)

	settle := supervisorAction{kind: supervisorSettleRuntime, token: 12, generation: generation}
	recorder.recordSupervisorActions([]supervisorAction{settle})
	terminalReservation := recorder.reserve(simulationRuntimeAuthority)
	runtime, applied = runtime.Apply(processruntime.ObserveAttemptCut(generation, processruntime.Settled(processruntime.AutomaticProfile, 0)))
	recorder.recordRuntime(terminalReservation, simulationRecord{runtimeCut: applied.RecordedCut()}, runtime)
	receipt := supervisorRuntimeCompletion{
		generation: generation,
		action: supervisorPendingAction{
			kind: supervisorSettleRuntime, token: settle.token,
		},
		kind: supervisorRuntimeAcknowledged,
	}
	terminalSource := recorder.supervisorSource(supervisorEvent{
		kind: supervisorRuntimeCompleted, generation: generation, runtime: &receipt,
	})
	assert.Equal(t, simulationOwnerDeliverySource, terminalSource.kind, "runtime receipt source=%#v", terminalSource)
	assert.Equal(t, terminalReservation.sequence, terminalSource.identity, "runtime receipt source=%#v", terminalSource)

	launchSource := recorder.campaignSource(attemptLaunchEvent{generation: generation})
	assert.Equal(t, simulationOwnerDeliverySource, launchSource.kind, "launch source=%#v", launchSource)
	assert.Equal(t, launchReservation.sequence, launchSource.identity, "launch source=%#v", launchSource)
}

func TestSimulationRecorderQuiescenceWaitsForInFlightActionCut(t *testing.T) {
	recorder := newSimulationRecorder()
	shell := newProcessRuntimeShellWithObserver(1, newSimulationRuntimeObserver(recorder, 1))
	campaign, _ := beginCampaign(campaignDefinition{
		identity: "campaign-action-barrier", lineage: 511, command: []string{"test"},
		profile: AutomaticProfile, peers: 1,
	})
	runner := &managedCampaignRunner{state: campaign, recorder: recorder}
	driver := &supervisorDriver{recorder: recorder}
	action := supervisorAction{kind: supervisorLaunchNative, token: 71, generation: 9}
	recorder.recordSupervisorActions([]supervisorAction{action})

	started := make(chan struct{})
	completed := make(chan struct{})
	go func() {
		close(started)
		recorder.quiescent(runner, shell, driver)
		close(completed)
	}()
	<-started
	select {
	case <-completed:
		require.FailNow(t, "quiescence returned with a supervisor action still in flight")
	case <-time.After(20 * time.Millisecond):
	}

	recorder.recordSupervisorCompletion(supervisorPendingAction{kind: action.kind, token: action.token})
	select {
	case <-completed:
	case <-time.After(time.Second):
		require.FailNow(t, "quiescence did not resume after the matching action cut")
	}
}

func TestSimulationRecorderReplaysAnEmptyProductionCampaign(t *testing.T) {
	recorder := newSimulationRecorder()
	shell := newProcessRuntimeShellWithObserver(1, newSimulationRuntimeObserver(recorder, 1))
	definition := campaignDefinition{
		identity: "campaign-recorded-replay", lineage: 52, command: []string{"test"},
		profile: AutomaticProfile, peers: 1,
	}
	campaign, _ := beginCampaign(definition)
	runner := &managedCampaignRunner{state: campaign, recorder: recorder}
	driver := &supervisorDriver{recorder: recorder}
	cut := func() { _, _ = recorder.quiescent(runner, shell, driver) }

	registration := campaignRegistrationEvidence(shell.RegisterCampaign(definition.lineage))
	cut()
	runner.advance(campaignRegisteredEvent{registration: registration})
	cut()
	runner.advance(snapshotEstablishedEvent{snapshot: "private-snapshot"})
	cut()
	runner.advance(catalogueDiscoveredEvent{snapshot: "private-snapshot"})
	cut()
	runner.advance(resourceSettledEvent{
		kind: campaignResourceSnapshot, identity: "private-snapshot",
	})
	cut()
	processedTerminal := shell.CommitTerminal(registration.token)
	terminal := terminalResult{decision: processedTerminal.Decision()}
	cut()
	runner.advance(terminalCommittedEvent{result: campaignTerminalEvidence(terminal)})

	trace, production := recorder.quiescent(runner, shell, driver)
	assert.EqualValues(t, 7, len(trace.barriers), "retained prefix barriers=%d, want 7", len(trace.barriers))
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

func TestSimulationRecorderReplaysNonEmptyManagedCampaignAtQuiescence(t *testing.T) {
	recorder := newSimulationRecorder()
	shell := newProcessRuntimeShellWithObserver(1, newSimulationRuntimeObserver(recorder, 1))
	var clockMutex sync.Mutex
	now := time.Unix(10_000, 0)
	exitCodes := make(map[attemptGeneration]int)
	tick := func() time.Time {
		clockMutex.Lock()
		defer clockMutex.Unlock()
		now = now.Add(time.Nanosecond)

		return now
	}
	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell, recorder: recorder, now: tick, launchProgress: time.Second, drainEpoch: 5 * time.Second,
		launchBoundary: func(time.Time) <-chan time.Time { return make(chan time.Time) },
		prepare: func(generation attemptGeneration, spec Spec) {
			clockMutex.Lock()
			defer clockMutex.Unlock()
			if spec.Deadline != baselineBootstrapDeadline {
				exitCodes[generation] = 1
			}
		},
		execute: func(action supervisorAction) *supervisorEvent {
			at := tick()
			switch action.kind {
			case supervisorLaunchNative:
				return &supervisorEvent{
					kind: supervisorLaunchCompleted, generation: action.generation, at: at,
					completion: &supervisorLaunchCompletion{
						generation: action.generation, action: action.token, at: at,
						kind: supervisorLaunchReleased,
					},
				}
			case supervisorWaitRoot:
				clockMutex.Lock()
				exitCode := exitCodes[action.generation]
				clockMutex.Unlock()
				return &supervisorEvent{
					kind: supervisorRunningObserved, generation: action.generation, at: at,
					drainBy: at.Add(5 * time.Second),
					running: &supervisorRunningBundle{
						generation: action.generation, waitAction: action.token,
						facts: []supervisorRunningFact{{
							generation: action.generation, action: action.token,
							kind: supervisorRunningRootExited, at: at, exitCode: exitCode,
						}},
					},
				}
			case supervisorObserveEmptiness:
				return &supervisorEvent{
					kind: supervisorDrainCompleted, generation: action.generation, at: at,
					drain: &supervisorDrainCompletion{
						generation: action.generation,
						action:     supervisorPendingAction{kind: action.kind, token: action.token},
						at:         at, kind: supervisorDrainObservedEmpty,
					},
				}
			case supervisorCaptureOutput:
				return &supervisorEvent{
					kind: supervisorOutputCompleted, generation: action.generation, at: at,
					output: &supervisorOutputCompletion{
						generation: action.generation,
						action:     supervisorPendingAction{kind: action.kind, token: action.token}, at: at, ref: 1,
					},
				}
			case supervisorReleaseDomain:
				return &supervisorEvent{
					kind: supervisorReleaseCompleted, generation: action.generation, at: at,
					release: &supervisorReleaseCompletion{
						generation: action.generation,
						action:     supervisorPendingAction{kind: action.kind, token: action.token}, at: at,
					},
				}
			default:
				assert.Fail(t, "unexpected scripted native action", "action: %#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "" },
	})
	attempts := &nativeManagedAttemptSystem{driver: driver}
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: shell, recorder: recorder, repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})
	result := runner.run(managedCampaignRequest{
		identity: "campaign-recorded-managed", lineage: 521, command: []string{"test"},
		profile: SerialProfile, peers: 1, viruses: []viruses.Virus{integerincrement.New()},
	})
	{
		completed, ok := result.outcome.(completedOutcome)
		require.True(t, ok, "managed outcome=%#v, want one completed mutant", result.outcome)
		assert.EqualValues(t, 1, len(completed.mutants), "managed outcome=%#v, want one completed mutant", result.outcome)
	}

	trace, production := recorder.quiescent(runner, shell, driver)
	require.Len(t, trace.barriers, 1, "quiescent barriers=%#v, want one final accepted-prefix cut", trace.barriers)
	assert.Equal(t, uint64(len(trace.records)), trace.barriers[0].afterSequence, "quiescent barriers=%#v, want one final accepted-prefix cut", trace.barriers)
	{
		path := simulationForbiddenValuePath(reflect.ValueOf(trace), "trace")
		assert.EqualValues(t, "", path, "managed trace retained a production capability at %s", path)
	}
	for index, record := range trace.records {
		assert.NotEqual(t, 0, record.source.kind, "managed production record %d has no causal source: %#v", index, record)
		assert.NotEqual(t, 0, record.source.identity, "managed production record %d has no causal source: %#v", index, record)
		{
			want := simulationExpectedProductionSourceKind(record)
			assert.Equal(t, want, record.source.kind, "managed production record %d source kind=%v, want %v", index, record.source.kind, want)
		}
	}
	replayed := ReplayLegal(trace)
	assert.Nil(t, replayed.failure, "non-empty production trace did not replay: %v", replayed.failure)
	replayed.world.runtimeState = processruntime.Replay{}
	assert.Equal(t, production, replayed.world, "non-empty production replay diverged:\n got=%#v\nwant=%#v", replayed.world, production)
}

func simulationExpectedProductionSourceKind(record simulationRecord) simulationCausalSourceKind {
	switch record.authority {
	case simulationRuntimeAuthority:
		switch record.runtimeCut.Operation() {
		case processruntime.ObserveAttemptOperation, processruntime.CompleteConfirmationQueueOperation, processruntime.SettleEmergencyOperation:
			return simulationSupervisorActionSource
		default:
			return simulationCampaignEffectSource
		}
	case simulationCampaignAuthority:
		switch record.campaignEvent.kind {
		case simulationCampaignRegistered, simulationAdmissionGranted, simulationAdmissionCancelled,
			simulationAdmissionRejected, simulationStartCommittedEvent, simulationAttemptLaunched,
			simulationConfirmationBarrierBound, simulationGrantReturnAcknowledged,
			simulationRuntimeEmergencyStarted, simulationTerminalCommitted:
			return simulationOwnerDeliverySource
		case simulationAttemptTerminal, simulationRuntimeEmergencySettled:
			return simulationSupervisorActionSource
		default:
			return simulationCampaignEffectSource
		}
	case simulationSupervisorAuthority:
		switch record.supervisorEvent.kind {
		case supervisorProspectiveRegistered:
			return simulationCampaignEffectSource
		case supervisorRuntimeCompleted, supervisorEmergencyStarted, supervisorEmergencySettlementCompleted:
			return simulationOwnerDeliverySource
		default:
			return simulationSupervisorActionSource
		}
	default:
		panic("simulation production record authority is invalid")
	}
}

func TestSimulationRecorderSealsCatalogueFactsAgainstCallerMutation(t *testing.T) {
	recorder := newSimulationRecorder()
	definition := campaignDefinition{
		identity: "campaign-immutable-record", lineage: 53, command: []string{"test"},
		profile: AutomaticProfile, peers: 1,
	}
	campaign, _ := beginCampaign(definition)
	runner := &managedCampaignRunner{state: campaign, recorder: recorder}
	shell := newProcessRuntimeShellWithObserver(1, newSimulationRuntimeObserver(recorder, 1))
	driver := &supervisorDriver{recorder: recorder}
	registration := campaignRegistrationEvidence(shell.RegisterCampaign(definition.lineage))
	runner.advance(campaignRegisteredEvent{registration: registration})
	runner.advance(snapshotEstablishedEvent{snapshot: "private-snapshot"})
	mutants := []mutantIdentity{"mutant-a", "mutant-b"}
	runner.advance(catalogueDiscoveredEvent{snapshot: "private-snapshot", mutants: mutants})

	trace, _ := recorder.quiescent(runner, shell, driver)
	mutants[0] = "caller-rewrite"
	got := trace.records[len(trace.records)-1].campaignEvent.production().payload.(catalogueDiscoveredEvent).mutants
	assert.Equal(t, []mutantIdentity{"mutant-a", "mutant-b"}, got, "recorded catalogue changed with caller input: %v", got)
}

func TestSimulationTraceStoresRuntimeOwnedRecordedCuts(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-runtime-cuts", lineage: 91, command: []string{"test"},
			profile: SerialProfile, peers: 1,
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
	shell := newProcessRuntimeShellWithObserver(1, newSimulationRuntimeObserver(recorder, 1))
	registration := shell.RegisterCampaign(71)
	await := shell.RequestAdmission(processruntime.Admission{
		Campaign: registration.Campaign(), Attempt: "attempt-a", Class: processruntime.SharedAdmission,
		Profile: AutomaticProfile,
	})
	assert.Equal(t, processruntime.AdmissionAccepted, await.Decision(), "admission decision=%v", await.Decision())
	campaign, _ := beginCampaign(campaignDefinition{
		identity: "campaign-projection", lineage: 71, command: []string{"test"},
		profile: AutomaticProfile, peers: 1,
	})
	runner := &managedCampaignRunner{state: campaign, recorder: recorder}
	driver := &supervisorDriver{recorder: recorder}

	trace, _ := recorder.quiescent(runner, shell, driver)
	{
		path := simulationForbiddenValuePath(reflect.ValueOf(trace), "trace")
		assert.EqualValues(t, "", path, "runtime trace leaked a delivery capability at %s", path)
	}
}

func TestSimulationCausalTerminalConsumesRecordedResolvedDeadline(t *testing.T) {
	recorded := attemptTerminalEvent{
		attempt: "baseline", generation: 7,
		terminal:                 Settled{ExecutionData: ExecutionData{Deadline: time.Minute}},
		resolvedMutationDeadline: mutationDeadlineResolution{duration: 37 * time.Second},
	}
	derived := recorded
	derived.resolvedMutationDeadline.duration = 99 * time.Second

	merged, ok := simulationCausalCampaignPayload(recorded, derived).(attemptTerminalEvent)
	require.True(t, ok, "merged terminal=%#v, want recorded deadline %#v", merged, recorded.resolvedMutationDeadline)
	assert.Equal(t, recorded.resolvedMutationDeadline, merged.resolvedMutationDeadline, "merged terminal=%#v, want recorded deadline %#v", merged, recorded.resolvedMutationDeadline)
}

func TestSimulationEnabledMovesAreCanonicalReducerOwnedWork(t *testing.T) {
	effects := []campaignEffect{
		{id: 9, kind: campaignEffectLaunchAttempt, attempt: "attempt-b", generation: 3},
		{id: 4, kind: campaignEffectMaterializeWorkspace, attempt: "attempt-b", mutant: "mutant-b"},
		{id: 3, kind: campaignEffectMaterializeWorkspace, attempt: "attempt-a", mutant: "mutant-a"},
	}
	actions := []supervisorAction{
		{kind: supervisorWaitRoot, generation: 3, token: 8},
		{kind: supervisorSampleRunning, generation: 3, token: 7},
	}

	got := simulationEnabledMoves(effects, actions, []mutantIdentity{"mutant-a", "mutant-b"})
	want := []simulationEnabledMove{
		{authority: simulationCampaignAuthority, effect: effects[2]},
		{authority: simulationCampaignAuthority, effect: effects[1]},
		{authority: simulationSupervisorAuthority, effect: effects[0]},
		{authority: simulationSupervisorAuthority, action: actions[1]},
		{authority: simulationSupervisorAuthority, action: actions[0]},
	}
	assert.Equal(t, want, got, "enabled moves=%#v, want %#v", got, want)
}

func TestSimulationChoiceRecordsMarkCanonicalRecovery(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-recovery", lineage: 91, command: []string{"test"},
			profile: AutomaticProfile, peers: 2,
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
		campaign: campaignDefinition{
			identity: "campaign-causal-empty", lineage: 92, command: []string{"test"},
			profile: AutomaticProfile, peers: 1,
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
		campaign: campaignDefinition{
			identity: "campaign-causal-nonempty", lineage: 93, command: []string{"test"},
			profile: AutomaticProfile, peers: 2,
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
	shell := newProcessRuntimeShellWithObserver(1, newSimulationRuntimeObserver(recorder, 1))
	definition := campaignDefinition{
		identity: "campaign-paths", lineage: 81, command: []string{"test"},
		profile: AutomaticProfile, peers: 1,
	}
	campaign, _ := beginCampaign(definition)
	runner := &managedCampaignRunner{state: campaign, recorder: recorder}
	driver := &supervisorDriver{recorder: recorder}
	registration := campaignRegistrationEvidence(shell.RegisterCampaign(definition.lineage))
	runner.advance(campaignRegisteredEvent{registration: registration})
	runner.advance(snapshotEstablishedEvent{snapshot: "/private/repository/snapshot-937"})

	trace, projection := recorder.quiescent(runner, shell, driver)
	projected := fmt.Sprintf("%#v %#v", trace, projection)
	assert.NotContains(t, projected, "/private/repository", "simulation projection leaked a filesystem path: %s", projected)
	assert.EqualValues(t, "snapshot:campaign-paths", projection.campaign.snapshot, "logical snapshot identity=%q", projection.campaign.snapshot)
}

func TestSimulationRecorderCanonicalizesSupervisorInstants(t *testing.T) {
	recorder := newSimulationRecorder()
	driver := &supervisorDriver{recorder: recorder}
	sourceLocation := time.FixedZone("private-host-zone", 9*60*60)
	registeredAt := time.Date(2026, 8, 26, 1, 2, 3, 4, sourceLocation)
	driver.reduce(supervisorEvent{
		kind: supervisorProspectiveRegistered, generation: 1, attempt: "attempt-a",
		at: registeredAt, launchBy: registeredAt.Add(time.Second),
		profile: AutomaticProfile, commandDeadline: time.Second,
	})

	recorder.mutex.Lock()
	record := recorder.records[0]
	recorder.mutex.Unlock()
	{
		got := record.supervisorEvent.at.production()
		assert.True(t, got.Equal(registeredAt), "canonical instant changed: got=%s want=%s", got, registeredAt)
		assert.Equal(t, time.UTC, got.Location(), "canonical instant changed: got=%s want=%s", got, registeredAt)
	}
	{
		got := record.supervisorState.attempts[0].registeredAt.production()
		assert.True(t, got.Equal(registeredAt), "canonical state instant changed: got=%s want=%s", got, registeredAt)
		assert.Equal(t, time.UTC, got.Location(), "canonical state instant changed: got=%s want=%s", got, registeredAt)
	}
}

func TestSimulationTraceCarriesNoProductionCapabilities(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-integer-time", lineage: 82, command: []string{"test"},
			profile: AutomaticProfile, peers: 1,
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
	assert.EqualValues(t, 3, definition.campaign.peers, "fuzz definition/choices=%#v/%v", definition, choices)
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
	firstEvent := supervisorEvent{kind: supervisorRuntimeCompleted, generation: 1}
	secondEvent := supervisorEvent{kind: supervisorEmergencyStarted, at: time.Unix(1, 0)}
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
	want := supervisorAction{kind: supervisorDeliverTerminal, token: 10}
	actions := []supervisorAction{
		want,
		{kind: supervisorSettleEmergency, token: 11},
	}
	{
		got := simulationOnlySupervisorAction(actions, supervisorDeliverTerminal)
		assert.Equal(t, want, got, "selected action=%#v, want %#v", got, want)
	}
}

func TestSimulationTerminalWaitsForItsCampaignLaunchDelivery(t *testing.T) {
	launch := attemptLaunchEvent{attempt: "attempt-a", generation: 2}
	engine := simulationEngine{pending: []simulationEngineMove{
		{
			source:   simulationCausalSource{kind: simulationOwnerDeliverySource, identity: 9},
			delivery: launch,
		},
		{
			source: simulationCausalSource{kind: simulationSupervisorActionSource, identity: 10},
			action: supervisorAction{kind: supervisorDeliverTerminal, generation: 2, token: 10},
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
			source: simulationCausalSource{kind: simulationSupervisorActionSource, identity: 9},
			action: supervisorAction{kind: supervisorTransferResidualCustody, generation: 2, token: 9},
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
	require.Len(t, admitted.deliveries, 1)
	runtime, start := runtime.Apply(processruntime.CommitStartCut(
		processRuntimeAdmission(campaignAdmissionValue(admitted.deliveries[0])),
	))
	started := runtimeStartResult(start.Start())
	runtime, _ = runtime.Apply(processruntime.ObserveAttemptCut(started.generation, processruntime.Owned()))
	return runtime, started.generation
}

func TestSimulationOrdersRuntimeCustodyActionsByOwnerToken(t *testing.T) {
	runtime, generation := simulationOwnedRuntime(t)
	engine := simulationEngine{
		runtime: runtime,
		pending: []simulationEngineMove{
			{
				source: simulationCausalSource{kind: simulationSupervisorActionSource, identity: 10},
				action: supervisorAction{kind: supervisorSettleRuntime, generation: generation, token: 10},
			},
			{
				source: simulationCausalSource{kind: simulationSupervisorActionSource, identity: 11},
				action: supervisorAction{kind: supervisorSettleEmergency, token: 11},
			},
		},
	}

	moves := engine.enabledMoves()
	require.Len(t, moves, 1, "enabled runtime custody actions=%#v, want token 10 only", moves)
	assert.EqualValues(t, 10, moves[0].action.token, "enabled runtime custody actions=%#v, want token 10 only", moves)
}

func TestSimulationEmergencySettlementRetiresPendingCampaignTerminals(t *testing.T) {
	engine := simulationEngine{pending: []simulationEngineMove{
		{
			source: simulationCausalSource{kind: simulationSupervisorActionSource, identity: 10},
			action: supervisorAction{kind: supervisorDeliverTerminal, generation: 2, token: 10},
		},
		{
			source: simulationCausalSource{kind: simulationCampaignEffectSource, identity: 12},
			effect: campaignEffect{id: 12, kind: campaignEffectReleaseSnapshot},
		},
	}}

	engine.retireCampaignTerminals()
	require.Len(t, engine.pending, 1, "pending work after emergency terminal retirement=%#v", engine.pending)
	assert.EqualValues(t, 12, engine.pending[0].effect.id, "pending work after emergency terminal retirement=%#v", engine.pending)
}

func TestSimulationOrdersRuntimeCompletionBeforeLaterCustodyAction(t *testing.T) {
	completion := supervisorEvent{kind: supervisorRuntimeCompleted, generation: 2}
	engine := simulationEngine{
		trace: simulationTrace{records: []simulationRecord{{
			sequence: 19,
			source:   simulationCausalSource{kind: simulationSupervisorActionSource, identity: 9},
		}}},
		pending: []simulationEngineMove{
			{
				source:             simulationCausalSource{kind: simulationOwnerDeliverySource, identity: 19},
				supervisorDelivery: &completion,
			},
			{
				source: simulationCausalSource{kind: simulationSupervisorActionSource, identity: 11},
				action: supervisorAction{kind: supervisorSettleEmergency, token: 11},
			},
		},
	}

	moves := engine.enabledMoves()
	require.Len(t, moves, 1, "enabled runtime custody moves=%#v, want completion cut 19 only", moves)
	assert.EqualValues(t, 19, moves[0].source.identity, "enabled runtime custody moves=%#v, want completion cut 19 only", moves)
}

func TestSimulationEmergencySettlementWaitsForCampaignRequest(t *testing.T) {
	engine := simulationEngine{pending: []simulationEngineMove{{
		source: simulationCausalSource{kind: simulationSupervisorActionSource, identity: 12},
		action: supervisorAction{kind: supervisorDeliverEmergencySettlement, token: 12},
	}}}

	{
		moves := engine.enabledMoves()
		assert.EqualValues(t, 0, len(moves), "enabled unrequested emergency settlement=%#v", moves)
	}
}

func TestSimulationEmergencySettlementWaitsForPublishedCampaignIngress(t *testing.T) {
	launch := attemptLaunchEvent{attempt: "attempt-a", generation: 2}
	engine := simulationEngine{
		campaign: campaignState{drain: campaignDrainIntent{kind: campaignDrainRuntimeEmergency, epoch: 1}},
		pending: []simulationEngineMove{
			{
				source:   simulationCausalSource{kind: simulationOwnerDeliverySource, identity: 19},
				delivery: launch,
			},
			{
				source: simulationCausalSource{kind: simulationSupervisorActionSource, identity: 20},
				action: supervisorAction{kind: supervisorDeliverEmergencySettlement, token: 20},
			},
		},
	}

	moves := engine.enabledMoves()
	require.Len(t, moves, 1, "enabled emergency settlement moves=%#v, want published campaign ingress", moves)
	assert.Equal(t, launch, moves[0].delivery, "enabled emergency settlement moves=%#v, want published campaign ingress", moves)
}

func TestSimulationEmergencyCutWaitsForCommittedStartDelivery(t *testing.T) {
	emergency := supervisorEvent{kind: supervisorEmergencyStarted, at: time.Unix(1, 0)}
	start := startCommittedEvent{
		attempt: "attempt-b",
		result:  campaignStartResult{decision: processruntime.StartAccepted, generation: 4},
	}
	engine := simulationEngine{
		campaign: campaignState{drain: campaignDrainIntent{kind: campaignDrainRuntimeEmergency, epoch: 1}},
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
	const generation attemptGeneration = 4
	launch := simulationEngineMove{
		source: simulationCausalSource{kind: simulationCampaignEffectSource, identity: 19},
		effect: campaignEffect{id: 19, kind: campaignEffectLaunchAttempt, generation: generation},
	}
	stop := simulationEngineMove{
		source: simulationCausalSource{kind: simulationCampaignEffectSource, identity: 20},
		effect: campaignEffect{id: 20, kind: campaignEffectStopAttempt, generation: generation},
	}
	launchAction := simulationEngineMove{
		source: simulationCausalSource{kind: simulationSupervisorActionSource, identity: 21},
		action: supervisorAction{kind: supervisorLaunchNative, generation: generation, token: 21},
	}
	tests := []struct {
		name   string
		engine simulationEngine
	}{
		{"campaign_launch_pending", simulationEngine{pending: []simulationEngineMove{launch, stop}}},
		{
			"supervisor_launch_action_pending",
			simulationEngine{
				supervisor: supervisorState{attempts: []supervisorAttemptState{{
					generation: generation, phase: supervisorLaunchEstablishing,
				}}},
				pending: []simulationEngineMove{launchAction, stop},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			moves := test.engine.enabledMoves()
			assert.NotEqual(t, 0, len(moves), "attempt ownership made no progress")
			for _, move := range moves {
				assert.NotEqual(t, campaignEffectStopAttempt, move.effect.kind, "enabled stop before supervisor ownership: %#v", moves)
			}
		})
	}
	t.Run("ownership_acquired", func(t *testing.T) {
		completed := simulationEngine{
			launches: map[attemptGeneration]campaignEffect{generation: launch.effect},
			pending:  []simulationEngineMove{stop},
		}
		moves := completed.enabledMoves()
		require.EqualValues(t, 1, len(moves), "completed generation retained disabled stop: %#v", moves)
		assert.Equal(t, campaignEffectStopAttempt, moves[0].effect.kind, "completed generation retained disabled stop: %#v", moves)
		assert.True(t, completed.consume(moves[0]), "completed generation stop was not pending")
		err := completed.apply(moves[0])
		assert.NoError(t, err, "completed generation stop=%v, want no-op", err)
	})
}

func TestSimulationOrdersSupervisorActionsWithinOneGeneration(t *testing.T) {
	engine := simulationEngine{pending: []simulationEngineMove{
		{
			source: simulationCausalSource{kind: simulationSupervisorActionSource, identity: 26},
			action: supervisorAction{kind: supervisorSealStopAdmission, generation: 4, token: 26},
		},
		{
			source: simulationCausalSource{kind: simulationSupervisorActionSource, identity: 27},
			action: supervisorAction{kind: supervisorReleaseDomain, generation: 4, token: 27},
		},
	}}

	moves := engine.enabledMoves()
	require.Len(t, moves, 1, "enabled same-generation actions=%#v, want token 26 only", moves)
	assert.EqualValues(t, 26, moves[0].action.token, "enabled same-generation actions=%#v, want token 26 only", moves)
}

func simulationFuzzInput(source []byte) (simulationDefinition, simulationChoiceBytes) {
	capacity := 1
	definition := simulationDefinition{campaign: campaignDefinition{
		identity: "campaign-fuzz", lineage: 61, command: []string{"test"},
		profile: AutomaticProfile, peers: capacity,
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
		definition.campaign.peers = capacity
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
			campaign: simulationTraceCampaignEvent(campaignEvent{
				id: 1, payload: snapshotEstablishedEvent{},
			}),
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
					authority: simulationSupervisorAuthority,
					supervisor: simulationTraceSupervisorEvent(supervisorEvent{
						kind: supervisorProspectiveRegistered,
					}),
				}
			}
		}
		first := ReplayViolation(prefix, malformed)
		second := ReplayViolation(prefix, malformed)
		require.Nil(t, first.failure, "violation replay diverged: first=%#v second=%#v", first, second)
		assert.Equal(t, second, first, "violation replay diverged: first=%#v second=%#v", first, second)
	})
}

func simulationRecordedActionSummary(trace simulationTrace) []string {
	var summary []string
	for _, record := range trace.records {
		for _, action := range record.supervisorActions {
			summary = append(summary, fmt.Sprintf(
				"record=%d kind=%d generation=%d token=%d",
				record.sequence, action.kind, action.generation, action.token,
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
