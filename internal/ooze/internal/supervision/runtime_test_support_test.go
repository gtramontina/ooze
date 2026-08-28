package supervision

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/stretchr/testify/require"
)

const (
	sharedAdmission        = processruntime.SharedAdmission
	exclusiveAdmission     = processruntime.ExclusiveAdmission
	serialPrimaryAdmission = processruntime.SerialPrimaryAdmission
)

type campaignLineage = processruntime.Lineage
type campaignToken = processruntime.Campaign
type fatalEpochID uint64
type runtimeFatalCause string

type campaignProvenance struct{ lineage campaignLineage }

type campaignRegistration struct {
	decision processruntime.CampaignDecision
	token    campaignToken
}

type admissionAuthority struct {
	campaign campaignToken
	attempt  attemptIdentity
	class    processruntime.AdmissionClass
	profile  Profile
	deadline time.Duration
	grant    processruntime.Grant
}

type admissionRequest = admissionAuthority
type admissionRequestToken = admissionAuthority
type admissionGrant = admissionAuthority

type admissionAwait struct {
	decision processruntime.AdmissionDecision
	request  admissionRequestToken
	delivery <-chan admissionGrant
	fatal    fatalEpochID
}

type startCommittedResult struct {
	decision   processruntime.StartDecision
	generation attemptGeneration
}

type observationResult struct {
	generation                                     attemptGeneration
	confirmationProvisional                        bool
	pressureTransitioned, runtimeClosureInProgress bool
	confirmationObserved, confirmationQueueDrained bool
	fatalEpoch                                     fatalEpochID
}

type runtimeClosure struct {
	epoch    fatalEpochID
	residual []residualCustody
}

type attemptTripKind uint8

const (
	deadlineTrip attemptTripKind = iota + 1
	fuseTrip
)

type attemptObservation interface{ attemptObservation() }

type launchOwned struct{}
type launchNotReleased struct{ resourceExhausted bool }
type attemptSettled struct {
	profile  Profile
	deadline time.Duration
}
type attemptTripped struct {
	kind     attemptTripKind
	profile  Profile
	deadline time.Duration
}
type launchUnconfirmed struct{}
type drainUnconfirmed struct{}
type attemptStopped struct{}
type attemptInfrastructure struct{ cause string }

func (launchOwned) attemptObservation()           {}
func (launchNotReleased) attemptObservation()     {}
func (attemptSettled) attemptObservation()        {}
func (attemptTripped) attemptObservation()        {}
func (launchUnconfirmed) attemptObservation()     {}
func (drainUnconfirmed) attemptObservation()      {}
func (attemptStopped) attemptObservation()        {}
func (attemptInfrastructure) attemptObservation() {}

type pendingStartCell = processruntime.StartCell
type installedStart = processruntime.PreparedStart
type processRuntimeShell = processruntime.Runtime

func newProcessRuntimeShell(capacity int) *processruntime.Runtime {
	return processruntime.New(capacity)
}

func newProcessRuntimeShellWithObserver(capacity int, observer processruntime.Observer) *processruntime.Runtime {
	return processruntime.NewObserved(capacity, observer)
}

type testOwnerObserver struct {
	gate sync.RWMutex
	next OwnerCutSequence
}

func newSimulationRecorder() *testOwnerObserver { return &testOwnerObserver{} }

func (observer *testOwnerObserver) enter() func() {
	observer.gate.RLock()
	return observer.gate.RUnlock
}

func (observer *testOwnerObserver) Enter() func() { return observer.enter() }

func (*testOwnerObserver) Publish(
	OwnerCutReservation,
	Fact,
	Event,
	Projection,
	[]Effect,
) {
}

func (*testOwnerObserver) Complete(Effect) {}

func simulationForbiddenValuePath(value reflect.Value, path string) string {
	if !value.IsValid() {
		return ""
	}
	if value.Type() == reflect.TypeOf(time.Time{}) {
		return path
	}
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return path
	case reflect.Interface, reflect.Pointer:
		if !value.IsNil() {
			return simulationForbiddenValuePath(value.Elem(), path)
		}
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			if found := simulationForbiddenValuePath(
				value.Field(index), path+"."+value.Type().Field(index).Name,
			); found != "" {
				return found
			}
		}
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			if found := simulationForbiddenValuePath(
				value.Index(index), fmt.Sprintf("%s[%d]", path, index),
			); found != "" {
				return found
			}
		}
	}
	return ""
}

func registerCampaignForTest(runtime *processruntime.Runtime, provenance campaignProvenance) campaignRegistration {
	result := runtime.RegisterCampaign(provenance.lineage)
	return campaignRegistration{decision: result.Decision(), token: result.Campaign()}
}

func processRuntimeAdmission(value admissionAuthority) processruntime.Admission {
	return processruntime.Admission{
		Campaign: value.campaign, Attempt: string(value.attempt), Class: value.class,
		Profile: value.profile, Deadline: value.deadline,
	}
}

func runtimeAdmissionValue(value processruntime.Admission) admissionAuthority {
	return admissionAuthority{
		campaign: value.Campaign, attempt: attemptIdentity(value.Attempt), class: value.Class,
		profile: value.Profile, deadline: value.Deadline,
	}
}

func requestAdmissionForTest(runtime *processruntime.Runtime, request admissionRequest) admissionAwait {
	await := runtime.RequestAdmission(processRuntimeAdmission(request))
	delivery := make(chan admissionGrant, 1)
	go func() {
		grant, received := await.Receive()
		if received {
			value := runtimeAdmissionValue(grant.Admission())
			value.grant = grant
			delivery <- value
		}
		close(delivery)
	}()
	return admissionAwait{
		decision: await.Decision(), request: runtimeAdmissionValue(await.Request()),
		delivery: delivery, fatal: fatalEpochID(runtime.FatalEpoch()),
	}
}

type startInstallation struct {
	grant admissionGrant
	cell  *pendingStartCell
}

type preparedStart struct {
	result startCommittedResult
	start  installedStart
}

func startCommittedForTest(runtime *processruntime.Runtime, grant admissionGrant, installation startInstallation) preparedStart {
	prepared := runtime.CommitStart(grant.grant, installation.cell)
	return preparedStart{
		result: startCommittedResult{decision: prepared.Decision(), generation: prepared.Generation()},
		start:  prepared,
	}
}

func processRuntimeObservation(observation attemptObservation) processruntime.Observation {
	switch observation := observation.(type) {
	case launchOwned:
		return processruntime.Owned()
	case launchNotReleased:
		return processruntime.NotReleased(observation.resourceExhausted)
	case attemptSettled:
		return processruntime.Settled(observation.profile, observation.deadline)
	case attemptTripped:
		return processruntime.Tripped(observation.kind == fuseTrip, observation.profile, observation.deadline)
	case launchUnconfirmed:
		return processruntime.LaunchUnconfirmed()
	case drainUnconfirmed:
		return processruntime.DrainUnconfirmed()
	case attemptStopped:
		return processruntime.Stopped()
	case attemptInfrastructure:
		return processruntime.Infrastructure(observation.cause)
	default:
		return processruntime.Observation{}
	}
}

func runtimeReceipt(receipt processruntime.Receipt) observationResult {
	return observationResult{
		generation:               receipt.Generation(),
		confirmationProvisional:  receipt.ConfirmationProvisional(),
		pressureTransitioned:     receipt.PressureTransitioned(),
		runtimeClosureInProgress: receipt.RuntimeClosureInProgress(),
		confirmationObserved:     receipt.ConfirmationObserved(),
		confirmationQueueDrained: receipt.ConfirmationQueueDrained(),
		fatalEpoch:               fatalEpochID(receipt.FatalEpoch()),
	}
}

func observeAttemptForTest(runtime *processruntime.Runtime, generation attemptGeneration, observation attemptObservation) observationResult {
	return runtimeReceipt(runtime.Observe(generation, processRuntimeObservation(observation)))
}

func settleEmergencyForTest(runtime *processruntime.Runtime, sweep emergencySweep) emergencySettlement {
	return runtimeEmergencySettlement(runtime.SettleEmergency(processRuntimeResolutions(sweep)))
}

func closeRuntimeForTest(runtime *processruntime.Runtime, cause runtimeFatalCause) runtimeClosure {
	closure := runtime.Close(string(cause))
	residuals := make([]residualCustody, len(closure.Residual()))
	for index, residual := range closure.Residual() {
		stage := admissionOwned
		if residual.Prospective() {
			stage = admissionProspective
		}
		residuals[index] = residualCustody{
			generation: residual.Generation(), attempt: attemptIdentity(residual.Attempt()), stage: stage,
		}
	}
	return runtimeClosure{epoch: fatalEpochID(closure.Epoch()), residual: residuals}
}

func startOwned(runtime *processruntime.Runtime, grant admissionGrant) startCommittedResult {
	cell := processruntime.NewStartCell()
	prepared := startCommittedForTest(runtime, grant, startInstallation{grant: grant, cell: cell})
	if prepared.result.decision == processruntime.StartAccepted {
		observeAttemptForTest(runtime, prepared.result.generation, launchOwned{})
	}
	return prepared.result
}

func assertInvariantViolation(t *testing.T, action func()) {
	t.Helper()
	defer func() {
		switch recover().(type) {
		case Violation, processruntime.Violation:
		default:
			require.FailNow(t, "action did not panic with an invariant violation")
		}
	}()
	action()
}
