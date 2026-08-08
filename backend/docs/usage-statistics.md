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
