package campaign

// CutReservation orders one accepted campaign transition.
type CutReservation uint64

// Observer records accepted campaign owner cuts and causal effect execution.
type Observer interface {
	Enter() func()
	Reserve() CutReservation
	Publish(CutReservation, Event, Projection, []Effect)
	BeginEffect(Effect) func()
}

func enterObserver(observer Observer) func() {
	if observer == nil {
		return func() {}
	}
	return observer.Enter()
}

func reserveObserver(observer Observer) CutReservation {
	if observer == nil {
		return 0
	}
	return observer.Reserve()
}

func publishObserver(observer Observer, reservation CutReservation, transition Transition) {
	if observer == nil {
		return
	}
	effects := make([]Effect, len(transition.effects))
	for index, effect := range transition.effects {
		effects[index] = Effect{value: effect}
	}
	observer.Publish(reservation, transition.event, transition.projection, effects)
}

func beginObservedEffect(observer Observer, effect campaignEffect) func() {
	if observer == nil {
		return func() {}
	}
	return observer.BeginEffect(Effect{value: effect})
}
