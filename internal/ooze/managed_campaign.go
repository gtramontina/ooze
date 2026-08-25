package ooze

import (
	"strconv"
	"strings"
	"time"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/viruses"
)

type managedTemporaryDirectoryFactory interface{ New() string }

type managedObservedLaunch struct {
	result  LaunchResult
	receipt observationResult
}

type managedObservedTerminal struct {
	terminal Terminal
	receipt  observationResult
}

type managedObservedEmergency struct {
	epoch      fatalEpochID
	settlement emergencySettlement
}

type managedAttemptSystem interface {
	launch(installedStart, Spec) managedObservedLaunch
	wait(attemptGeneration, *OwnedAttempt) managedObservedTerminal
	stop(*OwnedAttempt)
	emergency(fatalEpochID) managedObservedEmergency
}

type managedCampaignConstruction struct {
	runtime            *processRuntimeShell
	repository         Repository
	temporaryDirectory managedTemporaryDirectoryFactory
	attempts           managedAttemptSystem
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
	starts       map[attemptGeneration]installedStart
	owned        map[attemptGeneration]*OwnedAttempt
	authorities  map[campaignAdmission]admissionAuthority
	attemptFacts map[attemptGeneration]managedAttemptFacts
	runtimeToken campaignToken
	terminals    chan managedTerminalObservation
	pending      int
	emergency    bool
	recorder     *simulationRecorder
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
		starts:                      make(map[attemptGeneration]installedStart),
		owned:                       make(map[attemptGeneration]*OwnedAttempt),
		authorities:                 make(map[campaignAdmission]admissionAuthority),
		attemptFacts:                make(map[attemptGeneration]managedAttemptFacts),
		recorder:                    construction.runtime.recorder,
	}
}

func (runner *managedCampaignRunner) rememberAuthority(authority admissionAuthority) {
	runner.authorities[campaignAdmissionFact(authority)] = authority
}

func (runner *managedCampaignRunner) authority(fact campaignAdmission) admissionAuthority {
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
