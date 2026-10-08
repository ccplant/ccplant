# Session profile runtime customization — Fly dev verification

Date: 2026-10-08 UTC  
Implementation commit: `927f4b874169de23b8b3cf9ac43b88fb6a533ada`  
PR: https://github.com/ccplant/ccplant/pull/443

## Deployment

- The API, session runtime, and frontend images were built from the implementation commit.
- The API and worker deployment, and the Cloudflare frontend deployment, completed successfully:
  https://github.com/ccplant/ccplant-deploy/actions/runs/37770425601
- `https://ccplant-api-dev.fly.dev/health` returned HTTP 200 and version
  `dev.ccplant.927f4b874169de23b8b3cf9ac43b88fb6a533ada`.
- The branch CI rerun passed:
  https://github.com/ccplant/ccplant/actions/runs/37769482057

## API verification

An isolated local user and one-hour API token were created for the test. The token secret was
kept in mode-0600 temporary files and was not printed.

| Check | Result |
| --- | --- |
| Wrapper without the required Command template action | HTTP 400 |
| Invalid OCI asset image reference | HTTP 400 |
| Profile containing a valid wrapper and asset image | HTTP 201 |
| Returned wrapper equals the submitted template | Passed |
| Returned asset image equals the submitted image | Passed |
| Deleting both temporary profiles | HTTP 204 |

The accepted wrapper wrote a unique marker before executing the Command template action. The asset image
used the current compatible asset image so that the runtime check would test profile selection
without changing the toolchain contents.

## Runtime verification and environment finding

Three isolated sessions were started:

1. wrapper plus asset-image profile;
2. wrapper-only profile;
3. a control session with no profile.

All three start requests succeeded and received allocated runtime IDs, but all three remained in
`starting`. Their message endpoints returned HTTP 502 because the runtime endpoint on port 9000
was not listening. The control session reproduced the same behavior, so this was not specific to
the new wrapper or asset-image fields. Because the Fly dev session runtime infrastructure did not
become reachable, the marker file and live Pod image could not be inspected in this run.

### GitHub-token retry

At the user's suggestion, the runtime check was repeated with a valid GitHub token supplied as
`params.github_token`, repository `ccplant/ccplant`, and the asset image pinned to digest
`sha256:2d230b6c90a1e09c1ac54844cdb51655520bdf01be92981e4e9f10f3b89b4dc1`.
The start request succeeded and allocated runtime `78efd17a-6f0c-45bb-a532-02b1ccc5236e`, but the
session remained in `starting` for more than four minutes. Its messages endpoint again returned
HTTP 502 with `127.0.0.1:9000: connect: connection refused`. This confirms that supplying GitHub
authentication does not resolve the earlier startup failure: the failure occurs before the agent
or repository authentication path becomes reachable.

Deletion was requested for all three temporary sessions and returned HTTP 202. Both profiles were
deleted, the temporary API token was revoked, and all local and remote files containing token
secrets were removed. The test user remains as an inactive record without a usable token because
the local-user API has no delete operation.

The GitHub-token retry session was likewise queued for deletion with HTTP 202; its temporary
profile was deleted with HTTP 204, its API token was revoked, and its local and remote token files
were removed. The GitHub token value was never printed or persisted in the repository.

## Local verification

- `go test ./...` passed locally and in CI. The initial CI attempt encountered an unrelated
  temporary-directory cleanup race in
  `TestNativeSessionWithNilRepositorySettingsDerivesPathsFromVirtualHome`; the failed-job rerun
  passed without a code change.
- Frontend type checking, the focused `SessionProfileEditor` test suite (18 tests), and the
  production frontend build passed.
- Documentation build passed.
