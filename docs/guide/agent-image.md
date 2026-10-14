# Agent image lifecycle

Kubernetes session Pods managed by the in-process backend can run the independent
`ghcr.io/ccplant/ccplant-agent:assets-<hash>` image. Pods managed by a standalone
Session Manager use that manager's image repository and linked build version.
This guarantees that the manager, newly-created Pods, and pre-warmed stock use
the same release.

An `install-ccplant-cli` initContainer uses the manager's release image to copy
`/usr/local/bin/ccplant` into an `emptyDir`. The agent mounts it read-only at
`/opt/ccplant/bin`, which is on PATH, and uses
`CCPLANT_BINARY_PATH=/opt/ccplant/bin/ccplant` for the provisioner, hooks and child
processes. Copying runs as UID/GID 999 with the Pod's fsGroup; it needs no network
or root privileges. The CLI remains available across container restarts; a new
Pod gets a fresh copy. Ephemeral, PVC and stock sessions use the same Pod builder.

The API and Kubernetes session-manager use the lightweight `ccplant-api` image.
The full `ccplant-backend` image remains available for Docker Compose and local
process sessions; it extends the same agent image and adds the ccplant binary.

## Publishing asset updates

`backend/Dockerfile.agent` builds independently of application source. Run
`scripts/agent-image-tag.sh` to calculate its content-based tag. After changing
any listed input, update the agent image references in `backend/Dockerfile` and
`backend/helm/agentapi-proxy/values.yaml`. CI rejects stale references used by
the in-process backend.

The reusable `Agent assets image` workflow publishes both amd64 and arm64 before
application image builds. It skips publication when that asset tag already
exists, so an ordinary application release never rebuilds or retags the agent
image. The workflow can also be dispatched independently. No `latest` asset tag
is used. To refresh upstream installers or packages without another asset
change, bump the corresponding version argument or `ASSET_REVISION` in
Dockerfile.agent and update the calculated tag.

## Configuration

For the in-process backend, set `kubernetesSession.image` to the independent
agent image. A standalone Session Manager does not expose a session image
setting: both the workload and CLI init image are derived from its configured
manager image repository and the version linked into its binary at build time.
`imagePullPolicy` remains configurable and defaults to `IfNotPresent`.

The in-process backend continues to accept `AGENTAPI_K8S_SESSION_IMAGE` and
`AGENTAPI_K8S_SESSION_CLI_IMAGE`. A standalone Session Manager intentionally
ignores these variables for workload selection. Private registries must be
accessible using the session Pod's imagePullSecrets/service account credentials.

Manager auto-upgrades change the manager, workload, and CLI source image to the
same release. Already-running Pods keep their existing image; new and replacement
stock Pods use the upgraded manager release.
