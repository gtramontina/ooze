package ooze_test

import (
	"errors"
	"testing"

	"github.com/gtramontina/ooze"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComposeObserversPreservesOrderAndJoinsFailures(t *testing.T) {
	firstFailure := errors.New("first observer failed")
	secondFailure := errors.New("second observer failed")
	var observed []string
	first := observerFunc(func(ooze.CampaignEvent) error {
		observed = append(observed, "first")

		return firstFailure
	})
	second := observerFunc(func(ooze.CampaignEvent) error {
		observed = append(observed, "second")

		return secondFailure
	})
	observer := ooze.ComposeObservers(first, nil, second)

	err := observer.Observe(ooze.CampaignStarted{})

	assert.Equal(t, []string{"first", "second"}, observed)
	require.Error(t, err)
	assert.ErrorIs(t, err, firstFailure)
	assert.ErrorIs(t, err, secondFailure)
}

type observerFunc func(ooze.CampaignEvent) error

func (observe observerFunc) Observe(event ooze.CampaignEvent) error {
	return observe(event)
}
