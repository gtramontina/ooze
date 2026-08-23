package attempt

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The whole point of the injected clock is that these run with no sleeping and
// no real processes. t0 is an arbitrary fixed instant.
var t0 = time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

func at(offset time.Duration) time.Time { return t0.Add(offset) }

func spec() Spec {
	return Spec{
		Profile:     Automatic,
		Deadline:    20 * time.Second,
		Fuse:        64,
		Drain:       5 * time.Second,
		Cadence:     50 * time.Millisecond,
		DrainPoll:   time.Millisecond,
		LaunchBound: 30 * time.Second,
		Output:      "output.txt",
	}
}

func serialSpec() Spec {
	s := spec()
	// Serial() carries no fuse, and takes no count observation. The profile is
	// carried explicitly: nothing may infer it from a zero Fuse.
	s.Profile, s.Fuse = Serial, 0

	return s
}

func count(value int) *int               { return &value }
func exit(code int) *ExitStatus          { return &ExitStatus{Code: code} }
func yes() *bool                         { t := true; return &t }
func no() *bool                          { f := false; return &f }
func instant(o time.Duration) *time.Time { t := at(o); return &t }

// ------------------------------------------------------- the running phase

func TestRunningWaitsWhileNothingHasHappened(t *testing.T) {
	s := begin(t0, 0)

	next, act := advance(s, spec(), observed{now: at(time.Second), live: count(4)})

	if act.Done != nil || !act.Wait || act.Signal {
		t.Fatalf("want a plain wait, got %+v", act)
	}
	if next.phase != running {
		t.Fatalf("want still running, got phase %d", next.phase)
	}
	if next.peak != 4 {
		t.Fatalf("want peak 4, got %d", next.peak)
	}
}

func TestPeakIsTheHighestCensusEverSeen(t *testing.T) {
	s := begin(t0, 0)
	for i, live := range []int{3, 9, 2, 7} {
		s, _ = advance(s, spec(), observed{now: at(time.Duration(i) * time.Second), live: count(live)})
	}

	if s.peak != 9 {
		t.Fatalf("want peak 9, got %d", s.peak)
	}
}

func TestFuseTripsStrictlyAboveTheCeiling(t *testing.T) {
	for _, testCase := range []struct {
		live  int
		trips bool
	}{{63, false}, {64, false}, {65, true}} {
		t.Run(fmt.Sprint(testCase.live), func(t *testing.T) {
			next, _ := advance(begin(t0, 0), spec(), observed{now: at(time.Second), live: count(testCase.live)})

			tripped := next.phase == draining
			if tripped != testCase.trips {
				t.Fatalf("live %d: want trips=%v, got phase %d", testCase.live, testCase.trips, next.phase)
			}
		})
	}
}

func TestSerialAttemptHasNoFuseHoweverManyDescendants(t *testing.T) {
	next, act := advance(begin(t0, 0), serialSpec(), observed{now: at(time.Second), live: count(5000)})

	if next.phase != running || !act.Wait {
		t.Fatalf("a Serial() attempt must not trip a fuse; got phase %d, %+v", next.phase, act)
	}
}

func TestDeadlineTripsOnlyOnceItHasPassed(t *testing.T) {
	for _, testCase := range []struct {
		elapsed time.Duration
		trips   bool
	}{
		{19 * time.Second, false},
		{20 * time.Second, true},
		{21 * time.Second, true},
	} {
		t.Run(testCase.elapsed.String(), func(t *testing.T) {
			next, _ := advance(begin(t0, 0), spec(), observed{now: at(testCase.elapsed), live: count(2)})

			if (next.phase == draining) != testCase.trips {
				t.Fatalf("elapsed %s: want trips=%v", testCase.elapsed, testCase.trips)
			}
			if testCase.trips && next.trip != DeadlineBound {
				t.Fatalf("want DeadlineBound, got %d", next.trip)
			}
		})
	}
}

// -------------------------------------------------------------- precedence

func TestFuseBeatsTheDeadlineInOneObservation(t *testing.T) {
	next, _ := advance(begin(t0, 0), spec(), observed{now: at(30 * time.Second), live: count(90)})

	if next.trip != FuseBound {
		t.Fatalf("want FuseBound, got %d", next.trip)
	}
	if next.live != 90 {
		t.Fatalf("want the trip count recorded, got %d", next.live)
	}
}

func TestFuseBeatsACleanRootExitInOneObservation(t *testing.T) {
	next, _ := advance(begin(t0, 0), spec(), observed{now: at(time.Second), live: count(90), exit: exit(0)})

	if next.trip != FuseBound {
		t.Fatalf("a count over the ceiling is not erased by a clean exit; got trip %d", next.trip)
	}
}

func TestRootExitBeatsTheDeadlineInOneObservation(t *testing.T) {
	next, _ := advance(begin(t0, 0), spec(), observed{now: at(30 * time.Second), live: count(2), exit: exit(1)})

	if next.trip != 0 {
		t.Fatalf("a command that finished has real evidence; want no trip, got %d", next.trip)
	}
	if !next.exited || next.exit.Code != 1 {
		t.Fatalf("want the exit recorded, got %+v", next.exit)
	}
}

// ------------------------------------------------------- the draining phase

// drained runs an attempt from a running state through to its observation,
// feeding one draining observation at a time.
func drained(t *testing.T, s state, sp Spec, steps ...observed) Observation {
	t.Helper()

	for _, step := range steps {
		var act action
		s, act = advance(s, sp, step)
		if act.Done != nil {
			return act.Done
		}
	}
	t.Fatalf("no observation after %d steps", len(steps))

	return nil
}

func TestRootExitZeroAndAnEmptyDomainSettleAsAPass(t *testing.T) {
	s, _ := advance(begin(t0, 0), spec(), observed{now: at(time.Second), live: count(3), exit: exit(0)})

	got := drained(t, s, spec(), observed{now: at(time.Second), empty: yes()})

	settled, ok := got.(Settled)
	if !ok {
		t.Fatalf("want Settled, got %T: %+v", got, got)
	}
	if !settled.Exit.Passed() {
		t.Fatalf("want a pass, got %+v", settled.Exit)
	}
	if settled.Peak == nil || *settled.Peak != 3 ||
		settled.Deadline != 20*time.Second || settled.Output != "output.txt" {
		t.Fatalf("evidence not carried through: %+v", settled.Evidence)
	}
}

func TestRootExitNonZeroSettlesAsAnOrdinaryFailure(t *testing.T) {
	s, _ := advance(begin(t0, 0), spec(), observed{now: at(time.Second), live: count(1), exit: exit(1)})

	got := drained(t, s, spec(), observed{now: at(time.Second), empty: yes()})

	settled, ok := got.(Settled)
	if !ok {
		t.Fatalf("want Settled, got %T", got)
	}
	if settled.Exit.Passed() {
		t.Fatalf("want a failure, got %+v", settled.Exit)
	}
}

// This is the #75 seam, asserted rather than described. An inner command
// timeout arrives as an ordinary non-zero exit with no bound fired, and this
// contract classifies it no further.
func TestAnInnerCommandTimeoutIsIndistinguishableFromATestFailure(t *testing.T) {
	own, inner := exit(1), exit(1)

	fail, _ := advance(begin(t0, 0), spec(), observed{now: at(time.Second), live: count(1), exit: own})
	hang, _ := advance(begin(t0, 0), spec(), observed{now: at(9 * time.Second), live: count(1), exit: inner})

	left := drained(t, fail, spec(), observed{now: at(time.Second), empty: yes()}).(Settled)
	right := drained(t, hang, spec(), observed{now: at(9 * time.Second), empty: yes()}).(Settled)

	if left.Exit != right.Exit || left.Output != right.Output {
		t.Fatalf("the contract must not distinguish these; got %+v and %+v", left, right)
	}
	if left.Bound() != right.Bound() {
		t.Fatalf("neither may report a bound")
	}
}

// Bound reports which of Ooze's bounds fired. Settled never has one, which is
// the fact #75 needs.
func (Settled) Bound() Bound { return 0 }

func TestFuseTripDrainsToARunawayObservation(t *testing.T) {
	s, _ := advance(begin(t0, 0), spec(), observed{now: at(time.Second), live: count(90)})

	got := drained(t, s, spec(),
		observed{now: at(time.Second), empty: no()},
		observed{now: at(2 * time.Second), empty: yes()},
	)

	tripped, ok := got.(Tripped)
	if !ok {
		t.Fatalf("want Tripped, got %T", got)
	}
	if tripped.Bound != FuseBound || tripped.Live == nil || *tripped.Live != 90 {
		t.Fatalf("want a fuse trip at 90, got %+v", tripped)
	}
}

func TestANonEmptyDomainIsSignalledRepeatedly(t *testing.T) {
	s, _ := advance(begin(t0, 0), spec(), observed{now: at(time.Second), live: count(90)})

	for round := range 3 {
		var act action
		s, act = advance(s, spec(), observed{now: at(time.Second), empty: no()})
		if !act.Signal || !act.Wait {
			t.Fatalf("round %d: a group kill is not fork-atomic, so want signal+wait; got %+v", round, act)
		}
	}
}

func TestDrainBudgetExpiryIsUnconfirmedNotAnOutcome(t *testing.T) {
	s, _ := advance(begin(t0, 0), spec(), observed{now: at(time.Second), live: count(2), exit: exit(0)})

	got := drained(t, s, spec(),
		observed{now: at(2 * time.Second), empty: no()}, // one kill round first
		observed{now: at(7 * time.Second), empty: no()},
	)

	unconfirmed, ok := got.(DrainUnconfirmed)
	if !ok {
		t.Fatalf("an expired budget is not proof of drainage; want DrainUnconfirmed, got %T", got)
	}
	if unconfirmed.Residual != OwnedUndrained {
		t.Fatalf("want OwnedUndrained, got %d", unconfirmed.Residual)
	}
}

func TestAnUnexitedRootIsDiagnosedDifferentlyFromALingeringDescendant(t *testing.T) {
	s, _ := advance(begin(t0, 0), spec(), observed{now: at(30 * time.Second), live: count(2)})

	unexited := drained(t, s, spec(),
		observed{now: at(31 * time.Second), empty: no()},
		observed{now: at(40 * time.Second), empty: no()},
	).(DrainUnconfirmed)

	concluded, _ := advance(begin(t0, 0), spec(), observed{now: at(time.Second), live: count(2), exit: exit(0)})
	lingering := drained(t, concluded, spec(),
		observed{now: at(2 * time.Second), empty: no()},
		observed{now: at(7 * time.Second), empty: no()},
	).(DrainUnconfirmed)

	if unexited.Residual != OwnedUndrained || lingering.Residual != OwnedUndrained {
		t.Fatalf("both are owned and undrained, got %d and %d", unexited.Residual, lingering.Residual)
	}
	if unexited.Last == "" || unexited.Last == lingering.Last {
		t.Fatalf("want a distinct diagnosis for an unexited root, got %q for both", unexited.Last)
	}
}

// ------------------------------------------------------------------- stops

func TestAnExternalStopEntersDrainingWithItsOwnBudget(t *testing.T) {
	next, act := advance(begin(t0, 0), spec(), observed{
		now: at(time.Second), live: count(2), stopBy: instant(3 * time.Second),
	})

	if next.phase != draining || act.Done != nil {
		t.Fatalf("want draining, got phase %d %+v", next.phase, act)
	}
	if !next.drainBy.Equal(at(3 * time.Second)) {
		t.Fatalf("want the caller's deadline, got %s", next.drainBy)
	}
	if next.trip != 0 {
		t.Fatalf("a stop is not a bound trip; got %d", next.trip)
	}
}

// The runtime emergency sweep drives every domain against one absolute bound,
// which may be shorter than a local budget already in flight. Taking the
// earliest makes the result independent of the order the stops arrive in.
func TestStopsAreMonotoneAndOrderIndependent(t *testing.T) {
	early, late := instant(2*time.Second), instant(8*time.Second)

	forwards, _ := advance(begin(t0, 0), spec(), observed{now: at(time.Second), live: count(2), stopBy: late})
	forwards, _ = advance(forwards, spec(), observed{now: at(time.Second), empty: no(), stopBy: early})

	backwards, _ := advance(begin(t0, 0), spec(), observed{now: at(time.Second), live: count(2), stopBy: early})
	backwards, _ = advance(backwards, spec(), observed{now: at(time.Second), empty: no(), stopBy: late})

	if !forwards.drainBy.Equal(backwards.drainBy) {
		t.Fatalf("order changed the outcome: %s against %s", forwards.drainBy, backwards.drainBy)
	}
	if !forwards.drainBy.Equal(at(2 * time.Second)) {
		t.Fatalf("want the earliest deadline, got %s", forwards.drainBy)
	}
}

// A bound trip and the runtime sweep can land in the same observation. The
// first version of this contract let the trip install the local budget and
// silently discard the sweep's absolute bound, which would have let one
// tripping attempt outlive the epoch that was supposed to bound every domain.
func TestABoundTripCannotExtendTheRuntimeSweepsBound(t *testing.T) {
	for name, testCase := range map[string]struct {
		obs   observed
		trip  Bound
		entry reason
	}{
		"fuse": {
			obs:  observed{now: at(time.Second), live: count(90), stopBy: instant(2 * time.Second)},
			trip: FuseBound, entry: byTrip,
		},
		"deadline": {
			obs:  observed{now: at(30 * time.Second), live: count(2), stopBy: instant(31 * time.Second)},
			trip: DeadlineBound, entry: byTrip,
		},
		"root exit": {
			obs: observed{
				now: at(time.Second), live: count(2), exit: exit(0), stopBy: instant(2 * time.Second),
			},
			entry: byExit,
		},
	} {
		t.Run(name, func(t *testing.T) {
			next, _ := advance(begin(t0, 0), spec(), testCase.obs)

			if next.phase != draining {
				t.Fatalf("want draining, got phase %d", next.phase)
			}
			if !next.drainBy.Equal(*testCase.obs.stopBy) {
				t.Fatalf("want the sweep's bound %s, got %s", *testCase.obs.stopBy, next.drainBy)
			}
			// Asserting drainBy alone is what let the stop silently erase the
			// trip it landed with.
			if next.trip != testCase.trip || next.entry != testCase.entry {
				t.Fatalf("want trip %d entered %d, got trip %d entered %d",
					testCase.trip, testCase.entry, next.trip, next.entry)
			}
		})
	}
}

// ---------------------------------------------------------- infrastructure

func TestAFailedCensusIsInfrastructureOnlyOnceTheTreeIsProvenGone(t *testing.T) {
	next, act := advance(begin(t0, 0), spec(), observed{now: at(time.Second), liveErr: errors.New("sysctl")})

	// Infrastructure promises that whatever was created is proven gone, and the
	// tree is still running here. Concluding at once abandoned it.
	if act.Done != nil {
		t.Fatalf("want no observation over a live tree, got %T: %+v", act.Done, act.Done)
	}
	if next.phase != draining {
		t.Fatalf("a latched census fault must drain the domain, got phase %d", next.phase)
	}

	got, ok := drained(t, next, spec(), observed{now: at(2 * time.Second), empty: yes()}).(Infrastructure)
	if !ok || got.Cause != CensusFailed {
		t.Fatalf("want Infrastructure/CensusFailed after drainage, got %+v", got)
	}
}

// A census failure in the same observation as a clean exit must not abort a
// mutant whose evidence is already complete. The unobserved window is at most
// one cadence, and #60 already accepts that a runaway staying under the ceiling
// is reported on the command's own terms.
func TestACleanExitSurvivesACensusFailureInTheSameObservation(t *testing.T) {
	next, act := advance(begin(t0, 0), spec(), observed{
		now: at(time.Second), liveErr: errors.New("sysctl"), exit: exit(0),
	})

	if act.Done != nil {
		t.Fatalf("want no immediate observation, got %T: %+v", act.Done, act.Done)
	}
	if next.phase != draining || !next.exited {
		t.Fatalf("want draining with the exit recorded, got phase %d exited=%v", next.phase, next.exited)
	}
}

// A latched census fault whose tree cannot be proven gone is a fatal seed, not
// a benign abort: the promise Infrastructure makes is exactly the one that
// cannot be kept here.
func TestACensusFailureWhoseTreeCannotBeDrainedIsAFatalSeed(t *testing.T) {
	next, _ := advance(begin(t0, 0), spec(), observed{now: at(time.Second), liveErr: errors.New("sysctl")})

	got := drained(t, next, spec(),
		observed{now: at(2 * time.Second), empty: no()},
		observed{now: at(7 * time.Second), empty: no()},
	)

	if _, ok := got.(DrainUnconfirmed); !ok {
		t.Fatalf("want DrainUnconfirmed, got %T %+v", got, got)
	}
}

// Not being able to read emptiness is precisely not being able to prove the
// tree gone, which is DrainUnconfirmed's definition; Infrastructure promises the
// opposite. And the attempt is holding a budget it has not spent, so one failed
// read is not the end of it.
func TestAnUnobservableEmptinessCheckIsNotProofOfDrainage(t *testing.T) {
	s, _ := advance(begin(t0, 0), spec(), observed{now: at(time.Second), live: count(2), exit: exit(0)})

	next, act := advance(s, spec(), observed{now: at(time.Second), emptyErr: errors.New("sysctl")})

	if act.Done != nil {
		t.Fatalf("gave up on the first unreadable check with 5s of budget left: %T %+v", act.Done, act.Done)
	}
	if !act.Signal {
		t.Fatalf("want the termination rounds to continue, got %+v", act)
	}

	got := drained(t, next, spec(), observed{now: at(6 * time.Second), emptyErr: errors.New("sysctl")})

	unconfirmed, ok := got.(DrainUnconfirmed)
	if !ok {
		t.Fatalf("want DrainUnconfirmed, got %T %+v", got, got)
	}
	if !strings.Contains(unconfirmed.Last, "sysctl") {
		t.Fatalf("the diagnosis must name what was actually observed, got %q", unconfirmed.Last)
	}
}

// --------------------------------------------------------- the whole driver

func TestAProvenPreReleaseFailureNeedsNoDrainage(t *testing.T) {
	got := Run(&fakeClock{now: t0}, &fakePlatform{
		launchErr: fmt.Errorf("exec: %w", ErrNotReleased),
	}, spec(), nil)

	infra, ok := got.(Infrastructure)
	if !ok || infra.Cause != LaunchFailed {
		t.Fatalf("want Infrastructure/LaunchFailed, got %T %+v", got, got)
	}
}

func TestALaunchFailureWithoutProofIsAFatalSeed(t *testing.T) {
	got := Run(&fakeClock{now: t0}, &fakePlatform{
		launchErr: errors.New("assigned to job but could not be terminated"),
	}, spec(), nil)

	unconfirmed, ok := got.(DrainUnconfirmed)
	if !ok {
		t.Fatalf("an unproven launch failure is not an abort; got %T", got)
	}
	if unconfirmed.Residual != ProspectiveUnresolved {
		t.Fatalf("want ProspectiveUnresolved, got %d", unconfirmed.Residual)
	}
}

func TestTheDriverRunsAPassingAttemptToASettledObservation(t *testing.T) {
	domain := &fakeDomain{exit: make(chan ExitStatus, 1), empty: true}
	domain.exit <- ExitStatus{}

	got := Run(&fakeClock{now: t0}, &fakePlatform{domain: domain, live: 4}, spec(), nil)

	settled, ok := got.(Settled)
	if !ok {
		t.Fatalf("want Settled, got %T: %+v", got, got)
	}
	if !settled.Exit.Passed() {
		t.Fatalf("want a pass, got %+v", settled.Exit)
	}
	if !domain.released {
		t.Fatalf("a drained domain must be released")
	}
}

func TestTheDriverSignalsUntilTheDomainIsEmpty(t *testing.T) {
	domain := &fakeDomain{exit: make(chan ExitStatus, 1), emptyAfter: 3}
	domain.exit <- ExitStatus{Code: 1}

	got := Run(&fakeClock{now: t0}, &fakePlatform{domain: domain, live: 2}, spec(), nil)

	if _, ok := got.(Settled); !ok {
		t.Fatalf("want Settled, got %T", got)
	}
	if domain.signals < 3 {
		t.Fatalf("want at least 3 signals against a forking domain, got %d", domain.signals)
	}
}

// ------------------------------------------------------------------- fakes

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

// After advances virtual time and fires immediately, so a test never sleeps.
func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.now = c.now.Add(d)
	fired := make(chan time.Time, 1)
	fired <- c.now

	return fired
}

type fakePlatform struct {
	domain    Domain
	live      int
	launchErr error
	snapErr   error
	launches  int
}

func (p *fakePlatform) Launch(Spec) (Domain, error) {
	p.launches++
	if p.launchErr != nil {
		return nil, p.launchErr
	}

	return p.domain, nil
}

func (p *fakePlatform) Snapshot() (Snapshot, error) {
	if p.snapErr != nil {
		return nil, p.snapErr
	}

	return fakeSnapshot{live: p.live}, nil
}

type fakeSnapshot struct{ live int }

func (s fakeSnapshot) Live(Domain) (int, error) { return s.live, nil }

type fakeDomain struct {
	exit       chan ExitStatus
	empty      bool
	emptyAfter int // become empty after this many Empty calls
	calls      int
	signals    int
	released   bool
}

func (d *fakeDomain) RootExit() <-chan ExitStatus { return d.exit }

func (d *fakeDomain) Signal() error {
	d.signals++

	return nil
}

func (d *fakeDomain) Empty(Snapshot) (bool, error) {
	d.calls++
	if d.emptyAfter > 0 {
		return d.calls > d.emptyAfter, nil
	}

	return d.empty, nil
}

func (d *fakeDomain) Release() error {
	d.released = true

	return nil
}

// A census interval and a termination interval are different rates. Emptying a
// forking group on darwin takes tens of signal rounds; at the census
// cadence that is a second, at the drain interval it is milliseconds.
func TestTerminationRoundsUseTheTightIntervalNotTheCensusCadence(t *testing.T) {
	s, _ := advance(begin(t0, 0), spec(), observed{now: at(time.Second), live: count(90)})

	_, act := advance(s, spec(), observed{now: at(time.Second), empty: no()})

	if !act.Signal || !act.Wait {
		t.Fatalf("want signal+wait, got %+v", act)
	}
	if act.Interval != spec().DrainPoll {
		t.Fatalf("a termination round must not wait a whole census cadence, got %s", act.Interval)
	}
}

func TestTheCensusLoopUsesTheCadence(t *testing.T) {
	_, act := advance(begin(t0, 0), spec(), observed{now: at(time.Second), live: count(4)})

	if act.Interval != spec().Cadence {
		t.Fatalf("the running phase samples at the census cadence, got %s", act.Interval)
	}
}

// ===================================================================
// Adversarial review. Every test below FAILS against the current draft.
// ===================================================================

// R1. settle() consults s.trip and never s.exited, so an attempt that entered
// draining because of an external stop — a campaign abort — settles as
// Settled{Exit: ExitStatus{}}, and ExitStatus{}.Passed() is true. Every
// in-flight mutant at abort time is reported as a clean pass, i.e. a survivor.
func TestAnAbortedAttemptIsNotReportedAsAPassingMutant(t *testing.T) {
	s, _ := advance(begin(t0, 0), spec(), observed{
		now: at(time.Second), live: count(2), stopBy: instant(3 * time.Second),
	})

	got := drained(t, s, spec(), observed{now: at(2 * time.Second), empty: yes()})

	if settled, ok := got.(Settled); ok && settled.Exit.Passed() {
		t.Fatalf("an aborted attempt whose root never exited reports as a passing mutant: %T %+v", got, got)
	}
}

// R2. In advanceRunning the stop branch precedes the deadline branch and sets
// no trip, so a stop landing in the same observation as an expired deadline
// erases the deadline trip. The attempt then settles as a pass.
// TestABoundTripCannotExtendTheRuntimeSweepsBound/deadline feeds exactly this
// input and asserts only drainBy, so the lost trip is invisible to it.
func TestAStopDoesNotEraseTheDeadlineTrip(t *testing.T) {
	next, _ := advance(begin(t0, 0), spec(), observed{
		now: at(30 * time.Second), live: count(2), stopBy: instant(31 * time.Second),
	})

	if next.trip != DeadlineBound {
		t.Fatalf("the deadline had passed 10s earlier; want DeadlineBound, got trip %d", next.trip)
	}
}

// R3. drain() takes min(now+Drain, stopBy) with no floor, and advanceDraining
// tests the budget before it signals. A stop whose bound has already passed —
// which is what an emergency sweep's absolute epoch bound is by the time a
// busy attempt observes it — produces DrainUnconfirmed, a fatal seed, without
// a single termination round.
func TestAnExpiredStopStillGetsAtLeastOneTerminationRound(t *testing.T) {
	s, _ := advance(begin(t0, 0), spec(), observed{
		now: at(10 * time.Second), live: count(2), stopBy: instant(9 * time.Second),
	})

	_, act := advance(s, spec(), observed{now: at(10 * time.Second), empty: no()})

	if act.Done != nil {
		t.Fatalf("gave up on a live domain with zero kills: %T %+v", act.Done, act.Done)
	}
	if !act.Signal {
		t.Fatalf("want at least one termination round, got %+v", act)
	}
}

// R4. The idiomatic way to broadcast one abort to fourteen attempts is to close
// the channel. A closed time channel yields the zero time.Time, which is before
// everything, so every in-flight attempt returns the fatal DrainUnconfirmed
// seed having never signalled its domain. The contract never says the stop
// channel must be sent to rather than closed.
func TestABroadcastAbortDoesNotAbandonTheTreeUnsignalled(t *testing.T) {
	domain := &fakeDomain{exit: make(chan ExitStatus, 1)} // never exits, never empty
	stop := make(chan time.Time)
	close(stop)

	got := Run(&fakeClock{now: t0}, &fakePlatform{domain: domain, live: 2}, spec(), stop)

	if domain.signals == 0 {
		t.Fatalf("a closed stop channel abandoned a live tree with no kill at all; got %T %+v", got, got)
	}
}

// R5. A census failure while running calls conclude() immediately: the domain
// is never signalled, never proven empty, and Release is called on it. The
// resolution says "No observation is delivered before the domain is drained or
// declared unconfirmed", and Infrastructure means "whatever was is proven
// gone". Both are false on this path: the whole process tree is leaked.
func TestACensusFailureWhileRunningStillDrainsTheDomain(t *testing.T) {
	domain := &fakeDomain{exit: make(chan ExitStatus, 1), empty: true}

	got := Run(
		&fakeClock{now: t0},
		&fakePlatform{domain: domain, snapErr: errors.New("sysctl")},
		spec(), nil,
	)

	if domain.signals == 0 && domain.calls == 0 {
		t.Fatalf("concluded %T with the tree still running: no signal, no emptiness check, released=%v",
			got, domain.released)
	}
}

// R6. Domain.Release is documented "Called only after Empty reported true", but
// Run calls release() for every Done, including DrainUnconfirmed. On Windows
// the job is held by that handle with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, so
// releasing it destroys the residual the fatal seed exists to preserve.
// This test also measures the drain path's real cost: the signal count is the
// number of kill+census rounds one draining attempt performs.
func TestReleaseIsNotCalledOnATreeThatCouldNotBeProvenGone(t *testing.T) {
	sp := spec()
	sp.DrainPoll = time.Millisecond // the resolution's value; spec() leaves it 0

	domain := &fakeDomain{exit: make(chan ExitStatus, 1)} // never exits, never empty

	got := Run(&fakeClock{now: t0}, &fakePlatform{domain: domain, live: 2}, sp, nil)

	if _, ok := got.(DrainUnconfirmed); !ok {
		t.Fatalf("want DrainUnconfirmed, got %T %+v", got, got)
	}
	if domain.released {
		t.Fatalf("released an undrained domain after %d signal rounds and %d emptiness checks",
			domain.signals, domain.calls)
	}
}

// R7. A deadline trip whose domain is empty on the first drain poll concludes
// with s.exited false, so Tripped.Exit is the zero ExitStatus, whose Passed()
// is true. A mutant killed on Ooze's own deadline carries an exit status that
// says it passed.
func TestADeadlineTripDoesNotReportTheKilledRootAsPassing(t *testing.T) {
	domain := &fakeDomain{exit: make(chan ExitStatus, 1), empty: true}

	got := Run(&fakeClock{now: t0}, &fakePlatform{domain: domain, live: 2}, spec(), nil)

	tripped, ok := got.(Tripped)
	if !ok {
		t.Fatalf("want Tripped, got %T %+v", got, got)
	}
	if tripped.Exit.Passed() {
		t.Fatalf("a root killed at its deadline reports Exit.Passed()==true: %+v", tripped)
	}
}

// R8. Run gathers the root's exit only inside the select. A platform delivers
// it from a waiter goroutine, which can fire while the loop is between selects
// — reading the process table, signalling, or checking emptiness. The fact then
// sits in the channel while advance decides without it, and Run returns without
// ever looking again: the exit is dropped and a finished command is reported as
// killed at its deadline. On an unbuffered channel the waiter goroutine blocks
// forever instead, leaking the handle it holds.
func TestARootExitDeliveredDuringACensusIsNotDropped(t *testing.T) {
	domain := &fakeDomain{exit: make(chan ExitStatus, 1), empty: true}
	platform := &exitDuringCensus{domain: domain, on: 401}

	got := Run(&fakeClock{now: t0}, platform, spec(), nil)

	if _, tripped := got.(Tripped); tripped {
		t.Fatalf("the root had exited; reported as a bound trip anyway: %+v", got)
	}
	if len(domain.exit) != 0 {
		t.Fatalf("the root's exit was left in the channel and lost; got %T %+v", got, got)
	}
}

// R9. Under Serial() spec.Fuse is 0 and the resolution says no count
// observation is made, which is what TestSerialAttemptHasNoFuseHoweverMany-
// Descendants asserts. Run takes a census anyway, and advanceRunning turns its
// failure into Infrastructure/CensusFailed — losing a mutant to a syscall whose
// result nothing consumes, on the profile that has no fuse to leave unenforced.
func TestASerialAttemptIsNotAbortedByACensusItDoesNotNeed(t *testing.T) {
	domain := &fakeDomain{exit: make(chan ExitStatus, 1), empty: true}
	domain.exit <- ExitStatus{}

	got := Run(
		&fakeClock{now: t0},
		&fakePlatform{domain: domain, snapErr: errors.New("sysctl")},
		serialSpec(), nil,
	)

	if infra, ok := got.(Infrastructure); ok && infra.Cause == CensusFailed {
		t.Fatalf("Serial() carries no fuse, yet a failed census aborted the attempt: %+v", got)
	}
}

// R10. Run nils the stop channel after one read, so only one stop is ever
// observed. advanceDraining's monotone-earliest rule and
// TestStopsAreMonotoneAndOrderIndependent are therefore unreachable through the
// driver: a runtime emergency sweep arriving after a campaign abort is never
// read, and on an unbuffered channel the sweep blocks forever.
func TestASecondStopCanStillShortenTheBudget(t *testing.T) {
	sp := spec()
	sp.DrainPoll = time.Millisecond

	domain := &fakeDomain{exit: make(chan ExitStatus, 1)} // never exits, never empty
	stop := make(chan time.Time, 2)
	stop <- at(30 * time.Second) // a generous campaign abort
	stop <- at(21 * time.Second) // the sweep's absolute bound, later but tighter

	got := Run(&fakeClock{now: t0}, &fakePlatform{domain: domain, live: 2}, sp, stop)

	if len(stop) != 0 {
		t.Fatalf("%d stop(s) were never read, so the sweep's bound could not shorten anything; got %T",
			len(stop), got)
	}
}

// R11. Clock.After conflated "wait" and "advance", so a simulated clock could
// only advance by the interval it was asked to wait. A zero DrainPoll therefore
// stopped virtual time and Run never terminated, which is why the
// drain-budget-expiry branch — the one the resolution called untestable — was
// still unreachable through Run. With a real clock the same spec was a 5s busy
// loop issuing kills as fast as the kernel would take them. Two fixes: Spec is
// validated, and After delivers the instant it fired so the engine adopts it
// rather than asking a clock that never moved.
func TestTheDrainBudgetIsReachableThroughTheDriver(t *testing.T) {
	domain := &fakeDomain{exit: make(chan ExitStatus, 1)} // never empty
	domain.exit <- ExitStatus{}
	clock := &boundedClock{fakeClock: fakeClock{now: t0}, budget: 1000}

	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if err, ok := recovered.(error); ok && errors.Is(err, errClockExhausted) {
			t.Fatalf("Run did not conclude: %d waits, virtual clock at %s",
				clock.waits, clock.now.Format(time.RFC3339Nano))
		}
		panic(recovered)
	}()

	got := Run(clock, &fakePlatform{domain: domain, live: 2}, spec(), nil)

	unconfirmed, ok := got.(DrainUnconfirmed)
	if !ok {
		t.Fatalf("want the drain budget to expire into DrainUnconfirmed, got %T %+v", got, got)
	}
	if unconfirmed.Elapsed < spec().Drain {
		t.Fatalf("the budget cannot expire early; elapsed %s of %s", unconfirmed.Elapsed, spec().Drain)
	}
	if !clock.now.Equal(t0.Add(unconfirmed.Elapsed)) {
		t.Fatalf("virtual time and the reported elapsed disagree: %s against %s",
			clock.now.Sub(t0), unconfirmed.Elapsed)
	}
}

// The other half of the same defect: nothing validated Spec, so a zero interval
// reached the loop at all. It is rejected before anything is launched, because
// nothing has been created and there is nothing to drain.
func TestAnInvalidSpecIsRejectedBeforeAnythingIsLaunched(t *testing.T) {
	for name, sp := range map[string]Spec{
		"zero drain poll":   withSpec(func(s *Spec) { s.DrainPoll = 0 }),
		"no profile":        withSpec(func(s *Spec) { s.Profile = 0 }),
		"fuse under serial": withSpec(func(s *Spec) { s.Profile = Serial }),
		"poll over cadence": withSpec(func(s *Spec) { s.DrainPoll = time.Hour }),
		"no deadline":       withSpec(func(s *Spec) { s.Deadline = 0 }),
		"no launch bound":   withSpec(func(s *Spec) { s.LaunchBound = 0 }),
		"no drain budget":   withSpec(func(s *Spec) { s.Drain = 0 }),
		"no cadence":        withSpec(func(s *Spec) { s.Cadence = 0 }),
	} {
		t.Run(name, func(t *testing.T) {
			platform := &fakePlatform{domain: &fakeDomain{exit: make(chan ExitStatus, 1)}}

			got := Run(&fakeClock{now: t0}, platform, sp, nil)

			infra, ok := got.(Infrastructure)
			if !ok || infra.Cause != SpecInvalid {
				t.Fatalf("want Infrastructure/SpecInvalid, got %T %+v", got, got)
			}
			if !errors.Is(infra.Err, ErrInvalidSpec) {
				t.Fatalf("want an ErrInvalidSpec, got %v", infra.Err)
			}
			if platform.launches != 0 {
				t.Fatalf("nothing may be launched under an invalid spec, got %d", platform.launches)
			}
		})
	}
}

func withSpec(change func(*Spec)) Spec {
	sp := spec()
	change(&sp)

	return sp
}

// ------------------------------------------------------ adversarial fakes

// exitDuringCensus delivers the root's exit while the driver is between
// selects, which is where a real waiter goroutine delivers it.
type exitDuringCensus struct {
	domain *fakeDomain
	on     int
	calls  int
}

func (p *exitDuringCensus) Launch(Spec) (Domain, error) { return p.domain, nil }
func (p *exitDuringCensus) Snapshot() (Snapshot, error) { return p, nil }

func (p *exitDuringCensus) Live(Domain) (int, error) {
	p.calls++
	if p.calls == p.on {
		p.domain.exit <- ExitStatus{}
	}

	return 2, nil
}

var errClockExhausted = errors.New("clock budget exhausted without a conclusion")

type boundedClock struct {
	fakeClock
	budget int
	waits  int
}

func (c *boundedClock) After(d time.Duration) <-chan time.Time {
	c.waits++
	if c.waits > c.budget {
		panic(errClockExhausted)
	}

	return c.fakeClock.After(d)
}

// --------------------------------------------- coverage for surviving mutants
// These three pass against the draft. They are here because the original 37
// cases do not kill the corresponding mutants, in a repository whose whole
// purpose is to notice that.

// Kills `!obs.now.Before(s.drainBy)` -> `obs.now.After(s.drainBy)`. No existing
// case observes the drain deadline at exactly its instant, so the branch the
// resolution says was previously untestable is still off-by-one-untested.
func TestTheDrainBudgetExpiresAtItsInstantNotAfterIt(t *testing.T) {
	s, _ := advance(begin(t0, 0), spec(), observed{now: at(time.Second), live: count(2), exit: exit(0)})

	got := drained(t, s, spec(),
		observed{now: at(time.Second), empty: no()}, // the round the budget exists for
		observed{now: at(6 * time.Second), empty: no()},
	)

	if _, ok := got.(DrainUnconfirmed); !ok {
		t.Fatalf("the budget ran out at 6s exactly; want DrainUnconfirmed, got %T %+v", got, got)
	}
}

// Kills the removal of the monotone guard in drain(). The resolution's claim is
// "an external stop can only shorten it, never extend it", and no existing case
// pins the never-extend direction: TestStopsAreMonotoneAndOrderIndependent
// passes with the guard deleted.
func TestAStopLaterThanTheLocalBudgetCannotExtendIt(t *testing.T) {
	next, _ := advance(begin(t0, 0), spec(), observed{
		now: at(time.Second), live: count(2), stopBy: instant(30 * time.Second),
	})

	if !next.drainBy.Equal(at(6 * time.Second)) {
		t.Fatalf("a stop must never extend the local budget; want 6s, got %s", next.drainBy.Sub(t0))
	}
}

// Kills `interval = spec.DrainPoll` -> `spec.Cadence` in Run. spec() leaves
// DrainPoll zero and no driver case observes the interval, so the two rates are
// indistinguishable below advance.
func TestTheDriverActuallyWaitsTheTightIntervalWhileDraining(t *testing.T) {
	sp := spec()
	sp.DrainPoll = time.Millisecond

	domain := &fakeDomain{exit: make(chan ExitStatus, 1), emptyAfter: 2}
	domain.exit <- ExitStatus{Code: 1}
	clock := &recordingClock{fakeClock: fakeClock{now: t0}}

	Run(clock, &fakePlatform{domain: domain, live: 2}, sp, nil)

	if !clock.asked[time.Millisecond] {
		t.Fatalf("no termination round waited the drain interval; waits were %v", clock.asked)
	}
}

type recordingClock struct {
	fakeClock
	asked map[time.Duration]bool
}

func (c *recordingClock) After(d time.Duration) <-chan time.Time {
	if c.asked == nil {
		c.asked = map[time.Duration]bool{}
	}
	c.asked[d] = true

	return c.fakeClock.After(d)
}
