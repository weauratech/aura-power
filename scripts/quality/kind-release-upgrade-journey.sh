#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

LEGACY_REF="${LEGACY_REF:-v2.1.7}"
CLUSTER_NAME="${CLUSTER_NAME:-aura-power-release-upgrade}"
NODE_IMAGE="${NODE_IMAGE:-kindest/node:v1.35.0}"
CANDIDATE_SERVER_IMAGE="${CANDIDATE_SERVER_IMAGE:-aura-power-server:ci}"
CANDIDATE_CONTROLLER_IMAGE="${CANDIDATE_CONTROLLER_IMAGE:-aura-power-controller:ci}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-300}"
KIND_BIN="${KIND_BIN:-kind}"

[[ "$CLUSTER_NAME" == "aura-power-release-upgrade" ]] || { echo "CLUSTER_NAME must be aura-power-release-upgrade" >&2; exit 2; }
[[ "$TIMEOUT_SECONDS" =~ ^[0-9]+$ && "$TIMEOUT_SECONDS" -le 600 ]] || { echo "TIMEOUT_SECONDS must be an integer <= 600" >&2; exit 2; }
command -v "$KIND_BIN" >/dev/null || { echo "kind binary not found: $KIND_BIN" >&2; exit 2; }
command -v docker >/dev/null || { echo "docker is required" >&2; exit 2; }
command -v helm >/dev/null || { echo "helm is required" >&2; exit 2; }
command -v kubectl >/dev/null || { echo "kubectl is required" >&2; exit 2; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }
git rev-parse --verify "${LEGACY_REF}^{commit}" >/dev/null
docker image inspect "$CANDIDATE_SERVER_IMAGE" >/dev/null
docker image inspect "$CANDIDATE_CONTROLLER_IMAGE" >/dev/null

legacy_dir="$(mktemp -d -t aura-power-v217.XXXXXX)"
kubeconfig="$(mktemp -t aura-power-release-upgrade.XXXXXX.kubeconfig)"
port_forward_log="$(mktemp -t aura-power-release-upgrade-port-forward.XXXXXX.log)"
legacy_server_image="aura-power-server:upgrade-v217"
legacy_controller_image="aura-power-controller:upgrade-v217"
fixture_namespace="ap-release-upgrade"
workload_name="upgrade-fixture"
policy_name="release-upgrade-policy"
forward_pid=""
cluster_owned=false

cleanup() {
  local original_status=$?
  trap - EXIT INT TERM HUP
  if [[ -n "$forward_pid" ]]; then
    kill "$forward_pid" >/dev/null 2>&1 || true
    wait "$forward_pid" 2>/dev/null || true
  fi
  if [[ "$cluster_owned" == true ]] && "$KIND_BIN" get clusters 2>/dev/null | grep -Fxq "$CLUSTER_NAME"; then
    "$KIND_BIN" delete cluster --name "$CLUSTER_NAME" >/dev/null 2>&1 || {
      echo "FATAL: failed to delete isolated upgrade cluster" >&2
      original_status=90
    }
  fi
  rm -rf "$legacy_dir" "$kubeconfig" "$port_forward_log"
  docker image rm "$legacy_server_image" "$legacy_controller_image" >/dev/null 2>&1 || true
  exit "$original_status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

if "$KIND_BIN" get clusters | grep -Fxq "$CLUSTER_NAME"; then
  echo "refusing mutation: isolated upgrade cluster already exists" >&2
  exit 3
fi

git archive "$LEGACY_REF" | tar -x -C "$legacy_dir"
docker build -f "$legacy_dir/Dockerfile.server" -t "$legacy_server_image" "$legacy_dir"
docker build -f "$legacy_dir/Dockerfile.controller" -t "$legacy_controller_image" "$legacy_dir"

cluster_owned=true
"$KIND_BIN" create cluster --name "$CLUSTER_NAME" --image "$NODE_IMAGE" --kubeconfig "$kubeconfig" --wait 180s
export KUBECONFIG="$kubeconfig"
[[ "$(kubectl config current-context)" == "kind-${CLUSTER_NAME}" ]] || { echo "FAIL: unexpected Kubernetes context" >&2; exit 4; }

"$KIND_BIN" load docker-image --name "$CLUSTER_NAME" \
  "$legacy_server_image" "$legacy_controller_image" \
  "$CANDIDATE_SERVER_IMAGE" "$CANDIDATE_CONTROLLER_IMAGE"

helm upgrade --install aura-power "$legacy_dir/charts/aura-power" \
  --namespace aura-system --create-namespace --wait --timeout 8m \
  --set server.image.repository=aura-power-server \
  --set server.image.tag=upgrade-v217 \
  --set server.image.pullPolicy=Never \
  --set-string server.auth.initialAdmin.password='UpgradeAdmin-217!' \
  --set controller.image.repository=aura-power-controller \
  --set controller.image.tag=upgrade-v217 \
  --set controller.image.pullPolicy=Never

[[ "$(kubectl get statefulset aura-power-server -n aura-system -o jsonpath='{.status.readyReplicas}')" == "1" ]] || { echo "FAIL: legacy server is not ready" >&2; exit 10; }
[[ "$(kubectl get deployment aura-power-controller -n aura-system -o jsonpath='{.status.readyReplicas}')" == "1" ]] || { echo "FAIL: legacy controller is not ready" >&2; exit 11; }
[[ "$(kubectl get statefulset aura-power-server -n aura-system -o jsonpath='{.spec.template.spec.containers[0].image}')" == "$legacy_server_image" ]]
[[ "$(kubectl get deployment aura-power-controller -n aura-system -o jsonpath='{.spec.template.spec.containers[0].image}')" == "$legacy_controller_image" ]]

auth_before="$(kubectl get secret aura-power-server-secret -n aura-system -o json | jq -r '.data["jwt-secret"] + ":" + .data["admin-password"]')"
pvc_uid_before="$(kubectl get pvc data-aura-power-server-0 -n aura-system -o jsonpath='{.metadata.uid}')"
[[ -n "$auth_before" && -n "$pvc_uid_before" ]] || { echo "FAIL: legacy auth Secret or persistent volume is missing" >&2; exit 12; }
if [[ -n "$(kubectl get crd powertargets.power.aura.sh -o jsonpath='{.spec.versions[?(@.name=="v1alpha1")].schema.openAPIV3Schema.properties.spec.properties.targetRef.properties.uid.type}')" ]]; then
  echo "FAIL: legacy PowerTarget CRD unexpectedly contains UID" >&2
  exit 13
fi

kubectl port-forward -n aura-system statefulset/aura-power-server 19094:8080 >"$port_forward_log" 2>&1 &
forward_pid=$!
for _ in $(seq 1 45); do curl -fsS http://127.0.0.1:19094/readyz >/dev/null 2>&1 && break; sleep 1; done
legacy_login="$(curl -fsS -H 'content-type: application/json' \
  --data '{"username":"admin","password":"UpgradeAdmin-217!"}' \
  http://127.0.0.1:19094/api/v1/auth/login)"
refresh_token="$(jq -er .refreshToken <<<"$legacy_login")"
[[ -n "$(jq -er .accessToken <<<"$legacy_login")" && -n "$refresh_token" ]] || { echo "FAIL: legacy login returned no tokens" >&2; exit 14; }

kubectl create namespace "$fixture_namespace" >/dev/null
kubectl annotate namespace "$fixture_namespace" aura.sh/power-eligible=true >/dev/null
kubectl create deployment "$workload_name" -n "$fixture_namespace" --image=registry.k8s.io/pause:3.10 --replicas=3 >/dev/null
kubectl annotate deployment "$workload_name" -n "$fixture_namespace" aura.sh/power-eligible=true >/dev/null

deadline=$((SECONDS + TIMEOUT_SECONDS)); legacy_target=""
while (( SECONDS < deadline )); do
  legacy_target="$(kubectl get powertarget -n aura-system -l "power.aura.sh/target-namespace=${fixture_namespace},power.aura.sh/target-name=${workload_name},power.aura.sh/target-kind=Deployment" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  [[ -n "$legacy_target" ]] && break
  sleep 3
done
[[ -n "$legacy_target" ]] || { echo "FAIL: v2.1.7 did not discover the fixture" >&2; exit 15; }
[[ -z "$(kubectl get powertarget "$legacy_target" -n aura-system -o jsonpath='{.spec.targetRef.uid}')" ]] || { echo "FAIL: legacy target unexpectedly has a UID" >&2; exit 16; }

# Helm does not upgrade CRDs. Applying the candidate definitions first is the
# explicit, documented migration step before the workloads start new binaries.
kubectl apply --server-side --force-conflicts -f charts/aura-power/crds >/dev/null
helm upgrade aura-power charts/aura-power \
  --namespace aura-system --wait --timeout 8m --reset-then-reuse-values \
  --set server.image.repository="${CANDIDATE_SERVER_IMAGE%:*}" \
  --set server.image.tag="${CANDIDATE_SERVER_IMAGE##*:}" \
  --set server.image.pullPolicy=Never \
  --set controller.image.repository="${CANDIDATE_CONTROLLER_IMAGE%:*}" \
  --set controller.image.tag="${CANDIDATE_CONTROLLER_IMAGE##*:}" \
  --set controller.image.pullPolicy=Never

[[ "$(kubectl get statefulset aura-power-server -n aura-system -o jsonpath='{.spec.template.spec.containers[0].image}')" == "$CANDIDATE_SERVER_IMAGE" ]] || { echo "FAIL: server did not upgrade to candidate" >&2; exit 20; }
[[ "$(kubectl get deployment aura-power-controller -n aura-system -o jsonpath='{.spec.template.spec.containers[0].image}')" == "$CANDIDATE_CONTROLLER_IMAGE" ]] || { echo "FAIL: controller did not upgrade to candidate" >&2; exit 21; }
auth_after="$(kubectl get secret aura-power-server-secret -n aura-system -o json | jq -r '.data["jwt-secret"] + ":" + .data["admin-password"]')"
pvc_uid_after="$(kubectl get pvc data-aura-power-server-0 -n aura-system -o jsonpath='{.metadata.uid}')"
[[ "$auth_before" == "$auth_after" ]] || { echo "FAIL: auth Secret rotated during release upgrade" >&2; exit 22; }
[[ "$pvc_uid_before" == "$pvc_uid_after" ]] || { echo "FAIL: server persistent volume was replaced" >&2; exit 23; }
[[ "$(kubectl get crd powertargets.power.aura.sh -o jsonpath='{.spec.versions[?(@.name=="v1alpha1")].schema.openAPIV3Schema.properties.spec.properties.targetRef.properties.uid.type}')" == "string" ]] || { echo "FAIL: candidate PowerTarget UID schema is absent" >&2; exit 24; }
kubectl get crd powertargets.power.aura.sh -o json | jq -e '.spec.versions[] | select(.name == "v1alpha1") | .subresources.status == {}' >/dev/null || { echo "FAIL: PowerTarget status subresource is absent" >&2; exit 25; }

kill "$forward_pid" >/dev/null 2>&1 || true
wait "$forward_pid" 2>/dev/null || true
forward_pid=""
kubectl port-forward -n aura-system statefulset/aura-power-server 19094:8080 >"$port_forward_log" 2>&1 &
forward_pid=$!
for _ in $(seq 1 45); do curl -fsS http://127.0.0.1:19094/readyz >/dev/null 2>&1 && break; sleep 1; done
refresh_result="$(curl -fsS -H 'content-type: application/json' \
  --data "$(jq -nc --arg token "$refresh_token" '{refreshToken:$token}')" \
  http://127.0.0.1:19094/api/v1/auth/refresh)"
[[ -n "$(jq -er .accessToken <<<"$refresh_result")" ]] || { echo "FAIL: pre-upgrade refresh token was not accepted" >&2; exit 26; }

tls_before="$(kubectl get secret aura-power-controller-webhook-tls -n aura-system -o json | jq -r '.data["tls.crt"] + ":" + .data["tls.key"]')"
[[ -n "$tls_before" ]] || { echo "FAIL: candidate webhook TLS Secret is missing" >&2; exit 27; }
helm upgrade aura-power charts/aura-power --namespace aura-system --reuse-values --wait --timeout 8m >/dev/null
tls_after="$(kubectl get secret aura-power-controller-webhook-tls -n aura-system -o json | jq -r '.data["tls.crt"] + ":" + .data["tls.key"]')"
[[ "$tls_before" == "$tls_after" ]] || { echo "FAIL: webhook TLS rotated on repeated candidate upgrade" >&2; exit 28; }

workload_uid="$(kubectl get deployment "$workload_name" -n "$fixture_namespace" -o jsonpath='{.metadata.uid}')"
deadline=$((SECONDS + TIMEOUT_SECONDS)); migrated_target=""
while (( SECONDS < deadline )); do
  migrated_target="$(kubectl get powertarget -n aura-system -l "power.aura.sh/target-namespace=${fixture_namespace},power.aura.sh/target-name=${workload_name},power.aura.sh/target-kind=Deployment" -o json | jq -r --arg uid "$workload_uid" '.items[] | select(.spec.targetRef.uid == $uid) | .metadata.name' | head -1)"
  [[ -n "$migrated_target" ]] && break
  sleep 3
done
[[ -n "$migrated_target" ]] || { echo "FAIL: candidate did not migrate the target to UID identity" >&2; exit 29; }
matching_targets="$(kubectl get powertarget -n aura-system -l "power.aura.sh/target-namespace=${fixture_namespace},power.aura.sh/target-name=${workload_name},power.aura.sh/target-kind=Deployment" -o json)"
[[ "$(jq '.items | length' <<<"$matching_targets")" == "1" ]] || { echo "FAIL: migration left duplicate workload identities" >&2; exit 29; }
[[ "$(jq -r '.items[0].metadata.name' <<<"$matching_targets")" != "$legacy_target" ]] || { echo "FAIL: migration retained the legacy target name" >&2; exit 29; }
if kubectl get powertarget "$legacy_target" -n aura-system >/dev/null 2>&1; then
  echo "FAIL: legacy PowerTarget still exists after UID migration" >&2
  exit 29
fi

kubectl apply -f - >/dev/null <<YAML
apiVersion: power.aura.sh/v1alpha1
kind: PowerPolicy
metadata:
  name: ${policy_name}
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
    desiredState: "off"
    windows: []
  priority: 1000
YAML

deadline=$((SECONDS + TIMEOUT_SECONDS)); replicas=""
while (( SECONDS < deadline )); do
  replicas="$(kubectl get deployment "$workload_name" -n "$fixture_namespace" -o jsonpath='{.spec.replicas}')"
  snapshot="$(kubectl get powertarget "$migrated_target" -n aura-system -o jsonpath='{.status.snapshot.replicaCount}' 2>/dev/null || true)"
  action_phase="$(kubectl get powertarget "$migrated_target" -n aura-system -o jsonpath='{.status.action.phase}' 2>/dev/null || true)"
  [[ "$replicas" == "0" && "$snapshot" == "3" && "$action_phase" == "Converged" ]] && break
  sleep 3
done
[[ "$replicas" == "0" && "$snapshot" == "3" && "$action_phase" == "Converged" ]] || { echo "FAIL: candidate did not power off with a durable replica snapshot" >&2; exit 30; }

kubectl patch powerpolicy "$policy_name" -n aura-system --type=merge -p '{"spec":{"schedule":{"desiredState":"on","windows":[]}}}' >/dev/null
deadline=$((SECONDS + TIMEOUT_SECONDS)); replicas=""
while (( SECONDS < deadline )); do
  replicas="$(kubectl get deployment "$workload_name" -n "$fixture_namespace" -o jsonpath='{.spec.replicas}')"
  snapshot_available="$(kubectl get powertarget "$migrated_target" -n aura-system -o jsonpath='{.status.snapshot.available}' 2>/dev/null || true)"
  action_phase="$(kubectl get powertarget "$migrated_target" -n aura-system -o jsonpath='{.status.action.phase}' 2>/dev/null || true)"
  [[ "$replicas" == "3" && "$snapshot_available" != "true" && "$action_phase" == "Converged" ]] && break
  sleep 3
done
[[ "$replicas" == "3" && "$snapshot_available" != "true" && "$action_phase" == "Converged" ]] || { echo "FAIL: candidate did not restore replicas or clear the snapshot after observation" >&2; exit 31; }

audit_count="$(kubectl get powerauditevent -n aura-system -o json | jq --arg ns "$fixture_namespace" --arg name "$workload_name" '[.items[] | select(.spec.target.namespace == $ns and .spec.target.name == $name)] | length')"
[[ "$audit_count" -ge 2 ]] || { echo "FAIL: expected durable off/on audit events, observed $audit_count" >&2; exit 32; }

kubectl delete namespace "$fixture_namespace" --wait=true --timeout=180s >/dev/null
kubectl delete powertarget -n aura-system -l "power.aura.sh/target-namespace=${fixture_namespace}" --wait=true --timeout=120s >/dev/null 2>&1 || true
[[ -z "$(kubectl get powertarget -n aura-system -l "power.aura.sh/target-namespace=${fixture_namespace}" -o name)" ]] || { echo "FAIL: fixture PowerTarget cleanup was incomplete" >&2; exit 33; }

echo "kind_release_upgrade_journey=passed from=${LEGACY_REF} old_chart=true old_server=true old_controller=true crds_updated=true status_subresource=true auth_secret_stable=true refresh_token_survived=true pvc_stable=true webhook_tls_stable=true uid_migrated=true power_off=true snapshot=3 restore=3 audit_events=${audit_count} cleanup=true"
