#!/usr/bin/env bash
set -Eeuo pipefail

: "${KUBECONFIG:?set KUBECONFIG to the disposable Kind kubeconfig}"
[[ "$(kubectl config current-context)" == "kind-aura-power-quality" ]] || { echo "refusing mutation: this runner is restricted to kind-aura-power-quality" >&2; exit 2; }

RUN_ID="${RUN_ID:-kind$(date -u +%Y%m%d%H%M%S)}"
FIXTURE_NAMESPACE="aura-power-e2e-${RUN_ID}"
POLICY_NAME="quality-${RUN_ID}"
WORKLOAD_NAME="restore-two"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-180}"
[[ "$TIMEOUT_SECONDS" =~ ^[0-9]+$ && "$TIMEOUT_SECONDS" -le 300 ]] || { echo "TIMEOUT_SECONDS must be an integer <= 300" >&2; exit 2; }

if kubectl get namespace "$FIXTURE_NAMESPACE" >/dev/null 2>&1 || kubectl get powerpolicy "$POLICY_NAME" -n aura-system >/dev/null 2>&1; then
  echo "refusing mutation: campaign namespace or policy already exists" >&2
  exit 4
fi

namespace_uid=""
cleanup() {
  local original_status=$? cleanup_status=0 actual_uid actual_run
  trap - EXIT INT TERM HUP
  if kubectl get powerpolicy "$POLICY_NAME" -n aura-system >/dev/null 2>&1; then
    actual_run="$(kubectl get powerpolicy "$POLICY_NAME" -n aura-system -o json | jq -r '.metadata.labels["aura-power-quality/run"] // empty')"
    [[ "$actual_run" == "$RUN_ID" ]] && kubectl delete powerpolicy "$POLICY_NAME" -n aura-system --wait=true --timeout=60s || cleanup_status=1
  fi
  if [[ -n "$namespace_uid" ]] && kubectl get namespace "$FIXTURE_NAMESPACE" >/dev/null 2>&1; then
    actual_uid="$(kubectl get namespace "$FIXTURE_NAMESPACE" -o jsonpath='{.metadata.uid}')"
    actual_run="$(kubectl get namespace "$FIXTURE_NAMESPACE" -o json | jq -r '.metadata.labels["aura-power-quality/run"] // empty')"
    if [[ "$actual_uid" == "$namespace_uid" && "$actual_run" == "$RUN_ID" ]]; then
      kubectl scale deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" --replicas=2 >/dev/null 2>&1 || true
      kubectl delete namespace "$FIXTURE_NAMESPACE" --wait=true --timeout=180s || cleanup_status=1
    else
      echo "refusing cleanup: namespace ownership or UID mismatch" >&2
      cleanup_status=1
    fi
  fi
  if kubectl get powerpolicy "$POLICY_NAME" -n aura-system >/dev/null 2>&1 || kubectl get namespace "$FIXTURE_NAMESPACE" >/dev/null 2>&1; then
    cleanup_status=1
  fi
  [[ "$cleanup_status" -eq 0 ]] || { echo "FATAL: Kind fixture cleanup was not verified" >&2; exit 90; }
  exit "$original_status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

namespace_json="$(kubectl create -o json -f - <<YAML
apiVersion: v1
kind: Namespace
metadata:
  name: ${FIXTURE_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
  annotations:
    aura.sh/power-eligible: "true"
YAML
)"
namespace_uid="$(jq -er .metadata.uid <<<"$namespace_json")"

kubectl create -f - <<YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${WORKLOAD_NAME}
  namespace: ${FIXTURE_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
  annotations:
    aura.sh/power-eligible: "true"
spec:
  replicas: 2
  selector:
    matchLabels: {app: ${WORKLOAD_NAME}}
  template:
    metadata:
      labels: {app: ${WORKLOAD_NAME}}
    spec:
      containers:
        - name: pause
          image: registry.k8s.io/pause:3.10
YAML
kubectl rollout status deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" --timeout=120s

kubectl create -f - <<YAML
apiVersion: power.aura.sh/v1alpha1
kind: PowerPolicy
metadata:
  name: ${POLICY_NAME}
  namespace: aura-system
  labels:
    aura-power-quality/run: "${RUN_ID}"
spec:
  scope:
    namespaces: ["${FIXTURE_NAMESPACE}"]
  schedule:
    desiredState: "off"
    windows: []
  priority: 1000
YAML

deadline=$((SECONDS + TIMEOUT_SECONDS)); replicas=""
while (( SECONDS < deadline )); do
  replicas="$(kubectl get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')"
  [[ "$replicas" == "0" ]] && break
  sleep 5
done
[[ "$replicas" == "0" ]] || { echo "FAIL: power-down did not converge" >&2; exit 10; }
target="$(kubectl get powertarget -n aura-system -l "power.aura.sh/target-namespace=${FIXTURE_NAMESPACE},power.aura.sh/target-name=${WORKLOAD_NAME},power.aura.sh/target-kind=Deployment" -o jsonpath='{.items[0].metadata.name}')"
[[ -n "$target" ]] || { echo "FAIL: discovered PowerTarget not found" >&2; exit 10; }
snapshot="$(kubectl get powertarget "$target" -n aura-system -o jsonpath='{.status.snapshot.replicaCount}')"
[[ "$snapshot" == "2" ]] || { echo "FAIL: snapshot expected replicas=2 observed=${snapshot:-missing}" >&2; exit 12; }

kubectl patch powerpolicy "$POLICY_NAME" -n aura-system --type=merge -p '{"spec":{"schedule":{"desiredState":"on","windows":[]}}}'
deadline=$((SECONDS + TIMEOUT_SECONDS))
while (( SECONDS < deadline )); do
  replicas="$(kubectl get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')"
  [[ "$replicas" == "2" ]] && break
  sleep 5
done
[[ "$replicas" == "2" ]] || { echo "FAIL: restore expected replicas=2 observed=${replicas:-unknown}" >&2; exit 11; }
echo "kind_core_journey=passed"
