# Session runtime customization design

## Decision

Runtime customization has two different owners:

- A session profile owns `command_wrapper_template` because wrapping is a per-session behavior.
- A Session Manager owns the asset image because image selection determines the manager's stock inventory and runtime compatibility.

Session profiles do not accept or return an asset image. A start request also does not carry one across the parent/ESM boundary.

## Command wrapper

`SessionProfileConfig.command_wrapper_template` is an optional Go template. It must contain exactly one
<code v-pre>{{ .Command }}</code> action. The provisioner renders it after constructing the final agent
command and before starting the process.

```json
{
  "config": {
    "command_wrapper_template": "exec env WRAPPED=1 {{ .Command }}"
  }
}
```

The template receives `.Command`, `.AgentType`, `.SessionID`, and `.WorkDir`. `.Command` is the complete final agent command, POSIX-shell quoted as one command fragment.

Validation happens when the profile is saved and again in the provisioner. Empty means no wrapper. The rendered wrapper is executed through `/bin/sh -lc`.

Source-profile inheritance treats an empty child wrapper as inherited from the source profile. An explicit start request cannot override the profile wrapper.

## Asset image

Each Session Manager selects one asset image. Operators can set the initial image in the deployment:

```yaml
session:
  image: ghcr.io/example/ccplant-agent@sha256:...
```

The standalone chart renders this as `AGENTAPI_K8S_SESSION_IMAGE`. The Manager owner can subsequently
change the desired image without editing the deployment:

```http
PATCH /session-managers/{id}
Content-Type: application/json

{"asset_image":"ghcr.io/example/ccplant-agent@sha256:..."}
```

The Manager resource reports `asset_image`, `applied_asset_image`, and an asynchronous
`asset_image_status` (`pending`, `reconciling`, `ready`, or `failed`) and any
`asset_image_error`. The image applies to both pre-warmed
stock workloads and on-demand workloads created by that manager. The CLI init image, network filter,
and other sidecars remain independently operator-managed.

Changing the asset image is a Session Manager reconciliation operation:

1. Patch the Manager's `asset_image` desired state through the API.
2. The Manager obtains the changed authenticated runtime profile.
3. It purges idle stock workloads using the previous image; allocated sessions remain untouched.
4. It reconciles and registers fresh stock runners using the new image.
5. Its heartbeat reports the applied image and moves `asset_image_status` to `ready`.

For reproducibility, production configuration should use an image digest. Registry credentials and admission policy remain cluster/operator concerns.

## Allocation behavior

Asset image is not an allocation capability. Pools route sessions to a manager, and every runner in a manager revision uses that manager's configured image. Different images require different Session Manager releases or pools.

This keeps stock compatibility local to the manager and avoids sending infrastructure policy through user-controlled session profile data.

## Test coverage

- Profile API persistence and inheritance for `command_wrapper_template`.
- Template validation and safe command quoting.
- Provisioner rendering and process launch.
- Helm rendering of manager-owned `session.image` into `AGENTAPI_K8S_SESSION_IMAGE`.
- Manager rollout verification that replacement stock workloads use the configured image and register successfully with the parent pool.
