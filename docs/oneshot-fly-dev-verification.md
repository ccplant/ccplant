# Fly development oneshot verification — 2026-09-14

PR: https://github.com/ccplant/ccplant/pull/306

## Deployment

API and worker were updated to code commit `7adad9be4dbfbfe7a39973d20352b3d6495d4f04` using an image-only Fly Machines update, preserving existing machine configuration.

- API: `ccplant-api-dev`, https://ccplant-api-dev.fly.dev
- Worker: `ccplant-worker-dev`
- Image: `ghcr.io/ccplant/ccplant-api@sha256:da7221cb490394e750bc1a04ac24a7ef152b42fbf1a63f4d39dd1c46164b26fb`
- Both machines reported started; API health passed and worker binary reported the deployed version. The connected user session manager also reported this version.
- [CI](https://github.com/ccplant/ccplant/actions/runs/34831274422) passed, including the backend test suite.

## Live verification

Tests used isolated resources owned by the authenticated test user and the existing default codex-acp session profile. For the successful final runs below, messages contained the expected model response and ACP `end_turn`, and the session subsequently disappeared from `/search` automatically. Each session carried `oneshot=true` and `session_ttl=1m` and received a runner allocation.

Monitoring used `/search` and `/{session_id}/messages`. It did not call `/{session_id}/status` or manually delete these successful sessions. Deletion times are observation times, not exact cleanup latency.

| Path | Session ID | Expected response | Auto-deletion observed (UTC) |
| --- | --- | --- | --- |
| custom-manual | `4e90ec88-5665-49db-9379-1ad85eadfd63` | `ONESHOT_CUSTOM_MANUAL_PAYLOAD_OK` | 2026-09-14T10:15:29.446Z |
| github-receive | `84b16845-3a45-4753-85f4-3917c9649136` | `ONESHOT_GITHUB_RECEIVE_PAYLOAD_OK` | 2026-09-14T10:17:04.046Z |
| schedule-manual | `5b0c3810-0273-414c-b8b1-63c07e52ac13` | `ONESHOT_SCHEDULE_OK` | 2026-09-14T10:17:04.046Z |
| custom-receive | `44b1802f-ab5e-4f51-8266-d84ebb18f974` | `ONESHOT_CUSTOM_RECEIVE_PAYLOAD_OK` | 2026-09-14T10:17:04.046Z |
| schedule-automatic | `c0345c66-4c58-41c5-8572-0c303b525cf3` | `ONESHOT_AUTOMATIC_OK` | 2026-09-14T10:20:41.883Z |

Custom webhook tests read `/opt/webhook/payload.json` using the runtime shell and returned the payload's expected value. The custom receive request used an HMAC signature. The GitHub receive test used a signed synthetic `issues/opened` delivery and rendered a payload value through the message template; it did not create a real GitHub issue. GitHub ingress does not currently mount the raw payload file, so that was not asserted.

## Findings and limitations

- The first deployed revision exposed a cleanup race: stale manager `starting` heartbeats could replace runtime completion and delay TTL cleanup. Commit `7adad9b` ignores these coarse startup statuses for direct oneshot sessions. A regression test failed before the fix and passed afterward; the final tests above required no status correction.
- A separate allocation failure remains: initial final-version schedule session `2efaf02e-8809-4227-9608-cdf81429cb74` stayed in `starting`. At 10:10:30 UTC the API logged removal of claiming runner `3889689e-b098-4ca1-8d22-fed753cf9233` because it was absent from the manager inventory. The runner lookup subsequently returned 404 and messages returned 503. Retrying the schedule succeeded as recorded above. This run is a failure, not an automatic-cleanup success; the underlying allocation reconciliation issue is not fixed by this PR.
- One final-version custom receive attempt auto-deleted before its response was captured; it was rerun to obtain complete evidence. Earlier fixture mistakes (wrong custom payload path and assuming a GitHub payload mount) were corrected and excluded from the passing results.

## Cleanup

Both isolated schedules and both webhooks were deleted and their GET endpoints returned 404. No test sessions remained in `/search` at final verification. The failed orphaned schedule session was explicitly deleted; the five passing sessions above were automatically deleted. Both pre-existing user sessions were still present.
