package ooze

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/viruses"
	"github.com/gtramontina/ooze/viruses/integerincrement"
)

func TestManagedCampaignEmptyCatalogueRunsNoCommands(t *testing.T) {
	repository := &managedMemoryRepository{}
	attempts := &managedAttemptFixture{}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: newProcessRuntimeShell(2), repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 2, viruses: []viruses.Virus{},
	})

	if _, ok := result.outcome.(noMutantsOutcome); !ok {
		t.Fatalf("outcome = %#v, want NoMutants", result.outcome)
	}
	if attempts.launches != 0 || repository.materializations != 1 || !repository.snapshot.removed {
		t.Fatalf("launches/materializations/snapshot = %d/%d/%#v", attempts.launches, repository.materializations, repository.snapshot)
	}
}

func TestManagedCampaignLazilyOverlapsAutomaticPrimariesUpToCapacity(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar first = 0\nvar second = 0\n")),
	}}
	started := make(chan Spec, 2)
	release := make(chan struct{})
	attempts := &managedAttemptFixture{
		waitStarted: started, releasePrimaries: release,
		terminals: []Terminal{
			Settled{Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
			Settled{Exit: ExitStatus{Code: 1}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
			Settled{Exit: ExitStatus{Code: 1}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
		},
	}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: newProcessRuntimeShell(2), repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})
	completed := make(chan managedCampaignResult, 1)
	go func() {
		completed <- runner.run(managedCampaignRequest{
			identity: "campaign-a", lineage: 11, command: []string{"test"},
			profile: AutomaticProfile, peers: 2, viruses: []viruses.Virus{integerincrement.New()},
		})
	}()

	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("automatic primaries did not overlap before settlement")
		}
	}
	close(release)
	select {
	case result := <-completed:
		outcome, ok := result.outcome.(completedOutcome)
		if !ok || len(outcome.mutants) != 2 {
			t.Fatalf("outcome = %#v", result.outcome)
		}
	case <-time.After(time.Second):
		t.Fatal("overlapped campaign did not complete")
	}
}

func TestManagedCampaignSerialPrimariesAreExclusiveWithDetectedCapacityProfile(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar first = 0\nvar second = 0\n")),
	}}
	started := make(chan Spec, 2)
	release := make(chan struct{})
	attempts := &managedAttemptFixture{
		waitStarted: started, releasePrimaries: release,
		terminals: []Terminal{
			Settled{Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
			Settled{Exit: ExitStatus{Code: 1}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
			Settled{Exit: ExitStatus{Code: 1}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
		},
	}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: newProcessRuntimeShell(3), repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})
	completed := make(chan managedCampaignResult, 1)
	go func() {
		completed <- runner.run(managedCampaignRequest{
			identity: "campaign-a", lineage: 11, command: []string{"test"},
			profile: SerialProfile, peers: 3, viruses: []viruses.Virus{integerincrement.New()},
		})
	}()

	first := <-started
	if !slices.Contains(first.Env, "GOMAXPROCS=3") {
		t.Fatalf("serial environment = %#v", first.Env)
	}
	select {
	case second := <-started:
		close(release)
		t.Fatalf("serial primary overlapped: %#v", second)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case result := <-completed:
		if outcome, ok := result.outcome.(completedOutcome); !ok || len(outcome.mutants) != 2 {
			t.Fatalf("outcome = %#v", result.outcome)
		}
	case <-time.After(time.Second):
		t.Fatal("serial campaign did not complete")
	}
}

func TestManagedCampaignRunsBaselineBeforeOneAutomaticPrimary(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}
	attempts := &managedAttemptFixture{terminals: []Terminal{
		Settled{Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: 2 * time.Second}},
		Settled{Exit: ExitStatus{Code: 1}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
	}}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: newProcessRuntimeShell(2), repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 2, viruses: []viruses.Virus{integerincrement.New()},
	})

	completed, ok := result.outcome.(completedOutcome)
	if !ok || len(completed.mutants) != 1 || completed.mutants[0].kind != mutantKilled {
		t.Fatalf("outcome = %#v, want one killed mutant", result.outcome)
	}
	if attempts.launches != 2 || len(attempts.specs) != 2 {
		t.Fatalf("launches/specs = %d/%#v", attempts.launches, attempts.specs)
	}
	for _, spec := range attempts.specs {
		if !slices.Contains(spec.Env, "GOMAXPROCS=1") {
			t.Fatalf("automatic spec environment = %#v", spec.Env)
		}
	}
}

func TestManagedCampaignStopsOwnedPeerAndWaitsForSettlementBeforeAbort(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar first = 0\nvar second = 0\n")),
	}}
	attempts := &managedAttemptFixture{
		stopRelease: make(chan struct{}),
		terminals: []Terminal{
			Settled{Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
			Infrastructure{Cause: CensusFailed, Err: errors.New("census failed")},
			Stopped{},
		},
	}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: newProcessRuntimeShell(2), repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 2, viruses: []viruses.Virus{integerincrement.New()},
	})

	if _, ok := result.outcome.(abortedOutcome); !ok || attempts.stops != 1 {
		t.Fatalf("outcome/stops = %#v/%d, want aborted after one peer stop", result.outcome, attempts.stops)
	}
}

func TestManagedCampaignSettlesRuntimeEmergencyBeforeCleanupFailure(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}
	attempts := &managedAttemptFixture{terminals: []Terminal{
		Settled{Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
		DrainUnconfirmed{Residual: OwnedUndrained},
	}}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: newProcessRuntimeShell(1), repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 1, viruses: []viruses.Virus{integerincrement.New()},
	})

	if _, ok := result.failure.(cleanupUnconfirmedFault); !ok || attempts.emergencies != 1 {
		t.Fatalf("failure/emergencies = %#v/%d, want cleanup failure after one emergency", result.failure, attempts.emergencies)
	}
}

type managedMemoryRepository struct {
	files            []*gosourcefile.GoSourceFile
	materializations int
	snapshot         *managedMemoryTemporaryRepository
}

func (r *managedMemoryRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	return append([]*gosourcefile.GoSourceFile(nil), r.files...)
}

func (r *managedMemoryRepository) MaterializeTemporaryRepository(path string) TemporaryRepository {
	r.materializations++
	r.snapshot = &managedMemoryTemporaryRepository{root: path, files: r.ListGoSourceFiles()}

	return r.snapshot
}

type managedMemoryTemporaryRepository struct {
	root    string
	files   []*gosourcefile.GoSourceFile
	removed bool
}

func (r *managedMemoryTemporaryRepository) Root() string { return r.root }
func (r *managedMemoryTemporaryRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	return append([]*gosourcefile.GoSourceFile(nil), r.files...)
}
func (r *managedMemoryTemporaryRepository) MaterializeTemporaryRepository(path string) TemporaryRepository {
	return &managedMemoryTemporaryRepository{root: path, files: r.ListGoSourceFiles()}
}
func (r *managedMemoryTemporaryRepository) Overwrite(string, []byte) {}
func (r *managedMemoryTemporaryRepository) Remove()                  { r.removed = true }

type managedTemporaryDirectory struct{ next int }

func (d *managedTemporaryDirectory) New() string {
	d.next++

	return "temporary-" + time.Unix(int64(d.next), 0).Format("150405")
}

type managedAttemptFixture struct {
	mutex            sync.Mutex
	launches         int
	specs            []Spec
	terminals        []Terminal
	shell            *processRuntimeShell
	byGeneration     map[attemptGeneration]Spec
	terminalByGen    map[attemptGeneration]Terminal
	waitStarted      chan Spec
	releasePrimaries <-chan struct{}
	stopRelease      chan struct{}
	stops            int
	emergencies      int
	drainGeneration  attemptGeneration
}

func (f *managedAttemptFixture) launch(start installedStart, spec Spec) managedObservedLaunch {
	f.mutex.Lock()
	f.launches++
	f.specs = append(f.specs, spec)
	f.shell = start.shell
	if f.byGeneration == nil {
		f.byGeneration = make(map[attemptGeneration]Spec)
		f.terminalByGen = make(map[attemptGeneration]Terminal)
	}
	f.byGeneration[start.generation] = spec
	f.terminalByGen[start.generation] = f.terminals[f.launches-1]
	f.mutex.Unlock()
	var result LaunchResult
	observed := start.launch(func(attemptGeneration) attemptObservation {
		result = Owned{Attempt: newOwnedAttempt(func(StopRequest) {}, func() Terminal { return nil })}

		return launchOwned{}
	})
	receipt := start.shell.observeAttempt(start.generation, observed)

	return managedObservedLaunch{result: result, receipt: receipt}
}
func (f *managedAttemptFixture) wait(generation attemptGeneration, _ *OwnedAttempt) managedObservedTerminal {
	f.mutex.Lock()
	terminal := f.terminalByGen[generation]
	spec := f.byGeneration[generation]
	f.mutex.Unlock()
	if f.waitStarted != nil && spec.Deadline != baselineBootstrapDeadline {
		f.waitStarted <- spec
		<-f.releasePrimaries
	}
	data := terminalExecutionData(terminal)
	data.Deadline = spec.Deadline
	data.profile = spec.Profile
	switch terminal := terminal.(type) {
	case Settled:
		terminal.ExecutionData = data
		terminal = terminal
		return managedObservedTerminal{
			terminal: terminal,
			receipt:  f.shell.observeAttempt(generation, attemptSettled{profile: spec.Profile, deadline: spec.Deadline}),
		}
	case Infrastructure:
		terminal.ExecutionData = data

		return managedObservedTerminal{
			terminal: terminal,
			receipt:  f.shell.observeAttempt(generation, attemptInfrastructure{cause: terminal.Err.Error()}),
		}
	case Stopped:
		if f.stopRelease != nil {
			<-f.stopRelease
		}
		terminal.ExecutionData = data

		return managedObservedTerminal{
			terminal: terminal,
			receipt:  f.shell.observeAttempt(generation, attemptStopped{}),
		}
	case DrainUnconfirmed:
		terminal.ExecutionData = data
		f.mutex.Lock()
		f.drainGeneration = generation
		f.mutex.Unlock()

		return managedObservedTerminal{
			terminal: terminal,
			receipt:  f.shell.observeAttempt(generation, drainUnconfirmed{}),
		}
	default:
		panic("unsupported fixture terminal")
	}
}
func (f *managedAttemptFixture) stop(attemptGeneration) {
	f.mutex.Lock()
	f.stops++
	close(f.stopRelease)
	f.mutex.Unlock()
}
func (f *managedAttemptFixture) emergency(epoch fatalEpochID) managedObservedEmergency {
	f.mutex.Lock()
	f.emergencies++
	generation := f.drainGeneration
	f.mutex.Unlock()
	settlement := f.shell.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
		generation: generation, disposition: emergencyCustodyTransferred,
	}}})

	return managedObservedEmergency{epoch: epoch, settlement: settlement}
}
