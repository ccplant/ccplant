#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tool_dir="${XDG_CACHE_HOME:-/tmp}/ccplant-formal-tools"
jar="$tool_dir/tla2tools-1.8.0.jar"
mkdir -p "$tool_dir"

if [[ ! -f "$jar" ]]; then
  curl --fail --location --silent --show-error \
    https://github.com/tlaplus/tlaplus/releases/download/v1.8.0/tla2tools.jar \
    --output "$jar"
fi

cd "$repo_root/verification/session_status_propagation"
mise exec java@temurin-21 -- java -XX:+UseParallelGC -cp "$jar" tlc2.TLC \
  -cleanup -deadlock -config SessionStatusPropagation.cfg SessionStatusPropagation.tla

cd "$repo_root/backend"
mise exec -- go test \
  ./internal/domain/entities \
  ./internal/infrastructure/repositories \
  ./internal/infrastructure/services \
  ./internal/interfaces/controllers \
  -run 'NormalizeSessionStatus|StatusSilentAt|CrossPodStatus|CrossPodCompletion|RemoteStatus' -count=1
