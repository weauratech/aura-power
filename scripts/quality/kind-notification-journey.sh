#!/usr/bin/env bash
set -Eeuo pipefail

: "${KUBECONFIG:?set KUBECONFIG to the disposable Kind kubeconfig}"
[[ "$(kubectl config current-context)" == "kind-aura-power-quality" ]] || { echo "refusing mutation: this runner is restricted to kind-aura-power-quality" >&2; exit 2; }

run_id="${RUN_ID:-notify$(date -u +%H%M%S)}"
[[ "$run_id" =~ ^[a-z0-9-]+$ ]] || { echo "RUN_ID must contain only lowercase letters, digits, and hyphens" >&2; exit 2; }
fixture_namespace="ap-notify-${run_id}"
workload_name="notification-fixture"
policy_name="notification-${run_id}"
channel_name="notification-${run_id}"
secret_name="notification-url-${run_id}"
receiver_name="notification-receiver"
receiver_image="${RECEIVER_IMAGE:-python:3.12-alpine@sha256:b64631e04e4920160c50fbe8d8df828f7f35f06f425cb44aa09bca53e708a35a}"
timeout_seconds="${TIMEOUT_SECONDS:-180}"
[[ "$timeout_seconds" =~ ^[0-9]+$ && "$timeout_seconds" -le 300 ]] || { echo "TIMEOUT_SECONDS must be an integer <= 300" >&2; exit 2; }

namespace_uid=""
workload_uid=""
webhook_token="$(openssl rand -hex 24)"
webhook_url="http://${receiver_name}.${fixture_namespace}.svc.cluster.local:8080/hook/${webhook_token}"

owned_control_resource() {
  local resource="$1" name="$2"
  [[ "$(kubectl get "$resource" "$name" -n aura-system -o json | jq -r '.metadata.labels["aura-power-quality/run"] // empty')" == "$run_id" ]]
}

cleanup() {
  local original_status=$? cleanup_status=0 actual_uid actual_run audit_name target_name delivery_cleanup_deadline
  trap - EXIT INT TERM HUP

  if kubectl get powernotificationchannel "$channel_name" -n aura-system >/dev/null 2>&1; then
    if owned_control_resource powernotificationchannel "$channel_name"; then
      kubectl patch powernotificationchannel "$channel_name" -n aura-system --type=merge -p '{"spec":{"enabled":false}}' >/dev/null 2>&1 || cleanup_status=1
      kubectl delete powernotificationchannel "$channel_name" -n aura-system --wait=true --timeout=60s >/dev/null 2>&1 || cleanup_status=1
    else
      echo "refusing cleanup: notification channel ownership mismatch" >&2
      cleanup_status=1
    fi
  fi
  if kubectl get powerpolicy "$policy_name" -n aura-system >/dev/null 2>&1; then
    if owned_control_resource powerpolicy "$policy_name"; then
      kubectl delete powerpolicy "$policy_name" -n aura-system --wait=true --timeout=60s >/dev/null 2>&1 || cleanup_status=1
    else
      echo "refusing cleanup: notification policy ownership mismatch" >&2
      cleanup_status=1
    fi
  fi
  if kubectl get secret "$secret_name" -n aura-system >/dev/null 2>&1; then
    if owned_control_resource secret "$secret_name"; then
      kubectl delete secret "$secret_name" -n aura-system --wait=true --timeout=60s >/dev/null 2>&1 || cleanup_status=1
    else
      echo "refusing cleanup: notification Secret ownership mismatch" >&2
      cleanup_status=1
    fi
  fi

  if [[ -n "$workload_uid" ]]; then
    while IFS= read -r audit_name; do
      [[ -z "$audit_name" ]] && continue
      if kubectl get powerauditevent "$audit_name" -n aura-system -o json | jq -e --arg ns "$fixture_namespace" --arg uid "$workload_uid" \
        '.spec.target.namespace == $ns and .spec.target.uid == $uid' >/dev/null; then
        kubectl delete powerauditevent "$audit_name" -n aura-system --wait=true --timeout=60s >/dev/null 2>&1 || cleanup_status=1
      else
        echo "refusing cleanup: audit event identity mismatch" >&2
        cleanup_status=1
      fi
    done < <(kubectl get powerauditevent -n aura-system -l "power.aura.sh/target-namespace=${fixture_namespace},power.aura.sh/target-uid=${workload_uid}" -o name 2>/dev/null | sed 's#.*/##')

    while IFS= read -r target_name; do
      [[ -z "$target_name" ]] && continue
      if kubectl get powertarget "$target_name" -n aura-system -o json | jq -e --arg ns "$fixture_namespace" --arg uid "$workload_uid" \
        '.spec.targetRef.namespace == $ns and .spec.targetRef.uid == $uid' >/dev/null; then
        kubectl delete powertarget "$target_name" -n aura-system --wait=true --timeout=60s >/dev/null 2>&1 || cleanup_status=1
      else
        echo "refusing cleanup: PowerTarget identity mismatch" >&2
        cleanup_status=1
      fi
    done < <(kubectl get powertarget -n aura-system -l "power.aura.sh/target-namespace=${fixture_namespace},power.aura.sh/target-uid=${workload_uid}" -o name 2>/dev/null | sed 's#.*/##')
  fi

  if [[ -n "$namespace_uid" ]] && kubectl get namespace "$fixture_namespace" >/dev/null 2>&1; then
    actual_uid="$(kubectl get namespace "$fixture_namespace" -o jsonpath='{.metadata.uid}')"
    actual_run="$(kubectl get namespace "$fixture_namespace" -o json | jq -r '.metadata.labels["aura-power-quality/run"] // empty')"
    if [[ "$actual_uid" == "$namespace_uid" && "$actual_run" == "$run_id" ]]; then
      kubectl scale deployment "$workload_name" -n "$fixture_namespace" --replicas=1 >/dev/null 2>&1 || true
      kubectl delete namespace "$fixture_namespace" --wait=true --timeout=180s >/dev/null 2>&1 || cleanup_status=1
    else
      echo "refusing cleanup: fixture namespace ownership or UID mismatch" >&2
      cleanup_status=1
    fi
  fi

  delivery_cleanup_deadline=$((SECONDS + 60))
  while (( SECONDS < delivery_cleanup_deadline )); do
    if ! kubectl get namespace "$fixture_namespace" >/dev/null 2>&1 &&
      ! kubectl get powernotificationchannel "$channel_name" -n aura-system >/dev/null 2>&1 &&
      ! kubectl get powerpolicy "$policy_name" -n aura-system >/dev/null 2>&1 &&
      ! kubectl get secret "$secret_name" -n aura-system >/dev/null 2>&1 &&
      ! kubectl get powertarget -n aura-system -l "power.aura.sh/target-namespace=${fixture_namespace}" -o name 2>/dev/null | grep -q . &&
      ! kubectl get powerauditevent -n aura-system -l "power.aura.sh/target-namespace=${fixture_namespace}" -o name 2>/dev/null | grep -q . &&
      ! kubectl get powernotificationdelivery -n aura-system -o json 2>/dev/null | jq -e --arg channel "$channel_name" 'any(.items[]; .spec.channel.name == $channel)' >/dev/null; then
      break
    fi
    sleep 2
  done

  if kubectl get namespace "$fixture_namespace" >/dev/null 2>&1 ||
    kubectl get powernotificationchannel "$channel_name" -n aura-system >/dev/null 2>&1 ||
    kubectl get powerpolicy "$policy_name" -n aura-system >/dev/null 2>&1 ||
    kubectl get secret "$secret_name" -n aura-system >/dev/null 2>&1 ||
    kubectl get powertarget -n aura-system -l "power.aura.sh/target-namespace=${fixture_namespace}" -o name 2>/dev/null | grep -q . ||
    kubectl get powerauditevent -n aura-system -l "power.aura.sh/target-namespace=${fixture_namespace}" -o name 2>/dev/null | grep -q . ||
    kubectl get powernotificationdelivery -n aura-system -o json 2>/dev/null | jq -e --arg channel "$channel_name" 'any(.items[]; .spec.channel.name == $channel)' >/dev/null; then
    cleanup_status=1
  fi
  [[ "$cleanup_status" -eq 0 ]] || { echo "FATAL: notification fixture cleanup was not verified" >&2; exit 90; }
  exit "$original_status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

for resource in "namespace/${fixture_namespace}" "powerpolicy/${policy_name}" "powernotificationchannel/${channel_name}" "secret/${secret_name}"; do
  resource_name="${resource#*/}"
  resource_kind="${resource%%/*}"
  resource_namespace="aura-system"
  [[ "$resource_kind" == "namespace" ]] && resource_namespace=""
  if { [[ -n "$resource_namespace" ]] && kubectl get "$resource_kind" "$resource_name" -n "$resource_namespace" >/dev/null 2>&1; } ||
    { [[ -z "$resource_namespace" ]] && kubectl get "$resource_kind" "$resource_name" >/dev/null 2>&1; }; then
    echo "refusing mutation: fixture resource already exists" >&2
    exit 4
  fi
done

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

sed -e "s/namespace: PLACEHOLDER/namespace: ${fixture_namespace}/" -e "s/PLACEHOLDER_RUN/${run_id}/" <<'YAML' | kubectl create -f - >/dev/null
apiVersion: v1
kind: ConfigMap
metadata:
  name: notification-receiver-code
  namespace: PLACEHOLDER
  labels:
    aura-power-quality/run: "PLACEHOLDER_RUN"
data:
  receiver.py: |
    import json
    import os
    import threading
    from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

    lock = threading.Lock()
    state = {"count": 0, "requests": [], "idempotencyKeys": []}
    token = os.environ["WEBHOOK_TOKEN"]

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, _format, *_args):
            return

        def do_GET(self):
            if self.path != "/healthz":
                self.send_response(404)
                self.end_headers()
                return
            self.send_response(204)
            self.end_headers()

        def do_POST(self):
            if self.path != "/hook/" + token:
                self.send_response(404)
                self.end_headers()
                return
            try:
                length = int(self.headers.get("Content-Length", "0"))
                payload = json.loads(self.rfile.read(length))
            except (ValueError, json.JSONDecodeError):
                self.send_response(400)
                self.end_headers()
                return
            with lock:
                state["count"] += 1
                state["requests"].append(payload)
                key = self.headers.get("Idempotency-Key", "")
                state["idempotencyKeys"].append(key)
                first_delivery_request = state["count"] == 1
                with open("/tmp/receiver-state.json.tmp", "w", encoding="utf-8") as output:
                    json.dump(state, output)
                os.replace("/tmp/receiver-state.json.tmp", "/tmp/receiver-state.json")
            # Persist the first accepted request, then deliberately outlive the
            # controller's HTTP request while the test restarts its leader.
            if first_delivery_request:
                import time
                time.sleep(60)
            self.send_response(204)
            self.end_headers()

    ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
YAML

token_b64="$(printf '%s' "$webhook_token" | base64 | tr -d '\n')"
url_b64="$(printf '%s' "$webhook_url" | base64 | tr -d '\n')"
kubectl create -f - >/dev/null <<YAML
apiVersion: v1
kind: Secret
metadata:
  name: notification-receiver-token
  namespace: ${fixture_namespace}
  labels:
    aura-power-quality/run: "${run_id}"
type: Opaque
data:
  token: ${token_b64}
---
apiVersion: v1
kind: Secret
metadata:
  name: ${secret_name}
  namespace: aura-system
  labels:
    aura-power-quality/run: "${run_id}"
type: Opaque
data:
  url: ${url_b64}
YAML
unset token_b64 url_b64 webhook_url

kubectl create -f - >/dev/null <<YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${receiver_name}
  namespace: ${fixture_namespace}
  labels:
    app: ${receiver_name}
    aura-power-quality/run: "${run_id}"
  annotations:
    aura.sh/power-exempt: "true"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ${receiver_name}
  template:
    metadata:
      labels:
        app: ${receiver_name}
        aura-power-quality/run: "${run_id}"
    spec:
      automountServiceAccountToken: false
      containers:
        - name: receiver
          image: ${receiver_image}
          command: ["python", "/app/receiver.py"]
          env:
            - name: PYTHONDONTWRITEBYTECODE
              value: "1"
            - name: WEBHOOK_TOKEN
              valueFrom:
                secretKeyRef:
                  name: notification-receiver-token
                  key: token
          ports:
            - name: http
              containerPort: 8080
          readinessProbe:
            httpGet:
              path: /healthz
              port: http
          resources:
            requests:
              cpu: 10m
              memory: 24Mi
            limits:
              cpu: 100m
              memory: 64Mi
          securityContext:
            allowPrivilegeEscalation: false
            capabilities:
              drop: ["ALL"]
            readOnlyRootFilesystem: true
            runAsNonRoot: true
            runAsUser: 65532
          volumeMounts:
            - name: code
              mountPath: /app
              readOnly: true
            - name: state
              mountPath: /tmp
      securityContext:
        seccompProfile:
          type: RuntimeDefault
      volumes:
        - name: code
          configMap:
            name: notification-receiver-code
        - name: state
          emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: ${receiver_name}
  namespace: ${fixture_namespace}
  labels:
    aura-power-quality/run: "${run_id}"
spec:
  selector:
    app: ${receiver_name}
  ports:
    - name: http
      port: 8080
      targetPort: http
YAML
kubectl rollout status deployment "$receiver_name" -n "$fixture_namespace" --timeout=120s

kubectl create -f - >/dev/null <<YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${workload_name}
  namespace: ${fixture_namespace}
  labels:
    app: ${workload_name}
    aura-power-quality/run: "${run_id}"
  annotations:
    aura.sh/power-eligible: "true"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ${workload_name}
  template:
    metadata:
      labels:
        app: ${workload_name}
    spec:
      containers:
        - name: pause
          image: registry.k8s.io/pause:3.10
YAML
kubectl rollout status deployment "$workload_name" -n "$fixture_namespace" --timeout=120s
workload_uid="$(kubectl get deployment "$workload_name" -n "$fixture_namespace" -o jsonpath='{.metadata.uid}')"

kubectl create -f - >/dev/null <<YAML
apiVersion: power.aura.sh/v1alpha1
kind: PowerNotificationChannel
metadata:
  name: ${channel_name}
  namespace: aura-system
  labels:
    aura-power-quality/run: "${run_id}"
spec:
  type: generic
  urlFrom:
    name: ${secret_name}
    key: url
  events: ["workload.powered_down", "workload.restored"]
  namespaceFilter: ["${fixture_namespace}"]
  throttle: 1s
  enabled: true
  deliveryPolicy: at-least-once
  maxDeliveryAttempts: 4
---
apiVersion: power.aura.sh/v1alpha1
kind: PowerPolicy
metadata:
  name: ${policy_name}
  namespace: aura-system
  labels:
    aura-power-quality/run: "${run_id}"
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

deadline=$((SECONDS + timeout_seconds)); replicas=""
while (( SECONDS < deadline )); do
  replicas="$(kubectl get deployment "$workload_name" -n "$fixture_namespace" -o jsonpath='{.spec.replicas}')"
  [[ "$replicas" == "0" ]] && break
  sleep 3
done
[[ "$replicas" == "0" ]] || { echo "FAIL: notification fixture did not power down" >&2; exit 10; }

deadline=$((SECONDS + timeout_seconds)); delivery_json="" receiver_state=""
while (( SECONDS < deadline )); do
  delivery_json="$(kubectl get powernotificationdelivery -n aura-system -o json 2>/dev/null | jq --arg channel "$channel_name" '.items = [.items[] | select(.spec.channel.name == $channel)]' || true)"
  receiver_pod="$(kubectl get pod -n "$fixture_namespace" -l "app=${receiver_name}" -o jsonpath='{.items[0].metadata.name}')"
  receiver_state="$(kubectl exec -n "$fixture_namespace" "$receiver_pod" -- sh -c 'cat /tmp/receiver-state.json 2>/dev/null || true')"
  if jq -e '.items | length == 1 and .[0].status.phase == "InProgress" and .[0].status.attemptCount == 1' <<<"$delivery_json" >/dev/null 2>&1 &&
     jq -e '.count == 1 and (.idempotencyKeys[0] | length) > 0' <<<"$receiver_state" >/dev/null 2>&1; then
    break
  fi
  sleep 3
done
first_delivery_name="$(jq -er '.items[0].metadata.name' <<<"$delivery_json")"
first_idempotency_key="$(jq -er '.items[0].spec.idempotencyKey' <<<"$delivery_json")"
first_audit_name="$(jq -er '.items[0].spec.auditEvent.name' <<<"$delivery_json")"
[[ "$(jq -er '.idempotencyKeys[0]' <<<"$receiver_state")" == "$first_idempotency_key" ]] || { echo "FAIL: receiver did not observe the durable idempotency key" >&2; exit 11; }
if kubectl patch powernotificationdelivery "$first_delivery_name" -n aura-system --type=merge -p '{"spec":{"event":{"reason":"tampered"}}}' >/dev/null 2>&1; then
  echo "FAIL: API server allowed mutation of an immutable delivery contract" >&2
  exit 11
fi

# Kill the active request after the receiver has durably accepted it. The new
# leader must recover the InProgress record and replay with the same key.
controller_logs_before_restart="$(kubectl logs -n aura-system -l app.kubernetes.io/component=controller --all-containers --prefix 2>/dev/null || true)"
kubectl rollout restart deployment -n aura-system -l app.kubernetes.io/component=controller >/dev/null
kubectl rollout status deployment -n aura-system -l app.kubernetes.io/component=controller --timeout=120s
deadline=$((SECONDS + timeout_seconds)); channel_json=""
while (( SECONDS < deadline )); do
  delivery_json="$(kubectl get powernotificationdelivery "$first_delivery_name" -n aura-system -o json)"
  channel_json="$(kubectl get powernotificationchannel "$channel_name" -n aura-system -o json)"
  receiver_state="$(kubectl exec -n "$fixture_namespace" "$receiver_pod" -- cat /tmp/receiver-state.json)"
  if jq -e '.status.phase == "Succeeded" and .status.attemptCount == 2 and .status.channelStatusRecorded == true' <<<"$delivery_json" >/dev/null &&
     jq -e '.status.totalSent == 1 and (.status.totalErrors // 0) == 0' <<<"$channel_json" >/dev/null &&
     jq -e --arg key "$first_idempotency_key" '.count == 2 and .idempotencyKeys == [$key, $key]' <<<"$receiver_state" >/dev/null; then
    break
  fi
  sleep 3
done
jq -e '.status.phase == "Succeeded" and .status.attemptCount == 2 and .status.response == "accepted" and .status.channelStatusRecorded == true' <<<"$delivery_json" >/dev/null || { echo "FAIL: restart did not resume the durable delivery" >&2; exit 12; }
jq -e --arg key "$first_idempotency_key" '.count == 2 and .idempotencyKeys == [$key, $key]' <<<"$receiver_state" >/dev/null || { echo "FAIL: replay did not preserve the provider idempotency key" >&2; exit 13; }
kubectl get powerauditevent "$first_audit_name" -n aura-system -o json | jq -e --arg ns "$fixture_namespace" --arg uid "$workload_uid" '
  .spec.action == "workload.powered_down" and .spec.result == "success" and
  .spec.target.namespace == $ns and .spec.target.kind == "Deployment" and .spec.target.uid == $uid
' >/dev/null || { echo "FAIL: delivery does not correlate to the powered-down audit event" >&2; exit 13; }

policy_restored=false
for _ in $(seq 1 20); do
  if kubectl patch powerpolicy "$policy_name" -n aura-system --type=merge -p '{"spec":{"schedule":{"desiredState":"on","windows":[]}}}' >/dev/null 2>&1; then
    policy_restored=true
    break
  fi
  sleep 2
done
[[ "$policy_restored" == true ]] || { echo "FAIL: validation webhook did not recover after controller restart" >&2; exit 14; }
deadline=$((SECONDS + timeout_seconds)); replicas=""
while (( SECONDS < deadline )); do
  replicas="$(kubectl get deployment "$workload_name" -n "$fixture_namespace" -o jsonpath='{.spec.replicas}')"
  [[ "$replicas" == "1" ]] && break
  sleep 3
done
[[ "$replicas" == "1" ]] || { echo "FAIL: notification fixture did not restore" >&2; exit 14; }

deadline=$((SECONDS + timeout_seconds)); channel_json=""
while (( SECONDS < deadline )); do
  channel_json="$(kubectl get powernotificationchannel "$channel_name" -n aura-system -o json)"
  if jq -e '(.status.totalErrors // 0) == 0 and .status.totalSent == 2 and .status.lastAttempt.phase == "Succeeded"' <<<"$channel_json" >/dev/null; then
    break
  fi
  sleep 3
done
jq -e '
  (.status.totalErrors // 0) == 0 and .status.totalSent == 2 and
  (.status.recentAttempts | length) == 2 and
  .status.recentAttempts[0].phase == "Succeeded" and
  .status.lastAttempt.phase == "Succeeded" and
  .status.lastAttempt.providerStatusCode == 204 and .status.lastAttempt.attemptCount == 1 and
  .status.lastAttempt.response == "accepted" and (.status.lastError // "") == "" and
  (.status.lastAttempt.eventIDs | length) == 1 and (.status.lastAttempt.auditEventRefs | length) == 1
' <<<"$channel_json" >/dev/null || { echo "FAIL: successful transition did not preserve failure history and durable counters" >&2; exit 15; }
succeeded_attempt_id="$(jq -er '.status.lastAttempt.id' <<<"$channel_json")"
succeeded_audit_ref="$(jq -er '.status.lastAttempt.auditEventRefs[0]' <<<"$channel_json")"
succeeded_audit_name="${succeeded_audit_ref#aura-system/}"
kubectl get powerauditevent "$succeeded_audit_name" -n aura-system -o json | jq -e --arg ns "$fixture_namespace" --arg uid "$workload_uid" '
  .spec.action == "workload.restored" and .spec.result == "success" and
  .spec.target.namespace == $ns and .spec.target.kind == "Deployment" and .spec.target.uid == $uid
' >/dev/null || { echo "FAIL: successful delivery does not correlate to the restored audit event" >&2; exit 16; }

receiver_state="$(kubectl exec -n "$fixture_namespace" "$receiver_pod" -- cat /tmp/receiver-state.json)"
jq -e --arg attempt "$succeeded_attempt_id" --arg audit "$succeeded_audit_ref" '
  .count == 3 and (.requests | length) == 3 and
  .requests[2].correlation.attemptID == $attempt and
  .requests[2].correlation.auditEventRefs == [$audit] and
  (.idempotencyKeys[2] | length) > 0
' <<<"$receiver_state" >/dev/null || { echo "FAIL: receiver payload and durable successful attempt are not correlated" >&2; exit 17; }

# Neither the durable public status nor controller logs may disclose the Secret
# value or the token embedded in the destination URL.
if grep -Fq "$webhook_token" <<<"$channel_json"; then
  echo "FAIL: webhook credential leaked into notification status" >&2
  exit 18
fi
controller_logs="${controller_logs_before_restart}
$(kubectl logs -n aura-system -l app.kubernetes.io/component=controller --all-containers --prefix 2>/dev/null || true)"
if grep -Fq "$webhook_token" <<<"$controller_logs"; then
  echo "FAIL: webhook credential leaked into controller logs" >&2
  exit 18
fi

echo "kind_notification_journey=passed secret_url=true durable_outbox=true leader_restart=true stable_idempotency_key=true at_least_once=true audit_correlation=true durable_counters=true cleanup_guarded=true"
