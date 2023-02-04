package conditionalinvert

import (
	"go/ast"
	"go/token"

	"github.com/gtramontina/ooze/viruses"
)

type ConditionalInvert struct {
	mutations map[token.Token]token.Token
}

func New() *ConditionalInvert {
	return &ConditionalInvert{
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

func (v *ConditionalInvert) Incubate(node ast.Node) []*viruses.Infection {
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
			"Conditional Invert",
			func() { expression.Op = mutatedOperation },
			func() { expression.Op = originalOperation },
		),
	}
}
