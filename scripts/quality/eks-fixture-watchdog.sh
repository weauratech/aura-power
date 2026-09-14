#!/usr/bin/env bash
set -Eeuo pipefail

: "${KUBECONFIG:?missing watchdog kubeconfig}"
: "${EXPECTED_CLUSTER:?missing EKS cluster name}"
: "${AWS_REGION:?missing AWS region}"
: "${RUN_ID:?missing run id}"
: "${FIXTURE_NAMESPACE:?missing fixture namespace}"
: "${NAMESPACE_UID:?missing namespace UID}"
: "${WORKLOAD_NAME:?missing workload name}"
: "${POLICY_NAME:?missing policy name}"
: "${AURA_POWER_RUNTIME_KUBECONFIG:?missing private runtime kubeconfig}"
: "${RECOVERY_STATE:?missing private recovery state}"

[[ "$KUBECONFIG" == "$AURA_POWER_RUNTIME_KUBECONFIG" ]] || {
  echo "refusing cleanup: runtime kubeconfig identity mismatch" >&2
  exit 2
}
[[ -f "$KUBECONFIG" && "$(stat -f '%Lp' "$KUBECONFIG" 2>/dev/null || stat -c '%a' "$KUBECONFIG")" == "600" ]] || {
  echo "refusing cleanup: runtime kubeconfig is missing or not mode 0600" >&2
  exit 2
}
[[ -f "$RECOVERY_STATE" && "$(stat -f '%Lp' "$RECOVERY_STATE" 2>/dev/null || stat -c '%a' "$RECOVERY_STATE")" == "600" ]] || {
  echo "refusing cleanup: recovery state is missing or not mode 0600" >&2
  exit 2
}

kube() {
  kubectl "$@"
}

remove_runtime_kubeconfig() {
  if [[ -f "$AURA_POWER_RUNTIME_KUBECONFIG" ]]; then
    : >"$AURA_POWER_RUNTIME_KUBECONFIG"
    unlink "$AURA_POWER_RUNTIME_KUBECONFIG"
  fi
}

remove_recovery_state() {
  if [[ -f "$RECOVERY_STATE" ]]; then
    : >"$RECOVERY_STATE"
    unlink "$RECOVERY_STATE"
  fi
}

load_recovery_state() {
  local state_json state_namespace_uid state_workload_uid
  state_json="$(cat "$RECOVERY_STATE")"
  jq -e '
    type == "object" and
    (.runID | type == "string" and length > 0) and
    (.fixtureNamespace | type == "string" and length > 0) and
    (.namespaceUID | type == "string" and length > 0) and
    (.workloadName | type == "string" and length > 0) and
    (.workloadUID | type == "string") and
    (.policyName | type == "string" and length > 0)
  ' <<<"$state_json" >/dev/null || {
    echo "refusing cleanup: recovery state schema is invalid" >&2
    return 1
  }
  [[ "$(jq -r '.runID' <<<"$state_json")" == "$RUN_ID" &&
     "$(jq -r '.fixtureNamespace' <<<"$state_json")" == "$FIXTURE_NAMESPACE" &&
     "$(jq -r '.namespaceUID' <<<"$state_json")" == "$NAMESPACE_UID" &&
     "$(jq -r '.workloadName' <<<"$state_json")" == "$WORKLOAD_NAME" &&
     "$(jq -r '.policyName' <<<"$state_json")" == "$POLICY_NAME" ]] || {
    echo "refusing cleanup: recovery state identity mismatch" >&2
    return 1
  }
  state_namespace_uid="$(jq -r '.namespaceUID' <<<"$state_json")"
  state_workload_uid="$(jq -r '.workloadUID' <<<"$state_json")"
  if [[ -n "${WORKLOAD_UID:-}" && "$state_workload_uid" != "$WORKLOAD_UID" ]]; then
    echo "refusing cleanup: recovery workload UID mismatch" >&2
    return 1
  fi
  NAMESPACE_UID="$state_namespace_uid"
  WORKLOAD_UID="$state_workload_uid"
}

owned_uid() {
  local resource="$1" name="$2" namespace="$3" expected_uid="$4"
  local actual_run actual_uid
  actual_run="$(kube get "$resource" "$name" -n "$namespace" -o json 2>/dev/null | jq -r '.metadata.labels["aura-power-quality/run"] // empty' || true)"
  actual_uid="$(kube get "$resource" "$name" -n "$namespace" -o jsonpath='{.metadata.uid}' 2>/dev/null || true)"
  [[ "$actual_run" == "$RUN_ID" && "$actual_uid" == "$expected_uid" ]]
}

cleanup_fixture() {
  local failed=0 policy_uid
  load_recovery_state || return 1
  policy_uid="$(kube get powerpolicy "$POLICY_NAME" -n aura-system -o jsonpath='{.metadata.uid}' 2>/dev/null || true)"
  if [[ -n "$policy_uid" ]]; then
    if [[ "$(kube get powerpolicy "$POLICY_NAME" -n aura-system -o json | jq -r '.metadata.labels["aura-power-quality/run"] // empty')" != "$RUN_ID" ]]; then
      echo "refusing cleanup: policy ownership mismatch" >&2
      failed=1
    else
      kube delete powerpolicy "$POLICY_NAME" -n aura-system --wait=true --timeout=60s || failed=1
    fi
  fi

  if kube get namespace "$FIXTURE_NAMESPACE" >/dev/null 2>&1; then
    if ! owned_uid namespace "$FIXTURE_NAMESPACE" "$FIXTURE_NAMESPACE" "$NAMESPACE_UID"; then
      echo "refusing cleanup: namespace ownership or UID mismatch" >&2
      failed=1
    else
      if kube get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" >/dev/null 2>&1; then
        if [[ -n "$WORKLOAD_UID" ]] && owned_uid deployment "$WORKLOAD_NAME" "$FIXTURE_NAMESPACE" "$WORKLOAD_UID"; then
          kube scale deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" --replicas=2 || failed=1
          [[ "$(kube get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')" == "2" ]] || failed=1
        elif [[ -z "$WORKLOAD_UID" && "$(kube get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o json | jq -r '.metadata.labels["aura-power-quality/run"] // empty')" == "$RUN_ID" ]]; then
          # The parent may have stopped between workload creation and the
          # atomic state update. Namespace UID and both ownership labels still
          # bind this object to the campaign, so restore it before deletion.
          kube scale deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" --replicas=2 || failed=1
          [[ "$(kube get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')" == "2" ]] || failed=1
        else
          echo "refusing restore: workload ownership or UID mismatch" >&2
          failed=1
        fi
      fi
      if [[ "$failed" -eq 0 ]]; then
        kube delete namespace "$FIXTURE_NAMESPACE" --wait=true --timeout=180s || failed=1
      fi
    fi
  fi

  if kube get powerpolicy "$POLICY_NAME" -n aura-system >/dev/null 2>&1 || kube get namespace "$FIXTURE_NAMESPACE" >/dev/null 2>&1; then
    echo "cleanup verification failed: campaign resources remain" >&2
    failed=1
  fi
  return "$failed"
}

case "${1:-}" in
  cleanup)
    cleanup_fixture
    ;;
  watch)
    : "${PARENT_PID:?missing parent PID}"
    : "${HARD_DEADLINE_EPOCH:?missing cleanup deadline}"
    while kill -0 "$PARENT_PID" 2>/dev/null && (( $(date +%s) < HARD_DEADLINE_EPOCH )); do
      sleep 5
    done
    cleanup_status=0
    cleanup_fixture || cleanup_status=$?
    if [[ "$cleanup_status" -eq 0 ]]; then
      remove_recovery_state
      remove_runtime_kubeconfig
    else
      echo "recovery kubeconfig retained at $AURA_POWER_RUNTIME_KUBECONFIG" >&2
    fi
    exit "$cleanup_status"
    ;;
  *)
    echo "usage: eks-fixture-watchdog.sh {watch|cleanup}" >&2
    exit 2
    ;;
esac
