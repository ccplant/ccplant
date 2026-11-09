#!/usr/bin/env bash
set -euo pipefail

kube=(kubectl --context k3d-ccplant-dev --namespace ccplant-dev)
api_url=${CCPLANT_LOCAL_API_URL:-http://127.0.0.1:8080}
work_dir=$(mktemp -d)
session_id=
cleanup() {
  if [[ -n "$session_id" ]]; then
    curl -fsS --config "$work_dir/curl.conf" -X DELETE "$api_url/sessions/$session_id" >/dev/null || true
  fi
  rm -r -- "$work_dir"
}
trap cleanup EXIT
umask 077
key=$("${kube[@]}" get secret ccplant-local -o jsonpath='{.data.admin-key}' | base64 -d)
printf 'header = "X-API-Key: %s"\n' "$key" > "$work_dir/curl.conf"
unset key
curl -fsS "$api_url/health" >/dev/null
curl -fsS --max-time 300 --config "$work_dir/curl.conf" \
  -H 'Content-Type: application/json' -d '{"agent_type":"claude-acp","tags":{"test":"k3s-smoke"}}' \
  "$api_url/start" > "$work_dir/start.json"
session_id=$(jq -er '.session_id' "$work_dir/start.json")
"${kube[@]}" wait --for=condition=Ready pod -l "agentapi.proxy/session-id=$session_id" --timeout=300s
curl -fsS --config "$work_dir/curl.conf" "$api_url/search" | \
  jq -e --arg id "$session_id" '.sessions | any(.session_id == $id)' >/dev/null
curl -fsS --retry 30 --retry-all-errors --retry-delay 2 --max-time 10 \
  --config "$work_dir/curl.conf" "$api_url/$session_id/status" | jq -e '.status' >/dev/null
echo "Session created and routed successfully: $session_id"
