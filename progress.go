package ooze

import "errors"

// CampaignEvent is an immutable fact published after an accepted campaign transition.
type CampaignEvent interface {
	campaignEvent()
}

// CampaignStarted reports that a campaign has entered the process runtime.
type CampaignStarted struct{}

func (CampaignStarted) campaignEvent() {}

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
