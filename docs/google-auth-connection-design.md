# Google authentication with Google Connections

## Status

Proposed design. This document does not change the current API or runtime behavior.

## Summary

Add an administrator-managed `GoogleConnection` resource for Google OpenID
Connect (OIDC). Each connection owns one Google OAuth client and the policy that
decides who may sign in. Enabled connections can be shown on the login page and
can also be linked to an existing ccplant principal from Account Connections.

This design intentionally uses a provider-specific resource rather than turning
the existing GitHub Connection into a generic OAuth record. GitHub Connections
also own GitHub Enterprise URLs, organization routing, GitHub App credentials,
team discovery, and repository tokens. Putting Google fields into that model, or
prematurely hiding both providers behind a generic JSON configuration, would
weaken validation and authorization boundaries.

The shared concept is the ccplant principal. Google and GitHub identities attach
to the same stable principal, while their provider-specific credentials and
claims remain separate.

```text
GoogleConnection (admin configuration)
  -> OAuth/OIDC authorization attempt (short lived, single use)
  -> GoogleIdentity (provider subject linked to one Principal)
  -> ccplant application session
```

## Scope

### Goals

- allow an administrator to define multiple Google OAuth clients;
- support Google account and Google Workspace login through OIDC;
- restrict a connection by hosted domain and/or explicit email domain;
- support both first login and linking Google to an existing principal;
- keep client secrets and provider tokens out of API responses and logs;
- make identity uniqueness depend on the stable OIDC subject, not email;
- coexist with the deployment-wide GitHub OAuth flow and GitHub Connections;
- provide a staged path from the current GitHub-specific principal storage to a
  provider-neutral principal model.

### Non-goals

- using Google credentials in an agent session;
- access to Drive, Gmail, Calendar, Cloud, or Workspace Admin APIs;
- deriving ccplant Team membership from Google Groups;
- service-account or workload-identity authentication;
- accepting arbitrary OIDC issuers in the first version;
- replacing GitHub Connections with a generic connection resource.

If Google API access is needed later, it should be a separate credential and
consent design. Login should request only `openid email profile`.

## Resource model

### GoogleConnection

```go
type GoogleConnection struct {
    ID                string
    Name              string
    ClientID          string
    SecretSource      string // encrypted | environment
    SecretEnvironment string
    Enabled           bool
    ShowOnLogin       bool
    AllowUserCreation bool
    HostedDomains     []string
    EmailDomains      []string
    CreatedAt         time.Time
    UpdatedAt         time.Time
}
```

`HostedDomains` validates Google's verified `hd` claim. `EmailDomains` is an
optional additional allowlist evaluated against the normalized verified email.
An empty list means no restriction for that field. If both lists are non-empty,
both restrictions must pass. Consumer accounts have no `hd` claim and therefore
cannot use a connection with `HostedDomains` configured.

`issuer`, authorization endpoint, token endpoint, and JWKS endpoint are fixed to
Google's published OIDC metadata in version one. They are not administrator
input. The callback URL is derived from the deployment public URL and returned
as read-only data.

The client secret has an independent lifecycle:

- `encrypted`: store the value in the connection's encrypted Secret/KV record;
- `environment`: store only an environment variable name matching
  `^GOOGLE_OAUTH_[A-Z0-9_]+_CLIENT_SECRET$`;
- never return the resolved value;
- expose only `secret_source`, `secret_environment`, and `secret_configured`.

### Principal and identity

The existing `githubPrincipal` is already acting as a provider-neutral mapping
between an application user and external identities. Rename its persistence
concept to `Principal` and preserve its IDs and existing records.

```go
type Principal struct {
    ID              string
    InternalSubject string
    CreatedAt       time.Time
}

type GoogleIdentity struct {
    ID            string
    PrincipalID   string
    ConnectionID  string
    Subject       string // OIDC `sub`
    Email         string // display/audit data, not identity key
    EmailVerified bool
    Name          string
    AvatarURL     string
    HostedDomain  string
    CreatedAt     time.Time
    UpdatedAt     time.Time
}
```

The unique key is `(connection_id, subject)`. Email address, display name, and
hosted domain are mutable attributes and must never merge principals. Linking an
identity already owned by another principal returns `409 Conflict`.

Google access and refresh tokens are not persisted in version one. The ID token
is verified and discarded after claims are copied. This minimizes the consent
scope and prevents a login credential from silently becoming a Google API
credential.

## API

### Administrator endpoints

```text
GET    /admin/google-connections
POST   /admin/google-connections
GET    /admin/google-connections/{id}
PATCH  /admin/google-connections/{id}
DELETE /admin/google-connections/{id}
PUT    /admin/google-connections/{id}/secret
DELETE /admin/google-connections/{id}/secret
POST   /admin/google-connections/{id}/test
```

Metadata creation and update never return or implicitly erase the secret.
Deleting a connection with linked identities returns `409 Conflict`; the admin
must disable it or explicitly unlink/migrate the identities first. Disabling a
connection prevents new login and link attempts but preserves audit data.

`POST .../test` performs OIDC discovery/JWKS reachability checks and validates
that the configured client and resolved secret are present. It cannot complete
an end-user consent flow, so a successful test does not claim that domain policy
or user consent is valid.

### Public and user endpoints

```text
GET    /google-connections/login-options
POST   /google-connections/login
GET    /auth/google-connections/callback

GET    /google-identities
POST   /google-identities/link
DELETE /google-identities/{identity_id}
```

`login-options` returns only enabled, configured connections with
`show_on_login=true`, and only `id` and `name`. Identity list/link/unlink routes
require an authenticated user. The callback authenticates through its
server-side attempt record and is the only unauthenticated callback route.

All endpoints and DTOs are added to `backend/spec/openapi.json`. API errors use
stable machine-readable codes in addition to a safe message, including
`connection_unavailable`, `invalid_oauth_state`, `domain_not_allowed`,
`identity_conflict`, and `user_creation_disabled`.

## Authorization flow

Use OAuth 2.0 Authorization Code flow with OIDC and PKCE, even though the server
also has a client secret.

1. The browser requests login or link with a `connection_id`, a callback URL,
   and, for linking, a same-origin relative return path.
2. The backend validates the connection and creates a random, single-use
   `GoogleAuthAttempt` with a ten-minute expiry. It stores the mode, connection,
   principal for link mode, callback URL, nonce, and the PKCE verifier.
3. The authorization request contains `scope=openid email profile`, `state`,
   `nonce`, `code_challenge`, and `code_challenge_method=S256`. `hd` may be sent
   as a UI hint only; it is never an authorization check.
4. The callback atomically consumes the attempt before exchanging the code.
5. The backend validates the ID token signature against Google's JWKS and checks
   `iss`, `aud`, `exp`, `iat`, and `nonce`. It requires a non-empty `sub`, an
   email, and `email_verified=true`.
6. The backend applies the connection's hosted-domain and email-domain policy.
7. In link mode it attaches the identity to the attempt's principal. In login
   mode it resolves the identity and either uses its principal or, when allowed,
   creates a principal and identity atomically.
8. The backend creates a ccplant session and the frontend stores the normal
   ccplant session cookie. Google tokens are not sent to the browser as the
   application bearer token.

The callback URL must match the configured public origin and fixed callback
path. Return paths are relative and sanitized. Error redirects expose a bounded
error code, never an upstream response or token.

## Login and linking policy

For login:

- an existing `(connection_id, sub)` always resolves to its current principal;
- a new identity is created only when `allow_user_creation=true`;
- a matching email on another principal does not link or merge automatically;
- a disabled connection or failed domain policy is fail-closed;
- unlinking the last login method should be rejected unless another usable
  identity or personal API key remains.

For authenticated linking, the currently authenticated principal is captured in
the attempt. The callback must not recalculate ownership from mutable browser
state. Repeating a successful link is idempotent for the same principal and a
conflict for another principal.

Google identity does not grant ccplant Team membership in version one. Existing
GitHub membership resolution and team bindings continue unchanged.

## Persistence and concurrency

Follow the existing encrypted KV abstraction used by GitHub Connections, but
put repository interfaces and business rules outside the Echo controller. New
labels/record kinds are:

```text
agentapi.ccplant.io/google-connection=true
agentapi.ccplant.io/google-identity=true
agentapi.ccplant.io/auth-principal=true
agentapi.ccplant.io/google-auth-attempt=true
```

Required constraints are:

- unique Google Connection ID;
- unique `(connection_id, subject)` identity;
- one identity owner for its lifetime unless an explicit future transfer flow
  is introduced;
- compare-and-swap or database transaction for identity/principal creation;
- atomic single-use consumption of an auth attempt;
- expired attempt cleanup that is safe to repeat.

The current Kubernetes Secret implementation cannot guarantee uniqueness by a
list-then-create sequence alone. Derive the identity object name from a hash of
`connection_id + NUL + subject`, use create-if-absent, and verify the stored
owner after `AlreadyExists`. Do not place the raw subject or email in an object
name or label.

## Application session boundary

The current connection-based GitHub flow places its provider access token in
the OAuth session response. Google must not copy this behavior. Introduce a
provider-neutral authenticated-login result and mint an opaque application
session credential. Middleware resolves that credential to a principal and
constructs an ordinary `entities.User` with `UserTypeRegular` and
`AuthType="google_oidc"` for audit context.

The application session stores only the principal ID, authentication method,
connection ID, issue/expiry times, and authorization snapshot or inputs needed
to rebuild it. It does not store the ID token, client secret, authorization
code, PKCE verifier, or nonce after completion.

This boundary should be shared by the Google implementation and then adopted by
the GitHub Connection login flow as a follow-up. Google launch must not depend
on the larger GitHub token migration if application sessions can support both
credential forms during a transition.

## Backend structure

Avoid adding another large provider controller. Suggested boundaries are:

```text
internal/domain/entities/
  principal.go
  google_connection.go
  google_identity.go
internal/usecases/auth/
  start_external_login.go
  complete_google_login.go
  link_google_identity.go
internal/usecases/ports/repositories/
  principal_repository.go
  google_connection_repository.go
  google_identity_repository.go
  auth_attempt_repository.go
internal/usecases/ports/services/
  google_oidc_service.go
internal/infrastructure/repositories/
  kubernetes_* implementation
internal/infrastructure/services/
  google_oidc_service.go
internal/interfaces/controllers/
  google_connections_controller.go
  google_identities_controller.go
```

The OIDC service owns discovery, code exchange, and token verification. Use
context-aware HTTP calls, the repository's hardened HTTP client, response size
limits, timeouts, and bounded JWKS caching. Provider/network errors are wrapped
without logging response bodies or tokens.

## Frontend

- add an Admin > Google Connections page with metadata, secret lifecycle,
  callback URL copy, enable/show/create-user flags, domain policy, and test;
- list enabled Google connections on the login page as separate buttons;
- extend Account Connections with a Google section and link/unlink status;
- add a Next.js callback route that forwards only `code`, `state`, and OAuth
  error fields, then installs the ccplant application-session cookie;
- clear temporary OAuth cookies on both success and failure;
- use provider-neutral callback/cookie helpers where behavior is identical, but
  retain provider-specific API types and UI copy.

## Migration and rollout

1. Add provider-neutral principal repository types that can read the existing
   `github-principal` records and write the new label. Preserve every principal
   ID and internal subject. Dual-read old/new labels during rollout.
2. Add Google Connection, identity, attempt, OIDC service, and admin APIs behind
   a deployment feature flag. No connection exists by default.
3. Add application-session issuance that supports Google while retaining the
   existing GitHub session path.
4. Add admin, login, and account-linking UI and OpenAPI/client definitions.
5. Enable one test connection with `show_on_login=false`, validate linking and
   login, then expose it on the login page.
6. Backfill the new principal label and remove the legacy dual-read only after
   all supported versions understand the new record.
7. Separately migrate GitHub Connection login away from provider access tokens
   and rename GitHub-specific principal code.

Rollback disables Google connections and hides the UI. Existing Google identity
records remain dormant and must not be deleted automatically, so re-enabling the
feature preserves principal ownership.

## Observability and audit

Emit structured audit events for connection create/update/disable/delete,
secret replacement/removal, login success/failure category, identity link and
unlink, and policy rejection. Record actor/principal, connection ID, outcome,
and request correlation ID. Do not record email in general request logs; if the
audit policy permits it, store it only in the access-controlled identity audit
record.

Metrics should include attempts, successes, failures by bounded reason,
callback latency, token-exchange latency, JWKS refresh failures, and active
identity count. Connection ID is acceptable only where metric cardinality is
bounded; never label metrics with subject, email, state, or domain.

## Tests and acceptance criteria

Unit tests cover normalization and validation, domain policy, claim validation,
state expiry and one-time use, PKCE/nonce generation, identity conflicts, and
`allow_user_creation`. Repository contract tests cover concurrent first login,
concurrent linking, CAS updates, and cleanup.

Integration and browser tests cover:

- admin CRUD without secret disclosure;
- encrypted and environment-backed secrets;
- successful Workspace and consumer login under the corresponding policy;
- rejected unverified email, issuer/audience/nonce mismatch, disallowed domain,
  expired state, callback replay, and OAuth denial;
- linking, idempotent relinking, cross-principal conflict, and safe unlink;
- disabled/hidden connections and deletion with linked identities;
- coexistence with GitHub login, API key login, GitHub team resolution, and
  existing principal IDs;
- absence of client secret, code, verifier, provider tokens, subject, and email
  from API responses, logs, metrics, and application cookies.

Acceptance requires that a user can sign in through a configured Google
Connection and receive the same ccplant principal when subsequently using any
identity explicitly linked to that principal, without exposing or persisting a
Google provider token beyond the callback.

## Decisions and follow-ups

Decisions in this design:

- the resource is `GoogleConnection`, not a generic OAuth Connection;
- OIDC `sub` scoped by connection is the identity key;
- Google credentials are authentication-only and are not injected into agent
  sessions;
- Google Groups do not affect authorization;
- domain restrictions are enforced from verified claims, never request hints;
- application credentials are distinct from Google tokens.

Follow-up work may add Google Groups-to-Team mapping, generic OIDC providers, or
Google API grants. Each requires separate scopes, token retention, revocation,
and authorization review and should not be folded into the initial login work.
