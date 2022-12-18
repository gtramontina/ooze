package integerdecrement

import (
	"go/ast"
	"go/token"
	"strconv"

	"github.com/gtramontina/ooze/internal/viruses"
)

type IntegerDecrement struct{}

func New() *IntegerDecrement {
	return &IntegerDecrement{}
}

func (i *IntegerDecrement) Incubate(node ast.Node) []*viruses.Infection {
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
