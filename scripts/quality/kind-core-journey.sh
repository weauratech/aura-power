#!/usr/bin/env bash
set -Eeuo pipefail

: "${KUBECONFIG:?set KUBECONFIG to the disposable Kind kubeconfig}"
[[ "$(kubectl config current-context)" == "kind-aura-power-quality" ]] || { echo "refusing mutation: this runner is restricted to kind-aura-power-quality" >&2; exit 2; }

RUN_ID="${RUN_ID:-kind$(date -u +%Y%m%d%H%M%S)}"
FIXTURE_NAMESPACE="aura-power-e2e-${RUN_ID}"
RBAC_FIXTURE_NAMESPACE="aura-power-rbac-${RUN_ID}"
IMPLICIT_POLICY_NAME="ns-default-${RBAC_FIXTURE_NAMESPACE}"
POLICY_NAME="quality-${RUN_ID}"
GROUP_NAME="quality-${RUN_ID}"
WORKLOAD_NAME="restore-two"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-180}"
[[ "$TIMEOUT_SECONDS" =~ ^[0-9]+$ && "$TIMEOUT_SECONDS" -le 300 ]] || { echo "TIMEOUT_SECONDS must be an integer <= 300" >&2; exit 2; }

if kubectl get namespace "$FIXTURE_NAMESPACE" >/dev/null 2>&1 ||
  kubectl get namespace "$RBAC_FIXTURE_NAMESPACE" >/dev/null 2>&1 ||
  kubectl get powerpolicy "$POLICY_NAME" -n aura-system >/dev/null 2>&1 ||
  kubectl get powerpolicy "$IMPLICIT_POLICY_NAME" -n aura-system >/dev/null 2>&1; then
  echo "refusing mutation: campaign namespace or policy already exists" >&2
  exit 4
fi

namespace_uid=""
rbac_namespace_uid=""
cleanup() {
  local original_status=$? cleanup_status=0 actual_uid actual_run
  trap - EXIT INT TERM HUP
  if kubectl get powerpolicy "$POLICY_NAME" -n aura-system >/dev/null 2>&1; then
    actual_run="$(kubectl get powerpolicy "$POLICY_NAME" -n aura-system -o json | jq -r '.metadata.labels["aura-power-quality/run"] // empty')"
    [[ "$actual_run" == "$RUN_ID" ]] && kubectl delete powerpolicy "$POLICY_NAME" -n aura-system --wait=true --timeout=60s || cleanup_status=1
  fi
  kubectl delete powernamespacegroup "$GROUP_NAME" -n aura-system --ignore-not-found --wait=true --timeout=60s >/dev/null 2>&1 || cleanup_status=1
  kubectl delete powerpolicy "$IMPLICIT_POLICY_NAME" -n aura-system --ignore-not-found --wait=true --timeout=60s >/dev/null 2>&1 || cleanup_status=1
  if [[ -n "$rbac_namespace_uid" ]] && kubectl get namespace "$RBAC_FIXTURE_NAMESPACE" >/dev/null 2>&1; then
    actual_uid="$(kubectl get namespace "$RBAC_FIXTURE_NAMESPACE" -o jsonpath='{.metadata.uid}')"
    actual_run="$(kubectl get namespace "$RBAC_FIXTURE_NAMESPACE" -o json | jq -r '.metadata.labels["aura-power-quality/run"] // empty')"
    if [[ "$actual_uid" == "$rbac_namespace_uid" && "$actual_run" == "$RUN_ID" ]]; then
      kubectl delete namespace "$RBAC_FIXTURE_NAMESPACE" --wait=true --timeout=180s >/dev/null || cleanup_status=1
    else
      echo "refusing cleanup: RBAC fixture namespace ownership or UID mismatch" >&2
      cleanup_status=1
    fi
  fi
  if [[ -n "$namespace_uid" ]] && kubectl get namespace "$FIXTURE_NAMESPACE" >/dev/null 2>&1; then
    actual_uid="$(kubectl get namespace "$FIXTURE_NAMESPACE" -o jsonpath='{.metadata.uid}')"
    actual_run="$(kubectl get namespace "$FIXTURE_NAMESPACE" -o json | jq -r '.metadata.labels["aura-power-quality/run"] // empty')"
    if [[ "$actual_uid" == "$namespace_uid" && "$actual_run" == "$RUN_ID" ]]; then
      kubectl scale deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" --replicas=2 >/dev/null 2>&1 || true
      kubectl scale statefulset "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" --replicas=1 >/dev/null 2>&1 || true
      kubectl patch cronjob "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" --type=merge -p '{"spec":{"suspend":false}}' >/dev/null 2>&1 || true
      kubectl delete namespace "$FIXTURE_NAMESPACE" --wait=true --timeout=180s || cleanup_status=1
    else
      echo "refusing cleanup: namespace ownership or UID mismatch" >&2
      cleanup_status=1
    fi
  fi
  if kubectl get powerpolicy "$POLICY_NAME" -n aura-system >/dev/null 2>&1 ||
    kubectl get powerpolicy "$IMPLICIT_POLICY_NAME" -n aura-system >/dev/null 2>&1 ||
    kubectl get namespace "$FIXTURE_NAMESPACE" >/dev/null 2>&1 ||
    kubectl get namespace "$RBAC_FIXTURE_NAMESPACE" >/dev/null 2>&1; then
    cleanup_status=1
  fi
  [[ "$cleanup_status" -eq 0 ]] || { echo "FATAL: Kind fixture cleanup was not verified" >&2; exit 90; }
  exit "$original_status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

# Exercise the rendered controller ClusterRole through the real service account.
# Built-in schedule seeding and namespace-derived policies are controller-owned
# writes and must survive least-privilege changes.
for schedule in business-hours always-off weekdays-only; do
  deadline=$((SECONDS + TIMEOUT_SECONDS))
  until kubectl get powerschedule "$schedule" -n aura-system >/dev/null 2>&1; do
    (( SECONDS < deadline )) || { echo "FAIL: built-in schedule $schedule was not seeded" >&2; exit 5; }
    sleep 3
  done
done
rbac_namespace_json="$(kubectl create -o json -f - <<YAML
apiVersion: v1
kind: Namespace
metadata:
  name: ${RBAC_FIXTURE_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
YAML
)"
rbac_namespace_uid="$(jq -er .metadata.uid <<<"$rbac_namespace_json")"

# A namespace-derived policy name can collide with a user-owned object. The
# controller must observe the collision and leave every field untouched.
kubectl create -f - <<YAML
apiVersion: power.aura.sh/v1alpha1
kind: PowerPolicy
metadata:
  name: ${IMPLICIT_POLICY_NAME}
  namespace: aura-system
  labels:
    aura-power-quality/run: "${RUN_ID}"
    owner: user
spec:
  scope:
    namespaces: ["collision-sentinel"]
  schedule:
    desiredState: "on"
    windows: []
  priority: 999
  description: "user-owned collision sentinel"
YAML
collision_before="$(kubectl get powerpolicy "$IMPLICIT_POLICY_NAME" -n aura-system -o json | jq -c '{labels:.metadata.labels,spec:.spec}')"
collision_started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
kubectl annotate namespace "$RBAC_FIXTURE_NAMESPACE" \
  aura.sh/default-schedule=always-off aura.sh/power-priority=321 --overwrite >/dev/null
deadline=$((SECONDS + TIMEOUT_SECONDS)); collision_observed=false
while (( SECONDS < deadline )); do
  collision_current="$(kubectl get powerpolicy "$IMPLICIT_POLICY_NAME" -n aura-system -o json | jq -c '{labels:.metadata.labels,spec:.spec}')"
  [[ "$collision_current" == "$collision_before" ]] || { echo "FAIL: namespace annotation reconciliation mutated a colliding user policy" >&2; exit 6; }
  if controller_logs="$(kubectl logs -n aura-system -l app.kubernetes.io/component=controller \
    --all-containers=true --prefix=true --since-time="$collision_started_at" 2>/dev/null)"; then
    if grep -Fq 'refusing to overwrite policy that is not owned by namespace annotation reconciliation' <<<"$controller_logs"; then
      collision_observed=true
      break
    fi
  fi
  sleep 3
done
[[ "$collision_observed" == true ]] || { echo "FAIL: namespace policy ownership collision was not observed" >&2; exit 6; }
collision_after="$(kubectl get powerpolicy "$IMPLICIT_POLICY_NAME" -n aura-system -o json | jq -c '{labels:.metadata.labels,spec:.spec}')"
[[ "$collision_after" == "$collision_before" ]] || { echo "FAIL: colliding user policy changed after collision observation" >&2; exit 6; }
kubectl delete powerpolicy "$IMPLICIT_POLICY_NAME" -n aura-system --wait=true --timeout=60s >/dev/null

deadline=$((SECONDS + TIMEOUT_SECONDS))
until kubectl get powerpolicy "$IMPLICIT_POLICY_NAME" -n aura-system -o json 2>/dev/null | jq -e --arg ns "$RBAC_FIXTURE_NAMESPACE" '
  .metadata.labels["power.aura.sh/source"] == "namespace-annotation" and
  .metadata.labels["power.aura.sh/namespace"] == $ns and
  .spec.scope.namespaces == [$ns] and .spec.schedule.desiredState == "off" and .spec.priority == 321
' >/dev/null; do
  (( SECONDS < deadline )) || { echo "FAIL: controller service account could not create the implicit namespace policy" >&2; exit 6; }
  sleep 3
done
kubectl annotate namespace "$RBAC_FIXTURE_NAMESPACE" aura.sh/power-priority="654" --overwrite >/dev/null
deadline=$((SECONDS + TIMEOUT_SECONDS))
until [[ "$(kubectl get powerpolicy "$IMPLICIT_POLICY_NAME" -n aura-system -o jsonpath='{.spec.priority}' 2>/dev/null || true)" == "654" ]]; do
  (( SECONDS < deadline )) || { echo "FAIL: controller service account could not update the implicit namespace policy" >&2; exit 7; }
  sleep 3
done

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
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: ${WORKLOAD_NAME}
  namespace: ${FIXTURE_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
    aura-power-quality/selected: "true"
  annotations:
    aura.sh/power-eligible: "true"
spec:
  serviceName: ${WORKLOAD_NAME}
  replicas: 1
  selector:
    matchLabels: {app: ${WORKLOAD_NAME}-stateful}
  template:
    metadata:
      labels: {app: ${WORKLOAD_NAME}-stateful}
    spec:
      containers:
        - name: pause
          image: registry.k8s.io/pause:3.10
---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: ${WORKLOAD_NAME}
  namespace: ${FIXTURE_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
    aura-power-quality/selected: "true"
  annotations:
    aura.sh/power-eligible: "true"
spec:
  schedule: "0 0 1 1 *"
  suspend: false
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: Never
          containers:
            - name: pause
              image: registry.k8s.io/pause:3.10
YAML
kubectl rollout status deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" --timeout=120s
kubectl rollout status statefulset "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" --timeout=120s

deployment_uid="$(kubectl get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.metadata.uid}')"

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
    targetRefs:
      - apiVersion: apps/v1
        kind: Deployment
        namespace: "${FIXTURE_NAMESPACE}"
        name: "${WORKLOAD_NAME}"
        uid: "${deployment_uid}"
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
[[ "$(kubectl get statefulset "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')" == "1" ]] || { echo "FAIL: homonymous StatefulSet was selected by Deployment targetRef" >&2; exit 13; }
[[ "$(kubectl get cronjob "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.suspend}')" == "false" ]] || { echo "FAIL: homonymous CronJob was selected by Deployment targetRef" >&2; exit 13; }

kubectl patch powerpolicy "$POLICY_NAME" -n aura-system --type=merge -p '{"spec":{"schedule":{"desiredState":"on","windows":[]}}}'
deadline=$((SECONDS + TIMEOUT_SECONDS))
while (( SECONDS < deadline )); do
  replicas="$(kubectl get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')"
  [[ "$replicas" == "2" ]] && break
  sleep 5
done
[[ "$replicas" == "2" ]] || { echo "FAIL: restore expected replicas=2 observed=${replicas:-unknown}" >&2; exit 11; }

kubectl create -f - <<YAML
apiVersion: power.aura.sh/v1alpha1
kind: PowerNamespaceGroup
metadata:
  name: ${GROUP_NAME}
  namespace: aura-system
spec:
  namespaces: ["${FIXTURE_NAMESPACE}"]
YAML
kubectl label deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" aura-power-quality/selected=true --overwrite >/dev/null
kubectl patch powerpolicy "$POLICY_NAME" -n aura-system --type=merge -p "{\"spec\":{\"scope\":{\"targetRefs\":null,\"namespaceGroups\":[\"${GROUP_NAME}\"],\"namespaceLabels\":{\"aura-power-quality/run\":\"${RUN_ID}\"},\"workloadLabels\":{\"aura-power-quality/selected\":\"true\"}},\"schedule\":{\"desiredState\":\"off\",\"windows\":[]}}}"
deadline=$((SECONDS + TIMEOUT_SECONDS))
while (( SECONDS < deadline )); do
  dep_replicas="$(kubectl get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')"
  sts_replicas="$(kubectl get statefulset "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')"
  cron_suspended="$(kubectl get cronjob "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.suspend}')"
  [[ "$dep_replicas" == "0" && "$sts_replicas" == "0" && "$cron_suspended" == "true" ]] && break
  sleep 5
done
[[ "$dep_replicas/$sts_replicas/$cron_suspended" == "0/0/true" ]] || { echo "FAIL: group+label selection did not converge" >&2; exit 14; }

kubectl patch powerpolicy "$POLICY_NAME" -n aura-system --type=merge -p '{"spec":{"schedule":{"desiredState":"on","windows":[]}}}'
deadline=$((SECONDS + TIMEOUT_SECONDS))
while (( SECONDS < deadline )); do
  dep_replicas="$(kubectl get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')"
  sts_replicas="$(kubectl get statefulset "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')"
  cron_suspended="$(kubectl get cronjob "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.suspend}')"
  [[ "$dep_replicas" == "2" && "$sts_replicas" == "1" && "$cron_suspended" == "false" ]] && break
  sleep 5
done
[[ "$dep_replicas/$sts_replicas/$cron_suspended" == "2/1/false" ]] || { echo "FAIL: exact multi-kind restore failed: ${dep_replicas}/${sts_replicas}/${cron_suspended}" >&2; exit 15; }
echo "kind_core_journey=passed builtins=true implicit_policy_collision_fail_closed=true implicit_policy_create_update_rbac=true identity=true exact_refs=true groups_and_labels=true deployment=true statefulset=true cronjob=true"
