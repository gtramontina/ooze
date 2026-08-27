package processruntime

import "sync"

type Profile uint8

const (
	AutomaticProfile Profile = iota + 1
	SerialProfile
)

type Lineage uint64

type Campaign struct {
	id      uint64
	lineage Lineage
}

type Generation uint64

type CampaignDecision uint8

const (
	CampaignRegistered CampaignDecision = iota + 1
	CampaignRejectedRecursive
	CampaignRejectedClosed
)

type Registration struct {
	decision CampaignDecision
	campaign Campaign
}

func (r Registration) Decision() CampaignDecision { return r.decision }

func (r Registration) Campaign() Campaign { return r.campaign }

type AdmissionClass uint8

const (
	SharedAdmission AdmissionClass = iota + 1
	ExclusiveAdmission
	SerialPrimaryAdmission
	ConfirmationAdmission
)

type Admission struct {
	Campaign Campaign
	Attempt  string
	Class    AdmissionClass
	Profile  Profile
}

type AdmissionDecision uint8

const (
	AdmissionAccepted AdmissionDecision = iota + 1
	AdmissionRejectedClosed
	AdmissionRejectedCampaign
	AdmissionRejectedDuplicate
	AdmissionRejectedCapacity
)

type Grant struct {
	campaign Campaign
	attempt  string
	class    AdmissionClass
}

type Await struct {
	decision AdmissionDecision
	delivery <-chan Grant
}

func (a Await) Decision() AdmissionDecision { return a.decision }

func (a Await) Receive() (Grant, bool) {
	grant, ok := <-a.delivery
	return grant, ok
}

type StartDecision uint8

const (
	StartAccepted StartDecision = iota + 1
	StartRejected
)

type StartCell struct {
	mutex      sync.Mutex
	generation Generation
	launched   bool
}

func NewStartCell() *StartCell { return &StartCell{} }

type PreparedStart struct {
	runtime    *Runtime
	cell       *StartCell
	decision   StartDecision
	generation Generation
}

func (s PreparedStart) Decision() StartDecision { return s.decision }

func (s PreparedStart) Generation() Generation { return s.generation }

type Observation uint8

const (
	ownedObservation Observation = iota + 1
	settledObservation
)

func Owned() Observation { return ownedObservation }

func Settled() Observation { return settledObservation }

type Receipt struct {
	settlementAcknowledged bool
	settlementPending      bool
}

func (r Receipt) SettlementAcknowledged() bool { return r.settlementAcknowledged }

func (r Receipt) SettlementPending() bool { return r.settlementPending }

func (s PreparedStart) Launch(launch func(Generation) Observation) Receipt {
	if s.decision != StartAccepted || s.runtime == nil || s.cell == nil || launch == nil {
		panic(Violation{Operation: "launch", Reason: "start or launch is invalid"})
	}
	s.cell.mutex.Lock()
	if s.cell.generation != s.generation || s.cell.launched {
		s.cell.mutex.Unlock()
		panic(Violation{Operation: "launch", Reason: "start is stale or reused"})
	}
	s.cell.launched = true
	s.cell.mutex.Unlock()

	return s.runtime.Observe(s.generation, launch(s.generation))
}

type TerminalDecision uint8

const (
	TerminalCommitted TerminalDecision = iota + 1
	TerminalRejected
)

type TerminalResult struct{ decision TerminalDecision }

func (r TerminalResult) Decision() TerminalDecision { return r.decision }

type Violation struct {
	Operation string
	Reason    string
}

func (v Violation) Error() string { return v.Operation + ": " + v.Reason }

type admissionStage uint8

const (
	admissionGranted admissionStage = iota + 1
	admissionProspective
	admissionOwned
)

type admittedAttempt struct {
	grant      Grant
	stage      admissionStage
	generation Generation
}

type Runtime struct {
	mutex      sync.Mutex
	capacity   int
	next       uint64
	closed     bool
	campaigns  []Campaign
	admissions []admittedAttempt
}

func New(capacity int) *Runtime {
	if capacity <= 0 {
		panic(Violation{Operation: "construct", Reason: "capacity must be positive"})
	}
	return &Runtime{capacity: capacity}
}

func (r *Runtime) RegisterCampaign(lineage Lineage) Registration {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.closed {
		return Registration{decision: CampaignRejectedClosed}
	}
	if lineage == 0 {
		panic(Violation{Operation: "register campaign", Reason: "lineage is zero"})
	}
	for _, campaign := range r.campaigns {
		if campaign.lineage == lineage {
			return Registration{decision: CampaignRejectedRecursive}
		}
	}
	r.next++
	campaign := Campaign{id: r.next, lineage: lineage}
	r.campaigns = append(r.campaigns, campaign)
	return Registration{decision: CampaignRegistered, campaign: campaign}
}

func (r *Runtime) RequestAdmission(request Admission) Await {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	delivery := make(chan Grant, 1)
	if r.closed {
		close(delivery)
		return Await{decision: AdmissionRejectedClosed, delivery: delivery}
	}
	if !r.registered(request.Campaign) {
		close(delivery)
		return Await{decision: AdmissionRejectedCampaign, delivery: delivery}
	}
	for _, admission := range r.admissions {
		if admission.grant.campaign == request.Campaign && admission.grant.attempt == request.Attempt {
			close(delivery)
			return Await{decision: AdmissionRejectedDuplicate, delivery: delivery}
		}
	}
	if len(r.admissions) >= r.capacity {
		close(delivery)
		return Await{decision: AdmissionRejectedCapacity, delivery: delivery}
	}
	grant := Grant{campaign: request.Campaign, attempt: request.Attempt, class: request.Class}
	r.admissions = append(r.admissions, admittedAttempt{grant: grant, stage: admissionGranted})
	delivery <- grant
	close(delivery)
	return Await{decision: AdmissionAccepted, delivery: delivery}
}

func (r *Runtime) CommitStart(grant Grant, cell *StartCell) PreparedStart {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	for index := range r.admissions {
		if r.admissions[index].grant != grant || r.admissions[index].stage != admissionGranted {
			continue
		}
		r.next++
		generation := Generation(r.next)
		cell.mutex.Lock()
		cell.generation = generation
		cell.mutex.Unlock()
		r.admissions[index].generation = generation
		r.admissions[index].stage = admissionProspective
		return PreparedStart{runtime: r, cell: cell, decision: StartAccepted, generation: generation}
	}
	return PreparedStart{decision: StartRejected}
}

func (r *Runtime) Observe(generation Generation, observation Observation) Receipt {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	for index := range r.admissions {
		if r.admissions[index].generation != generation {
			continue
		}
		switch observation {
		case ownedObservation:
			if r.admissions[index].stage != admissionProspective {
				panic(Violation{Operation: "observe attempt", Reason: "owned launch is not prospective"})
			}
			r.admissions[index].stage = admissionOwned
			return Receipt{settlementPending: true}
		case settledObservation:
			if r.admissions[index].stage != admissionOwned {
				panic(Violation{Operation: "observe attempt", Reason: "terminal is not owned"})
			}
			r.admissions = append(r.admissions[:index], r.admissions[index+1:]...)
			return Receipt{settlementAcknowledged: true}
		default:
			panic(Violation{Operation: "observe attempt", Reason: "observation is invalid"})
		}
	}
	panic(Violation{Operation: "observe attempt", Reason: "generation is not live"})
}

func (r *Runtime) CommitTerminal(campaign Campaign) TerminalResult {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if !r.registered(campaign) || len(r.admissions) != 0 {
		return TerminalResult{decision: TerminalRejected}
	}
	for index := range r.campaigns {
		if r.campaigns[index] == campaign {
			r.campaigns = append(r.campaigns[:index], r.campaigns[index+1:]...)
			return TerminalResult{decision: TerminalCommitted}
		}
	}
	return TerminalResult{decision: TerminalRejected}
}

func (r *Runtime) registered(campaign Campaign) bool {
	for _, registered := range r.campaigns {
		if registered == campaign {
			return true
		}
	}
	return false
}
