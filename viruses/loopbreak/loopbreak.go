package loopbreak

import (
	"go/ast"
	"go/token"

	"github.com/gtramontina/ooze/viruses"
)

type LoopBreak struct {
	mutations map[token.Token]token.Token
}

func New() *LoopBreak {
	return &LoopBreak{
		mutations: map[token.Token]token.Token{
			token.CONTINUE: token.BREAK,
			token.BREAK:    token.CONTINUE,
		},
	}
}

func (l *LoopBreak) Incubate(node ast.Node) []*viruses.Infection {
	statement, matches := node.(*ast.BranchStmt)
	if !matches {
		return nil
	}

	originalToken := statement.Tok

	mutatedToken, ok := l.mutations[statement.Tok]
	if !ok {
		return nil
	}

	return []*viruses.Infection{
		viruses.NewInfection(
			"Loop Break",
			func() { statement.Tok = mutatedToken },
			func() { statement.Tok = originalToken },
		),
	}
}
