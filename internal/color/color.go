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
