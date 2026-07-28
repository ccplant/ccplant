# Server-side initial message history

## Context

The session creation UI currently stores recent messages only in browser
`localStorage`:

- `InitialMessageCache` keeps 2 messages.
- `RecentMessagesManager` keeps 10 messages.
- Both are device- and browser-local, and the two overlapping stores can drift.
- A message is saved before session creation succeeds.

The goal is to keep roughly 40 initial messages per user on the server so that
the history follows the user across devices.

## Decisions

### Scope and semantics

- History is user-scoped, not team-scoped. An initial message can contain
  private material and must not become visible to team members merely because
  the resulting session is team-scoped.
- Keep at most 40 distinct messages per user.
- Normalize only leading and trailing whitespace. Preserve all internal
  whitespace and casing because both can be meaningful in prompts.
- When an identical message is used again, move the existing entry to the
  front and update its timestamp instead of creating a duplicate.
- Record an entry inside the server-side session creation flow only after the
  session start request succeeds. Failed attempts are not history, and clients
  do not need a second write request.
- Limit an individual message to the same maximum accepted by session
  creation. Until that limit is centralized, the recorder must also enforce a
  defensive byte limit.
- History is product data and can contain sensitive text. Do not log message
  bodies, include them in metrics labels, or expose them through team-scoped
  APIs.

### API

Expose the authenticated user's collection under the existing `/users/me`
namespace rather than adding it to `PUT /settings/:name`. This makes the
ownership boundary explicit and keeps high-churn data out of the settings
secret.

```http
GET /users/me/initial-messages?limit=40
```

```json
{
  "items": [
    {
      "id": "01J...",
      "content": "Design the retry policy",
      "last_used_at": "2026-07-28T12:34:56Z"
    }
  ]
}
```

- The authenticated user is always the owner; no user ID is accepted from the
  client.
- `limit` defaults to 40 and is capped at 40.
- Items are ordered by `last_used_at` descending.

```http
DELETE /users/me/initial-messages
```

- Clears the authenticated user's history and returns `204`.
- A per-entry delete endpoint can be added later if the UI needs it.

GET requires the existing session read permission. DELETE requires session
create permission. There is deliberately no public POST endpoint: accepting a
separate client write would allow history and actual session creation to
diverge. OpenAPI and the TypeScript client are updated with the backend
implementation.

The existing session creation paths (`POST /start` and session creation through
`POST /acp`) call one shared history recorder after the session has been
created. The recorder receives the authenticated user ID and the resolved
initial message from the server-side launch flow. This keeps REST and ACP
behavior identical and also covers non-browser clients without extra work.

### Domain and repository

Introduce a small bounded aggregate:

```go
type InitialMessageHistoryItem struct {
    ID         string
    Content    string
    LastUsedAt time.Time
}

type InitialMessageHistory struct {
    UserID    string
    Items     []InitialMessageHistoryItem
    UpdatedAt time.Time
}
```

The repository boundary expresses the atomic behavior rather than exposing a
generic read/modify/write sequence:

```go
type InitialMessageHistoryRepository interface {
    List(ctx context.Context, userID string, limit int) ([]InitialMessageHistoryItem, error)
    UpsertAndTrim(ctx context.Context, userID, content string, maxItems int) (InitialMessageHistoryItem, error)
    DeleteAll(ctx context.Context, userID string) error
}
```

For the current Kubernetes deployment, store one resource per user named from a
sanitized user ID plus a stable hash, following the settings repository naming
pattern. Use a separate Secret, labelled
`agentapi.proxy/initial-message-history=true`, with one versioned JSON payload:

```json
{
  "version": 1,
  "user_id": "user-123",
  "items": [],
  "updated_at": "2026-07-28T12:34:56Z"
}
```

A Secret is preferred over a ConfigMap because prompt text may be sensitive.
Forty entries are well below the Kubernetes Secret size limit under the
session-message size cap. The implementation must preserve `resourceVersion`
and retry update conflicts with bounded exponential backoff so simultaneous
tabs cannot overwrite each other.

If the project later adopts a transactional database, the repository interface
allows replacing this with a table keyed by `(user_id, normalized_content_hash)`
and ordered by `last_used_at`.

### Frontend flow

1. On the new-session page, fetch `GET /users/me/initial-messages` and render
   it in the existing recent-message chooser.
2. Session creation records the message on the server. The frontend performs
   no history write.
3. Do not fail or roll back a successfully created session when the
   server-side history recorder fails. Emit a body-free structured error and
   return the created session normally.
4. During rollout, merge local history behind the server list, deduplicate by
   exact trimmed content, and display at most 40 items.
5. Do not upload old browser history through a general-purpose write endpoint.
   Existing local entries remain as a temporary fallback and naturally age out
   as new server-recorded entries accumulate.
6. Remove `InitialMessageCache` and replace `RecentMessagesManager` with the
   server-backed client after one compatibility release. The chat composer can
   continue using the same history if that is intentional; otherwise rename
   the current shared UI concept to make “initial messages” explicit.

### Failure behavior

- GET failure: fall back to existing local history for that page load.
- Recorder failure: session creation remains successful; emit a body-free
  structured error and increment a low-cardinality failure counter.
- Repository conflict: retry server-side; return `503` only after bounded
  retries are exhausted.
- Corrupt stored payload: return `500`, emit a body-free structured error, and
  preserve the resource for investigation rather than silently overwriting it.

## Delivery plan

1. Add domain types, repository port, Kubernetes implementation, conflict
   tests, controller routes, and OpenAPI.
2. Add TypeScript client methods and controller/client tests for authorization,
   ordering and clear; add launch/ACP tests for automatic recording,
   deduplication, trimming from 41 to 40, validation, and concurrent updates.
3. Switch the session creation UI to read the server resource with local
   fallback; session creation itself performs the write.
4. Observe GET/recorder error counts and resource sizes for one release.
5. Delete the redundant local-only managers and migration code.

## Acceptance criteria

- A signed-in user sees the same recent initial messages on another browser.
- The 41st distinct message evicts the least recently used entry.
- Reusing an existing message moves it to the top without increasing the count.
- Two concurrent additions are both retained.
- Users cannot read, write, or clear another user's history.
- Team-scoped session creation does not make the history team-readable.
- A history storage outage does not prevent session creation.
- REST and ACP session creation both record history without a second client
  request.

## Alternatives rejected

- **Expose `POST /initial-message-history`:** creates a second client write
  after session creation, so network failures can make the history disagree
  with real session usage. Its top-level path also hides the fact that the
  resource is always owned by the authenticated user.
- **Add the array to user settings:** simpler wiring, but settings use whole
  resource updates and contain unrelated credentials/configuration. Frequent
  history writes increase conflict and accidental overwrite risk.
- **Derive history by listing sessions:** couples the feature to session
  retention and authorization, may expose messages from shared/team sessions,
  and cannot reliably represent deleted or externally created sessions.
- **Keep only browser storage:** does not meet cross-device server-side
  persistence.
