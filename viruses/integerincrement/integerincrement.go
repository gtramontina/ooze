package integerincrement

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"

	"github.com/gtramontina/ooze/viruses"
)

type IntegerIncrement struct{}

// New returns a new IntegerIncrement virus.
//
// It increments integers by `1`.
func New() *IntegerIncrement {
	return &IntegerIncrement{}
}

func (v *IntegerIncrement) Incubate(node ast.Node, _ *types.Info) []*viruses.Infection {
	literal, ok := node.(*ast.BasicLit)
	if !ok || literal.Kind != token.INT {
		return nil
	}

	originalValue := literal.Value

	originalInteger, err := strconv.Atoi(originalValue)
	if err != nil {
		return nil
	}

	originalInteger++
	mutatedValue := strconv.Itoa(originalInteger)

	return []*viruses.Infection{
		viruses.NewInfection(
			"Integer Increment",
			func() { literal.Value = mutatedValue },
			func() { literal.Value = originalValue },
		),
	}
}
