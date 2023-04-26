package bitwise

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/gtramontina/ooze/viruses"
)

type Bitwise struct {
	mutations map[token.Token]token.Token
}

// New creates a new Bitwise virus.
//
// It replaces `&` with `|`, `|` with `&`, `^` with `&`, `&^` with `&`, `<<`
// with `>>` and `>>` with `<<`.
func New() *Bitwise {
	return &Bitwise{
		mutations: map[token.Token]token.Token{
			token.AND:     token.OR,
			token.OR:      token.AND,
			token.XOR:     token.AND,
			token.AND_NOT: token.AND,
			token.SHL:     token.SHR,
			token.SHR:     token.SHL,
		},
	}
}

func (v *Bitwise) Incubate(node ast.Node, _ *types.Info) []*viruses.Infection {
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
			"Bitwise",
			func() { expression.Op = mutatedOperation },
			func() { expression.Op = originalOperation },
		),
	}
}
