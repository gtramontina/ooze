package ooze_test

import (
	"os"
	"strings"
	"testing"
)

func TestNativeWorkflowUsesSupportedToolchainsAndRejectsSkippedEvidence(t *testing.T) {
	workflow, err := os.ReadFile(".github/workflows/os-compatibility.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)

	for _, required := range []string{
		"toolchain: devbox",
		"toolchain: raw-go",
		"if: ${{ matrix.toolchain == 'devbox' }}",
		"if: ${{ matrix.toolchain == 'raw-go' }}",
		"go-command: devbox run -- go",
		`grep -q '"Action":"skip"'`,
		"unexpected skip in required native evidence",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("native workflow is missing %q", required)
		}
	}
}

func TestMutationWorkflowUsesDevboxExceptForNativeWindows(t *testing.T) {
	workflow, err := os.ReadFile(".github/workflows/mutation.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)

	for _, required := range []string{
		"toolchain: devbox",
		"toolchain: raw-go",
		"if: ${{ matrix.toolchain == 'devbox' }}",
		"if: ${{ matrix.toolchain == 'raw-go' }}",
		"go-command: devbox run -- go",
		"test-command: devbox run -- go test",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("mutation workflow is missing %q", required)
		}
	}
}
