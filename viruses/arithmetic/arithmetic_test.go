package arithmetic_test

import (
	"testing"

	"github.com/gtramontina/ooze/oozetesting"
	"github.com/gtramontina/ooze/viruses/arithmetic"
)

func TestArithmetic(t *testing.T) {
	oozetesting.Run(t, oozetesting.NewScenarios(
		"Arithmetic",
		arithmetic.New(),
		oozetesting.Mutations{
			"no mutations": {"source.0.go", []string{}},
			"one mutation + to -": {"source.1.go", []string{
				"source.2.go",
			}},
			"one mutation - to +": {"source.2.go", []string{
				"source.1.go",
			}},
			"one mutation * to /": {"source.3.go", []string{
				"source.4.go",
			}},
			"one mutation / to *": {"source.4.go", []string{
				"source.3.go",
			}},
			"one mutation % to *": {"source.5.go", []string{
				"source.3.go",
			}},
			"many mutations": {"source.6.go", []string{
				"source.6.mut.1.go",
				"source.6.mut.2.go",
				"source.6.mut.3.go",
				"source.6.mut.4.go",
				"source.6.mut.5.go",
			}},
		},
	))
}
