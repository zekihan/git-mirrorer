# Workflow security (OPS-43)

Every external Action is pinned to its full upstream commit with a release-version
comment. `.github/action-pins.json` records the official GitHub commit API tag
resolution reviewed on 2026-10-27. Existing Action versions are unchanged. This
verifies release identity, not a claim of a complete upstream source audit.

Renovate's `helpers:pinGitHubActionDigests` keeps digest updates in reviewed PRs;
Action automerge is explicitly disabled.
For each update, compare the upstream release and commit, review its changes, and
update the manifest and version comment together. The offline policy tests reject
drift between these files. Repository-level allowed-action rules remain an owner
setting; these changes do not alter repository settings.

Run locally:

```sh
python3 -m venv /tmp/git-mirrorer-policy
/tmp/git-mirrorer-policy/bin/pip install -r .github/scripts/requirements-policy.txt
/tmp/git-mirrorer-policy/bin/python -m unittest discover -s .github/scripts -p test_workflow_policy.py
/tmp/git-mirrorer-policy/bin/python .github/scripts/check_workflows.py
```

CI runs the same structural checks in the read-only checks workflow. Fixtures cover
mutable/reusable references, quoted/flow YAML, inherited secrets, checkout credential
persistence, and write-token scope. Only the release and container publisher jobs
receive their respective write permissions. Checkout does not retain credentials.
Docker Hub description credentials are passed only to the pinned description Action;
the redundant login has been removed. Existing triggers and publication checks stay
in place. The description job continues using the existing `DOCKERHUB_USERNAME` and
`DOCKERHUB_PASSWORD` secrets; image publishing continues using `DOCKER_USERNAME` and
`DOCKER_PASSWORD`. No credential values were inspected or changed.

Local validation: 14 policy tests, policy inspection of all four workflows,
actionlint 1.7.7, `make test build lint release-check`, and isolated
`MIRROR_SMOKE_IMAGE=git-mirrorer:ops43-20261027 make container-test` passed.

## Docker Hub description input wiring

The pinned description Action requires nonempty `username` and `password` inputs.
Its [input helper](https://github.com/peter-evans/dockerhub-description/blob/1b9a80c056b620d92cedb9d9b5a223409c68ddfa/src/input-helper.ts)
also accepts environment fallbacks. Before credential scoping, workflow-level
`DOCKERHUB_USERNAME` and `DOCKERHUB_PASSWORD` aliases pointed at the existing
`DOCKER_USERNAME` and `DOCKER_PASSWORD` secrets. Removing that environment exposed
stale `DOCKERHUB_*` secret references in the Action inputs.

Pass the existing `DOCKER_USERNAME` and `DOCKER_PASSWORD` secrets directly to the
pinned Action's inputs and derive the repository namespace from the same username.
Do not restore workflow-wide credential exposure or provision alias secrets.
Offline contract tests resolve inputs using synthetic values for only the existing
secret names; they reject the old aliases and missing/empty required inputs, and
verify credentials remain confined to the description step. These tests perform
no authentication or publication and never read real secret values.
