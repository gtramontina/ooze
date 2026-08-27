package ooze

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCampaignEmptyCatalogueRunsNoCommandAndCommitsNoMutants(t *testing.T) {
	definition := campaignDefinition{
		identity: "campaign-a",
		lineage:  11,
		command:  []string{"go", "test", "./..."},
		profile:  AutomaticProfile,
		peers:    4,
	}
	state, effects := beginCampaign(definition)
	assertCampaignEffects(t, effects, campaignEffectRegister)

	state, effects = advanceCampaign(state, campaignEvent{
		id: 1, payload: campaignRegisteredEvent{registration: campaignRegistration{
			decision: processruntime.CampaignRegistered,
			token:    campaignTokenForTest(11),
		}},
	})
	assertCampaignEffects(t, effects, campaignEffectEstablishSnapshot)

	state, effects = advanceCampaign(state, campaignEvent{
		id: 2, payload: snapshotEstablishedEvent{snapshot: "snapshot-a"},
	})
	assertCampaignEffects(t, effects, campaignEffectDiscoverCatalogue)

	state, effects = advanceCampaign(state, campaignEvent{
		id: 3, payload: catalogueDiscoveredEvent{snapshot: "snapshot-a"},
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
	{
		got := state.commandCount()
		assert.EqualValues(t, 0, got, "command count=%d, want 0", got)
	}

	state, effects = advanceCampaign(state, campaignEvent{
		id: 4, payload: resourceSettledEvent{kind: campaignResourceSnapshot, identity: "snapshot-a"},
	})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)

	state, effects = advanceCampaign(state, campaignEvent{
		id: 5, payload: terminalCommittedEvent{result: campaignTerminalResult{decision: processruntime.TerminalCommitted}},
	})
	assert.EqualValues(t, 0, len(effects), "terminal state/effects=%#v/%#v", state, effects)
	assert.Equal(t, noMutantsOutcome{}, state.outcome, "terminal state/effects=%#v/%#v", state, effects)
	assert.EqualValues(t, 0, len(state.obligations), "normal terminal retained obligations: %#v", state.obligations)
	assert.EqualValues(t, "go", definition.command[0], "definition was not preserved: input=%#v state=%#v", definition, state.definition)
	assert.EqualValues(t, "go", state.definition.command[0], "definition was not preserved: input=%#v state=%#v", definition, state.definition)
}

func TestCampaignDefinitionOwnsPositiveTenMinuteBaselineBootstrap(t *testing.T) {
	definition := campaignDefinition{
		identity: "campaign-a", lineage: 11, command: []string{"test"}, profile: SerialProfile, peers: 8,
	}
	state, _ := beginCampaign(definition)
	assert.Equal(t, 10*time.Minute, state.definition.baselineDeadline, "baseline deadline=%s, want 10m", state.definition.baselineDeadline)

	definition.baselineDeadline = time.Second
	defer func() {
		assert.NotNil(t, recover(), "caller-selected baseline deadline was accepted")
	}()
	_, _ = beginCampaign(definition)
}

func TestCampaignNonEmptyCatalogueRunsOneSnapshotBoundBaselineBeforePrimaries(t *testing.T) {
	definition := campaignDefinition{
		identity: "campaign-a", lineage: 11, command: []string{"go", "test"},
		env: []string{"A=1"}, profile: AutomaticProfile, peers: 4,
	}
	runtime := processruntime.NewReplay(4)
	state, _ := beginCampaign(definition)
	runtime, registrationResult := runtime.Apply(processruntime.RegisterCampaignCut(definition.lineage))
	registered := campaignRegistrationEvidence(registrationResult.Registration())
	state, _ = advanceCampaign(state, campaignEvent{
		id: 1, payload: campaignRegisteredEvent{registration: registered},
	})
	state, _ = advanceCampaign(state, campaignEvent{
		id: 2, payload: snapshotEstablishedEvent{snapshot: "snapshot-a"},
	})
	mutants := []mutantIdentity{"mutant-a", "mutant-b"}
	state, effects := advanceCampaign(state, campaignEvent{
		id: 3, payload: catalogueDiscoveredEvent{snapshot: "snapshot-a", mutants: mutants},
	})
	assertCampaignEffects(t, effects, campaignEffectMaterializeWorkspace)
	baseline := effects[0]
	assert.EqualValues(t, "snapshot-a", baseline.snapshot, "baseline materialization=%#v", baseline)
	assert.NotEqual(t, "", baseline.attempt, "baseline materialization=%#v", baseline)
	mutants[0] = "mutated-by-caller"
	assert.Equal(t, []mutantIdentity{"mutant-a", "mutant-b"}, state.catalogue, "catalogue aliases caller input: %#v", state.catalogue)

	state, effects = advanceCampaign(state, campaignEvent{
		id: 4, payload: workspaceMaterializedEvent{
			attempt: baseline.attempt, workspace: "workspace-baseline", snapshot: baseline.snapshot,
		},
	})
	assertCampaignEffects(t, effects, campaignEffectRequestAdmission)
	assert.Equal(t, exclusiveAdmission, effects[0].request.class, "baseline admission=%#v, want exclusive", effects[0].request)
	runtime, admissionResult := runtime.Apply(processruntime.RequestAdmissionCut(
		processRuntimeAdmission(effects[0].request),
	))
	admitted := runtimeAdmissionResult(admissionResult.Admission())
	state, effects = advanceCampaign(state, campaignEvent{
		id: 5, payload: admissionGrantedEvent{
			attempt: baseline.attempt, grant: campaignAdmissionValue(admitted.deliveries[0]),
		},
	})
	assertCampaignEffects(t, effects, campaignEffectRequestStartCommitment)
	runtime, startResult := runtime.Apply(processruntime.CommitStartCut(
		processRuntimeAdmission(campaignAdmissionValue(admitted.deliveries[0])),
	))
	started := runtimeStartResult(startResult.Start())
	state, effects = advanceCampaign(state, campaignEvent{
		id: 6, payload: startCommittedEvent{
			attempt: baseline.attempt, grant: effects[0].grant, result: campaignStartEvidence(started),
		},
	})
	assertCampaignEffects(t, effects, campaignEffectLaunchAttempt)
	assert.EqualValues(t, "snapshot-a", effects[0].snapshot, "baseline launch=%#v", effects[0])
	assert.EqualValues(t, "workspace-baseline", effects[0].workspace, "baseline launch=%#v", effects[0])
	wantSpec := Spec{
		Attempt: string(baseline.attempt), Command: []string{"go", "test"}, Dir: "workspace-baseline",
		Env: []string{"A=1"}, Profile: AutomaticProfile, Deadline: 10 * time.Minute,
	}
	assert.Equal(t, wantSpec, effects[0].spec, "baseline supervisor spec=%#v, want %#v", effects[0].spec, wantSpec)
	runtime, launchResult := runtime.Apply(processruntime.ObserveAttemptCut(started.generation, processruntime.Owned()))
	launchReceipt := runtimeReceipt(launchResult.Receipt())
	state, effects = advanceCampaign(state, campaignEvent{
		id: 7, payload: attemptLaunchEvent{
			attempt: baseline.attempt, generation: started.generation,
			result: campaignLaunchObservation{kind: campaignLaunchOwned}, receipt: campaignReceiptValue(launchReceipt),
		},
	})
	assert.EqualValues(t, 0, len(effects), "owned baseline emitted effects=%#v", effects)

	terminal := Settled{
		Exit:          ExitStatus{},
		ExecutionData: ExecutionData{Deadline: 10 * time.Minute, CommandDuration: 2 * time.Second},
	}
	_, terminalResult := runtime.Apply(processruntime.ObserveAttemptCut(
		started.generation, processRuntimeObservation(attemptSettled{}),
	))
	terminalReceipt := runtimeReceipt(terminalResult.Receipt())
	resolved := resolveMutationDeadline(terminal.CommandDuration, definition.peers)
	state, effects = advanceCampaign(state, campaignEvent{
		id: 8, payload: attemptTerminalEvent{
			attempt: baseline.attempt, generation: started.generation, terminal: terminal,
			receipt:                  campaignReceiptValue(terminalReceipt),
			resolvedMutationDeadline: resolveBaselineMutationDeadline(terminal.CommandDuration, definition.peers),
		},
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	assert.Equal(t, 20*time.Second, resolved, "baseline resolution/count=%s/%s/%d", resolved, state.mutationDeadline, state.commandCount())
	assert.Equal(t, resolved, state.mutationDeadline, "baseline resolution/count=%s/%s/%d", resolved, state.mutationDeadline, state.commandCount())
	assert.EqualValues(t, 1, state.commandCount(), "baseline resolution/count=%s/%s/%d", resolved, state.mutationDeadline, state.commandCount())

	state, effects = advanceCampaign(state, campaignEvent{
		id: 9, payload: resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-baseline"},
	})
	assert.Equal(t, campaignRunning, state.phase, "phase=%v, want running", state.phase)
	assert.EqualValues(t, 2, len(effects), "primary demand effects=%#v, want one per mutant up to peers", effects)
	for _, effect := range effects {
		assert.Equal(t, campaignEffectMaterializeWorkspace, effect.kind, "primary materialization escaped snapshot: %#v", effect)
		assert.EqualValues(t, "snapshot-a", effect.snapshot, "primary materialization escaped snapshot: %#v", effect)
	}
}

func TestCampaignReplayConsumesPositiveRecordedBaselineDeadlineWithoutRecomputation(t *testing.T) {
	harness := newCampaignHarness(t, []mutantIdentity{"mutant-a"}, AutomaticProfile, 2)
	baseline := harness.launchMaterialized(t, harness.effects[0], "workspace-baseline")
	effects := harness.settleAttempt(t, baseline, Settled{
		Exit: ExitStatus{}, ExecutionData: ExecutionData{
			Deadline: 10 * time.Minute, CommandDuration: 2 * time.Second,
		},
	}, time.Nanosecond)
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	assert.Equal(t, time.Nanosecond, harness.state.mutationDeadline, "replay deadline=%s, want recorded 1ns", harness.state.mutationDeadline)
}

func TestCampaignCompletedRunsExactlyOneBaselineAndOnePrimaryPerMutant(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a", "mutant-b"}, 2)
	require.EqualValues(t, 2, len(primaryEffects), "primary effects=%#v", primaryEffects)

	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	second := harness.launchMaterialized(t, primaryEffects[1], "workspace-b")
	firstEffects := harness.settleAttempt(t, first, Settled{
		Exit:          ExitStatus{},
		ExecutionData: ExecutionData{Deadline: harness.state.mutationDeadline, CommandDuration: time.Second},
	}, 0)
	secondEffects := harness.settleAttempt(t, second, Settled{
		Exit:          ExitStatus{Code: 1},
		ExecutionData: ExecutionData{Deadline: harness.state.mutationDeadline, CommandDuration: time.Second},
	}, 0)
	assertCampaignEffects(t, firstEffects, campaignEffectReleaseWorkspace)
	assertCampaignEffects(t, secondEffects, campaignEffectReleaseWorkspace)

	effects := harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-a"})
	assert.EqualValues(t, 0, len(effects), "first primary cleanup effects=%#v", effects)
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-b"})
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
	assert.Equal(t, campaignDraining, harness.state.phase, "completion drain=%#v", harness.state.drain)
	assert.Equal(t, campaignDrainComplete, harness.state.drain.kind, "completion drain=%#v", harness.state.drain)
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceSnapshot, identity: "snapshot-a"})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)
	_ = runtimeReplayTerminal(harness.applyRuntime(processruntime.CommitTerminalCut(harness.state.runtimeToken)))
	harness.advance(terminalCommittedEvent{result: campaignTerminalResult{decision: processruntime.TerminalCommitted}})

	completed, ok := harness.state.outcome.(completedOutcome)
	want := []mutantResult{{mutant: "mutant-a", kind: mutantSurvived}, {mutant: "mutant-b", kind: mutantKilled}}
	got := make([]mutantResult, len(completed.mutants))
	for index, mutant := range completed.mutants {
		got[index] = mutantResult{mutant: mutant.mutant, kind: mutant.kind}
	}
	require.True(t, ok, "completed outcome=%#v, want %#v", harness.state.outcome, want)
	assert.Equal(t, want, got, "completed outcome=%#v, want %#v", harness.state.outcome, want)
	assert.Equal(t, campaignEvidenceSettled, completed.mutants[0].primary.kind, "completed outcome discarded primary evidence: %#v", completed.mutants)
	assert.Equal(t, campaignEvidenceSettled, completed.mutants[1].primary.kind, "completed outcome discarded primary evidence: %#v", completed.mutants)
	assert.EqualValues(t, 3, harness.state.commandCount(), "commands/obligations=%d/%#v", harness.state.commandCount(), harness.state.obligations)
	assert.EqualValues(t, 0, len(harness.state.obligations), "commands/obligations=%d/%#v", harness.state.commandCount(), harness.state.obligations)
}

func TestCampaignFailedBaselineAbortsUnscoredAfterSettlement(t *testing.T) {
	harness := newCampaignHarness(t, []mutantIdentity{"mutant-a"}, AutomaticProfile, 2)
	baseline := harness.launchMaterialized(t, harness.effects[0], "workspace-baseline")
	effects := harness.settleAttempt(t, baseline, Settled{
		Exit: ExitStatus{Code: 1},
		ExecutionData: ExecutionData{
			Deadline: 10 * time.Minute, CommandDuration: time.Second,
			Output: OutputSnapshot{
				Bytes: "full baseline failure\n", Cutoff: 22, CompleteThroughCutoff: true, Final: true,
			},
		},
	}, 0)
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-baseline"})
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceSnapshot, identity: "snapshot-a"})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)
	committedResult := harness.applyRuntime(processruntime.CommitTerminalCut(harness.state.runtimeToken)).Terminal()
	committed := terminalResult{decision: committedResult.Decision(), epoch: fatalEpochID(committedResult.Epoch())}
	harness.advance(terminalCommittedEvent{result: campaignTerminalEvidence(committed)})
	aborted, ok := harness.state.outcome.(abortedOutcome)
	require.True(t, ok, "baseline failure outcome/count=%#v/%d", harness.state.outcome, harness.state.commandCount())
	assert.EqualValues(t, "full baseline failure\n", aborted.baseline.output.Bytes, "baseline failure outcome/count=%#v/%d", harness.state.outcome, harness.state.commandCount())
	assert.True(t, aborted.baseline.output.Final, "baseline failure outcome/count=%#v/%d", harness.state.outcome, harness.state.commandCount())
	assert.EqualValues(t, 1, aborted.total, "baseline failure outcome/count=%#v/%d", harness.state.outcome, harness.state.commandCount())
	assert.EqualValues(t, 1, harness.state.commandCount(), "baseline failure outcome/count=%#v/%d", harness.state.outcome, harness.state.commandCount())
}

func TestCampaignBaselineAbortCauseDistinguishesCommandAndInfrastructureTerminals(t *testing.T) {
	tests := []struct {
		name     string
		terminal Terminal
		want     string
	}{
		{"nonzero_exit", Settled{Exit: ExitStatus{Code: 1}}, "baseline did not pass"},
		{"automatic_deadline", Tripped{Trip: AutomaticDeadlineTrip{}}, "baseline command deadline fired"},
		{"process_fuse", Tripped{Trip: FuseTrip{Live: 65}}, "baseline process fuse fired"},
		{"stopped", Stopped{}, "baseline was stopped"},
		{"infrastructure_census", Infrastructure{Cause: CensusFailed}, "baseline infrastructure uncertainty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := campaignBaselineAbortCause(test.terminal)
			assert.Equal(t, test.want, got, "cause for %T = %q, want %q", test.terminal, got, test.want)
		})
	}
}

func TestCampaignPrimaryInfrastructureAbortStopsCommittedPeersWithoutRetry(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a", "mutant-b"}, 2)
	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	second := harness.launchMaterialized(t, primaryEffects[1], "workspace-b")
	effects := harness.settleAttempt(t, first, Infrastructure{
		Cause: CensusFailed, Err: errors.New("census failed"),
		ExecutionData: ExecutionData{Deadline: harness.state.mutationDeadline, CommandDuration: time.Second},
	}, 0)
	assertCampaignEffects(t, effects, campaignEffectStopAttempt, campaignEffectReleaseWorkspace)
	assert.Equal(t, second.attempt, effects[0].attempt, "abort stop/drain=%#v/%#v", effects[0], harness.state.drain)
	assert.Equal(t, campaignDrainAbort, harness.state.drain.kind, "abort stop/drain=%#v/%#v", effects[0], harness.state.drain)
	effects = harness.settleAttempt(t, second, Stopped{
		ExecutionData: ExecutionData{Deadline: harness.state.mutationDeadline, CommandDuration: time.Second},
	}, 0)
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-a"})
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-b"})
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
	assert.EqualValues(t, 3, harness.state.commandCount(), "abort duplicated command: count=%d", harness.state.commandCount())
}

func TestCampaignBaselineProvenNotReleasedAbortsWithoutASecondLaunch(t *testing.T) {
	harness := newCampaignHarness(t, []mutantIdentity{"mutant-a"}, AutomaticProfile, 1)
	effect := harness.effects[0]
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: effect.attempt, workspace: "workspace-baseline", snapshot: effect.snapshot,
	})
	admitted := runtimeReplayAdmission(harness.applyRuntime(processruntime.RequestAdmissionCut(processRuntimeAdmission(effects[0].request))))
	effects = harness.advance(admissionGrantedEvent{
		attempt: effect.attempt, grant: campaignAdmissionValue(admitted.deliveries[0]),
	})
	grant := effects[0].grant
	started := runtimeReplayStart(harness.applyRuntime(processruntime.CommitStartCut(processRuntimeAdmission(campaignAdmissionValue(admitted.deliveries[0])))))
	harness.advance(startCommittedEvent{
		attempt: effect.attempt, grant: grant, result: campaignStartEvidence(started),
	})
	receipt := runtimeReplayReceipt(harness.applyRuntime(processruntime.ObserveAttemptCut(started.generation, processRuntimeObservation(launchNotReleased{reason: launchFailed}))))
	effects = harness.advance(attemptLaunchEvent{
		attempt: effect.attempt, generation: started.generation,
		result:  campaignLaunchObservation{kind: campaignLaunchNotReleased, failure: LaunchFailed},
		receipt: campaignReceiptValue(receipt),
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	assert.Equal(t, campaignDrainAbort, harness.state.drain.kind, "launch failure drain/count=%#v/%d", harness.state.drain, harness.state.commandCount())
	assert.EqualValues(t, 1, harness.state.commandCount(), "launch failure drain/count=%#v/%d", harness.state.drain, harness.state.commandCount())
	effects = harness.advance(resourceSettledEvent{
		kind: campaignResourceWorkspace, identity: "workspace-baseline",
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
}

func TestCampaignRejectedStartAfterConfirmationGateReturnsTheGrant(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(
		t, []mutantIdentity{"mutant-a", "mutant-b", "mutant-c"}, 3,
	)
	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	_ = harness.launchMaterialized(t, primaryEffects[1], "workspace-b")
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: primaryEffects[2].attempt, workspace: "workspace-c", snapshot: primaryEffects[2].snapshot,
	})
	admitted := runtimeReplayAdmission(harness.applyRuntime(processruntime.RequestAdmissionCut(processRuntimeAdmission(effects[0].request))))
	effects = harness.advance(admissionGrantedEvent{
		attempt: primaryEffects[2].attempt, grant: campaignAdmissionValue(admitted.deliveries[0]),
	})
	grant := effects[0].grant

	deadline := harness.state.mutationDeadline
	effects = harness.settleAttempt(t, first, Tripped{
		Trip: AutomaticDeadlineTrip{}, ExecutionData: ExecutionData{
			Deadline: deadline, CommandDuration: deadline, BoundFired: CommandDeadlineFired,
		},
	}, 0)
	assertCampaignEffects(t, effects, campaignEffectReturnAdmission, campaignEffectReleaseWorkspace)
	rejected := runtimeReplayStart(harness.applyRuntime(processruntime.CommitStartCut(processRuntimeAdmission(campaignAdmissionValue(admitted.deliveries[0])))))
	assert.Equal(t, processruntime.StartRejectedGrant, rejected.decision, "start decision=%v, want compensated-grant rejection", rejected.decision)
	effects = harness.advance(startCommittedEvent{
		attempt: primaryEffects[2].attempt, grant: grant, result: campaignStartEvidence(rejected),
	})
	assert.EqualValues(t, 0, len(effects), "rejected start duplicated compensation: %#v", effects)
}

func TestCampaignLateProvisionalReceiptDoesNotRepeatAcknowledgedGrantReturn(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(
		t, []mutantIdentity{"mutant-a", "mutant-b", "mutant-c"}, 3,
	)
	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	_ = harness.launchMaterialized(t, primaryEffects[1], "workspace-b")
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: primaryEffects[2].attempt, workspace: "workspace-c", snapshot: primaryEffects[2].snapshot,
	})
	admitted := runtimeReplayAdmission(harness.applyRuntime(processruntime.RequestAdmissionCut(processRuntimeAdmission(effects[0].request))))
	effects = harness.advance(admissionGrantedEvent{
		attempt: primaryEffects[2].attempt, grant: campaignAdmissionValue(admitted.deliveries[0]),
	})
	grant := effects[0].grant

	deadline := harness.state.mutationDeadline
	receipt := runtimeReplayReceipt(harness.applyRuntime(processruntime.ObserveAttemptCut(first.generation, processRuntimeObservation(attemptTripped{
		kind: deadlineTrip, profile: AutomaticProfile, deadline: deadline,
	}))))

	require.Len(t, receipt.compensatedGrants, 1, "provisional compensation=%#v, want exactly %#v", receipt.compensatedGrants, grant)
	assert.Equal(t, grant, campaignAdmissionValue(receipt.compensatedGrants[0]), "provisional compensation=%#v, want exactly %#v", receipt.compensatedGrants, grant)

	rejected := runtimeReplayStart(harness.applyRuntime(processruntime.CommitStartCut(processRuntimeAdmission(grant))))
	assert.Equal(t, processruntime.StartRejectedGrant, rejected.decision, "start decision=%v, want compensated-grant rejection", rejected.decision)
	effects = harness.advance(startCommittedEvent{
		attempt: primaryEffects[2].attempt, grant: grant, result: campaignStartEvidence(rejected),
	})
	assertCampaignEffects(t, effects, campaignEffectReturnAdmission)
	returned := runtimeReplayAdmission(harness.applyRuntime(processruntime.ReturnGrantCut(processRuntimeAdmission(grant))))
	effects = harness.advance(grantReturnAcknowledgedEvent{
		grant: grant, result: campaignAdmissionEvidence(returned),
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-c"})

	effects = harness.advance(attemptTerminalEvent{
		attempt: first.attempt, generation: first.generation,
		terminal: Tripped{
			Trip: AutomaticDeadlineTrip{},
			ExecutionData: ExecutionData{
				Deadline: deadline, CommandDuration: deadline, BoundFired: CommandDeadlineFired,
			},
		},
		receipt: campaignReceiptValue(receipt),
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
}

func TestCampaignLateRejectedStartDoesNotReopenAcknowledgedGrantReturn(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(
		t, []mutantIdentity{"mutant-a", "mutant-b", "mutant-c"}, 3,
	)
	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	_ = harness.launchMaterialized(t, primaryEffects[1], "workspace-b")
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: primaryEffects[2].attempt, workspace: "workspace-c", snapshot: primaryEffects[2].snapshot,
	})
	admitted := runtimeReplayAdmission(harness.applyRuntime(processruntime.RequestAdmissionCut(processRuntimeAdmission(effects[0].request))))
	effects = harness.advance(admissionGrantedEvent{
		attempt: primaryEffects[2].attempt, grant: campaignAdmissionValue(admitted.deliveries[0]),
	})
	grant := effects[0].grant

	effects = harness.settleAttempt(t, first, DrainUnconfirmed{
		Residual: OwnedUndrained,
		ExecutionData: ExecutionData{
			Deadline: harness.state.mutationDeadline, CommandDuration: time.Second,
		},
	}, 0)
	assertCampaignEffects(t, effects, campaignEffectReturnAdmission)
	rejected := runtimeReplayStart(harness.applyRuntime(processruntime.CommitStartCut(processRuntimeAdmission(grant))))
	assert.Equal(t, processruntime.StartRejectedClosed, rejected.decision, "start decision=%v, want closed-runtime rejection", rejected.decision)
	returned := admissionResult{decision: processruntime.AdmissionReturnedAfterGateClosure}
	effects = harness.advance(grantReturnAcknowledgedEvent{
		grant: grant, result: campaignAdmissionEvidence(returned),
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-c"})

	effects = harness.advance(startCommittedEvent{
		attempt: primaryEffects[2].attempt, grant: grant, result: campaignStartEvidence(rejected),
	})
	assert.EqualValues(t, 0, len(effects), "late rejected start reopened acknowledged grant return: %#v", effects)
}

func TestCampaignFailedCleanupAcceptsLateAcknowledgedStartRejection(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(
		t, []mutantIdentity{"mutant-a", "mutant-b"}, 2,
	)
	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: primaryEffects[1].attempt, workspace: "workspace-b", snapshot: primaryEffects[1].snapshot,
	})
	admitted := runtimeReplayAdmission(harness.applyRuntime(processruntime.RequestAdmissionCut(processRuntimeAdmission(effects[0].request))))
	effects = harness.advance(admissionGrantedEvent{
		attempt: primaryEffects[1].attempt, grant: campaignAdmissionValue(admitted.deliveries[0]),
	})
	grant := effects[0].grant
	effects = harness.settleAttempt(t, first, DrainUnconfirmed{
		Residual: OwnedUndrained,
		ExecutionData: ExecutionData{
			Deadline: harness.state.mutationDeadline, CommandDuration: time.Second,
		},
	}, 0)
	assertCampaignEffects(t, effects, campaignEffectReturnAdmission)
	rejected := runtimeReplayStart(harness.applyRuntime(processruntime.CommitStartCut(processRuntimeAdmission(grant))))
	assert.Equal(t, processruntime.StartRejectedClosed, rejected.decision, "start decision=%v, want closed-runtime rejection", rejected.decision)
	returned := runtimeReplayAdmission(harness.applyRuntime(processruntime.ReturnGrantCut(processRuntimeAdmission(grant))))
	effects = harness.advance(grantReturnAcknowledgedEvent{
		grant: grant, result: campaignAdmissionEvidence(returned),
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	settlement := runtimeReplaySettlement(harness.applyRuntime(processruntime.SettleEmergencyCut(processRuntimeResolutions(emergencySweep{resolutions: []emergencyResolution{{
		generation: first.generation, disposition: emergencyCustodyTransferred,
	}}}))))

	harness.advance(runtimeEmergencySettledEvent{
		epoch: harness.state.drain.epoch, settlement: campaignSettlementValue(settlement),
	})
	assert.NotNil(t, harness.state.failure, "transferred residual did not establish cleanup failure")
	harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-b"})

	effects = harness.advance(startCommittedEvent{
		attempt: primaryEffects[1].attempt, grant: grant, result: campaignStartEvidence(rejected),
	})
	assert.EqualValues(t, 0, len(effects), "late rejected start reopened failed cleanup: %#v", effects)
}

func TestCampaignFailedCleanupAcceptsPendingGrantReturnAcknowledgement(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(
		t, []mutantIdentity{"mutant-a", "mutant-b"}, 2,
	)
	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: primaryEffects[1].attempt, workspace: "workspace-b", snapshot: primaryEffects[1].snapshot,
	})
	admitted := runtimeReplayAdmission(harness.applyRuntime(processruntime.RequestAdmissionCut(processRuntimeAdmission(effects[0].request))))
	effects = harness.advance(admissionGrantedEvent{
		attempt: primaryEffects[1].attempt, grant: campaignAdmissionValue(admitted.deliveries[0]),
	})
	grant := effects[0].grant
	effects = harness.settleAttempt(t, first, DrainUnconfirmed{
		Residual: OwnedUndrained,
		ExecutionData: ExecutionData{
			Deadline: harness.state.mutationDeadline, CommandDuration: time.Second,
		},
	}, 0)
	assertCampaignEffects(t, effects, campaignEffectReturnAdmission)
	returned := runtimeReplayAdmission(harness.applyRuntime(processruntime.ReturnGrantCut(processRuntimeAdmission(grant))))
	settlement := runtimeReplaySettlement(harness.applyRuntime(processruntime.SettleEmergencyCut(processRuntimeResolutions(emergencySweep{resolutions: []emergencyResolution{{
		generation: first.generation, disposition: emergencyCustodyTransferred,
	}}}))))

	harness.advance(runtimeEmergencySettledEvent{
		epoch: harness.state.drain.epoch, settlement: campaignSettlementValue(settlement),
	})
	assert.NotNil(t, harness.state.failure, "transferred residual did not establish cleanup failure")

	effects = harness.advance(grantReturnAcknowledgedEvent{
		grant: grant, result: campaignAdmissionEvidence(returned),
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
}

func TestCampaignFailedCleanupReturnsCompensatedGrantDeliveredAfterClosure(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(
		t, []mutantIdentity{"mutant-a", "mutant-b"}, 2,
	)
	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: primaryEffects[1].attempt, workspace: "workspace-b", snapshot: primaryEffects[1].snapshot,
	})
	admitted := runtimeReplayAdmission(harness.applyRuntime(processruntime.RequestAdmissionCut(processRuntimeAdmission(effects[0].request))))
	require.Len(t, admitted.deliveries, 1, "admission deliveries=%#v, want one delayed grant", admitted.deliveries)
	grant := campaignAdmissionValue(admitted.deliveries[0])
	effects = harness.settleAttempt(t, first, DrainUnconfirmed{
		Residual: OwnedUndrained,
		ExecutionData: ExecutionData{
			Deadline: harness.state.mutationDeadline, CommandDuration: time.Second,
		},
	}, 0)
	assert.EqualValues(t, 0, len(effects), "closure compensation state/effects=%#v/%#v", harness.state.pendingGrantReturns, effects)
	assert.True(t, slices.Contains(harness.state.pendingGrantReturns, grant), "closure compensation state/effects=%#v/%#v", harness.state.pendingGrantReturns, effects)
	settlement := runtimeReplaySettlement(harness.applyRuntime(processruntime.SettleEmergencyCut(processRuntimeResolutions(emergencySweep{resolutions: []emergencyResolution{{
		generation: first.generation, disposition: emergencyCustodyTransferred,
	}}}))))

	harness.advance(runtimeEmergencySettledEvent{
		epoch: harness.state.drain.epoch, settlement: campaignSettlementValue(settlement),
	})
	assert.NotNil(t, harness.state.failure, "transferred residual did not establish cleanup failure")

	effects = harness.advance(admissionGrantedEvent{
		attempt: primaryEffects[1].attempt, grant: grant,
	})
	assertCampaignEffects(t, effects, campaignEffectReturnAdmission)
}

func TestCampaignGateRejectedStartOwnsExactlyOneGrantReturn(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a"}, 1)
	effect := primaryEffects[0]
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: effect.attempt, workspace: "workspace-a", snapshot: effect.snapshot,
	})
	admitted := runtimeReplayAdmission(harness.applyRuntime(processruntime.RequestAdmissionCut(processRuntimeAdmission(effects[0].request))))
	effects = harness.advance(admissionGrantedEvent{
		attempt: effect.attempt, grant: campaignAdmissionValue(admitted.deliveries[0]),
	})
	grant := effects[0].grant
	rejected := startCommittedResult{decision: processruntime.StartRejectedGate}
	assert.Equal(t, processruntime.StartRejectedGate, rejected.decision, "start decision=%v, want gate rejection", rejected.decision)
	effects = harness.advance(startCommittedEvent{
		attempt: effect.attempt, grant: grant, result: campaignStartEvidence(rejected),
	})
	assertCampaignEffects(t, effects, campaignEffectReturnAdmission)
	require.Len(t, harness.state.pendingGrantReturns, 1, "pending grant returns=%#v, want exactly %#v", harness.state.pendingGrantReturns, grant)
	assert.Equal(t, grant, harness.state.pendingGrantReturns[0], "pending grant returns=%#v, want exactly %#v", harness.state.pendingGrantReturns, grant)

	returned := admissionResult{decision: processruntime.AdmissionReturnedAfterGateClosure}
	effects = harness.advance(grantReturnAcknowledgedEvent{
		grant: grant, result: campaignAdmissionEvidence(returned),
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
}

func TestCampaignConfirmationInfrastructureAbortsUnscored(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a", "mutant-b"}, 2)
	primary := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	peer := harness.launchMaterialized(t, primaryEffects[1], "workspace-b")
	deadline := harness.state.mutationDeadline
	harness.settleAttempt(t, primary, Tripped{
		Trip: AutomaticDeadlineTrip{}, ExecutionData: ExecutionData{
			Deadline: deadline, CommandDuration: deadline, BoundFired: CommandDeadlineFired,
		},
	}, 0)
	harness.settleAttempt(t, peer, Settled{
		Exit: ExitStatus{}, ExecutionData: ExecutionData{Deadline: deadline, CommandDuration: time.Second},
	}, 0)
	harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-a"})
	effects := harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-b"})
	confirmation := harness.launchConfirmation(t, effects[0], "workspace-confirm")
	effects = harness.settleAttempt(t, confirmation, Infrastructure{
		Cause: CensusFailed, Err: errors.New("census failed"),
		ExecutionData: ExecutionData{Deadline: deadline, CommandDuration: time.Second},
	}, 0)
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	assert.Equal(t, campaignDrainAbort, harness.state.drain.kind, "confirmation infrastructure drain=%#v", harness.state.drain)
}

func TestCampaignConfirmationFuseIsAuthoritativeRunaway(t *testing.T) {
	harness, confirmation, mutant := newCampaignConfirmationHarness(t)
	effects := harness.settleConfirmation(t, confirmation, Tripped{
		Trip: FuseTrip{Live: 9}, ExecutionData: ExecutionData{
			Deadline: harness.state.mutationDeadline, CommandDuration: time.Second,
		},
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	assert.Equal(t, mutantRunaway, harness.state.mutants[harness.state.mutantIndex(mutant)].result, "confirmation fuse result=%#v", harness.state.mutants)
}

func TestCampaignConfirmationRequiresConfirmationRuntimeReceipt(t *testing.T) {
	harness, confirmation, _ := newCampaignConfirmationHarness(t)
	defer func() {
		violation, ok := recover().(runtimeInvariantViolation)
		require.True(t, ok, "recovered=%#v", violation)
		assert.EqualValues(t, "campaign observe confirmation terminal", violation.operation, "recovered=%#v", violation)
	}()
	harness.settleAttempt(t, confirmation, Settled{
		Exit: ExitStatus{}, ExecutionData: ExecutionData{
			Deadline: harness.state.mutationDeadline, CommandDuration: time.Second,
		},
	}, 0)
}

func newCampaignConfirmationHarness(
	t *testing.T,
) (*campaignHarness, launchedCampaignAttempt, mutantIdentity) {
	t.Helper()
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a", "mutant-b"}, 2)
	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	second := harness.launchMaterialized(t, primaryEffects[1], "workspace-b")
	deadline := harness.state.mutationDeadline
	harness.settleAttempt(t, first, Tripped{
		Trip: AutomaticDeadlineTrip{}, ExecutionData: ExecutionData{
			Deadline: deadline, CommandDuration: deadline, BoundFired: CommandDeadlineFired,
		},
	}, 0)
	harness.settleAttempt(t, second, Settled{
		Exit: ExitStatus{}, ExecutionData: ExecutionData{Deadline: deadline, CommandDuration: time.Second},
	}, 0)
	harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-a"})
	effects := harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-b"})
	confirmation := harness.launchConfirmation(t, effects[0], "workspace-confirm")

	return harness, confirmation, "mutant-a"
}

func TestCampaignRejectsMutationEvidenceWithWrongDeadline(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a"}, 1)
	primary := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	defer func() {
		violation, ok := recover().(runtimeInvariantViolation)
		require.True(t, ok, "recovered=%#v", violation)
		assert.EqualValues(t, "campaign observe terminal", violation.operation, "recovered=%#v", violation)
	}()
	harness.settleAttempt(t, primary, Settled{
		Exit: ExitStatus{}, ExecutionData: ExecutionData{Deadline: time.Nanosecond, CommandDuration: time.Second},
	}, 0)
}

func TestCampaignConfirmedDrainedFatalEpochForcesAbort(t *testing.T) {
	harness := newCampaignHarness(t, []mutantIdentity{"mutant-a"}, AutomaticProfile, 1)
	effect := harness.effects[0]
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: effect.attempt, workspace: "workspace-baseline", snapshot: effect.snapshot,
	})
	admitted := runtimeReplayAdmission(harness.applyRuntime(processruntime.RequestAdmissionCut(processRuntimeAdmission(effects[0].request))))
	effects = harness.advance(admissionGrantedEvent{
		attempt: effect.attempt, grant: campaignAdmissionValue(admitted.deliveries[0]),
	})
	grant := effects[0].grant
	started := runtimeReplayStart(harness.applyRuntime(processruntime.CommitStartCut(processRuntimeAdmission(campaignAdmissionValue(admitted.deliveries[0])))))
	harness.advance(startCommittedEvent{
		attempt: effect.attempt, grant: grant, result: campaignStartEvidence(started),
	})
	receipt := runtimeReplayReceipt(harness.applyRuntime(processruntime.ObserveAttemptCut(started.generation, processRuntimeObservation(launchUnconfirmed{}))))
	harness.advance(attemptLaunchEvent{
		attempt: effect.attempt, generation: started.generation,
		result:  campaignLaunchObservation{kind: campaignLaunchUnconfirmed, residual: ProspectiveUnresolved},
		receipt: campaignReceiptValue(receipt),
	})
	settlement := runtimeReplaySettlement(harness.applyRuntime(processruntime.SettleEmergencyCut(processRuntimeResolutions(emergencySweep{resolutions: []emergencyResolution{{
		generation: started.generation, disposition: emergencyConfirmedDrained,
	}}}))))

	effects = harness.advance(runtimeEmergencySettledEvent{
		epoch: receipt.fatalEpoch, settlement: campaignSettlementValue(settlement),
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-baseline"})
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceSnapshot, identity: "snapshot-a"})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)
	forced := runtimeReplayTerminal(harness.applyRuntime(processruntime.AuthorizeForcedAbortCut(harness.state.runtimeToken, uint64(receipt.fatalEpoch))))
	harness.advance(terminalCommittedEvent{result: campaignTerminalEvidence(forced)})
	{
		_, ok := harness.state.outcome.(abortedOutcome)
		require.True(t, ok, "forced outcome/obligations=%#v/%#v", harness.state.outcome, harness.state.obligations)
		assert.EqualValues(t, 0, len(harness.state.obligations), "forced outcome/obligations=%#v/%#v", harness.state.outcome, harness.state.obligations)
	}
}

func TestCampaignPreparationFailuresAbortWithoutCommands(t *testing.T) {
	definition := campaignDefinition{
		identity: "campaign-a", lineage: 11, command: []string{"test"}, profile: AutomaticProfile, peers: 1,
	}
	state, _ := beginCampaign(definition)
	state, effects := advanceCampaign(state, campaignEvent{id: 1, payload: campaignRegisteredEvent{
		registration: campaignRegistration{decision: processruntime.CampaignRejectedRecursive},
	}})
	assert.EqualValues(t, 0, len(effects), "registration rejection state/effects=%#v/%#v", state, effects)
	assert.EqualValues(t, 0, state.commandCount(), "registration rejection state/effects=%#v/%#v", state, effects)
	{
		_, ok := state.outcome.(abortedOutcome)
		require.True(t, ok, "registration rejection outcome=%#v", state.outcome)
	}

	state, _ = beginCampaign(definition)
	runtime := processruntime.NewReplay(1)
	_, registrationResult := runtime.Apply(processruntime.RegisterCampaignCut(definition.lineage))
	registered := campaignRegistrationEvidence(registrationResult.Registration())
	state, _ = advanceCampaign(state, campaignEvent{id: 1, payload: campaignRegisteredEvent{registration: registered}})
	state, effects = advanceCampaign(state, campaignEvent{id: 2, payload: campaignPreparationFailedEvent{
		stage: campaignPreparingSnapshot, cause: "snapshot failed",
	}})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)
	assert.EqualValues(t, 0, state.commandCount(), "snapshot failure state=%#v", state)
	assert.Equal(t, campaignDrainAbort, state.drain.kind, "snapshot failure state=%#v", state)
}

func TestCampaignAttemptWorkspaceFailureAbortsAndStopsCommittedPeers(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a", "mutant-b"}, 2)
	peer := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	effects := harness.advance(workspaceMaterializationFailedEvent{
		attempt: primaryEffects[1].attempt, cause: "workspace copy failed",
	})
	assertCampaignEffects(t, effects, campaignEffectStopAttempt)
	assert.Equal(t, peer.attempt, effects[0].attempt, "workspace abort state/effects=%#v/%#v", harness.state.drain, effects)
	assert.Equal(t, campaignDrainAbort, harness.state.drain.kind, "workspace abort state/effects=%#v/%#v", harness.state.drain, effects)
}

func TestCampaignAbortCancelsWaitingAdmissionBeforeTerminal(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a", "mutant-b"}, 2)
	peerRegistration := campaignRegistrationEvidence(harness.applyRuntime(processruntime.RegisterCampaignCut(99)).Registration())
	peerRequest := admissionRequest{
		campaign: peerRegistration.token, attempt: "peer:1", class: sharedAdmission,
	}
	peerAdmission := runtimeReplayAdmission(harness.applyRuntime(processruntime.RequestAdmissionCut(processRuntimeAdmission(campaignAdmissionValue(peerRequest)))))
	peerStart := runtimeReplayStart(harness.applyRuntime(processruntime.CommitStartCut(processRuntimeAdmission(campaignAdmissionValue(peerAdmission.deliveries[0])))))
	_ = runtimeReplayReceipt(harness.applyRuntime(processruntime.ObserveAttemptCut(peerStart.generation, processRuntimeObservation(launchOwned{}))))
	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: primaryEffects[1].attempt, workspace: "workspace-b", snapshot: primaryEffects[1].snapshot,
	})
	request := effects[0].request
	waiting := runtimeReplayAdmission(harness.applyRuntime(processruntime.RequestAdmissionCut(processRuntimeAdmission(request))))
	assert.Equal(t, processruntime.AdmissionAccepted, waiting.decision, "second request was not waiting: %#v", waiting)
	assert.EqualValues(t, 0, len(waiting.deliveries), "second request was not waiting: %#v", waiting)
	effects = harness.settleAttempt(t, first, Infrastructure{
		Cause: CensusFailed, Err: errors.New("census failed"),
		ExecutionData: ExecutionData{Deadline: harness.state.mutationDeadline, CommandDuration: time.Second},
	}, 0)
	assertCampaignEffects(t, effects, campaignEffectCancelAdmission, campaignEffectReleaseWorkspace)
	cancelled := runtimeReplayAdmission(harness.applyRuntime(processruntime.CancelAdmissionCut(processRuntimeAdmission(request))))
	assert.False(t, cancelled.decision != processruntime.AdmissionCancelledWaiting && cancelled.decision != processruntime.AdmissionCancelledGranted, "unexpected cancellation=%#v", cancelled)
	effects = harness.advance(admissionCancelledEvent{
		attempt: primaryEffects[1].attempt, request: request, result: campaignAdmissionEvidence(cancelled),
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
}

func TestCampaignSettledArtifactFailureIsUnscoredAbort(t *testing.T) {
	harness := newCampaignHarness(t, nil, AutomaticProfile, 1)
	effects := harness.advance(resourceSettlementFailedEvent{
		kind: campaignResourceSnapshot, identity: "snapshot-a", cause: "snapshot removal failed",
	})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)
	assert.Equal(t, campaignTerminalAborted, harness.state.candidate.kind, "artifact failure state=%#v", harness.state)
	assert.Equal(t, campaignDrainAbort, harness.state.drain.kind, "artifact failure state=%#v", harness.state)
}

func TestCampaignWorkspaceReleaseFailureStopsOwnedPeer(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a", "mutant-b"}, 2)
	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	second := harness.launchMaterialized(t, primaryEffects[1], "workspace-b")
	harness.settleAttempt(t, first, Settled{
		Exit: ExitStatus{}, ExecutionData: ExecutionData{
			Deadline: harness.state.mutationDeadline, CommandDuration: time.Second,
		},
	}, 0)
	effects := harness.advance(resourceSettlementFailedEvent{
		kind: campaignResourceWorkspace, identity: "workspace-a", cause: "workspace removal failed",
	})
	assertCampaignEffects(t, effects, campaignEffectStopAttempt)
	assert.Equal(t, second.attempt, effects[0].attempt, "workspace failure stopped=%s, want %s", effects[0].attempt, second.attempt)
}

func TestCampaignPeerFatalClosureDrainsUncommittedLocalResources(t *testing.T) {
	harness := newCampaignHarness(t, []mutantIdentity{"mutant-a"}, AutomaticProfile, 1)
	closure := runtimeReplayClosure(harness.applyRuntime(processruntime.CloseCut(string("peer fatal"))))
	effects := harness.advance(runtimeEmergencyStartedEvent{closure: campaignClosureValue(closure)})
	require.EqualValues(t, 0, len(effects), "peer emergency state/effects=%#v/%#v", harness.state.drain, effects)
	assert.Equal(t, campaignDrainRuntimeEmergency, harness.state.drain.kind, "peer emergency state/effects=%#v/%#v", harness.state.drain, effects)
	settlement := runtimeReplaySettlement(harness.applyRuntime(processruntime.SettleEmergencyCut(processRuntimeResolutions(emergencySweep{}))))
	harness.advance(runtimeEmergencySettledEvent{
		epoch: closure.epoch, settlement: campaignSettlementValue(settlement),
	})
	effects = harness.advance(workspaceMaterializedEvent{
		attempt: harness.effects[0].attempt, workspace: "workspace-baseline", snapshot: "snapshot-a",
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-baseline"})
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
}

func TestCampaignWithoutElectedResidualOwnershipForceAborts(t *testing.T) {
	harness := newCampaignHarness(t, []mutantIdentity{"mutant-a"}, AutomaticProfile, 1)
	closure := runtimeReplayClosure(harness.applyRuntime(processruntime.CloseCut(string("peer fatal"))))
	harness.advance(runtimeEmergencyStartedEvent{closure: campaignClosureValue(closure)})
	effects := harness.advance(runtimeEmergencySettledEvent{
		epoch: closure.epoch,
		settlement: campaignEmergencySettlement{
			epoch: closure.epoch, owner: campaignTokenForTest(99),
			residual: []campaignResidualCustody{{generation: 91, stage: admissionOwned, transferred: true}},
		},
	})
	require.Nil(t, harness.state.failure, "non-owner settlement failure/effects = %#v/%#v, want deferred forced abort", harness.state.failure, effects)
	assert.EqualValues(t, 0, len(effects), "non-owner settlement failure/effects = %#v/%#v, want deferred forced abort", harness.state.failure, effects)
	effects = harness.advance(workspaceMaterializedEvent{
		attempt: harness.effects[0].attempt, workspace: "workspace-baseline", snapshot: "snapshot-a",
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-baseline"})
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceSnapshot, identity: "snapshot-a"})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)
	assert.Equal(t, closure.epoch, effects[0].fatalEpoch, "forced-abort epoch = %d, want %d", effects[0].fatalEpoch, closure.epoch)
}

func TestCampaignTransfersNonOwnedResidualWithoutDeletingItsWorkspace(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a"}, 1)
	attempt := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	harness.advance(runtimeEmergencyStartedEvent{closure: campaignRuntimeClosure{epoch: 7}})
	effects := harness.advance(runtimeEmergencySettledEvent{
		epoch: 7,
		settlement: campaignEmergencySettlement{
			epoch: 7, owner: campaignTokenForTest(99),
			acknowledged: []attemptGeneration{attempt.generation},
			residual: []campaignResidualCustody{{
				attempt: attempt.attempt, generation: attempt.generation,
				stage: admissionOwned, transferred: true,
			}},
		},
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
	assert.False(t, slices.ContainsFunc(effects, func(effect campaignEffect) bool {
		return effect.kind == campaignEffectReleaseWorkspace
	}), "transferred residual deleted its backing workspace: %#v", effects)
}

func TestCampaignTerminalRejectedByFatalClosureAwaitsForcedAbort(t *testing.T) {
	harness := newCampaignHarness(t, nil, AutomaticProfile, 1)
	effects := harness.advance(resourceSettledEvent{kind: campaignResourceSnapshot, identity: "snapshot-a"})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)
	closure := runtimeReplayClosure(harness.applyRuntime(processruntime.CloseCut(string("terminal race"))))
	rejected := runtimeReplayTerminal(harness.applyRuntime(processruntime.CommitTerminalCut(harness.state.runtimeToken)))
	effects = harness.advance(terminalCommittedEvent{result: campaignTerminalEvidence(rejected)})
	assert.EqualValues(t, 0, len(effects), "terminal race state/effects=%#v/%#v", harness.state.drain, effects)
	assert.Equal(t, campaignDrainRuntimeEmergency, harness.state.drain.kind, "terminal race state/effects=%#v/%#v", harness.state.drain, effects)
	settlement := runtimeReplaySettlement(harness.applyRuntime(processruntime.SettleEmergencyCut(processRuntimeResolutions(emergencySweep{}))))
	effects = harness.advance(runtimeEmergencySettledEvent{
		epoch: closure.epoch, settlement: campaignSettlementValue(settlement),
	})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)
}

func TestCampaignAdmissionClosedByFatalEpochDrainsWithoutLaunching(t *testing.T) {
	harness := newCampaignHarness(t, []mutantIdentity{"mutant-a"}, AutomaticProfile, 1)
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: harness.effects[0].attempt, workspace: "workspace-baseline", snapshot: "snapshot-a",
	})
	request := effects[0].request
	closure := runtimeReplayClosure(harness.applyRuntime(processruntime.CloseCut(string("admission race"))))
	rejected := runtimeReplayAdmission(harness.applyRuntime(processruntime.RequestAdmissionCut(processRuntimeAdmission(request))))
	effects = harness.advance(admissionRejectedEvent{
		attempt: harness.effects[0].attempt, result: campaignAdmissionEvidence(rejected), cause: "runtime closed",
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	assert.Equal(t, campaignDrainRuntimeEmergency, harness.state.drain.kind, "closed admission state=%#v", harness.state)
	assert.Equal(t, closure.epoch, harness.state.drain.epoch, "closed admission state=%#v", harness.state)
	assert.EqualValues(t, 0, harness.state.commandCount(), "closed admission state=%#v", harness.state)
}

func TestCampaignTraceDistinguishesNormalizedTerminalEvidence(t *testing.T) {
	base := attemptTerminalEvent{attempt: "attempt-a", generation: 7, terminal: Settled{
		Exit: ExitStatus{}, ExecutionData: ExecutionData{Deadline: time.Minute, CommandDuration: time.Second},
	}}
	passing := campaignEventSummary(campaignEvent{id: 1, payload: base})
	base.terminal = Settled{
		Exit: ExitStatus{Code: 1}, ExecutionData: ExecutionData{
			Deadline: time.Minute, CommandDuration: time.Second,
		},
	}
	failing := campaignEventSummary(campaignEvent{id: 1, payload: base})
	assert.NotEqual(t, failing, passing, "trace collapsed distinct terminal facts: %q", passing)
}

func TestCampaignTraceDistinguishesReceiptFactsThatChangeAttribution(t *testing.T) {
	event := attemptTerminalEvent{
		attempt: "attempt-a", generation: 7, terminal: Tripped{
			Trip: AutomaticDeadlineTrip{}, ExecutionData: ExecutionData{Deadline: time.Minute},
		},
		receipt: campaignRuntimeReceipt{generation: 7, settlementAcknowledged: true},
	}
	direct := campaignEventSummary(campaignEvent{id: 1, payload: event})
	event.receipt.confirmationProvisional = true
	provisional := campaignEventSummary(campaignEvent{id: 1, payload: event})
	assert.NotEqual(t, provisional, direct, "trace collapsed attribution-changing receipt facts: %q", direct)
}

func TestCampaignRejectsBrokerContradictionAsInvariant(t *testing.T) {
	harness := newCampaignHarness(t, []mutantIdentity{"mutant-a"}, AutomaticProfile, 1)
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: harness.effects[0].attempt, workspace: "workspace-baseline", snapshot: "snapshot-a",
	})
	defer func() {
		violation, ok := recover().(runtimeInvariantViolation)
		require.True(t, ok, "recovered=%#v", violation)
		assert.EqualValues(t, "campaign reject admission", violation.operation, "recovered=%#v", violation)
	}()
	harness.advance(admissionRejectedEvent{
		attempt: harness.effects[0].attempt,
		result:  campaignAdmissionResult{decision: processruntime.AdmissionRejectedDuplicate, request: effects[0].request},
		cause:   "duplicate",
	})
}

func TestCampaignOverlapProvisionalsDrainInCatalogueOrderAndConfirmOnce(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a", "mutant-b"}, 2)
	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	second := harness.launchMaterialized(t, primaryEffects[1], "workspace-b")
	deadline := harness.state.mutationDeadline
	trip := func() Tripped {
		return Tripped{
			Trip:          AutomaticDeadlineTrip{},
			ExecutionData: ExecutionData{Deadline: deadline, CommandDuration: deadline, BoundFired: CommandDeadlineFired},
		}
	}
	firstEffects := harness.settleAttempt(t, first, trip(), 0)
	secondEffects := harness.settleAttempt(t, second, trip(), 0)
	assertCampaignEffects(t, firstEffects, campaignEffectReleaseWorkspace)
	assertCampaignEffects(t, secondEffects, campaignEffectReleaseWorkspace)
	assert.Equal(t, campaignDraining, harness.state.phase, "confirmation drain=%#v", harness.state.drain)
	assert.Equal(t, campaignDrainConfirm, harness.state.drain.kind, "confirmation drain=%#v", harness.state.drain)
	assert.Equal(t, []mutantIdentity{"mutant-a", "mutant-b"}, harness.state.drain.provisionals, "confirmation drain=%#v", harness.state.drain)

	harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-a"})
	effects := harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-b"})
	assertCampaignEffects(t, effects, campaignEffectMaterializeWorkspace)
	assert.EqualValues(t, "mutant-a", effects[0].mutant, "first confirmation=%#v phase=%v", effects[0], harness.state.phase)
	assert.Equal(t, campaignConfirming, harness.state.phase, "first confirmation=%#v phase=%v", effects[0], harness.state.phase)
	confirmationA := harness.launchConfirmation(t, effects[0], "workspace-confirm-a")
	effects = harness.settleConfirmation(t, confirmationA, Settled{
		Exit:          ExitStatus{},
		ExecutionData: ExecutionData{Deadline: deadline, CommandDuration: time.Second},
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	effects = harness.advance(resourceSettledEvent{
		kind: campaignResourceWorkspace, identity: "workspace-confirm-a",
	})
	assertCampaignEffects(t, effects, campaignEffectMaterializeWorkspace)
	assert.EqualValues(t, "mutant-b", effects[0].mutant, "second confirmation=%#v", effects[0])
	confirmationB := harness.launchConfirmation(t, effects[0], "workspace-confirm-b")
	effects = harness.settleConfirmation(t, confirmationB, Tripped{
		Trip:          AutomaticDeadlineTrip{},
		ExecutionData: ExecutionData{Deadline: deadline, CommandDuration: deadline, BoundFired: CommandDeadlineFired},
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	effects = harness.advance(resourceSettledEvent{
		kind: campaignResourceWorkspace, identity: "workspace-confirm-b",
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
	image := harness.runtime.Projection()
	assert.EqualValues(t, 5, harness.state.commandCount(), "command count/single admission=%d/%v", harness.state.commandCount(), image.SingleAdmission())
	assert.True(t, image.SingleAdmission(), "command count/single admission=%d/%v", harness.state.commandCount(), image.SingleAdmission())

	harness.advance(resourceSettledEvent{kind: campaignResourceSnapshot, identity: "snapshot-a"})
	committed := runtimeReplayTerminal(harness.applyRuntime(processruntime.CommitTerminalCut(harness.state.runtimeToken)))
	harness.advance(terminalCommittedEvent{result: campaignTerminalEvidence(committed)})
	completed := harness.state.outcome.(completedOutcome)
	want := []mutantResult{{mutant: "mutant-a", kind: mutantSurvived}, {mutant: "mutant-b", kind: mutantTimedOut}}
	got := make([]mutantResult, len(completed.mutants))
	for index, mutant := range completed.mutants {
		got[index] = mutantResult{mutant: mutant.mutant, kind: mutant.kind}
	}
	assert.Equal(t, want, got, "confirmation outcome=%#v, want %#v", completed.mutants, want)
	for _, mutant := range completed.mutants {
		assert.True(t, mutant.primary.confirmationProvisional, "confirmation outcome discarded provenance: %#v", completed.mutants)
		assert.NotEqual(t, 0, mutant.confirmation.kind, "confirmation outcome discarded provenance: %#v", completed.mutants)
	}
}

func TestCampaignReturnsKnownGrantDeliveredAfterPrimaryClosure(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(
		t, []mutantIdentity{"mutant-a", "mutant-b", "mutant-c"}, 3,
	)
	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	second := harness.launchMaterialized(t, primaryEffects[1], "workspace-b")

	thirdRequestEffects := harness.advance(workspaceMaterializedEvent{
		attempt: primaryEffects[2].attempt, workspace: "workspace-c", snapshot: primaryEffects[2].snapshot,
	})
	thirdAdmission := runtimeReplayAdmission(harness.applyRuntime(processruntime.RequestAdmissionCut(processRuntimeAdmission(
		thirdRequestEffects[0].request))))

	assert.EqualValues(t, 1, len(thirdAdmission.deliveries), "third grant=%#v", thirdAdmission)

	deadline := harness.state.mutationDeadline
	effects := harness.settleAttempt(t, first, Tripped{
		Trip:          AutomaticDeadlineTrip{},
		ExecutionData: ExecutionData{Deadline: deadline, CommandDuration: deadline, BoundFired: CommandDeadlineFired},
	}, 0)
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	effects = harness.advance(admissionGrantedEvent{
		attempt: primaryEffects[2].attempt, grant: campaignAdmissionValue(thirdAdmission.deliveries[0]),
	})
	assertCampaignEffects(t, effects, campaignEffectReturnAdmission)
	assert.Equal(t, campaignAdmissionValue(thirdAdmission.deliveries[0]), effects[0].grant, "late grant return=%#v, want %#v", effects[0].grant, thirdAdmission.deliveries[0])
	returned := runtimeReplayAdmission(harness.applyRuntime(processruntime.ReturnGrantCut(processRuntimeAdmission(campaignAdmissionValue(thirdAdmission.deliveries[0])))))
	effects = harness.advance(grantReturnAcknowledgedEvent{
		grant: effects[0].grant, result: campaignAdmissionEvidence(returned),
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)

	assert.Equal(t, campaignDraining, harness.state.phase, "phase/drain=%v/%#v", harness.state.phase, harness.state.drain)
	assert.Equal(t, campaignDrainConfirm, harness.state.drain.kind, "phase/drain=%v/%#v", harness.state.phase, harness.state.drain)
	effects = harness.settleAttempt(t, second, Settled{
		Exit: ExitStatus{}, ExecutionData: ExecutionData{Deadline: deadline, CommandDuration: time.Second},
	}, 0)
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
}

func TestCampaignBindsFreshBarrierForEachConfirmationClosure(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(
		t, []mutantIdentity{"mutant-a", "mutant-b", "mutant-c", "mutant-d"}, 2,
	)
	deadline := harness.state.mutationDeadline
	trip := Tripped{
		Trip: AutomaticDeadlineTrip{},
		ExecutionData: ExecutionData{
			Deadline: deadline, CommandDuration: deadline, BoundFired: CommandDeadlineFired,
		},
	}
	killed := Settled{
		Exit:          ExitStatus{Code: 1},
		ExecutionData: ExecutionData{Deadline: deadline, CommandDuration: time.Second},
	}

	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	peer := harness.launchMaterialized(t, primaryEffects[1], "workspace-b")
	harness.settleAttempt(t, first, trip, 0)
	harness.settleAttempt(t, peer, killed, 0)
	harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-a"})
	effects := harness.advance(resourceSettledEvent{
		kind: campaignResourceWorkspace, identity: "workspace-b",
	})
	assertCampaignEffects(t, effects, campaignEffectMaterializeWorkspace)
	confirmation := harness.launchConfirmation(t, effects[0], "workspace-confirm-a")
	harness.settleConfirmation(t, confirmation, trip)
	effects = harness.advance(resourceSettledEvent{
		kind: campaignResourceWorkspace, identity: "workspace-confirm-a",
	})
	assertCampaignEffects(t, effects, campaignEffectMaterializeWorkspace, campaignEffectMaterializeWorkspace)

	second := harness.launchMaterialized(t, effects[0], "workspace-c")
	peer = harness.launchMaterialized(t, effects[1], "workspace-d")
	harness.settleAttempt(t, second, trip, 0)
	harness.settleAttempt(t, peer, killed, 0)
	harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-c"})
	effects = harness.advance(resourceSettledEvent{
		kind: campaignResourceWorkspace, identity: "workspace-d",
	})
	assertCampaignEffects(t, effects, campaignEffectMaterializeWorkspace)
	effects = harness.advance(workspaceMaterializedEvent{
		attempt: effects[0].attempt, workspace: "workspace-confirm-c", snapshot: effects[0].snapshot,
	})
	assertCampaignEffects(t, effects, campaignEffectBindConfirmationBarrier)
}

func TestCampaignClearsConfirmationClosureBeforeOrdinaryPrimariesResume(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(
		t, []mutantIdentity{"mutant-a", "mutant-b", "mutant-c", "mutant-d", "mutant-e"}, 2,
	)
	deadline := harness.state.mutationDeadline
	trip := Tripped{
		Trip: AutomaticDeadlineTrip{},
		ExecutionData: ExecutionData{
			Deadline: deadline, CommandDuration: deadline, BoundFired: CommandDeadlineFired,
		},
	}
	killed := Settled{
		Exit:          ExitStatus{Code: 1},
		ExecutionData: ExecutionData{Deadline: deadline, CommandDuration: time.Second},
	}

	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	peer := harness.launchMaterialized(t, primaryEffects[1], "workspace-b")
	harness.settleAttempt(t, first, trip, 0)
	harness.settleAttempt(t, peer, killed, 0)
	harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-a"})
	effects := harness.advance(resourceSettledEvent{
		kind: campaignResourceWorkspace, identity: "workspace-b",
	})
	confirmation := harness.launchConfirmation(t, effects[0], "workspace-confirm-a")
	harness.settleConfirmation(t, confirmation, trip)
	effects = harness.advance(resourceSettledEvent{
		kind: campaignResourceWorkspace, identity: "workspace-confirm-a",
	})
	assertCampaignEffects(t, effects, campaignEffectMaterializeWorkspace, campaignEffectMaterializeWorkspace)
	third := harness.launchMaterialized(t, effects[0], "workspace-c")
	fourth := harness.launchMaterialized(t, effects[1], "workspace-d")
	harness.settleAttempt(t, third, killed, 0)
	harness.settleAttempt(t, fourth, killed, 0)
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-c"})
	assertCampaignEffects(t, effects, campaignEffectMaterializeWorkspace)
	effects = harness.advance(resourceSettledEvent{
		kind: campaignResourceWorkspace, identity: "workspace-d",
	})
	assert.EqualValues(t, 0, len(effects), "second resumed workspace settlement effects=%#v, want none", effects)
}

func TestCampaignAdoptsConfirmationClosureBeforeCausativeTerminalDelivery(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(
		t, []mutantIdentity{"mutant-a", "mutant-b", "mutant-c"}, 3,
	)
	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	harness.launchMaterialized(t, primaryEffects[1], "workspace-b")

	deadline := harness.state.mutationDeadline
	receipt := runtimeReplayReceipt(harness.applyRuntime(processruntime.ObserveAttemptCut(first.generation, processRuntimeObservation(attemptTripped{
		kind: deadlineTrip, profile: AutomaticProfile, deadline: deadline,
	}))))

	assert.True(t, receipt.confirmationProvisional, "deadline receipt=%#v", receipt)

	effects := harness.advance(workspaceMaterializedEvent{
		attempt: primaryEffects[2].attempt, workspace: "workspace-c", snapshot: primaryEffects[2].snapshot,
	})
	rejected := runtimeReplayAdmission(harness.applyRuntime(processruntime.RequestAdmissionCut(processRuntimeAdmission(effects[0].request))))
	assert.Equal(t, processruntime.AdmissionRejectedGateClosed, rejected.decision, "pending primary admission=%#v", rejected)
	effects = harness.advance(admissionRejectedEvent{
		attempt: primaryEffects[2].attempt,
		result:  campaignAdmissionEvidence(rejected),
		cause:   "primary gate closed",
	})
	assert.Equal(t, campaignDraining, harness.state.phase, "phase/drain=%v/%#v", harness.state.phase, harness.state.drain)
	assert.Equal(t, campaignDrainConfirm, harness.state.drain.kind, "phase/drain=%v/%#v", harness.state.phase, harness.state.drain)
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	effects = harness.advance(resourceSettledEvent{
		kind: campaignResourceWorkspace, identity: "workspace-c",
	})
	assert.EqualValues(t, 0, len(effects), "rejected workspace settlement effects=%#v, want drain pause", effects)

	effects = harness.advance(attemptTerminalEvent{
		attempt: first.attempt, generation: first.generation,
		terminal: Tripped{
			Trip: AutomaticDeadlineTrip{},
			ExecutionData: ExecutionData{
				Deadline: deadline, CommandDuration: deadline, BoundFired: CommandDeadlineFired,
			},
		},
		receipt: campaignReceiptValue(receipt),
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	assert.Equal(t, []mutantIdentity{"mutant-a"}, harness.state.drain.provisionals, "confirmation provisionals=%#v", harness.state.drain.provisionals)
}

func TestCampaignAdoptsStartCommittedBeforeClosureWhoseFactArrivesAfterClosure(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a", "mutant-b"}, 2)
	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")

	effects := harness.advance(workspaceMaterializedEvent{
		attempt: primaryEffects[1].attempt, workspace: "workspace-b", snapshot: primaryEffects[1].snapshot,
	})
	admitted := runtimeReplayAdmission(harness.applyRuntime(processruntime.RequestAdmissionCut(processRuntimeAdmission(effects[0].request))))
	effects = harness.advance(admissionGrantedEvent{
		attempt: primaryEffects[1].attempt, grant: campaignAdmissionValue(admitted.deliveries[0]),
	})
	grant := effects[0].grant
	delayed := runtimeReplayStart(harness.applyRuntime(processruntime.CommitStartCut(processRuntimeAdmission(campaignAdmissionValue(admitted.deliveries[0])))))

	deadline := harness.state.mutationDeadline
	harness.settleAttempt(t, first, Tripped{
		Trip:          AutomaticDeadlineTrip{},
		ExecutionData: ExecutionData{Deadline: deadline, CommandDuration: deadline, BoundFired: CommandDeadlineFired},
	}, 0)
	assert.Equal(t, campaignDraining, harness.state.phase, "phase=%v, want draining before delayed committed fact", harness.state.phase)

	effects = harness.advance(startCommittedEvent{
		attempt: primaryEffects[1].attempt, grant: grant, result: campaignStartEvidence(delayed),
	})
	assertCampaignEffects(t, effects, campaignEffectLaunchAttempt)
	assert.Equal(t, delayed.generation, effects[0].generation, "adopted generation=%d, want %d", effects[0].generation, delayed.generation)
	receipt := runtimeReplayReceipt(harness.applyRuntime(processruntime.ObserveAttemptCut(delayed.generation, processRuntimeObservation(launchOwned{}))))
	effects = harness.advance(attemptLaunchEvent{
		attempt: primaryEffects[1].attempt, generation: delayed.generation,
		result: campaignLaunchObservation{kind: campaignLaunchOwned}, receipt: campaignReceiptValue(receipt),
	})
	assert.EqualValues(t, 0, len(effects), "adopted pre-closure start emitted synthetic work: %#v", effects)
}

func TestCampaignSettlesLateProvenNoReleaseDuringRuntimeEmergency(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a", "mutant-b"}, 2)
	prospective := make([]launchedCampaignAttempt, 0, len(primaryEffects))
	for index, primary := range primaryEffects {
		prospective = append(prospective, harness.startProspective(
			t, primary, "workspace-"+strconv.Itoa(index+1),
		))
	}

	unconfirmed := runtimeReplayReceipt(harness.applyRuntime(processruntime.ObserveAttemptCut(prospective[0].generation, processRuntimeObservation(launchUnconfirmed{}))))
	harness.advance(attemptLaunchEvent{
		attempt: prospective[0].attempt, generation: prospective[0].generation,
		result:  campaignLaunchObservation{kind: campaignLaunchUnconfirmed, residual: ProspectiveUnresolved},
		receipt: campaignReceiptValue(unconfirmed),
	})
	wantDrain := harness.state.drain

	notReleased := runtimeReplayReceipt(harness.applyRuntime(processruntime.ObserveAttemptCut(
		prospective[1].generation, processRuntimeObservation(
			launchNotReleased{reason: launchFailed}))))

	effects := harness.advance(attemptLaunchEvent{
		attempt: prospective[1].attempt, generation: prospective[1].generation,
		result:  campaignLaunchObservation{kind: campaignLaunchNotReleased, failure: LaunchFailed},
		receipt: campaignReceiptValue(notReleased),
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	attemptAt := harness.state.attemptIndex(prospective[1].attempt)
	require.False(t, attemptAt < 0, "late no-release state=%#v, want settled peer and unchanged drain %#v", harness.state, wantDrain)
	assert.Equal(t, campaignAttemptSettled, harness.state.attempts[attemptAt].stage, "late no-release state=%#v, want settled peer and unchanged drain %#v", harness.state, wantDrain)
	assert.Equal(t, wantDrain, harness.state.drain, "late no-release state=%#v, want settled peer and unchanged drain %#v", harness.state, wantDrain)
}

func TestCampaignPreservesRuntimeEmergencyForPreClosureNoReleaseDeliveredLate(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a", "mutant-b"}, 2)
	first := harness.startProspective(t, primaryEffects[0], "workspace-a")
	second := harness.startProspective(t, primaryEffects[1], "workspace-b")

	notReleased := runtimeReplayReceipt(harness.applyRuntime(processruntime.ObserveAttemptCut(
		second.generation, processRuntimeObservation(
			launchNotReleased{reason: launchFailed}))))

	assert.False(t, notReleased.runtimeClosureInProgress, "pre-closure no-release receipt=%#v", notReleased)
	unconfirmed := runtimeReplayReceipt(harness.applyRuntime(processruntime.ObserveAttemptCut(first.generation, processRuntimeObservation(launchUnconfirmed{}))))
	harness.advance(attemptLaunchEvent{
		attempt: first.attempt, generation: first.generation,
		result:  campaignLaunchObservation{kind: campaignLaunchUnconfirmed, residual: ProspectiveUnresolved},
		receipt: campaignReceiptValue(unconfirmed),
	})
	wantDrain := harness.state.drain

	effects := harness.advance(attemptLaunchEvent{
		attempt: second.attempt, generation: second.generation,
		result:  campaignLaunchObservation{kind: campaignLaunchNotReleased, failure: LaunchFailed},
		receipt: campaignReceiptValue(notReleased),
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	assert.Equal(t, wantDrain, harness.state.drain, "delayed pre-closure no-release drain=%#v, want runtime emergency %#v", harness.state.drain, wantDrain)
}

func TestCampaignPreservesRuntimeEmergencyForPreClosureProvisionalDeliveredLate(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a", "mutant-b"}, 2)
	first := harness.startProspective(t, primaryEffects[0], "workspace-a")
	second := harness.startProspective(t, primaryEffects[1], "workspace-b")
	owned := runtimeReplayReceipt(harness.applyRuntime(processruntime.ObserveAttemptCut(first.generation, processRuntimeObservation(launchOwned{}))))
	harness.advance(attemptLaunchEvent{
		attempt: first.attempt, generation: first.generation,
		result: campaignLaunchObservation{kind: campaignLaunchOwned}, receipt: campaignReceiptValue(owned),
	})

	deadline := harness.state.mutationDeadline
	terminal := runtimeReplayReceipt(harness.applyRuntime(processruntime.ObserveAttemptCut(first.generation, processRuntimeObservation(attemptTripped{
		kind: deadlineTrip, profile: AutomaticProfile, deadline: deadline,
	}))))

	assert.True(t, terminal.confirmationProvisional, "pre-closure provisional receipt=%#v", terminal)
	assert.False(t, terminal.runtimeClosureInProgress, "pre-closure provisional receipt=%#v", terminal)
	unconfirmed := runtimeReplayReceipt(harness.applyRuntime(processruntime.ObserveAttemptCut(second.generation, processRuntimeObservation(launchUnconfirmed{}))))
	harness.advance(attemptLaunchEvent{
		attempt: second.attempt, generation: second.generation,
		result:  campaignLaunchObservation{kind: campaignLaunchUnconfirmed, residual: ProspectiveUnresolved},
		receipt: campaignReceiptValue(unconfirmed),
	})
	wantDrain := harness.state.drain

	effects := harness.advance(attemptTerminalEvent{
		attempt: first.attempt, generation: first.generation,
		terminal: Tripped{
			Trip: AutomaticDeadlineTrip{},
			ExecutionData: ExecutionData{
				Deadline: deadline, CommandDuration: deadline, BoundFired: CommandDeadlineFired,
			},
		},
		receipt: campaignReceiptValue(terminal),
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	assert.Equal(t, wantDrain, harness.state.drain, "delayed pre-closure provisional drain=%#v, want runtime emergency %#v", harness.state.drain, wantDrain)
}

func TestCampaignAbortDrainSurvivesLateProvisionalTerminal(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a", "mutant-b"}, 2)
	owned := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	prospective := harness.startProspective(t, primaryEffects[1], "workspace-b")

	notReleased := runtimeReplayReceipt(harness.applyRuntime(processruntime.ObserveAttemptCut(
		prospective.generation, processRuntimeObservation(
			launchNotReleased{reason: launchFailed}))))

	effects := harness.advance(attemptLaunchEvent{
		attempt: prospective.attempt, generation: prospective.generation,
		result:  campaignLaunchObservation{kind: campaignLaunchNotReleased, failure: LaunchFailed},
		receipt: campaignReceiptValue(notReleased),
	})
	assertCampaignEffects(t, effects, campaignEffectStopAttempt, campaignEffectReleaseWorkspace)
	wantDrain := harness.state.drain

	deadline := harness.state.mutationDeadline
	harness.settleAttempt(t, owned, Tripped{
		Trip: AutomaticDeadlineTrip{},
		ExecutionData: ExecutionData{
			Deadline: deadline, CommandDuration: deadline, BoundFired: CommandDeadlineFired,
		},
	}, 0)
	assert.Equal(t, wantDrain, harness.state.drain, "late provisional terminal drain=%#v, want abort drain %#v", harness.state.drain, wantDrain)
	assert.Equal(t, campaignDraining, harness.state.phase, "late provisional terminal drain=%#v, want abort drain %#v", harness.state.drain, wantDrain)
}

func TestCampaignLateGrantDuringAbortAwaitsQueuedCancellation(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a", "mutant-b"}, 2)
	prospective := harness.startProspective(t, primaryEffects[0], "workspace-a")
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: primaryEffects[1].attempt, workspace: "workspace-b", snapshot: primaryEffects[1].snapshot,
	})
	request := effects[0].request
	admitted := runtimeReplayAdmission(harness.applyRuntime(processruntime.RequestAdmissionCut(processRuntimeAdmission(request))))
	grant := campaignAdmissionValue(admitted.deliveries[0])

	notReleased := runtimeReplayReceipt(harness.applyRuntime(processruntime.ObserveAttemptCut(
		prospective.generation, processRuntimeObservation(
			launchNotReleased{reason: launchFailed}))))

	effects = harness.advance(attemptLaunchEvent{
		attempt: prospective.attempt, generation: prospective.generation,
		result:  campaignLaunchObservation{kind: campaignLaunchNotReleased, failure: LaunchFailed},
		receipt: campaignReceiptValue(notReleased),
	})
	assertCampaignEffects(t, effects, campaignEffectCancelAdmission, campaignEffectReleaseWorkspace)
	wantDrain := harness.state.drain

	effects = harness.advance(admissionGrantedEvent{attempt: primaryEffects[1].attempt, grant: grant})
	assert.EqualValues(t, 0, len(effects), "late abort grant effects=%#v, want queued cancellation only", effects)
	attemptAt := harness.state.attemptIndex(primaryEffects[1].attempt)
	require.False(t, attemptAt < 0, "late abort grant state=%#v, want cancelling attempt under drain %#v", harness.state, wantDrain)
	assert.Equal(t, campaignAttemptAdmissionWaiting, harness.state.attempts[attemptAt].stage, "late abort grant state=%#v, want cancelling attempt under drain %#v", harness.state, wantDrain)
	assert.Equal(t, wantDrain, harness.state.drain, "late abort grant state=%#v, want cancelling attempt under drain %#v", harness.state, wantDrain)
}

func TestCampaignCleanupUnconfirmedRequiresStaticNonEmptyResidual(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a"}, 1)
	primary := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	effects := harness.settleAttempt(t, primary, DrainUnconfirmed{
		Residual:      OwnedUndrained,
		ExecutionData: ExecutionData{Deadline: harness.state.mutationDeadline, CommandDuration: time.Second},
	}, 0)
	assert.EqualValues(t, 0, len(effects), "runtime emergency state/effects=%#v/%#v", harness.state, effects)
	assert.Equal(t, campaignDraining, harness.state.phase, "runtime emergency state/effects=%#v/%#v", harness.state, effects)
	assert.Equal(t, campaignDrainRuntimeEmergency, harness.state.drain.kind, "runtime emergency state/effects=%#v/%#v", harness.state, effects)
	assert.NotEqual(t, 0, harness.state.drain.epoch, "runtime emergency state/effects=%#v/%#v", harness.state, effects)

	settlement := runtimeReplaySettlement(harness.applyRuntime(processruntime.SettleEmergencyCut(processRuntimeResolutions(emergencySweep{resolutions: []emergencyResolution{{
		generation: primary.generation, disposition: emergencyCustodyTransferred,
	}}}))))

	harness.advance(runtimeEmergencySettledEvent{
		epoch: harness.state.drain.epoch, settlement: campaignSettlementValue(settlement),
	})
	failure, ok := harness.state.failure.(cleanupUnconfirmedFault)
	require.True(t, ok, "cleanup failure/ledger=%#v/%#v", harness.state.failure, harness.state.obligations)
	assert.Equal(t, primary.generation, failure.residual.head.generation, "cleanup failure/ledger=%#v/%#v", harness.state.failure, harness.state.obligations)
	assert.NotEqual(t, 0, len(harness.state.obligations), "cleanup failure/ledger=%#v/%#v", harness.state.failure, harness.state.obligations)
}

func TestCampaignMalformedEventEmergencyCleansRuntimeAndRepanicsOriginalViolation(t *testing.T) {
	definition := campaignDefinition{
		identity: "campaign-a", lineage: 11, command: []string{"test"}, profile: AutomaticProfile, peers: 1,
	}
	state, _ := beginCampaign(definition)
	runtime := processruntime.NewReplay(1)
	runtime, registrationResult := runtime.Apply(processruntime.RegisterCampaignCut(definition.lineage))
	registered := campaignRegistrationEvidence(registrationResult.Registration())
	state, _ = advanceCampaign(state, campaignEvent{
		id: 1, payload: campaignRegisteredEvent{registration: registered},
	})

	malformed := campaignEvent{id: 2, payload: snapshotEstablishedEvent{}}
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _ = advanceCampaignGuarded(&runtime, state, malformed, func(closure runtimeClosure) emergencySweep {
			assert.NotEqual(t, 0, closure.epoch, "invariant closure=%#v", closure)
			assert.EqualValues(t, 0, len(closure.residual), "invariant closure=%#v", closure)

			return emergencySweep{}
		})
	}()
	violation, ok := recovered.(runtimeInvariantViolation)
	require.True(t, ok, "recovered=%#v", recovered)
	assert.EqualValues(t, "campaign establish snapshot", violation.operation, "recovered=%#v", recovered)
	assert.EqualValues(t, "snapshot observation is invalid", violation.reason, "recovered=%#v", recovered)
	assert.Equal(t, uint8(campaignPreparing), violation.phase, "invariant diagnostic is incomplete: %#v", violation)
	assert.NotEqual(t, "", violation.rejectedEvent, "invariant diagnostic is incomplete: %#v", violation)
	assert.NotEqual(t, 0, len(violation.stableIdentities), "invariant diagnostic is incomplete: %#v", violation)
	assert.NotEqual(t, 0, len(violation.obligationSnapshot), "invariant diagnostic is incomplete: %#v", violation)
	assert.NotEqual(t, 0, len(violation.traceTail), "invariant diagnostic is incomplete: %#v", violation)
	image := runtime.Projection()
	assert.True(t, image.Drained(), "runtime after invariant=%#v", image)
	assert.NotEqual(t, 0, image.FatalEpoch(), "runtime after invariant=%#v", image)
	assert.EqualValues(t, 1, image.FatalCauseCount(), "runtime after invariant=%#v", image)
}

func TestCampaignInvariantProjectionOmitsPrivateCustodyAndFilesystemFacts(t *testing.T) {
	state := campaignState{
		definition:   campaignDefinition{identity: "campaign-a"},
		snapshot:     "/private/snapshot",
		runtimeToken: campaignTokenForTest(8888),
		drain:        campaignDrainIntent{epoch: 9999},
		attempts: []campaignAttempt{{
			identity: "campaign-a:2", generation: 7777, workspace: "/private/workspace",
		}},
		obligations: []campaignObligation{{
			kind: campaignResourceWorkspace, identity: "/private/workspace",
			attempt: "campaign-a:2", generation: 7777,
		}},
	}
	event := campaignEvent{id: 4, payload: resourceSettlementFailedEvent{
		kind: campaignResourceWorkspace, identity: "/private/workspace", cause: "private cause",
	}}
	projected := strings.Join(append(
		[]string{campaignEventSummary(event)},
		append(state.stableIdentitySnapshot(event), state.obligationSnapshot()...)...,
	), "\n")

	for _, public := range []string{
		"event=4 kind=resource settlement failed resource=workspace",
		"campaign=campaign-a", "attempt=campaign-a:2", "workspace/attempt=campaign-a:2",
	} {
		assert.Contains(t, projected, public, "safe invariant projection missing %q:\n%s", public, projected)
	}
	for _, private := range []string{"/private", "7777", "8888", "9999", "private cause", "generation", "token"} {
		assert.NotContains(t, projected, private, "safe invariant projection leaked %q:\n%s", private, projected)
	}
}

func TestCampaignAdvanceKeepsEarlierStateImmutable(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a"}, 1)
	before := harness.state
	want := before.clone()
	harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	assert.Equal(t, want, before, "earlier campaign state was mutated:\n got=%#v\nwant=%#v", before, want)
}

func FuzzCampaignDeterministicSerialTraceCompletesWithinCommandBound(f *testing.F) {
	f.Add([]byte{0})
	f.Add([]byte{1, 2, 0, 2})
	f.Fuzz(func(t *testing.T, choices []byte) {
		if len(choices) == 0 {
			choices = []byte{0}
		}
		if len(choices) > 6 {
			choices = choices[:6]
		}
		first := runSerialCampaignChoices(t, choices)
		second := runSerialCampaignChoices(t, append([]byte(nil), choices...))
		assert.Equal(t, second, first, "same definition/choices diverged:\nfirst=%#v\nsecond=%#v", first, second)
		assert.Equal(t, len(choices)+1, first.commandCount(), "command count=%d, want %d", first.commandCount(), len(choices)+1)
		{
			_, ok := first.outcome.(completedOutcome)
			require.True(t, ok, "serial trace did not cooperatively settle: %#v", first)
			assert.EqualValues(t, 0, len(first.obligations), "serial trace did not cooperatively settle: %#v", first)
		}
		for index, record := range first.trace {
			assert.Equal(t, campaignEventID(index+1), record.id, "trace identity[%d]=%d", index, record.id)
		}
	})
}

func FuzzCampaignPeerEmergencyMaterializationSettlesWithinEffectBound(f *testing.F) {
	f.Add(byte(1))
	f.Add(byte(7))
	f.Fuzz(func(t *testing.T, size byte) {
		count := int(size%6) + 1
		first := runMaterializingPeerEmergency(t, count)
		second := runMaterializingPeerEmergency(t, count)
		assert.Equal(t, second, first, "peer emergency replay diverged:\nfirst=%#v\nsecond=%#v", first, second)
		{
			_, ok := first.outcome.(abortedOutcome)
			require.True(t, ok, "peer emergency did not cooperatively settle: %#v", first)
			assert.EqualValues(t, 0, first.commandCount(), "peer emergency did not cooperatively settle: %#v", first)
			assert.EqualValues(t, 0, len(first.obligations), "peer emergency did not cooperatively settle: %#v", first)
			assert.False(t, len(first.trace) > 9, "peer emergency did not cooperatively settle: %#v", first)
		}
	})
}

func runMaterializingPeerEmergency(t *testing.T, count int) campaignState {
	t.Helper()
	mutants := make([]mutantIdentity, count)
	for index := range mutants {
		mutants[index] = mutantIdentity("mutant-" + strconv.Itoa(index+1))
	}
	harness := newCampaignHarness(t, mutants, AutomaticProfile, 2)
	baseline := harness.effects[0]
	closure := runtimeReplayClosure(harness.applyRuntime(processruntime.CloseCut(string("peer fatal"))))
	harness.advance(runtimeEmergencyStartedEvent{closure: campaignClosureValue(closure)})
	settlement := runtimeReplaySettlement(harness.applyRuntime(processruntime.SettleEmergencyCut(processRuntimeResolutions(emergencySweep{}))))
	harness.advance(runtimeEmergencySettledEvent{
		epoch: closure.epoch, settlement: campaignSettlementValue(settlement),
	})
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: baseline.attempt, workspace: "workspace-baseline", snapshot: baseline.snapshot,
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	effects = harness.advance(resourceSettledEvent{
		kind: campaignResourceWorkspace, identity: "workspace-baseline",
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceSnapshot, identity: "snapshot-a"})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)
	forced := runtimeReplayTerminal(harness.applyRuntime(processruntime.AuthorizeForcedAbortCut(harness.state.runtimeToken, uint64(closure.epoch))))
	harness.advance(terminalCommittedEvent{result: campaignTerminalEvidence(forced)})

	return harness.state
}

func FuzzCampaignMalformedReplayHasDeterministicInvariant(f *testing.F) {
	f.Add(byte(0))
	f.Add(byte(255))
	f.Fuzz(func(t *testing.T, identity byte) {
		run := func() (violation runtimeInvariantViolation) {
			definition := campaignDefinition{
				identity: "campaign-a", lineage: 11, command: []string{"test"},
				profile: AutomaticProfile, peers: 1,
			}
			state, _ := beginCampaign(definition)
			defer func() {
				violation, _ = recover().(runtimeInvariantViolation)
			}()
			_, _ = advanceCampaign(state, campaignEvent{
				id: campaignEventID(identity), payload: snapshotEstablishedEvent{},
			})

			return violation
		}
		first, second := run(), run()
		assert.Equal(t, second, first, "malformed replay violation diverged: %#v / %#v", first, second)
		assert.NotEqual(t, "", first.operation, "malformed replay violation diverged: %#v / %#v", first, second)
	})
}

func runSerialCampaignChoices(t *testing.T, choices []byte) campaignState {
	t.Helper()
	mutants := make([]mutantIdentity, len(choices))
	for index := range mutants {
		mutants[index] = mutantIdentity("mutant-" + strconv.Itoa(index+1))
	}
	harness := newCampaignHarness(t, mutants, SerialProfile, 3)
	baseline := harness.launchMaterialized(t, harness.effects[0], "workspace-baseline")
	effects := harness.settleAttempt(t, baseline, Settled{
		Exit:          ExitStatus{},
		ExecutionData: ExecutionData{Deadline: 10 * time.Minute, CommandDuration: 2 * time.Second},
	}, resolveMutationDeadline(2*time.Second, 3))
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	effects = harness.advance(resourceSettledEvent{
		kind: campaignResourceWorkspace, identity: "workspace-baseline",
	})
	for index, choice := range choices {
		require.EqualValues(t, 1, len(effects), "primary %d materialization=%#v", index, effects)
		assert.Equal(t, campaignEffectMaterializeWorkspace, effects[0].kind, "primary %d materialization=%#v", index, effects)
		workspace := "workspace-" + strconv.Itoa(index+1)
		attempt := harness.launchMaterialized(t, effects[0], workspace)
		var terminal Terminal
		switch choice % 3 {
		case 0:
			terminal = Settled{
				Exit:          ExitStatus{},
				ExecutionData: ExecutionData{Deadline: harness.state.mutationDeadline, CommandDuration: time.Second},
			}
		case 1:
			terminal = Settled{
				Exit:          ExitStatus{Code: 1},
				ExecutionData: ExecutionData{Deadline: harness.state.mutationDeadline, CommandDuration: time.Second},
			}
		default:
			terminal = Tripped{
				Trip: SerialDeadlineTrip{},
				ExecutionData: ExecutionData{
					Deadline: harness.state.mutationDeadline, CommandDuration: harness.state.mutationDeadline,
					BoundFired: CommandDeadlineFired,
				},
			}
		}
		effects = harness.settleAttempt(t, attempt, terminal, 0)
		assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
		effects = harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: workspace})
	}
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceSnapshot, identity: "snapshot-a"})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)
	committed := runtimeReplayTerminal(harness.applyRuntime(processruntime.CommitTerminalCut(harness.state.runtimeToken)))
	harness.advance(terminalCommittedEvent{result: campaignTerminalEvidence(committed)})

	return harness.state
}

type campaignHarness struct {
	state      campaignState
	runtime    processruntime.Replay
	admissions map[attemptGeneration]admissionAuthority
	nextEvent  campaignEventID
	effects    []campaignEffect
}

func (harness *campaignHarness) admissionByGeneration(generation attemptGeneration) (admissionAuthority, bool) {
	admission, found := harness.admissions[generation]
	return admission, found
}

func (harness *campaignHarness) applyRuntime(cut processruntime.Cut) processruntime.ReplayResult {
	var result processruntime.ReplayResult
	harness.runtime, result = harness.runtime.Apply(cut)
	return result
}

func runtimeReplayAdmission(result processruntime.ReplayResult) admissionResult {
	return runtimeAdmissionResult(result.Admission())
}

func runtimeReplayQueue(result processruntime.ReplayResult) confirmationQueueResult {
	return runtimeQueueResult(result.Queue())
}

func runtimeReplayStart(result processruntime.ReplayResult) startCommittedResult {
	return runtimeStartResult(result.Start())
}

func runtimeReplayTerminal(result processruntime.ReplayResult) terminalResult {
	return runtimeTerminalResult(result.Terminal())
}

func runtimeReplayReceipt(result processruntime.ReplayResult) observationResult {
	return runtimeReceipt(result.Receipt())
}

func runtimeReplayClosure(result processruntime.ReplayResult) runtimeClosure {
	return runtimeClosureValue(result.Closure())
}

func runtimeReplaySettlement(result processruntime.ReplayResult) emergencySettlement {
	return runtimeEmergencySettlement(result.Settlement())
}

type launchedCampaignAttempt struct {
	attempt    attemptIdentity
	generation attemptGeneration
}

func newCampaignHarness(
	t *testing.T,
	mutants []mutantIdentity,
	profile Profile,
	peers int,
) *campaignHarness {
	t.Helper()
	definition := campaignDefinition{
		identity: "campaign-a", lineage: 11, command: []string{"go", "test"}, profile: profile, peers: peers,
	}
	state, _ := beginCampaign(definition)
	runtime := processruntime.NewReplay(peers)
	runtime, registeredResult := runtime.Apply(processruntime.RegisterCampaignCut(definition.lineage))
	registered := campaignRegistrationEvidence(registeredResult.Registration())
	harness := &campaignHarness{state: state, runtime: runtime, admissions: make(map[attemptGeneration]admissionAuthority)}
	harness.advance(campaignRegisteredEvent{registration: registered})
	harness.advance(snapshotEstablishedEvent{snapshot: "snapshot-a"})
	harness.effects = harness.advance(catalogueDiscoveredEvent{snapshot: "snapshot-a", mutants: mutants})

	return harness
}

func newRunningCampaignHarness(
	t *testing.T,
	mutants []mutantIdentity,
	peers int,
) (*campaignHarness, []campaignEffect) {
	t.Helper()
	harness := newCampaignHarness(t, mutants, AutomaticProfile, peers)
	baseline := harness.launchMaterialized(t, harness.effects[0], "workspace-baseline")
	effects := harness.settleAttempt(t, baseline, Settled{
		Exit:          ExitStatus{},
		ExecutionData: ExecutionData{Deadline: 10 * time.Minute, CommandDuration: 2 * time.Second},
	}, resolveMutationDeadline(2*time.Second, peers))
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)

	return harness, harness.advance(resourceSettledEvent{
		kind: campaignResourceWorkspace, identity: "workspace-baseline",
	})
}

func (harness *campaignHarness) advance(payload campaignEventPayload) []campaignEffect {
	harness.nextEvent++
	var effects []campaignEffect
	harness.state, effects = advanceCampaign(harness.state, campaignEvent{id: harness.nextEvent, payload: payload})

	return effects
}

func (harness *campaignHarness) launchMaterialized(
	t *testing.T,
	effect campaignEffect,
	workspace string,
) launchedCampaignAttempt {
	t.Helper()
	started := harness.startProspective(t, effect, workspace)
	receipt := runtimeReplayReceipt(harness.applyRuntime(processruntime.ObserveAttemptCut(started.generation, processruntime.Owned())))
	harness.advance(attemptLaunchEvent{
		attempt: effect.attempt, generation: started.generation,
		result: campaignLaunchObservation{kind: campaignLaunchOwned}, receipt: campaignReceiptValue(receipt),
	})

	return started
}

func (harness *campaignHarness) startProspective(
	t *testing.T,
	effect campaignEffect,
	workspace string,
) launchedCampaignAttempt {
	t.Helper()
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: effect.attempt, workspace: workspace, snapshot: effect.snapshot,
	})
	admitted := runtimeReplayAdmission(harness.applyRuntime(processruntime.RequestAdmissionCut(
		processRuntimeAdmission(effects[0].request),
	)))

	effects = harness.advance(admissionGrantedEvent{
		attempt: effect.attempt, grant: campaignAdmissionValue(admitted.deliveries[0]),
	})
	grant := effects[0].grant
	started := runtimeReplayStart(harness.applyRuntime(processruntime.CommitStartCut(
		processRuntimeAdmission(campaignAdmissionValue(admitted.deliveries[0])),
	)))

	harness.admissions[started.generation] = admitted.deliveries[0]
	harness.advance(startCommittedEvent{
		attempt: effect.attempt, grant: grant, result: campaignStartEvidence(started),
	})

	return launchedCampaignAttempt{attempt: effect.attempt, generation: started.generation}
}

func (harness *campaignHarness) settleAttempt(
	t *testing.T,
	attempt launchedCampaignAttempt,
	terminal Terminal,
	resolved time.Duration,
) []campaignEffect {
	t.Helper()
	grant, found := harness.admissionByGeneration(attempt.generation)
	require.True(t, found, "attempt generation is not live")
	var observation attemptObservation
	switch observed := terminal.(type) {
	case Settled:
		observation = attemptSettled{profile: grant.profile, deadline: grant.deadline}
	case Tripped:
		switch observed.Trip.(type) {
		case FuseTrip:
			observation = attemptTripped{kind: fuseTrip}
		default:
			observation = attemptTripped{kind: deadlineTrip, profile: grant.profile, deadline: grant.deadline}
		}
	case Stopped:
		observation = attemptStopped{}
	case Infrastructure:
		observation = attemptInfrastructure{cause: "campaign fixture"}
	case DrainUnconfirmed:
		observation = drainUnconfirmed{}
	default:
		require.FailNowf(t, "unsupported fixture terminal", "terminal: %#v", terminal)
	}
	receipt := runtimeReplayReceipt(harness.applyRuntime(processruntime.ObserveAttemptCut(
		attempt.generation, processRuntimeObservation(observation),
	)))

	delete(harness.admissions, attempt.generation)

	event := attemptTerminalEvent{
		attempt: attempt.attempt, generation: attempt.generation, terminal: terminal,
		receipt: campaignReceiptValue(receipt),
	}
	if resolved > 0 {
		event.resolvedMutationDeadline = recordedMutationDeadline(resolved)
	}

	return harness.advance(event)
}

func (harness *campaignHarness) launchConfirmation(
	t *testing.T,
	effect campaignEffect,
	workspace string,
) launchedCampaignAttempt {
	t.Helper()
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: effect.attempt, workspace: workspace, snapshot: effect.snapshot,
	})
	if effects[0].kind == campaignEffectBindConfirmationBarrier {
		binding := runtimeBarrierBinding(effects[0].binding)
		bound := runtimeBarrierResult(harness.applyRuntime(processruntime.BindConfirmationBarrierCut(processruntime.Barrier{
			Campaign: binding.campaign, Attempt: string(binding.attempt), Profile: binding.profile, Deadline: binding.deadline,
		})).Barrier())
		effects = harness.advance(confirmationBarrierBoundEvent{
			attempt: effect.attempt, result: campaignBarrierEvidence(bound),
		})
	} else {
		admitted := runtimeReplayAdmission(harness.applyRuntime(processruntime.RequestAdmissionCut(
			processRuntimeAdmission(effects[0].request),
		)))

		effects = harness.advance(admissionGrantedEvent{
			attempt: effect.attempt, grant: campaignAdmissionValue(admitted.deliveries[0]),
		})
	}
	grant := effects[0].grant
	started := runtimeReplayStart(harness.applyRuntime(processruntime.CommitStartCut(
		processRuntimeAdmission(grant),
	)))

	harness.admissions[started.generation] = runtimeAdmissionRequest(grant)
	harness.advance(startCommittedEvent{
		attempt: effect.attempt, grant: grant, result: campaignStartEvidence(started),
	})
	receipt := runtimeReplayReceipt(harness.applyRuntime(processruntime.ObserveAttemptCut(started.generation, processruntime.Owned())))
	harness.advance(attemptLaunchEvent{
		attempt: effect.attempt, generation: started.generation,
		result: campaignLaunchObservation{kind: campaignLaunchOwned}, receipt: campaignReceiptValue(receipt),
	})

	return launchedCampaignAttempt{attempt: effect.attempt, generation: started.generation}
}

func (harness *campaignHarness) settleConfirmation(
	t *testing.T,
	attempt launchedCampaignAttempt,
	terminal Terminal,
) []campaignEffect {
	t.Helper()
	queueDrained := len(harness.state.drain.provisionals) == 1
	grant, found := harness.admissionByGeneration(attempt.generation)
	require.True(t, found, "confirmation generation is not live")
	var observation attemptObservation
	switch terminal := terminal.(type) {
	case Settled:
		observation = attemptSettled{profile: grant.profile, deadline: grant.deadline}
	case Tripped:
		switch terminal.Trip.(type) {
		case FuseTrip:
			observation = attemptTripped{kind: fuseTrip}
		case AutomaticDeadlineTrip, SerialDeadlineTrip:
			observation = attemptTripped{kind: deadlineTrip, profile: grant.profile, deadline: grant.deadline}
		default:
			require.FailNow(t, "confirmation trip is invalid")
		}
	default:
		require.FailNow(t, "confirmation terminal is invalid")
	}
	receipt := runtimeReplayReceipt(harness.applyRuntime(processruntime.ObserveAttemptCut(attempt.generation, processRuntimeObservation(observation))))
	if queueDrained {
		completed := runtimeReplayQueue(harness.applyRuntime(processruntime.CompleteConfirmationQueueCut(grant.campaign)))
		assert.Equal(t, processruntime.ConfirmationQueueCompleted, completed.decision, "confirmation queue completion = %#v", completed)
		receipt.confirmationQueueDrained = true
		receipt.deliveries = append(receipt.deliveries, completed.deliveries...)
	}

	return harness.advance(attemptTerminalEvent{
		attempt: attempt.attempt, generation: attempt.generation, terminal: terminal,
		receipt: campaignReceiptValue(receipt),
	})
}

func assertCampaignEffects(t *testing.T, effects []campaignEffect, kinds ...campaignEffectKind) {
	t.Helper()
	got := make([]campaignEffectKind, len(effects))
	for index := range effects {
		got[index] = effects[index].kind
	}
	assert.Equal(t, kinds, got, "effect kinds=%v, want %v; effects=%#v", got, kinds, effects)
}
