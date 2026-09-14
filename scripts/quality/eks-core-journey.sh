#!/usr/bin/env bash
set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "${script_dir}/process-guard.sh"

# Mutating acceptance journey for one uniquely labelled fixture on eks-aura-prd.
# It validates the live EKS endpoint, refuses collisions, and starts a detached
# recovery process before creating any Kubernetes object.

: "${KUBECONFIG:?set KUBECONFIG to the campaign-specific file}"
: "${AURA_POWER_EKS_MUTATION_ACK:?set AURA_POWER_EKS_MUTATION_ACK=eks-aura-prd}"
: "${AURA_POWER_AWS_PROFILE:?set AURA_POWER_AWS_PROFILE to the Aura Hub operations profile}"
: "${AURA_POWER_EXPECTED_CONTROLLER_DIGEST:?set AURA_POWER_EXPECTED_CONTROLLER_DIGEST to the verified v2.2.2 sha256 digest}"
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
WATCHDOG_LOG=""
RUNTIME_KUBECONFIG=""
RECOVERY_DIR=""
RECOVERY_NONCE="${RECOVERY_NONCE:-$(openssl rand -hex 16)}"
NAMESPACE_UID=""
WORKLOAD_UID=""
POLICY_UID=""
MUTATION_IN_PROGRESS=""
WATCHDOG_IDENTITY=""
CHANNEL_WATCH_IDENTITY=""
PARENT_IDENTITY="$(process_identity "$$")"
AWS_COMMAND_TIMEOUT_SECONDS="${AWS_COMMAND_TIMEOUT_SECONDS:-60}"
KUBE_COMMAND_TIMEOUT_SECONDS="${KUBE_COMMAND_TIMEOUT_SECONDS:-330}"
KUBE_WATCH_TIMEOUT_SECONDS="${KUBE_WATCH_TIMEOUT_SECONDS:-1500}"

[[ "$AURA_POWER_EKS_MUTATION_ACK" == "$EXPECTED_CLUSTER" ]] || { echo "refusing mutation: acknowledgement must equal ${EXPECTED_CLUSTER}" >&2; exit 2; }
[[ "$AURA_POWER_EXPECTED_CONTROLLER_DIGEST" =~ ^sha256:[a-f0-9]{64}$ ]] || { echo "refusing mutation: invalid expected controller digest" >&2; exit 2; }
[[ "$AURA_POWER_EXPECTED_CONTROLLER_RUNTIME_DIGEST" =~ ^sha256:[a-f0-9]{64}$ ]] || { echo "refusing mutation: invalid expected controller runtime digest" >&2; exit 2; }
[[ "$CHANNEL_QUIESCENCE_SECONDS" =~ ^[0-9]+$ && "$CHANNEL_QUIESCENCE_SECONDS" -le 30 ]] || { echo "refusing mutation: CHANNEL_QUIESCENCE_SECONDS must be an integer <= 30" >&2; exit 2; }
[[ "$TIMEOUT_SECONDS" =~ ^[0-9]+$ && "$TIMEOUT_SECONDS" -le 300 ]] || { echo "refusing mutation: TIMEOUT_SECONDS must be an integer <= 300" >&2; exit 2; }
[[ "$RECOVERY_NONCE" =~ ^[a-f0-9]{32}$ ]] || { echo "refusing mutation: recovery nonce must be 32 lowercase hex characters" >&2; exit 2; }
[[ "$RUN_ID" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ && ${#RUN_ID} -le 40 ]] || { echo "refusing mutation: RUN_ID must be a lowercase DNS label with at most 40 characters" >&2; exit 2; }
for timeout_value in "$AWS_COMMAND_TIMEOUT_SECONDS" "$KUBE_COMMAND_TIMEOUT_SECONDS" "$KUBE_WATCH_TIMEOUT_SECONDS"; do
  [[ "$timeout_value" =~ ^[1-9][0-9]*$ && "$timeout_value" -le 1800 ]] || { echo "refusing mutation: external command timeouts must be between 1 and 1800 seconds" >&2; exit 2; }
done

aws_cmd() {
  run_with_process_timeout "$AWS_COMMAND_TIMEOUT_SECONDS" aws "$@"
}

kube() {
  run_with_process_timeout "$KUBE_COMMAND_TIMEOUT_SECONDS" kubectl --request-timeout=30s "$@"
}

kube_watch() {
  run_with_process_timeout "$KUBE_WATCH_TIMEOUT_SECONDS" kubectl --request-timeout=30s "$@"
}

[[ "$(kube config current-context)" == "$EXPECTED_CONTEXT" ]] || { echo "refusing mutation: unexpected kube context" >&2; exit 2; }

remove_runtime_kubeconfig() {
  if [[ -n "${RUNTIME_KUBECONFIG:-}" && -f "$RUNTIME_KUBECONFIG" ]]; then
    unlink "$RUNTIME_KUBECONFIG"
  fi
}

remove_recovery_directory() {
  local entry pending_state
  if [[ -n "${RECOVERY_DIR:-}" && -d "$RECOVERY_DIR" ]]; then
    for entry in cleanup-complete cleanup-requested cleanup-started mutation-in-progress supervisor-ready; do
      rmdir "${RECOVERY_DIR}/${entry}" 2>/dev/null || true
    done
    for pending_state in "${RECOVERY_DIR}"/state.pending.*; do
      [[ ! -f "$pending_state" ]] || unlink "$pending_state"
    done
    [[ ! -f "${RECOVERY_DIR}/state.json" ]] || unlink "${RECOVERY_DIR}/state.json"
    rmdir "$RECOVERY_DIR" 2>/dev/null || true
  fi
}

write_recovery_state_locked() {
  local pending_state
  pending_state="$(mktemp "${RECOVERY_DIR}/state.pending.XXXXXX")"
  chmod 600 "$pending_state"
  if ! jq -n \
    --arg runID "$RUN_ID" \
    --arg recoveryNonce "$RECOVERY_NONCE" \
    --arg fixtureNamespace "$FIXTURE_NAMESPACE" \
    --arg namespaceUID "$NAMESPACE_UID" \
    --arg workloadName "$WORKLOAD_NAME" \
    --arg workloadUID "$WORKLOAD_UID" \
    --arg policyName "$POLICY_NAME" \
    --arg policyUID "$POLICY_UID" \
    '{runID:$runID,recoveryNonce:$recoveryNonce,fixtureNamespace:$fixtureNamespace,namespaceUID:$namespaceUID,workloadName:$workloadName,workloadUID:$workloadUID,policyName:$policyName,policyUID:$policyUID}' \
    >"$pending_state"; then
    unlink "$pending_state"
    return 1
  fi
  if ! mv -f "$pending_state" "${RECOVERY_DIR}/state.json"; then
    unlink "$pending_state" 2>/dev/null || true
    return 1
  fi
}

begin_mutation() {
  mkdir "$MUTATION_IN_PROGRESS" 2>/dev/null || { echo "refusing mutation: another mutation marker exists" >&2; return 1; }
  if [[ -d "${RECOVERY_DIR}/cleanup-requested" || -d "${RECOVERY_DIR}/cleanup-started" || -d "${RECOVERY_DIR}/cleanup-complete" ]] ||
     (( $(date +%s) >= HARD_DEADLINE_EPOCH )); then
    rmdir "$MUTATION_IN_PROGRESS"
    echo "refusing mutation: recovery cleanup has started or deadline elapsed" >&2
    return 1
  fi
}

end_mutation() {
  rmdir "$MUTATION_IN_PROGRESS"
}

cleanup_bootstrap() {
  remove_runtime_kubeconfig
  remove_recovery_directory
}

trap cleanup_bootstrap EXIT

# Use the standard EKS exec credential plugin from a private kubeconfig. This
# refreshes tokens for long journeys without ever putting a bearer token in a
# process argument, where another local process could observe it.
RECOVERY_DIR="$(mktemp -d)"
chmod 700 "$RECOVERY_DIR"
RUNTIME_KUBECONFIG="${RECOVERY_DIR}/kubeconfig"
: >"$RUNTIME_KUBECONFIG"
chmod 600 "$RUNTIME_KUBECONFIG"
aws_cmd eks update-kubeconfig --name "$EXPECTED_CLUSTER" --region "$AWS_REGION" \
  --alias "$EXPECTED_CONTEXT" --kubeconfig "$RUNTIME_KUBECONFIG" \
  --profile "$AURA_POWER_AWS_PROFILE" >/dev/null
export KUBECONFIG="$RUNTIME_KUBECONFIG"
export AURA_POWER_RUNTIME_KUBECONFIG="$RUNTIME_KUBECONFIG"
[[ "$(kube config current-context)" == "$EXPECTED_CONTEXT" ]] || { echo "refusing mutation: generated kube context is unexpected" >&2; exit 2; }

cluster_json="$(aws_cmd eks describe-cluster --name "$EXPECTED_CLUSTER" --region "$AWS_REGION" --profile "$AURA_POWER_AWS_PROFILE" --output json)"
expected_endpoint="$(jq -er .cluster.endpoint <<<"$cluster_json")"
cluster_arn="$(jq -er .cluster.arn <<<"$cluster_json")"
current_endpoint="$(kube config view --minify -o jsonpath='{.clusters[0].cluster.server}')"
[[ "$current_endpoint" == "$expected_endpoint" ]] || { echo "refusing mutation: kubeconfig endpoint does not match EKS" >&2; exit 2; }
[[ "$cluster_arn" == arn:aws:eks:"$AWS_REGION":*:cluster/"$EXPECTED_CLUSTER" ]] || { echo "refusing mutation: unexpected cluster ARN" >&2; exit 2; }

for permission in \
  "create namespaces -" \
  "get namespaces -" \
  "list namespaces -" \
  "delete namespaces -" \
  "create deployments.apps ${FIXTURE_NAMESPACE}" \
  "get deployments.apps ${FIXTURE_NAMESPACE}" \
  "list deployments.apps ${FIXTURE_NAMESPACE}" \
  "watch deployments.apps ${FIXTURE_NAMESPACE}" \
  "update deployments.apps ${FIXTURE_NAMESPACE}" \
  "patch deployments.apps ${FIXTURE_NAMESPACE}" \
  "get deployments.apps/scale ${FIXTURE_NAMESPACE}" \
  "update deployments.apps/scale ${FIXTURE_NAMESPACE}" \
  "patch deployments.apps/scale ${FIXTURE_NAMESPACE}" \
  "delete deployments.apps ${FIXTURE_NAMESPACE}" \
  "get deployments.apps ${CONTROL_NAMESPACE}" \
  "watch deployments.apps ${CONTROL_NAMESPACE}" \
  "list pods ${CONTROL_NAMESPACE}" \
  "get customresourcedefinitions.apiextensions.k8s.io -" \
  "create powerpolicies.power.aura.sh ${CONTROL_NAMESPACE}" \
  "get powerpolicies.power.aura.sh ${CONTROL_NAMESPACE}" \
  "list powerpolicies.power.aura.sh ${CONTROL_NAMESPACE}" \
  "patch powerpolicies.power.aura.sh ${CONTROL_NAMESPACE}" \
  "delete powerpolicies.power.aura.sh ${CONTROL_NAMESPACE}" \
  "get powertargets.power.aura.sh ${CONTROL_NAMESPACE}" \
  "list powertargets.power.aura.sh ${CONTROL_NAMESPACE}" \
  "get powerauditevents.power.aura.sh ${CONTROL_NAMESPACE}" \
  "list powerauditevents.power.aura.sh ${CONTROL_NAMESPACE}" \
  "get powernotificationchannels.power.aura.sh ${CONTROL_NAMESPACE}" \
  "list powernotificationchannels.power.aura.sh ${CONTROL_NAMESPACE}" \
  "watch powernotificationchannels.power.aura.sh ${CONTROL_NAMESPACE}"; do
  read -r verb resource permission_namespace <<<"$permission"
  auth_args=(auth can-i "$verb" "$resource")
  if [[ "$permission_namespace" != "-" ]]; then
    auth_args+=(-n "$permission_namespace")
  fi
  [[ "$(kube "${auth_args[@]}")" == "yes" ]] || { echo "missing permission: ${verb} ${resource} namespace=${permission_namespace}" >&2; exit 3; }
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
cleanup_on_exit() {
  local original_status=$?
  local watchdog_status=0 cleanup_status=0 shutdown_deadline=0
  trap - EXIT INT TERM HUP
  rmdir "$MUTATION_IN_PROGRESS" 2>/dev/null || true
  if [[ -n "$CHANNEL_WATCH_PID" ]]; then
    [[ -z "$CHANNEL_WATCH_IDENTITY" ]] || terminate_process_tree "$CHANNEL_WATCH_PID" "$CHANNEL_WATCH_IDENTITY" TERM
    wait "$CHANNEL_WATCH_PID" 2>/dev/null || true
  fi
  if ! mkdir "${RECOVERY_DIR}/cleanup-requested" 2>/dev/null && [[ ! -d "${RECOVERY_DIR}/cleanup-requested" && ! -d "${RECOVERY_DIR}/cleanup-complete" ]]; then
    echo "FATAL: could not signal the recovery supervisor; artifacts retained in $RECOVERY_DIR" >&2
    exit 90
  fi
  if [[ -n "$WATCHDOG_PID" ]]; then
    shutdown_deadline=$(( $(date +%s) + 420 ))
    while process_is_running_identity "$WATCHDOG_PID" "$WATCHDOG_IDENTITY" && (( $(date +%s) < shutdown_deadline )); do
      sleep 1
    done
    if process_is_running_identity "$WATCHDOG_PID" "$WATCHDOG_IDENTITY"; then
      terminate_process_tree "$WATCHDOG_PID" "$WATCHDOG_IDENTITY" TERM
      watchdog_status=124
    fi
    wait "$WATCHDOG_PID" 2>/dev/null || watchdog_status=$?
  fi
  if [[ ! -d "${RECOVERY_DIR}/cleanup-complete" ]]; then
    "$WATCHDOG" cleanup || cleanup_status=$?
  fi
  if [[ "$watchdog_status" -ne 0 && "$cleanup_status" -eq 0 && ! -d "${RECOVERY_DIR}/cleanup-complete" ]]; then
    cleanup_status="$watchdog_status"
  fi
  if [[ "$cleanup_status" -ne 0 || ! -d "${RECOVERY_DIR}/cleanup-complete" ]]; then
    echo "FATAL: recovery artifacts retained in $RECOVERY_DIR" >&2
    echo "FATAL: fixture recovery or deletion was not verified" >&2
    exit 90
  fi
  for private_log in "$CHANNEL_WATCH_LOG" "$CHANNEL_WATCH_ERROR_LOG" "$WATCHDOG_LOG"; do
    [[ -z "$private_log" || ! -f "$private_log" ]] || unlink "$private_log"
  done
  remove_recovery_directory
  if [[ -e "$RUNTIME_KUBECONFIG" ]]; then
    echo "FATAL: watchdog completed without removing the runtime kubeconfig" >&2
    exit 90
  fi
  unset AURA_POWER_RUNTIME_KUBECONFIG
  exit "$original_status"
}

# Build the recovery state and start its supervisor before the first Kubernetes
# mutation. Empty UIDs mean "capture the exact run+nonce-labelled object"; once
# the API server returns an identity, the state is atomically replaced.
MUTATION_IN_PROGRESS="${RECOVERY_DIR}/mutation-in-progress"
HARD_DEADLINE_EPOCH="$(( $(date +%s) + TIMEOUT_SECONDS * 4 + CHANNEL_QUIESCENCE_SECONDS + 300 ))"
state_status=0
write_recovery_state_locked || state_status=$?
[[ "$state_status" -eq 0 ]] || exit "$state_status"
export RUN_ID RECOVERY_NONCE FIXTURE_NAMESPACE WORKLOAD_NAME POLICY_NAME RECOVERY_DIR PARENT_IDENTITY
export PARENT_PID="$$" HARD_DEADLINE_EPOCH
WATCHDOG_LOG="$(mktemp)"
chmod 600 "$WATCHDOG_LOG"
nohup "$WATCHDOG" watch </dev/null >"$WATCHDOG_LOG" 2>&1 &
WATCHDOG_PID=$!
disown "$WATCHDOG_PID" 2>/dev/null || true
WATCHDOG_IDENTITY="$(process_identity "$WATCHDOG_PID")" || {
  echo "refusing mutation: could not capture recovery supervisor identity" >&2
  exit 5
}
trap cleanup_on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP
ready_attempts=0
while [[ ! -d "${RECOVERY_DIR}/supervisor-ready" ]]; do
  process_is_running_identity "$WATCHDOG_PID" "$WATCHDOG_IDENTITY" || {
    cat "$WATCHDOG_LOG" >&2
    echo "refusing mutation: recovery supervisor failed before readiness" >&2
    exit 5
  }
  ready_attempts=$((ready_attempts + 1))
  (( ready_attempts < 40 )) || { echo "refusing mutation: recovery supervisor readiness timed out" >&2; exit 5; }
  sleep 0.25
done

begin_mutation
namespace_status=0
namespace_json="$(kube create -f - -o json <<YAML
apiVersion: v1
kind: Namespace
metadata:
  name: ${FIXTURE_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
    aura-power-quality/nonce: "${RECOVERY_NONCE}"
    app.kubernetes.io/part-of: aura-power-quality
    power.aura.sh/notification-policy: disabled
  annotations:
    aura.sh/power-eligible: "true"
YAML
)" || namespace_status=$?
if [[ "$namespace_status" -ne 0 ]]; then
  end_mutation
  exit "$namespace_status"
fi
uid_status=0
NAMESPACE_UID="$(jq -er '.metadata.uid' <<<"$namespace_json")" || uid_status=$?
if [[ "$uid_status" -ne 0 ]]; then
  end_mutation
  exit "$uid_status"
fi
state_status=0
write_recovery_state_locked || state_status=$?
end_mutation
[[ "$state_status" -eq 0 ]] || exit "$state_status"

begin_mutation
workload_status=0
workload_json="$(kube create -f - -o json <<YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${WORKLOAD_NAME}
  namespace: ${FIXTURE_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
    aura-power-quality/nonce: "${RECOVERY_NONCE}"
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
)" || workload_status=$?
if [[ "$workload_status" -ne 0 ]]; then
  end_mutation
  exit "$workload_status"
fi
uid_status=0
WORKLOAD_UID="$(jq -er '.metadata.uid' <<<"$workload_json")" || uid_status=$?
if [[ "$uid_status" -ne 0 ]]; then
  end_mutation
  exit "$uid_status"
fi
state_status=0
write_recovery_state_locked || state_status=$?
end_mutation
[[ "$state_status" -eq 0 ]] || exit "$state_status"
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

echo "run_id=${RUN_ID} context=${EXPECTED_CONTEXT} cluster_arn=${cluster_arn}"
live_namespace_json="$(kube get namespace "$FIXTURE_NAMESPACE" -o json)"
live_workload_json="$(kube get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o json)"
live_target_json="$(kube get powertarget "$target" -n "$CONTROL_NAMESPACE" -o json)"
[[ "$(jq -r '.metadata.uid' <<<"$live_namespace_json")" == "$NAMESPACE_UID" && "$(jq -r '.metadata.labels["power.aura.sh/notification-policy"]' <<<"$live_namespace_json")" == "disabled" ]] || { echo "FAIL: campaign namespace identity or suppression policy changed" >&2; exit 13; }
[[ "$(jq -r '.metadata.uid' <<<"$live_workload_json")" == "$WORKLOAD_UID" ]] || { echo "FAIL: fixture workload UID changed before policy creation" >&2; exit 13; }
[[ "$(jq -r '.spec.targetRef.uid' <<<"$live_target_json")" == "$WORKLOAD_UID" ]] || { echo "FAIL: exact target UID changed before policy creation" >&2; exit 13; }
CHANNEL_WATCH_LOG="$(mktemp)"
CHANNEL_WATCH_ERROR_LOG="$(mktemp)"
chmod 600 "$CHANNEL_WATCH_LOG"
chmod 600 "$CHANNEL_WATCH_ERROR_LOG"
(
  kube_watch get powernotificationchannel -n "$CONTROL_NAMESPACE" --watch -o json 2>>"$CHANNEL_WATCH_ERROR_LOG" |
    jq --unbuffered -r '.status.recentAttempts[]?.auditEventRefs[]? // empty' >>"$CHANNEL_WATCH_LOG" 2>>"$CHANNEL_WATCH_ERROR_LOG"
) &
CHANNEL_WATCH_PID=$!
CHANNEL_WATCH_IDENTITY="$(process_identity "$CHANNEL_WATCH_PID")" || { echo "FAIL: notification channel watch identity unavailable" >&2; exit 15; }
sleep 2
same_process_identity "$CHANNEL_WATCH_PID" "$CHANNEL_WATCH_IDENTITY" || {
  echo "FAIL: notification channel watch did not become ready" >&2
  tail -n 20 "$CHANNEL_WATCH_ERROR_LOG" >&2 || true
  exit 15
}
begin_mutation
policy_status=0
policy_json="$(kube create -f - -o json <<YAML
apiVersion: power.aura.sh/v1alpha1
kind: PowerPolicy
metadata:
  name: ${POLICY_NAME}
  namespace: ${CONTROL_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
    aura-power-quality/nonce: "${RECOVERY_NONCE}"
spec:
  scope:
    namespaces: ["${FIXTURE_NAMESPACE}"]
  schedule:
    desiredState: "off"
    windows: []
  priority: 1000
  description: "Disposable Aura Power acceptance fixture ${RUN_ID}"
YAML
)" || policy_status=$?
if [[ "$policy_status" -ne 0 ]]; then
  end_mutation
  exit "$policy_status"
fi
uid_status=0
POLICY_UID="$(jq -er '.metadata.uid' <<<"$policy_json")" || uid_status=$?
if [[ "$uid_status" -ne 0 ]]; then
  end_mutation
  exit "$uid_status"
fi
state_status=0
write_recovery_state_locked || state_status=$?
end_mutation
[[ "$state_status" -eq 0 ]] || exit "$state_status"

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

begin_mutation
patch_status=0
kube patch powerpolicy "$POLICY_NAME" -n "$CONTROL_NAMESPACE" --type=json -p "$(jq -nc --arg uid "$POLICY_UID" '[{op:"test",path:"/metadata/uid",value:$uid},{op:"replace",path:"/spec/schedule/desiredState",value:"on"}]')" || patch_status=$?
end_mutation
[[ "$patch_status" -eq 0 ]] || exit "$patch_status"
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
same_process_identity "$CHANNEL_WATCH_PID" "$CHANNEL_WATCH_IDENTITY" || {
  echo "FAIL: notification channel watch ended before the evidence window closed" >&2
  tail -n 20 "$CHANNEL_WATCH_ERROR_LOG" >&2 || true
  exit 15
}
terminate_process_tree "$CHANNEL_WATCH_PID" "$CHANNEL_WATCH_IDENTITY" TERM
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
