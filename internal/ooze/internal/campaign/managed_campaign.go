package campaign

import (
	"strconv"
	"strings"
	"time"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
	"github.com/gtramontina/ooze/viruses"
)

type managedTemporaryDirectoryFactory interface{ New() string }

type managedObservedLaunch struct {
	result  supervision.LaunchResult
	receipt processruntime.Receipt
}

type managedObservedTerminal struct {
	terminal supervision.Terminal
	receipt  processruntime.Receipt
}

type managedObservedEmergency struct {
	epoch      fatalEpochID
	settlement processruntime.EmergencySettlement
}

type managedAttemptSystem interface {
	reserveLaunch(*processruntime.StartCell, supervision.Spec)
	discardLaunch(*processruntime.StartCell)
	launch(processruntime.PreparedStart, supervision.Spec) managedObservedLaunch
	wait(attemptGeneration, *supervision.OwnedAttempt) managedObservedTerminal
	stop(*supervision.OwnedAttempt)
	emergency(fatalEpochID) managedObservedEmergency
}

type managedCampaignConstruction struct {
	runtime            *processruntime.Runtime
	observer           Observer
	repository         Repository
	temporaryDirectory managedTemporaryDirectoryFactory
	attempts           managedAttemptSystem
	observe            func(ManagedProgress)
}

type managedCampaignRequest struct {
	identity        campaignIdentity
	lineage         campaignLineage
	command         []string
	env             []string
	profile         Profile
	peers           int
	mutationTimeout time.Duration
	viruses         []viruses.Virus
}

type managedCampaignResult struct {
	outcome   campaignOutcome
	failure   campaignFailure
	mutations map[mutantIdentity]*gomutatedfile.GoMutatedFile
}

type managedCampaignRunner struct {
	managedCampaignConstruction
	state        campaignState
	nextEvent    campaignEventID
	snapshot     TemporaryRepository
	mutations    map[mutantIdentity]*gomutatedfile.GoMutatedFile
	workspaces   map[string]TemporaryRepository
	starts       map[attemptGeneration]processruntime.PreparedStart
	owned        map[attemptGeneration]*supervision.OwnedAttempt
	authorities  map[campaignAdmission]processruntime.Grant
	attemptFacts map[attemptGeneration]managedAttemptFacts
	runtimeToken campaignToken
	terminals    chan managedTerminalObservation
	pending      int
	emergency    bool
	observer     Observer
}

type managedAttemptFacts struct {
	kind                       campaignAttemptKind
	completesConfirmationQueue bool
}

type managedTerminalObservation struct {
	attempt    attemptIdentity
	generation attemptGeneration
	observed   managedObservedTerminal
}

func newManagedCampaignRunner(construction managedCampaignConstruction) *managedCampaignRunner {
	if construction.runtime == nil || construction.repository == nil || construction.temporaryDirectory == nil ||
		construction.attempts == nil {
		panic("managed campaign construction is incomplete")
	}

	return &managedCampaignRunner{
		managedCampaignConstruction: construction,
		mutations:                   make(map[mutantIdentity]*gomutatedfile.GoMutatedFile),
		workspaces:                  make(map[string]TemporaryRepository),
		starts:                      make(map[attemptGeneration]processruntime.PreparedStart),
		owned:                       make(map[attemptGeneration]*supervision.OwnedAttempt),
		authorities:                 make(map[campaignAdmission]processruntime.Grant),
		attemptFacts:                make(map[attemptGeneration]managedAttemptFacts),
		observer:                    construction.observer,
	}
}

func (runner *managedCampaignRunner) rememberAuthority(authority processruntime.Grant) {
	runner.authorities[campaignAdmissionFact(authority.Admission())] = authority
}

func (runner *managedCampaignRunner) authority(fact campaignAdmission) processruntime.Grant {
	authority, ok := runner.authorities[fact]
	if !ok {
		panic("managed admission authority is missing")
	}

	return authority
}

func managedExecutionEnvironment(environment []string, profile Profile, capacity int) []string {
	value := 1
	if profile == SerialProfile {
		value = capacity
	}
	const prefix = "GOMAXPROCS="
	resolved := make([]string, 0, len(environment)+1)
	for _, variable := range environment {
		if !strings.HasPrefix(variable, prefix) {
			resolved = append(resolved, variable)
		}
	}

	return append(resolved, prefix+strconv.Itoa(value))
}
