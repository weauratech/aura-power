#!/usr/bin/env bash
set -Eeuo pipefail

: "${KUBECONFIG:?set KUBECONFIG to the disposable Kind kubeconfig}"
[[ "$(kubectl config current-context)" == "kind-aura-power-quality" ]] || {
  echo "refusing mutation: this runner is restricted to kind-aura-power-quality" >&2
  exit 2
}

RUN_ID="${RUN_ID:-isolation$(date -u +%Y%m%d%H%M%S)}"
CONTROL_NAMESPACE="${CONTROL_NAMESPACE:-aura-system}"
FOREIGN_NAMESPACE="aura-power-foreign-${RUN_ID}"
FIXTURE_NAMESPACE="aura-power-isolation-${RUN_ID}"
WORKLOAD_NAME="foreign-policy-must-not-act"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-75}"
[[ "$TIMEOUT_SECONDS" =~ ^[0-9]+$ && "$TIMEOUT_SECONDS" -ge 30 && "$TIMEOUT_SECONDS" -le 180 ]] || {
  echo "TIMEOUT_SECONDS must be an integer between 30 and 180" >&2
  exit 2
}

fixture_uid=""
foreign_uid=""
forward_pid=""
controller_deployment=""
controller_replicas=""
cleanup() {
  local original_status=$? cleanup_status=0 actual_uid actual_run
  trap - EXIT INT TERM HUP
  if [[ -n "$forward_pid" ]]; then
    kill "$forward_pid" >/dev/null 2>&1 || true
    wait "$forward_pid" 2>/dev/null || true
  fi
  if [[ -n "$controller_deployment" && -n "$controller_replicas" ]]; then
    kubectl scale deployment "$controller_deployment" -n "$CONTROL_NAMESPACE" --replicas="$controller_replicas" >/dev/null 2>&1 || cleanup_status=1
    kubectl rollout status deployment "$controller_deployment" -n "$CONTROL_NAMESPACE" --timeout=180s >/dev/null 2>&1 || cleanup_status=1
  fi
  for ns_var in FIXTURE_NAMESPACE FOREIGN_NAMESPACE; do
    local ns="${!ns_var}" expected_uid
    [[ "$ns_var" == "FIXTURE_NAMESPACE" ]] && expected_uid="$fixture_uid" || expected_uid="$foreign_uid"
    if [[ -n "$expected_uid" ]] && kubectl get namespace "$ns" >/dev/null 2>&1; then
      actual_uid="$(kubectl get namespace "$ns" -o jsonpath='{.metadata.uid}')"
      actual_run="$(kubectl get namespace "$ns" -o json | jq -r '.metadata.labels["aura-power-quality/run"] // empty')"
      if [[ "$actual_uid" == "$expected_uid" && "$actual_run" == "$RUN_ID" ]]; then
        kubectl delete namespace "$ns" --wait=true --timeout=180s >/dev/null || cleanup_status=1
      else
        echo "refusing cleanup: namespace $ns ownership or UID mismatch" >&2
        cleanup_status=1
      fi
    fi
  done
  [[ "$cleanup_status" -eq 0 ]] || { echo "FATAL: isolation fixture cleanup was not verified" >&2; exit 90; }
  exit "$original_status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

for ns in "$FOREIGN_NAMESPACE" "$FIXTURE_NAMESPACE"; do
  kubectl get namespace "$ns" >/dev/null 2>&1 && {
    echo "refusing mutation: fixture namespace already exists: $ns" >&2
    exit 4
  }
done

foreign_json="$(kubectl create -o json -f - <<YAML
apiVersion: v1
kind: Namespace
metadata:
  name: ${FOREIGN_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
YAML
)"
foreign_uid="$(jq -er .metadata.uid <<<"$foreign_json")"

fixture_json="$(kubectl create -o json -f - <<YAML
apiVersion: v1
kind: Namespace
metadata:
  name: ${FIXTURE_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
YAML
)"
fixture_uid="$(jq -er .metadata.uid <<<"$fixture_json")"

controller_sa="$(kubectl get deployment -n "$CONTROL_NAMESPACE" -l app.kubernetes.io/component=controller -o jsonpath='{.items[0].spec.template.spec.serviceAccountName}')"
controller_deployment="$(kubectl get deployment -n "$CONTROL_NAMESPACE" -l app.kubernetes.io/component=controller -o jsonpath='{.items[0].metadata.name}')"
controller_replicas="$(kubectl get deployment "$controller_deployment" -n "$CONTROL_NAMESPACE" -o jsonpath='{.spec.replicas}')"
server_sa="$(kubectl get statefulset -n "$CONTROL_NAMESPACE" -l app.kubernetes.io/component=server -o jsonpath='{.items[0].spec.template.spec.serviceAccountName}')"
for sa in "$controller_sa" "$server_sa"; do
  [[ "$(kubectl auth can-i --as="system:serviceaccount:${CONTROL_NAMESPACE}:${sa}" list powerpolicies.power.aura.sh -n "$FOREIGN_NAMESPACE")" == "no" ]] || {
    echo "FAIL: service account $sa can list foreign PowerPolicies" >&2
    exit 5
  }
done

kubectl create -f - <<YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${WORKLOAD_NAME}
  namespace: ${FIXTURE_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
spec:
  replicas: 1
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

# The HTTP API must reject the same cross-namespace write instead of reporting
# success for an object that the controller will deliberately ignore.
kubectl port-forward -n "$CONTROL_NAMESPACE" statefulset/aura-power-server 19096:8080 >/tmp/aura-power-isolation-port-forward.log 2>&1 &
forward_pid=$!
for _ in $(seq 1 30); do curl -fsS http://127.0.0.1:19096/readyz >/dev/null 2>&1 && break; sleep 1; done
admin_password="$(kubectl get secret aura-power-server-secret -n "$CONTROL_NAMESPACE" -o jsonpath='{.data.admin-password}' | base64 -d)"
access_token="$(curl -fsS -H 'content-type: application/json' --data "$(jq -nc --arg password "$admin_password" '{username:"admin",password:$password}')" http://127.0.0.1:19096/api/v1/auth/login | jq -er .accessToken)"
http_code="$(curl -sS -o /tmp/aura-power-isolation-api-response.json -w '%{http_code}' \
  -H "authorization: Bearer $access_token" -H 'content-type: application/json' \
  --data "$(jq -nc --arg namespace "$FOREIGN_NAMESPACE" '{apiVersion:"power.aura.sh/v1alpha1",kind:"PowerPolicy",metadata:{name:"api-foreign",namespace:$namespace},spec:{scope:{namespaces:["never"]},schedule:{desiredState:"off",windows:[]},priority:1}}')" \
  http://127.0.0.1:19096/api/v1/policies)"
unset admin_password access_token
[[ "$http_code" == "422" ]] || { echo "FAIL: API accepted foreign policy with HTTP $http_code" >&2; exit 8; }
kubectl get powerpolicy api-foreign -n "$FOREIGN_NAMESPACE" >/dev/null 2>&1 && { echo "FAIL: rejected API policy was persisted" >&2; exit 8; }
kubectl rollout status deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" --timeout=120s
workload_uid="$(kubectl get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.metadata.uid}')"

kubectl create -f - <<YAML
apiVersion: power.aura.sh/v1alpha1
kind: PowerTarget
metadata:
  name: foreign-${RUN_ID}
  namespace: ${FOREIGN_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
spec:
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    namespace: ${FIXTURE_NAMESPACE}
    name: ${WORKLOAD_NAME}
    uid: "${workload_uid}"
---
apiVersion: power.aura.sh/v1alpha1
kind: PowerPolicy
metadata:
  name: foreign-${RUN_ID}
  namespace: ${FOREIGN_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
spec:
  scope:
    targetRefs:
      - apiVersion: apps/v1
        kind: Deployment
        namespace: ${FIXTURE_NAMESPACE}
        name: ${WORKLOAD_NAME}
        uid: "${workload_uid}"
  schedule:
    desiredState: "off"
    windows: []
  priority: 1000
---
apiVersion: power.aura.sh/v1alpha1
kind: PowerOverride
metadata:
  name: foreign-${RUN_ID}
  namespace: ${FOREIGN_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
spec:
  scope:
    targetRefs:
      - apiVersion: apps/v1
        kind: Deployment
        namespace: ${FIXTURE_NAMESPACE}
        name: ${WORKLOAD_NAME}
        uid: "${workload_uid}"
  state: "off"
  priority: 1000
  expiresAt: "$(date -u -v+1H +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '+1 hour' +%Y-%m-%dT%H:%M:%SZ)"
  reason: "control namespace isolation acceptance"
YAML

# Cover more than one controller sync period and continuously verify negative
# evidence. An object outside the configured control namespace must be inert.
deadline=$((SECONDS + TIMEOUT_SECONDS))
while (( SECONDS < deadline )); do
  replicas="$(kubectl get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.spec.replicas}')"
  [[ "$replicas" == "1" ]] || {
    echo "FAIL: a foreign control object changed the fixture replicas to $replicas" >&2
    exit 9
  }
  target_status="$(kubectl get powertarget "foreign-${RUN_ID}" -n "$FOREIGN_NAMESPACE" -o json | jq -c '.status // {}')"
  override_status="$(kubectl get poweroverride "foreign-${RUN_ID}" -n "$FOREIGN_NAMESPACE" -o json | jq -c '.status // {}')"
  [[ "$target_status" == "{}" ]] || { echo "FAIL: foreign target status was reconciled: $target_status" >&2; exit 10; }
  [[ "$override_status" == "{}" ]] || { echo "FAIL: foreign override status was reconciled: $override_status" >&2; exit 11; }
  sleep 3
done

[[ "$(kubectl get deployment "$WORKLOAD_NAME" -n "$FIXTURE_NAMESPACE" -o jsonpath='{.status.readyReplicas}')" == "1" ]] || {
  echo "FAIL: isolation fixture is not ready after observation" >&2
  exit 12
}

# A failed webhook instance must not become a cluster-wide failure domain for
# another release namespace. The selector is exercised by taking this release's
# controller endpoint away and creating a valid CR in the foreign namespace.
kubectl scale deployment "$controller_deployment" -n "$CONTROL_NAMESPACE" --replicas=0 >/dev/null
kubectl wait --for=delete pod -n "$CONTROL_NAMESPACE" -l app.kubernetes.io/component=controller --timeout=120s >/dev/null
kubectl create --request-timeout=10s -f - <<YAML >/dev/null
apiVersion: power.aura.sh/v1alpha1
kind: PowerPolicy
metadata:
  name: webhook-foreign-${RUN_ID}
  namespace: ${FOREIGN_NAMESPACE}
  labels:
    aura-power-quality/run: "${RUN_ID}"
spec:
  scope:
    namespaces: ["does-not-exist"]
  schedule:
    desiredState: "on"
    windows: []
  priority: 1
YAML
kubectl delete powerpolicy "webhook-foreign-${RUN_ID}" -n "$FOREIGN_NAMESPACE" --wait=true --timeout=30s >/dev/null
kubectl scale deployment "$controller_deployment" -n "$CONTROL_NAMESPACE" --replicas="$controller_replicas" >/dev/null
kubectl rollout status deployment "$controller_deployment" -n "$CONTROL_NAMESPACE" --timeout=180s >/dev/null

echo "control_namespace_isolation=passed control=${CONTROL_NAMESPACE} foreign=${FOREIGN_NAMESPACE} rbac_denied=true api_rejected=true webhook_failure_domain=scoped observation_seconds=${TIMEOUT_SECONDS}"
