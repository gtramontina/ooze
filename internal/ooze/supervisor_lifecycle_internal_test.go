package ooze

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func validSupervisorSpec() Spec {
	return Spec{
		Attempt:  "attempt-a",
		Command:  []string{"go", "test", "./..."},
		Dir:      "/workspace",
		Env:      []string{"GOMAXPROCS=1"},
		Profile:  AutomaticProfile,
		Deadline: 20 * time.Second,
	}
}

func TestSupervisorRejectsInvalidSpecBeforeStartCommit(t *testing.T) {
	startCalls := 0
	nativeCalls := 0
	supervisor := newSupervisorForTest(
		func(attemptIdentity, *pendingStartCell) installedStart {
			startCalls++
			return installedStart{}
		},
		func(attemptGeneration, Spec) LaunchResult {
			nativeCalls++
			return NotReleased{Kind: LaunchFailed}
		},
	)

	invalid := []struct {
		name   string
		mutate func(*Spec)
	}{
		{name: "attempt", mutate: func(spec *Spec) { spec.Attempt = "" }},
		{name: "command", mutate: func(spec *Spec) { spec.Command = nil }},
		{name: "command executable", mutate: func(spec *Spec) { spec.Command[0] = "" }},
		{name: "profile", mutate: func(spec *Spec) { spec.Profile = 0 }},
		{name: "deadline", mutate: func(spec *Spec) { spec.Deadline = 0 }},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			spec := validSupervisorSpec()
			test.mutate(&spec)
			assertPanicsWith(t, ErrInvalidSpec, func() { supervisor.Launch(spec) })
		})
	}

	if startCalls != 0 || nativeCalls != 0 {
		t.Fatalf("invalid specs reached start/native work: start=%d native=%d", startCalls, nativeCalls)
	}
}

func TestSupervisorConstructionRejectsUnsupportedPlatform(t *testing.T) {
	startCalls := 0
	nativeCalls := 0
	supervisor, err := constructSupervisor(supervisorConstruction{
		supported: false,
		installStart: func(attemptIdentity, *pendingStartCell) installedStart {
			startCalls++
			return installedStart{}
		},
		launchNative: func(attemptGeneration, Spec) LaunchResult {
			nativeCalls++
			return NotReleased{Kind: LaunchFailed}
		},
	})

	if !errors.Is(err, ErrUnsupportedPlatform) || supervisor != nil {
		t.Fatalf("unsupported construction = %#v, %v", supervisor, err)
	}
	if startCalls != 0 || nativeCalls != 0 {
		t.Fatalf("unsupported construction reached start/native work: start=%d native=%d", startCalls, nativeCalls)
	}
}

func TestSupervisorLaunchRegistersExactGenerationBeforeNativeAndClassifiesBranches(t *testing.T) {
	for _, test := range []struct {
		name string
		want LaunchResult
	}{
		{name: "owned", want: Owned{Attempt: newOwnedAttemptForTest()}},
		{name: "not released", want: NotReleased{Kind: LaunchFailed, Err: errors.New("exec")}},
		{name: "launch unconfirmed", want: LaunchUnconfirmed{Residual: ProspectiveUnresolved}},
	} {
		t.Run(test.name, func(t *testing.T) {
			spec := validSupervisorSpec()
			shell := newProcessRuntimeShell(1)
			campaign := shell.registerCampaign(campaignProvenance{lineage: 11})
			requested := shell.requestAdmission(admissionRequest{
				campaign: campaign.token,
				attempt:  attemptIdentity(spec.Attempt),
				class:    sharedAdmission,
			})
			grant := <-requested.delivery
			order := make([]string, 0, 2)
			var cell *pendingStartCell
			var generation attemptGeneration
			supervisor := newSupervisorForTest(
				func(attempt attemptIdentity, pending *pendingStartCell) installedStart {
					order = append(order, "start-committed")
					if attempt != attemptIdentity(spec.Attempt) {
						t.Fatalf("start attempt = %q, want %q", attempt, spec.Attempt)
					}
					cell = pending
					prepared := shell.startCommitted(grant, startInstallation{grant: grant, cell: pending})
					if prepared.result.decision != startCommittedAccepted {
						t.Fatalf("start committed = %#v", prepared.result)
					}
					generation = prepared.result.generation

					return prepared.start
				},
				func(observed attemptGeneration, spec Spec) LaunchResult {
					order = append(order, "native")
					snapshot := shell.snapshot()
					index := snapshot.admissionIndexByGeneration(observed)
					if observed == 0 || observed != generation || cell == nil ||
						cell.installedGeneration() != observed || index < 0 ||
						snapshot.admissions[index].stage != admissionProspective {
						t.Fatalf("native observed generation/cell/runtime = %d/%v/%#v", observed, cell, snapshot)
					}
					if spec.Attempt != validSupervisorSpec().Attempt {
						t.Fatalf("native spec attempt = %q", spec.Attempt)
					}
					spec.Command[0] = "mutated-by-native"
					spec.Env[0] = "MUTATED=1"

					return test.want
				},
			)

			got := supervisor.Launch(spec)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("launch = %#v, want %#v", got, test.want)
			}
			if spec.Command[0] != "go" || spec.Env[0] != "GOMAXPROCS=1" {
				t.Fatalf("caller slices mutated through launch snapshot: command=%v env=%v", spec.Command, spec.Env)
			}
			if !reflect.DeepEqual(order, []string{"start-committed", "native"}) {
				t.Fatalf("launch order = %v", order)
			}
		})
	}
}

func TestSupervisorConcurrentLaunchesPairDistinctAttemptsAndGenerations(t *testing.T) {
	shell := newProcessRuntimeShell(2)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 11})
	first := validSupervisorSpec()
	first.Attempt = "attempt-a"
	second := validSupervisorSpec()
	second.Attempt = "attempt-b"
	second.Command = []string{"go", "test", "./second"}
	second.Env = []string{"GOMAXPROCS=2"}

	grants := make(map[attemptIdentity]admissionGrant, 2)
	for _, spec := range []Spec{first, second} {
		requested := shell.requestAdmission(admissionRequest{
			campaign: campaign.token,
			attempt:  attemptIdentity(spec.Attempt),
			class:    sharedAdmission,
		})
		grants[attemptIdentity(spec.Attempt)] = <-requested.delivery
	}

	enteredStart := make(chan attemptIdentity, 2)
	releaseStart := make(chan struct{})
	var observedMu sync.Mutex
	cells := make(map[attemptIdentity]*pendingStartCell, 2)
	generations := make(map[attemptIdentity]attemptGeneration, 2)
	nativeCalls := make(map[attemptIdentity]int, 2)
	supervisor := newSupervisorForTest(
		func(attempt attemptIdentity, cell *pendingStartCell) installedStart {
			enteredStart <- attempt
			<-releaseStart
			grant, ok := grants[attempt]
			if !ok || grant.attempt != attempt {
				t.Fatalf("start grant for %q = %#v", attempt, grant)
			}
			prepared := shell.startCommitted(grant, startInstallation{grant: grant, cell: cell})
			if prepared.result.decision != startCommittedAccepted {
				t.Fatalf("start committed for %q = %#v", attempt, prepared.result)
			}
			observedMu.Lock()
			cells[attempt] = cell
			generations[attempt] = prepared.result.generation
			observedMu.Unlock()

			return prepared.start
		},
		func(generation attemptGeneration, spec Spec) LaunchResult {
			attempt := attemptIdentity(spec.Attempt)
			observedMu.Lock()
			cell := cells[attempt]
			wantGeneration := generations[attempt]
			nativeCalls[attempt]++
			observedMu.Unlock()
			snapshot := shell.snapshot()
			index := snapshot.admissionIndexByGeneration(generation)
			if cell == nil || generation == 0 || generation != wantGeneration ||
				cell.installedGeneration() != generation || index < 0 ||
				snapshot.admissions[index].grant.attempt != attempt ||
				snapshot.admissions[index].stage != admissionProspective {
				t.Fatalf("native pairing for %q = generation %d, cell %p, state %#v", attempt, generation, cell, snapshot)
			}
			if attempt == attemptIdentity(first.Attempt) {
				spec.Command[0] = "mutated-first-snapshot"
				spec.Env[0] = "MUTATED=1"
			}

			return Owned{Attempt: newOwnedAttemptForTest()}
		},
	)

	results := make(chan LaunchResult, 2)
	go func() { results <- supervisor.Launch(first) }()
	go func() { results <- supervisor.Launch(second) }()
	startedA, startedB := <-enteredStart, <-enteredStart
	if startedA == startedB {
		t.Fatalf("concurrent starts used one attempt: %q/%q", startedA, startedB)
	}
	close(releaseStart)
	for range 2 {
		if _, ok := (<-results).(Owned); !ok {
			t.Fatalf("concurrent launch was not owned")
		}
	}

	observedMu.Lock()
	firstCell, secondCell := cells[attemptIdentity(first.Attempt)], cells[attemptIdentity(second.Attempt)]
	firstGeneration := generations[attemptIdentity(first.Attempt)]
	secondGeneration := generations[attemptIdentity(second.Attempt)]
	firstCalls := nativeCalls[attemptIdentity(first.Attempt)]
	secondCalls := nativeCalls[attemptIdentity(second.Attempt)]
	observedMu.Unlock()
	if firstCell == nil || secondCell == nil || firstCell == secondCell ||
		firstGeneration == 0 || secondGeneration == 0 || firstGeneration == secondGeneration ||
		firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("cells/generations/native calls = %p/%p %d/%d %d/%d",
			firstCell, secondCell, firstGeneration, secondGeneration, firstCalls, secondCalls)
	}
	if first.Command[0] != "go" || first.Env[0] != "GOMAXPROCS=1" ||
		second.Command[0] != "go" || second.Env[0] != "GOMAXPROCS=2" {
		t.Fatalf("caller snapshots crossed: first=%v/%v second=%v/%v", first.Command, first.Env, second.Command, second.Env)
	}
	snapshot := shell.snapshot()
	if snapshot.lifecycle != runtimeOpen || len(snapshot.admissions) != 2 ||
		snapshot.admissions[0].stage != admissionOwned || snapshot.admissions[1].stage != admissionOwned {
		t.Fatalf("concurrent launch runtime = %#v", snapshot)
	}
}

func TestSupervisorOwnedAttemptWaitIsIdempotentAndStopIsConcurrent(t *testing.T) {
	waitEntered := make(chan struct{})
	releaseWait := make(chan struct{})
	stopped := make(chan StopRequest, 1)
	waits := 0
	attempt := newOwnedAttempt(
		func(request StopRequest) { stopped <- request },
		func(sealStop func()) Terminal {
			waits++
			close(waitEntered)
			<-releaseWait
			sealStop()

			return Stopped{}
		},
	)

	results := make(chan Terminal, 2)
	var callers sync.WaitGroup
	callers.Add(2)
	go func() {
		defer callers.Done()
		results <- attempt.Wait()
	}()
	<-waitEntered
	go func() {
		defer callers.Done()
		results <- attempt.Wait()
	}()

	request := StopRequest{At: time.Unix(1, 0), DrainBy: time.Unix(2, 0)}
	attempt.Stop(request)
	close(releaseWait)
	callers.Wait()
	close(results)

	if got := <-stopped; got != request {
		t.Fatalf("stop = %#v, want %#v", got, request)
	}
	first, second := <-results, <-results
	if waits != 1 || !reflect.DeepEqual(first, second) {
		t.Fatalf("waits/results = %d/%#v/%#v", waits, first, second)
	}
}

func newOwnedAttemptForTest() *OwnedAttempt {
	return newOwnedAttempt(func(StopRequest) {}, func(sealStop func()) Terminal {
		sealStop()
		return Stopped{}
	})
}

func assertPanicsWith(t *testing.T, target error, action func()) {
	t.Helper()
	defer func() {
		recovered := recover()
		err, ok := recovered.(error)
		if !ok || !errors.Is(err, target) {
			t.Fatalf("panic = %#v, want error matching %v", recovered, target)
		}
	}()
	action()
}
