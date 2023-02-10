package arithmeticassignment

import (
	"go/ast"
	"go/token"

	"github.com/gtramontina/ooze/viruses"
)

type ArithmeticAssignment struct {
	mutations map[token.Token]token.Token
}

// New creates a new ArithmeticAssignment virus.
//
// It replaces `+=`, `-=`, `*=`, `/=`, `%=`, `&=`, <code>&#124;=</code>, `^=`,
// `<<=`, `>>=` and `&^=` with `=`.
func New() *ArithmeticAssignment {
	return &ArithmeticAssignment{
		mutations: map[token.Token]token.Token{
			token.ADD_ASSIGN:     token.ASSIGN,
			token.SUB_ASSIGN:     token.ASSIGN,
			token.MUL_ASSIGN:     token.ASSIGN,
			token.QUO_ASSIGN:     token.ASSIGN,
			token.REM_ASSIGN:     token.ASSIGN,
			token.AND_ASSIGN:     token.ASSIGN,
			token.OR_ASSIGN:      token.ASSIGN,
			token.XOR_ASSIGN:     token.ASSIGN,
			token.SHL_ASSIGN:     token.ASSIGN,
			token.SHR_ASSIGN:     token.ASSIGN,
			token.AND_NOT_ASSIGN: token.ASSIGN,
		},
	}
}

func (v *ArithmeticAssignment) Incubate(node ast.Node) []*viruses.Infection {
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
			"Arithmetic Assignment",
			func() { statement.Tok = mutatedToken },
			func() { statement.Tok = originalToken },
		),
	}
}
