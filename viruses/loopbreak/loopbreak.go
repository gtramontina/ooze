package loopbreak

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/gtramontina/ooze/viruses"
)

type LoopBreak struct {
	mutations map[token.Token]token.Token
}

// New returns a new LoopBreak virus.
//
// It replaces loop `break` with `continue` and vice versa.
func New() *LoopBreak {
	return &LoopBreak{
		mutations: map[token.Token]token.Token{
			token.CONTINUE: token.BREAK,
			token.BREAK:    token.CONTINUE,
		},
	}
}

func (v *LoopBreak) Incubate(node ast.Node, _ *types.Info) []*viruses.Infection {
	statement, matches := node.(*ast.BranchStmt)
	if !matches {
		return nil
	}

	originalToken := statement.Tok

	mutatedToken, ok := v.mutations[statement.Tok]
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
