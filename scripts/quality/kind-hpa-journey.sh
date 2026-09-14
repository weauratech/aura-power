#!/usr/bin/env bash
set -Eeuo pipefail

: "${KUBECONFIG:?set KUBECONFIG to the disposable Kind kubeconfig}"
EXPECTED_KIND_CONTEXT="${EXPECTED_KIND_CONTEXT:-kind-aura-power-quality}"
[[ "$EXPECTED_KIND_CONTEXT" == kind-* ]] || { echo "EXPECTED_KIND_CONTEXT must identify a Kind context" >&2; exit 2; }
[[ "$(kubectl config current-context)" == "$EXPECTED_KIND_CONTEXT" ]] || {
  echo "refusing mutation: current context does not match the admitted Kind context" >&2
  exit 2
}

RUN_ID="${RUN_ID:-hpa$(date -u +%Y%m%d%H%M%S)}"
FIXTURE_NAMESPACE="aura-power-hpa-${RUN_ID}"
WORKLOAD_NAME="hpa-safe"
STATEFULSET_NAME="hpa-stateful"
POLICY_NAME="hpa-quality-${RUN_ID}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-300}"
[[ "$TIMEOUT_SECONDS" =~ ^[0-9]+$ && "$TIMEOUT_SECONDS" -le 300 ]] || {
  echo "TIMEOUT_SECONDS must be an integer <= 300" >&2
  exit 2
}

if kubectl get namespace "$FIXTURE_NAMESPACE" >/dev/null 2>&1 ||
  kubectl get powerpolicy "$POLICY_NAME" -n aura-system >/dev/null 2>&1; then
  echo "refusing mutation: campaign resources already exist" >&2
  exit 4
fi

namespace_uid=""
deployment_uid=""
statefulset_uid=""
cleanup() {
  local original_status=$? cleanup_status=0 actual_uid actual_run cleanup_deadline remaining_targets
  trap - EXIT INT TERM HUP
  kubectl patch powerpolicy "$POLICY_NAME" -n aura-system --type=merge \
    -p '{"spec":{"schedule":{"desiredState":"on","windows":[]}}}' >/dev/null 2>&1 || true
  kubectl scale deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" --replicas=2 >/dev/null 2>&1 || true
  kubectl scale statefulset "$STATEFULSET_NAME" -n "$FIXTURE_NAMESPACE" --replicas=1 >/dev/null 2>&1 || true
  kubectl delete powerpolicy "$POLICY_NAME" -n aura-system --ignore-not-found --wait=true --timeout=60s >/dev/null 2>&1 || cleanup_status=1
  if [[ -n "$namespace_uid" ]] && kubectl get namespace "$FIXTURE_NAMESPACE" >/dev/null 2>&1; then
    actual_uid="$(kubectl get namespace "$FIXTURE_NAMESPACE" -o jsonpath='{.metadata.uid}')"
    actual_run="$(kubectl get namespace "$FIXTURE_NAMESPACE" -o json | jq -r '.metadata.labels["aura-power-quality/run"] // empty')"
    if [[ "$actual_uid" == "$namespace_uid" && "$actual_run" == "$RUN_ID" ]]; then
      kubectl delete namespace "$FIXTURE_NAMESPACE" --wait=true --timeout=180s >/dev/null || cleanup_status=1
    else
      echo "refusing cleanup: fixture namespace ownership or UID mismatch" >&2
      cleanup_status=1
    fi
  fi
  if kubectl get namespace "$FIXTURE_NAMESPACE" >/dev/null 2>&1 ||
    kubectl get powerpolicy "$POLICY_NAME" -n aura-system >/dev/null 2>&1; then
    cleanup_status=1
  fi
  # PowerTargets live in the control namespace. Require discovery to remove
  # both exact UID-bound fixture records before declaring cleanup complete.
  if [[ -n "$deployment_uid" && -n "$statefulset_uid" ]]; then
    cleanup_deadline=$((SECONDS + 180))
    while (( SECONDS < cleanup_deadline )); do
      remaining_targets="$(kubectl get powertargets -n aura-system -o json 2>/dev/null | jq \
        --arg ns "$FIXTURE_NAMESPACE" --arg duid "$deployment_uid" --arg suid "$statefulset_uid" \
        '[.items[] | select(.spec.targetRef.namespace == $ns and (.spec.targetRef.uid == $duid or .spec.targetRef.uid == $suid))] | length' || echo 2)"
      [[ "$remaining_targets" == "0" ]] && break
      sleep 3
    done
    [[ "${remaining_targets:-2}" == "0" ]] || cleanup_status=1
  fi
  [[ "$cleanup_status" -eq 0 ]] || { echo "FATAL: HPA fixture cleanup was not verified" >&2; exit 90; }
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
spec:
  replicas: 2
  selector:
    matchLabels: {app: ${WORKLOAD_NAME}}
  template:
    metadata:
      labels: {app: ${WORKLOAD_NAME}}
    spec:
      containers:
        - name: app
          image: registry.k8s.io/pause:3.10
          resources:
            requests: {cpu: 10m, memory: 8Mi}
            limits: {cpu: 100m, memory: 32Mi}
---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: ${WORKLOAD_NAME}
  namespace: ${FIXTURE_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: ${WORKLOAD_NAME}
  minReplicas: 1
  maxReplicas: 3
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 50
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: ${STATEFULSET_NAME}
  namespace: ${FIXTURE_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
spec:
  serviceName: ${STATEFULSET_NAME}
  replicas: 1
  selector:
    matchLabels: {app: ${STATEFULSET_NAME}}
  template:
    metadata:
      labels: {app: ${STATEFULSET_NAME}}
    spec:
      containers:
        - name: app
          image: registry.k8s.io/pause:3.10
          resources:
            requests: {cpu: 10m, memory: 8Mi}
            limits: {cpu: 100m, memory: 32Mi}
---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: ${STATEFULSET_NAME}
  namespace: ${FIXTURE_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: ${STATEFULSET_NAME}
  minReplicas: 1
  maxReplicas: 3
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 50
---
apiVersion: power.aura.sh/v1alpha1
kind: PowerPolicy
metadata:
  name: ${POLICY_NAME}
  namespace: aura-system
  labels:
    aura-power-quality/run: "${RUN_ID}"
spec:
  scope:
    targetRefs:
      - namespace: ${FIXTURE_NAMESPACE}
        name: ${WORKLOAD_NAME}
        kind: Deployment
        apiVersion: apps/v1
      - namespace: ${FIXTURE_NAMESPACE}
        name: ${STATEFULSET_NAME}
        kind: StatefulSet
        apiVersion: apps/v1
  schedule:
    desiredState: "off"
    windows: []
  priority: 900
YAML

deployment_uid="$(kubectl get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.metadata.uid}')"
statefulset_uid="$(kubectl get statefulset "$STATEFULSET_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.metadata.uid}')"
target_json() {
  kubectl get powertargets -n aura-system -o json | jq -ce \
    --arg ns "$FIXTURE_NAMESPACE" --arg name "$WORKLOAD_NAME" --arg uid "$deployment_uid" '
      [.items[] | select(
        .spec.targetRef.namespace == $ns and .spec.targetRef.name == $name and
        .spec.targetRef.kind == "Deployment" and .spec.targetRef.apiVersion == "apps/v1" and
        .spec.targetRef.uid == $uid
      )] | if length == 1 then .[0] else error("expected exactly one UID-bound target") end'
}
stateful_target_json() {
  kubectl get powertargets -n aura-system -o json | jq -ce \
    --arg ns "$FIXTURE_NAMESPACE" --arg name "$STATEFULSET_NAME" --arg uid "$statefulset_uid" '
      [.items[] | select(
        .spec.targetRef.namespace == $ns and .spec.targetRef.name == $name and
        .spec.targetRef.kind == "StatefulSet" and .spec.targetRef.apiVersion == "apps/v1" and
        .spec.targetRef.uid == $uid
      )] | if length == 1 then .[0] else error("expected exactly one UID-bound target") end'
}

# HPA ownership is discovered from scaleTargetRef. Without opt-in the target
# must fail closed and the policy must not mutate replicas.
deadline=$((SECONDS + TIMEOUT_SECONDS))
until current_target="$(target_json 2>/dev/null)" && stateful_target="$(stateful_target_json 2>/dev/null)" && jq -e '
  any(.status.ownership[]?; .type == "HPA" and .optedIn == false) and
  .status.blocked == true and any(.status.blockReasons[]?; .type == "HPAControlled")
' <<<"$current_target" >/dev/null && jq -e '
  any(.status.ownership[]?; .type == "HPA" and .optedIn == false) and
  .status.blocked == true and any(.status.blockReasons[]?; .type == "HPAControlled")
' <<<"$stateful_target" >/dev/null; do
  (( SECONDS < deadline )) || { echo "FAIL: HPA ownership did not produce a fail-closed target" >&2; exit 10; }
  sleep 3
done
[[ "$(kubectl get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')" == "2" ]] || {
  echo "FAIL: HPA-managed workload changed without opt-in" >&2
  exit 11
}
[[ "$(kubectl get statefulset "$STATEFULSET_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')" == "1" ]] || {
  echo "FAIL: HPA-managed StatefulSet changed without opt-in" >&2
  exit 11
}

# Explicit workload opt-in admits one bounded transition. The audit and
# snapshot prove that Aura Power wrote the scale subresource even if the HPA
# controller quickly raises the target back to minReplicas.
kubectl annotate deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" aura.sh/power-eligible=true --overwrite >/dev/null
deadline=$((SECONDS + TIMEOUT_SECONDS))
until current_target="$(target_json 2>/dev/null)" && jq -e '
  any(.status.ownership[]?; .type == "HPA" and .optedIn == true) and
  .status.snapshot.available == true and .status.snapshot.replicaCount == 2 and
  .status.action.desiredState == "off" and .status.action.auditPhase == "Recorded"
' <<<"$current_target" >/dev/null; do
  (( SECONDS < deadline )) || { echo "FAIL: opted-in HPA workload did not record one bounded shutdown" >&2; exit 12; }
  sleep 3
done
powered_down_audit_count() {
  kubectl get powerauditevents -n aura-system \
    -l "power.aura.sh/target-namespace=${FIXTURE_NAMESPACE},power.aura.sh/target-name=${WORKLOAD_NAME},power.aura.sh/target-kind=Deployment,power.aura.sh/action=workload.powered_down" \
    -o json | jq '[.items[] | select(.spec.result == "success")] | length'
}
[[ "$(powered_down_audit_count)" == "1" ]] || { echo "FAIL: expected exactly one power-down audit" >&2; exit 13; }

# The standard HPA controller disables scaling while its target has zero
# replicas and minReplicas is positive. Assert that native controller state and
# a stable off interval before testing restore.
deadline=$((SECONDS + TIMEOUT_SECONDS))
until current_target="$(target_json 2>/dev/null)" && jq -e '
  .status.action.phase == "Converged" and .status.observedState.powerState == "off"
' <<<"$current_target" >/dev/null &&
  [[ "$(kubectl get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')" == "0" ]] &&
  kubectl get hpa "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o json | jq -e '
    .status.desiredReplicas == 0 and
    any(.status.conditions[]?; .type == "ScalingActive" and .status == "False" and .reason == "ScalingDisabled")
  ' >/dev/null; do
  if (( SECONDS >= deadline )); then
    echo "FAIL: standard HPA zero-replica hold was not observed within ${TIMEOUT_SECONDS}s" >&2
    target_json 2>/dev/null | jq -c '{action:.status.action,ownership:.status.ownership,observed:.status.observedState}' >&2 || true
    kubectl get hpa "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o json | jq -c '{desired:.status.desiredReplicas,current:.status.currentReplicas,conditions:.status.conditions}' >&2 || true
    exit 14
  fi
  sleep 5
done
sleep 35
[[ "$(kubectl get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')" == "0" ]] || {
  echo "FAIL: HPA changed a zero-replica target during the stability interval" >&2
  exit 15
}
[[ "$(powered_down_audit_count)" == "1" ]] || { echo "FAIL: stable off state replayed the power-down audit" >&2; exit 16; }

# Restore the captured live value. Once positive replicas exist, the HPA resumes
# evaluation; unavailable metrics may keep the restored value unchanged.
kubectl patch powerpolicy "$POLICY_NAME" -n aura-system --type=merge \
  -p '{"spec":{"schedule":{"desiredState":"on","windows":[]}}}' >/dev/null
deadline=$((SECONDS + TIMEOUT_SECONDS))
until current_target="$(target_json 2>/dev/null)" && jq -e '
  .status.desiredState == "on" and .status.observedState.powerState == "on" and
  (.status.snapshot == null or .status.snapshot.available == false)
' <<<"$current_target" >/dev/null &&
  [[ "$(kubectl get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')" == "2" ]]; do
  (( SECONDS < deadline )) || { echo "FAIL: HPA target did not restore its exact snapshot" >&2; exit 17; }
  sleep 3
done

# Exercise shared-field contention without attributing the write to HPA. A
# controlled external scale reactivates an off target while the real HPA stays
# attached. Aura must report Contended and avoid a write loop.
kubectl patch powerpolicy "$POLICY_NAME" -n aura-system --type=merge \
  -p '{"spec":{"schedule":{"desiredState":"off","windows":[]}}}' >/dev/null
deadline=$((SECONDS + TIMEOUT_SECONDS))
until current_target="$(target_json 2>/dev/null)" && jq -e '
  .status.action.phase == "Converged" and .status.observedState.powerState == "off"
' <<<"$current_target" >/dev/null &&
  [[ "$(kubectl get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')" == "0" ]]; do
  (( SECONDS < deadline )) || { echo "FAIL: second bounded shutdown did not converge" >&2; exit 18; }
  sleep 3
done
kubectl scale deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" --replicas=1 >/dev/null

deadline=$((SECONDS + TIMEOUT_SECONDS))
native_hpa_gate() {
  current_target="$(target_json 2>/dev/null)" || return 1
  jq -e '
    .status.action.phase == "Contended" and
    any(.status.ownership[]?; .type == "HPA" and .optedIn == true)
  ' <<<"$current_target" >/dev/null || return 1
  [[ "$(kubectl get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')" == "1" ]]
}
until native_hpa_gate; do
  if (( SECONDS >= deadline )); then
    native_hpa_gate && break
    echo "FAIL: external scale contention was not reported within ${TIMEOUT_SECONDS}s" >&2
    target_json 2>/dev/null | jq -c '{action:.status.action,ownership:.status.ownership,observed:.status.observedState}' >&2 || true
    exit 19
  fi
  sleep 5
done
sleep 35
[[ "$(kubectl get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')" == "1" ]] || {
  echo "FAIL: controller rewrote replicas after reporting contention" >&2
  exit 20
}
current_target="$(target_json)"
jq -e '.status.action.phase == "Contended"' <<<"$current_target" >/dev/null || {
  echo "FAIL: contention status was not stable" >&2
  exit 21
}
[[ "$(powered_down_audit_count)" == "2" ]] || { echo "FAIL: contention replayed the bounded power-down audits" >&2; exit 22; }

# Return to on before cleanup. The external live value is authoritative, so the
# stale second-cycle snapshot must be retired without overwriting replicas.
kubectl patch powerpolicy "$POLICY_NAME" -n aura-system --type=merge \
  -p '{"spec":{"schedule":{"desiredState":"on","windows":[]}}}' >/dev/null
deadline=$((SECONDS + TIMEOUT_SECONDS))
until current_target="$(target_json 2>/dev/null)" && jq -e '
  .status.desiredState == "on" and .status.observedState.powerState == "on" and
  (.status.snapshot == null or .status.snapshot.available == false)
' <<<"$current_target" >/dev/null; do
  (( SECONDS < deadline )) || { echo "FAIL: contended recovery did not retire its stale snapshot" >&2; exit 23; }
  sleep 3
done
hpa_conditions="$(kubectl get hpa "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o json | jq -c '.status.conditions // []')"

echo "kind_hpa_journey=passed exact_scale_target=true deployment_detected=true statefulset_detected=true blocked_without_opt_in=true no_unauthorized_mutation=true bounded_shutdown_audit=true native_hpa_zero_hold=true exact_snapshot_restore=true hpa_conditions_observed=true external_scale_contention=true contention_stable=true no_write_loop=true hpa_conditions=${hpa_conditions}"
