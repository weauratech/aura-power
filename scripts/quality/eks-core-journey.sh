#!/usr/bin/env bash
set -Eeuo pipefail

# Mutating acceptance journey for one uniquely labelled fixture on eks-aura-prd.
# It validates the live EKS endpoint, refuses collisions, and starts a detached
# recovery process before creating the policy that can change replicas.

: "${KUBECONFIG:?set KUBECONFIG to the campaign-specific file}"
: "${AURA_POWER_EKS_MUTATION_ACK:?set AURA_POWER_EKS_MUTATION_ACK=eks-aura-prd}"
: "${AURA_POWER_AWS_PROFILE:?set AURA_POWER_AWS_PROFILE to the Aura Hub operations profile}"

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

[[ "$AURA_POWER_EKS_MUTATION_ACK" == "$EXPECTED_CLUSTER" ]] || { echo "refusing mutation: acknowledgement must equal ${EXPECTED_CLUSTER}" >&2; exit 2; }
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

if kube get namespace "$FIXTURE_NAMESPACE" >/dev/null 2>&1 || kube get powerpolicy "$POLICY_NAME" -n "$CONTROL_NAMESPACE" >/dev/null 2>&1; then
  echo "refusing mutation: campaign namespace or policy already exists" >&2
  exit 4
fi

WATCHDOG_PID=""
NAMESPACE_UID=""
WORKLOAD_UID=""
cleanup_on_exit() {
  local original_status=$?
  trap - EXIT INT TERM HUP
  if [[ -n "$WATCHDOG_PID" ]]; then
    kill "$WATCHDOG_PID" >/dev/null 2>&1 || true
    wait "$WATCHDOG_PID" 2>/dev/null || true
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

export RUN_ID FIXTURE_NAMESPACE NAMESPACE_UID WORKLOAD_NAME WORKLOAD_UID POLICY_NAME
export PARENT_PID="$$" HARD_DEADLINE_EPOCH="$(( $(date +%s) + TIMEOUT_SECONDS * 2 + 60 ))"
watchdog_log="$(mktemp)"
chmod 600 "$watchdog_log"
nohup "$WATCHDOG" watch </dev/null >"$watchdog_log" 2>&1 &
WATCHDOG_PID=$!
disown "$WATCHDOG_PID" 2>/dev/null || true

echo "run_id=${RUN_ID} context=${EXPECTED_CONTEXT} cluster_arn=${cluster_arn}"
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

target="${FIXTURE_NAMESPACE}--${WORKLOAD_NAME}"
snapshot="$(kube get powertarget "$target" -n "$CONTROL_NAMESPACE" -o jsonpath='{.status.snapshot.replicaCount}')"
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
