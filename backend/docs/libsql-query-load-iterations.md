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

## Iteration 2

- Deployed commit: `b12bf4b1a4fab7227871344b793b0e302a8bdee3`
- Image workflow: `42155832545` (success); deployment workflow: `42156346239`.
- Fly health: `ok`; reported version matches `b12bf4b`.
- Load session: `f670ad18-34e7-4aad-8136-98ea4780dbf8`; reached `stable`.
- Verification: Turso `EXPLAIN QUERY PLAN` now selects
  `agentapi_session_allocations_runner_pool_lookup` with
  `(runner_pool,namespace,key range)`; `PRAGMA index_info` confirms all columns.
- Observation: point reads and writes still locate a runner key by unioning all
  physical resource tables, although runner key prefixes uniquely identify a
  dedicated table.
- Next improvement: route the seven session-runner key prefixes to their exact
  dedicated table plus only the legacy fallback table.

## Iteration 3

- Deployed commit: `b9d5e0781c5d59a5f5b726401f067295bcc204b6`.
- Image workflow: `42156774966` (success); deployment workflow: `42157260289`.
- Fly health reported the matching version; load session
  `84e79bfd-a31e-46b3-8ad7-5be72eaacd23` reached `stable`.
- Observation: a runner's table is immutable after resource migration, but
  repeated point updates still execute the same table-location union.
- Next improvement: cache only the physical table location (never record
  contents), populate it on create/lookup, and evict it on delete. This removes
  repeated location SQL without introducing stale record data.

The later same-image restarts used while clarifying the requested process are
excluded from the iteration count because they contained no new improvement.

## Iteration 4

- Deployed commit: `e86c0a7c8042ead7c6a86ba11c9a9f81ff77e14d`.
- Image workflow: `42157818955` (success); deployment workflow: `42158303894`.
- Fly health reported the matching version; load session
  `a3650700-c7c9-451d-acd4-7379d8cfc016` reached `stable`.
- Turso's cumulative top-query window was unchanged; no new legacy query shape
  displaced the existing historical entries.
- Observation: a dedicated-table List knows the physical table for every
  returned record, but did not warm the location cache. Claiming a listed
  allocation therefore still needed a subsequent location lookup before update.
- Next improvement: warm table-location entries from single-table List results.

## Iteration 5

- Deployed commit: `d8ed240828158fcd961d29117fa9f35bee6b5ff9`.
- Image workflow: `42158661999` (success); deployment workflow: `42159120832`.
- Fly health reported the matching version; load session
  `5c869225-ccba-47db-8b2d-f8b4f4612227` reached `stable`.
- Turso's cumulative top-query shapes remained unchanged after the load.
- Observation: a cold runner Get still uses one query to locate its table and a
  second query to fetch the record.
- Next improvement: fetch table name and record together from the exact runner
  table plus legacy fallback using one `UNION ALL ... LIMIT 1` round trip.

## Iteration 6

- Deployed commit: `3ce5d770f9434820c849651c5ce58e2b32702756`.
- Image workflow: `42159421777` (success); deployment workflow: `42159917467`.
- Fly health reported the matching version; load session
  `d7097e7c-dcf0-4bd8-b1b2-85979f308ce2` reached `stable`.
- Turso's cumulative top-query shapes remained unchanged after the load.
- Observation: session-runner Create performs a preflight existence query even
  though its key prefix identifies exactly one table and INSERT's primary-key
  constraint already provides atomic conflict detection.
- Next improvement: skip that redundant preflight for correctly routed
  session-runner records while preserving duplicate conflict behavior.

## Iteration 7

- Deployed commit: `500c339aedb141fa6be625d41aa25ce5af55c68b`.
- Image workflow: `42160281451` (success); deployment workflow: `42160931066`.
- Fly health reported the matching version; load session
  `04a78aca-6404-4c98-9a85-ebc30393d2b2` reached `stable`.
- Turso's cumulative top-query shapes remained unchanged after the load.
- Observation: List executes `ORDER BY key` remotely and then unconditionally
  sorts the records by key in Go. For multi-table unions the remote ordering
  requires a merge/temporary sort that does not affect the final contract.
- Next improvement: remove the SQL ordering and retain the existing Go sort.

## Iteration 8

- Deployed commit: `75c463c5aa065043a81f9f08a8de77592f9b9fb8`.
- Fly health reported the matching version; load session
  `3785a1a7-0d6a-4bb7-970c-40b2e13baf38` reached `stable`.
- Turso's cumulative top-query shapes remained unchanged after the load; the
  historical legacy query still includes `ORDER BY`, while newly generated
  queries no longer add it.
- Observation: a cold Update has both resource labels and an unambiguous runner
  key but still performs a table-location query first.
- Next improvement: route an Update directly when key and labels independently
  identify the same dedicated table, while honoring cached legacy locations.

## Iteration 9

- Deployed commit: `1da1e029c9e18a4a8622e19e188fc5b1153d55cc`.
- Fly health reported the matching version; load session
  `5051826f-5622-47d4-8bdc-aee013c35f53` reached `stable`.
- Turso's cumulative top-query shapes remained unchanged after the load.
- Observation: a cold Delete still locates the physical table before deleting,
  although a runner key uniquely identifies its dedicated table.
- Next improvement: attempt the versioned DELETE directly on that table and
  fall back to location discovery only for legacy, missing, or conflict cases.
