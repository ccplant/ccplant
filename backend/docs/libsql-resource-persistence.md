# libSQL resource persistence

The application repository interfaces continue to define the persistence
boundary. Kubernetes deployments implement those interfaces with Secrets and
ConfigMaps. When `kv_store.backend` is `libsql` or `libsql-encrypted`, the
Kubernetes compatibility adapter now routes each repository resource to a
dedicated SQL table instead of putting all application data in `agentapi_kv`.

The resource tables are:

- `agentapi_settings`, `agentapi_credentials`, and `agentapi_user_files`
- `agentapi_shares`, `agentapi_team_configs`, and `agentapi_user_team_mappings`
- `agentapi_personal_api_keys`, `agentapi_api_tokens`, and `agentapi_local_users`
- `agentapi_sandbox_policies` and `agentapi_sandbox_domains`
- `agentapi_session_routes` and `agentapi_session_profiles`
- `agentapi_webhooks`, `agentapi_slackbots`, and `agentapi_schedules`
- `agentapi_codex_auth_attempts` and `agentapi_codex_auth_locks`
- `agentapi_system_settings`

Each table has its own primary key and ownership index. The stored document is
kept in the Kubernetes-compatible representation so the repository behavior,
envelope encryption, and Kubernetes/libSQL replication remain identical.
Version columns retain optimistic concurrency semantics.

## Compatibility and migration

On startup, classified rows from the legacy `agentapi_kv` table are moved to
their resource table transactionally. The migration is idempotent and keeps
the version, ownership scope, metadata, encrypted or plaintext value, and
update timestamp unchanged. Unknown extension records remain in
`agentapi_kv`; this fallback prevents a new or third-party resource from being
lost before it receives an explicit table mapping.

Reads and bounded list operations cover both the resource tables and the
fallback table. Deploy the database-writing processes from the same release
during this schema transition; an older process only knows `agentapi_kv` and
must not write concurrently with the resource-table migration.
