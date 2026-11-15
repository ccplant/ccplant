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
