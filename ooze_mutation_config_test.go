package ooze_test

import (
	"path/filepath"
	"testing"

	"github.com/gtramontina/ooze"
	"github.com/gtramontina/ooze/internal/gosourcefile"
	engine "github.com/gtramontina/ooze/internal/ooze"
)

var selfMutationProductionPaths = []string{ //nolint:gochecknoglobals // Immutable test-campaign contract.
	"internal/fsrepository/fstemporaryrepository.go",
	"internal/fsrepository/remove_unix.go",
	"internal/fsrepository/remove_windows.go",
	"internal/ignoredrepository/ignoredrepository.go",
	"internal/ooze/managed_attempt_system.go",
	"internal/ooze/managed_campaign.go",
	"internal/ooze/supervisor_native_darwin.go",
}

func selfMutationOptions() []ooze.Option {
	return []ooze.Option{
		ooze.ForceColors(),
		ooze.WithRepositoryRoot("."),
		withSelfMutationProductionCatalogue(selfMutationProductionPaths),
		ooze.WithTestCommand("gotestsum --format-hide-empty-pkg --max-fails=1 -- -failfast -skip=^TestDarwinNativeSupervisorTripsAutomaticDescendantFuse$ ./..."),
		ooze.WithMinimumThreshold(0.5),
		ooze.IgnoreSourceFiles("(^release\\.go$|^docs/prototypes/|testdata\\/.*)"),
	}
}

func withSelfMutationProductionCatalogue(paths []string) ooze.Option {
	selected := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		selected[filepath.ToSlash(path)] = struct{}{}
	}

	return func(options ooze.Options) ooze.Options {
		options.Repository = selfMutationRepository{
			Repository: options.Repository,
			selected:   selected,
		}

		return options
	}
}

type selfMutationRepository struct {
	engine.Repository
	selected map[string]struct{}
}

func (r selfMutationRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	return selectSelfMutationSources(r.Repository.ListGoSourceFiles(), r.selected)
}

func (r selfMutationRepository) MaterializeTemporaryRepository(path string) engine.TemporaryRepository {
	return selfMutationTemporaryRepository{
		TemporaryRepository: r.Repository.MaterializeTemporaryRepository(path),
		selected:            r.selected,
	}
}

type selfMutationTemporaryRepository struct {
	engine.TemporaryRepository
	selected map[string]struct{}
}

func (r selfMutationTemporaryRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	return selectSelfMutationSources(r.TemporaryRepository.ListGoSourceFiles(), r.selected)
}

func (r selfMutationTemporaryRepository) MaterializeTemporaryRepository(path string) engine.TemporaryRepository {
	return selfMutationTemporaryRepository{
		TemporaryRepository: r.TemporaryRepository.MaterializeTemporaryRepository(path),
		selected:            r.selected,
	}
}

func selectSelfMutationSources(
	sources []*gosourcefile.GoSourceFile,
	selected map[string]struct{},
) []*gosourcefile.GoSourceFile {
	selection := make([]*gosourcefile.GoSourceFile, 0, len(selected))
	for _, source := range sources {
		if _, included := selected[filepath.ToSlash(source.String())]; included {
			selection = append(selection, source)
		}
	}

	return selection
}

func TestSelfMutationCatalogueIsProportionalToChangedProduction(t *testing.T) {
	configured := ooze.Options{}
	for _, option := range selfMutationOptions() {
		configured = option(configured)
	}
	allowed := make(map[string]struct{}, len(selfMutationProductionPaths))
	for _, path := range selfMutationProductionPaths {
		allowed[path] = struct{}{}
	}
	assertSelfMutationCatalogue(t, configured.Repository.ListGoSourceFiles(), allowed)
	snapshot := configured.Repository.MaterializeTemporaryRepository(t.TempDir())
	assertSelfMutationCatalogue(t, snapshot.ListGoSourceFiles(), allowed)
}

func assertSelfMutationCatalogue(
	t *testing.T,
	sources []*gosourcefile.GoSourceFile,
	allowed map[string]struct{},
) {
	t.Helper()
	seen := make(map[string]struct{})
	for _, source := range sources {
		path := filepath.ToSlash(source.String())
		if _, selected := allowed[path]; !selected {
			t.Errorf("self-mutation catalogue includes unchanged production source %q", path)
		}
		seen[path] = struct{}{}
	}
	for _, path := range []string{
		"internal/fsrepository/fstemporaryrepository.go",
		"internal/ignoredrepository/ignoredrepository.go",
		"internal/ooze/managed_attempt_system.go",
		"internal/ooze/managed_campaign.go",
	} {
		if _, selected := seen[path]; !selected {
			t.Errorf("self-mutation catalogue omits changed portable source %q", path)
		}
	}
}
