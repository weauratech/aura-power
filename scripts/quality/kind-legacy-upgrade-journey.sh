#!/usr/bin/env bash
set -Eeuo pipefail

: "${KUBECONFIG:?set KUBECONFIG to the disposable Kind kubeconfig}"
[[ "$(kubectl config current-context)" == "kind-aura-power-quality" ]] || { echo "refusing mutation: this runner is restricted to kind-aura-power-quality" >&2; exit 2; }
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

RUN_ID="${RUN_ID:-u$(date -u +%H%M%S)}"
FIXTURE_NAMESPACE="ap-upgrade-${RUN_ID}"
WORKLOAD_NAME="legacy-restore"
LEGACY_TARGET="${FIXTURE_NAMESPACE}--${WORKLOAD_NAME}"
EXEMPT_WORKLOAD_NAME="legacy-exempt"
EXEMPT_LEGACY_TARGET="${FIXTURE_NAMESPACE}--${EXEMPT_WORKLOAD_NAME}"
AMBIGUOUS_WORKLOAD_NAME="legacy-ambiguous"
AMBIGUOUS_LEGACY_TARGET="${FIXTURE_NAMESPACE}--${AMBIGUOUS_WORKLOAD_NAME}"
POLICY_NAME="upgrade-${RUN_ID}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-180}"
[[ "$TIMEOUT_SECONDS" =~ ^[0-9]+$ && "$TIMEOUT_SECONDS" -le 300 ]] || { echo "TIMEOUT_SECONDS must be an integer <= 300" >&2; exit 2; }
ORIGINAL_CONTROLLER_REPLICAS="$(kubectl get deployment aura-power-controller -n aura-system -o jsonpath='{.spec.replicas}')"

namespace_uid=""
cleanup() {
  local original_status=$? cleanup_status=0 actual_uid actual_run
  trap - EXIT INT TERM HUP
  kubectl apply --server-side --force-conflicts -f charts/aura-power/crds/powertargets.yaml -f charts/aura-power/crds/powerschedules.yaml >/dev/null 2>&1 || cleanup_status=1
  kubectl scale deployment aura-power-controller -n aura-system --replicas="$ORIGINAL_CONTROLLER_REPLICAS" >/dev/null 2>&1 || cleanup_status=1
  kubectl delete powerpolicy "$POLICY_NAME" -n aura-system --ignore-not-found --wait=true --timeout=60s >/dev/null 2>&1 || cleanup_status=1
  kubectl delete powertarget "$LEGACY_TARGET" -n aura-system --ignore-not-found --wait=true --timeout=60s >/dev/null 2>&1 || cleanup_status=1
  kubectl delete powertarget "$EXEMPT_LEGACY_TARGET" -n aura-system --ignore-not-found --wait=true --timeout=60s >/dev/null 2>&1 || cleanup_status=1
  kubectl delete powertarget "$AMBIGUOUS_LEGACY_TARGET" -n aura-system --ignore-not-found --wait=true --timeout=60s >/dev/null 2>&1 || cleanup_status=1
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
# Reproduce the supported v2.1.7 -> candidate sequence. Helm deliberately does
# not upgrade CRDs, so the documented explicit CRD apply is part of the gate.
for crd in powertargets powerschedules; do
  git show "v2.1.7:charts/aura-power/crds/${crd}.yaml" | kubectl apply --server-side --force-conflicts -f - >/dev/null
done
if [[ -n "$(kubectl get crd powertargets.power.aura.sh -o jsonpath='{.spec.versions[?(@.name=="v1alpha1")].schema.openAPIV3Schema.properties.spec.properties.targetRef.properties.uid.type}')" ]]; then
  echo "FAIL: v2.1.7 PowerTarget CRD unexpectedly contains UID" >&2
  exit 6
fi

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
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${EXEMPT_WORKLOAD_NAME}
  namespace: ${FIXTURE_NAMESPACE}
  annotations:
    aura.sh/power-eligible: "true"
    aura.sh/power-exempt: "true"
spec:
  replicas: 0
  selector:
    matchLabels: {app: ${EXEMPT_WORKLOAD_NAME}}
  template:
    metadata:
      labels: {app: ${EXEMPT_WORKLOAD_NAME}}
    spec:
      containers:
        - name: pause
          image: registry.k8s.io/pause:3.10
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${AMBIGUOUS_WORKLOAD_NAME}
  namespace: ${FIXTURE_NAMESPACE}
  annotations:
    aura.sh/power-eligible: "true"
spec:
  replicas: 0
  selector:
    matchLabels: {app: ${AMBIGUOUS_WORKLOAD_NAME}}
  template:
    metadata:
      labels: {app: ${AMBIGUOUS_WORKLOAD_NAME}}
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
    kind: Deployment
    namespace: ${FIXTURE_NAMESPACE}
    name: ${WORKLOAD_NAME}
---
apiVersion: power.aura.sh/v1alpha1
kind: PowerTarget
metadata:
  name: ${EXEMPT_LEGACY_TARGET}
  namespace: aura-system
spec:
  targetRef:
    kind: Deployment
    namespace: ${FIXTURE_NAMESPACE}
    name: ${EXEMPT_WORKLOAD_NAME}
---
apiVersion: power.aura.sh/v1alpha1
kind: PowerTarget
metadata:
  name: ${AMBIGUOUS_LEGACY_TARGET}
  namespace: aura-system
spec:
  targetRef:
    kind: Deployment
    namespace: ${FIXTURE_NAMESPACE}
    name: ${AMBIGUOUS_WORKLOAD_NAME}
YAML
kubectl patch powertarget "$LEGACY_TARGET" -n aura-system --subresource=status --type=merge \
  -p "{\"status\":{\"observedState\":{\"replicas\":0,\"powerState\":\"off\"},\"snapshot\":{\"available\":true,\"replicaCount\":3,\"capturedAt\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}}}"
kubectl patch powertarget "$EXEMPT_LEGACY_TARGET" -n aura-system --subresource=status --type=merge \
  -p "{\"status\":{\"observedState\":{\"replicas\":0,\"powerState\":\"off\"},\"snapshot\":{\"available\":true,\"replicaCount\":4,\"capturedAt\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}}}"
kubectl patch powertarget "$AMBIGUOUS_LEGACY_TARGET" -n aura-system --subresource=status --type=merge \
  -p "{\"status\":{\"observedState\":{\"replicas\":0,\"powerState\":\"off\"},\"snapshot\":{\"available\":true,\"replicaCount\":0,\"capturedAt\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}}}"

kubectl apply --server-side --force-conflicts -f charts/aura-power/crds/powertargets.yaml -f charts/aura-power/crds/powerschedules.yaml >/dev/null
[[ "$(kubectl get crd powertargets.power.aura.sh -o jsonpath='{.spec.versions[?(@.name=="v1alpha1")].schema.openAPIV3Schema.properties.spec.properties.targetRef.properties.uid.type}')" == "string" ]] || { echo "FAIL: candidate UID schema was not installed" >&2; exit 7; }
schedule_enum="$(kubectl get crd powerschedules.power.aura.sh -o json | jq -r '.spec.versions[] | select(.name=="v1alpha1") | .schema.openAPIV3Schema.properties.spec.properties.desiredState.enum | join(",")')"
[[ "$schedule_enum" == "on,off" ]] || { echo "FAIL: candidate schedule enum is ${schedule_enum:-missing}" >&2; exit 8; }

kubectl scale deployment aura-power-controller -n aura-system --replicas=1
kubectl rollout status deployment aura-power-controller -n aura-system --timeout=120s

deadline=$((SECONDS + TIMEOUT_SECONDS)); exempt_replicas=""
while (( SECONDS < deadline )); do
  exempt_replicas="$(kubectl get deployment "$EXEMPT_WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')"
  [[ "$exempt_replicas" == "4" ]] && break
  sleep 5
done
[[ "$exempt_replicas" == "4" ]] || { echo "FAIL: exempt legacy snapshot was not restored" >&2; exit 9; }
deadline=$((SECONDS + TIMEOUT_SECONDS)); exempt_record_present=true
while (( SECONDS < deadline )); do
  if ! kubectl get powertarget "$EXEMPT_LEGACY_TARGET" -n aura-system >/dev/null 2>&1; then
    exempt_record_present=false
    break
  fi
  sleep 2
done
[[ "$exempt_record_present" == "false" ]] || { echo "FAIL: exempt legacy recovery record remained after restoration was observed" >&2; exit 9; }

# A zero replica snapshot is the exact corrupted state observed on v2.1.7. It
# must remain available for diagnosis and may not be presented as a successful
# migration. A verified operator scale restores service and lets discovery
# retire the ambiguous record without replaying zero.
kubectl get powertarget "$AMBIGUOUS_LEGACY_TARGET" -n aura-system >/dev/null
[[ "$(kubectl get deployment "$AMBIGUOUS_WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')" == "0" ]] || { echo "FAIL: ambiguous legacy snapshot changed the workload" >&2; exit 9; }
kubectl scale deployment "$AMBIGUOUS_WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" --replicas=2 >/dev/null
deadline=$((SECONDS + TIMEOUT_SECONDS)); ambiguous_record_present=true
while (( SECONDS < deadline )); do
  if ! kubectl get powertarget "$AMBIGUOUS_LEGACY_TARGET" -n aura-system >/dev/null 2>&1; then
    ambiguous_record_present=false
    break
  fi
  sleep 2
done
[[ "$ambiguous_record_present" == "false" ]] || { echo "FAIL: ambiguous legacy record remained after verified operator recovery" >&2; exit 9; }

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
echo "kind_legacy_upgrade_journey=passed from=v2.1.7 crds_updated=true snapshot=true uid_binding=true restore=true exempt_restore=true ambiguous_zero_fail_closed=true"
