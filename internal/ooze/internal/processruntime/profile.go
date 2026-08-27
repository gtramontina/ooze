package processruntime

// Profile selects cooperative execution behavior for an attempt.
type Profile uint8

// Supported execution profiles.
const (
	UnspecifiedProfile Profile = iota
	AutomaticProfile
	SerialProfile
)
