package arithmeticassignment_test

import (
	"testing"

	"github.com/gtramontina/ooze/oozetesting"
	"github.com/gtramontina/ooze/viruses/arithmeticassignment"
)

func TestArithmeticAssignment(t *testing.T) {
	oozetesting.Run(t, oozetesting.NewScenarios(
		"Arithmetic Assignment",
		arithmeticassignment.New(),
		oozetesting.Mutations{
			"no mutations": {"source.0.go", []string{}},
			"one mutation += to =": {"source.1.go", []string{
				"source.*.mut.1.go",
			}},
			"one mutation -= to =": {"source.2.go", []string{
				"source.*.mut.1.go",
			}},
			"one mutation *= to =": {"source.3.go", []string{
				"source.*.mut.1.go",
			}},
			"one mutation /= to =": {"source.4.go", []string{
				"source.*.mut.1.go",
			}},
			"one mutation %= to =": {"source.5.go", []string{
				"source.*.mut.1.go",
			}},
			"one mutation &= to =": {"source.6.go", []string{
				"source.*.mut.1.go",
			}},
			"one mutation |= to =": {"source.7.go", []string{
				"source.*.mut.1.go",
			}},
			"one mutation ^= to =": {"source.8.go", []string{
				"source.*.mut.1.go",
			}},
			"one mutation <<= to =": {"source.9.go", []string{
				"source.*.mut.1.go",
			}},
			"one mutation >>= to =": {"source.10.go", []string{
				"source.*.mut.1.go",
			}},
			"one mutation &^= to =": {"source.11.go", []string{
				"source.*.mut.1.go",
			}},
			"many mutations": {"source.12.go", []string{
				"source.12.mut.1.go",
				"source.12.mut.2.go",
				"source.12.mut.3.go",
				"source.12.mut.4.go",
				"source.12.mut.5.go",
				"source.12.mut.6.go",
				"source.12.mut.7.go",
				"source.12.mut.8.go",
				"source.12.mut.9.go",
				"source.12.mut.10.go",
				"source.12.mut.11.go",
			}},
		},
	))
}
