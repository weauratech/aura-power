#!/usr/bin/env bash
set -Eeuo pipefail

: "${KUBECONFIG:?set KUBECONFIG to the disposable Kind kubeconfig}"
[[ "$(kubectl config current-context)" == "kind-aura-power-quality" ]] || { echo "refusing mutation: this runner is restricted to kind-aura-power-quality" >&2; exit 2; }

RUN_ID="${RUN_ID:-u$(date -u +%H%M%S)}"
FIXTURE_NAMESPACE="ap-upgrade-${RUN_ID}"
WORKLOAD_NAME="legacy-restore"
LEGACY_TARGET="${FIXTURE_NAMESPACE}--${WORKLOAD_NAME}"
POLICY_NAME="upgrade-${RUN_ID}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-180}"
[[ "$TIMEOUT_SECONDS" =~ ^[0-9]+$ && "$TIMEOUT_SECONDS" -le 300 ]] || { echo "TIMEOUT_SECONDS must be an integer <= 300" >&2; exit 2; }

namespace_uid=""
cleanup() {
  local original_status=$? cleanup_status=0 actual_uid actual_run
  trap - EXIT INT TERM HUP
  kubectl scale deployment aura-power-controller -n aura-system --replicas=1 >/dev/null 2>&1 || cleanup_status=1
  kubectl delete powerpolicy "$POLICY_NAME" -n aura-system --ignore-not-found --wait=true --timeout=60s >/dev/null 2>&1 || cleanup_status=1
  kubectl delete powertarget "$LEGACY_TARGET" -n aura-system --ignore-not-found --wait=true --timeout=60s >/dev/null 2>&1 || cleanup_status=1
  if [[ -n "$namespace_uid" ]] && kubectl get namespace "$FIXTURE_NAMESPACE" >/dev/null 2>&1; then
    actual_uid="$(kubectl get namespace "$FIXTURE_NAMESPACE" -o jsonpath='{.metadata.uid}')"
    actual_run="$(kubectl get namespace "$FIXTURE_NAMESPACE" -o json | jq -r '.metadata.labels["aura-power-quality/run"] // empty')"
    if [[ "$actual_uid" == "$namespace_uid" && "$actual_run" == "$RUN_ID" ]]; then
      kubectl scale deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" --replicas=3 >/dev/null 2>&1 || true
      kubectl delete namespace "$FIXTURE_NAMESPACE" --wait=true --timeout=180s >/dev/null || cleanup_status=1
    else
      echo "refusing cleanup: namespace ownership or UID mismatch" >&2
      cleanup_status=1
    fi
  fi
  [[ "$cleanup_status" -eq 0 ]] || { echo "FATAL: legacy upgrade fixture cleanup was not verified" >&2; exit 90; }
  exit "$original_status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

if kubectl get namespace "$FIXTURE_NAMESPACE" >/dev/null 2>&1 || kubectl get powertarget "$LEGACY_TARGET" -n aura-system >/dev/null 2>&1; then
  echo "refusing mutation: legacy upgrade fixture already exists" >&2
  exit 4
fi

# Freeze discovery so the legacy recovery record and powered-down workload form
# one deterministic pre-upgrade baseline.
kubectl scale deployment aura-power-controller -n aura-system --replicas=0
kubectl rollout status deployment aura-power-controller -n aura-system --timeout=120s

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
  annotations:
    aura.sh/power-eligible: "true"
spec:
  replicas: 0
  selector:
    matchLabels: {app: ${WORKLOAD_NAME}}
  template:
    metadata:
      labels: {app: ${WORKLOAD_NAME}}
    spec:
      containers:
        - name: pause
          image: registry.k8s.io/pause:3.10
---
apiVersion: power.aura.sh/v1alpha1
kind: PowerTarget
metadata:
  name: ${LEGACY_TARGET}
  namespace: aura-system
spec:
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    namespace: ${FIXTURE_NAMESPACE}
    name: ${WORKLOAD_NAME}
YAML
kubectl patch powertarget "$LEGACY_TARGET" -n aura-system --subresource=status --type=merge \
  -p "{\"status\":{\"observedState\":{\"replicas\":0,\"powerState\":\"off\"},\"snapshot\":{\"available\":true,\"replicaCount\":3,\"capturedAt\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}}}"

kubectl scale deployment aura-power-controller -n aura-system --replicas=1
kubectl rollout status deployment aura-power-controller -n aura-system --timeout=120s

deadline=$((SECONDS + TIMEOUT_SECONDS)); migrated=""
while (( SECONDS < deadline )); do
  migrated="$(kubectl get powertarget -n aura-system -l "power.aura.sh/target-namespace=${FIXTURE_NAMESPACE},power.aura.sh/target-name=${WORKLOAD_NAME},power.aura.sh/target-kind=Deployment" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  [[ -n "$migrated" && "$migrated" != "$LEGACY_TARGET" ]] && break
  sleep 5
done
[[ -n "$migrated" && "$migrated" != "$LEGACY_TARGET" ]] || { echo "FAIL: legacy target was not migrated" >&2; exit 10; }
[[ "$(kubectl get powertarget "$migrated" -n aura-system -o jsonpath='{.status.snapshot.replicaCount}')" == "3" ]] || { echo "FAIL: legacy snapshot was not retained" >&2; exit 11; }
workload_uid="$(kubectl get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.metadata.uid}')"
[[ "$(kubectl get powertarget "$migrated" -n aura-system -o jsonpath='{.spec.targetRef.uid}')" == "$workload_uid" ]] || { echo "FAIL: migrated target was not bound to the live UID" >&2; exit 12; }

kubectl create -f - <<YAML
apiVersion: power.aura.sh/v1alpha1
kind: PowerPolicy
metadata:
  name: ${POLICY_NAME}
  namespace: aura-system
spec:
  scope:
    targetRefs:
      - apiVersion: apps/v1
        kind: Deployment
        namespace: ${FIXTURE_NAMESPACE}
        name: ${WORKLOAD_NAME}
        uid: ${workload_uid}
  schedule:
    desiredState: "on"
    windows: []
  priority: 1000
YAML

deadline=$((SECONDS + TIMEOUT_SECONDS)); replicas=""
while (( SECONDS < deadline )); do
  replicas="$(kubectl get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')"
  [[ "$replicas" == "3" ]] && break
  sleep 5
done
[[ "$replicas" == "3" ]] || { echo "FAIL: migrated snapshot did not restore replicas=3 (observed ${replicas:-unknown})" >&2; exit 13; }
echo "kind_legacy_upgrade_journey=passed snapshot=true uid_binding=true restore=true"
