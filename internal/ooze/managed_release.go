package ooze

import (
	"runtime"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
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
	Observe         func(ManagedProgress)
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

// BoundFired identifies the execution bound recorded in report evidence.
type BoundFired uint8

const (
	NoBoundFired BoundFired = iota
	CommandDeadlineFired
)

// OutputSnapshot records captured output through a fixed cutoff.
type OutputSnapshot struct {
	Bytes                 string
	Cutoff                uint64
	CompleteThroughCutoff bool
	Final                 bool
}

// FailureDiagnostics retains independent attempt failures.
type FailureDiagnostics struct {
	Wait          string
	RunningCensus string
	DrainCensus   string
	Termination   string
	Output        string
	Release       string
}

// ObservedCount distinguishes an absent count from zero.
type ObservedCount struct {
	Value   int
	Present bool
}

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
	Cause                   ManagedAbortCause
	Residual                []ManagedResidualCustody
	Invariant               *ManagedInvariantEvidence
	FatalAttempts           []ManagedFatalAttemptEvidence
	ArtifactResidue         []string
	SingleAdmissionFallback bool
}

type ManagedAbortCause uint8

const (
	ManagedAbortCampaignRegistration ManagedAbortCause = iota + 1
	ManagedAbortSnapshotMaterialization
	ManagedAbortCatalogueDiscovery
	ManagedAbortWorkspaceMaterialization
	ManagedAbortAdmissionRejected
	ManagedAbortFatalEpoch
	ManagedAbortWorkspaceCleanup
	ManagedAbortSnapshotCleanup
	ManagedAbortAttemptNotReleased
	ManagedAbortProspectiveLaunch
	ManagedAbortDrainageUnconfirmed
	ManagedAbortBaselineFailed
	ManagedAbortBaselineDeadline
	ManagedAbortBaselineFuse
	ManagedAbortBaselineStopped
	ManagedAbortBaselineInfrastructure
	ManagedAbortPrimaryStopped
	ManagedAbortPrimaryInfrastructure
	ManagedAbortConfirmationInfrastructure
	ManagedAbortProcessEmergency
	ManagedAbortInfrastructure
)

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
	runtime  *processruntime.Runtime
	attempts managedAttemptSystem
	next     atomic.Uint64
}

var defaultManagedProcess struct {
	once    sync.Once
	process *managedProcess
}

func ProcessManagedRelease(configuration ManagedReleaseConfiguration) ManagedReleaseResult {
	defaultManagedProcess.once.Do(func() {
		capacity := runtime.GOMAXPROCS(0)
		runtimeShell := processruntime.New(capacity)
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
			if configuration.Observe != nil {
				configuration.Observe(terminalManagedProgress(presented.Outcome))
			}
			return
		}
		violation, ok := recovered.(runtimeInvariantViolation)
		runtimeViolation, runtimeFailed := recovered.(processruntime.Violation)
		if !ok && !runtimeFailed {
			panic(recovered)
		}
		if runtimeFailed {
			violation.operation = runtimeViolation.Operation()
			violation.reason = runtimeViolation.Reason()
		}
		closure := process.runtime.Close(violation.reason)
		residual := campaignResiduals(closure.Residual())
		if process.runtime.EmergencySettlementRequired() {
			settled := process.attempts.emergency(fatalEpochID(closure.Epoch()))
			residual = campaignResiduals(settled.settlement.Residual())
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
		if configuration.Observe != nil {
			configuration.Observe(terminalManagedProgress(presented.Outcome))
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
		observe: configuration.Observe,
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
		return ManagedReleaseResult{
			Outcome: ManagedCompleted, Mutations: presentManagedMutations(result, outcome.mutants),
			SingleAdmissionFallback: outcome.singleAdmissionFallback,
		}
	case abortedOutcome:
		mutations := presentManagedMutations(result, outcome.mutants)
		presented := ManagedReleaseResult{
			Outcome: ManagedAborted, Cause: presentManagedAbortCause(outcome.cause),
			Mutations: mutations, Total: outcome.total,
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
				Outcome:  ManagedCleanupUnconfirmed,
				Residual: presentManagedResiduals(residual), FatalAttempts: attempts,
			}
		}
	}
	panic("managed campaign produced no terminal result")
}

func presentManagedAbortCause(cause string) ManagedAbortCause {
	switch cause {
	case "campaign registration rejected":
		return ManagedAbortCampaignRegistration
	case "repository snapshot could not be materialized":
		return ManagedAbortSnapshotMaterialization
	case "mutation catalogue discovery failed":
		return ManagedAbortCatalogueDiscovery
	case "mutation workspace could not be materialized",
		"mutation workspace could not be materialized; workspace cleanup could not be confirmed":
		return ManagedAbortWorkspaceMaterialization
	case "managed admission rejected":
		return ManagedAbortAdmissionRejected
	case "process runtime entered a fatal epoch while admission waited", "fatal closure won terminal commitment":
		return ManagedAbortFatalEpoch
	case "mutation workspace cleanup could not be confirmed":
		return ManagedAbortWorkspaceCleanup
	case "repository snapshot cleanup could not be confirmed":
		return ManagedAbortSnapshotCleanup
	case "attempt was not released":
		return ManagedAbortAttemptNotReleased
	case "prospective launch unresolved":
		return ManagedAbortProspectiveLaunch
	case "execution-domain drainage unconfirmed":
		return ManagedAbortDrainageUnconfirmed
	case "baseline did not pass":
		return ManagedAbortBaselineFailed
	case "baseline command deadline fired":
		return ManagedAbortBaselineDeadline
	case "baseline process fuse fired":
		return ManagedAbortBaselineFuse
	case "baseline was stopped":
		return ManagedAbortBaselineStopped
	case "baseline infrastructure uncertainty":
		return ManagedAbortBaselineInfrastructure
	case "primary stopped":
		return ManagedAbortPrimaryStopped
	case "primary infrastructure uncertainty":
		return ManagedAbortPrimaryInfrastructure
	case "confirmation infrastructure uncertainty":
		return ManagedAbortConfirmationInfrastructure
	case "process runtime emergency":
		return ManagedAbortProcessEmergency
	default:
		return ManagedAbortInfrastructure
	}
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
		Kind: presentManagedAttemptKind(evidence.kind), Passed: evidence.passed,
		Deadline: evidence.deadline, LaunchDuration: evidence.launchDuration,
		CommandDuration: evidence.commandDuration, BoundFired: BoundFired(evidence.boundFired),
		Output: OutputSnapshot(evidence.output), Failures: FailureDiagnostics(evidence.failures),
		Count:                   ObservedCount(evidence.count),
		ConfirmationProvisional: evidence.confirmationProvisional,
	}
}

func presentManagedAttemptKind(kind campaignAttemptEvidenceKind) ManagedAttemptKind {
	switch kind {
	case campaignEvidenceSettled:
		return ManagedAttemptSettled
	case campaignEvidenceDeadline:
		return ManagedAttemptDeadline
	case campaignEvidenceFuse:
		return ManagedAttemptFuse
	case campaignEvidenceStopped:
		return ManagedAttemptStopped
	case campaignEvidenceInfrastructure:
		return ManagedAttemptInfrastructure
	case campaignEvidenceDrainUnconfirmed:
		return ManagedAttemptDrainUnconfirmed
	default:
		panic("managed attempt evidence kind is invalid")
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
