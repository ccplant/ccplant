# Built-in GitHub App authentication for marketplace repositories

## Status and scope

Proposed design. This concerns the deployment-wide GitHub App configured by
Helm (`github.app.id` and `github.app.privateKey`). It does **not** use GitHub
Connections or the Connection-backed token broker.

The required case is a team session whose private plugin marketplace is in
another organization. The built-in App is installed for that repository,
possibly as a different installation, and the administrator is willing to give
all team sessions on the server read access to that exact repository.

## Root cause

`KubernetesSessionManager.setTeamGitHubInstallationToken` loads the team's
`github_app_installation_id` and calls
`startup.GenerateGitHubAppTokenFromPEM`. The request is restricted to the
working repository and the token is assigned to `GITHUB_TOKEN`.

Marketplace startup later calls `startup.cloneMarketplace`, which uses
`SetupGitHubAuth` and `gh repo clone`. It reuses the working repository's token.
That cannot work for the other marketplace because:

1. the token was requested with `repositories=[working repository]`; and
2. another organization normally has a different installation ID.

The App PEM currently reaches session settings for legacy startup behavior.
Letting the workload discover marketplace installations with that PEM is not an
acceptable fix: a user-controlled URL would select the target and the workload
would retain authority over every App installation.

## Security boundary

Server-wide marketplace access is an administrative grant. A marketplace URL
in base, team, or user settings is configuration, not authorization.

If the proxy minted a token merely because a URL appeared in merged settings, a
team member could replace it with any private repository visible to the App.
The App may therefore be used only for repositories in a separate,
administrator-controlled exact-repository allowlist.

Granting a repository server-wide means every team session can read it. A token
cannot be restricted to "plugin installation" after delivery to a workload.
The UI and documentation must state this explicitly.

## Proposed configuration

Extend the existing built-in App configuration:

```yaml
github:
  app:
    id: "123456"
    privateKey:
      secretName: github-app-private-key
      key: private-key
    marketplaceRepositories:
      - github.com/shared-tools/private-marketplace
```

For a session-manager-specific GitHub block, use the same precedence as the
existing App ID and private key.

Rules:

- normalize entries to `(host, owner, repository)`;
- require exact repositories; reject wildcards and organization-wide grants;
- reject duplicates, URL credentials, query strings, fragments, and non-HTTPS
  remote URLs;
- support GitHub.com and the configured GHES host;
- keep the list in administrator-owned Helm/runtime configuration, not the
  Settings API;
- do not configure a marketplace installation ID. Discover it from the exact
  repository using the built-in App JWT;
- retain team `github_app_installation_id` for the working repository in this
  implementation.

Expose the allowlist, without secrets, in admin runtime configuration and check
it in `doctor`. An admin CRUD API can be added later.

## Built-in App resolver

Add a resolver owned by the API/session-manager process:

```text
ResolveMarketplaceToken(host, owner/repo)
  -> require exact marketplaceRepositories match
  -> create App JWT from deployment App ID + PEM
  -> GET /repos/{owner}/{repo}/installation
  -> POST /app/installations/{id}/access_tokens
       repositories: [repo]
       permissions: { contents: read }
  -> return token + expiry
```

The installation ID is transient. Do not persist it in team settings, session
metadata, logs, or responses. The working repository and marketplace may use
different installations of the same App.

Cache discovery by `(App identity, host, owner/repo)` for a short bounded time.
Cache tokens by `(installation ID, owner/repo, contents:read)` until five
minutes before expiry, with single-flight refresh. Invalidate caches when the
App configuration changes. Never fall back to a personal OAuth token,
Connection credential, PAT, or the working repository token.

## Startup-only credentials

Credentials are needed once, before the agent starts. Add a provisioning-only
field to `SessionSettings`, separate from `Env` and generated agent settings:

```yaml
setup_credentials:
  git_repositories:
    github.com/shared-tools/private-marketplace:
      username: x-access-token
      token: ghs_...
      expires_at: 2026-10-06T07:00:00Z
```

Requirements:

- populate it only for allowlisted marketplace URLs present in materialized
  settings of a team session, preferably only when referenced by an enabled
  plugin;
- never merge it into process-wide `GITHUB_TOKEN`, `GH_TOKEN`, settings JSON,
  managed files, archives, logs, or the agent runtime environment;
- redact it anywhere `SessionSettings` is logged or returned;
- clear it and remove temporary helper files immediately after marketplace
  synchronization;
- never send App ID, PEM, JWT, or installation ID in this field.

The provisioning payload already transports session secrets. Keeping this
field explicitly setup-only avoids creating a general runtime credential API.
If runtime marketplace updates are later required, put the same exact-repo
policy behind a renewable session broker instead of extending token lifetime.

## Marketplace clone

Change marketplace clone to accept per-repository credentials instead of
reading global authentication implicitly.

For an allowlisted private marketplace:

1. normalize its URL to the allowlist key;
2. obtain the setup credential;
3. clone over HTTPS Git using a temporary credential helper or askpass file with
   mode `0600`;
4. keep the token out of argv, remotes, and global Git config;
5. remove the helper and credential on both success and failure;
6. verify the stored `origin` URL contains no credentials.

Do not use process-wide `gh` authentication for this path: `gh repo clone`
would prefer the working repository's `GITHUB_TOKEN`. Public, non-allowlisted
marketplaces continue with anonymous HTTPS clone.

If an allowlisted token cannot be resolved, fail closed. Do not try the working
token or personal credentials. If a plugin is enabled from that marketplace,
clone failure must fail setup instead of only logging a warning.

## Placement in the current flow

After settings materialization and before provisioning:

1. collect and normalize repositories from materialized marketplaces;
2. intersect them with the administrator allowlist;
3. for team scope, mint setup credentials with the built-in App;
4. attach them to the provisioning-only field;
5. let `sessionsettings.SetupSettings` consume them in `syncExtra`;
6. discard them before the agent starts.

This belongs in a shared service invoked before runtime-specific launch so
Kubernetes, native, and External Session Manager behave identically. The
existing Kubernetes manager is only a practical first integration point; the
shared path is a release requirement. A repository-less team session can use
the flow because it does not depend on `settings.Repository` or the working
installation ID.

## Failure handling and observability

Distinguish invalid allowlist configuration, App-not-installed, missing
`contents:read`, rate limits/timeouts, invalid App credentials, expired setup
credentials, and clone rejection. Audit use with session ID, team ID, host,
normalized repository, and result.

Never log PEM, JWT, installation ID, access token, authorization headers,
GitHub response bodies, or credential-helper output. Avoid repository names and
installation IDs as unbounded metric labels.

## Rejected alternatives

- **Reuse or broaden the working token:** it is repository-restricted, may be
  from another installation, and a token cannot span installations.
- **Add another installation ID to team settings:** the grant is server-wide,
  would be duplicated per team, and does not scale to several marketplaces.
- **Trust merged marketplace URLs:** team/user settings could then request any
  private repository visible to the App.
- **Put the PEM or a server-wide PAT in sessions:** both grant substantially
  broader and longer-lived authority.
- **Use GitHub Connections:** the affected credential is the built-in App;
  Connection configuration is unrelated to this path.

## Implementation slices

1. Add and validate `github.app.marketplaceRepositories` in config, Helm values,
   schema, examples, `doctor`, and documentation.
2. Extract the built-in App resolver with exact-repo installation discovery and
   cached `contents:read`, single-repository tokens.
3. Add the redacted provisioning-only field and populate it from the
   materialized-marketplace/allowlist intersection.
4. Use per-repository HTTPS Git credentials for marketplace clone, clean them up
   after setup, and make enabled-plugin clone failures fatal.
5. Apply preparation to all session-manager paths and add audit/metrics.

## Test plan

- existing team working-repository behavior remains unchanged;
- a private marketplace in another organization discovers its own installation
  and clones with a token restricted to that repository;
- working repository and marketplace installation IDs differ;
- multiple allowlisted marketplaces across installations receive only their own
  tokens;
- a repository-less team session can clone an allowlisted marketplace;
- changing a team/user URL to an unallowlisted private repository visible to the
  App yields no token;
- malformed, credential-bearing, wrong-host, case-variant, `.git`-suffixed, and
  URL-escaped inputs normalize or reject deterministically;
- failures never fall back to working-token, PAT, OAuth, or Connection auth;
- public unallowlisted marketplaces still clone anonymously;
- enabled-plugin startup fails when its marketplace clone fails;
- argv, `origin`, environment, global Git config, logs, generated settings,
  files, and archives contain no setup token;
- temporary credential files are `0600` and removed on success and failure;
- cache keys isolate host, installation, repository, and permission;
- concurrent launches never reuse a token for another repository;
- Kubernetes, native, and External Session Manager paths behave identically.
