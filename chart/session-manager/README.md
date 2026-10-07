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

Auto-upgrade does not apply Helm chart structure or configuration migrations.
An old chart that auto-upgrades its application image therefore keeps using the
legacy shared Lease; the updated binary retains that default when the dedicated
Lease environment variable is absent. A later `ccplant session-manager install`
detects the missing variable and performs the one-time safe Lease migration
described below. Managers already installed with the new chart keep their
release-specific Lease across auto-upgrades.

Session checkpoint persistence is configured with `sessionPersistence`. Set
`backend` to `s3` and provide the bucket and credential Secret references, or
set it to `volume` to keep each checkpoint on that session's workdir PVC.
Volume persistence automatically enables `session.pvc`; no shared manager PVC
is created. `suspendAfter` controls how long an idle session waits before it is
checkpointed and suspended.

The default manager image is `ccplant-api`. Session Pods use `ccplant-agent`
with an independent content-based tag and `IfNotPresent`. An initContainer
copies ccplant into an `emptyDir` mounted read-only at `/opt/ccplant/bin`.
The CLI source automatically uses the session-manager image repository and the
version embedded when that image was built. `session.cliImage` optionally
overrides it.
See [Agent image lifecycle](../../docs/guide/agent-image.md).

Session Pod isolation is configurable per manager release under
`session.isolation`. `disableServiceLinks` suppresses Service-derived environment
variables, while `disableServiceAccountToken` prevents the projected Kubernetes
credentials from being mounted. On Cilium clusters, `blockKubernetesAPI` also
creates a `CiliumNetworkPolicy` that preserves normal session Pod egress while
denying access to the `kube-apiserver` entity. Setting `egressMode` to
`public-only` instead permits kube-dns on TCP/UDP port 53 and Cilium's `world`
entity, while default-denying all other cluster-internal egress (including the
session-manager service and kube-apiserver). Isolation defaults remain disabled
for compatibility.

Resource names and the leader-election Lease are derived from the Helm release
name by default. This allows multiple session-manager releases to run in one
namespace without sharing Kubernetes objects or electing a leader across managers.
Use a distinct release name for each manager. `fullnameOverride` remains available
when a fixed resource name is required and must also be unique within the namespace.
Session and stock inventory discovery is also scoped by `runner.managerId`, so a
manager cannot adopt, count, reconcile, or purge another manager's workloads in
the shared namespace.

Upgrades are backward compatible with both supported installation paths. The
`ccplant session-manager install` command has always supplied
`fullnameOverride=<release>`, so those resources are already release-qualified
and retain their names. A direct Helm installation that used the old chart's
fixed `session-manager` name is migrated to release-qualified resources on
upgrade; Helm creates the new resources and removes the old release objects.
Older chart versions and binaries remain operable before they are upgraded: the
application retains the legacy allocator Lease default when the new environment
variable is absent, and manager-less local configurations retain namespace-wide
session discovery.

For installer-managed upgrades, the installer detects whether the existing
Deployment still lacks the dedicated Lease setting. That one upgrade uses the
Deployment `Recreate` strategy so no old Pod using the shared Lease overlaps a
new Pod using the release-specific Lease. Helm `--atomic` rollback is enabled for
the migration; later upgrades detect the new setting and return to the normal
rolling strategy.
