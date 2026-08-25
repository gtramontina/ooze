package color

import "github.com/fatih/color"

type Palette struct{ enabled bool }

func NewPalette(enabled bool) Palette { return Palette{enabled: enabled} }

func EnabledByDefault() bool { return !color.NoColor }

func (p Palette) format(attributes ...color.Attribute) func(string, ...any) string {
	style := color.New(attributes...)
	if p.enabled {
		style.EnableColor()
	} else {
		style.DisableColor()
	}

	return style.SprintfFunc()
}

func (p Palette) BoldRed(format string, args ...any) string {
	return p.format(color.Bold, color.FgRed)(format, args...)
}
func (p Palette) Green(format string, args ...any) string {
	return p.format(color.FgGreen)(format, args...)
}
func (p Palette) Blue(format string, args ...any) string {
	return p.format(color.FgBlue)(format, args...)
}
func (p Palette) Yellow(format string, args ...any) string {
	return p.format(color.FgYellow)(format, args...)
}

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
