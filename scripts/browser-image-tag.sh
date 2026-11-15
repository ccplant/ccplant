#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
dockerfile="$repo_root/browser/Dockerfile"

version=$(sed -n 's/^ARG MOLI_VERSION=//p' "$dockerfile" | head -n 1)
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$ ]]; then
  echo "invalid or missing MOLI_VERSION in browser/Dockerfile: $version" >&2
  exit 1
fi

printf 'moli-%s\n' "$version"
