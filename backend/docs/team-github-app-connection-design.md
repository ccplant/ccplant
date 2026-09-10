# GitHub App credentials for GitHub Connections

## Status

Proposed design. This document does not change the current API or runtime behavior.

## Summary

Store one GitHub App credential set directly on each GitHub Connection. The
credential consists of an App ID and PEM private key. It is connection-scoped,
not user-scoped or team-scoped.

When a team session starts for a repository, CCPlant first selects the GitHub
Connection using the repository-to-connection mapping already held by the
GitHub Connection model. It then uses that Connection's App ID and PEM to
discover the repository's GitHub App installation and mint a short-lived token.

```text
repository
  -> existing GitHub Connection mapping
  -> selected GitHub Connection
  -> that Connection's App ID + PEM
  -> repository installation discovery
  -> session-scoped token broker
```

There is no team-to-App binding and no configured Installation ID.

## Goals and non-goals

Goals:

- configure one GitHub App per GitHub Connection;
- reuse the existing repository-to-Connection mapping;
- use the selected Connection's App for team sessions;
- support GitHub.com and GitHub Enterprise Server;
- keep the PEM encrypted and out of API responses, logs, session metadata,
  session archives, and workload environments;
- refresh repository-scoped installation tokens without giving the PEM to a
  session;
- migrate safely from the deployment-wide GitHub App configuration.

Non-goals:

- configuring different Apps per team;
- configuring or persisting an Installation ID;
- discovering a Connection by probing every configured App;
- using an installation token to authenticate a human user;
- changing team membership or authorization from GitHub App data.

## Model

Extend the existing `githubConnection` with GitHub App metadata:

```go
type githubConnection struct {
    // Existing fields: ID, Name, BaseURL, APIURL, OAuth settings,
    // Organizations, Enabled, and so on.

    GitHubApp *githubAppConfiguration `json:"github_app,omitempty"`
}

type githubAppConfiguration struct {
    AppID             int64  `json:"app_id"`
    PrivateKeySource  string `json:"private_key_source"` // encrypted | environment
    PrivateKeyEnv     string `json:"private_key_environment,omitempty"`
}
```

The PEM is secret payload, not model metadata. With encrypted storage it is held
in the Connection's existing Secret/KV document under a separate
`github_app_private_key` key. With an environment reference, only the validated
environment-variable name is stored.

The response DTO adds only non-secret state:

```json
{
  "github_app": {
    "app_id": 123456,
    "private_key_source": "encrypted",
    "private_key_configured": true,
    "updated_at": "2026-09-10T12:00:00Z"
  }
}
```

The PEM is never returned. `installation_id` is not a field in the entity,
request, response, settings, or persistence format.

OAuth App fields and GitHub App fields have different purposes:

- OAuth client ID/secret: login and personal GitHub identity linking;
- GitHub App ID/PEM: team-session repository access.

A Connection may configure either or both. Adding a GitHub App must not make the
Connection appear as an additional login option.

## API

Keep App metadata within the existing administrator-only Connection endpoints:

```text
POST  /admin/github-connections
PATCH /admin/github-connections/{id}
```

Use dedicated endpoints for private-key lifecycle so an ordinary metadata update
cannot erase or echo the PEM:

```text
PUT    /admin/github-connections/{id}/github-app/private-key
DELETE /admin/github-connections/{id}/github-app/private-key
POST   /admin/github-connections/{id}/github-app/test
```

Example metadata update:

```json
{
  "github_app": {
    "app_id": 123456,
    "private_key_source": "encrypted"
  }
}
```

Example key upload:

```json
{
  "value": "-----BEGIN RSA PRIVATE KEY-----\n..."
}
```

Changing the App ID does not erase the current PEM, but the Connection is marked
`untested` until the App is tested again. Deleting the key disables GitHub App
use without affecting OAuth login.

Validation requires a positive App ID, a parseable RSA PEM private key,
and an allowed environment-variable name. Encrypted key upload is available
only when the configured KV backend supports encrypted-at-rest values.

The test endpoint takes a repository because no Installation ID is configured:

```json
{
  "repository": "acme/payments"
}
```

It verifies the App identity, discovers the installation for that repository,
and mints a repository-scoped token. The response may include App slug, account,
repository selection, permission names, and token expiry, but never the PEM,
token, or resolved Installation ID.

All endpoints and schemas must be added to `spec/openapi.json`.

## Repository-to-Connection selection

The existing GitHub Connection mapping remains the only source of truth for
selecting a Connection. GitHub App installation probes do not participate in
Connection selection.

For the current model, `organizations` maps a normalized repository owner to one
Connection, and `validateOrganizationAssignments` prevents the same organization
from being assigned to multiple Connections. The runtime should reuse that same
lookup rather than implement a second mapping.

Given a team session repository, selection is:

1. Parse and normalize the repository URL into `(host, owner, name)`.
2. Filter enabled Connections to the repository's GitHub host/API authority.
3. Apply the existing repository-to-Connection mapping. With the current model,
   match normalized `owner` against `connection.organizations`.
4. Require exactly one mapped Connection.
5. Load the GitHub App configuration from that Connection.

Outcomes:

| Mapping result | Runtime result |
|---|---|
| One enabled Connection with App ID and PEM | Use its GitHub App |
| No mapped Connection | `github_connection_not_found` |
| Mapped Connection has no complete App credential | `github_app_not_configured` |
| More than one mapped Connection | Configuration error; do not choose by list order |
| Mapped Connection is disabled | `github_connection_disabled` |

The multiple-match case should normally be prevented at write time by the
existing uniqueness validation, but runtime still checks it to protect against
legacy or externally modified data.

If the Connection model later gains repository-exact or pattern mappings, the
GitHub App resolver consumes the Connection selected by that model without
changing credential storage. Mapping precedence belongs to the Connection
model, not to GitHub App authentication.

Example:

```text
repository: https://github.com/acme/payments

Connection A: organizations=["other-org"]
Connection B: organizations=["acme"]

existing mapping selects Connection B
Connection B has App ID 202 + PEM
proxy discovers App 202's installation for acme/payments
proxy mints a token restricted to acme/payments
```

An optional explicit `connection_id` may continue to select a Connection where
the current API already supports it. It must match the repository host and the
Connection's repository mapping; it cannot bypass the mapping policy.

## Team-session runtime

Team sessions use the GitHub App from the repository-selected Connection. They
must not fall back to the initiating user's linked OAuth token or forwarded
token. This keeps the workload identity independent of which team member starts
the session.

After Connection selection:

1. Resolve the selected Connection's PEM inside the proxy.
2. Create a short-lived App JWT from its App ID and PEM.
3. Call `GET /repos/{owner}/{repo}/installation` on the selected Connection's
   `api_url`.
4. Use the returned ID only to call the installation-token endpoint.
5. Request a token restricted to the target repository.
6. Return the token through the session-scoped token broker.

The Installation ID is transient protocol data. It may exist in memory and in a
GitHub request path, but is not persisted, returned, or configured.

```text
client -> POST /start (scope=team, repository=acme/payments)
proxy  -> authorize creation in the requested team
proxy  -> existing repository mapping selects Connection B
proxy  -> load App ID + PEM from Connection B
proxy  -> discover the installation for acme/payments
proxy  -> mint a repository-scoped installation token
proxy  -> launch session with a broker credential, not the PEM or GitHub token
session -> credential helper obtains a current token and clones/fetches
```

A team App flow requires a repository. If none is present, return
`github_repository_required`; do not guess a Connection or installation. If the
App is not installed for the mapped repository, return
`github_app_not_installed`. Authentication errors, rate limits, timeouts, and
GitHub `5xx` responses fail closed and do not trigger personal-token fallback.

Refactor this resolution into a `GitHubCredentialResolver` use case before the
Kubernetes, native, or External Session Manager runtime is selected. The current
`setTeamGitHubInstallationToken` implementation in `KubernetesSessionManager`
should no longer combine team settings with deployment-wide App credentials.

Cache installation lookup results by `(connection_id, repository)` for a short
bounded period. Cache installation tokens by `(connection_id, repository,
permissions)` until five minutes before expiry with single-flight refresh.
Invalidate both caches when the Connection's App ID or PEM changes. Do not
persist either value.

## Token refresh without distributing the PEM

An installation token normally expires while a long-running session may remain
active. A `GITHUB_TOKEN` environment variable set only at launch therefore
cannot be the final interface: environment variables in an already running
process cannot be updated, and a background refresh would leave tools holding
the old value.

Keep App ID/PEM and token minting in the proxy. Give the session a narrowly
scoped broker credential instead:

```text
GET /internal/sessions/{session_id}/github-credentials
Authorization: Bearer <session-github-credential>

200 {
  "username": "x-access-token",
  "token": "ghs_...",
  "expires_at": "2026-09-10T13:00:00Z"
}
```

The broker credential claims and enforces:

- the exact session ID;
- selected Connection ID;
- normalized repository full name;
- allowed operation `github:token:read` only;
- a 24-hour idle expiry that is extended while the Session actively uses it;
- a random identifier that can be revoked when the Session is stopped.

It is revoked on explicit Session deletion and otherwise expires after 24 hours
without a broker request. It cannot request another repository, select another Connection, read the PEM,
or call normal management APIs. For Kubernetes it should additionally be usable
only from the Session workload identity or network path when that facility is
available. Possession still grants renewable access to the repository for the
Session lifetime, so it must be treated as a secret and excluded from logs and
archives.

The endpoint runs repository-to-Connection validation again, then returns a
cached installation token if it remains valid for more than five minutes.
Otherwise it uses the Connection's PEM to mint a replacement. Concurrent refresh
requests share one mint operation. Revoked/disabled Connections, changed mapping,
deleted Sessions, or repositories no longer accessible fail closed.

### Tool integration

Do not set a long-lived `GITHUB_TOKEN` or `GH_TOKEN` in the Session environment.
Configure tools to obtain a current token per command:

- install a Git credential helper that calls the broker on `get` for only the
  selected GitHub host/repository and returns `x-access-token` plus the current
  token;
- place a `gh` wrapper earlier in `PATH`; it calls the broker and executes the
  real `gh` with `GH_TOKEN` set only in that child process;
- provide a small `github-token` command for tools that need to make direct API
  calls (`Authorization: Bearer "$(github-token)"`).

Each new `git` or `gh` command obtains a token that is valid for that command.
The proxy refreshes only near expiry, so this does not mint a token per command.
A single command that receives `401` because its token crossed expiry may fetch
once more and retry only when the operation is safe to retry; never busy-loop.

Arbitrary applications that only read `GITHUB_TOKEN` once cannot transparently
refresh without receiving the PEM. They must use the broker command/helper or a
separate authenticated HTTP proxy. The initial implementation supports Git and
`gh`; it must not imply transparent refresh for every GitHub client.

## Authorization and secret handling

GitHub App configuration is part of the existing administrator-owned GitHub
Connection. Initially, only system administrators can create, update, rotate,
test, or remove it. No new team-scoped CRUD authorization is introduced.

The runtime may use the App only after the caller has been authorized to create
the team session. Team membership controls session creation, not ownership of
the Connection credential.

Use optimistic versions/ETags for metadata updates, rotation, and deletion.
Concurrent rotation returns `409 Conflict` instead of overwriting a newer key.

Environment references, if supported, are restricted to
`GITHUB_APP_[A-Z0-9_]+_PRIVATE_KEY` and resolved only inside the proxy. A missing
encryption capability or environment value is a hard error, not a silent
downgrade.

## UI

Extend the administrator GitHub Connection editor with a `GitHub App` section:

- App ID;
- PEM upload/rotation or environment reference;
- configured state and last rotation time;
- repository input for testing;
- test result.

There is no team selector. The team settings UI no longer needs
`github_app_installation_id` after migration.

Users must never be able to download or reveal an uploaded PEM. Test failures
should distinguish invalid key, App not installed for the repository,
insufficient repository access, rate limiting, and network failure without
including authorization headers or GitHub response bodies verbatim.

## Audit and observability

Emit audit events for App metadata update, key rotation, test, runtime use, and
key deletion. Include actor ID, Connection ID, result, and request correlation
ID. Runtime-use events may include the repository according to the deployment's
audit policy. Exclude PEM, JWT, installation token, and Installation ID.

Suggested metrics:

- repository-to-Connection resolution result;
- installation discovery and token mint latency by GitHub host and result;
- installation/token cache hit and miss count;
- token expiry remaining when returned by the broker;
- broker request count by result.

Do not use repository, Connection ID, App ID, or Installation ID as unbounded
metric labels.

## Migration

Roll out in compatible phases:

1. Add GitHub App fields and secret lifecycle endpoints to GitHub Connections.
2. Add the central resolver while retaining the current deployment-wide App ID/
   PEM plus team `github_app_installation_id` path as a legacy fallback.
3. Configure App ID and PEM on each repository-mapped Connection and test with a
   representative repository.
4. Once a mapped Connection has App credentials, make it authoritative. Errors
   do not fall back to deployment-wide or personal credentials.
5. Warn on legacy fallback usage, then remove `github_app_installation_id` from
   team settings and deployment-wide App credentials after migration.

No Installation ID is copied into the new Connection model.

## Test plan

- Connection persistence encrypts PEM and response DTOs never contain it;
- App ID and PEM rotation use optimistic concurrency;
- existing organization/repository mapping selects the expected Connection;
- host normalization works for GitHub.com and GitHub Enterprise Server;
- zero, one, multiple, and disabled Connection mapping outcomes are deterministic;
- the resolver never probes Apps to choose a Connection;
- the selected Connection's App performs repository-installation discovery;
- team sessions never use personal OAuth or forwarded tokens;
- missing repository, incomplete App configuration, App-not-installed, rate
  limit, timeout, and GitHub outage all fail closed;
- tokens are restricted to the requested repository;
- an expired installation token is refreshed without exposing the PEM;
- broker credentials cannot change Session, Connection, or repository;
- broker access is revoked when the Session ends;
- Git credential helper and `gh` wrapper fetch a current token for each command;
- refresh uses single-flight and does not retry a persistent GitHub error;
- API responses, logs, events, session metadata, archives, and workload
  environments never contain the PEM, App JWT, or Installation ID;
- the Session environment contains neither a static `GITHUB_TOKEN` nor
  `GH_TOKEN`;
- concurrent launches use single-flight discovery/token minting;
- Kubernetes, native, and External Session Manager launches resolve credentials
  identically;
- existing deployments retain legacy behavior until Connections are configured.

## Implementation slices

1. Extend the Connection model, repository adapter, response DTO, OpenAPI, and
   private-key lifecycle endpoints.
2. Add the administrator UI and repository-based test operation.
3. Centralize existing repository-to-Connection selection and add the GitHub App
   token resolver/cache and session-scoped broker endpoint.
4. Add the Git credential helper and `gh` wrapper, adapt all session launch paths,
   and retain the legacy fallback.
5. Add audit events, migration warnings, and remove legacy team Installation ID
   settings after adoption.
