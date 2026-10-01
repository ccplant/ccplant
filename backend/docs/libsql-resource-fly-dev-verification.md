# libSQL resource migration — Fly dev verification

Date: 2026-10-01

- Implementation PR: https://github.com/ccplant/ccplant/pull/408
- Deployment workflow PR: https://github.com/ccplant/ccplant-deploy/pull/93
- Successful deployment run: https://github.com/ccplant/ccplant-deploy/actions/runs/36849747131
- Fly API: `ccplant-api-dev`
- Fly worker: `ccplant-worker-dev`
- Image: `ghcr.io/ccplant/ccplant-api@sha256:acc8281a9d5fd896889b42268c3cb055097e7cad4c8112ffdb760418c3c48206`
- Runtime version: `dev.ccplant.ece0a46497e5eb6a60db269ee8d0ef35244a1d5f`

## Snapshot

The successful run exported every Turso table to JSONL before inserting the
verification fixtures and before the API rollout. It compressed and encrypted
the export with the dev SOPS age recipient, deleted the plaintext export, and
stored the encrypted result as a private GitHub Actions artifact for 30 days.

- Artifact: `turso-before-libsql-resource-migration-36849747131`
- Artifact ID: `11155675151`
- Size: `1,253,171` bytes
- Artifact digest: `sha256:2ca03dfeb792e2daadbd56b5e0c4bc646468c673611a1b5238a89261900955ac`

An initial cancelled run exposed that Fly worker and API jobs could start in
parallel. The worker reached the new version before that run's snapshot upload.
No fixture remained because the always-run cleanup completed. The workflow was
then corrected so the API-owned snapshot and migration verification complete
before the worker rollout. Consequently, the retained artifact is a backup
immediately before the controlled 19-resource fixture migration and API
rollout; it is not a snapshot from before the cancelled worker rollout.

## Migration verification

The verifier inserted one legacy `agentapi_kv` row for each resource type in
the isolated namespace `migration-verification-36849747131-1`. Each fixture had
a distinct version and a deterministic marker value. After API startup, it
verified that the row existed only in its expected table and that `version`,
`value`, and `owner_scope` were unchanged.

All 19 tables passed:

- settings, credentials, shares, team configs, personal API keys, API tokens,
  and local users
- sandbox policies and sandbox domains
- session routes, user files, and session profiles
- SlackBots, webhooks, user/team mappings, and schedules
- Codex auth attempts, Codex auth locks, and system settings

The verification fixtures were deleted after the assertions. The Fly API
machine reached a healthy state, `/health` reported the expected runtime
version, the worker verification passed, and the complete deployment workflow
finished successfully.
