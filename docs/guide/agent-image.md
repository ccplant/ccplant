# Agent image lifecycle

Kubernetes session Pods run `ghcr.io/ccplant/ccplant-agent:assets-<hash>` with
`IfNotPresent`. This image contains Claude, Codex, Pi, Cursor, ACP adapters,
agentapi, MCP tools, language/package tools, configuration and startup scripts.
It contains no ccplant application binary.

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
any listed input, update the agent image references in `backend/Dockerfile`,
`backend/helm/agentapi-proxy/values.yaml` and `chart/session-manager/values.yaml`.
CI rejects stale references (including both backend session configuration blocks).

The reusable `Agent assets image` workflow publishes both amd64 and arm64 before
application image builds. It skips publication when that asset tag already
exists, so an ordinary application release never rebuilds or retags the agent
image. The workflow can also be dispatched independently. No `latest` asset tag
is used. To refresh upstream installers or packages without another asset
change, bump the corresponding version argument or `ASSET_REVISION` in
Dockerfile.agent and update the calculated tag.

## Configuration

For the backend chart, set `kubernetesSession.image` to the independent agent
image. `kubernetesSession.cliImage` defaults to the session-manager release image
(or the API image for an in-process manager). The standalone session-manager
chart has the equivalent `session.image` and `session.cliImage` settings. Both
images use `imagePullPolicy`, defaulting to `IfNotPresent`.

Outside Helm, set `AGENTAPI_K8S_SESSION_IMAGE` and
`AGENTAPI_K8S_SESSION_CLI_IMAGE`. An empty CLI image disables injection for legacy
images with an embedded binary. Custom asset images must provide the same tools
and include `/opt/ccplant/bin` in PATH. Private registries must be accessible
using the session Pod's imagePullSecrets/service account credentials.

Manager auto-upgrades change the CLI source and manager image while preserving
the agent asset reference. Already-running Pods keep their CLI. Nodes download
new assets only when their hash changes or their cached image has been evicted.
