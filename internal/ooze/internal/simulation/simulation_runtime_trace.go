package simulation

import "github.com/gtramontina/ooze/internal/ooze/internal/processruntime"

type simulationRuntimeState = processruntime.Projection

func simulationTraceRuntimeState(value processruntime.Replay) simulationRuntimeState {
	return value.Projection()
}
