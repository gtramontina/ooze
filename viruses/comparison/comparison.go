package comparison

import (
	"go/ast"
	"go/token"

	"github.com/gtramontina/ooze/viruses"
)

type Comparison struct {
	mutations map[token.Token]token.Token
}

// New returns a new Comparison virus.
//
// It replaces `<` with `<=`, `>` with `>=` and vice versa.
func New() *Comparison {
	return &Comparison{
		mutations: map[token.Token]token.Token{
			token.LSS: token.LEQ,
			token.LEQ: token.LSS,
			token.GTR: token.GEQ,
			token.GEQ: token.GTR,
		},
	}
}

func (v *Comparison) Incubate(node ast.Node) []*viruses.Infection {
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
			"Comparison",
			func() { expression.Op = mutatedOperation },
			func() { expression.Op = originalOperation },
		),
	}
}
