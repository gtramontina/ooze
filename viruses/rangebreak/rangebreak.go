package rangebreak

import (
	"go/ast"
	"go/token"

	"github.com/gtramontina/ooze/viruses"
)

type RangeBreak struct{}

func New() *RangeBreak {
	return &RangeBreak{}
}

func (v *RangeBreak) Incubate(node ast.Node) []*viruses.Infection {
	statement, matches := node.(*ast.RangeStmt)
	if !matches {
		return nil
	}

	originalStatementBody := statement.Body
	mutatedStatementBody := &ast.BlockStmt{
		Lbrace: 0,
		List: []ast.Stmt{
			&ast.BranchStmt{
				TokPos: 0,
				Tok:    token.BREAK,
				Label:  nil,
			},
		},
		Rbrace: 0,
	}

	mutatedStatementBody.List = append(mutatedStatementBody.List, originalStatementBody.List...)

	return []*viruses.Infection{
		viruses.NewInfection(
			"Range Break",
			func() { statement.Body = mutatedStatementBody },
			func() { statement.Body = originalStatementBody },
		),
	}
}
