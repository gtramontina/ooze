package ooze

import (
	"runtime"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/ooze/internal/campaign"
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
	executor *campaign.Executor
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
		executor, err := campaign.NewExecutor(runtimeShell)
		if err != nil {
			panic(err)
		}
		defaultManagedProcess.process = &managedProcess{
			capacity: capacity, runtime: runtimeShell, executor: executor,
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
		violation, ok := recovered.(campaign.Violation)
		runtimeViolation, runtimeFailed := recovered.(processruntime.Violation)
		if !ok && !runtimeFailed {
			panic(recovered)
		}
		invariant := &ManagedInvariantEvidence{}
		if runtimeFailed {
			invariant.Operation = runtimeViolation.Operation()
			invariant.Reason = runtimeViolation.Reason()
		} else {
			invariant.Operation = violation.Operation()
			invariant.Reason = violation.Reason()
			invariant.Phase = violation.PhaseName()
			invariant.RejectedEvent = violation.RejectedEvent()
			invariant.StableIdentities = violation.StableIdentities()
			invariant.Obligations = violation.Obligations()
			invariant.TraceTail = violation.TraceTail()
		}
		closure := process.runtime.Close(invariant.Reason)
		residual := presentProcessRuntimeResiduals(closure.Residual())
		if process.runtime.EmergencySettlementRequired() {
			residual = process.executor.Emergency(closure.Epoch())
		}
		presented = ManagedReleaseResult{
			Outcome: ManagedInvariantViolation, Invariant: invariant,
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
	identity := "campaign-" + strconv.FormatUint(process.next.Add(1), 10)
	result := process.executor.Execute(campaign.Configuration{
		Identity: identity, Lineage: processruntime.Lineage(configuration.Lineage),
		Repository: campaignRepository{Repository: configuration.Repository}, TemporaryDir: configuration.TemporaryDir,
		Command: configuration.Command, Environment: configuration.Environment,
		Profile: configuration.Profile, Peers: process.capacity,
		MutationTimeout: configuration.MutationTimeout, Viruses: configuration.Viruses,
		Observe: func(progress campaign.ManagedProgress) {
			if configuration.Observe != nil {
				configuration.Observe(presentManagedProgress(progress))
			}
		},
	})

	return presentManagedRelease(result)
}

func presentManagedRelease(result campaign.Result) ManagedReleaseResult {
	switch result.Outcome {
	case campaign.ManagedNoMutants:
		return ManagedReleaseResult{Outcome: ManagedNoMutants}
	case campaign.ManagedCompleted:
		return ManagedReleaseResult{
			Outcome: ManagedCompleted, Mutations: presentManagedMutations(result.Mutations),
			SingleAdmissionFallback: result.SingleAdmissionFallback,
		}
	case campaign.ManagedAborted:
		presented := ManagedReleaseResult{
			Outcome: ManagedAborted, Cause: presentManagedAbortCause(result.Cause),
			Mutations: presentManagedMutations(result.Mutations), Total: result.Total,
			ArtifactResidue:         append([]string(nil), result.ArtifactResidue...),
			SingleAdmissionFallback: result.SingleAdmissionFallback,
		}
		if result.Baseline != nil {
			baseline := presentManagedAttempt(*result.Baseline)
			presented.Baseline = &baseline
		}

		return presented
	case campaign.ManagedCleanupUnconfirmed:
		attempts := make([]ManagedFatalAttemptEvidence, len(result.FatalAttempts))
		for index, attempt := range result.FatalAttempts {
			attempts[index] = ManagedFatalAttemptEvidence{
				Attempt: attempt.Attempt, Evidence: presentManagedAttempt(attempt.Evidence),
			}
		}
		return ManagedReleaseResult{
			Outcome:  ManagedCleanupUnconfirmed,
			Residual: presentManagedResiduals(result.Residual), FatalAttempts: attempts,
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

func presentManagedResiduals(residual []campaign.ResidualCustody) []ManagedResidualCustody {
	presented := make([]ManagedResidualCustody, len(residual))
	for index, custody := range residual {
		var stage ManagedResidualStage
		switch custody.Prospective {
		case true:
			stage = ManagedResidualProspective
		case false:
			stage = ManagedResidualOwned
		}
		presented[index] = ManagedResidualCustody{
			Attempt: custody.Attempt, Generation: uint64(custody.Generation),
			Stage: stage, Transferred: custody.Transferred,
		}
	}

	return presented
}

func presentProcessRuntimeResiduals(residual []processruntime.Residual) []campaign.ResidualCustody {
	presented := make([]campaign.ResidualCustody, len(residual))
	for index, custody := range residual {
		presented[index] = campaign.ResidualCustody{
			Attempt: custody.Attempt(), Generation: custody.Generation(),
			Prospective: custody.Prospective(), Transferred: custody.Transferred(),
		}
	}
	return presented
}

func presentManagedMutations(mutants []campaign.MutationResult) []ManagedMutationResult {
	presented := make([]ManagedMutationResult, len(mutants))
	for index, mutant := range mutants {
		presented[index] = ManagedMutationResult{
			File: mutant.File, Primary: presentManagedAttempt(mutant.Primary),
		}
		if mutant.Outcome != 0 {
			presented[index].Outcome = ManagedMutationOutcome(mutant.Outcome)
		}
		if mutant.Confirmation != nil {
			confirmation := presentManagedAttempt(*mutant.Confirmation)
			presented[index].Confirmation = &confirmation
		}
	}

	return presented
}

func presentManagedAttempt(evidence campaign.AttemptEvidence) ManagedAttemptEvidence {
	return ManagedAttemptEvidence{
		Kind: ManagedAttemptKind(evidence.Kind), Passed: evidence.Passed,
		Deadline: evidence.Deadline, LaunchDuration: evidence.LaunchDuration,
		CommandDuration: evidence.CommandDuration, BoundFired: BoundFired(evidence.BoundFired),
		Output: OutputSnapshot(evidence.Output), Failures: FailureDiagnostics(evidence.Failures),
		Count: ObservedCount(evidence.Count), ConfirmationProvisional: evidence.ConfirmationProvisional,
	}
}
