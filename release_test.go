package ooze_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gtramontina/ooze"
	"github.com/gtramontina/ooze/viruses/integerincrement"
)

const managedReleaseHelper = "OOZE_MANAGED_RELEASE_HELPER"

const (
	managedSerialExpected = "OOZE_MANAGED_SERIAL_EXPECTED"
	managedSerialLock     = "OOZE_MANAGED_SERIAL_LOCK"
	managedSerialOverlap  = "OOZE_MANAGED_SERIAL_OVERLAP"
)

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
	role := os.Getenv(managedReleaseHelper)
	if role == "serial" {
		runManagedSerialHelper()
		return
	}
	if role != "1" {
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

func TestReleaseSerialUsesDetectedCapacityWithoutMutantOverlap(t *testing.T) {
	repository := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(repository, "source.go"),
		[]byte("package fixture\nvar first = 0\nvar second = 0\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	coordination := t.TempDir()
	t.Setenv(managedReleaseHelper, "serial")
	t.Setenv(managedSerialExpected, strconv.Itoa(runtime.GOMAXPROCS(0)))
	t.Setenv(managedSerialLock, filepath.Join(coordination, "active"))
	t.Setenv(managedSerialOverlap, filepath.Join(coordination, "overlap"))

	ooze.Release(t,
		ooze.WithRepositoryRoot(repository),
		ooze.WithTestCommand(os.Args[0]+" -test.run=^TestManagedReleaseCommandHelper$"),
		ooze.WithViruses(integerincrement.New()),
		ooze.Serial(),
	)
	if _, err := os.Stat(os.Getenv(managedSerialOverlap)); !os.IsNotExist(err) {
		t.Fatalf("serial mutant commands overlapped: %v", err)
	}
}

func runManagedSerialHelper() {
	if os.Getenv("GOMAXPROCS") != os.Getenv(managedSerialExpected) {
		os.Exit(2)
	}
	data, err := os.ReadFile("source.go")
	if err != nil {
		os.Exit(2)
	}
	if !strings.Contains(string(data), " = 1") {
		return
	}
	lock, err := os.OpenFile(os.Getenv(managedSerialLock), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		_ = os.WriteFile(os.Getenv(managedSerialOverlap), []byte("overlap"), 0o600)
		os.Exit(1)
	}
	_ = lock.Close()
	time.Sleep(50 * time.Millisecond)
	_ = os.Remove(os.Getenv(managedSerialLock))
	os.Exit(1)
}
