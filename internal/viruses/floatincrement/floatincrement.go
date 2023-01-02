package floatincrement

import (
	"go/ast"
	"go/token"
	"strconv"

	"github.com/gtramontina/ooze/internal/viruses"
)

type FloatIncrement struct{}

func New() *FloatIncrement {
	return &FloatIncrement{}
}

func (i *FloatIncrement) Incubate(node ast.Node) []*viruses.Infection {
	literal, ok := node.(*ast.BasicLit)
	if !ok || literal.Kind != token.FLOAT {
		return nil
	}

	originalValue := literal.Value

	originalFloat, err := strconv.ParseFloat(originalValue, 64)
	if err != nil {
		return nil
	}

	originalFloat++
	mutatedValue := strconv.FormatFloat(originalFloat, 'f', -1, 64)

	return []*viruses.Infection{
		viruses.NewInfection(
			"Float Increment",
			func() { literal.Value = mutatedValue },
			func() { literal.Value = originalValue },
		),
	}
}
