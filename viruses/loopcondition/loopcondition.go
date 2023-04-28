package loopcondition

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/gtramontina/ooze/viruses"
)

type LoopCondition struct {
	falseExpression *ast.BinaryExpr
}

// New returns a new LoopCondition virus.
//
// It replaces loop condition with an always false value.
func New() *LoopCondition {
	return &LoopCondition{
		falseExpression: &ast.BinaryExpr{
			X:     ast.NewIdent("0"),
			OpPos: 0,
			Op:    token.NEQ,
			Y:     ast.NewIdent("0"),
		},
	}
}

func (v *LoopCondition) Incubate(node ast.Node, _ *types.Info) []*viruses.Infection {
	statement, matches := node.(*ast.ForStmt)
	if !matches {
		return nil
	}

	originalCondition, matches := statement.Cond.(*ast.BinaryExpr)
	if !matches {
		return nil
	}

	return []*viruses.Infection{
		viruses.NewInfection(
			"Loop Condition",
			func() { statement.Cond = v.falseExpression },
			func() { statement.Cond = originalCondition },
		),
	}
}
