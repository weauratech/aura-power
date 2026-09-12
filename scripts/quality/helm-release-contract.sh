#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

for crd in powerpolicies poweroverrides; do
  chart="charts/aura-power/crds/${crd}.yaml"
  config="config/crd/bases/power.aura.sh_${crd}.yaml"
  rg -q 'namespaceGroups:' "$chart"
  rg -q 'namespaceGroups:' "$config"
done

# Chart.appVersion omits the conventional Git tag prefix. The release workflow
# must publish and sign the same unprefixed image tag used by Helm defaults.
rg -q 'VERSION="?\$\{TAG#v\}"?' .github/workflows/release.yaml
rg -q 'IMAGE_SERVER.*\$\{VERSION\}' .github/workflows/release.yaml
rg -q 'IMAGE_CONTROLLER.*\$\{VERSION\}' .github/workflows/release.yaml

default_render="$(mktemp)"
ephemeral_render="$(mktemp)"
external_secret_render="$(mktemp)"
trap 'rm -f "$default_render" "$ephemeral_render" "$external_secret_render"' EXIT

helm template aura-power charts/aura-power >"$default_render"
helm template aura-power charts/aura-power --set server.persistence.enabled=false >"$ephemeral_render"
helm template aura-power charts/aura-power --set server.auth.existingSecret=managed-auth >"$external_secret_render"

rg -q 'name: ACCESS_TOKEN_TTL' "$default_render"
rg -q 'name: REFRESH_TOKEN_TTL' "$default_render"
rg -q 'name: CONTROL_NAMESPACE' "$default_render"
rg -q 'name: LEADER_ELECTION_ENABLED' "$default_render"
rg -q 'name: SYSTEM_NAMESPACES' "$default_render"
rg -U -q 'name: data\n[[:space:]]+emptyDir:' "$ephemeral_render"
if rg -q '^kind: Secret$' "$external_secret_render"; then
  echo "server.auth.existingSecret unexpectedly rendered a managed Secret" >&2
  exit 1
fi
rg -U -q 'name: managed-auth\n[[:space:]]+key: jwt-secret' "$external_secret_render"
rg -U -q 'name: managed-auth\n[[:space:]]+key: admin-password' "$external_secret_render"

if helm template aura-power charts/aura-power --set server.replicas=2 >/dev/null 2>&1; then
  echo "server.replicas=2 must be rejected while SQLite is pod-local" >&2
  exit 1
fi
if helm template aura-power charts/aura-power --set controller.replicas=2 --set controller.leaderElection.enabled=false >/dev/null 2>&1; then
  echo "multiple controllers without leader election must be rejected" >&2
  exit 1
fi

observability_render="$(helm template aura-power charts/aura-power --namespace aura-system --set networkPolicy.enabled=true --set serviceMonitor.enabled=true --set serviceMonitor.namespace=monitoring)"
grep -q 'port: 9003' <<<"$observability_render"
[[ "$(grep -c '^kind: ServiceMonitor$' <<<"$observability_render")" -eq 2 ]]
[[ "$(grep -c '^  namespace: monitoring$' <<<"$observability_render")" -eq 2 ]]
[[ "$(grep -c '^      - aura-system$' <<<"$observability_render")" -eq 2 ]]

external_webhook_render="$(helm template aura-power charts/aura-power --set webhook.enabled=true --set webhook.existingSecret=managed-webhook --set webhook.caBundle=Y2E=)"
grep -q 'secretName: managed-webhook' <<<"$external_webhook_render"
grep -q 'caBundle: Y2E=' <<<"$external_webhook_render"
[[ "$(grep -c '^kind: Secret$' <<<"$external_webhook_render")" -eq 1 ]]
if helm template aura-power charts/aura-power --set webhook.enabled=true --set webhook.existingSecret=managed-webhook >/dev/null 2>&1; then
  echo "webhook.existingSecret without webhook.caBundle must be rejected" >&2
  exit 1
fi

echo "Helm/release contract checks passed"
