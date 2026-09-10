# Team-scoped GitHub App credentials for GitHub Connections

## Status

Proposed design. This document does not change the current API or runtime behavior.

## Summary

Extend a GitHub Connection with optional **team bindings**. A binding says that a
CCPlant team uses a particular GitHub App when a session accesses that
connection. The binding owns only the App ID and private-key reference; the
connection continues to own the GitHub host and organization routing
information. The installation is discovered from the target repository at
runtime and is not configured or persisted.

Do not expose or mount the GitHub App private key in a session. The proxy uses it
to mint a short-lived installation token immediately before launch and injects
only that token as `GITHUB_TOKEN`.

This preserves the current meanings of the two existing credential paths:

- a GitHub Connection OAuth client secret authenticates users and links their
  personal GitHub identity;
- a team binding authenticates a team workload as a GitHub App installation.

The same connection may support both paths. A team binding is not a login method
and is never shown on the login page.

## Goals and non-goals

Goals:

- let a team administrator attach GitHub App credentials to a GitHub Connection;
- select credentials from the session's authorized `team_id`;
- support GitHub.com and GitHub Enterprise Server;
- keep private keys encrypted and out of API responses, logs, session metadata,
  session archives, and workload environments;
- preserve existing personal OAuth-token and deployment-wide GitHub App behavior
  during migration;
- discover the applicable GitHub App installation from the target repository.

Non-goals:

- using an installation token to authenticate a human user;
- changing CCPlant team membership based on a GitHub App installation;
- managing the GitHub App's permissions or installation from CCPlant;
- long-term storage of installation access tokens.

## Why a binding is separate from the connection

A connection describes a GitHub authority (`base_url`, `api_url`) and its routing
rules. GitHub App installation credentials describe a workload identity and its
repository permissions. Putting one App credential directly on the connection
would make it global, while putting a complete connection in each team's
settings would duplicate host and organization routing configuration.

The relationship is therefore:

```text
GitHubConnection
  id, name, base_url, api_url, organizations, OAuth settings
       |
       +-- TeamGitHubAppBinding (team A)
       |     app_id, private_key
       |
       +-- TeamGitHubAppBinding (team B)
             app_id, private_key
```

There is at most one binding for a `(connection_id, team_id)` pair. Different
teams may use different GitHub Apps or share the same App credentials. The
installation is derived from the repository, so no installation ID belongs to
the binding.

## Domain model

Introduce a `TeamGitHubAppBinding` entity:

```go
type TeamGitHubAppBinding struct {
    ID             string    `json:"id"`
    ConnectionID   string    `json:"connection_id"`
    TeamID         string    `json:"team_id"`
    AppID        int64     `json:"app_id"`
    SecretSource string    `json:"private_key_source"` // encrypted | environment
    SecretEnv    string    `json:"private_key_environment,omitempty"`
    Enabled      bool      `json:"enabled"`
    CreatedBy    string    `json:"created_by"`
    CreatedAt    time.Time `json:"created_at"`
    UpdatedAt    time.Time `json:"updated_at"`
}
```

The PEM value is not part of the entity or any response DTO. For encrypted
storage it is held in the binding's secret payload under `private_key`; for an
environment reference only the validated environment-variable name is stored.
Responses contain `private_key_configured: true|false` and optionally a stable
SHA-256 public-key fingerprint, never the PEM.

`installation_id` is deliberately absent. At launch, the proxy creates an App
JWT and calls GitHub's `GET /repos/{owner}/{repo}/installation`. GitHub returns
the one installation through which that App can access the repository. The
proxy then uses the returned ID only to mint the short-lived access token. An
App installed in several organizations is therefore resolved unambiguously by
the repository without adding configuration to CCPlant.

The existing `github_app_installation_id` team setting becomes deprecated. It
does not coexist as a second source of truth once a team binding has been
created.

## API

Use team-scoped routes under the existing connection resource:

```text
GET    /github-connections/{connection_id}/team-bindings?team_id=org/team
PUT    /github-connections/{connection_id}/team-bindings/{team_id}
PATCH  /github-connections/{connection_id}/team-bindings/{team_id}
DELETE /github-connections/{connection_id}/team-bindings/{team_id}
PUT    /github-connections/{connection_id}/team-bindings/{team_id}/private-key
DELETE /github-connections/{connection_id}/team-bindings/{team_id}/private-key
POST   /github-connections/{connection_id}/team-bindings/{team_id}/test
```

`team_id` in the path must be URL encoded. `PUT` is idempotent and is the normal
create/update operation from the settings UI. Secret rotation is a separate
endpoint so an ordinary metadata update cannot accidentally erase or echo the
key.

Example request:

```json
{
  "app_id": 123456,
  "enabled": true,
  "private_key": {
    "source": "encrypted",
    "value": "-----BEGIN RSA PRIVATE KEY-----\n..."
  }
}
```

Example response:

```json
{
  "id": "uuid",
  "connection_id": "connection-uuid",
  "team_id": "acme/platform",
  "app_id": 123456,
  "private_key_source": "encrypted",
  "private_key_configured": true,
  "enabled": true,
  "updated_at": "2026-09-10T12:00:00Z"
}
```

Validation includes a positive numeric App ID, a parseable RSA or EC PEM private key,
connection existence, allowed environment-variable names, and uniqueness of the
connection/team pair. The `test` operation mints a token and calls the GitHub
installation endpoint. Because there is no configured installation, its request
must contain a repository such as `{"repository":"acme/api"}`. It returns App
slug, account, repository selection, token expiry, and permission names, but not
the resolved installation ID, token, or key.

All endpoints must be added to `spec/openapi.json`. Client methods and frontend
types should use the response DTO rather than sharing the persistence struct.

## Authorization

System administrators may manage and inspect every binding. Other users:

- need `CanReadInTeam(team_id)` to list or get metadata;
- need `CanCreateInTeam(team_id)` to create a binding;
- need the team's update/delete permission for metadata, rotation, testing, and
  deletion.

The current authorization context has create/read helpers but no update/delete
helpers. Add `CanUpdateInTeam` and `CanDeleteInTeam`; do not approximate those
operations with membership or `CanCreateInTeam`.

For every request, take the effective team from the authenticated authorization
context and verify it against the path value. A service account is limited to its
own `TeamID`. Neither `X-Forwarded-Team` nor a request body alone grants access.

Reading connection metadata must not imply access to another team's binding.
The global administrator page may show only a binding count by default; team IDs
are returned only to callers authorized for those teams.

## Persistence and encryption

Add a repository port instead of expanding the already large controller:

```go
type TeamGitHubAppBindingRepository interface {
    Upsert(context.Context, *TeamGitHubAppBinding, []byte) error
    Get(context.Context, connectionID, teamID string) (*TeamGitHubAppBinding, error)
    GetPrivateKey(context.Context, bindingID string) ([]byte, error)
    List(context.Context, connectionID string, teamIDs []string) ([]*TeamGitHubAppBinding, error)
    Delete(context.Context, connectionID, teamID string) error
}
```

Store the record as an application KV Secret with labels for resource kind,
connection ID, and a hash of team ID. Use a UUID-based object name; do not put the
raw team ID into a Kubernetes object name. Keep `record.json` and `private_key`
in the same versioned object so metadata and key rotation are atomic.

Encrypted key upload is available only when the configured KV backend provides
encrypted-at-rest values, matching the existing GitHub OAuth secret rule.
Environment references remain useful for externally managed secrets, but should
be restricted to `GITHUB_APP_[A-Z0-9_]+_PRIVATE_KEY` and resolved only inside the
proxy. A missing encryption capability or environment value is a hard validation
error, not a silent downgrade.

Use optimistic versions/ETags for PATCH, rotation, and deletion. Concurrent
rotation must return `409 Conflict` rather than overwrite a newer key.

## Runtime resolution

Resolve GitHub credentials only after scope normalization and team authorization.
Refactor token selection behind a `GitHubCredentialResolver` use case:

```text
explicit request token
  -> explicit connection_id + team session binding
  -> organization-mapped connection + team session binding
  -> explicit connection_id + user's linked OAuth identity
  -> organization-mapped connection + user's linked OAuth identity
  -> authenticated user's forwarded token
  -> legacy deployment-wide GitHub App fallback (migration period only)
```

For a team-scoped session, a matching enabled team binding takes precedence over
the user's linked identity. This makes the workload identity stable regardless
of which team member starts the session. A caller-supplied explicit token keeps
the current highest precedence for backward compatibility; a future policy may
disable explicit tokens per team.

The resolver takes the already authorized `team_id`, connection ID, and required
repository full name. It loads the binding, resolves the private key, creates an
App JWT, and discovers the installation with
`GET /repos/{owner}/{repo}/installation`. It then calls the installation-token
endpoint using the returned ID. Request a token restricted to the target
repository and reject repositories not present in the installation. A team App
binding cannot be used for a launch without a repository; return a validation
error instead of guessing an installation.

Cache the discovered installation in memory by `(binding_id, repository)` for a
short bounded period, and cache tokens by `(binding_id, repository, permissions)`
until five minutes before expiry, with single-flight refresh. Invalidate both
caches when the binding or private key changes. Installation IDs may appear only
inside this ephemeral cache and GitHub request path; do not persist or return
them.

Return a typed result containing the token, connection ID, credential kind, and
binding ID. Logs and session metadata record only those non-secret identifiers
and token expiry. Never log the token hash: it adds little diagnostic value and
creates a stable credential correlate.

Failures are fail-closed when a binding was selected: a disabled binding,
unreadable key, GitHub rejection, or repository mismatch returns an actionable
4xx/503 and must not fall back to a person's OAuth token. Fallback is allowed
only when no binding exists, during the documented migration period.

## Runtime sequence

```text
client -> POST /start (scope=team, team_id, repository)
proxy  -> authorize team session creation
proxy  -> select GitHub Connection from connection_id or repository owner
proxy  -> load (connection_id, team_id) binding
proxy  -> discover the App installation for the target repository
proxy  -> mint/cache short-lived installation token using private key
proxy  -> create session with GITHUB_TOKEN only
session -> clone/fetch repository
```

The existing `setTeamGitHubInstallationToken` path should move out of
`KubernetesSessionManager`; authentication selection belongs in a use case before
the Kubernetes or native runtime is chosen. This also gives all session managers
identical behavior.

## UI

Keep deployment-wide OAuth Connection management under the administrator page.
Add a `Team GitHub App` panel when a team scope is selected:

- choose a GitHub Connection;
- enter App ID;
- enter a repository when testing so the installation can be discovered;
- upload/rotate the PEM or select an environment reference;
- show configured state, last rotation time, and test result;
- require re-entry of the team name before deletion.

Users must never be able to download or reveal an uploaded PEM. A test failure
should distinguish invalid key, App not installed for the repository, insufficient repository
access, GitHub rate limiting, and network failure without including GitHub's
authorization headers or response bodies verbatim.

## Audit and observability

Emit audit events for binding create, metadata update, key rotation, enable/
disable, test, use, and delete. Include actor ID, team ID, connection ID, binding
ID, result, and request correlation ID. Exclude PEM and installation tokens.

Suggested metrics:

- installation token mint count and latency by connection host and result;
- cache hit/miss count;
- binding resolution count by result (`found`, `not_found`, `disabled`);
- token expiry remaining at injection.

Do not use team ID, repository name, App ID, or a discovered installation ID as unbounded
metric labels.

## Migration

Roll out in four compatible phases:

1. Add storage, API, authorization helpers, UI, and resolver. Keep existing
   `github_app_installation_id` and deployment-wide App configuration as fallback.
2. Let administrators create bindings with an App ID and PEM. Provide a dry-run
   report listing teams that still have a legacy installation ID; do not carry
   that ID into the new binding and do not copy a private key automatically.
3. Warn when a team session uses the legacy path. Once a binding exists, it is
   authoritative and failures do not fall back.
4. After all teams migrate, remove `github_app_installation_id` from team settings
   and stop passing deployment-wide App private keys into session construction.

If one deployment-wide App key is intentionally shared, support an
administrator-created, encrypted credential reference that multiple bindings
can reference. This is an optimization after the initial per-binding model; it
must preserve team-scoped authorization and must not make the secret retrievable.

## Test plan

- repository tests for uniqueness, optimistic concurrency, encrypted values, and
  filtering by authorized team IDs;
- authorization tests for admin, team create/read/update/delete permissions,
  unrelated team members, and team service accounts;
- PEM parsing and environment-name validation tests;
- GitHub.com and GHES repository-installation discovery and token endpoint tests;
- resolution precedence tests for explicit connection, organization mapping,
  team binding, personal identity, explicit token, and legacy fallback;
- fail-closed tests for disabled binding, expired/revoked key, missing repository,
  App not installed for the repository, repository restriction, and GitHub outage;
- assertions that API responses, logs, events, session metadata, archives, and
  workload environments never contain the PEM;
- concurrent token requests use one mint operation and refresh before expiry;
- Kubernetes, native, and External Session Manager launches receive identical
  resolved token settings;
- migration tests prove existing teams retain behavior until a binding is made.

## Implementation slices

1. Entity, repository port/adapter, authorization helpers, and OpenAPI schemas.
2. Team binding controller/use cases and secret rotation/test endpoints.
3. Central credential resolver and installation-token cache; adapt all launch
   paths and retain legacy fallback.
4. Team settings UI and API client.
5. Migration report, deprecation warnings, audit events, and operator docs.

Each slice can be released independently behind
`team_github_app_connections`. Enable API/UI creation first, then runtime
selection, and remove the feature flag only after migration telemetry is clean.
