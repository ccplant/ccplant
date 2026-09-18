# session-manager

Deploys only the Kubernetes External Session Manager execution plane. It does
not deploy the parent API, workers, or Redis. Multiple replicas coordinate the
single upstream runner/control loop with a Kubernetes Lease. Idle runners
atomically claim work from the parent; the manager is never pre-selected.

The parent API remains the source of truth for allocations, routes, quotas and
provision status. This chart keeps only Kubernetes workload resources and a
cached runtime profile in the remote cluster.

Required values are `parent.url`, the parent connection/HMAC
Secret references, `runner.managerId`, `runner.pool`, `internalApi.tokenSecretRef.name`, and
`session.provisioner.tokenSecretRef.name`.

By default, the authenticated heartbeat also advertises the parent proxy's
semantic version. When it is newer, the elected manager updates its own
Deployment image and the CLI source for newly created session Pods. The
`session.image` asset reference stays fixed across application upgrades. Set
`autoUpgrade=false` to pin the manager to the installed chart version. Existing
session Pods are never restarted or mutated.

Session checkpoint persistence is configured with `sessionPersistence`. Set
`backend` to `s3` and provide the bucket and credential Secret references, or
set it to `volume` to keep each checkpoint on that session's workdir PVC.
Volume persistence automatically enables `session.pvc`; no shared manager PVC
is created. `suspendAfter` controls how long an idle session waits before it is
checkpointed and suspended.

The default manager image is `ccplant-api`. Session Pods use `ccplant-agent`
with an independent content-based tag and `IfNotPresent`. An initContainer
copies ccplant into an `emptyDir` mounted read-only at `/opt/ccplant/bin`.
`session.cliImage` optionally overrides the release image used for this copy.
See [Agent image lifecycle](../../docs/guide/agent-image.md).

Session Pod isolation is configurable per manager release under
`session.isolation`. `disableServiceLinks` suppresses Service-derived environment
variables, while `disableServiceAccountToken` prevents the projected Kubernetes
credentials from being mounted. On Cilium clusters, `blockKubernetesAPI` also
creates a `CiliumNetworkPolicy` that preserves normal session Pod egress while
denying access to the `kube-apiserver` entity. All three options default to
`false` for compatibility.
