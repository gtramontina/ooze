package ooze

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Profile fixes the supervision strategy before an attempt starts.
type Profile uint8

const (
	// AutomaticProfile enables automatic process-fuse supervision.
	AutomaticProfile Profile = iota + 1
	// SerialProfile excludes process-fuse supervision.
	SerialProfile
)

// Spec contains only caller-resolved facts for one managed attempt.
type Spec struct {
	Attempt  string
	Command  []string
	Dir      string
	Env      []string
	Profile  Profile
	Deadline time.Duration
}

// ErrInvalidSpec identifies a malformed managed-attempt specification.
var ErrInvalidSpec = errors.New("invalid attempt spec")

func (s Spec) validate() error {
	switch {
	case s.Attempt == "":
		return fmt.Errorf("%w: attempt identity is required", ErrInvalidSpec)
	case len(s.Command) == 0 || s.Command[0] == "":
		return fmt.Errorf("%w: command is required", ErrInvalidSpec)
	case s.Profile != AutomaticProfile && s.Profile != SerialProfile:
		return fmt.Errorf("%w: profile is invalid", ErrInvalidSpec)
	case s.Deadline <= 0:
		return fmt.Errorf("%w: resolved command deadline must be positive", ErrInvalidSpec)
	default:
		return nil
	}
}

func (s Spec) snapshot() Spec {
	s.Command = append([]string(nil), s.Command...)
	s.Env = append([]string(nil), s.Env...)

	return s
}

// LaunchFailure distinguishes ordinary launch failure from normalized launch
// resource exhaustion.
type LaunchFailure uint8

const (
	// LaunchFailed means the command was proved not released after an ordinary failure.
	LaunchFailed LaunchFailure = iota + 1
	// LaunchResourceExhausted means an eligible primary launch operation exhausted resources.
	LaunchResourceExhausted
)

// Residual identifies retained native custody after a bounded drain.
type Residual uint8

const (
	// ProspectiveUnresolved means target release could not be confirmed or excluded.
	ProspectiveUnresolved Residual = iota + 1
	// OwnedUndrained means a released attempt could not be proved empty.
	OwnedUndrained
)

// LaunchResult closes an accepted prospective start in exactly one of three ways.
type LaunchResult interface{ launchResult() }

// Owned transfers an attempt capability after release inside containment.
type Owned struct{ Attempt *OwnedAttempt }

// NotReleased proves that target code did not run.
type NotReleased struct {
	Kind LaunchFailure
	Err  error
}

// LaunchUnconfirmed retains prospective custody when release remains unknown.
type LaunchUnconfirmed struct{ Residual Residual }

func (Owned) launchResult()             {}
func (NotReleased) launchResult()       {}
func (LaunchUnconfirmed) launchResult() {}

// ErrUnsupportedPlatform is returned before admission or native work on an
// unsupported build target.
var ErrUnsupportedPlatform = errors.New("managed attempts are unsupported on this platform")

type supervisorConstruction struct {
	supported    bool
	installStart func(attemptIdentity, *pendingStartCell) installedStart
	launchNative func(attemptGeneration, Spec) LaunchResult
}

// Supervisor is the concrete entry point for managed command execution.
type Supervisor struct {
	installStart   func(attemptIdentity, *pendingStartCell) installedStart
	launchNative   func(attemptGeneration, Spec) LaunchResult
	driveLaunch    func(installedStart, Spec) LaunchResult
	emergencyDrain func(EmergencyRequest) SweepResult
}

func constructSupervisor(construction supervisorConstruction) (*Supervisor, error) {
	if !construction.supported {
		return nil, ErrUnsupportedPlatform
	}
	if construction.installStart == nil || construction.launchNative == nil {
		return nil, errors.New("supervisor construction requires start and native launch plumbing")
	}

	return &Supervisor{
		installStart: construction.installStart,
		launchNative: construction.launchNative,
	}, nil
}

func newSupervisorForTest(
	installStart func(attemptIdentity, *pendingStartCell) installedStart,
	launchNative func(attemptGeneration, Spec) LaunchResult,
) *Supervisor {
	supervisor, err := constructSupervisor(supervisorConstruction{
		supported: true, installStart: installStart, launchNative: launchNative,
	})
	if err != nil {
		panic(err)
	}

	return supervisor
}

// Launch validates and snapshots caller facts, registers the exact prospective
// generation, and only then invokes native launch mechanics after unlock.
func (s *Supervisor) Launch(spec Spec) LaunchResult {
	if err := spec.validate(); err != nil {
		panic(err)
	}
	snapshot := spec.snapshot()
	pendingStart := pendingStartCell{}
	start := s.installStart(attemptIdentity(snapshot.Attempt), &pendingStart)
	if s.driveLaunch != nil {
		return s.driveLaunch(start, snapshot)
	}
	var result LaunchResult
	observed := start.launch(func(generation attemptGeneration) attemptObservation {
		result = s.launchNative(generation, snapshot)

		return brokerLaunchObservation(result)
	})
	start.shell.observeAttempt(start.generation, observed)

	return result
}

// EmergencyDrain runs one exact runtime-wide emergency settlement.
func (s *Supervisor) EmergencyDrain(request EmergencyRequest) SweepResult {
	if err := request.validate(); err != nil {
		panic(err)
	}
	if s.emergencyDrain == nil {
		panic("supervisor emergency drain plumbing is absent")
	}

	return s.emergencyDrain(request)
}

func brokerLaunchObservation(result LaunchResult) attemptObservation {
	switch result := result.(type) {
	case Owned:
		if result.Attempt == nil {
			return nil
		}

		return launchOwned{}
	case NotReleased:
		switch result.Kind {
		case LaunchFailed:
			return launchNotReleased{reason: launchFailed}
		case LaunchResourceExhausted:
			return launchNotReleased{reason: launchResourceExhausted}
		default:
			return nil
		}
	case LaunchUnconfirmed:
		if result.Residual != ProspectiveUnresolved {
			return nil
		}

		return launchUnconfirmed{}
	default:
		return nil
	}
}

// StopRequest separates the terminal fact time from the absolute drainage bound.
type StopRequest struct {
	At      time.Time
	DrainBy time.Time
}

// EmergencyRequest fixes the logical emergency cut and its drainage bound.
type EmergencyRequest struct {
	At      time.Time
	DrainBy time.Time
}

func (request EmergencyRequest) validate() error {
	return StopRequest(request).validate()
}

// SweepResult is the stable settlement of one emergency epoch.
type SweepResult interface{ sweepResult() }

// SweepDrained reports that the emergency epoch retained no residual custody.
type SweepDrained struct{}

// ResidualRef identifies one stable ordered emergency residual.
type ResidualRef struct {
	Attempt string
	Kind    Residual
}

// SweepUnconfirmed reports immutable ordered residual custody.
type SweepUnconfirmed struct{ residuals []ResidualRef }

// Residuals returns a defensive copy of the ordered residual inventory.
func (settlement SweepUnconfirmed) Residuals() []ResidualRef {
	return append([]ResidualRef(nil), settlement.residuals...)
}

func (SweepDrained) sweepResult()     {}
func (SweepUnconfirmed) sweepResult() {}

var errInvalidStopRequest = errors.New("stop request requires an instant and a later drain deadline")

func (r StopRequest) validate() error {
	if r.At.IsZero() || !r.DrainBy.After(r.At) {
		return errInvalidStopRequest
	}

	return nil
}

// ExecutionData is the evidence common to every owned terminal.
type ExecutionData struct {
	Deadline        time.Duration
	LaunchDuration  time.Duration
	CommandDuration time.Duration
	BoundFired      BoundFired
	Output          OutputSnapshot
	Failures        FailureDiagnostics

	profile                 Profile
	confirmationProvisional bool
}

// BoundFired identifies the command bound that selected terminal intent.
type BoundFired uint8

const (
	// NoBoundFired means an observed fact selected terminal intent.
	NoBoundFired BoundFired = iota
	// CommandDeadlineFired means the resolved command deadline selected intent.
	CommandDeadlineFired
)

// OutputSnapshot is one immutable prefix of the private merged-output file.
type OutputSnapshot struct {
	Bytes                 string
	Cutoff                uint64
	CompleteThroughCutoff bool
	Final                 bool
}

// FailureDiagnostics retains independent supervision failures.
type FailureDiagnostics struct {
	Wait          string
	RunningCensus string
	DrainCensus   string
	Termination   string
	Output        string
	Release       string
}

// ExitStatus is the native root's normalized exit evidence.
type ExitStatus struct {
	Code   int
	Signal int
}

// Passed reports an ordinary zero exit.
func (status ExitStatus) Passed() bool { return status.Code == 0 && status.Signal == 0 }

// Terminal is the immutable result returned by an owned attempt.
type Terminal interface{ terminal() }

// Settled reports an ordinary root exit after authoritative drainage.
type Settled struct {
	Exit ExitStatus
	ExecutionData
}

// Trip distinguishes fuse and deadline terminal evidence.
type Trip interface{ trip() }

// FuseTrip records the live descendant count that crossed the fuse.
type FuseTrip struct{ Live int }

// ObservedCount preserves whether an automatic running count was observed.
type ObservedCount struct {
	Value   int
	Present bool
}

// AutomaticDeadlineTrip records the observed running peak, if any.
type AutomaticDeadlineTrip struct{ Peak ObservedCount }

// SerialDeadlineTrip carries no fabricated count.
type SerialDeadlineTrip struct{}

func (FuseTrip) trip()              {}
func (AutomaticDeadlineTrip) trip() {}
func (SerialDeadlineTrip) trip()    {}

// Tripped reports a process-fuse or command-deadline trip.
type Tripped struct {
	Trip Trip
	ExecutionData
}

// Stopped reports that Stop supplied the earliest running intent.
type Stopped struct{ ExecutionData }

// Cause identifies the stable presentation cause of infrastructure failure.
type Cause uint8

const (
	CensusFailed Cause = iota + 1
	WaitFailed
	TerminationControlFailed
	OutputCaptureFailed
	ReleaseFailed
)

// Infrastructure reports a supervision failure after authoritative drainage.
type Infrastructure struct {
	Cause Cause
	Err   error
	ExecutionData
}

// DrainUnconfirmed reports retained owned custody without an emptiness proof.
type DrainUnconfirmed struct {
	Residual Residual
	ExecutionData
}

func (Settled) terminal()          {}
func (Tripped) terminal()          {}
func (Stopped) terminal()          {}
func (Infrastructure) terminal()   {}
func (DrainUnconfirmed) terminal() {}

// OwnedAttempt is the opaque capability for one released, contained attempt.
type OwnedAttempt struct {
	stop func(StopRequest)
	wait func() Terminal

	stateMu       sync.Mutex
	stateChanged  *sync.Cond
	stopSealed    bool
	stopsInFlight int

	waitOnce sync.Once
	terminal Terminal
}

func newOwnedAttempt(stop func(StopRequest), wait func() Terminal) *OwnedAttempt {
	if stop == nil || wait == nil {
		panic("owned attempt requires stop and wait plumbing")
	}
	attempt := &OwnedAttempt{stop: stop, wait: wait}
	attempt.stateChanged = sync.NewCond(&attempt.stateMu)

	return attempt
}

// Stop submits a timestamped stop fact until terminal delivery seals admission.
func (a *OwnedAttempt) Stop(request StopRequest) {
	if err := request.validate(); err != nil {
		panic(err)
	}
	a.stateMu.Lock()
	if a.stopSealed {
		a.stateMu.Unlock()

		return
	}
	a.stopsInFlight++
	a.stateMu.Unlock()

	defer func() {
		a.stateMu.Lock()
		a.stopsInFlight--
		a.stateChanged.Broadcast()
		a.stateMu.Unlock()
	}()
	a.stop(request)
}

// Wait joins supervision and returns the same immutable terminal to every caller.
func (a *OwnedAttempt) Wait() Terminal {
	a.waitOnce.Do(func() {
		terminal := a.wait()
		if terminal == nil {
			panic("owned attempt wait returned no terminal")
		}
		a.stateMu.Lock()
		sealed := a.stopSealed
		a.stateMu.Unlock()
		if !sealed {
			panic("owned attempt wait returned before sealing stop admission")
		}
		a.terminal = terminal
	})

	return a.terminal
}

func (a *OwnedAttempt) sealStopAdmission() {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.stopSealed {
		panic("owned attempt stop admission sealed twice")
	}
	a.stopSealed = true
	for a.stopsInFlight != 0 {
		a.stateChanged.Wait()
	}
}
