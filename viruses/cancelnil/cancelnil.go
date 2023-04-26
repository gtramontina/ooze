package cancelnil

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/gtramontina/ooze/viruses"
)

type CancelNil struct{}

var _ viruses.Virus = (*CancelNil)(nil)

// New returns a new CancelNil virus. It changes calls to context.CancelCauseFunc to pass nil.
func New() *CancelNil {
	return &CancelNil{}
}

func (v *CancelNil) Incubate(node ast.Node, typeInfo *types.Info) []*viruses.Infection {
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return nil
	}

	funType := typeInfo.TypeOf(call.Fun)
	if funType == nil {
		return nil
	}

	if funType.String() != "context.CancelCauseFunc" {
		return nil
	}

	origArg := call.Args[0]

	if ident, ok := origArg.(*ast.Ident); ok && ident.Name == "nil" {
		return nil
	}

	return []*viruses.Infection{
		viruses.NewInfection(
			"Call cancel(nil)",
			func() { call.Args[0] = &ast.Ident{Name: "nil", NamePos: token.NoPos, Obj: nil} },
			func() { call.Args[0] = origArg },
		),
	}
}
