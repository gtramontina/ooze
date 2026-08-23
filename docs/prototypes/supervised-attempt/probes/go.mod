// The measurement probes need golang.org/x/sys; the contract sketch beside them
// needs no dependency at all. They are separate modules so `go mod tidy` on the
// contract cannot strip a requirement that only a `go run` program uses.
module supervisedattemptprobes

go 1.26

require golang.org/x/sys v0.47.0
