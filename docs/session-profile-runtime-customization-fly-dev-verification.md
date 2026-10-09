# Session profile runtime customization — Fly dev verification

Date: 2026-10-08 UTC

Implementation commit: `60ebe4c705a6557a46245e2c42b98c249cbc9ca2`

PR: https://github.com/ccplant/ccplant/pull/443

## Deployment

- The API, session runtime, and frontend images were built by
  https://github.com/ccplant/ccplant/actions/runs/37855861517.
- The downstream dev deployment completed successfully:
  https://github.com/ccplant/ccplant-deploy/actions/runs/37856556884.
- `https://ccplant-api-dev.fly.dev/health` returned HTTP 200 and version
  `dev.ccplant.60ebe4c705a6557a46245e2c42b98c249cbc9ca2`.
- The `ccplant-session-dev/ccplant-session` deployment ran the matching API image.

## Configuration ownership

The verification used the intended ownership split:

- the session profile supplied `command_wrapper_template`;
- the Session Manager supplied `AGENTAPI_K8S_SESSION_IMAGE`;
- the profile API did not persist or return the removed `asset_image` field.

The Manager image setting was
`ghcr.io/ccplant/ccplant-agent:assets-4b37a396b0634dac6b9a5dd6614af405`.

## Manager API image update

Manager `5095c058-a00c-4a91-bc22-242f5ffb35e8` was patched through
`PATCH /session-managers/{id}` to use the digest form of the same tested asset image:

```text
ghcr.io/ccplant/ccplant-agent@sha256:2d230b6c90a1e09c1ac54844cdb51655520bdf01be92981e4e9f10f3b89b4dc1
```

| Check | Result |
| --- | --- |
| Initial API status | `pending` |
| Heartbeat convergence | `ready` with matching `applied_asset_image` |
| Invalid image reference | HTTP 400 |
| Old idle `fly-dev` stock | Both runners removed |
| Replacement idle `fly-dev` stock | Two new runners, both using the digest reference |
| Stock owned by the canary Manager | Unchanged |
| Session allocated from replacement stock | Running on the digest reference |

The desired image was then restored to the original tag. The API again converged from `pending`
to `ready`, and two fresh idle runners used the restored tag. The already allocated digest-based
runner remained running during this second replacement, confirming that reconciliation removes
idle stock without disrupting allocated sessions. The verification session was then deleted with
HTTP 202.

## Runtime verification

A temporary profile targeting pool `fly-dev` used this wrapper:

```text
printf passed > /tmp/ccplant-wrapper-test; exec {{ .Command }}
```

Starting a session with that profile allocated runner
`6ee5ad3e-441f-4b53-97f0-b4e9f41ec80b`. Direct inspection in namespace
`ccplant-session-dev` confirmed:

| Check | Result |
| --- | --- |
| Allocated workload image | `ghcr.io/ccplant/ccplant-agent:assets-4b37a396b0634dac6b9a5dd6614af405` |
| Image equals the Session Manager setting | Passed |
| `/tmp/ccplant-wrapper-test` contents | `passed` |
| Wrapper command continued into the agent process | Passed; session became active |

This also verifies the pooled-session propagation path. The first attempt exposed that the
resolved profile wrapper was not copied from `StartRequest` into `RunServerRequest`; commit
`92171b47` fixes that path and adds regression coverage.

The temporary session and the earlier failed verification session were deleted with HTTP 202.
The temporary profile was deleted with HTTP 204. Authentication used the current GitHub CLI token
as a bearer token; the token value was neither printed nor stored in the repository.

## Local verification

- `go test ./...` passed.
- `go vet` passed for the changed application, controller, and session use-case packages.
- Regression tests cover profile-to-start-request and start-request-to-pooled-run-request wrapper
  propagation.
- Frontend type checking, the focused `SessionProfileEditor` suite (18 tests), and ESLint passed
  for the ownership-change commit; ESLint reported only pre-existing warnings.
