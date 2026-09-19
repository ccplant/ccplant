#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../backend"
# Only inputs copied into the asset image participate, never application source.
files=(Dockerfile.agent config/gitignore_global config/CLAUDE.md config/managed-settings.json config/AGENTS.md config/codex-config.toml scripts/entrypoint.sh scripts/wrapped_claude.sh)
printf 'assets-%s\n' "$(sha256sum "${files[@]}" | sha256sum | cut -c1-32)"
