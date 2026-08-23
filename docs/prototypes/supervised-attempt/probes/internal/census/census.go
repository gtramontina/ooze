// Package census is the one implementation of the darwin descendant census
// that every probe in this module measures. It exists because an earlier draft
// had the census probe and the cadence probe each carry their own copy: the
// census probe published a parent walk (98-102 µs) cheaper than the bare
// kern.proc.all read the cadence probe published (115 µs), even though the walk
// strictly contains that read. Two copies of the same work can disagree; one
// cannot.
//
// # Phases
//
// Every entry point reports Phases, the clock read at each boundary of the work
// it does. The phases exist so a probe can compare parts of ONE call against
// each other — a paired comparison — instead of comparing medians of
// independent timing runs, which is hopeless when the difference is smaller
// than the noise:
//
//	Read    the kern.proc.all sysctl. Once per sampler tick for the whole runtime.
//	Index   the children and liveness maps built from that table. Once per tick.
//	Search  the breadth-first walk from one root over that index. Once per attempt.
//	Group   the kern.proc.pgrp sysctl plus its liveness filter. Once per attempt.
//	Merge   the map merge that unions the walk set with the group set.
//	Total   clock-to-clock across the whole call.
//
// Total is read from its own clock pair OUTSIDE the phase boundaries, so it is
// not the phase sum by construction: Total minus Sum is the cost of the two
// outermost clock reads, which is what Overhead reports. A probe that finds a
// phase difference no larger than that is measuring its own instrument.
package census

import (
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

// zombie is SZOMB, matching process_tree_darwin.go's
// darwinZombieProcessState. A zombie cannot execute or fork, so no census here
// counts one as a live descendant.
const zombie = 5

// Phases is the clock read at every boundary of one census call. See the
// package comment for what each field covers.
type Phases struct {
	Read   time.Duration
	Index  time.Duration
	Search time.Duration
	Group  time.Duration
	Merge  time.Duration
	Total  time.Duration
}

// Sum is the work the phases account for.
func (p Phases) Sum() time.Duration {
	return p.Read + p.Index + p.Search + p.Group + p.Merge
}

// Overhead is what Total contains that the phases do not: the intermediate
// clock reads. Any phase difference smaller than this is instrumentation, not
// measurement.
func (p Phases) Overhead() time.Duration { return p.Total - p.Sum() }

// Walk is the parent-identity census: every live process reachable from root by
// following parent identity, root excluded. Its index maps are presized from
// the table length.
func Walk(root int) (map[int]bool, Phases, error) { return walk(root, true) }

// WalkGrowing is Walk with the index maps left to grow from empty. It exists
// because the earlier draft of the census probe built them that way, and its
// published walk cost carried the growth. Presizing is the better
// implementation; keeping both measurable is how the two numbers can be
// reconciled instead of contradicting each other.
func WalkGrowing(root int) (map[int]bool, Phases, error) { return walk(root, false) }

func walk(root int, presize bool) (map[int]bool, Phases, error) {
	outerStart := time.Now()
	start := time.Now()

	table, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	afterRead := time.Now()
	if err != nil {
		return nil, Phases{}, fmt.Errorf("read kern.proc.all: %w", err)
	}

	children, live := index(table, presize)
	afterIndex := time.Now()

	found := search(children, live, root)
	end := time.Now()
	outerEnd := time.Now()

	return found, Phases{
		Read:   afterRead.Sub(start),
		Index:  afterIndex.Sub(afterRead),
		Search: end.Sub(afterIndex),
		Total:  outerEnd.Sub(outerStart),
	}, nil
}

// Group is the process-group census as the supervisor takes it: every live
// member of the group except the leader, which is the attempt root itself and
// therefore not a descendant. Mirrors process_tree_darwin.go:334-350.
func Group(pgid int) (map[int]bool, Phases, error) {
	return group(pgid, true)
}

// GroupLive is Group including the leader: every live process the group still
// contains. This is the set a drain has to empty, so the drain probe uses it
// while the census probe uses Group.
func GroupLive(pgid int) (map[int]bool, Phases, error) {
	return group(pgid, false)
}

func group(pgid int, excludeLeader bool) (map[int]bool, Phases, error) {
	outerStart := time.Now()
	start := time.Now()

	table, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pgid)
	if err != nil {
		return nil, Phases{}, fmt.Errorf("read kern.proc.pgrp %d: %w", pgid, err)
	}

	found := map[int]bool{}
	for i := range table {
		pid := int(table[i].Proc.P_pid)
		if table[i].Proc.P_stat == zombie || (excludeLeader && pid == pgid) {
			continue
		}
		found[pid] = true
	}
	end := time.Now()
	outerEnd := time.Now()

	return found, Phases{Group: end.Sub(start), Total: outerEnd.Sub(outerStart)}, nil
}

// Views is one paired sample of all three census views. All three come from the
// same Union call, so a comparison between them is within a sample.
type Views struct {
	Walk  map[int]bool
	Group map[int]bool
	Union map[int]bool
}

// Union is the census the #61 research argues for: the parent-identity walk
// unioned with the process-group census. It does the walk's work, the group's
// work and the merge in one call, and reports each as a phase, so "the union
// costs more than the walk it contains" is a paired per-sample fact rather than
// a comparison of separately timed medians.
func Union(root, pgid int) (Views, Phases, error) { return union(root, pgid, true) }

// UnionGrowing is Union with the index maps left to grow from empty, the
// variant WalkGrowing documents.
func UnionGrowing(root, pgid int) (Views, Phases, error) { return union(root, pgid, false) }

func union(root, pgid int, presize bool) (Views, Phases, error) {
	outerStart := time.Now()
	start := time.Now()

	table, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	afterRead := time.Now()
	if err != nil {
		return Views{}, Phases{}, fmt.Errorf("read kern.proc.all: %w", err)
	}

	children, live := index(table, presize)
	afterIndex := time.Now()

	walked := search(children, live, root)
	afterSearch := time.Now()

	grouped, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pgid)
	members := map[int]bool{}
	for i := range grouped {
		pid := int(grouped[i].Proc.P_pid)
		if grouped[i].Proc.P_stat == zombie || pid == pgid {
			continue
		}
		members[pid] = true
	}
	afterGroup := time.Now()
	if err != nil {
		return Views{}, Phases{}, fmt.Errorf("read kern.proc.pgrp %d: %w", pgid, err)
	}

	merged := make(map[int]bool, len(walked)+len(members))
	for pid := range walked {
		merged[pid] = true
	}
	for pid := range members {
		merged[pid] = true
	}
	end := time.Now()
	outerEnd := time.Now()

	return Views{Walk: walked, Group: members, Union: merged}, Phases{
		Read:   afterRead.Sub(start),
		Index:  afterIndex.Sub(afterRead),
		Search: afterSearch.Sub(afterIndex),
		Group:  afterGroup.Sub(afterSearch),
		Merge:  end.Sub(afterGroup),
		Total:  outerEnd.Sub(outerStart),
	}, nil
}

// Table is the bare kern.proc.all read with nothing built on top of it, timed
// the same way. It is the quantity the cadence probe publishes as the shared
// half of a sampler tick, and it is the same call Walk and Union make, so the
// walk can never come out cheaper than the read.
func Table() (int, time.Duration, error) {
	start := time.Now()
	table, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	elapsed := time.Since(start)
	if err != nil {
		return 0, elapsed, fmt.Errorf("read kern.proc.all: %w", err)
	}

	return len(table), elapsed, nil
}

func index(table []unix.KinfoProc, presize bool) (map[int][]int, map[int]bool) {
	hint := 0
	if presize {
		hint = len(table)
	}
	children := make(map[int][]int, hint)
	live := make(map[int]bool, hint)
	for i := range table {
		pid := int(table[i].Proc.P_pid)
		ppid := int(table[i].Eproc.Ppid)
		children[ppid] = append(children[ppid], pid)
		live[pid] = table[i].Proc.P_stat != zombie
	}

	return children, live
}

func search(children map[int][]int, live map[int]bool, root int) map[int]bool {
	found := map[int]bool{}
	queue := []int{root}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		for _, child := range children[pid] {
			if found[child] {
				continue
			}
			if live[child] {
				found[child] = true
			}
			queue = append(queue, child)
		}
	}

	return found
}

// Population is the live process population counted at both scopes that
// matter, because mixing them is how an earlier draft produced a headroom
// figure that compared a system-wide count against a per-uid cap.
//
// kern.maxprocperuid caps processes per REAL uid, so ThisUID is the denominator
// a per-uid headroom claim needs. SystemWide is what kern.proc.all reports and
// is the denominator for kern.maxproc only. Counts include zombies, because a
// zombie still occupies a proc slot against both caps.
type Population struct {
	UID              int
	SystemWide       int
	Zombies          int
	ThisUID          int // kern.proc.ruid <uid>: the kernel's own filter.
	ThisUIDFromTable int // kern.proc.all entries whose real uid is ours.
}

// Snapshot counts the population at both scopes, by two independent routes for
// the per-uid scope so the count can be cross-checked instead of trusted.
func Snapshot(uid int) (Population, error) {
	all, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return Population{}, fmt.Errorf("read kern.proc.all: %w", err)
	}

	population := Population{UID: uid, SystemWide: len(all)}
	for i := range all {
		if all[i].Proc.P_stat == zombie {
			population.Zombies++
		}
		if int(all[i].Eproc.Pcred.P_ruid) == uid {
			population.ThisUIDFromTable++
		}
	}

	mine, err := unix.SysctlKinfoProcSlice("kern.proc.ruid", uid)
	if err != nil {
		return population, fmt.Errorf("read kern.proc.ruid %d: %w", uid, err)
	}
	population.ThisUID = len(mine)

	return population, nil
}
