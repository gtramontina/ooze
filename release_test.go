package ooze_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gtramontina/ooze"
	"github.com/gtramontina/ooze/viruses/integerincrement"
)

const managedReleaseHelper = "OOZE_MANAGED_RELEASE_HELPER"

func TestReleaseRunsManagedBaselineAndMutation(t *testing.T) {
	repository := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(repository, "source.go"),
		[]byte("package fixture\nvar number = 0\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv(managedReleaseHelper, "1")

	ooze.Release(t,
		ooze.WithRepositoryRoot(repository),
		ooze.WithTestCommand(os.Args[0]+" -test.run=^TestManagedReleaseCommandHelper$"),
		ooze.WithViruses(integerincrement.New()),
	)
}

func TestManagedReleaseCommandHelper(t *testing.T) {
	if os.Getenv(managedReleaseHelper) != "1" {
		return
	}
	data, err := os.ReadFile("source.go")
	if err != nil {
		os.Exit(2)
	}
	if strings.Contains(string(data), "number = 1") {
		os.Exit(1)
	}
}
