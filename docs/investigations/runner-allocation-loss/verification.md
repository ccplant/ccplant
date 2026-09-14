# Fix verification — 2026-09-14

The fix preserves runner registrations and allocations when a manager inventory snapshot omits a workload. Missing workloads do not count toward reported pool capacity, allowing a replacement to start. Claiming runners and durable leased/claimed/running allocations are included in cleanup protection, including the interval before the runner status update.

An expired lease can be reclaimed without losing its queue record. Reclaim increments the runtime generation; route rebinding resets the previous runtime's completion status and TTL timestamp. Old lease acknowledgements are rejected. Acknowledged work is not automatically replayed merely because an inventory omitted it. Missing registration metadata is retained rather than destructively treating a snapshot as a deletion acknowledgement.

## Regression validation

Before the fix, the new preservation tests failed: claiming runners were absent from the protection response and a missing running runner was deleted. After the fix:

- An unexpired claim survives an empty heartbeat and can acknowledge successfully.
- A persisted lease is protected even before its runner changes from idle to claiming.
- Missing workloads do not consume reported pool capacity.
- After lease expiry, a replacement claims the same session, receives a newer generation, repairs its route, and resets the old completion TTL. The old acknowledgement is rejected and the new acknowledgement succeeds.
- Existing allocated-running inventory and stock-purge protection tests pass.

The controller and runner-store package suites passed locally. [Full backend CI and image builds](https://github.com/ccplant/ccplant/actions/runs/34835144346) passed for deployed code `947b8154604da4b5cd56ccf2d7dabc9316792f43`.

The opt-in `.go.txt` fixtures in this directory document the pre-fix behavior. Their original assertions are diagnostic evidence, not the current regression suite; current assertions live in `backend/internal/interfaces/controllers/runner_inventory_test.go` and `session_pool_controller_test.go`.

## Fly development deployment

API `ccplant-api-dev` and worker `ccplant-worker-dev` were updated using image-only Fly Machines updates, preserving their other configuration:

`ghcr.io/ccplant/ccplant-api@sha256:f6af34ec9848bcf5e97164e47c046686546cf17bf1870d649072a2ebbe8e8306`

Both machines reached `started`, API health passed, and the worker binary reported `dev.ccplant.947b8154604da4b5cd56ccf2d7dabc9316792f43`. The first schedule and custom webhook were triggered immediately after the new API health response (10:59:19 UTC).

The connected external manager reported the earlier `7adad9b` version during the first status check. The fix is in the updated control-plane API; these live tests do not establish that a manager restart overlapped a claim. Missing-inventory and lease-recovery faults were injected in deterministic local tests, not by deleting a shared manager's Pods.

## Live E2E

All five runs returned the expected model response, reported ACP `end_turn`, and disappeared automatically from `/search`. Each had a runner allocation, `oneshot=true`, and `session_ttl=1m`. No session status correction, manual session deletion, or retry was used.

| Path | Session ID | Automatic deletion observed (UTC) |
| --- | --- | --- |
| schedule-manual | `9cf4eb45-b08b-41a9-a508-4fa1c5bb94cc` | 2026-09-14T11:01:21.609Z |
| custom-receive | `60608454-6e80-4ec6-9fe5-65286c563150` | 2026-09-14T11:01:21.609Z |
| github-receive | `d5abc4dc-370c-40c5-bdae-7e6d934a8ee6` | 2026-09-14T11:02:22.933Z |
| schedule-automatic | `bf84dec7-8384-4242-b09d-55064f34b395` | 2026-09-14T11:03:24.210Z |
| custom-manual | `bf408dfb-8678-4335-8972-695913f547fa` | 2026-09-14T11:04:27.749Z |

Custom webhook runs read the mounted `/opt/webhook/payload.json` and returned its expected value. Custom and GitHub ingress requests were HMAC-signed; the GitHub request was a synthetic issues event using template rendering, not a real issue creation or a mounted-payload test. Deletion timestamps are observation times. Monitoring used `/search` and `/messages`, without calling a session `/status` endpoint.


## Cleanup

Both test schedules and both test webhooks were deleted; their GET endpoints returned 404. No test sessions remained. The two pre-existing user sessions were still present.
