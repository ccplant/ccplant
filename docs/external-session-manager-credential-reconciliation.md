# External Session Manager credential reconciliation

Status: proposed

Tracking issue: [#296](https://github.com/ccplant/ccplant/issues/296)

## Problem

An External Session Manager (ESM) installation and its parent registration are
two independently durable resources:

- Kubernetes keeps a manager ID and connection token in the release Secret.
- The parent keeps the manager registration, a hash of the connection token,
  ownership, pool suppliers, and bindings.

Restoring or replacing either side independently can leave a healthy ESM Pod
retrying credentials which the parent cannot authenticate. A restart cannot
repair this condition. Today the resulting `offline` status does not distinguish
credential drift from an ordinary network or process outage.

The installation CLI already probes the manager runtime-profile endpoint and can
re-enroll with an owner API key. The remaining gaps are an unambiguous identity
match, safe credential cutover, rollout verification, and actionable runtime
diagnostics.

### Primary root cause

A generic registration-token request creates a new Manager record. The store
assigns a new UUID when that record has no ID. If a deployment pipeline obtains a
fresh generic registration token for every update and passes it to
`session-manager install`, the installer enrolls that new record even when the
Kubernetes Secret already contains credentials for the previous installation.
The resulting manager ID change is therefore expected from the current API
semantics, but incorrect for an update workflow.

Credential drift after an independent datastore or Secret restore is a secondary
case. The primary invariant this design establishes is:

> Updating one installation MUST rotate credentials or configuration on the same
> Manager record; it MUST NOT create a new Manager ID.

### Current trigger conditions

The ID changes when the installer enters enrollment and the registration token is
bound to a newly created Manager. In the current implementation this happens in
the following update scenarios:

| Update scenario | Current result |
| --- | --- |
| Existing Secret and no registration token; stored pool/upstream unchanged; credential probe succeeds | Reuses the existing ID. |
| Existing Secret and a generic registration token is supplied | Enrolls the newly created Manager behind that token and overwrites the Secret with its new ID. |
| Existing Secret but pool or upstream differs | Skips the reuse path. Automatic token resolution can create a new Manager when its name/pool/namespace/release lookup no longer matches. |
| Existing Secret but credential probe fails and metadata lookup finds no exact parent match | Automatic recovery creates a new Manager and replaces the ID. |
| Connection Secret was deleted, renamed, or is absent in a replacement cluster | Treats the run as an initial enrollment and creates a new Manager unless the supplied token targets an existing ID. |
| Helm-only upgrade or Pod restart with the retained Secret and values | Keeps the existing ID. |

Issuing a generic token alone creates a pending Manager, but the running
installation changes identity only when that token is subsequently consumed by
the installer and the returned credentials are written to its Secret. A targeted
`POST /session-managers/{id}/registration-token` token rotates credentials on the
specified Manager and therefore does not change its ID.

## Goals

- Detect an unknown manager ID or rejected connection token during install and
  upgrade.
- Keep the manager ID stable across ordinary install reruns, Helm upgrades,
  credential rotation, and re-enrollment.
- Reconcile to the existing, owned parent manager while retaining its ID, pool
  suppliers, and bindings.
- Avoid exposing credentials in API responses, logs, events, and command output.
- Avoid a failed Secret write or rollout immediately invalidating the last usable
  credential.
- Give unattended deployment tooling machine-readable failure reasons.

## Non-goals

- Reconstruct registrations, suppliers, or bindings which are absent from the
  restored parent datastore.
- Treat a stable installation ID as an authentication credential.
- Automatically adopt a similarly named manager or create a replacement when
  identity is ambiguous.
- Make a parent datastore transaction and a Kubernetes API update literally
  atomic. The protocol instead provides staged, reversible cutover.

## Identity model

Add `installation_id` to the parent Manager record. It is an opaque, non-secret
identifier generated once by the installer and stored in the Kubernetes Secret.
The default may remain `<namespace>/<release>` for compatibility, but new
installations SHOULD use a random UUID and record namespace and release as labels
for display only.

`installation_id` is unique within `(scope, owner_id)`. It is set on first
enrollment and is not changed by re-enrollment. Existing registrations acquire it
on their first successful authenticated reconciliation. Name, pool, namespace,
and release are not identity keys because each can legitimately change.

Possession of `installation_id` proves nothing. Reconciliation requires both:

1. authentication as an owner or administrator, and
2. authorization to manage the matched registration.

## Idempotent registration

Generic `POST /session-managers/registration-tokens` remains a create operation and
is used only for the first installation. Updates use an owner-authenticated,
idempotent installation endpoint:

```text
PUT /session-manager-installations/{installationId}/registration-token
```

The request contains ownership scope, display metadata, and the intended pool. In
one datastore transaction the parent:

1. finds a Manager with the same `(scope, owner_id, installation_id)`;
2. creates one only when no such Manager exists;
3. otherwise issues a registration token bound to that existing Manager ID; and
4. returns `manager_id`, `created`, and the one-time registration token.

Concurrent requests for the same installation are serialized by the unique
identity constraint. Both callers observe the same manager ID; the newest token
supersedes any older unused registration token.

Enrollment consumes the token and rotates credentials on the token's Manager
record. It never generates or substitutes a manager ID. Changes to name, labels,
pool supply, or chart version do not affect identity.

For callers which already know the manager ID,
`POST /session-managers/{id}/registration-token` remains supported and has the same
update semantics. The UI and generated deployment commands must use one of these
targeted operations after initial creation; they must not generate a fresh generic
registration for an update.

## Parent API

### Credential probe

Keep the authenticated runtime-profile request for normal startup, but attach a
stable error code to authentication failures:

| HTTP status | `code` | Meaning |
| --- | --- | --- |
| 404 | `manager_registration_not_found` | Configured manager ID is absent. |
| 401 | `manager_credential_rejected` | Manager exists but the token is invalid. |
| 409 | `manager_identity_mismatch` | Token belongs to a different manager or installation. |

The response includes the requested manager ID and documentation URL, never a
token or token hash. The parent MUST NOT scan all manager token hashes and silently
accept a token under a different path ID; doing so hides drift and makes routing
identity depend on which endpoint is called.

### Reconciliation endpoint

Add an owner-authenticated endpoint:

```text
POST /session-managers/reconciliations
```

Request:

```json
{
  "installation_id": "b55df6f7-...",
  "expected_manager_id": "5095c058-...",
  "scope": "team",
  "team_id": "platform",
  "proof": {
    "manager_id": "5065f4fa-...",
    "connection_token": "..."
  }
}
```

`proof` is optional and is only used to improve drift diagnosis. The endpoint is
authenticated with the owner API key regardless of whether the old credential is
valid. Sensitive request fields must be redacted from access and audit logs.

The parent looks up exactly one manager by owner and `installation_id`. The
optional expected ID is a compare-and-swap guard. No match returns 404; multiple
legacy matches return 409 and require `--manager-id`; an ownership mismatch is
reported as 404. The operation creates a pending credential generation and returns
the existing manager ID, a one-time connection token, and a reconciliation ID.
It does not change the manager ID or any supplier/binding records.

```json
{
  "reconciliation_id": "rec_...",
  "manager_id": "5095c058-...",
  "connection_token": "...",
  "expires_at": "..."
}
```

Add two owner-authenticated completion operations:

```text
POST   /session-managers/reconciliations/{id}/commit
DELETE /session-managers/reconciliations/{id}
```

Until commit, both the current and pending credential generations are accepted for
that manager. Commit revokes the old generation. Pending generations expire after
15 minutes and are deleted without affecting the current credential. Only one
pending reconciliation is allowed per manager; creating another invalidates the
older pending generation.

These endpoints should be auditable with actor, manager ID, installation ID,
outcome, and timestamp, but no credential material.

## Installer workflow

`ccplant session-manager install` becomes the supported reconcile entry point;
`ccplant session-manager reconcile` may be added as an explicit alias for operators
who do not want a chart version change.

1. Read the retained Secret and validate all required fields locally.
2. If the Secret exists and its credential is valid, reject a generic create token
   supplied by an update pipeline with guidance to use the targeted
   installation-token endpoint. Configuration-only updates reuse the Secret.
3. Probe the exact configured manager ID with its connection token.
4. If valid, run Helm and proceed to rollout verification.
5. If the probe returns a credential drift code, stop with a concrete recovery
   message unless an owner API key is available or `--reconcile` was supplied.
6. Call the reconciliation endpoint using the Secret's installation ID. Refuse an
   ambiguous or missing match; never fall back to name/pool/labels automatically.
7. Assert that the returned manager ID equals the existing Secret's manager ID.
   Changing it requires a separate explicit `--replace-manager` workflow and is
   never part of an update or reconciliation.
8. Update the Secret with the returned existing manager ID and pending token using
   Kubernetes `resourceVersion` as a compare-and-swap guard. Preserve a local
   in-memory copy of the prior Secret for rollback during this command.
9. Run `helm upgrade --install` so `runner.managerId` is rendered from the same
   credential result written to the Secret.
10. Wait for the Deployment rollout, then probe with the new ID/token and wait for
   the parent to observe a heartbeat from that manager generation.
11. Commit the reconciliation. If steps 8-10 fail, restore the prior Secret where
   safe, abort the pending generation, and report both the primary and rollback
   outcomes.

The command prints manager ID, installation ID, phase, and recovery commands. It
must not print tokens. `--output=json` returns stable phase and error-code fields
for CI. `--dry-run` performs discovery and authorization checks without issuing a
pending credential or changing Kubernetes.

Because Helm values are not a credential authority, `runner.managerId` must always
be generated by the installer from the same Secret state. Direct chart users get a
preflight Job (or an equivalent documented CI command) that probes the configured
ID/token before a rollout is considered successful.

## Runtime behavior

An ESM classifies heartbeat and control-channel failures as follows:

- network errors, 429, and 5xx remain retryable with capped exponential backoff;
- the three credential drift codes mark readiness false and emit one rate-limited
  structured error containing manager ID, installation ID, parent URL, and the
  reconcile command template;
- liveness remains true so Kubernetes does not hide the terminal condition behind
  a restart loop;
- a successful authenticated request clears the condition.

The UI and `/session-pools/status` expose a non-secret condition such as
`credential_rejected` separately from generic `offline`. The condition is derived
from recent rejected requests and expires, so stale diagnostic state cannot remain
after recovery.

## Restore requirements

Parent backups must treat managers, credential generations, logical pools,
suppliers, and bindings as one consistency unit. A restore runbook must require a
post-restore reconciliation check for every installed ESM. If a manager record or
its suppliers/bindings were not restored, the CLI reports that recovery is not
lossless and requires an explicit create/adopt decision.

## Compatibility and rollout

1. Add nullable `installation_id` and credential-generation storage; retain the
   current token hash as generation zero.
2. Return coded authentication errors and add runtime/UI diagnostics.
3. Release the staged reconciliation API and updated CLI.
4. Backfill installation IDs only through a successful existing credential or an
   owner-authorized reconcile. Do not infer them from labels in a migration.
5. Remove token-hash scanning fallback after the CLI version containing
   reconciliation is broadly deployed.

Old ESMs continue using the current credential. During a staged reconciliation,
they can continue with the old generation until commit or pending expiry.

## Test plan

- Controller tests for exact-ID authentication and each stable error code.
- Authorization tests across user, team, system, wrong-owner, and administrator
  callers.
- Repository tests for installation-ID uniqueness, pending expiry, commit, abort,
  and concurrent reconciliation.
- Installer tests for Secret compare-and-swap conflict, Helm failure rollback,
  rollout timeout, failed commit, ambiguous legacy identity, and token redaction.
- Repeated-update integration test: request an installation-scoped token and run
  install multiple times, asserting every response, Secret, Helm value, supplier,
  and heartbeat uses the original manager ID.
- Regression test: pass a generic create token while an existing valid Secret is
  present and assert that install refuses to replace the manager implicitly.
- Runtime tests proving readiness becomes false without a liveness restart loop and
  that diagnostic events are rate limited.
- Integration test: retain the Kubernetes Secret, restore a parent snapshot whose
  copy of the same manager has an older credential, run reconcile, and assert that
  the original manager ID, supplier IDs, and binding IDs are unchanged and status
  becomes online.
- Integration test: restore a snapshot without the registration and assert that
  reconcile refuses to create or adopt a manager implicitly.

## Acceptance criteria mapping

- Install/upgrade detects missing or mismatched parent state through the exact-ID
  probe and rollout heartbeat check.
- Repeated updates retain the first enrollment's manager ID.
- Operators receive a coded error and a copyable reconciliation command instead of
  an indefinitely unexplained offline state.
- Reconciliation updates credentials on the existing manager, so suppliers and
  bindings remain unchanged.
- The parent-restore integration scenarios above cover both recoverable drift and
  irrecoverable missing state.
