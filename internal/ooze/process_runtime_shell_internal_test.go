//nolint:cyclop,exhaustruct // Race tables deliberately construct zero-state private values.
package ooze

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

func (s *processRuntimeShell) snapshot() processRuntime {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.core.clone()
}

func TestProcessRuntimeShellCompletesSealedConfirmationQueue(t *testing.T) {
	core := runtimeAtBoundConfirmation(t)
	barrierAt := core.grantedConfirmationIndex()
	grant := core.admissions[barrierAt].grant
	core, started := core.startCommitted(grant)
	core, _ = core.observeAttempt(started.generation, launchOwned{})
	core, _ = core.observeAttempt(started.generation, automaticDeadlineTrip())
	shell := &processRuntimeShell{core: core, emergency: make(chan struct{})}

	result := shell.completeConfirmationQueue(grant.campaign)
	snapshot := shell.snapshot()
	if result.decision != confirmationQueueCompleted ||
		!snapshot.campaigns[snapshot.campaignIndex(grant.campaign)].primaryGateOpen {
		t.Fatalf("queue completion/state = %#v/%#v", result, snapshot)
	}
}

func TestProcessRuntimeShellUsesBufferedOneShotGrantDelivery(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 11})

	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "a",
		class:    sharedAdmission,
	})
	if cap(requested.delivery) != 1 || len(requested.delivery) != 1 {
		t.Fatalf("delivery cap/len=%d/%d, want 1/1", cap(requested.delivery), len(requested.delivery))
	}
	grant, open := <-requested.delivery
	if !open || grant != requested.request {
		t.Fatalf("grant/open=%#v/%t", grant, open)
	}
	if _, open = <-requested.delivery; open {
		t.Fatal("one-shot delivery remained open after its grant")
	}
}

func TestProcessRuntimeShellFatalClosureClosesWaiterAndAcceptsLateGrantReturn(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 11})
	granted := shell.requestAdmission(admissionRequest{campaign: campaign.token, attempt: "a", class: sharedAdmission})
	waiting := shell.requestAdmission(admissionRequest{campaign: campaign.token, attempt: "b", class: sharedAdmission})
	grant := <-granted.delivery

	closed := shell.closeRuntime(runtimeFatalCause("fatal test"))
	if shell.snapshot().lifecycle != runtimeFatalClosing {
		t.Fatalf("closure=%#v", closed)
	}
	if _, open := <-waiting.delivery; open {
		t.Fatal("fatal closure left a waiting delivery open")
	}
	returned := shell.acknowledgeGrantReturn(grant)
	if returned.decision != admissionReturnedAfterClosure {
		t.Fatalf("late grant return=%#v", returned)
	}
	requested := shell.requestAdmission(admissionRequest{campaign: campaign.token, attempt: "c", class: sharedAdmission})
	if requested.decision != admissionRejectedClosed {
		t.Fatalf("post-close request=%#v", requested)
	}
	if _, open := <-requested.delivery; open {
		t.Fatal("rejected post-close request left a delivery open")
	}
}

func TestProcessRuntimeShellBroadcastsEmergencyOnceToEveryCampaign(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	emergency := shell.runtimeEmergency()
	if emergency != shell.runtimeEmergency() {
		t.Fatal("runtime exposed more than one emergency broadcast")
	}
	select {
	case <-emergency:
		t.Fatal("emergency broadcast closed while runtime was open")
	default:
	}
	shell.registerCampaign(campaignProvenance{lineage: 11}) // No waiter to close for this campaign.
	campaignB := shell.registerCampaign(campaignProvenance{lineage: 22})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaignB.token, attempt: "fatal seed", class: sharedAdmission,
	})
	started := startOwned(shell, <-requested.delivery)
	shell.observeAttempt(started.generation, drainUnconfirmed{})
	select {
	case <-emergency:
	default:
		t.Fatal("runtime emergency did not reach a campaign without a waiter")
	}
	shell.closeRuntime(runtimeFatalCause("joined fatal cause"))
	select {
	case <-emergency:
	default:
		t.Fatal("joined fatal cause replaced the closed broadcast")
	}
}

func TestProcessRuntimeShellFatalClosureDoesNotEmitUnboundBarrierDelivery(t *testing.T) {
	shell := newProcessRuntimeShell(2)
	campaignA := shell.registerCampaign(campaignProvenance{lineage: 11})
	campaignB := shell.registerCampaign(campaignProvenance{lineage: 22})
	requestA := shell.requestAdmission(admissionRequest{campaign: campaignA.token, attempt: "a", class: sharedAdmission})
	requestB := shell.requestAdmission(admissionRequest{campaign: campaignB.token, attempt: "b", class: sharedAdmission})
	startedA := startOwned(shell, <-requestA.delivery)
	_ = startOwned(shell, <-requestB.delivery)
	shell.observeAttempt(startedA.generation, automaticDeadlineTrip())
	if shell.snapshot().unboundBarrierIndex(campaignA.token) < 0 {
		t.Fatalf("setup has no unbound barrier: %#v", shell.snapshot())
	}
	closed := shell.closeRuntime(runtimeFatalCause("fatal before barrier binding"))
	if len(closed.cancelledWaiting) != 0 || shell.snapshot().lifecycle != runtimeFatalClosing {
		t.Fatalf("unbound barrier produced delivery action: %#v/%#v", closed, shell.snapshot())
	}
}

func TestProcessRuntimeShellBindsBarrierToBufferedOneShot(t *testing.T) {
	shell := newProcessRuntimeShell(2)
	campaignA := shell.registerCampaign(campaignProvenance{lineage: 11})
	campaignB := shell.registerCampaign(campaignProvenance{lineage: 22})
	requestA := shell.requestAdmission(admissionRequest{campaign: campaignA.token, attempt: "a", class: sharedAdmission})
	requestB := shell.requestAdmission(admissionRequest{campaign: campaignB.token, attempt: "b", class: sharedAdmission})
	startedA := startOwned(shell, <-requestA.delivery)
	startedB := startOwned(shell, <-requestB.delivery)
	shell.observeAttempt(startedA.generation, automaticDeadlineTrip())
	shell.observeAttempt(startedB.generation, attemptSettled{})

	confirmation := shell.sealAndBindConfirmationBarrier(barrierBinding{
		campaign: campaignA.token,
		attempt:  confirmationAttempt,
		profile:  AutomaticProfile, deadline: 31 * time.Second,
	})
	if confirmation.decision != barrierBound || cap(confirmation.delivery) != 1 || len(confirmation.delivery) != 1 {
		t.Fatalf("confirmation await=%#v cap/len=%d/%d", confirmation, cap(confirmation.delivery), len(confirmation.delivery))
	}
	grant, open := <-confirmation.delivery
	if !open || grant != confirmation.request || grant.class != confirmationBarrierAdmission {
		t.Fatalf("confirmation grant/open=%#v/%t", grant, open)
	}
	if _, open = <-confirmation.delivery; open {
		t.Fatal("confirmation delivery remained open")
	}
}

func TestProcessRuntimeShellExposesTerminalAndEmergencyAcknowledgements(t *testing.T) {
	t.Run("terminal", func(t *testing.T) {
		shell := newProcessRuntimeShell(1)
		campaign := shell.registerCampaign(campaignProvenance{lineage: 11})
		result := shell.commitTerminal(campaign.token)
		if result.decision != terminalCommitted {
			t.Fatalf("terminal=%#v", result)
		}
	})

	t.Run("emergency", func(t *testing.T) {
		shell := newProcessRuntimeShell(1)
		campaign := shell.registerCampaign(campaignProvenance{lineage: 11})
		requested := shell.requestAdmission(admissionRequest{campaign: campaign.token, attempt: "a", class: sharedAdmission})
		started := startOwned(shell, <-requested.delivery)
		shell.closeRuntime(runtimeFatalCause("fatal test"))
		result := shell.settleEmergency(emergencySweep{
			resolutions: []emergencyResolution{{
				generation:  started.generation,
				disposition: emergencyConfirmedDrained,
			}},
		})
		if !reflect.DeepEqual(result.acknowledged, []attemptGeneration{started.generation}) {
			t.Fatalf("emergency settlement=%#v", result)
		}
	})
}

func TestProcessRuntimeShellFatalCloseProgressesWhileNativeLaunchIsBlocked(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 11})
	requested := shell.requestAdmission(admissionRequest{campaign: campaign.token, attempt: "a", class: sharedAdmission})
	grant := <-requested.delivery
	launchEntered := make(chan struct{})
	allowLaunch := make(chan struct{})
	started := make(chan startCommittedResult, 1)
	cell := pendingStartCell{}
	prepared := shell.startCommitted(grant, startInstallation{grant: grant, cell: &cell})

	go func() {
		observed := prepared.start.launch(func(_ attemptGeneration) attemptObservation {
			close(launchEntered)
			<-allowLaunch

			return launchOwned{}
		})
		settled := shell.observeAttempt(prepared.result.generation, observed)
		result := prepared.result
		result.runtimeClosureInProgress = settled.runtimeClosureInProgress
		started <- result
	}()
	<-launchEntered
	installed := cell.installedGeneration()
	closed := shell.closeRuntime(runtimeFatalCause("launch race"))
	close(allowLaunch)
	result := <-started

	if installed == 0 || installed != result.generation || shell.snapshot().lifecycle != runtimeFatalClosing ||
		result.decision != startCommittedAccepted ||
		!result.runtimeClosureInProgress {
		t.Fatalf("cell/close/start=%d/%#v/%#v", installed, closed, result)
	}
	want := []residualCustody{{
		generation: result.generation, attempt: grant.attempt, stage: admissionOwned, transferred: false,
	}}
	if got := shell.snapshot().residualCustody(); !reflect.DeepEqual(got, want) {
		t.Fatalf("late-owned residual=%#v, want %#v", got, want)
	}
}

func TestStartInstallationKeepsLaunchDormantUntilInstalledStart(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	cell := pendingStartCell{}
	launchCalls := 0
	installation := startInstallation{cell: &cell}
	//nolint:unparam // A fixed observation keeps this thunk useful only as a launch counter.
	dormant := func(_ attemptGeneration) attemptObservation {
		launchCalls++

		return launchOwned{}
	}
	if launchCalls != 0 || cell.installedGeneration() != 0 {
		t.Fatalf("construction launched or installed: calls/generation=%d/%d", launchCalls, cell.installedGeneration())
	}
	installed := installation.install(7, shell)
	if launchCalls != 0 || cell.installedGeneration() != 7 {
		t.Fatalf("installation launched or lost generation: calls/cell/start=%d/%d/%#v",
			launchCalls, cell.installedGeneration(), installed)
	}
	assertInvariantViolation(t, func() { (installedStart{}).launch(dormant) })
	nilInstalled := startInstallation{cell: &pendingStartCell{}}.install(8, newProcessRuntimeShell(1))
	assertInvariantViolation(t, func() { nilInstalled.launch(nil) })
	if launchCalls != 0 {
		t.Fatalf("zero or nil launch reached native work: calls=%d", launchCalls)
	}
	observed := installed.launch(dormant)
	if launchCalls != 1 {
		t.Fatalf("narrowed launch calls/observation=%d/%#v", launchCalls, observed)
	}
	assertInvariantViolation(t, func() { installed.launch(dormant) })
	if launchCalls != 1 {
		t.Fatalf("reused installed start reached native work: calls=%d", launchCalls)
	}
}

func TestStartInstallationRejectsCrossPairedGrantBeforeCellMutation(t *testing.T) {
	for _, reverse := range []bool{false, true} {
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
		if cellA.installedGeneration() != 0 || cellB.installedGeneration() != 0 ||
			snapshot.lifecycle != runtimeFatalClosing || len(snapshot.residualCustody()) != 0 ||
			indexA < 0 || indexB < 0 || snapshot.admissions[indexA].stage != admissionGranted ||
			snapshot.admissions[indexB].stage != admissionGranted ||
			snapshot.admissions[indexA].disposition != dispositionReturnedAfterClosure ||
			snapshot.admissions[indexB].disposition != dispositionReturnedAfterClosure {
			t.Fatalf("reverse=%t cross-pair mutated installation/custody: %#v", reverse, snapshot)
		}
		select {
		case <-emergency:
		default:
			t.Fatalf("reverse=%t cross-pair did not broadcast fatal closure", reverse)
		}
		if returned := shell.acknowledgeGrantReturn(grantA); returned.decision != admissionReturnedAfterClosure {
			t.Fatalf("reverse=%t grant A return=%#v", reverse, returned)
		}
		if returned := shell.acknowledgeGrantReturn(grantB); returned.decision != admissionReturnedAfterClosure {
			t.Fatalf("reverse=%t grant B return=%#v", reverse, returned)
		}
		if snapshot = shell.snapshot(); snapshot.lifecycle != runtimeFatalClosing || len(snapshot.admissions) != 0 {
			t.Fatalf("reverse=%t peer returns closed ahead of settlement: %#v", reverse, snapshot)
		}
		settled := shell.settleEmergency(emergencySweep{})
		if snapshot = shell.snapshot(); snapshot.lifecycle != runtimeClosedDrained ||
			len(settled.acknowledged) != 0 || len(settled.residual) != 0 {
			t.Fatalf("reverse=%t empty settlement did not finish closure: %#v/%#v", reverse, settled, snapshot)
		}
	}
}

func TestInstalledStartNativeFailureBroadcastsBeforePanicking(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(attemptGeneration) attemptObservation
	}{
		{name: "panic", run: func(attemptGeneration) attemptObservation { panic("native panic") }},
		{name: "nil observation", run: func(attemptGeneration) attemptObservation { return nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			shell := newProcessRuntimeShell(1)
			campaign := shell.registerCampaign(campaignProvenance{lineage: 11})
			requested := shell.requestAdmission(admissionRequest{
				campaign: campaign.token, attempt: "native failure", class: sharedAdmission,
			})
			grant := <-requested.delivery
			prepared := shell.startCommitted(grant, startInstallation{grant: grant, cell: &pendingStartCell{}})
			emergency := shell.runtimeEmergency()
			assertInvariantViolation(t, func() { prepared.start.launch(test.run) })
			select {
			case <-emergency:
			default:
				t.Fatal("native failure panicked before broadcasting emergency")
			}
			want := []residualCustody{{
				generation: prepared.result.generation, attempt: grant.attempt,
				stage: admissionProspective, transferred: true,
			}}
			snapshot := shell.snapshot()
			if snapshot.lifecycle != runtimeClosedUnconfirmed || !reflect.DeepEqual(snapshot.residualCustody(), want) {
				t.Fatalf("native failure custody=%#v, want %#v", snapshot, want)
			}
		})
	}
}

func TestInstalledStartFailuresRecordOneExactCausePerGeneration(t *testing.T) {
	shell := newProcessRuntimeShell(3)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 11})
	starts := make([]preparedStart, 0, 2)
	var pendingGrant admissionGrant
	for _, attempt := range []attemptIdentity{"a", "pending", "b"} {
		requested := shell.requestAdmission(admissionRequest{
			campaign: campaign.token, attempt: attempt, class: sharedAdmission,
		})
		grant := <-requested.delivery
		if attempt == "pending" {
			pendingGrant = grant

			continue
		}
		starts = append(starts, shell.startCommitted(grant, startInstallation{
			grant: grant, cell: &pendingStartCell{},
		}))
	}
	for _, start := range starts {
		assertInvariantViolation(t, func() {
			start.start.launch(func(attemptGeneration) attemptObservation { panic("native panic") })
		})
	}
	want := []runtimeFatalCause{
		runtimeFatalCause(fmt.Sprintf(
			"launch invariant: launch thunk panicked or returned nil generation=%d", starts[0].result.generation,
		)),
		runtimeFatalCause(fmt.Sprintf(
			"launch invariant: launch thunk panicked or returned nil generation=%d", starts[1].result.generation,
		)),
	}
	snapshot := shell.snapshot()
	if snapshot.lifecycle != runtimeFatalSettledClosing || !reflect.DeepEqual(snapshot.fatalCauses, want) {
		t.Fatalf("launch invariant causes=%#v, want %#v", snapshot, want)
	}
	if returned := shell.acknowledgeGrantReturn(pendingGrant); returned.decision != admissionReturnedAfterClosure {
		t.Fatalf("pending peer return=%#v", returned)
	}
	if snapshot = shell.snapshot(); snapshot.lifecycle != runtimeClosedUnconfirmed {
		t.Fatalf("pending return did not finalize invariant custody: %#v", snapshot)
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
	if launchCalls != 0 || snapshot.lifecycle != runtimeFatalSettledClosing ||
		!reflect.DeepEqual(snapshot.fatalCauses, want) {
		t.Fatalf("zeroed copy calls/state=%d/%#v, want causes %#v", launchCalls, snapshot, want)
	}
	if returned := shell.acknowledgeGrantReturn(pendingGrant); returned.decision != admissionReturnedAfterClosure {
		t.Fatalf("pending peer return=%#v", returned)
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
	if launchCalls != 0 || panics[0] == nil || panics[1] == nil {
		t.Fatalf("cross-pair calls/panics=%d/%#v", launchCalls, panics)
	}
	want := []residualCustody{
		{generation: preparedA.result.generation, attempt: grantA.attempt, stage: admissionProspective, transferred: true},
		{generation: preparedB.result.generation, attempt: grantB.attempt, stage: admissionProspective, transferred: true},
	}
	snapshot := shell.snapshot()
	if got := snapshot.residualCustody(); snapshot.lifecycle != runtimeClosedUnconfirmed || !reflect.DeepEqual(got, want) {
		t.Fatalf("cross-pair state/residual=%#v/%#v, want %#v", snapshot, got, want)
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
		if launchCalls != 1 || (panics[0] == nil) == (panics[1] == nil) {
			t.Fatalf("sample %d calls/panics=%d/%#v", sample, launchCalls, panics)
		}
		want := []residualCustody{{
			generation: prepared.result.generation, attempt: grant.attempt,
			stage: admissionProspective, transferred: true,
		}}
		snapshot := shell.snapshot()
		if snapshot.lifecycle != runtimeClosedUnconfirmed || !reflect.DeepEqual(snapshot.residualCustody(), want) {
			t.Fatalf("sample %d copied-start state=%#v", sample, snapshot)
		}
	}
}

func TestStartCommittedLockedBoundaryContainsNoExecutionCapability(t *testing.T) {
	type executableAlias = func()
	type nestedExecutable struct {
		next      *nestedExecutable
		callbacks map[string][]executableAlias
	}
	_ = nestedExecutable{next: nil, callbacks: nil}
	if path, found := executionCapabilityPath(reflect.TypeFor[nestedExecutable](), nil); !found {
		t.Fatalf("structural guard missed nested executable field at %s", path)
	}
	method := reflect.TypeOf((*processRuntimeShell).startCommitted)
	for parameter := range method.NumIn() {
		if path, found := executionCapabilityPath(method.In(parameter), nil); found {
			t.Fatalf("startCommitted input %d contains executable capability at %s", parameter, path)
		}
	}
	if path, found := executionCapabilityPath(reflect.TypeFor[startInstallation](), nil); found {
		t.Fatalf("startInstallation contains executable capability at %s", path)
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

func TestProcessRuntimeShellSerializesGateClosureAgainstStartCommit(t *testing.T) {
	const samples = 100
	for sample := range samples {
		shell, campaignA, grantA2, generationA1 := shellAtGateStartRace(t, campaignLineage(sample+1))
		begin := make(chan struct{})
		var wait sync.WaitGroup
		wait.Add(2)
		cell := pendingStartCell{}
		launchCalls := 0
		var prepared preparedStart
		go func() {
			defer wait.Done()
			<-begin
			prepared = shell.startCommitted(grantA2, startInstallation{grant: grantA2, cell: &cell})
			if prepared.result.decision == startCommittedAccepted {
				observed := prepared.start.launch(func(_ attemptGeneration) attemptObservation {
					launchCalls++

					return launchNotReleased{reason: launchFailed}
				})
				shell.observeAttempt(prepared.result.generation, observed)
			}
		}()
		go func() {
			defer wait.Done()
			<-begin
			shell.observeAttempt(generationA1, automaticDeadlineTrip())
		}()
		close(begin)
		wait.Wait()

		wantGeneration := attemptGeneration(0)
		if prepared.result.decision == startCommittedAccepted {
			wantGeneration = prepared.result.generation
		} else if prepared.result.decision != startCommittedRejectedGate &&
			prepared.result.decision != startCommittedRejectedGrant {
			t.Fatalf("sample %d: start decision=%v", sample, prepared.result.decision)
		}
		wantCalls := 0
		if wantGeneration != 0 {
			wantCalls = 1
		}
		if cell.installedGeneration() != wantGeneration || launchCalls != wantCalls {
			t.Fatalf("sample %d: cell/calls=%d/%d, want %d/%d", sample,
				cell.installedGeneration(), launchCalls, wantGeneration, wantCalls)
		}
		snapshot := shell.snapshot()
		campaignAt := snapshot.campaignIndex(campaignA)
		if campaignAt < 0 || snapshot.campaigns[campaignAt].primaryGateOpen || snapshot.unboundBarrierIndex(campaignA) < 0 {
			t.Fatalf("sample %d: non-atomic gate/barrier state=%#v", sample, snapshot)
		}
	}
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

	if first.result.decision != startCommittedAccepted || launchCalls != 1 ||
		secondShell.snapshot().lifecycle != runtimeFatalClosing ||
		len(secondShell.snapshot().residualCustody()) != 0 {
		t.Fatalf("first/calls/second=%#v/%d/%#v", first.result, launchCalls, secondShell.snapshot())
	}
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
			if snapshot.lifecycle != runtimeFatalClosing || len(snapshot.residualCustody()) != 0 {
				t.Fatalf("invalid installation published prospective custody: %#v", snapshot)
			}
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
	if first.decision != startCommittedAccepted || second.result.decision != startCommittedRejectedGrant ||
		cell.installedGeneration() != 0 || launchCalls != 0 {
		t.Fatalf("first/second/cell/calls=%#v/%#v/%d/%d", first, second.result,
			cell.installedGeneration(), launchCalls)
	}
}

func TestProcessRuntimeShellSerializesCancellationAgainstGrant(t *testing.T) {
	const samples = 100
	for sample := range samples {
		shell := newProcessRuntimeShell(1)
		campaign := shell.registerCampaign(campaignProvenance{lineage: campaignLineage(sample*2 + 1)})
		waitingCampaign := shell.registerCampaign(campaignProvenance{lineage: campaignLineage(sample*2 + 2)})
		active := shell.requestAdmission(admissionRequest{
			campaign: campaign.token, attempt: "active", class: sharedAdmission,
		})
		waiting := shell.requestAdmission(admissionRequest{
			campaign: waitingCampaign.token, attempt: "waiting", class: sharedAdmission,
		})
		activeGrant := <-active.delivery
		begin := make(chan struct{})
		var wait sync.WaitGroup
		wait.Add(2)
		var waitingCancellation admissionResult
		go func() {
			defer wait.Done()
			<-begin
			waitingCancellation = shell.cancelAdmission(waiting.request)
		}()
		go func() {
			defer wait.Done()
			<-begin
			shell.cancelAdmission(activeGrant)
		}()
		close(begin)
		wait.Wait()

		//nolint:exhaustive // The default arm attributes every impossible decision.
		switch waitingCancellation.decision {
		case admissionCancelledWaiting:
			if _, open := <-waiting.delivery; open {
				t.Fatalf("sample %d: cancelled waiter received a grant", sample)
			}
		case admissionCancelledGranted:
			grant, open := <-waiting.delivery
			if !open || grant != waiting.request {
				t.Fatalf("sample %d: late grant=%#v/%t", sample, grant, open)
			}
		default:
			t.Fatalf("sample %d: cancellation=%#v", sample, waitingCancellation)
		}
	}
}

func TestProcessRuntimeShellSerializesTerminalCommitAgainstFatalClose(t *testing.T) {
	const samples = 100
	for sample := range samples {
		shell := newProcessRuntimeShell(1)
		campaign := shell.registerCampaign(campaignProvenance{lineage: campaignLineage(sample + 1)})
		begin := make(chan struct{})
		var wait sync.WaitGroup
		wait.Add(2)
		var terminal terminalResult
		go func() {
			defer wait.Done()
			<-begin
			terminal = shell.commitTerminal(campaign.token)
		}()
		go func() {
			defer wait.Done()
			<-begin
			shell.closeRuntime(runtimeFatalCause("terminal race"))
		}()
		close(begin)
		wait.Wait()

		if terminal.decision != terminalCommitted && terminal.decision != terminalRejectedClosed {
			t.Fatalf("sample %d: terminal=%#v", sample, terminal)
		}
	}
}

func TestProcessRuntimeShellSerializesOwnedTerminalAgainstFatalClose(t *testing.T) {
	const samples = 100
	for sample := range samples {
		shell := newProcessRuntimeShell(1)
		campaign := shell.registerCampaign(campaignProvenance{lineage: campaignLineage(sample + 1)})
		requested := shell.requestAdmission(admissionRequest{
			campaign: campaign.token, attempt: "terminal race", class: sharedAdmission,
		})
		started := startOwned(shell, <-requested.delivery)
		begin := make(chan struct{})
		var wait sync.WaitGroup
		wait.Add(2)
		var result observationResult
		var terminalPanic any
		go func() {
			defer wait.Done()
			defer func() { terminalPanic = recover() }()
			<-begin
			result = shell.observeAttempt(started.generation, attemptSettled{})
		}()
		go func() {
			defer wait.Done()
			<-begin
			shell.closeRuntime(runtimeFatalCause("owned terminal race"))
		}()
		close(begin)
		wait.Wait()

		if terminalPanic != nil {
			t.Fatalf("sample %d: owned terminal panicked: %v", sample, terminalPanic)
		}
		if result.generation != started.generation || result.confirmationProvisional ||
			result.pressureTransitioned || len(result.deliveries) != 0 ||
			len(result.cancelledWaiting) != 0 || len(result.compensatedGrants) != 0 {
			t.Fatalf("sample %d: terminal receipt=%#v", sample, result)
		}
		snapshot := shell.snapshot()
		index := snapshot.admissionIndexByGeneration(started.generation)
		if result.settlementAcknowledged {
			if result.runtimeClosureInProgress || index >= 0 {
				t.Fatalf("sample %d: open terminal path=%#v/%#v", sample, result, snapshot)
			}
		} else if !result.runtimeClosureInProgress || index < 0 ||
			snapshot.admissions[index].disposition != dispositionTerminalDeferred {
			t.Fatalf("sample %d: deferred terminal path=%#v/%#v", sample, result, snapshot)
		}
	}
}

func TestProcessRuntimeShellSerializesFatalCloseAgainstStartCommit(t *testing.T) {
	const samples = 100
	for sample := range samples {
		shell := newProcessRuntimeShell(1)
		campaign := shell.registerCampaign(campaignProvenance{lineage: campaignLineage(sample + 1)})
		requested := shell.requestAdmission(admissionRequest{
			campaign: campaign.token, attempt: "racing-start", class: sharedAdmission,
		})
		grant := <-requested.delivery
		cell := pendingStartCell{}
		begin := make(chan struct{})
		var wait sync.WaitGroup
		wait.Add(2)
		var prepared preparedStart
		go func() {
			defer wait.Done()
			<-begin
			prepared = shell.startCommitted(grant, startInstallation{grant: grant, cell: &cell})
		}()
		go func() {
			defer wait.Done()
			<-begin
			shell.closeRuntime(runtimeFatalCause("start race"))
		}()
		close(begin)
		wait.Wait()

		residual := shell.snapshot().residualCustody()
		switch prepared.result.decision {
		case startCommittedAccepted:
			want := []residualCustody{{
				generation: prepared.result.generation, attempt: grant.attempt, stage: admissionProspective,
			}}
			if cell.installedGeneration() != prepared.result.generation || !reflect.DeepEqual(residual, want) {
				t.Fatalf("sample %d accepted cell/residual=%d/%#v, want %#v", sample,
					cell.installedGeneration(), residual, want)
			}
		case startCommittedRejectedClosed:
			if cell.installedGeneration() != 0 || len(residual) != 0 {
				t.Fatalf("sample %d rejected cell/residual=%d/%#v", sample, cell.installedGeneration(), residual)
			}
		case startCommittedRejectedGrant, startCommittedRejectedGate:
			t.Fatalf("sample %d start decision=%#v", sample, prepared.result)
		}
	}
}

func TestProcessRuntimeShellSerializesFatalCloseAgainstGrantDelivery(t *testing.T) {
	const samples = 100
	for sample := range samples {
		shell := newProcessRuntimeShell(1)
		campaign := shell.registerCampaign(campaignProvenance{lineage: campaignLineage(sample*2 + 1)})
		waitingCampaign := shell.registerCampaign(campaignProvenance{lineage: campaignLineage(sample*2 + 2)})
		active := shell.requestAdmission(admissionRequest{
			campaign: campaign.token, attempt: "active", class: sharedAdmission,
		})
		waiting := shell.requestAdmission(admissionRequest{
			campaign: waitingCampaign.token, attempt: "waiting", class: sharedAdmission,
		})
		begin := make(chan struct{})
		var wait sync.WaitGroup
		wait.Add(2)
		go func() {
			defer wait.Done()
			<-begin
			shell.cancelAdmission(<-active.delivery)
		}()
		go func() {
			defer wait.Done()
			<-begin
			shell.closeRuntime(runtimeFatalCause("grant race"))
		}()
		close(begin)
		wait.Wait()

		if grant, open := <-waiting.delivery; open {
			returned := shell.acknowledgeGrantReturn(grant)
			if returned.decision != admissionReturnedAfterClosure {
				t.Fatalf("sample %d late grant return=%#v", sample, returned)
			}
		}
	}
}

func TestProcessRuntimeShellSerializesGrantReturnAgainstEmergencySettlement(t *testing.T) {
	const samples = 100
	for sample := range samples {
		shell := newProcessRuntimeShell(2)
		campaignA := shell.registerCampaign(campaignProvenance{lineage: campaignLineage(sample*2 + 1)})
		campaignB := shell.registerCampaign(campaignProvenance{lineage: campaignLineage(sample*2 + 2)})
		obligation := shell.requestAdmission(admissionRequest{
			campaign: campaignA.token, attempt: "owned", class: sharedAdmission,
		})
		granted := shell.requestAdmission(admissionRequest{
			campaign: campaignB.token, attempt: "granted", class: sharedAdmission,
		})
		started := startOwned(shell, <-obligation.delivery)
		grant := <-granted.delivery
		shell.closeRuntime(runtimeFatalCause("return versus emergency"))
		begin := make(chan struct{})
		var returned admissionResult
		var wait sync.WaitGroup
		wait.Go(func() {
			<-begin
			returned = shell.acknowledgeGrantReturn(grant)
		})
		wait.Go(func() {
			<-begin
			shell.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
				generation: started.generation, disposition: emergencyCustodyTransferred,
			}}})
		})
		close(begin)
		wait.Wait()
		snapshot := shell.snapshot()
		want := []residualCustody{{
			generation: started.generation, attempt: "owned", stage: admissionOwned, transferred: true,
		}}
		if returned.decision != admissionReturnedAfterClosure ||
			snapshot.lifecycle != runtimeClosedUnconfirmed || !reflect.DeepEqual(snapshot.residualCustody(), want) {
			t.Fatalf("sample %d return/final state=%#v/%#v", sample, returned, snapshot)
		}
	}
}

func TestProcessRuntimeShellSerializesGrantReturnAgainstExactEmptySettlement(t *testing.T) {
	const samples = 100
	for sample := range samples {
		shell := newProcessRuntimeShell(2)
		campaignA := shell.registerCampaign(campaignProvenance{lineage: campaignLineage(sample*2 + 101)})
		campaignB := shell.registerCampaign(campaignProvenance{lineage: campaignLineage(sample*2 + 102)})
		prospective := shell.requestAdmission(admissionRequest{
			campaign: campaignA.token, attempt: "prospective", class: sharedAdmission,
		})
		granted := shell.requestAdmission(admissionRequest{
			campaign: campaignB.token, attempt: "granted", class: sharedAdmission,
		})
		grant := <-granted.delivery
		prepared := shell.startCommitted(
			<-prospective.delivery,
			startInstallation{grant: prospective.request, cell: &pendingStartCell{}},
		)
		if prepared.result.decision != startCommittedAccepted {
			t.Fatalf("sample %d prospective start=%#v", sample, prepared.result)
		}
		shell.closeRuntime(runtimeFatalCause("empty settlement race"))
		noRelease := shell.observeAttempt(
			prepared.result.generation,
			launchNotReleased{reason: launchFailed},
		)
		if !noRelease.settlementAcknowledged || !noRelease.runtimeClosureInProgress ||
			shell.snapshot().lifecycle != runtimeFatalClosing {
			t.Fatalf("sample %d no-release state=%#v/%#v", sample, noRelease, shell.snapshot())
		}

		begin := make(chan struct{})
		var returned admissionResult
		var settled emergencySettlement
		var returnPanic, settlementPanic any
		var wait sync.WaitGroup
		wait.Go(func() {
			defer func() { returnPanic = recover() }()
			<-begin
			returned = shell.acknowledgeGrantReturn(grant)
		})
		wait.Go(func() {
			defer func() { settlementPanic = recover() }()
			<-begin
			settled = shell.settleEmergency(emergencySweep{})
		})
		close(begin)
		wait.Wait()
		snapshot := shell.snapshot()
		if returnPanic != nil || settlementPanic != nil ||
			returned.decision != admissionReturnedAfterClosure ||
			len(settled.acknowledged) != 0 || len(settled.residual) != 0 ||
			snapshot.lifecycle != runtimeClosedDrained || len(snapshot.admissions) != 0 ||
			len(snapshot.residualCustody()) != 0 {
			t.Fatalf(
				"sample %d return/settlement/panics/state=%#v/%#v/%#v/%#v/%#v",
				sample, returned, settled, returnPanic, settlementPanic, snapshot,
			)
		}
	}
}

func TestProcessRuntimeShellDoesNotExposeDeliveredGrantTwice(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 11})
	waitingCampaign := shell.registerCampaign(campaignProvenance{lineage: 22})
	active := shell.requestAdmission(admissionRequest{campaign: campaign.token, attempt: "active", class: sharedAdmission})
	waiting := shell.requestAdmission(admissionRequest{
		campaign: waitingCampaign.token, attempt: "waiting", class: sharedAdmission,
	})
	result := shell.cancelAdmission(<-active.delivery)
	grant := <-waiting.delivery
	if len(result.deliveries) != 0 || grant != waiting.request {
		t.Fatalf("shell result/channel duplicated or lost delivery: %#v/%#v", result, grant)
	}
}

func shellAtGateStartRace(
	t *testing.T,
	lineage campaignLineage,
) (*processRuntimeShell, campaignToken, admissionGrant, attemptGeneration) {
	t.Helper()
	shell := newProcessRuntimeShell(3)
	campaignA := shell.registerCampaign(campaignProvenance{lineage: lineage*10 + 1})
	campaignB := shell.registerCampaign(campaignProvenance{lineage: lineage*10 + 2})
	a1 := shell.requestAdmission(admissionRequest{campaign: campaignA.token, attempt: "a1", class: sharedAdmission})
	a2 := shell.requestAdmission(admissionRequest{campaign: campaignA.token, attempt: "a2", class: sharedAdmission})
	b1 := shell.requestAdmission(admissionRequest{campaign: campaignB.token, attempt: "b1", class: sharedAdmission})
	grantA1 := <-a1.delivery
	grantA2 := <-a2.delivery
	grantB1 := <-b1.delivery
	startedA1 := startOwned(shell, grantA1)
	_ = startOwned(shell, grantB1)

	return shell, campaignA.token, grantA2, startedA1.generation
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
