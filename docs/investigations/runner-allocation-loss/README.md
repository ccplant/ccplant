# Runner allocation loss investigation

Investigated on 2026-09-14 against code `7adad9be4dbfbfe7a39973d20352b3d6495d4f04`, following the [Fly development E2E run](../../oneshot-fly-dev-verification.md).

This report records the pre-fix investigation. See [fix verification](verification.md) for the subsequent implementation and validation.

The allocation was explicitly deleted by heartbeat inventory reconciliation. This is not a schedule-specific failure: the same runner lifecycle is used by `/start`. A single missing inventory entry removes an in-flight allocation, bypasses its lease recovery, and leaves its public session route in `starting`.

## Evidence from the failed live run

| UTC | Evidence |
| --- | --- |
| 10:09:58 | Updated Fly development API passed health checks. |
| 10:10:12–13 | Manual schedule created session `2efaf02e-8809-4227-9608-cdf81429cb74`. It was allocated runner `3889689e-b098-4ca1-8d22-fed753cf9233`. |
| 10:10:30 | API logged `Removed stale claiming runner 3889689e-b098-4ca1-8d22-fed753cf9233 absent from manager 5095c058-a00c-4a91-bc22-242f5ffb35e8 inventory`. |
| Subsequent inspection | Session remained `starting`; messages returned 503 (runtime connection unavailable), and runner logs lookup returned 404 (runner not found). |
| Cleanup | The orphaned session required explicit deletion. A separate schedule retry completed normally. |

The API log identifies the deletion path. It does not identify what first made the local Service absent. Manager-side logs or Kubernetes deletion audit events for this runner were not available in the collected evidence. A Loki label lookup for 10:09–10:11 UTC returned no labels. Therefore the initiating Service deletion is not proven.

## Confirmed implementation defects

1. `HeartbeatManager` invokes `reconcileMissingManagerRunners` for an inventory-bearing heartbeat. The inventory comes from live Services in `ListRunnerSessionIDs`; entries with deletion timestamps are omitted. The reconciler immediately deletes both `running` and `claiming` runner records and their allocations when absent. It does not check lease expiry, recent activity, repeated absence, or an inventory observation generation.
2. `ClaimRunnerAllocation` gives a claim a 45-second lease and marks the runner `claiming`. `ClaimNext` can recover an expired leased allocation, but only if the allocation still exists. Inventory reconciliation deletes that recovery record entirely.
3. Reconciliation does not update or clear the corresponding public session route. `repairManagerRoutes` iterates surviving allocations, so it cannot repair this orphan. This explains the durable `starting` entry and unavailable runtime.
4. The heartbeat response's `allocated_runner_ids` includes only `RunnerRunning`, excluding `RunnerClaiming`. `PurgeStockSessions` uses that list to protect allocated workloads. Direct runtimes retain stock labels while allocated. Before local settings/provision-request artifacts exist, a claiming runner can therefore be mistaken for disposable stock during manager startup. The same omission affects stock counting.

Relevant implementation:

- `backend/internal/interfaces/controllers/session_pool_controller.go`: `HeartbeatManager`, `ClaimRunnerAllocation`, `reconcileMissingManagerRunners`, `repairManagerRoutes`.
- `backend/internal/infrastructure/sessionrunner/kv_store.go`: `ClaimNext`.
- `backend/internal/infrastructure/services/kubernetes_session_manager.go`: `ListRunnerSessionIDs`, `fetchAllocatedRunnerIDs`, `PurgeStockSessions`, `CountStockSessionsForPool`.
- `backend/internal/app/session_manager_runtime.go`: startup purge and version-triggered manager upgrade.

## Most plausible initiating sequence, not yet proven for this incident

The API update can trigger a manager image update. Manager startup purges stock. If a runner claims work during that transition, but has not yet acknowledged or written local allocation artifacts, the parent omits it from the purge protection list. Startup cleanup can delete its Service. The following inventory heartbeat then deletes the runner and allocation records and leaves the public route behind.

A missing or outdated inventory snapshot can also trigger the final destructive reconciliation. The confirmed deletion bug does not require a restart; restart cleanup is a concrete, locally reproducible way to create the missing-Service condition. A simple grace period would reduce timing sensitivity but would not resolve all snapshot/claim races.

## Local reproduction

Two diagnostic fixtures are included as `.go.txt` files so they do not enshrine the defective behavior in the normal regression suite. They **pass when the observed bug is reproduced**, not when the system is correct.

From the repository root, with the temporary destination files absent:

```sh
cp docs/investigations/runner-allocation-loss/controller_test.go.txt backend/internal/interfaces/controllers/runner_investigation_test.go
cp docs/investigations/runner-allocation-loss/purge_test.go.txt backend/internal/infrastructure/services/runner_investigation_test.go
cd backend
go test ./internal/interfaces/controllers -run '^TestInvestigationMissingClaimingRunner$' -count=1 -v
go test ./internal/infrastructure/services -run '^TestInvestigationPurgeClaimingWindow$' -count=1 -v
rm internal/interfaces/controllers/runner_investigation_test.go internal/infrastructure/services/runner_investigation_test.go
```

Both were executed successfully with Go 1.25.0. They use fake Kubernetes clients and a local HTTP test server; no real workloads are deleted.

- Controller reproduction: a freshly leased allocation is omitted from the protection response; one empty inventory deletes the runner and allocation while the lease is still valid and leaves the route in `starting`.
- Purge reproduction: with stock labels and no local allocation artifacts, an empty parent protection list causes Service deletion. Including the runner in the parent protection list preserves the same Service.

## Recommended fix and validation

Protect in-flight claims in allocation inventory and stock cleanup, and coordinate purge decisions with claim ownership so a snapshot taken before a new claim cannot authorize deletion. Do not unconditionally delete allocation records because one manager inventory omitted the runner. Preserve lease recovery for unacknowledged claims and reconcile the public route under the same allocation generation. For acknowledged work, report a terminal failure or recover only with a policy that prevents duplicate execution.

Regression coverage should assert protection before acknowledgement, stale/empty inventory during a valid lease, recovery after genuine runner loss, and route consistency. Include a controlled manager rollout overlapping a claim in Fly development E2E. The diagnostics here prove the mechanisms separately; they are not an E2E reproduction of the exact rollout timing.

This investigation changes documentation and diagnostic fixtures only. No production fix or additional deployment was performed.
