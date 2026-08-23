// Command postexit measures which census can still see a live descendant AFTER
// the attempt root has exited.
//
// This is the drainage question, and it is not the same as the fuse question.
// A fuse observes a LIVE root, so a walk of the parent-pid graph from that root
// reaches every descendant that has not been reparented. Drainage is judged
// after root exit, and the #66 session reports that there a parent walk returns
// zero, because every descendant has been reparented to process 1 — making the
// parent-identity correction useless for the drain proof.
//
// That claim is decisive for whether the drain predicate should be a union, so
// it is measured here rather than accepted. The four escape shapes are the same
// ones the census probe plants; what differs is that the root exits before the
// census is taken.
//
//	go run ./postexit
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	modeEnv  = "OOZE_POSTEXIT_MODE"
	dirEnv   = "OOZE_POSTEXIT_DIR"
	shapeEnv = "OOZE_POSTEXIT_SHAPE"

	zombie = 5 // SZOMB

	settle = 500 * time.Millisecond
	linger = 15 * time.Second
)

// Each shape names how the descendant leaves, if it does. "plain" outlives the
// root without escaping anything; "orphan" is reparented by a double fork;
// "regrouped" calls setpgid; "orphan-setsid" does both.
var shapes = []string{"plain", "orphan", "regrouped", "orphan-setsid"}

func main() {
	switch os.Getenv(modeEnv) {
	case "root":
		root()
	case "middle":
		middle()
	case "leaf":
		leaf()
	default:
		supervise()
	}
}

func supervise() {
	dir, err := os.MkdirTemp("", "ooze-postexit-")
	must(err)
	defer func() { _ = os.RemoveAll(dir) }()

	self, err := os.Executable()
	must(err)

	root := exec.Command(self)
	root.Env = append(os.Environ(), modeEnv+"=root", dirEnv+"="+dir)
	root.Stdout, root.Stderr = os.Stdout, os.Stderr
	// The attempt root leads its own process group, as process_tree_darwin.go
	// arranges for its launcher.
	root.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	must(root.Start())

	rootPID := root.Process.Pid
	defer func() {
		_ = syscall.Kill(-rootPID, syscall.SIGKILL)
		for _, pid := range readLabels(dir) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}()

	time.Sleep(settle)
	labels := readLabels(dir)

	fmt.Printf("attempt root pid %d, process group %d\n", rootPID, rootPID)
	fmt.Printf("descendants planted: %d\n\n", len(labels))

	// Census while the root is still alive, for the contrast.
	beforeWalk, beforeGroup := walkPIDs(rootPID), groupPIDs(rootPID)

	// Now let the root exit and reap it, exactly as a supervisor does when it
	// observes root exit. Root exit is not drainage: every descendant below is
	// still alive.
	must(signalRootToExit(dir))
	state, err := root.Process.Wait()
	must(err)
	fmt.Printf("root exited: %s\n", state)

	// #66's claim is about this instant.
	afterWalk, afterGroup := walkPIDs(rootPID), groupPIDs(rootPID)

	fmt.Printf("\n%-14s  %-22s  %-22s\n", "", "ROOT ALIVE", "ROOT EXITED (drainage)")
	fmt.Printf("%-14s  %-10s %-11s  %-10s %-11s\n", "shape", "walk", "group", "walk", "group")
	fmt.Printf("%-14s  %-10s %-11s  %-10s %-11s\n", "-----", "----", "-----", "----", "-----")
	for _, shape := range shapes {
		pid, ok := labels[shape]
		if !ok {
			fmt.Printf("%-14s  %s\n", shape, "NOT PLANTED")

			continue
		}
		fmt.Printf("%-14s  %-10s %-11s  %-10s %-11s   (pid %d, ppid now %d)\n",
			shape,
			seen(beforeWalk, pid), seen(beforeGroup, pid),
			seen(afterWalk, pid), seen(afterGroup, pid),
			pid, parentOf(pid))
	}

	fmt.Printf("\ncounts (live, zombies excluded)\n")
	fmt.Printf("  root alive : walk %d, group %d, union %d\n",
		len(beforeWalk), len(beforeGroup), len(unionOf(beforeWalk, beforeGroup)))
	fmt.Printf("  root exited: walk %d, group %d, union %d\n",
		len(afterWalk), len(afterGroup), len(unionOf(afterWalk, afterGroup)))

	fmt.Printf("\nthe drainage question\n")
	stillAlive := 0
	for _, pid := range labels {
		if alive(pid) {
			stillAlive++
		}
	}
	after := unionOf(afterWalk, afterGroup)
	fmt.Printf("  descendants actually still alive after root exit : %d\n", stillAlive)
	fmt.Printf("  of those, visible to a parent walk               : %d\n", len(afterWalk))
	fmt.Printf("  of those, visible to a process-group census      : %d\n", len(afterGroup))
	fmt.Printf("  of those, visible to the union                   : %d\n", len(after))
	if len(afterWalk) == 0 && stillAlive > 0 {
		fmt.Printf("  => a parent walk from an exited root is BLIND: #66's claim holds for the walk\n")
	}
	if len(afterGroup) > 0 {
		fmt.Printf("  => the process-group census still sees %d, so the two do NOT fail identically\n", len(afterGroup))
	} else if stillAlive > 0 {
		fmt.Printf("  => the process-group census is blind too: both fail identically\n")
	}
}

// ------------------------------------------------------------------- census

func walkPIDs(root int) map[int]bool {
	table, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	must(err)

	children := map[int][]int{}
	live := map[int]bool{}
	for i := range table {
		pid := int(table[i].Proc.P_pid)
		children[int(table[i].Eproc.Ppid)] = append(children[int(table[i].Eproc.Ppid)], pid)
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

func groupPIDs(group int) map[int]bool {
	table, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", group)
	if err != nil {
		// An empty or vanished group is not an error worth aborting for.
		return map[int]bool{}
	}

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

func parentOf(pid int) int {
	table, err := unix.SysctlKinfoProcSlice("kern.proc.pid", pid)
	if err != nil || len(table) == 0 {
		return -1
	}

	return int(table[0].Eproc.Ppid)
}

func alive(pid int) bool {
	table, err := unix.SysctlKinfoProcSlice("kern.proc.pid", pid)
	if err != nil || len(table) == 0 {
		return false
	}

	return table[0].Proc.P_stat != zombie
}

// -------------------------------------------------------------------- tree

func root() {
	dir := os.Getenv(dirEnv)
	self, _ := os.Executable()

	spawn(self, dir, "leaf", "plain", false)
	spawn(self, dir, "middle", "orphan", false)
	spawn(self, dir, "leaf", "regrouped", false)
	spawn(self, dir, "middle", "orphan-setsid", true)

	// Wait to be told to exit, so the supervisor can census a live root first.
	for {
		if _, err := os.Stat(filepath.Join(dir, "exit-now")); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func middle() {
	dir := os.Getenv(dirEnv)
	self, _ := os.Executable()
	spawn(self, dir, "leaf", os.Getenv(shapeEnv), os.Getenv("OOZE_POSTEXIT_SETSID") == "1")
	// Exit at once, orphaning the leaf.
}

func leaf() {
	shape := os.Getenv(shapeEnv)
	if shape == "regrouped" {
		_ = syscall.Setpgid(0, 0)
	}
	if os.Getenv("OOZE_POSTEXIT_SETSID") == "1" {
		_, _ = unix.Setsid()
	}
	_ = os.WriteFile(
		filepath.Join(os.Getenv(dirEnv), shape),
		[]byte(strconv.Itoa(os.Getpid())),
		0o600,
	)
	time.Sleep(linger)
}

func spawn(self, dir, mode, shape string, setsid bool) {
	command := exec.Command(self)
	command.Env = append(os.Environ(),
		modeEnv+"="+mode,
		dirEnv+"="+dir,
		shapeEnv+"="+shape,
		"OOZE_POSTEXIT_SETSID="+flag(setsid),
	)
	_ = command.Start()
	if mode == "middle" {
		go func() { _, _ = command.Process.Wait() }()
	}
}

func signalRootToExit(dir string) error {
	return os.WriteFile(filepath.Join(dir, "exit-now"), []byte("go"), 0o600)
}

// ----------------------------------------------------------------- plumbing

func readLabels(dir string) map[string]int {
	labels := map[string]int{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return labels
	}
	for _, entry := range entries {
		if entry.Name() == "exit-now" {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(string(contents))
		if err != nil {
			continue
		}
		labels[entry.Name()] = pid
	}

	return labels
}

func seen(view map[int]bool, pid int) string {
	if view[pid] {
		return "yes"
	}

	return "MISSED"
}

func unionOf(a, b map[int]bool) map[int]bool {
	out := map[int]bool{}
	for pid := range a {
		out[pid] = true
	}
	for pid := range b {
		out[pid] = true
	}

	return out
}

func sortedKeys(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for pid := range m {
		out = append(out, pid)
	}
	sort.Ints(out)

	return out
}

func flag(value bool) string {
	if value {
		return "1"
	}

	return "0"
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
