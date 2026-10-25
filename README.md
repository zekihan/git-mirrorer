# git-mirrorer

A one-shot Git repository mirroring executable for Linux and macOS. Each invocation
fetches each source once, pushes that snapshot to its destinations, prints a report,
and exits. Schedule invocations externally with cron, systemd timers, or Kubernetes
CronJobs.

## Run

Install Git and, if needed, Git LFS. Build with the Go version in `go.mod`:

```sh
make build
cp git-mirrorer.example.yaml git-mirrorer.yaml
# Edit repository addresses before running.
./dist/git-mirrorer --config git-mirrorer.yaml --check
./dist/git-mirrorer --config git-mirrorer.yaml
```

`--check` validates configuration offline without creating a cache or testing
credentials. Normal execution contacts the configured repositories and changes
**every destination's refs to match its source**, including force updates and
ref deletions. Destinations must already exist. This is replication, not a backup
that retains deleted history.

The default config path is `./git-mirrorer.yaml`. `--format json` writes a single
machine-readable final report to stdout; progress and diagnostics use stderr.
`--version` and `--help` exit without loading configuration.

## Configuration

```yaml
stateDir: .git-mirrorer
concurrency: 4
operationTimeout: 10m
runTimeout: 0s
logLevel: info
cache:
  sourceBytes: 5368709120
  totalBytes: 21474836480
  sourceEntries: 250000
  totalEntries: 1000000
  sourceObjects: 2000000
  totalObjects: 8000000
  reserveBytes: 1073741824
  validationTimeout: 10s
repositories:
  - name: example
    source: git@github.com:owner/example.git
    lfs: false
    destinations:
      - name: gitlab
        url: git@gitlab.com:owner/example.git
        atomic: true
```

The example shows all defaults. `repositories` must be nonempty. Every repository
needs a unique name, source, and at least one named destination. `lfs` defaults to
false and destination `atomic` defaults to true. `concurrency` limits active source
jobs; destinations for a source run sequentially. `operationTimeout` bounds each
Git/LFS subprocess and the complete retry sequence for each network command. `runTimeout: 0s` disables the whole-run deadline.

Paths, including relative local repository paths, resolve from the configuration
file's directory. Addresses support HTTPS, SSH URLs, SCP-style SSH addresses, and
local paths/file URLs. Repository names are labels; addresses determine cache identity.
URL passwords, HTTPS userinfo, queries, and fragments are rejected. Use SSH agents
or credential helpers for credentials.

Unknown/duplicate YAML keys and multiple documents are errors. Duplicate sources,
duplicate destination ownership, destinations that are also sources, and repository
paths overlapping the cache are rejected. Combine a source's destinations in one
entry. Different SSH aliases or transport URLs may still refer to the same remote;
configure only one owner for each actual repository.

There is no environment-variable configuration overlay, `.env` loading, legacy
configuration support, HTTP endpoint, or internal scheduler.

## Mirroring and failures

All source-advertised refs under `refs/` are mirrored, including tags, branches,
notes, and custom refs. Hidden refs cannot be fetched. Hosts may reject protected
or host-managed refs; these are reported as failed destinations. Remote `HEAD`,
default-branch settings, access rules, issues, releases, and submodule repositories
are not synchronized. Configure each submodule repository separately if needed.

The tool refreshes a source successfully before pushing any of its destinations.
A failed refresh never causes stale cached refs to be pushed. A failed destination
allows other destinations and sources to continue. Every result is reported in
configuration order, even when sources run concurrently.

Clone, fetch, push, and LFS transfers retry recognized transient connection failures
up to three attempts total, waiting 1–1.5 seconds and then 2–3 seconds. Retries
share the operation deadline and stop on cancellation. Authentication failures,
rejected refs, and repository corruption fail immediately. Push retries use the
same locked source snapshot; change counts describe the final attempt.

Pushes require atomic support by default, so a rejected ref leaves that destination's
refs unchanged. Use `atomic: false` explicitly for a server that lacks support;
a failed non-atomic push may have updated some refs. There is no transaction across
destinations. A lost connection may leave the outcome uncertain; rerun to reconcile.
No application-level automatic retries are performed.

An empty source removes all deletable destination refs. If both sides have no refs,
the tool checks the destination and reports success without issuing an empty push.

| Exit | Meaning |
| --- | --- |
| 0 | All destinations succeeded, or validation/help/version succeeded |
| 1 | Incomplete run, operational failure, timeout, or report output failure |
| 2 | Invalid configuration/arguments or unavailable prerequisite |
| 130 | SIGINT |
| 143 | SIGTERM |

Signals stop new work and terminate Git/SSH helper process groups. The final report
includes skipped and canceled destinations. JSON uses `schemaVersion: 1`, RFC3339
timestamps, `durationNs` integer durations, per-source refresh results, per-destination
outcomes/attempt counts/ref-change counts, and totals. `unsuccessful` counts failed,
skipped, and canceled destinations exactly once. Ref counters on a failed push may
be incomplete; a failed result is never a guarantee that nothing changed. Configuration
and prerequisite errors are printed to stderr before a run report exists.

## Git LFS

Set `lfs: true` on a repository to transfer its LFS payloads as well as Git objects.
The native executable then requires `git-lfs` on PATH. Payloads reachable from all
snapshot refs, including historical commits and custom refs, are fetched once and
uploaded to each destination **before** publishing its Git refs. Missing source
payloads block that source; upload failures block only the affected destination.

LFS transfers use standard endpoint discovery and authentication. Custom endpoint
configuration, custom transfer adapters, and repositories containing `.lfsconfig`
are unsupported and rejected. Each active source uses one LFS transfer at a time.
There is no LFS/ref transaction or remote payload deletion: a failed ref push may
leave uploaded payloads on the destination. Local LFS payloads are rebuilt for each
refresh to avoid retaining obsolete generations. With `lfs: false`, ordinary
Git pointer files are mirrored but their external payloads are not transferred.

## Authentication and cache

Normal Git credential helpers, SSH agent access, proxies, SSH config, and known-host
verification remain available. Configure credentials and trusted hosts in advance;
Git prompts are disabled. SSH uses the explicit `GIT_SSH_COMMAND`, then `GIT_SSH`,
then `core.sshCommand`, or `ssh`, and appends `-o BatchMode=yes`. Custom commands
must accept OpenSSH options and must not override batch behavior. Repository-selection
environment variables are removed and managed repository hooks are disabled.

Caches live at `<stateDir>/repositories/<SHA-256 of source identity>/repo.git` with
ownership metadata alongside them. Advisory locks live separately in `locks/`.
Overlapping invocations sharing the same state directory fail contended sources
rather than waiting for a source lease. Shared storage must support advisory locks.
Separate state directories cannot coordinate competing writers to the same destination.

Cache limits are mandatory, positive values; omitted fields retain the defaults
shown above. Byte limits count logical regular-file sizes, including Git, LFS,
metadata, and temporary data. Entry limits include files and directories. Object
limits count loose Git objects and every packed object, including duplicate copies
during repacking. Total limits cover all entries under `repositories/`, including
idle sources and abandoned clones; each total must be at least its source limit.
`reserveBytes` preserves a sampled free-space margin for unrelated filesystem users.

Before and after storage operations, and periodically during them, the tool checks
these limits and the free-space reserve. A breach cancels the owned subprocess
process group and blocks publication of an unvalidated source snapshot. Inspections
stream batches of at most 128 directory entries, stop at the entry/object/byte
budget, reject symlinks and special files, limit depth to 128, and use
`validationTimeout` even with `runTimeout: 0s`. Metadata reads are capped at 64 KiB.
A state-wide advisory budget lock serializes clone/fetch, reclamation, and LFS
operations across processes. Lock waiting plus the complete storage operation is
bounded by `operationTimeout`; interrupted clone cleanup has a separate bounded
`validationTimeout` allowance. Cleanup also streams entries, stops at its entry
budget, and may leave an abandoned temporary entry for operator recovery. Partial
clone cleanup between retry attempts has a fixed 10-second/1,000,000-entry ceiling
within the retry deadline. Git-only destination pushes can still run concurrently.
All invocations sharing a state directory must use the same limits and this version
of the tool. Existing state directories must not allow group/other write access.
Keep state private to one trusted service account, on a filesystem supporting
ordinary bounded local directory operations and advisory locks. Hostile local
writers or filesystems that hang inside a system call are outside this model.

After a successful Git refresh, while holding the source lease, the tool expires
cache reflogs and runs foreground Git garbage collection with immediate pruning.
It then removes this source's previous local LFS storage before fetching all
payloads reachable from the refreshed refs. This costs repeated LFS downloads, but
prevents disjoint generations accumulating indefinitely. Reclamation finishes
before any destination is attempted; the snapshot and its complete LFS payloads
remain intact through every destination, including after a destination fails.

These are application budgets, **not hard filesystem quotas**: a subprocess can
write beyond a limit between inspections (a 200 ms sampling timer,
plus the bounded scan time and process shutdown). A fast transfer or repack can
therefore consume the free-space reserve before cancellation. Use a dedicated
filesystem/subvolume with byte and inode quotas, preferably per source, to enforce
a strict allocation ceiling and protect unrelated jobs. Size budgets below those
quotas with room for transfer/repack temporary data and normal filesystem overhead.
No quota, mount, or deployment settings are changed by the executable. An entry
left over budget by a canceled refresh fails closed on subsequent runs; it is not
automatically deleted. A total-budget/inspection failure blocks new storage work
until an operator restores capacity; source-only failures allow healthy sources
to continue when the total cache still fits its budget.

A cache is disposable. Invalid ownership, symlinks, partial/shallow repositories,
and unexpected origins produce errors instead of automatic deletion. To recover,
stop invocations that use that source and remove only its identified managed cache
entry, then rerun. A persistent lockfile is normal: its presence does not mean a
process holds the lock. Do not delete lockfiles to bypass a live lock. Abrupt machine
or process crashes may leave `.clone-*` directories; remove abandoned temporary
entries only while no invocation uses that state directory.

## Containers

```sh
cp git-mirrorer.example.yaml git-mirrorer.yaml
# Edit addresses and arrange SSH/HTTPS credentials.
docker compose run --rm --build git-mirrorer
```

The image contains Git, Git LFS, OpenSSH, and CA certificates, runs as UID 25090,
and exposes no ports. Compose mounts the config at `/data/git-mirrorer.yaml` and
uses a named volume for `/data/.git-mirrorer`; it does not restart completed runs.
If you change `stateDir`, update the mount accordingly. For bind-mounted state,
make the directory writable by UID 25090.

Mount a dedicated SSH directory read-only at `/home/git-mirrorer/.ssh` with readable
configuration/known_hosts and appropriately protected keys, or expose a compatible
agent socket with `SSH_AUTH_SOCK`. HTTPS credential helpers must be installed and
configured inside the container; host helper binaries are not automatically available.
Do not put credentials in repository URLs or bake them into images.

## Development and releases

```sh
make fmt              # write formatting changes
make tidy             # update module metadata
make lint             # read-only formatting/lint checks
make test             # local Git integration tests and race detector
make build            # dist/git-mirrorer
make release-check    # validate GoReleaser config
make container-test   # isolated local image build and smoke test
```

Tests create temporary local repositories and never use the user's configured
remotes. Install Git LFS to run payload integration tests; CI requires it. Linux
and macOS are supported; Windows binaries are not produced. CI pins golangci-lint
2.13.2 and GoReleaser 2.18.1. Release archives target amd64/arm64 on Linux/macOS.
Containers target linux/amd64 and linux/arm64. Publishing is performed by the release
workflows on version tags, with container publication also available manually.

See [the rewrite plan](docs/rewrite-plan.md) for the design contract and verification
scope. Licensed under [AGPL-3.0](LICENSE).
