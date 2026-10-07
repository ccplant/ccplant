# Built-in GitHub App authentication for marketplace repositories

## Status and scope

Proposed design. This concerns the deployment-wide GitHub App configured by
Helm (`github.app.id` and `github.app.privateKey`). It does **not** use GitHub
Connections or the Connection-backed token broker.

The built-in App's repository access is treated as server-wide authority. A
team may configure any marketplace repository that the App can read. Therefore
this design does not add a separate marketplace allowlist.

## Root cause

`KubernetesSessionManager.setTeamGitHubInstallationToken` loads the team's
`github_app_installation_id` and calls
`startup.GenerateGitHubAppTokenFromPEM`. The request is restricted to the
working repository and the resulting token becomes `GITHUB_TOKEN`.

Marketplace startup later calls `startup.cloneMarketplace`, which uses
`SetupGitHubAuth` and `gh repo clone`. It reuses that working-repository token.
This fails when the marketplace is in another organization because:

1. the token was requested with `repositories=[working repository]`; and
2. another organization normally has a different installation ID.

The existing App already supports repository-based installation discovery.
Marketplace clone does not currently invoke that flow because
`GITHUB_TOKEN` and `GITHUB_INSTALLATION_ID` take precedence.

## Proposed behavior

For each GitHub marketplace URL, independently resolve credentials from the
built-in App:

```text
marketplace URL
  -> normalize host and owner/repository
  -> create App JWT from built-in App ID + PEM
  -> GET /repos/{owner}/{repository}/installation
  -> POST /app/installations/{id}/access_tokens
       repositories: [repository]
       permissions: { contents: read }
  -> clone marketplace with that token
```

Marketplace resolution must deliberately ignore:

- the team `github_app_installation_id`, because it identifies the working
  repository's installation;
- the existing `GITHUB_TOKEN`, because it is restricted to the working
  repository;
- personal OAuth tokens, PATs, and GitHub Connection credentials.

The resolved installation ID is transient. Do not write it back to team
settings or include it in session metadata, logs, or API responses.

## Authorization model

No extra marketplace allowlist is introduced. The effective authorization is:

```text
team may configure marketplace URL
AND
built-in GitHub App installation can access that exact repository
```

Consequently, every team session can read any repository visible to the
built-in App by configuring it as a marketplace. This is intentional because
the App's installation access is considered server-wide. Deployments that do
not want this behavior must restrict the repositories selected when installing
the GitHub App.

Tokens must still be minted for exactly one repository with `contents:read`.
Do not request an unrestricted installation token.

## Implementation design

### Repository-specific resolver

Extract a function that does not consult ambient working-repository auth:

```go
type MarketplaceCredential struct {
    Username  string
    Token     string
    ExpiresAt time.Time
}

func ResolveMarketplaceCredential(
    ctx context.Context,
    appID string,
    pem []byte,
    repositoryURL string,
) (MarketplaceCredential, error)
```

The resolver must:

1. accept only a supported GitHub.com or configured GHES HTTPS URL;
2. normalize it to `(host, owner, repository)` and remove an optional `.git`;
3. create an App JWT from the built-in App ID and PEM;
4. discover the installation with `GET /repos/{owner}/{repo}/installation`;
5. mint a token with `repositories=[repo]` and `contents:read`;
6. return a sanitized error without GitHub authorization headers or bodies.

Do not reuse `GetGitHubToken` unchanged: it intentionally prioritizes
`GITHUB_TOKEN` and the configured installation ID. The marketplace resolver
needs explicit repository-discovery semantics.

### Cache

Cache installation discovery by `(App identity, host, owner/repository)` for a
short bounded period. Cache tokens by
`(installation ID, owner/repository, contents:read)` until five minutes before
expiry, with single-flight refresh. Do not persist either cache.

If the App ID or private key changes, invalidate both caches. Cache entries for
one repository must never satisfy another repository.

### Clone command

Change `cloneMarketplace` to accept an optional repository-specific credential.
For a private GitHub marketplace:

1. resolve a marketplace credential;
2. clone over HTTPS Git using a temporary credential helper or askpass file;
3. keep the token out of argv, the remote URL, and global Git config;
4. remove the temporary credential on success and failure;
5. verify that the stored `origin` URL contains no credentials.

Do not use process-wide `gh` authentication for this path. `gh repo clone`
would prefer the working repository's `GITHUB_TOKEN`.

Public marketplaces should remain usable when App discovery reports that the
App is not installed. In that case, retry once with anonymous HTTPS clone. Do
not fall back to the working token or a personal credential.

For an enabled plugin, failure of both App-authenticated and anonymous clone
must fail setup rather than only logging a warning. Otherwise the session starts
without the requested plugin and obscures the authentication error.

## Credential lifetime and placement

Marketplace clone happens during session setup, so it does not require a
renewable runtime broker. Resolve the token immediately before clone and retain
it only for that command.

The current startup path already receives the built-in App ID and PEM. The
initial implementation can perform discovery inside the setup process using
those values. It must not copy the resulting token into:

- process-wide `GITHUB_TOKEN` or `GH_TOKEN`;
- Claude/Codex settings;
- managed files or session archives;
- the agent runtime environment;
- logs or diagnostic responses.

After all marketplace clones complete, remove temporary credentials. The agent
continues to receive only its existing working-repository token.

Longer term, moving App JWT/token minting into the API process would allow the
PEM to be removed from workloads. That hardening is valuable but is not required
to fix cross-installation marketplace clone behavior.

## GHES and host handling

Use the marketplace URL's host to select the API endpoint:

- `github.com` uses `https://api.github.com`;
- the configured GHES host uses its configured API URL;
- any other host is rejected by the App resolver and follows the existing
  unauthenticated non-GitHub clone path.

Do not inherit `GH_HOST` from the working repository when cloning a marketplace
on another supported host.

## Failure handling and observability

Distinguish these results without exposing secrets:

- marketplace URL is invalid;
- built-in App ID/PEM is missing or invalid;
- App is not installed for the marketplace repository;
- installation lacks repository access or `contents:read`;
- GitHub rate limit, timeout, or outage;
- authenticated clone rejected;
- anonymous fallback rejected.

Log the normalized host/repository and result. Never log the PEM, App JWT,
installation ID, access token, authorization headers, GitHub response body, or
credential-helper output. Avoid repository and installation IDs as metric
labels.

## Rejected alternatives

- **Reuse the working token:** it is restricted to another repository and may
  belong to another installation.
- **Broaden the working token:** it still cannot span installations and would
  unnecessarily expose other repositories in the working installation.
- **Add marketplace installation IDs to team settings:** discovery already
  identifies the correct installation and avoids duplicated configuration.
- **Add a marketplace allowlist:** the built-in App's repository selection is
  intentionally the server-wide access boundary for this deployment.
- **Use GitHub Connections:** they are unrelated to the built-in App credential
  path involved here.

## Implementation slices

1. Extract repository normalization and the built-in App marketplace resolver,
   explicitly bypassing ambient `GITHUB_TOKEN`/`GITHUB_INSTALLATION_ID`.
2. Add installation/token caches scoped by host, repository, installation, and
   permission.
3. Update marketplace clone to use a per-command HTTPS Git credential and clean
   it up reliably.
4. Preserve anonymous public clone fallback and make enabled-plugin clone
   failures fatal.
5. Add sanitized logs, metrics, and documentation of the server-wide App access
   implication.

## Test plan

- existing team working-repository authentication remains unchanged;
- a marketplace in another organization discovers a different installation and
  clones successfully;
- the marketplace token is restricted to that one repository with
  `contents:read`;
- multiple marketplaces across different installations each receive their own
  token;
- marketplace resolution ignores working `GITHUB_TOKEN` and
  `GITHUB_INSTALLATION_ID`;
- a repository visible to the App can be selected without separate allowlist
  configuration;
- a repository not visible to the App falls back only to anonymous clone;
- public marketplaces still clone when the App is not installed;
- private inaccessible marketplaces fail setup when their plugin is enabled;
- malformed, credential-bearing, wrong-host, case-variant, `.git`-suffixed, and
  URL-escaped inputs normalize or reject deterministically;
- GitHub.com and GHES API/clone hosts do not leak configuration into each other;
- argv, `origin`, environment, global Git config, logs, generated settings,
  files, and archives contain no marketplace token;
- temporary credential files are mode `0600` and removed on success/failure;
- concurrent resolution never reuses a token for another repository.
