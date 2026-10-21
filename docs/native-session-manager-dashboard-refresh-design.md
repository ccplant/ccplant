# Native Session Manager dashboard refresh and deletion consistency design

## Background

The macOS dashboard was built against the first native External Session Manager
(ESM) contract. The daemon and registration model have since gained ownership,
assignment, labels, runtime-profile inheritance, and richer session lifecycle
metadata, while the dashboard still exposes the original subset.

Session deletion also has an inconsistent completion boundary. Today
`NativeSessionManager.DeleteSession` changes the in-memory status to `stopped`,
starts process and directory cleanup in a goroutine, and immediately returns
success. It does not persist the intermediate status. Until that goroutine
removes the session directory, `native session-list` continues to read the old
`runtime/state.json`; if the process previously exited, the dashboard therefore
continues to show the deleted session as `error`. The caller can also observe a
successful DELETE before local cleanup has completed.

## Goals

- Make the native dashboard expose the current native ESM configuration and
  session lifecycle model.
- Make a successful native session deletion mean that the session is absent
  from both the manager's in-memory list and its durable state directory.
- Preserve useful diagnostics for sessions that end by themselves; terminal
  sessions are not removed merely because they are in `error` or `stopped`.
- Keep the CLI JSON contract forward compatible and make it the single boundary
  between the Tauri shell and the Go implementation.
- Keep deletion safe, idempotent, and recoverable across daemon restarts.

## Non-goals

- Redesign the parent ccplant web application.
- Add remote session control to the native dashboard.
- Change the public parent API's session status vocabulary.
- Automatically delete naturally completed or failed sessions.
- Replace the Tauri sidecar/CLI integration with direct filesystem access.

## Current gaps

### Configuration

`native install` and `nativeDaemonConfig` support values the dashboard cannot
display or configure:

- owner scope and team ID;
- manager labels;
- automatic assignment;
- runtime-profile inheritance;
- arbitrary manager environment values (the UI currently edits only `PATH`).

The status DTO also omits `listen`, ownership, assignment, and runtime-profile
inheritance, so the UI cannot accurately describe an installed instance.

Manager name currently has a more fundamental lifecycle gap. Initial enrollment
accepts and persists `name`, but `nativeDaemonConfig` does not retain it and
`native status` does not return it. Token rotation then enrolls with
`name = manager_id`, which can replace the operator-selected name. The current
setup field is therefore not a durable, manageable identity even though it
appears to register successfully.

The current unified Session Manager enrollment endpoint also ignores the
`name` sent by `native install`; it keeps the name chosen when the registration
token was issued. Enrollment must explicitly accept the native name before the
field in the setup UI can be considered effective.

### Sessions

The durable native state already contains `updated_at`, `last_message_at`,
`status_message`, and `description`, as well as request ownership and tags. The
CLI DTO and dashboard reduce this to ID, PID, start time, status, and log path.
This hides the information an operator needs most when a session is delayed or
failed.

### Deletion

The current ordering is:

```text
mark stopped in memory -> return success -> stop process -> remove directory
```

`state.json` is not rewritten after the in-memory transition. The CLI reads the
directory rather than the manager map, so UI state can move backwards from the
successful delete response to the old persisted `error` state.

## Proposed contracts

### Manager identity and name lifecycle

Treat these identifiers as separate concepts throughout the stack:

- `manager_id`: immutable server-generated technical identity;
- `instance`: local installation selector such as `default`;
- `instance_id`: stable identity used to make enrollment idempotent;
- `name`: operator-selected display name, mutable without re-enrollment.

Add `Name` to `nativeDaemonConfig`, populate it after enrollment from the
canonical server response, and include it in `native status --json` and
`native list --json`. All lifecycle operations, especially token rotation and
update, preserve this value. They must never derive a display name from
`manager_id`.

Post-install rename remains an authenticated control-plane operation. A future
manager self-update operation may allow it directly from the native dashboard;
the initial implementation makes enrollment authoritative and keeps the name
stable through local lifecycle operations. The preferred future API shape is:

```http
PATCH /external-session-managers/{manager_id}/self
Authorization: Bearer <connection token>

{
  "name": "Mina's Mac Studio",
  "labels": {"pool": "local-arm64"},
  "automatic_assignment_enabled": true
}
```

The endpoint may update only the manager represented by that connection token;
it cannot change owner scope, team, manager ID, or instance ID. It validates a
trimmed display name (1-100 Unicode characters), labels, and assignment policy,
then updates both Settings storage and the session-runner registry atomically
from the caller's perspective. On a partial storage/registry failure it returns
an error and reconciliation repairs the secondary representation.

Expose this through a `native configure` CLI command and a Tauri command rather
than having React call the parent API directly. After a successful update, save
the returned canonical name/labels/assignment values to local config atomically.
If the parent is unavailable, retain the previous local values and show the
failed update; do not present an unsynchronized local rename as successful.

For compatibility, an existing config without `name` obtains the canonical
name from a new authenticated manager metadata GET during status/configure. If
that lookup is unavailable, the UI displays the manager ID as a fallback but
labels it as an ID, never as the configured name. The next successful configure
or token rotation backfills the local name.

Token rotation uses the existing server-side manager name as its source of
truth and must not mutate registration metadata. Prefer a rotation response
that only changes the credential; if the existing enrollment exchange must be
retained temporarily, send the canonical stored name and verify with a
regression test that it is unchanged.

### Instance status DTO

Extend `native status --json` additively:

```json
{
  "instance": "default",
  "service": "running",
  "manager_id": "manager-id",
  "name": "Mina's Mac Studio",
  "upstream": "https://ccplant.example.com",
  "listen": ":8080",
  "scope": "team",
  "team_id": "team-id",
  "labels": {"pool": "native"},
  "automatic_assignment_enabled": true,
  "inherit_runtime_profile": true,
  "version": "v0.3.310",
  "filesystem_sandbox": true,
  "active_sessions": 1,
  "health": "ok",
  "state": "/path/to/state"
}
```

All new Rust and TypeScript fields use serde/default or optional values so the
new app can still inspect an older bundled/installed manager and show an
"unsupported by this version" value instead of failing deserialization.

### Session summary DTO

Extend `native session-list --json` with an intentional summary rather than
exposing the full persisted `RunServerRequest`:

```json
{
  "id": "session-id",
  "pid": 1234,
  "started_at": "...",
  "updated_at": "...",
  "last_message_at": "...",
  "status": "error",
  "status_message": "process exited with status 1",
  "description": "Fix the failing build",
  "scope": "team",
  "team_id": "team-id",
  "user_id": "user-id",
  "tags": {"repository": "owner/repo"},
  "log_path": "/path/to/provisioner.log"
}
```

Secrets and provision settings are never included. Missing request metadata on
legacy state files produces empty/default values. Sessions are returned newest
first, with ID as a stable secondary sort key.

### Delete completion contract

`NativeSessionManager.DeleteSession(id)` becomes synchronous with respect to
local visibility and storage. A nil result guarantees:

1. the process group no longer exists;
2. the session root has been removed (or was already absent);
3. the entry is absent from `sessions` and `provisionRequests`.

The operation remains bounded by the existing graceful termination deadline.
It sends SIGTERM, waits up to ten seconds, sends SIGKILL when necessary, waits
for process disappearance/reaping, and removes the directory. Cleanup errors
are returned instead of being discarded.

## Deletion sequence and concurrency

```text
DELETE
  -> lock manager, find session
  -> mark session "terminating" (deletion fence)
  -> remove pending provision request
  -> unlock
  -> cancel context and terminate process group
  -> remove session root
  -> lock manager and remove the same session pointer
  -> unlock and return success
```

The entry remains in the manager map while cleanup runs. This prevents another
create with the same ID and makes concurrent delete calls join or observe the
same deletion rather than racing a new session. Add a per-session deletion
completion channel/result guarded by `sync.Once` (or an equivalent small
lifecycle object): the first caller performs cleanup and other callers wait for
its bounded result.

The process `Wait` goroutine must treat `terminating` and `stopped` as fenced
states. It must not overwrite either with `error`, and must not persist state
after deletion has started. This closes the race in which normal process exit
handling recreates or rewrites `state.json` during cleanup.

The terminal in-memory value after process termination is `stopped`; it exists
only until the map entry is removed. `terminating` is appropriate while a
requested delete is in progress, whereas `error` remains reserved for an
unrequested process failure.

### Failure behavior

- If process termination fails, retain the fenced session in memory, return an
  error, and allow a later DELETE to retry cleanup.
- If directory removal fails after process termination, retain the session as
  `stopped` with a cleanup-specific status message and return an error. The UI
  shows an actionable cleanup failure rather than the old process error.
- A repeated DELETE for an already removed session is successful. This matches
  the parent deletion flow's idempotent semantics.
- Validate the session ID before deriving a cleanup path. Never accept path
  separators or clean a path outside `<stateDir>/sessions`.

### Restart recovery

`restoreSessions` must not restore `terminating` or `stopped` state as a live
session. It attempts bounded process termination when the recorded PID still
matches that session root, removes the directory, and continues startup. A
failed recovery is logged with the session ID and path and leaves the directory
for explicit retry; it is not silently reported as a healthy session.

## Dashboard information architecture

The dashboard's primary job is host operation: answer "is this manager ready?",
"what is running?", and "what needs attention?" quickly. Preserve the compact
macOS utility character, but replace the current sequence of equally weighted
cards with a clear operational hierarchy.

```text
+-------------------------------------------------------------+
| ccplant / instance                         Refresh  Actions  |
| online  v0.3.310   1 running   updated 12:41                |
+-------------------------------+-----------------------------+
| Manager                       | Configuration               |
| upstream, owner, assignment   | labels, sandbox, runtime    |
+-------------------------------------------------------------+
| Sessions (1 running / 2 retained)              filter       |
| status  description / id        activity        process     |
| > expanded failure detail + View logs                       |
+-------------------------------------------------------------+
| Diagnostics / environment (collapsed when healthy)          |
+-------------------------------------------------------------+
```

Design direction:

- Use the existing system typography so the utility feels native to macOS;
  reserve monospace for IDs, versions, paths, and log content.
- Use neutral graphite surfaces (`#F4F5F7`, `#FFFFFF`, `#20242B`) with blue
  (`#2F6FEB`) only for selected/control state, green (`#238636`) for healthy,
  amber (`#9A6700`) for transitional, and red (`#CF222E`) for actionable
  failure. Status color is always paired with text or an icon.
- Remove decorative all-caps card headings and identical shadows. Use spacing
  and a small number of dividers to communicate hierarchy.
- Make the session list the dominant surface. Show description first, full ID
  on expansion/copy, relative last activity, and PID as secondary detail.
- Use sentence-case, action-specific copy: "View logs", "Restart manager",
  "Update manager", and "Remove instance".
- Collapse healthy doctor/environment details; automatically open the relevant
  section when a check fails.
- At narrow widths, render session rows as stacked records instead of forcing a
  four-column table.
- Preserve keyboard focus visibility, semantic tables/list controls, reduced
  motion, and color-scheme support.

## UI behavior

### Status model

Map session states by meaning rather than enumerating only the original three:

- healthy/active: `running`, `active`, `stable`;
- transitional: `creating`, `starting`, `queued`, `terminating`, `stopping`;
- unsuccessful terminal: `error`, `failed`;
- successful terminal: `exited`, `stopped`, `terminated`;
- unknown: neutral, displaying the server value verbatim.

The active count continues to come from `native status`; the retained session
count comes from the list. This avoids labelling failed-but-inspectable sessions
as active.

### Current configuration

The setup form labels `name` as **Manager name** and explains that this is the
name shown in ccplant when choosing or inspecting a Session Manager. After
installation it remains editable in a **Manager identity** section alongside
the read-only manager ID, local instance, owner, and connection status. Saving
the name updates the parent registration and then the local config; success is
confirmed with the canonical value returned by the server.

Ownership continues to come from the one-time registration token rather than a
second, conflicting native form control. The setup form gains labels and
runtime-profile inheritance. Advanced values are grouped but not hidden after
installation. Manager environment editing is
generalized to key/value entries while retaining a purpose-built PATH editor
and command checks.

The form must mirror CLI validation. Tokens remain write-only, are cleared from
React state after submission, and are never included in errors or diagnostics.

### Refresh and deletion

Auto-refresh requests are serialized per instance. An older response may not
replace data from a newer request or from a newly selected instance. Switching
instances cancels/invalidates outstanding results.

Once the backend delete contract is fixed, a completed delete cannot reappear
from disk. The UI still keys rows by session ID and preserves a selected log
target only while that ID remains in the refreshed list; otherwise it returns
to daemon logs.

## Implementation slices

1. **Deletion consistency**
   - Refactor native deletion into a bounded, synchronous cleanup operation.
   - Add the deletion fence, concurrent-delete joining, idempotency, error
     propagation, and restart recovery.
   - Add focused race and filesystem tests before changing the UI.
2. **Manager identity contract**
   - Persist the canonical manager name locally and return it from status/list.
   - Add manager-authenticated metadata read/update, registry reconciliation,
     and `native configure`.
   - Prevent update and token rotation from changing the configured name.
   - Keep ownership and pool assignment controlled by the parent registration
     and pool APIs rather than duplicating ineffective native controls.
3. **CLI contracts**
   - Extend status/session DTOs and tests.
   - Keep JSON additions backward compatible and update human-readable output
     only where it improves diagnostics.
4. **Tauri boundary**
   - Add optional/defaulted Rust DTO fields and contract fixtures for old and
     new CLI JSON.
5. **Dashboard state and controls**
   - Add the new TypeScript fields, stale-request protection, configuration
     inputs, status grouping, session details, and log-selection behavior.
6. **Visual hierarchy and accessibility**
   - Apply the operational layout, responsive session records, focus states,
     dark mode, and reduced-motion behavior.
7. **Documentation**
   - Update `native/README.md` and the session-manager guide with the current
     fields, deletion guarantee, and failure recovery guidance.

## Test plan

### Go unit and integration tests

- Deleting running, failed, and already-exited sessions removes map entries and
  state directories before returning.
- A deleted failed session never appears in `readNativeSessionList` after a
  successful return.
- The `Wait` goroutine cannot overwrite `terminating`/`stopped` with `error` or
  recreate state during cleanup; run this under `go test -race`.
- Concurrent DELETE calls produce one cleanup and the same successful result.
- Repeated DELETE after completion succeeds.
- SIGTERM timeout escalates to SIGKILL and remains bounded.
- A removal failure returns an error and leaves retryable, truthful state.
- Restart cleanup handles `terminating` and `stopped` records without restoring
  them as active sessions.
- A malicious session ID cannot escape the session state root.
- Old state files decode; extended CLI JSON contains no provision settings or
  secrets and sorts deterministically.
- Initial enrollment preserves the requested manager name in Settings, the
  session-runner registry, local config, `native status`, and `native list`.
- Renaming updates Settings and the session-runner registry and returns the
  canonical value to local config and UI.
- Update, heartbeat, restart, and token rotation do not change the name.
- Automatic assignment is persisted and read back; unknown enrollment JSON is
  not mistaken for a successful setting update.

### Rust tests

- Old CLI JSON without additive fields deserializes successfully.
- New status and session fixtures round-trip into the Tauri DTOs.
- Command argument tests cover ownership, labels, automatic assignment, and
  runtime-profile inheritance without exposing the registration token.

### Frontend tests

- Every known lifecycle state maps to the intended semantic presentation.
- Failed sessions show status details and remain available for logs.
- Removed sessions clear the selected log target.
- An older refresh cannot overwrite a newer refresh or another instance.
- Setup validation covers personal/team ownership and label syntax.
- Responsive and keyboard navigation checks cover the session list and
  collapsible diagnostics.

### Manual verification

1. Start a native session and confirm the dashboard shows its description,
   ownership, activity, and running process.
2. Force the provisioner to exit and confirm the retained session shows the
   actual error and its logs.
3. Delete that session from the parent and confirm it disappears on the first
   refresh after DELETE succeeds and does not return.
4. Repeat deletion and restart the daemon during deletion to verify idempotency
   and recovery.
5. Verify personal and team installs, labels, automatic assignment, runtime
   profile inheritance, light/dark mode, and a narrow window.

## Acceptance criteria

- The dashboard can display and configure every supported native installation
  field listed in this design.
- The operator-selected manager name is visible after installation, can be
  changed later, and survives restart, update, and token rotation; manager ID,
  instance, and instance ID remain clearly distinct and immutable.
- Session rows expose current lifecycle and diagnostic metadata without leaking
  secrets or the raw provision request.
- After native DELETE returns success, the session is absent from memory,
  `native session-list`, and the dashboard.
- A deletion cleanup failure returns a failure and displays cleanup-specific
  state; it never masquerades as the session's previous `error` state.
- Concurrent and repeated deletion is safe and idempotent.
- Existing native state and older additive CLI JSON remain readable.
- Go race tests, Go tests, Rust tests, frontend typecheck/tests, and lint pass.
