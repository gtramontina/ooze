module github.com/gtramontina/ooze

go 1.20

retract (
	// This version contains retractions only.
	v0.3.1
	// This version contains an issue that prevents Ooze from running on
	// internal packages. See https://github.com/gtramontina/ooze/issues/9.
	v0.3.0
)

require (
	github.com/fatih/color v1.15.0
	github.com/hexops/gotextdiff v1.0.3
	github.com/stretchr/testify v1.8.3
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.17 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/sys v0.6.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
