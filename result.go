package ooze

import (
	"time"

	"github.com/gtramontina/ooze/internal/gotextdiff"
	internalooze "github.com/gtramontina/ooze/internal/ooze"
)

// Outcome identifies whether a campaign produced a score or why it did not.
type Outcome uint8

const (
	// Completed identifies a campaign that produced a mutation score.
	Completed Outcome = iota + 1
	// NoMutants identifies a campaign with an empty mutant catalogue.
	NoMutants
	// Aborted identifies a campaign stopped by attributable infrastructure evidence.
	Aborted
	// CleanupUnconfirmed identifies a campaign whose execution domains could not be proven drained.
	CleanupUnconfirmed
	// InvariantViolation identifies an invalid internal state transition.
	InvariantViolation
)

// MutationOutcome identifies the attributable outcome of one mutant.
type MutationOutcome uint8

const (
	// Survived identifies a mutant whose test command passed.
	Survived MutationOutcome = iota + 1
	// Killed identifies a mutant whose test command failed.
	Killed
	// TimedOut identifies a mutant whose command deadline fired.
	TimedOut
	// Runaway identifies a mutant whose execution domain crossed the process fuse.
	Runaway
)

// Score is the authoritative score and threshold decision for a completed campaign.
type Score struct {
	Detected int
	Total    int
	Value    float32
	Minimum  float32
	Passed   bool
}

// MutationResult contains the terminal presentation facts for one mutant.
type MutationResult struct {
	Label        string
	Diff         string
	Outcome      MutationOutcome
	Primary      AttemptResult
	Confirmation *AttemptResult
}

// AbortCause identifies why a campaign could not produce a score.
type AbortCause uint8

const (
	// RegistrationRejected identifies rejected process-runtime registration.
	RegistrationRejected AbortCause = iota + 1
	// SnapshotMaterializationFailed identifies repository snapshot creation failure.
	SnapshotMaterializationFailed
	// CatalogueDiscoveryFailed identifies mutant catalogue discovery failure.
	CatalogueDiscoveryFailed
	// WorkspaceMaterializationFailed identifies mutation workspace creation failure.
	WorkspaceMaterializationFailed
	// AdmissionRejected identifies rejected process-runtime admission.
	AdmissionRejected
	// FatalEpochInterrupted identifies a process-runtime fatal epoch.
	FatalEpochInterrupted
	// WorkspaceCleanupFailed identifies unconfirmed mutation workspace cleanup.
	WorkspaceCleanupFailed
	// SnapshotCleanupFailed identifies unconfirmed repository snapshot cleanup.
	SnapshotCleanupFailed
	// AttemptNotReleased identifies a launch proven not to have crossed its release boundary.
	AttemptNotReleased
	// ProspectiveLaunchUnresolved identifies a launch whose release could not be confirmed.
	ProspectiveLaunchUnresolved
	// DrainageUnconfirmed identifies an execution domain whose drainage could not be confirmed.
	DrainageUnconfirmed
	// BaselineFailed identifies a settled baseline whose command did not pass.
	BaselineFailed
	// BaselineTimedOut identifies a baseline command deadline.
	BaselineTimedOut
	// BaselineRunaway identifies a baseline process-fuse trip.
	BaselineRunaway
	// BaselineStopped identifies an externally stopped baseline.
	BaselineStopped
	// BaselineInfrastructureFailed identifies uncertain baseline supervision.
	BaselineInfrastructureFailed
	// PrimaryStopped identifies an externally stopped primary attempt.
	PrimaryStopped
	// PrimaryInfrastructureFailed identifies uncertain primary supervision.
	PrimaryInfrastructureFailed
	// ConfirmationInfrastructureFailed identifies uncertain confirmation supervision.
	ConfirmationInfrastructureFailed
	// ProcessEmergencyInterrupted identifies process-runtime emergency interruption.
	ProcessEmergencyInterrupted
	// InfrastructureFailed identifies otherwise classified campaign infrastructure failure.
	InfrastructureFailed
)

// AttemptKind identifies the terminal evidence retained for an attempt.
type AttemptKind uint8

const (
	// AttemptSettled identifies a command with an observed exit status.
	AttemptSettled AttemptKind = iota + 1
	// AttemptTimedOut identifies a command deadline.
	AttemptTimedOut
	// AttemptRunaway identifies a process-fuse trip.
	AttemptRunaway
	// AttemptStopped identifies an externally stopped attempt.
	AttemptStopped
	// AttemptInfrastructureFailed identifies uncertain attempt supervision.
	AttemptInfrastructureFailed
	// AttemptDrainageUnconfirmed identifies unconfirmed execution-domain drainage.
	AttemptDrainageUnconfirmed
)

// BoundFired identifies the command bound retained in attempt evidence.
type BoundFired uint8

const (
	// NoBoundFired identifies attempt evidence produced before its command deadline.
	NoBoundFired BoundFired = iota
	// CommandDeadlineFired identifies an inclusive command deadline observation.
	CommandDeadlineFired
)

// OutputSnapshot contains the immutable captured command-output prefix.
type OutputSnapshot struct {
	Bytes                 string
	Cutoff                uint64
	CompleteThroughCutoff bool
	Final                 bool
}

// FailureDiagnostics contains independent attempt infrastructure diagnostics.
type FailureDiagnostics struct {
	Wait          string
	RunningCensus string
	DrainCensus   string
	Termination   string
	Output        string
	Release       string
}

// ObservedCount contains an optional process-count observation.
type ObservedCount struct {
	Value   int
	Present bool
}

// AttemptResult contains the terminal evidence retained for one attempt.
type AttemptResult struct {
	Kind                    AttemptKind
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

// ResidualStage identifies the last proven custody stage of an unresolved attempt.
type ResidualStage uint8

const (
	// ResidualProspective identifies a launch whose release remained unresolved.
	ResidualProspective ResidualStage = iota + 1
	// ResidualOwned identifies an owned execution domain whose drainage remained unresolved.
	ResidualOwned
)

// ResidualCustody identifies one unresolved execution-domain obligation.
type ResidualCustody struct {
	Attempt     string
	Generation  uint64
	Stage       ResidualStage
	Transferred bool
}

// FatalAttempt contains the terminal evidence retained for an attempt in a fatal epoch.
type FatalAttempt struct {
	Attempt  string
	Evidence AttemptResult
}

// InvariantEvidence contains stable diagnostic context for a rejected internal transition.
type InvariantEvidence struct {
	Operation        string
	Reason           string
	Phase            string
	RejectedEvent    string
	StableIdentities []string
	Obligations      []string
	TraceTail        []string
}

// Result contains the terminal presentation facts for one campaign.
type Result struct {
	Outcome                 Outcome
	Score                   *Score
	Mutations               []MutationResult
	Total                   int
	Cause                   AbortCause
	Baseline                *AttemptResult
	Residual                []ResidualCustody
	FatalAttempts           []FatalAttempt
	Invariant               *InvariantEvidence
	ArtifactResidue         []string
	SingleAdmissionFallback bool
	report                  internalooze.ManagedReport
}

func projectResult(managed internalooze.ManagedReleaseResult, minimum float32, serial, colors bool) Result {
	result := Result{
		Outcome: projectOutcome(managed.Outcome), Total: managed.Total,
		ArtifactResidue:         append([]string(nil), managed.ArtifactResidue...),
		SingleAdmissionFallback: managed.SingleAdmissionFallback,
		report:                  internalooze.ProjectManagedReport(managed, minimum, serial, colors),
	}
	if managed.Cause != 0 {
		result.Cause = projectAbortCause(managed.Cause)
	}
	if managed.Baseline != nil {
		baseline := projectAttempt(*managed.Baseline)
		result.Baseline = &baseline
	}
	result.Residual = make([]ResidualCustody, len(managed.Residual))
	for index, residual := range managed.Residual {
		result.Residual[index] = ResidualCustody{
			Attempt: residual.Attempt, Generation: residual.Generation,
			Stage: projectResidualStage(residual.Stage), Transferred: residual.Transferred,
		}
	}
	result.FatalAttempts = make([]FatalAttempt, len(managed.FatalAttempts))
	for index, attempt := range managed.FatalAttempts {
		result.FatalAttempts[index] = FatalAttempt{
			Attempt: attempt.Attempt, Evidence: projectAttempt(attempt.Evidence),
		}
	}
	if managed.Invariant != nil {
		result.Invariant = &InvariantEvidence{
			Operation: managed.Invariant.Operation, Reason: managed.Invariant.Reason,
			Phase: managed.Invariant.Phase, RejectedEvent: managed.Invariant.RejectedEvent,
			StableIdentities: append([]string(nil), managed.Invariant.StableIdentities...),
			Obligations:      append([]string(nil), managed.Invariant.Obligations...),
			TraceTail:        append([]string(nil), managed.Invariant.TraceTail...),
		}
	}
	result.Mutations = make([]MutationResult, len(managed.Mutations))
	detected := 0
	for index, mutation := range managed.Mutations {
		outcome := projectMutationOutcome(mutation.Outcome)
		result.Mutations[index] = MutationResult{
			Label: mutation.File.Label(), Diff: mutation.File.Diff(gotextdiff.New()),
			Outcome: outcome, Primary: projectAttempt(mutation.Primary),
		}
		if mutation.Confirmation != nil {
			confirmation := projectAttempt(*mutation.Confirmation)
			result.Mutations[index].Confirmation = &confirmation
		}
		if outcome != Survived && outcome != 0 {
			detected++
		}
	}
	if result.Outcome == Completed {
		total := len(result.Mutations)
		result.Total = total
		value := float32(detected) / float32(total)
		result.Score = &Score{
			Detected: detected, Total: total, Value: value, Minimum: minimum, Passed: value >= minimum,
		}
	}

	return result
}

func projectResidualStage(stage internalooze.ManagedResidualStage) ResidualStage {
	switch stage {
	case internalooze.ManagedResidualProspective:
		return ResidualProspective
	case internalooze.ManagedResidualOwned:
		return ResidualOwned
	default:
		panic("managed residual stage is invalid")
	}
}

func projectAbortCause(cause internalooze.ManagedAbortCause) AbortCause {
	switch cause {
	case internalooze.ManagedAbortCampaignRegistration:
		return RegistrationRejected
	case internalooze.ManagedAbortSnapshotMaterialization:
		return SnapshotMaterializationFailed
	case internalooze.ManagedAbortCatalogueDiscovery:
		return CatalogueDiscoveryFailed
	case internalooze.ManagedAbortWorkspaceMaterialization:
		return WorkspaceMaterializationFailed
	case internalooze.ManagedAbortAdmissionRejected:
		return AdmissionRejected
	case internalooze.ManagedAbortFatalEpoch:
		return FatalEpochInterrupted
	case internalooze.ManagedAbortWorkspaceCleanup:
		return WorkspaceCleanupFailed
	case internalooze.ManagedAbortSnapshotCleanup:
		return SnapshotCleanupFailed
	case internalooze.ManagedAbortAttemptNotReleased:
		return AttemptNotReleased
	case internalooze.ManagedAbortProspectiveLaunch:
		return ProspectiveLaunchUnresolved
	case internalooze.ManagedAbortDrainageUnconfirmed:
		return DrainageUnconfirmed
	case internalooze.ManagedAbortBaselineFailed:
		return BaselineFailed
	case internalooze.ManagedAbortBaselineDeadline:
		return BaselineTimedOut
	case internalooze.ManagedAbortBaselineFuse:
		return BaselineRunaway
	case internalooze.ManagedAbortBaselineStopped:
		return BaselineStopped
	case internalooze.ManagedAbortBaselineInfrastructure:
		return BaselineInfrastructureFailed
	case internalooze.ManagedAbortPrimaryStopped:
		return PrimaryStopped
	case internalooze.ManagedAbortPrimaryInfrastructure:
		return PrimaryInfrastructureFailed
	case internalooze.ManagedAbortConfirmationInfrastructure:
		return ConfirmationInfrastructureFailed
	case internalooze.ManagedAbortProcessEmergency:
		return ProcessEmergencyInterrupted
	case internalooze.ManagedAbortInfrastructure:
		return InfrastructureFailed
	default:
		panic("managed abort cause is invalid")
	}
}

func projectAttempt(evidence internalooze.ManagedAttemptEvidence) AttemptResult {
	return AttemptResult{
		Kind: projectAttemptKind(evidence.Kind), Passed: evidence.Passed,
		Deadline: evidence.Deadline, LaunchDuration: evidence.LaunchDuration,
		CommandDuration: evidence.CommandDuration, BoundFired: BoundFired(evidence.BoundFired),
		Output: OutputSnapshot{
			Bytes: evidence.Output.Bytes, Cutoff: evidence.Output.Cutoff,
			CompleteThroughCutoff: evidence.Output.CompleteThroughCutoff, Final: evidence.Output.Final,
		},
		Failures: FailureDiagnostics{
			Wait: evidence.Failures.Wait, RunningCensus: evidence.Failures.RunningCensus,
			DrainCensus: evidence.Failures.DrainCensus, Termination: evidence.Failures.Termination,
			Output: evidence.Failures.Output, Release: evidence.Failures.Release,
		},
		Count:                   ObservedCount{Value: evidence.Count.Value, Present: evidence.Count.Present},
		ConfirmationProvisional: evidence.ConfirmationProvisional,
	}
}

func projectAttemptKind(kind internalooze.ManagedAttemptKind) AttemptKind {
	switch kind {
	case internalooze.ManagedAttemptSettled:
		return AttemptSettled
	case internalooze.ManagedAttemptDeadline:
		return AttemptTimedOut
	case internalooze.ManagedAttemptFuse:
		return AttemptRunaway
	case internalooze.ManagedAttemptStopped:
		return AttemptStopped
	case internalooze.ManagedAttemptInfrastructure:
		return AttemptInfrastructureFailed
	case internalooze.ManagedAttemptDrainUnconfirmed:
		return AttemptDrainageUnconfirmed
	default:
		panic("managed attempt kind is invalid")
	}
}

func projectOutcome(outcome internalooze.ManagedOutcome) Outcome {
	switch outcome {
	case internalooze.ManagedCompleted:
		return Completed
	case internalooze.ManagedNoMutants:
		return NoMutants
	case internalooze.ManagedAborted:
		return Aborted
	case internalooze.ManagedCleanupUnconfirmed:
		return CleanupUnconfirmed
	case internalooze.ManagedInvariantViolation:
		return InvariantViolation
	default:
		panic("managed campaign outcome is invalid")
	}
}

func projectMutationOutcome(outcome internalooze.ManagedMutationOutcome) MutationOutcome {
	switch outcome {
	case internalooze.ManagedSurvived:
		return Survived
	case internalooze.ManagedKilled:
		return Killed
	case internalooze.ManagedTimedOut:
		return TimedOut
	case internalooze.ManagedRunaway:
		return Runaway
	case 0:
		return 0
	default:
		panic("managed mutation outcome is invalid")
	}
}
