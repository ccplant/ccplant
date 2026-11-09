#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
cluster=ccplant-dev
namespace=ccplant-dev
context=k3d-ccplant-dev
chart=backend/helm/agentapi-proxy
kube=(kubectl --context "$context" --namespace "$namespace")

case "${1:-up}" in
  up)
    for tool in docker k3d kubectl helm openssl jq; do
      command -v "$tool" >/dev/null || { echo "Required command: $tool" >&2; exit 1; }
    done
    docker info >/dev/null
    if ! k3d cluster list -o json | jq -e '.[] | select(.name == "ccplant-dev")' >/dev/null; then
      k3d cluster create "$cluster" --image rancher/k3s:v1.34.1-k3s1 \
        --k3s-arg '--disable=traefik@server:*' --wait
    fi
    version=$(helm show chart "$chart" | awk '/^appVersion:/ {gsub(/"/, "", $2); print $2}')
    docker build --target api --build-arg RELEASE_VERSION="$version" \
      -t "ccplant-local-api:$version" backend
    k3d image import "ccplant-local-api:$version" --cluster "$cluster"
    "${kube[@]}" create namespace "$namespace" --dry-run=client -o yaml | "${kube[@]}" apply -f -
    if ! "${kube[@]}" get secret ccplant-local >/dev/null 2>&1; then
      secret_dir=$(mktemp -d)
      trap 'rm -r -- "$secret_dir"' EXIT
      for key in admin-key manager-token worker-token execution-token-signing-key provisioner-token; do
        openssl rand -hex 32 | tr -d '\n' > "$secret_dir/$key"
      done
      "${kube[@]}" create secret generic ccplant-local --from-file="$secret_dir"
    fi
    helm dependency build "$chart"
    helm upgrade --install ccplant "$chart" --kube-context "$context" \
      --namespace "$namespace" -f dev/k3s/values.yaml --wait --timeout 10m
    # The image tag stays stable across local rebuilds.
    "${kube[@]}" rollout restart deployment/ccplant deployment/ccplant-session-manager deployment/ccplant-worker
    for deployment in ccplant ccplant-session-manager ccplant-worker; do
      "${kube[@]}" rollout status "deployment/$deployment" --timeout=300s
    done
    echo 'Ready. Run make k3s-forward, then make k3s-smoke.'
    ;;
  forward)
    exec "${kube[@]}" port-forward --address 127.0.0.1 service/ccplant 8080:8080
    ;;
  down)
    k3d cluster delete "$cluster"
    ;;
  *) echo 'Usage: scripts/local-k3s.sh {up|forward|down}' >&2; exit 2 ;;
esac
