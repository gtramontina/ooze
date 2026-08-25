//go:build ignore

// Command render shows every user-visible presentation fixed by the outcome-reporting
// resolution. See readme.md.
package main

import (
	"fmt"
	"strings"
)

const inner = 38 // visible columns between the box edges

type counts struct {
	killed, timedOut, runaway, survived int
	serial                              bool // Serial() carries no fuse, so runaway is unobservable
}

func (c counts) detected() int  { return c.killed + c.timedOut + c.runaway }
func (c counts) total() int     { return c.detected() + c.survived }
func (c counts) score() float32 { return float32(c.detected()) / float32(c.total()) }

func main() {
	scored := counts{killed: 129, timedOut: 3, runaway: 1, survived: 10}

	section("Completed, above threshold")
	summary(scored, 0.50)

	section("Completed, below threshold")
	summary(scored, 0.95)

	section("Completed under Serial() — runaway row omitted, never observed")
	summary(counts{killed: 130, timedOut: 3, survived: 10, serial: true}, 0.50)

	section("Completed, after the one-way admission fallback")
	summary(scored, 0.50)
	fmt.Println("  ! Ooze fell back to single-admission automatic after validated capacity")
	fmt.Println("    pressure. Every later automatic campaign admits one attempt at a time.")

	section("Per-mutant detail: a survivor")
	survivor("internal/laboratory/laboratory.go:37", "Comparison Invert", "")

	section("Per-mutant detail: a survivor whose confirmation reversed the primary")
	survivor("internal/gotextdiff/gotextdiff.go:14", "Integer Increment",
		"primary timed out at 41.9s with peer overlap; exclusive confirmation passed in 3.2s")

	section("Per-mutant detail: detected, one line each")
	fmt.Println("⏱  Timed out: internal/fsrepository/fsrepository.go:88 → Loop Condition")
	fmt.Println("⏱  Timed out: internal/iologger/iologger.go:19 → Integer Decrement")
	fmt.Println("     primary and confirmation both timed out")
	fmt.Println("☢  Runaway:   internal/cmdtestrunner/cmdtestrunner.go:29 → Loop Condition")
	fmt.Println("     64 live descendants")
	fmt.Println("✓  Killed:    internal/prettydiff/prettydiff.go:52 → Range Break")
	fmt.Println("     primary timed out at 39.4s with peer overlap; confirmation failed in 2.8s")
	fmt.Println("   (a killed mutant is otherwise silent; this line exists only because it was confirmed)")

	section("NoMutants")
	banner(
		"⨯ No mutants were discovered. Nothing to score.",
		"",
		"  Check WithRepositoryRoot, the IgnoreSourceFiles patterns, the WithViruses",
		"  set, and whether build constraints exclude every source file.",
	)

	section("Aborted: the baseline failed")
	banner(
		"⨯ Campaign aborted. No mutation score.",
		"",
		"  Cause: the unmutated baseline failed.",
		"  Mutation evidence requires a green suite; 0 of 143 mutants were evaluated.",
		"┄",
		"  --- FAIL: TestThing (0.02s)",
		"      thing_test.go:41: want 3, got 4",
		"  FAIL",
	)

	section("Aborted mid-run: partial diagnostics are retained, unscored")
	survivor("internal/laboratory/laboratory.go:37", "Comparison Invert", "")
	fmt.Println("⏱  Timed out: internal/fsrepository/fsrepository.go:88 → Loop Condition")
	banner(
		"⨯ Campaign aborted. No mutation score.",
		"",
		"  Cause: could not materialize a workspace from the repository snapshot.",
		"  Evaluated 140 of 143 mutants: 129 detected, 11 survived.",
		"  Those results are real, but 3 mutants were never evaluated, so no score",
		"  can be computed from them.",
		"",
		"  Artifact residue — remove manually:",
		"    /var/folders/x8/T/ooze-3f2a91/",
	)

	section("Fatal: cleanup unconfirmed — residual obligations, then panic")
	banner(
		"☠ Containment fault. Ooze cannot prove every test process has exited.",
		"",
		"  The process runtime is closed for the remainder of this process.",
		"  2 execution-domain obligations remain unresolved:",
		"    attempt 41  internal/prettydiff/prettydiff.go:52 → Range Break",
		"    attempt 77  internal/iologger/iologger.go:19 → Integer Decrement",
	)
	fmt.Println("panic: ooze: cleanup unconfirmed")

	section("Fatal: invariant violation dominates — different content, one panic")
	banner(
		"☠ Internal invariant violated. No campaign in this process is scored.",
		"",
		"  Phase:          Confirming",
		"  Rejected event: AttemptSettled(attempt 77, generation 2)",
		"  Obligations:    1 lease, 1 owned execution domain",
		"  Trace tail:     … 16 events …",
		"",
		"  1 further cause joined this epoch; peer campaigns aborted unscored.",
	)
	fmt.Println("panic: ooze: invariant violation")
}

func summary(c counts, minimum float32) {
	fmt.Println("┏" + strings.Repeat("━", inner) + "┓")
	fmt.Println(row("• Total", c.total()))
	fmt.Println(row("• Detected", c.detected()))
	fmt.Println(row("  ├ killed", c.killed))
	if c.serial {
		fmt.Println(row("  └ timed out", c.timedOut))
	} else {
		fmt.Println(row("  ├ timed out", c.timedOut))
		fmt.Println(row("  └ runaway", c.runaway))
	}
	fmt.Println(row("• Survived", c.survived))
	fmt.Println("┠" + strings.Repeat("┄", inner) + "┨")

	icon := "✓"
	if c.score() < minimum {
		icon = "⨯"
	}

	line := fmt.Sprintf(" %s Score: %8.2f (minimum: %.2f)", icon, c.score(), minimum)
	fmt.Println("┃" + line + strings.Repeat(" ", inner-len([]rune(line))) + "┃")
	fmt.Println("┗" + strings.Repeat("━", inner) + "┛")
}

func row(label string, value int) string {
	left := " " + label + ":"
	right := fmt.Sprintf("%d", value)
	pad := inner - len([]rune(left)) - len(right) - 4

	return "┃" + left + strings.Repeat(" ", pad) + right + "    ┃"
}

func survivor(where, virus, provenance string) {
	fmt.Println("┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╍┅")
	fmt.Printf("┃ 🧬 Mutant survived: %s → %s\n", where, virus)
	if provenance != "" {
		fmt.Println("┃    " + provenance)
	}
	fmt.Println("┠┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄")
	fmt.Printf("┃ --- %s (original)\n", where)
	fmt.Printf("┃ +++ %s (mutated with '%s')\n", where, virus)
	fmt.Println("┃ -\tif attempts > limit {")
	fmt.Println("┃ +\tif attempts <= limit {")
	fmt.Println("┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╍┅")
}

func banner(lines ...string) {
	fmt.Println("┏" + strings.Repeat("━", inner) + "┓")
	for _, line := range lines {
		if line == "┄" {
			fmt.Println("┠" + strings.Repeat("┄", inner) + "┨")

			continue
		}
		fmt.Println("┃ " + line)
	}
	fmt.Println("┗" + strings.Repeat("━", inner) + "┛")
}

func section(title string) {
	fmt.Printf("\n═══ %s ═══\n\n", title)
}
