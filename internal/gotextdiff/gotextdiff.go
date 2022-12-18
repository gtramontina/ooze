package gotextdiff

import (
	"fmt"

	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"
)

type GoTextDiff struct{}

func New() *GoTextDiff {
	return &GoTextDiff{}
}

func (GoTextDiff) Diff(a, b string, aData, bData []byte) string {
	edits := myers.ComputeEdits(span.URIFromPath(a), string(aData), string(bData))

	return fmt.Sprint(gotextdiff.ToUnified(a, b, string(aData), edits))
}
