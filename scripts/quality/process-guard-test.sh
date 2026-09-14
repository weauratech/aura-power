#!/usr/bin/env bash
set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "${script_dir}/process-guard.sh"

test_dir="$(mktemp -d)"
trap 'rm -rf "$test_dir"' EXIT

run_with_process_timeout 3 sh -c 'exit 0'

started="$(date +%s)"
timeout_status=0
# shellcheck disable=SC2016
run_with_process_timeout 1 sh -c 'sleep 30 & child=$!; printf "%s\n" "$child" >"$1"; wait "$child"' sh "${test_dir}/child.pid" 2>/dev/null || timeout_status=$?
elapsed=$(( $(date +%s) - started ))
[[ "$timeout_status" -eq 124 && "$elapsed" -lt 8 ]] || {
  echo "hard timeout failed: status=${timeout_status} elapsed=${elapsed}" >&2
  exit 1
}
child_pid="$(cat "${test_dir}/child.pid")"
if process_identity "$child_pid" >/dev/null 2>&1; then
  echo "hard timeout left a credential-exec descendant running" >&2
  exit 1
fi

self_identity="$(process_identity "$$")"
[[ -n "$self_identity" ]]
if same_process_identity "$$" "identity-from-a-reused-pid"; then
  echo "process identity accepted a reused PID" >&2
  exit 1
fi
same_process_identity "$$" "$self_identity"

echo "process_guard_contract=passed external_timeout=true descendants=true pid_reuse=false"
