# Session runtime customization design

## Decision

Runtime customization has two different owners:

- A session profile owns `command_wrapper_template` because wrapping is a per-session behavior.
- A Session Manager binary version determines its session workload image and stock inventory.

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

## Session image

Session workloads always use the running Session Manager release. At startup,
the manager reads its linked build version and combines it with the configured
manager image repository. For example, a manager built as `v0.3.304` creates
workloads from `ghcr.io/ccplant/ccplant-backend:v0.3.304`.

There is no parent runtime-profile, Manager API, environment, or Helm override
for the session image. This keeps pre-warmed stock and on-demand workloads on
the exact protocol and binary version used by their manager. Auto-upgrade
updates the manager, CLI init image, and future session image together.

## Allocation behavior

Asset image is not an allocation capability. Pools route sessions to a manager, and every runner in a manager revision uses that manager's configured image. Different images require different Session Manager releases or pools.

This keeps stock compatibility local to the manager and avoids sending infrastructure policy through user-controlled session profile data.

## Test coverage

- Profile API persistence and inheritance for `command_wrapper_template`.
- Template validation and safe command quoting.
- Provisioner rendering and process launch.
- Manager startup derives the workload image from its linked build version.
- Auto-upgrade updates manager, CLI, and future session images atomically.
- Parent runtime profiles and Manager API patches cannot override the image.
