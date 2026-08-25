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

func TestManagedCampaignPropagatesAbsoluteTimeoutAndRetainsTimedOutResult(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}
	attempts := &managedAttemptFixture{terminals: []Terminal{
		Settled{Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
		Tripped{Trip: AutomaticDeadlineTrip{}},
	}}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: newProcessRuntimeShell(1), repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 1, mutationTimeout: 37 * time.Millisecond,
		viruses: []viruses.Virus{integerincrement.New()},
	})

	completed, ok := result.outcome.(completedOutcome)
	if !ok || len(completed.mutants) != 1 || completed.mutants[0].kind != mutantTimedOut {
		t.Fatalf("outcome = %#v, want one timed-out mutant", result.outcome)
	}
	if len(attempts.specs) != 2 || attempts.specs[1].Deadline != 37*time.Millisecond {
		t.Fatalf("attempt specs = %#v, want exact 37ms primary deadline", attempts.specs)
	}
}

func TestManagedCampaignConfirmsOverlapDeadlineAndTransitionsFutureAdmission(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar first = 0\nvar second = 0\n")),
	}}
	launchesReady := make(chan struct{})
	attempts := &managedAttemptFixture{
		waitForLaunches: 3, launchesReady: launchesReady,
		terminals: []Terminal{
			Settled{Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
			Tripped{Trip: AutomaticDeadlineTrip{}},
			Settled{Exit: ExitStatus{Code: 1}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
			Settled{Exit: ExitStatus{Code: 1}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
		},
	}
	shell := newProcessRuntimeShell(2)
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: shell, repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 2, mutationTimeout: time.Minute,
		viruses: []viruses.Virus{integerincrement.New()},
	})

	completed, ok := result.outcome.(completedOutcome)
	if !ok || len(completed.mutants) != 2 || completed.mutants[0].kind != mutantKilled ||
		completed.mutants[1].kind != mutantKilled {
		t.Fatalf("outcome = %#v, want two killed mutants after confirmation", result.outcome)
	}
	if attempts.launches != 4 || shell.snapshot().mode != singleAdmission {
		t.Fatalf("launches/mode = %d/%v, want one confirmation and single admission", attempts.launches, shell.snapshot().mode)
	}
}

func TestManagedCampaignAbortsResourceExhaustionAndTransitionsFutureAdmission(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}
	attempts := &managedAttemptFixture{
		notReleasedAt: 2,
		terminals: []Terminal{
			Settled{Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
		},
	}
	shell := newProcessRuntimeShell(2)
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: shell, repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 2, viruses: []viruses.Virus{integerincrement.New()},
	})

	if _, ok := result.outcome.(abortedOutcome); !ok || shell.snapshot().mode != singleAdmission ||
		attempts.launches != 2 {
		t.Fatalf("outcome/mode/launches = %#v/%v/%d", result.outcome, shell.snapshot().mode, attempts.launches)
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

func TestManagedCampaignAuthorizesForcedAbortAfterEmptyEmergencySweep(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}
	attempts := &managedAttemptFixture{shell: shell, emergencyEmpty: true}
	started := make(chan struct{})
	release := make(chan struct{})
	temporaryDirectory := &blockingManagedTemporaryDirectory{
		managedTemporaryDirectory: managedTemporaryDirectory{}, started: started, release: release,
	}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: shell, repository: repository, temporaryDirectory: temporaryDirectory, attempts: attempts,
	})
	go func() {
		<-started
		shell.closeRuntime("peer fatal without custody")
		close(release)
	}()

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 1, viruses: []viruses.Virus{integerincrement.New()},
	})

	if _, ok := result.outcome.(abortedOutcome); !ok || result.failure != nil {
		t.Fatalf("outcome/failure = %#v/%#v, want forced Aborted", result.outcome, result.failure)
	}
}

func TestManagedCampaignNormalizesSnapshotBoundaryPanic(t *testing.T) {
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: newProcessRuntimeShell(1), repository: managedPanickingRepository{},
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: &managedAttemptFixture{},
	})

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 1, viruses: []viruses.Virus{integerincrement.New()},
	})

	if outcome, ok := result.outcome.(abortedOutcome); !ok || outcome.cause != "snapshot exploded" {
		t.Fatalf("boundary outcome = %#v, want typed snapshot abort", result.outcome)
	}
}

func TestManagedCampaignCleansWorkspaceAcquiredBeforeMutationPanic(t *testing.T) {
	repository := &managedPartialWorkspaceRepository{}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: newProcessRuntimeShell(1), repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: &managedAttemptFixture{
			terminals: []Terminal{Settled{
				Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second},
			}},
		},
	})

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 1, viruses: []viruses.Virus{integerincrement.New()},
	})

	if _, ok := result.outcome.(abortedOutcome); !ok || repository.workspace == nil || !repository.workspace.removed {
		t.Fatalf("outcome/workspace = %#v/%#v, want aborted with acquired workspace removed",
			result.outcome, repository.workspace)
	}
}

func TestManagedCampaignConsumesFatalEpochWhileWaitingForAdmission(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	heldCampaign := shell.registerCampaign(campaignProvenance{lineage: 99})
	held := shell.requestAdmission(admissionRequest{
		campaign: heldCampaign.token, attempt: "held", class: exclusiveAdmission,
		profile: AutomaticProfile, deadline: baselineBootstrapDeadline,
	})
	heldGrant := <-held.delivery
	prepared := shell.startCommitted(heldGrant, startInstallation{grant: heldGrant, cell: &pendingStartCell{}})
	prepared.start.launch(func(attemptGeneration) attemptObservation { return launchOwned{} })
	shell.observeAttempt(prepared.result.generation, launchOwned{})

	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}
	attempts := &managedAttemptFixture{shell: shell, emergencyGeneration: prepared.result.generation}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: shell, repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})
	go func() {
		deadline := time.Now().Add(time.Second)
		for len(shell.snapshot().admissions) < 2 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		shell.observeAttempt(prepared.result.generation, drainUnconfirmed{})
	}()

	result := runner.run(managedCampaignRequest{
		identity: "campaign-waiting", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 1, viruses: []viruses.Virus{integerincrement.New()},
	})

	if _, ok := result.outcome.(abortedOutcome); !ok || result.failure != nil || attempts.emergencies != 1 {
		t.Fatalf("outcome/failure/emergencies = %#v/%#v/%d, want non-owner forced abort",
			result.outcome, result.failure, attempts.emergencies)
	}
}

type managedMemoryRepository struct {
	files            []*gosourcefile.GoSourceFile
	materializations int
	snapshot         *managedMemoryTemporaryRepository
}

type managedPanickingRepository struct{}

func (managedPanickingRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile { return nil }
func (managedPanickingRepository) MaterializeTemporaryRepository(string) TemporaryRepository {
	panic("snapshot exploded")
}

type managedPartialWorkspaceRepository struct {
	snapshot  *managedPartialSnapshot
	workspace *managedPartialWorkspace
}

func (r *managedPartialWorkspaceRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	return []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}
}
func (r *managedPartialWorkspaceRepository) MaterializeTemporaryRepository(path string) TemporaryRepository {
	r.snapshot = &managedPartialSnapshot{owner: r, root: path}
	return r.snapshot
}

type managedPartialSnapshot struct {
	owner *managedPartialWorkspaceRepository
	root  string
}

func (r *managedPartialSnapshot) Root() string { return r.root }
func (r *managedPartialSnapshot) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	return r.owner.ListGoSourceFiles()
}
func (r *managedPartialSnapshot) MaterializeTemporaryRepository(path string) TemporaryRepository {
	r.owner.workspace = &managedPartialWorkspace{root: path}
	return r.owner.workspace
}
func (*managedPartialSnapshot) Overwrite(string, []byte) {}
func (*managedPartialSnapshot) Remove()                  {}

type managedPartialWorkspace struct {
	root    string
	removed bool
}

func (r *managedPartialWorkspace) Root() string                                  { return r.root }
func (*managedPartialWorkspace) ListGoSourceFiles() []*gosourcefile.GoSourceFile { return nil }
func (*managedPartialWorkspace) MaterializeTemporaryRepository(string) TemporaryRepository {
	panic("nested workspace is invalid")
}
func (*managedPartialWorkspace) Overwrite(string, []byte) { panic("mutation write exploded") }
func (r *managedPartialWorkspace) Remove()                { r.removed = true }

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

type blockingManagedTemporaryDirectory struct {
	managedTemporaryDirectory
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (d *blockingManagedTemporaryDirectory) New() string {
	d.once.Do(func() {
		close(d.started)
		<-d.release
	})

	return d.managedTemporaryDirectory.New()
}

type managedAttemptFixture struct {
	mutex               sync.Mutex
	launches            int
	specs               []Spec
	terminals           []Terminal
	shell               *processRuntimeShell
	byGeneration        map[attemptGeneration]Spec
	terminalByGen       map[attemptGeneration]Terminal
	waitStarted         chan Spec
	releasePrimaries    <-chan struct{}
	waitAll             <-chan struct{}
	launchStarted       chan Spec
	waitForLaunches     int
	launchesReady       chan struct{}
	launchesReadyOnce   sync.Once
	notReleasedAt       int
	stopRelease         chan struct{}
	stops               int
	emergencies         int
	drainGeneration     attemptGeneration
	emergencyGeneration attemptGeneration
	emergencyEmpty      bool
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
	if f.launches <= len(f.terminals) {
		f.terminalByGen[start.generation] = f.terminals[f.launches-1]
	}
	if f.waitForLaunches != 0 && f.launches >= f.waitForLaunches {
		f.launchesReadyOnce.Do(func() { close(f.launchesReady) })
	}
	f.mutex.Unlock()
	if f.launchStarted != nil {
		f.launchStarted <- spec
	}
	var result LaunchResult
	if f.notReleasedAt == f.launches {
		observed := start.launch(func(attemptGeneration) attemptObservation {
			result = NotReleased{Kind: LaunchResourceExhausted}

			return launchNotReleased{reason: launchResourceExhausted}
		})
		receipt := start.shell.observeAttempt(start.generation, observed)

		return managedObservedLaunch{result: result, receipt: receipt}
	}
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
	if f.waitAll != nil {
		<-f.waitAll
	}
	if f.waitForLaunches != 0 && spec.Deadline != baselineBootstrapDeadline {
		<-f.launchesReady
	}
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
	case Tripped:
		terminal.ExecutionData = data
		terminal.BoundFired = CommandDeadlineFired

		return managedObservedTerminal{
			terminal: terminal,
			receipt: f.shell.observeAttempt(generation, attemptTripped{
				kind: deadlineTrip, profile: spec.Profile, deadline: spec.Deadline,
			}),
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
	if f.emergencyGeneration != 0 {
		generation = f.emergencyGeneration
	}
	f.mutex.Unlock()
	if f.emergencyEmpty {
		settlement := f.shell.settleEmergency(emergencySweep{})

		return managedObservedEmergency{epoch: epoch, settlement: settlement}
	}
	settlement := f.shell.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
		generation: generation, disposition: emergencyCustodyTransferred,
	}}})

	return managedObservedEmergency{epoch: epoch, settlement: settlement}
}
