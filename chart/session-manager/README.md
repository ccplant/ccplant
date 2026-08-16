# session-manager

Deploys only the Kubernetes External Session Manager execution plane. It does
not deploy the parent API, workers, or Redis. Multiple replicas coordinate the
single upstream allocator/control loop with a Kubernetes Lease.

The parent API remains the source of truth for allocations, routes, quotas and
provision status. This chart keeps only Kubernetes workload resources and a
cached runtime profile in the remote cluster.

Required values are `parent.url`, `parent.publicUrl`, the parent connection/HMAC
Secret references, `internalApi.tokenSecretRef.name`, and
`session.provisioner.tokenSecretRef.name`.
