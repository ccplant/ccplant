# libSQL query-load optimization iterations

## Pre-iteration rollout attempt 1 — startup backfill failure and rollback

- Source: `c04a70970a9f8fadb234a362ccb052eecf7af721`
- Image workflow: `ccplant/ccplant` run `42152422433` (success)
- Fly deployment: `ccplant/ccplant-deploy` run `42153306086` (failed)
- Failed image: `ghcr.io/ccplant/ccplant-api@sha256:b4bc408752fa69248ba0aa53d0b5b849e4e56849fda5dc142b7abd977943d02a`
- Failure: the new query-column migration replayed the existing row-by-row
  backfill over `agentapi_kv`; Turso's pipeline request exceeded the 30-second
  startup context deadline.
- Recovery: API machine `2870160b500508` was rolled back to release 745 image
  `sha256:1fd2b346cf368e986268ebd1a62982e8f55cf4b62554e190135d75488b7f18d1`.
  `/health` returned `status=ok` afterward.
- Improvement: preserve the already-applied v2 migration marker and backfill
  only the three new session-runner columns, using one set-based `UPDATE` per
  dedicated table instead of one remote statement per KV row.
- Verification: complete backend tests and changed-code lint passed locally.

This attempt did not reach session traffic generation or post-deployment query
statistics because rollout health failed, so it does not count toward the ten
required complete iterations.

## Iteration 1

- Deployed commit: `c7cf7868839cbcfc55ee110a517798c28e3459b6`
- Fly health: `ok`; machine version 747, 1/1 checks passing.
- Load session: `95593983-43e5-4d3a-9146-dd58fa7d4c34`; proxy status reached `stable`.
- Traffic: session start plus 12 authenticated `/search` polls and a direct status read.
- Turso observation: the top-query window remains dominated by cumulative,
  pre-deployment `substr(key, ...)` shapes. The new dedicated
  `agentapi_session_runners` table contains 11 rows and is serving traffic.
- Query-plan finding: allocation lookup with namespace, key prefix, pool, and
  status selected the `(namespace,key)` primary key instead of the existing
  `(runner_pool,namespace)` index.
- Next improvement: replace runner pool indexes with
  `(runner_pool,namespace,key)`, preserving the index count while enabling pool
  narrowing and key-prefix range/order in one lookup.
