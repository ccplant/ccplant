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

cd "$repo_root/verification/session_secret_authorization"
tlc=(mise exec java@temurin-21 -- java -XX:+UseParallelGC -cp "$jar" tlc2.TLC -cleanup -deadlock SessionSecretAuthorization.tla)
"${tlc[@]}" -config good.cfg

for broken in broken-token.cfg broken-generation.cfg broken-scope.cfg broken-secret.cfg broken-capability.cfg; do
  output="$(mktemp)"
  if "${tlc[@]}" -config "$broken" >"$output" 2>&1; then
    rm -f "$output"
    echo "$broken unexpectedly satisfied AuthorizedDisclosure" >&2
    exit 1
  fi
  if ! grep -Eq "Invariant (AuthorizedDisclosure|CapabilityIsLaunchSnapshot) is violated" "$output"; then
    cat "$output" >&2
    rm -f "$output"
    echo "$broken failed without the expected authorization counterexample" >&2
    exit 1
  fi
  rm -f "$output"
done

cd "$repo_root/backend"
mise exec -- go test ./internal/interfaces/controllers ./internal/infrastructure/repositories ./internal/infrastructure/services ./pkg/provisioner ./cmd \
  -run 'SessionSettingsSecret|SecretCapability|HandleSettingsSecret|GetSecret|NativeSession' -count=1
