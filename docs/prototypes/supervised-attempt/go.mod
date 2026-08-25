// A nested module, following the precedent set by the deadline-calibration
// harness, so this prototype stays out of the repository's own `go test ./...`
// and cannot affect the mutation catalogue.
module supervisedattempt

go 1.26

require github.com/stretchr/testify v1.12.0

require gopkg.in/yaml.v3 v3.0.1 // indirect
