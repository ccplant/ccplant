# Built-in session manager pool registration

## Status

Implemented as an opt-in feature. The private API compatibility role and the
new runner/ESM role remain independent protocol roles on the same manager
deployment.

## Purpose

Split deployments already connect the public API to the built-in Kubernetes
session manager through the private session-manager API. That path handles
ordinary direct sessions and settings preview, but it is not represented in the
session-runner registry. Consequently, the resolver cannot select the built-in
manager through a logical pool, and Codex device auth cannot route to it through
the external session manager control tunnel.

This design registers the built-in manager as a first-class runner manager with
the same Manager, LogicalPool, PoolSupplier, Binding, and control-tunnel
primitives used by external session managers. It is additive and opt-in.

The same session-manager deployment continues to serve both roles. The private
API remains the compatibility path for sessions that already exist, while the
ESM/runner identity makes the same deployment selectable for new sessions and
Codex auth workloads. This is a protocol-role addition, not a second manager
deployment.

## Goals

- Make the built-in session manager selectable by pool binding.
- Let Codex device auth use the built-in manager through the same authorized
  ESM route as external managers.
- Preserve the private session-manager API as the authority for settings
  materialization and direct runtime callbacks.
- Keep legacy single-process and existing split deployments unchanged.
- Keep the built-in identity stable across Pod restarts, Helm upgrades, and
  secret rotation.

## Non-goals

- Do not run Codex auth workloads inside the public API process.
- Do not remove the private `session_manager.api_url` path.
- Do not make all existing installations schedule through the built-in manager
  without an explicit configuration change.
- Do not replace per-user and team ESM registrations.

## Runtime modes

| Mode | Behavior |
| --- | --- |
| Legacy single process | The Kubernetes manager is available in-process. No registry registration is needed; the existing local Codex auth fallback remains. |
| Split, built-in registration disabled | Current behavior is preserved: normal sessions use the private manager API; Codex auth requires an enrolled ESM. |
| Split, built-in registration enabled | The same session-manager deployment serves both roles: the private API continues to own legacy sessions, and the manager is additionally registered as a runner/ESM for new pool placement and Codex auth. |
| Standalone session manager with an external parent | Existing runner/ESM behavior is unchanged. The new reconciler is a parent-side convenience, not a protocol change. |

## Identity and ownership

Use a stable manager ID supplied by Helm, not a per-Pod or generated UUID. The
default is deterministic and unique per namespace/release:

```text
builtin-<namespace>-<release>
```

### Naming

| Object | Default name or ID | Display value |
| --- | --- | --- |
| Manager record | `builtin-<namespace>-<release>` | `Built-in session manager` |
| Logical pool | `builtin` | `Built-in session manager` |
| Pool supplier | `builtin` + the manager ID | Same as the manager display value |
| Binding | A generated binding ID on pool `builtin` | `all` with role `use` by default |
| Helm parent API key | `api.sessionManager.builtin` | — |
| Helm execution-plane key | `sessionManager.builtin` | — |
| Origin marker | `builtin` | — |
| Manager kind label | `agentapi.proxy/manager-kind=builtin` | — |

The pool remains `builtin` unless a collision already exists. If a deployment
shares one parent registry with several built-in managers, its default pool name
becomes `builtin-<namespace>-<release>`. The manager ID is always
release-specific; the pool name may stay release-agnostic when there is only one
built-in manager per parent registry.

The manager is recorded as system-owned. A new `origin` field or the label
`agentapi.proxy/manager-kind=builtin` marks it as built-in. Existing records
with no origin continue to be treated as external. The parent reconciler may
update only a record whose origin is builtin or whose configured connection
token hash matches. A record created through the normal ESM UI must never be
silently adopted.

The connection token remains a secret. The registry stores only its SHA-256
hash, and API responses keep returning only `has_connection_token`.

## Registration model

Add an opt-in parent-side built-in manager registration instead of reusing the
generic user-facing enrollment endpoint as the only path. The parent already
trusts the built-in manager through the internal API token, so it can safely
bootstrap the system-owned registry entry without a one-time enrollment token.

Proposed configuration:

```yaml
api:
  sessionManager:
    url: http://agentapi-proxy-session-manager:8080
    tokenSecretRef:
      name: manager-internal
    builtin:
      enabled: true
      managerId: builtin-agentapi-default
      name: Built-in Kubernetes manager
      connectionTokenSecretRef:
        name: builtin-manager-connection
      hmacSecretRef:
        name: builtin-manager-hmac
      pool:
        name: builtin
        labels:
          runtime: kubernetes
        autoAssign: true
        explicitOnly: false
        priority: -1
        binding:
          subjectType: all
          role: use

sessionManager:
  enabled: true
  runner:
    enabled: true
    managerId: builtin-agentapi-default
    pool: builtin
    upstreamUrl: http://agentapi-proxy:8080
    connectionTokenSecretRef:
      name: builtin-manager-connection
    hmacSecretRef:
      name: builtin-manager-hmac
```

`priority: -1` keeps the built-in pool below operator-created pools that use the
normal default priority. An explicit pool request can still select it. Operators
who want built-in placement by default can raise the priority.

## Parent-side reconciler

A parent-side reconciler runs as part of public API startup and periodically
thereafter. It owns only records marked as built-in. Before pool reconciliation,
it checks the existing registry for the configured stable manager ID:

- If no such manager exists, perform the full manager, pool, supplier, and
  binding reconciliation below.
- If the manager already exists and is already a supplier of any pool, preserve
  that operator-owned placement unchanged. Skip creation and modification of the
  configured default logical pool, pool supplier, and binding.
- If the manager exists but has no pool supplier, update the built-in manager
  metadata and capabilities, then reconcile the configured default pool.

1. Ensure `Manager` with the configured ID and a connection token hash.
2. Add capabilities `runner_claim_v1`, `direct_session_runtime_v1`, and
   `codex_device_auth_v1` after the connected manager advertises or is known to
   support the route.
3. If the manager has no existing pool assignment, ensure the configured
   `LogicalPool`.
4. If the manager has no existing pool assignment, ensure the Manager is a
   `PoolSupplier` for that pool.
5. If the manager has no existing pool assignment, ensure the configured
   `Binding` for the desired subject.
6. Stop making the manager a candidate when disabled, drained, disconnected, or
   its logical pool is disabled.

The reconciler is idempotent and does not create an unbounded registry history.
It never deletes a manager or pool merely because a Pod is restarting. Disabling
the feature drains first; deletion is an explicit operator action.

## Session flow

1. Public API materializes settings with the existing
   `BuildRemoteProvisionSettings` path.
2. The resolver selects an authorized pool and binding.
3. The pool route can select either the built-in manager or an external ESM.
4. The parent enqueues a direct-runtime allocation.
5. The selected manager claims the allocation with `runner_claim_v1` and creates
   the workload.
6. Runtime messages continue to use the direct runtime route, not the private
   API session object.

The direct private-manager path remains available for:

- existing sessions;
- sessions created before the pool registration was enabled;
- deployments where built-in registration is disabled;
- local fallback when no authorized pool is available; and
- lifecycle operations that still use the private session-manager API.

The private and ESM roles coexist on the same manager process. Do not disable
the private session-manager API while any `SessionRoute` still has an empty
`ManagerID`; those routes are legacy direct-manager aliases. After all legacy
sessions drain, the private role may be removed in a later release.

## Codex device auth flow

1. The public API starts a Codex auth attempt for the caller subject.
2. The launcher calls the same authorized pool resolver used for normal
   sessions.
3. If the selected pool has the built-in manager as a healthy, connected
   supplier, the control tunnel sends the auth workload to it.
4. The built-in manager creates the short-lived Codex auth Pod and reports the
   result through the existing parent callback.

Add an optional `manager_id` to the public Codex device auth start request so
the UI can explicitly request the built-in manager. It is passed as
`RequiredManagerID` to the resolver. A request for a manager that is not an
authorized supplier of the caller's selected pool fails closed.

Also make Codex auth availability subject-aware. `configured` should be true
only when either local fallback is available or the configured pool resolution
has at least one connected supplier for the current user/team. This avoids
advertising a connected manager that has no authorized binding.

## Backward compatibility

- All new Helm fields default to disabled.
- Existing `sessionManager.externalRegistration` configurations keep working.
  If both external registration and built-in registration point to the same
  stable manager ID, the reconciler updates that existing record instead of
  creating a duplicate.
- If the built-in manager is already registered and belongs to another pool,
  its existing pool assignment wins. The default `builtin` pool creation,
  supplier assignment, and binding reconciliation are ignored. This prevents an
  upgrade from silently moving the manager into a new pool or changing its
  authorization.
- Existing ESM records remain owned by their original user or team. The
  built-in reconciler does not mutate them unless the origin and token match.
- Existing bindings and pool suppliers take precedence when they have higher
  priority. The built-in pool defaults to lower priority.
- The private API contract used by `session_manager.api_url` does not change.
- Old managers that do not support the Codex auth endpoint return `404` or
  `501`; the launcher continues to the next candidate or reports a stable
  failure.
- Settings saved before an `origin` field exists are treated as external.
- Pool assignment and binding APIs remain unchanged; the built-in manager is
  just another authorized manager in the registry.
- The same deployment can expose the private session-manager API and the ESM
  control endpoint concurrently. Existing sessions continue through the private
  path; new pool sessions use the runner path. No active session needs to be
  recreated solely because registration is enabled.

## Security and failure behavior

- Connection and HMAC secrets are read from Kubernetes Secrets and never
  returned, logged, or embedded in provision settings.
- A token mismatch is an explicit startup or reconciliation error. It never
  silently takes over another manager.
- The built-in manager is eligible only while its runner heartbeat and control
  tunnel are healthy.
- If the built-in manager is disconnected, the resolver simply loses that
  supplier. Other authorized suppliers can still serve the pool; otherwise the
  existing local fallback rules apply.
- Codex auth cannot fall back to the public API process. It either resolves an
  authorized manager route or returns `503`.

## Migration

1. Add opt-in Helm values and shared Secrets.
2. Add the parent-side reconciler and origin marker.
3. Add built-in manager awareness to the ESM list UI without treating it as a
   user-owned ESM.
4. Add subject-aware Codex auth availability and optional manager selection.
5. Keep the private API enabled until all legacy direct routes drain. A
   dual-role manager is therefore valid indefinitely during migration and may be
   retained as a compatibility mode if an operator chooses.
6. Verify:
   - old values render without new registry records;
   - enabling registration creates one stable manager, pool, supplier, and
     binding;
   - an already-assigned built-in manager keeps its original pool, supplier, and
     bindings, with a reconciliation event naming `existing_pool_assignment`;
   - an explicit pool request selects the built-in manager;
   - Codex auth routes to the built-in manager over the control tunnel;
   - sessions created before registration continue through the private path;
   - legacy sessions are still listed and can be deleted through the private
     path until drained;
   - disabling the feature drains the manager without deleting active sessions.

## Open decisions

- Whether to expose built-in manager credentials through the existing settings
  ESM API or through a new managed-manager read API.
- Whether default automatic assignment should be disabled or lower priority by
  default. This design recommends lower priority because it minimizes surprise
  while still allowing explicit selection.
- Whether connection-token rotation needs a two-hash overlap window before
  rollout. It can be deferred if initial deployments treat rotation as a
  managed rollout.
