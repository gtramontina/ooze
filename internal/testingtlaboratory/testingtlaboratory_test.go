package testingtlaboratory //nolint:testpackage // Exercises the private scheduling seam.

import (
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/future"
	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/oozetesting/fakerepository"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/stretchr/testify/assert"
)

func TestTestingTLaboratorySchedulesSerialMutation(t *testing.T) {
	repository := fakerepository.New(fakerepository.FS{})
	mutatedFile := gomutatedfile.New("test-infection", "some-path.go", nil, nil)
	subtests := newControlledSubtests()
	delegate := &recordingLaboratory{result: result.Ok("mutant killed"), calls: 0}

	fut := newWithSubtests(subtests, delegate, serial).Test(repository, mutatedFile)

	assert.Equal(t, []scheduledSubtest{{
		name: "some-path.go → test-infection",
		mode: serial,
	}}, subtests.scheduled)
	assert.Equal(t, 1, delegate.calls)
	assert.Equal(t, result.Ok("mutant killed"), fut.Await())
}

func TestTestingTLaboratorySchedulesParallelMutation(t *testing.T) {
	repository := fakerepository.New(fakerepository.FS{})
	mutatedFile := gomutatedfile.New("test-infection", "some-path.go", nil, nil)
	subtests := newControlledSubtests()
	delegate := &recordingLaboratory{result: result.Err[string]("mutant survived"), calls: 0}

	fut := newWithSubtests(subtests, delegate, parallel).Test(repository, mutatedFile)

	assert.Equal(t, []scheduledSubtest{{
		name: "some-path.go → test-infection",
		mode: parallel,
	}}, subtests.scheduled)
	assert.Zero(t, delegate.calls)

	subtests.Release()

	assert.Equal(t, 1, delegate.calls)
	assert.Equal(t, result.Err[string]("mutant survived"), fut.Await())
}

func TestTestingTLaboratoryRejectsFilteredMutationSubtest(t *testing.T) {
	subtests := newControlledSubtests()
	subtests.accept = false
	laboratory := newWithSubtests(
		subtests,
		&recordingLaboratory{result: result.Ok("mutant killed"), calls: 0},
		serial,
	)
	mutatedFile := gomutatedfile.New("test-infection", "some-path.go", nil, nil)

	assert.PanicsWithValue(
		t,
		`mutation subtest "some-path.go → test-infection" was filtered out`,
		func() {
			laboratory.Test(fakerepository.New(fakerepository.FS{}), mutatedFile)
		},
	)
}

func TestTestingTLaboratoryResolvesParallelFutureWhenDelegatePanics(t *testing.T) {
	subtests := newControlledSubtests()
	laboratory := newWithSubtests(subtests, panickingLaboratory{}, parallel)
	mutatedFile := gomutatedfile.New("test-infection", "some-path.go", nil, nil)
	fut := laboratory.Test(fakerepository.New(fakerepository.FS{}), mutatedFile)

	assert.PanicsWithValue(t, "laboratory panic", subtests.Release)

	resolved := make(chan result.Result[string], 1)
	go func() { resolved <- fut.Await() }()
	select {
	case actual := <-resolved:
		assert.Equal(t, result.Err[string]("mutation execution aborted"), actual)
	case <-time.After(time.Second):
		t.Fatal("future was not resolved after delegate panic")
	}
}

func TestTestingSubtestsRunSerialBodyBeforeReturning(t *testing.T) {
	executed := false

	started := (testingSubtests{t: t}).Run("serial mutation", serial, func() {
		executed = true
	})

	assert.True(t, started)
	assert.True(t, executed)
}

func TestTestingSubtestsDeferParallelBodyUntilParentReturns(t *testing.T) {
	parentReturned := make(chan struct{})
	executed := make(chan struct{}, 1)

	passed := t.Run("parent", func(t *testing.T) {
		started := (testingSubtests{t: t}).Run("parallel mutation", parallel, func() {
			select {
			case <-parentReturned:
				executed <- struct{}{}
			default:
				t.Error("parallel mutation ran before its parent returned")
			}
		})

		assert.True(t, started)
		select {
		case <-executed:
			t.Fatal("parallel mutation ran while its parent was active")
		default:
		}

		close(parentReturned)
	})

	assert.True(t, passed)
	select {
	case <-executed:
	default:
		t.Fatal("parallel mutation did not finish before its parent subtest returned")
	}
}

func TestParallelMutationFutureResolvesBeforeParentCleanup(t *testing.T) {
	cleanupRan := make(chan struct{}, 1)
	passed := t.Run("parent", func(t *testing.T) {
		mutatedFile := gomutatedfile.New("test-infection", "some-path.go", nil, nil)
		fut := New(
			t,
			&recordingLaboratory{result: result.Err[string]("mutant survived"), calls: 0},
			true,
		).Test(fakerepository.New(fakerepository.FS{}), mutatedFile)

		t.Cleanup(func() {
			assert.Equal(t, result.Err[string]("mutant survived"), fut.Await())
			cleanupRan <- struct{}{}
		})
	})

	assert.True(t, passed, "a survived mutant is data, not a failed Go subtest")
	select {
	case <-cleanupRan:
	default:
		t.Fatal("parent cleanup did not run after its parallel mutation")
	}
}

type scheduledSubtest struct {
	name string
	mode execution
}

type controlledSubtests struct {
	scheduled []scheduledSubtest
	pending   []func()
	accept    bool
}

func newControlledSubtests() *controlledSubtests {
	return &controlledSubtests{
		scheduled: []scheduledSubtest{},
		pending:   []func(){},
		accept:    true,
	}
}

func (s *controlledSubtests) Run(name string, mode execution, body func()) bool {
	s.scheduled = append(s.scheduled, scheduledSubtest{name: name, mode: mode})
	if !s.accept {
		return false
	}

	if mode == parallel {
		s.pending = append(s.pending, body)

		return true
	}

	body()

	return true
}

func (s *controlledSubtests) Release() {
	for _, body := range s.pending {
		body()
	}
}

type recordingLaboratory struct {
	result result.Result[string]
	calls  int
}

type panickingLaboratory struct{}

func (panickingLaboratory) Test(
	_ ooze.Repository,
	_ *gomutatedfile.GoMutatedFile,
) future.Future[result.Result[string]] {
	panic("laboratory panic")
}

func (l *recordingLaboratory) Test(
	_ ooze.Repository,
	_ *gomutatedfile.GoMutatedFile,
) future.Future[result.Result[string]] {
	l.calls++

	return future.Resolved(l.result)
}
