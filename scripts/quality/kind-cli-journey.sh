#!/usr/bin/env bash
set -Eeuo pipefail

: "${KUBECONFIG:?set KUBECONFIG to the disposable Kind kubeconfig}"
[[ "$(kubectl config current-context)" == "kind-aura-power-quality" ]] || { echo "refusing mutation: this runner is restricted to kind-aura-power-quality" >&2; exit 2; }

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
run_id="${RUN_ID:-cli$(date -u +%H%M%S)}"
fixture_namespace="ap-cli-${run_id}"
workload_name="cli-fixture"
base_url="http://127.0.0.1:19093"
cli_home="$(mktemp -d -t aura-power-cli-home.XXXXXX)"
policy_file="$(mktemp -t aura-power-cli-policy.XXXXXX.yaml)"
cli_bin="${AURA_POWER_CLI_BIN:-${repo_root}/bin/aura-power}"
forward_pid=""
namespace_uid=""
override_name=""
port_forward_log="$(mktemp -t aura-power-cli-port-forward.XXXXXX.log)"
negative_log="$(mktemp -t aura-power-cli-negative.XXXXXX.log)"
logged_out_log="$(mktemp -t aura-power-cli-logged-out.XXXXXX.log)"

cleanup() {
  local original_status=$? actual_uid actual_run cleanup_status=0
  trap - EXIT INT TERM HUP
  [[ -z "$override_name" ]] || kubectl delete poweroverride "$override_name" -n aura-system --ignore-not-found --wait=true --timeout=60s >/dev/null 2>&1 || cleanup_status=1
  if [[ -n "$namespace_uid" ]] && kubectl get namespace "$fixture_namespace" >/dev/null 2>&1; then
    actual_uid="$(kubectl get namespace "$fixture_namespace" -o jsonpath='{.metadata.uid}')"
    actual_run="$(kubectl get namespace "$fixture_namespace" -o json | jq -r '.metadata.labels["aura-power-quality/run"] // empty')"
    if [[ "$actual_uid" == "$namespace_uid" && "$actual_run" == "$run_id" ]]; then
      kubectl delete namespace "$fixture_namespace" --wait=true --timeout=180s >/dev/null 2>&1 || cleanup_status=1
    else
      echo "refusing cleanup: CLI fixture namespace ownership or UID mismatch" >&2
      cleanup_status=1
    fi
  fi
  kubectl delete powertarget -n aura-system -l "power.aura.sh/target-namespace=${fixture_namespace}" --ignore-not-found --wait=true --timeout=90s >/dev/null 2>&1 || cleanup_status=1
  if [[ -n "$forward_pid" ]]; then
    kill "$forward_pid" >/dev/null 2>&1 || true
    wait "$forward_pid" 2>/dev/null || true
  fi
  rm -rf "$cli_home" "$policy_file" "$port_forward_log" "$negative_log" "$logged_out_log"
  if kubectl get namespace "$fixture_namespace" >/dev/null 2>&1 ||
    kubectl get powertarget -n aura-system -l "power.aura.sh/target-namespace=${fixture_namespace}" -o name 2>/dev/null | grep -q .; then
    cleanup_status=1
  fi
  [[ "$cleanup_status" -eq 0 ]] || { echo "FATAL: CLI fixture cleanup was not verified" >&2; exit 90; }
  exit "$original_status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

[[ -x "$cli_bin" ]] || { echo "CLI binary not found at $cli_bin; run make build-cli" >&2; exit 3; }
if kubectl get namespace "$fixture_namespace" >/dev/null 2>&1; then
  echo "refusing mutation: CLI fixture namespace already exists" >&2
  exit 4
fi

namespace_json="$(kubectl create -o json -f - <<YAML
apiVersion: v1
kind: Namespace
metadata:
  name: ${fixture_namespace}
  labels:
    aura-power-quality/run: "${run_id}"
  annotations:
    aura.sh/power-eligible: "true"
YAML
)"
namespace_uid="$(jq -er .metadata.uid <<<"$namespace_json")"
kubectl create deployment "$workload_name" -n "$fixture_namespace" --image=registry.k8s.io/pause:3.10 --replicas=1 >/dev/null
kubectl annotate deployment "$workload_name" -n "$fixture_namespace" aura.sh/power-eligible=true >/dev/null

deadline=$((SECONDS + 180)); target_name=""
while (( SECONDS < deadline )); do
  target_name="$(kubectl get powertarget -n aura-system -l "power.aura.sh/target-namespace=${fixture_namespace},power.aura.sh/target-name=${workload_name},power.aura.sh/target-kind=Deployment" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  [[ -n "$target_name" ]] && break
  sleep 3
done
[[ -n "$target_name" ]] || { echo "FAIL: controller did not discover CLI fixture" >&2; exit 10; }
workload_uid="$(kubectl get deployment "$workload_name" -n "$fixture_namespace" -o jsonpath='{.metadata.uid}')"

kubectl port-forward -n aura-system statefulset/aura-power-server 19093:8080 >"$port_forward_log" 2>&1 &
forward_pid=$!
for _ in $(seq 1 30); do curl -fsS "$base_url/api/v1/health" >/dev/null 2>&1 && break; sleep 1; done
curl -fsS "$base_url/api/v1/health" >/dev/null

admin_password="$(kubectl get secret aura-power-server-secret -n aura-system -o jsonpath='{.data.admin-password}' | base64 -d)"
run_cli() { HOME="$cli_home" "$cli_bin" "$@"; }

# Exercise the root and every implemented command against the live server.
[[ "$(run_cli --help)" == *"Aura Power CLI"* ]]
[[ "$(run_cli override --help)" == *"create"* ]]
[[ "$(run_cli login --server "$base_url" --username admin --password "$admin_password")" == *"Logged in as admin"* ]]
[[ "$(run_cli whoami)" == *"Role:     admin"* ]]
run_cli discover --namespace "$fixture_namespace" --output json | jq -e '.totalWorkloads >= 1' >/dev/null
run_cli status --namespace "$fixture_namespace" --output json | jq -e --arg ns "$fixture_namespace" --arg uid "$workload_uid" '.targets[] | select(.spec.targetRef.namespace == $ns and .spec.targetRef.uid == $uid)' >/dev/null
run_cli explain "${fixture_namespace}/${workload_name}" --kind Deployment --uid "$workload_uid" --output json | jq -e --arg uid "$workload_uid" '.ref.uid == $uid' >/dev/null

cat >"$policy_file" <<YAML
apiVersion: power.aura.sh/v1alpha1
kind: PowerPolicy
metadata:
  name: cli-preview-${run_id}
  namespace: aura-system
spec:
  scope:
    targetRefs:
      - apiVersion: apps/v1
        kind: Deployment
        namespace: ${fixture_namespace}
        name: ${workload_name}
        uid: ${workload_uid}
  schedule:
    desiredState: "on"
    windows: []
  priority: 1000
YAML
run_cli preview --file "$policy_file" --output json | jq -e '.totalAffected == 1' >/dev/null
override_output="$(run_cli override create --target "${fixture_namespace}/${workload_name}" --kind Deployment --uid "$workload_uid" --state on --duration 15m --reason 'CLI real-server acceptance' --reference "quality-${run_id}")"
override_name="$(awk '/^Override created:/ {print $3}' <<<"$override_output")"
[[ -n "$override_name" ]] || { echo "FAIL: override command returned no resource name" >&2; exit 11; }
kubectl get poweroverride "$override_name" -n aura-system -o json | jq -e --arg uid "$workload_uid" --arg ref "quality-${run_id}" '.spec.scope.targetRefs[0].uid == $uid and .spec.reference == $ref' >/dev/null
state_status='{"targets":[],"count":0}'
for _ in $(seq 1 40); do
  state_status="$(run_cli status --namespace "$fixture_namespace" --state on --output json)"
  if jq -e --arg ns "$fixture_namespace" --arg uid "$workload_uid" '
    .count >= 1 and
    any(.targets[]; .spec.targetRef.namespace == $ns and .spec.targetRef.uid == $uid and .status.desiredState == "on") and
    all(.targets[]; .status.desiredState == "on")
  ' <<<"$state_status" >/dev/null; then
    break
  fi
  sleep 3
done
jq -e --arg ns "$fixture_namespace" --arg uid "$workload_uid" '
  .count >= 1 and
  any(.targets[]; .spec.targetRef.namespace == $ns and .spec.targetRef.uid == $uid and .status.desiredState == "on") and
  all(.targets[]; .status.desiredState == "on")
' <<<"$state_status" >/dev/null
run_cli savings --namespace "$fixture_namespace" --period 7d --output json | jq -e 'has("totalCPUHours") and has("totalMemoryGiB") and has("totalEstimatedCost")' >/dev/null

# Exit semantics are part of the public CLI contract.
if run_cli explain "${fixture_namespace}/${workload_name}" --kind Invalid >"$negative_log" 2>&1; then
  echo "FAIL: invalid workload kind returned success" >&2
  exit 12
fi
[[ "$(run_cli logout)" == *"Logged out"* ]]
if run_cli whoami >"$logged_out_log" 2>&1; then
  echo "FAIL: authenticated command succeeded after logout" >&2
  exit 13
fi

echo "kind_cli_journey=passed root=true login=true logout=true whoami=true discover=true status=true explain=true preview_yaml=true override_create=true savings=true errors_nonzero=true cleanup=armed"
