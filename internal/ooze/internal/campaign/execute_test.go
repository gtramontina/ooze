package campaign_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/ooze/internal/campaign"
	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
	"github.com/stretchr/testify/assert"
)

func TestExecutePublishesAndReleasesAnEmptyCatalogue(t *testing.T) {
	runtime := processruntime.New(2)
	executor := campaign.NewExecutorWithSystem(runtime, unusedAttemptSystem{})
	repository := &emptyRepository{}
	directories := &temporaryDirectories{}
	var events []campaign.ManagedProgressKind

	result := executor.Execute(campaign.Configuration{
		Identity: "campaign-a", Lineage: 11, Repository: repository, TemporaryDir: directories,
		Command: []string{"go", "test", "./..."}, Profile: processruntime.AutomaticProfile, Peers: 2,
		Observe: func(event campaign.ManagedProgress) { events = append(events, event.Kind) },
	})

	assert.Equal(t, campaign.ManagedNoMutants, result.Outcome)
	assert.Equal(t, []campaign.ManagedProgressKind{
		campaign.ManagedCampaignStarted, campaign.ManagedCatalogueDiscovered,
	}, events)
	assert.Equal(t, []string{"workspace-1"}, repository.removed)
}

type emptyRepository struct{ removed []string }

func (*emptyRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile { return nil }

func (repository *emptyRepository) MaterializeTemporaryRepository(path string) campaign.TemporaryRepository {
	return &emptyTemporaryRepository{path: path, owner: repository}
}

type emptyTemporaryRepository struct {
	path  string
	owner *emptyRepository
}

func (repository *emptyTemporaryRepository) Root() string                         { return repository.path }
func (*emptyTemporaryRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile { return nil }
func (repository *emptyTemporaryRepository) MaterializeTemporaryRepository(path string) campaign.TemporaryRepository {
	return &emptyTemporaryRepository{path: path, owner: repository.owner}
}
func (*emptyTemporaryRepository) Overwrite(string, []byte) {}
func (repository *emptyTemporaryRepository) Remove() {
	repository.owner.removed = append(repository.owner.removed, repository.path)
}

type temporaryDirectories struct{ next int }

func (directories *temporaryDirectories) New() string {
	directories.next++
	return "workspace-" + string(rune('0'+directories.next))
}

type unusedAttemptSystem struct{}

func (unusedAttemptSystem) ReserveLaunch(*processruntime.StartCell, supervision.Spec) {
	panic("unexpected launch")
}
func (unusedAttemptSystem) DiscardLaunch(*processruntime.StartCell) { panic("unexpected launch") }
func (unusedAttemptSystem) Launch(processruntime.PreparedStart, supervision.Spec) supervision.ObservedLaunch {
	panic("unexpected launch")
}
func (unusedAttemptSystem) Wait(supervision.Generation, *supervision.OwnedAttempt) supervision.ObservedTerminal {
	panic("unexpected wait")
}
func (unusedAttemptSystem) Stop(*supervision.OwnedAttempt) { panic("unexpected stop") }
func (unusedAttemptSystem) EmergencyDrain(supervision.EmergencyRequest) (supervision.SweepResult, processruntime.EmergencySettlement) {
	panic("unexpected emergency")
}
