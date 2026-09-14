#!/usr/bin/env bash
set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "${script_dir}/process-guard.sh"

: "${KUBECONFIG:?missing watchdog kubeconfig}"
: "${EXPECTED_CLUSTER:?missing EKS cluster name}"
: "${AWS_REGION:?missing AWS region}"
: "${RUN_ID:?missing run id}"
: "${RECOVERY_NONCE:?missing recovery nonce}"
: "${FIXTURE_NAMESPACE:?missing fixture namespace}"
: "${WORKLOAD_NAME:?missing workload name}"
: "${POLICY_NAME:?missing policy name}"
: "${AURA_POWER_RUNTIME_KUBECONFIG:?missing private runtime kubeconfig}"
: "${RECOVERY_DIR:?missing private recovery directory}"
: "${PARENT_PID:?missing parent PID}"
: "${PARENT_IDENTITY:?missing parent process identity}"
: "${HARD_DEADLINE_EPOCH:?missing cleanup deadline}"

RECOVERY_STATE="${RECOVERY_DIR}/state.json"
CLEANUP_REQUESTED="${RECOVERY_DIR}/cleanup-requested"
CLEANUP_STARTED="${RECOVERY_DIR}/cleanup-started"
CLEANUP_COMPLETE="${RECOVERY_DIR}/cleanup-complete"
MUTATION_IN_PROGRESS="${RECOVERY_DIR}/mutation-in-progress"
SUPERVISOR_READY="${RECOVERY_DIR}/supervisor-ready"
WATCHDOG_COMMAND_TIMEOUT_SECONDS="${WATCHDOG_COMMAND_TIMEOUT_SECONDS:-30}"
[[ "$WATCHDOG_COMMAND_TIMEOUT_SECONDS" =~ ^[1-9][0-9]*$ && "$WATCHDOG_COMMAND_TIMEOUT_SECONDS" -le 60 ]] || {
  echo "refusing cleanup: watchdog command timeout must be between 1 and 60 seconds" >&2
  exit 2
}

file_mode() {
  local mode
  mode="$(stat -c '%a' "$1" 2>/dev/null)" || mode="$(stat -f '%Lp' "$1" 2>/dev/null)" || return 1
  printf '%s' "$mode"
}

[[ "$KUBECONFIG" == "$AURA_POWER_RUNTIME_KUBECONFIG" ]] || {
  echo "refusing cleanup: runtime kubeconfig identity mismatch" >&2
  exit 2
}
[[ -f "$KUBECONFIG" && "$(file_mode "$KUBECONFIG")" == "600" ]] || {
  echo "refusing cleanup: runtime kubeconfig is missing or not mode 0600" >&2
  exit 2
}
[[ -d "$RECOVERY_DIR" && "$(file_mode "$RECOVERY_DIR")" == "700" ]] || {
  echo "refusing cleanup: recovery directory is missing or not mode 0700" >&2
  exit 2
}
[[ "$RECOVERY_NONCE" =~ ^[a-f0-9]{32}$ && "$FIXTURE_NAMESPACE" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ && "$WORKLOAD_NAME" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ && "$POLICY_NAME" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || {
  echo "refusing cleanup: recovery identifiers are not safe Kubernetes names" >&2
  exit 2
}

kube() {
  run_with_process_timeout "$WATCHDOG_COMMAND_TIMEOUT_SECONDS" kubectl --request-timeout=15s "$@"
}

remove_runtime_kubeconfig() {
  [[ ! -e "$AURA_POWER_RUNTIME_KUBECONFIG" ]] || unlink "$AURA_POWER_RUNTIME_KUBECONFIG"
}

remove_recovery_state() {
  local pending_state
  [[ ! -e "$RECOVERY_STATE" ]] || unlink "$RECOVERY_STATE"
  for pending_state in "${RECOVERY_DIR}"/state.pending.*; do
    [[ ! -f "$pending_state" ]] || unlink "$pending_state"
  done
}

load_recovery_state() {
  local state_json
  [[ -f "$RECOVERY_STATE" && "$(file_mode "$RECOVERY_STATE")" == "600" ]] || {
    echo "refusing cleanup: recovery state is missing or not mode 0600" >&2
    return 1
  }
  state_json="$(cat "$RECOVERY_STATE")"
  jq -e '
    type == "object" and
    (.runID | type == "string" and length > 0) and
    (.recoveryNonce | type == "string" and length > 0) and
    (.fixtureNamespace | type == "string" and length > 0) and
    (.namespaceUID | type == "string") and
    (.workloadName | type == "string" and length > 0) and
    (.workloadUID | type == "string") and
    (.policyName | type == "string" and length > 0) and
    (.policyUID | type == "string")
  ' <<<"$state_json" >/dev/null || {
    echo "refusing cleanup: recovery state schema is invalid" >&2
    return 1
  }
  [[ "$(jq -r '.runID' <<<"$state_json")" == "$RUN_ID" &&
     "$(jq -r '.recoveryNonce' <<<"$state_json")" == "$RECOVERY_NONCE" &&
     "$(jq -r '.fixtureNamespace' <<<"$state_json")" == "$FIXTURE_NAMESPACE" &&
     "$(jq -r '.workloadName' <<<"$state_json")" == "$WORKLOAD_NAME" &&
     "$(jq -r '.policyName' <<<"$state_json")" == "$POLICY_NAME" ]] || {
    echo "refusing cleanup: recovery state identity mismatch" >&2
    return 1
  }
  NAMESPACE_UID="$(jq -r '.namespaceUID' <<<"$state_json")"
  WORKLOAD_UID="$(jq -r '.workloadUID' <<<"$state_json")"
  POLICY_UID="$(jq -r '.policyUID' <<<"$state_json")"
}

capture_owned_uid() {
  local resource="$1" name="$2" namespace="$3" expected_uid="$4"
  local object actual_run actual_nonce actual_uid
  local -a get_args
  get_args=(get "$resource" "$name")
  [[ "$namespace" == "-" ]] || get_args+=(-n "$namespace")
  # --ignore-not-found is the only absence path: transport, auth, timeout and
  # server errors remain non-zero and therefore inconclusive/fail-closed.
  object="$(kube "${get_args[@]}" --ignore-not-found -o json)" || return 4
  [[ -n "$object" ]] || return 3
  jq -e 'type == "object" and (.metadata | type == "object")' <<<"$object" >/dev/null || return 4
  actual_run="$(jq -r '.metadata.labels["aura-power-quality/run"] // empty' <<<"$object")"
  actual_nonce="$(jq -r '.metadata.labels["aura-power-quality/nonce"] // empty' <<<"$object")"
  actual_uid="$(jq -r '.metadata.uid // empty' <<<"$object")"
  [[ "$actual_run" == "$RUN_ID" && "$actual_nonce" == "$RECOVERY_NONCE" && -n "$actual_uid" ]] || return 1
  [[ -z "$expected_uid" || "$actual_uid" == "$expected_uid" ]] || return 1
  printf '%s' "$actual_uid"
}

delete_owned() {
  local resource="$1" name="$2" namespace="$3" expected_uid="$4" timeout="$5"
  local actual_uid api_path deadline get_status=0
  actual_uid="$(capture_owned_uid "$resource" "$name" "$namespace" "$expected_uid")" || return $?
  case "$resource" in
    namespace) api_path="/api/v1/namespaces/${name}" ;;
    deployment) api_path="/apis/apps/v1/namespaces/${namespace}/deployments/${name}" ;;
    powerpolicy) api_path="/apis/power.aura.sh/v1alpha1/namespaces/${namespace}/powerpolicies/${name}" ;;
    *) echo "refusing cleanup: unsupported delete resource ${resource}" >&2; return 1 ;;
  esac
  if ! jq -nc --arg uid "$actual_uid" \
      '{apiVersion:"v1",kind:"DeleteOptions",preconditions:{uid:$uid},propagationPolicy:"Background"}' |
      kube delete --raw "$api_path" -f - >/dev/null; then
    return 1
  fi
  deadline=$((SECONDS + ${timeout%s}))
  while (( SECONDS < deadline )); do
    get_status=0
    capture_owned_uid "$resource" "$name" "$namespace" "$actual_uid" >/dev/null || get_status=$?
    [[ "$get_status" -eq 3 ]] && return 0
    [[ "$get_status" -eq 0 ]] || return 1
    sleep 1
  done
  echo "cleanup deletion timed out: ${resource}/${name}" >&2
  return 1
}

cleanup_fixture_locked() {
  local failed=0 captured_uid capture_status=0 workload_capture_status=0 final_policy_status=0 final_namespace_status=0
  load_recovery_state || return 1

  captured_uid="$(capture_owned_uid powerpolicy "$POLICY_NAME" aura-system "$POLICY_UID")" || capture_status=$?
  if [[ "$capture_status" -eq 0 ]]; then
    delete_owned powerpolicy "$POLICY_NAME" aura-system "$captured_uid" 60s || failed=1
  elif [[ "$capture_status" -ne 3 ]]; then
    echo "refusing cleanup: policy lookup was inconclusive or identity mismatched" >&2
    failed=1
  fi

  capture_status=0
  captured_uid="$(capture_owned_uid namespace "$FIXTURE_NAMESPACE" - "$NAMESPACE_UID")" || capture_status=$?
  if [[ "$capture_status" -eq 0 ]]; then
    NAMESPACE_UID="$captured_uid"
    captured_uid="$(capture_owned_uid deployment "$WORKLOAD_NAME" "$FIXTURE_NAMESPACE" "$WORKLOAD_UID")" || workload_capture_status=$?
    if [[ "$workload_capture_status" -eq 0 ]]; then
      WORKLOAD_UID="$captured_uid"
      kube patch deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" --type=json \
        -p "$(jq -nc --arg uid "$WORKLOAD_UID" '[{op:"test",path:"/metadata/uid",value:$uid},{op:"replace",path:"/spec/replicas",value:2}]')" || failed=1
      [[ "$(kube get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')" == "2" ]] || failed=1
      if [[ "$failed" -eq 0 ]]; then
        delete_owned deployment "$WORKLOAD_NAME" "$FIXTURE_NAMESPACE" "$WORKLOAD_UID" 60s || failed=1
      fi
    elif [[ "$workload_capture_status" -ne 3 ]]; then
      echo "refusing restore: workload lookup was inconclusive or identity mismatched" >&2
      failed=1
    fi
    if [[ "$failed" -eq 0 ]]; then
      delete_owned namespace "$FIXTURE_NAMESPACE" - "$NAMESPACE_UID" 180s || failed=1
    fi
  elif [[ "$capture_status" -ne 3 ]]; then
    echo "refusing cleanup: namespace lookup was inconclusive or identity mismatched" >&2
    failed=1
  fi

  capture_owned_uid powerpolicy "$POLICY_NAME" aura-system "$POLICY_UID" >/dev/null || final_policy_status=$?
  capture_owned_uid namespace "$FIXTURE_NAMESPACE" - "$NAMESPACE_UID" >/dev/null || final_namespace_status=$?
  if [[ "$final_policy_status" -ne 3 || "$final_namespace_status" -ne 3 ]]; then
    echo "cleanup verification failed: resources remain or absence is inconclusive" >&2
    failed=1
  fi
  return "$failed"
}

cleanup_transaction() {
  local cleanup_status=0 wait_attempts=0 parent_identity
  [[ ! -d "$CLEANUP_COMPLETE" ]] || return 0
  if ! mkdir "$CLEANUP_STARTED" 2>/dev/null; then
    [[ -d "$CLEANUP_COMPLETE" ]] && return 0
    echo "refusing cleanup: another cleanup executor is active" >&2
    return 75
  fi
  while [[ -d "$MUTATION_IN_PROGRESS" ]]; do
    parent_identity="$(process_identity "$PARENT_PID" 2>/dev/null || true)"
    if [[ "$parent_identity" != "$PARENT_IDENTITY" ]]; then
      rmdir "$MUTATION_IN_PROGRESS" 2>/dev/null || true
      break
    fi
    if (( $(date +%s) >= HARD_DEADLINE_EPOCH )); then
      signal_process_identity "$PARENT_PID" "$PARENT_IDENTITY" TERM
    fi
    wait_attempts=$((wait_attempts + 1))
    (( wait_attempts < 40 )) || {
      signal_process_identity "$PARENT_PID" "$PARENT_IDENTITY" KILL
      sleep 1
      if ! same_process_identity "$PARENT_PID" "$PARENT_IDENTITY"; then
        rmdir "$MUTATION_IN_PROGRESS" 2>/dev/null || true
        break
      fi
      echo "recovery mutation marker did not quiesce" >&2
      rmdir "$CLEANUP_STARTED" 2>/dev/null || true
      return 1
    }
    sleep 0.25
  done
  cleanup_fixture_locked || cleanup_status=$?
  if [[ "$cleanup_status" -eq 0 ]]; then
    remove_recovery_state
    remove_runtime_kubeconfig
    mkdir "$CLEANUP_COMPLETE" 2>/dev/null || true
  else
    rmdir "$CLEANUP_STARTED" 2>/dev/null || true
    echo "recovery artifacts retained in $RECOVERY_DIR" >&2
  fi
  return "$cleanup_status"
}

case "${1:-}" in
  cleanup)
    cleanup_transaction
    ;;
  watch)
    load_recovery_state >/dev/null
    mkdir "$SUPERVISOR_READY" 2>/dev/null || {
      echo "refusing cleanup: supervisor readiness marker already exists" >&2
      exit 2
    }
    while process_is_running_identity "$PARENT_PID" "$PARENT_IDENTITY" &&
          [[ ! -d "$CLEANUP_REQUESTED" ]] &&
          (( $(date +%s) < HARD_DEADLINE_EPOCH )); do
      sleep 1
    done
    cleanup_transaction
    ;;
  *)
    echo "usage: eks-fixture-watchdog.sh {watch|cleanup}" >&2
    exit 2
    ;;
esac
