# Session Manager registration and installation

Session Managers use one registry and one ownership model. A manager has a `user`,
`team`, or `system` scope. Runtime heartbeat, pool supply, and allocation behavior do
not vary by scope; scope only controls who can view and administer the manager.

## Registration

Every manager, including a system-scoped manager created by an administrator, uses
the same two-step flow:

1. An authenticated caller creates a pending manager with
   `POST /session-managers/registration-tokens`.
2. The manager exchanges the 15-minute, single-use token through
   `POST /session-managers/enroll` and receives its connection token once.

The enrollment endpoint intentionally does not require user authentication. Its
single-use registration token is the credential. The durable connection token is
then used only by the manager's internal heartbeat and allocation requests.

The legacy `/external-session-managers` and `/admin/session-managers` APIs no longer
exist. Manager administration uses `/session-managers`; logical pools, suppliers,
and bindings use `/session-pools`.

## Kubernetes one-command install

Create a registration token in the UI or API, then run:

```bash
ccplant session-manager install --type kubernetes \
  --upstream https://dev.ccplant.com \
  --namespace ccplant-session-dev \
  --release session-manager \
  --pool default \
  --registration-token-file ./registration-token
```

On the initial install, the command enrolls the manager, stores its manager ID,
connection token, and HMAC secret in `<release>-parent`, creates the internal and
provisioner Secrets, and runs `helm upgrade --install`. The connection Secrets carry
the `helm.sh/resource-policy: keep` annotation and are not chart-owned.

On upgrade, omit the registration token. If the connection Secret exists, the
command reuses it and does not enroll again. Passing a registration token while the
Secret exists is rejected to make accidental credential replacement visible.

Enrollment also enables the requested pool supplier. The installer requests a
cluster-wide default binding when that intent was included in the registration token,
so requests without an explicit pool can use it. The admin Session Pools page issues
system tokens with the `default` pool and this binding enabled.

All manager and Session Pod connections are outbound-only. The parent never connects
to a manager Service, so no ingress or parent-reachable manager URL is required.

To intentionally replace credentials, issue a token with
`POST /session-managers/{id}/registration-token`, remove or replace the local
connection Secret in a controlled maintenance window, and run the installer with the
new registration token.
