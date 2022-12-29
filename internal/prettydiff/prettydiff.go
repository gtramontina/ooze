package prettydiff

import (
	"strings"

	"github.com/fatih/color"
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
			line = color.New(color.Bold, color.FgRed).Sprint(line)
		case strings.HasPrefix(trimmed, "+"):
			line = color.GreenString(line)
		case strings.HasPrefix(trimmed, "@"):
			line = color.BlueString(line)
		}

		pretty[lineNumber] = line
	}

	return strings.Join(pretty, "\n")
}
