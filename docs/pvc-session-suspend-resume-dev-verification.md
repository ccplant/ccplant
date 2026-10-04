# PVC session suspend/resume development verification

Verified on 2026-10-04 UTC against the Kubernetes session manager in the
`ccplant-session-dev` namespace.

## Environment

- Helm release: `ccplant-session-canary`, revision 2
- Chart: `session-manager-0.1.0-dev.ccplant.e09652f`
- Manager image after auto-upgrade:
  `ghcr.io/ccplant/ccplant-api:dev.ccplant.9679506b0790e5124f3481482adaf689e383f954`
- Persistence backend: `volume`
- Suspend timeout: `5m`
- Storage class selected by the cluster: `nfs-csi`

The canary manager was changed with a Helm upgrade using the existing values
plus the following settings:

```yaml
sessionPersistence:
  backend: volume
  suspendAfter: 5m
```

Volume persistence enables a per-session workdir PVC even though the explicit
`session.pvc.enabled` value remains false.

## Procedure and result

1. Created a dedicated non-ACP session through the canary manager's signed
   private API.
2. Waited for its Deployment to become available and its 10 Gi PVC to become
   Bound.
3. Wrote a unique marker to
   `/home/agentapi/workdir/pvc-suspend-resume-marker`.
4. Called the signed `POST /api/v1/sessions/{id}/suspend` endpoint.
5. Confirmed HTTP 204, removal of the Deployment and Pod, retention of the
   Service and Bound PVC, and the `agentapi.proxy/suspended-at` annotation.
6. Called the signed `POST /api/v1/sessions/{id}/resume` endpoint.
7. Confirmed HTTP 202 with status `resuming`, then waited for the recreated
   Deployment to become available.
8. Confirmed that the PVC UID and PV name were unchanged and that the marker
   content was intact after resume.
9. Deleted the dedicated verification session and confirmed that its
   Deployment, Pod, Service, PVC, and session-labelled Secrets were removed.

The tested session ID was `1af0af9e-7974-480f-a7e2-af852e17135d`. Its PVC UID
and PV name were both based on
`56f93363-1eba-44c1-ac2e-77ac1e0526fa`. The marker before and after resume was
`pvc-suspend-resume-ok-20261004T030258Z`.

## Checkpoint boundary observed

An initial suspend request with no explicit agent type was resolved to the
default ACP agent type. It correctly refused to remove the workload with HTTP
503 because that directly-created test session had no connected session-control
channel for an ACP checkpoint. Repeating the test with an explicit non-ACP
agent type isolated the PVC workload lifecycle and completed successfully.

This verifies PVC retention and workload reconstruction. It does not by itself
verify ACP conversation checkpointing; that path requires a parent-allocated,
connected ACP session.
