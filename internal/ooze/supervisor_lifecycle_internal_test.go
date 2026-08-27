package ooze

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	assert.EqualValues(t, 0, startCalls, "invalid specs reached start/native work: start=%d native=%d", startCalls, nativeCalls)
	assert.EqualValues(t, 0, nativeCalls, "invalid specs reached start/native work: start=%d native=%d", startCalls, nativeCalls)
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

	require.ErrorIs(t, err, ErrUnsupportedPlatform, "unsupported construction = %#v, %v", supervisor, err)
	assert.Nil(t, supervisor, "unsupported construction = %#v, %v", supervisor, err)
	assert.EqualValues(t, 0, startCalls, "unsupported construction reached start/native work: start=%d native=%d", startCalls, nativeCalls)
	assert.EqualValues(t, 0, nativeCalls, "unsupported construction reached start/native work: start=%d native=%d", startCalls, nativeCalls)
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
			campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 11})
			requested := requestAdmissionForTest(shell, admissionRequest{
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
					assert.Equal(t, attemptIdentity(spec.Attempt), attempt, "start attempt = %q, want %q", attempt, spec.Attempt)
					cell = pending
					prepared := startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: pending})
					assert.Equal(t, startCommittedAccepted, prepared.result.decision, "start committed = %#v", prepared.result)
					generation = prepared.result.generation

					return prepared.start
				},
				func(observed attemptGeneration, spec Spec) LaunchResult {
					order = append(order, "native")
					snapshot := shell.Image()
					_, admitted := runtimeAdmissionByGeneration(snapshot, observed)
					assert.NotEqual(t, 0, observed, "native observed generation/cell/runtime = %d/%v/%#v", observed, cell, snapshot)
					assert.Equal(t, generation, observed, "native observed generation/cell/runtime = %d/%v/%#v", observed, cell, snapshot)
					if assert.NotNil(t, cell, "native observed generation/cell/runtime = %d/%v/%#v", observed, cell, snapshot) {
						assert.Equal(t, observed, cell.InstalledGeneration(), "native observed generation/cell/runtime = %d/%v/%#v", observed, cell, snapshot)
					}
					assert.True(t, admitted, "native observed generation/cell/runtime = %d/%v/%#v", observed, cell, snapshot)
					assert.True(t, snapshot.Prospective(observed), "native observed generation/cell/runtime = %d/%v/%#v", observed, cell, snapshot)
					assert.Equal(t, validSupervisorSpec().Attempt, spec.Attempt, "native spec attempt = %q", spec.Attempt)
					spec.Command[0] = "mutated-by-native"
					spec.Env[0] = "MUTATED=1"

					return test.want
				},
			)

			got := supervisor.Launch(spec)
			assert.Equal(t, test.want, got, "launch = %#v, want %#v", got, test.want)
			assert.EqualValues(t, "go", spec.Command[0], "caller slices mutated through launch snapshot: command=%v env=%v", spec.Command, spec.Env)
			assert.EqualValues(t, "GOMAXPROCS=1", spec.Env[0], "caller slices mutated through launch snapshot: command=%v env=%v", spec.Command, spec.Env)
			assert.Equal(t, []string{"start-committed", "native"}, order, "launch order = %v", order)
		})
	}
}

func TestSupervisorConcurrentLaunchesPairDistinctAttemptsAndGenerations(t *testing.T) {
	shell := newProcessRuntimeShell(2)
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 11})
	first := validSupervisorSpec()
	first.Attempt = "attempt-a"
	second := validSupervisorSpec()
	second.Attempt = "attempt-b"
	second.Command = []string{"go", "test", "./second"}
	second.Env = []string{"GOMAXPROCS=2"}

	grants := make(map[attemptIdentity]admissionGrant, 2)
	for _, spec := range []Spec{first, second} {
		requested := requestAdmissionForTest(shell, admissionRequest{
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
			assert.True(t, ok, "start grant for %q = %#v", attempt, grant)
			assert.Equal(t, attempt, grant.attempt, "start grant for %q = %#v", attempt, grant)
			prepared := startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell})
			assert.Equal(t, startCommittedAccepted, prepared.result.decision, "start committed for %q = %#v", attempt, prepared.result)
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
			snapshot := shell.Image()
			admission, admitted := runtimeAdmissionByGeneration(snapshot, generation)
			cellPresent := assert.NotNil(t, cell, "native pairing for %q = generation %d, cell %p, state %#v", attempt, generation, cell, snapshot)
			assert.NotEqual(t, 0, generation, "native pairing for %q = generation %d, cell %p, state %#v", attempt, generation, cell, snapshot)
			assert.Equal(t, wantGeneration, generation, "native pairing for %q = generation %d, cell %p, state %#v", attempt, generation, cell, snapshot)
			if cellPresent {
				assert.Equal(t, generation, cell.InstalledGeneration(), "native pairing for %q = generation %d, cell %p, state %#v", attempt, generation, cell, snapshot)
			}
			if assert.True(t, admitted, "native pairing for %q = generation %d, cell %p, state %#v", attempt, generation, cell, snapshot) {
				assert.Equal(t, attempt, admission.attempt, "native pairing for %q = generation %d, cell %p, state %#v", attempt, generation, cell, snapshot)
				assert.True(t, snapshot.Prospective(generation), "native pairing for %q = generation %d, cell %p, state %#v", attempt, generation, cell, snapshot)
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
	assert.NotEqual(t, startedB, startedA, "concurrent starts used one attempt: %q/%q", startedA, startedB)
	close(releaseStart)
	for range 2 {
		{
			_, ok := (<-results).(Owned)
			require.True(t, ok, "concurrent launch was not owned")
		}
	}

	observedMu.Lock()
	firstCell, secondCell := cells[attemptIdentity(first.Attempt)], cells[attemptIdentity(second.Attempt)]
	firstGeneration := generations[attemptIdentity(first.Attempt)]
	secondGeneration := generations[attemptIdentity(second.Attempt)]
	firstCalls := nativeCalls[attemptIdentity(first.Attempt)]
	secondCalls := nativeCalls[attemptIdentity(second.Attempt)]
	observedMu.Unlock()
	assert.NotNil(t, firstCell, "cells/generations/native calls = %p/%p %d/%d %d/%d", firstCell, secondCell, firstGeneration, secondGeneration, firstCalls, secondCalls)
	assert.NotNil(t, secondCell, "cells/generations/native calls = %p/%p %d/%d %d/%d", firstCell, secondCell, firstGeneration, secondGeneration, firstCalls, secondCalls)
	assert.NotEqual(t, secondCell, firstCell, "cells/generations/native calls = %p/%p %d/%d %d/%d", firstCell, secondCell, firstGeneration, secondGeneration, firstCalls, secondCalls)
	assert.NotEqual(t, 0, firstGeneration, "cells/generations/native calls = %p/%p %d/%d %d/%d", firstCell, secondCell, firstGeneration, secondGeneration, firstCalls, secondCalls)
	assert.NotEqual(t, 0, secondGeneration, "cells/generations/native calls = %p/%p %d/%d %d/%d", firstCell, secondCell, firstGeneration, secondGeneration, firstCalls, secondCalls)
	assert.NotEqual(t, secondGeneration, firstGeneration, "cells/generations/native calls = %p/%p %d/%d %d/%d", firstCell, secondCell, firstGeneration, secondGeneration, firstCalls, secondCalls)
	assert.EqualValues(t, 1, firstCalls, "cells/generations/native calls = %p/%p %d/%d %d/%d", firstCell, secondCell, firstGeneration, secondGeneration, firstCalls, secondCalls)
	assert.EqualValues(t, 1, secondCalls, "cells/generations/native calls = %p/%p %d/%d %d/%d", firstCell, secondCell, firstGeneration, secondGeneration, firstCalls, secondCalls)
	assert.EqualValues(t, "go", first.Command[0], "caller snapshots crossed: first=%v/%v second=%v/%v", first.Command, first.Env, second.Command, second.Env)
	assert.EqualValues(t, "GOMAXPROCS=1", first.Env[0], "caller snapshots crossed: first=%v/%v second=%v/%v", first.Command, first.Env, second.Command, second.Env)
	assert.EqualValues(t, "go", second.Command[0], "caller snapshots crossed: first=%v/%v second=%v/%v", first.Command, first.Env, second.Command, second.Env)
	assert.EqualValues(t, "GOMAXPROCS=2", second.Env[0], "caller snapshots crossed: first=%v/%v second=%v/%v", first.Command, first.Env, second.Command, second.Env)
	snapshot := shell.Image()
	assert.True(t, snapshot.Open(), "concurrent launch runtime = %#v", snapshot)
	require.EqualValues(t, 2, snapshot.AdmissionCount(), "concurrent launch runtime = %#v", snapshot)
	assert.True(t, snapshot.Owned(firstGeneration), "concurrent launch runtime = %#v", snapshot)
	assert.True(t, snapshot.Owned(secondGeneration), "concurrent launch runtime = %#v", snapshot)
}

func TestSupervisorOwnedAttemptWaitIsIdempotentAndStopIsConcurrent(t *testing.T) {
	waitEntered := make(chan struct{})
	releaseWait := make(chan struct{})
	stopped := make(chan StopRequest, 1)
	waits := 0
	var attempt *OwnedAttempt
	attempt = newOwnedAttempt(
		func(request StopRequest) { stopped <- request },
		func() Terminal {
			waits++
			close(waitEntered)
			<-releaseWait
			attempt.sealStopAdmission()

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

	{
		got := <-stopped
		assert.Equal(t, request, got, "stop = %#v, want %#v", got, request)
	}
	first, second := <-results, <-results
	assert.EqualValues(t, 1, waits, "waits/results = %d/%#v/%#v", waits, first, second)
	assert.Equal(t, second, first, "waits/results = %d/%#v/%#v", waits, first, second)
}

func newOwnedAttemptForTest() *OwnedAttempt {
	attempt := newOwnedAttempt(func(StopRequest) {}, func() Terminal {
		return Stopped{}
	})
	attempt.sealStopAdmission()

	return attempt
}

func assertPanicsWith(t *testing.T, target error, action func()) {
	t.Helper()
	defer func() {
		recovered := recover()
		err, ok := recovered.(error)
		require.True(t, ok, "panic = %#v, want error matching %v", recovered, target)
		assert.ErrorIs(t, err, target, "panic = %#v, want error matching %v", recovered, target)
	}()
	action()
}
