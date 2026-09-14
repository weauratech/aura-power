#!/usr/bin/env bash
set -Eeuo pipefail

: "${KUBECONFIG:?missing watchdog kubeconfig}"
: "${EXPECTED_CLUSTER:?missing EKS cluster name}"
: "${AWS_REGION:?missing AWS region}"
: "${RUN_ID:?missing run id}"
: "${FIXTURE_NAMESPACE:?missing fixture namespace}"
: "${NAMESPACE_UID:?missing namespace UID}"
: "${WORKLOAD_NAME:?missing workload name}"
: "${WORKLOAD_UID:?missing workload UID}"
: "${POLICY_NAME:?missing policy name}"
: "${AURA_POWER_RUNTIME_KUBECONFIG:?missing private runtime kubeconfig}"

[[ "$KUBECONFIG" == "$AURA_POWER_RUNTIME_KUBECONFIG" ]] || {
  echo "refusing cleanup: runtime kubeconfig identity mismatch" >&2
  exit 2
}
[[ -f "$KUBECONFIG" && "$(stat -f '%Lp' "$KUBECONFIG" 2>/dev/null || stat -c '%a' "$KUBECONFIG")" == "600" ]] || {
  echo "refusing cleanup: runtime kubeconfig is missing or not mode 0600" >&2
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

owned_uid() {
  local resource="$1" name="$2" namespace="$3" expected_uid="$4"
  local actual_run actual_uid
  actual_run="$(kube get "$resource" "$name" -n "$namespace" -o json 2>/dev/null | jq -r '.metadata.labels["aura-power-quality/run"] // empty' || true)"
  actual_uid="$(kube get "$resource" "$name" -n "$namespace" -o jsonpath='{.metadata.uid}' 2>/dev/null || true)"
  [[ "$actual_run" == "$RUN_ID" && "$actual_uid" == "$expected_uid" ]]
}

cleanup_fixture() {
  local failed=0 policy_uid
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
        if owned_uid deployment "$WORKLOAD_NAME" "$FIXTURE_NAMESPACE" "$WORKLOAD_UID"; then
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
