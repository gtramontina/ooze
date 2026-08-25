package ooze_test

import (
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/gtramontina/ooze"
	"github.com/gtramontina/ooze/internal/gosourcefile"
	engine "github.com/gtramontina/ooze/internal/ooze"
)

type selfMutationShard struct {
	name     string
	paths    []string
	packages []string
}

var selfMutationProductionShards = []selfMutationShard{ //nolint:gochecknoglobals // Immutable test-campaign contract.
	{name: "repository", paths: []string{
		"internal/fsrepository/fstemporaryrepository.go",
		"internal/fsrepository/remove_unix.go",
		"internal/fsrepository/remove_windows.go",
		"internal/ignoredrepository/ignoredrepository.go",
	}, packages: []string{"./internal/fsrepository", "./internal/ignoredrepository"}},
	{name: "attempt-system", paths: []string{
		"internal/ooze/managed_attempt_system.go",
	}, packages: []string{"./internal/ooze"}},
	{name: "campaign-runner", paths: []string{
		"internal/ooze/managed_campaign.go",
	}, packages: []string{"./internal/ooze"}},
	{name: "campaign-cycle", paths: []string{
		"internal/ooze/managed_campaign_cycle.go",
	}, packages: []string{"./internal/ooze"}},
	{name: "campaign-effects", paths: []string{
		"internal/ooze/managed_campaign_effects.go",
	}, packages: []string{"./internal/ooze"}},
	{name: "darwin", paths: []string{
		"internal/ooze/supervisor_native_darwin.go",
	}, packages: []string{"./internal/ooze"}},
}

func TestSelfMutationShardsPartitionSelectedProduction(t *testing.T) {
	want := map[string]string{
		"internal/fsrepository/fstemporaryrepository.go":  "repository",
		"internal/fsrepository/remove_unix.go":            "repository",
		"internal/fsrepository/remove_windows.go":         "repository",
		"internal/ignoredrepository/ignoredrepository.go": "repository",
		"internal/ooze/managed_attempt_system.go":         "attempt-system",
		"internal/ooze/managed_campaign.go":               "campaign-runner",
		"internal/ooze/managed_campaign_cycle.go":         "campaign-cycle",
		"internal/ooze/managed_campaign_effects.go":       "campaign-effects",
		"internal/ooze/supervisor_native_darwin.go":       "darwin",
	}
	seen := make(map[string]string)
	for _, shard := range selfMutationProductionShards {
		for _, path := range shard.paths {
			if previous := seen[path]; previous != "" {
				t.Errorf("production source %q appears in shards %q and %q", path, previous, shard.name)
			}
			seen[path] = shard.name
		}
	}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("mutation shard partition=%#v, want %#v", seen, want)
	}
}

func TestSelfMutationShardsUseOwningPackageTests(t *testing.T) {
	want := map[string][]string{
		"repository":       {"./internal/fsrepository", "./internal/ignoredrepository"},
		"attempt-system":   {"./internal/ooze"},
		"campaign-runner":  {"./internal/ooze"},
		"campaign-cycle":   {"./internal/ooze"},
		"campaign-effects": {"./internal/ooze"},
		"darwin":           {"./internal/ooze"},
	}
	for _, shard := range selfMutationProductionShards {
		if !reflect.DeepEqual(shard.packages, want[shard.name]) {
			t.Errorf("mutation shard %q packages=%#v, want %#v", shard.name, shard.packages, want[shard.name])
		}
		for _, packagePattern := range shard.packages {
			if packagePattern == "./..." {
				t.Errorf("mutation shard %q repeats the full module test suite", shard.name)
			}
		}
	}
}

const selfMutationSubprocessSkip = "^(TestDarwinNativeSupervisorCapturesEscapeeBehindLiveGroupMember|TestDarwinNativeSupervisorTripsAutomaticDescendantFuse|TestMutation|TestRelease)"

func selfMutationOptions(shardName string) []ooze.Option {
	shard, found := selfMutationShardNamed(shardName)
	if !found {
		panic("unknown self-mutation shard " + shardName)
	}
	return []ooze.Option{
		ooze.ForceColors(),
		ooze.WithRepositoryRoot("."),
		withSelfMutationProductionCatalogue(shard.paths),
		ooze.WithTestCommand("gotestsum --format-hide-empty-pkg --max-fails=1 -- -failfast -skip=" +
			selfMutationSubprocessSkip + " " + strings.Join(shard.packages, " ")),
		ooze.WithMinimumThreshold(0.5),
		ooze.IgnoreSourceFiles("(^release\\.go$|^docs/prototypes/|testdata\\/.*)"),
	}
}

func selfMutationShardNamed(name string) (selfMutationShard, bool) {
	for _, shard := range selfMutationProductionShards {
		if shard.name == name {
			return shard, true
		}
	}

	return selfMutationShard{}, false
}

func activeSelfMutationShards() []selfMutationShard {
	active := selfMutationProductionShards[:len(selfMutationProductionShards)-1]
	if runtime.GOOS == "darwin" {
		active = selfMutationProductionShards
	}

	return active
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
	for _, shard := range activeSelfMutationShards() {
		configured := ooze.Options{}
		for _, option := range selfMutationOptions(shard.name) {
			configured = option(configured)
		}
		allowed := make(map[string]struct{}, len(shard.paths))
		for _, path := range shard.paths {
			allowed[path] = struct{}{}
		}
		assertSelfMutationCatalogue(t, configured.Repository.ListGoSourceFiles(), allowed)
		snapshot := configured.Repository.MaterializeTemporaryRepository(t.TempDir())
		assertSelfMutationCatalogue(t, snapshot.ListGoSourceFiles(), allowed)
		nested := snapshot.MaterializeTemporaryRepository(t.TempDir())
		assertSelfMutationCatalogue(t, nested.ListGoSourceFiles(), allowed)
	}
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
	for path := range allowed {
		active := path != "internal/fsrepository/remove_windows.go" &&
			path != "internal/ooze/supervisor_native_darwin.go"
		active = active || runtime.GOOS == "windows" && path == "internal/fsrepository/remove_windows.go"
		active = active || runtime.GOOS == "darwin" && path == "internal/ooze/supervisor_native_darwin.go"
		active = active && !(runtime.GOOS == "windows" && path == "internal/fsrepository/remove_unix.go")
		if _, selected := seen[path]; active && !selected {
			t.Errorf("self-mutation catalogue omits active changed production source %q", path)
		}
	}
}
