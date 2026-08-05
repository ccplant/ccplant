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
  kubernetes_projection: true
  legacy_fallback: true
```

`legacy_fallback` reads pre-existing Kubernetes objects without importing them.
New objects are written to libSQL. With `kubernetes_projection`, new writes are
also projected to Kubernetes for consumers that mount a Secret or ConfigMap.
This mode supports a non-migrating trial: existing data remains untouched.

The `agentapi_kv` table stores a resource kind, namespace, key, complete JSON
document, and optimistic version. Both Secret and ConfigMap repositories retain
their existing labels and selectors. Migration is deliberately not performed by
server startup and will be provided as an explicit, separately reviewed command.
