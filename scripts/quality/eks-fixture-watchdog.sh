#!/usr/bin/env bash
set -Eeuo pipefail

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

RECOVERY_STATE="${RECOVERY_DIR}/state.json"
RECOVERY_LOCK="${RECOVERY_DIR}/lock"
CLEANUP_REQUESTED="${RECOVERY_DIR}/cleanup-requested"
CLEANUP_STARTED="${RECOVERY_DIR}/cleanup-started"
CLEANUP_COMPLETE="${RECOVERY_DIR}/cleanup-complete"

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
  kubectl "$@"
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

acquire_recovery_lock() {
  local attempts=0 owner=""
  while ! mkdir "$RECOVERY_LOCK" 2>/dev/null; do
    if [[ -d "$CLEANUP_COMPLETE" ]]; then
      return 2
    fi
    owner="$(cat "${RECOVERY_LOCK}/owner" 2>/dev/null || true)"
    if [[ "$owner" =~ ^[0-9]+$ ]] && ! kill -0 "$owner" 2>/dev/null; then
      if mkdir "${RECOVERY_DIR}/lock-reclaim" 2>/dev/null; then
        if [[ "$(cat "${RECOVERY_LOCK}/owner" 2>/dev/null || true)" == "$owner" ]] && ! kill -0 "$owner" 2>/dev/null; then
          unlink "${RECOVERY_LOCK}/owner" 2>/dev/null || true
          rmdir "$RECOVERY_LOCK" 2>/dev/null || true
        fi
        rmdir "${RECOVERY_DIR}/lock-reclaim" 2>/dev/null || true
      fi
    fi
    attempts=$((attempts + 1))
    (( attempts < 120 )) || {
      echo "recovery lock acquisition timed out" >&2
      return 1
    }
    sleep 0.25
  done
  printf '%s\n' "$$" >"${RECOVERY_LOCK}/owner"
}

release_recovery_lock() {
  [[ "$(cat "${RECOVERY_LOCK}/owner" 2>/dev/null || true)" == "$$" ]] || {
    echo "refusing to release a recovery lock owned by another process" >&2
    return 1
  }
  unlink "${RECOVERY_LOCK}/owner"
  rmdir "$RECOVERY_LOCK" 2>/dev/null || true
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
  object="$(kube "${get_args[@]}" -o json 2>/dev/null)" || return 3
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
  jq -nc --arg uid "$actual_uid" \
    '{apiVersion:"v1",kind:"DeleteOptions",preconditions:{uid:$uid},propagationPolicy:"Background"}' |
    kube delete --raw "$api_path" -f - >/dev/null
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
  local failed=0 captured_uid capture_status=0 workload_capture_status=0
  load_recovery_state || return 1

  captured_uid="$(capture_owned_uid powerpolicy "$POLICY_NAME" aura-system "$POLICY_UID")" || capture_status=$?
  if [[ "$capture_status" -eq 0 ]]; then
    delete_owned powerpolicy "$POLICY_NAME" aura-system "$captured_uid" 60s || failed=1
  elif [[ "$capture_status" -ne 3 ]]; then
    echo "refusing cleanup: policy ownership or UID mismatch" >&2
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
      echo "refusing restore: workload ownership or UID mismatch" >&2
      failed=1
    fi
    if [[ "$failed" -eq 0 ]]; then
      delete_owned namespace "$FIXTURE_NAMESPACE" - "$NAMESPACE_UID" 180s || failed=1
    fi
  elif [[ "$capture_status" -ne 3 ]]; then
    echo "refusing cleanup: namespace ownership or UID mismatch" >&2
    failed=1
  fi

  if kube get powerpolicy "$POLICY_NAME" -n aura-system >/dev/null 2>&1 ||
     kube get namespace "$FIXTURE_NAMESPACE" >/dev/null 2>&1; then
    echo "cleanup verification failed: campaign resources remain" >&2
    failed=1
  fi
  return "$failed"
}

cleanup_transaction() {
  local lock_status=0 cleanup_status=0
  acquire_recovery_lock || lock_status=$?
  if [[ "$lock_status" -eq 2 && -d "$CLEANUP_COMPLETE" ]]; then
    return 0
  fi
  [[ "$lock_status" -eq 0 ]] || return "$lock_status"
  if [[ -d "$CLEANUP_COMPLETE" ]]; then
    release_recovery_lock
    return 0
  fi
  mkdir "$CLEANUP_STARTED" 2>/dev/null || true
  cleanup_fixture_locked || cleanup_status=$?
  if [[ "$cleanup_status" -eq 0 ]]; then
    remove_recovery_state
    remove_runtime_kubeconfig
    mkdir "$CLEANUP_COMPLETE" 2>/dev/null || true
  else
    rmdir "$CLEANUP_STARTED" 2>/dev/null || true
    echo "recovery artifacts retained in $RECOVERY_DIR" >&2
  fi
  release_recovery_lock
  return "$cleanup_status"
}

case "${1:-}" in
  cleanup)
    cleanup_transaction
    ;;
  watch)
    : "${PARENT_PID:?missing parent PID}"
    : "${HARD_DEADLINE_EPOCH:?missing cleanup deadline}"
    while kill -0 "$PARENT_PID" 2>/dev/null &&
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
