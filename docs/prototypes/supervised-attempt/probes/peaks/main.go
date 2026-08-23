// Command peaks measures the peak live-descendant count of a real test command
// under two instruments at once: a parent-identity walk from the attempt root,
// and the union of that walk with the root's process-group membership.
//
// "Choose the automatic runaway fuse" (#60) set the fuse ceiling at 64 from
// peaks measured with the parent walk alone. Independent research for #61 found
// that on darwin a descendant whose intermediate parent exits reparents to
// launchd and leaves the parent walk while keeping its process group, so the
// census should be the union. A union can only raise a count, so the ceiling has
// to be re-checked against it.
//
// Both counts must come from the same sample of the same run: run-to-run
// variance on a build-heavy workload is far larger than the difference being
// measured. Each sample therefore does ONE kern.proc.all read and derives the
// walk, the group and their union from it. The verbatim census functions from
// the census probe are kept here and cross-checked against that derivation
// every -xcheck-every samples, so the single-read shortcut is evidence, not
// assumption.
//
// Each run materializes the repository into a fresh unique directory, because
// #59 established that without -trimpath the absolute package directory enters
// the build action ID: reusing a path turns the compile phase into cache hits
// and understates the peak.
//
//	go run ./peaks -label s1 -runs 3 -src /path/to/repo -- /path/to/go test -count=1 ./...
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const zombie = 5 // SZOMB, matching process_tree_darwin.go's darwinZombieProcessState.

func main() {
	src := flag.String("src", "", "repository to materialize for every run")
	runs := flag.Int("runs", 3, "runs of this scenario")
	label := flag.String("label", "scenario", "scenario label")
	gomaxprocs := flag.String("gomaxprocs", "1", "GOMAXPROCS for the child; empty leaves it ambient")
	interval := flag.Duration("interval", 10*time.Millisecond, "sampling interval")
	timeout := flag.Duration("timeout", 30*time.Minute, "per-run timeout")
	exclude := flag.String("exclude", ".git", "comma-separated repo-relative paths excluded from the copy")
	xcheckEvery := flag.Int("xcheck-every", 10, "cross-check the single-read census against the verbatim ones every N samples")
	keep := flag.Bool("keep", false, "keep the workspaces")
	flag.Parse()

	argv := flag.Args()
	if *src == "" || len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "usage: peaks -src DIR [flags] -- COMMAND [ARGS...]")
		os.Exit(2)
	}

	excluded := map[string]bool{}
	for _, entry := range strings.Split(*exclude, ",") {
		if entry = strings.TrimSpace(entry); entry != "" {
			excluded[filepath.Clean(entry)] = true
		}
	}

	fmt.Printf("scenario   : %s\n", *label)
	fmt.Printf("argv       : %s\n", strings.Join(argv, " "))
	fmt.Printf("src        : %s\n", *src)
	fmt.Printf("child env  : os.Environ()")
	if *gomaxprocs != "" {
		fmt.Printf(" + GOMAXPROCS=%s", *gomaxprocs)
	} else {
		fmt.Printf(" (ambient GOMAXPROCS, not set)")
	}
	fmt.Printf("\nexcluded   : %s\n", *exclude)
	fmt.Printf("interval   : %s\n", *interval)
	fmt.Printf("supervisor : pid %d, %d cpus\n\n", os.Getpid(), unixCPUs())

	for index := 1; index <= *runs; index++ {
		outcome := measure(*src, excluded, argv, *gomaxprocs, *interval, *timeout, *xcheckEvery, *keep)
		outcome.label = *label
		outcome.index = index
		outcome.report()
	}
}

// ------------------------------------------------------------------- measuring

type outcome struct {
	label string
	index int

	workspace string
	rootPID   int

	walkPeak  int
	groupPeak int
	unionPeak int

	// The sample where the union peaked, and what the other views said there.
	atUnion sampleFacts
	// The sample where the walk peaked.
	atWalk sampleFacts
	// The sample where group-minus-walk was widest, which need not be either.
	maxGroupOnly int
	atGroupOnly  sampleFacts
	maxWalkOnly  int

	tableMax int
	samples  int
	wall     time.Duration
	exitCode int
	exitNote string

	groupOnlyEver map[int]string
	commsEver     map[string]int
	leftovers     string

	xchecked        int
	xcheckWalkDiff  int
	xcheckGroupDiff int
	readErrors      int
}

type sampleFacts struct {
	index   int
	elapsed time.Duration
	walk    int
	group   int
	union   int
	table   int
	members string
}

func measure(
	src string, excluded map[string]bool, argv []string,
	gomaxprocs string, interval, timeout time.Duration, xcheckEvery int, keep bool,
) outcome {
	workspace, err := os.MkdirTemp("", "ooze-peaks-")
	must(err)
	// A fresh unique path per run: see #59. Never reuse, or the compile phase
	// becomes cache hits and the peak collapses.
	must(copyTree(src, workspace, excluded))

	logFile, err := os.CreateTemp("", "ooze-peaks-log-")
	must(err)

	command := exec.Command(argv[0], argv[1:]...)
	command.Dir = workspace
	command.Env = os.Environ()
	if gomaxprocs != "" {
		command.Env = append(command.Env, "GOMAXPROCS="+gomaxprocs)
	}
	command.Stdout, command.Stderr = logFile, logFile
	// Setpgid with Pgid 0 makes the child the leader of a new group, so the
	// root's pid is both the attempt root and the process-group id.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	started := time.Now()
	must(command.Start())
	root := command.Process.Pid

	result := outcome{
		workspace:     workspace,
		rootPID:       root,
		groupOnlyEver: map[int]string{},
		commsEver:     map[string]int{},
	}

	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	deadline := time.After(timeout)

	var waitErr error
	timedOut := false

collect:
	for {
		result.observe(root, time.Since(started), xcheckEvery)
		select {
		case waitErr = <-waited:
			break collect
		case <-deadline:
			timedOut = true
			_ = syscall.Kill(-root, syscall.SIGKILL)
			waitErr = <-waited

			break collect
		case <-ticker.C:
		}
	}

	result.wall = time.Since(started)
	result.exitCode = command.ProcessState.ExitCode()
	if timedOut {
		result.exitNote = "TIMED OUT after " + timeout.String() + "; killed"
	} else if waitErr != nil && result.exitCode == 0 {
		result.exitNote = waitErr.Error()
	}

	// Anything still in the root's group after the root is reaped would be a
	// straggler the run leaked. Report it rather than assume there is none.
	if leftovers := groupPIDs(root); len(leftovers) > 0 {
		result.leftovers = fmt.Sprint(sorted(leftovers))
	}

	if result.exitCode != 0 {
		result.exitNote += " | last child output: " + tail(logFile.Name(), 12)
	}
	_ = logFile.Close()
	_ = os.Remove(logFile.Name())
	if !keep {
		_ = os.RemoveAll(workspace)
	}

	return result
}

// observe takes one sample: a single kern.proc.all read from which the parent
// walk, the process group and their union are all derived, so the two
// instruments describe the same instant.
func (o *outcome) observe(root int, elapsed time.Duration, xcheckEvery int) {
	table, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		o.readErrors++

		return
	}
	o.samples++

	children := map[int][]int{}
	live := map[int]bool{}
	pgid := map[int]int{}
	ppid := map[int]int{}
	comm := map[int]string{}
	for i := range table {
		pid := int(table[i].Proc.P_pid)
		parent := int(table[i].Eproc.Ppid)
		children[parent] = append(children[parent], pid)
		live[pid] = table[i].Proc.P_stat != zombie
		pgid[pid] = int(table[i].Eproc.Pgid)
		ppid[pid] = parent
		comm[pid] = commName(table[i].Proc.P_comm)
	}

	// Parent walk: identical logic to census/main.go's walkPIDs, over the table
	// already read. Zombies are traversed but not counted.
	walk := map[int]bool{}
	queue := []int{root}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		for _, child := range children[pid] {
			if walk[child] {
				continue
			}
			if live[child] {
				walk[child] = true
			}
			queue = append(queue, child)
		}
	}

	// Process group: identical logic to census/main.go's groupPIDs, over the
	// same table. The leader is the root, which is excluded, as are zombies.
	group := map[int]bool{}
	for pid, id := range pgid {
		if id == root && pid != root && live[pid] {
			group[pid] = true
		}
	}

	union := map[int]bool{}
	for pid := range walk {
		union[pid] = true
	}
	for pid := range group {
		union[pid] = true
	}

	groupOnly := 0
	for pid := range group {
		if !walk[pid] {
			groupOnly++
			o.groupOnlyEver[pid] = fmt.Sprintf("%s ppid=%d pgid=%d", comm[pid], ppid[pid], pgid[pid])
		}
	}
	walkOnly := 0
	for pid := range walk {
		if !group[pid] {
			walkOnly++
		}
	}
	for pid := range union {
		o.commsEver[comm[pid]]++
	}

	facts := sampleFacts{
		index:   o.samples,
		elapsed: elapsed.Round(time.Millisecond),
		walk:    len(walk),
		group:   len(group),
		union:   len(union),
		table:   len(table),
	}

	if len(table) > o.tableMax {
		o.tableMax = len(table)
	}
	if len(walk) > o.walkPeak {
		o.walkPeak = len(walk)
		o.atWalk = facts
		o.atWalk.members = members(walk, comm, ppid, pgid, root)
	}
	if len(group) > o.groupPeak {
		o.groupPeak = len(group)
	}
	if len(union) > o.unionPeak {
		o.unionPeak = len(union)
		o.atUnion = facts
		o.atUnion.members = members(union, comm, ppid, pgid, root)
	}
	if groupOnly > o.maxGroupOnly {
		o.maxGroupOnly = groupOnly
		o.atGroupOnly = facts
	}
	if walkOnly > o.maxWalkOnly {
		o.maxWalkOnly = walkOnly
	}

	// Cross-check the single-read derivation against the verbatim census
	// functions, which each do their own read. A difference here is either a
	// derivation bug or genuine motion between reads; both are worth counting.
	if xcheckEvery > 0 && o.samples%xcheckEvery == 0 {
		o.xchecked++
		if !same(walkPIDs(root), walk) {
			o.xcheckWalkDiff++
		}
		if !same(groupPIDs(root), group) {
			o.xcheckGroupDiff++
		}
	}
}

func (o outcome) report() {
	fmt.Printf("RUN\t%s\t%d\twalk=%d\tgroup=%d\tunion=%d\tdelta=%d\tmax_group_only=%d\tmax_walk_only=%d\twall=%s\tsamples=%d\texit=%d\ttable_at_union_peak=%d\ttable_max=%d\troot=%d\n",
		o.label, o.index, o.walkPeak, o.groupPeak, o.unionPeak, o.unionPeak-o.walkPeak,
		o.maxGroupOnly, o.maxWalkOnly, o.wall.Round(time.Millisecond), o.samples, o.exitCode,
		o.atUnion.table, o.tableMax, o.rootPID)
	fmt.Printf("\tunion peak at sample %d (t+%s): walk=%d group=%d union=%d table=%d\n",
		o.atUnion.index, o.atUnion.elapsed, o.atUnion.walk, o.atUnion.group, o.atUnion.union, o.atUnion.table)
	fmt.Printf("\twalk  peak at sample %d (t+%s): walk=%d group=%d union=%d table=%d\n",
		o.atWalk.index, o.atWalk.elapsed, o.atWalk.walk, o.atWalk.group, o.atWalk.union, o.atWalk.table)
	if o.maxGroupOnly > 0 {
		fmt.Printf("\twidest group-only at sample %d (t+%s): walk=%d group=%d union=%d\n",
			o.atGroupOnly.index, o.atGroupOnly.elapsed, o.atGroupOnly.walk, o.atGroupOnly.group, o.atGroupOnly.union)
		fmt.Printf("\tgroup-only pids ever seen (%d): %s\n", len(o.groupOnlyEver), describe(o.groupOnlyEver))
	} else {
		fmt.Printf("\tgroup-only pids ever seen: none (the group never added anything the walk missed)\n")
	}
	fmt.Printf("\tunion-peak members: %s\n", o.atUnion.members)
	fmt.Printf("\tcommands ever counted: %s\n", commSummary(o.commsEver))
	fmt.Printf("\tcross-check: %d samples, walk mismatches %d, group mismatches %d, read errors %d\n",
		o.xchecked, o.xcheckWalkDiff, o.xcheckGroupDiff, o.readErrors)
	if o.leftovers != "" {
		fmt.Printf("\tLEFTOVERS in the root's group after reap: %s\n", o.leftovers)
	}
	if o.exitNote != "" {
		fmt.Printf("\tNOTE: %s\n", o.exitNote)
	}
	fmt.Println()
}

// ------------------------------------------------- verbatim census (unchanged)

// walkPIDs returns every live process reachable from root by following parent
// identity, excluding root itself.
func walkPIDs(root int) map[int]bool {
	table, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	must(err)

	children := map[int][]int{}
	live := map[int]bool{}
	for i := range table {
		pid := int(table[i].Proc.P_pid)
		ppid := int(table[i].Eproc.Ppid)
		children[ppid] = append(children[ppid], pid)
		live[pid] = table[i].Proc.P_stat != zombie
	}

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

// groupPIDs returns every live member of the attempt root's process group,
// excluding the leader. This mirrors process_tree_darwin.go:334-350.
func groupPIDs(group int) map[int]bool {
	table, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", group)
	must(err)

	found := map[int]bool{}
	for i := range table {
		pid := int(table[i].Proc.P_pid)
		if pid == group || table[i].Proc.P_stat == zombie {
			continue
		}
		found[pid] = true
	}

	return found
}

// ---------------------------------------------------------------- materializing

// copyTree copies src into dst, skipping the excluded repo-relative paths.
func copyTree(src, dst string, excluded map[string]bool) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if excluded[relative] || entry.Name() == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		target := filepath.Join(dst, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}

			return os.Symlink(link, target)
		case !info.Mode().IsRegular():
			return nil
		}

		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(from, to string, mode os.FileMode) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()

	destination, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(destination, source); err != nil {
		_ = destination.Close()

		return err
	}

	return destination.Close()
}

// ------------------------------------------------------------------- plumbing

func members(view map[int]bool, comm map[int]string, ppid, pgid map[int]int, root int) string {
	pids := sorted(view)
	parts := make([]string, 0, len(pids))
	for _, pid := range pids {
		mark := ""
		if pgid[pid] != root {
			mark = "!pgid"
		}
		parts = append(parts, fmt.Sprintf("%d:%s(ppid=%d)%s", pid, comm[pid], ppid[pid], mark))
	}

	return strings.Join(parts, " ")
}

func describe(entries map[int]string) string {
	pids := make([]int, 0, len(entries))
	for pid := range entries {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	parts := make([]string, 0, len(pids))
	for _, pid := range pids {
		parts = append(parts, fmt.Sprintf("%d:%s", pid, entries[pid]))
	}

	return strings.Join(parts, " ")
}

func commSummary(counts map[string]int) string {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	slices.Sort(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+strconv.Itoa(counts[name]))
	}

	return strings.Join(parts, " ")
}

func commName(raw [17]byte) string {
	out := make([]byte, 0, len(raw))
	for _, char := range raw {
		if char == 0 {
			break
		}
		out = append(out, char)
	}

	return string(out)
}

func sorted(view map[int]bool) []int {
	pids := make([]int, 0, len(view))
	for pid := range view {
		pids = append(pids, pid)
	}
	sort.Ints(pids)

	return pids
}

func same(a, b map[int]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for pid := range a {
		if !b[pid] {
			return false
		}
	}

	return true
}

func tail(path string, lines int) string {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "unreadable: " + err.Error()
	}
	all := strings.Split(strings.TrimRight(string(contents), "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}

	return strings.Join(all, " / ")
}

func unixCPUs() int {
	count, err := unix.SysctlUint32("hw.logicalcpu")
	if err != nil {
		return -1
	}

	return int(count)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
