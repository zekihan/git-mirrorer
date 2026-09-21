# One-shot git-mirrorer rewrite

Status: implemented. The verification record at the end maps the design to tests
and local delivery checks. No release has been published.

## Objective

Replace the current application with one executable that loads a configuration,
mirrors the configured repositories once, reports every outcome, and exits.
Scheduling belongs to cron, a systemd timer, a Kubernetes CronJob, or the caller.
There is no HTTP server, health endpoint, ticker, daemon mode, or `runOnce` switch.

This is a clean replacement. Preserve neither the existing configuration schema,
environment overrides, package APIs, cache layout, nor current tests. Retain Go,
the Git CLI, and YAML because they fit the task. The user authorized implementation
after reviewing this plan.

## Scope decisions

Confirmed by the user:

1. Mirror all advertised refs under `refs/`, including force updates and deletions.
2. Require destinations to exist. Do not integrate hosting-provider APIs.
3. Include Git LFS only if it remains a small addition; do not build a substantial
   LFS subsystem.

Design decision: include opt-in LFS transfer through the existing `git-lfs` CLI,
with the bounded scope described below. This adds transfer stages to the existing
runner rather than introducing a separate client, server, or storage abstraction.

Implementation choices:

- Persistent, disposable local bare repositories allow efficient repeated runs.
- Support Linux and macOS first. Drop untested Windows release artifacts for this
  rewrite; Windows process-tree cancellation and file locking are separate work.
- Keep an optional container image that runs the same one-shot command.
- No automatic retry loop in the first version. A caller can rerun an incomplete
  job; successful destinations tolerate repetition.
- No database, durable work queue, web UI, metrics endpoint, or provider plugins.
- No repository settings, default-branch settings, issues, releases, access rules,
  or recursive submodule synchronization. Submodule repositories need their own
  configuration entries. This is replication, not historical backup retention.

## User interface

```sh
git-mirrorer --config ./git-mirrorer.yaml
git-mirrorer --config ./git-mirrorer.yaml --format json
git-mirrorer --config ./git-mirrorer.yaml --check
git-mirrorer --version
```

- Default configuration path: `./git-mirrorer.yaml`.
- `--check` parses and validates configuration without connecting to repositories,
  creating state directories, or acquiring locks. It does not test credentials.
- `--format text|json` selects the final report; text is the default.
- Send progress and diagnostic logs to stderr. Write one final report to stdout,
  including on ordinary operational failure or cancellation. JSON must remain
  parseable without removing log lines. Forced termination cannot promise a report.
- Reject unknown flags and positional arguments.
- No automatic `.env` loading, configuration discovery through environment
  variables, YAML environment interpolation, or generic environment overlay.
  Git and SSH retain their documented authentication environment.

Exit codes:

| Code | Meaning |
| --- | --- |
| 0 | Every destination succeeded, or validation/help/version succeeded |
| 1 | Incomplete run: source/destination failure, timeout, lock conflict, or local runtime failure |
| 2 | Invalid command line, unreadable/invalid configuration, or missing required executable |
| 130 | Interrupted by SIGINT |
| 143 | Terminated by SIGTERM |

An operation timeout fails that operation and permits unrelated work to continue.
An optional whole-run deadline cancels outstanding work and exits 1. Signals take
precedence over ordinary failures when determining the final exit code.

## Configuration contract

```yaml
stateDir: .git-mirrorer
concurrency: 4
operationTimeout: 10m
runTimeout: 0s
logLevel: info
repositories:
  - name: example
    source: git@github.com:owner/example.git
    lfs: false
    destinations:
      - name: gitlab
        url: git@gitlab.com:owner/example.git
        atomic: true
      - name: archive
        url: ssh://git@archive.example/owner/example.git
        atomic: true
```

All top-level fields shown above have those defaults except `repositories`, which
is required and nonempty. Repository names, source addresses, and nonempty
destination lists are required. `lfs` defaults to false for each repository.
Destination names and URLs are required;
`atomic` defaults to true. Decode optional booleans so explicit false survives
defaulting. Unknown and duplicate YAML keys and extra YAML documents are errors.

Resolve `stateDir` and relative filesystem repository addresses against the
configuration file's directory. Accept SSH URLs, SCP-style SSH addresses, HTTPS
URLs, and local paths/file URLs. Reject unsupported schemes, blank addresses,
control characters, and option-like addresses. Use a dedicated address parser;
SCP-style SSH addresses are not ordinary URLs.

Validate before performing repository work:

- Positive concurrency and operation timeout; nonnegative run timeout; known log level.
- Unique repository names and unique source identities.
- Unique destination names within a repository, and one owning source per destination.
- No destination is also any configured source. This keeps chains/cycles and
  concurrent source mutation outside the tool's contract.
- No source or destination overlaps the managed cache tree.
- No credentials embedded in URL passwords, queries, or fragments. Use SSH agents,
  SSH configuration, or Git credential helpers; SSH usernames remain valid.

Use conservative identity normalization: normalize local paths and resolve
existing symlinks; normalize scheme/host where appropriate, but preserve remote
path case and do not remove `.git` or infer provider-specific aliases. Distinct
SSH aliases and transport URLs may identify the same remote; detecting that is
not generally possible offline and is the operator's responsibility.

Errors name the configuration field and job, without printing secret-bearing
input. Do not expose compatibility aliases for the old schema.

## Execution model

1. Parse flags, load YAML, apply defaults, validate, and compile immutable job specs.
2. For a normal run, verify Git is available (and Git LFS when enabled), then
   establish the signal/deadline context.
3. Create a bounded pool of `min(concurrency, source count)` workers.
4. Each worker owns one source through its complete fetch-and-push sequence:
   - Acquire its cross-process cache lock without waiting indefinitely.
   - Open and validate its cache, or prepare a new mirror in a temporary directory.
   - Fetch the source once successfully before attempting any destination.
   - If LFS is enabled, fetch the snapshot's LFS payloads once as well.
   - Hold that snapshot unchanged while pushing destinations in configuration order.
     For LFS-enabled jobs, upload destination payloads before publishing Git refs.
   - Record every destination outcome, then release the source lock.
5. Finish other sources even if one source or destination fails.
6. Join all workers, produce a report in configuration order, and return an exit code.

Destinations for one source run sequentially. This keeps active Git operations
bounded by the source worker count and avoids a second pool and nested concurrency.
Duplicate sources are configuration errors; users express fan-out in one entry.

Source refresh failure marks every destination for that source as skipped with a
source-failure reason. Never push from an old cache after a failed refresh. A
destination failure leaves later destinations eligible to run. Cancellation stops
new dispatch, terminates active commands, and marks unstarted work canceled.

## Git behavior

Prepare new caches with a full `git clone --mirror --no-local` into a sibling
temporary directory. The `--no-local` choice avoids hardlinked objects when a
source is a local path. Do not request shallow or partial clones. Publish the
cache only after clone and validation succeed, using a rename on the same filesystem.

Refresh existing caches with an explicit forced mapping of `refs/*` to `refs/*`,
pruning removed refs and using `git fetch --atomic`. Disable implicit tag following
and submodule recursion so the mapping defines the transferred namespace. Use
the configured source address directly, not a mutable push URL or inferred remote.

Push directly to each destination with `git push --mirror --porcelain`, adding
`--atomic` when enabled. Never change `origin`'s push URL. Never add tool metadata
refs to the mirrored namespace. Require atomic support by default; an explicitly
configured `atomic: false` allows a destination that cannot provide it. Do not
silently fall back from atomic to non-atomic pushes.

Full mirroring includes force updates and deletion of destination refs absent
from the source snapshot. Only refs exposed by the source server are available;
hidden refs and host-owned namespaces can prevent a complete mirror. A rejected
ref is a failed destination, not a successful result with a warning. `HEAD` and
hosting-provider default-branch settings are outside the `refs/*` contract.
See the official [git-push documentation](https://git-scm.com/docs/git-push).

Fetch atomicity applies to local ref updates; push atomicity applies to one
destination. There is no transaction spanning multiple destinations. A lost
connection after a server accepts a push can leave its outcome uncertain: report
failure/uncertainty and let a later invocation reconcile again. Pruning follows
the explicit refspec. See [git-fetch](https://git-scm.com/docs/git-fetch).

## Bounded Git LFS support

For repositories configured with `lfs: true`, require the `git-lfs` executable.
After Git refresh, enumerate the full snapshot ref set. Use `git lfs fetch --all`
for the source and `git lfs push --all --stdin` for each destination, supplying
the explicit snapshot refs on stdin. Explicit refs matter because LFS fetch/push
defaults differ for namespaces outside branches and tags. Handle an empty ref set
explicitly. See the official [LFS fetch manual](https://github.com/git-lfs/git-lfs/blob/main/docs/man/git-lfs-fetch.adoc)
and [LFS push manual](https://github.com/git-lfs/git-lfs/blob/main/docs/man/git-lfs-push.adoc).

Use command-scoped named remotes for endpoint selection, with no persistent
destination remote edits. Support standard endpoint discovery and ordinary
Git LFS authentication. Custom LFS endpoint routing and custom transfer adapters
are outside this feature's supported contract; diagnose conflicting endpoint
overrides rather than risk uploading to the source endpoint. Do not write global
Git configuration or run a global `git lfs install`.

Upload LFS payloads before updating a destination's Git refs. An LFS fetch failure
blocks all destinations for that source; an upload failure blocks only that
destination's ref push. Use distinct `lfs-fetch` and `lfs-push` error stages. LFS
payload upload and ref push are not one transaction; a failed ref push may leave
unreferenced payloads on the server. Do not delete remote LFS objects or promise
LFS storage reclamation. Disable implicit hook-based LFS pushes; invoke these
stages explicitly through the same command runner and deadlines.

Cap LFS transfer concurrency at one per active source worker so it does not
multiply the configured concurrency unexpectedly. Keep downloaded payloads in
the managed cache. Native users install Git LFS only if enabling it; bundle it
in the container. Cover historical payloads and payloads reachable only from a
custom ref, missing payload failures, upload-before-ref ordering, and disabled
LFS behavior with an isolated local fixture. Do not introduce production HTTP
server code to support or test this feature.

## Cache and locking

Layout:

```text
<stateDir>/
  locks/<source-identity-hash>.lock
  repositories/<source-identity-hash>/
    metadata.json
    repo.git/
```

Use the full SHA-256 of the normalized, credential-free source identity as the
key. Names are presentation labels, so renaming a job does not invalidate its
cache. Metadata records the source identity and internal cache format version.
Keep metadata outside `repo.git` and out of Git refs.

Use OS-backed advisory file locks, released on process exit. Lockfiles may remain
after a crash; file existence does not mean a live lock. Keep lockfiles outside
directories that are renamed or replaced. Fail that source clearly on lock
contention and continue unrelated jobs. Shared state requires a filesystem with
working advisory locks; separate state directories do not coordinate competing
writers to the same remote.

Validate ownership metadata, bare/full repository shape, and expected source
before reuse. Reject symlinked cache entries. Remove incomplete temporary clones
created by the current attempt. For an invalid completed cache, return an
actionable error instead of deleting unfamiliar data or automatically moving
directories. The cache is disposable; document recovery by removing only the
identified managed entry while no run owns it. Do not implement cache migration
or automatic cache garbage collection in this rewrite.

## Command runner and authentication

Use a narrow Git command runner around `os/exec`, with argument arrays and no
shell interpolation. Supply explicit working directories and command-scoped
configuration for mirroring behavior. Disable hooks in managed caches, recursive
submodule operations, and detached automatic maintenance. Prevent inherited
repository-selection variables such as `GIT_DIR`, `GIT_WORK_TREE`, and
`GIT_NAMESPACE` from redirecting commands away from the managed repository.

Keep credential helpers, SSH agent access, known-host verification, proxy settings,
and the user's trusted transport configuration usable. Set noninteractive Git
authentication and SSH batch behavior; do not disable host-key verification.
Define how a supplied SSH command is composed or rejected rather than silently
discarding required user transport options.

Each operation has a deadline. On Linux/macOS, own a process group and terminate
the Git/SSH/helper process tree on cancellation, with a short bounded cleanup
period and forced termination if necessary. Bound pipe draining so inherited
child descriptors cannot hang shutdown. Release locks after command cleanup.

Keep bounded diagnostic tails, separately consume machine-readable push output,
and never truncate parsing merely because diagnostics are capped. Stream/aggregate
ref statuses without keeping unlimited output in memory. Redact credentials in
Git errors as well as application logs. Avoid logging entire command environments.

## Results and internal structure

Use typed results rather than making callers parse joined error strings:

- Run: start/end/duration, overall outcome, source/destination totals, job results.
- Source: name, refresh status, duration, error stage/code, destination results.
- Destination: name, outcome (`succeeded`, `failed`, `skipped`, `canceled`),
  duration, reason/error, and aggregate ref changes when available.

Use fixed error codes for config, lock, cache, fetch, push, LFS fetch/upload,
timeout, and cancellation.
Preserve wrapped errors internally; render bounded sanitized details at the edge.
Distinguish an unchanged successful push from a successful push with updates.
Count unsuccessful destinations once, including skipped and canceled destinations,
without pretending they were attempted. JSON receives a report schema version.

Suggested layout:

```text
cmd/git-mirrorer/main.go       flags, signals, composition, exit status
internal/config/             strict YAML, defaults, validation, address identity
internal/mirror/             job specs, worker pool, results
internal/git/                subprocess lifecycle and Git operations
internal/cache/              paths, metadata, locks, temporary clone publication
internal/report/             text/JSON rendering and redaction
```

Keep interfaces only at boundaries that need controlled failure injection, chiefly
Git execution. Pass configuration and logger explicitly. No package-level config
singleton, `init` startup work, global channels, generic service framework, or
shared helper dependency for simple command execution. Only `main` calls `os.Exit`,
after a testable entry function returns and its defers have completed.

## Repository and delivery changes

- Replace `main.go`, `config/`, `git/`, and `mirror/` with the new structure and tests.
- Remove HTTP dependencies, health code, scheduler types, `runOnce`, web/API-key
  settings, `.env` loading, and the generic environment configuration dependency.
- Remove the go-commons dependency; own the small command/log/version boundary.
- Replace `config.yaml` with a documented example of the new schema.
- Replace the README and AGENTS.md architecture/command descriptions.
- Remove `scripts/go_test.sh` and its `CONFIG_DIR` dependency; tests own fixtures.
- Remove the obsolete environment-dump workflow script if no longer used.
- Make `make build` use `go build`; reserve GoReleaser for release packaging.
- Make `make lint` and `make test` read-only checks. Separate formatting and module
  tidying from validation; stop hiding autofix failures.
- Simplify lint configuration to applicable project rules and pin CI tool versions.
- Update GoReleaser entrypoint, version symbols, targets, and packaged example/docs.
- Use an Alpine runtime with Git, Git LFS, OpenSSH, CA certificates, and a non-root user;
  no Go toolchain or exposed HTTP port is needed in the final image.
- Replace Compose's port mapping and `unless-stopped` policy with a one-shot
  example, explicit config path, writable state mount, and credential instructions.
- Remove private-module credentials from CI once no dependency requires them.
- Provide Linux CI checks plus local-container smoke validation; tests never
  require real hosting credentials or configured production destinations.
- Keep actual release publication and pushes outside implementation validation.

## Verification plan

Build integration fixtures from isolated local repositories with bare destinations.
Compare actual ref names and object IDs, not only subprocess return codes. Isolate
test HOME/Git configuration and hooks so developer settings do not change results.

Required behavior coverage:

1. Initial mirror, unchanged rerun, new commits, branches, tags, and notes/custom refs.
2. Rewritten history, moved tags, deleted source refs, and destination-only ref removal.
3. Exactly one refresh per source and the same snapshot sent to all its destinations.
4. Source failure never pushes cached data; one destination failure does not stop others.
5. Atomic rejection leaves all destination refs unchanged; explicitly non-atomic
   rejection reports failure even when some refs changed.
6. Empty source and populated destination behavior, including destination policies
   that reject deletion of their default branch.
7. Bounded concurrency, cancellation before dispatch, cancellation during Git/SSH
   helper execution, timeout, pipe cleanup, and absence of orphan processes.
8. Two executable processes contending for the same cache; lock recovery after exit.
9. Interrupted initial clone, invalid cache metadata, symlink/path conflicts, and
   source identity changes.
10. YAML/flag errors, duration/default handling, duplicate ownership, address forms,
    credential redaction, and config-directory-relative path resolution.
11. CLI exit codes and text/JSON reports for success, partial failure, and cancellation;
    output stays ordered even when jobs complete out of order.
12. One-shot container execution with local repositories, proper exit propagation,
    writable state, and no listener or restart loop.
13. Opt-in LFS transfers for historical/custom-ref payloads, correct destination
    routing, failure propagation, and ordinary Git operation without Git LFS installed.

Use synchronization barriers and helper processes for concurrency tests rather
than timing-dependent sleeps. Use fake execution only for hard-to-produce command
failures and protocol capability responses; keep real Git coverage for ref semantics.

Before completing implementation: run formatting checks, lint, all unit/integration
tests with the race detector, native build, release configuration validation, and
the container smoke test when a container runtime is available. Record any unavailable
environment check explicitly instead of treating it as passed.

## Implementation sequence

1. Encode the agreed scope and finalize the public config/CLI/result contracts.
2. Implement strict configuration and a testable one-shot command skeleton.
3. Implement command deadlines/cancellation, cache ownership/locking, and Git operations.
4. Implement source workers, independent destination outcomes, and final reports.
5. Replace the old application and prove mirroring behavior with local Git integration tests.
6. Replace build, CI, container, configuration examples, and documentation; run the full checks.

Keep implementation commits logically separated and validated. No transitional
compatibility layer or second execution mode is needed. Completion means one
invocation attempts every eligible destination once, cleans up, reports accurately,
and exits with a meaningful status; no background service remains.

## Implementation verification record

The production entrypoint is `cmd/git-mirrorer/main.go`. The old `main.go`,
`config/`, `git/`, and `mirror/` implementations were removed. The module now
depends only on YAML; there is no server, scheduler, shared configuration singleton,
or private helper module. The new example and README describe the replacement
configuration without migration aliases.

| Contract | Direct evidence |
| --- | --- |
| Strict config, paths, aliases, ownership, offline validation | `internal/config/config_test.go`; CLI validation test confirms no state creation and no Git dependency for `--check` |
| Full ref creation/update/deletion, tags, notes, custom refs, rewritten history | `TestExactMirror` compares complete ref/OID sets on actual bare destinations |
| Atomic rejection, explicit non-atomic partial update, unsupported atomic capability | `TestAtomicAndPartialPush` uses an update hook and disabled atomic advertisement |
| Empty source and server deletion policy | `TestEmptySourceAndDefaultBranchPolicy`, including empty-to-empty matching |
| One refresh and stable fan-out snapshot | `TestOneSnapshotForEveryDestination` changes the source between destination pushes |
| Partial failure and no stale cache push | `TestPartialFailureAndStaleCache`, `TestSourceFailureDoesNotStopOtherSources` |
| Bounded workers, stable report order, cancellation/deadlines | Mirror runner tests with synchronization barriers, plus real command timeout tests |
| Child cleanup and inherited pipes | `TestCancellationKillsProcessTree`, `TestParentExitWithInheritedPipes`, Linux/macOS execution |
| Cross-process cache lock and recovery after crash | `TestCrossProcessLockAndCrashRecovery` acquires the lock in another executable process |
| Temporary clone cleanup, invalid metadata, symlinks, source identity change | Cache tests, mirror invalid-cache test, and CLI signal tests |
| CLI codes, stdout/stderr separation, structured reports | CLI tests execute successful/failed runs and subprocess SIGINT/SIGTERM cases |
| Optional LFS historical/custom-ref payloads and endpoint selection | `TestLFSTransferAllSnapshotRefs` checks destination payload bytes; missing fetch/upload, empty source, endpoint override tests |
| LFS publication ordering and independent failure handling | `TestLFSOrderingAndFailurePropagation`; native CLI test works with only Git on PATH |
| One-shot non-root image, cache persistence, exit propagation, no exposed port | `scripts/container_smoke.sh`, with networking disabled during mirroring |

Local checks completed during implementation:

- `make lint`: clean with golangci-lint 2.13.2; no files changed by lint.
- `make test`: full race-enabled suite on macOS, including installed Git LFS 3.8.0.
- Linux container: race-enabled suite with Git 2.54.0 and Git LFS 3.7.1.
- `make build` and offline validation of `git-mirrorer.example.yaml`.
- `goreleaser check` and snapshot cross-builds for linux/darwin, amd64/arm64.
- Actionlint 1.7.7 validates the GitHub Actions workflows.
- Container smoke test builds the final Alpine image and verifies actual local ref equality.

Implementation refinements discovered through testing:

- Git errors on an empty-to-empty mirror push. An empty local snapshot now checks
  remote refs and succeeds without a push when both sides already match.
- LFS storage configuration passed with `git -c` reaches the standalone-file
  remote process. The cache storage setting is therefore local to the cache, while
  endpoint selection remains command-scoped. This preserves separate destination
  payload storage and keeps cached payloads under the managed repository.
- Command cleanup bounds inherited pipe draining as well as process lifetime.
  Linux tests distinguish terminated zombies awaiting reaping from live helpers.

Deliberate boundaries remain as designed: existing destinations only, advertised
refs only, no provider settings, no custom LFS routing/transfer adapters or
`.lfsconfig`, no remote LFS deletion, no retries/daemon, and no Windows artifacts.
