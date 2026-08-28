package campaign

// CutReservation orders one accepted campaign transition.
type CutReservation uint64

// Recorder records accepted campaign owner cuts and causal effect execution.
type Recorder interface {
	Enter() func()
	Reserve() CutReservation
	Publish(CutReservation, Event, Projection, []Effect)
	BeginEffect(Effect) func()
}

func enterRecorder(recorder Recorder) func() {
	if recorder == nil {
		return func() {}
	}
	return recorder.Enter()
}

func reserveRecorder(recorder Recorder) CutReservation {
	if recorder == nil {
		return 0
	}
	return recorder.Reserve()
}

func publishRecorder(recorder Recorder, reservation CutReservation, transition Transition) {
	if recorder == nil {
		return
	}
	effects := make([]Effect, len(transition.effects))
	for index, effect := range transition.effects {
		effects[index] = Effect{value: effect}
	}
	recorder.Publish(reservation, transition.event, transition.projection, effects)
}

func beginRecordedEffect(recorder Recorder, effect campaignEffect) func() {
	if recorder == nil {
		return func() {}
	}
	return recorder.BeginEffect(Effect{value: effect})
}
