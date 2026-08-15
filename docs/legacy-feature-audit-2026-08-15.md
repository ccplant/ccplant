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

## Suggested execution order

1. Land Batch 1 as small, independently testable PRs (frontend artifacts/dead UI, backend dead domain/use cases, generated docs, test duplication).
2. Add usage telemetry or inspect existing logs for routes, redirects, charts, Netlify, and backport workflow.
3. Deprecate externally reachable compatibility surfaces before Batch 2 removal.
4. Run frontend lint/tests/build, backend lint/tests/build, native checks, and Helm render tests for every affected batch.
