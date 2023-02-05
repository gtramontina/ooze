package comparisonreplace_test

import (
	"testing"

	"github.com/gtramontina/ooze/oozetesting"
	"github.com/gtramontina/ooze/viruses/comparisonreplace"
)

func TestComparisonInvert(t *testing.T) {
	oozetesting.Run(t, oozetesting.NewScenarios(
		"Comparison Replace",
		comparisonreplace.New(),
		oozetesting.Mutations{
			"no mutations": {"source.0.go", []string{}},
			"mutate && left and right to true": {"source.1.go", []string{
				"source.1.mut.1.go",
				"source.1.mut.2.go",
			}},
			"mutate || left and right to false": {"source.2.go", []string{
				"source.2.mut.1.go",
				"source.2.mut.2.go",
			}},
			"many mutations": {"source.3.go", []string{
				"source.3.mut.1.go",
				"source.3.mut.2.go",
				"source.3.mut.3.go",
				"source.3.mut.4.go",
				"source.3.mut.5.go",
				"source.3.mut.6.go",
				"source.3.mut.7.go",
				"source.3.mut.8.go",
				"source.3.mut.9.go",
				"source.3.mut.10.go",
				"source.3.mut.11.go",
				"source.3.mut.12.go",
				"source.3.mut.13.go",
				"source.3.mut.14.go",
				"source.3.mut.15.go",
				"source.3.mut.16.go",
			}},
		},
	))
}
