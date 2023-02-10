package floatincrement

import (
	"fmt"
	"go/ast"
	"go/token"
	"reflect"
	"strconv"

	"github.com/gtramontina/ooze/viruses"
)

type FloatIncrement struct{}

// New returns a new FloatIncrement virus.
//
// It increments floating points by `1.0`.
func New() *FloatIncrement {
	return &FloatIncrement{}
}

func (v *FloatIncrement) Incubate(node ast.Node) []*viruses.Infection {
	literal, ok := node.(*ast.BasicLit)
	if !ok || literal.Kind != token.FLOAT {
		return nil
	}

	originalValue := literal.Value

	var originalFloat float64
	bitSize := reflect.TypeOf(originalFloat).Bits()

	originalFloat, err := strconv.ParseFloat(originalValue, bitSize)
	if err != nil {
		return nil
	}

	originalFloat++
	mutatedValue := fmt.Sprintf("%v", originalFloat)

	return []*viruses.Infection{
		viruses.NewInfection(
			"Float Increment",
			func() { literal.Value = mutatedValue },
			func() { literal.Value = originalValue },
		),
	}
}
