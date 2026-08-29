package supervision

import "github.com/gtramontina/ooze/internal/ooze/internal/processruntime"

type Identity string

type Generation = processruntime.Generation

type attemptIdentity = Identity
type attemptGeneration = Generation

type Violation struct {
	operation, reason string
}

func (violation Violation) Operation() string { return violation.operation }

func (violation Violation) Reason() string { return violation.reason }

type runtimeInvariantViolation = Violation

func invariant(operation, reason string) {
	panic(Violation{operation: operation, reason: reason})
}
