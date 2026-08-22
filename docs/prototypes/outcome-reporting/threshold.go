//go:build ignore

// Command threshold measures whether Ooze's mutation-score threshold comparison needs
// exact rational arithmetic. See readme.md.
package main

import "fmt"

// A threshold as the user types it, and as Go stores it.
type threshold struct {
	name     string
	stored   float32 // what WithMinimumThreshold actually receives
	num, den int     // the decimal the user typed, as an exact rational
}

// production is the comparison Ooze performs today.
func production(detected, total int, stored float32) (fails bool) {
	return float32(detected)/float32(total) < stored
}

// exactVersusTyped compares against the decimal the user wrote. This is the reading that
// matches intent.
func exactVersusTyped(detected, total, num, den int) (fails bool) {
	return detected*den < num*total
}

// exactVersusStored compares against the float32 actually held. This reading rejects a
// score that exactly equals the typed decimal, and is therefore the wrong target.
func exactVersusStored(detected, total int, stored float32) (fails bool) {
	return float64(detected)/float64(total) < float64(stored)
}

func main() {
	thresholds := []threshold{
		{"0.1", 0.1, 1, 10}, {"0.2", 0.2, 1, 5}, {"0.5", 0.5, 1, 2},
		{"0.6", 0.6, 3, 5}, {"0.7", 0.7, 7, 10}, {"0.75", 0.75, 3, 4},
		{"0.8", 0.8, 4, 5}, {"0.85", 0.85, 17, 20}, {"0.9", 0.9, 9, 10},
		{"0.95", 0.95, 19, 20}, {"0.99", 0.99, 99, 100}, {"0.999", 0.999, 999, 1000},
		{"0.9995", 0.9995, 9995, 10000}, {"0.9999", 0.9999, 9999, 10000},
		{"1.0", 1.0, 1, 1},
	}

	const maxTotal = 2_000_000

	fmt.Printf("%-8s  %-14s  %s\n", "", "vs typed", "vs stored float32")
	for _, t := range thresholds {
		fmt.Printf("%-8s  %-14s  %s\n", t.name,
			firstDivergence(t, maxTotal, func(d, n int) bool {
				return exactVersusTyped(d, n, t.num, t.den)
			}),
			firstDivergence(t, maxTotal, func(d, n int) bool {
				return exactVersusStored(d, n, t.stored)
			}))
	}

	fmt.Println()
	fmt.Println("The decisive case: the score equals the typed decimal exactly.")
	for _, t := range []threshold{{"0.1", 0.1, 1, 10}, {"0.2", 0.2, 1, 5}} {
		fmt.Printf("  threshold %s, detected=%d total=%d: production fails=%v, "+
			"vs typed fails=%v, vs stored fails=%v\n",
			t.name, t.num, t.den,
			production(t.num, t.den, t.stored),
			exactVersusTyped(t.num, t.den, t.num, t.den),
			exactVersusStored(t.num, t.den, t.stored))
	}
}

// firstDivergence walks the true decision boundary for every total.
func firstDivergence(t threshold, maxTotal int, exact func(detected, total int) bool) string {
	for total := 1; total <= maxTotal; total++ {
		boundary := (t.num*total + t.den - 1) / t.den
		for _, detected := range []int{boundary - 1, boundary, boundary + 1} {
			if detected < 0 || detected > total {
				continue
			}
			if production(detected, total, t.stored) != exact(detected, total) {
				return fmt.Sprintf("%d", total)
			}
		}
	}

	return "none"
}
