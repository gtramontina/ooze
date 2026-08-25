package ooze

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/viruses"
	"github.com/gtramontina/ooze/viruses/integerincrement"
)

type simulationFocusedChoiceSource func([]simulationEngineMove) int

func (simulationFocusedChoiceSource) choose(int) int { return 0 }

func (source simulationFocusedChoiceSource) chooseMove(moves []simulationEngineMove) int {
	return source(moves)
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
	if explored.failure != nil {
		t.Fatalf("exploration failure=%v", explored.failure)
	}
	if got, want := explored.world.campaign.outcome, (campaignOutcome)(noMutantsOutcome{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("explored outcome=%#v, want %#v", got, want)
	}
	if got, want := simulationAuthorities(explored.trace), []simulationAuthority{
		simulationRuntimeAuthority,
		simulationCampaignAuthority,
		simulationCampaignAuthority,
		simulationCampaignAuthority,
		simulationCampaignAuthority,
		simulationRuntimeAuthority,
		simulationCampaignAuthority,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trace authorities=%v, want %v", got, want)
	}
	for index, record := range explored.trace.records {
		if got, want := record.sequence, uint64(index+1); got != want {
			t.Fatalf("record %d sequence=%d, want %d", index, got, want)
		}
	}

	replayed := ReplayLegal(explored.trace)
	if replayed.failure != nil {
		t.Fatalf("replay failure=%v", replayed.failure)
	}
	if !reflect.DeepEqual(replayed.world, explored.world) {
		t.Fatalf("replayed world diverged:\n got: %#v\nwant: %#v", replayed.world, explored.world)
	}
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
	if explored.failure != nil {
		t.Fatalf("exploration failure=%v", explored.failure)
	}
	if _, ok := explored.world.campaign.outcome.(abortedOutcome); !ok {
		t.Fatalf("explored outcome=%#v, want aborted baseline", explored.world.campaign.outcome)
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
	if !reflect.DeepEqual(supervisorKinds, wantSupervisorKinds) {
		t.Fatalf("supervisor lifecycle=%v, want %v", supervisorKinds, wantSupervisorKinds)
	}
	if len(explored.world.supervisor.attempts) != 0 || len(explored.world.runtime.campaigns) != 0 {
		t.Fatalf("terminal world is not quiescent: %#v", explored.world)
	}

	replayed := ReplayLegal(explored.trace)
	if replayed.failure != nil {
		t.Fatalf("replay failure=%v", replayed.failure)
	}
	if !reflect.DeepEqual(replayed.world, explored.world) {
		t.Fatalf("replayed world diverged:\n got: %#v\nwant: %#v", replayed.world, explored.world)
	}
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
			if launch.kind != test.kind || launch.completion == nil {
				t.Fatalf("selected launch fact=%#v, want kind %v", launch, test.kind)
			}
			if got := launch.completion.at.Equal(launchBy); got != test.equalAt {
				t.Fatalf("completion/boundary equality=%v, want %v", got, test.equalAt)
			}
			replayed := ReplayLegal(explored.trace)
			if replayed.failure != nil || !reflect.DeepEqual(replayed.world, explored.world) {
				t.Fatalf("boundary replay diverged: %#v", replayed)
			}
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

				return failed && world.runtime.lifecycle == runtimeClosedUnconfirmed
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
			if explored.failure != nil || !selected || !test.outcome(explored.world) ||
				!test.observe(explored.trace) {
				t.Fatalf("after-boundary selected=%v outcome=%T campaign-failure=%T simulation-failure=%v",
					selected, explored.world.campaign.outcome, explored.world.campaign.failure, explored.failure)
			}
			if replayed := ReplayLegal(explored.trace); replayed.failure != nil ||
				!reflect.DeepEqual(replayed.world, explored.world) {
				t.Fatalf("after-boundary replay=%#v", replayed)
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
			if explored.failure != nil || !ok || len(completed.mutants) != 1 ||
				completed.mutants[0].kind != test.want {
				t.Fatalf("completed outcome=%#v failure=%v, want %v; choices=%#v",
					explored.world.campaign.outcome, explored.failure, test.want, explored.trace.choices)
			}
			if explored.world.campaign.commandCount() != 2 || len(explored.world.campaign.obligations) != 0 {
				t.Fatalf("terminal commands/obligations=%d/%#v",
					explored.world.campaign.commandCount(), explored.world.campaign.obligations)
			}
			replayed := ReplayLegal(explored.trace)
			if replayed.failure != nil || !reflect.DeepEqual(replayed.world, explored.world) {
				t.Fatalf("outcome replay diverged: %#v", replayed)
			}
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
	if explored.failure != nil || !ok {
		t.Fatalf("catalogue exploration=%#v failure=%v", explored.world.campaign.outcome, explored.failure)
	}
	got := make([]mutantIdentity, len(completed.mutants))
	for index, mutant := range completed.mutants {
		got[index] = mutant.mutant
	}
	if want := []mutantIdentity{"mutant-a", "mutant-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("completed catalogue order=%v, want %v", got, want)
	}
	if explored.world.campaign.commandCount() != 3 {
		t.Fatalf("command count=%d, want baseline plus two primaries", explored.world.campaign.commandCount())
	}
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
	if result.failure != nil || !ok || len(completed.mutants) != 1 {
		t.Fatalf("spare-capacity exploration outcome=%#v failure=%v", result.world.campaign.outcome, result.failure)
	}
}

func TestSimulationExploresPeerPrimaryOverlapFromEmittedEffectWave(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-overlap", lineage: 25, command: []string{"test"},
			profile: AutomaticProfile, peers: 2,
		},
		capacity: 2, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, simulationChoiceBytes{0})
	if explored.failure != nil {
		t.Fatalf("overlap exploration failed: %v", explored.failure)
	}
	found := false
	for _, record := range explored.trace.records {
		if record.authority != simulationRuntimeAuthority || len(record.runtimeState.admissions) != 2 {
			continue
		}
		if record.runtimeState.admissions[0].overlapped && record.runtimeState.admissions[1].overlapped {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("two emitted primary effects never reached overlapping start commitments")
	}
	replayed := ReplayLegal(explored.trace)
	if replayed.failure != nil {
		t.Fatalf("overlap replay failed: %v", replayed.failure)
	}
	if !reflect.DeepEqual(replayed.world, explored.world) {
		t.Fatalf("overlap replay world diverged:\n got=%#v\nwant=%#v", replayed.world, explored.world)
	}
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
	if explored.failure != nil || !delayed {
		t.Fatalf("late-grant exploration failure=%v delayed=%v", explored.failure, delayed)
	}
	if replayed := ReplayLegal(explored.trace); replayed.failure != nil ||
		!reflect.DeepEqual(replayed.world, explored.world) {
		t.Fatalf("late-grant replay=%#v", replayed)
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
	if explored.failure != nil || !ok || len(completed.mutants) != 2 {
		t.Fatalf("repeated-deadline exploration=%#v failure=%v", explored.world.campaign.outcome, explored.failure)
	}
	for _, mutant := range completed.mutants {
		if mutant.kind != mutantTimedOut || mutant.primary.kind != campaignEvidenceDeadline ||
			mutant.confirmation.kind != campaignAttemptEvidenceKind(0) {
			t.Fatalf("repeated-deadline mutant=%#v, want direct deadline without confirmation", mutant)
		}
	}
	if explored.world.runtime.mode != fullAutomatic || completed.singleAdmissionFallback {
		t.Fatalf("repeated-deadline admission mode/fallback=%v/%v",
			explored.world.runtime.mode, completed.singleAdmissionFallback)
	}
	if replayed := ReplayLegal(explored.trace); replayed.failure != nil ||
		!reflect.DeepEqual(replayed.world, explored.world) {
		t.Fatalf("repeated-deadline replay=%#v", replayed)
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
	if explored.failure != nil || !ok || !timedOut || len(completed.mutants) != 2 {
		t.Fatalf("confirmation exploration=%#v failure=%v", explored.world.campaign.outcome, explored.failure)
	}
	if completed.mutants[0].kind != mutantSurvived ||
		completed.mutants[0].primary.kind != campaignEvidenceDeadline ||
		completed.mutants[0].confirmation.kind != campaignEvidenceSettled ||
		!completed.singleAdmissionFallback || explored.world.campaign.commandCount() != 4 {
		t.Fatalf("confirmation outcome=%#v commands=%d", completed, explored.world.campaign.commandCount())
	}
	replayed := ReplayLegal(explored.trace)
	if replayed.failure != nil || !reflect.DeepEqual(replayed.world, explored.world) {
		t.Fatalf("confirmation replay failure=%v world-equal=%v",
			replayed.failure, reflect.DeepEqual(replayed.world, explored.world))
	}
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
	if explored.failure != nil || !ok || len(timedOut) != len(catalogue) || len(completed.mutants) != len(catalogue) {
		t.Fatalf("multiple-provisional exploration=%#v timed=%v failure=%v",
			explored.world.campaign.outcome, timedOut, explored.failure)
	}
	for index, mutant := range completed.mutants {
		if mutant.mutant != catalogue[index] || mutant.primary.kind != campaignEvidenceDeadline ||
			mutant.confirmation.kind != campaignEvidenceSettled {
			t.Fatalf("multiple-provisional mutant[%d]=%#v", index, mutant)
		}
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
				if attemptAt < 0 {
					t.Fatalf("confirmation launch attempt %q is absent", effect.attempt)
				}
				confirmations = append(confirmations, record.campaignState.attempts[attemptAt].mutant)
			}
		}
	}
	if !reflect.DeepEqual(barriers, catalogue[:1]) || !reflect.DeepEqual(confirmations, catalogue) {
		t.Fatalf("confirmation barrier/FIFO order=%v/%v, want %v/%v",
			barriers, confirmations, catalogue[:1], catalogue)
	}
	if replayed := ReplayLegal(explored.trace); replayed.failure != nil ||
		!reflect.DeepEqual(replayed.world, explored.world) {
		t.Fatalf("multiple-provisional replay=%#v", replayed)
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
		t.Fatalf("drain-expiry exploration failure=%v expired=%v phase=%v event=%v obligations=%d campaign-failure=%T",
			explored.failure, expired, last.campaignState.phase, last.campaignEvent.kind,
			len(last.campaignState.obligations), last.campaignState.failure)
	}
	if _, ok := explored.world.campaign.failure.(cleanupUnconfirmedFault); !ok ||
		explored.world.runtime.lifecycle != runtimeClosedUnconfirmed {
		t.Fatalf("drain-expiry world=%#v", explored.world)
	}
	startAt, closedAt, forcedAbortAt := -1, -1, -1
	for index, record := range explored.trace.records {
		if record.authority != simulationRuntimeAuthority {
			continue
		}
		if record.runtimeOperation == simulationStartCommitted &&
			record.runtimeStart.decision == startCommittedAccepted {
			startAt = index
		}
		if closedAt < 0 && record.runtimeState.lifecycle != runtimeOpen {
			closedAt = index
		}
		if record.runtimeOperation == simulationAuthorizeForcedAbort {
			forcedAbortAt = index
		}
		if closedAt >= 0 && index > closedAt && record.runtimeOperation == simulationCommitTerminal {
			t.Fatalf("normal terminal commitment followed fatal closure at record %d", index)
		}
	}
	if startAt < 0 || closedAt <= startAt || forcedAbortAt >= 0 {
		t.Fatalf("start/closure/forced-abort order=%d/%d/%d", startAt, closedAt, forcedAbortAt)
	}
	replayed := ReplayLegal(explored.trace)
	if replayed.failure != nil || !reflect.DeepEqual(replayed.world, explored.world) {
		t.Fatalf("drain-expiry replay failure=%v world-equal=%v",
			replayed.failure, reflect.DeepEqual(replayed.world, explored.world))
	}

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
	if repeatedEmergency.kind == 0 || settledAt == 0 {
		t.Fatalf("fatal trace lacks emergency start/settlement: start=%v settlement=%d",
			repeatedEmergency.kind, settledAt)
	}

	malformed := simulationMalformedFact{
		authority:  simulationSupervisorAuthority,
		supervisor: repeatedEmergency,
	}
	firstViolation := ReplayViolation(explored.trace, malformed)
	secondViolation := ReplayViolation(explored.trace, malformed)
	if firstViolation.failure != nil ||
		firstViolation.invariant.operation != supervisorReducerOperation ||
		firstViolation.invariant.reason != "emergency epoch is invalid, duplicated, or conflicting" ||
		!reflect.DeepEqual(firstViolation, secondViolation) ||
		!reflect.DeepEqual(firstViolation.world.campaign.outcome, explored.world.campaign.outcome) ||
		!reflect.DeepEqual(firstViolation.world.campaign.failure, explored.world.campaign.failure) {
		t.Fatalf("repeated closure/invariant dominance first=%#v second=%#v", firstViolation, secondViolation)
	}
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
	if first.failure != nil || second.failure != nil {
		t.Fatalf("choice exploration failures=%v/%v", first.failure, second.failure)
	}
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
	if reflect.DeepEqual(firstOrder, secondOrder) {
		t.Fatalf("distinct choice streams selected the same primary order: %v; first=%#v second=%#v",
			firstOrder, first.trace.choices, second.trace.choices)
	}
	for _, explored := range []SimulationResult{first, second} {
		if replayed := ReplayLegal(explored.trace); replayed.failure != nil {
			t.Fatalf("choice-selected trace did not replay: %v", replayed.failure)
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
	if explored.failure != nil {
		t.Fatalf("resource exploration failed: %v", explored.failure)
	}
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
	if replayed.failure == nil || !strings.Contains(replayed.failure.Error(), "external campaign fact is not enabled") {
		t.Fatalf("wrong resource replay failure=%v, want exact resource rejection", replayed.failure)
	}
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
	if first.failure != nil || second.failure != nil {
		t.Fatalf("violation replay failures=%v/%v", first.failure, second.failure)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("violation replay is nondeterministic:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if first.invariant.operation != "campaign establish snapshot" ||
		first.invariant.reason != "snapshot observation is invalid" {
		t.Fatalf("retained invariant=%#v", first.invariant)
	}
	if first.key.authority != simulationCampaignAuthority ||
		first.key.operation != first.invariant.operation || first.key.reason != first.invariant.reason {
		t.Fatalf("failure key=%#v, invariant=%#v", first.key, first.invariant)
	}
	if first.world.runtime.lifecycle != runtimeClosedDrained || first.world.runtime.fatalEpoch == 0 ||
		len(first.world.runtime.fatalCauses) != 1 {
		t.Fatalf("runtime cleanup=%#v", first.world.runtime)
	}
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
	if first.failure != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("supervisor violation replay diverged: first=%#v second=%#v", first, second)
	}
	if first.key.authority != simulationSupervisorAuthority ||
		first.invariant.operation != supervisorReducerOperation {
		t.Fatalf("supervisor invariant/key=%#v/%#v", first.invariant, first.key)
	}
	residual := first.world.runtime.residualCustody()
	if first.world.runtime.lifecycle != runtimeClosedUnconfirmed ||
		len(residual) != 1 || residual[0].generation != registered.generation || !residual[0].transferred {
		t.Fatalf("supervisor violation cleanup did not transfer exact custody: %#v", first.world.runtime)
	}
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
	if explored.failure != nil {
		t.Fatalf("supervisor corruption exploration failure=%v", explored.failure)
	}
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
	if registeredAt < 0 || boundaryAt < 0 {
		t.Fatalf("supervisor corruption cuts registration/boundary=%d/%d", registeredAt, boundaryAt)
	}
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
			if first.failure != nil || !reflect.DeepEqual(first, second) {
				t.Fatalf("supervisor corruption replay diverged: first=%#v second=%#v", first, second)
			}
			if first.invariant.operation != supervisorReducerOperation || first.invariant.reason != test.reason {
				t.Fatalf("supervisor corruption invariant=%#v", first.invariant)
			}
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
		authority: simulationRuntimeAuthority, runtimeOperation: simulationRequestAdmission,
	}

	first := ReplayViolation(prefix, malformed)
	second := ReplayViolation(prefix, malformed)
	if first.failure != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("runtime violation replay diverged: first=%#v second=%#v", first, second)
	}
	if first.key.authority != simulationRuntimeAuthority ||
		first.invariant.operation != "request admission" || first.invariant.reason != "invalid request" {
		t.Fatalf("runtime invariant/key=%#v/%#v", first.invariant, first.key)
	}
	if first.world.runtime.lifecycle != runtimeClosedDrained || len(first.world.runtime.admissions) != 0 {
		t.Fatalf("runtime violation cleanup retained custody: %#v", first.world.runtime)
	}
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
		authority: simulationRuntimeAuthority, runtimeOperation: simulationAcknowledgeGrantReturn,
	}

	first := ReplayViolation(prefix, malformed)
	second := ReplayViolation(prefix, malformed)
	if first.failure != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("stale return replay diverged: first=%#v second=%#v", first, second)
	}
	if first.key.authority != simulationRuntimeAuthority ||
		first.invariant.operation != "acknowledge grant return" ||
		first.invariant.reason != "grant return authority is stale or wrong" {
		t.Fatalf("stale return invariant/key=%#v/%#v", first.invariant, first.key)
	}
	if first.world.runtime.lifecycle != runtimeClosedDrained || len(first.world.runtime.admissions) != 0 {
		t.Fatalf("stale return cleanup=%#v", first.world.runtime)
	}
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
				authority: simulationRuntimeAuthority, runtimeOperation: simulationObserveAttempt,
				runtimeGeneration:  99,
				runtimeObservation: simulationRuntimeObservation{kind: simulationLaunchOwnedObservation},
			},
			operation: observeOperation, reason: "generation is not live",
		},
		{
			name: "emergency settlement while open",
			malformed: simulationMalformedFact{
				authority: simulationRuntimeAuthority, runtimeOperation: simulationSettleEmergency,
			},
			operation: settleEmergencyOperation, reason: "resolution cardinality is invalid",
		},
		{
			name: "empty fatal cause",
			malformed: simulationMalformedFact{
				authority: simulationRuntimeAuthority, runtimeOperation: simulationCloseRuntime,
			},
			operation: "close runtime", reason: "fatal cause is empty",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first := ReplayViolation(prefix, test.malformed)
			second := ReplayViolation(prefix, test.malformed)
			if first.failure != nil || !reflect.DeepEqual(first, second) {
				t.Fatalf("runtime violation replay diverged: first=%#v second=%#v", first, second)
			}
			if first.key.authority != simulationRuntimeAuthority ||
				first.invariant.operation != test.operation || first.invariant.reason != test.reason {
				t.Fatalf("runtime invariant/key=%#v/%#v", first.invariant, first.key)
			}
			if first.world.runtime.lifecycle != runtimeClosedDrained ||
				len(first.world.runtime.admissions) != 0 {
				t.Fatalf("runtime violation cleanup=%#v", first.world.runtime)
			}
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
	if len(shrunk.records) >= len(counterexample.records) {
		t.Fatalf("record count was not reduced: got=%d input=%d", len(shrunk.records), len(counterexample.records))
	}
	if len(shrunk.definition.catalogue) != 0 {
		t.Fatalf("shrunk catalogue=%v, want no unrelated members", shrunk.definition.catalogue)
	}
	if shrunk.definition.capacity != 1 || shrunk.definition.campaign.peers != 1 {
		t.Fatalf(
			"shrunk capacity/peers=%d/%d, want accepted lower bounds 1/1",
			shrunk.definition.capacity, shrunk.definition.campaign.peers,
		)
	}
	if shrunk.malformed == nil {
		t.Fatal("shrink removed the one intended corruption")
	}
	first := ReplayViolation(shrunk, *shrunk.malformed)
	second := ReplayViolation(shrunk, *shrunk.malformed)
	if first.failure != nil || !reflect.DeepEqual(first.key, key) || !reflect.DeepEqual(first, second) {
		t.Fatalf("shrunk replay did not retain stable failure:\nkey=%#v\nfirst=%#v\nsecond=%#v", key, first, second)
	}
}

func TestSimulationShrinkRemovesPositiveTraceSuffixAndRetainsReplayFailure(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-positive-shrink", lineage: 46, command: []string{"test"},
			profile: AutomaticProfile, peers: 3,
		},
		capacity: 3, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, nil)
	if explored.failure != nil {
		t.Fatalf("positive shrink exploration failure=%v", explored.failure)
	}
	counterexample := simulationCloneTrace(explored.trace)
	counterexample.records[0].runtimeState.capacity++
	replayed := ReplayLegal(counterexample)
	if replayed.failure == nil || reflect.DeepEqual(replayed.key, FailureKey{}) {
		t.Fatalf("positive replay failure/key=%v/%#v", replayed.failure, replayed.key)
	}

	shrunk := Shrink(counterexample, replayed.key)
	if len(shrunk.records) >= len(counterexample.records) {
		t.Fatalf("positive record count was not reduced: got=%d input=%d",
			len(shrunk.records), len(counterexample.records))
	}
	if shrunk.definition.capacity != 1 || shrunk.definition.campaign.peers != 1 ||
		len(shrunk.definition.catalogue) != 0 ||
		shrunk.definition.campaign.identity != "campaign-1" || shrunk.definition.campaign.lineage != 1 {
		t.Fatalf("positive shrunk definition=%#v", shrunk.definition)
	}
	first := ReplayLegal(shrunk)
	second := ReplayLegal(shrunk)
	if first.failure == nil || !reflect.DeepEqual(first.key, replayed.key) || !reflect.DeepEqual(first, second) {
		t.Fatalf("positive shrunk replay did not retain stable failure:\nkey=%#v\nfirst=%#v\nsecond=%#v",
			replayed.key, first, second)
	}
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
	if explored.failure != nil {
		t.Fatalf("positive boundary exploration failure=%v", explored.failure)
	}
	counterexample := simulationCloneTrace(explored.trace)
	cut := -1
	for index, record := range counterexample.records {
		if record.authority == simulationSupervisorAuthority &&
			record.supervisorEvent.kind == supervisorLaunchCompleted {
			cut = index
			break
		}
	}
	if cut < 0 {
		t.Fatal("positive boundary trace has no equality cut")
	}
	counterexample.records = slices.Clone(counterexample.records[:cut+1])
	counterexample.records[cut].supervisorState.nextAction++
	replayed := ReplayLegal(counterexample)
	if replayed.failure == nil || replayed.key.kind != simulationReplayFailureKind {
		t.Fatalf("positive boundary replay=%#v", replayed)
	}
	originalMeasure := simulationTraceShrinkMeasure(counterexample)

	shrunk := Shrink(counterexample, replayed.key)
	if measure := simulationTraceShrinkMeasure(shrunk); !simulationShrinkMeasureLess(measure, originalMeasure) {
		t.Fatalf("positive boundary measure=%v, want less than %v", measure, originalMeasure)
	}
	first := ReplayLegal(shrunk)
	second := ReplayLegal(shrunk)
	if first.failure == nil || !reflect.DeepEqual(first.key, replayed.key) || !reflect.DeepEqual(first, second) {
		t.Fatalf("positive boundary shrunk replay diverged: first=%#v second=%#v", first, second)
	}
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
	if !simulationShrinkMeasureLess(simulationTraceShrinkMeasure(near), simulationTraceShrinkMeasure(far)) {
		t.Fatalf("near/far shrink measures=%v/%v", simulationTraceShrinkMeasure(near), simulationTraceShrinkMeasure(far))
	}

	simple := simulationCloneTrace(near)
	simpleRunning := *near.records[0].supervisorEvent.running
	simple.records[0].supervisorEvent.running = &simpleRunning
	simple.records[0].supervisorEvent.running.facts = nil
	if !simulationShrinkMeasureLess(simulationTraceShrinkMeasure(simple), simulationTraceShrinkMeasure(near)) {
		t.Fatalf("simple/rich payload measures=%v/%v",
			simulationTraceShrinkMeasure(simple), simulationTraceShrinkMeasure(near))
	}

	uncanonical := simulationTrace{definition: simulationDefinition{
		campaign: campaignDefinition{identity: "a", lineage: 1, peers: 1}, capacity: 1,
	}}
	canonical := simulationCloneTrace(uncanonical)
	canonical.definition.campaign.identity = "campaign-1"
	if !simulationShrinkMeasureLess(
		simulationTraceShrinkMeasure(canonical), simulationTraceShrinkMeasure(uncanonical),
	) {
		t.Fatalf("canonical/short identity measures=%v/%v",
			simulationTraceShrinkMeasure(canonical), simulationTraceShrinkMeasure(uncanonical))
	}
}

func TestSimulationShrinkRetainsTypedReplayDivergenceIndependentOfDiagnostic(t *testing.T) {
	candidate := simulationRecord{authority: simulationRuntimeAuthority}
	failing := simulationRecord{
		authority:    simulationRuntimeAuthority,
		runtimeState: simulationRuntimeState{capacity: 3},
	}
	key := simulationReplayDivergenceFailure(
		simulationTrace{}, simulationRuntimeStateDivergence, "rewritten diagnostic",
	).key
	retained := simulationRetainRecordedFailure(candidate, failing, key.divergence)
	if retained.runtimeState.capacity != 3 {
		t.Fatalf("typed replay divergence retained state=%#v", retained.runtimeState)
	}
}

func TestSimulationShrinkRemovesCatalogueMembersWithTheirCausalRecords(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-shrink-causal", lineage: 42, command: []string{"test"},
			profile: AutomaticProfile, peers: 2,
		},
		capacity: 2, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, nil)
	if explored.failure != nil {
		t.Fatalf("causal shrink exploration failure=%v", explored.failure)
	}
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
	if len(shrunk.definition.catalogue) != 0 {
		t.Fatalf("causal shrink catalogue=%v, want no unrelated mutants", shrunk.definition.catalogue)
	}
	if len(shrunk.records) >= len(counterexample.records) {
		t.Fatalf("causal shrink records=%d, want fewer than %d", len(shrunk.records), len(counterexample.records))
	}
	if shrunk.definition.capacity != 1 || shrunk.definition.campaign.peers != 1 {
		t.Fatalf("causal shrink capacity/peers=%d/%d, want 1/1",
			shrunk.definition.capacity, shrunk.definition.campaign.peers)
	}
	replayed := ReplayViolation(shrunk, *shrunk.malformed)
	if replayed.failure != nil || !reflect.DeepEqual(replayed.key, key) {
		t.Fatalf("causal shrink replay=%#v, want key %#v", replayed, key)
	}
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
	if explored.failure != nil {
		t.Fatalf("boundary shrink exploration failure=%v", explored.failure)
	}
	prefixLength := 0
	for index, record := range explored.trace.records {
		if record.authority != simulationSupervisorAuthority ||
			record.supervisorEvent.kind != supervisorLaunchCompleted {
			continue
		}
		prefixLength = index + 1
		break
	}
	if prefixLength == 0 {
		t.Fatal("boundary shrink trace has no launch completion")
	}
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
	if measure := simulationTraceShrinkMeasure(shrunk); !simulationShrinkMeasureLess(measure, originalMeasure) {
		t.Fatalf("boundary measure=%v, want less than %v", measure, originalMeasure)
	}
	if shrunk.definition.campaign.identity != "campaign-1" || shrunk.definition.campaign.lineage != 1 ||
		len(shrunk.definition.catalogue) != 0 {
		t.Fatalf("canonical shrink definition=%#v", shrunk.definition)
	}
	replayed := ReplayViolation(shrunk, *shrunk.malformed)
	if replayed.failure != nil || !reflect.DeepEqual(replayed.key, key) {
		t.Fatalf("boundary shrink replay=%#v, want key %#v", replayed, key)
	}
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

	if first.failure == nil || second.failure == nil || !reflect.DeepEqual(first.key, second.key) {
		t.Fatalf("liveness failures/keys diverged: first=%#v second=%#v", first, second)
	}
	if first.key.kind != simulationLivenessFailureKind || first.key.liveness != simulationLivenessNoMove {
		t.Fatalf("liveness failure key=%#v", first.key)
	}
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
	if shrunk.definition.capacity != 1 || shrunk.definition.campaign.peers != 1 ||
		len(shrunk.definition.catalogue) != 0 ||
		shrunk.definition.campaign.identity != "campaign-1" || shrunk.definition.campaign.lineage != 1 {
		t.Fatalf("liveness shrunk definition=%#v", shrunk.definition)
	}
	if len(shrunk.choices) != 0 {
		t.Fatalf("liveness shrunk choices=%#v", shrunk.choices)
	}
}

func TestSimulationRecorderLinearizesProductionOwnerCutsAndQuiescentProjection(t *testing.T) {
	recorder := newSimulationRecorder()
	shell := newProcessRuntimeShellWithRecorder(1, recorder)
	definition := campaignDefinition{
		identity: "campaign-conformance", lineage: 51, command: []string{"test"},
		profile: AutomaticProfile, peers: 1,
	}
	campaign, _ := beginCampaign(definition)
	runner := &managedCampaignRunner{state: campaign, recorder: recorder}
	driver := &supervisorDriver{recorder: recorder}

	registration := shell.registerCampaign(campaignProvenance{lineage: definition.lineage})
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
	if got, want := simulationAuthorities(trace), []simulationAuthority{
		simulationRuntimeAuthority, simulationCampaignAuthority, simulationSupervisorAuthority,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("production authority order=%v, want %v", got, want)
	}
	for index, record := range trace.records {
		if record.sequence != uint64(index+1) {
			t.Fatalf("production sequence at %d=%d", index, record.sequence)
		}
	}

	wantRuntime := newProcessRuntime(1)
	wantRuntime, wantRegistration := wantRuntime.registerCampaign(campaignProvenance{lineage: definition.lineage})
	wantCampaign, wantEffects := advanceCampaign(campaign, campaignEvent{
		id: 1, payload: campaignRegisteredEvent{registration: wantRegistration},
	})
	wantSupervisor, wantActions := reduceSupervisor(supervisorState{}, event)
	wantProjection := simulationWorld{
		campaign: wantCampaign, runtime: wantRuntime,
		supervisor: simulationProjectSupervisorState(wantSupervisor),
	}
	if !reflect.DeepEqual(projection, wantProjection) {
		t.Fatalf("production projection diverged:\n got=%#v\nwant=%#v", projection, wantProjection)
	}
	if !reflect.DeepEqual(trace.records[1].campaignEffects, wantEffects) ||
		!reflect.DeepEqual(trace.records[2].supervisorActions, simulationTraceSupervisorActions(wantActions)) {
		t.Fatalf("recorded ordered outputs diverged: %#v", trace.records)
	}
}

func TestSimulationReplayChecksIndependentOwnerCutsAtQuiescence(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-commutation", lineage: 510, command: []string{"test"},
			profile: AutomaticProfile, peers: 1,
		},
		capacity: 1, catalogue: []mutantIdentity{"mutant-a"},
	}, nil)
	if explored.failure != nil {
		t.Fatalf("commutation exploration failure=%v", explored.failure)
	}
	trace := simulationCloneTrace(explored.trace)
	trace.barriers = []simulationQuiescentBarrier{{
		afterSequence: trace.records[len(trace.records)-1].sequence,
		campaign:      simulationTraceCampaignState(explored.world.campaign),
		runtime:       simulationTraceRuntimeState(explored.world.runtime),
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
	if independent == 0 || causal == 0 {
		t.Fatalf("commutation trace pairs independent/causal=%d/%d", independent, causal)
	}
	if replayed := ReplayLegal(trace); replayed.failure != nil {
		t.Fatalf("quiescent commutation replay failure=%v", replayed.failure)
	}
}

func TestSimulationRecorderCorrelatesQueuedGrantWithItsRuntimeCut(t *testing.T) {
	recorder := newSimulationRecorder()
	shell := newProcessRuntimeShellWithRecorder(1, recorder)
	activeCampaign := shell.registerCampaign(campaignProvenance{lineage: 512})
	waitingCampaign := shell.registerCampaign(campaignProvenance{lineage: 513})
	active := shell.requestAdmission(admissionRequest{
		campaign: activeCampaign.token, attempt: "active", class: sharedAdmission,
	})
	waiting := shell.requestAdmission(admissionRequest{
		campaign: waitingCampaign.token, attempt: "waiting", class: sharedAdmission,
	})

	shell.cancelAdmission(<-active.delivery)
	grant := <-waiting.delivery
	event := admissionGrantedEvent{attempt: grant.attempt, grant: campaignAdmissionFact(grant)}
	recorder.mutex.Lock()
	cancelSequence := recorder.records[len(recorder.records)-1].sequence
	recorder.mutex.Unlock()
	if source := recorder.campaignSource(event); source.kind != simulationOwnerDeliverySource ||
		source.identity != cancelSequence {
		t.Fatalf("queued grant source=%#v, want cancellation cut %d", source, cancelSequence)
	}
}

func TestSimulationRecorderQuiescenceWaitsForInFlightActionCut(t *testing.T) {
	recorder := newSimulationRecorder()
	shell := newProcessRuntimeShellWithRecorder(1, recorder)
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
		t.Fatal("quiescence returned with a supervisor action still in flight")
	case <-time.After(20 * time.Millisecond):
	}

	recorder.recordSupervisorCompletion(supervisorPendingAction{kind: action.kind, token: action.token})
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("quiescence did not resume after the matching action cut")
	}
}

func TestSimulationRecorderReplaysAnEmptyProductionCampaign(t *testing.T) {
	recorder := newSimulationRecorder()
	shell := newProcessRuntimeShellWithRecorder(1, recorder)
	definition := campaignDefinition{
		identity: "campaign-recorded-replay", lineage: 52, command: []string{"test"},
		profile: AutomaticProfile, peers: 1,
	}
	campaign, _ := beginCampaign(definition)
	runner := &managedCampaignRunner{state: campaign, recorder: recorder}
	driver := &supervisorDriver{recorder: recorder}
	cut := func() { _, _ = recorder.quiescent(runner, shell, driver) }

	registration := shell.registerCampaign(campaignProvenance{lineage: definition.lineage})
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
	terminal := shell.commitTerminal(registration.token)
	cut()
	runner.advance(terminalCommittedEvent{result: campaignTerminalEvidence(terminal)})

	trace, production := recorder.quiescent(runner, shell, driver)
	if len(trace.barriers) != 7 {
		t.Fatalf("retained prefix barriers=%d, want 7", len(trace.barriers))
	}
	replayed := ReplayLegal(trace)
	if replayed.failure != nil {
		t.Fatalf("recorded production trace did not replay: %v", replayed.failure)
	}
	if !reflect.DeepEqual(replayed.world, production) {
		t.Fatalf("recorded production replay diverged:\n got=%#v\nwant=%#v", replayed.world, production)
	}
	corrupted := trace
	corrupted.barriers = slices.Clone(trace.barriers)
	corrupted.barriers[0].runtime.capacity++
	if failure := ReplayLegal(corrupted).failure; failure == nil ||
		!strings.Contains(failure.Error(), "quiescent world diverged") {
		t.Fatalf("corrupted barrier replay failure=%v", failure)
	}
}

func TestSimulationRecorderReplaysNonEmptyManagedCampaignAtQuiescence(t *testing.T) {
	recorder := newSimulationRecorder()
	shell := newProcessRuntimeShellWithRecorder(1, recorder)
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
		runtime: shell, now: tick, launchProgress: time.Second, drainEpoch: 5 * time.Second,
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
				t.Fatalf("unexpected scripted native action: %#v", action)

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
		runtime: shell, repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})
	result := runner.run(managedCampaignRequest{
		identity: "campaign-recorded-managed", lineage: 521, command: []string{"test"},
		profile: SerialProfile, peers: 1, viruses: []viruses.Virus{integerincrement.New()},
	})
	if completed, ok := result.outcome.(completedOutcome); !ok || len(completed.mutants) != 1 {
		t.Fatalf("managed outcome=%#v, want one completed mutant", result.outcome)
	}

	trace, production := recorder.quiescent(runner, shell, driver)
	if len(trace.barriers) != 1 || trace.barriers[0].afterSequence != uint64(len(trace.records)) {
		t.Fatalf("quiescent barriers=%#v, want one final accepted-prefix cut", trace.barriers)
	}
	if path := simulationForbiddenValuePath(reflect.ValueOf(trace), "trace"); path != "" {
		t.Fatalf("managed trace retained a production capability at %s", path)
	}
	for index, record := range trace.records {
		if record.source.kind == 0 || record.source.identity == 0 {
			t.Fatalf("managed production record %d has no causal source: %#v", index, record)
		}
		if want := simulationExpectedProductionSourceKind(record); record.source.kind != want {
			t.Fatalf("managed production record %d source kind=%v, want %v", index, record.source.kind, want)
		}
	}
	replayed := ReplayLegal(trace)
	if replayed.failure != nil {
		t.Fatalf("non-empty production trace did not replay: %v", replayed.failure)
	}
	if !reflect.DeepEqual(replayed.world, production) {
		t.Fatalf("non-empty production replay diverged:\n got=%#v\nwant=%#v", replayed.world, production)
	}
}

func simulationExpectedProductionSourceKind(record simulationRecord) simulationCausalSourceKind {
	switch record.authority {
	case simulationRuntimeAuthority:
		switch record.runtimeOperation {
		case simulationObserveAttempt, simulationCompleteConfirmationQueue, simulationSettleEmergency:
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
	shell := newProcessRuntimeShellWithRecorder(1, recorder)
	driver := &supervisorDriver{recorder: recorder}
	registration := shell.registerCampaign(campaignProvenance{lineage: definition.lineage})
	runner.advance(campaignRegisteredEvent{registration: registration})
	runner.advance(snapshotEstablishedEvent{snapshot: "private-snapshot"})
	mutants := []mutantIdentity{"mutant-a", "mutant-b"}
	runner.advance(catalogueDiscoveredEvent{snapshot: "private-snapshot", mutants: mutants})

	trace, _ := recorder.quiescent(runner, shell, driver)
	mutants[0] = "caller-rewrite"
	got := trace.records[len(trace.records)-1].campaignEvent.production().payload.(catalogueDiscoveredEvent).mutants
	if !reflect.DeepEqual(got, []mutantIdentity{"mutant-a", "mutant-b"}) {
		t.Fatalf("recorded catalogue changed with caller input: %v", got)
	}
}

func TestSimulationRecorderProjectsRuntimeCustodyWithoutDeliveryCapabilities(t *testing.T) {
	recorder := newSimulationRecorder()
	shell := newProcessRuntimeShellWithRecorder(1, recorder)
	registration := shell.registerCampaign(campaignProvenance{lineage: 71})
	await := shell.requestAdmission(admissionRequest{
		campaign: registration.token, attempt: "attempt-a", class: sharedAdmission,
		profile: AutomaticProfile,
	})
	if await.decision != admissionAccepted {
		t.Fatalf("admission decision=%v", await.decision)
	}
	campaign, _ := beginCampaign(campaignDefinition{
		identity: "campaign-projection", lineage: 71, command: []string{"test"},
		profile: AutomaticProfile, peers: 1,
	})
	runner := &managedCampaignRunner{state: campaign, recorder: recorder}
	driver := &supervisorDriver{recorder: recorder}

	trace, projection := recorder.quiescent(runner, shell, driver)
	if shell.core.admissions[0].grant.delivery == nil {
		t.Fatal("fixture did not retain the shell-only delivery capability")
	}
	if projection.runtime.admissions[0].grant.delivery != nil {
		t.Fatal("quiescent projection leaked a delivery channel")
	}
	if path := simulationForbiddenValuePath(reflect.ValueOf(trace), "trace"); path != "" {
		t.Fatalf("runtime trace leaked a delivery capability at %s", path)
	}
}

func TestSimulationTraceContainsOnlyCapabilityFreeDTOs(t *testing.T) {
	if path, found := executionCapabilityPath(reflect.TypeFor[simulationTrace](), nil); found {
		t.Fatalf("simulation trace contains executable capability at %s", path)
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
	if !ok || merged.resolvedMutationDeadline != recorded.resolvedMutationDeadline {
		t.Fatalf("merged terminal=%#v, want recorded deadline %#v", merged, recorded.resolvedMutationDeadline)
	}
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
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("enabled moves=%#v, want %#v", got, want)
	}
}

func TestSimulationChoiceRecordsMarkCanonicalRecovery(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-recovery", lineage: 91, command: []string{"test"},
			profile: AutomaticProfile, peers: 2,
		},
		capacity: 2, catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, simulationChoiceBytes{2})
	if explored.failure != nil {
		t.Fatalf("recovery exploration failed: %v", explored.failure)
	}
	seenExploration, seenRecovery := false, false
	for _, choice := range explored.trace.choices {
		if !choice.recovery {
			if seenRecovery {
				t.Fatalf("exploration resumed after recovery: %#v", explored.trace.choices)
			}
			seenExploration = true
			continue
		}
		seenRecovery = true
		if choice.selected != 0 {
			t.Fatalf("non-canonical recovery choice=%#v", choice)
		}
	}
	if !seenExploration || !seenRecovery {
		t.Fatalf("choice records=%#v, want exploration followed by recovery", explored.trace.choices)
	}
}

func TestSimulationEmptyCampaignRecordsOnlyEnabledCausalMoves(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-causal-empty", lineage: 92, command: []string{"test"},
			profile: AutomaticProfile, peers: 1,
		},
		capacity: 1,
	}, simulationChoiceBytes{})
	if explored.failure != nil {
		t.Fatalf("empty exploration failed: %v", explored.failure)
	}
	for index, record := range explored.trace.records {
		if record.source.kind == 0 || record.source.identity == 0 {
			t.Fatalf("record %d has no reducer-emitted causal source: %#v", index, record.source)
		}
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
	if explored.failure != nil {
		t.Fatalf("non-empty exploration failed: %v", explored.failure)
	}
	for index, record := range explored.trace.records {
		if record.source.kind == 0 || record.source.identity == 0 {
			t.Fatalf("record %d has no reducer-emitted causal source: %#v", index, record.source)
		}
	}
}

func TestSimulationRecorderProjectsFilesystemPathsToLogicalIdentities(t *testing.T) {
	recorder := newSimulationRecorder()
	shell := newProcessRuntimeShellWithRecorder(1, recorder)
	definition := campaignDefinition{
		identity: "campaign-paths", lineage: 81, command: []string{"test"},
		profile: AutomaticProfile, peers: 1,
	}
	campaign, _ := beginCampaign(definition)
	runner := &managedCampaignRunner{state: campaign, recorder: recorder}
	driver := &supervisorDriver{recorder: recorder}
	registration := shell.registerCampaign(campaignProvenance{lineage: definition.lineage})
	runner.advance(campaignRegisteredEvent{registration: registration})
	runner.advance(snapshotEstablishedEvent{snapshot: "/private/repository/snapshot-937"})

	trace, projection := recorder.quiescent(runner, shell, driver)
	projected := fmt.Sprintf("%#v %#v", trace, projection)
	if strings.Contains(projected, "/private/repository") {
		t.Fatalf("simulation projection leaked a filesystem path: %s", projected)
	}
	if projection.campaign.snapshot != "snapshot:campaign-paths" {
		t.Fatalf("logical snapshot identity=%q", projection.campaign.snapshot)
	}
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
	if got := record.supervisorEvent.at.production(); !got.Equal(registeredAt) || got.Location() != time.UTC {
		t.Fatalf("canonical instant changed: got=%s want=%s", got, registeredAt)
	}
	if got := record.supervisorState.attempts[0].registeredAt.production(); !got.Equal(registeredAt) || got.Location() != time.UTC {
		t.Fatalf("canonical state instant changed: got=%s want=%s", got, registeredAt)
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
	if explored.failure != nil {
		t.Fatalf("exploration failed: %v", explored.failure)
	}
	if path := simulationForbiddenValuePath(reflect.ValueOf(explored.trace), "trace"); path != "" {
		t.Fatalf("canonical trace retains a production capability at %s", path)
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
	if definition.capacity != 3 || definition.campaign.peers != 3 || len(definition.catalogue) != 3 ||
		len(choices) != 4 {
		t.Fatalf("fuzz definition/choices=%#v/%v", definition, choices)
	}
	explored := Explore(definition, choices)
	if explored.failure != nil {
		t.Fatalf("sustained fuzz exploration failed: %v", explored.failure)
	}
	nonRecovery := 0
	for _, choice := range explored.trace.choices {
		if !choice.recovery {
			nonRecovery++
		}
	}
	if nonRecovery < len(choices) {
		t.Fatalf("exploration consumed %d choices, want at least %d: %#v", nonRecovery, len(choices), explored.trace.choices)
	}
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

	if !engine.consume(second) || len(engine.pending) != 1 ||
		!reflect.DeepEqual(engine.pending[0].supervisorDelivery, first.supervisorDelivery) {
		t.Fatalf("pending delivery after consuming second=%#v, want first", engine.pending)
	}
}

func TestSimulationSelectsOneTypedActionFromACompoundOwnerCut(t *testing.T) {
	want := supervisorAction{kind: supervisorDeliverTerminal, token: 10}
	actions := []supervisorAction{
		want,
		{kind: supervisorSettleEmergency, token: 11},
	}
	if got := simulationOnlySupervisorAction(actions, supervisorDeliverTerminal); !reflect.DeepEqual(got, want) {
		t.Fatalf("selected action=%#v, want %#v", got, want)
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
	if len(moves) != 1 || !reflect.DeepEqual(moves[0].delivery, launch) {
		t.Fatalf("enabled moves=%#v, want only the causal launch delivery", moves)
	}
}

func TestSimulationDisablesResidualTransferAfterRuntimeCustodyMoves(t *testing.T) {
	engine := simulationEngine{
		runtime: processRuntime{
			lifecycle: runtimeFatalClosing,
			admissions: []admittedAttempt{{
				grant: admissionGrant{attempt: "attempt-a"}, stage: admissionOwned,
				generation: 2, disposition: dispositionCustodyTransferred,
			}},
		},
		pending: []simulationEngineMove{{
			source: simulationCausalSource{kind: simulationSupervisorActionSource, identity: 9},
			action: supervisorAction{kind: supervisorTransferResidualCustody, generation: 2, token: 9},
		}},
	}

	if moves := engine.enabledMoves(); len(moves) != 0 {
		t.Fatalf("enabled stale residual transfer=%#v", moves)
	}
}

func TestSimulationOrdersRuntimeCustodyActionsByOwnerToken(t *testing.T) {
	engine := simulationEngine{
		runtime: processRuntime{
			lifecycle: runtimeOpen,
			admissions: []admittedAttempt{{
				grant: admissionGrant{attempt: "attempt-a"}, stage: admissionOwned, generation: 2,
			}},
		},
		pending: []simulationEngineMove{
			{
				source: simulationCausalSource{kind: simulationSupervisorActionSource, identity: 10},
				action: supervisorAction{kind: supervisorSettleRuntime, generation: 2, token: 10},
			},
			{
				source: simulationCausalSource{kind: simulationSupervisorActionSource, identity: 11},
				action: supervisorAction{kind: supervisorSettleEmergency, token: 11},
			},
		},
	}

	moves := engine.enabledMoves()
	if len(moves) != 1 || moves[0].action.token != 10 {
		t.Fatalf("enabled runtime custody actions=%#v, want token 10 only", moves)
	}
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
	if len(engine.pending) != 1 || engine.pending[0].effect.id != 12 {
		t.Fatalf("pending work after emergency terminal retirement=%#v", engine.pending)
	}
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
	if len(moves) != 1 || moves[0].source.identity != 19 {
		t.Fatalf("enabled runtime custody moves=%#v, want completion cut 19 only", moves)
	}
}

func TestSimulationEmergencySettlementWaitsForCampaignRequest(t *testing.T) {
	engine := simulationEngine{pending: []simulationEngineMove{{
		source: simulationCausalSource{kind: simulationSupervisorActionSource, identity: 12},
		action: supervisorAction{kind: supervisorDeliverEmergencySettlement, token: 12},
	}}}

	if moves := engine.enabledMoves(); len(moves) != 0 {
		t.Fatalf("enabled unrequested emergency settlement=%#v", moves)
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
	if len(moves) != 1 || !reflect.DeepEqual(moves[0].delivery, launch) {
		t.Fatalf("enabled emergency settlement moves=%#v, want published campaign ingress", moves)
	}
}

func TestSimulationEmergencyCutWaitsForCommittedStartDelivery(t *testing.T) {
	emergency := supervisorEvent{kind: supervisorEmergencyStarted, at: time.Unix(1, 0)}
	start := startCommittedEvent{
		attempt: "attempt-b",
		result:  campaignStartResult{decision: startCommittedAccepted, generation: 4},
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
	if len(moves) != 1 || !reflect.DeepEqual(moves[0].delivery, start) {
		t.Fatalf("enabled emergency cut moves=%#v, want committed start delivery only", moves)
	}
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
	tests := []simulationEngine{
		{pending: []simulationEngineMove{launch, stop}},
		{
			supervisor: supervisorState{attempts: []supervisorAttemptState{{
				generation: generation, phase: supervisorLaunchEstablishing,
			}}},
			pending: []simulationEngineMove{launchAction, stop},
		},
	}
	for _, engine := range tests {
		moves := engine.enabledMoves()
		if len(moves) == 0 {
			t.Fatal("attempt ownership made no progress")
		}
		for _, move := range moves {
			if move.effect.kind == campaignEffectStopAttempt {
				t.Fatalf("enabled stop before supervisor ownership: %#v", moves)
			}
		}
	}
	completed := simulationEngine{
		launches: map[attemptGeneration]campaignEffect{generation: launch.effect},
		pending:  []simulationEngineMove{stop},
	}
	moves := completed.enabledMoves()
	if len(moves) != 1 || moves[0].effect.kind != campaignEffectStopAttempt {
		t.Fatalf("completed generation retained disabled stop: %#v", moves)
	}
	if !completed.consume(moves[0]) {
		t.Fatal("completed generation stop was not pending")
	}
	if err := completed.apply(moves[0]); err != nil {
		t.Fatalf("completed generation stop=%v, want no-op", err)
	}
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
	if len(moves) != 1 || moves[0].action.token != 26 {
		t.Fatalf("enabled same-generation actions=%#v, want token 26 only", moves)
	}
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
	f.Fuzz(func(t *testing.T, source []byte) {
		definition, choices := simulationFuzzInput(source)
		explored := Explore(definition, choices)
		if explored.failure != nil {
			t.Fatalf("legal exploration failed: %v; runtime=%#v; actions=%v", explored.failure,
				simulationTraceRuntimeState(explored.world.runtime), simulationRecordedActionSummary(explored.trace))
		}
		replayed := ReplayLegal(explored.trace)
		if replayed.failure != nil {
			t.Fatalf("legal replay failed: %v", replayed.failure)
		}
		if !reflect.DeepEqual(explored.world, replayed.world) {
			t.Fatalf("legal replay world diverged:\nexplored=%#v\nreplayed=%#v", explored.world, replayed.world)
		}
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
					authority: simulationRuntimeAuthority, runtimeOperation: simulationRequestAdmission,
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
		if first.failure != nil || !reflect.DeepEqual(first, second) {
			t.Fatalf("violation replay diverged: first=%#v second=%#v", first, second)
		}
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
