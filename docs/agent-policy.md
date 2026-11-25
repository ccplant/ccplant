# Agent ccplant policy

A session profile can set `config.policy_yaml` to restrict requests made by the
agent through the local ccplant broker. The value uses the `policy.yaml` v1
interface:

```yaml
version: v1
resources:
  sessions:
    read: true
    update: true
  notifications:
    create: true
  schedules:
    read: true
```

Resource keys are the first path component of the ccplant REST API, such as
`sessions`, `schedules`, `tasks`, `task-groups`, `memories`, `files`,
`session-profiles`, and `webhooks`. `start` and `search` are normalized to the
`sessions` resource. A `*` resource supplies permissions only when an exact
resource entry is absent.

Each resource supports four boolean actions:

- `create`: POST to a resource collection
- `read`: GET or HEAD
- `update`: PUT, PATCH, or POST below a resource collection
- `delete`: DELETE

Unspecified resources and actions are denied. Unknown YAML fields, unsupported
versions, and invalid resource names make the session profile invalid. An
invalid policy that nevertheless reaches a provisioner fails closed.

When `policy_yaml` is omitted, the legacy broker allowlist remains active for
backward compatibility. A derived session profile inherits its source
profile's policy unless it supplies its own policy.

The policy is an additional restriction. It does not bypass API authentication,
ownership checks, team membership, or the session-scoped control-token rules.
Provisioner-internal usage reporting is not an agent operation and remains
available regardless of this policy.
