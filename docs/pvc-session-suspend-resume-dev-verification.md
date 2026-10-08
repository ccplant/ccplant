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

1. Added a temporary explicit-use binding for the canary pool and created a
   parent-allocated `codex-acp` session. This is important: direct manager API
   creation does not provide the parent runtime channel required for an ACP
   checkpoint.
2. Waited for the ACP runtime to report `stable` and its 10 Gi PVC to become
   Bound.
3. Wrote the marker `ACP_PVC_20261004T040458Z` to
   `/home/agentapi/workdir/acp-pvc-marker`.
4. Sent an ACP `session/prompt` containing the marker and confirmed the exact
   `ACK-ACP_PVC_20261004T040458Z` response in conversation history.
5. Called `POST /sessions/{publicSessionId}/suspend`. It returned HTTP 200 with
   status `suspended`; the ACP checkpoint completed before the Deployment and
   Pod were removed.
6. Confirmed that the canonical Service, Bound PVC, runner Secret, saved
   settings Secret, and `agentapi.proxy/suspended-at` annotation remained.
7. Called `POST /sessions/{publicSessionId}/resume`. It returned HTTP 202 with
   status `resuming`; the recreated Deployment subsequently became available
   and the ACP runtime returned to `stable`.
8. Confirmed that the PVC UID remained
   `4a26f312-8b53-4e1e-8b64-32e34e5c1466`, the file marker was unchanged, and
   the restored ACP history still contained one user prompt and the exact ACK
   response.
9. Deleted the verification session, temporary session profile, and temporary
   pool binding, and restored the user settings changed for the test.

The public session ID was `e21d29fc-8e3f-4fe9-976e-281c6b624de8`; its manager
runner ID was `17a078dc-af72-4294-89cf-003b62a82a5d`.

## Additional findings

- A stock runner created before volume persistence was enabled still had an
  `emptyDir` workdir. When it was selected and resumed, the reconstructed Pod
  referenced a per-session PVC that did not exist and remained Pending. The
  legacy runner was deleted before the successful test used a newly created
  PVC-backed runner.
- The user's configured external skill could not be installed because the
  session image did not contain `/home/agentapi/.bun/bin/skills`. The skill list
  was backed up, temporarily cleared for the isolated verification profile,
  and restored exactly after the test.
