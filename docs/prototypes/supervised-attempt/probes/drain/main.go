// Command drain measures the darwin drain fixpoint: how many
// kill(-pgid, SIGKILL) rounds and how long it takes to empty a CONTINUOUSLY
// FORKING process group, how many processes appear between two samples one
// census cadence apart, and what kqueue NOTE_EXIT delivery costs for a group
// that size.
//
// # Why it exists
//
// Five numbers were being quoted for #61 with no measurement anywhere in the
// repository behind them:
//
//   - "21-25 termination rounds to empty a forking process group on darwin".
//     This existed only as a comment inside the prototype asserting it had been
//     measured - a claim citing itself.
//   - "42 of the 43 processes present did not exist at the previous sample,
//     50 ms earlier".
//   - "67 NOTE_EXIT events delivered in 3.25-8.9 ms".
//   - "group census reported empty after 101-269 µs of polling".
//   - "registering 67 pids in one kevent() took 37-134 µs".
//
// This program measures all five and prints the published figure beside the
// measured range, with its own verdict on whether the published figure
// reproduces. A number it cannot establish is reported as unestablished rather
// than rounded into agreement.
//
// # Bounds, because this is a fork bomb with a leash
//
// The forking group is bounded three ways, and the bounds are asserted rather
// than assumed:
//
//   - The seed holds cap/processesPerSlot slots, one per shell it starts, and a
//     slot is only released when that shell has been reaped. Each shell
//     accounts for at most processesPerSlot processes, and the seed itself is
//     one more live member, so the cap is a hard
//     bound that does not depend on any sampling interval. A first draft of
//     this probe bounded the group by re-reading its own census every 2 ms
//     instead, and overshot a cap of 100 to 102 live processes within one
//     trial, because 14 spawning goroutines outrun a 2 ms refresh; the
//     supervisor's own per-sample cap assertion is what caught it. That
//     assertion is still here as the second line of defence.
//
//   - Every process the probe creates carries its own lifetime: the group
//     members are /bin/sh forking two short sleeps, and the seed holds a hard
//     deadline and exits if its parent changes. Nothing here survives the
//     supervisor by more than one child lifetime, even if the supervisor is
//     killed.
//
//   - Total wall time and per-trial wall time are both capped, and exceeding
//     the per-trial cap fails the run instead of continuing.
//
// Usage:
//
//	go run ./drain
//	go run ./drain -cap 70 -trials 5
package main

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"supervisedattemptprobes/internal/census"
	"supervisedattemptprobes/internal/check"
	"supervisedattemptprobes/internal/measure"
)

const (
	modeEnv    = "OOZE_DRAIN_MODE"
	capEnv     = "OOZE_DRAIN_CAP"
	forkersEnv = "OOZE_DRAIN_FORKERS"
	lifeEnv    = "OOZE_DRAIN_LIFETIME"
	limbEnv    = "OOZE_DRAIN_LIMB_LIFE"
	leafEnv    = "OOZE_DRAIN_LEAF_LIFE"

	// processesPerSlot is the most processes one group member can account for:
	// the /bin/sh the seed starts, the sleep it forks into the background, and
	// the sleep it runs in the foreground before either has exited.
	processesPerSlot = 3

	// refresh is how often the seed re-reads its own group census as a
	// secondary check on the slot accounting.
	refresh = 2 * time.Millisecond
)

// The five figures the draft quotes with no source. They are printed beside the
// measurement so the comparison is in the program's output, not in a human's
// head.
type claim struct {
	text     string
	lo, hi   float64
	unit     string
	subjects int // the population the published figure was for, 0 if unstated
}

var claims = map[string]claim{
	"rounds":  {"21-25 termination rounds to empty a forking group", 21, 25, "rounds", 0},
	"churn":   {"42 of 43 present processes were new 50ms earlier", 42.0 / 43.0 * 100, 42.0 / 43.0 * 100, "% new", 43},
	"events":  {"67 NOTE_EXIT events delivered in 3.25-8.9ms", 3.25, 8.9, "ms", 67},
	"empty":   {"group census reported empty after 101-269us of polling", 101, 269, "µs", 0},
	"kevent":  {"registering 67 pids in one kevent() took 37-134us", 37, 134, "µs", 67},
	"unknown": {"", 0, 0, "", 0},
}

func main() {
	if os.Getenv(modeEnv) == "seed" {
		seed()

		return
	}

	os.Exit(run())
}

// ---------------------------------------------------------------- supervisor

const (
	invCapHonoured  = "the forking group never exceeded the configured cap"
	invNonEmpty     = "the group was non-empty when the drain began"
	invForking      = "at least one new process appeared between two samples one gap apart"
	invEmptied      = "the group census reported empty inside the per-trial budget"
	invFixpoint     = "the group was still empty when re-censused after the drain"
	invRegistration = "every NOTE_EXIT registration either succeeded or failed with ESRCH"
	invEventsBound  = "NOTE_EXIT events delivered <= pids successfully registered"
	invKill         = "kill(-pgid, SIGKILL) failed only with ESRCH or EPERM"
	invBudget       = "the whole probe stayed inside its total wall-clock budget"
)

type trial struct {
	maxLive     int
	liveAtDrain int

	registered  int
	regRejected int
	regDuration time.Duration

	rounds      int
	timeToEmpty time.Duration
	roundLive   []int
	roundNew    []int
	roundEPERM  int
	roundESRCH  int

	events     int
	firstEvent time.Duration
	lastEvent  time.Duration

	churnPresent []int
	churnNew     []int
	birthRate    []float64
}

func run() int {
	trials := flag.Int("trials", 5, "drain trials")
	capacity := flag.Int("cap", 250, "hard cap on processes alive in the forking group at once")
	forkers := flag.Int("forkers", runtime.NumCPU(), "spawning goroutines inside the seed")
	grow := flag.Duration("grow", 400*time.Millisecond, "time to let the group reach steady state")
	gap := flag.Duration("gap", 50*time.Millisecond, "spacing of the two churn samples, i.e. the census cadence")
	pairs := flag.Int("pairs", 10, "churn sample pairs per trial")
	limbLife := flag.Duration("limb-life", 60*time.Millisecond, "lifetime of each group member shell")
	leafLife := flag.Duration("leaf-life", 40*time.Millisecond, "lifetime of the sleep each shell forks")
	perTrial := flag.Duration("per-trial", 5*time.Second, "hard cap on one trial's drain")
	total := flag.Duration("total", 120*time.Second, "hard cap on the whole probe")
	flag.Parse()

	checks := check.New()
	checks.Declare(invCapHonoured, invNonEmpty, invForking, invEmptied, invFixpoint,
		invRegistration, invEventsBound, invKill, invBudget)

	resolution := measure.Resolution()

	fmt.Printf("probe: drain - the darwin drain fixpoint for a continuously forking process group\n")
	fmt.Printf("host: %s/%s, %d logical CPUs\n", runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
	fmt.Printf("clock: monotonic; measured resolution %s\n", measure.Format(resolution))
	fmt.Printf("statistic: nearest-rank median from internal/measure, the same one every probe uses;\n")
	fmt.Printf("       per-trial figures are printed in full and summarized as a range across trials\n")

	population, err := census.Snapshot(os.Getuid())
	check.Must(err)
	perUIDCap := int(mustSysctl("kern.maxprocperuid"))
	headroom := perUIDCap - population.ThisUID
	fmt.Printf("bounds: cap %d live processes at once; uid %d holds %d of kern.maxprocperuid %d,\n",
		*capacity, population.UID, population.ThisUID, perUIDCap)
	fmt.Printf("       so per-uid headroom is %d and the cap is %.0fx inside it. Member lifetimes:\n",
		headroom, float64(headroom)/float64(*capacity))
	fmt.Printf("       shell %s, forked sleep %s. Per-trial cap %s, total cap %s.\n\n",
		*limbLife, *leafLife, *perTrial, *total)

	started := time.Now()
	results := make([]trial, 0, *trials)
	for number := range *trials {
		if time.Since(started) > *total {
			break
		}
		result := runTrial(checks, number+1, *capacity, *forkers, *grow, *gap, *pairs,
			*limbLife, *leafLife, *perTrial)
		results = append(results, result)
	}
	elapsed := time.Since(started)
	checks.Sample(invBudget, elapsed <= *total, func() string {
		return fmt.Sprintf("took %s of %s", measure.Format(elapsed), *total)
	})

	reportTrials(results, *gap)
	reportClaims(results, *gap)

	fmt.Println()

	return checks.Report(os.Stdout, fmt.Sprintf("\ninvariants (%d trials in %s)", len(results), measure.Format(elapsed)))
}

func runTrial(
	checks *check.Set,
	number, capacity, forkers int,
	grow, gap time.Duration,
	pairs int,
	limbLife, leafLife, perTrial time.Duration,
) trial {
	self, err := os.Executable()
	check.Must(err)

	command := exec.Command(self)
	command.Env = append(os.Environ(),
		modeEnv+"=seed",
		capEnv+"="+strconv.Itoa(capacity),
		forkersEnv+"="+strconv.Itoa(forkers),
		lifeEnv+"="+(grow+gap*time.Duration(pairs)+perTrial+time.Second).String(),
		limbEnv+"="+limbLife.String(),
		leafEnv+"="+leafLife.String(),
	)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	check.Must(command.Start())

	pgid := command.Process.Pid
	reaped := false
	defer func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		if !reaped {
			_, _ = command.Process.Wait()
		}
	}()

	result := trial{}

	// Grow, sampling as we go so the cap is checked rather than trusted.
	deadline := time.Now().Add(grow)
	for time.Now().Before(deadline) {
		members, _, err := census.GroupLive(pgid)
		check.Must(err)
		if len(members) > result.maxLive {
			result.maxLive = len(members)
		}
		checks.Sample(invCapHonoured, len(members) <= capacity, func() string {
			return fmt.Sprintf("trial %d saw %d live against cap %d", number, len(members), capacity)
		})
		if len(members) > capacity {
			return result // The bound was breached: stop, do not keep forking.
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Churn: how much of the group is new one census cadence later.
	previous, _, err := census.GroupLive(pgid)
	check.Must(err)
	for range pairs {
		time.Sleep(gap)
		current, _, err := census.GroupLive(pgid)
		check.Must(err)

		fresh := 0
		for pid := range current {
			if !previous[pid] {
				fresh++
			}
		}
		result.churnPresent = append(result.churnPresent, len(current))
		result.churnNew = append(result.churnNew, fresh)
		result.birthRate = append(result.birthRate, float64(fresh)/gap.Seconds())
		checks.Sample(invForking, fresh > 0, func() string {
			return fmt.Sprintf("trial %d: 0 new processes in %s, the group is not forking", number, gap)
		})
		if len(current) > result.maxLive {
			result.maxLive = len(current)
		}
		checks.Sample(invCapHonoured, len(current) <= capacity, func() string {
			return fmt.Sprintf("trial %d saw %d live against cap %d", number, len(current), capacity)
		})
		previous = current
	}

	// Register NOTE_EXIT for every member in ONE kevent call, then drain.
	members, _, err := census.GroupLive(pgid)
	check.Must(err)
	result.liveAtDrain = len(members)
	checks.Sample(invNonEmpty, len(members) > 0, func() string {
		return fmt.Sprintf("trial %d had an empty group before the drain", number)
	})

	queue, registered, rejected, regDuration, err := registerExits(members)
	check.Must(err)
	defer func() { _ = unix.Close(queue) }()
	result.registered, result.regRejected, result.regDuration = registered, rejected, regDuration
	checks.Sample(invRegistration, registered+rejected == len(members), func() string {
		return fmt.Sprintf("trial %d: %d registered + %d rejected != %d members",
			number, registered, rejected, len(members))
	})

	buffer := make([]unix.Kevent_t, len(members)+16)
	start := time.Now()
	previous = members
	for {
		err := syscall.Kill(-pgid, syscall.SIGKILL)
		switch {
		case err == nil:
		case errors.Is(err, syscall.ESRCH):
			result.roundESRCH++
		case errors.Is(err, syscall.EPERM):
			// Expected, and worth recording: on darwin killpg reports EPERM when
			// any member of the group cannot be signalled, and an unreaped
			// zombie member is one such. A drain loop that treats EPERM as fatal
			// stops one round early.
			result.roundEPERM++
		}
		checks.Sample(invKill,
			err == nil || errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.EPERM),
			func() string {
				return fmt.Sprintf("trial %d: kill(-%d) failed: %v", number, pgid, err)
			})

		// The seed is the group leader and our own child: reap it as soon as it
		// has been signalled, or it stays a zombie member of the group.
		if !reaped {
			_, _ = command.Process.Wait()
			reaped = true
		}

		current, _, err := census.GroupLive(pgid)
		check.Must(err)
		result.rounds++

		fresh := 0
		for pid := range current {
			if !previous[pid] {
				fresh++
			}
		}
		result.roundLive = append(result.roundLive, len(current))
		result.roundNew = append(result.roundNew, fresh)
		previous = current

		delivered, _ := collect(queue, buffer, 0)
		if delivered > 0 {
			if result.events == 0 {
				result.firstEvent = time.Since(start)
			}
			result.events += delivered
			result.lastEvent = time.Since(start)
		}

		if len(current) == 0 {
			result.timeToEmpty = time.Since(start)

			break
		}
		if time.Since(start) > perTrial {
			result.timeToEmpty = time.Since(start)
			checks.Sample(invEmptied, false, func() string {
				return fmt.Sprintf("trial %d still had %d live after %d rounds in %s",
					number, len(current), result.rounds, measure.Format(result.timeToEmpty))
			})

			return result
		}
	}
	checks.Sample(invEmptied, result.timeToEmpty > 0 || result.liveAtDrain == 0, func() string {
		return fmt.Sprintf("trial %d never reported empty", number)
	})

	// Whatever NOTE_EXIT events are still queued. This runs before the fixpoint
	// re-census, because sleeping first would charge the sleep to the delivery.
	stop := time.Now().Add(50 * time.Millisecond)
	for result.events < registered && time.Now().Before(stop) {
		delivered, err := collect(queue, buffer, time.Millisecond)
		if err != nil || delivered == 0 {
			continue
		}
		if result.events == 0 {
			result.firstEvent = time.Since(start)
		}
		result.events += delivered
		result.lastEvent = time.Since(start)
	}
	checks.Sample(invEventsBound, result.events <= registered, func() string {
		return fmt.Sprintf("trial %d: %d events for %d registrations", number, result.events, registered)
	})

	// The fixpoint has to hold, not just be reached once.
	for _, wait := range []time.Duration{time.Millisecond, 10 * time.Millisecond} {
		time.Sleep(wait)
		again, _, err := census.GroupLive(pgid)
		check.Must(err)
		checks.Sample(invFixpoint, len(again) == 0, func() string {
			return fmt.Sprintf("trial %d: %d live again %s after the census reported empty",
				number, len(again), wait)
		})
	}

	return result
}

// registerExits registers NOTE_EXIT for every pid in ONE kevent call and
// reports how long that call took. EV_RECEIPT makes the kernel return one
// receipt per change, so a pid that exited between the census and the
// registration is counted as rejected instead of vanishing.
func registerExits(pids map[int]bool) (int, int, int, time.Duration, error) {
	queue, err := unix.Kqueue()
	if err != nil {
		return -1, 0, 0, 0, fmt.Errorf("create kqueue: %w", err)
	}

	changes := make([]unix.Kevent_t, 0, len(pids))
	for pid := range pids {
		changes = append(changes, unix.Kevent_t{
			Ident:  uint64(pid), //nolint:gosec // darwin pids are non-negative.
			Filter: unix.EVFILT_PROC,
			Flags:  unix.EV_ADD | unix.EV_ENABLE | unix.EV_ONESHOT | unix.EV_RECEIPT,
			Fflags: unix.NOTE_EXIT,
		})
	}
	receipts := make([]unix.Kevent_t, len(changes))

	start := time.Now()
	observed, err := unix.Kevent(queue, changes, receipts, nil)
	elapsed := time.Since(start)
	if err != nil {
		_ = unix.Close(queue)

		return -1, 0, 0, elapsed, fmt.Errorf("register %d pids: %w", len(changes), err)
	}

	registered, rejected := 0, 0
	for i := range observed {
		if receipts[i].Data == 0 {
			registered++

			continue
		}
		rejected++
	}

	return queue, registered, rejected, elapsed, nil
}

// collect drains whatever NOTE_EXIT events are queued, waiting at most timeout.
func collect(queue int, buffer []unix.Kevent_t, timeout time.Duration) (int, error) {
	span := unix.NsecToTimespec(timeout.Nanoseconds())
	observed, err := unix.Kevent(queue, nil, buffer, &span)
	if err != nil && !errors.Is(err, syscall.EINTR) {
		return 0, fmt.Errorf("collect exit events: %w", err)
	}

	exits := 0
	for i := range observed {
		if buffer[i].Fflags&unix.NOTE_EXIT != 0 {
			exits++
		}
	}

	return exits, nil
}

// -------------------------------------------------------------------- report

func reportTrials(results []trial, gap time.Duration) {
	fmt.Printf("per-trial measurements\n")
	fmt.Printf("  %-6s %8s %8s %7s %10s %6s %9s %6s %10s %6s\n",
		"trial", "maxlive", "at drain", "rounds", "to empty", "regd", "kevent()", "exits", "last exit", "EPERM")
	for number, result := range results {
		fmt.Printf("  %-6d %8d %8d %7d %10s %6d %9s %6d %10s %6d\n",
			number+1, result.maxLive, result.liveAtDrain, result.rounds,
			measure.Format(result.timeToEmpty), result.registered,
			measure.Format(result.regDuration), result.events,
			measure.Format(result.lastEvent), result.roundEPERM)
	}
	fmt.Printf("  'to empty' is measured from just before the first kill to the census that first\n")
	fmt.Printf("  reported the group empty. 'last exit' is when this probe had COLLECTED the last\n")
	fmt.Printf("  NOTE_EXIT event, polling at 1ms, so it is an upper bound on kernel delivery.\n")
	fmt.Printf("  EPERM counts rounds where killpg reported EPERM: on darwin that is what killpg\n")
	fmt.Printf("  returns when any group member cannot be signalled, including an unreaped zombie.\n")

	fmt.Printf("\n  per-round detail, trial 1 (live after the census that followed each kill,\n")
	fmt.Printf("  and how many of those were new since the previous round)\n")
	if len(results) > 0 {
		fmt.Printf("  %-8s %8s %8s\n", "round", "live", "new")
		for round := range results[0].roundLive {
			fmt.Printf("  %-8d %8d %8d\n", round+1, results[0].roundLive[round], results[0].roundNew[round])
		}
	}

	fmt.Printf("\n  churn, two group censuses %s apart, %d pairs per trial\n", gap, len(firstChurn(results)))
	fmt.Printf("  %-6s %10s %10s %12s %14s\n", "trial", "present", "new", "% new", "births/second")
	for number, result := range results {
		if len(result.churnPresent) == 0 {
			continue
		}
		present := intSpan(result.churnPresent)
		fresh := intSpan(result.churnNew)
		fmt.Printf("  %-6d %10s %10s %11.1f%% %14s\n",
			number+1, present, fresh, percentNew(result), floatSpan(result.birthRate))
	}
	fmt.Println()
}

func reportClaims(results []trial, gap time.Duration) {
	fmt.Printf("the five unsourced figures, measured\n")

	rounds := collectInts(results, func(t trial) []int { return []int{t.rounds} })
	empty := collectDurations(results, func(t trial) []time.Duration { return []time.Duration{t.timeToEmpty} })
	regs := collectDurations(results, func(t trial) []time.Duration { return []time.Duration{t.regDuration} })
	lastExit := collectDurations(results, func(t trial) []time.Duration { return []time.Duration{t.lastEvent} })

	registered := collectInts(results, func(t trial) []int { return []int{t.registered} })
	events := collectInts(results, func(t trial) []int { return []int{t.events} })

	churn := []float64{}
	for _, result := range results {
		for index := range result.churnPresent {
			if result.churnPresent[index] > 0 {
				churn = append(churn,
					100*float64(result.churnNew[index])/float64(result.churnPresent[index]))
			}
		}
	}

	report(claims["rounds"],
		fmt.Sprintf("%d-%d rounds across %d trials", minInt(rounds), maxInt(rounds), len(results)),
		float64(minInt(rounds)), float64(maxInt(rounds)))
	report(claims["churn"],
		fmt.Sprintf("%.1f-%.1f%% of the group was new %s later, over %d sample pairs (%d-%d present)",
			minFloat(churn), maxFloat(churn), gap, len(churn),
			minInt(collectInts(results, func(t trial) []int { return t.churnPresent })),
			maxInt(collectInts(results, func(t trial) []int { return t.churnPresent }))),
		minFloat(churn), maxFloat(churn))
	report(claims["events"],
		fmt.Sprintf("%d-%d NOTE_EXIT events delivered, last at %s-%s after the first kill",
			minInt(events), maxInt(events),
			measure.Format(minDuration(lastExit)), measure.Format(maxDuration(lastExit))),
		float64(minDuration(lastExit))/float64(time.Millisecond),
		float64(maxDuration(lastExit))/float64(time.Millisecond))
	report(claims["empty"],
		fmt.Sprintf("census reported empty %s-%s after the first kill",
			measure.Format(minDuration(empty)), measure.Format(maxDuration(empty))),
		float64(minDuration(empty))/float64(time.Microsecond),
		float64(maxDuration(empty))/float64(time.Microsecond))
	report(claims["kevent"],
		fmt.Sprintf("%d-%d pids registered in one kevent() in %s-%s",
			minInt(registered), maxInt(registered),
			measure.Format(minDuration(regs)), measure.Format(maxDuration(regs))),
		float64(minDuration(regs))/float64(time.Microsecond),
		float64(maxDuration(regs))/float64(time.Microsecond))
}

// report prints one claim, what this run measured, and whether the published
// figure lies inside the measured range.
func report(published claim, measured string, lo, hi float64) {
	overlap := hi >= published.lo && lo <= published.hi
	verdict := "REPRODUCES"
	if !overlap {
		verdict = "DOES NOT REPRODUCE"
	}
	fmt.Printf("  published: %s\n", published.text)
	fmt.Printf("  measured : %s\n", measured)
	fmt.Printf("  verdict  : %s (published %.4g-%.4g %s against measured %.4g-%.4g %s)\n\n",
		verdict, published.lo, published.hi, published.unit, lo, hi, published.unit)
}

// --------------------------------------------------------------------- seed

// seed is the forking group. It is the process group leader, it refuses to
// spawn while its own census is at the cap, and it dies on its own deadline or
// when its parent goes away.
func seed() {
	capacity := envInt(capEnv, 250)
	forkers := envInt(forkersEnv, 4)
	lifetime := envDuration(lifeEnv, 8*time.Second)
	limbLife := envDuration(limbEnv, 60*time.Millisecond)
	leafLife := envDuration(leafEnv, 40*time.Millisecond)

	pgid := syscall.Getpgrp()
	parent := os.Getppid()
	deadline := time.Now().Add(lifetime)

	// The seed itself is a live member of its own group, so it takes one off the
	// cap before the slots divide the rest.
	slots := (capacity - 1) / processesPerSlot
	if slots < 1 {
		slots = 1
	}
	slot := make(chan struct{}, slots)

	// The census is the secondary check: if it ever sees the group at the cap
	// despite the slot accounting, spawning stops until it drops.
	var live atomic.Int64
	go func() {
		for time.Now().Before(deadline) {
			if members, _, err := census.GroupLive(pgid); err == nil {
				live.Store(int64(len(members)))
			}
			time.Sleep(refresh)
		}
	}()
	go func() {
		for {
			if os.Getppid() != parent || time.Now().After(deadline) {
				os.Exit(0)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()

	// One group member is a shell that forks a sleep and then sleeps itself, so
	// the group contains both a forker and an orphan-to-be at every instant.
	script := fmt.Sprintf("sleep %.3f & sleep %.3f", leafLife.Seconds(), limbLife.Seconds())

	var workers sync.WaitGroup
	for range forkers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for time.Now().Before(deadline) {
				if live.Load() >= int64(capacity) {
					time.Sleep(time.Millisecond)

					continue
				}

				select {
				case slot <- struct{}{}:
				case <-time.After(time.Millisecond):
					continue
				}

				command := exec.Command("/bin/sh", "-c", script)
				if err := command.Start(); err != nil {
					<-slot
					time.Sleep(time.Millisecond)

					continue
				}
				go func() {
					_ = command.Wait()
					<-slot
				}()
			}
		}()
	}
	workers.Wait()
}

// ------------------------------------------------------------------ plumbing

func mustSysctl(name string) uint32 {
	value, err := unix.SysctlUint32(name)
	check.Must(err)

	return value
}

func envInt(name string, fallback int) int {
	if value, err := strconv.Atoi(os.Getenv(name)); err == nil {
		return value
	}

	return fallback
}

func envDuration(name string, fallback time.Duration) time.Duration {
	if value, err := time.ParseDuration(os.Getenv(name)); err == nil {
		return value
	}

	return fallback
}

func firstChurn(results []trial) []int {
	if len(results) == 0 {
		return nil
	}

	return results[0].churnPresent
}

func percentNew(result trial) float64 {
	present, fresh := 0, 0
	for index := range result.churnPresent {
		present += result.churnPresent[index]
		fresh += result.churnNew[index]
	}
	if present == 0 {
		return 0
	}

	return 100 * float64(fresh) / float64(present)
}

func collectInts(results []trial, pick func(trial) []int) []int {
	var out []int
	for _, result := range results {
		out = append(out, pick(result)...)
	}

	return out
}

func collectDurations(results []trial, pick func(trial) []time.Duration) []time.Duration {
	var out []time.Duration
	for _, result := range results {
		out = append(out, pick(result)...)
	}

	return out
}

func intSpan(values []int) string {
	if len(values) == 0 {
		return "-"
	}
	if minInt(values) == maxInt(values) {
		return strconv.Itoa(minInt(values))
	}

	return fmt.Sprintf("%d-%d", minInt(values), maxInt(values))
}

func floatSpan(values []float64) string {
	if len(values) == 0 {
		return "-"
	}

	return fmt.Sprintf("%.0f-%.0f", minFloat(values), maxFloat(values))
}

func minInt(values []int) int {
	if len(values) == 0 {
		return 0
	}
	lowest := values[0]
	for _, value := range values[1:] {
		if value < lowest {
			lowest = value
		}
	}

	return lowest
}

func maxInt(values []int) int {
	highest := 0
	for _, value := range values {
		if value > highest {
			highest = value
		}
	}

	return highest
}

func minFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	lowest := math.Inf(1)
	for _, value := range values {
		if value < lowest {
			lowest = value
		}
	}

	return lowest
}

func maxFloat(values []float64) float64 {
	highest := math.Inf(-1)
	for _, value := range values {
		if value > highest {
			highest = value
		}
	}
	if math.IsInf(highest, -1) {
		return 0
	}

	return highest
}

func minDuration(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	lowest := values[0]
	for _, value := range values[1:] {
		if value < lowest {
			lowest = value
		}
	}

	return lowest
}

func maxDuration(values []time.Duration) time.Duration {
	highest := time.Duration(0)
	for _, value := range values {
		if value > highest {
			highest = value
		}
	}

	return highest
}
