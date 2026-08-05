# KV Store

AgentAPI Proxy treats application-owned Kubernetes Secrets and ConfigMaps as KV
documents through one storage adapter. Operational resources such as Pod-mounted
Secrets, Deployments, Services, PVCs, and leader-election Leases remain
Kubernetes resources.

```yaml
kv_store:
  backend: libsql
  database_url: http://libsql-server:8080
  auth_token: ""
```

Backend selection is exclusive: `kubernetes` stores application KV data only in
Kubernetes, while `libsql` stores it only in libSQL. Existing Kubernetes data is
not read, copied, changed, or deleted in libSQL mode. Pod-mounted Secrets and
ConfigMaps are operational resources outside this KV boundary.

The `agentapi_kv` table stores a resource kind, namespace, key, complete JSON
document, and optimistic version. Both Secret and ConfigMap repositories retain
their existing labels and selectors. Migration is deliberately not performed by
server startup and will be provided as an explicit, separately reviewed command.

For development-only validation, the Helm chart can start an ephemeral server
with `libsqlTrial.enabled=true`. Its `emptyDir` is discarded with the Pod and it
does not scan or import Kubernetes objects.
