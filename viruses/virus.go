package viruses

import (
	"go/ast"
	"go/types"
)

type Virus interface {
	Incubate(node ast.Node, typeInfo *types.Info) []*Infection
}
