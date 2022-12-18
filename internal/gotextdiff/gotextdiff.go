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

func (GoTextDiff) Diff(leftName, rightName string, leftData, rightData []byte) string {
	edits := myers.ComputeEdits(span.URIFromPath(leftName), string(leftData), string(rightData))

	return fmt.Sprint(gotextdiff.ToUnified(leftName, rightName, string(leftData), edits))
}
