package supervision

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
)

// Spec describes one supervised command.
type Spec struct {
	Attempt  string
	Command  []string
	Dir      string
	Env      []string
	Profile  Profile
	Deadline time.Duration
}

// ErrInvalidSpec reports an incomplete command specification.
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

// Validate checks the complete attempt specification.
func (s Spec) Validate() error { return s.validate() }

// Snapshot returns a detached copy of the attempt specification.
func (s Spec) Snapshot() Spec { return s.snapshot() }

// Residual identifies custody that supervision could not prove drained.
type Residual uint8

const (
	ProspectiveUnresolved Residual = iota + 1
	OwnedUndrained
)

// LaunchResult is the closed set of launch outcomes.
type LaunchResult interface{ launchResult() }

// Owned transfers a launched attempt to the caller.
type Owned struct{ Attempt *OwnedAttempt }

// NotReleased reports a launch that provably did not cross release.
type NotReleased struct {
	Kind LaunchFailure
	Err  error
}

// LaunchUnconfirmed reports unresolved prospective custody.
type LaunchUnconfirmed struct{ Residual Residual }

func (Owned) launchResult()             {}
func (NotReleased) launchResult()       {}
func (LaunchUnconfirmed) launchResult() {}

// ErrUnsupportedPlatform reports missing native supervision support.
var ErrUnsupportedPlatform = errors.New("managed attempts are unsupported on this platform")

type supervisorConstruction struct {
	supported    bool
	installStart func(attemptIdentity, *processruntime.StartCell) processruntime.PreparedStart
	launchNative func(attemptGeneration, Spec) LaunchResult
}

// Supervisor binds process-runtime start custody to native launch.
type Supervisor struct {
	installStart   func(attemptIdentity, *processruntime.StartCell) processruntime.PreparedStart
	launchNative   func(attemptGeneration, Spec) LaunchResult
	reserveLaunch  func(*processruntime.StartCell, Spec)
	discardLaunch  func(*processruntime.StartCell)
	driveLaunch    func(processruntime.PreparedStart, Spec) LaunchResult
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
	installStart func(attemptIdentity, *processruntime.StartCell) processruntime.PreparedStart,
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

// Launch commits process-runtime custody before invoking native launch.
func (s *Supervisor) Launch(spec Spec) LaunchResult {
	if err := spec.validate(); err != nil {
		panic(err)
	}
	snapshot := spec.snapshot()
	pendingStart := processruntime.NewStartCell()
	if s.reserveLaunch != nil {
		s.reserveLaunch(pendingStart, snapshot)
	}
	start := s.installStart(attemptIdentity(snapshot.Attempt), pendingStart)
	if start.Generation() == 0 && s.discardLaunch != nil {
		s.discardLaunch(pendingStart)
	}
	if s.driveLaunch != nil {
		return s.driveLaunch(start, snapshot)
	}
	var result LaunchResult
	observed := start.Launch(func(generation processruntime.Generation) processruntime.Observation {
		result = s.launchNative(generation, snapshot)

		return brokerLaunchObservation(result)
	})
	start.Observe(observed)

	return result
}

// EmergencyDrain joins one bounded global drainage epoch.
func (s *Supervisor) EmergencyDrain(request EmergencyRequest) SweepResult {
	if err := request.validate(); err != nil {
		panic(err)
	}
	if s.emergencyDrain == nil {
		panic("supervisor emergency drain plumbing is absent")
	}

	return s.emergencyDrain(request)
}

func brokerLaunchObservation(result LaunchResult) processruntime.Observation {
	switch result := result.(type) {
	case Owned:
		if result.Attempt == nil {
			return processruntime.Observation{}
		}

		return processruntime.Owned()
	case NotReleased:
		switch result.Kind {
		case LaunchFailed:
			return processruntime.NotReleased(false)
		case LaunchResourceExhausted:
			return processruntime.NotReleased(true)
		default:
			return processruntime.Observation{}
		}
	case LaunchUnconfirmed:
		if result.Residual != ProspectiveUnresolved {
			return processruntime.Observation{}
		}

		return processruntime.LaunchUnconfirmed()
	default:
		return processruntime.Observation{}
	}
}

// EmergencyRequest bounds one global drainage epoch.
type EmergencyRequest struct {
	At      time.Time
	DrainBy time.Time
}

func (request EmergencyRequest) validate() error {
	return StopRequest(request).validate()
}

// SweepResult is the closed set of emergency drainage outcomes.
type SweepResult interface{ sweepResult() }

// SweepDrained proves that emergency drainage emptied all custody.
type SweepDrained struct{}

// ResidualRef identifies one undrained attempt.
type ResidualRef struct {
	Attempt string
	Kind    Residual
}

// SweepUnconfirmed retains exact emergency residual custody.
type SweepUnconfirmed struct{ residuals []ResidualRef }

// Residuals returns a detached copy of unresolved custody.
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

// ExecutionData is immutable terminal evidence.
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

// BoundFired identifies the bound that ended execution.
type BoundFired uint8

const (
	NoBoundFired BoundFired = iota
	CommandDeadlineFired
)

// OutputSnapshot records output captured through a fixed cutoff.
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

// ExitStatus records native exit evidence.
type ExitStatus struct {
	Code   int
	Signal int
}

// Passed reports a successful exit status.
func (status ExitStatus) Passed() bool { return status.Code == 0 && status.Signal == 0 }

// Terminal is the closed set of supervised terminal outcomes.
type Terminal interface{ terminal() }

// Settled reports a native root exit.
type Settled struct {
	Exit ExitStatus
	ExecutionData
}

// Trip is the closed set of supervision trips.
type Trip interface{ trip() }

// FuseTrip reports a descendant-count fuse crossing.
type FuseTrip struct{ Live int }

// ObservedCount distinguishes an absent count from zero.
type ObservedCount struct {
	Value   int
	Present bool
}

// AutomaticDeadlineTrip reports an automatic-profile deadline.
type AutomaticDeadlineTrip struct{ Peak ObservedCount }

// SerialDeadlineTrip reports a serial-profile deadline.
type SerialDeadlineTrip struct{}

func (FuseTrip) trip()              {}
func (AutomaticDeadlineTrip) trip() {}
func (SerialDeadlineTrip) trip()    {}

// Tripped reports deadline or fuse termination.
type Tripped struct {
	Trip Trip
	ExecutionData
}

// Stopped reports explicit or emergency termination.
type Stopped struct{ ExecutionData }

// Cause identifies the primary infrastructure failure.
type Cause uint8

const (
	CensusFailed Cause = iota + 1
	WaitFailed
	TerminationControlFailed
	OutputCaptureFailed
	ReleaseFailed
)

// Infrastructure reports supervision failure evidence.
type Infrastructure struct {
	Cause Cause
	Err   error
	ExecutionData
}

// DrainUnconfirmed reports custody that was not proven empty.
type DrainUnconfirmed struct {
	Residual Residual
	ExecutionData
}

func (Settled) terminal()          {}
func (Tripped) terminal()          {}
func (Stopped) terminal()          {}
func (Infrastructure) terminal()   {}
func (DrainUnconfirmed) terminal() {}

// OwnedAttempt controls and joins one launched attempt.
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

// NewOwnedAttempt constructs an owned attempt from its stop and wait behaviors.
func NewOwnedAttempt(stop func(StopRequest), wait func() Terminal) *OwnedAttempt {
	return newOwnedAttempt(stop, wait)
}

// Stop records an explicit bounded stop request.
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

// Wait joins the attempt's latched terminal outcome.
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
