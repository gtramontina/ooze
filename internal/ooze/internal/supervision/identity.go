package supervision

import "github.com/gtramontina/ooze/internal/ooze/internal/processruntime"

// Identity identifies one supervised attempt.
type Identity string

// Generation identifies one supervised execution-domain obligation.
type Generation = processruntime.Generation

type attemptIdentity = Identity
type attemptGeneration = Generation

// Violation reports a supervision invariant breach.
type Violation struct {
	operation, reason                               string
	phase                                           uint8
	rejectedEvent                                   string
	stableIdentities, obligationSnapshot, traceTail []string
}

// Operation returns the supervision operation that rejected the fact.
func (violation Violation) Operation() string { return violation.operation }

// Reason returns why the supervision fact was rejected.
func (violation Violation) Reason() string { return violation.reason }

type runtimeInvariantViolation = Violation

func invariant(operation, reason string) {
	panic(Violation{operation: operation, reason: reason})
}
