package runtimeshapes

type campaignID uint64
type attemptID string
type generation uint64

type admissionClass uint8

const (
	shared admissionClass = iota + 1
	exclusive
	barrier
)

type entryStage uint8

const (
	waiting entryStage = iota + 1
	granted
	prospective
	owned
)

type observationKind uint8

const (
	launchOwned observationKind = iota + 1
	launchFailed
	launchResourceExhausted
	settled
	deadline
	fuse
	infrastructure
	drainUnconfirmed
)

type campaign struct {
	id       campaignID
	lineage  uint64
	gateOpen bool
}

type entry struct {
	campaign   campaignID
	attempt    attemptID
	class      admissionClass
	stage      entryStage
	generation generation
	overlapped bool
	bound      bool
}

type state struct {
	capacity       int
	single         bool
	closed         bool
	nextCampaign   campaignID
	nextGeneration generation
	campaigns      []campaign
	entries        []entry
}

type grant struct {
	campaign campaignID
	attempt  attemptID
}

type registerInput struct{ lineage uint64 }
type requestInput struct {
	campaign campaignID
	attempt  attemptID
	class    admissionClass
}
type cancelInput struct{ grant grant }
type startInput struct{ grant grant }
type observationInput struct {
	generation          generation
	kind                observationKind
	confirmationDrained bool
}
type bindInput struct {
	campaign campaignID
	attempt  attemptID
}
type terminalInput struct{ campaign campaignID }
type closeInput struct{}
type settleInput struct{ generation generation }

type reply struct {
	accepted   bool
	recursive  bool
	campaign   campaignID
	generation generation
	deliveries []grant
	closed     bool
}

type call struct {
	name  string
	input any
}

type shape interface {
	apply(state, call) (state, reply)
}
