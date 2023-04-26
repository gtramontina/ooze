package floatdecrement

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"strconv"

	"github.com/gtramontina/ooze/viruses"
)

type FloatDecrement struct{}

// New returns a new FloatDecrement virus.
//
// It decrements floating points by `1.0`.
func New() *FloatDecrement {
	return &FloatDecrement{}
}

func (v *FloatDecrement) Incubate(node ast.Node, _ *types.Info) []*viruses.Infection {
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

	originalFloat--
	mutatedValue := fmt.Sprintf("%v", originalFloat)

	return []*viruses.Infection{
		viruses.NewInfection(
			"Float Decrement",
			func() { literal.Value = mutatedValue },
			func() { literal.Value = originalValue },
		),
	}
}
