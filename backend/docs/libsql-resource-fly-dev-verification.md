# libSQL resource migration — Fly dev verification

Date: 2026-10-01

- Implementation PR: https://github.com/ccplant/ccplant/pull/408
- Deployment workflow PR: https://github.com/ccplant/ccplant-deploy/pull/93
- Successful deployment run: https://github.com/ccplant/ccplant-deploy/actions/runs/36849747131
- Fly API: `ccplant-api-dev`
- Fly worker: `ccplant-worker-dev`
- Image: `ghcr.io/ccplant/ccplant-api@sha256:acc8281a9d5fd896889b42268c3cb055097e7cad4c8112ffdb760418c3c48206`
- Runtime version: `dev.ccplant.ece0a46497e5eb6a60db269ee8d0ef35244a1d5f`

## Rollout ordering

An initial cancelled run exposed that Fly worker and API jobs could start in
parallel. The worker reached the new version before the API-owned verification.
No fixture remained because the always-run cleanup completed. The workflow was
then corrected so migration verification completes before the worker rollout.

The temporary GitHub Actions snapshot artifact used during this development
verification was deleted, and the deployment workflow does not export database
contents or upload database snapshots as workflow artifacts.

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
