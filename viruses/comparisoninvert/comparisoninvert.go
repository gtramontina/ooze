package comparisoninvert

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/gtramontina/ooze/viruses"
)

type ComparisonInvert struct {
	mutations map[token.Token]token.Token
}

// New returns a new ComparisonInvert virus.
//
// It replaces `>` with `<=`, `<` with `>=`, `==` with `!=` and vice versa.
func New() *ComparisonInvert {
	return &ComparisonInvert{
		mutations: map[token.Token]token.Token{
			token.GTR: token.LEQ,
			token.LSS: token.GEQ,
			token.GEQ: token.LSS,
			token.LEQ: token.GTR,
			token.EQL: token.NEQ,
			token.NEQ: token.EQL,
		},
	}
}

func (v *ComparisonInvert) Incubate(node ast.Node, _ *types.Info) []*viruses.Infection {
	expression, matches := node.(*ast.BinaryExpr)
	if !matches {
		return nil
	}

	originalOperation := expression.Op

	mutatedOperation, matches := v.mutations[expression.Op]
	if !matches {
		return nil
	}

	return []*viruses.Infection{
		viruses.NewInfection(
			"Comparison Invert",
			func() { expression.Op = mutatedOperation },
			func() { expression.Op = originalOperation },
		),
	}
}
