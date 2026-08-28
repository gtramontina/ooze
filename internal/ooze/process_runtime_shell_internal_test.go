package ooze

import "github.com/gtramontina/ooze/internal/ooze/internal/processruntime"

const (
	sharedAdmission        = processruntime.SharedAdmission
	exclusiveAdmission     = processruntime.ExclusiveAdmission
	serialPrimaryAdmission = processruntime.SerialPrimaryAdmission
)

type pendingStartCell = processruntime.StartCell
type installedStart = processruntime.PreparedStart
type processRuntimeShell = processruntime.Runtime

func newProcessRuntimeShell(capacity int) *processruntime.Runtime {
	return processruntime.New(capacity)
}

func newProcessRuntimeShellWithObserver(capacity int, observer processruntime.Observer) *processruntime.Runtime {
	return processruntime.NewObserved(capacity, observer)
}

func campaignTokenForTest(lineage campaignLineage) campaignToken {
	_, result := processruntime.NewReplay(1).Apply(processruntime.RegisterCampaignCut(lineage))
	return result.Registration().Campaign()
}
