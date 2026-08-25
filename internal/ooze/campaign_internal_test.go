package ooze

import (
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"
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
			decision: campaignRegistered,
			token:    campaignToken{id: 1, lineage: 11},
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
	if got := state.commandCount(); got != 0 {
		t.Fatalf("command count=%d, want 0", got)
	}

	state, effects = advanceCampaign(state, campaignEvent{
		id: 4, payload: resourceSettledEvent{kind: campaignResourceSnapshot, identity: "snapshot-a"},
	})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)

	state, effects = advanceCampaign(state, campaignEvent{
		id: 5, payload: terminalCommittedEvent{result: campaignTerminalResult{decision: terminalCommitted}},
	})
	if len(effects) != 0 || !reflect.DeepEqual(state.outcome, noMutantsOutcome{}) {
		t.Fatalf("terminal state/effects=%#v/%#v", state, effects)
	}
	if len(state.obligations) != 0 {
		t.Fatalf("normal terminal retained obligations: %#v", state.obligations)
	}
	if definition.command[0] != "go" || state.definition.command[0] != "go" {
		t.Fatalf("definition was not preserved: input=%#v state=%#v", definition, state.definition)
	}
}

func TestCampaignDefinitionOwnsPositiveTenMinuteBaselineBootstrap(t *testing.T) {
	definition := campaignDefinition{
		identity: "campaign-a", lineage: 11, command: []string{"test"}, profile: SerialProfile, peers: 8,
	}
	state, _ := beginCampaign(definition)
	if state.definition.baselineDeadline != 10*time.Minute {
		t.Fatalf("baseline deadline=%s, want 10m", state.definition.baselineDeadline)
	}

	definition.baselineDeadline = time.Second
	defer func() {
		if recover() == nil {
			t.Fatal("caller-selected baseline deadline was accepted")
		}
	}()
	_, _ = beginCampaign(definition)
}

func TestCampaignNonEmptyCatalogueRunsOneSnapshotBoundBaselineBeforePrimaries(t *testing.T) {
	definition := campaignDefinition{
		identity: "campaign-a", lineage: 11, command: []string{"go", "test"},
		env: []string{"A=1"}, profile: AutomaticProfile, peers: 4,
	}
	runtime := newProcessRuntime(4)
	state, _ := beginCampaign(definition)
	runtime, registered := runtime.registerCampaign(campaignProvenance{lineage: definition.lineage})
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
	if baseline.snapshot != "snapshot-a" || baseline.attempt == "" {
		t.Fatalf("baseline materialization=%#v", baseline)
	}
	mutants[0] = "mutated-by-caller"
	if !reflect.DeepEqual(state.catalogue, []mutantIdentity{"mutant-a", "mutant-b"}) {
		t.Fatalf("catalogue aliases caller input: %#v", state.catalogue)
	}

	state, effects = advanceCampaign(state, campaignEvent{
		id: 4, payload: workspaceMaterializedEvent{
			attempt: baseline.attempt, workspace: "workspace-baseline", snapshot: baseline.snapshot,
		},
	})
	assertCampaignEffects(t, effects, campaignEffectRequestAdmission)
	if effects[0].request.class != exclusiveAdmission {
		t.Fatalf("baseline admission=%#v, want exclusive", effects[0].request)
	}
	runtime, admitted := runtime.requestAdmission(runtimeAdmissionRequest(effects[0].request))
	state, effects = advanceCampaign(state, campaignEvent{
		id: 5, payload: admissionGrantedEvent{
			attempt: baseline.attempt, grant: campaignAdmissionFact(admitted.deliveries[0]),
		},
	})
	assertCampaignEffects(t, effects, campaignEffectRequestStartCommitment)
	runtime, started := runtime.startCommitted(admitted.deliveries[0])
	state, effects = advanceCampaign(state, campaignEvent{
		id: 6, payload: startCommittedEvent{
			attempt: baseline.attempt, grant: effects[0].grant, result: campaignStartEvidence(started),
		},
	})
	assertCampaignEffects(t, effects, campaignEffectLaunchAttempt)
	if effects[0].snapshot != "snapshot-a" || effects[0].workspace != "workspace-baseline" {
		t.Fatalf("baseline launch=%#v", effects[0])
	}
	wantSpec := Spec{
		Attempt: string(baseline.attempt), Command: []string{"go", "test"}, Dir: "workspace-baseline",
		Env: []string{"A=1"}, Profile: AutomaticProfile, Deadline: 10 * time.Minute,
	}
	if !reflect.DeepEqual(effects[0].spec, wantSpec) {
		t.Fatalf("baseline supervisor spec=%#v, want %#v", effects[0].spec, wantSpec)
	}
	runtime, launchReceipt := runtime.observeAttempt(started.generation, launchOwned{})
	state, effects = advanceCampaign(state, campaignEvent{
		id: 7, payload: attemptLaunchEvent{
			attempt: baseline.attempt, generation: started.generation,
			result: campaignLaunchObservation{kind: campaignLaunchOwned}, receipt: campaignReceipt(launchReceipt),
		},
	})
	if len(effects) != 0 {
		t.Fatalf("owned baseline emitted effects=%#v", effects)
	}

	terminal := Settled{
		Exit:          ExitStatus{},
		ExecutionData: ExecutionData{Deadline: 10 * time.Minute, CommandDuration: 2 * time.Second},
	}
	_, terminalReceipt := runtime.observeAttempt(started.generation, attemptSettled{})
	resolved := resolveMutationDeadline(terminal.CommandDuration, definition.peers)
	state, effects = advanceCampaign(state, campaignEvent{
		id: 8, payload: attemptTerminalEvent{
			attempt: baseline.attempt, generation: started.generation, terminal: terminal,
			receipt:                  campaignReceipt(terminalReceipt),
			resolvedMutationDeadline: resolveBaselineMutationDeadline(terminal.CommandDuration, definition.peers),
		},
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	if resolved != 20*time.Second || state.mutationDeadline != resolved || state.commandCount() != 1 {
		t.Fatalf("baseline resolution/count=%s/%s/%d", resolved, state.mutationDeadline, state.commandCount())
	}

	state, effects = advanceCampaign(state, campaignEvent{
		id: 9, payload: resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-baseline"},
	})
	if state.phase != campaignRunning {
		t.Fatalf("phase=%v, want running", state.phase)
	}
	if len(effects) != 2 {
		t.Fatalf("primary demand effects=%#v, want one per mutant up to peers", effects)
	}
	for _, effect := range effects {
		if effect.kind != campaignEffectMaterializeWorkspace || effect.snapshot != "snapshot-a" {
			t.Fatalf("primary materialization escaped snapshot: %#v", effect)
		}
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
	if harness.state.mutationDeadline != time.Nanosecond {
		t.Fatalf("replay deadline=%s, want recorded 1ns", harness.state.mutationDeadline)
	}
}

func TestCampaignCompletedRunsExactlyOneBaselineAndOnePrimaryPerMutant(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a", "mutant-b"}, 2)
	if len(primaryEffects) != 2 {
		t.Fatalf("primary effects=%#v", primaryEffects)
	}

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
	if len(effects) != 0 {
		t.Fatalf("first primary cleanup effects=%#v", effects)
	}
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-b"})
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
	if harness.state.phase != campaignDraining || harness.state.drain.kind != campaignDrainComplete {
		t.Fatalf("completion drain=%#v", harness.state.drain)
	}
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceSnapshot, identity: "snapshot-a"})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)
	harness.runtime, _ = harness.runtime.commitTerminal(harness.state.runtimeToken)
	harness.advance(terminalCommittedEvent{result: campaignTerminalResult{decision: terminalCommitted}})

	completed, ok := harness.state.outcome.(completedOutcome)
	want := []mutantResult{{mutant: "mutant-a", kind: mutantSurvived}, {mutant: "mutant-b", kind: mutantKilled}}
	if !ok || !reflect.DeepEqual(completed.mutants, want) {
		t.Fatalf("completed outcome=%#v, want %#v", harness.state.outcome, want)
	}
	if harness.state.commandCount() != 3 || len(harness.state.obligations) != 0 {
		t.Fatalf("commands/obligations=%d/%#v", harness.state.commandCount(), harness.state.obligations)
	}
}

func TestCampaignFailedBaselineAbortsUnscoredAfterSettlement(t *testing.T) {
	harness := newCampaignHarness(t, []mutantIdentity{"mutant-a"}, AutomaticProfile, 2)
	baseline := harness.launchMaterialized(t, harness.effects[0], "workspace-baseline")
	effects := harness.settleAttempt(t, baseline, Settled{
		Exit:          ExitStatus{Code: 1},
		ExecutionData: ExecutionData{Deadline: 10 * time.Minute, CommandDuration: time.Second},
	}, 0)
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-baseline"})
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceSnapshot, identity: "snapshot-a"})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)
	var committed terminalResult
	harness.runtime, committed = harness.runtime.commitTerminal(harness.state.runtimeToken)
	harness.advance(terminalCommittedEvent{result: campaignTerminalEvidence(committed)})
	if _, ok := harness.state.outcome.(abortedOutcome); !ok || harness.state.commandCount() != 1 {
		t.Fatalf("baseline failure outcome/count=%#v/%d", harness.state.outcome, harness.state.commandCount())
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
	if effects[0].attempt != second.attempt || harness.state.drain.kind != campaignDrainAbort {
		t.Fatalf("abort stop/drain=%#v/%#v", effects[0], harness.state.drain)
	}
	effects = harness.settleAttempt(t, second, Stopped{
		ExecutionData: ExecutionData{Deadline: harness.state.mutationDeadline, CommandDuration: time.Second},
	}, 0)
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-a"})
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-b"})
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
	if harness.state.commandCount() != 3 {
		t.Fatalf("abort duplicated command: count=%d", harness.state.commandCount())
	}
}

func TestCampaignBaselineProvenNotReleasedAbortsWithoutASecondLaunch(t *testing.T) {
	harness := newCampaignHarness(t, []mutantIdentity{"mutant-a"}, AutomaticProfile, 1)
	effect := harness.effects[0]
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: effect.attempt, workspace: "workspace-baseline", snapshot: effect.snapshot,
	})
	var admitted admissionResult
	harness.runtime, admitted = harness.runtime.requestAdmission(runtimeAdmissionRequest(effects[0].request))
	effects = harness.advance(admissionGrantedEvent{
		attempt: effect.attempt, grant: campaignAdmissionFact(admitted.deliveries[0]),
	})
	grant := effects[0].grant
	var started startCommittedResult
	harness.runtime, started = harness.runtime.startCommitted(admitted.deliveries[0])
	harness.advance(startCommittedEvent{
		attempt: effect.attempt, grant: grant, result: campaignStartEvidence(started),
	})
	var receipt observationResult
	harness.runtime, receipt = harness.runtime.observeAttempt(started.generation, launchNotReleased{reason: launchFailed})
	effects = harness.advance(attemptLaunchEvent{
		attempt: effect.attempt, generation: started.generation,
		result:  campaignLaunchObservation{kind: campaignLaunchNotReleased, failure: LaunchFailed},
		receipt: campaignReceipt(receipt),
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	if harness.state.drain.kind != campaignDrainAbort || harness.state.commandCount() != 1 {
		t.Fatalf("launch failure drain/count=%#v/%d", harness.state.drain, harness.state.commandCount())
	}
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
	var admitted admissionResult
	harness.runtime, admitted = harness.runtime.requestAdmission(runtimeAdmissionRequest(effects[0].request))
	effects = harness.advance(admissionGrantedEvent{
		attempt: primaryEffects[2].attempt, grant: campaignAdmissionFact(admitted.deliveries[0]),
	})
	grant := effects[0].grant

	deadline := harness.state.mutationDeadline
	effects = harness.settleAttempt(t, first, Tripped{
		Trip: AutomaticDeadlineTrip{}, ExecutionData: ExecutionData{
			Deadline: deadline, CommandDuration: deadline, BoundFired: CommandDeadlineFired,
		},
	}, 0)
	assertCampaignEffects(t, effects, campaignEffectReturnAdmission, campaignEffectReleaseWorkspace)
	var rejected startCommittedResult
	harness.runtime, rejected = harness.runtime.startCommitted(admitted.deliveries[0])
	if rejected.decision != startCommittedRejectedGrant {
		t.Fatalf("start decision=%v, want compensated-grant rejection", rejected.decision)
	}
	effects = harness.advance(startCommittedEvent{
		attempt: primaryEffects[2].attempt, grant: grant, result: campaignStartEvidence(rejected),
	})
	if len(effects) != 0 {
		t.Fatalf("rejected start duplicated compensation: %#v", effects)
	}
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
	if harness.state.drain.kind != campaignDrainAbort {
		t.Fatalf("confirmation infrastructure drain=%#v", harness.state.drain)
	}
}

func TestCampaignConfirmationFuseIsAuthoritativeRunaway(t *testing.T) {
	harness, confirmation, mutant := newCampaignConfirmationHarness(t)
	effects := harness.settleConfirmation(t, confirmation, Tripped{
		Trip: FuseTrip{Live: 9}, ExecutionData: ExecutionData{
			Deadline: harness.state.mutationDeadline, CommandDuration: time.Second,
		},
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	if harness.state.mutants[harness.state.mutantIndex(mutant)].result != mutantRunaway {
		t.Fatalf("confirmation fuse result=%#v", harness.state.mutants)
	}
}

func TestCampaignConfirmationRequiresConfirmationRuntimeReceipt(t *testing.T) {
	harness, confirmation, _ := newCampaignConfirmationHarness(t)
	defer func() {
		violation, ok := recover().(runtimeInvariantViolation)
		if !ok || violation.operation != "campaign observe confirmation terminal" {
			t.Fatalf("recovered=%#v", violation)
		}
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
		if !ok || violation.operation != "campaign observe terminal" {
			t.Fatalf("recovered=%#v", violation)
		}
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
	var admitted admissionResult
	harness.runtime, admitted = harness.runtime.requestAdmission(runtimeAdmissionRequest(effects[0].request))
	effects = harness.advance(admissionGrantedEvent{
		attempt: effect.attempt, grant: campaignAdmissionFact(admitted.deliveries[0]),
	})
	grant := effects[0].grant
	var started startCommittedResult
	harness.runtime, started = harness.runtime.startCommitted(admitted.deliveries[0])
	harness.advance(startCommittedEvent{
		attempt: effect.attempt, grant: grant, result: campaignStartEvidence(started),
	})
	var receipt observationResult
	harness.runtime, receipt = harness.runtime.observeAttempt(started.generation, launchUnconfirmed{})
	harness.advance(attemptLaunchEvent{
		attempt: effect.attempt, generation: started.generation,
		result:  campaignLaunchObservation{kind: campaignLaunchUnconfirmed, residual: ProspectiveUnresolved},
		receipt: campaignReceipt(receipt),
	})
	var settlement emergencySettlement
	harness.runtime, settlement = harness.runtime.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
		generation: started.generation, disposition: emergencyConfirmedDrained,
	}}})
	effects = harness.advance(runtimeEmergencySettledEvent{
		epoch: receipt.fatalEpoch, settlement: campaignSettlement(settlement),
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-baseline"})
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceSnapshot, identity: "snapshot-a"})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)
	var forced terminalResult
	harness.runtime, forced = harness.runtime.authorizeForcedAbort(harness.state.runtimeToken, receipt.fatalEpoch)
	harness.advance(terminalCommittedEvent{result: campaignTerminalEvidence(forced)})
	if _, ok := harness.state.outcome.(abortedOutcome); !ok || len(harness.state.obligations) != 0 {
		t.Fatalf("forced outcome/obligations=%#v/%#v", harness.state.outcome, harness.state.obligations)
	}
}

func TestCampaignPreparationFailuresAbortWithoutCommands(t *testing.T) {
	definition := campaignDefinition{
		identity: "campaign-a", lineage: 11, command: []string{"test"}, profile: AutomaticProfile, peers: 1,
	}
	state, _ := beginCampaign(definition)
	state, effects := advanceCampaign(state, campaignEvent{id: 1, payload: campaignRegisteredEvent{
		registration: campaignRegistration{decision: campaignRejectedRecursive},
	}})
	if len(effects) != 0 || state.commandCount() != 0 {
		t.Fatalf("registration rejection state/effects=%#v/%#v", state, effects)
	}
	if _, ok := state.outcome.(abortedOutcome); !ok {
		t.Fatalf("registration rejection outcome=%#v", state.outcome)
	}

	state, _ = beginCampaign(definition)
	runtime := newProcessRuntime(1)
	_, registered := runtime.registerCampaign(campaignProvenance{lineage: definition.lineage})
	state, _ = advanceCampaign(state, campaignEvent{id: 1, payload: campaignRegisteredEvent{registration: registered}})
	state, effects = advanceCampaign(state, campaignEvent{id: 2, payload: campaignPreparationFailedEvent{
		stage: campaignPreparingSnapshot, cause: "snapshot failed",
	}})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)
	if state.commandCount() != 0 || state.drain.kind != campaignDrainAbort {
		t.Fatalf("snapshot failure state=%#v", state)
	}
}

func TestCampaignAttemptWorkspaceFailureAbortsAndStopsCommittedPeers(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a", "mutant-b"}, 2)
	peer := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	effects := harness.advance(workspaceMaterializationFailedEvent{
		attempt: primaryEffects[1].attempt, cause: "workspace copy failed",
	})
	assertCampaignEffects(t, effects, campaignEffectStopAttempt)
	if effects[0].attempt != peer.attempt || harness.state.drain.kind != campaignDrainAbort {
		t.Fatalf("workspace abort state/effects=%#v/%#v", harness.state.drain, effects)
	}
}

func TestCampaignAbortCancelsWaitingAdmissionBeforeTerminal(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a", "mutant-b"}, 2)
	var peerRegistration campaignRegistration
	harness.runtime, peerRegistration = harness.runtime.registerCampaign(campaignProvenance{lineage: 99})
	peerRequest := admissionRequest{
		campaign: peerRegistration.token, attempt: "peer:1", class: sharedAdmission,
	}
	var peerAdmission admissionResult
	harness.runtime, peerAdmission = harness.runtime.requestAdmission(peerRequest)
	var peerStart startCommittedResult
	harness.runtime, peerStart = harness.runtime.startCommitted(peerAdmission.deliveries[0])
	harness.runtime, _ = harness.runtime.observeAttempt(peerStart.generation, launchOwned{})
	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: primaryEffects[1].attempt, workspace: "workspace-b", snapshot: primaryEffects[1].snapshot,
	})
	request := effects[0].request
	var waiting admissionResult
	harness.runtime, waiting = harness.runtime.requestAdmission(runtimeAdmissionRequest(request))
	if waiting.decision != admissionAccepted || len(waiting.deliveries) != 0 {
		t.Fatalf("second request was not waiting: %#v", waiting)
	}
	effects = harness.settleAttempt(t, first, Infrastructure{
		Cause: CensusFailed, Err: errors.New("census failed"),
		ExecutionData: ExecutionData{Deadline: harness.state.mutationDeadline, CommandDuration: time.Second},
	}, 0)
	assertCampaignEffects(t, effects, campaignEffectCancelAdmission, campaignEffectReleaseWorkspace)
	var cancelled admissionResult
	harness.runtime, cancelled = harness.runtime.cancelAdmission(runtimeAdmissionRequest(request))
	if cancelled.decision != admissionCancelledWaiting && cancelled.decision != admissionCancelledGranted {
		t.Fatalf("unexpected cancellation=%#v", cancelled)
	}
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
	if harness.state.candidate.kind != campaignTerminalAborted || harness.state.drain.kind != campaignDrainAbort {
		t.Fatalf("artifact failure state=%#v", harness.state)
	}
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
	if effects[0].attempt != second.attempt {
		t.Fatalf("workspace failure stopped=%s, want %s", effects[0].attempt, second.attempt)
	}
}

func TestCampaignPeerFatalClosureDrainsUncommittedLocalResources(t *testing.T) {
	harness := newCampaignHarness(t, []mutantIdentity{"mutant-a"}, AutomaticProfile, 1)
	var closure runtimeClosure
	harness.runtime, closure = harness.runtime.closeRuntime("peer fatal")
	effects := harness.advance(runtimeEmergencyStartedEvent{closure: campaignClosure(closure)})
	if len(effects) != 0 || harness.state.drain.kind != campaignDrainRuntimeEmergency {
		t.Fatalf("peer emergency state/effects=%#v/%#v", harness.state.drain, effects)
	}
	var settlement emergencySettlement
	harness.runtime, settlement = harness.runtime.settleEmergency(emergencySweep{})
	harness.advance(runtimeEmergencySettledEvent{
		epoch: closure.epoch, settlement: campaignSettlement(settlement),
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
	var closure runtimeClosure
	harness.runtime, closure = harness.runtime.closeRuntime("peer fatal")
	harness.advance(runtimeEmergencyStartedEvent{closure: campaignClosure(closure)})
	effects := harness.advance(runtimeEmergencySettledEvent{
		epoch: closure.epoch,
		settlement: campaignEmergencySettlement{
			epoch: closure.epoch, owner: campaignToken{id: 99, lineage: 99},
			residual: []campaignResidualCustody{{generation: 91, stage: admissionOwned, transferred: true}},
		},
	})
	if harness.state.failure != nil || len(effects) != 0 {
		t.Fatalf("non-owner settlement failure/effects = %#v/%#v, want deferred forced abort", harness.state.failure, effects)
	}
	effects = harness.advance(workspaceMaterializedEvent{
		attempt: harness.effects[0].attempt, workspace: "workspace-baseline", snapshot: "snapshot-a",
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-baseline"})
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceSnapshot, identity: "snapshot-a"})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)
	if effects[0].fatalEpoch != closure.epoch {
		t.Fatalf("forced-abort epoch = %d, want %d", effects[0].fatalEpoch, closure.epoch)
	}
}

func TestCampaignTerminalRejectedByFatalClosureAwaitsForcedAbort(t *testing.T) {
	harness := newCampaignHarness(t, nil, AutomaticProfile, 1)
	effects := harness.advance(resourceSettledEvent{kind: campaignResourceSnapshot, identity: "snapshot-a"})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)
	var closure runtimeClosure
	harness.runtime, closure = harness.runtime.closeRuntime("terminal race")
	var rejected terminalResult
	harness.runtime, rejected = harness.runtime.commitTerminal(harness.state.runtimeToken)
	effects = harness.advance(terminalCommittedEvent{result: campaignTerminalEvidence(rejected)})
	if len(effects) != 0 || harness.state.drain.kind != campaignDrainRuntimeEmergency {
		t.Fatalf("terminal race state/effects=%#v/%#v", harness.state.drain, effects)
	}
	var settlement emergencySettlement
	harness.runtime, settlement = harness.runtime.settleEmergency(emergencySweep{})
	effects = harness.advance(runtimeEmergencySettledEvent{
		epoch: closure.epoch, settlement: campaignSettlement(settlement),
	})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)
}

func TestCampaignAdmissionClosedByFatalEpochDrainsWithoutLaunching(t *testing.T) {
	harness := newCampaignHarness(t, []mutantIdentity{"mutant-a"}, AutomaticProfile, 1)
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: harness.effects[0].attempt, workspace: "workspace-baseline", snapshot: "snapshot-a",
	})
	request := effects[0].request
	var closure runtimeClosure
	harness.runtime, closure = harness.runtime.closeRuntime("admission race")
	var rejected admissionResult
	harness.runtime, rejected = harness.runtime.requestAdmission(runtimeAdmissionRequest(request))
	effects = harness.advance(admissionRejectedEvent{
		attempt: harness.effects[0].attempt, result: campaignAdmissionEvidence(rejected), cause: "runtime closed",
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	if harness.state.drain.kind != campaignDrainRuntimeEmergency ||
		harness.state.drain.epoch != closure.epoch || harness.state.commandCount() != 0 {
		t.Fatalf("closed admission state=%#v", harness.state)
	}
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
	if passing == failing {
		t.Fatalf("trace collapsed distinct terminal facts: %q", passing)
	}
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
	if direct == provisional {
		t.Fatalf("trace collapsed attribution-changing receipt facts: %q", direct)
	}
}

func TestCampaignRejectsBrokerContradictionAsInvariant(t *testing.T) {
	harness := newCampaignHarness(t, []mutantIdentity{"mutant-a"}, AutomaticProfile, 1)
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: harness.effects[0].attempt, workspace: "workspace-baseline", snapshot: "snapshot-a",
	})
	defer func() {
		violation, ok := recover().(runtimeInvariantViolation)
		if !ok || violation.operation != "campaign reject admission" {
			t.Fatalf("recovered=%#v", violation)
		}
	}()
	harness.advance(admissionRejectedEvent{
		attempt: harness.effects[0].attempt,
		result:  campaignAdmissionResult{decision: admissionRejectedDuplicate, request: effects[0].request},
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
	if harness.state.phase != campaignDraining || harness.state.drain.kind != campaignDrainConfirm ||
		!reflect.DeepEqual(harness.state.drain.provisionals, []mutantIdentity{"mutant-a", "mutant-b"}) {
		t.Fatalf("confirmation drain=%#v", harness.state.drain)
	}

	harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-a"})
	effects := harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-b"})
	assertCampaignEffects(t, effects, campaignEffectMaterializeWorkspace)
	if effects[0].mutant != "mutant-a" || harness.state.phase != campaignConfirming {
		t.Fatalf("first confirmation=%#v phase=%v", effects[0], harness.state.phase)
	}
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
	if effects[0].mutant != "mutant-b" {
		t.Fatalf("second confirmation=%#v", effects[0])
	}
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
	if harness.state.commandCount() != 5 || harness.runtime.mode != singleAdmission {
		t.Fatalf("command count/mode=%d/%v", harness.state.commandCount(), harness.runtime.mode)
	}

	harness.advance(resourceSettledEvent{kind: campaignResourceSnapshot, identity: "snapshot-a"})
	var committed terminalResult
	harness.runtime, committed = harness.runtime.commitTerminal(harness.state.runtimeToken)
	harness.advance(terminalCommittedEvent{result: campaignTerminalEvidence(committed)})
	completed := harness.state.outcome.(completedOutcome)
	want := []mutantResult{{mutant: "mutant-a", kind: mutantSurvived}, {mutant: "mutant-b", kind: mutantTimedOut}}
	if !reflect.DeepEqual(completed.mutants, want) {
		t.Fatalf("confirmation outcome=%#v, want %#v", completed.mutants, want)
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
	var thirdAdmission admissionResult
	harness.runtime, thirdAdmission = harness.runtime.requestAdmission(
		runtimeAdmissionRequest(thirdRequestEffects[0].request),
	)
	if len(thirdAdmission.deliveries) != 1 {
		t.Fatalf("third grant=%#v", thirdAdmission)
	}

	deadline := harness.state.mutationDeadline
	effects := harness.settleAttempt(t, first, Tripped{
		Trip:          AutomaticDeadlineTrip{},
		ExecutionData: ExecutionData{Deadline: deadline, CommandDuration: deadline, BoundFired: CommandDeadlineFired},
	}, 0)
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	effects = harness.advance(admissionGrantedEvent{
		attempt: primaryEffects[2].attempt, grant: campaignAdmissionFact(thirdAdmission.deliveries[0]),
	})
	assertCampaignEffects(t, effects, campaignEffectReturnAdmission)
	if effects[0].grant != campaignAdmissionFact(thirdAdmission.deliveries[0]) {
		t.Fatalf("late grant return=%#v, want %#v", effects[0].grant, thirdAdmission.deliveries[0])
	}
	var returned admissionResult
	harness.runtime, returned = harness.runtime.acknowledgeGrantReturn(thirdAdmission.deliveries[0])
	effects = harness.advance(grantReturnAcknowledgedEvent{
		grant: effects[0].grant, result: campaignAdmissionEvidence(returned),
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)

	if harness.state.phase != campaignDraining || harness.state.drain.kind != campaignDrainConfirm {
		t.Fatalf("phase/drain=%v/%#v", harness.state.phase, harness.state.drain)
	}
	effects = harness.settleAttempt(t, second, Settled{
		Exit: ExitStatus{}, ExecutionData: ExecutionData{Deadline: deadline, CommandDuration: time.Second},
	}, 0)
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
}

func TestCampaignAdoptsStartCommittedBeforeClosureWhoseFactArrivesAfterClosure(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a", "mutant-b"}, 2)
	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")

	effects := harness.advance(workspaceMaterializedEvent{
		attempt: primaryEffects[1].attempt, workspace: "workspace-b", snapshot: primaryEffects[1].snapshot,
	})
	var admitted admissionResult
	harness.runtime, admitted = harness.runtime.requestAdmission(runtimeAdmissionRequest(effects[0].request))
	effects = harness.advance(admissionGrantedEvent{
		attempt: primaryEffects[1].attempt, grant: campaignAdmissionFact(admitted.deliveries[0]),
	})
	grant := effects[0].grant
	var delayed startCommittedResult
	harness.runtime, delayed = harness.runtime.startCommitted(admitted.deliveries[0])

	deadline := harness.state.mutationDeadline
	harness.settleAttempt(t, first, Tripped{
		Trip:          AutomaticDeadlineTrip{},
		ExecutionData: ExecutionData{Deadline: deadline, CommandDuration: deadline, BoundFired: CommandDeadlineFired},
	}, 0)
	if harness.state.phase != campaignDraining {
		t.Fatalf("phase=%v, want draining before delayed committed fact", harness.state.phase)
	}

	effects = harness.advance(startCommittedEvent{
		attempt: primaryEffects[1].attempt, grant: grant, result: campaignStartEvidence(delayed),
	})
	assertCampaignEffects(t, effects, campaignEffectLaunchAttempt)
	if effects[0].generation != delayed.generation {
		t.Fatalf("adopted generation=%d, want %d", effects[0].generation, delayed.generation)
	}
	var receipt observationResult
	harness.runtime, receipt = harness.runtime.observeAttempt(delayed.generation, launchOwned{})
	effects = harness.advance(attemptLaunchEvent{
		attempt: primaryEffects[1].attempt, generation: delayed.generation,
		result: campaignLaunchObservation{kind: campaignLaunchOwned}, receipt: campaignReceipt(receipt),
	})
	if len(effects) != 0 {
		t.Fatalf("adopted pre-closure start emitted synthetic work: %#v", effects)
	}
}

func TestCampaignCleanupUnconfirmedRequiresStaticNonEmptyResidual(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a"}, 1)
	primary := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	effects := harness.settleAttempt(t, primary, DrainUnconfirmed{
		Residual:      OwnedUndrained,
		ExecutionData: ExecutionData{Deadline: harness.state.mutationDeadline, CommandDuration: time.Second},
	}, 0)
	if len(effects) != 0 || harness.state.phase != campaignDraining ||
		harness.state.drain.kind != campaignDrainRuntimeEmergency || harness.state.drain.epoch == 0 {
		t.Fatalf("runtime emergency state/effects=%#v/%#v", harness.state, effects)
	}

	var settlement emergencySettlement
	harness.runtime, settlement = harness.runtime.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
		generation: primary.generation, disposition: emergencyCustodyTransferred,
	}}})
	harness.advance(runtimeEmergencySettledEvent{
		epoch: harness.state.drain.epoch, settlement: campaignSettlement(settlement),
	})
	failure, ok := harness.state.failure.(cleanupUnconfirmedFault)
	if !ok || failure.residual.head.generation != primary.generation || len(harness.state.obligations) == 0 {
		t.Fatalf("cleanup failure/ledger=%#v/%#v", harness.state.failure, harness.state.obligations)
	}
}

func TestCampaignMalformedEventEmergencyCleansRuntimeAndRepanicsOriginalViolation(t *testing.T) {
	definition := campaignDefinition{
		identity: "campaign-a", lineage: 11, command: []string{"test"}, profile: AutomaticProfile, peers: 1,
	}
	state, _ := beginCampaign(definition)
	runtime := newProcessRuntime(1)
	var registered campaignRegistration
	runtime, registered = runtime.registerCampaign(campaignProvenance{lineage: definition.lineage})
	state, _ = advanceCampaign(state, campaignEvent{
		id: 1, payload: campaignRegisteredEvent{registration: registered},
	})

	malformed := campaignEvent{id: 2, payload: snapshotEstablishedEvent{}}
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _ = advanceCampaignGuarded(&runtime, state, malformed, func(closure runtimeClosure) emergencySweep {
			if closure.epoch == 0 || len(closure.residual) != 0 {
				t.Fatalf("invariant closure=%#v", closure)
			}

			return emergencySweep{}
		})
	}()
	violation, ok := recovered.(runtimeInvariantViolation)
	if !ok || violation.operation != "campaign establish snapshot" ||
		violation.reason != "snapshot observation is invalid" {
		t.Fatalf("recovered=%#v", recovered)
	}
	if violation.phase != uint8(campaignPreparing) || violation.rejectedEvent == "" ||
		len(violation.stableIdentities) == 0 || len(violation.obligationSnapshot) == 0 ||
		len(violation.traceTail) == 0 {
		t.Fatalf("invariant diagnostic is incomplete: %#v", violation)
	}
	if runtime.lifecycle != runtimeClosedDrained || runtime.fatalEpoch == 0 || len(runtime.fatalCauses) != 1 {
		t.Fatalf("runtime after invariant=%#v", runtime)
	}
}

func TestCampaignAdvanceKeepsEarlierStateImmutable(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a"}, 1)
	before := harness.state
	want := before.clone()
	harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	if !reflect.DeepEqual(before, want) {
		t.Fatalf("earlier campaign state was mutated:\n got=%#v\nwant=%#v", before, want)
	}
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
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("same definition/choices diverged:\nfirst=%#v\nsecond=%#v", first, second)
		}
		if first.commandCount() != len(choices)+1 {
			t.Fatalf("command count=%d, want %d", first.commandCount(), len(choices)+1)
		}
		if _, ok := first.outcome.(completedOutcome); !ok || len(first.obligations) != 0 {
			t.Fatalf("serial trace did not cooperatively settle: %#v", first)
		}
		for index, record := range first.trace {
			if record.id != campaignEventID(index+1) {
				t.Fatalf("trace identity[%d]=%d", index, record.id)
			}
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
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("peer emergency replay diverged:\nfirst=%#v\nsecond=%#v", first, second)
		}
		if _, ok := first.outcome.(abortedOutcome); !ok || first.commandCount() != 0 ||
			len(first.obligations) != 0 || len(first.trace) > 9 {
			t.Fatalf("peer emergency did not cooperatively settle: %#v", first)
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
	var closure runtimeClosure
	harness.runtime, closure = harness.runtime.closeRuntime("peer fatal")
	harness.advance(runtimeEmergencyStartedEvent{closure: campaignClosure(closure)})
	var settlement emergencySettlement
	harness.runtime, settlement = harness.runtime.settleEmergency(emergencySweep{})
	harness.advance(runtimeEmergencySettledEvent{
		epoch: closure.epoch, settlement: campaignSettlement(settlement),
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
	var forced terminalResult
	harness.runtime, forced = harness.runtime.authorizeForcedAbort(harness.state.runtimeToken, closure.epoch)
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
		if !reflect.DeepEqual(first, second) || first.operation == "" {
			t.Fatalf("malformed replay violation diverged: %#v / %#v", first, second)
		}
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
		if len(effects) != 1 || effects[0].kind != campaignEffectMaterializeWorkspace {
			t.Fatalf("primary %d materialization=%#v", index, effects)
		}
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
	var committed terminalResult
	harness.runtime, committed = harness.runtime.commitTerminal(harness.state.runtimeToken)
	harness.advance(terminalCommittedEvent{result: campaignTerminalEvidence(committed)})

	return harness.state
}

type campaignHarness struct {
	state     campaignState
	runtime   processRuntime
	nextEvent campaignEventID
	effects   []campaignEffect
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
	runtime := newProcessRuntime(peers)
	runtime, registered := runtime.registerCampaign(campaignProvenance{lineage: definition.lineage})
	harness := &campaignHarness{state: state, runtime: runtime}
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
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: effect.attempt, workspace: workspace, snapshot: effect.snapshot,
	})
	var admitted admissionResult
	harness.runtime, admitted = harness.runtime.requestAdmission(runtimeAdmissionRequest(effects[0].request))
	effects = harness.advance(admissionGrantedEvent{
		attempt: effect.attempt, grant: campaignAdmissionFact(admitted.deliveries[0]),
	})
	var started startCommittedResult
	grant := effects[0].grant
	harness.runtime, started = harness.runtime.startCommitted(admitted.deliveries[0])
	harness.advance(startCommittedEvent{
		attempt: effect.attempt, grant: grant, result: campaignStartEvidence(started),
	})
	var receipt observationResult
	harness.runtime, receipt = harness.runtime.observeAttempt(started.generation, launchOwned{})
	harness.advance(attemptLaunchEvent{
		attempt: effect.attempt, generation: started.generation,
		result: campaignLaunchObservation{kind: campaignLaunchOwned}, receipt: campaignReceipt(receipt),
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
	admissionAt := harness.runtime.admissionIndexByGeneration(attempt.generation)
	if admissionAt < 0 {
		t.Fatal("attempt generation is not live")
	}
	grant := harness.runtime.admissions[admissionAt].grant
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
		t.Fatalf("unsupported fixture terminal %#v", terminal)
	}
	var receipt observationResult
	harness.runtime, receipt = harness.runtime.observeAttempt(attempt.generation, observation)

	event := attemptTerminalEvent{
		attempt: attempt.attempt, generation: attempt.generation, terminal: terminal,
		receipt: campaignReceipt(receipt),
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
		var bound barrierResult
		harness.runtime, bound = harness.runtime.sealAndBindConfirmationBarrier(runtimeBarrierBinding(effects[0].binding))
		effects = harness.advance(confirmationBarrierBoundEvent{
			attempt: effect.attempt, result: campaignBarrierEvidence(bound),
		})
	} else {
		var admitted admissionResult
		harness.runtime, admitted = harness.runtime.requestAdmission(runtimeAdmissionRequest(effects[0].request))
		effects = harness.advance(admissionGrantedEvent{
			attempt: effect.attempt, grant: campaignAdmissionFact(admitted.deliveries[0]),
		})
	}
	var started startCommittedResult
	grant := effects[0].grant
	grantAt := harness.runtime.admissionIndex(runtimeAdmissionRequest(grant))
	if grantAt < 0 {
		t.Fatal("confirmation grant is not live")
	}
	harness.runtime, started = harness.runtime.startCommitted(harness.runtime.admissions[grantAt].grant)
	harness.advance(startCommittedEvent{
		attempt: effect.attempt, grant: grant, result: campaignStartEvidence(started),
	})
	var receipt observationResult
	harness.runtime, receipt = harness.runtime.observeAttempt(started.generation, launchOwned{})
	harness.advance(attemptLaunchEvent{
		attempt: effect.attempt, generation: started.generation,
		result: campaignLaunchObservation{kind: campaignLaunchOwned}, receipt: campaignReceipt(receipt),
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
	admissionAt := harness.runtime.admissionIndexByGeneration(attempt.generation)
	if admissionAt < 0 {
		t.Fatal("confirmation generation is not live")
	}
	grant := harness.runtime.admissions[admissionAt].grant
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
			t.Fatal("confirmation trip is invalid")
		}
	default:
		t.Fatal("confirmation terminal is invalid")
	}
	var receipt observationResult
	harness.runtime, receipt = harness.runtime.observeAttempt(attempt.generation, observation)
	if queueDrained {
		var completed confirmationQueueResult
		harness.runtime, completed = harness.runtime.completeConfirmationQueue(grant.campaign)
		if completed.decision != confirmationQueueCompleted {
			t.Fatalf("confirmation queue completion = %#v", completed)
		}
		receipt.confirmationQueueDrained = true
		receipt.deliveries = append(receipt.deliveries, completed.deliveries...)
	}

	return harness.advance(attemptTerminalEvent{
		attempt: attempt.attempt, generation: attempt.generation, terminal: terminal,
		receipt: campaignReceipt(receipt),
	})
}

func assertCampaignEffects(t *testing.T, effects []campaignEffect, kinds ...campaignEffectKind) {
	t.Helper()
	got := make([]campaignEffectKind, len(effects))
	for index := range effects {
		got[index] = effects[index].kind
	}
	if !reflect.DeepEqual(got, kinds) {
		t.Fatalf("effect kinds=%v, want %v; effects=%#v", got, kinds, effects)
	}
}
