#!/usr/bin/env bash
set -euo pipefail

HELM_BIN="${HELM:-helm}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

"$HELM_BIN" template backend "$REPO_ROOT/backend/helm/agentapi-proxy" >"$TMP_DIR/backend-default.yaml"
"$HELM_BIN" template frontend "$REPO_ROOT/frontend/helm/agentapi-ui" >"$TMP_DIR/frontend-default.yaml"
"$HELM_BIN" template ccplant "$REPO_ROOT/chart/ccplant" >"$TMP_DIR/ccplant-default.yaml"

assert_contains() {
  local pattern="$1"
  local file="$2"
  if ! grep -Eq "$pattern" "$file"; then
    echo "expected pattern not found: $pattern ($file)" >&2
    exit 1
  fi
}

assert_not_contains() {
  local pattern="$1"
  local file="$2"
  if grep -Eq "$pattern" "$file"; then
    echo "unexpected pattern found: $pattern ($file)" >&2
    exit 1
  fi
}

# A default install must include the ServiceAccount and RBAC required to launch
# direct Kubernetes sessions, while keeping the proxy at one replica.
assert_contains '^  name: agentapi-proxy-session$' "$TMP_DIR/backend-default.yaml"
assert_contains '^  replicas: 1$' "$TMP_DIR/backend-default.yaml"

# Optional controllers and persistent components are disabled in the minimal defaults.
assert_contains 'name: AGENTAPI_SCHEDULE_WORKER_ENABLED' "$TMP_DIR/backend-default.yaml"
assert_contains 'name: AGENTAPI_SLACKBOT_CLEANUP_WORKER_ENABLED' "$TMP_DIR/backend-default.yaml"
assert_contains 'name: AGENTAPI_STOCK_INVENTORY_WORKER_ENABLED' "$TMP_DIR/backend-default.yaml"
assert_not_contains '^kind: PersistentVolumeClaim$' "$TMP_DIR/ccplant-default.yaml"
assert_not_contains 'app.kubernetes.io/component: scia' "$TMP_DIR/ccplant-default.yaml"
assert_not_contains 'app.kubernetes.io/component: asset' "$TMP_DIR/ccplant-default.yaml"

# The frontend must configure the environment name consumed by the application
# and derive http URLs when TLS is disabled.
assert_contains 'name: ALLOWED_ORIGINS' "$TMP_DIR/frontend-default.yaml"
assert_contains 'value: "http://agentapi-ui.local"' "$TMP_DIR/frontend-default.yaml"
assert_not_contains 'name: NEXT_PUBLIC_ALLOWED_ORIGINS' "$TMP_DIR/frontend-default.yaml"
assert_contains 'key: cookie-encryption-secret' "$TMP_DIR/frontend-default.yaml"

# Sensitive runtime values can be supplied without being stored in Helm values.
"$HELM_BIN" template backend-secrets "$REPO_ROOT/backend/helm/agentapi-proxy" \
  --set config.auth.github.oauth.clientSecretRef.name=backend-runtime \
  --set config.auth.github.oauth.clientSecretRef.key=oauth-client-secret \
  --set github.tokenRef.name=backend-runtime \
  --set config.vapid.privateKeyRef.name=backend-runtime >"$TMP_DIR/backend-secrets.yaml"
assert_contains 'name: "backend-runtime"' "$TMP_DIR/backend-secrets.yaml"
assert_contains 'key: "oauth-client-secret"' "$TMP_DIR/backend-secrets.yaml"

"$HELM_BIN" template frontend-secrets "$REPO_ROOT/frontend/helm/agentapi-ui" \
  --set envFrom[0].secretRef.name=frontend-runtime >"$TMP_DIR/frontend-secrets.yaml"
assert_contains 'name: frontend-runtime' "$TMP_DIR/frontend-secrets.yaml"

# Stock inventory must receive leader-election RBAC even when other workers and
# Kubernetes session creation are disabled explicitly.
"$HELM_BIN" template backend "$REPO_ROOT/backend/helm/agentapi-proxy" \
  --skip-schema-validation \
  --set kubernetesSession.enabled=false \
  --set scheduleWorker.enabled=false \
  --set slackbotCleanupWorker.enabled=false \
  --set stockInventoryWorker.enabled=true >"$TMP_DIR/backend-stock.yaml"
assert_contains 'resources: \["leases"\]' "$TMP_DIR/backend-stock.yaml"
assert_contains 'name: backend-agentapi-proxy-session-manager' "$TMP_DIR/backend-stock.yaml"

# TLS-enabled frontend URLs must use https.
"$HELM_BIN" template frontend "$REPO_ROOT/frontend/helm/agentapi-ui" \
  --set ingress.enabled=true \
  --set ingress.tls[0].secretName=frontend-tls \
  --set ingress.tls[0].hosts[0]=agentapi.example.com \
  --set hostname=agentapi.example.com >"$TMP_DIR/frontend-tls.yaml"
assert_contains 'value: "https://agentapi.example.com"' "$TMP_DIR/frontend-tls.yaml"

# Multiple proxy replicas require shared Redis state.
if "$HELM_BIN" template backend "$REPO_ROOT/backend/helm/agentapi-proxy" \
  --set replicaCount=2 >"$TMP_DIR/backend-invalid-replicas.yaml" 2>/dev/null; then
  echo "replicaCount=2 without Redis unexpectedly passed schema validation" >&2
  exit 1
fi
"$HELM_BIN" template backend "$REPO_ROOT/backend/helm/agentapi-proxy" \
  --set replicaCount=2 \
  --set redis.enabled=true >"$TMP_DIR/backend-replicas.yaml"

echo "Helm render assertions passed"
