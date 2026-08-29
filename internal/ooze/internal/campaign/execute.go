package campaign

import (
	"time"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
	"github.com/gtramontina/ooze/viruses"
)

type TemporaryDirectory interface{ New() string }

type Conformance interface {
	Enter() func()
	Reserve() uint64
	Publish(uint64, Event, Projection, []Effect)
	BeginEffect(Effect) func()
}

type Configuration struct {
	Identity        string
	Lineage         processruntime.Lineage
	Repository      Repository
	TemporaryDir    TemporaryDirectory
	Command         []string
	Environment     []string
	Profile         processruntime.Profile
	Peers           int
	MutationTimeout time.Duration
	Viruses         []viruses.Virus
	Observe         func(Event)
}

type Executor struct {
	runtime     *processruntime.Runtime
	attempts    managedAttemptSystem
	conformance Conformance
}

func NewExecutor(runtime *processruntime.Runtime) (*Executor, error) {
	attempts, err := newNativeManagedAttemptSystem(runtime)
	if err != nil {
		return nil, err
	}
	return &Executor{runtime: runtime, attempts: attempts}, nil
}

func NewExecutorWithSystem(runtime *processruntime.Runtime, system supervision.System) *Executor {
	if runtime == nil {
		panic("campaign process runtime is required")
	}
	return &Executor{runtime: runtime, attempts: newManagedAttemptSystem(system)}
}

func NewConformingExecutorWithSystem(
	runtime *processruntime.Runtime,
	system supervision.System,
	conformance Conformance,
) *Executor {
	if conformance == nil {
		panic("campaign conformance recorder is required")
	}
	executor := NewExecutorWithSystem(runtime, system)
	executor.conformance = conformance
	return executor
}

func (executor *Executor) Emergency(epoch uint64) []ResidualCustody {
	settled := executor.attempts.emergency(fatalEpochID(epoch))
	return presentResiduals(campaignResiduals(settled.settlement.Residual()))
}

type Result struct {
	Outcome                 ManagedOutcome
	Mutations               []MutationResult
	Total                   int
	Baseline                *AttemptEvidence
	Cause                   AbortCause
	Residual                []ResidualCustody
	FatalAttempts           []FatalAttemptEvidence
	ArtifactResidue         []string
	SingleAdmissionFallback bool
}

type MutationResult struct {
	File         *gomutatedfile.GoMutatedFile
	Outcome      ManagedMutationOutcome
	Primary      AttemptEvidence
	Confirmation *AttemptEvidence
}

type AttemptEvidence struct {
	Kind                    AttemptKind
	Passed                  bool
	Deadline                time.Duration
	LaunchDuration          time.Duration
	CommandDuration         time.Duration
	BoundFired              supervision.BoundFired
	Output                  supervision.OutputSnapshot
	Failures                supervision.FailureDiagnostics
	Count                   supervision.ObservedCount
	ConfirmationProvisional bool
}

type AttemptKind uint8

const (
	AttemptSettled AttemptKind = iota + 1
	AttemptDeadline
	AttemptFuse
	AttemptStopped
	AttemptInfrastructure
	AttemptDrainUnconfirmed
)

type ResidualCustody struct {
	Attempt     string
	Generation  processruntime.Generation
	Prospective bool
	Transferred bool
}

type FatalAttemptEvidence struct {
	Attempt  string
	Evidence AttemptEvidence
}

func (executor *Executor) Execute(configuration Configuration) Result {
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: executor.runtime, repository: configuration.Repository,
		temporaryDirectory: configuration.TemporaryDir, attempts: executor.attempts,
		observe: configuration.Observe, conformance: executor.conformance,
	})
	result := runner.run(managedCampaignRequest{
		identity: campaignIdentity(configuration.Identity), lineage: configuration.Lineage,
		command: configuration.Command, env: configuration.Environment,
		profile: configuration.Profile, peers: configuration.Peers,
		mutationTimeout: configuration.MutationTimeout, viruses: configuration.Viruses,
	})
	return presentResult(result)
}

func presentResult(result managedCampaignResult) Result {
	switch outcome := result.outcome.(type) {
	case noMutantsOutcome:
		return Result{Outcome: ManagedNoMutants}
	case completedOutcome:
		return Result{
			Outcome: ManagedCompleted, Mutations: presentMutations(result, outcome.mutants),
			SingleAdmissionFallback: outcome.singleAdmissionFallback,
		}
	case abortedOutcome:
		presented := Result{
			Outcome: ManagedAborted, Cause: outcome.cause,
			Mutations: presentMutations(result, outcome.mutants), Total: outcome.total,
			ArtifactResidue:         append([]string(nil), outcome.artifactResidue...),
			SingleAdmissionFallback: outcome.singleAdmissionFallback,
		}
		if outcome.baseline.kind != 0 {
			baseline := presentAttempt(outcome.baseline)
			presented.Baseline = &baseline
		}
		return presented
	case nil:
		if fatal, ok := result.failure.(cleanupUnconfirmedFault); ok {
			residual := append([]campaignResidualCustody{fatal.residual.head}, fatal.residual.tail...)
			attempts := make([]FatalAttemptEvidence, len(fatal.attempts))
			for index, attempt := range fatal.attempts {
				attempts[index] = FatalAttemptEvidence{Attempt: string(attempt.attempt), Evidence: presentAttempt(attempt.evidence)}
			}
			return Result{Outcome: ManagedCleanupUnconfirmed, Residual: presentResiduals(residual), FatalAttempts: attempts}
		}
	}
	panic("managed campaign produced no terminal result")
}

func presentMutations(result managedCampaignResult, mutants []mutantResult) []MutationResult {
	presented := make([]MutationResult, len(mutants))
	for index, mutant := range mutants {
		presented[index] = MutationResult{
			File: result.mutations[mutant.mutant], Primary: presentAttempt(mutant.primary),
		}
		if mutant.kind != 0 {
			presented[index].Outcome = presentManagedMutation(mutant.kind)
		}
		if mutant.confirmation.kind != 0 {
			confirmation := presentAttempt(mutant.confirmation)
			presented[index].Confirmation = &confirmation
		}
	}
	return presented
}

func presentAttempt(evidence campaignAttemptEvidence) AttemptEvidence {
	return AttemptEvidence{
		Kind: AttemptKind(evidence.kind), Passed: evidence.passed, Deadline: evidence.deadline,
		LaunchDuration: evidence.launchDuration, CommandDuration: evidence.commandDuration,
		BoundFired: evidence.boundFired, Output: evidence.output, Failures: evidence.failures,
		Count: evidence.count, ConfirmationProvisional: evidence.confirmationProvisional,
	}
}

func presentResiduals(residual []campaignResidualCustody) []ResidualCustody {
	presented := make([]ResidualCustody, len(residual))
	for index, custody := range residual {
		presented[index] = ResidualCustody{
			Attempt: string(custody.attempt), Generation: custody.generation,
			Prospective: custody.stage == admissionProspective, Transferred: custody.transferred,
		}
	}
	return presented
}
