package loopbreak_test

import (
	"testing"

	"github.com/gtramontina/ooze/oozetesting"
	"github.com/gtramontina/ooze/viruses/loopbreak"
)

func TestLoopBreak_ContinueBreak(t *testing.T) {
	oozetesting.Run(t, oozetesting.NewScenarios(
		"Loop Break",
		loopbreak.New(),
		oozetesting.Mutations{
			"no mutations": {"source.continue.0.go", []string{}},
			"one mutation": {"source.continue.1.go", []string{
				"source.continue.1.mut.1.go",
			}},
			"two mutations": {"source.continue.2.go", []string{
				"source.continue.2.mut.1.go",
				"source.continue.2.mut.2.go",
			}},
			"many mutations": {"source.continue.3.go", []string{
				"source.continue.3.mut.1.go",
				"source.continue.3.mut.2.go",
				"source.continue.3.mut.3.go",
			}},
		},
	))
}

func TestLoopBreak_BreakContinue(t *testing.T) {
	oozetesting.Run(t, oozetesting.NewScenarios(
		"Loop Break",
		loopbreak.New(),
		oozetesting.Mutations{
			"no mutations": {"source.break.0.go", []string{}},
			"one mutation": {"source.break.1.go", []string{
				"source.break.1.mut.1.go",
			}},
			"two mutations": {"source.break.2.go", []string{
				"source.break.2.mut.1.go",
				"source.break.2.mut.2.go",
			}},
			"many mutations": {"source.break.3.go", []string{
				"source.break.3.mut.1.go",
				"source.break.3.mut.2.go",
				"source.break.3.mut.3.go",
			}},
		},
	))
}
