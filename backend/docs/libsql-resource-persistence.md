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

## Migrating from Kubernetes

`kv-store migrate` reads application-owned Secrets and ConfigMaps from the
Kubernetes backend, reconstructs their canonical labels, and writes them
through the destination store. For a `libsql-encrypted` destination, the
command requires an active encryption key ID and key configuration. It fails
before copying records when either is missing; it never silently falls back to
plaintext libSQL.

```sh
agentapi-proxy kv-store migrate \
  --primary-backend kubernetes \
  --secondary-backend libsql-encrypted \
  --secondary-database-url "$DESTINATION_DATABASE_URL" \
  --secondary-auth-token "$DESTINATION_AUTH_TOKEN" \
  --destination-namespace agentapi-ui \
  --encryption-provider cloud-kms-branch-scoped \
  --encryption-active-key-id production \
  --encryption-keys-json "$DESTINATION_KMS_KEYS"
```

Run with `--dry-run` first and stop writers for the actual copy. Afterward,
run `kv-store verify` with the corresponding primary and secondary encryption
configuration. The migration is restart-safe: identical destination values
are skipped, while different existing values fail as conflicts unless an
operator explicitly selects `--overwrite`.

The integration test covers every dedicated resource mapping from a
Kubernetes source through an encrypted libSQL destination. It asserts that
each raw SQL value is an `agentapi-kv-envelope/v1` envelope, decrypts to the
original Kubernetes document, lands in its dedicated table, and leaves no
classified row in the fallback table.
