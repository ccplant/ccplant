# Legacy feature audit (2026-08-15)

## Scope and method

The audit covered every top-level subtree recursively: `.github`, `backend`, `chart`, `docs`, `frontend`, `native`, and `scripts`. Pi coding agent was run read-only with `ollama-cloud/glm-5.2` for each subtree. Its findings were then checked with repository-wide reference searches, route and dependency wiring inspection, and Git tracking state. No legacy implementation was removed in this change.

Confidence means confidence that the item is obsolete or redundant, not confidence that it is safe to remove without the stated check.

## Recommended removal batches

### Batch 1 — remove now

| Area | Candidate | Evidence | Confidence | Removal notes |
| --- | --- | --- | --- | --- |
| frontend | `src/utils/pushNotification.ts.backup` | Tracked backup file; no import; replaced by `pushNotification.ts`. | High | Delete the file. |
| frontend | `src/components/MCPServerSettings.tsx` | No imports. The active implementation is `src/components/settings/MCPServerSettings.tsx`. | High | Delete the duplicate root component. |
| frontend | `src/hooks/useTheme.ts` | Unreferenced re-export shim. | High | Delete; also consider removing the unused `useTheme` export from `ThemeContext.tsx`. |
| frontend | `src/lib/subscriptions.ts` | No imports; describes itself as temporary; current push notification storage uses other modules. | High | Delete. |
| frontend | `src/utils/repositoryHistory.ts` | No imports. The active history implementation/types live in `organizationHistory.ts` and `types/settings.ts`. | High | Delete. |
| frontend | `src/components/settings/{ExperimentalSettings,GoogleOAuthSettings,MemorySettings,ACPServerSettings,LogoutButton}.tsx` | Only referenced by barrel exports; no rendered/imported consumers. | High | Delete components and matching exports in `settings/index.ts`. |
| frontend | `playwright-report/index.html`, `test-results/.last-run.json` | Tracked generated files even though both directories are ignored. | High | Remove from Git; ignore rules already exist. |
| frontend docs | `frontend/docs/_book/` | 56 tracked Honkit build artifacts; source Markdown and `honkit build` already exist. Contains orphan pages with no source. | High | Delete directory and add `/docs/_book/` to `frontend/.gitignore`. Confirm it is not used directly by a static hosting job. |
| backend | `internal/usecases/session/session.go` Create/List/Get/Delete use cases | Constructors and types have no production caller; active controller paths use `LaunchUseCase`/`SessionManager`. | High | Preserve `ValidateTeamAccessUseCase` in this file. |
| backend | `internal/usecases/share/` | Package has no imports; active `ShareController` uses the repository directly. | High | Delete package; preserve controller and repository. |
| backend | `internal/domain/entities/repository.go` | Entity and constructors have no repository references outside their own package. | High | Delete and verify with Go build/tests. |
| scripts | Duplicate assertion at `scripts/test-helm-render.sh:232` | Exact duplicate of line 230 against the same output file and string. | High | Delete the duplicate assertion only. |

### Batch 2 — remove after repository-level verification

| Area | Candidate | Evidence | Confidence | Required check |
| --- | --- | --- | --- | --- |
| backend | Legacy `AuthController` cluster and DI wiring | Controller has no registered route; its controller/use-case fields are only constructed in `internal/di/container.go`. | High | Remove controller, obsolete auth use cases, tests, and DI fields together. Preserve `ValidateAPIKeyUseCase` and active auth middleware. Update `backend/docs/architecture.md`, which still describes this cluster. |
| native | `native_is_installed` Tauri command | Registered in `invoke_handler` but never invoked by the bundled UI; setup state is inferred from `fetchInstances`. | High | Confirm no external Tauri caller/plugin invokes it dynamically. |
| frontend | `SandboxDomainsButton.tsx` and `getSessionSandboxDomains` | Component has no imports; API method's only caller is that component. | High/Medium | Confirm there is no planned UI for the existing endpoint, then remove both. |
| frontend | `/agents` page plus `getAgent(s)` and Agent-only types | Page has no internal navigation/link and is the only consumer of agent-list APIs/types. | Medium | Check analytics and external bookmarks before removing the route and client surface. |
| frontend | Duplicate PWA manifests: `app/manifest.ts` and `app/api/manifest/route.ts` | Two implementations produce substantially the same manifest; layout explicitly links `/api/manifest`. | Medium | Browser-test install/update behavior, choose one canonical endpoint, and remove the other. |
| frontend | Redirect-only routes `/settings/admin` and `/settings/sandbox-policies` | No internal links target the old paths. | Medium | Check access logs/bookmarks and announce deprecation before removal. |
| backend | `SlackSignatureVerifier` | Production references are absent; only its tests use it. | Medium | Confirm custom Slack webhook receivers do not instantiate it dynamically or require its behavior. |
| backend | `pkg/utils` unused helpers and unused `TTLCache` methods | Production references are absent; remaining references are tests. | Medium | Decide whether `pkg/` is a supported external Go API. If not, remove helpers and tests together. |
| backend | `netlify.toml`, `netlify/functions/api.js`, `public/api/*.json` | Fixed-response demo assets have no build, route, CI, or documentation references; Go static serving embeds `spec.FS`, not these files. | Medium | Confirm no externally configured Netlify site consumes them. |
| docs | `backend/docs/xapi.md` | Unlinked duplicate of API docs; says authentication is absent and references two nonexistent scripts. | Medium-high | Check external inbound links, then delete or replace with a redirect/link to current API/OpenAPI docs. |
| docs | `frontend/docs/CONTAINER_SETUP.md`, `PR_URL_INTEGRATION.md` | Not in Honkit `SUMMARY.md` and have no repository references. | Medium | Confirm they are not externally linked. |
| GitHub Actions | `.github/workflows/backport.yml` | Manual-only sync to old split repositories; monorepo is documented as the sole development source. | Medium | Check last runs, `BACKPORT_TOKEN`, old repository activity, and downstream users. |
| GitHub Actions/Helm | Publishing and testing split `agentapi-proxy`/`agentapi-ui` charts | Documentation identifies split charts as compatibility artifacts while the umbrella chart is current. | Low | Use registry pull/installation telemetry and publish a retirement window before removal. |
| GitHub Actions | `publish-dev-charts` | No documented in-repository consumer for `-dev.ccplant.<sha>` chart versions. | Low | Check GHCR downloads and team deployment workflows. |

### Batch 3 — cleanup or documentation fixes, not feature removal

| Area | Candidate | Recommendation |
| --- | --- | --- |
| scripts | Temporary `backend-invalid-*.yaml` and `backend-replicas.yaml` outputs | Redirect to `/dev/null` where contents are never asserted; retain the Helm exit-status tests. |
| chart | `chart/ccplant/templates/NOTES.txt` service names | Render and verify names. Static inspection suggests `-backend`/`-frontend` may not match subchart fullname helpers; fix rather than remove. |
| native docs | README component tree, tray menu, and command table | Update stale/incomplete documentation; the underlying UI commands are mostly active. |
| frontend docs | `PWA_ICONS_NEEDED.md`, `IMPLEMENTATION_SUMMARY.md` | Archive or delete completed implementation notes according to documentation policy. |
| backend misc | `misc/acp_e2e.go`, `misc/start_session_with_tags.sh` | Unreferenced manual tools; move into documented developer tooling or delete after maintainer confirmation. |
| backend examples | `examples/webhooks/` | Unlinked but potentially useful guides; integrate into current docs before deleting. |

## Explicitly retained compatibility paths

The following were examined and should not be removed solely because they contain “legacy” or migration language:

- Backend legacy in-process manager routes, API-token migration, Kubernetes `env_vars` fallback, and session-manager runtime compatibility paths are active upgrade/runtime behavior.
- Helm nil-safe and `--reuse-values` assertions protect supported upgrades.
- Native version and sidecar build scripts are called by CI/release/Tauri configuration.
- `Chart.lock` is a valid reproducibility artifact despite local `file://` dependencies.
- Split Helm chart CI/release remains necessary until downstream use is measured and formally retired.

## Options, modes, compatibility, and migration surfaces

This follow-up pass specifically traced switches and maintenance commands. These are best removed as complete surfaces rather than as isolated branches.

### Strong removal candidates

| Removal set | Current/legacy behavior | Evidence and removal boundary | Exit condition |
| --- | --- | --- | --- |
| `LoadConfigLegacy` | Dedicated JSON decoder versus the current Viper-backed JSON/YAML/environment loader. | `backend/pkg/config/config.go:1467`; no caller exists. Delete function and its now-unused imports/tests. | Go build/tests pass. No deprecation window is needed for an uncalled Go symbol unless `pkg/config` is a supported external library. |
| Old frontend proxy URL Helm value | `global.proxyUrl` is preferred; `oauthOnlyMode.proxyUrl` is only a fallback. | `frontend/helm/agentapi-ui/templates/deployment.yaml:66-70`. Remove fallback plus the old values/schema/docs entries together. | Render deployed release values and confirm `oauthOnlyMode.proxyUrl` is absent everywhere. |
| Legacy native `PublicURL` input | Native config accepts `public_url`, but outbound control does not use it. | `backend/cmd/native_session_manager.go:51`; remove field, serialization/input docs, and tests that only round-trip it. Do not confuse it with the still-used parent/session-manager `PublicURL`. | Inspect native daemon configuration files before removing or silently ignore for one release. |
| `helpers init` alias | Same handler as `helpers setup-claude-code`; no behavior difference. | `backend/cmd/helpers.go:48-61`. Remove alias registration/help/tests only. | Search automation and shell history/telemetry; announce replacement first. |
| Deprecated KV migration flags/env | Old `--database-url`, `--auth-token`, `AGENTAPI_KV_STORE_DATABASE_URL`, and `AGENTAPI_KV_STORE_AUTH_TOKEN` synthesize Kubernetes→libSQL defaults. | `backend/cmd/kv_store_migrate.go:106-121`. Keep `kv-store migrate`, but remove the legacy fields, flags, `storeConfigs` branch, docs, and tests. | Confirm no jobs invoke deprecated flags/env; warn for at least one release if CLI telemetry is unavailable. |

### Remove after migration completion

| Removal set | Purpose | Full removal boundary | Proof migration is complete |
| --- | --- | --- | --- |
| `oneshot migrate-credentials` | Converts `agentapi-credentials-*` and `agentapi-agent-env-*` Secrets into `agentapi-agent-files-*`. | `backend/cmd/oneshot.go`, `migrate_credentials.go`, command registration/tests, and obsolete Secret handling in doctor/prune paths. | No source Secrets with the documented labels/prefixes remain in any supported cluster. |
| `helpers migrate` | Verifies unified `agentapi-settings-*` then removes derived `mcp-servers-*`, `marketplaces-*`, and `agent-env-*` Secrets. | `backend/cmd/migrate.go`, registration/tests/docs, then legacy repository reads only after data inventory. | No derived Secrets remain across namespaces; new versions have not written them for the agreed support window. |
| `helm migrate` and `helm migrate-values` | Moves split `agentapi-proxy`/`agentapi-ui` releases and values to the `ccplant` umbrella release. | `backend/cmd/helm*.go` migration commands/tests, `docs/helm-chart-migration.md`, split-chart backport/release paths, and old root Helm values after the final upgrade window. | No split releases, legacy sessions, or downstream pulls of split charts remain. This should be the last migration surface removed. |
| Startup API-token migration | Copies legacy personal/team credentials into named tokens on every startup and retains shadowing logic to make revocation safe. | `api_token_migration.go`, server wiring/call, deterministic migration helpers/annotations/tests, and only later the legacy auth stores/bootstrap/shadow map. | Migration logs report no legacy inputs across all replicas; inventories show no personal/team legacy sources. Remove data readers only after old credentials are deleted, otherwise revocation semantics regress. |
| Schedule Secret migration | Imports the legacy aggregate schedule Secret into per-schedule storage. | `MigrateFromLegacy`, startup call, legacy Secret constant/reader, tests and docs. | Legacy Secret is absent and all schedules exist in the new repository in every environment. |
| Legacy HMAC manager routes | `sessionManager.hmac_secret` registers the old external-manager HTTP routes in addition to current internal allocation/control routes. | `session_manager_runtime.go:189-201`, `externalmanager` handlers, config/Helm values, client call sites and tests. | Route metrics/access logs show zero old-route traffic and all managers use connection-token allocation/control. |
| Legacy managed-file Secret merge | New managed-file storage merges files that only exist in `agentapi-agent-files-*` legacy Secrets. | `kubernetes_session_manager.go:4408+`, legacy Secret conversion helpers and tests. | Inventory confirms all files are in the current credentials/managed-file repository and old Secrets are absent. |
| Slack last-message annotation fallback | Reads `agentapi.proxy/slack-last-message-at` when the current annotation is missing. | Cleanup worker and Kubernetes session parsing fallback/tests. | No live/restorable session contains only the old annotation after the maximum session retention period. |

### Modes to retain

- `auth_mode=oauth|bedrock` is a real provider choice with active settings UI and environment materialization; it is not a legacy toggle.
- `claude-legacy`, `auto`, `claude-acp`, `codex-acp`, `pi-ollama`, and `cursor` are active agent modes and must be retained.
- KV replication modes, sandbox `count_mode`, dry-run flags, native/External Session Manager modes, and ACP session modes have active consumers or provide safety for destructive operations.
- `kv-store migrate/verify`, `doctor`, prune commands, native lifecycle commands, and client resource commands are ongoing operational tools. Retire only their explicitly deprecated inputs.
- Docker entrypoint/wrapper scripts, native sidecar/version scripts, and Helm render tests are invoked by build or runtime paths.

The Helm migration/control-plane subcommands are planned for use and must be retained. Removal of root `kubernetesSession` and the in-process manager mode is explicitly deferred.

## Suggested execution order

1. Land Batch 1 as small, independently testable PRs (frontend artifacts/dead UI, backend dead domain/use cases, generated docs, test duplication).
2. Add usage telemetry or inspect existing logs for routes, redirects, charts, Netlify, and backport workflow.
3. Deprecate externally reachable compatibility surfaces before Batch 2 removal.
4. Run frontend lint/tests/build, backend lint/tests/build, native checks, and Helm render tests for every affected batch.
