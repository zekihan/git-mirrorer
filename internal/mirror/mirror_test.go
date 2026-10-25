package mirror

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zekihan/git-mirrorer/internal/cache"
	"github.com/zekihan/git-mirrorer/internal/config"
	"github.com/zekihan/git-mirrorer/internal/git"
	"github.com/zekihan/git-mirrorer/internal/testutil"
)

func configured(t *testing.T, source string, destinations ...string) config.Config {
	t.Helper()
	repo := config.Repository{Name: "source", Source: source}
	for i, dest := range destinations {
		repo.Destinations = append(repo.Destinations, config.Destination{Name: fmt.Sprintf("dest-%d", i), URL: dest})
	}
	return config.Config{StateDir: t.TempDir(), Concurrency: 2, OperationTimeout: 10 * time.Second, Repositories: []config.Repository{repo}}
}

func realRunner(t *testing.T, cfg config.Config) *Runner {
	t.Helper()
	client, err := git.New(t.Context(), cfg.OperationTimeout)
	if err != nil {
		t.Fatal(err)
	}
	return &Runner{Config: cfg, Git: CachedGit{Client: client, StateDir: cfg.StateDir}}
}

func TestPartialFailureAndStaleCache(t *testing.T) {
	testutil.Isolate(t)
	source, dest := testutil.Repository(t, false), testutil.Repository(t, true)
	testutil.Commit(t, source, "a", "one")
	cfg := configured(t, source, filepath.Join(t.TempDir(), "missing"), dest)
	runner := realRunner(t, cfg)
	result := runner.Run(t.Context())
	if result.Outcome != Failed || result.Totals.Failed != 1 || result.Totals.Succeeded != 1 || result.Totals.Attempted != 2 {
		t.Fatalf("partial result: %+v", result)
	}
	testutil.EqualRefs(t, source, dest)
	before := testutil.Refs(t, dest)
	if err := os.Rename(source, source+"-offline"); err != nil {
		t.Fatal(err)
	}
	result = runner.Run(t.Context())
	if result.Totals.Skipped != 2 || result.Totals.Attempted != 0 || result.Sources[0].Error.Stage != "fetch" {
		t.Fatalf("stale cache result: %+v", result)
	}
	if testutil.Refs(t, dest) != before {
		t.Fatal("pushed after failed source refresh")
	}
}

type observedBackend struct {
	Backend
	prepareCount   int
	pushCount      int
	afterFirstPush func()
}

func (b *observedBackend) Prepare(ctx context.Context, source string) (string, func() error, string, error) {
	b.prepareCount++
	return b.Backend.Prepare(ctx, source)
}

func (b *observedBackend) Push(ctx context.Context, path, dest string, atomic bool) (git.Changes, error) {
	changes, err := b.Backend.Push(ctx, path, dest, atomic)
	b.pushCount++
	if b.pushCount == 1 {
		b.afterFirstPush()
	}
	return changes, err
}

func TestOneSnapshotForEveryDestination(t *testing.T) {
	testutil.Isolate(t)
	source, dest1, dest2 := testutil.Repository(t, false), testutil.Repository(t, true), testutil.Repository(t, true)
	first := testutil.Commit(t, source, "a", "one")
	cfg := configured(t, source, dest1, dest2)
	runner := realRunner(t, cfg)
	backend := &observedBackend{Backend: runner.Git, afterFirstPush: func() { testutil.Commit(t, source, "a", "two") }}
	runner.Git = backend
	result := runner.Run(t.Context())
	if result.Outcome != Succeeded || backend.prepareCount != 1 {
		t.Fatalf("%+v; prepares=%d", result, backend.prepareCount)
	}
	testutil.EqualRefs(t, dest1, dest2)
	if testutil.Git(t, dest2, "rev-parse", "refs/heads/main") != first {
		t.Fatal("destinations saw different snapshots")
	}
	result = runner.Run(t.Context())
	if result.Outcome != Succeeded || backend.prepareCount != 2 {
		t.Fatal("second invocation did not refresh once")
	}
	testutil.EqualRefs(t, source, dest2)
}

type fakeBackend struct {
	mu                                                   sync.Mutex
	active, maximum, prepares                            int
	entered                                              chan string
	proceed                                              chan struct{}
	prepareError, pushError, lfsFetchError, lfsPushError error
	events                                               []string
	pushErrors                                           map[string]error
	gates                                                map[string]chan struct{}
	pushed                                               chan string
}

func (b *fakeBackend) event(event string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, event)
}

func (b *fakeBackend) Prepare(ctx context.Context, source string) (string, func() error, string, error) {
	b.mu.Lock()
	b.active++
	b.prepares++
	b.maximum = max(b.maximum, b.active)
	b.mu.Unlock()
	b.event("prepare")
	release := func() error { b.mu.Lock(); b.active--; b.mu.Unlock(); return nil }
	if b.entered != nil {
		b.entered <- source
	}
	if gate := b.gates[source]; gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return "", release, "fetch", ctx.Err()
		}
	}
	if b.proceed != nil {
		select {
		case <-b.proceed:
		case <-ctx.Done():
			return "", release, "fetch", ctx.Err()
		}
	}
	return source, release, "fetch", b.prepareError
}
func (b *fakeBackend) Push(_ context.Context, _, destination string, _ bool) (git.Changes, error) {
	b.event("push")
	if b.pushed != nil {
		b.pushed <- destination
	}
	if err := b.pushErrors[destination]; err != nil {
		return git.Changes{}, err
	}
	return git.Changes{}, b.pushError
}

func TestReportKeepsConfigurationOrder(t *testing.T) {
	first, second := make(chan struct{}), make(chan struct{})
	backend := &fakeBackend{gates: map[string]chan struct{}{"first": first, "second": second}, entered: make(chan string, 2), pushed: make(chan string, 2)}
	cfg := configured(t, "first", "first-dest")
	cfg.Repositories = append(cfg.Repositories, config.Repository{Name: "second", Source: "second", Destinations: []config.Destination{{Name: "backup", URL: "second-dest"}}})
	done := make(chan Report, 1)
	go func() { done <- (&Runner{Config: cfg, Git: backend}).Run(t.Context()) }()
	for range 2 {
		select {
		case <-backend.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("workers not ready")
		}
	}
	close(second)
	select {
	case name := <-backend.pushed:
		if name != "second-dest" {
			t.Fatal(name)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second job did not finish first")
	}
	close(first)
	result := <-done
	if result.Outcome != Succeeded || result.Sources[0].Name != cfg.Repositories[0].Name || result.Sources[1].Name != "second" {
		t.Fatalf("unordered result: %+v", result)
	}
}

func TestOperationTimeoutDoesNotCancelOtherDestinations(t *testing.T) {
	backend := &fakeBackend{pushErrors: map[string]error{"slow": context.DeadlineExceeded}}
	result := (&Runner{Config: configured(t, "source", "slow", "healthy"), Git: backend}).Run(t.Context())
	if result.Totals.Failed != 1 || result.Totals.Succeeded != 1 || result.Sources[0].Destinations[0].Error.Code != "timeout" {
		t.Fatalf("%+v", result)
	}
}

func TestSourceFailureDoesNotStopOtherSources(t *testing.T) {
	testutil.Isolate(t)
	source, dest := testutil.Repository(t, false), testutil.Repository(t, true)
	testutil.Commit(t, source, "a", "one")
	cfg := configured(t, source, dest)
	cfg.Concurrency = 1
	bad := config.Repository{Name: "offline", Source: filepath.Join(t.TempDir(), "missing"), Destinations: []config.Destination{{Name: "backup", URL: filepath.Join(t.TempDir(), "unused")}}}
	cfg.Repositories = append([]config.Repository{bad}, cfg.Repositories...)
	result := realRunner(t, cfg).Run(t.Context())
	if result.Totals.Skipped != 1 || result.Totals.Succeeded != 1 || result.Sources[0].Error.Stage != "fetch" {
		t.Fatalf("%+v", result)
	}
	testutil.EqualRefs(t, source, dest)
}
func (b *fakeBackend) LFSFetch(context.Context, string, string) error {
	b.event("lfs-fetch")
	return b.lfsFetchError
}
func (b *fakeBackend) LFSPush(context.Context, string, string) error {
	b.event("lfs-push")
	return b.lfsPushError
}

func TestBoundedConcurrencyCancellationAndOrder(t *testing.T) {
	backend := &fakeBackend{entered: make(chan string, 4), proceed: make(chan struct{})}
	cfg := configured(t, "first", "dest")
	for _, name := range []string{"second", "third", "fourth"} {
		cfg.Repositories = append(cfg.Repositories, config.Repository{Name: name, Source: name, Destinations: []config.Destination{{Name: "dest", URL: name + "-dest"}}})
	}
	runner := Runner{Config: cfg, Git: backend}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan Report, 1)
	go func() { done <- runner.Run(ctx) }()
	for range 2 {
		select {
		case <-backend.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("workers did not start")
		}
	}
	cancel()
	result := <-done
	if backend.maximum != 2 || backend.prepares != 2 || result.Totals.Canceled != 4 || result.Totals.Attempted != 0 {
		t.Fatalf("concurrency/cancel: %+v, %+v", backend, result)
	}
	for i, source := range result.Sources {
		if source.Name != cfg.Repositories[i].Name {
			t.Fatal("report lost config order")
		}
	}
	if backend.active != 0 {
		t.Fatal("source lease leaked")
	}
}

func TestCanceledBeforeDispatchAndRunDeadline(t *testing.T) {
	backend := &fakeBackend{}
	runner := Runner{Config: configured(t, "source", "dest"), Git: backend}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result := runner.Run(ctx)
	if backend.prepares != 0 || result.Totals.Canceled != 1 {
		t.Fatalf("%+v", result)
	}
	runner.Config.RunTimeout = 20 * time.Millisecond
	backend.proceed = make(chan struct{})
	result = runner.Run(t.Context())
	if result.Error == nil || result.Error.Code != "timeout" || result.Totals.Canceled != 1 {
		t.Fatalf("deadline: %+v", result)
	}
}

func TestLFSOrderingAndFailurePropagation(t *testing.T) {
	for _, scenario := range []string{"success", "fetch failure", "push failure", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			backend := &fakeBackend{}
			cfg := configured(t, "source", "dest1", "dest2")
			cfg.Repositories[0].LFS = scenario != "disabled"
			if scenario == "fetch failure" {
				backend.lfsFetchError = errors.New("missing payload")
			}
			if scenario == "push failure" {
				backend.lfsPushError = errors.New("upload rejected")
			}
			result := (&Runner{Config: cfg, Git: backend}).Run(t.Context())
			want := "[prepare lfs-fetch lfs-push push lfs-push push]"
			switch scenario {
			case "fetch failure":
				want = "[prepare lfs-fetch]"
				if result.Totals.Skipped != 2 {
					t.Fatal(result)
				}
			case "push failure":
				want = "[prepare lfs-fetch lfs-push lfs-push]"
				if result.Totals.Failed != 2 {
					t.Fatal(result)
				}
			case "disabled":
				want = "[prepare push push]"
			}
			if fmt.Sprint(backend.events) != want {
				t.Fatalf("events: %v", backend.events)
			}
		})
	}
}

func TestCacheLockConflictAndInvalidCache(t *testing.T) {
	testutil.Isolate(t)
	source, dest := testutil.Repository(t, false), testutil.Repository(t, true)
	testutil.Commit(t, source, "a", "one")
	cfg := configured(t, source, dest)
	runner := realRunner(t, cfg)
	lease, err := cache.Acquire(cfg.StateDir, source)
	if err != nil {
		t.Fatal(err)
	}
	result := runner.Run(t.Context())
	if result.Sources[0].Error.Stage != "lock" || result.Totals.Skipped != 1 {
		t.Fatal(result)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if result = runner.Run(t.Context()); result.Outcome != Succeeded {
		t.Fatal(result)
	}
	metadata := filepath.Join(cfg.StateDir, "repositories", cache.Key(source), "metadata.json")
	if err := os.WriteFile(metadata, []byte(`{"version":99,"source":"other"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result = runner.Run(t.Context())
	if result.Sources[0].Error.Stage != "cache" || result.Totals.Attempted != 0 {
		t.Fatal(result)
	}
}

func TestLoadedEndpointIdentityPreventsPublication(t *testing.T) {
	for _, replaceSource := range []bool{false, true} {
		t.Run(fmt.Sprint(replaceSource), func(t *testing.T) {
			testutil.Isolate(t)
			source, destination := testutil.Repository(t, false), testutil.Repository(t, true)
			testutil.Commit(t, source, "a", "one")
			input := fmt.Sprintf("stateDir: %q\nrepositories:\n- name: fixture\n  source: %q\n  destinations:\n  - name: backup\n    url: %q\n", filepath.Join(t.TempDir(), "state"), source, destination)
			cfg, err := config.Decode(strings.NewReader(input), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			target := destination
			if replaceSource {
				target = source
			}
			if err := os.Rename(target, target+".old"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(target, 0o700); err != nil {
				t.Fatal(err)
			}
			testutil.Git(t, target, "init", "--bare", "--initial-branch=main")
			result := realRunner(t, cfg).Run(t.Context())
			if result.Outcome != Failed {
				t.Fatalf("accepted replaced endpoint: %+v", result)
			}
			if got := testutil.Refs(t, destination); got != "" {
				t.Fatalf("published to replacement: %s", got)
			}
			if replaceSource && result.Sources[0].Error.Stage != "source-path" {
				t.Fatalf("source was used: %+v", result)
			}
			if !replaceSource && result.Sources[0].Destinations[0].Error.Stage != "destination-path" {
				t.Fatalf("destination was used: %+v", result)
			}
		})
	}
}
