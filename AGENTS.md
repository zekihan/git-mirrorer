# AGENTS.md

## Commands

- `make build`: build `dist/git-mirrorer` using Go directly.
- `make lint`: check formatting and run golangci-lint without modifying files.
- `make test`: run all unit/integration tests with the race detector.
- `go test -run TestName ./internal/git`: run a focused package test.
- `make fmt` / `make tidy`: explicitly format or update module metadata.
- `make release-check`: validate GoReleaser configuration.
- `make container-test`: build and smoke-test the image with temporary local repositories.

Git is required for integration tests. Git LFS enables payload tests and is required
in CI. Test helpers isolate HOME/Git configuration; no real remote credentials or
project config are used. Never execute the user's mirroring config as validation.

## Architecture

- `cmd/git-mirrorer`: flags, strict startup, signals, composition, and exit codes.
- `internal/config`: YAML defaults/validation, source ownership, address normalization.
- `internal/git`: Git/LFS CLI operations, bounded diagnostics, process-group cleanup.
- `internal/cache`: SHA-256 source directories, metadata, advisory locks, clone publication.
- `internal/mirror`: bounded source workers, one snapshot per source, independent destination results.
- `internal/report`: ordered text/JSON final output.
- `internal/redact`: sanitize diagnostics before reporting.
- `internal/testutil`: isolated local repository fixtures.

The executable runs once. There is no HTTP server, scheduler, global config,
configuration environment overlay, or backward compatibility layer. YAML is loaded
from `--config` (default `./git-mirrorer.yaml`). Relative paths resolve from that
file's directory. Sources are unique; destinations cannot have multiple owners or
also be sources. Fetch once, then push all destinations from the locked snapshot.
Never push an old cache after refresh failure. Continue unrelated work on failures.

Production Git operations must use the command runner, argument arrays, explicit
working directories, deadlines, and owned process groups. Keep stdout reserved for
the final report and stderr for progress. Failures must reach the process exit code.
Only `main` may exit the production process. Preserve meaningful local-Git behavior
tests; do not replace ref equality assertions with command-only mocks.

## Delivery

Linux/macOS, amd64/arm64. GoReleaser packages native binaries; Docker builds its own
binary and uses a non-root Alpine runtime with Git/LFS/SSH. Compose runs once with
persistent state and no ports. Do not push or publish unless explicitly requested.
Make commits logically separated; run lint/tests before committing. Do not add
custom author/co-author metadata or trailers to commits.
