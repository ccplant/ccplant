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

Each Session Manager selects one asset image. Operators set the image in the
manager deployment:

```yaml
session:
  image: ghcr.io/example/ccplant-agent@sha256:...
```

The standalone chart renders this as `AGENTAPI_K8S_SESSION_IMAGE`. The same
manager-owned configuration supplies the CLI init image, network filter,
sidecars, resources, and other workload settings. Parent runtime settings do not
replace them.

The image applies to both pre-warmed stock workloads and on-demand workloads
created by that manager. The parent API neither stores nor changes it.

Changing the asset image is a Session Manager deployment operation:

1. Update the manager's `session.image` value or equivalent command environment.
2. Roll out the manager Deployment.
3. The manager creates new stock and on-demand workloads with that image.

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
