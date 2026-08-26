package ooze

import (
	"errors"
	"sync"

	internalooze "github.com/gtramontina/ooze/internal/ooze"
)

// CampaignEvent is an immutable fact published after an accepted campaign transition.
type CampaignEvent interface {
	campaignEvent()
}

// CampaignStarted reports that a campaign has entered the process runtime.
type CampaignStarted struct{}

func (CampaignStarted) campaignEvent() {}

// CatalogueDiscovered reports the fixed number of mutants in a campaign.
type CatalogueDiscovered struct {
	Total int
}

func (CatalogueDiscovered) campaignEvent() {}

// BaselineStarted reports that the unmutated attempt entered execution.
type BaselineStarted struct{}

func (BaselineStarted) campaignEvent() {}

// BaselineFinished reports whether the unmutated attempt passed.
type BaselineFinished struct {
	Passed bool
}

func (BaselineFinished) campaignEvent() {}

// Mutation identifies one mutant in the fixed campaign catalogue.
type Mutation struct {
	Label string
}

// MutationStarted reports that a primary or confirmation attempt entered execution.
type MutationStarted struct {
	Mutation     Mutation
	Confirmation bool
}

func (MutationStarted) campaignEvent() {}

// MutationFinished reports the attributable outcome of one mutant.
type MutationFinished struct {
	Mutation Mutation
	Outcome  MutationOutcome
}

func (MutationFinished) campaignEvent() {}

// CampaignCompleted reports that the campaign produced a mutation score.
type CampaignCompleted struct{}

func (CampaignCompleted) campaignEvent() {}

// ProgressObserver observes campaign events without controlling campaign policy.
type ProgressObserver interface {
	Observe(CampaignEvent) error
}

// ComposeObservers combines observers in declaration order.
func ComposeObservers(observers ...ProgressObserver) ProgressObserver {
	composed := make(multiObserver, 0, len(observers))
	for _, observer := range observers {
		if observer != nil {
			composed = append(composed, observer)
		}
	}

	return composed
}

type multiObserver []ProgressObserver

func (observers multiObserver) Observe(event CampaignEvent) error {
	failures := make([]error, 0, len(observers))
	for _, observer := range observers {
		if err := observer.Observe(event); err != nil {
			failures = append(failures, err)
		}
	}

	return errors.Join(failures...)
}

type observerDispatcher struct {
	observer ProgressObserver
	mutex    sync.Mutex
	wake     *sync.Cond
	queue    []CampaignEvent
	closed   bool
	done     chan struct{}
	failures []error
	panic    any
}

func newObserverDispatcher(observer ProgressObserver) *observerDispatcher {
	if observer == nil {
		return nil
	}
	dispatcher := &observerDispatcher{observer: observer, done: make(chan struct{})}
	dispatcher.wake = sync.NewCond(&dispatcher.mutex)
	go dispatcher.run()

	return dispatcher
}

func (dispatcher *observerDispatcher) publish(event CampaignEvent) {
	dispatcher.mutex.Lock()
	defer dispatcher.mutex.Unlock()
	if dispatcher.closed {
		panic("campaign observation published after dispatcher closure")
	}
	dispatcher.queue = append(dispatcher.queue, event)
	dispatcher.wake.Signal()
}

func (dispatcher *observerDispatcher) finish() (error, any) {
	if dispatcher == nil {
		return nil, nil
	}
	dispatcher.mutex.Lock()
	dispatcher.closed = true
	dispatcher.wake.Signal()
	dispatcher.mutex.Unlock()
	<-dispatcher.done

	return errors.Join(dispatcher.failures...), dispatcher.panic
}

func (dispatcher *observerDispatcher) run() {
	defer close(dispatcher.done)
	for {
		dispatcher.mutex.Lock()
		for len(dispatcher.queue) == 0 && !dispatcher.closed {
			dispatcher.wake.Wait()
		}
		if len(dispatcher.queue) == 0 {
			dispatcher.mutex.Unlock()
			return
		}
		event := dispatcher.queue[0]
		dispatcher.queue = dispatcher.queue[1:]
		dispatcher.mutex.Unlock()

		failure, panicValue := observeCampaignEvent(dispatcher.observer, event)
		if failure != nil || panicValue != nil {
			dispatcher.mutex.Lock()
			if failure != nil {
				dispatcher.failures = append(dispatcher.failures, failure)
			}
			if dispatcher.panic == nil && panicValue != nil {
				dispatcher.panic = panicValue
			}
			dispatcher.mutex.Unlock()
		}
	}
}

func observeCampaignEvent(observer ProgressObserver, event CampaignEvent) (failure error, panicValue any) {
	defer func() {
		panicValue = recover()
	}()

	return observer.Observe(event), nil
}

func projectCampaignEvent(progress internalooze.ManagedProgress) CampaignEvent {
	switch progress.Kind {
	case internalooze.ManagedCampaignStarted:
		return CampaignStarted{}
	case internalooze.ManagedCatalogueDiscovered:
		return CatalogueDiscovered{Total: progress.Total}
	case internalooze.ManagedBaselineStarted:
		return BaselineStarted{}
	case internalooze.ManagedBaselineFinished:
		return BaselineFinished{Passed: progress.Passed}
	case internalooze.ManagedMutationStarted:
		return MutationStarted{
			Mutation: Mutation{Label: progress.Label}, Confirmation: progress.Confirmation,
		}
	case internalooze.ManagedMutationFinished:
		return MutationFinished{
			Mutation: Mutation{Label: progress.Label}, Outcome: projectMutationOutcome(progress.Outcome),
		}
	case internalooze.ManagedCampaignCompleted:
		return CampaignCompleted{}
	default:
		panic("managed campaign progress is invalid")
	}
}
