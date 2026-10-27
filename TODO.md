# Owner acceptance remaining for OPS-43

- [ ] Verify Docker Hub tokens are repository-scoped with only the necessary image
  or description write permission. Token scopes and organization controls cannot
  be established from source; no credentials or live settings were changed.
- [ ] Review protected environments, branch/tag protection and allowed-action policy
  in GitHub before enabling stricter settings. This PR does not change live access.
- [ ] After explicit publication approval and available Actions budget, validate the
  release, image publication, and Docker Hub description workflows with approved
  credentials. No publication, workflow retry, or billing changes were performed.
