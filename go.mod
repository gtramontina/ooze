module github.com/gtramontina/ooze

go 1.25.0

toolchain go1.26.6

retract (
	// This version contains retractions only.
	v0.3.1
	// This version contains an issue that prevents Ooze from running on
	// internal packages. See https://github.com/gtramontina/ooze/issues/9.
	v0.3.0
)

require (
	github.com/fatih/color v1.19.0
	github.com/hexops/gotextdiff v1.0.3
	github.com/stretchr/testify v1.12.1
	golang.org/x/sys v0.47.0
)

require (
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
)
