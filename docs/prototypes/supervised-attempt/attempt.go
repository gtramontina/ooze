// Package attempt is a throwaway contract sketch for issue #61.
//
// It deliberately contains no process, clock, filesystem, or platform code.
// The public shape is concrete and small; the timestamped decision functions
// model the private reducer that production and deterministic simulation share.
package attempt

import (
	"errors"
	"fmt"
	"maps"
	"sort"
	"sync"
	"time"
)

// Profile is fixed before an attempt starts. Automatic attempts carry the
// process fuse; Serial attempts never take a fuse census.
type Profile uint8

const (
	Automatic Profile = iota + 1
	Serial
)

// AttemptID is the stable logical identity used by production traces and
// deterministic replay. Physical process identifiers never cross the module.
type AttemptID string

// Spec contains caller facts only. Supervision policy is private to Supervisor.
type Spec struct {
	ID       AttemptID
	Command  []string
	Dir      string
	Env      []string
	Profile  Profile
	Deadline time.Duration
}

var ErrInvalidSpec = errors.New("invalid attempt spec")

func (s Spec) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("%w: attempt identity is required", ErrInvalidSpec)
	}
	if len(s.Command) == 0 || s.Command[0] == "" {
		return fmt.Errorf("%w: command is required", ErrInvalidSpec)
	}
	if s.Profile != Automatic && s.Profile != Serial {
		return fmt.Errorf("%w: profile must be Automatic or Serial", ErrInvalidSpec)
	}
	if s.Deadline <= 0 {
		return fmt.Errorf("%w: resolved command deadline must be positive", ErrInvalidSpec)
	}

	return nil
}

// StopRequest separates the terminal fact from the cleanup bound. At decides
// which attempt fact happened first; DrainBy only limits authoritative drain.
type StopRequest struct {
	At      time.Time
	DrainBy time.Time
}

func (r StopRequest) validate() error {
	if r.At.IsZero() || !r.DrainBy.After(r.At) {
		return errors.New("stop request needs an instant and a later drain deadline")
	}

	return nil
}

// EmergencyRequest starts one runtime-wide concurrent drain epoch. It has the
// same shape as a local stop but a different scope and result.
type EmergencyRequest struct {
	At      time.Time
	DrainBy time.Time
}

func (r EmergencyRequest) validate() error {
	return StopRequest(r).validate()
}

// Supervisor is concrete. Its fields stand in for the private implementation
// in this sketch; callers see only Launch and EmergencyDrain.
type Supervisor struct {
	commitStart    func(AttemptID) generation
	launch         func(Spec, generation) LaunchResult
	emergencyDrain func(EmergencyRequest) SweepResult
}

var ErrUnsupportedPlatform = errors.New("supervised attempts are unsupported on this platform")

// newSupervisor receives one build-tag-resolved fact. Unsupported builds fail
// before the caller can admit or launch an attempt.
func newSupervisor(
	supported bool,
	commitStart func(AttemptID) generation,
	launch func(Spec, generation) LaunchResult,
	emergencyDrain func(EmergencyRequest) SweepResult,
) (*Supervisor, error) {
	if !supported {
		return nil, ErrUnsupportedPlatform
	}
	if commitStart == nil || launch == nil || emergencyDrain == nil {
		return nil, errors.New("supervisor native operations are required")
	}

	return &Supervisor{commitStart: commitStart, launch: launch, emergencyDrain: emergencyDrain}, nil
}

func (s *Supervisor) Launch(spec Spec) LaunchResult {
	err := spec.Validate()
	if err != nil {
		panic(err)
	}
	snapshot := spec
	snapshot.Command = append([]string(nil), spec.Command...)
	snapshot.Env = append([]string(nil), spec.Env...)
	// This hook is the process runtime's StartCommitted linearization: it
	// validates the matching grant/open gate and records the prospective
	// generation before the native launch below can run.
	gen := s.commitStart(snapshot.ID)
	if gen == 0 {
		panic("prospective obligation registration returned no generation")
	}

	return s.launch(snapshot, gen)
}

func (s *Supervisor) EmergencyDrain(request EmergencyRequest) SweepResult {
	err := request.validate()
	if err != nil {
		panic(err)
	}

	return s.emergencyDrain(request)
}

// OwnedAttempt is the opaque capability returned only after target release
// inside initial containment. The caller owns any goroutine around Wait.
type OwnedAttempt struct {
	stop       func(StopRequest)
	stateMu    sync.Mutex
	stopClosed bool
	wait       func(sealStopAdmission func()) Terminal
	waitOnce   sync.Once
	terminal   Terminal
}

func (a *OwnedAttempt) Stop(request StopRequest) {
	err := request.validate()
	if err != nil {
		panic(err)
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.stopClosed {
		return
	}
	a.stop(request)
}

func (a *OwnedAttempt) Wait() Terminal {
	a.waitOnce.Do(func() {
		sealed := false
		// The driver seals at either native release or the no-release custody
		// transfer which precedes DrainUnconfirmed delivery.
		terminal := a.wait(func() {
			a.stateMu.Lock()
			defer a.stateMu.Unlock()
			if a.stopClosed {
				panic("stop admission sealed twice")
			}
			a.stopClosed = true
			sealed = true
		})
		if terminal == nil {
			panic("owned wait returned no terminal")
		}
		if !sealed {
			panic("owned wait delivered before sealing stop admission")
		}
		a.terminal = terminal
	})

	return a.terminal
}

// LaunchResult closes the prospective obligation in one of three ways.
type LaunchResult interface{ launchResult() }

// Owned means the target was released inside containment.
type Owned struct {
	Attempt        *OwnedAttempt
	LaunchDuration time.Duration
}

// NotReleased is proof that target code never ran. Kind distinguishes the
// single normalized pressure input from every other ordinary launch failure.
type NotReleased struct {
	Kind           LaunchFailure
	LaunchDuration time.Duration
	Err            error
}

// LaunchUnconfirmed is the launch-side DrainUnconfirmed branch. The Supervisor
// retains the pending operation so a late physical start can be adopted.
type LaunchUnconfirmed struct {
	Last           string
	LaunchDuration time.Duration
}

func (Owned) launchResult()             {}
func (NotReleased) launchResult()       {}
func (LaunchUnconfirmed) launchResult() {}

type LaunchFailure uint8

const (
	LaunchFailed LaunchFailure = iota + 1
	LaunchResourceExhausted
)

// ExecutionData is common to every owned terminal. Output is a string because
// it is immutable merged bytes, not a mutable buffer or a filesystem path.
// CommandDuration ends when terminal intent latches, never after drainage.
type ExecutionData struct {
	Deadline        time.Duration
	LaunchDuration  time.Duration
	CommandDuration time.Duration
	BoundFired      BoundFired
	Output          OutputSnapshot
	Failures        FailureDiagnostics
}

// FailureDiagnostics retains every independent failure axis even when one
// stable Cause is selected for Infrastructure presentation.
type FailureDiagnostics struct {
	Wait          string
	RunningCensus string
	DrainCensus   string
	Termination   string
	Output        string
	Release       string
}

type BoundFired uint8

const (
	NoBoundFired BoundFired = iota
	CommandDeadlineFired
)

// OutputSnapshot is one immutable contiguous prefix of the attempt's merged
// output file. Cutoff is captured once; bytes appended later are excluded.
// Final means drainage proved no writer remains. CompleteThroughCutoff is
// false when the read returned only a successful prefix before an error.
type OutputSnapshot struct {
	Bytes                 string
	Cutoff                uint64
	CompleteThroughCutoff bool
	Final                 bool
}

type Terminal interface{ terminal() }

type Settled struct {
	Exit ExitStatus
	ExecutionData
}

type Tripped struct {
	Trip Trip
	ExecutionData
}

type Stopped struct{ ExecutionData }

type Infrastructure struct {
	Cause Cause
	Err   error
	ExecutionData
}

// DrainUnconfirmed is the only owned terminal without an emptiness proof. The
// Supervisor retains the domain capability for runtime-fatal handling.
type DrainUnconfirmed struct {
	Residual Residual
	ExecutionData
}

func (Settled) terminal()          {}
func (Tripped) terminal()          {}
func (Stopped) terminal()          {}
func (Infrastructure) terminal()   {}
func (DrainUnconfirmed) terminal() {}

// Trip keeps count evidence on only the variants where it has meaning.
type Trip interface{ trip() }

type (
	FuseTrip      struct{ Live int }
	ObservedCount struct {
		Value   int
		Present bool
	}
)
type (
	AutomaticDeadlineTrip struct{ Peak ObservedCount }
	SerialDeadlineTrip    struct{}
)

func (FuseTrip) trip()              {}
func (AutomaticDeadlineTrip) trip() {}
func (SerialDeadlineTrip) trip()    {}

type Cause uint8

const (
	CensusFailed Cause = iota + 1
	WaitFailed
	TerminationControlFailed
	OutputCaptureFailed
	ReleaseFailed
)

type Residual uint8

const (
	ProspectiveUnresolved Residual = iota + 1
	OwnedUndrained
)

type ExitStatus struct {
	Code   int
	Signal int
}

func (e ExitStatus) Passed() bool { return e.Code == 0 && e.Signal == 0 }

// SweepResult is the stable settlement of one emergency epoch.
type SweepResult interface{ sweepResult() }

type SweepDrained struct{}

type ResidualRef struct {
	ID         AttemptID
	Kind       Residual
	generation generation
}

type SweepUnconfirmed struct{ residuals []ResidualRef }

func newSweepUnconfirmed(residuals []ResidualRef) SweepUnconfirmed {
	return SweepUnconfirmed{residuals: append([]ResidualRef(nil), residuals...)}
}

func (s SweepUnconfirmed) Residuals() []ResidualRef {
	return append([]ResidualRef(nil), s.residuals...)
}

func (SweepDrained) sweepResult()     {}
func (SweepUnconfirmed) sweepResult() {}

// These are private zero-configuration policies. Fifty milliseconds is an
// argued nominal fuse cadence, not a guaranteed maximum observation gap or a
// measured process-safety bound. Launch and drain durations are intentionally
// absent: #61 requires resolved deadlines but chooses no numeric defaults.
const (
	fuseCeiling        = 64
	nominalFuseCadence = 50 * time.Millisecond
)

// ----------------------------------------------------------------- launch

// The native adapter classifies the exact primary launch operation before it
// joins cleanup diagnostics. Errors from unrelated siblings cannot manufacture
// resource exhaustion.
type launchStage uint8

const (
	preRelease launchStage = iota + 1
	unknownRelease
	postRelease
)

type launchPlatform uint8

const (
	platformLinux launchPlatform = iota + 1
	platformDarwin
	platformWindows
)

type launchOperation uint8

const (
	acquireInternalDescriptor launchOperation = iota + 1
	startLauncher
	createExitTracker
	registerExitTracker
	execTarget
	configureWindowsContainment
)

type launchCode uint8

const (
	codeOther launchCode = iota + 1
	codeEAGAIN
	codeENOMEM
	codeEMFILE
	codeENFILE
	codeWinTooManyOpenFiles
	codeWinNotEnoughMemory
	codeWinOutOfMemory
	codeWinNoProcessSlots
	codeWinNoSystemResources
	codeWinCommitmentLimit
)

type nativeLaunchFailure struct {
	platform      launchPlatform
	operation     launchOperation
	stage         launchStage
	code          launchCode
	closureProven bool
	duration      time.Duration
	err           error
	owned         *OwnedAttempt
}

func classifyLaunch(f nativeLaunchFailure) LaunchResult {
	if f.err == nil {
		panic("native launch failure requires an error")
	}
	switch f.stage {
	case unknownRelease:
		return LaunchUnconfirmed{Last: f.err.Error(), LaunchDuration: f.duration}
	case postRelease:
		if f.owned == nil {
			panic("post-release failure requires an adopted owned attempt")
		}

		return Owned{Attempt: f.owned, LaunchDuration: f.duration}
	case preRelease:
		if !f.closureProven {
			return LaunchUnconfirmed{Last: f.err.Error(), LaunchDuration: f.duration}
		}
	default:
		panic("unknown native launch stage")
	}

	kind := LaunchFailed
	if isLaunchResourceExhaustion(f.platform, f.operation, f.code) {
		kind = LaunchResourceExhausted
	}

	return NotReleased{Kind: kind, LaunchDuration: f.duration, Err: f.err}
}

func isLaunchResourceExhaustion(platform launchPlatform, operation launchOperation, code launchCode) bool {
	switch platform {
	case platformLinux:
		switch operation {
		case acquireInternalDescriptor:
			return code == codeEMFILE || code == codeENFILE
		case startLauncher:
			return code == codeEAGAIN || code == codeENOMEM || code == codeEMFILE || code == codeENFILE
		case execTarget:
			return code == codeEAGAIN || code == codeENOMEM
		default:
			return false
		}
	case platformDarwin:
		switch operation {
		case acquireInternalDescriptor:
			return code == codeEMFILE || code == codeENFILE
		case startLauncher:
			return code == codeEAGAIN || code == codeENOMEM || code == codeEMFILE || code == codeENFILE
		case createExitTracker:
			return code == codeENOMEM || code == codeEMFILE || code == codeENFILE
		case registerExitTracker, execTarget:
			return code == codeENOMEM
		default:
			return false
		}
	case platformWindows:
		if operation != acquireInternalDescriptor && operation != startLauncher &&
			operation != configureWindowsContainment {
			return false
		}
		switch code {
		case codeWinTooManyOpenFiles, codeWinNotEnoughMemory, codeWinOutOfMemory,
			codeWinNoProcessSlots, codeWinNoSystemResources, codeWinCommitmentLimit:
			return true
		default:
			return false
		}
	default:
		panic("unknown launch platform")
	}
}

// pendingLaunch is the single launch-progress reducer. Notifications are
// wakeups only: the boundary event carries the completion atomically visible
// at LaunchBy, so scheduler delivery order cannot decide ownership.
type pendingLaunch struct {
	generation    generation
	launchBy      time.Time
	state         pendingState
	nativeHeld    bool
	revoked       bool
	revokedAt     time.Time
	releaseDenied bool
	lastEventAt   time.Time
}

type pendingState uint8

const (
	launching pendingState = iota + 1
	reportedUnconfirmed
	adoptedOwned
	closedNotReleased
)

type pendingEventKind uint8

const (
	nativeIdentityAcquired pendingEventKind = iota + 1
	launchCompleted
	launchBoundary
	launchReleaseRevoked
)

type pendingEvent struct {
	generation generation
	kind       pendingEventKind
	at         time.Time
	completion *launchCompletion
}

type launchCompletionKind uint8

const (
	completedNotReleased launchCompletionKind = iota + 1
	completedReleased
)

type launchCompletion struct {
	kind launchCompletionKind
	at   time.Time
}

type pendingAction uint8

const (
	continueLaunchEstablishment pendingAction = iota + 1
	reportLaunchUnconfirmed
	reportUnconfirmedAndCloseUnreleased
	closeUnreleasedIdentity
	returnNotReleased
	returnOwned
	returnOwnedAndForceDrain
	closeProspective
	adoptAndForceDrain
)

func beginPending(gen generation, launchBy time.Time) pendingLaunch {
	if gen == 0 || launchBy.IsZero() {
		panic("pending launch requires a generation and launch deadline")
	}

	return pendingLaunch{generation: gen, launchBy: launchBy, state: launching}
}

func advancePending(p pendingLaunch, event pendingEvent) (pendingLaunch, pendingAction) {
	if p.generation == 0 || event.generation != p.generation || p.launchBy.IsZero() || event.at.IsZero() {
		panic("incomplete pending-launch transition")
	}
	previousAt := p.lastEventAt
	if !previousAt.IsZero() && event.at.Before(previousAt) {
		panic("pending-launch event moved backward")
	}
	p.lastEventAt = event.at
	switch event.kind {
	case nativeIdentityAcquired:
		if event.completion != nil || p.nativeHeld ||
			(p.state == launching && event.at.After(p.launchBy)) ||
			(p.state == reportedUnconfirmed && event.at.Before(p.revokedAt)) {
			panic("invalid native-identity acquisition")
		}
		p.nativeHeld = true
		if p.state == reportedUnconfirmed {
			p.releaseDenied = true
			return p, closeUnreleasedIdentity
		}
		if p.state != launching {
			panic("native identity acquired after launch closure")
		}

		return p, continueLaunchEstablishment
	case launchCompleted:
		if event.completion == nil || !event.at.Equal(event.completion.at) {
			panic("completion event requires one stamped completion")
		}
		if p.state == launching {
			if !event.at.Before(p.launchBy) {
				panic("completion at the boundary must arrive through its snapshot")
			}

			return completePending(p, *event.completion, false)
		}
		if p.state != reportedUnconfirmed || event.at.Before(p.revokedAt) {
			panic("invalid late launch completion")
		}

		return completePending(p, *event.completion, true)
	case launchBoundary:
		if p.state != launching || !event.at.Equal(p.launchBy) {
			panic("launch boundary must occur once at LaunchBy")
		}
		if event.completion != nil {
			if event.completion.at.After(p.launchBy) ||
				(!previousAt.IsZero() && event.completion.at.Before(previousAt)) {
				panic("boundary snapshot contains a post-bound completion")
			}

			return completePending(p, *event.completion, false)
		}
		p.state, p.revoked, p.revokedAt = reportedUnconfirmed, true, p.launchBy
		if p.nativeHeld {
			p.releaseDenied = true
			return p, reportUnconfirmedAndCloseUnreleased
		}

		return p, reportLaunchUnconfirmed
	case launchReleaseRevoked:
		if p.state != launching || p.revoked || !event.at.Before(p.launchBy) {
			panic("launch release revoked outside an active prospective launch")
		}
		if event.completion != nil {
			if event.completion.at.After(event.at) ||
				(!previousAt.IsZero() && event.completion.at.Before(previousAt)) {
				panic("revocation snapshot contains an impossible completion")
			}
			completed, action := completePending(p, *event.completion, false)
			if action == returnOwned {
				action = returnOwnedAndForceDrain
			}
			return completed, action
		}
		p.state, p.revoked, p.revokedAt = reportedUnconfirmed, true, event.at
		if p.nativeHeld {
			p.releaseDenied = true
			return p, reportUnconfirmedAndCloseUnreleased
		}

		return p, reportLaunchUnconfirmed
	default:
		panic("unknown pending-launch event")
	}
}

func completePending(p pendingLaunch, completion launchCompletion, late bool) (pendingLaunch, pendingAction) {
	if completion.at.IsZero() ||
		(completion.kind != completedNotReleased && completion.kind != completedReleased) {
		panic("invalid launch completion")
	}
	switch completion.kind {
	case completedNotReleased:
		p.state = closedNotReleased
		if late {
			return p, closeProspective
		}

		return p, returnNotReleased
	case completedReleased:
		if p.releaseDenied {
			panic("target released after the sequencer revoked release")
		}
		// A fully classified released completion itself carries custody. Native
		// adapters must publish an earlier acquisition whenever identity becomes
		// controllable before this release cut; an unavoidable already-released
		// late completion is still adopted rather than denied.
		p.nativeHeld = true
		p.state = adoptedOwned
		if late {
			return p, adoptAndForceDrain
		}

		return p, returnOwned
	default:
		panic("unknown launch completion")
	}
}

// ------------------------------------------------------------ intent choice

type factKind uint8

const (
	factFuse factKind = iota + 1
	factExit
	factSupervisionFailure
	factStop
	factDeadline
)

// generation is allocated privately for every accepted launch. Caller-facing
// AttemptID is diagnostic identity and may be reused.
type generation uint64

type terminalFact struct {
	generation generation
	kind       factKind
	at         time.Time
	rootLive   bool
	live       int
	exit       ExitStatus
	err        error
	cause      Cause
	stop       StopRequest
}

type intent struct {
	kind      factKind
	at        time.Time
	live      int
	exit      ExitStatus
	err       error
	cause     Cause
	waitErr   error
	censusErr error
	stop      StopRequest
	peak      int
	counted   bool
}

func priority(kind factKind) int {
	switch kind {
	case factFuse:
		return 0
	case factExit:
		return 1
	case factSupervisionFailure:
		return 2
	case factDeadline:
		return 3
	case factStop:
		return 4
	default:
		return 5
	}
}

// chooseIntent selects the earliest valid logical fact through a known
// observation instant. Priority exists only for exact ties. A fuse sample is
// valid only for the owned generation and when that sample proves the root was
// live. Production must stamp facts after native operations, never reuse a
// pre-operation clock reading.
func chooseIntent(spec Spec, gen generation, released, through time.Time, facts []terminalFact) (intent, bool) {
	deadlineAt := released.Add(spec.Deadline)
	candidates := make([]terminalFact, 0, len(facts)+1)
	for _, fact := range facts {
		if fact.generation == 0 || (fact.kind == factFuse && fact.live < 0) {
			panic("invalid terminal fact")
		}
		if fact.generation != gen || fact.at.After(through) || fact.at.Before(released) {
			continue
		}
		switch fact.kind {
		case factFuse:
			if spec.Profile != Automatic || !fact.rootLive || fact.live <= fuseCeiling {
				continue
			}
		case factSupervisionFailure:
			if fact.err == nil || (fact.cause != CensusFailed && fact.cause != WaitFailed) {
				continue
			}
			if fact.cause == CensusFailed && spec.Profile != Automatic {
				continue
			}
			for _, existing := range candidates {
				if existing.kind == factSupervisionFailure && existing.cause == fact.cause &&
					existing.at.Equal(fact.at) {
					panic("duplicate supervision failure at one logical instant")
				}
			}
		case factStop:
			if fact.stop.validate() != nil || !fact.stop.At.Equal(fact.at) {
				continue
			}
		case factExit:
		default:
			continue
		}
		candidates = append(candidates, fact)
	}
	if !through.Before(deadlineAt) {
		candidates = append(candidates, terminalFact{
			generation: gen,
			kind:       factDeadline,
			at:         deadlineAt,
		})
	}
	if len(candidates) == 0 {
		return intent{}, false
	}

	selected := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.at.Before(selected.at) ||
			(candidate.at.Equal(selected.at) && priority(candidate.kind) < priority(selected.kind)) {
			selected = candidate

			continue
		}
		if !candidate.at.Equal(selected.at) || candidate.kind != selected.kind {
			continue
		}
		switch selected.kind {
		case factFuse:
			selected.live = max(selected.live, candidate.live)
		case factSupervisionFailure:
			if candidate.cause == selected.cause {
				panic("duplicate supervision failure at one logical instant")
			}
			if candidate.cause == WaitFailed {
				selected = candidate
			}
		case factStop:
			if candidate.stop.DrainBy.Before(selected.stop.DrainBy) {
				selected = candidate
			}
		default:
			panic("duplicate same-kind terminal fact at one logical instant")
		}
	}
	for _, candidate := range candidates {
		if candidate.kind == factStop && candidate.at.Equal(selected.at) &&
			(selected.stop.DrainBy.IsZero() || candidate.stop.DrainBy.Before(selected.stop.DrainBy)) {
			selected.stop = candidate.stop
		}
	}

	chosen := intent{
		kind:  selected.kind,
		at:    selected.at,
		live:  selected.live,
		exit:  selected.exit,
		err:   selected.err,
		cause: selected.cause,
		stop:  selected.stop,
	}
	for _, candidate := range candidates {
		if candidate.kind != factSupervisionFailure || !candidate.at.Equal(selected.at) {
			continue
		}
		if candidate.cause == WaitFailed {
			chosen.waitErr = candidate.err
		} else {
			chosen.censusErr = candidate.err
		}
	}

	return chosen, true
}

type machine struct {
	spec           Spec
	generation     generation
	released       time.Time
	launchDuration time.Duration
	intent         *intent
	drainBy        time.Time
	peak           int
	counted        bool
	drainStarted   bool
	forced         bool
	drained        bool
	unconfirmed    bool
	controlErr     error
	lastCensusErr  error
	lastDrainAt    time.Time
	output         OutputSnapshot
	outputCaptured bool
	outputErr      error
	releasedDomain bool
	releaseErr     error
	actionSequence uint64
	awaiting       drainAction
}

func begin(spec Spec, gen generation, released time.Time, launchDuration time.Duration) (machine, error) {
	err := spec.Validate()
	if err != nil {
		return machine{}, err
	}
	if gen == 0 || released.IsZero() || launchDuration < 0 {
		return machine{}, errors.New("private generation, release instant, and non-negative launch duration are required")
	}

	return machine{spec: spec, generation: gen, released: released, launchDuration: launchDuration}, nil
}

// observeRunning applies running samples and latches exactly one intent. Facts
// after the chosen instant do not inflate a timeout peak or rewrite the result.
func observeRunning(m machine, through, localDrainBy time.Time, facts []terminalFact) machine {
	if m.intent != nil {
		return m
	}
	chosen, ok := chooseIntent(m.spec, m.generation, m.released, through, facts)
	cutoff := through
	if ok {
		cutoff = chosen.at
	}
	if m.spec.Profile == Automatic {
		for _, fact := range facts {
			if fact.generation == m.generation && fact.kind == factFuse && fact.rootLive &&
				!fact.at.Before(m.released) && !fact.at.After(cutoff) {
				m.peak, m.counted = max(m.peak, fact.live), true
			}
		}
	}
	if !ok {
		return m
	}
	chosen.peak, chosen.counted = m.peak, m.counted
	m.intent = &chosen
	m.drainBy = localDrainBy
	if !chosen.stop.DrainBy.IsZero() && chosen.stop.DrainBy.Before(m.drainBy) {
		m.drainBy = chosen.stop.DrainBy
	}
	if !m.drainBy.After(chosen.at) {
		panic("local drain deadline must follow terminal intent")
	}

	return m
}

// ------------------------------------------------------------------- drain

type drainActionKind uint8

const (
	forceDomain drainActionKind = iota + 1
	observeDomain
	captureOutput
	releaseDomain
	deliverTerminal
)

type drainAction struct {
	kind       drainActionKind
	by         time.Time
	generation generation
	sequence   uint64
}

type drainEventKind uint8

const (
	forceCompleted drainEventKind = iota + 1
	domainObserved
)

type drainEvent struct {
	kind       drainEventKind
	at         time.Time
	action     drainAction
	empty      bool
	err        error
	controlErr error
}

type outputObservation struct {
	action drainAction
	cutoff uint64
	prefix string
	err    error
}

// shortenDrain intersects a runtime-emergency bound with the local epoch. It
// is independent of terminal-cause selection and cannot be lost while the
// first native action is in flight.
func shortenDrain(m machine, by time.Time) machine {
	if m.intent == nil || by.IsZero() || !by.After(m.intent.at) {
		panic("drain clamp requires an absolute deadline")
	}
	if m.drainBy.IsZero() || by.Before(m.drainBy) {
		m.drainBy = by
	}

	return m
}

func issue(m machine, kind drainActionKind) (machine, drainAction) {
	if m.awaiting.kind != 0 {
		panic("native action issued before its predecessor completed")
	}
	m.actionSequence++
	m.awaiting = drainAction{
		kind:       kind,
		by:         m.drainBy,
		generation: m.generation,
		sequence:   m.actionSequence,
	}

	return m, m.awaiting
}

func consume(m machine, completed drainAction, want drainActionKind) machine {
	if completed.kind != want || completed != m.awaiting ||
		completed.generation != m.generation || completed.sequence == 0 {
		panic("native completion does not match the pending action")
	}
	m.awaiting = drainAction{}

	return m
}

// advanceDrain has causal native phases. Timeout/fuse/stop/fault force first;
// a natural exit first asks for authoritative emptiness and forces only when a
// residual is observed. Every action carries the same absolute deadline.
func advanceDrain(m machine, event *drainEvent) (machine, drainAction) {
	if m.intent == nil {
		panic("drain before terminal intent")
	}
	if event != nil {
		if event.at.IsZero() || event.at.Before(m.intent.at) ||
			(!m.lastDrainAt.IsZero() && event.at.Before(m.lastDrainAt)) {
			panic("native drain event requires a logical instant")
		}
		want := observeDomain
		if event.kind == forceCompleted {
			want = forceDomain
		}
		m = consume(m, event.action, want)
		m.lastDrainAt = event.at
		switch event.kind {
		case forceCompleted:
			if !m.forced || event.empty || event.err != nil {
				panic("invalid forced-termination result")
			}
			if event.controlErr != nil && m.controlErr == nil {
				m.controlErr = event.controlErr
			}
		case domainObserved:
			if event.controlErr != nil || (event.empty && event.err != nil) {
				panic("invalid authoritative-domain observation")
			}
			if event.err != nil {
				m.lastCensusErr = event.err
			}
		default:
			panic("unknown native drain event")
		}
		if event.kind == domainObserved && !m.forced && (!event.empty || event.err != nil) {
			m.forced = true

			return issue(m, forceDomain)
		}
		if !event.at.Before(m.drainBy) {
			m.unconfirmed = true

			return issue(m, captureOutput)
		}
		if event.kind == domainObserved && event.err == nil {
			if event.empty {
				m.drained = true

				return issue(m, captureOutput)
			}
			if !m.forced {
				m.forced = true

				return issue(m, forceDomain)
			}
		}
	}
	if !m.drainStarted {
		m.drainStarted = true
		if m.intent.kind != factExit {
			m.forced = true

			return issue(m, forceDomain)
		}
	}

	return issue(m, observeDomain)
}

// acceptOutput deliberately captures retained bytes on both the drained and
// unconfirmed paths. Only proven drainage permits release.
func acceptOutput(m machine, observed outputObservation) (machine, drainAction, Terminal) {
	if (!m.drained && !m.unconfirmed) || m.outputCaptured {
		panic("output capture before drain settlement")
	}
	if uint64(len(observed.prefix)) > observed.cutoff ||
		(observed.err == nil && uint64(len(observed.prefix)) != observed.cutoff) {
		panic("output observation is not a contiguous prefix through its cutoff")
	}
	m = consume(m, observed.action, captureOutput)
	m.output = OutputSnapshot{
		Bytes:                 observed.prefix,
		Cutoff:                observed.cutoff,
		CompleteThroughCutoff: observed.err == nil,
		Final:                 m.drained,
	}
	m.outputErr, m.outputCaptured = observed.err, true
	if m.unconfirmed {
		m, next := issue(m, deliverTerminal)

		return m, next, DrainUnconfirmed{
			Residual:      OwnedUndrained,
			ExecutionData: executionData(m),
		}
	}
	m, next := issue(m, releaseDomain)

	return m, next, nil
}

func acceptRelease(m machine, completed drainAction, releaseErr error) (machine, drainAction, Terminal) {
	if !m.drained || m.unconfirmed || !m.outputCaptured || m.releasedDomain {
		panic("release without authoritative drainage")
	}
	m = consume(m, completed, releaseDomain)
	m.releaseErr, m.releasedDomain = releaseErr, true
	m, next := issue(m, deliverTerminal)

	return m, next, finish(m)
}

func finish(m machine) Terminal {
	data := executionData(m)
	switch {
	case m.controlErr != nil:
		return Infrastructure{Cause: TerminationControlFailed, Err: m.controlErr, ExecutionData: data}
	case m.outputErr != nil:
		return Infrastructure{Cause: OutputCaptureFailed, Err: m.outputErr, ExecutionData: data}
	case m.releaseErr != nil:
		return Infrastructure{Cause: ReleaseFailed, Err: m.releaseErr, ExecutionData: data}
	case m.intent.kind == factSupervisionFailure:
		return Infrastructure{Cause: m.intent.cause, Err: m.intent.err, ExecutionData: data}
	}

	switch m.intent.kind {
	case factFuse:
		return Tripped{Trip: FuseTrip{Live: m.intent.live}, ExecutionData: data}
	case factDeadline:
		if m.spec.Profile == Automatic {
			peak := ObservedCount{Value: m.intent.peak, Present: m.intent.counted}

			return Tripped{Trip: AutomaticDeadlineTrip{Peak: peak}, ExecutionData: data}
		}

		return Tripped{Trip: SerialDeadlineTrip{}, ExecutionData: data}
	case factExit:
		return Settled{Exit: m.intent.exit, ExecutionData: data}
	case factStop:
		return Stopped{ExecutionData: data}
	default:
		panic("unknown terminal intent")
	}
}

func executionData(m machine) ExecutionData {
	bound := NoBoundFired
	if m.intent.kind == factDeadline {
		bound = CommandDeadlineFired
	}

	return ExecutionData{
		Deadline:        m.spec.Deadline,
		LaunchDuration:  m.launchDuration,
		CommandDuration: m.intent.at.Sub(m.released),
		BoundFired:      bound,
		Output:          m.output,
		Failures:        failureDiagnostics(m),
	}
}

func failureDiagnostics(m machine) FailureDiagnostics {
	diagnostics := FailureDiagnostics{}
	if m.intent.waitErr != nil {
		diagnostics.Wait = m.intent.waitErr.Error()
	}
	if m.intent.censusErr != nil {
		diagnostics.RunningCensus = m.intent.censusErr.Error()
	}
	if m.controlErr != nil {
		diagnostics.Termination = m.controlErr.Error()
	}
	if m.lastCensusErr != nil {
		diagnostics.DrainCensus = m.lastCensusErr.Error()
	}
	if m.outputErr != nil {
		diagnostics.Output = m.outputErr.Error()
	}
	if m.releaseErr != nil {
		diagnostics.Release = m.releaseErr.Error()
	}

	return diagnostics
}

// ---------------------------------------------------------- emergency epoch

type obligation struct {
	ID         AttemptID
	generation generation
	Kind       Residual
}

type emergencyDispatch struct {
	obligation obligation
	request    EmergencyRequest
}

type emergencyLedger struct {
	request     EmergencyRequest
	obligations map[generation]obligation
	order       []generation
	settled     bool
	result      SweepResult
}

func beginEmergency(request EmergencyRequest, obligations []obligation) (emergencyLedger, []emergencyDispatch) {
	err := request.validate()
	if err != nil {
		panic(err)
	}
	ledger := emergencyLedger{
		request:     request,
		obligations: make(map[generation]obligation, len(obligations)),
	}
	ordered := append([]obligation(nil), obligations...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].generation < ordered[j].generation })
	dispatches := make([]emergencyDispatch, 0, len(ordered))
	for _, item := range ordered {
		if item.ID == "" || item.generation == 0 ||
			(item.Kind != ProspectiveUnresolved && item.Kind != OwnedUndrained) ||
			ledger.obligations[item.generation].generation != 0 {
			panic("emergency ledger requires unique private generations")
		}
		ledger.obligations[item.generation] = item
		ledger.order = append(ledger.order, item.generation)
		dispatches = append(dispatches, emergencyDispatch{obligation: item, request: request})
	}

	return ledger, dispatches
}

func (l emergencyLedger) adoptLate(item obligation, at time.Time) (emergencyLedger, emergencyDispatch) {
	existing := l.obligations[item.generation]
	if at.IsZero() || at.Before(l.request.At) || item.ID == "" || item.generation == 0 || item.Kind != OwnedUndrained ||
		existing.generation == 0 || existing.ID != item.ID || existing.Kind != ProspectiveUnresolved {
		panic("invalid late emergency adoption")
	}
	if !at.Before(l.request.DrainBy) && !l.settled {
		l, _, _ = l.settle(l.request.DrainBy)
	}
	copyOfObligations := make(map[generation]obligation, len(l.obligations)+1)
	maps.Copy(copyOfObligations, l.obligations)
	l.obligations = copyOfObligations
	l.order = append([]generation(nil), l.order...)
	l.obligations[item.generation] = item

	return l, emergencyDispatch{obligation: item, request: l.request}
}

func (l emergencyLedger) resolve(gen generation, kind Residual, at time.Time) emergencyLedger {
	existing := l.obligations[gen]
	if at.IsZero() || at.Before(l.request.At) || existing.generation == 0 || existing.Kind != kind {
		panic("emergency resolution does not match a retained obligation")
	}
	if !at.Before(l.request.DrainBy) && !l.settled {
		l, _, _ = l.settle(l.request.DrainBy)
	}
	copyOfObligations := make(map[generation]obligation, len(l.obligations)-1)
	for existingGen, item := range l.obligations {
		if existingGen != gen {
			copyOfObligations[existingGen] = item
		}
	}
	l.obligations = copyOfObligations
	l.order = append([]generation(nil), l.order...)

	return l
}

func (l emergencyLedger) settle(at time.Time) (emergencyLedger, SweepResult, bool) {
	if at.IsZero() || at.Before(l.request.At) {
		panic("emergency settlement requires a logical instant in its epoch")
	}
	if l.settled {
		return l, l.result, true
	}
	if len(l.obligations) != 0 && at.Before(l.request.DrainBy) {
		return l, nil, false
	}
	if len(l.obligations) == 0 {
		l.settled, l.result = true, SweepDrained{}

		return l, l.result, true
	}
	residuals := make([]ResidualRef, 0, len(l.obligations))
	for _, gen := range l.order {
		item, retained := l.obligations[gen]
		if !retained {
			continue
		}
		residuals = append(residuals, ResidualRef{ID: item.ID, Kind: item.Kind, generation: item.generation})
	}
	l.settled, l.result = true, newSweepUnconfirmed(residuals)

	return l, l.result, true
}

// ----------------------------------------------------- Darwin native script

type darwinTerminationStep uint8

const (
	captureLiveMembersAndClosure darwinTerminationStep = iota + 1
	freezeProcessGroup
	revalidateCapturedIdentityBeforeFreeze
	freezeCapturedEscapeesByPID
	convergeFrozenClosure
	killProcessGroup
	revalidateCapturedIdentityBeforeKill
	killCapturedEscapeesByPID
)

type processIdentity struct {
	pid        int
	birthToken uint64
}

func sameProcess(captured, observed processIdentity) bool {
	if captured.pid <= 0 || captured.birthToken == 0 || observed.pid <= 0 || observed.birthToken == 0 {
		panic("process identity requires pid and birth token")
	}

	return captured == observed
}

// darwinTerminationScript makes the destructive-order acceptance contract
// executable without pretending these Unix mechanics are portable reducer
// events. #67 binds this order to #66's real escapee fixture.
func darwinTerminationScript() []darwinTerminationStep {
	return []darwinTerminationStep{
		captureLiveMembersAndClosure,
		freezeProcessGroup,
		revalidateCapturedIdentityBeforeFreeze,
		freezeCapturedEscapeesByPID,
		convergeFrozenClosure,
		killProcessGroup,
		revalidateCapturedIdentityBeforeKill,
		killCapturedEscapeesByPID,
	}
}

// policyResolver is private and numeric-free in the contract. Production will
// resolve concrete defaults once; tests inject durations and record the three
// resulting absolute instants.
type policyResolver struct {
	launchProgress time.Duration
	drainEpoch     time.Duration
}

type resolvedDeadlines struct {
	LaunchBy         time.Time
	LocalDrainBy     time.Time
	EmergencyDrainBy time.Time
}

func (p policyResolver) resolve(launchAt, localDrainAt, emergencyAt time.Time) resolvedDeadlines {
	if p.launchProgress <= 0 || p.drainEpoch <= 0 ||
		launchAt.IsZero() || localDrainAt.IsZero() || emergencyAt.IsZero() {
		panic("positive supervision policy and non-zero epoch starts are required")
	}

	return resolvedDeadlines{
		LaunchBy:         launchAt.Add(p.launchProgress),
		LocalDrainBy:     localDrainAt.Add(p.drainEpoch),
		EmergencyDrainBy: emergencyAt.Add(p.drainEpoch),
	}
}
