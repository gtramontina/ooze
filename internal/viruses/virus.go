package viruses

import "go/ast"

type Virus interface {
	Incubate(node ast.Node) []*Infection
}
