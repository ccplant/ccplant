# Storage namespace mount — Fly dev verification

Date: 2026-11-21 UTC

Implementation commit: `7dcb21fab7bf9baf8b5ed1d64df58e8aec05909f`

Application PR: https://github.com/ccplant/ccplant/pull/639

Image workflow: https://github.com/ccplant/ccplant/actions/runs/44893286826

Deployment workflow: https://github.com/ccplant/ccplant-deploy/actions/runs/44894097363

## Deployment

The manual development workflow built and published the API, session runtime, and frontend from the
same implementation commit. The downstream Fly API, Fly worker, and Cloudflare frontend deployment
jobs all completed successfully.

`https://ccplant-api-dev.fly.dev/health` returned HTTP 200 and version
`dev.ccplant.7dcb21fab7bf9baf8b5ed1d64df58e8aec05909f`. The `fly-dev` Session Manager status also reported
the same version before runtime verification.

The frontend was deployed successfully to `https://dev.ccplant.com`. Anonymous visual inspection is
blocked by the expected Cloudflare Access redirect. The frontend image build, focused component test,
TypeScript check, and lint passed, and the live Profile API accepted and returned the exact
`namespace` object emitted by the editor.

## Profile and namespace resolution

The verification created one temporary `object_snapshot` Storage and one temporary Profile with this
attachment:

```json
{
  "storage_id": "<temporary-storage>",
  "storage_path": "repositories",
  "namespace": {
    "mode": "tags",
    "tag_keys": ["repository"]
  },
  "mount_path": "/home/agentapi/ns-e2e"
}
```

The create response preserved the namespace selector. Two `POST /start?dry_run=true` requests selected
the `fly-dev` pool and `direct_session_runtime` transport and resolved different repository tags to
different paths:

| repository tag | resolved prefix |
| --- | --- |
| `ccplant/ns-e2e-alpha` | `repositories/.ccplant/namespaces/v1/3n/3nxz...` |
| `ccplant/ns-e2e-beta` | `repositories/.ccplant/namespaces/v1/zx/zxw3...` |

The tag values did not appear in the persisted paths.

## Runtime persistence and isolation

Runtime sessions used the existing repositories `ccplant/ccplant` and `ccplant/ccplant-deploy` so the
normal repository clone setup also completed. Both Codex ACP sessions reached `stable`, returned the
expected model response, and synchronized a wrapper-created `marker.txt` into the shared Storage.

The resulting manifest contained two separate namespace prefixes:

| repository | namespace prefix | persisted files |
| --- | --- | --- |
| `ccplant/ccplant` | `repositories/.ccplant/namespaces/v1/hl/hlbop...` | `marker.txt` |
| `ccplant/ccplant-deploy` | `repositories/.ccplant/namespaces/v1/kd/kdezt...` | `marker.txt` |

The sessions ran concurrently and neither namespace contained the other repository's files.

The `ccplant/ccplant` session was then deleted. Its replacement used the same repository tag and a
wrapper that created `restored.txt` only when the previous `marker.txt` already existed. The replacement
session reached `stable`; the Storage revision advanced and `restored.txt` appeared beside `marker.txt`
under the original `hlbop...` namespace. The `ccplant/ccplant-deploy` namespace remained unchanged.
This confirms that the same tag set reuses persisted content while a different tag set remains isolated.

An earlier setup attempt used a deliberately synthetic repository name. It failed during the expected
repository clone step before the agent started and was not counted as a namespace failure. Repeating the
test with real repositories passed as described above.

## Cleanup

All temporary sessions were deleted with HTTP 202. The temporary Profile and Storage were deleted with
HTTP 204, and subsequent GET requests for both resources returned HTTP 404.
