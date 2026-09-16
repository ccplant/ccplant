# Session Manager volume persistence verification — 2026-09-16

## Deployment

The Fly development API and worker were updated to release `v0.3.197` using an
image-only Machines update, preserving their existing configuration.

- API: `ccplant-api-dev`
- Worker: `ccplant-worker-dev`
- Image: `ghcr.io/ccplant/ccplant-api:v0.3.197@sha256:329523dbc9073444bb67f632db48e759fd57313cd4f109fb2fe3dce14df5aae4`
- API health returned `v0.3.197`; both Machines reached `started`.

The `ccplant-session` release in the `ccplant-session-dev` namespace was then
updated to Session Manager chart and application version `v0.3.197`. Its
manager and CLI images use `v0.3.197`, and its persistence values are:

```yaml
sessionPersistence:
  backend: volume
  suspendAfter: 1h
session:
  pvc:
    enabled: true
    storageSize: 10Gi
```

## Live suspend and resume verification

An isolated Codex ACP session was created through the Fly development API and
routed to the development Session Manager. The agent wrote
`VOLUME_RESUME_OK` to
`/home/agentapi/workdir/.volume-resume-marker`, which is on the per-session
workdir PVC, and returned `PVC_MARKER_READY`.

`POST /sessions/{id}/suspend` returned HTTP 200 with status `suspended`. The
public session search also reported `suspended`. An explicit resume returned
HTTP 202 with status `resuming`; during workload recreation the runtime status
briefly returned connection-refused responses, then reached `stable`.

After resume, a second prompt asked the same conversation to read the marker.
The agent returned `VOLUME_RESUME_OK`, demonstrating that both the ACP
conversation and the workdir PVC survived workload suspension and recreation.

As a negative control, a marker written to `/tmp` disappeared after the first
suspend/resume cycle. This is expected because `/tmp` is outside the workdir
PVC and confirms the successful result depends on the configured volume.

The verification session was deleted after the test.
