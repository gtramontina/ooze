package comparison

import (
	"go/ast"
	"go/token"

	"github.com/gtramontina/ooze/viruses"
)

type Comparison struct {
	mutations map[token.Token]token.Token
}

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

func (a *Comparison) Incubate(node ast.Node) []*viruses.Infection {
	expression, matches := node.(*ast.BinaryExpr)
	if !matches {
		return nil
	}

	originalOperation := expression.Op

	mutatedOperation, matches := a.mutations[expression.Op]
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
