# libSQL query load runbook

Use Turso's query statistics after each backend deployment to verify that KV
changes reduce both statement count and rows read:

```bash
turso db inspect ccplant-dev --queries --verbose
turso db inspect ccplant --queries --verbose
```

For session-runner traffic, verify the following:

- Queries target `agentapi_session_allocations`, `agentapi_session_runners`, or
  another dedicated session-runner table instead of probing every resource
  table.
- Key-prefix predicates appear as `key >= ? AND key < ?`; `substr(key, ...)`
  indicates a regression that prevents a primary-key range scan.
- Allocation claims include materialized `runner_pool` and `runner_status`
  predicates. JSON extraction should not be used for these labels.
- Rows read should track the number of candidates in the requested pool, not
  the total number of KV records.

Compare statistics over an equivalent traffic window. A large write count on
an `UPDATE` can include secondary-index maintenance and does not by itself
prove an N+1 call pattern. Repeated, structurally identical `SELECT` statements
against different resource tables do.
