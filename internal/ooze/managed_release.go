package ooze

import (
	"runtime"
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
)

type ManagedMutationOutcome uint8

const (
	ManagedSurvived ManagedMutationOutcome = iota + 1
	ManagedKilled
	ManagedTimedOut
	ManagedRunaway
)

type ManagedMutationResult struct {
	File    *gomutatedfile.GoMutatedFile
	Outcome ManagedMutationOutcome
}

type ManagedReleaseResult struct {
	Outcome   ManagedOutcome
	Mutations []ManagedMutationResult
	Cause     string
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

func (process *managedProcess) release(configuration ManagedReleaseConfiguration) ManagedReleaseResult {
	if configuration.Lineage == 0 || configuration.Repository == nil || configuration.TemporaryDir == nil {
		panic("managed release configuration is incomplete")
	}
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

func presentManagedRelease(result managedCampaignResult) ManagedReleaseResult {
	switch outcome := result.outcome.(type) {
	case noMutantsOutcome:
		return ManagedReleaseResult{Outcome: ManagedNoMutants}
	case completedOutcome:
		mutations := make([]ManagedMutationResult, len(outcome.mutants))
		for index, mutant := range outcome.mutants {
			mutations[index] = ManagedMutationResult{
				File: result.mutations[mutant.mutant], Outcome: presentManagedMutation(mutant.kind),
			}
		}

		return ManagedReleaseResult{Outcome: ManagedCompleted, Mutations: mutations}
	case abortedOutcome:
		return ManagedReleaseResult{Outcome: ManagedAborted, Cause: outcome.cause}
	case nil:
		if _, fatal := result.failure.(cleanupUnconfirmedFault); fatal {
			return ManagedReleaseResult{Outcome: ManagedCleanupUnconfirmed, Cause: "cleanup unconfirmed"}
		}
	}
	panic("managed campaign produced no terminal result")
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
