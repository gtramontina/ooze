package integerdecrement

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"

	"github.com/gtramontina/ooze/viruses"
)

type IntegerDecrement struct{}

// New returns a new IntegerDecrement virus.
//
// It decrements integers by `1`.
func New() *IntegerDecrement {
	return &IntegerDecrement{}
}

func (v *IntegerDecrement) Incubate(node ast.Node, _ *types.Info) []*viruses.Infection {
	literal, ok := node.(*ast.BasicLit)
	if !ok || literal.Kind != token.INT {
		return nil
	}

	originalValue := literal.Value

	originalInteger, err := strconv.Atoi(originalValue)
	if err != nil {
		return nil
	}

	originalInteger--
	mutatedValue := strconv.Itoa(originalInteger)

	return []*viruses.Infection{
		viruses.NewInfection(
			"Integer Decrement",
			func() { literal.Value = mutatedValue },
			func() { literal.Value = originalValue },
		),
	}
}
