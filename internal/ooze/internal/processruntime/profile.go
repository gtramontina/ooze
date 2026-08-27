package processruntime

// Profile selects cooperative execution behavior for an attempt.
type Profile uint8

// Supported execution profiles.
const (
	// AutomaticProfile permits bounded concurrent execution.
	AutomaticProfile Profile = iota + 1
	// SerialProfile requires exclusive execution.
	SerialProfile
)
