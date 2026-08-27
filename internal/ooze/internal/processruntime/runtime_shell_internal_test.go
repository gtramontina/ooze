package processruntime

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (s *processRuntimeShell) snapshot() processRuntime {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.core.clone()
}

func TestStartInstallationKeepsLaunchDormantUntilInstalledStart(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	cell := pendingStartCell{}
	launchCalls := 0
	installation := startInstallation{cell: &cell}
	dormant := func(_ attemptGeneration) attemptObservation {
		launchCalls++

		return launchOwned{}
	}
	assert.EqualValues(t, 0, launchCalls, "construction launched or installed: calls/generation=%d/%d", launchCalls, cell.installedGeneration())
	assert.EqualValues(t, 0, cell.installedGeneration(), "construction launched or installed: calls/generation=%d/%d", launchCalls, cell.installedGeneration())
	installed := installation.install(7, shell)
	assert.EqualValues(t, 0, launchCalls, "installation launched or lost generation: calls/cell/start=%d/%d/%#v", launchCalls, cell.installedGeneration(), installed)
	assert.EqualValues(t, 7, cell.installedGeneration(), "installation launched or lost generation: calls/cell/start=%d/%d/%#v", launchCalls, cell.installedGeneration(), installed)
	assertInvariantViolation(t, func() { (installedStart{}).launch(dormant) })
	nilInstalled := startInstallation{cell: &pendingStartCell{}}.install(8, newProcessRuntimeShell(1))
	assertInvariantViolation(t, func() { nilInstalled.launch(nil) })
	assert.EqualValues(t, 0, launchCalls, "zero or nil launch reached native work: calls=%d", launchCalls)
	observed := installed.launch(dormant)
	assert.EqualValues(t, 1, launchCalls, "narrowed launch calls/observation=%d/%#v", launchCalls, observed)
	assertInvariantViolation(t, func() { installed.launch(dormant) })
	assert.EqualValues(t, 1, launchCalls, "reused installed start reached native work: calls=%d", launchCalls)
}

func TestStartInstallationRejectsCrossPairedGrantBeforeCellMutation(t *testing.T) {
	for _, test := range []struct {
		name    string
		reverse bool
	}{
		{"campaign_a_grant_with_campaign_b_installation", false},
		{"campaign_b_grant_with_campaign_a_installation", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reverse := test.reverse
			shell := newProcessRuntimeShell(2)
			campaignA := shell.registerCampaign(campaignProvenance{lineage: 11})
			campaignB := shell.registerCampaign(campaignProvenance{lineage: 22})
			requestA := shell.requestAdmission(admissionRequest{campaign: campaignA.token, attempt: "a", class: sharedAdmission})
			requestB := shell.requestAdmission(admissionRequest{campaign: campaignB.token, attempt: "b", class: sharedAdmission})
			grantA, grantB := <-requestA.delivery, <-requestB.delivery
			cellA, cellB := pendingStartCell{}, pendingStartCell{}
			emergency := shell.runtimeEmergency()
			grant, installation := grantA, (startInstallation{grant: grantB, cell: &cellB})
			if reverse {
				grant, installation = grantB, startInstallation{grant: grantA, cell: &cellA}
			}
			assertInvariantViolation(t, func() { shell.startCommitted(grant, installation) })
			snapshot := shell.snapshot()
			indexA, indexB := snapshot.admissionIndex(grantA), snapshot.admissionIndex(grantB)
			assert.EqualValues(t, 0, cellA.installedGeneration(), "reverse=%t cross-pair mutated installation/custody: %#v", reverse, snapshot)
			assert.EqualValues(t, 0, cellB.installedGeneration(), "reverse=%t cross-pair mutated installation/custody: %#v", reverse, snapshot)
			assert.Equal(t, runtimeFatalClosing, snapshot.lifecycle, "reverse=%t cross-pair mutated installation/custody: %#v", reverse, snapshot)
			assert.EqualValues(t, 0, len(snapshot.residualCustody()), "reverse=%t cross-pair mutated installation/custody: %#v", reverse, snapshot)
			require.False(t, indexA < 0, "reverse=%t cross-pair mutated installation/custody: %#v", reverse, snapshot)
			require.False(t, indexB < 0, "reverse=%t cross-pair mutated installation/custody: %#v", reverse, snapshot)
			assert.Equal(t, admissionGranted, snapshot.admissions[indexA].stage, "reverse=%t cross-pair mutated installation/custody: %#v", reverse, snapshot)
			assert.Equal(t, admissionGranted, snapshot.admissions[indexB].stage, "reverse=%t cross-pair mutated installation/custody: %#v", reverse, snapshot)
			assert.Equal(t, dispositionReturnedAfterClosure, snapshot.admissions[indexA].disposition, "reverse=%t cross-pair mutated installation/custody: %#v", reverse, snapshot)
			assert.Equal(t, dispositionReturnedAfterClosure, snapshot.admissions[indexB].disposition, "reverse=%t cross-pair mutated installation/custody: %#v", reverse, snapshot)
			select {
			case <-emergency:
			default:
				require.FailNowf(t, "cross-pair did not broadcast fatal closure", "reverse=%t", reverse)
			}
			{
				returned := shell.acknowledgeGrantReturn(grantA)
				assert.Equal(t, admissionReturnedAfterClosure, returned.decision, "reverse=%t grant A return=%#v", reverse, returned)
			}
			{
				returned := shell.acknowledgeGrantReturn(grantB)
				assert.Equal(t, admissionReturnedAfterClosure, returned.decision, "reverse=%t grant B return=%#v", reverse, returned)
			}
			{
				snapshot = shell.snapshot()
				assert.Equal(t, runtimeFatalClosing, snapshot.lifecycle, "reverse=%t peer returns closed ahead of settlement: %#v", reverse, snapshot)
				assert.EqualValues(t, 0, len(snapshot.admissions), "reverse=%t peer returns closed ahead of settlement: %#v", reverse, snapshot)
			}
			settled := shell.settleEmergency(emergencySweep{})
			{
				snapshot = shell.snapshot()
				assert.Equal(t, runtimeClosedDrained, snapshot.lifecycle, "reverse=%t empty settlement did not finish closure: %#v/%#v", reverse, settled, snapshot)
				assert.EqualValues(t, 0, len(settled.acknowledged), "reverse=%t empty settlement did not finish closure: %#v/%#v", reverse, settled, snapshot)
				assert.EqualValues(t, 0, len(settled.residual), "reverse=%t empty settlement did not finish closure: %#v/%#v", reverse, settled, snapshot)
			}
		})
	}
}

func TestInstalledStartZeroedCopyUsesAuthoritativeCellGeneration(t *testing.T) {
	shell := newProcessRuntimeShell(2)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 11})
	active := shell.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "active", class: sharedAdmission,
	})
	pending := shell.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "pending", class: sharedAdmission,
	})
	activeGrant, pendingGrant := <-active.delivery, <-pending.delivery
	prepared := shell.startCommitted(activeGrant, startInstallation{grant: activeGrant, cell: &pendingStartCell{}})
	corrupted := prepared.start
	corrupted.generation = 0
	launchCalls := 0
	assertInvariantViolation(t, func() {
		corrupted.launch(func(attemptGeneration) attemptObservation {
			launchCalls++

			return launchOwned{}
		})
	})
	want := []runtimeFatalCause{runtimeFatalCause(fmt.Sprintf(
		"launch invariant: start or launch is zero generation=%d", prepared.result.generation,
	))}
	snapshot := shell.snapshot()
	assert.EqualValues(t, 0, launchCalls, "zeroed copy calls/state=%d/%#v, want causes %#v", launchCalls, snapshot, want)
	assert.Equal(t, runtimeFatalSettledClosing, snapshot.lifecycle, "zeroed copy calls/state=%d/%#v, want causes %#v", launchCalls, snapshot, want)
	assert.Equal(t, want, snapshot.fatalCauses, "zeroed copy calls/state=%d/%#v, want causes %#v", launchCalls, snapshot, want)
	{
		returned := shell.acknowledgeGrantReturn(pendingGrant)
		assert.Equal(t, admissionReturnedAfterClosure, returned.decision, "pending peer return=%#v", returned)
	}
}

func TestInstalledStartRejectsConcurrentCrossPairWithoutConsumingCustody(t *testing.T) {
	shell := newProcessRuntimeShell(2)
	campaignA := shell.registerCampaign(campaignProvenance{lineage: 11})
	campaignB := shell.registerCampaign(campaignProvenance{lineage: 22})
	requestA := shell.requestAdmission(admissionRequest{
		campaign: campaignA.token, attempt: "a", class: sharedAdmission,
	})
	requestB := shell.requestAdmission(admissionRequest{
		campaign: campaignB.token, attempt: "b", class: sharedAdmission,
	})
	cellA, cellB := pendingStartCell{}, pendingStartCell{}
	grantA, grantB := <-requestA.delivery, <-requestB.delivery
	preparedA := shell.startCommitted(grantA, startInstallation{grant: grantA, cell: &cellA})
	preparedB := shell.startCommitted(grantB, startInstallation{grant: grantB, cell: &cellB})
	crossedA, crossedB := preparedA.start, preparedB.start
	crossedA.generation = preparedB.result.generation
	crossedB.generation = preparedA.result.generation
	crossed := []installedStart{crossedA, crossedB}
	begin := make(chan struct{})
	panics := make([]any, len(crossed))
	launchCalls := 0
	var launchMutex sync.Mutex
	var wait sync.WaitGroup
	for index, start := range crossed {
		wait.Go(func() {
			defer func() { panics[index] = recover() }()
			<-begin
			start.launch(func(_ attemptGeneration) attemptObservation {
				launchMutex.Lock()
				launchCalls++
				launchMutex.Unlock()

				return launchOwned{}
			})
		})
	}
	close(begin)
	wait.Wait()
	assert.EqualValues(t, 0, launchCalls, "cross-pair calls/panics=%d/%#v", launchCalls, panics)
	assert.NotNil(t, panics[0], "cross-pair calls/panics=%d/%#v", launchCalls, panics)
	assert.NotNil(t, panics[1], "cross-pair calls/panics=%d/%#v", launchCalls, panics)
	want := []residualCustody{
		{generation: preparedA.result.generation, attempt: grantA.attempt, stage: admissionProspective, transferred: true},
		{generation: preparedB.result.generation, attempt: grantB.attempt, stage: admissionProspective, transferred: true},
	}
	snapshot := shell.snapshot()
	{
		got := snapshot.residualCustody()
		assert.Equal(t, runtimeClosedUnconfirmed, snapshot.lifecycle, "cross-pair state/residual=%#v/%#v, want %#v", snapshot, got, want)
		assert.Equal(t, want, got, "cross-pair state/residual=%#v/%#v, want %#v", snapshot, got, want)
	}
}

func TestInstalledStartCopiesLaunchExactlyOnceConcurrently(t *testing.T) {
	const samples = 100
	for sample := range samples {
		shell := newProcessRuntimeShell(1)
		campaign := shell.registerCampaign(campaignProvenance{lineage: campaignLineage(sample + 1)})
		requested := shell.requestAdmission(admissionRequest{
			campaign: campaign.token, attempt: "a", class: sharedAdmission,
		})
		grant := <-requested.delivery
		prepared := shell.startCommitted(grant, startInstallation{grant: grant, cell: &pendingStartCell{}})
		copies := []installedStart{prepared.start, prepared.start}
		begin := make(chan struct{})
		panics := make([]any, len(copies))
		launchCalls := 0
		var launchMutex sync.Mutex
		var wait sync.WaitGroup
		for index, start := range copies {
			wait.Go(func() {
				defer func() { panics[index] = recover() }()
				<-begin
				start.launch(func(_ attemptGeneration) attemptObservation {
					launchMutex.Lock()
					launchCalls++
					launchMutex.Unlock()

					return launchOwned{}
				})
			})
		}
		close(begin)
		wait.Wait()
		assert.EqualValues(t, 1, launchCalls, "sample %d calls/panics=%d/%#v", sample, launchCalls, panics)
		assert.NotEqual(t, (panics[1] == nil), (panics[0] == nil), "sample %d calls/panics=%d/%#v", sample, launchCalls, panics)
		want := []residualCustody{{
			generation: prepared.result.generation, attempt: grant.attempt,
			stage: admissionProspective, transferred: true,
		}}
		snapshot := shell.snapshot()
		assert.Equal(t, runtimeClosedUnconfirmed, snapshot.lifecycle, "sample %d copied-start state=%#v", sample, snapshot)
		assert.Equal(t, want, snapshot.residualCustody(), "sample %d copied-start state=%#v", sample, snapshot)
	}
}

func TestStartCommittedLockedBoundaryContainsNoExecutionCapability(t *testing.T) {
	type executableAlias = func()
	type nestedExecutable struct {
		next      *nestedExecutable
		callbacks map[string][]executableAlias
	}
	_ = nestedExecutable{next: nil, callbacks: nil}
	{
		path, found := executionCapabilityPath(reflect.TypeFor[nestedExecutable](), nil)
		assert.True(t, found, "structural guard missed nested executable field at %s", path)
	}
	method := reflect.TypeOf((*processRuntimeShell).startCommitted)
	for parameter := 1; parameter < method.NumIn(); parameter++ {
		{
			path, found := executionCapabilityPath(method.In(parameter), nil)
			assert.False(t, found, "startCommitted input %d contains executable capability at %s", parameter, path)
		}
	}
	{
		path, found := executionCapabilityPath(reflect.TypeFor[startInstallation](), nil)
		assert.False(t, found, "startInstallation contains executable capability at %s", path)
	}
}

func executionCapabilityPath(value reflect.Type, seen map[reflect.Type]bool) (string, bool) {
	if seen == nil {
		seen = make(map[reflect.Type]bool)
	}
	if seen[value] {
		return "", false
	}
	seen[value] = true
	if value.Kind() == reflect.Func || value.Kind() == reflect.Interface {
		return value.String(), true
	}
	if value.Kind() == reflect.Pointer {
		return executionCapabilityPath(value.Elem(), seen)
	}
	if value.Kind() == reflect.Array || value.Kind() == reflect.Chan ||
		value.Kind() == reflect.Slice {
		return executionCapabilityPath(value.Elem(), seen)
	}
	if value.Kind() == reflect.Map {
		if path, found := executionCapabilityPath(value.Key(), seen); found {
			return "key." + path, true
		}

		return executionCapabilityPath(value.Elem(), seen)
	}
	if value.Kind() != reflect.Struct {
		return "", false
	}
	for field := range value.NumField() {
		path, found := executionCapabilityPath(value.Field(field).Type, seen)
		if found {
			return value.String() + "." + value.Field(field).Name + "." + path, true
		}
	}

	return "", false
}

func TestProcessRuntimeShellCopiedInstallationCannotInstallOrLaunchTwice(t *testing.T) {
	cell := pendingStartCell{}
	launchCalls := 0
	dormant := func(_ attemptGeneration) attemptObservation {
		launchCalls++

		return launchOwned{}
	}
	firstShell := newProcessRuntimeShell(1)
	firstCampaign := firstShell.registerCampaign(campaignProvenance{lineage: 11})
	firstRequest := firstShell.requestAdmission(admissionRequest{
		campaign: firstCampaign.token, attempt: "first", class: sharedAdmission,
	})
	firstGrant := <-firstRequest.delivery
	installation := startInstallation{grant: firstGrant, cell: &cell}
	copied := installation
	first := firstShell.startCommitted(firstGrant, installation)
	observed := first.start.launch(dormant)
	firstShell.observeAttempt(first.result.generation, observed)

	secondShell := newProcessRuntimeShell(1)
	secondCampaign := secondShell.registerCampaign(campaignProvenance{lineage: 22})
	secondRequest := secondShell.requestAdmission(admissionRequest{
		campaign: secondCampaign.token, attempt: "second", class: sharedAdmission,
	})
	secondGrant := <-secondRequest.delivery
	copied.grant = secondGrant
	assertInvariantViolation(t, func() { secondShell.startCommitted(secondGrant, copied) })

	assert.Equal(t, startCommittedAccepted, first.result.decision, "first/calls/second=%#v/%d/%#v", first.result, launchCalls, secondShell.snapshot())
	assert.EqualValues(t, 1, launchCalls, "first/calls/second=%#v/%d/%#v", first.result, launchCalls, secondShell.snapshot())
	assert.Equal(t, runtimeFatalClosing, secondShell.snapshot().lifecycle, "first/calls/second=%#v/%d/%#v", first.result, launchCalls, secondShell.snapshot())
	assert.EqualValues(t, 0, len(secondShell.snapshot().residualCustody()), "first/calls/second=%#v/%d/%#v", first.result, launchCalls, secondShell.snapshot())
}

func TestProcessRuntimeShellInvalidInstallationPublishesNoProspectiveCustody(t *testing.T) {
	tests := []struct {
		name         string
		installation startInstallation
	}{
		{
			name:         "zero installation",
			installation: startInstallation{},
		},
		{
			name:         "nil cell",
			installation: startInstallation{cell: nil},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			shell := newProcessRuntimeShell(1)
			campaign := shell.registerCampaign(campaignProvenance{lineage: 11})
			requested := shell.requestAdmission(admissionRequest{
				campaign: campaign.token,
				attempt:  "a",
				class:    sharedAdmission,
			})
			grant := <-requested.delivery

			installation := test.installation
			installation.grant = grant
			assertInvariantViolation(t, func() { shell.startCommitted(grant, installation) })
			snapshot := shell.snapshot()
			assert.Equal(t, runtimeFatalClosing, snapshot.lifecycle, "invalid installation published prospective custody: %#v", snapshot)
			assert.EqualValues(t, 0, len(snapshot.residualCustody()), "invalid installation published prospective custody: %#v", snapshot)
		})
	}
}

func TestProcessRuntimeShellConsumedGrantCannotLaunchTwice(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 11})
	requested := shell.requestAdmission(admissionRequest{campaign: campaign.token, attempt: "a", class: sharedAdmission})
	grant := <-requested.delivery
	first := startOwned(shell, grant)
	cell := pendingStartCell{}
	launchCalls := 0
	second := shell.startCommitted(grant, startInstallation{grant: grant, cell: &cell})
	assert.Equal(t, startCommittedAccepted, first.decision, "first/second/cell/calls=%#v/%#v/%d/%d", first, second.result, cell.installedGeneration(), launchCalls)
	assert.Equal(t, startCommittedRejectedGrant, second.result.decision, "first/second/cell/calls=%#v/%#v/%d/%d", first, second.result, cell.installedGeneration(), launchCalls)
	assert.EqualValues(t, 0, cell.installedGeneration(), "first/second/cell/calls=%#v/%#v/%d/%d", first, second.result, cell.installedGeneration(), launchCalls)
	assert.EqualValues(t, 0, launchCalls, "first/second/cell/calls=%#v/%#v/%d/%d", first, second.result, cell.installedGeneration(), launchCalls)
}

func startOwned(shell *processRuntimeShell, grant admissionGrant) startCommittedResult {
	prepared := shell.startCommitted(grant, startInstallation{grant: grant, cell: &pendingStartCell{}})
	if prepared.result.decision != startCommittedAccepted {
		return prepared.result
	}
	observed := prepared.start.launch(func(_ attemptGeneration) attemptObservation { return launchOwned{} })
	shell.observeAttempt(prepared.result.generation, observed)

	return prepared.result
}
