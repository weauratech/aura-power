#!/usr/bin/env bash
set -Eeuo pipefail

# Mutating acceptance journey for one uniquely labelled fixture on eks-aura-prd.
# It validates the live EKS endpoint, refuses collisions, and starts a detached
# recovery process before creating the policy that can change replicas.

: "${KUBECONFIG:?set KUBECONFIG to the campaign-specific file}"
: "${AURA_POWER_EKS_MUTATION_ACK:?set AURA_POWER_EKS_MUTATION_ACK=eks-aura-prd}"
: "${AURA_POWER_AWS_PROFILE:?set AURA_POWER_AWS_PROFILE to the Aura Hub operations profile}"
: "${AURA_POWER_EXPECTED_CONTROLLER_DIGEST:?set AURA_POWER_EXPECTED_CONTROLLER_DIGEST to the verified v2.2.1 sha256 digest}"
: "${AURA_POWER_EXPECTED_CONTROLLER_RUNTIME_DIGEST:?set AURA_POWER_EXPECTED_CONTROLLER_RUNTIME_DIGEST to the resolved controller platform digest}"

EXPECTED_CLUSTER="eks-aura-prd"
EXPECTED_CONTEXT="aura-power-quality-eks-operations"
AWS_REGION="${AWS_REGION:-us-east-2}"
export EXPECTED_CLUSTER AWS_REGION
CONTROL_NAMESPACE="aura-system"
RUN_ID="${RUN_ID:-$(date -u +%Y%m%d%H%M%S)}"
FIXTURE_NAMESPACE="aura-power-e2e-${RUN_ID}"
POLICY_NAME="quality-${RUN_ID}"
WORKLOAD_NAME="restore-two"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-300}"
WATCHDOG="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/eks-fixture-watchdog.sh"
CHANNEL_QUIESCENCE_SECONDS="${CHANNEL_QUIESCENCE_SECONDS:-10}"
CHANNEL_WATCH_LOG=""
CHANNEL_WATCH_ERROR_LOG=""

[[ "$AURA_POWER_EKS_MUTATION_ACK" == "$EXPECTED_CLUSTER" ]] || { echo "refusing mutation: acknowledgement must equal ${EXPECTED_CLUSTER}" >&2; exit 2; }
[[ "$AURA_POWER_EXPECTED_CONTROLLER_DIGEST" =~ ^sha256:[a-f0-9]{64}$ ]] || { echo "refusing mutation: invalid expected controller digest" >&2; exit 2; }
[[ "$AURA_POWER_EXPECTED_CONTROLLER_RUNTIME_DIGEST" =~ ^sha256:[a-f0-9]{64}$ ]] || { echo "refusing mutation: invalid expected controller runtime digest" >&2; exit 2; }
[[ "$CHANNEL_QUIESCENCE_SECONDS" =~ ^[0-9]+$ && "$CHANNEL_QUIESCENCE_SECONDS" -le 30 ]] || { echo "refusing mutation: CHANNEL_QUIESCENCE_SECONDS must be an integer <= 30" >&2; exit 2; }
[[ "$TIMEOUT_SECONDS" =~ ^[0-9]+$ && "$TIMEOUT_SECONDS" -le 300 ]] || { echo "refusing mutation: TIMEOUT_SECONDS must be an integer <= 300" >&2; exit 2; }
[[ "$(kubectl config current-context)" == "$EXPECTED_CONTEXT" ]] || { echo "refusing mutation: unexpected kube context" >&2; exit 2; }

credential_file="$(mktemp)"
chmod 600 "$credential_file"
remove_credential_file() {
  if [[ -n "${credential_file:-}" && -f "$credential_file" ]]; then
    : >"$credential_file"
    unlink "$credential_file"
  fi
}
trap remove_credential_file EXIT
aws configure export-credentials --profile "$AURA_POWER_AWS_PROFILE" --format process >"$credential_file"
AWS_ACCESS_KEY_ID="$(jq -er .AccessKeyId "$credential_file")"
AWS_SECRET_ACCESS_KEY="$(jq -er .SecretAccessKey "$credential_file")"
export AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY
session_token="$(jq -r '.SessionToken // empty' "$credential_file")"
[[ -n "$session_token" ]] && export AWS_SESSION_TOKEN="$session_token"
remove_credential_file
trap - EXIT

cluster_json="$(aws eks describe-cluster --name "$EXPECTED_CLUSTER" --region "$AWS_REGION" --output json)"
expected_endpoint="$(jq -er .cluster.endpoint <<<"$cluster_json")"
cluster_arn="$(jq -er .cluster.arn <<<"$cluster_json")"
current_endpoint="$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')"
[[ "$current_endpoint" == "$expected_endpoint" ]] || { echo "refusing mutation: kubeconfig endpoint does not match EKS" >&2; exit 2; }
[[ "$cluster_arn" == arn:aws:eks:"$AWS_REGION":*:cluster/"$EXPECTED_CLUSTER" ]] || { echo "refusing mutation: unexpected cluster ARN" >&2; exit 2; }

kube() {
  local token
  token="$(aws eks get-token --cluster-name "$EXPECTED_CLUSTER" --region "$AWS_REGION" | jq -er .status.token)"
  kubectl --token="$token" "$@"
}

for permission in "create namespaces" "delete namespaces" "create deployments.apps" "patch deployments.apps" "create powerpolicies.power.aura.sh" "delete powerpolicies.power.aura.sh"; do
  read -r verb resource <<<"$permission"
  [[ "$(kube auth can-i "$verb" "$resource" -n "$CONTROL_NAMESPACE")" == "yes" ]] || { echo "missing permission: ${verb} ${resource}" >&2; exit 3; }
done

# Prove the running controller, not only the chart metadata, has the suppression
# capability and matches the independently verified release digest.
kube rollout status deployment aura-power-controller -n "$CONTROL_NAMESPACE" --timeout=120s >/dev/null
controller_json="$(kube get deployment aura-power-controller -n "$CONTROL_NAMESPACE" -o json)"
[[ "$(jq -r '.status.observedGeneration' <<<"$controller_json")" == "$(jq -r '.metadata.generation' <<<"$controller_json")" ]] || { echo "refusing mutation: controller generation is not observed" >&2; exit 3; }
[[ "$(jq -r '.status.availableReplicas // 0' <<<"$controller_json")" == "$(jq -r '.spec.replicas' <<<"$controller_json")" ]] || { echo "refusing mutation: controller is not fully available" >&2; exit 3; }
[[ "$(jq -r '.spec.template.spec.containers[] | select(.name == "controller") | .image' <<<"$controller_json")" == *@"$AURA_POWER_EXPECTED_CONTROLLER_DIGEST" ]] || { echo "refusing mutation: controller spec is not pinned to the expected digest" >&2; exit 3; }
[[ "$(jq -r '.spec.template.spec.containers[] | select(.name == "controller") | .readinessProbe.httpGet.path' <<<"$controller_json")" == "/readyz/notification-suppression-v1" ]] || { echo "refusing mutation: controller suppression capability probe is absent" >&2; exit 3; }
controller_pods="$(kube get pod -n "$CONTROL_NAMESPACE" -l 'app.kubernetes.io/instance=aura-power,app.kubernetes.io/component=controller' -o json)"
[[ "$(jq '.items | length' <<<"$controller_pods")" -ge 1 ]] || { echo "refusing mutation: no controller pod found" >&2; exit 3; }
jq -e --arg digest "$AURA_POWER_EXPECTED_CONTROLLER_RUNTIME_DIGEST" 'all(.items[]; ([.status.containerStatuses[] | select(.name == "controller")] | length == 1 and all(.[]; .ready == true and (.imageID | endswith("@" + $digest)))))' <<<"$controller_pods" >/dev/null || { echo "refusing mutation: a controller container is unready or has an unexpected platform imageID" >&2; exit 3; }
kube get crd powerauditevents.power.aura.sh powertargets.power.aura.sh -o json | jq -e '
  def valid($p):
    $p.notificationSuppressed.type == "boolean" and
    $p.notificationSuppressionNamespaceUID.type == "string" and
    $p.notificationSuppressionSource.type == "string" and
    $p.notificationSuppressionSource.enum == ["namespace-label"];
  valid(.items[] | select(.metadata.name == "powerauditevents.power.aura.sh") | .spec.versions[] | select(.storage) | .schema.openAPIV3Schema.properties.spec.properties) and
  valid(.items[] | select(.metadata.name == "powertargets.power.aura.sh") | .spec.versions[] | select(.storage) | .schema.openAPIV3Schema.properties.status.properties.action.properties)
' >/dev/null || { echo "refusing mutation: suppression capability CRD schema is absent" >&2; exit 3; }

if kube get namespace "$FIXTURE_NAMESPACE" >/dev/null 2>&1 || kube get powerpolicy "$POLICY_NAME" -n "$CONTROL_NAMESPACE" >/dev/null 2>&1; then
  echo "refusing mutation: campaign namespace or policy already exists" >&2
  exit 4
fi

WATCHDOG_PID=""
CHANNEL_WATCH_PID=""
NAMESPACE_UID=""
WORKLOAD_UID=""
cleanup_on_exit() {
  local original_status=$?
  trap - EXIT INT TERM HUP
  if [[ -n "$WATCHDOG_PID" ]]; then
    kill "$WATCHDOG_PID" >/dev/null 2>&1 || true
    wait "$WATCHDOG_PID" 2>/dev/null || true
  fi
  if [[ -n "$CHANNEL_WATCH_PID" ]]; then
    kill "$CHANNEL_WATCH_PID" >/dev/null 2>&1 || true
    wait "$CHANNEL_WATCH_PID" 2>/dev/null || true
  fi
  if [[ -n "$CHANNEL_WATCH_LOG" && -f "$CHANNEL_WATCH_LOG" ]]; then
    rm -f "$CHANNEL_WATCH_LOG"
  fi
  if [[ -n "$CHANNEL_WATCH_ERROR_LOG" && -f "$CHANNEL_WATCH_ERROR_LOG" ]]; then
    rm -f "$CHANNEL_WATCH_ERROR_LOG"
  fi
  local cleanup_status=0
  if [[ -n "$NAMESPACE_UID" && -n "$WORKLOAD_UID" ]]; then
    export RUN_ID FIXTURE_NAMESPACE NAMESPACE_UID WORKLOAD_NAME WORKLOAD_UID POLICY_NAME
    "$WATCHDOG" cleanup || cleanup_status=$?
  fi
  unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN
  if [[ "$cleanup_status" -ne 0 ]]; then
    echo "FATAL: fixture recovery or deletion was not verified" >&2
    exit 90
  fi
  exit "$original_status"
}
trap cleanup_on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

kube create -f - <<YAML
apiVersion: v1
kind: Namespace
metadata:
  name: ${FIXTURE_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
    app.kubernetes.io/part-of: aura-power-quality
    power.aura.sh/notification-policy: disabled
  annotations:
    aura.sh/power-eligible: "true"
YAML
NAMESPACE_UID="$(kube get namespace "$FIXTURE_NAMESPACE" -o jsonpath='{.metadata.uid}')"

kube create -f - <<YAML
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
      tolerations:
        - {key: scheduling.aura.io/domain, operator: Equal, value: workloads, effect: NoSchedule}
      containers:
        - name: pause
          image: registry.k8s.io/pause:3.10
          resources:
            requests: {cpu: 10m, memory: 8Mi}
            limits: {cpu: 20m, memory: 16Mi}
YAML
WORKLOAD_UID="$(kube get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.metadata.uid}')"
kube rollout status deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" --timeout=120s

# Suppression is a discovered, durable property of the exact target. Prove it
# before creating a policy that can emit a transition audit or external event.
deadline=$((SECONDS + TIMEOUT_SECONDS))
target=""
while (( SECONDS < deadline )); do
  target_list="$(kube get powertarget -n "$CONTROL_NAMESPACE" -l "power.aura.sh/target-namespace=${FIXTURE_NAMESPACE},power.aura.sh/target-name=${WORKLOAD_NAME},power.aura.sh/target-kind=Deployment" -o json 2>/dev/null || true)"
  target="$(jq -r --arg uid "$WORKLOAD_UID" --arg namespace "$FIXTURE_NAMESPACE" --arg name "$WORKLOAD_NAME" '.items | map(select(.spec.targetRef.uid == $uid and .spec.targetRef.namespace == $namespace and .spec.targetRef.name == $name and .spec.targetRef.kind == "Deployment")) | if length == 1 then .[0].metadata.name else "" end' <<<"${target_list:-{}}" 2>/dev/null || true)"
  [[ -n "$target" ]] && break
  sleep 5
done
[[ -n "$target" ]] || { echo "FAIL: discovered PowerTarget not found before mutation" >&2; exit 10; }
[[ "$(jq '.items | length' <<<"$target_list")" == "1" ]] || { echo "FAIL: target selector is ambiguous" >&2; exit 13; }
target_json="$(jq '.items[0]' <<<"$target_list")"
namespace_notification_policy="$(jq -r '.status.namespaceLabels["power.aura.sh/notification-policy"] // ""' <<<"$target_json")"
[[ "$(jq -r '.spec.targetRef.uid' <<<"$target_json")" == "$WORKLOAD_UID" && "$namespace_notification_policy" == "disabled" ]] || {
  echo "FAIL: notification suppression was not discovered on the exact fixture target" >&2
  exit 13
}

export RUN_ID FIXTURE_NAMESPACE NAMESPACE_UID WORKLOAD_NAME WORKLOAD_UID POLICY_NAME
export PARENT_PID="$$" HARD_DEADLINE_EPOCH="$(( $(date +%s) + TIMEOUT_SECONDS * 2 + 60 ))"
watchdog_log="$(mktemp)"
chmod 600 "$watchdog_log"
nohup "$WATCHDOG" watch </dev/null >"$watchdog_log" 2>&1 &
WATCHDOG_PID=$!
disown "$WATCHDOG_PID" 2>/dev/null || true

echo "run_id=${RUN_ID} context=${EXPECTED_CONTEXT} cluster_arn=${cluster_arn}"
live_namespace_json="$(kube get namespace "$FIXTURE_NAMESPACE" -o json)"
live_target_json="$(kube get powertarget "$target" -n "$CONTROL_NAMESPACE" -o json)"
[[ "$(jq -r '.metadata.uid' <<<"$live_namespace_json")" == "$NAMESPACE_UID" && "$(jq -r '.metadata.labels["power.aura.sh/notification-policy"]' <<<"$live_namespace_json")" == "disabled" ]] || { echo "FAIL: campaign namespace identity or suppression policy changed" >&2; exit 13; }
[[ "$(jq -r '.spec.targetRef.uid' <<<"$live_target_json")" == "$WORKLOAD_UID" ]] || { echo "FAIL: exact target UID changed before policy creation" >&2; exit 13; }
CHANNEL_WATCH_LOG="$(mktemp)"
CHANNEL_WATCH_ERROR_LOG="$(mktemp)"
chmod 600 "$CHANNEL_WATCH_LOG"
chmod 600 "$CHANNEL_WATCH_ERROR_LOG"
kube get powernotificationchannel -n "$CONTROL_NAMESPACE" --watch -o json 2>>"$CHANNEL_WATCH_ERROR_LOG" |
  jq --unbuffered -r '.status.recentAttempts[]?.auditEventRefs[]? // empty' >>"$CHANNEL_WATCH_LOG" 2>>"$CHANNEL_WATCH_ERROR_LOG" &
CHANNEL_WATCH_PID=$!
sleep 2
kill -0 "$CHANNEL_WATCH_PID" 2>/dev/null || {
  echo "FAIL: notification channel watch did not become ready" >&2
  tail -n 20 "$CHANNEL_WATCH_ERROR_LOG" >&2 || true
  exit 15
}
kube create -f - <<YAML
apiVersion: power.aura.sh/v1alpha1
kind: PowerPolicy
metadata:
  name: ${POLICY_NAME}
  namespace: ${CONTROL_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
spec:
  scope:
    namespaces: ["${FIXTURE_NAMESPACE}"]
  schedule:
    desiredState: "off"
    windows: []
  priority: 1000
  description: "Disposable Aura Power acceptance fixture ${RUN_ID}"
YAML

deadline=$((SECONDS + TIMEOUT_SECONDS))
replicas=""
while (( SECONDS < deadline )); do
  replicas="$(kube get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')"
  [[ "$replicas" == "0" ]] && break
  sleep 5
done
[[ "$replicas" == "0" ]] || { echo "FAIL: power-down did not converge" >&2; exit 10; }

snapshot="$(kube get powertarget "$target" -n "$CONTROL_NAMESPACE" -o jsonpath='{.status.snapshot.replicaCount}')"
[[ "$snapshot" == "2" ]] || { echo "FAIL: snapshot expected replicas=2 observed=${snapshot:-missing}" >&2; exit 12; }
power_down_action="$(kube get powertarget "$target" -n "$CONTROL_NAMESPACE" -o json)"
jq -e --arg namespaceUID "$NAMESPACE_UID" '.status.action.notificationSuppressed == true and .status.action.notificationSuppressionSource == "namespace-label" and .status.action.notificationSuppressionNamespaceUID == $namespaceUID' <<<"$power_down_action" >/dev/null || { echo "FAIL: power-down action did not persist the suppression decision" >&2; exit 15; }
echo "power_down=passed original_replicas=2 snapshot_replicas=${snapshot:-missing}"

kube patch powerpolicy "$POLICY_NAME" -n "$CONTROL_NAMESPACE" --type=merge -p '{"spec":{"schedule":{"desiredState":"on","windows":[]}}}'
deadline=$((SECONDS + TIMEOUT_SECONDS))
while (( SECONDS < deadline )); do
  replicas="$(kube get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')"
  [[ "$replicas" == "2" ]] && break
  sleep 5
done
[[ "$replicas" == "2" ]] || { echo "FAIL: restore expected replicas=2 observed=${replicas:-unknown}" >&2; exit 11; }
echo "restore=passed replicas=2"
restore_action="$(kube get powertarget "$target" -n "$CONTROL_NAMESPACE" -o json)"
jq -e --arg namespaceUID "$NAMESPACE_UID" '.status.action.desiredState == "on" and .status.action.notificationSuppressed == true and .status.action.notificationSuppressionSource == "namespace-label" and .status.action.notificationSuppressionNamespaceUID == $namespaceUID' <<<"$restore_action" >/dev/null || { echo "FAIL: restore action did not persist the suppression decision" >&2; exit 15; }

# Both transitions remain auditable, but the campaign label must keep their
# references out of every external channel attempt.
deadline=$((SECONDS + TIMEOUT_SECONDS))
audit_json=""
while (( SECONDS < deadline )); do
  audit_json="$(kube get powerauditevent -n "$CONTROL_NAMESPACE" -l "power.aura.sh/target-namespace=${FIXTURE_NAMESPACE},power.aura.sh/target-name=${WORKLOAD_NAME},power.aura.sh/target-kind=Deployment,power.aura.sh/target-uid=${WORKLOAD_UID}" -o json)"
  if jq -e 'any(.items[]; .spec.action == "workload.powered_down") and any(.items[]; .spec.action == "workload.restored")' <<<"$audit_json" >/dev/null; then
    break
  fi
  sleep 5
done
[[ -n "$audit_json" ]] || { echo "FAIL: fixture audit evidence was not readable" >&2; exit 14; }
jq -e 'any(.items[]; .spec.action == "workload.powered_down") and any(.items[]; .spec.action == "workload.restored")' <<<"$audit_json" >/dev/null || {
  echo "FAIL: both fixture transition audits were not persisted" >&2
  exit 14
}
jq -e --arg namespaceUID "$NAMESPACE_UID" '[.items[] | select(.spec.action == "workload.powered_down" or .spec.action == "workload.restored" or .spec.action == "execution.error") | .spec.notificationSuppressed == true and .spec.notificationSuppressionSource == "namespace-label" and .spec.notificationSuppressionNamespaceUID == $namespaceUID] | all' <<<"$audit_json" >/dev/null || {
  echo "FAIL: a notifiable fixture audit lacks suppression evidence" >&2
  exit 15
}
sleep "$CHANNEL_QUIESCENCE_SECONDS"
kill -0 "$CHANNEL_WATCH_PID" 2>/dev/null || {
  echo "FAIL: notification channel watch ended before the evidence window closed" >&2
  tail -n 20 "$CHANNEL_WATCH_ERROR_LOG" >&2 || true
  exit 15
}
kill "$CHANNEL_WATCH_PID" >/dev/null 2>&1 || true
wait "$CHANNEL_WATCH_PID" 2>/dev/null || true
CHANNEL_WATCH_PID=""
while IFS= read -r audit_name; do
  audit_ref="${CONTROL_NAMESPACE}/${audit_name}"
  if grep -Fxq "$audit_ref" "$CHANNEL_WATCH_LOG"; then
    echo "FAIL: channel watch observed externally queued fixture audit ${audit_ref}" >&2
    exit 15
  fi
done < <(jq -r '.items[] | select(.spec.action == "workload.powered_down" or .spec.action == "workload.restored" or .spec.action == "execution.error") | .metadata.name' <<<"$audit_json")
echo "notification_suppression=passed audit_events=$(jq '.items | length' <<<"$audit_json") decision_source=namespace-label"
