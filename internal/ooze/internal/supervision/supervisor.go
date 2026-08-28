package supervision

import (
	"errors"
	"fmt"
	"sync"
	"time"
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
