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

## Session usage snapshots from status events

The proxy persists an append-only event whenever a session changes state. Each
event contains the session, logical pool, scope, stable user or team principal,
status, and transition time. The current state is seeded once at proxy startup;
there is no periodic polling.

```yaml
session_count:
  enabled: true
  backend: libsql
  database_url: libsql://statistics.example
  auth_token: "..."
```

Events use the table `agentapi_session_status_events`. The following query
reconstructs the former snapshot at any timestamp (`:snapshot_at`). It selects
the last transition for every session and applies the same status categories as
the previous one-minute worker:

```sql
WITH ranked AS (
  SELECT *, ROW_NUMBER() OVER (
    PARTITION BY session_id ORDER BY occurred_at DESC, event_id DESC
  ) AS position
  FROM agentapi_session_status_events
  WHERE occurred_at <= :snapshot_at
), latest AS (
  SELECT * FROM ranked WHERE position = 1
)
SELECT pool, principal_id,
  SUM(CASE WHEN status IN (
    'active','stable','running','suspended','creating','starting','resuming',
    'restoring','suspending','stopped','error','timeout','unhealthy'
  ) THEN 1 ELSE 0 END) AS all_count,
  SUM(CASE WHEN status IN ('active','stable') THEN 1 ELSE 0 END) AS active_count,
  SUM(CASE WHEN status = 'running' THEN 1 ELSE 0 END) AS running_count,
  SUM(CASE WHEN status = 'suspended' THEN 1 ELSE 0 END) AS suspended_count
FROM latest
GROUP BY pool, principal_id;
```

Authenticated clients can also request interval-based runtime statistics for a
personal scope or accessible team:

```text
GET /session-usage/dashboard?from=2026-09-30T15:00:00Z&to=2026-10-31T15:00:00Z&timezone=Asia%2FTokyo
GET /session-usage/dashboard?team_id=example/team&pool=linux&from=2026-10-01T00:00:00Z&to=2026-11-01T00:00:00Z
```

The response includes total runtime and running seconds, suspended time, peak
concurrency, daily buckets, and the highest-usage sessions. Runtime includes
`creating`, `starting`, `active`, `stable`, `running`, `resuming`, `restoring`,
and `suspending`. It excludes suspended, stopped, error, timeout, unhealthy,
and terminated intervals. The repository carries the last status before
`from` into the requested range. A range may not exceed 90 days; an ongoing
range is clipped to the response's `as_of` timestamp.

Generating one row per minute is now a SQL concern: join the same latest-event
logic against a recursive minute series. Status-event writes are idempotent by
`event_id`, and a recorder failure never blocks session status propagation.

Successful session deletion is recorded as the terminal `terminated` status so
the latest-event query no longer counts the deleted session. The recorder
captures pool and principal dimensions before destructive cleanup and writes
the terminal event after deletion succeeds. See
[Session deletion usage-event design](session-delete-usage-event-design.md) for
the deletion-path, retry, and failure semantics.
