//go:build mutation

package ooze_test

import (
	"testing"

	"github.com/gtramontina/ooze"
)

func runMutationShard(t *testing.T, shardName string) {
	t.Helper()
	ooze.Release(t, selfMutationOptions(shardName)...)
}

func TestMutationRepository(t *testing.T) {
	runMutationShard(t, "repository")
}

func TestMutationAttemptSystem(t *testing.T) {
	runMutationShard(t, "attempt-system")
}

func TestMutationCampaignRunner(t *testing.T) {
	runMutationShard(t, "campaign-runner")
}

func TestMutationCampaignCycle(t *testing.T) {
	runMutationShard(t, "campaign-cycle")
}

func TestMutationCampaignEffects(t *testing.T) {
	runMutationShard(t, "campaign-effects")
}

func TestMutationPlatform(t *testing.T) {
	runMutationShard(t, "darwin")
}
