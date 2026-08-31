# Codex Device Auth credential save

Codex device auth (`POST /codex/device-auth`) runs `codex login --device-auth` and
persists the generated `~/.codex/auth.json` into the per-user credential Secret
(`agentapi-agent-files-<owner>`) via `KubernetesCredentialsRepository.Save`.

## Operational notes

- The backing KV store (LibSQL/garage adapter) enforces strict version checking on
  `Update` (`WHERE version = ?`). The repository always propagates the current
  Secret `ResourceVersion` to the `Update` call and retries the read-merge-write
  cycle on conflict, so a stale version never aborts a save.
- `device-auth/token` returns `status: denied` when the login subprocess succeeds
  but the credential **save** fails. Inspect the proxy logs for
  `[CODEX_AUTH] Failed to save credentials ...` to distinguish a save failure from
  a user-side denial of the device code.
- If `status` stays `denied` after a successful browser login, the most likely
  cause is a credential-save failure (Secret version conflict, KV store error),
  not an OpenAI-side rejection.

## Related

- Controller: `internal/interfaces/controllers/codex_device_auth_controller.go`
- Repository: `internal/infrastructure/repositories/kubernetes_credentials_repository.go`
- API: `GET /codex/device-auth/config`, `POST /codex/device-auth`,
  `POST /codex/device-auth/token`