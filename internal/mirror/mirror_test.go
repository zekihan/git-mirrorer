package mirror

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	return &Runner{Config: cfg, Git: CachedGit{Client: client, StateDir: cfg.StateDir, Limits: cfg.Cache, Timeout: cfg.OperationTimeout}}
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

func TestHighCardinalityCacheDoesNotStopHealthySource(t *testing.T) {
	testutil.Isolate(t)
	source, dest := testutil.Repository(t, false), testutil.Repository(t, true)
	testutil.Commit(t, source, "data", "old")
	cfg := configured(t, source, dest)
	if result := realRunner(t, cfg).Run(t.Context()); result.Outcome != Succeeded {
		t.Fatal(result)
	}
	path := filepath.Join(cfg.StateDir, "repositories", cache.Key(source), "repo.git", "extra")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := range 256 {
		if err := os.WriteFile(filepath.Join(path, fmt.Sprintf("%04d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	healthy, backup := testutil.Repository(t, false), testutil.Repository(t, true)
	testutil.Commit(t, healthy, "healthy", "new")
	cfg.Cache = cache.DefaultLimits()
	cfg.Cache.SourceEntries = 128
	cfg.Repositories = append(cfg.Repositories, config.Repository{Name: "healthy", Source: healthy, Destinations: []config.Destination{{Name: "backup", URL: backup}}})
	cfg.Concurrency = 2 // RunTimeout is deliberately disabled.
	start := time.Now()
	result := realRunner(t, cfg).Run(t.Context())
	if result.Totals.Skipped != 1 || result.Totals.Succeeded != 1 || result.Sources[0].Error.Stage != "cache" || !strings.Contains(result.Sources[0].Error.Detail, "entry budget") || time.Since(start) > 5*time.Second {
		t.Fatalf("cardinality isolation: %+v (%v)", result, time.Since(start))
	}
	testutil.EqualRefs(t, healthy, backup)
}

func TestReclaimsDisjointGitAndLFSSnapshotsUnderLease(t *testing.T) {
	if _, err := exec.LookPath("git-lfs"); err != nil {
		t.Skip("Git LFS is not installed")
	}
	testutil.Isolate(t)
	source, dest1, dest2 := testutil.Repository(t, false), testutil.Repository(t, true), testutil.Repository(t, true)
	testutil.Git(t, source, "lfs", "install", "--local")
	cfg := configured(t, source, dest1, dest2)
	cfg.Repositories[0].LFS = true
	cfg.Cache = cache.DefaultLimits()
	cfg.Cache.SourceBytes = 1 << 20
	cfg.Cache.SourceObjects = 100
	runner := realRunner(t, cfg)
	repoPath := filepath.Join(cfg.StateDir, "repositories", cache.Key(source), "repo.git")
	var previous, previousPayload string
	for generation := range 4 {
		if generation > 0 {
			testutil.Git(t, source, "checkout", "--orphan", "next")
			testutil.Git(t, source, "rm", "-rf", ".")
			testutil.Git(t, source, "branch", "-D", "main")
			testutil.Git(t, source, "branch", "-m", "main")
		}
		testutil.Commit(t, source, ".gitattributes", "*.bin filter=lfs diff=lfs merge=lfs -text\n")
		payload := fmt.Sprintf("payload generation %d", generation)
		current := testutil.Commit(t, source, "asset.bin", payload)
		oid := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
		payloadPath := filepath.Join(repoPath, "lfs", "objects", oid[:2], oid[2:4], oid)
		backend := &observedBackend{Backend: realRunner(t, cfg).Git, afterFirstPush: func() {
			if lease, err := cache.Acquire(cfg.StateDir, source); err == nil {
				lease.Close()
				t.Error("source lease released before second destination")
			}
			if got, err := os.ReadFile(payloadPath); err != nil || string(got) != payload {
				t.Errorf("payload removed during fan-out: %q %v", got, err)
			}
		}}
		runner.Git = backend
		result := runner.Run(t.Context())
		if result.Outcome != Succeeded || result.Totals.Succeeded != 2 {
			t.Fatalf("generation %d: %+v", generation, result)
		}
		testutil.EqualRefs(t, source, dest1)
		testutil.EqualRefs(t, source, dest2)
		for _, dest := range []string{dest1, dest2} {
			if got, err := os.ReadFile(filepath.Join(dest, "lfs", "objects", oid[:2], oid[2:4], oid)); err != nil || string(got) != payload {
				t.Fatalf("destination payload: %q %v", got, err)
			}
		}
		if previous != "" {
			cmd := exec.CommandContext(t.Context(), "git", "cat-file", "-e", previous)
			cmd.Dir = repoPath
			if err := cmd.Run(); err == nil {
				t.Fatal("unreachable Git history retained")
			}
			oldOID := fmt.Sprintf("%x", sha256.Sum256([]byte(previousPayload)))
			if _, err := os.Lstat(filepath.Join(repoPath, "lfs", "objects", oldOID[:2], oldOID[2:4], oldOID)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unreachable LFS payload retained: %v", err)
			}
		}
		previous, previousPayload = current, payload
	}
}

func TestCacheBudgetFailureSkipsEveryDestination(t *testing.T) {
	testutil.Isolate(t)
	source, dest := testutil.Repository(t, false), testutil.Repository(t, true)
	testutil.Commit(t, source, "data", "one")
	cfg := configured(t, source, dest)
	cfg.Cache = cache.DefaultLimits()
	cfg.Cache.SourceBytes = 1
	result := realRunner(t, cfg).Run(t.Context())
	if result.Totals.Attempted != 0 || result.Totals.Skipped != 1 || result.Sources[0].Error == nil || !strings.Contains(result.Sources[0].Error.Detail, "budget") {
		t.Fatalf("oversize snapshot published: %+v", result)
	}
	if testutil.Refs(t, dest) != "" {
		t.Fatal("destination changed on cache failure")
	}
}

func TestLFSBudgetFailureBlocksGitPublication(t *testing.T) {
	if _, err := exec.LookPath("git-lfs"); err != nil {
		t.Skip("Git LFS is not installed")
	}
	testutil.Isolate(t)
	source, dest := testutil.Repository(t, false), testutil.Repository(t, true)
	testutil.Git(t, source, "lfs", "install", "--local")
	testutil.Commit(t, source, ".gitattributes", "*.bin filter=lfs diff=lfs merge=lfs -text\n")
	testutil.Commit(t, source, "large.bin", strings.Repeat("payload", 20_000))
	cfg := configured(t, source, dest)
	cfg.Repositories[0].LFS = true
	cfg.Cache = cache.DefaultLimits()
	cfg.Cache.SourceBytes = 64 << 10
	result := realRunner(t, cfg).Run(t.Context())
	if result.Totals.Attempted != 0 || result.Totals.Skipped != 1 || result.Sources[0].Error == nil || result.Sources[0].Error.Stage != "lfs-fetch" || !strings.Contains(result.Sources[0].Error.Detail, "byte budget") {
		t.Fatalf("oversize LFS snapshot published: %+v", result)
	}
	if testutil.Refs(t, dest) != "" {
		t.Fatal("Git refs published despite oversized LFS payload")
	}
	// The failed refresh is not reused or pushed on a subsequent invocation.
	result = realRunner(t, cfg).Run(t.Context())
	if result.Totals.Attempted != 0 || result.Totals.Skipped != 1 {
		t.Fatalf("oversize cache reused: %+v", result)
	}
}
