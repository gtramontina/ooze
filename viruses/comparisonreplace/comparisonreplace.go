package comparisonreplace

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/gtramontina/ooze/viruses"
)

type ComparisonReplace struct {
	mutations map[token.Token]*ast.Ident
}

// New returns a new ComparisonReplace virus.
//
// It replaces the left and right sides of an `&&` comparison with `true` and
// the left and right sides of an `||` with false. E.g. `1 == 1 && 2 == 2` gets
// two mutations: `true && 2 == 2` and `1 == 1 && true`.
func New() *ComparisonReplace {
	return &ComparisonReplace{
		mutations: map[token.Token]*ast.Ident{
			token.LAND: ast.NewIdent("true"),
			token.LOR:  ast.NewIdent("false"),
		},
	}
}

func (v *ComparisonReplace) Incubate(node ast.Node, _ *types.Info) []*viruses.Infection {
	expression, matches := node.(*ast.BinaryExpr)
	if !matches {
		return nil
	}

	mutatedBoolean, matches := v.mutations[expression.Op]
	if !matches {
		return nil
	}

	originalX := expression.X
	originalY := expression.Y

	infections := []*viruses.Infection{}

	if fmt.Sprint(originalX) != fmt.Sprint(mutatedBoolean) {
		infections = append(infections, viruses.NewInfection(
			"Comparison Replace",
			func() { expression.X = mutatedBoolean },
			func() { expression.X = originalX },
		))
	}

	if fmt.Sprint(originalY) != fmt.Sprint(mutatedBoolean) {
		infections = append(infections, viruses.NewInfection(
			"Comparison Replace",
			func() { expression.Y = mutatedBoolean },
			func() { expression.Y = originalY },
		))
	}

	return infections
}
