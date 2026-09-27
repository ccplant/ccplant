# Usage statistics

AgentAPI Proxy can persist response-level token usage in a dedicated libSQL
database. This database is independent from the application KV store and uses
the fixed table name `agentapi_usage_events`.

```yaml
config:
  usage:
    enabled: true
    databaseUrlSecretRef:
      name: agentapi-usage-libsql
      key: database-url
    authTokenSecretRef:
      name: agentapi-usage-libsql
      key: auth-token
```

The referenced Secret is operator-managed. The chart does not create or copy
the database URL or token. When collection is enabled, both Secret references
are required. They are exposed to the proxy as `AGENTAPI_USAGE_DATABASE_URL`
and `AGENTAPI_USAGE_AUTH_TOKEN`.

At every supported agent Stop hook, `agentapi-proxy client report-usage` reads
the local transcript, extracts response usage metadata, and submits it to the
proxy. Prompt and response bodies are not submitted. Event identifiers are
stable, so replaying a transcript does not count the same response twice.

Authenticated clients can retrieve totals for their personal scope or an
accessible team:

```text
GET /usage?from=2026-08-01T00:00:00Z&to=2026-09-01T00:00:00Z
GET /usage?team_id=example/team
GET /sessions/{sessionId}/usage
```

For browser-side analytics, authorized raw events can be exported as Parquet:

```text
GET /usage/export.parquet?from=2026-08-01T00:00:00Z&to=2026-09-01T00:00:00Z
GET /usage/export.parquet?team_id=example/team&model=gpt-5
```

The export defaults to the last 30 days and is limited to a 90-day range and
100,000 events. The proxy applies personal or team authorization before
generating the file. It includes timestamps, session/model identifiers, and
token counts, but excludes user IDs, team IDs, event IDs, and message content.
The frontend helper in `src/lib/usage-parquet.ts` loads this file into a local
DuckDB-Wasm `usage_events` view so visualization SQL remains browser-local.

## Active session count snapshots

The proxy can also periodically persist active session counts by logical pool
and stable user or team principal. This collector has an independent backend
selection and repository interface; `libsql` is currently implemented.

```yaml
session_count:
  enabled: true
  backend: libsql
  database_url: libsql://statistics.example
  auth_token: "..."
  check_interval: 1m
```

Snapshots use the table `agentapi_session_count_samples`. Counts are derived
from durable, user-visible session routes; allocations only backfill the pool
for legacy routes. `all_count` includes routes in a recognized UI status but
excludes `terminating`, deletion-pending, incomplete, and orphaned records.
`active_count` includes sessions whose public
status is `active` or `stable` (the green/available UI state), while
`running_count` includes sessions whose public status is `running` (the yellow
UI state), and `suspended_count` includes sessions whose public status is
`suspended`. Suspended sessions are excluded from active and running counts.
Samples are change points: the worker
writes the initial value and subsequent changes, but does not repeat an
unchanged count every minute. Previously observed and explicitly bound
pool/principal pairs receive a row containing zero after their last session
stops. Consumers reconstruct a time series by carrying the last value forward.
