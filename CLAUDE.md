# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build for local platform (requires goreleaser)
make build

# Run tests
make test

# Run a single test package
./scripts/go_test.sh -v -run TestName ./git/...

# Lint
make lint

# Format and tidy
make tidy

# Run the binary (after build)
make run
```

The test script (`scripts/go_test.sh`) sets `CONFIG_DIR` to the project root so tests can find `config.yaml`.

## Architecture

**Entry point:** `main.go` — loads `.env`, reads config, then either runs once (`mirror.Reconcile`) or starts a ticker loop (`mirror.StartReconcileLoop`). An optional HTTP health endpoint (`/`) runs concurrently when `web.enabled` is true.

**Config (`config/`):** Config is loaded from a YAML file (default: `config.yaml` in cwd, or via `CONFIG_PATH`/`CONFIG_DIR` env vars) and then overlaid with env vars prefixed `GM__`. The singleton is initialized once via `GetConfig()`. Key fields: `values` (list of source→mirrors pairs), `schedule.interval`, `runOnce`, `repoPath`.

**Mirror loop (`mirror/`):** `Reconcile` fans out to 8 goroutines via a jobs channel, one job per `Value` entry. Each job iterates its mirrors and calls `git.HandleMirror` for each.

**Git operations (`git/`):**
- `HandleMirror` — clones or fetches the source, then pushes to the mirror URL.
- `clone.go` — `git clone --mirror <url> <path>`
- `fetch.go` — `git fetch -p origin` (prune-enabled)
- `push.go` — sets the push URL via `git remote set-url --push origin <mirrorURL>`, then `git push --mirror`

Repos are stored locally at `<repoPath>/<url-escaped-source-url>` (e.g. `tmp/repos/git%40github.com%3Azekihan%2Ffoo.git`).

**Build:** Uses goreleaser (`build/local` for snapshot single-target). The Dockerfile copies pre-built binaries from `dist/` into an Alpine-based final image using the `golang:alpine` base (for git/ssh).

## Configuration

`config.yaml` structure:
```yaml
repoPath: tmp/repos
runOnce: false
schedule:
  interval: 15m
web:
  enabled: false
  port: 8080
log:
  level: warn
  addSource: false
values:
  - source:
      url: git@github.com:owner/repo.git
    mirrors:
      - url: git@gitlab.com:owner/repo.git
```

All `Config` fields are also overridable via env with `GM__` prefix (e.g. `GM__RUN_ONCE`, `GM__SCHEDULE_INTERVAL`).
