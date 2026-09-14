#!/usr/bin/env bash
set -Eeuo pipefail

: "${KUBECONFIG:?set KUBECONFIG to the disposable Kind kubeconfig}"
[[ "$(kubectl config current-context)" == "kind-aura-power-quality" ]] || { echo "refusing mutation: this runner is restricted to kind-aura-power-quality" >&2; exit 2; }

auth_before="$(kubectl get secret aura-power-server-secret -n aura-system -o json | jq -S '.data' | openssl dgst -sha256 | awk '{print $NF}')"
tls_before="$(kubectl get secret aura-power-controller-webhook-tls -n aura-system -o json | jq -S '.data' | openssl dgst -sha256 | awk '{print $NF}')"
helm upgrade aura-power charts/aura-power --namespace aura-system --reuse-values --wait --timeout 5m >/dev/null
auth_after="$(kubectl get secret aura-power-server-secret -n aura-system -o json | jq -S '.data' | openssl dgst -sha256 | awk '{print $NF}')"
tls_after="$(kubectl get secret aura-power-controller-webhook-tls -n aura-system -o json | jq -S '.data' | openssl dgst -sha256 | awk '{print $NF}')"
[[ "$auth_before" == "$auth_after" ]] || { echo "FAIL: Helm upgrade rotated the auth Secret" >&2; exit 10; }
[[ "$tls_before" == "$tls_after" ]] || { echo "FAIL: Helm upgrade rotated the webhook TLS Secret" >&2; exit 11; }

invalid_manifest="$(cat <<'YAML'
apiVersion: power.aura.sh/v1alpha1
kind: PowerPolicy
metadata:
  name: invalid-timezone-quality
  namespace: aura-system
spec:
  scope:
    namespaces: [fixture]
  schedule:
    desiredState: "off"
    windows:
      - start: "08:00"
        end: "18:00"
        days: [1]
        timezone: "Mars/Olympus"
YAML
)"

# Pods can be Ready a few seconds before the webhook Service has propagated
# usable endpoints. Retry only transport/readiness failures; a successful apply
# remains an immediate failure, and the gate passes only on the validator's
# semantic rejection.
deadline=$((SECONDS + 60))
while true; do
  invalid_output="$(kubectl apply -f - 2>&1 <<<"$invalid_manifest" || true)"
  if kubectl get powerpolicy invalid-timezone-quality -n aura-system >/dev/null 2>&1; then
    kubectl delete powerpolicy invalid-timezone-quality -n aura-system >/dev/null
    echo "FAIL: admission accepted an invalid IANA timezone" >&2
    exit 12
  fi
  if grep -Eiq 'denied|invalid|timezone|time zone' <<<"$invalid_output"; then
    break
  fi
  if grep -Eiq 'failed calling webhook|connection refused|no endpoints available|service unavailable|context deadline exceeded|tls handshake timeout|unexpected eof|the server is currently unable to handle the request' <<<"$invalid_output"; then
    (( SECONDS < deadline )) || { echo "FAIL: admission webhook did not become available: $invalid_output" >&2; exit 13; }
    sleep 2
    continue
  fi
  echo "FAIL: invalid timezone failed without an admission explanation: $invalid_output" >&2
  exit 13
done
echo "kind_helm_upgrade_admission=passed auth_secret_stable=true webhook_tls_stable=true invalid_timezone_rejected=true"
