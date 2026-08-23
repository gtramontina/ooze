// Package check is the assertion harness the probes share. It exists because
// an earlier draft of the census probe printed the words "consistency check:
// union should equal walk + group" followed by two numbers and no comparison at
// all: the invariant inverted in 4 of 11 runs and the program exited 0 every
// time. A printed claim that nothing evaluates is worse than no claim.
//
// Every invariant a probe states goes through a Set. A Set counts how many
// samples an invariant held for, keeps the first counterexample, prints one
// line per invariant with those counts, and makes the process exit non-zero if
// anything was violated. An invariant that was never sampled is itself a
// violation, so a check cannot silently disappear.
package check

import (
	"fmt"
	"io"
	"os"
	"sort"
)

// Set is a named collection of invariants sampled any number of times each.
type Set struct {
	order    []string
	sampled  map[string]int
	held     map[string]int
	first    map[string]string
	declared map[string]bool
}

// New returns an empty Set.
func New() *Set {
	return &Set{
		sampled:  map[string]int{},
		held:     map[string]int{},
		first:    map[string]string{},
		declared: map[string]bool{},
	}
}

// Declare registers an invariant that MUST be sampled at least once. If the
// probe never samples it, Report treats that as a violation rather than
// omitting the line.
func (s *Set) Declare(names ...string) {
	for _, name := range names {
		if !s.declared[name] {
			s.declared[name] = true
			s.order = append(s.order, name)
		}
	}
}

// Sample records one evaluation of an invariant. detail is only called when the
// invariant fails, and only for the first failure, so a per-sample check on a
// hot path costs a comparison.
func (s *Set) Sample(name string, held bool, detail func() string) {
	if !s.declared[name] {
		s.Declare(name)
	}
	s.sampled[name]++
	if held {
		s.held[name]++

		return
	}
	if _, seen := s.first[name]; !seen {
		s.first[name] = detail()
	}
}

// Violations is the number of invariants that failed at least one sample or
// were never sampled at all.
func (s *Set) Violations() int {
	count := 0
	for _, name := range s.order {
		if s.sampled[name] == 0 || s.held[name] != s.sampled[name] {
			count++
		}
	}

	return count
}

// Report prints one line per invariant and returns the process exit code: 0
// when every declared invariant was sampled and held every time, 1 otherwise.
func (s *Set) Report(out io.Writer, title string) int {
	fmt.Fprintf(out, "%s\n", title)

	names := make([]string, len(s.order))
	copy(names, s.order)
	sort.SliceStable(names, func(i, j int) bool {
		return s.failing(names[i]) && !s.failing(names[j])
	})

	for _, name := range names {
		switch {
		case s.sampled[name] == 0:
			fmt.Fprintf(out, "  FAIL  %-58s never sampled\n", name)
		case s.held[name] == s.sampled[name]:
			fmt.Fprintf(out, "  ok    %-58s %d/%d samples hold\n",
				name, s.held[name], s.sampled[name])
		default:
			fmt.Fprintf(out, "  FAIL  %-58s %d/%d samples hold; first counterexample: %s\n",
				name, s.held[name], s.sampled[name], s.first[name])
		}
	}

	violations := s.Violations()
	if violations == 0 {
		fmt.Fprintf(out, "  %d invariants, 0 violated\n", len(s.order))

		return 0
	}
	fmt.Fprintf(out, "  %d invariants, %d VIOLATED - this run is not evidence\n", len(s.order), violations)

	return 1
}

func (s *Set) failing(name string) bool {
	return s.sampled[name] == 0 || s.held[name] != s.sampled[name]
}

// Must aborts the probe when a measurement could not be taken at all. A probe
// that cannot measure must not print a number.
func Must(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe aborted, no measurement taken: %v\n", err)
		os.Exit(2)
	}
}
