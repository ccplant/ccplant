#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
tag=$("$repo_root/scripts/agent-image-tag.sh")
image="ghcr.io/ccplant/ccplant-agent:$tag"
files=(
  backend/Dockerfile
  backend/pkg/config/config.go
  backend/helm/agentapi-proxy/values.yaml
  chart/session-manager/values.yaml
)

status=0
for file in "${files[@]}"; do
  if ! grep -Fq "$image" "$repo_root/$file"; then
    echo "$file must reference $image" >&2
    status=1
  fi
done
exit "$status"
