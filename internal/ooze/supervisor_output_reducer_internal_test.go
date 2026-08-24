package ooze

import (
	"reflect"
	"testing"
	"time"
)

func TestSupervisorReducerOutputEvidenceCompletenessAndFinalityAreIndependent(t *testing.T) {
	for _, test := range []struct {
		name         string
		provenEmpty  bool
		cutoff       uint64
		prefixLength uint64
		diagnostic   supervisorDiagnosticRef
		wantComplete bool
		wantFinal    bool
	}{
		{
			name: "drained empty success", provenEmpty: true,
			wantComplete: true, wantFinal: true,
		},
		{
			name: "drained complete", provenEmpty: true, cutoff: 12, prefixLength: 12,
			wantComplete: true, wantFinal: true,
		},
		{
			name: "drained partial failure", provenEmpty: true, cutoff: 12, prefixLength: 7,
			diagnostic: 901, wantFinal: true,
		},
		{
			name: "unconfirmed complete", cutoff: 12, prefixLength: 12,
			wantComplete: true,
		},
		{
			name: "unconfirmed partial failure", cutoff: 12, prefixLength: 7,
			diagnostic: 902,
		},
		{
			name: "diagnostic at full cutoff remains incomplete", cutoff: 12, prefixLength: 12,
			diagnostic: 904,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, capturing, capture := newOutputReducerFixture(t, test.provenEmpty)
			at := capture.at.Add(time.Nanosecond)
			next, actions := completeReducerOutput(
				t, capturing, fixture.generation, capture, at,
				11, test.cutoff, test.prefixLength, test.diagnostic,
			)
			assertSupervisorActions(t, actions, supervisorSealStopAdmission)
			attempt := supervisorAttemptByGeneration(t, next, fixture.generation)
			wantEvidence := supervisorOutputEvidence{
				ref:                   11,
				cutoff:                test.cutoff,
				prefixLength:          test.prefixLength,
				completeThroughCutoff: test.wantComplete,
				final:                 test.wantFinal,
				diagnostic:            test.diagnostic,
			}
			if attempt.phase != supervisorSealingStopAdmission ||
				!reflect.DeepEqual(attempt.output, wantEvidence) ||
				attempt.pendingAction != (supervisorPendingAction{kind: actions[0].kind, token: actions[0].token}) ||
				!actions[0].drainBy.Equal(fixture.drainBy) {
				t.Fatalf("output evidence/seal = %#v actions=%#v, want %#v", attempt, actions, wantEvidence)
			}
			if actions[0].kind == supervisorReleaseDomain || actions[0].kind == supervisorTransferResidualCustody {
				t.Fatalf("output completion bypassed stop-admission seal: %#v", actions)
			}
		})
	}
}

func TestSupervisorReducerOutputRejectsMalformedEvidenceAndCorrelation(t *testing.T) {
	fixture, capturing, capture := newOutputReducerFixture(t, true)
	validAt := capture.at.Add(time.Nanosecond)
	valid := supervisorOutputCompletion{
		generation:   fixture.generation,
		action:       supervisorPendingAction{kind: capture.kind, token: capture.token},
		at:           validAt,
		ref:          21,
		cutoff:       10,
		prefixLength: 10,
	}
	for _, malformed := range []struct {
		name   string
		mutate func(*supervisorOutputCompletion)
	}{
		{name: "zero output ref", mutate: func(completion *supervisorOutputCompletion) { completion.ref = 0 }},
		{name: "prefix exceeds cutoff", mutate: func(completion *supervisorOutputCompletion) {
			completion.prefixLength = completion.cutoff + 1
		}},
		{name: "short prefix without diagnostic", mutate: func(completion *supervisorOutputCompletion) {
			completion.prefixLength--
		}},
		{name: "wrong generation", mutate: func(completion *supervisorOutputCompletion) { completion.generation++ }},
		{name: "wrong token", mutate: func(completion *supervisorOutputCompletion) { completion.action.token++ }},
		{name: "wrong action kind", mutate: func(completion *supervisorOutputCompletion) {
			completion.action.kind = supervisorObserveEmptiness
		}},
		{name: "backward instant", mutate: func(completion *supervisorOutputCompletion) {
			completion.at = capture.at.Add(-time.Nanosecond)
		}},
	} {
		t.Run(malformed.name, func(t *testing.T) {
			completion := valid
			malformed.mutate(&completion)
			assertSupervisorInvariant(t, func() {
				reduceSupervisor(capturing, supervisorEvent{
					kind: supervisorOutputCompleted, generation: fixture.generation,
					at: completion.at, output: &completion,
				})
			})
		})
	}

	completed, _ := reduceSupervisor(capturing, supervisorEvent{
		kind: supervisorOutputCompleted, generation: fixture.generation,
		at: valid.at, output: &valid,
	})
	assertSupervisorInvariant(t, func() {
		reduceSupervisor(completed, supervisorEvent{
			kind: supervisorOutputCompleted, generation: fixture.generation,
			at: valid.at, output: &valid,
		})
	})
}

func TestSupervisorReducerStopSealBranchesOnlyAfterCorrelatedCompletion(t *testing.T) {
	for _, test := range []struct {
		name        string
		provenEmpty bool
		wantAction  supervisorActionKind
		wantPhase   supervisorAttemptPhase
	}{
		{
			name: "proven empty releases domain", provenEmpty: true,
			wantAction: supervisorReleaseDomain, wantPhase: supervisorReleasingDomain,
		},
		{
			name:       "unconfirmed transfers residual custody",
			wantAction: supervisorTransferResidualCustody, wantPhase: supervisorTransferringResidualCustody,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, capturing, capture := newOutputReducerFixture(t, test.provenEmpty)
			withOutput, sealActions := completeReducerOutput(
				t, capturing, fixture.generation, capture, capture.at.Add(time.Nanosecond),
				31, 8, 5, 903,
			)
			assertSupervisorActions(t, sealActions, supervisorSealStopAdmission)
			before := supervisorAttemptByGeneration(t, withOutput, fixture.generation)
			if before.pendingAction.kind != supervisorSealStopAdmission {
				t.Fatalf("output did not stop at seal: %#v", before)
			}

			at := sealActions[0].at.Add(time.Nanosecond)
			next, actions := completeReducerStopSeal(
				t, withOutput, fixture.generation, sealActions[0], at,
			)
			assertSupervisorActions(t, actions, test.wantAction)
			after := supervisorAttemptByGeneration(t, next, fixture.generation)
			if after.phase != test.wantPhase || !reflect.DeepEqual(after.output, before.output) ||
				after.pendingAction != (supervisorPendingAction{kind: actions[0].kind, token: actions[0].token}) ||
				!actions[0].drainBy.Equal(fixture.drainBy) {
				t.Fatalf("seal branch = %#v actions=%#v", after, actions)
			}
		})
	}
}

func TestSupervisorReducerStopSealRequiresExactCorrelationAndCannotRewriteOutput(t *testing.T) {
	fixture, capturing, capture := newOutputReducerFixture(t, true)
	withOutput, sealActions := completeReducerOutput(
		t, capturing, fixture.generation, capture, capture.at.Add(time.Nanosecond),
		41, 8, 8, 0,
	)
	before := supervisorAttemptByGeneration(t, withOutput, fixture.generation).output
	validAt := sealActions[0].at.Add(time.Nanosecond)
	valid := supervisorStopSealCompletion{
		generation: fixture.generation,
		action:     supervisorPendingAction{kind: sealActions[0].kind, token: sealActions[0].token},
		at:         validAt,
	}
	for _, malformed := range []struct {
		name   string
		mutate func(*supervisorStopSealCompletion)
	}{
		{name: "wrong generation", mutate: func(completion *supervisorStopSealCompletion) { completion.generation++ }},
		{name: "wrong token", mutate: func(completion *supervisorStopSealCompletion) { completion.action.token++ }},
		{name: "wrong action kind", mutate: func(completion *supervisorStopSealCompletion) {
			completion.action.kind = supervisorCaptureOutput
		}},
		{name: "backward instant", mutate: func(completion *supervisorStopSealCompletion) {
			completion.at = sealActions[0].at.Add(-time.Nanosecond)
		}},
	} {
		t.Run(malformed.name, func(t *testing.T) {
			completion := valid
			malformed.mutate(&completion)
			assertSupervisorInvariant(t, func() {
				reduceSupervisor(withOutput, supervisorEvent{
					kind: supervisorStopAdmissionSealed, generation: fixture.generation,
					at: completion.at, seal: &completion,
				})
			})
		})
	}

	next, _ := reduceSupervisor(withOutput, supervisorEvent{
		kind: supervisorStopAdmissionSealed, generation: fixture.generation,
		at: valid.at, seal: &valid,
	})
	if got := supervisorAttemptByGeneration(t, next, fixture.generation).output; !reflect.DeepEqual(got, before) {
		t.Fatalf("seal rewrote immutable output evidence: before=%#v after=%#v", before, got)
	}
	assertSupervisorInvariant(t, func() {
		reduceSupervisor(next, supervisorEvent{
			kind: supervisorStopAdmissionSealed, generation: fixture.generation,
			at: valid.at, seal: &valid,
		})
	})
}

func TestSupervisorReducerOutputPipelineRejectsImpossibleFinalityAndBranchOwnership(t *testing.T) {
	for _, provenEmpty := range []bool{true, false} {
		name := "unconfirmed"
		if provenEmpty {
			name = "proven empty"
		}
		t.Run("seal finality mismatch "+name, func(t *testing.T) {
			fixture, capturing, capture := newOutputReducerFixture(t, provenEmpty)
			withOutput, seal := completeReducerOutput(
				t, capturing, fixture.generation, capture, capture.at.Add(time.Nanosecond),
				45, 2, 2, 0,
			)
			withOutput.attempts[0].output.final = !withOutput.attempts[0].output.final
			assertSupervisorInvariant(t, func() {
				completeReducerStopSeal(
					t, withOutput, fixture.generation, seal[0], seal[0].at.Add(time.Nanosecond),
				)
			})
		})
	}

	for _, test := range []struct {
		name   string
		mutate func(*supervisorAttemptState)
	}{
		{
			name: "emergency seal finality mismatch",
			mutate: func(attempt *supervisorAttemptState) {
				attempt.output.final = !attempt.output.final
			},
		},
		{
			name: "release phase with unconfirmed decision",
			mutate: func(attempt *supervisorAttemptState) {
				attempt.phase = supervisorReleasingDomain
				attempt.pendingAction.kind = supervisorReleaseDomain
				attempt.drain.decision = supervisorDrainUnconfirmed
				attempt.output.final = false
			},
		},
		{
			name: "transfer phase with proven empty decision",
			mutate: func(attempt *supervisorAttemptState) {
				attempt.phase = supervisorTransferringResidualCustody
				attempt.pendingAction.kind = supervisorTransferResidualCustody
				attempt.drain.decision = supervisorDrainProvenEmpty
				attempt.output.final = true
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, capturing, capture := newOutputReducerFixture(t, true)
			state, pending := completeReducerOutput(
				t, capturing, fixture.generation, capture, capture.at.Add(time.Nanosecond),
				46, 2, 2, 0,
			)
			test.mutate(&state.attempts[0])
			assertSupervisorInvariant(t, func() {
				reduceSupervisor(state, supervisorEvent{
					kind:               supervisorEmergencyStarted,
					at:                 pending[0].at.Add(time.Nanosecond),
					drainBy:            pending[0].at.Add(time.Second),
					emergencySnapshots: []supervisorEmergencySnapshot{{generation: fixture.generation}},
				})
			})
		})
	}
}

func TestSupervisorReducerOutputPipelineEmergencyCompositionPreservesInflightState(t *testing.T) {
	for _, stage := range []struct {
		name  string
		build func(*testing.T) (drainReducerFixture, supervisorState, supervisorAction)
	}{
		{
			name: "capture output",
			build: func(t *testing.T) (drainReducerFixture, supervisorState, supervisorAction) {
				return newOutputReducerFixture(t, true)
			},
		},
		{
			name: "seal stop admission",
			build: func(t *testing.T) (drainReducerFixture, supervisorState, supervisorAction) {
				fixture, capturing, capture := newOutputReducerFixture(t, true)
				state, actions := completeReducerOutput(
					t, capturing, fixture.generation, capture, capture.at.Add(time.Nanosecond),
					51, 3, 3, 0,
				)

				return fixture, state, actions[0]
			},
		},
		{
			name: "release domain",
			build: func(t *testing.T) (drainReducerFixture, supervisorState, supervisorAction) {
				return outputPipelineAfterSeal(t, true)
			},
		},
		{
			name: "transfer residual",
			build: func(t *testing.T) (drainReducerFixture, supervisorState, supervisorAction) {
				return outputPipelineAfterSeal(t, false)
			},
		},
	} {
		t.Run(stage.name, func(t *testing.T) {
			fixture, state, pending := stage.build(t)
			before := supervisorAttemptByGeneration(t, state, fixture.generation)
			emergencyAt := pending.at.Add(time.Nanosecond)
			emergencyDrainBy := emergencyAt.Add(time.Second)
			next, actions := reduceSupervisor(state, supervisorEvent{
				kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: emergencyDrainBy,
				emergencySnapshots: []supervisorEmergencySnapshot{{generation: fixture.generation}},
			})
			if len(actions) != 0 {
				t.Fatalf("pipeline emergency emitted competing action: %#v", actions)
			}
			after := supervisorAttemptByGeneration(t, next, fixture.generation)
			wantBound := before.drain.effectiveDrainBy
			if emergencyDrainBy.Before(wantBound) {
				wantBound = emergencyDrainBy
			}
			if after.phase != before.phase || after.pendingAction != before.pendingAction ||
				after.pendingAction != (supervisorPendingAction{kind: pending.kind, token: pending.token}) ||
				!reflect.DeepEqual(after.output, before.output) ||
				after.drain.decision != before.drain.decision ||
				!after.drain.effectiveDrainBy.Equal(wantBound) ||
				!next.emergency.active || !next.emergency.at.Equal(emergencyAt) ||
				!next.emergency.drainBy.Equal(emergencyDrainBy) {
				t.Fatalf("pipeline emergency changed custody: before=%#v after=%#v", before, after)
			}
		})
	}
}

func TestSupervisorReducerOutputPipelinePropagatesEmergencyClampAfterCompletion(t *testing.T) {
	t.Run("capture completion emits clamped seal", func(t *testing.T) {
		fixture, capturing, capture := newOutputReducerFixture(t, true)
		emergencyAt := capture.at.Add(time.Nanosecond)
		clamp := fixture.drainBy.Add(-time.Second)
		clamped, actions := reduceSupervisor(capturing, supervisorEvent{
			kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: clamp,
			emergencySnapshots: []supervisorEmergencySnapshot{{generation: fixture.generation}},
		})
		if len(actions) != 0 {
			t.Fatalf("capture clamp emitted competing action: %#v", actions)
		}
		next, seal := completeReducerOutput(
			t, clamped, fixture.generation, capture, emergencyAt.Add(time.Nanosecond),
			71, 5, 5, 0,
		)
		assertSupervisorActions(t, seal, supervisorSealStopAdmission)
		if !seal[0].drainBy.Equal(clamp) ||
			supervisorAttemptByGeneration(t, next, fixture.generation).pendingAction !=
				(supervisorPendingAction{kind: seal[0].kind, token: seal[0].token}) {
			t.Fatalf("post-clamp output completion lost bound: %#v", seal)
		}
	})

	for _, test := range []struct {
		name        string
		provenEmpty bool
		want        supervisorActionKind
	}{
		{name: "seal completion releases under clamp", provenEmpty: true, want: supervisorReleaseDomain},
		{name: "seal completion transfers under earlier local bound", want: supervisorTransferResidualCustody},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, capturing, capture := newOutputReducerFixture(t, test.provenEmpty)
			withOutput, seal := completeReducerOutput(
				t, capturing, fixture.generation, capture, capture.at.Add(time.Nanosecond),
				72, 5, 5, 0,
			)
			emergencyAt := seal[0].at.Add(time.Nanosecond)
			emergencyDrainBy := emergencyAt.Add(time.Second)
			clamped, actions := reduceSupervisor(withOutput, supervisorEvent{
				kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: emergencyDrainBy,
				emergencySnapshots: []supervisorEmergencySnapshot{{generation: fixture.generation}},
			})
			if len(actions) != 0 {
				t.Fatalf("seal clamp emitted competing action: %#v", actions)
			}
			wantBound := fixture.drainBy
			if emergencyDrainBy.Before(wantBound) {
				wantBound = emergencyDrainBy
			}
			_, branch := completeReducerStopSeal(
				t, clamped, fixture.generation, seal[0], emergencyAt.Add(time.Nanosecond),
			)
			assertSupervisorActions(t, branch, test.want)
			if !branch[0].drainBy.Equal(wantBound) {
				t.Fatalf("post-clamp seal completion lost effective bound: %#v", branch)
			}
		})
	}
}

func newOutputReducerFixture(
	t *testing.T,
	provenEmpty bool,
) (drainReducerFixture, supervisorState, supervisorAction) {
	t.Helper()
	if provenEmpty {
		fixture := newRootExitDrainReducerFixture(t)
		state, actions := fixture.complete(
			t, fixture.state, fixture.first, supervisorDrainObservedEmpty,
			fixture.first.at.Add(time.Nanosecond), 0,
		)
		assertSupervisorActions(t, actions, supervisorCaptureOutput)

		return fixture, state, actions[0]
	}
	fixture := newForcedDrainReducerFixture(t, supervisorIntentDeadline)
	state, actions := fixture.complete(
		t, fixture.state, fixture.first, supervisorDrainForceCompleted, fixture.drainBy, 0,
	)
	assertSupervisorActions(t, actions, supervisorCaptureOutput)

	return fixture, state, actions[0]
}

func completeReducerOutput(
	t *testing.T,
	state supervisorState,
	generation attemptGeneration,
	capture supervisorAction,
	at time.Time,
	ref supervisorOutputRef,
	cutoff uint64,
	prefixLength uint64,
	diagnostic supervisorDiagnosticRef,
) (supervisorState, []supervisorAction) {
	t.Helper()
	completion := supervisorOutputCompletion{
		generation:   generation,
		action:       supervisorPendingAction{kind: capture.kind, token: capture.token},
		at:           at,
		ref:          ref,
		cutoff:       cutoff,
		prefixLength: prefixLength,
		diagnostic:   diagnostic,
	}

	return reduceSupervisor(state, supervisorEvent{
		kind: supervisorOutputCompleted, generation: generation,
		at: at, output: &completion,
	})
}

func completeReducerStopSeal(
	t *testing.T,
	state supervisorState,
	generation attemptGeneration,
	seal supervisorAction,
	at time.Time,
) (supervisorState, []supervisorAction) {
	t.Helper()
	completion := supervisorStopSealCompletion{
		generation: generation,
		action:     supervisorPendingAction{kind: seal.kind, token: seal.token},
		at:         at,
	}

	return reduceSupervisor(state, supervisorEvent{
		kind: supervisorStopAdmissionSealed, generation: generation,
		at: at, seal: &completion,
	})
}

func outputPipelineAfterSeal(
	t *testing.T,
	provenEmpty bool,
) (drainReducerFixture, supervisorState, supervisorAction) {
	t.Helper()
	fixture, capturing, capture := newOutputReducerFixture(t, provenEmpty)
	withOutput, sealActions := completeReducerOutput(
		t, capturing, fixture.generation, capture, capture.at.Add(time.Nanosecond),
		61, 4, 4, 0,
	)
	state, actions := completeReducerStopSeal(
		t, withOutput, fixture.generation, sealActions[0], sealActions[0].at.Add(time.Nanosecond),
	)

	return fixture, state, actions[0]
}

func TestSupervisorReducerOutputDataRemainsCapabilityFree(t *testing.T) {
	for _, dataType := range []reflect.Type{
		reflect.TypeOf(supervisorOutputCompletion{}),
		reflect.TypeOf(supervisorStopSealCompletion{}),
		reflect.TypeOf(supervisorOutputEvidence{}),
		reflect.TypeOf(supervisorState{}),
		reflect.TypeOf(supervisorEvent{}),
		reflect.TypeOf(supervisorAction{}),
	} {
		t.Run(dataType.Name(), func(t *testing.T) {
			assertReducerDataOnly(t, dataType, make(map[reflect.Type]bool), dataType.Name())
		})
	}
}
