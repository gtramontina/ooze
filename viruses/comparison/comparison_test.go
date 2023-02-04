package comparison_test

import (
	"testing"

	"github.com/gtramontina/ooze/oozetesting"
	"github.com/gtramontina/ooze/viruses/comparison"
)

func TestComparison(t *testing.T) {
	oozetesting.Run(t, oozetesting.NewScenarios(
		"Comparison",
		comparison.New(),
		oozetesting.Mutations{
			"no mutations": {"source.0.go", []string{}},
			"one mutation < to <=": {"source.1.go", []string{
				"source.2.go",
			}},
			"one mutation <= to <": {"source.2.go", []string{
				"source.1.go",
			}},
			"one mutation > to >=": {"source.3.go", []string{
				"source.4.go",
			}},
			"one mutation >= to >": {"source.4.go", []string{
				"source.3.go",
			}},
			"many mutations": {"source.5.go", []string{
				"source.5.mut.1.go",
				"source.5.mut.2.go",
				"source.5.mut.3.go",
				"source.5.mut.4.go",
			}},
		},
	))
}
