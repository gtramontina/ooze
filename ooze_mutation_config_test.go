package ooze_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gtramontina/ooze"
	"github.com/gtramontina/ooze/internal/gosourcefile"
	engine "github.com/gtramontina/ooze/internal/ooze"
	"github.com/stretchr/testify/assert"
)

func TestSelfMutationCampaignSelectsOnePlatformCatalogue(t *testing.T) {
	portable := []string{
		"internal/fsrepository/fstemporaryrepository.go",
		"internal/fsrepository/remove_unix.go",
		"internal/fsrepository/remove_windows.go",
		"internal/ignoredrepository/ignoredrepository.go",
		"internal/ooze/managed_attempt_system.go",
		"internal/ooze/managed_campaign.go",
		"internal/ooze/managed_campaign_cycle.go",
		"internal/ooze/managed_campaign_emergency.go",
		"internal/ooze/managed_campaign_effects.go",
	}
	for _, test := range []struct {
		name string
		goos string
		want []string
	}{
		{name: "Linux", goos: "linux", want: portable},
		{name: "Windows", goos: "windows", want: portable},
		{name: "Darwin", goos: "darwin", want: append(
			append([]string(nil), portable...),
			"internal/ooze/supervisor_native_darwin.go",
		)},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, selfMutationProductionPaths(test.goos))
		})
	}
}

var selfMutationPortableProductionPaths = []string{
	"internal/fsrepository/fstemporaryrepository.go",
	"internal/fsrepository/remove_unix.go",
	"internal/fsrepository/remove_windows.go",
	"internal/ignoredrepository/ignoredrepository.go",
	"internal/ooze/managed_attempt_system.go",
	"internal/ooze/managed_campaign.go",
	"internal/ooze/managed_campaign_cycle.go",
	"internal/ooze/managed_campaign_emergency.go",
	"internal/ooze/managed_campaign_effects.go",
}

func selfMutationProductionPaths(goos string) []string {
	paths := append([]string(nil), selfMutationPortableProductionPaths...)
	if goos == "darwin" {
		paths = append(paths, "internal/ooze/supervisor_native_darwin.go")
	}

	return paths
}

const selfMutationSubprocessSkip = "^(TestDarwinNativeSupervisorCapturesEscapeeBehindLiveGroupMember|TestDarwinNativeSupervisorTripsAutomaticDescendantFuse|TestNativeSupervisorDrainsWideFanout)$"

func selfMutationOptions() []ooze.Option {
	return []ooze.Option{
		ooze.ForceColors(),
		ooze.WithRepositoryRoot("."),
		withSelfMutationProductionCatalogue(selfMutationProductionPaths(runtime.GOOS)),
		ooze.WithTestCommand("gotestsum --format-hide-empty-pkg --max-fails=1 -- -failfast -skip=" +
			selfMutationSubprocessSkip +
			" ./internal/fsrepository ./internal/ignoredrepository ./internal/ooze"),
		ooze.WithMinimumThreshold(0.5),
		ooze.IgnoreSourceFiles("(^release\\.go$|testdata\\/.*)"),
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
	paths := selfMutationProductionPaths(runtime.GOOS)
	allowed := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		allowed[path] = struct{}{}
	}
	assertSelfMutationCatalogue(t, configured.Repository.ListGoSourceFiles(), allowed)
	snapshot := configured.Repository.MaterializeTemporaryRepository(t.TempDir())
	assertSelfMutationCatalogue(t, snapshot.ListGoSourceFiles(), allowed)
	nested := snapshot.MaterializeTemporaryRepository(t.TempDir())
	assertSelfMutationCatalogue(t, nested.ListGoSourceFiles(), allowed)
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
		{
			_, selected := allowed[path]
			assert.True(t, selected, "self-mutation catalogue includes unchanged production source %q", path)
		}
		seen[path] = struct{}{}
	}
	for path := range allowed {
		active := path != "internal/fsrepository/remove_windows.go" &&
			path != "internal/ooze/supervisor_native_darwin.go"
		active = active || runtime.GOOS == "windows" && path == "internal/fsrepository/remove_windows.go"
		active = active || runtime.GOOS == "darwin" && path == "internal/ooze/supervisor_native_darwin.go"
		active = active && (runtime.GOOS != "windows" || path != "internal/fsrepository/remove_unix.go")
		{
			_, selected := seen[path]
			assert.False(t, active && !selected, "self-mutation catalogue omits active changed production source %q", path)
		}
	}
}
