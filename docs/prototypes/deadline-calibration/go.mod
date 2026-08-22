// A nested module, so the calibration harness stays out of the repository's own
// `go test ./...` and cannot affect the mutation catalogue it is used to measure.
module deadlinecalibration

go 1.26
