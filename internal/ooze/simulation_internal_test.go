package ooze

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

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

	explored := Explore(definition, simulationChoiceBytes{simulationChooseBaselineFailure})
	if explored.failure != nil {
		t.Fatalf("exploration failure=%v", explored.failure)
	}
	if _, ok := explored.world.campaign.outcome.(abortedOutcome); !ok {
		t.Fatalf("explored outcome=%#v, want aborted baseline", explored.world.campaign.outcome)
	}
	if got := countSimulationAuthority(explored.trace, simulationSupervisorAuthority); got != 8 {
		t.Fatalf("supervisor record count=%d, want 8 for complete supervised lifecycle", got)
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
					launchBy = record.supervisorEvent.launchBy
				}
				if record.authority == simulationSupervisorAuthority &&
					(record.supervisorEvent.kind == supervisorLaunchCompleted ||
						record.supervisorEvent.kind == supervisorLaunchBoundary) {
					launch = record.supervisorEvent
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
		campaign:  snapshotEstablishedEvent{},
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

func TestSimulationShrinkRemovesLegalRecordsAndDefinitionMembersToFixpoint(t *testing.T) {
	explored := Explore(simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-shrink", lineage: 41, command: []string{"test"},
			profile: AutomaticProfile, peers: 1,
		},
		capacity:  1,
		catalogue: []mutantIdentity{"mutant-a", "mutant-b"},
	}, simulationChoiceBytes{simulationChooseBaselineFailure})
	malformed := simulationMalformedFact{
		authority: simulationCampaignAuthority,
		campaign:  snapshotEstablishedEvent{},
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
	if shrunk.malformed == nil {
		t.Fatal("shrink removed the one intended corruption")
	}
	first := ReplayViolation(shrunk, *shrunk.malformed)
	second := ReplayViolation(shrunk, *shrunk.malformed)
	if first.failure != nil || !reflect.DeepEqual(first.key, key) || !reflect.DeepEqual(first, second) {
		t.Fatalf("shrunk replay did not retain stable failure:\nkey=%#v\nfirst=%#v\nsecond=%#v", key, first, second)
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
	driver.reduce(event)

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
		!reflect.DeepEqual(trace.records[2].supervisorActions, simulationProjectSupervisorActions(wantActions)) {
		t.Fatalf("recorded ordered outputs diverged: %#v", trace.records)
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
	for _, record := range trace.records {
		for _, admission := range record.runtimeState.admissions {
			if admission.grant.delivery != nil {
				t.Fatalf("runtime trace leaked a delivery channel: %#v", record)
			}
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
	if record.supervisorEvent.at.Location() != time.UTC ||
		record.supervisorState.attempts[0].registeredAt.Location() != time.UTC {
		t.Fatalf("supervisor trace retained host locations: %#v", record)
	}
	if !record.supervisorEvent.at.Equal(registeredAt) {
		t.Fatalf("canonical instant changed: got=%s want=%s", record.supervisorEvent.at, registeredAt)
	}
}

func FuzzSimulationLegalReplayAndViolationRemainDeterministic(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{1, 7, 9})
	f.Fuzz(func(t *testing.T, source []byte) {
		definition := simulationDefinition{
			campaign: campaignDefinition{
				identity: "campaign-fuzz", lineage: 61, command: []string{"test"},
				profile: AutomaticProfile, peers: 1,
			},
			capacity: 1,
		}
		choices := simulationChoiceBytes(nil)
		if len(source) != 0 && source[0]&1 != 0 {
			definition.catalogue = []mutantIdentity{"mutant-a"}
			choices = simulationChoiceBytes{simulationChooseBaselineFailure}
		}
		explored := Explore(definition, choices)
		replayed := ReplayLegal(explored.trace)
		if explored.failure != nil || replayed.failure != nil ||
			!reflect.DeepEqual(explored.world, replayed.world) {
			t.Fatalf("legal replay diverged: explored=%#v replayed=%#v", explored, replayed)
		}
		prefix := simulationTrace{
			definition: explored.trace.definition,
			records:    append([]simulationRecord(nil), explored.trace.records[:2]...),
		}
		malformed := simulationMalformedFact{
			authority: simulationCampaignAuthority, campaign: snapshotEstablishedEvent{},
		}
		first := ReplayViolation(prefix, malformed)
		second := ReplayViolation(prefix, malformed)
		if first.failure != nil || !reflect.DeepEqual(first, second) {
			t.Fatalf("violation replay diverged: first=%#v second=%#v", first, second)
		}
	})
}

func simulationAuthorities(trace simulationTrace) []simulationAuthority {
	authorities := make([]simulationAuthority, 0, len(trace.records))
	for _, record := range trace.records {
		authorities = append(authorities, record.authority)
	}

	return authorities
}

func countSimulationAuthority(trace simulationTrace, authority simulationAuthority) int {
	count := 0
	for _, record := range trace.records {
		if record.authority == authority {
			count++
		}
	}

	return count
}
