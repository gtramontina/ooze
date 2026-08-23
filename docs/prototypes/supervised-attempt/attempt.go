// Package attempt is a throwaway sketch of the supervised attempt contract for
// issue #61. It exists to test one claim: that the whole lifecycle policy —
// deadline, descendant census, stop escalation, drain budget, and the mapping
// to a typed observation — can be written once, over a per-operating-system
// interface small enough to be worth having.
//
// Nothing here is production code. The point is the shape and the decision
// table, both of which are exercised by attempt_test.go.
package attempt

import (
	"errors"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------- spec

// Profile is the resolved execution profile. It is carried in the spec because
// #60 scopes the fuse to the "automatic execution profile ONLY": under Serial()
// there is no fuse, and no count observation is made at all. Nothing in the
// engine may infer the profile from a zero Fuse, so the two are separate fields
// and Validate insists they agree.
type Profile uint8

const (
	Automatic Profile = iota + 1
	Serial
)

// Spec is everything one attempt generation needs. It is immutable, it carries
// no clock, and it derives nothing: #59 resolved the deadline once at baseline
// settlement, and #60 fixed the fuse ceiling at a constant.
type Spec struct {
	Command []string // opaque, exactly as WithTestCommand supplied it
	Dir     string   // the materialized attempt workspace
	Env     []string // the child's environment
	Output  string   // merged stdout/stderr, a path inside Dir
	Profile Profile  // Automatic or Serial; never inferred

	Deadline time.Duration // resolved once for the whole campaign
	Fuse     int           // 0 under Serial(); 64 under the automatic profile
	Drain    time.Duration // budget for proving the domain empty
	Cadence  time.Duration // census interval while running

	// LaunchBound bounds establishing containment and releasing the target
	// instruction. It is separate from Deadline because launch cost is not
	// attributable to the mutant: fork/exec into a cold workspace under 14-way
	// contention is Ooze's own cost, and #59 does not want it spent on the
	// mutation deadline, whose false trips consume a process-wide one-shot
	// confirmation net. The engine cannot preempt a syscall, so this is an
	// obligation on the adapter (see Platform) that the engine measures and
	// reports rather than one it enforces.
	LaunchBound time.Duration

	// DrainPoll is the interval between the first termination rounds, and is
	// much tighter than Cadence. They are different rates for different
	// reasons: a census is a whole-process-table read that only has to beat a
	// runaway's growth, while a termination round is racing a forking domain.
	// On darwin kill(-pgid) enumerates members at call time, and emptying a
	// forking domain needs tens of rounds (unquantified: see readme "Corrections").
	// At the census cadence that
	// would take a second; at this interval it takes milliseconds. See
	// drainInterval for why the rounds after those first ones back off.
	DrainPoll time.Duration
}

// ErrInvalidSpec is a programming fault in the caller, not a fault of the
// attempt. Nothing is launched, so it reports as an abort with nothing owned.
var ErrInvalidSpec = errors.New("invalid attempt spec")

// Validate rejects the specs the engine's own progress argument depends on.
// Every interval the loop waits on must be positive, or time does not advance
// and Run never terminates — which is how the drain-budget branch stayed
// unreachable through Run, and how the same spec would have become a five
// second busy loop issuing kills against a real clock. It checks the timing and
// profile invariants only: whether Command or Dir make sense is the campaign's
// business.
func (s Spec) Validate() error {
	for _, check := range []struct {
		ok  bool
		why string
	}{
		{s.Profile == Automatic || s.Profile == Serial, "profile must be Automatic or Serial"},
		{(s.Profile == Automatic) == (s.Fuse > 0), "the fuse belongs to the automatic profile, and only to it"},
		{s.Deadline > 0 && s.LaunchBound > 0 && s.Drain > 0, "deadline, launch bound and drain budget must be positive"},
		{s.Cadence > 0 && s.DrainPoll > 0, "census cadence and drain poll must be positive"},
		{s.DrainPoll <= s.Cadence, "drain poll must not exceed the census cadence"},
	} {
		if !check.ok {
			return fmt.Errorf("%w: %s", ErrInvalidSpec, check.why)
		}
	}

	return nil
}

// counts reports whether this attempt takes a descendant count at all. #60: no
// count observation is made for a Serial() attempt, so its observations carry
// no count — absent, not zero.
func (s Spec) counts() bool { return s.Profile == Automatic }

// -------------------------------------------------------------- observations

// Observation is the one terminal report per attempt generation, delivered
// after the domain is proven drained or drainage is declared unconfirmed. The
// five concrete types cover #57's vocabulary — "normalized attempt trip,
// settlement, infrastructure-fault, and local DrainUnconfirmed fatal-seed
// observations" — plus Stopped, which carries no verdict at all.
type Observation interface{ observation() }

// Evidence is common to every observation. Elapsed, Launch and Peak are
// diagnostics, never evidence for a mutant outcome. Deadline is carried so a
// replayed trace is self-describing when the constants change, which #64's
// codicil requires.
//
// Elapsed measures from the instant the target instruction was released, and
// Launch holds what establishing containment cost before that. Keeping them
// apart is what stops Ooze's own launch cost from being read as the mutant's
// wall time — one of the three things #75 needs preserved, alongside which
// bound fired (Tripped.Bound, absent from every other observation) and the
// captured output bytes (Output).
//
// Peak is a pointer because "no count was taken" and "zero descendants were
// seen" are different facts: #60 makes the first one true of every Serial()
// attempt, and #63 omits the runaway row rather than printing zero.
type Evidence struct {
	Deadline time.Duration
	Elapsed  time.Duration
	Launch   time.Duration
	Peak     *int
	Output   string
}

// Settled means the command reached its own conclusion inside every bound Ooze
// imposed, and the domain is drained. Exit distinguishes a pass from an
// ordinary failure. This is also where a command that timed out by its own
// internal timeout lands: it is an ordinary non-zero exit with no bound fired,
// and #75 owns whether anything may reinterpret it by reading Output.
type Settled struct {
	Exit ExitStatus
	Evidence
}

// Tripped means one of Ooze's own bounds fired. Bound says which. Live is the
// census reading at the trip, present only for FuseBound — for a deadline trip
// a count is not applicable, which a zero could not say. Exit is nil unless the
// root was seen to exit, which for a killed root it usually was not: a zero
// ExitStatus would claim the killed root passed.
type Tripped struct {
	Bound Bound
	Live  *int
	Exit  *ExitStatus // as observed after termination; diagnostic only
	Evidence
}

// Stopped means an external stop — a campaign abort or the runtime emergency
// sweep — ended the attempt before it concluded, and the domain is drained. It
// deliberately carries no exit status and no bound: #57 says an aborted attempt
// produces NO mutant evidence, and that a campaign abort accepts observations
// from already-committed attempts "only as drainage diagnostics". Collapsing
// this into Settled is how the first draft reported every in-flight mutant at
// abort time as a passing survivor.
type Stopped struct{ Evidence }

// Infrastructure means execution conditions could not be established or
// observed well enough to attribute anything to the mutant. Either nothing was
// ever created, or whatever was is proven gone — that second promise is why a
// census failure over a live tree is not one of these until the tree is
// drained.
type Infrastructure struct {
	Cause Cause
	Err   error
	Evidence
}

// DrainUnconfirmed means the domain could not be proven empty within its
// budget. It is a fatal seed for the process runtime, never a mutant outcome.
// It is also the only observation the engine emits without a proof of
// emptiness, which is what lets the driver decide from the observation's type
// alone whether Release may be called.
type DrainUnconfirmed struct {
	Residual Residual
	Last     string // the last authoritative observation, for diagnosis
	Evidence
}

func (Settled) observation()          {}
func (Tripped) observation()          {}
func (Stopped) observation()          {}
func (Infrastructure) observation()   {}
func (DrainUnconfirmed) observation() {}

// Bound names which of Ooze's own bounds fired.
type Bound uint8

const (
	FuseBound Bound = iota + 1
	DeadlineBound
)

// Cause names why execution conditions were untrustworthy. There is
// deliberately no emptiness-unobservable cause: failing to read emptiness is
// precisely not being able to prove the tree gone, which is DrainUnconfirmed's
// definition, and Infrastructure promises the opposite.
type Cause uint8

const (
	SpecInvalid Cause = iota + 1
	LaunchFailed
	CensusFailed
	SignalFailed
	ReleaseFailed
)

// Residual distinguishes #57's two unresolved-obligation variants.
type Residual uint8

const (
	ProspectiveUnresolved Residual = iota + 1
	OwnedUndrained
)

// ExitStatus is the root's exit, normalized across platforms.
type ExitStatus struct {
	Code   int
	Signal int // 0 when the root exited rather than being signalled
}

// Passed is nil-safe and on the pointer receiver on purpose: the absence of an
// exit is not a pass. A root killed on a bound has no status at all, and a
// value receiver made that indistinguishable from exit code 0 — a mutant
// reported as killed whose exit said it passed.
func (e *ExitStatus) Passed() bool { return e != nil && e.Code == 0 && e.Signal == 0 }

// ------------------------------------------------------------------ platform

// ErrNotReleased is the proof that a launch failed before the target
// instruction was released: nothing ran, and nothing needs draining. A Launch
// error that does not wrap it means something was created that could not be
// proven gone, which is a drain-unconfirmed fatal seed rather than an abort.
//
// All three supported platforms can produce this proof. Windows creates the
// root suspended and assigns it to its job before resuming, so any failure
// before the resume ran no target code. Darwin's launcher writes launch
// failures to a close-on-exec status pipe, which reports zero bytes exactly
// when the exec succeeded. Linux needs one change: its guardian currently
// returns the same exit code for "could not start the command" and "drainage
// failed", so the two are indistinguishable to the parent.
var ErrNotReleased = errors.New("target instruction never released")

// ErrLaunchBound is how an adapter reports that it gave up establishing
// containment within Spec.LaunchBound. On its own it proves nothing either way,
// so it resolves as an unresolved prospective obligation; an adapter that can
// also prove nothing ran must wrap ErrNotReleased as well.
var ErrLaunchBound = errors.New("launch bound expired")

// ErrDomainGone is a positive emptiness proof delivered by Signal. On darwin
// kill(-pgid) answers ESRCH exactly when the group has no members, and the
// first draft discarded that: thousands of futile rounds and a fatal seed
// diagnosed as a lingering descendant, on a domain the kernel had just reported
// gone.
var ErrDomainGone = errors.New("domain has no members")

// Platform is the per-operating-system half of the contract. Every method wraps
// syscalls; none of them decides anything.
//
// Every method here is synchronous, and the engine cannot preempt a syscall:
// while one is in flight the stop channel is unread, so the runtime emergency
// sweep's "single absolute bound" is cooperative at exactly these points. That
// is an obligation on implementers, not a property of this engine — each native
// operation MUST be internally bounded and non-blocking (windows: no waits on
// process handles, only job queries; darwin: one bounded sysctl buffer; linux:
// one /proc pass). Launch is the exception that gets its own explicit bound,
// because its cost is unbounded in principle rather than by accident.
type Platform interface {
	// Launch establishes the containment its platform contract promises before
	// releasing the target instruction, then starts the attempt root. It must
	// return within Spec.LaunchBound, wrapping ErrLaunchBound if it cannot.
	Launch(Spec) (Domain, error)

	// Snapshot captures operating-system process state once, so a single read
	// serves every live domain in the process runtime. On darwin that is one
	// process-table read at 115us; per-domain it would be 115us each.
	Snapshot() (Snapshot, error)
}

// Snapshot is one instant of operating-system process state.
type Snapshot interface {
	// Live counts the processes in one domain that can still execute or create
	// descendants. On darwin this is the union of a parent-identity walk and
	// process-group membership: measured, each view alone misses a descendant
	// shape the other catches. On linux it is a parent walk from the subreaper
	// guardian, which orphans cannot leave. On windows it is job membership,
	// which is stronger than either.
	//
	// The seam returns a count, so it can neither express nor check that a
	// union is over DISTINCT pids: an implementer MUST deduplicate by pid
	// before counting, or darwin's plain direct child — which both views see —
	// counts twice and the ceiling is reached at 32 descendants.
	//
	// The count is of DESCENDANTS: the attempt root is never included. The
	// three native views do not agree on that by themselves — windows'
	// JobObjectBasicProcessIdList includes the root, a linux walk seeded at the
	// guardian includes the attempt root, and darwin's two views exclude it —
	// so those two adapters subtract it. Normalizing here is what makes #60's
	// ceiling of 64 mean the same thing on every platform.
	//
	// A zombie is not live: it can neither execute nor create descendants, so
	// it is not counted here and must not keep Empty from reporting true. The
	// attempt root is itself an unreaped zombie for exactly this window.
	Live(Domain) (int, error)
}

// Domain is one owned execution domain.
//
// The root process must stay unreaped until Empty reports true, which pins both
// its pid and the pgid that equals it. This is not conservatism: the census's
// parent-identity walk is safe under pid reuse, because it rebuilds the ppid map
// from each fresh snapshot and is seeded from a pid Ooze holds as an unreaped
// zombie — but the process-group half is not. The pgid IS the root's pid, so a
// reaped root frees it, another process can be handed that pid and become
// leader of that pgid, and kern.proc.pgrp then reports a foreign group's
// members: Empty() is false forever and a domain that is actually gone produces
// a fatal DrainUnconfirmed. Measured pid wrap on the same host was 145 seconds,
// well inside one run. Release performs the reap, which is why the engine never
// releases a domain it could not prove empty.
type Domain interface {
	// RootExit delivers the root's exit exactly once. Root exit is not
	// drainage.
	//
	// The channel MUST be buffered with room for that one value, and the
	// platform's waiter MUST be able to send without blocking. The engine reads
	// it non-blockingly at the top of every iteration, again immediately before
	// each decision, and once more before it returns — so an exit delivered
	// while a census or a kill was in flight is never decided against. On an
	// unbuffered channel the waiter would block between those reads and leak
	// the handle it holds.
	RootExit() <-chan ExitStatus

	// Signal requests termination of every process in the domain. It is not a
	// barrier: on darwin, kill(-pgid) enumerates members at call time, so a
	// forking domain needs repeated signals, tens of them (unquantified). It may
	// return ErrDomainGone, which the engine reads as proof of emptiness; any
	// other error means Ooze's own containment could not be exercised.
	Signal() error

	// Empty reports whether the domain provably contains no process that can
	// execute or create descendants. It takes the snapshot rather than reading
	// process state itself, so one read serves every draining domain: a private
	// read on darwin is a ~115us union walk plus a kill that iterates the whole
	// table per domain per round, which at 14 concurrent attempts and a 1ms
	// poll was 1.82 cores sustained for up to five seconds.
	Empty(Snapshot) (bool, error)

	// Release relinquishes handles and reaps the root. Called only after Empty
	// reported true — on windows the job handle carries
	// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, so closing it kills the residual a
	// fatal seed exists to preserve, and on linux releasing the guardian
	// orphans its descendants to init and destroys the subreaper drainage
	// proof.
	Release() error
}

// --------------------------------------------------------------- the machine

// phase is the attempt's own lifecycle, distinct from the campaign's.
type phase uint8

const (
	running  phase = iota // the target is live and bounded
	draining              // a conclusion is fixed; the domain must be proven empty
	finished
)

// reason records why draining was entered, which is what the terminal
// observation is made of. Exactly one reason holds, decided by the precedence
// in advanceRunning, so settle needs no second opinion about which fact wins.
type reason uint8

const (
	byTrip  reason = iota + 1 // one of Ooze's own bounds fired
	byExit                    // the command reached its own conclusion
	byFault                   // execution conditions became unobservable
	byStop                    // an external stop: campaign abort or runtime sweep
)

// state is the whole machine. It holds no clock and no handle.
type state struct {
	phase   phase
	entry   reason
	started time.Time     // when the target instruction was released
	launch  time.Duration // what establishing containment cost before that
	drainBy time.Time     // zero until draining; thereafter monotone earliest

	peak    int
	counted bool // whether any count observation was ever taken
	trip    Bound
	live    int // the census reading at a fuse trip; see settle

	exit   ExitStatus
	exited bool

	fault    Cause // a latched infrastructure fault, reported only after drainage
	faultErr error

	rounds   int   // termination rounds requested
	probed   bool  // whether any emptiness check was ever taken
	probeErr error // the last emptiness read's error, nil when it succeeded
}

// observed is everything the driver learned since the last step. Absent facts
// are nil, so the decision never guesses.
type observed struct {
	now       time.Time
	exit      *ExitStatus // the root exited
	live      *int        // a successful census
	liveErr   error       // a failed census
	empty     *bool       // a successful emptiness check
	emptyErr  error       // a failed emptiness check
	stopBy    *time.Time  // an external stop request, from abort or the runtime sweep
	signalErr error       // the previous termination round failed
}

// note records a stop instant, keeping the earliest: a campaign abort and the
// runtime emergency sweep can both be in flight, and the tighter bound wins.
func (o *observed) note(at, now time.Time) {
	if at.IsZero() {
		at = now
	}
	if o.stopBy == nil || at.Before(*o.stopBy) {
		o.stopBy = &at
	}
}

// action is what the driver must do next. Interval is meaningful with Wait.
type action struct {
	Signal   bool          // request termination of the domain
	Wait     bool          // wait for Interval, the root's exit, or a stop
	Interval time.Duration // Cadence while running; the backoff while draining
	Done     Observation   // non-nil exactly once
}

// begin starts the machine at the instant the target instruction was released.
// The deadline runs from there and not from the launch request: #59 does not
// want Ooze's own launch cost spending the mutation deadline.
func begin(released time.Time, launch time.Duration) state {
	return state{phase: running, started: released, launch: launch}
}

// advance is the entire lifecycle policy. It reads no clock, performs no
// syscall, and is a total function of (state, spec, observed) — including on a
// finished attempt, which observes nothing more rather than panicking, so #64's
// trace replay can feed it any prefix of a trace.
//
// Trip precedence inside one observation is fuse, then root exit, then an
// unobservable census, then deadline, then stop. The fuse wins because a count
// over the ceiling is a latched fact about breadth that a later clean exit does
// not erase, and #60 makes it directly attributable. Root exit beats the
// deadline because a command that finished has real evidence, and being generous
// with the deadline is the direction #59 wants: a false deadline consumes the
// process-wide confirmation net described on #74. The deadline is decided before
// the stop so that a sweep landing in the same observation cannot erase a trip
// that had already fired — drain takes the earliest budget either way.
func advance(s state, spec Spec, obs observed) (state, action) {
	switch s.phase {
	case running:
		return advanceRunning(s, spec, obs)
	case draining:
		return advanceDraining(s, spec, obs)
	default:
		return s, action{}
	}
}

func advanceRunning(s state, spec Spec, obs observed) (state, action) {
	s = s.count(spec, obs)

	// The fuse. Contention-independent, so it needs no confirmation and beats
	// everything else in this observation.
	if spec.counts() && obs.live != nil && *obs.live > spec.Fuse {
		s.trip, s.live = FuseBound, *obs.live

		return s.drain(spec, obs, byTrip)
	}

	// The root exited. Not drainage: the domain must still be proven empty.
	if obs.exit != nil {
		s.exit, s.exited = *obs.exit, true

		return s.drain(spec, obs, byExit)
	}

	// An unobservable census matters only while the attempt is still running,
	// and only where a fuse exists to go unenforced — under Serial() no count
	// observation is made, so there is nothing to lose a mutant over. It is
	// latched rather than reported: the tree is still live, and Infrastructure
	// promises whatever was created is proven gone, so the fault is only
	// reported after drainage is proven and becomes DrainUnconfirmed otherwise.
	// Checked after the root exit, because once the command has concluded the
	// unobserved window is at most one cadence and #60 already accepts that a
	// runaway staying under the ceiling is reported on its own terms.
	if spec.counts() && obs.liveErr != nil {
		s.fault, s.faultErr = CensusFailed, obs.liveErr

		return s.drain(spec, obs, byFault)
	}

	// The deadline, before the stop so a stop cannot erase the trip.
	if !obs.now.Before(s.started.Add(spec.Deadline)) {
		s.trip = DeadlineBound

		return s.drain(spec, obs, byTrip)
	}

	// An external stop, from a campaign abort or the runtime emergency sweep.
	if obs.stopBy != nil {
		return s.drain(spec, obs, byStop)
	}

	return s, action{Wait: true, Interval: spec.Cadence}
}

func advanceDraining(s state, spec Spec, obs observed) (state, action) {
	// One shared sampler pushes readings at draining attempts too, and a fuse
	// trip at 65 whose domain forks to 4000 during the drain is a materially
	// different diagnosis, so the peak keeps rising here. A census error is
	// ignored on purpose and only here: the fuse no longer enforces anything
	// once a conclusion is fixed, and drainage is proven by Empty, not by a
	// count.
	s = s.count(spec, obs)

	// A stop arriving mid-drain can only shorten the budget. Monotone, so two
	// orderings of the same stops reach the same deadline.
	if obs.stopBy != nil && obs.stopBy.Before(s.drainBy) {
		s.drainBy = *obs.stopBy
	}

	// The first exit observed is the authoritative one; a later one is a replay
	// of the same fact and must not overwrite the status already recorded.
	if obs.exit != nil && !s.exited {
		s.exit, s.exited = *obs.exit, true
	}

	// ErrDomainGone is the kernel answering that the domain has no members.
	// Any other signal failure means Ooze's containment could not be exercised:
	// latched, so a proven drainage still reports it rather than claiming a
	// bound it never applied, and so the fatal seed's diagnosis names it.
	if errors.Is(obs.signalErr, ErrDomainGone) {
		return s.conclude(), action{Done: s.settle(spec, obs.now)}
	}
	if obs.signalErr != nil && s.fault == 0 {
		s.fault, s.faultErr = SignalFailed, obs.signalErr
	}

	if obs.empty != nil || obs.emptyErr != nil {
		s.probed, s.probeErr = true, obs.emptyErr
	}
	if obs.empty != nil && *obs.empty {
		return s.conclude(), action{Done: s.settle(spec, obs.now)}
	}

	// The budget is honoured only once the domain has actually been asked to
	// die. An emergency sweep's absolute bound is routinely already in the past
	// by the time a busy attempt observes it, and the first draft answered that
	// with a fatal seed and zero kills.
	if s.rounds > 0 && !obs.now.Before(s.drainBy) {
		return s.conclude(), action{Done: DrainUnconfirmed{
			Residual: OwnedUndrained, Last: s.diagnosis(), Evidence: s.evidence(spec, obs.now),
		}}
	}

	// Not empty and still in budget: signal again, then wait. Repeated because
	// a group kill is not fork-atomic.
	s.rounds++

	return s, action{Signal: true, Wait: true, Interval: drainInterval(spec, s.rounds)}
}

// count keeps the peak. It is gated on the profile so that a Serial() attempt
// carries no count at all, absent rather than zero.
func (s state) count(spec Spec, obs observed) state {
	if spec.counts() && obs.live != nil {
		s.peak, s.counted = max(s.peak, *obs.live), true
	}

	return s
}

// drain fixes the conclusion and enters the drain phase. It asks for neither a
// wait nor a signal, so the driver checks emptiness immediately. A domain that
// is already empty needs no kill, which is what the current darwin adapter does
// and why its census-before-signal ordering is correct rather than the defect it
// first looks like. That justification covers only the paths where the command
// concluded on its own; on the other four it is simply one cheap read, already
// amortized across every draining domain, before the first kill.
//
// The budget is the local one unless a stop arrived in the same observation
// carrying an earlier deadline. Taking the earliest matters: the runtime
// emergency sweep drives every domain against one absolute bound, and a bound
// trip landing in the same observation as the sweep must not extend it.
func (s state) drain(spec Spec, obs observed, why reason) (state, action) {
	s.phase, s.entry = draining, why
	s.drainBy = obs.now.Add(spec.Drain)
	if obs.stopBy != nil && obs.stopBy.Before(s.drainBy) {
		s.drainBy = *obs.stopBy
	}

	return s, action{}
}

func (s state) conclude() state {
	s.phase = finished

	return s
}

// settle maps a drained domain to its observation. A latched fault outranks the
// entry reason: an attempt whose census or whose kill could not be performed was
// not the bounded execution this contract promises, whatever else happened to
// it, and #57 takes an infrastructure fault as its own semantic input.
func (s state) settle(spec Spec, now time.Time) Observation {
	evidence := s.evidence(spec, now)

	switch {
	case s.fault != 0:
		return Infrastructure{Cause: s.fault, Err: s.faultErr, Evidence: evidence}
	case s.entry == byTrip:
		return Tripped{
			Bound: s.trip, Live: present(s.live, s.trip == FuseBound),
			Exit: present(s.exit, s.exited), Evidence: evidence,
		}
	case s.entry == byExit:
		return Settled{Exit: s.exit, Evidence: evidence}
	default:
		return Stopped{Evidence: evidence}
	}
}

// present is how every observation says "not applicable" rather than zero: no
// count was taken, the root never exited, no census was ever possible. Each of
// those is a different fact from a zero, and #63 prints them differently.
func present[T any](value T, ok bool) *T {
	if !ok {
		return nil
	}

	return &value
}

// diagnosis is what the fatal seed carries. Every branch names something that
// was actually observed: the first draft reported "domain observed non-empty" on
// paths where no emptiness check had been taken at all.
func (s state) diagnosis() string {
	switch {
	case s.fault == SignalFailed:
		return fmt.Sprintf("the domain could not be signalled: %v", s.faultErr)
	case s.probeErr != nil:
		return fmt.Sprintf("emptiness stayed unobservable to the drain deadline: %v", s.probeErr)
	case s.probed:
		return fmt.Sprintf("domain observed non-empty at the drain deadline, root exited=%v", s.exited)
	default:
		return "the drain deadline passed with no emptiness observation taken"
	}
}

func (s state) evidence(spec Spec, now time.Time) Evidence {
	return Evidence{
		Deadline: spec.Deadline,
		Elapsed:  now.Sub(s.started),
		Launch:   s.launch,
		Peak:     present(s.peak, s.counted),
		Output:   spec.Output,
	}
}

// drainBurst is how many rounds run at DrainPoll before the interval starts
// doubling. Emptying a forking group on darwin takes tens of rounds, so
// the burst has to cover those at the tight interval: 32 rounds at 1ms is 32ms,
// and the measured cases finish inside 25ms of it.
const drainBurst = 32

// drainInterval is the wait before the next termination round. A flat DrainPoll
// for the whole budget is what made one draining attempt perform exactly 5000
// kills and 5001 emptiness checks: on darwin each round is a ~115us union walk
// plus a kill that iterates the process table, ~130us, which at 14 concurrent
// attempts is 1.82 cores sustained for up to five seconds. Doubling after the
// burst, capped at Cadence, keeps a few tens of rounds inside the first
// ~32ms and brings a five-second worst case to about 137 rounds, because the
// tail runs at the census cadence where 5s costs 100 rounds however it is
// reached.
func drainInterval(spec Spec, round int) time.Duration {
	interval := spec.DrainPoll
	for step := round; step > drainBurst && interval < spec.Cadence; step-- {
		interval *= 2
	}
	if interval > spec.Cadence {
		return spec.Cadence
	}

	return interval
}

// ---------------------------------------------------------------- the driver

// Clock is the only nondeterminism the engine reads directly. Now and After are
// separate operations on purpose: the first draft's simulated clock could only
// advance by the interval it was asked to wait, so a zero interval stopped
// virtual time altogether and the drain-budget branch was unreachable through
// Run. After therefore delivers the instant its interval elapsed, and the engine
// adopts that instant as the next observation's "now" rather than asking again —
// a simulated clock advances when a wait completes, not because one was
// requested. Spec.Validate guarantees every interval handed to After is
// positive.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

// Run is the impure half: it turns real time and real syscalls into the
// observations advance consumes. Every decision above this line is in advance.
//
// The two reports #57's obligation ledger needs are the return of Launch, which
// resolves the prospective obligation, and the single Observation.
//
// stop is a send-only channel of absolute instants: each value is a bound the
// attempt must be finished by, and the earliest of all of them wins. It stays
// live for the whole run, because a campaign abort and the runtime emergency
// sweep are two different stops and the second must still be able to shorten
// the first. Send a real instant. Closing the channel is accepted — it is the
// idiomatic way to broadcast one abort to fourteen attempts — but a closed
// channel supplies no instant, so it is read as "the bound is now", which after
// one termination round leaves the domain unconfirmed if it has not vanished.
func Run(clock Clock, platform Platform, spec Spec, stop <-chan time.Time) Observation {
	if err := spec.Validate(); err != nil {
		return Infrastructure{
			Cause: SpecInvalid, Err: err,
			Evidence: Evidence{Deadline: spec.Deadline, Output: spec.Output},
		}
	}

	requested := clock.Now()
	domain, launchErr := platform.Launch(spec)
	released := clock.Now()

	if launchErr != nil {
		return launchFailure(spec, released.Sub(requested), launchErr)
	}

	// The deadline starts here, not at the launch request: containment and
	// fork/exec are Ooze's cost, and a launch slowed by 14-way contention on a
	// cold workspace biases straight towards a false deadline.
	current := begin(released, released.Sub(requested))
	rootExit := domain.RootExit()

	// Facts learned outside a decision — while waiting, or from the previous
	// termination round — carried into the next observation.
	var pending observed

	for {
		obs := pending
		pending = observed{}
		if obs.now.IsZero() {
			obs.now = clock.Now()
		}

		// Gathered before the syscalls and again after them. The second gather
		// is what stops a root exit delivered during a census, a kill or an
		// emptiness check from being decided against: without it the loop
		// learned of the exit only inside the select, and reported a command
		// that had finished as killed on its deadline. It also means the
		// select's random choice among ready cases decides nothing — precedence
		// lives in advance, not in the runtime's scheduler.
		gather(&obs, &rootExit, &stop, obs.now)
		observe(platform, domain, spec, current.phase, &obs)
		gather(&obs, &rootExit, &stop, obs.now)

		var act action
		current, act = advance(current, spec, obs)

		if act.Done != nil {
			done := finish(domain, act.Done, current, spec, obs.now)
			// Emptied after the last syscall, so a waiter that delivered late
			// is not left blocked on a send nobody will ever read.
			gather(&observed{}, &rootExit, &stop, obs.now)

			return done
		}
		if act.Signal {
			pending.signalErr = domain.Signal()
		}
		if act.Wait {
			select {
			case exit, ok := <-rootExit:
				if ok {
					pending.exit = &exit
				}
				rootExit = nil // the root exits once; a nil channel never fires
			case at, ok := <-stop:
				if !ok {
					stop, at = nil, obs.now
				}
				pending.note(at, obs.now)
			case fired := <-clock.After(act.Interval):
				pending.now = fired
			}
		}
	}
}

// gather takes every fact that is already available, without blocking. Only the
// root exit is once-only; stops keep arriving for as long as the run lasts.
func gather(obs *observed, rootExit *<-chan ExitStatus, stop *<-chan time.Time, now time.Time) {
	select { // a nil channel never fires, so a source already read drops out
	case exit, ok := <-*rootExit:
		if ok {
			obs.exit = &exit
		}
		*rootExit = nil
	default:
	}

	for {
		select {
		case at, ok := <-*stop:
			if !ok {
				*stop, at = nil, now
			}
			obs.note(at, now)
		default:
			return
		}
	}
}

// observe performs this phase's process-state reads. One snapshot serves both
// the count and the emptiness proof, so a draining attempt does not pay for its
// own process-table read. A Serial() attempt takes no snapshot at all while
// running — #60 makes no count observation for it, and the first draft lost
// mutants to a syscall whose result nothing consumed. It still needs one while
// draining, because emptiness is read from process state on every platform;
// that is a drainage proof, not a count, and no count is derived from it.
func observe(platform Platform, domain Domain, spec Spec, at phase, obs *observed) {
	if at == running && !spec.counts() {
		return
	}

	// Each phase reads only its own field, so one fault can be recorded as
	// both without either decision seeing the other's: advanceRunning ignores
	// emptiness and advanceDraining ignores the count.
	snapshot, snapErr := platform.Snapshot()
	if snapErr != nil {
		obs.liveErr = snapErr
		obs.emptyErr = fmt.Errorf("process state unreadable: %w", snapErr)

		return
	}

	if spec.counts() {
		live, liveErr := snapshot.Live(domain)
		obs.live, obs.liveErr = present(live, liveErr == nil), liveErr
	}

	if at == draining {
		empty, emptyErr := domain.Empty(snapshot)
		obs.empty, obs.emptyErr = present(empty, emptyErr == nil), emptyErr
	}
}

// launchFailure resolves the prospective obligation. Elapsed is zero because
// nothing ran to measure, Launch carries what the attempt did cost, and Peak is
// absent rather than zero because no count was ever taken.
func launchFailure(spec Spec, launch time.Duration, err error) Observation {
	evidence := Evidence{Deadline: spec.Deadline, Launch: launch, Output: spec.Output}

	if errors.Is(err, ErrNotReleased) {
		return Infrastructure{Cause: LaunchFailed, Err: err, Evidence: evidence}
	}

	last := fmt.Sprintf("launch failed without proof nothing ran: %v", err)
	if errors.Is(err, ErrLaunchBound) {
		last = fmt.Sprintf("launch bound %s expired without proof nothing ran: %v", spec.LaunchBound, err)
	}

	return DrainUnconfirmed{Residual: ProspectiveUnresolved, Last: last, Evidence: evidence}
}

// finish relinquishes the domain, but only when the engine can honour Release's
// precondition. DrainUnconfirmed is the one observation emitted without a proof
// of emptiness, so it is the one that must not release: closing a windows job
// carrying JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE kills the residual the fatal seed
// exists to preserve, and releasing a linux guardian orphans its descendants to
// init and destroys the drainage proof. A release failure replaces only a
// trustworthy observation: a latched infrastructure fault is the more diagnostic
// of the two and keeps its own cause.
func finish(domain Domain, done Observation, s state, spec Spec, now time.Time) Observation {
	if _, undrained := done.(DrainUnconfirmed); undrained {
		return done
	}

	err := domain.Release()
	if err == nil {
		return done
	}
	if _, latched := done.(Infrastructure); latched {
		return done
	}

	return Infrastructure{Cause: ReleaseFailed, Err: err, Evidence: s.evidence(spec, now)}
}
