package rangebreak_test

import (
	"testing"

	"github.com/gtramontina/ooze/oozetesting"
	"github.com/gtramontina/ooze/viruses/rangebreak"
)

func TestRangeBreak(t *testing.T) {
	oozetesting.Run(t, oozetesting.NewScenarios(
		"Range Break",
		rangebreak.New(),
		oozetesting.Mutations{
			"no mutations": {"source.0.go", []string{}},
			"one mutation": {"source.1.go", []string{
				"source.1.mut.1.go",
			}},
			"two mutations": {"source.2.go", []string{
				"source.2.mut.1.go",
				"source.2.mut.2.go",
			}},
			"many mutations": {"source.3.go", []string{
				"source.3.mut.1.go",
				"source.3.mut.2.go",
				"source.3.mut.3.go",
			}},
		},
	))
}
