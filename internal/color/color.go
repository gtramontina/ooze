package color

import "github.com/fatih/color"

var (
	bold      = color.New(color.Bold).SprintfFunc()                //nolint:gochecknoglobals
	boldRed   = color.New(color.Bold, color.FgRed).SprintfFunc()   //nolint:gochecknoglobals
	boldGreen = color.New(color.Bold, color.FgGreen).SprintfFunc() //nolint:gochecknoglobals
)

func Bold(format string, args ...any) string      { return bold(format, args...) }
func BoldRed(format string, args ...any) string   { return boldRed(format, args...) }
func BoldGreen(format string, args ...any) string { return boldGreen(format, args...) }
func Green(format string, args ...any) string     { return color.GreenString(format, args...) }
func Blue(format string, args ...any) string      { return color.BlueString(format, args...) }
func Yellow(format string, args ...any) string    { return color.YellowString(format, args...) }
func Cyan(format string, args ...any) string      { return color.CyanString(format, args...) }

func Force() func() {
	original := color.NoColor
	color.NoColor = false

	return func() { color.NoColor = original }
}
