package ooze

import (
	"runtime"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/viruses"
)

type ManagedTemporaryDirectory interface{ New() string }

type ManagedReleaseConfiguration struct {
	Lineage         uint64
	Repository      Repository
	TemporaryDir    ManagedTemporaryDirectory
	Command         []string
	Environment     []string
	Profile         Profile
	MutationTimeout time.Duration
	Viruses         []viruses.Virus
}

type ManagedOutcome uint8

const (
	ManagedNoMutants ManagedOutcome = iota + 1
	ManagedCompleted
	ManagedAborted
	ManagedCleanupUnconfirmed
	ManagedInvariantViolation
)

type ManagedMutationOutcome uint8

const (
	ManagedSurvived ManagedMutationOutcome = iota + 1
	ManagedKilled
	ManagedTimedOut
	ManagedRunaway
)

type ManagedMutationResult struct {
	File         *gomutatedfile.GoMutatedFile
	Outcome      ManagedMutationOutcome
	Primary      ManagedAttemptEvidence
	Confirmation *ManagedAttemptEvidence
}

type ManagedAttemptKind uint8

const (
	ManagedAttemptSettled ManagedAttemptKind = iota + 1
	ManagedAttemptDeadline
	ManagedAttemptFuse
	ManagedAttemptStopped
	ManagedAttemptInfrastructure
	ManagedAttemptDrainUnconfirmed
)

// ManagedAttemptEvidence is the immutable reporting projection of one owned
// attempt terminal. It contains no native identity or runtime custody object.
type ManagedAttemptEvidence struct {
	Kind                    ManagedAttemptKind
	Passed                  bool
	Deadline                time.Duration
	LaunchDuration          time.Duration
	CommandDuration         time.Duration
	BoundFired              BoundFired
	Output                  OutputSnapshot
	Failures                FailureDiagnostics
	Count                   ObservedCount
	ConfirmationProvisional bool
}

type ManagedResidualStage uint8

const (
	ManagedResidualProspective ManagedResidualStage = iota + 1
	ManagedResidualOwned
)

type ManagedResidualCustody struct {
	Attempt     string
	Generation  uint64
	Stage       ManagedResidualStage
	Transferred bool
}

type ManagedReleaseResult struct {
	Outcome                 ManagedOutcome
	Mutations               []ManagedMutationResult
	Total                   int
	Baseline                *ManagedAttemptEvidence
	Cause                   string
	Residual                []ManagedResidualCustody
	Invariant               *ManagedInvariantEvidence
	FatalAttempts           []ManagedFatalAttemptEvidence
	ArtifactResidue         []string
	SingleAdmissionFallback bool
}

type ManagedFatalAttemptEvidence struct {
	Attempt  string
	Evidence ManagedAttemptEvidence
}

type ManagedInvariantEvidence struct {
	Operation        string
	Reason           string
	Phase            string
	RejectedEvent    string
	StableIdentities []string
	Obligations      []string
	TraceTail        []string
}

type managedProcess struct {
	capacity int
	runtime  *processRuntimeShell
	attempts managedAttemptSystem
	next     atomic.Uint64
}

var defaultManagedProcess struct { //nolint:gochecknoglobals
	once    sync.Once
	process *managedProcess
}

func ProcessManagedRelease(configuration ManagedReleaseConfiguration) ManagedReleaseResult {
	defaultManagedProcess.once.Do(func() {
		capacity := runtime.GOMAXPROCS(0)
		runtimeShell := newProcessRuntimeShell(capacity)
		attempts, err := newNativeManagedAttemptSystem(runtimeShell)
		if err != nil {
			panic(err)
		}
		defaultManagedProcess.process = &managedProcess{
			capacity: capacity, runtime: runtimeShell, attempts: attempts,
		}
	})

	return defaultManagedProcess.process.release(configuration)
}

func (process *managedProcess) release(configuration ManagedReleaseConfiguration) (presented ManagedReleaseResult) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		violation, ok := recovered.(runtimeInvariantViolation)
		if !ok {
			panic(recovered)
		}
		closure := process.runtime.closeRuntime(runtimeFatalCause(violation.reason))
		residual := campaignResiduals(closure.residual)
		if process.runtime.emergencySettlementRequired() {
			settled := process.attempts.emergency(closure.epoch)
			residual = campaignResiduals(settled.settlement.residual)
		}
		presented = ManagedReleaseResult{
			Outcome: ManagedInvariantViolation,
			Invariant: &ManagedInvariantEvidence{
				Operation: violation.operation, Reason: violation.reason,
				Phase: managedCampaignPhase(violation.phase), RejectedEvent: violation.rejectedEvent,
				StableIdentities: append([]string(nil), violation.stableIdentities...),
				Obligations:      append([]string(nil), violation.obligationSnapshot...),
				TraceTail:        append([]string(nil), violation.traceTail...),
			},
			Residual: presentManagedResiduals(residual),
		}
	}()

	if configuration.Lineage == 0 || configuration.Repository == nil || configuration.TemporaryDir == nil {
		panic("managed release configuration is incomplete")
	}
	configuration.Command = slices.Clone(configuration.Command)
	configuration.Environment = slices.Clone(configuration.Environment)
	configuration.Viruses = slices.Clone(configuration.Viruses)
	identity := campaignIdentity("campaign-" + strconv.FormatUint(process.next.Add(1), 10))
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: process.runtime, repository: configuration.Repository,
		temporaryDirectory: configuration.TemporaryDir, attempts: process.attempts,
	})
	result := runner.run(managedCampaignRequest{
		identity: identity, lineage: campaignLineage(configuration.Lineage),
		command: configuration.Command, env: configuration.Environment,
		profile: configuration.Profile, peers: process.capacity,
		mutationTimeout: configuration.MutationTimeout, viruses: configuration.Viruses,
	})

	return presentManagedRelease(result)
}

func managedCampaignPhase(phase uint8) string {
	switch campaignPhase(phase) {
	case campaignPreparing:
		return "Preparing"
	case campaignBaselining:
		return "Baselining"
	case campaignRunning:
		return "Running"
	case campaignDraining:
		return "Draining"
	case campaignConfirming:
		return "Confirming"
	default:
		return ""
	}
}

func presentManagedRelease(result managedCampaignResult) ManagedReleaseResult {
	switch outcome := result.outcome.(type) {
	case noMutantsOutcome:
		return ManagedReleaseResult{Outcome: ManagedNoMutants}
	case completedOutcome:
		mutations := make([]ManagedMutationResult, len(outcome.mutants))
		for index, mutant := range outcome.mutants {
			mutations[index] = ManagedMutationResult{
				File: result.mutations[mutant.mutant], Outcome: presentManagedMutation(mutant.kind),
				Primary: presentManagedAttempt(mutant.primary),
			}
			if mutant.confirmation.kind != 0 {
				confirmation := presentManagedAttempt(mutant.confirmation)
				mutations[index].Confirmation = &confirmation
			}
		}

		return ManagedReleaseResult{
			Outcome: ManagedCompleted, Mutations: mutations,
			SingleAdmissionFallback: outcome.singleAdmissionFallback,
		}
	case abortedOutcome:
		mutations := presentManagedMutations(result, outcome.mutants)
		presented := ManagedReleaseResult{
			Outcome: ManagedAborted, Cause: outcome.cause, Mutations: mutations, Total: outcome.total,
			ArtifactResidue:         append([]string(nil), outcome.artifactResidue...),
			SingleAdmissionFallback: outcome.singleAdmissionFallback,
		}
		if outcome.baseline.kind != 0 {
			baseline := presentManagedAttempt(outcome.baseline)
			presented.Baseline = &baseline
		}

		return presented
	case nil:
		if fatal, ok := result.failure.(cleanupUnconfirmedFault); ok {
			residual := append([]campaignResidualCustody{fatal.residual.head}, fatal.residual.tail...)
			attempts := make([]ManagedFatalAttemptEvidence, len(fatal.attempts))
			for index, attempt := range fatal.attempts {
				attempts[index] = ManagedFatalAttemptEvidence{
					Attempt: string(attempt.attempt), Evidence: presentManagedAttempt(attempt.evidence),
				}
			}
			return ManagedReleaseResult{
				Outcome: ManagedCleanupUnconfirmed, Cause: "cleanup unconfirmed",
				Residual: presentManagedResiduals(residual), FatalAttempts: attempts,
			}
		}
	}
	panic("managed campaign produced no terminal result")
}

func presentManagedResiduals(residual []campaignResidualCustody) []ManagedResidualCustody {
	presented := make([]ManagedResidualCustody, len(residual))
	for index, custody := range residual {
		var stage ManagedResidualStage
		switch custody.stage {
		case admissionProspective:
			stage = ManagedResidualProspective
		case admissionOwned:
			stage = ManagedResidualOwned
		default:
			panic("managed residual stage is invalid")
		}
		presented[index] = ManagedResidualCustody{
			Attempt: string(custody.attempt), Generation: uint64(custody.generation),
			Stage: stage, Transferred: custody.transferred,
		}
	}

	return presented
}

func presentManagedMutations(
	result managedCampaignResult,
	mutants []mutantResult,
) []ManagedMutationResult {
	presented := make([]ManagedMutationResult, len(mutants))
	for index, mutant := range mutants {
		presented[index] = ManagedMutationResult{
			File: result.mutations[mutant.mutant], Primary: presentManagedAttempt(mutant.primary),
		}
		if mutant.kind != 0 {
			presented[index].Outcome = presentManagedMutation(mutant.kind)
		}
		if mutant.confirmation.kind != 0 {
			confirmation := presentManagedAttempt(mutant.confirmation)
			presented[index].Confirmation = &confirmation
		}
	}

	return presented
}

func presentManagedAttempt(evidence campaignAttemptEvidence) ManagedAttemptEvidence {
	return ManagedAttemptEvidence{
		Kind: evidence.kind, Passed: evidence.passed,
		Deadline: evidence.deadline, LaunchDuration: evidence.launchDuration,
		CommandDuration: evidence.commandDuration, BoundFired: evidence.boundFired,
		Output: evidence.output, Failures: evidence.failures, Count: evidence.count,
		ConfirmationProvisional: evidence.confirmationProvisional,
	}
}

func presentManagedMutation(kind mutantResultKind) ManagedMutationOutcome {
	switch kind {
	case mutantSurvived:
		return ManagedSurvived
	case mutantKilled:
		return ManagedKilled
	case mutantTimedOut:
		return ManagedTimedOut
	case mutantRunaway:
		return ManagedRunaway
	default:
		panic("managed mutant outcome is invalid")
	}
}
