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

dynamic_dir="${test_dir}/dynamic-tree"
mkdir "$dynamic_dir"
sh -c '
  trap "" TERM INT HUP
  index=0
  while [ "$index" -lt 30 ]; do
    sleep 30 & printf "%s\n" "$!" >>"$1/dummy.pids"
    index=$((index + 1))
  done
  (sleep 0.05; sleep 30 & printf "%s\n" "$!" >"$1/late.pid"; wait) &
  : >"$1/ready"
  wait
' sh "$dynamic_dir" 2>/dev/null &
dynamic_root=$!
while [[ ! -f "${dynamic_dir}/ready" ]]; do sleep 0.01; done
dynamic_identity="$(process_identity "$dynamic_root")"
terminate_and_wait_process_tree "$dynamic_root" "$dynamic_identity" 0 3
wait "$dynamic_root" 2>/dev/null || true
[[ -f "${dynamic_dir}/late.pid" ]] || { echo 'dynamic descendant was not created during tree capture' >&2; exit 1; }
while IFS= read -r dynamic_pid; do
  dynamic_pid_identity="$(process_identity "$dynamic_pid" 2>/dev/null || true)"
  [[ -z "$dynamic_pid_identity" ]] || ! process_is_running_identity "$dynamic_pid" "$dynamic_pid_identity" || {
    echo "dynamic descendant survived freeze/capture: ${dynamic_pid}" >&2; exit 1;
  }
done < <(cat "${dynamic_dir}/dummy.pids" "${dynamic_dir}/late.pid")

self_identity="$(process_identity "$$")"
[[ -n "$self_identity" ]]
if same_process_identity "$$" "identity-from-a-reused-pid"; then
  echo "process identity accepted a reused PID" >&2
  exit 1
fi
same_process_identity "$$" "$self_identity"

cat >"${test_dir}/interrupt-worker.sh" <<'WORKER'
#!/usr/bin/env bash
set -Eeuo pipefail
source "$1"
state_dir="$2"
requested_signal="$3"
mkdir "${state_dir}/mutation-in-progress"
PROCESS_GUARD_ACTIVE_FILE="${state_dir}/active-command"
cleanup_worker() {
  local signal="$1" code="$2"
  trap - EXIT INT TERM HUP
  quiesce_active_process_state "$PROCESS_GUARD_ACTIVE_FILE"
  rmdir "${state_dir}/mutation-in-progress"
  printf '%s\n' "$signal" >"${state_dir}/handled-signal"
  exit "$code"
}
trap 'cleanup_worker INT 130' INT
trap 'cleanup_worker TERM 143' TERM
trap 'cleanup_worker HUP 129' HUP
(
  attempts=0
  while [[ ! -f "${state_dir}/active-command" || ! -f "${state_dir}/descendant.pid" ]]; do
    attempts=$((attempts + 1))
    (( attempts < 200 )) || exit 2
    sleep 0.05
  done
  kill -s "$requested_signal" "$$"
) &
# Mutating create/patch/delete calls run in the principal shell so these traps
# can synchronously quiesce the registry before its EXIT cleanup removes state.
run_with_process_timeout 30 sh -c 'trap "" INT TERM HUP; sleep 30 & child=$!; printf "%s\n" "$child" >"$1/descendant.pid"; wait "$child"' sh "$state_dir"
WORKER
chmod +x "${test_dir}/interrupt-worker.sh"

for signal_case in TERM INT HUP; do
  signal_dir="${test_dir}/signal-${signal_case}"
  mkdir "$signal_dir"
  worker_status=0
  signal_started="$(date +%s)"
  "${test_dir}/interrupt-worker.sh" "${script_dir}/process-guard.sh" "$signal_dir" "$signal_case" 2>/dev/null || worker_status=$?
  signal_elapsed=$(( $(date +%s) - signal_started ))
  case "$signal_case" in TERM) expected_status=143 ;; INT) expected_status=130 ;; HUP) expected_status=129 ;; esac
  descendant_pid="$(cat "${signal_dir}/descendant.pid")"
  [[ "$worker_status" -eq "$expected_status" && "$signal_elapsed" -lt 8 && "$(cat "${signal_dir}/handled-signal")" == "$signal_case" ]] || {
    echo "${signal_case}: prior trap was not promptly chained after quiescence (status=${worker_status}, elapsed=${signal_elapsed})" >&2; exit 1;
  }
  [[ ! -d "${signal_dir}/mutation-in-progress" && ! -e "${signal_dir}/active-command" ]] || {
    echo "${signal_case}: mutation marker cleared before active registry" >&2; exit 1;
  }
  if process_identity "$descendant_pid" >/dev/null 2>&1; then
    echo "${signal_case}: descendant survived guarded interruption" >&2; exit 1
  fi
done

echo "process_guard_contract=passed external_timeout=true descendants=true dynamic_descendants=true pid_reuse=false signals=TERM,INT,HUP marker_order=true"
