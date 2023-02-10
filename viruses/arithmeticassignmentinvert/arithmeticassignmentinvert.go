package arithmeticassignmentinvert

import (
	"go/ast"
	"go/token"

	"github.com/gtramontina/ooze/viruses"
)

type ArithmeticAssignmentInvert struct {
	mutations map[token.Token]token.Token
}

// New creates a new ArithmeticAssignmentInvert virus.
//
// It replaces `+=` with `-=`, `*=` with `/=`, `%=` with `*=` and vice versa.
func New() *ArithmeticAssignmentInvert {
	return &ArithmeticAssignmentInvert{
		mutations: map[token.Token]token.Token{
			token.ADD_ASSIGN: token.SUB_ASSIGN,
			token.SUB_ASSIGN: token.ADD_ASSIGN,
			token.MUL_ASSIGN: token.QUO_ASSIGN,
			token.QUO_ASSIGN: token.MUL_ASSIGN,
			token.REM_ASSIGN: token.MUL_ASSIGN,
		},
	}
}

func (v *ArithmeticAssignmentInvert) Incubate(node ast.Node) []*viruses.Infection {
	statement, matches := node.(*ast.AssignStmt)
	if !matches {
		return nil
	}

	originalToken := statement.Tok

	mutatedToken, matches := v.mutations[statement.Tok]
	if !matches {
		return nil
	}

	return []*viruses.Infection{
		viruses.NewInfection(
			"Arithmetic Assignment Invert",
			func() { statement.Tok = mutatedToken },
			func() { statement.Tok = originalToken },
		),
	}
}
