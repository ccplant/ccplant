# Moli browser image — Fly development verification

Date: 2026-11-15 UTC

PR: https://github.com/ccplant/ccplant/pull/605

## Deployment

The image was built from `browser/Dockerfile` by a Fly remote builder and
deployed to an isolated temporary app. Existing ccplant development API,
worker, and session-manager resources were not changed.

- Temporary app: `ccplant-browser-pr605` (removed after verification)
- Region: `nrt`
- Machine: `84ed375bd23408`, shared CPU, 512 MB memory
- Moli: `1.1.24`
- Fly image digest:
  `sha256:250349b0aa2ce3adf78ad04029c5b265ef9a61fc96bd78b149ffdb1d2eb28a3c`
- Fly reported image size: 202 MB

The first remote build found that the download stage executed `moli --version`
without `libfontconfig.so.1`. The final runtime stage already contained that
library, but the build-time validation failed. Commit `3258cb7` adds
`libfontconfig1` to the download stage. The next remote build completed and
printed `moli 1.1.24` before producing the final image.

## CDP verification

The temporary Fly process bound Moli to IPv6 for Fly private-network access.
Production session sidecars keep the designed `127.0.0.1:9222` Pod-local bind.
A `fly proxy` tunnel connected the verifier directly to the Machine's port
9222. CDP discovery returned:

```json
{
  "Browser": "Chrome/156.0.8078.74",
  "Moli-Version": "1.1.24",
  "Protocol-Version": "1.3"
}
```

A raw WebSocket CDP client then completed all of the following operations:

1. `Target.createTarget` for a new blank page.
2. `Target.attachToTarget` with a flat session.
3. `Page.enable` and `Page.navigate` to `https://example.com`.
4. `Runtime.evaluate` to read the committed URL, document title, and body text.

The final assertions passed:

```json
{
  "url": "https://example.com/",
  "title": "Example Domain",
  "bodyVerified": true
}
```

An unauthenticated request through the public Fly hostname returned HTTP 403
from Moli's host guard. The successful CDP checks used only the private tunnel,
which confirms the intended property that the browser control endpoint is not
usable through an arbitrary public Host header.

## Cleanup

The private proxy was stopped and the temporary Fly app was deleted after the
verification. The local-only Fly configuration was not committed.

## Manager-wide opt-in verification

The manager integration was subsequently deployed from commit
`9592e26ecd969c85194979d42e654a1f746f62f0`. The manual deployment workflow
published the browser manifest as
`sha256:54d79844e0369fc8c9dfade4c28325751a13686a2713da1dd29eed10d88cd5db`
and smoke-tested it before deploying the API and session runtime images.

The Fly development API was deployed with the following runtime-profile values,
which were also confirmed in the running API process environment:

```text
AGENTAPI_K8S_SESSION_MOLI_ENABLED=true
AGENTAPI_K8S_SESSION_MOLI_IMAGE=ghcr.io/ccplant/ccplant-browser:moli-1.1.24
AGENTAPI_K8S_SESSION_MOLI_PORT=9222
AGENTAPI_K8S_SESSION_MOLI_CPU_REQUEST=100m
AGENTAPI_K8S_SESSION_MOLI_CPU_LIMIT=1
AGENTAPI_K8S_SESSION_MOLI_MEMORY_REQUEST=128Mi
AGENTAPI_K8S_SESSION_MOLI_MEMORY_LIMIT=512Mi
```

The external manager received the profile and replenished its stock pool with
runner `0d40e1a6-45ee-42eb-ac46-492388c41178`. It registered online seven
seconds after the preceding stock runner was claimed, then was successfully
claimed by verification session `7a2376c6-77ab-4666-aba0-595240d35e19`.
Stock runners are not published until the complete Pod is Ready; the generated
Moli container contributes a `/json/version` startup/readiness probe, so this
also verifies Pod-local CDP discovery with the real manager configuration.

The verification session intentionally used the development bootstrap user,
which has no Codex credential, so its agent process stopped with
`Codex authentication required` after the Pod and Moli sidecar were ready. This
does not affect the browser readiness result. Raw Target, Page, and Runtime CDP
operations were independently verified against the same immutable browser image
as described above.
