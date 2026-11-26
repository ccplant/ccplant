#!/usr/bin/env bash
set -euo pipefail

repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
model_dir="$repo_dir/formal/session-profile-policy"
tools_version=v1.8.0
tools_url="https://github.com/tlaplus/tlaplus/releases/download/${tools_version}/tla2tools.jar"

if ! command -v java >/dev/null 2>&1; then
  echo "Java 11 or later is required (install it with mise, or add java to PATH)." >&2
  exit 1
fi

if [[ -n "${TLA_TOOLS_JAR:-}" ]]; then
  tools_jar=$TLA_TOOLS_JAR
else
  cache_root=${XDG_CACHE_HOME:-"$HOME/.cache"}
  tools_dir="$cache_root/ccplant/tlaplus/$tools_version"
  tools_jar="$tools_dir/tla2tools.jar"
  if [[ ! -f "$tools_jar" ]]; then
    mkdir -p "$tools_dir"
    temp_jar="$tools_jar.tmp"
    curl --fail --location --silent --show-error "$tools_url" --output "$temp_jar"
    mv "$temp_jar" "$tools_jar"
  fi
fi

if [[ ! -f "$tools_jar" ]]; then
  echo "TLA+ tools jar not found: $tools_jar" >&2
  exit 1
fi

java -cp "$tools_jar" tla2sany.SANY "$model_dir/SessionProfilePolicy.tla"
java -cp "$tools_jar" tlc2.TLC \
  -cleanup \
  -deadlock \
  -workers auto \
  -config "$model_dir/Fast.cfg" \
  "$model_dir/SessionProfilePolicy.tla"
