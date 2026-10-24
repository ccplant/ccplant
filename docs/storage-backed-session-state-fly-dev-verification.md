# Storage-backed session state — Fly dev verification

Date: 2026-10-24 UTC

Implementation commit: `e41e36adb22c5b63275752920da252b55b6c23b2`

Application PR: https://github.com/ccplant/ccplant/pull/488

Deployment configuration PR: https://github.com/ccplant/ccplant-deploy/pull/119

## Deployment

- The API, session runtime, and frontend images were built by
  https://github.com/ccplant/ccplant/actions/runs/40058496845.
- The final dev deployment completed successfully at
  https://github.com/ccplant/ccplant-deploy/actions/runs/40058847447.
- `https://ccplant-api-dev.fly.dev/health` returned HTTP 200 and version
  `dev.ccplant.e41e36adb22c5b63275752920da252b55b6c23b2`.
- The Cloudflare frontend deployment job succeeded. Direct anonymous UI inspection is blocked by
  the expected Cloudflare Access login redirect.

The first Storage create attempt returned HTTP 503 because Fly dev did not configure the object
store used by the Storage API. The deployment configuration PR reuses the existing development R2
session-state bucket, publishes its credentials to the Fly API, and keeps Storage blobs below the
existing prefix's `storages/` namespace.

## Profile and placement verification

The test used one temporary `object_snapshot` Storage and two temporary profiles. Both profiles
selected `agent_state` and `workspace`; one selected Codex and the other selected Claude.

`POST /start?dry_run=true` resolved the single Storage into two attachments for each profile:

| Profile | Agent-state attachment | Workspace attachment |
| --- | --- | --- |
| Codex | `session-state/codex` → `/home/agentapi/.codex` | `workspace` → `/home/agentapi/workdir` |
| Claude | `session-state/claude` → `/home/agentapi/.claude` | `workspace` → `/home/agentapi/workdir` |

Both dry runs selected pool `fly-dev` and the direct session runtime transport. This confirms that
one Storage can be resolved more than once when each attachment uses a distinct source path.

## Runtime persistence verification

Sessions were run sequentially so that the same writable Storage was never mounted concurrently.

| Check | Result |
| --- | --- |
| Codex agent-state upload | `session-state/codex/sessions/ccplant-state-e2e/marker.jsonl` uploaded |
| Codex workspace upload | `workspace/ccplant-state-marker.txt` uploaded |
| Codex second-session restore | Both marker contents matched |
| Claude agent-state upload | `session-state/claude/projects/ccplant-state-e2e/marker.jsonl` uploaded |
| Claude workspace upload | `workspace/ccplant-state-marker.txt` uploaded |
| Claude second-session restore | Both marker contents matched |
| Agent isolation | Codex and Claude markers remained under separate Storage prefixes |

Direct Pod inspection in namespace `ccplant-session-dev` confirmed the restored files in the new
session workloads. The provisioner image and Session Manager deployments used the matching
implementation version.

The first pass also found that a file already present when the background sync worker started was
treated as its local baseline and was not uploaded until a later modification. Commit `e41e36a`
changes the initial baseline to the manifest restored by provisioning. A second Fly dev run created
new Codex and workspace markers during the command wrapper and did not modify them afterwards;
both appeared in the Storage manifest on the first periodic sync.

All temporary sessions, profiles, and the Storage were deleted after verification.

## Local verification

- `go test ./pkg/provisioner ./internal/app ./internal/interfaces/controllers ./internal/infrastructure/services`
  passed.
- The new regression test verifies that a file present before the sync worker starts is uploaded by
  its final/first reconciliation.
- The deployment repository passed `go test ./...`; its Fly TOML parsed successfully. `actionlint`
  reported only the pre-existing constant-false condition at `.github/workflows/deploy.yml:82`.
