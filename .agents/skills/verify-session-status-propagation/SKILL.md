---
name: verify-session-status-propagation
description: Verify session status normalization and timestamp-ordered propagation across KubernetesSession, Redis, replicas, and SSE. Use whenever changing session status values, SetStatus/SetStatusSilentAt, status callbacks, Redis status persistence, cross-pod subscribers, or remote runtime status recording.
---

# Verify session status propagation

The user-facing promise is: `running`, `active`, and every other documented session state reaches replicas and SSE consumers without an older, duplicate, conflicting, or unknown event moving the session backwards or introducing an invalid state.

Run from the repository root:

```bash
verification/session_status_propagation/verify.sh
```

The command runs TLC 1.8.0 and the Go tests tied to the real normalization, Redis compare-and-set, in-memory replay, SSE relay, and remote-route code. Java, curl, mise, and network access for the first TLC download are required. The downloaded jar is cached outside the repository.

## Reading a result

- `Model checking completed. No error has been found.` means all event orders in the bounded model preserve monotonic versions and canonical statuses.
- `VersionNeverRegresses` failing means a replica accepted an older event after seeing a newer one.
- `OnlyCanonicalStatusPropagates` failing means an unrecognized status reached replica-visible state.
- A Go failure identifies the concrete normalization, Redis, replica, or controller path that drifted from the model.

For every TLC result, use `/statechart`. On failure it renders the shortest counterexample in repository terms; on success it draws the verified state chart. Do not weaken an invariant to make a failure pass. Decide whether the implementation violates the promise or the model no longer describes the intended status protocol.

## Code covered by the model

- `backend/internal/domain/entities/session_status.go`: `NormalizeSessionStatus`
- `backend/internal/infrastructure/services/kubernetes_session.go`: `SetStatus`, `SetStatusSilentAt`
- `backend/internal/infrastructure/services/kubernetes_session_manager.go`: `consumeStatusEvents`, `broadcastStatusChangeLocal`, `broadcastStatusChange`
- `backend/internal/infrastructure/repositories/redis_status_repository.go`: `SetStatus`, `GetStatus`
- `backend/internal/usecases/ports/repositories/status_event_repository.go`: `StatusChangeEvent`, `StatusEventRepository`
- `backend/internal/interfaces/controllers/session_controller.go`: `RecordRemoteSessionStatus`

The TLA+ model treats a local state transition and each replica's event application as atomic. It explores every delivery order, including duplicates, over three replicas, two ordered valid events (`active` at version 1 and `running` at version 2), and one invalid event. It proves that each replica retains the greatest valid version it has observed and exposes only a canonical status. The Go tests additionally exercise real timestamps, equal-time conflicts, Redis optimistic transactions, runtime `stable` normalization, lifecycle states, and SSE relay behavior.

It does not prove wall-clock synchronization between hosts, guaranteed delivery after a Pub/Sub channel drops an event, authorization, Redis availability, or unbounded replica/event counts. Timestamp ordering assumes transition timestamps from a session's owning process are monotonic enough to order that session's events.

## Keeping the model synchronized

Last synchronized against implementation commit `bcf239eba8fdded7224747ca7129443b577026b2`.

When any covered function changes, compare its accepted status vocabulary, timestamp ordering, atomic boundary, and broadcast behavior with `verification/session_status_propagation/SessionStatusPropagation.tla`. Update the model and Go tests for intended behavior, rerun the verifier, and move the synchronization commit forward only after confirming the model describes the code at that commit.

Before trusting changes to this skill, prove it can fail. On a scratch change, remove the stale-event rejection in `KubernetesSession.SetStatusSilentAt`, run `go test ./internal/infrastructure/services -run TestSetStatusSilentAtRejectsOutOfOrderAndConflictingEvents -count=1`, confirm the older event is accepted and the test fails, restore the guard, then run the full verification command until both TLC and Go pass.
