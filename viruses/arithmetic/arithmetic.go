package arithmetic

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/gtramontina/ooze/viruses"
)

type Arithmetic struct {
	mutations map[token.Token]token.Token
}

// New creates a new Arithmetic virus.
//
// It replaces `+` with `-`, `*` with `/`, `%` with `*` and vice versa.
func New() *Arithmetic {
	return &Arithmetic{
		mutations: map[token.Token]token.Token{
			token.ADD: token.SUB,
			token.SUB: token.ADD,
			token.MUL: token.QUO,
			token.QUO: token.MUL,
			token.REM: token.MUL,
		},
	}
}

func (v *Arithmetic) Incubate(node ast.Node, _ *types.Info) []*viruses.Infection {
	expression, matches := node.(*ast.BinaryExpr)
	if !matches {
		return nil
	}

	// Go overloaded the + operator, meaning is not just used for
	// arithmetic operations, so it needs to be ignored for strings.
	if isString(expression.X) || isString(expression.Y) {
		return nil
	}

	originalOperation := expression.Op

	mutatedOperation, matches := v.mutations[expression.Op]
	if !matches {
		return nil
	}

	return []*viruses.Infection{
		viruses.NewInfection(
			"Arithmetic",
			func() { expression.Op = mutatedOperation },
			func() { expression.Op = originalOperation },
		),
	}
}

func isString(expr ast.Expr) bool {
	switch expr := expr.(type) {
	case *ast.Ident:
		// We need to get the type here, however, the `types` parameter
		// is explicitly set to `nil` upon function invocation.
	case *ast.BasicLit:
		// Strings only support concatenation (+)
		if expr.Kind == token.STRING {
			return true
		}
	case *ast.CallExpr:
		if expr, isIdentifier := expr.Fun.(*ast.Ident); isIdentifier {
			// Make sure we have a builtin.
			// Technically, we could encounter something like this:
			//
			// func string(b []byte) int {
			// 	return len(b)
			// }
			if expr.Name == "string" && expr.Obj == nil {
				return true
			}
		}
	}

	return false
}
