package ooze

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestNativeSupervisorCapturePreservesPartialPrefixAndDiagnostic(t *testing.T) {
	readErr := errors.New("read merged output prefix")
	executor := &supervisorNativeExecutor{
		attempts: map[attemptGeneration]*supervisorNativeAttempt{
			1: {output: &os.File{}},
		},
		outputs:     make(map[supervisorOutputRef]string),
		diagnostics: make(map[supervisorDiagnosticRef]error),
		readOutputFile: func(*os.File) (string, uint64, error) {
			return "short", 10, readErr
		},
	}
	action := supervisorAction{
		kind: supervisorCaptureOutput, generation: 1, token: 7, at: time.Unix(1, 0),
	}
	event := executor.captureOutput(action)
	if event == nil || event.output == nil {
		t.Fatal("capture returned no output completion")
	}
	completion := event.output
	if completion.ref == 0 || completion.cutoff != 10 || completion.prefixLength != 5 ||
		completion.diagnostic == 0 || executor.readOutput(completion.ref) != "short" ||
		executor.readDiagnostic(completion.diagnostic).Error() != readErr.Error() {
		t.Fatalf("partial output completion = %#v", completion)
	}
}
