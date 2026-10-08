# Session profile runtime customization — Fly dev verification

Date: 2026-10-08 UTC

Implementation commit: `92171b47c50816cfa8d563cfadc690e7dc570204`

PR: https://github.com/ccplant/ccplant/pull/443

## Deployment

- The API, session runtime, and frontend images were built by
  https://github.com/ccplant/ccplant/actions/runs/37852237414.
- The downstream dev deployment completed successfully:
  https://github.com/ccplant/ccplant-deploy/actions/runs/37852671682.
- `https://ccplant-api-dev.fly.dev/health` returned HTTP 200 and version
  `dev.ccplant.92171b47c50816cfa8d563cfadc690e7dc570204`.
- The `ccplant-session-dev/ccplant-session` deployment ran the matching API image.

## Configuration ownership

The verification used the intended ownership split:

- the session profile supplied `command_wrapper_template`;
- the Session Manager supplied `AGENTAPI_K8S_SESSION_IMAGE`;
- the profile API did not persist or return the removed `asset_image` field.

The Manager image setting was
`ghcr.io/ccplant/ccplant-agent:assets-4b37a396b0634dac6b9a5dd6614af405`.

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
