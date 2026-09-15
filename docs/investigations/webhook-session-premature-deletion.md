# Webhook session deleted by a legacy cleanup worker

Investigated on 2026-09-15. Session: `47ded3f7-a589-48fd-a933-71490adb4b81`.

## Conclusion

The session was created successfully by the production GitHub webhook and then automatically deleted by the Kubernetes cleanup worker in namespace `agentapi-ui`. That worker was still running **v0.3.156**, while the Fly production API and worker ran **v0.3.186**.

The old worker interprets `session_ttl=1m` relative to `LastMessageAt`, falling back to session creation, without checking whether a oneshot session has completed. The new API exposes oneshot sessions with that TTL. Consequently, the old worker deleted this session 74 seconds after creation. Its deletion log explicitly uses the creation timestamp as the reference time.

This identifies an automatic caller; attributing the deletion to a user's browser from the API log or source IP was incorrect. The same egress IP was also observed from the agent environment.

## Evidence

All timestamps below are UTC on 2026-09-15; add nine hours for Japan time.

| Time | Evidence |
| --- | --- |
| 02:51:07 | Fly API: webhook `8c1f2119-c63f-4005-a27d-9beeee32b8d2` matched the PR trigger and logged `Session created successfully: 47ded3f7-a589-48fd-a933-71490adb4b81`. |
| 02:51:35 | Session manager: runtime `e8a1c5c8-30ff-49cd-b03a-14b57ca6da3b` reported `running`. |
| 02:52:21 | Legacy Kubernetes worker: TTL cleanup selected the public session for deletion using `last message at 2026-09-15T02:51:07Z`, threshold `2026-09-15T02:51:21Z`, and TTL `1m0s`. |
| 02:52:21 | Fly API entered its shared session deletion handler for the same public ID. |
| 02:52:22 | Session manager logged successful deletion of runtime `e8a1c5c8-30ff-49cd-b03a-14b57ca6da3b`. |
| 02:52:35 | Fly API: `Finalized queued deletion for session 47ded3f7-a589-48fd-a933-71490adb4b81`. |

Decisive log from pod `agentapi-proxy-worker-5477964c44-jqcmp` in `agentapi-ui`:

```text
2026/09/15 02:52:21 [SESSION_TTL_CLEANUP] Deleting session 47ded3f7-a589-48fd-a933-71490adb4b81 (last message at 2026-09-15T02:51:07Z, threshold 2026-09-15T02:51:21Z, ttl 1m0s)
2026/09/15 02:52:21 [SESSION_TTL_CLEANUP] Deleted session 47ded3f7-a589-48fd-a933-71490adb4b81
```

The worker's success message records acceptance of the deletion request. Workload removal and route cleanup completed asynchronously, as shown by the manager and API logs.

The collected manager log last reported `running` before deletion; it does not establish the precise runtime status at the instant of deletion. The old worker's lack of a completion check is established independently by its source code.

## Deployed configuration

Read directly from the live Kubernetes Deployment and Pod:

| Setting | Observed value |
| --- | --- |
| Deployment | `agentapi-ui/agentapi-proxy-worker` |
| Ready replicas | `1` |
| Helm release | `agentapi-proxy`, namespace `agentapi-ui` |
| Chart/version label | `agentapi-proxy-v0.3.156` / `v0.3.156` |
| Container image | `ghcr.io/ccplant/ccplant-api:v0.3.156` |
| Running image digest | `sha256:1afe313ba9b6fc85462af6e7c43edbda51dabfdf8263fb2fe221c59d09136b64` |
| Pod created | `2026-09-09T03:07:31Z` |
| Worker control API | `https://app.ccplant.com/api/v1` |
| Worker session API | `https://app.ccplant.com/api/v1` |
| Cleanup enabled | `true` |
| TTL scan interval | `1m` |
| Cleanup dry run | `false` |
| Schedule worker enabled | `false` |

Disabling the schedule worker did not disable the independently configured cleanup worker.

The Fly API binary reported `v0.3.186`. Fly API and Fly worker machine configurations both referenced image digest `sha256:8995e592596ffad28639dd3b09661c54f36b832cb3c7289d9c95055c1f47c626`. Their rollout did not update or remove this separate Helm-managed Kubernetes worker. This investigation did not establish why it was retained or the leader-election history between the deployments.

## Code path and version mismatch

The following files were compared at tags `v0.3.156` and `v0.3.186`:

1. `backend/internal/app/server.go`: the newer pool-session creation path sets `oneshot=true` and defaults `session_ttl` to `1m`.
2. `backend/internal/interfaces/controllers/worker_control_controller.go`: worker session listing exposes route-backed TTL/oneshot sessions, carrying the route start time and, separately, its status update time.
3. `backend/pkg/slackbot_cleanup/worker.go`: v0.3.156 checks only TTL and `LastMessageAt`/`StartedAt`. It has no oneshot completion guard. v0.3.186 skips oneshots unless their status is `stopped` and measures their TTL from `UpdatedAt` instead.
4. `backend/internal/infrastructure/controlapi/session_manager.go`: the worker sends `DELETE /internal/worker/sessions/{id}`.
5. `backend/internal/interfaces/controllers/worker_control_controller.go`: deletion of an externally managed route delegates to the shared session deletion handler.
6. `backend/internal/interfaces/controllers/session_controller.go`: that shared handler logs a **hard-coded** `Request: DELETE /sessions/{id}` rather than the actual incoming path. Therefore that log also appears for internal worker cleanup and does not prove a public/browser DELETE request.

The old/new predicate difference is:

```text
v0.3.156: LastMessageAt (or StartedAt) <= now - TTL
v0.3.186, oneshot: status == stopped AND UpdatedAt <= now - TTL
```

For the timestamps recorded by the old worker, `02:51:07 <= 02:51:21`, so it selected the session regardless of whether the agent had finished. The current regression test `TestOneshotCleanupRegardlessOfOriginAndExplicitTTL` in `backend/pkg/slackbot_cleanup/worker_test.go` covers retaining running oneshots with old creation timestamps; that protection cannot affect a worker still executing the older image.

## Remediation

1. Disable the legacy Kubernetes cleanup worker in the Helm deployment's source configuration, or upgrade it to a version with completion-based oneshot cleanup. If retiring the entire worker, account for any Slack Socket Mode duties it also serves.
2. Establish one intended production worker deployment and verify its cleanup leadership after the transition. Updating the Fly API/worker alone does not retire the old Kubernetes Deployment.
3. Verify that a controlled oneshot remains visible while running beyond one minute, then is cleaned up only after completion plus its configured TTL. Confirm that the legacy worker emits no further cleanup requests.
4. Improve deletion diagnostics by recording the actual route and authenticated caller category. A future server-side cleanup eligibility check could also protect against stale workers rather than trusting their deletion decision.

Disabling `oneshot` on this webhook alone would hide this compatibility issue and change the desired lifecycle; it would not retire the legacy cleanup process.

## Investigation scope and limits

This report is based on live API, Kubernetes worker/manager logs, deployment metadata, and source comparison of the deployed versions. No production configuration, webhook setting, or session lifecycle was changed. No webhook was replayed, since its configured task performs production release operations.

The deleted session's normal messages endpoint returned 404. Legacy session-control Redis streams for its public/runtime IDs were empty; this is not proof that all possible runtime storage was empty. Recovering the complete agent transcript was not required to identify the deletion caller. No claim is made that the release task completed.

This is a documentation-only investigation PR. No automated tests were run; the checks were the evidence/source comparisons described above.
