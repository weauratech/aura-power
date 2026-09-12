#!/usr/bin/env bash
set -Eeuo pipefail

chart="${1:-charts/aura-power}"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT

helm template aura-power "$chart" --namespace aura-system >"$scratch/disabled.yaml"
if grep -q 'kind: ValidatingWebhookConfiguration' "$scratch/disabled.yaml"; then
  echo "webhook resources rendered while disabled" >&2
  exit 1
fi

helm template aura-power "$chart" --namespace aura-system \
  --set webhook.enabled=true >"$scratch/self-signed.yaml"
grep -q 'kind: ValidatingWebhookConfiguration' "$scratch/self-signed.yaml"
grep -q 'kind: Secret' "$scratch/self-signed.yaml"
grep -q 'name: WEBHOOK_ENABLED' "$scratch/self-signed.yaml"
grep -q 'path: /validate-power-aura-sh-v1alpha1-powerpolicy' "$scratch/self-signed.yaml"
grep -q 'path: /validate-power-aura-sh-v1alpha1-poweroverride' "$scratch/self-signed.yaml"
if [[ "$(grep -c 'caBundle: ' "$scratch/self-signed.yaml")" -ne 2 ]]; then
  echo "both validation routes must trust the generated CA" >&2
  exit 1
fi

if helm template aura-power "$chart" --namespace aura-system \
  --set webhook.enabled=true \
  --set webhook.certManager.enabled=true >"$scratch/missing-issuer.yaml" 2>/dev/null; then
  echo "cert-manager mode accepted an empty issuer name" >&2
  exit 1
fi

helm template aura-power "$chart" --namespace aura-system \
  --set webhook.enabled=true \
  --set webhook.certManager.enabled=true \
  --set webhook.certManager.issuerRef.name=platform-ca >"$scratch/cert-manager.yaml"
grep -q 'kind: Certificate' "$scratch/cert-manager.yaml"
grep -q 'cert-manager.io/inject-ca-from:' "$scratch/cert-manager.yaml"
if grep -q 'caBundle: ' "$scratch/cert-manager.yaml"; then
  echo "cert-manager mode rendered a stale static CA bundle" >&2
  exit 1
fi

echo "Helm webhook contracts passed"
