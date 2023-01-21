package prettydiff

import (
	"strings"

	"github.com/gtramontina/ooze/internal/color"
	"github.com/gtramontina/ooze/internal/gomutatedfile"
)

type PrettyDiff struct {
	delegate gomutatedfile.Differ
}

func New(delegate gomutatedfile.Differ) *PrettyDiff {
	return &PrettyDiff{
		delegate: delegate,
	}
}

func (d *PrettyDiff) Diff(a, b string, aData, bData []byte) string {
	pretty := strings.Split(d.delegate.Diff(a, b, aData, bData), "\n")

	for lineNumber, line := range pretty {
		trimmed := strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(trimmed, "-"):
			line = color.BoldRed(line)
		case strings.HasPrefix(trimmed, "+"):
			line = color.Green(line)
		case strings.HasPrefix(trimmed, "@"):
			line = color.Blue(line)
		}

		pretty[lineNumber] = line
	}

	return strings.Join(pretty, "\n")
}
